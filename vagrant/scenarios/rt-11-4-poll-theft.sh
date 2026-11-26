#!/usr/bin/env bash
# vagrant/scenarios/rt-11-4-poll-theft.sh — RT-11: own-netns dump
# amplification / poll-budget theft (docs/redteam-plan.md row rt-11-4).
#
# The chain: every cycle dumps EVERY namespace. The attacker packs ITS OWN
# netns with hundreds of thousands of sockets (sockfill -pairs: loopback
# pairs, 2 sockets each, no external dialing) so its dump alone should eat
# seconds of the cycle and slide the host's and victims' dumps late.
#
# THE MEASURED DESIGN ANSWER (audited, then measured here): the dumps run
# PARALLEL per namespace (one netlink socket + netlinkers each), and
# -timeout (1s in this deploy) is the wall-clock valve: when a dump
# outlasts it, poll_timeout fires, the partial envelope flushes, and the
# next tick starts a fresh poll — the cycle CANNOT overrun. The thief's
# dump keeps draining in the background into the next envelope; only the
# thief's own records straggle. The victim namespaces' records decode in
# parallel and ride the poll_timeout flush at cadence.
#
# Phase A (the default budget, 1M records/poll): the thief at ~300K sockets
# sits UNDER the gate (max-min fair share never engages). Expected: the
# victim coverage lag stays at cadence, PollTimeout fires every cycle, the
# decode fleet absorbs the load, RSS bounded.
#
# Phase B (the walkable budget, XTCP2_MAX_RECORDS=50000): the gate engages —
# the thief's namespace is degraded (TIME_WAIT/SYN_RECV dropped from the
# ask, then port-shard sampling: xtcp_poll_degraded_total), so even a
# socket bomb cannot eat the budget. Expected: the thief's own record count
# collapses while the victim namespaces keep full coverage.
set -euo pipefail

: "${XTCP2_MODE:=bridge}"
: "${XTCP2_AUTH:=raw}"
: "${XTCP2_ATTACKER:=plain}"
: "${XTCP2_PPROF:=0}"
XTCP2_MAX_RECORDS="${XTCP2_MAX_RECORDS:-0}"
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

echo "rt-11-4: evidence in $RT_EVIDENCE"

mval() { # mval <series substring> — one xtcp_counts series' value
  vm_exec victim "curl -s -m 6 -H 'Authorization: Bearer $RT_READ_TOKEN' http://127.0.0.1:9088/metrics | grep '$1' | grep -v '^#' | grep -oE '[0-9.e+]+\$' | head -1" || echo 0
}

qval() { # qval <series substring> <quantile> — a summary quantile's value
  vm_exec victim "curl -s -m 6 -H 'Authorization: Bearer $RT_READ_TOKEN' http://127.0.0.1:9088/metrics | grep '$1' | grep 'quantile=\"$2\"' | grep -v '^#' | grep -oE '[0-9.e+]+\$' | head -1" || echo 0
}

daemon_rss_kb() {
  vm_exec victim "sudo docker stats --no-stream --format '{{.MemUsage}}' rt-xtcp2" \
    | grep -oE '^[0-9.]+' | awk '{printf "%d", $1 * 1024}'
}

# victim_lag_s: the WORST age of the two victim namespaces' newest records at
# the catcher (rt-work-1/rt-work-2 are the "victims" whose coverage must not
# slide). One line: multi-line payloads do not survive vm_exec_root's %q
# layer (the mint() lesson); the tail keeps the scan bounded.
victim_lag_s() { # victim_lag_s <id12> <id12>
  vm_exec_root victim "tail -n 50000 /var/lib/xtcp2-catcher/records.jsonl | python3 -c \"import json,time,sys; ids={'$1','$2'}; last={}; [last.__setitem__(r['netns'], max(last.get(r['netns'],0), int(r.get('timestampNs','0')))) for r in (json.loads(l) for l in sys.stdin if l.strip()) if r.get('netns') in ids]; now=time.time_ns(); print(max([0]+[(now-t)//10**9 for t in last.values()]))\"" || echo 999
}

