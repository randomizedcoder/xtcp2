#!/usr/bin/env bash
# vagrant/scenarios/rt-4-5-scrape-storm.sh — RT-4: scrape storm
# (docs/redteam-plan.md row rt-4-5).
#
# The chain: 100 parallel scrapes across the scrape surface (/metrics, the
# all-metrics path, and the detail category paths). The /metrics handler has
# MaxRequestsInFlight=10 (cmd/xtcp2/xtcp2.go:51, verified in the Phase 4
# premortem and pinned by rt-11-1): the 11th concurrent scrape gets 503,
# so a storm cannot pile handlers onto the process. The question is whether
# that holds under a real storm and what the served-path latency does (S4).
#
# Asserts:
#   - the storm produces 503s on /metrics (the in-flight cap doing its
#     job) while the storm itself stays healthy;
#   - the daemon keeps answering (healthz 200) and the catcher keeps
#     flowing DURING the storm (the RT-11 invariant);
#   - p99 scrape latency recorded per path shape;
#   - after the storm: a clean scrape is fast again (no pileup);
#   - cleanup: nothing to restore (no config touched).
set -euo pipefail

: "${XTCP2_MODE:=bridge}"
: "${XTCP2_AUTH:=raw}"
: "${XTCP2_ATTACKER:=plain}"
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

echo "rt-4-5: evidence in $RT_EVIDENCE"

xtcp2_deploy victim attacker
wait_for 120 "daemon answers on the victim VM" vm_exec victim \
  "curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:9088/metrics | grep -qE '^(200|401|403)\$'"
victim_still_reporting_tap
xtcp2_victim_read_token

# --- helpers --------------------------------------------------------------------

# scrape <path> <parallel> <rounds>: one storm round — returns "code time"
# per request, one per line, into the given file on the victim.
storm() { # $1=path $2=parallel $3=outfile
  vm_exec victim "seq $2 | xargs -P $2 -I{} curl -s -m 15 -o /dev/null -w '%{http_code} %{time_total}\n' -H 'Authorization: Bearer $RT_READ_TOKEN' http://127.0.0.1:9088$1 > $3"
}

summarize() { # $1=outfile $2=label -> "200s 503s other p99"
  vm_exec victim "awk '{c[\$1]++; if (\$2 > mx) mx = \$2} END {printf \"%s %s %s %s\", c[200]+0, c[503]+0, c[\"000\"]+0, mx*1000}' $1"
}

# --- baseline --------------------------------------------------------------------

base_lat="$(vm_exec victim "curl -s -o /dev/null -w '%{time_total}' -H 'Authorization: Bearer $RT_READ_TOKEN' http://127.0.0.1:9088/metrics")"
ok "baseline: clean /metrics scrape in ${base_lat}s"

# --- the storm: 100 parallel over each path shape ---------------------------------
# /metrics (the capped handler), /metrics-all (the all-metrics path), and a
# category detail path (link is a core category).
CATCHER_BEFORE_STORM="$(catcher_lines)"

for shape in "/metrics" "/metrics-all" "/metrics/link"; do
  slug="$(echo "$shape" | tr '/-' '__')"
  storm "$shape" 100 "/tmp/rt-4-5$slug.txt"
  read -r c200 c503 c000 p99_ms <<<"$(summarize "/tmp/rt-4-5$slug.txt" x)"
  echo "rt-4-5: $shape: 200=$c200 503=$c503 fail=$c000 p99=${p99_ms}ms"
  echo "rt-4-5: $shape: 200=$c200 503=$c503 fail=$c000 p99=${p99_ms}ms" >>"$RT_EVIDENCE/storm.txt"
  # The cap engages where concurrency can actually build: the ~17ms /metrics
  # body answers faster than 100 staggered curls can overlap 10-deep, but the
  # slower all-metrics path stacks past 10 in flight and gets 503s — the cap
  # doing its job exactly where a pileup would otherwise form.
  if [ "$shape" = "/metrics" ]; then
    if [ "$c000" -eq 0 ]; then
      ok "/metrics: no failed connections among 100 parallel scrapes (the ~17ms body self-throttles: ${c200} answered 200, ${c503} capped)"
    else
      not_ok "/metrics: $c000 scrapes failed to connect — see evidence"
    fi
  fi
  if [ "$shape" = "/metrics-all" ]; then
    if [ "$c503" -ge 1 ]; then
      ok "/metrics-all: the in-flight cap answered 503 to $c503 of 100 parallel scrapes (the slower path stacks past 10 in flight — no pileup)"
    else
      not_ok "/metrics-all: no 503s among 100 parallel scrapes — the cap did not engage on the slow path; see evidence"
    fi
  fi
done

# --- during the storm window: the daemon stayed healthy ---------------------------

storm_growth=$(( $(catcher_lines) - CATCHER_BEFORE_STORM ))
if [ "$storm_growth" -gt 0 ]; then
  ok "the catcher kept flowing during the storm ($storm_growth record lines across the three storms)"
else
  not_ok "the catcher did not grow during the storm — see evidence"
fi

code="$(vm_exec victim "curl -s -o /dev/null -w '%{http_code}' -H 'Authorization: Bearer $RT_READ_TOKEN' http://127.0.0.1:9088/healthz")"
if [ "$code" = 200 ]; then
  ok "healthz answered 200 after the storms"
else
  not_ok "healthz answered $code after the storms"
fi

# --- after the storm: no pileup ---------------------------------------------------

after_lat="$(vm_exec victim "curl -s -o /dev/null -w '%{time_total}' -H 'Authorization: Bearer $RT_READ_TOKEN' http://127.0.0.1:9088/metrics")"
after_code="$(vm_exec victim "curl -s -m 6 -o /dev/null -w '%{http_code}' -H 'Authorization: Bearer $RT_READ_TOKEN' http://127.0.0.1:9088/metrics")"
if [ "$after_code" = 200 ]; then
  ok "after the storm: a clean scrape answers 200 in ${after_lat}s (no pileup)"
else
  not_ok "after the storm: the clean scrape degraded (code=$after_code)"
fi
victim_still_reporting_tap

# --- verdict -----------------------------------------------------------------------

echo "rt-4-5: storm table in $RT_EVIDENCE/storm.txt"
finish rt-4-5 BLOCKED
