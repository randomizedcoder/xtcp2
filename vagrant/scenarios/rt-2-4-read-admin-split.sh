#!/usr/bin/env bash
# vagrant/scenarios/rt-2-4-read-admin-split.sh — RT-2, tier 1: the read/admin
# split enforced over both surfaces (docs/redteam-plan.md RT-2).
#
# The 401/403 grid on the §29 prod shape (auth=raw, both listeners
# tokenFile+adminTokenFile): a read credential must reach read methods and
# 403/PermissionDenied on admin ones; no credential must be 401 everywhere.
# HTTP side: /metrics (read), /debug/pprof/ (admin — not served on this
# listener: read=403 at the gate, admin=404 past it). gRPC side:
# ConfigService.get (read) and set-poll-frequency (admin) via xtcp2ctl.
set -euo pipefail

: "${XTCP2_AUTH:=raw}"
: "${XTCP2_ATTACKER:=read-cred}" # the grid presents the read credential
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

echo "rt-2-4: evidence in $RT_EVIDENCE"

xtcp2_deploy victim attacker
wait_for 120 "daemon answers on the victim VM" vm_exec victim \
  "curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:9088/metrics | grep -qE '^(200|401|403)\$'"
victim_still_reporting_tap

xtcp2_cotenant_read_token # read-cred tier: the read token as the co-tenant sees it
co_read="$RT_READ_TOKEN"
admin_token="$(vm_exec_root victim 'cat /opt/xtcp2-redteam/secrets/admin')"
[ -n "$admin_token" ] || xtcp2_die 'no admin token on the victim'
PROM="http://172.17.0.1:9088"

# cotenant <path> <token>: HTTP code of one scrape from the attacker container
cotenant() {
  vm_exec victim "sudo docker exec rt-attacker curl -s -m 10 -o /dev/null -w '%{http_code}' -H 'Authorization: Bearer $2' $PROM$1"
}
grid_line() { # grid_line <name> <path> <want-none> <want-read> <want-admin>
  local name="$1" path="$2" wn="$3" wr="$4" wa="$5" cn cr ca
  cn="$(vm_exec victim "sudo docker exec rt-attacker curl -s -m 10 -o /dev/null -w '%{http_code}' $PROM$path")"
  cr="$(cotenant "$path" "$co_read")"
  ca="$(cotenant "$path" "$admin_token")"
  echo "rt-2-4: $name — none:$cn read:$cr admin:$ca (want $wn/$wr/$wa)" \
    | tee -a "$RT_EVIDENCE/grid.txt"
  if [ "$cn" = "$wn" ] && [ "$cr" = "$wr" ] && [ "$ca" = "$wa" ]; then
    ok "$name grid $wn/$wr/$wa holds"
  else
    not_ok "$name grid $wn/$wr/$wa BROKEN (got $cn/$cr/$ca)"
  fi
}

# --- HTTP surface ------------------------------------------------------------
grid_line "GET /metrics (read)" /metrics 401 200 200
# pprof is the admin path on the metrics listener; it is not served there, so
# the admin credential passes the gate and 404s — the read one 403s at it.
grid_line "GET /debug/pprof/ (admin; unserved here)" /debug/pprof/ 401 403 404
victim_still_reporting_tap

# --- gRPC surface (xtcp2ctl drives ConfigService) -----------------------------
CTL=/opt/xtcp2-redteam/bin/xtcp2ctl
# get is ACCESS_READ; set-poll-frequency is ACCESS_ADMIN (-frequency/-timeout
# are its required args, exactly as it would be invoked in the fleet). The
# tools image lives on the attacker VM (deploy.sh), so the gRPC grid runs
# from there — the split is a server-side behavior, not a vantage behavior.
ctl_run() { # ctl_run <token> <subcommand> [args...]: "ok" or "fail"
  local tok="$1"
  shift
  # xtcp2ctl takes the command first, then the shared flags
  vm_exec attacker "$CTL $* -network tcp -target 192.168.142.10 -port 8889 -auth-token '$tok'" \
    >>"$RT_EVIDENCE/xtcp2ctl.log" 2>&1 && echo ok || echo fail
}

g_none="$(ctl_run a-wrong-token-of-the-right-shape get)"
g_read="$(ctl_run "$co_read" get)"
g_admin="$(ctl_run "$admin_token" get)"
s_read="$(ctl_run "$co_read" set-poll-frequency -frequency 2s -timeout 1s)"
s_admin="$(ctl_run "$admin_token" set-poll-frequency -frequency 2s -timeout 1s)"
echo "rt-2-4: gRPC — get none:$g_none read:$g_read admin:$g_admin; set-poll-frequency read:$s_read admin:$s_admin" \
  | tee -a "$RT_EVIDENCE/grid.txt"

if [ "$g_read" = ok ] && [ "$g_admin" = ok ] && [ "$s_admin" = ok ]; then
  ok "gRPC read methods: read and admin tokens work (get)"
else
  not_ok "gRPC read method failed (get none:$g_none read:$g_read admin:$g_admin — see evidence)"
fi
if [ "$s_read" = fail ] && grep -qiE "PermissionDenied" "$RT_EVIDENCE/xtcp2ctl.log"; then
  ok "gRPC admin method denied to the read token (PermissionDenied)"
else
  not_ok "gRPC admin method NOT denied to the read token (set-poll-frequency read:$s_read)"
fi
if [ "$s_admin" = ok ]; then
  ok "gRPC admin method accepts the admin token"
else
  not_ok "gRPC admin method rejected the admin token (set-poll-frequency admin:$s_admin)"
fi
victim_still_reporting_tap

# The split is confirmed when every line held; a future regression to a
# shared credential flips the 403 to 200 and fails CI.
if grep -q "BROKEN" "$RT_EVIDENCE/grid.txt"; then
  finish rt-2-4 NA
else
  finish rt-2-4 CONFIRMED
fi
