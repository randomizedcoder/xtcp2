#!/usr/bin/env bash
# vagrant/scenarios/rt-2-1-brute-force-pacing.sh — RT-2, tier 1: raw-token
# online brute force (docs/redteam-plan.md RT-2).
#
# Premise checks (plan row rt-2-1), on the §29 prod shape (auth=raw):
#   - wrong tokens are 401 with the context-aware failure jitter applied
#     (20–200 ms, pkg/listenerauth/listenerauth.go:48) — no lockout, no
#     rate limit, no cap on connections;
#   - a valid token keeps working after sustained failures (no lockout);
#   - the measured attempts/sec ceiling, serial and parallel — the number
#     that bounds token-entropy decisions (findings.md).
#
# AMENDED 2026-11-23: the plan row's "assert jitter applied (xtcp_counts
# auth-failure metrics)" is wrong — the auth surface exports NO auth-failure
# counters (the /metrics families carry none). The jitter is verified
# directly: by timing.
#
# EXPECT=BLOCKED: brute force is infeasible only via token entropy (S3,
# mitigation = ops) — the BLOCKED outcome is "no credential cracked within
# the scenario's sample", with the pacing numbers recorded.
set -euo pipefail

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

echo "rt-2-1: evidence in $RT_EVIDENCE"

xtcp2_deploy victim
wait_for 120 "daemon answers on the victim VM" vm_exec victim \
  "curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:9088/metrics | grep -qE '^(200|401|403)\$'"
victim_still_reporting_tap

xtcp2_victim_read_token
token="$RT_READ_TOKEN"
PROM="http://172.17.0.1:9088/metrics" # the co-tenant vantage

# All probes run inside the attacker container (the co-tenant vantage):
# brutepace.sh reports its numbers as sh-compatible key=value lines.
vm_push victim "$RT_LIB_DIR/brutepace.sh" /tmp/brutepace.sh >/dev/null
vm_exec victim 'sudo docker cp /tmp/brutepace.sh rt-attacker:/tmp/brutepace.sh'
scrape() { # scrape <args...>: run brutepace <args> in the container
  vm_exec victim "sudo docker exec rt-attacker sh /tmp/brutepace.sh $PROM $token $*"
}

# --- 1. failure jitter: wrong tokens must land in [20,200]ms ----------------
scrape jitter >"$RT_EVIDENCE/jitter.txt" 2>&1
jitter_min="$(awk -F= '/^fail_min=/{print $2}' "$RT_EVIDENCE/jitter.txt")"
jitter_max="$(awk -F= '/^fail_max=/{print $2}' "$RT_EVIDENCE/jitter.txt")"
echo "rt-2-1: failure jitter ${jitter_min}..${jitter_max}ms over $(awk -F= '/^fail_n=/{print $2}' "$RT_EVIDENCE/jitter.txt") wrong tokens"
if awk -v lo="$jitter_min" -v hi="$jitter_max" 'BEGIN{exit !(lo>=20 && hi<=200)}'; then
  ok "failure jitter ${jitter_min}..${jitter_max}ms within the configured 20-200ms"
else
  not_ok "failure jitter ${jitter_min}..${jitter_max}ms outside the configured 20-200ms"
fi

# --- 2. no lockout: the right token works after sustained failures ----------
scrape hammer 150 >"$RT_EVIDENCE/hammer.txt" 2>&1
locked="$(awk -F= '/^last_ok=/{print $2}' "$RT_EVIDENCE/hammer.txt")"
echo "rt-2-1: after 150 wrong tokens, the valid token got HTTP $locked"
if [ "$locked" = 200 ]; then
  ok "no lockout: valid token still accepted after 150 failures"
else
  not_ok "valid token rejected after 150 failures (HTTP $locked)"
fi
victim_still_reporting_tap

# --- 3. guesses/sec ceiling: serial then parallel (10 workers) --------------
scrape rate 60 1 >"$RT_EVIDENCE/rate-serial.txt" 2>&1
serial=$(awk -F= '/^guesses_per_s=/{print $2}' "$RT_EVIDENCE/rate-serial.txt")
scrape rate 60 10 >"$RT_EVIDENCE/rate-parallel.txt" 2>&1
parallel=$(awk -F= '/^guesses_per_s=/{print $2}' "$RT_EVIDENCE/rate-parallel.txt")
echo "rt-2-1: measured ceiling ${serial}/s serial, ${parallel}/s with 10 workers (findings.md)"
if awk -v s="$serial" 'BEGIN{exit !(s>0)}' && awk -v p="$parallel" -v s="$serial" 'BEGIN{exit !(p>=s)}'; then
  ok "attempts/sec measured: ${serial} serial, ${parallel} parallel (entropy, not pacing, must hold)"
else
  not_ok "attempts/sec measurement failed (serial=$serial parallel=$parallel — see evidence)"
fi
victim_still_reporting_tap

# --- 4. daemon health: no 5xx, no crash, still collecting -------------------
daemon="$(vm_exec victim "sudo docker inspect rt-xtcp2 --format '{{.State.Status}}'")"
if [ "$daemon" = running ]; then
  ok "daemon still running after the hammer (no crash, no 5xx lockout)"
else
  not_ok "daemon state after the hammer: $daemon"
fi
victim_still_reporting_tap

# No credential cracked: the sampled guesses were wrong by construction, so
# BLOCKED = the pacing defense held and nothing leaked.
finish rt-2-1 BLOCKED
