#!/usr/bin/env bash
# vagrant/scenarios/rt-2-2-hmac-replay.sh — RT-2: HMAC token capture and
# replay (docs/redteam-plan.md RT-2, plan row rt-2-2).
#
# AMENDED 2026-11-23 (verified): the plan row's "token valid for
# previous/current/next minute bucket" does NOT hold for the provisioned
# shape — the effective skew is 0 (exact minute). pkg/listenerauth's
# newWithClock sets a.skew = DefaultSignedTokenSkewMinutes (1) and then
# immediately overwrites it with cfg.GetSignedTokenSkewMinutes(), which is
# 0 when the proto field is unset — so the named default is unreachable and
# the window is exact unless signedTokenSkewMinutes is in the spec. The
# mint-minute replay still works; the plan's replay analysis stands with
# the corrected window.
#
# Flow (minute-boundary deterministic — every step is guarded to land at a
# known offset within the UTC minute):
#   1. fresh-minute guard, mint (SignedToken: HMAC-SHA256(key, utc-minute))
#   2. scrape with the minted token -> 200 (the fleet client's view)
#   3. on-path capture of the bearer from a docker0 pcap
#   4. replay immediately (same minute) -> 200 — CONFIRMED (documented)
#   5. wait to mint-minute+2, replay -> 401 (outside the window)
#   6. a previous-minute token -> 401 (the skew-0 pin: a future regression
#      to the ±1-minute default flips this cell)
#   7. a fresh mint still works (the credential was not revoked by replays)
set -euo pipefail

: "${XTCP2_AUTH:=hmac}"
: "${XTCP2_ATTACKER:=read-cred}" # the scrape must carry a real token
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

echo "rt-2-2: evidence in $RT_EVIDENCE"

xtcp2_deploy victim
wait_for 120 "daemon answers on the victim VM" vm_exec victim \
  "curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:9088/metrics | grep -qE '^(200|401|403)\$'"
victim_still_reporting_tap
PROM="http://172.17.0.1:9088/metrics"

# mint <minute-delta>: SignedToken(key, utc-minute + delta) — one line: the
# payload must survive vm_exec_root's %q quoting layer.
mint() { # mint <delta-minutes>
  vm_exec_root victim "python3 -c \"import hmac,hashlib,base64,time,pathlib; key=pathlib.Path(\\\"/opt/xtcp2-redteam/secrets/read\\\").read_text().strip(); minute=int(time.time())//60+$1; mac=hmac.new(key.encode(),str(minute).encode(),hashlib.sha256).digest(); print(base64.urlsafe_b64encode(mac).rstrip(b\\\"=\\\").decode())\""
}

# fresh-minute guard: land the mint in seconds 05..45 of a UTC minute.
sec=$(( $(date -u +%s) % 60 ))
if [ "$sec" -lt 5 ] || [ "$sec" -gt 45 ]; then
  wait=$(( 65 - sec ))
  echo "rt-2-2: minute-boundary guard: sleeping ${wait}s (second :$sec)"
  sleep "$wait"
fi

# --- 1. the mint + the fleet client's scrape --------------------------------
MINT_MINUTE=$(( $(date -u +%s) / 60 ))
HMAC_TOKEN="$(mint 0)" || xtcp2_die 'could not mint the HMAC token (python3 on the VM?)'
echo "rt-2-2: minted minute ${MINT_MINUTE}, token ${HMAC_TOKEN:0:8}…"
vm_exec_root victim 'apt-get install -y -qq tcpdump >/dev/null 2>&1 || true'
vm_exec_root victim 'rm -f /tmp/rt2-2.pcap'
vm_exec_root victim 'timeout 8 tcpdump -i docker0 -A -s 0 -w /tmp/rt2-2.pcap port 9088 >/dev/null 2>&1 &'
sleep 1
code="$(vm_exec victim "sudo docker exec rt-attacker curl -s -m 5 -o /dev/null -w '%{http_code}' -H 'Authorization: Bearer $HMAC_TOKEN' $PROM")"
echo "rt-2-2: minted token scrape -> HTTP $code (second :$(( $(date -u +%s) % 60 )))"
if [ "$code" = 200 ]; then
  ok "the minted HMAC token authenticates (the fleet client's view)"
