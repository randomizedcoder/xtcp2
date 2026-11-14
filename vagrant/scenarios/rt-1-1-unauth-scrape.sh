#!/usr/bin/env bash
# vagrant/scenarios/rt-1-1-unauth-scrape.sh — RT-1, scenario 1:
# unauthenticated co-tenant scrape (docs/redteam-plan.md RT-1; the default-
# exposure family). Driver contract: docs/redteam-implementation.md.
#
# Question: with the daemon in its default deployment row (host network,
# auth off), can a plain container on the same docker host read
# TCP-observability metrics about the base OS and every other tenant?
#
# EXPECT=BLOCKED today. The plan doc originally guessed CONFIRMED (auth off
# the old default), but the listener-security-hardening program (PRs #291
# and #300) landed in between: the daemon refuses to start on an
# unauthenticated non-loopback TCP listener, so the harness's AUTH=off row
# serves loopback TCP with auth=disabled (the one legal unauth shape) and the
# co-tenant — a different netns — cannot reach 127.0.0.1 of the host netns.
# The scenario pins that: the exposure cannot regress silently. When it does
# regress, the expectation flip travels in the same PR as the fix.
#
# The exposure now gates on credential theft — the read-cred tier:
#   XTCP2_AUTH=raw XTCP2_ATTACKER=read-cred XTCP2_EXPECT=CONFIRMED \
#     nix run .#redteam-rt-1-1
# (a co-tenant that can read the mounted token file scrapes everything).
#
# Matrix flip (Phase 0 DoD — proves the harness drives real behavior):
#   XTCP2_AUTH=raw XTCP2_ATTACKER=plain nix run .#redteam-rt-1-1
# (the §29 prod shape: 401 for a tokenless co-tenant, still BLOCKED).
set -euo pipefail

XTCP2_EXPECT="${XTCP2_EXPECT:-BLOCKED}"

# shellcheck source=/dev/null
source "$(dirname "${BASH_SOURCE[0]}")/../lib/matrix.sh"
# shellcheck source=/dev/null
source "$(dirname "${BASH_SOURCE[0]}")/../lib/evidence.sh"
# shellcheck source=/dev/null
source "$(dirname "${BASH_SOURCE[0]}")/../lib/ssh.sh"
# shellcheck source=/dev/null
source "$(dirname "${BASH_SOURCE[0]}")/../lib/invariant.sh"
# shellcheck source=/dev/null
source "$(dirname "${BASH_SOURCE[0]}")/../lib/assert.sh"

xtcp2_matrix_defaults
xtcp2_preflight
evidence_init

echo "rt-1-1: evidence in $RT_EVIDENCE"

# Boot the victim VM unprovisioned, deliver the image tarballs the Vagrantfile
# cannot sync (the box ships no rsync), then provision the matrix row.
vm_up --no-provision victim
# The delivery dir exists only after common.sh; create it now so the scp that
# precedes the first `vagrant provision` has somewhere to land.
vm_exec victim 'mkdir -p xtcp2-redteam'
vm_push victim "$XTCP2_OCI_TAR" xtcp2-redteam/xtcp2-oci.tar.gz
vm_push victim "$XTCP2_TCP_STRESS_TAR" xtcp2-redteam/xtcp2-tcp-stress.tar.gz
vm_provision victim

# Baseline: the daemon answers locally and the catcher sees records before any
# attack. AUTH=off binds 127.0.0.1 (200); auth rows bind :9088 and answer
# 401 there — either is a live daemon.
PROM="http://127.0.0.1:9088/metrics"
wait_for 120 "daemon answers on the victim VM" vm_exec victim \
  "curl -s -o /dev/null -w '%{http_code}' $PROM | grep -qE '^(200|401|403)\$'"
victim_still_reporting_tap

# Attack: plain-tier co-tenant container, no credentials, curl the docker0
# gateway — the one address every bridge-networked container can always reach.
# The read-cred tier reads the token out of its own mount and presents it:
# the credential-theft exposure that is the real remaining default-row risk.
COTENANT_PROM="$(xtcp2_cotenant_prom_url)"
HDR=""
if [ "$XTCP2_ATTACKER" = read-cred ] || [ "$XTCP2_ATTACKER" = admin ]; then
  token="$(vm_exec victim 'sudo docker exec rt-attacker cat /run/secrets/read')"
  [ -n "$token" ] || xtcp2_die "read-cred tier: no token in the attacker container"
  HDR="-H \"Authorization: Bearer $token\""
  echo "rt-1-1: read-cred tier, presenting the stolen read token"
fi
code="$(vm_exec victim "sudo docker exec rt-attacker curl -s -m 5 -o /dev/null -w '%{http_code}' $HDR $COTENANT_PROM")" || code=000
echo "rt-1-1: co-tenant scrape HTTP $code"
vm_exec victim "sudo docker exec rt-attacker curl -s -m 5 $HDR $COTENANT_PROM" \
  >"$RT_EVIDENCE/cotenant-scrape.txt" || true

case "$code" in
  200)
    if grep -q 'xtcp_' "$RT_EVIDENCE/cotenant-scrape.txt"; then
      ok "co-tenant read /metrics (200, xtcp_ series present)"
    else
      not_ok "co-tenant got 200 but no xtcp_ series in the body"
    fi
    ;;
  401 | 403)
    ok "co-tenant scrape rejected (HTTP $code)"
    ;;
  000)
    ok "co-tenant cannot reach the metrics listener (loopback-only binding holds)"
    ;;
  *)
    not_ok "co-tenant scrape returned HTTP $code"
    ;;
esac
victim_still_reporting_tap

# Verdict.
case "$code" in
  200) actual=CONFIRMED ;;
  401 | 403 | 000) actual=BLOCKED ;;
  *) actual=NA ;;
esac
finish rt-1-1 "$actual"
