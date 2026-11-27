#!/usr/bin/env bash
# vagrant/scenarios/rt-11-5-l2-delivery-kill.sh — RT-11: L2 kill of the
# delivery path (docs/redteam-plan.md row rt-11-5).
#
# The chain: a privileged co-tenant (CAP_NET_RAW) sibling container on the
# same bridge as the daemon sends gratuitous ARP replies claiming the SINK
# endpoint's IP resolves to a poison MAC nobody owns. The daemon's
# established TCP to the sink blackholes: no RST, no FIN — the kernel just
# retransmits.
#
# PRE-FIX (evidence 20261126T014816, findings.md row rt-11-5, #345/#339):
# the delivery Write ran ON the poll goroutine with no deadline, so once
# SO_SNDBUF filled the whole POLLER froze for the TCP retransmit window
# (~15.4 min at the prod default retries2=15) with zero error counters and
# scrape 200 — collection, not just delivery, silently dead — and the
# window's close left delivery permanently blind (no reconnect).
#
# POST-FIX (this run, EXPECT=MITIGATED, XTCP2_TCP_WRITE_TIMEOUT=5s): the
# tcp destination queues to a bounded worker (Send never blocks the
# poller), every write and dial carries the deadline, and a failed write
# reconnects under full-jitter backoff. The same poison now costs:
#   - polls CONTINUE through the stall (the freeze is gone — the core fix),
#   - delivery stalls but the blind is VISIBLE and COUNTED (destTCP
#     Write/Dial errors, xtcp_tcp_dropped_total) instead of silent,
#   - de-poison heals delivery with NO daemon restart (the reconnect,
#     #339's fix) — measured twice, two poison cycles.
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
: "${XTCP2_TCP_WRITE_TIMEOUT:=5s}"
XTCP2_EXPECT="${XTCP2_EXPECT:-MITIGATED}"

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
tcp_write_err() { mval 'function="destTCP",type="error",variable="Write"'; }
tcp_dial_err() { mval 'function="destTCP",type="error",variable="Dial"'; }
tcp_dropped() { mval 'xtcp_tcp_dropped_total{'; }
tcp_reconnects() { mval 'function="destTCP",type="count",variable="Reconnect"'; }

error_counters() {
  vm_exec victim "curl -s -m 6 -H 'Authorization: Bearer $RT_READ_TOKEN' http://127.0.0.1:9088/metrics | grep -E 'error.*envelopeFlush|destTCP.*error' | grep -v '^#'" || true
}

# --- baseline --------------------------------------------------------------------

B_DELTA="$(catcher_delta_6s)"
B_POLLS="$(polls_issued)"
ok "baseline: catcher=$B_DELTA lines/6s polls=$B_POLLS scrape=$(scrape_code)"

# --- the killer -------------------------------------------------------------------

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

# --- cycle 1: the poison hold --------------------------------------------------------
# With the 5s write deadline the stall unwinds in bounded steps: the in-flight
# write errors at the deadline, the reconnect dials time out at the same bound,
# and each envelope burns its attempt budget and is dropped. What must NOT
# happen (the pre-fix freeze): the poller stopping.

P0="$(polls_issued)"
sleep 20 # the deadline fires (~5s), the first dial windows close (~5s each)

DELTA="$(catcher_delta_6s)"
if [ "$DELTA" -le 10 ]; then
  ok "cycle 1: the catcher is dark within a cycle of the poison ($DELTA lines/6s) — delivery stalls (the premise's blind)"
else
  not_ok "cycle 1: the catcher kept flowing ($DELTA lines/6s) — the poison failed; see evidence"
fi
code="$(scrape_code)"
if [ "${code:-000}" = 200 ]; then
  ok "cycle 1: the daemon keeps answering (scrape 200)"
else
  not_ok "cycle 1: the scrape degraded (code=${code:-000}) — see evidence"
fi

sleep 12 # more envelope budgets burn; polls sampled across the whole window
P1="$(polls_issued)"
if [ "$P1" -gt "$P0" ]; then
  ok "cycle 1: THE FREEZE IS GONE — polls kept issuing through the stall (polls $P0->$P1 over 32s; the flush no longer runs on the poll goroutine — the #345 fix)"
else
  not_ok "cycle 1: the poller froze during the stall ($P0->$P1) — the fix regressed; see evidence"
fi

W1_ERR="$(tcp_write_err)"
D1_ERR="$(tcp_dial_err)"
DROP1="$(tcp_dropped)"
echo "rt-11-5: cycle 1: destTCP/Write/error=$W1_ERR destTCP/Dial/error=$D1_ERR tcp_dropped_total=$DROP1"
if [ "${W1_ERR:-0}" -ge 1 ] && [ "${D1_ERR:-0}" -ge 1 ]; then
  ok "cycle 1: the blind is VISIBLE while dark (Write/error=$W1_ERR, Dial/error=$D1_ERR — the deadline turns the silent window into counted errors)"
