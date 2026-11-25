#!/usr/bin/env bash
# vagrant/scenarios/rt-4-3-grpc-storm.sh — RT-4: gRPC stream abuse
# (docs/redteam-plan.md row rt-4-3).
#
# The chain: the premise floods the gRPC listener — many conns x up to 20
# streams each, 4 MB messages, and reflection spam. The guards: the per-conn
# stream cap (MaxConcurrentStreams=20), the 4 MB recv cap
# (MaxRecvMsgSize=4_000_000, pkg/xtcp/grpc_server.go:31-33) and the
# listenerauth policy in front of every method — reflection included
# (reflection.Register at pkg/xtcp/grpc_server.go:122 sits behind the same
# StreamServerInterceptor, so an unauthenticated reflection flood is
# already the rt-1-2 shape: refused at the interceptor).
#
# Legs measured:
#   A. oversize messages (read creds): the method ACL answers
#      PermissionDenied — PollFlatRecords is admin-served, so a read
#      credential never even reaches the recv-size check (no method
#      enumeration for the read role either: Unimplemented would leak the
#      method's existence; the policy answers first).
#   B. oversize messages (admin creds): the 4 MB cap answers
#      ResourceExhausted per RPC — the transport backstop.
#   C. the stream storm: 10 conns x 20 streams (the per-conn cap) of
#      READING FlatRecords streams for 60 s: bounded degradation only —
#      the fan-out cost is goroutines (linear, ~1/stream) and the RT-11
#      invariant holds; no goroutine leak after close.
#
# The never-reading variant of the storm is NOT this row: that is the
# rt-11-3 wedge (a read credential stopping collection) and is measured
# there.
set -euo pipefail

: "${XTCP2_MODE:=bridge}"
: "${XTCP2_AUTH:=raw}"
: "${XTCP2_ATTACKER:=plain}"
: "${XTCP2_PPROF:=1}" # the goroutine-count asserts need the debug listener
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

echo "rt-4-3: evidence in $RT_EVIDENCE"

xtcp2_deploy victim attacker
wait_for 120 "daemon answers on the victim VM" vm_exec victim \
  "curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:9088/metrics | grep -qE '^(200|401|403)\$'"
victim_still_reporting_tap
xtcp2_victim_read_token
admin_token="$(vm_exec_root victim 'cat /opt/xtcp2-redteam/secrets/admin')"

# --- helpers -------------------------------------------------------------------

GS=/opt/xtcp2-redteam/bin/grpcstorm
GRPC=192.168.142.10:8889
CONNS=10
STREAMS=20 # the per-conn cap (MaxConcurrentStreams)
HOLD=60s

pprof_goroutines() {
  vm_exec victim "curl -s -m 6 -H 'Authorization: Bearer $RT_READ_TOKEN' 'http://127.0.0.1:9090/debug/pprof/goroutine?debug=1' | head -1" \
    | grep -oE '[0-9]+$' || echo 0
}

scrape_code() {
  vm_exec victim "curl -s -m 6 -o /dev/null -w '%{http_code}' -H 'Authorization: Bearer $RT_READ_TOKEN' http://127.0.0.1:9088/metrics" || true
}

BASE_G="$(pprof_goroutines)"

# --- leg A: oversize with read creds — the method ACL answers first ----------------

vm_exec attacker "$GS -target $GRPC -token '$RT_READ_TOKEN' -conns 3 -oversize 8000000 > /tmp/gs-over-read.log 2>&1; echo exit=\$?" || true
read_denied="$(vm_exec attacker "grep -c 'status=PermissionDenied' /tmp/gs-over-read.log" || true)"
read_denied="${read_denied:-0}"
read_ok="$(vm_exec attacker "grep -c 'status=OK' /tmp/gs-over-read.log" || true)"
read_ok="${read_ok:-0}"
echo "rt-4-3: leg A (read creds, 8MB): PermissionDenied=$read_denied OK=$read_ok"
if [ "$read_denied" -eq 3 ] && [ "$read_ok" -eq 0 ]; then
  ok "oversize with READ creds: every RPC answered PermissionDenied — the method ACL (PollFlatRecords is admin-served) refuses before the recv cap is even reached"