else
  not_ok "the minted token was rejected (HTTP $code — see evidence)"
fi

# --- 2. the on-path capture ------------------------------------------------
sleep 8 # the 8s tcpdump boundary
vm_exec_root victim 'chmod 0644 /tmp/rt2-2.pcap'
CAPTURED="$(vm_exec_root victim "grep -aoE 'Authorization: Bearer [A-Za-z0-9_-]+' /tmp/rt2-2.pcap | head -1")"
echo "rt-2-2: pcap header line: ${CAPTURED:0:24}…"
if [ -n "$CAPTURED" ]; then
  ok "on-path capture reads the HMAC bearer off the wire"
else
  not_ok "no Authorization header in the pcap (see evidence)"
fi

# --- 3. replay 1: same minute (inside the window) --------------------------
r1="$(vm_exec victim "sudo docker exec rt-attacker curl -s -m 5 -o /dev/null -w '%{http_code}' -H '$CAPTURED' $PROM")"
echo "rt-2-2: replay in-minute -> HTTP $r1 (second :$(( $(date -u +%s) % 60 )))"
if [ "$r1" = 200 ]; then
  ok "replay inside the token's minute succeeds (documented)"
else
  not_ok "same-minute replay rejected (HTTP $r1)"
fi
victim_still_reporting_tap

# --- 4. replay 2: mint-minute+2 (outside the window) -----------------------
now=$(date -u +%s)
target=$(( (MINT_MINUTE + 2) * 60 + 5 ))
if [ "$target" -le "$now" ]; then
  xtcp2_die 'minute guard failed: the run outlived the mint minute — rerun'
fi
sleep $(( target - now ))
r2="$(vm_exec victim "sudo docker exec rt-attacker curl -s -m 5 -o /dev/null -w '%{http_code}' -H '$CAPTURED' $PROM")"
echo "rt-2-2: replay after the window -> HTTP $r2"
if [ "$r2" = 401 ]; then
  ok "replay outside the window is rejected (HTTP 401) — the replay defense holds"
else
  not_ok "replay outside the window got HTTP $r2 (want 401)"
fi

# --- 5. the skew-0 pin: a previous-minute token is not accepted -------------
PREV="$(mint -1)" || xtcp2_die 'could not mint the previous-minute token'
r3="$(vm_exec victim "sudo docker exec rt-attacker curl -s -m 5 -o /dev/null -w '%{http_code}' -H 'Authorization: Bearer $PREV' $PROM")"
echo "rt-2-2: previous-minute token -> HTTP $r3"
if [ "$r3" = 401 ]; then
  ok "previous-minute token rejected (effective skew is 0; the ±1-minute plan premise is amended)"
else
  not_ok "previous-minute token accepted (HTTP $r3 — skew is NOT 0; amend this scenario)"
fi

# --- 6. a fresh mint still works (no credential lockout from replays) -------
NEXT="$(mint 0)" || xtcp2_die 'could not mint the current token'
r4="$(vm_exec victim "sudo docker exec rt-attacker curl -s -m 5 -o /dev/null -w '%{http_code}' -H 'Authorization: Bearer $NEXT' $PROM")"
if [ "$r4" = 200 ]; then
  ok "a fresh mint still authenticates (no lockout from replays)"
else
  not_ok "fresh mint rejected (HTTP $r4)"
fi
victim_still_reporting_tap

vm_exec_root victim 'base64 /tmp/rt2-2.pcap' 2>/dev/null | grep -v '^\[fog\]' >"$RT_EVIDENCE/rt2-2.pcap.b64" \
  && vm_exec_root victim 'rm -f /tmp/rt2-2.pcap'

# CONFIRMED: the in-minute replay worked (documented); the out-of-window and
# previous-minute pins catch a skew regression.
finish rt-2-2 CONFIRMED
