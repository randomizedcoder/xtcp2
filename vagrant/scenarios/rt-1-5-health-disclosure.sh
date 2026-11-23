#!/usr/bin/env bash
# vagrant/scenarios/rt-1-5-health-disclosure.sh — RT-1, scenario 5:
# health/ready endpoints without credentials (docs/redteam-plan.md RT-1).
#
# Question: can a co-tenant container learn daemon readiness (liveness) for
# free, without a credential? The documented design: health is read-classified
# and rides the same listener as metrics, with NO unauthenticated exception —
# so with auth on, /healthz and /readyz are 401. The K8s-probe tradeoff
# (probes need the token too) is the documented cost of that choice.
#
# EXPECT=BLOCKED (default row: §29 prod shape host+raw, plain attacker):
# both /healthz and /readyz answer 401 to a tokenless co-tenant.
#
# Flip (the tradeoff is real — read credential gets both):
#   XTCP2_ATTACKER=read-cred XTCP2_EXPECT=CONFIRMED nix run .#redteam-rt-1-5
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

echo "rt-1-5: evidence in $RT_EVIDENCE"

xtcp2_deploy victim

# Baseline: the daemon answers locally (auth rows bind :9088 → 401 there).
wait_for 120 "daemon answers on the victim VM" vm_exec victim \
  "curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:9088/healthz | grep -qE '^(200|401|403)\$'"
victim_still_reporting_tap

# Attack: tokenless co-tenant reads both probe paths via the docker0 gateway.
GW=172.17.0.1
HDR=""
HDR=""
if [ "$XTCP2_ATTACKER" = read-cred ] || [ "$XTCP2_ATTACKER" = admin ]; then
  xtcp2_cotenant_read_token
  HDR="-H \"Authorization: Bearer $RT_READ_TOKEN\""
fi
leaked=0
for path in healthz readyz; do
  code="$(vm_exec victim "sudo docker exec rt-attacker curl -s -m 5 -o /dev/null -w '%{http_code}' $HDR http://$GW:9088/$path")" || code=000
  echo "rt-1-5: co-tenant /$path -> HTTP $code"
  vm_exec victim "sudo docker exec rt-attacker curl -s -m 5 $HDR http://$GW:9088/$path" \
    >"$RT_EVIDENCE/cotenant-$path.txt" || true
  case "$code" in
    200)
      ok "co-tenant read /$path without a credential barrier (HTTP 200)"
      leaked=$((leaked + 1))
      ;;
    401 | 403) ok "co-tenant /$path rejected (HTTP $code — no unauth health exception)" ;;
    000) ok "co-tenant cannot reach /$path (loopback-only binding holds)" ;;
    *) not_ok "co-tenant /$path returned HTTP $code" ;;
  esac
done
victim_still_reporting_tap

# Verdict: CONFIRMED only if BOTH probe paths leaked readiness without creds.
case "$leaked" in
  2) actual=CONFIRMED ;;
  0) actual=BLOCKED ;;
  *) actual=NA ;;
esac
finish rt-1-5 "$actual"
