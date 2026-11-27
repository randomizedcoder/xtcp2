#!/usr/bin/env bash
# vagrant/scenarios/rt-4-7-sink-outage.sh — RT-4: broker/sink outage
# (docs/redteam-plan.md row rt-4-7).
#
# The chain: the sink (the plan's Redpanda; the harness's queue-shaped sink
# is the s3parquet→Garage fixture, and its raw TCP sink is the jsonl
# catcher) dies mid-run and comes back. The plan's expectation: "memory
# bounded while dest is down; recovers on broker return; no unbounded
# queue". The premortem audit split the verdict by destination:
#
#   half 1 — tcp (the catcher): the Write errors on the dead conn
#   (FIN/RST — fast, no retransmit stall), the destTCP Write/Dial counters
#   and xtcp_tcp_dropped_total light up, polling continues, and — post-#339
#   — the worker RECONNECTS: a catcher restart heals delivery with no
#   daemon restart. The pre-fix permanent blind is recorded in findings.md
#   (row rt-4-7, evidence 20261126T020602).
#
#   half 2 — s3 (Garage): the queue bounds the outage (depth 16 /
#   64 MiB, oldest-first eviction, s3_dropped_total), uploads retry with
#   full-jitter backoff, and minio dials per request so the daemon RECOVERS
#   when the endpoint returns. BLOCKED — the plan's expectation holds.
#
# The scenario runs half 1 on the default deploy, then re-provisions with
# XTCP2_DEST=s3 for half 2. finish() reports BLOCKED (the queue-sink guard
# the row pins); half 1's permanent blind is asserted as the #339 evidence.
set -euo pipefail

: "${XTCP2_MODE:=bridge}"
: "${XTCP2_AUTH:=raw}"
: "${XTCP2_ATTACKER:=plain}"
: "${XTCP2_PPROF:=0}"
XTCP2_DEST="${XTCP2_DEST:-tcp}"
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

echo "rt-4-7: evidence in $RT_EVIDENCE"

mval() { # mval <series substring> — one xtcp_counts series' value
  vm_exec victim "curl -s -m 6 -H 'Authorization: Bearer $RT_READ_TOKEN' http://127.0.0.1:9088/metrics | grep '$1' | grep -v '^#' | grep -oE '[0-9.e+]+\$' | head -1" || echo 0
}

tcp_write_err() { mval 'function="destTCP",type="error",variable="Write"'; }
tcp_dial_err() { mval 'function="destTCP",type="error",variable="Dial"'; }
tcp_dropped() { mval 'xtcp_tcp_dropped_total{'; }
tcp_reconnects() { mval 'function="destTCP",type="count",variable="Reconnect"'; }

daemon_rss_kb() {
  vm_exec victim "sudo docker stats --no-stream --format '{{.MemUsage}}' rt-xtcp2" \
    | grep -oE '^[0-9.]+' | awk '{printf "%d", $1 * 1024}'
}

# =============================================================================
# half 1 — tcp: the catcher dies and comes back; the daemon does not
# =============================================================================

xtcp2_deploy victim attacker
wait_for 120 "daemon answers on the victim VM" vm_exec victim \
  "curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:9088/metrics | grep -qE '^(200|401|403)\$'"
victim_still_reporting_tap
xtcp2_victim_read_token

B_POLLS="$(mval 'pollRequestCh')"

echo "rt-4-7: half 1 — killing the catcher"
vm_exec_root victim "systemctl stop xtcp2-catcher"
sleep 8 # a few cycles against the dead conn (FIN/RST, fast errors)

DARK_DELTA=0
{
  a="$(catcher_lines)"
  sleep 6
  b="$(catcher_lines)"
  DARK_DELTA=$((b - a))
}
H1_WERR="$(tcp_write_err)"
H1_DERR="$(tcp_dial_err)"
H1_DROP="$(tcp_dropped)"
H1_POLLS="$(mval 'pollRequestCh')"
echo "rt-4-7: catcher down: delta=$DARK_DELTA Write/error=$H1_WERR Dial/error=$H1_DERR dropped=$H1_DROP polls=$B_POLLS->$H1_POLLS"

if [ "$DARK_DELTA" -le 10 ]; then
  ok "half 1: the catcher is dark while the sink is down"
else
  not_ok "half 1: the catcher kept flowing with the sink down — see evidence"
fi
if [ "${H1_WERR:-0}" -ge 1 ] && [ "${H1_DERR:-0}" -ge 1 ] && [ "${H1_DROP:-0}" -ge 1 ]; then
  ok "half 1: the outage is VISIBLE and COUNTED (Write/error=$H1_WERR, Dial/error=$H1_DERR, xtcp_tcp_dropped_total=$H1_DROP — the worker's errors, not the poller's)"
else
  not_ok "half 1: the outage counters did not light up (Write=$H1_WERR Dial=$H1_DERR drops=$H1_DROP) — see evidence"
fi
if [ "${H1_POLLS:-0}" -gt "${B_POLLS:-0}" ]; then
  ok "half 1: polling continues through the outage (polls $B_POLLS->$H1_POLLS — the fast error frees the poller; no rt-11-5-style freeze)"
else
  not_ok "half 1: polls stopped with the sink down — see evidence"
