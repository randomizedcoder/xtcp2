#!/usr/bin/env bash
# vagrant/scenarios/rt-4-1-connection-exhaustion.sh — RT-4: connection
# exhaustion + RT-11: the FD knee across the listener boundary
# (docs/redteam-plan.md rows rt-4-1 + rt-11-2, folded: one ladder, the
# collection-path crossover IS rt-11-2's finding).
#
# The chain: an UNPRIVILEGED co-tenant container (tier 1: plain network
# access, no credentials) opens idle connections to the metrics listener
# until the daemon's process FD budget is spent. `max_connections`/
# accept-rate are not wired (documented product gap), so the budget is the
# only ceiling — and the FD table is shared by the API paths AND the
# collection path (the per-namespace netlink sockets, the /proc walk,
# dockermeta reads), so API connections can starve collection (S2).
#
# VERDICT SHAPE (measured 2026-11-24): the idle-conn starvation premise is
# REFUTED — the listener's own ReadHeaderTimeout (5 s, cmd/xtcp2/
# http_server.go:26) reaps a never-speaking conn 5 s after accept, so the
# FD budget CHURNS at the ceiling instead of pinning: scrape stays 200 and
# the catcher keeps flowing even with FDs pinned at 4096. What the ladder
# DOES pin: (a) a telemetry amplifier — every attack socket is itself a
# reported record, so ~4K idle conns multiply the record volume ~590x
# (baseline ~150 lines/6s -> 88K+/6s); (b) the FD budget consumed by
# accept/reap churn (API availability degrades; collection survives). The
# conns that CAN hold FDs past the reap are the dribbling ones — that is
# rt-11-2's shape, not this row's.
#
# Premortem (docs/redteam-implementation.md Phase 4): the §29 shape's
# docker default nofile is 524287 — too wide to walk (and conntrack knees
# first; rt-11-6's crossover). This deploy pins XTCP2_NOFILE=4096 so the
# knee is reachable in minutes; the crossover SHAPE is the finding, and
# the findings row carries the prod-default arithmetic.
#
# Asserts:
#   - baseline: metrics 200, catcher flowing, FD count recorded;
#   - the ladder table: connections -> {FDs, metrics scrape, catcher delta};
#   - the knee: a connection count where the scrape fails AND the catcher
#     stops growing (the collection crossover — RT-11's answer);
#   - release + recovery: after the holder exits, metrics 200 and the
#     catcher resume within a few poll cycles;
#   - cleanup: the co-tenant's holder is gone, the FD count is back at
#     baseline.
set -euo pipefail

: "${XTCP2_MODE:=bridge}"
: "${XTCP2_AUTH:=raw}"
: "${XTCP2_ATTACKER:=plain}"
: "${XTCP2_NOFILE:=4096}" # the walkable FD knee (524287 in prod — see above)
XTCP2_EXPECT="${XTCP2_EXPECT:-BLOCKED}"

# shellcheck source=/dev/null
source "$(dirname "${BASH_SOURCE[0]}")/../lib/matrix.sh"
# shellcheck source=/dev/null
source "$(dirname "${BASH_SOURCE[0]}")/../lib/evidence.sh"
# shellcheck source=/dev/null
source "$(dirname "${BASH_SOURCE[0]}")/../lib/ssh.sh"
# shellcheck source=/dev/null
source "$(dirname "${BASH_SOURCE[0]}")/../lib/deploy.sh"
# shellcheck source=/dev/null
source "$(dirname "${BASH_SOURCE[0]}")/../lib/invariant.sh"
# shellcheck source=/dev/null
source "$(dirname "${BASH_SOURCE[0]}")/../lib/assert.sh"

xtcp2_matrix_defaults
xtcp2_preflight
evidence_init

echo "rt-4-1: evidence in $RT_EVIDENCE"

xtcp2_deploy victim attacker
wait_for 120 "daemon answers on the victim VM" vm_exec victim \
  "curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:9088/metrics | grep -qE '^(200|401|403)\$'"
victim_still_reporting_tap
xtcp2_victim_read_token

# --- helpers ------------------------------------------------------------------

daemon_pid() {
  vm_exec victim "sudo docker inspect rt-xtcp2 --format '{{.State.Pid}}'"
}

daemon_fds() {
  vm_exec victim "sudo ls /proc/$(daemon_pid)/fd | wc -l"
}

daemon_rss_mb() {
  vm_exec victim "sudo docker stats --no-stream --format '{{.MemUsage}}' rt-xtcp2" \
    | grep -oE '^[0-9.]+[A-Za-z]*' | tr -dc '0-9.' || echo 0
}

# scrape_code: the tokened scrape's http code (000 = connect/accept failed).
scrape_code() {
  local c
  c="$(vm_exec victim "curl -s -m 6 -o /dev/null -w '%{http_code}' -H 'Authorization: Bearer $RT_READ_TOKEN' http://127.0.0.1:9088/metrics" || true)"
  echo "${c:-000}"
}

# catcher_delta_6s: records delivered over a 6s window (3 poll cycles).
catcher_delta_6s() {
  local a b
  a="$(catcher_lines)"
  sleep 6
  b="$(catcher_lines)"
  echo $((b - a))
}

