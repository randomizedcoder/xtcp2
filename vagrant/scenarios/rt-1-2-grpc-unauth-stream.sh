#!/usr/bin/env bash
# vagrant/scenarios/rt-1-2-grpc-unauth-stream.sh — RT-1, scenario 2:
# unauthenticated record streaming over gRPC (docs/redteam-plan.md RT-1).
#
# Question: can a remote peer (the attacker VM, one private-network hop away)
# stream the full XtcpFlatRecord stream — peer addresses, container ids,
# uplink NIC/LLDP — without credentials, via the repo's own client?
#
# EXPECT=BLOCKED (default row: §29 prod shape host+raw, plain attacker): the
# tokenless xtcp2client stream is refused (gRPC Unauthenticated). The plan
# doc's "default: stream succeeds" column predates the listener-security
# hardening (see findings.md, the premise-shift row).
#
# Flip (the credential-gated stream is the exfil premise of RT-3/RT-11
# tier 2 — a read token streams everything):
#   XTCP2_AUTH=raw XTCP2_ATTACKER=read-cred XTCP2_EXPECT=CONFIRMED \
#     nix run .#redteam-rt-1-2
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

echo "rt-1-2: evidence in $RT_EVIDENCE"

# Both VMs: the gRPC vantage is the attacker VM's xtcp2client.
xtcp2_deploy victim attacker

wait_for 120 "daemon answers on the victim VM" vm_exec victim \
  "curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:9088/metrics | grep -qE '^(200|401|403)\$'"
victim_still_reporting_tap

# Attack: one FlatRecords stream from the attacker VM, 8s budget.
# -format json keeps the records in the evidence (the question is access;
# ~60 rec/s × 8 s ≈ 500 KiB, gitignored). The timeout bounds the remote
# client; `timeout` cannot wrap vm_exec (a shell function, not a binary).
CLIENT=/opt/xtcp2-redteam/bin/xtcp2client
GRPC_ARGS=(-network tcp -target 192.168.142.10 -port 8889 -format json -d 11)
if [ "$XTCP2_ATTACKER" = read-cred ] || [ "$XTCP2_ATTACKER" = admin ]; then
  xtcp2_victim_read_token
  GRPC_ARGS+=(-auth-token "$RT_READ_TOKEN")
fi
vm_exec attacker "timeout 8 $CLIENT ${GRPC_ARGS[*]} -workers 1" \
  >"$RT_EVIDENCE/xtcp2client.log" 2>&1 || true
cat "$RT_EVIDENCE/xtcp2client.log" | tail -5
unauth="$(grep -ciE 'unauthenticated|permission denied|401|access' "$RT_EVIDENCE/xtcp2client.log" || true)"
records="$(grep -cE '^\{' "$RT_EVIDENCE/xtcp2client.log" || true)"

if [ "$records" -gt 0 ]; then
  ok "remote peer streamed XtcpFlatRecords ($records record lines)"
elif [ "$unauth" -gt 0 ]; then
  ok "remote peer's tokenless stream refused (unauthenticated)"
elif [ -s "$RT_EVIDENCE/xtcp2client.log" ]; then
  ok "remote peer got no stream and no auth handshake (unreachable: loopback-only gRPC)"
else
  not_ok "no client output at all (harness failure — see evidence)"
fi
victim_still_reporting_tap

if [ "$records" -gt 0 ]; then
  actual=CONFIRMED
elif [ -s "$RT_EVIDENCE/xtcp2client.log" ]; then
  actual=BLOCKED # refused, unauthenticated, or unreachable — no records leaked
else
  actual=NA
fi
finish rt-1-2 "$actual"
