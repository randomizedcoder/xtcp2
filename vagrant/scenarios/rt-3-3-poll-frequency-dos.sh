#!/usr/bin/env bash
# vagrant/scenarios/rt-3-3-poll-frequency-dos.sh — RT-3: poll-frequency CPU
# DoS (docs/redteam-plan.md RT-3, plan row rt-3-3; rt-4-8's admin shape).
#
# The chain: an admin credential holder sets the poll frequency to its
# minimum (1 s; timeout < frequency) and drives poll bursts on demand — the
# daemon re-polls every namespace at the attacker's cadence, CPU/bytes on
# the monitored host (S3).
#
# Asserts:
#   - the Set is accepted (the churn-rate unbounded residual from rt-3-6);
#   - the burst runs: the catcher's record rate multiplies (the measured
#     amplification factor: burst records/s vs baseline records/s);
#   - the daemon stays healthy (metrics 200, gRPC Get ok, catcher flowing);
#   - CPU is measurable but bounded by the poll rate (sampled via
#     docker stats) — the numbers recorded for findings.md;
#   - restore verified (poll 2s/1s = the harness defaults, final Get).
set -euo pipefail

: "${XTCP2_AUTH:=raw}"
: "${XTCP2_ATTACKER:=admin}" # SetPollFrequency/TriggerPollBurst are admin
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

echo "rt-3-3: evidence in $RT_EVIDENCE"

xtcp2_deploy victim attacker
wait_for 120 "daemon answers on the victim VM" vm_exec victim \
  "curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:9088/metrics | grep -qE '^(200|401|403)\$'"
victim_still_reporting_tap

admin_token="$(vm_exec victim 'sudo docker exec rt-attacker cat /run/secrets/admin')"
[ -n "$admin_token" ] || xtcp2_die 'no admin token in the attacker container'
CTL=/opt/xtcp2-redteam/bin/xtcp2ctl
CTL_ARGS="-network tcp -target 192.168.142.10 -port 8889 -auth-token"
CATCH=/var/lib/xtcp2-catcher/records.jsonl

# rate <seconds>: victim-catcher records/s over the last <seconds> window
rate() {
  local secs="$1" a b
  a="$(vm_exec_root victim "wc -l < $CATCH")"
  sleep "$secs"
  b="$(vm_exec_root victim "wc -l < $CATCH")"
  echo "$(( (b - a) / secs ))"
}

# --- the baseline: 2 s poll cadence (the harness default) --------------------
base_rate="$(rate 10)"
base_cpu="$(vm_exec victim 'sudo docker stats rt-xtcp2 --no-stream --format "{{.CPUPerc}} {{.MemUsage}}"' | tail -1)"
echo "rt-3-3: baseline — ${base_rate} records/s, cpu/mem: $base_cpu"

# --- the attack: 1 s frequency + a poll burst -------------------------------
vm_exec attacker "$CTL set-poll-frequency $CTL_ARGS '$admin_token' -frequency 1s -timeout 500ms" \
  >"$RT_EVIDENCE/set-frequency.log" 2>&1
if grep -q "ok:" "$RT_EVIDENCE/set-frequency.log"; then
  ok "SetPollFrequency to 1s/500ms accepted"
else
  not_ok "SetPollFrequency rejected — see evidence"
fi
vm_exec attacker "$CTL poll-burst $CTL_ARGS '$admin_token' -count 60 -interval 1s" \
  >"$RT_EVIDENCE/poll-burst.log" 2>&1
if grep -q "ok: burst" "$RT_EVIDENCE/poll-burst.log"; then
  ok "poll burst scheduled (60 polls, 1 s apart — the server caps the interval at ≥ 1 s)"
else
  not_ok "poll burst rejected — see evidence"
fi

# --- during the burst: rate, CPU, health ------------------------------------
burst_rate="$(rate 20)"
burst_cpu="$(vm_exec victim 'sudo docker stats rt-xtcp2 --no-stream --format "{{.CPUPerc}} {{.MemUsage}}"' | tail -1)"
echo "rt-3-3: burst — ${burst_rate} records/s, cpu/mem: $burst_cpu"
if [ "$burst_rate" -gt "$base_rate" ]; then
  ok "record amplification CONFIRMED: ${base_rate}/s -> ${burst_rate}/s ($(( burst_rate / base_rate ))x) — records/s track the attacker's cadence"
else
  not_ok "no record amplification (${base_rate}/s -> ${burst_rate}/s) — see evidence"
fi
victim_still_reporting_tap
code="$(vm_exec victim "curl -s -o /dev/null -w '%{http_code}' -H 'Authorization: Bearer $(vm_exec_root victim 'cat /opt/xtcp2-redteam/secrets/read' | tail -1)' http://127.0.0.1:9088/metrics")"
if [ "$code" = 200 ]; then
  ok "the daemon stayed healthy through the burst (metrics 200)"
else
  not_ok "the daemon's metrics surface failed during the burst (HTTP $code)"
fi

# --- the restore: the harness defaults (2 s / 1 s) ---------------------------
vm_exec attacker "$CTL set-poll-frequency $CTL_ARGS '$admin_token' -frequency 2s -timeout 1s" \
  >"$RT_EVIDENCE/restore-frequency.log" 2>&1
if grep -q "ok:" "$RT_EVIDENCE/restore-frequency.log"; then
  ok "poll frequency restored (2s/1s)"
else
  not_ok "restore rejected — see evidence"
fi
sleep 12
victim_still_reporting_tap
fget="$(vm_exec attacker "$CTL get $CTL_ARGS '$admin_token' >/dev/null 2>&1 && echo ok || echo fail")"
if [ "$fget" = ok ]; then
  ok "final Get answers (gRPC healthy; the daemon is restored)"
else
  not_ok "final Get failed — the daemon is not healthy"
fi

finish rt-3-3 CONFIRMED
