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
#   half 1 — tcp (the catcher): the daemon's Write errors immediately on
#   the dead conn (FIN/RST — no retransmit stall), the counters light up,
#   polling continues, and the blind is PERMANENT: tcpDest never reconnects
#   (#339), so a catcher restart changes nothing — delivery stays dead
#   until the daemon itself restarts. CONFIRMED (the pinned finding).
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
B_ERR="$(mval 'error.*envelopeFlush')"

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
H1_ERR="$(mval 'error.*envelopeFlush')"
H1_WERR="$(mval 'destTCP.*error')"
H1_POLLS="$(mval 'pollRequestCh')"
echo "rt-4-7: catcher down: delta=$DARK_DELTA envelopeFlush/error=$B_ERR->$H1_ERR destTCP/Write/error=$H1_WERR polls=$B_POLLS->$H1_POLLS"

if [ "$DARK_DELTA" -le 10 ]; then
  ok "half 1: the catcher is dark while the sink is down"
else
  not_ok "half 1: the catcher kept flowing with the sink down — see evidence"
fi
if [ "${H1_ERR:-0}" -gt "${B_ERR:-0}" ] && [ "${H1_WERR:-0}" -gt 0 ]; then
  ok "half 1: the delivery errors light up immediately (envelopeFlush/error $B_ERR->$H1_ERR, destTCP/Write/error=$H1_WERR — the blind IS visible, unlike rt-11-5's silent window)"
else
  not_ok "half 1: no delivery-error counters with the sink down — see evidence"
fi
if [ "${H1_POLLS:-0}" -gt "${B_POLLS:-0}" ]; then
  ok "half 1: polling continues through the outage (polls $B_POLLS->$H1_POLLS — the fast error frees the poller; no rt-11-5-style freeze)"
else
  not_ok "half 1: polls stopped with the sink down — see evidence"
fi

echo "rt-4-7: half 1 — the catcher is BACK; does the daemon reconnect?"
vm_exec_root victim "systemctl start xtcp2-catcher"
sleep 12 # a few cycles with the catcher listening again

BACK_DELTA=0
{
  a="$(catcher_lines)"
  sleep 6
  b="$(catcher_lines)"
  BACK_DELTA=$((b - a))
}
H2_ERR="$(mval 'error.*envelopeFlush')"
echo "rt-4-7: catcher restored: delta=$BACK_DELTA envelopeFlush/error=$H2_ERR"
if [ "$BACK_DELTA" -le 10 ]; then
  ok "half 1: THE PERMANENT BLIND — delivery stays dead after the sink returns ($BACK_DELTA lines/6s; tcpDest dials once and never reconnects, #339)"
else
  not_ok "half 1: the catcher recovered on its own (no reconnect exists — reconcile this) — see evidence"
fi

# The blind is the conn, not the catcher: a daemon restart re-dials and
# delivery resumes — the recovery the daemon itself needs.
vm_exec victim "sudo docker restart rt-xtcp2" >/dev/null
wait_for 90 "the restarted daemon answers" vm_exec victim \
  "curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:9088/metrics | grep -qE '^(200|401|403)\$'"
sleep 3
if victim_still_reporting; then
  ok "half 1: a daemon restart re-dials and delivery resumes (the fix for #339 measured)"
else
  not_ok "half 1: delivery did not resume after the daemon restart — see evidence"
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
UPLOAD_ERR="$(mval 'destS3Parquet.*type="error",variable="upload"}')"
DROPPED="$(mval 's3_dropped_total')"
echo "rt-4-7: garage down: upload/error=$UPLOAD_ERR s3_dropped_total=$DROPPED RSS=$((RSS1_KB / 1024))->$((RSS2_KB / 1024))MiB"

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

# The queue-sink guard holds (bounded, counted, recovering) — the plan's
# expectation. The raw-TCP half is the permanent-blind finding (#339),
# asserted above: a receiver restart does not restore delivery; only a
# daemon restart does. File under the pinned issue; rt-11-5 measured the
# L2 variant.
finish rt-4-7 BLOCKED