fi

echo "rt-4-7: half 1 — the catcher is BACK; the worker reconnects (#339 fixed)"
vm_exec_root victim "systemctl start xtcp2-catcher"

resume() {
  a="$(catcher_lines)"
  sleep 6
  b="$(catcher_lines)"
  [ $((b - a)) -gt 10 ]
}
wait_for 90 "delivery resumes after the catcher returns (the worker re-dials; NO daemon restart)" resume
BACK_DELTA="$(a="$(catcher_lines)"; sleep 6; b="$(catcher_lines)"; echo $((b - a)))"
H1_RECONN="$(tcp_reconnects)"
echo "rt-4-7: catcher restored: delta=$BACK_DELTA reconnects=$H1_RECONN"
if [ "$BACK_DELTA" -gt 10 ]; then
  ok "half 1: RECONNECT — delivery resumed after the sink returned with NO daemon restart ($BACK_DELTA lines/6s, reconnects=$H1_RECONN — the #339 fix measured)"
else
  not_ok "half 1: delivery stayed dead after the sink returned — see evidence"
fi

# =============================================================================
# half 2 — s3: the queue bounds the outage and recovers on return
# =============================================================================

export XTCP2_DEST=s3
unset XTCP2_SINK_IP
xtcp2_deploy victim attacker
wait_for 120 "daemon answers on the victim VM (s3 row)" vm_exec victim \
  "curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:9088/metrics | grep -qE '^(200|401|403)\$'"
xtcp2_victim_read_token

garage_objects() {
  vm_exec victim 'sudo docker exec rt-garage /garage -c /etc/garage/garage.toml bucket info xtcp2-records 2>/dev/null' \
    | grep -E '^Objects:' | grep -oE '[0-9]+'
}
garage_blocks_ge_1() { [ "$(garage_objects)" -ge 1 ]; }
wait_for 120 "garage holds at least one object" garage_blocks_ge_1

G0="$(garage_objects)"

echo "rt-4-7: half 2 — killing Garage (the s3 endpoint)"
RSS1_KB="$(daemon_rss_kb)"
vm_exec victim "sudo docker stop rt-garage" >/dev/null
sleep 45 # uploads retry with backoff, the queue evicts oldest-first

RSS2_KB="$(daemon_rss_kb)"
DROPPED="$(mval 's3_dropped_total')"
echo "rt-4-7: garage down: s3_dropped_total=$DROPPED RSS=$((RSS1_KB / 1024))->$((RSS2_KB / 1024))MiB"

upload_error_ge_1() { [ "$(mval 'destS3Parquet.*type="error",variable="upload"}')" -ge 1 ]; }
wait_for 150 "the upload retries exhaust and the error counter lights" upload_error_ge_1
UPLOAD_ERR="$(mval 'destS3Parquet.*type="error",variable="upload"}')"
DROPPED="$(mval 's3_dropped_total')"

SLOPE_KB=$(( (RSS2_KB - RSS1_KB) / 45 ))
if [ "$SLOPE_KB" -le 2048 ]; then
  ok "half 2: daemon memory bounded while the sink is down (${SLOPE_KB}KiB/s slope; queue depth 16 / 64MiB, oldest-first eviction)"
else
  not_ok "half 2: RSS climbing at ${SLOPE_KB}KiB/s with the sink down — see evidence"
fi
if [ "${UPLOAD_ERR:-0}" -gt 0 ] && [ "${DROPPED:-0}" -gt 0 ]; then
  ok "half 2: the outage is VISIBLE and COUNTED (destS3Parquet/upload/error=$UPLOAD_ERR, s3_dropped_total=$DROPPED — bounded loss, not silent)"
else
  not_ok "half 2: upload errors=$UPLOAD_ERR evictions=$DROPPED with the sink down — see evidence"
fi

echo "rt-4-7: half 2 — Garage is BACK; the daemon recovers"
vm_exec victim "sudo docker start rt-garage" >/dev/null
recovered() {
  [ "$(garage_objects)" -gt "${G0:-0}" ]
}
wait_for 120 "objects resume after the endpoint returns (minio dials per request)" recovered
G1="$(garage_objects)"
if [ "$G1" -gt "${G0:-0}" ]; then
  ok "half 2: RECOVERY — objects resumed after the endpoint returned ($G0 -> $G1); the plan's expectation holds"
else
  not_ok "half 2: no recovery after the endpoint returned — see evidence"
fi

if vm_exec victim "sudo docker inspect rt-xtcp2 --format '{{.State.Running}}' | grep -q true"; then
  ok "half 2: the daemon survived the whole outage (no OOM, no crash)"
else
  not_ok "half 2: the daemon died — see evidence"
fi

# --- verdict -------------------------------------------------------------------------

# Both halves now hold the plan's expectation ("memory bounded while dest
# is down; recovers on broker return; no unbounded queue"): the s3 queue
# bounds and recovers, and — post-#339 — the tcp worker reconnects when the
# catcher returns, with the outage counted in destTCP errors +
# xtcp_tcp_dropped_total the whole time. The pre-fix permanent blind is the
# findings.md record (evidence 20261126T020602).
finish rt-4-7 BLOCKED
