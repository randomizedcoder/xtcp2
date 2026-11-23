#!/usr/bin/env bash
# vagrant/scenarios/rt-11-1-scrape-starvation.sh — RT-11, tier 1:
# scrape-slot starvation from a co-tenant container
# (docs/redteam-plan.md RT-11; the plan's headline monitoring-blindness row).
#
# The plan doc's mechanism: the Prometheus handler runs with
# promMaxRequestsInFlight = 10 (cmd/xtcp2/xtcp2.go:51), so an attacker
# holding 10 scrape slots with slow reads leaves the real scraper 503ing
# forever while collection and delivery carry on.
#
# EXPECT=BLOCKED — REFUTED AT HARNESS BUILD (2026-11-23, findings.md). The
# cap exists (client_golang v1.25.0 inFlightSem wraps reg.Gather through the
# response write), but a slow-reading co-tenant cannot hold it: /metrics is
# ~18 KiB, well under the kernel send buffer (autotunes to ~87 KiB), so the
# handler's write completes into the kernel even against a closed receive
# window (verified: Send-Q stuck at 14415 with the slot already released).
# The scenario pins the refutation with BOTH holder mechanisms so a future
# regression in either (bigger bodies, smaller buffers) flips it.
#
#   attempt 1 — rate-limited curl readers (the plan doc's mechanism)
#   attempt 2 — SO_RCVBUF=2048 sockets that never read (the obvious
#               workaround; needs python3 in the attacker container)
set -euo pipefail

# This scenario's premise needs authorized scrapes, so its default row is the
# §29 prod shape (an env override still wins).
: "${XTCP2_AUTH:=raw}"
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

echo "rt-11-1: evidence in $RT_EVIDENCE"

xtcp2_deploy victim

wait_for 120 "daemon answers on the victim VM" vm_exec victim \
  "curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:9088/metrics | grep -qE '^(200|401|403)\$'"
victim_still_reporting_tap

PROM="$(xtcp2_cotenant_prom_url)"
# The attacker needs an authorized scrape to occupy slots: read-cred tier
# mounts the token; the plain tier reads the victim's token host-side (the
# plan's point is that a read credential holder — which the fleet scraper is —
# can attempt this legitimately).
if [ "$XTCP2_ATTACKER" = plain ]; then
  xtcp2_victim_read_token
else
  xtcp2_cotenant_read_token
fi
token="$RT_READ_TOKEN"
HDR="-H \"Authorization: Bearer $token\""

# Pre-attack: an authorized scrape works, and the body is small (the number
# that makes the whole attack moot — recorded for the findings row).
body_bytes="$(vm_exec victim "sudo docker exec rt-attacker curl -s -m 10 $HDR $PROM | wc -c")" || body_bytes=0
echo "rt-11-1: authorized /metrics body is $body_bytes bytes"
if [ "$body_bytes" -gt 0 ]; then
  ok "authorized scrape before the attack (${body_bytes}B body)"
else
  not_ok "authorized scrape before the attack returned an empty body"
fi

RT_PROBE_CODE=""
probe_11th() { # probe_11th <desc>: sets RT_PROBE_CODE to the 11th scrape's
  # HTTP code (a global: the description echo must not ride the return path)
  local desc="$1" code
  code="$(vm_exec victim "sudo docker exec rt-attacker curl -s -m 10 -o /dev/null -w '%{http_code}' $HDR $PROM")" || code=000
  echo "rt-11-1: $desc -> 11th scrape HTTP $code"
  RT_PROBE_CODE="$code"
}

# --- attempt 1: 10 rate-limited readers (the plan doc's mechanism) ----------
holders=()
kill_holders() {
  local pid
  for pid in "${holders[@]:-}"; do
    kill "$pid" 2>/dev/null || true
  done
  holders=()
}
trap 'kill_holders || true' EXIT
for i in $(seq 1 10); do
  vm_exec victim "sudo docker exec rt-attacker curl -s --limit-rate 500 -m 45 $HDR $PROM -o /dev/null" \
    >>"$RT_EVIDENCE/holder-rate-$i.log" 2>&1 &
  holders+=("$!")
done
sleep 4
probe_11th 'rate-limited holders'
code_rate="$RT_PROBE_CODE"
case "$code_rate" in
  503) ok "attempt 1: 11th scrape 503s — the documented starvation HOLDS" ;;
  200) ok "attempt 1: 11th scrape still 200 (rate-limited readers hold no slot)" ;;
  *) not_ok "attempt 1: 11th scrape returned HTTP $code_rate" ;;
esac
kill_holders
wait 2>/dev/null || true

# --- attempt 2: 10 SO_RCVBUF sockets that never read -------------------------
vm_push victim "$RT_LIB_DIR/hold.py" /tmp/hold.py
vm_exec victim 'sudo docker cp /tmp/hold.py rt-attacker:/tmp/hold.py'
vm_exec victim 'sudo docker exec rt-attacker bash -c "command -v python3 >/dev/null || { apt-get update -qq >/dev/null && apt-get install -y -qq python3 >/dev/null; }"'
for i in $(seq 1 10); do
  vm_exec victim "sudo docker exec -d rt-attacker python3 /tmp/hold.py $PROM $token 45"
done
sleep 4
probe_11th 'SO_RCVBUF holders'
code_rcv="$RT_PROBE_CODE"
case "$code_rcv" in
  503) ok "attempt 2: 11th scrape 503s — never-reading sockets DO starve the slots" ;;
  200) ok "attempt 2: 11th scrape still 200 (response fits the send buffer; slot freed)" ;;
  *) not_ok "attempt 2: 11th scrape returned HTTP $code_rcv" ;;
esac

# The RT-11 invariant either way: collection and delivery carry on.
victim_still_reporting_tap

# Recovery: after the holders are gone the scrape surface answers.
sleep 42 # let the 45s holders expire
codeafter="$(vm_exec victim "sudo docker exec rt-attacker curl -s -m 10 -o /dev/null -w '%{http_code}' $HDR $PROM")" || codeafter=000
if [ "$codeafter" = 200 ]; then
  ok "scrape surface answers after the holders are gone (HTTP 200)"
else
  not_ok "scrape surface did not recover (HTTP $codeafter)"
fi
victim_still_reporting_tap
trap - EXIT

if [ "$code_rate" = 503 ] || [ "$code_rcv" = 503 ]; then
  actual=CONFIRMED # either holder mechanism starves the slots
elif [ "$code_rate" = 200 ] && [ "$code_rcv" = 200 ]; then
  actual=BLOCKED # refuted: neither mechanism holds a slot
else
  actual=NA
fi
finish rt-11-1 "$actual"
