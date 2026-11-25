#!/usr/bin/env bash
# vagrant/scenarios/rt-3-4-envelope-extremes.sh — RT-3: envelope/memory
# manipulation (docs/redteam-plan.md RT-3, plan row rt-3-4).
#
# The chain: SetEnvelopeFlush has no upper bound on its caps (uint32, 0 =
# default 768 KiB / 10000 rows) — an admin credential holder can set both
# to 4294967295 (4 GiB / 4294967295 rows). AMENDED 2026-11-23 (verified on
# the VM): the caps are a flush CEILING, not a growth driver — the envelope
# flushes on the poll cycle, so even at the uint32 max the daemon's RSS
# stays within noise of the baseline (no unbounded growth: the plan row's
# expected outcome HOLDS). The residual: the Set itself is unvalidated at
# the top end (a shape question, not a behavior question — the pin keeps
# the growth assert so a flush regression flips it).
set -euo pipefail

: "${XTCP2_AUTH:=raw}"
: "${XTCP2_ATTACKER:=admin}" # SetEnvelopeFlush is an admin method
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

echo "rt-3-4: evidence in $RT_EVIDENCE"

xtcp2_deploy victim attacker
wait_for 120 "daemon answers on the victim VM" vm_exec victim \
  "curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:9088/metrics | grep -qE '^(200|401|403)\$'"
victim_still_reporting_tap

admin_token="$(vm_exec victim 'sudo docker exec rt-attacker cat /run/secrets/admin')"
[ -n "$admin_token" ] || xtcp2_die 'no admin token in the attacker container'
CTL=/opt/xtcp2-redteam/bin/xtcp2ctl
CTL_ARGS="-network tcp -target 192.168.142.10 -port 8889 -auth-token"

# rss_mib: the daemon's resident set in MiB (docker stats)
rss_mib() {
  vm_exec victim 'sudo docker stats rt-xtcp2 --no-stream --format "{{.MemUsage}}"' \
    | tail -1 | grep -oE '^[0-9.]+'
}

# --- the baseline (default caps, 2 s polls) ----------------------------------
rss0="$(rss_mib)"
echo "rt-3-4: baseline RSS: ${rss0}MiB"

# --- the extremes: max uint32 caps + the fastest legal polls -----------------
vm_exec attacker "$CTL set-envelope-flush $CTL_ARGS '$admin_token' -threshold-bytes 4294967295 -threshold-rows 4294967295" \
  >"$RT_EVIDENCE/set-envelope.log" 2>&1
if grep -q "ok:" "$RT_EVIDENCE/set-envelope.log"; then
  ok "SetEnvelopeFlush to the uint32 max accepted (4 GiB / 4294967295 rows)"
else
  not_ok "SetEnvelopeFlush rejected — see evidence"
fi
vm_exec attacker "$CTL set-poll-frequency $CTL_ARGS '$admin_token' -frequency 1s -timeout 500ms" \
  >"$RT_EVIDENCE/set-frequency.log" 2>&1
if grep -q "ok:" "$RT_EVIDENCE/set-frequency.log"; then
  ok "poll frequency to 1s accepted (the growth driver)"
else
  not_ok "set-poll-frequency rejected — see evidence"
fi

# --- the window: 40 s at the extreme, sampled RSS twice ----------------------
sleep 20
rss1="$(rss_mib)"
sleep 20
rss2="$(rss_mib)"
# The plan row's expected outcome: NO unbounded growth under the extremes —
# the envelope flushes on the poll cycle (the caps are a ceiling that
# triggers a flush when crossed, not a growth driver). What distinguishes
# bounded from unbounded is the RATE, not any single delta: a legitimate
# run takes an initial step (heap growth to the working-set, one GC cycle)
# and then flattens, while runaway growth is linear in both windows. So the
# assert is the SECOND window's slope (rss2-rss1), which must sit inside
# noise (~1 MiB); a generous band (4 MiB) absorbs GC jitter. Measured once
# at +4.35 MiB cumulative on the FIRST window alone (20261125T024305) —
# flat after, i.e. the step, not the slope.
step1="$(awk -v a="$rss0" -v b="$rss1" 'BEGIN{printf "%.2f", b - a}')"
step2="$(awk -v a="$rss1" -v b="$rss2" 'BEGIN{printf "%.2f", b - a}')"
echo "rt-3-4: RSS under the extremes: ${rss0} -> ${rss1} -> ${rss2}MiB (windows: +${step1} then +${step2}MiB)"
if awk -v b="$step2" 'BEGIN{exit !(b < 4)}'; then
  ok "no sustained growth: the second window's slope is +${step2}MiB (the envelope flushes on the poll cycle — the caps are a flush ceiling, not a growth driver)"
else
  not_ok "RSS kept climbing: +${step2}MiB in the second window — unbounded growth under the extremes (see evidence)"
fi

# no instant wedge: the daemon still answers
code="$(vm_exec victim "curl -s -o /dev/null -w '%{http_code}' -H 'Authorization: Bearer $(vm_exec_root victim 'cat /opt/xtcp2-redteam/secrets/read' | tail -1)' http://127.0.0.1:9088/metrics")"
if [ "$code" = 200 ]; then
  ok "no instant OOM/wedge: the daemon answers through the window (HTTP 200)"
else
  not_ok "the daemon failed under the extremes (HTTP $code) — see evidence"
fi
victim_still_reporting_tap

# --- the restore: the daemon defaults (786432 bytes / 10000 rows, 2 s / 1 s) --
vm_exec attacker "$CTL set-envelope-flush $CTL_ARGS '$admin_token' -threshold-bytes 786432 -threshold-rows 10000" \
  >"$RT_EVIDENCE/restore-envelope.log" 2>&1
if grep -q "ok:" "$RT_EVIDENCE/restore-envelope.log"; then
  ok "envelope caps restored to the defaults (786432/10000)"
else
  not_ok "envelope restore rejected — see evidence"
fi
vm_exec attacker "$CTL set-poll-frequency $CTL_ARGS '$admin_token' -frequency 2s -timeout 1s" \
  >"$RT_EVIDENCE/restore-frequency.log" 2>&1
if grep -q "ok:" "$RT_EVIDENCE/restore-frequency.log"; then
  ok "poll frequency restored (2s/1s)"
else
  not_ok "frequency restore rejected — see evidence"
fi
sleep 12
victim_still_reporting_tap
fget="$(vm_exec attacker "$CTL get $CTL_ARGS '$admin_token' >/dev/null 2>&1 && echo ok || echo fail")"
if [ "$fget" = ok ]; then
  ok "final Get answers (the daemon is restored and healthy)"
else
  not_ok "final Get failed — the daemon is not healthy"
fi

finish rt-3-4 BLOCKED
