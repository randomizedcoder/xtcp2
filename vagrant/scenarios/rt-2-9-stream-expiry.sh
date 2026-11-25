#!/usr/bin/env bash
# vagrant/scenarios/rt-2-9-stream-expiry.sh — RT-2: stream-expiry carry-over
# (the rt-2-2 Phase-4 deferral, docs/redteam-plan.md row rt-2-9).
#
# The chain: HMAC-signed listener auth is windowed (the skew-0 exact-minute
# window measured in rt-2-2). An RPC is authorized by ONE interceptor call
# at stream open — a stream opened with a fresh signature and then held
# open past the window never re-authenticates: gRPC has no per-message
# listenerauth re-validation, and the FlatRecords handler blocks in select
# until the client disconnects (pkg/xtcp/grpc_flatRecordService.go:124).
#
# THE MEASURED SHAPE (probe, 2026-11-25): a stream opened just after a
# minute boundary, reading at 20 records/s for 110 s — 2180 of an expected
# 2200 records arrived: the stream kept delivering ~50 s PAST its auth
# window. File-don't-fix: per-stream authz at establishment is standard
# gRPC semantics (the same shape as every HTTP bearer connection), and the
# signed-token window's purpose — replay resistance for NEW RPCs — is
# intact (a fresh RPC with the stale signature is refused; rt-2-2).
#
# Asserts:
#   - a fresh signed scrape answers 200 (the key works);
#   - the held stream keeps flowing PAST the window close (records keep
#     arriving; total ≈ rate x full-hold, not rate x window);
#   - a NEW stream still opens after the window (fresh signature) — the
#     listener is not degraded, only the open stream carries.
set -euo pipefail

: "${XTCP2_MODE:=bridge}"
: "${XTCP2_AUTH:=hmac}" # the carry-over is an HMAC-window shape
: "${XTCP2_ATTACKER:=plain}"
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

echo "rt-2-9: evidence in $RT_EVIDENCE"

xtcp2_deploy victim attacker
wait_for 120 "daemon answers on the victim VM" vm_exec victim \
  "curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:9088/metrics | grep -qE '^(200|401|403)\$'"
victim_still_reporting_tap
xtcp2_victim_read_token # in HMAC mode this is the shared key

# --- helpers -------------------------------------------------------------------

GS=/opt/xtcp2-redteam/bin/grpcstorm
GRPC=192.168.142.10:8889
RATE=20   # records/s — total ≈ 2200 if the stream survives the whole hold
HOLD=110  # seconds; spans at least one minute boundary from any open point

mint() { # SignedToken(key, utc-minute) — same one-liner as rt-2-2
  vm_exec_root victim "python3 -c \"import hmac,hashlib,base64,time,pathlib; key=pathlib.Path('/opt/xtcp2-redteam/secrets/read').read_text().strip(); minute=int(time.time())//60; mac=hmac.new(key.encode(),str(minute).encode(),hashlib.sha256).digest(); print(base64.urlsafe_b64encode(mac).rstrip(b'=').decode())\""
}

# --- control: the key works — a fresh signed scrape answers 200 ---------------------

SIGNED="$(mint)"
code="$(vm_exec victim "curl -s -m 6 -o /dev/null -w '%{http_code}' -H 'Authorization: Bearer $SIGNED' http://127.0.0.1:9088/metrics" || true)"
if [ "${code:-000}" = 200 ]; then
  ok "control: a fresh signed scrape answers 200"
else
  not_ok "control: the fresh signed scrape answered ${code:-000} — see evidence"
fi

# --- the carry-over: open just after a minute boundary, hold past the window --------

# The skew-0 window closes at the NEXT boundary (<=59 s in), so >=51 s of
# the hold is past expiry: if re-auth existed, delivery would stop at ~40%
# of the hold; carry-over reads ~100%.
S="$(date +%S)"
sleep $((61 - 10#$S))
echo "rt-2-9: stream opens at $(date -u +%H:%M:%S) (window closes at the next boundary)"

vm_exec attacker "nohup $GS -target $GRPC -hmac-key '$RT_READ_TOKEN' -conns 1 -streams 1 -read-rate $RATE -duration ${HOLD}s > /tmp/gs9.log 2>&1 &" >/dev/null
wait_for 30 "the carry-over stream is up" vm_exec attacker "grep -q GRPCSTORM_READY /tmp/gs9.log"
echo "rt-2-9: holding ${HOLD}s (auth window closes mid-hold)"

deadline=$((SECONDS + HOLD + 90))
until vm_exec attacker "grep -q GRPCSTORM_DONE /tmp/gs9.log" >/dev/null 2>&1; do
  [ "$SECONDS" -ge "$deadline" ] && break
  sleep 5
done
vm_exec attacker "grep GRPCSTORM /tmp/gs9.log" || true

total="$(vm_exec attacker "sed -n 's/GRPCSTORM_DONE total_records=//p' /tmp/gs9.log" || true)"
total="${total:-0}"
full_hold=$((RATE * HOLD))
stop_at_window=$((RATE * 45)) # generous upper bound if delivery stopped at the window
echo "rt-2-9: total_records=$total (full-hold ≈ $full_hold, stopped-at-window < $stop_at_window)"
if [ "$total" -ge $((full_hold * 80 / 100)) ]; then
  ok "CARRY-OVER CONFIRMED: the stream kept flowing past its auth window ($total of ~$full_hold records — the interceptor authorized once at open; no re-auth exists)"
else
  not_ok "the stream stopped early ($total of ~$full_hold records) — the window DID bite; see evidence"
fi

# --- control 2: a NEW stream (fresh signature) still opens after the window ----------

if vm_exec attacker "$GS -target $GRPC -hmac-key '$RT_READ_TOKEN' -conns 1 -streams 1 -read-rate 0 -duration 5s" >/dev/null 2>&1; then
  ok "control: a NEW stream with a fresh signature opens after the window (the listener is fine; only the open stream carries)"
else
  not_ok "control: a new stream with a fresh signature failed to open — see evidence"
fi

victim_still_reporting_tap

# --- verdict ------------------------------------------------------------------------

# In-flight streams do not re-authenticate: a captured-key holder (the
# rt-2-8 plaintext-capture tier) keeps an open stream for as long as it
# holds it, past the signature window. Standard stream semantics — filed
# as documentation, not a code fix (the product finding: the window bounds
# NEW RPCs only; operators rotating a leaked key must also break the
# streams — e.g. a listener restart).
finish rt-2-9 CONFIRMED