else
  not_ok "cycle 1: no Write/Dial errors during the stall — the blind is still silent; see evidence"
fi
if [ "${DROP1:-0}" -ge 1 ]; then
  ok "cycle 1: the stall is COUNTED (xtcp_tcp_dropped_total=$DROP1 — bounded, visible loss, not silent latency)"
else
  not_ok "cycle 1: no drops counted during the stall — see evidence"
fi

# --- cycle 1 release: the reconnect heals delivery -------------------------------------

vm_exec victim "sudo docker exec rt-l2killer pkill -f arppoison" >/dev/null 2>&1 || true
vm_exec_root victim "nsenter -t $DPID -n ip neigh del $SINK dev eth0" >/dev/null 2>&1 || true

resume() {
  [ "$(catcher_delta_6s)" -gt 10 ]
}
wait_for 90 "delivery resumes after de-poison (the next dial lands; NO daemon restart — the #339 fix)" resume
CATCHUP="$(catcher_delta_6s)"
R1="$(tcp_reconnects)"
if [ "$CATCHUP" -gt 10 ]; then
  ok "cycle 1: de-poison heals delivery with NO restart (catcher $CATCHUP lines/6s; reconnects=$R1 — the daemon re-dials on its own)"
else
  not_ok "cycle 1: the catcher did not resume after de-poison — see evidence"
fi

# --- cycle 2: the same poison again — the reconnect is not one-shot --------------------

vm_exec victim "sudo docker exec rt-l2killer sh -c 'nohup /tmp/arppoison -iface eth0 -ip $SINK -interval 200ms > /tmp/ap2.log 2>&1 &'" >/dev/null

# The cycle-1 de-poison re-dialed, so the sink's neighbor entry is REACHABLE
# again — and a REACHABLE entry ignores gratuitous replies until it ages to
# STALE (~30s at base_reachable_time; cycle 1's entry was already stale).
# Wait for the flip rather than guessing the age.
neigh_flipped() {
  vm_exec_root victim "nsenter -t $DPID -n ip neigh show $SINK" | grep -q "02:00:00:00:00:ff"
}
wait_for 90 "the second poison flips the freshly re-dialed neighbor entry" neigh_flipped
W2A="$(tcp_write_err)"
sleep 18
DARK2="$(catcher_delta_6s)"
W2B="$(tcp_write_err)"
P2A="$(polls_issued)"
sleep 8
P2B="$(polls_issued)"
echo "rt-11-5: cycle 2: catcher=$DARK2 lines/6s Write/error=$W2A->$W2B polls=$P2A->$P2B"
if [ "$DARK2" -le 10 ] && [ "${W2B:-0}" -gt "${W2A:-0}" ]; then
  ok "cycle 2: the same poison stalls delivery again and errors again (Write/error $W2A->$W2B, catcher $DARK2 lines/6s)"
else
  not_ok "cycle 2: the second stall did not behave like the first — see evidence"
fi
if [ "$P2B" -gt "$P2A" ]; then
  ok "cycle 2: polls continue through the second stall too ($P2A->$P2B)"
else
  not_ok "cycle 2: the poller froze on the second stall — see evidence"
fi

vm_exec victim "sudo docker exec rt-l2killer pkill -f arppoison" >/dev/null 2>&1 || true
vm_exec_root victim "nsenter -t $DPID -n ip neigh del $SINK dev eth0" >/dev/null 2>&1 || true
wait_for 90 "delivery resumes after the second de-poison" resume
CATCHUP2="$(catcher_delta_6s)"
if [ "$CATCHUP2" -gt 10 ]; then
  ok "cycle 2: de-poison heals delivery again ($CATCHUP2 lines/6s — the reconnect is repeatable, not a one-shot)"
else
  not_ok "cycle 2: the catcher did not resume after the second de-poison — see evidence"
fi

victim_alive_check() {
  vm_exec victim "sudo docker inspect rt-xtcp2 --format '{{.State.Running}}' | grep -q true"
}
if victim_alive_check; then
  ok "the daemon survives the whole attack (both cycles)"
else
  not_ok "the daemon died — see evidence"
fi

# --- teardown ------------------------------------------------------------------------

vm_exec victim "sudo docker rm -f rt-l2killer" >/dev/null

# --- verdict -------------------------------------------------------------------------

# MITIGATED (#345 + #339 landed with this expectation): the L2 kill still
# stalls delivery (that much is physics — the route is blackholed), but the
# poller keeps polling through the stall, the blind is visible and counted
# (Write/Dial errors, tcp_dropped_total), and de-poison heals delivery with
# no daemon restart. The pre-fix freeze and permanent blind are recorded in
# findings.md row rt-11-5 (evidence 20261126T014816).
finish rt-11-5 MITIGATED
