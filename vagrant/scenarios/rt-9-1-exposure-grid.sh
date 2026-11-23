#!/usr/bin/env bash
# vagrant/scenarios/rt-9-1-exposure-grid.sh — RT-9, scenario 1: the
# bridge-vs-host exposure grid (docs/redteam-plan.md RT-9).
#
# Question: from each vantage (co-tenant container on the same docker host;
# attacker VM one private-network hop away; the victim VM's own host), which
# listener addresses answer, per deployment mode (host vs bridge)? The grid
# feeds docs/security.md's listener matrix.
#
# EXPECT=CONFIRMED (default row: host+raw): the listeners are REACHABLE from
# every vantage (that is the documented §29 prod shape — an internet-exposed
# host is gated by auth + the low TTL, not by unreachability). CONFIRMED here
# means "reachable surface confirmed and recorded", not "breach": the auth
# gate itself is rt-1-1's 401 assertion.
#
# Off row (loopback-only listeners): every remote vantage is refused —
#   XTCP2_AUTH=off XTCP2_EXPECT=BLOCKED nix run .#redteam-rt-9-1
set -euo pipefail

# This scenario's premise is the §29 prod shape (auth on); an env override
# still wins.
: "${XTCP2_AUTH:=raw}"
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

echo "rt-9-1: evidence in $RT_EVIDENCE (MODE=$XTCP2_MODE AUTH=$XTCP2_AUTH)"

xtcp2_deploy victim attacker

wait_for 120 "daemon answers on the victim VM" vm_exec victim \
  "curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:9088/metrics | grep -qE '^(200|401|403)\$'"
victim_still_reporting_tap

# The grid: three vantages × the metrics listener. Any HTTP answer (200/401/403)
# = reachable; 000 = unreachable. (The auth gate's outcome is rt-1-1's column.)
probe() { # probe <desc> <curl-command…>
  local desc="$1"
  shift
  local code
  code="$("$@" 2>/dev/null)" || code=000
  case "$code" in
    200 | 401 | 403)
      ok "$desc: reachable (HTTP $code)"
      echo "$desc reachable $code"
      ;;
    000) ok "$desc: unreachable (refused/timeout)" ;;
    *)
      not_ok "$desc: unexpected HTTP $code"
      echo "$desc unexpected $code"
      ;;
  esac
}

grid="$RT_EVIDENCE/grid.txt"
: >"$grid"

# vantage 1: the victim VM's own host loopback.
probe "vm-host loopback" vm_exec victim \
  "curl -s -m 5 -o /dev/null -w '%{http_code}' http://127.0.0.1:9088/metrics" \
  | tee -a "$grid"

# vantage 2: the co-tenant container via the docker0 gateway.
probe "co-tenant (docker0 gw)" vm_exec victim \
  "sudo docker exec rt-attacker curl -s -m 5 -o /dev/null -w '%{http_code}' http://172.17.0.1:9088/metrics" \
  | tee -a "$grid"

# vantage 3: the attacker VM across the private network.
probe "attacker VM (private net)" vm_exec attacker \
  "curl -s -m 5 -o /dev/null -w '%{http_code}' http://192.168.142.10:9088/metrics" \
  | tee -a "$grid"

victim_still_reporting_tap

# Verdict: CONFIRMED = at least the co-tenant and the remote VM both reached
# the listener (the exposed surface this scenario documents). BLOCKED = every
# remote vantage refused (the off row's loopback-only binding).
reach_cotenant=$(grep -c "co-tenant (docker0 gw) reachable" "$grid" || true)
reach_remote=$(grep -c "attacker VM (private net) reachable" "$grid" || true)
case $((reach_cotenant + reach_remote)) in
  2) actual=CONFIRMED ;;
  0) actual=BLOCKED ;;
  *) actual=NA ;;
esac
echo "rt-9-1: grid recorded in $grid"
finish rt-9-1 "$actual"