# victim_ids: the 12-char docker IDs the daemon uses as the work netns names.
victim_ids() {
  vm_exec victim "sudo docker inspect rt-work-1 rt-work-2 --format '{{.Id}}' | cut -c1-12"
}

# =============================================================================
# phase A — the thief at ~300K sockets, under the default 1M budget
# =============================================================================

xtcp2_deploy victim attacker
wait_for 120 "daemon answers on the victim VM" vm_exec victim \
  "curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:9088/metrics | grep -qE '^(200|401|403)\$'"
victim_still_reporting_tap
xtcp2_victim_read_token

# The victim ids: the daemon names each netns by the 12-char docker id.
ids="$(victim_ids)"
W1="$(echo "$ids" | head -1)"
W2="$(echo "$ids" | tail -1)"
echo "rt-11-4: victim netns ids: $W1 $W2"

B_LAG="$(victim_lag_s "$W1" "$W2")"
B_Q99="$(qval 'pollToDoneDuration' '0.99')"
B_POLLS="$(mval 'pollRequestCh')"
ok "baseline: victim coverage lag=${B_LAG}s pollToDone p99=${B_Q99}s"

PAIRS=150000 # 300K sockets in the attacker's own netns
echo "rt-11-4: filling the attacker netns with $PAIRS loopback pairs"
vm_exec victim "sudo docker cp /opt/xtcp2-redteam/bin/sockfill rt-attacker:/tmp/sockfill" >/dev/null
vm_exec victim "sudo docker exec rt-attacker sh -c 'nohup /tmp/sockfill -pairs $PAIRS -perPort 20000 > /tmp/sf.log 2>&1 &'" >/dev/null
wait_for 900 "sockfill holds the pairs open" vm_exec victim \
  "sudo docker exec rt-attacker grep -q SOCKFILL_READY /tmp/sf.log"
FILLED="$(vm_exec victim "sudo docker exec rt-attacker sh -c 'cat /proc/net/sockstat | grep TCP:' || true")"
echo "rt-11-4: attacker netns sockstat: $FILLED"

sleep 20 # a few cycles under the load

A_LAG="$(victim_lag_s "$W1" "$W2")"
A_TIMEOUTS="$(mval 'PollTimeout')"
A_Q99="$(qval 'pollToDoneDuration' '0.99')"
A_POLLS="$(mval 'pollRequestCh')"
A_RATE=0
{
  a="$(catcher_lines)"
  sleep 6
  b="$(catcher_lines)"
  A_RATE=$((b - a))
}
echo "rt-11-4: while held: victim_lag=${A_LAG}s PollTimeout=$A_TIMEOUTS p99=${A_Q99}s polls=$B_POLLS->$A_POLLS catcher=$A_RATE lines/6s"

if [ "$A_LAG" -le 15 ]; then
  ok "phase A: NO COVERAGE LAG — the victims' records keep arriving at cadence (lag ${A_LAG}s; the dumps are parallel per netns and the 1s poll timeout bounds the cycle — the thief only delays its own records)"
else
  not_ok "phase A: the victim coverage lagged (${A_LAG}s) — the premise WON; see evidence"
fi
if [ "${A_TIMEOUTS:-0}" -gt 0 ]; then
  ok "phase A: the timeout valve fires every cycle (PollTimeout=$A_TIMEOUTS — the thief's dump outlasts 1s, the cycle still advances)"
else
  not_ok "phase A: PollTimeout never fired — the thief did not even outlast the timeout; see evidence"
fi
if [ "${A_POLLS:-0}" -gt "${B_POLLS:-0}" ]; then
  ok "phase A: polls keep issuing through the load (the cadence held)"
else
  not_ok "phase A: polls stopped issuing — see evidence"
fi

