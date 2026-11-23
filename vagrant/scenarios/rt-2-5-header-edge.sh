#!/usr/bin/env bash
# vagrant/scenarios/rt-2-5-header-edge.sh — RT-2, tier 1: Authorization
# header parser edge cases (docs/redteam-plan.md RT-2, plan rows rt-2-5 and
# rt-2-7).
#
# Table-driven: each near-miss presentation of the read token must 401 —
# the token in a URL query, duplicate headers, `Bearer` alone, `Basic`,
# lowercase `bearer`, a padded token, a bare token without scheme. The
# one presentation that must 200 is the exact `Bearer <token>`.
#
# AMENDED 2026-11-23: the plan row's "scheme case-insensitive" is wrong —
# the parser is exact-case (`Bearer ` prefix, pkg/listenerauth/listenerauth.go:
# 43/311), so a lowercase `bearer` is malformed and 401s. The grid pins the
# actual behavior; a future relaxation flips the lowercase cell.
set -euo pipefail

: "${XTCP2_AUTH:=raw}"
: "${XTCP2_ATTACKER:=read-cred}" # the grid presents the read credential
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

echo "rt-2-5: evidence in $RT_EVIDENCE"

xtcp2_deploy victim
wait_for 120 "daemon answers on the victim VM" vm_exec victim \
  "curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:9088/metrics | grep -qE '^(200|401|403)\$'"
victim_still_reporting_tap

xtcp2_cotenant_read_token
token="$RT_READ_TOKEN"
PROM="http://172.17.0.1:9088/metrics"

# probe <name> <want> <curl-args>: one table row from the attacker container.
# <curl-args> is a single string of sh-escaped curl arguments (the quotes
# around header values must survive vm_exec's ssh flattening).
probe() {
  local name="$1" want="$2" args="$3" code
  code="$(vm_exec victim "sudo docker exec rt-attacker curl -s -m 10 -o /dev/null -w '%{http_code}' $args")"
  echo "rt-2-5: $name -> $code (want $want)" | tee -a "$RT_EVIDENCE/grid.txt"
  if [ "$code" = "$want" ]; then
    ok "$name -> $want"
  else
    not_ok "$name -> $code (want $want)"
  fi
}

#                 presentation                       want  curl args (quote-escaped)
probe "exact Bearer (the one valid shape)"          200   "-H 'Authorization: Bearer $token' $PROM"
probe "token in URL query"                          401   "$PROM?token=$token"
probe "duplicate Authorization headers"             401   "-H 'Authorization: Bearer $token' -H 'Authorization: Bearer $token' $PROM"
probe "Bearer alone (no token)"                     401   "-H 'Authorization: Bearer' $PROM"
probe "Basic scheme"                                401   "-H 'Authorization: Basic $token' $PROM"
probe "lowercase bearer (scheme is case-sensitive)" 401   "-H 'Authorization: bearer $token' $PROM"
probe "padded token (trailing space)"               401   "-H 'Authorization: Bearer  $token ' $PROM"
probe "bare token (no scheme)"                     401   "-H 'Authorization: $token' $PROM"
probe "empty header value"                          401   "-H 'Authorization;' $PROM"
victim_still_reporting_tap

# BLOCKED = no presentation except the exact one yields the metrics body.
finish rt-2-5 BLOCKED