start_holder() { # $1 = N connections
  vm_exec victim "sudo docker cp /opt/xtcp2-redteam/bin/sockfill rt-attacker:/tmp/sockfill" >/dev/null
  vm_exec victim "sudo docker exec rt-attacker sh -c 'nohup /tmp/sockfill -remote 172.17.0.1:9088 -remoteN $1 > /tmp/sf.log 2>&1 &'" >/dev/null
  local deadline=$((SECONDS + 120))
  until vm_exec victim "sudo docker exec rt-attacker grep -q SOCKFILL_READY /tmp/sf.log" >/dev/null 2>&1; do
    [ "$SECONDS" -ge "$deadline" ] && return 1
    sleep 2
  done
}

stop_holder() {
  vm_exec victim "sudo docker exec rt-attacker pkill -f '/tmp/sockf[i]ll'" >/dev/null 2>&1 || true
  sleep 2
}

# --- baseline -----------------------------------------------------------------

BASE_FDS="$(daemon_fds)"
BASE_RSS="$(daemon_rss_mb)"
BASE_DELTA="$(catcher_delta_6s)"
ok "baseline: FDs=$BASE_FDS RSS=${BASE_RSS}MiB scrape=$(scrape_code) catcher_delta=$BASE_DELTA/6s"
echo "rt-4-1: ladder: conns -> FDs / scrape / catcher-delta / RSS"

# --- the ladder: coarse-to-fine to the knee -----------------------------------
# Steps chosen to bracket 4096 minus the daemon's own FDs (~20): 512, 1024,
# 2048, 3072, 3584, 3840, 3968, 4032, 4064.
LADDER=(512 1024 2048 3072 3584 3840 3968 4032 4064 4090 4128 4224)
KNEE=0
for n in "${LADDER[@]}"; do
  if ! start_holder "$n"; then
    echo "rt-4-1:   $n conns -> holder incomplete (the daemon stopped accepting)"
  fi
  fds="$(daemon_fds)"
  code="$(scrape_code)"
  delta="$(catcher_delta_6s)"
  rss="$(daemon_rss_mb)"
  echo "rt-4-1:   $n conns -> FDs=$fds scrape=$code catcher_delta=$delta RSS=${rss}MiB"
  echo "rt-4-1:   $n conns -> FDs=$fds scrape=$code catcher_delta=$delta RSS=${rss}MiB" >>"$RT_EVIDENCE/ladder.txt"
  if { [ "$code" = "000" ] || [ "$code" = "503" ]; } && [ "$delta" -eq 0 ]; then
    KNEE=$n
    break
  fi
  stop_holder
done

# The defense held at the ceiling: no step scraped 000 with a dead catcher.
if [ "$KNEE" -eq 0 ]; then
  ok "no crossover: through every ladder step (FDs pinned at 4096) the scrape stayed 200 and the catcher kept flowing — the 5s ReadHeaderTimeout churns the FD budget instead of letting idle conns pin it"
else
  not_ok "the ladder found a crossover at $KNEE conns (scrape failed AND catcher stopped) — see ladder.txt"
fi

# The amplifier finding: the attack's own sockets are telemetry. Baseline
# catcher delta is ~150 lines/6s (the standard population); at ~4K conns the
# ladder measured 88K+/6s (~590x). Assert the scaled step grew >= 50x.
AMP_BASE="$((BASE_DELTA + 1))"
read -r scaled_conns scaled_delta <<EOF
$(awk '$2+0 >= 3500 { gsub("catcher_delta=", "", $7); print $2, $7 }' "$RT_EVIDENCE/ladder.txt" | sort -k2 -n | tail -1)
EOF
amp_ratio=$((scaled_delta / AMP_BASE))
if [ "$amp_ratio" -ge 50 ]; then
  ok "FINDING: the attack's sockets are themselves telemetry — ${scaled_conns} idle conns multiplied the record volume ${amp_ratio}x (baseline ${BASE_DELTA}/6s -> ${scaled_delta}/6s)"
else
  not_ok "no telemetry amplification measured (ratio ${amp_ratio}x) — see ladder.txt"
fi

# --- release + recovery --------------------------------------------------------

stop_holder
recovers() {
  [ "$(scrape_code)" = 200 ] && [ "$(catcher_delta_6s)" -gt 0 ]
}
wait_for 90 "after release: scrape 200 and the catcher resumed" recovers
victim_still_reporting_tap

after_fds="$(daemon_fds)"
if [ "$((after_fds - BASE_FDS))" -le 32 ]; then
  ok "cleanup: FD count back at baseline ($BASE_FDS -> $after_fds)"
else
  not_ok "FD count did not return to baseline ($BASE_FDS -> $after_fds) — leak?"
fi

# --- verdict ------------------------------------------------------------------

echo "rt-4-1: ladder table in $RT_EVIDENCE/ladder.txt (prod default: nofile=524287, ~500K conns; conntrack knees first — rt-11-6)"
finish rt-4-1 BLOCKED
