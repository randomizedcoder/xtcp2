#!/usr/bin/env bash
# vagrant/scenarios/rt-4-4-slow-consumer.sh — RT-4: slow FlatRecords
# consumer (docs/redteam-plan.md row rt-4-4).
#
# The chain: an authenticated (read-cred) consumer opens FlatRecords streams
# and reads them at 1 message / 30 s, never canceling. gRPC flow control
# backpressures the daemon pipeline — the plan's expectation is "daemon
# memory bounded under stalled consumers, no OOM, record the envelope
# buffer high-water mark".
#
# The audited shape makes the backpressure the finding: flatRecordServiceSend
# does a BLOCKING per-client Send on the record hot path (per record, per
# client stream; pkg/xtcp/deserialize.go:274). A 1/30 s reader holds each
# Send up to 30 s, so the decode fleet advances at the SLOWEST reader's
# pace — record delivery to the sink (the envelope flush only completes a
# cycle) degrades to the trickle, exactly like the rt-11-3 wedge only
# paced. Memory stays bounded (envelope row/byte caps + the record pool)
# — that is the OOM axis this row pins BLOCKED.
#
# Matrix default: 4 paced streams for 4 minutes. RT_SOAK=1h stretches the
# hold to the plan's 1 h soak (env-gated; not in the matrix).
set -euo pipefail

: "${XTCP2_MODE:=bridge}"
: "${XTCP2_AUTH:=raw}"
: "${XTCP2_ATTACKER:=plain}"
: "${XTCP2_PPROF:=1}"    # the goroutine-count asserts need the debug listener
RT_SOAK="${RT_SOAK:-}"   # e.g. 1h — the plan's soak variant
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

echo "rt-4-4: evidence in $RT_EVIDENCE"

xtcp2_deploy victim attacker
wait_for 120 "daemon answers on the victim VM" vm_exec victim \
  "curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:9088/metrics | grep -qE '^(200|401|403)\$'"
victim_still_reporting_tap
xtcp2_victim_read_token

# --- helpers -------------------------------------------------------------------

GS=/opt/xtcp2-redteam/bin/grpcstorm
GRPC=192.168.142.10:8889
CONNS=2
STREAMS=2
RATE=0.0333 # 1 record / 30 s per stream
HOLD="${RT_SOAK:-240s}"

pprof_goroutines() {
  vm_exec victim "curl -s -m 6 -H 'Authorization: Bearer $RT_READ_TOKEN' 'http://127.0.0.1:9090/debug/pprof/goroutine?debug=1' | head -1" \
    | grep -oE '[0-9]+$' || echo 0
}

daemon_rss_kb() {
  vm_exec victim "sudo docker stats --no-stream --format '{{.MemUsage}}' rt-xtcp2" \
    | grep -oE '^[0-9.]+' | awk '{printf "%d", $1 * 1024}'
}

daemon_alive() {
  vm_exec victim "sudo docker inspect rt-xtcp2 --format '{{.State.Running}}' | grep -q true"
}

rate6() {
  local a b
  a="$(catcher_lines)"
  sleep 6
  b="$(catcher_lines)"
  echo $((b - a))
}

# --- baseline --------------------------------------------------------------------

BASE_RATE="$(rate6)"
BASE_RSS_KB="$(daemon_rss_kb)"
BASE_G="$(pprof_goroutines)"
ok "baseline: catcher=${BASE_RATE} lines/6s RSS=$((BASE_RSS_KB / 1024))MiB goroutines=$BASE_G"

# --- the slow consumers -----------------------------------------------------------

vm_exec attacker "nohup $GS -target $GRPC -token '$RT_READ_TOKEN' -conns $CONNS -streams $STREAMS -read-rate $RATE -duration $HOLD > /tmp/gs4.log 2>&1 &" >/dev/null
wait_for 60 "the slow streams are up" vm_exec attacker "grep -q GRPCSTORM_READY /tmp/gs4.log"
sleep 12 # the first Sends land and the decoder's pace collapses to the readers'

# --- while held: the OOM axis (bounded) and the backpressure finding ---------------

# RSS: measure the SECOND-window slope, not a cumulative jump (a legitimate
# run takes one GC step then flattens — the rt-3-4 lesson).
RSS1_KB="$(daemon_rss_kb)"
sleep 30
RSS2_KB="$(daemon_rss_kb)"
SLOPE_KB=$(( (RSS2_KB - RSS1_KB) / 30 ))
echo "rt-4-4: RSS slope ${SLOPE_KB}KiB/s (RSS $((RSS1_KB / 1024)) -> $((RSS2_KB / 1024))MiB over 30s)"
if [ "$SLOPE_KB" -le 2048 ]; then
  ok "daemon memory bounded under the stalled consumers (${SLOPE_KB}KiB/s slope over 30s; the envelope row/byte caps + record pool hold)"
else
  not_ok "daemon RSS climbing at ${SLOPE_KB}KiB/s under the stalled consumers — see evidence"
fi

if daemon_alive; then
  ok "the daemon survives the slow-consumer hold (no OOM)"
else
  not_ok "the daemon died under the slow consumers — see evidence"
fi

# The backpressure finding: the catcher collapses to the readers' pace.
HELD_RATE="$(rate6)"
echo "rt-4-4: catcher $BASE_RATE -> $HELD_RATE lines/6s while held"
if [ "$HELD_RATE" -le $((BASE_RATE * 25 / 100)) ]; then
  ok "FINDING: sink delivery backpressured to the slowest reader ($BASE_RATE -> $HELD_RATE lines/6s) — the blocking per-client Send on the record hot path couples the whole pipeline to one consumer's pace (the rt-11-3 wedge, paced)"
else
  not_ok "the catcher kept flowing ($BASE_RATE -> $HELD_RATE lines/6s) — no backpressure coupling; see evidence"
fi

# --- release: recovery + no leak ----------------------------------------------------

deadline=$((SECONDS + 400))
until vm_exec attacker "grep -q GRPCSTORM_DONE /tmp/gs4.log" >/dev/null 2>&1; do
  [ "$SECONDS" -ge "$deadline" ] && break
  sleep 5
done
total="$(vm_exec attacker "sed -n 's/GRPCSTORM_DONE total_records=//p' /tmp/gs4.log" || true)"
total="${total:-0}"
echo "rt-4-4: slow streams read $total records"
if [ "$total" -gt 0 ]; then
  ok "the slow streams did read ($total records — a trickle, as paced)"
else
  not_ok "the slow streams read nothing ($total) — see evidence"
fi

if victim_still_reporting; then
  ok "release: the catcher resumes once the readers close"
else
  not_ok "release: the catcher did not resume — see evidence"
fi

sleep 5
AFTER_G="$(pprof_goroutines)"
if [ "$AFTER_G" -le $((BASE_G + 4)) ]; then
  ok "no goroutine leak after release ($BASE_G -> $AFTER_G)"
else
  not_ok "goroutines leaked after release ($BASE_G -> $AFTER_G) — see evidence"
fi
victim_still_reporting_tap

# --- verdict -------------------------------------------------------------------------

# The plan's OOM expectation holds (memory bounded, daemon alive). The
# degradation is the pinned product finding: one slow reader couples the
# whole pipeline — same root cause as rt-11-3 (flatRecordServiceSend's
# blocking fan-out), expected=BLOCKED pins the memory axis only.
finish rt-4-4 BLOCKED
