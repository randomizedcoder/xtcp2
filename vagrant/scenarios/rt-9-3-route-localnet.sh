#!/usr/bin/env bash
# vagrant/scenarios/rt-9-3-route-localnet.sh — RT-9: route_localnet loopback
# reach (docs/redteam-plan.md RT-9, plan row rt-9-3; CVE-2020-8558 class).
#
# The attack shape: a co-tenant enables (or finds) the kube-proxy-style
# misconfig — docker0 with route_localnet=1 plus a NAT rule redirecting the
# gateway address to the host's loopback — then targets the loopback-only
# listener (the one shape an unauthenticated listener may take):
#   172.17.0.1:9088 --DNAT--> 127.0.0.1:9088 (auth=disabled row).
#
# The defense: a loopback TCP listener closes connections from non-loopback
# peers (pkg/listeners/listeners.go Listen -> listener.LoopbackOnly, the
# CVE-2020-8558-class check), so the routed peer never completes a scrape.
#
# EXPECT=BLOCKED: connection closed; the rate-limited reject line logs once
# per minute (cmd/xtcp2/reject_log.go), and the catcher keeps reporting.
set -euo pipefail

: "${XTCP2_AUTH:=off}" # the loopback-only shape is the off row
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

echo "rt-9-3: evidence in $RT_EVIDENCE"

xtcp2_deploy victim
wait_for 120 "daemon answers on the victim VM" vm_exec victim \
  "curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:9088/metrics | grep -qE '^(200|401|403)\$'"
victim_still_reporting_tap

# The misconfig a co-tenant (or kube-proxy-style glue) can produce. It is
# host-side state, so the scenario itself installs it — the attacker's part
# is the probe. Restore is the trap; nothing leaks between scenarios.
restore() {
  vm_exec_root victim 'sysctl -w net.ipv4.conf.docker0.route_localnet=0 >/dev/null' || true
  vm_exec_root victim 'iptables -t nat -D PREROUTING -d 172.17.0.1 -p tcp --dport 9088 -j DNAT --to-destination 127.0.0.1 2>/dev/null' || true
}
trap 'restore || true' EXIT
restore # idempotent: clean any earlier run's leftovers first
vm_exec_root victim 'sysctl -w net.ipv4.conf.docker0.route_localnet=1'
vm_exec_root victim 'iptables -t nat -A PREROUTING -d 172.17.0.1 -p tcp --dport 9088 -j DNAT --to-destination 127.0.0.1'
vm_exec_root victim 'iptables -t nat -S PREROUTING | grep 9088' >"$RT_EVIDENCE/nat.txt" 2>&1

# --- the co-tenant probe: the daemon must close the routed peer -----------
# curl: the daemon closing the socket reads as 000 (or 52/56), never 200.
code="$(vm_exec victim "sudo docker exec rt-attacker curl -s -m 10 -o /dev/null -w '%{http_code}' http://172.17.0.1:9088/metrics")" || code=000
echo "rt-9-3: co-tenant probe through the DNAT -> HTTP $code"
if [ "$code" = 200 ]; then
  not_ok "routed co-tenant scraped the loopback listener (HTTP 200 — the CVE-2020-8558 class defense FAILED)"
else
  ok "routed co-tenant got no scrape (HTTP $code: connection closed by the loopback-peer check)"
fi
victim_still_reporting_tap

# --- the defense's own trace: the rate-limited reject line -----------------
sleep 61 # the line is logged at most once per minute
vm_exec_root victim 'sudo docker logs rt-xtcp2 2>&1 | grep -c "non-loopback peers"' \
  >"$RT_EVIDENCE/reject.txt" 2>&1
n="$(grep -v fog "$RT_EVIDENCE/reject.txt" | tail -1)"
if awk -v n="$n" 'BEGIN{exit !(n>=1)}'; then
  ok "daemon logged the non-loopback peer close ($n line(s), rate-limited to 1/min)"
else
  not_ok "no non-loopback reject line in the daemon log (see evidence)"
fi

restore
trap - EXIT
victim_still_reporting_tap

finish rt-9-3 BLOCKED
