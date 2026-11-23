#!/usr/bin/env bash
# vagrant/scenarios/rt-2-8-plaintext-capture.sh — RT-2: plaintext transport
# token capture (docs/redteam-plan.md RT-2, plan row rt-2-8).
#
# An on-path attacker (here: the victim VM's host network, the position a
# privileged co-tenant or a compromised docker0 bridge peer holds) captures
# an authenticated scrape and reads the raw bearer token out of the pcap.
#
# EXPECT=CONFIRMED (documented S3): there is no TLS by default — the token
# is visible in cleartext. The mitigation is transport security (a TLS
# proxy), which the app cannot provide itself; the scenario pins that the
# ONLY gap is the transport (nothing app-layer about HMAC keys leaks on a
# raw row — there is no HMAC key to leak).
set -euo pipefail

: "${XTCP2_AUTH:=raw}"
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

echo "rt-2-8: evidence in $RT_EVIDENCE"

xtcp2_deploy victim
wait_for 120 "daemon answers on the victim VM" vm_exec victim \
  "curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:9088/metrics | grep -qE '^(200|401|403)\$'"
victim_still_reporting_tap
xtcp2_cotenant_read_token
token="$RT_READ_TOKEN"
PROM="http://172.17.0.1:9088/metrics"

# --- on-path capture: tcpdump on docker0 while the co-tenant scrapes -------
vm_exec_root victim 'apt-get install -y -qq tcpdump >/dev/null 2>&1 || true'
vm_exec_root victim 'rm -f /tmp/rt2-8.pcap'
vm_exec_root victim 'timeout 12 tcpdump -i docker0 -A -s 0 -w /tmp/rt2-8.pcap port 9088 >/dev/null 2>&1 &'
sleep 1
vm_exec victim "for i in 1 2 3; do sudo docker exec rt-attacker curl -s -m 5 -o /dev/null -H 'Authorization: Bearer $token' $PROM || true; done"
sleep 10
vm_exec_root victim 'chmod 0644 /tmp/rt2-8.pcap'

# --- the read: pull the bearer out of the pcap ------------------------------
captured="$(vm_exec_root victim "grep -ac 'Authorization: Bearer $token' /tmp/rt2-8.pcap" || true)"
echo "rt-2-8: bearer-token occurrences in the docker0 pcap: $captured"
if [ "$captured" -ge 1 ]; then
  ok "on-path capture reads the raw bearer token in cleartext ($captured occurrence(s) — documented; TLS is the only mitigation)"
else
  not_ok "bearer token not found in the pcap (see evidence)"
fi

# --- the app-layer non-leak: the HMAC key never appears (nothing to leak
# on a raw row — pinned so an hmac row regression flips it) ----------------
key_lines="$(vm_exec_root victim "grep -ac 'HMAC' /tmp/rt2-8.pcap" || true)"
if [ "$key_lines" = 0 ]; then
  ok "no HMAC key material on the wire (raw row: there is none)"
else
  not_ok "HMAC-looking strings in the pcap — see evidence"
fi
victim_still_reporting_tap

# Pull the pcap into the evidence dir before the VM's tmp gets recycled.
vm_exec_root victim 'base64 /tmp/rt2-8.pcap' 2>/dev/null | grep -v '^\[fog\]' >"$RT_EVIDENCE/rt2-8.pcap.b64" \
  && vm_exec_root victim 'rm -f /tmp/rt2-8.pcap'

finish rt-2-8 CONFIRMED