else
  not_ok "oversize with READ creds: denied=$read_denied accepted=$read_ok — see evidence"
fi

# --- leg B: oversize with admin creds — the 4 MB cap answers ------------------------

vm_exec attacker "$GS -target $GRPC -token '$admin_token' -conns 3 -oversize 8000000 > /tmp/gs-over-admin.log 2>&1; echo exit=\$?" || true
admin_exhausted="$(vm_exec attacker "grep -c 'status=ResourceExhausted' /tmp/gs-over-admin.log" || true)"
admin_exhausted="${admin_exhausted:-0}"
echo "rt-4-3: leg B (admin creds, 8MB): ResourceExhausted=$admin_exhausted"
if [ "$admin_exhausted" -eq 3 ]; then
  ok "oversize with ADMIN creds: every RPC answered ResourceExhausted — the MaxRecvMsgSize=4MB cap answers (no oversized message reaches the stream)"
else
  not_ok "oversize with ADMIN creds: ResourceExhausted=$admin_exhausted of 3 — see evidence"
fi

# --- leg C: the reading stream storm — bounded degradation only --------------------

vm_exec attacker "nohup $GS -target $GRPC -token '$RT_READ_TOKEN' -conns $CONNS -streams $STREAMS -read-rate 100 -duration $HOLD > /tmp/gs-storm.log 2>&1 &" >/dev/null
wait_for 60 "the storm streams are up" vm_exec attacker "grep -q GRPCSTORM_READY /tmp/gs-storm.log"
sleep 8 # let the streams register server-side and the fan-out engage

STORM_G="$(pprof_goroutines)"
code="$(scrape_code)"
echo "rt-4-3: leg C: goroutines=$BASE_G->$STORM_G scrape=${code:-000}"

# goroutine cost is linear: ~1/stream (handlers) + ~5/conn (transport)
goroutine_budget=$((BASE_G + CONNS * STREAMS + CONNS * 6 + 16))
if [ "$STORM_G" -le "$goroutine_budget" ]; then
  ok "the goroutine cost is bounded and linear: $BASE_G -> $STORM_G for $CONNS conns x $STREAMS streams (budget $goroutine_budget)"
else
  not_ok "goroutines $BASE_G -> $STORM_G exceeded the linear budget $goroutine_budget — see evidence"
fi

if [ "${code:-000}" = 200 ]; then
  ok "the scrape path stays 200 through the storm"
else
  not_ok "the scrape path degraded (code=${code:-000}) — see evidence"
fi

victim_still_reporting_tap

# --- release: no leak --------------------------------------------------------------

deadline=$((SECONDS + 120))
until vm_exec attacker "grep -q GRPCSTORM_DONE /tmp/gs-storm.log" >/dev/null 2>&1; do
  [ "$SECONDS" -ge "$deadline" ] && break
  sleep 3
done
total="$(vm_exec attacker "sed -n 's/GRPCSTORM_DONE total_records=//p' /tmp/gs-storm.log" || true)"
total="${total:-0}"
echo "rt-4-3: storm read $total records"
if [ "$total" -gt 0 ]; then
  ok "the storm streams delivered ($total records read)"
else
  not_ok "the storm streams read nothing ($total) — see evidence"
fi

sleep 5 # transport teardown
AFTER_G="$(pprof_goroutines)"
if [ "$AFTER_G" -le $((BASE_G + 4)) ]; then
  ok "no goroutine leak after the storm ($BASE_G -> $STORM_G -> $AFTER_G)"
else
  not_ok "goroutines leaked after the storm ($BASE_G -> $AFTER_G) — see evidence"
fi
victim_still_reporting_tap

# --- verdict -----------------------------------------------------------------------

# All three legs land inside the guards: the ACL answers read-cred oversized
# RPCs before the cap, the 4 MB cap answers admin-cred ones, and the reading
# storm degrades linearly with no leak. Bounded degradation only.
finish rt-4-3 BLOCKED
