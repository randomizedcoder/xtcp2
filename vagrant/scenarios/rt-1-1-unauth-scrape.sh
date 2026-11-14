#!/usr/bin/env bash
# vagrant/scenarios/rt-1-1-unauth-scrape.sh — RT-1, scenario 1:
# unauthenticated co-tenant scrape (docs/redteam-plan.md RT-1; the default-
# exposure family). Driver contract: docs/redteam-implementation.md.
#
# Question: with the daemon in its default deployment row (host network, auth
# off), can a plain container on the same docker host read TCP-observability
# metrics about the base OS and every other tenant?
#
# EXPECT=CONFIRMED today: auth off is the daemon default; docs/security.md's
# "Listener security" documents the exposure and recommends bearer auth +
# loopback/UDS listeners in deployment. This scenario pins that exposure as a
# finding so the default cannot regress silently; when the default changes,
# the expectation flip travels in the same PR as the fix.
#
# Matrix flip (Phase 0 DoD — proves the harness drives real behavior):
#   XTCP2_MODE=bridge XTCP2_AUTH=raw XTCP2_EXPECT=BLOCKED \
#     nix run .#redteam-rt-1-1
set -euo pipefail

XTCP2_EXPECT="${XTCP2_EXPECT:-CONFIRMED}"

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
vm_exec victim 'mkdir -p /tmp/xtcp2-redteam'
vm_push victim "$XTCP2_OCI_TAR" /tmp/xtcp2-redteam/xtcp2-oci.tar.gz
vm_push victim "$XTCP2_TCP_STRESS_TAR" /tmp/xtcp2-redteam/xtcp2-tcp-stress.tar.gz
vm_provision victim

# Baseline: the daemon is up and the catcher sees records before any attack.
PROM="http://127.0.0.1:9088/metrics"
wait_for 120 "daemon answers on the victim VM" vm_exec victim \
  "curl -s -o /dev/null -w '%{http_code}' $PROM | grep -q 200"
victim_still_reporting_tap

# Attack: plain-tier co-tenant container, no credentials, curl the docker0
# gateway — the one address every bridge-networked container can always reach
# (the prod-shape host network answers there; so does -p publishing).
COTENANT_PROM="$(xtcp2_cotenant_prom_url)"
code="$(vm_exec victim "docker exec rt-attacker curl -s -m 5 -o /dev/null -w '%{http_code}' $COTENANT_PROM")" || code=000
echo "rt-1-1: co-tenant scrape HTTP $code"
vm_exec victim "docker exec rt-attacker curl -s -m 5 $COTENANT_PROM" \
  >"$RT_EVIDENCE/cotenant-scrape.txt" || true

case "$code" in
  200)
    if grep -q 'xtcp_' "$RT_EVIDENCE/cotenant-scrape.txt"; then
      ok "co-tenant read /metrics without credentials (200, xtcp_ series present)"
    else
      not_ok "co-tenant got 200 but no xtcp_ series in the body"
    fi
    ;;
  401 | 403)
    ok "co-tenant scrape rejected (HTTP $code)"
    ;;
  *)
    not_ok "co-tenant scrape returned HTTP $code"
    ;;
esac
victim_still_reporting_tap

# Verdict.
case "$code" in
  200) actual=CONFIRMED ;;
  401 | 403) actual=BLOCKED ;;
  *) actual=NA ;;
esac
finish rt-1-1 "$actual"
