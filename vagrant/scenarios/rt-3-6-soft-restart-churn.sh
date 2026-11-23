#!/usr/bin/env bash
# vagrant/scenarios/rt-3-6-soft-restart-churn.sh — RT-3: soft-restart churn
# (docs/redteam-plan.md RT-3, plan row rt-3-6).
#
# Premise: every Set ends in a syscall.Exec re-exec; an admin credential
# holder can churn the daemon at will (bounded only by their own rate). The
# pass criteria: the restart loop must not wedge listeners or leak UDS
# files — after N Sets the daemon still serves metrics + gRPC, the catcher
# keeps reporting, and no socket files accumulate.
#
# The harness daemon's listeners are spec'd TCP (the §29 shape), so the UDS
# leak assert runs against the daemon's /run/xtcp2: the container binds no
# UDS listener, and the pre-existing socket directory must stay empty of
# stale sockets — the plan's "assert socket cleanup" on a shape where a UDS
# listener is not in the spec.
set -euo pipefail

: "${XTCP2_AUTH:=raw}"
: "${XTCP2_ATTACKER:=admin}" # Set is an admin method
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

echo "rt-3-6: evidence in $RT_EVIDENCE"

xtcp2_deploy victim attacker
wait_for 120 "daemon answers on the victim VM" vm_exec victim \
  "curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:9088/metrics | grep -qE '^(200|401|403)\$'"
victim_still_reporting_tap

admin_token="$(vm_exec victim 'sudo docker exec rt-attacker cat /run/secrets/admin')"
[ -n "$admin_token" ] || xtcp2_die 'no admin token in the attacker container'
CTL=/opt/xtcp2-redteam/bin/xtcp2ctl
CTL_ARGS="-network tcp -target 192.168.142.10 -port 8889 -auth-token"

# the reconfigure payload: the CURRENT config (a no-op Set — the churn is the
# point, not a config change)
vm_exec attacker "$CTL get $CTL_ARGS '$admin_token' 2>/dev/null | grep -v '^\[fog\]'" \
  >"$RT_EVIDENCE/config.json" || xtcp2_die 'get failed'
vm_push attacker "$RT_EVIDENCE/config.json" /tmp/rt3-6-config.json

# --- the churn: N Sets back-to-back -----------------------------------------
N=10
ok_sets=0
for _ in $(seq 1 "$N"); do
  if vm_exec attacker "$CTL reconfigure $CTL_ARGS '$admin_token' -file /tmp/rt3-6-config.json" \
    >>"$RT_EVIDENCE/churn.log" 2>&1; then
    ok_sets=$((ok_sets + 1))
  fi
done
echo "rt-3-6: $ok_sets/$N reconfigures accepted"
if [ "$ok_sets" = "$N" ]; then
  ok "all $N Sets accepted (the churn rate is bounded only by the attacker)"
else
  not_ok "only $ok_sets/$N Sets accepted (see evidence)"
fi

# --- the survivors: listeners, catcher, and the audit count -----------------
sleep 10
xtcp2_victim_read_token
read_token="$RT_READ_TOKEN"
code="$(vm_exec victim "curl -s -o /dev/null -w '%{http_code}' -H 'Authorization: Bearer $read_token' http://127.0.0.1:9088/metrics")"
if [ "$code" = 200 ]; then
  ok "the metrics listener survived the churn (HTTP 200)"
else
  not_ok "the metrics listener is wedged after the churn (HTTP $code)"
fi
get_code="$(vm_exec attacker "$CTL get $CTL_ARGS '$admin_token' >/dev/null 2>&1 && echo ok || echo fail")"
if [ "$get_code" = ok ]; then
  ok "the gRPC listener survived the churn (Get answers)"
else
  not_ok "the gRPC listener is wedged after the churn (Get failed)"
fi
victim_still_reporting_tap

restarts="$(vm_exec victim 'sudo docker logs rt-xtcp2 2>&1 | grep -c "Set: reconfigure requested"')"
echo "rt-3-6: audit: $restarts reconfigure line(s) for $ok_sets Set(s) (+rt-3-1's earlier runs share this daemon log)"
if [ "$restarts" -ge "$ok_sets" ]; then
  ok "every Set left an audit line ($restarts >= $ok_sets)"
else
  not_ok "audit undercount: $restarts line(s) for $ok_sets Set(s)"
fi

# --- the UDS leak assert: no stale sockets accumulate ------------------------
# docker diff (not exec): the scratch image carries no shell.
socks="$(vm_exec victim 'sudo docker diff rt-xtcp2 2>/dev/null | grep -c "/run/xtcp2/"' || true)"
echo "rt-3-6: /run/xtcp2 filesystem entries after the churn: $socks"
if [ "$socks" = 0 ]; then
  ok "no UDS leak (no stale sockets; this spec binds TCP listeners only)"
else
  not_ok "sockets present in /run/xtcp2 after the churn: $socks — see evidence"
fi

victim_still_reporting_tap

# BLOCKED = the churn did not wedge anything (the S3 residual: the rate
# itself is unbounded — that is the rt-3-6 finding, not a failure).
finish rt-3-6 BLOCKED
