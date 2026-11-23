#!/usr/bin/env bash
# vagrant/scenarios/rt-1-6-fingerprint.sh — RT-1, scenario 6: version/flag
# fingerprint (docs/redteam-plan.md RT-1).
#
# Question: what can a remote peer learn about the daemon binary (flavor,
# enrichers, listener layout, version) from a metrics scrape? The fingerprint
# is inherent to the metrics surface (xtcp2_listener_active{...},
# compiledInEnrichers, go runtime families); the question is only whether it
# is reachable without a credential.
#
# EXPECT=BLOCKED (default row: §29 prod shape host+raw, plain attacker): the
# tokenless scrape body carries no xtcp_/xtcp2_/go_ families — nothing beyond
# the auth error.
#
# Flip (the fingerprint is inherent — a read credential reads it all):
#   XTCP2_ATTACKER=read-cred XTCP2_EXPECT=CONFIRMED nix run .#redteam-rt-1-6
set -euo pipefail

# This scenario's premise is the §29 prod shape (auth on); an env override
# still wins.
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

echo "rt-1-6: evidence in $RT_EVIDENCE"

xtcp2_deploy victim

wait_for 120 "daemon answers on the victim VM" vm_exec victim \
  "curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:9088/metrics | grep -qE '^(200|401|403)\$'"
victim_still_reporting_tap

# Attack: scrape from the co-tenant; inventory what the body discloses.
PROM="$(xtcp2_cotenant_prom_url)"
HDR=""
HDR=""
if [ "$XTCP2_ATTACKER" = read-cred ] || [ "$XTCP2_ATTACKER" = admin ]; then
  xtcp2_cotenant_read_token
  HDR="-H \"Authorization: Bearer $RT_READ_TOKEN\""
fi
code="$(vm_exec victim "sudo docker exec rt-attacker curl -s -m 10 -o /dev/null -w '%{http_code}' $HDR $PROM")" || code=000
vm_exec victim "sudo docker exec rt-attacker curl -s -m 10 $HDR $PROM" \
  >"$RT_EVIDENCE/cotenant-scrape.txt" || true
echo "rt-1-6: co-tenant scrape HTTP $code"

families=0
if grep -qE '^(xtcp|go_|process_)' "$RT_EVIDENCE/cotenant-scrape.txt"; then
  families=1
fi

case "$code" in
  200)
    if [ "$families" = 1 ]; then
      # The fingerprint surface, quoted into the findings row: listener
      # layout + compiled-in enrichers + runtime families.
      grep -E '^(xtcp2_listener_active|xtcp_gauges\{function="InitPromethus")' \
        "$RT_EVIDENCE/cotenant-scrape.txt" | head -8 \
        >"$RT_EVIDENCE/fingerprint.txt" || true
      ok "co-tenant read the fingerprint surface (200, xtcp/go families present)"
    else
      not_ok "co-tenant got 200 but no fingerprint families in the body"
    fi
    ;;
  401 | 403)
    if [ "$families" = 0 ]; then
      ok "co-tenant scrape rejected (HTTP $code) with no fingerprint leakage"
    else
      not_ok "co-tenant got $code but the body leaked fingerprint families"
    fi
    ;;
  000)
    ok "co-tenant cannot reach the metrics listener (loopback-only binding holds)"
    ;;
  *)
    not_ok "co-tenant scrape returned HTTP $code"
    ;;
esac
victim_still_reporting_tap

case "$code" in
  200) [ "$families" = 1 ] && actual=CONFIRMED || actual=NA ;;
  401 | 403 | 000) [ "$families" = 0 ] && actual=BLOCKED || actual=CONFIRMED ;;
  *) actual=NA ;;
esac
finish rt-1-6 "$actual"