RSS1_KB="$(daemon_rss_kb)"
sleep 30
RSS2_KB="$(daemon_rss_kb)"
SLOPE_KB=$(( (RSS2_KB - RSS1_KB) / 30 ))
echo "rt-11-4: RSS slope ${SLOPE_KB}KiB/s ($((RSS1_KB / 1024))->$((RSS2_KB / 1024))MiB over 30s)"
if [ "$SLOPE_KB" -le 4096 ]; then
  ok "phase A: memory bounded under the bomb (${SLOPE_KB}KiB/s slope; envelope row/byte caps)"
else
  not_ok "phase A: RSS climbing at ${SLOPE_KB}KiB/s — see evidence"
fi
if vm_exec victim "sudo docker inspect rt-xtcp2 --format '{{.State.Running}}' | grep -q true"; then
  ok "phase A: the daemon survives the socket bomb"
else
  not_ok "phase A: the daemon died — see evidence"
fi

# =============================================================================
# phase B — the walkable budget: the gate degrades the thief's namespace
# =============================================================================

vm_exec victim "sudo docker exec rt-attacker pkill -f sockfill" >/dev/null 2>&1 || true
vm_exec victim "sudo docker rm -f rt-attacker" >/dev/null # drop the bomb netns

export XTCP2_MAX_RECORDS=50000
xtcp2_deploy victim attacker
wait_for 120 "daemon answers on the victim VM (budget row)" vm_exec victim \
  "curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:9088/metrics | grep -qE '^(200|401|403)\$'"
xtcp2_victim_read_token

echo "rt-11-4: re-filling the attacker netns at the walkable budget"
vm_exec victim "sudo docker cp /opt/xtcp2-redteam/bin/sockfill rt-attacker:/tmp/sockfill" >/dev/null
vm_exec victim "sudo docker exec rt-attacker sh -c 'nohup /tmp/sockfill -pairs $PAIRS -perPort 20000 > /tmp/sf2.log 2>&1 &'" >/dev/null
wait_for 900 "sockfill holds the pairs open (budget row)" vm_exec victim \
  "sudo docker exec rt-attacker grep -q SOCKFILL_READY /tmp/sf2.log"
sleep 20

DEGRADED_STATES="$(vm_exec victim "curl -s -m 6 -H 'Authorization: Bearer $RT_READ_TOKEN' http://127.0.0.1:9088/metrics | grep 'poll_degraded_total' | grep -v '^#' | head -4" || true)"
DEGRADED_SUM="$(echo "$DEGRADED_STATES" | grep -oE '[0-9.e+]+$' | awk '{s+=$1} END {printf "%d", s+0}')"
B_LAG2="$(victim_lag_s "$W1" "$W2")"
B_RATE=0
{
  a="$(catcher_lines)"
  sleep 6
  b="$(catcher_lines)"
  B_RATE=$((b - a))
}
echo "rt-11-4: budget row: degraded=[$DEGRADED_STATES] victim_lag=${B_LAG2}s catcher=$B_RATE lines/6s"

if [ "${DEGRADED_SUM:-0}" -gt 0 ]; then
  ok "phase B: the gate engages — the thief's namespace is degraded (xtcp_poll_degraded_total sum=$DEGRADED_SUM: $DEGRADED_STATES)"
else
  not_ok "phase B: no degradation with the 50K budget and ~300K sockets — see evidence"
fi
if [ "$B_LAG2" -le 15 ]; then
  ok "phase B: the victims keep full coverage under the gate (lag ${B_LAG2}s — the fair share spends the budget on the victims, not the thief)"
else
  not_ok "phase B: the victim coverage lagged under the gate (${B_LAG2}s) — see evidence"
fi
victim_still_reporting_tap

# --- teardown -----------------------------------------------------------------------

vm_exec victim "sudo docker exec rt-attacker pkill -f sockfill" >/dev/null 2>&1 || true

# --- verdict ------------------------------------------------------------------------

# The timeout valve and the per-netns parallel dumps cap the thief at its
# own namespace's records; the record-budget gate (maxRecordsPerPoll)
# degrades the thief's dump when over its fair share. The victim coverage
# never slides — the premise loses. The socket bomb's remaining cost is the
# telemetry amplifier (#342): its own records are reported at volume.
finish rt-11-4 BLOCKED
