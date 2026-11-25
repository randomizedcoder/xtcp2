#!/usr/bin/env bash
# vagrant/scenarios/rt-11-2-fd-crossover.sh — RT-11: FD exhaustion across
# the listener boundary (docs/redteam-plan.md row rt-11-2).
#
# The chain: can an unprivileged co-tenant stop xtcp2 reporting TCP
# statistics about the base OS and the non-attacker containers by spending
# the daemon's shared FD budget from the API side? Two attack shapes were
# measured before this one (both reaped by the server's own timeouts):
#   - idle conns (rt-4-1's ladder): the 5 s ReadHeaderTimeout is an
#     ABSOLUTE cap from request start — the budget churns at the ceiling
#     but never pins; collection survived every step;
#   - header-dribbling conns (slowhttptest-class slowloris): the same 5 s
#     absolute cap kills a dribbling conn at 5 s total — the drip cannot
#     extend it (net/http sets the header deadline once, at readRequest
#     start; only AFTER the headers complete does ReadTimeout/IdleTimeout
#     take over). So no HTTP-speaking conn can hold an FD past ~30 s.
# The conns that CAN hold FDs are gRPC ones: the server's MaxConnectionIdle
# is 15 min (pkg/xtcp/grpc_server.go:39), so ~4K idle gRPC connections pin
# the FD budget with no accept/reap churn — the strongest pin available.
#
# THE MEASURED QUESTION: at a perfectly-pinned FD budget, does the
# collection path die? The premise says it must (the FD table is shared).
# The design says it must not: the /proc scanner holds its fd persistently
# (pkg/nsdiscover/scanner.go NewScanner, one unix.Open), the per-namespace
# netlink sockets are created once at discovery, and the steady-state poll
# opens ZERO new FDs — so FD exhaustion starves the API (accept: EMFILE)
# but not the records. This scenario measures which claim wins.
#
# Deploy: XTCP2_NOFILE=4096 (the walkable knee; prod default 524287 — the
# premortem arithmetic; the SHAPE of the crossover is the finding).
#
# Asserts:
#   - the gRPC conns pin the FD budget at the ceiling and it STAYS pinned;
#   - the API is unreachable while pinned (scrape 000 — accept EMFILE);
#   - the crossover verdict: the catcher keeps flowing (collection
#     FD-independent by construction) or stops (the premise wins — S2);
#   - the daemon process survives (spent FD table, not a spent process);
#   - release + recovery: scrape 200 and the catcher within a few cycles;
#   - cleanup: FD count back at baseline.
set -euo pipefail

: "${XTCP2_MODE:=bridge}"
: "${XTCP2_AUTH:=raw}"
: "${XTCP2_ATTACKER:=plain}"
: "${XTCP2_NOFILE:=4096}" # the walkable FD knee (rt-4-1's ladder shape)
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

echo "rt-11-2: evidence in $RT_EVIDENCE"

xtcp2_deploy victim attacker
wait_for 120 "daemon answers on the victim VM" vm_exec victim \
  "curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:9088/metrics | grep -qE '^(200|401|403)\$'"
victim_still_reporting_tap
xtcp2_victim_read_token

# --- helpers --------------------------------------------------------------------

N=4200 # past the 4096 budget minus the daemon's own ~80

daemon_pid() {
  vm_exec victim "sudo docker inspect rt-xtcp2 --format '{{.State.Pid}}'"
}

daemon_fds() {
  vm_exec victim "sudo ls /proc/$(daemon_pid)/fd | wc -l"
}

daemon_alive() {
  vm_exec victim "sudo docker inspect rt-xtcp2 --format '{{.State.Running}}' | grep -q true"
}

scrape_code() {
  local c
  c="$(vm_exec victim "curl -s -m 6 -o /dev/null -w '%{http_code}' -H 'Authorization: Bearer $RT_READ_TOKEN' http://127.0.0.1:9088/metrics" || true)"
  echo "${c:-000}"
}

catcher_delta_6s() {
  local a b
  a="$(catcher_lines)"
  sleep 6
  b="$(catcher_lines)"
  echo $((b - a))
}

# --- baseline --------------------------------------------------------------------

BASE_FDS="$(daemon_fds)"
ok "baseline: FDs=$BASE_FDS scrape=$(scrape_code)"

# --- the attack: idle gRPC conns pin the budget -----------------------------------

vm_exec victim "sudo docker cp /opt/xtcp2-redteam/bin/grpcidle rt-attacker:/tmp/grpcidle" >/dev/null
vm_exec victim "sudo docker exec rt-attacker sh -c 'nohup /tmp/grpcidle -target 172.17.0.1:8889 -n $N > /tmp/gi.log 2>&1 &'" >/dev/null

pins() {
  [ "$(daemon_fds)" -ge 4090 ]
}
wait_for 120 "the idle gRPC conns pin the FD budget at the ceiling" pins
PIN_FDS="$(daemon_fds)"
if [ "$PIN_FDS" -ge 4090 ] && [ "$PIN_FDS" -le 4096 ]; then
  ok "the FD budget is pinned at ${PIN_FDS}/4096 (idle gRPC conns — MaxConnectionIdle 15 min, no accept/reap churn)"
else
  not_ok "the FD count is $PIN_FDS (expected ~4096) — see evidence"
fi

# --- the crossover ----------------------------------------------------------------

sleep 6 # a few poll cycles with the budget pinned
code="$(scrape_code)"
delta="$(catcher_delta_6s)"
echo "rt-11-2: pinned: scrape=$code catcher_delta=$delta FDs=$(daemon_fds)"

if [ "$code" != 200 ]; then
  ok "the API is unreachable while the budget is pinned (scrape=$code — accept EMFILE)"
else
  not_ok "the scrape still answers 200 with the budget pinned — see evidence"
fi

if [ "$delta" -gt 0 ]; then
  ok "NO CROSSOVER: the catcher kept flowing ($delta lines/6s) at the pinned budget — the collection path holds its FDs (persistent /proc fd + per-namespace netlink sockets) and opens nothing per poll; FD exhaustion starves the API, not the records"
else
  not_ok "the catcher received nothing for 6s at the pinned budget — the premise WON (S2 finding); see evidence"
fi

if daemon_alive; then
  ok "the daemon process survives the pin (the FD table is spent, not the process)"
else
  not_ok "the daemon died — see evidence"
fi

# --- release + recovery -------------------------------------------------------------

deadline=$((SECONDS + 180))
until vm_exec victim "sudo docker exec rt-attacker pkill -f '/tmp/grpc[i]dle'" >/dev/null 2>&1; do
  [ "$SECONDS" -ge "$deadline" ] && break
  sleep 3
done
recovers() {
  [ "$(scrape_code)" = 200 ] && [ "$(catcher_delta_6s)" -gt 0 ]
}
wait_for 120 "after release: scrape 200 and the catcher resumed" recovers
victim_still_reporting_tap

after_fds="$(daemon_fds)"
if [ "$((after_fds - BASE_FDS))" -le 32 ]; then
  ok "cleanup: FD count back at baseline ($BASE_FDS -> $after_fds)"
else
  not_ok "FD count did not return to baseline ($BASE_FDS -> $after_fds) — leak?"
fi

# --- verdict ------------------------------------------------------------------------

finish rt-11-2 BLOCKED
