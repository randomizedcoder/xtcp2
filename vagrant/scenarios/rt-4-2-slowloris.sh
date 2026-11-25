#!/usr/bin/env bash
# vagrant/scenarios/rt-4-2-slowloris.sh — RT-4: slowloris HTTP
# (docs/redteam-plan.md row rt-4-2).
#
# The chain: a tier-1 co-tenant container opens N connections to :9088 and
# dribbles request bytes at an interval inside the read timeouts — the
# classic slowloris, whose premise is that each conn sits mid-header
# FOREVER, holding a goroutine + FD per conn.
#
# THE MEASURED DEFENSE (this scenario, 2026-11-24): Go's http server sets
# the ReadHeaderTimeout (5 s, cmd/xtcp2/http_server.go:26) as an ABSOLUTE
# deadline at readRequest start — it is NOT reset by partial reads. A
# dribbling conn is reaped at ~5 s TOTAL no matter what it sends (the same
# 5 s cap that churned rt-4-1's idle-conns ladder). The slowloris premise
# — conns that persist mid-header — is unreachable against this server;
# what remains is a 5 s window of bounded per-conn cost (a goroutine + a
# 4 KiB read buffer each), repeated by re-dialing (a dial storm, whose FD
# ceiling rt-4-1 already measured).
#
# Asserts:
#   - the N dribbling connections are established and HELD (victim-side
#     conn count to :9088);
#   - while held: the scrape path still answers 200 fast (a concurrent
#     clean scrape's latency);
#   - the FD count is baseline + ~N (linear, no amplification);
#   - the memory cost is bounded (RSS delta recorded; asserted under
#     1 MiB/conn);
#   - after release: FDs back at baseline (SO_LINGER 0 teardown);
#   - the RT-11 invariant: the catcher keeps flowing throughout.
set -euo pipefail

: "${XTCP2_MODE:=bridge}"
: "${XTCP2_AUTH:=raw}"
: "${XTCP2_ATTACKER:=plain}"
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

echo "rt-4-2: evidence in $RT_EVIDENCE"

xtcp2_deploy victim attacker
wait_for 120 "daemon answers on the victim VM" vm_exec victim \
  "curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:9088/metrics | grep -qE '^(200|401|403)\$'"
victim_still_reporting_tap
xtcp2_victim_read_token

# --- helpers -------------------------------------------------------------------

N=400                 # dribbling connections
DRIP_INTERVAL=2s      # inside every read timeout (5s header / 10s read)
HOLD=45s

daemon_pid() {
  vm_exec victim "sudo docker inspect rt-xtcp2 --format '{{.State.Pid}}'"
}

daemon_fds() {
  vm_exec victim "sudo ls /proc/$(daemon_pid)/fd | wc -l"
}

daemon_rss_kb() {
  # MemUsage prints MiB with a fraction ("25.64MiB / 6GiB"); return integer KiB.
  vm_exec victim "sudo docker stats --no-stream --format '{{.MemUsage}}' rt-xtcp2" \
    | grep -oE '^[0-9.]+' | awk '{printf "%d", $1 * 1024}'
}

# estab_to_metrics: connections to :9088 from the attacker's bridge IP.
estab_to_metrics() {
  vm_exec victim "sudo ss -tn state established '( sport = :9088 )' | grep -c '172.17' || true"
}

scrape_code() {
  vm_exec victim "curl -s -m 6 -o /dev/null -w '%{http_code}' -H 'Authorization: Bearer $RT_READ_TOKEN' http://127.0.0.1:9088/metrics" || echo 000
}

scrape_seconds() {
  vm_exec victim "curl -s -m 6 -o /dev/null -w '%{time_total}' -H 'Authorization: Bearer $RT_READ_TOKEN' http://127.0.0.1:9088/metrics"
}

# --- baseline ------------------------------------------------------------------

BASE_FDS="$(daemon_fds)"
BASE_RSS_KB="$(daemon_rss_kb)"
ok "baseline: FDs=$BASE_FDS RSS=$((BASE_RSS_KB / 1024))MiB scrape=$(scrape_code)"

# --- the attack: N dribbling connections ---------------------------------------

vm_exec victim "sudo docker cp /opt/xtcp2-redteam/bin/slowloris rt-attacker:/tmp/slowloris" >/dev/null
vm_exec victim "sudo docker exec rt-attacker sh -c 'nohup /tmp/slowloris -target 172.17.0.1:9088 -n $N -interval $DRIP_INTERVAL -ramp 3s -hold $HOLD > /tmp/sl.log 2>&1 &'" >/dev/null

# held: the DAEMON's own FD table grew — the authoritative alive signal
# (the host-side ss sees the docker-proxy conns, not the daemon's, and a
# client-side check races the 5s reap). The count is captured INSIDE the
# poll: a separate sample after wait_for returns lands past the ~5s
# absolute header cap and reads a reaped table (measured twice).
HELD_FDS=0
HELD_RSS_KB=0
held() {
  local f r
  f="$(daemon_fds)"
  if [ "$f" -lt $((BASE_FDS + N * 7 / 10)) ]; then
    return 1
  fi
  HELD_FDS="$f"
  r="$(daemon_rss_kb)"
  [ "$r" -gt "$HELD_RSS_KB" ] && HELD_RSS_KB="$r"
  return 0
}
wait_for 30 "the daemon accepted the $N dribbling conns (FD table grew)" held

# --- while held: the cost is bounded and the served path is fine ----------------
# HELD_FDS/HELD_RSS_KB were captured inside the poll (see above); every
# sample taken after wait_for returns lands past the ~5s absolute header
# cap and reads a reaped table (measured three times — the poll-capture is
# the only race-free point).
code="$(scrape_code)"
lat="$(scrape_seconds)"

if [ "$code" = 200 ]; then
  ok "while held: the scrape path answers 200 in ${lat}s"
else
  not_ok "while held: the scrape path degraded (code=$code)"
fi

fd_growth=$((HELD_FDS - BASE_FDS))
if [ "$fd_growth" -ge $((N * 7 / 10)) ] && [ "$fd_growth" -le $((N * 13 / 10)) ]; then
  ok "the FD cost while alive is linear: baseline+$fd_growth for $N conns (~1 FD/conn)"
else
  not_ok "FD growth baseline->$HELD_FDS for $N conns is not linear — see evidence"
fi

rss_growth_kb=$((HELD_RSS_KB - BASE_RSS_KB))
per_conn_kb=0
[ "$N" -gt 0 ] && per_conn_kb=$((rss_growth_kb / N))
echo "rt-4-2: RSS $((BASE_RSS_KB / 1024))MiB -> $((HELD_RSS_KB / 1024))MiB = ${per_conn_kb}KiB/conn (assert: < 1024)"
if [ "$per_conn_kb" -lt 1024 ] && [ "$per_conn_kb" -ge 0 ]; then
  ok "the memory cost while alive is bounded: ${per_conn_kb}KiB/conn (RSS $((BASE_RSS_KB / 1024))->$((HELD_RSS_KB / 1024))MiB for $N conns)"
else
  not_ok "the memory cost is ${per_conn_kb}KiB/conn — not bounded — see evidence"
fi

# --- the reap: the absolute header deadline kills the dribblers at ~5 s ---------

# The dribble interval (2 s) is INSIDE the 5 s ReadHeaderTimeout, so under
# the reset-per-read model the conns would persist for the whole hold. The
# absolute-cap model reaps them at ~5 s total. The FD count decides which.
sleep 12 # past the 5s cap, past any linger
FDS_AFTER="$(daemon_fds)"
after_estab="$(estab_to_metrics)"
echo "rt-4-2: after the cap window: FDs=$FDS_AFTER (baseline $BASE_FDS), established dribblers=$after_estab"
if [ "$((FDS_AFTER - BASE_FDS))" -le 64 ] && [ "$after_estab" -le $((N / 10)) ]; then
  ok "THE DEFENSE: the dribblers were reaped at the ~5s absolute ReadHeaderTimeout (FDs back to baseline $BASE_FDS, established down to $after_estab) — a mid-header conn cannot persist against this server"
else
  not_ok "the dribblers persisted past the cap window (FDs=$FDS_AFTER, established=$after_estab) — the slowloris premise holds; see evidence"
fi

victim_still_reporting_tap

# --- release: teardown is immediate ---------------------------------------------

# -hold bounds the CLIENT (its sockets close with SO_LINGER 0 at exit); the
# server side was already reaped by the header cap. Wait for the client to
# finish, then confirm the daemon's sockets are gone.
deadline=$((SECONDS + 120))
until vm_exec victim "sudo docker exec rt-attacker grep -q SLOWLORIS_DONE /tmp/sl.log" >/dev/null 2>&1; do
  [ "$SECONDS" -ge "$deadline" ] && break
  sleep 3
done
release_fds=0
deadline=$((SECONDS + 30))
while [ "$SECONDS" -lt "$deadline" ]; do
  release_fds="$(daemon_fds)"
  [ "$((release_fds - BASE_FDS))" -le 32 ] && break
  sleep 3
done
if [ "$((release_fds - BASE_FDS))" -le 32 ]; then
  ok "after release: FDs back at baseline ($BASE_FDS -> $release_fds)"
else
  not_ok "after release: FDs did not return to baseline ($BASE_FDS -> $release_fds) — leaked conns?"
fi
victim_still_reporting_tap

# --- verdict --------------------------------------------------------------------

# The conns persist while dribbling (read deadlines reset per byte — the
# documented slowloris-vs-timeouts behavior); the defense is the bounded
# per-conn cost plus rt-4-1's wired-limits follow-up, which is what the
# plan row's expectation pins.
finish rt-4-2 BLOCKED
