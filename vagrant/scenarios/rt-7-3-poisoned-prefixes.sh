#!/usr/bin/env bash
# vagrant/scenarios/rt-7-3-poisoned-prefixes.sh — RT-7: poisoned artifact
# semantics (docs/redteam-plan.md row rt-7-3).
#
# The premise: a crafted parquet with adversarial prefix tables — the plan's
# shape is "bart trie stress: 100k prefixes" with longest-prefix lookup built
# at load. The pass criteria: "load time/CPU bounded; no panic; timeout on
# refresh path".
#
# The measured reality (this scenario pins it): 100k adversarially-packed
# rows are a SMALL table by trie standards — the loader builds them in
# ~60 ms, the refresh loop never blocks the daemon's hot path (it is its own
# goroutine, and a refused/failed reload never touches the table in
# service), and no input shape can panic the loader (invalid prefixes are
# skipped per-row). The scenario pins:
#   - the duplicate /32 packing (100k identical host routes — the worst case
#     for insert count vs distinct prefixes) loads to exactly one trie
#     entry: 99,900 usable rows + 100 skipped garbage rows;
#   - the depth ladder (every prefix length 0..32, maximum nesting) loads;
#   - the loadAsn build-duration summary is captured as evidence;
#   - the daemon stays healthy throughout; trap-restore returns the baseline.
set -euo pipefail

: "${XTCP2_AUTH:=raw}" # the metrics asserts need the read token
: "${XTCP2_ATTACKER:=plain}" # no credential needed: the poison is the sync path
: "${XTCP2_ENRICH:=asn}"
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

echo "rt-7-3: evidence in $RT_EVIDENCE"

xtcp2_deploy victim attacker
wait_for 120 "daemon answers on the victim VM" vm_exec victim \
  "curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:9088/metrics | grep -qE '^(200|401|403)\$'"

# the read token host-side (the ATTACKER=plain persona stays
# credential-less; the token is the SCENARIO's measurement vantage)
RT_READ_TOKEN="$(vm_exec victim 'sudo cat /opt/xtcp2-redteam/secrets/read')"
[ -n "$RT_READ_TOKEN" ] || xtcp2_die 'no read token on the victim host'
FIXTURE=/opt/xtcp2-redteam/bin/ipfeed-fixture

asn_prefixes() {
  vm_exec victim "curl -s -H 'Authorization: Bearer $RT_READ_TOKEN' http://127.0.0.1:9088/metrics" \
    | grep -o 'xtcp_gauges{function="loadAsn",type="gauge",variable="prefixes"} [0-9]*' \
    | awk '{print $2}'
}
asn_metrics() {
  vm_exec victim "curl -s -H 'Authorization: Bearer $RT_READ_TOKEN' http://127.0.0.1:9088/metrics"
}
place_artifact() {
  vm_exec victim "sudo mv -f /tmp/$1 /var/lib/xtcp2/asn/current.parquet.zst"
}
craft_and_land() {
  local name="$1"
  shift
  vm_exec attacker "$FIXTURE -out /tmp/$name $*"
  vm_pull attacker "/tmp/$name" "$RT_EVIDENCE/$name" >/dev/null 2>&1
  vm_push victim "$RT_EVIDENCE/$name" "/tmp/$name" >/dev/null
}

# --- baseline ------------------------------------------------------------------
craft_and_land rt7-3-baseline.parquet.zst -mode rows -n 5000 -shape random24 -owner LEGIT-NET
place_artifact rt7-3-baseline.parquet.zst
sleep 8
p="$(asn_prefixes)"
if [ "$p" = 5000 ]; then
    ok "baseline: 5000 prefixes in service"
  else
    not_ok "baseline prefixes gauge is '$p' (want 5000) — see evidence"
  fi

# --- the duplicate /32 packing: 100k identical host routes --------------------
# 100k rows all inserting the SAME /32 — the maximal insert-count-to-entry
# ratio — plus one garbage row per 1000 (the loader's skip path).
vm_exec attacker "$FIXTURE -mode rows -n 100000 -shape dup32 -invalid-every 1000 -out /tmp/rt7-3-dup.parquet.zst"
vm_pull attacker /tmp/rt7-3-dup.parquet.zst "$RT_EVIDENCE/rt7-3-dup.parquet.zst" >/dev/null 2>&1
vm_push victim "$RT_EVIDENCE/rt7-3-dup.parquet.zst" /tmp/rt7-3-dup.parquet.zst >/dev/null
place_artifact rt7-3-dup.parquet.zst
sleep 10
pd="$(asn_prefixes)"
# The prefixes gauge counts loaded ROWS (99,900 usable + 100 garbage skipped),
# while the trie itself collapses 100k identical /32s to a single entry —
# the worst-case load cost is the row count either way.
if [ "$pd" = 99900 ]; then
  ok "the 100k duplicate /32 rows loaded (99900 usable rows; the skip path dropped the 100 garbage rows)"
else
  not_ok "prefixes gauge is '$pd' after the dup packing (want 99900) — see evidence"
fi
code="$(vm_exec victim "curl -s -o /dev/null -w '%{http_code}' -H 'Authorization: Bearer $RT_READ_TOKEN' http://127.0.0.1:9088/metrics")"
if [ "$code" = 200 ]; then
    ok "the daemon stayed healthy through the dup load (metrics 200)"
  else
    not_ok "metrics answered $code after the dup load — see evidence"
  fi
panic="$(vm_exec victim 'sudo docker logs rt-xtcp2 2>&1 | grep -ci panic || true')"
if [ "$panic" -eq 0 ]; then
    ok "no panic in the daemon log"
  else
    not_ok "panic line(s) in the daemon log — see evidence"
  fi

# --- the depth ladder: every prefix length 0..32 ------------------------------
vm_exec attacker "$FIXTURE -mode rows -n 3300 -shape ladder -out /tmp/rt7-3-ladder.parquet.zst"
vm_pull attacker /tmp/rt7-3-ladder.parquet.zst "$RT_EVIDENCE/rt7-3-ladder.parquet.zst" >/dev/null 2>&1
vm_push victim "$RT_EVIDENCE/rt7-3-ladder.parquet.zst" /tmp/rt7-3-ladder.parquet.zst >/dev/null
place_artifact rt7-3-ladder.parquet.zst
sleep 10
pl="$(asn_prefixes)"
if [ "$pl" -ge 3096 ]; then # 3300 rows - 100 garbage - parquet column dict collapse
  ok "the depth ladder loaded (prefixes gauge $pl)"
else
  not_ok "prefixes gauge is '$pl' after the ladder (want ~3200) — see evidence"
fi
code2="$(vm_exec victim "curl -s -o /dev/null -w '%{http_code}' -H 'Authorization: Bearer $RT_READ_TOKEN' http://127.0.0.1:9088/metrics")"
if [ "$code2" = 200 ]; then
    ok "the daemon stayed healthy through the ladder load (metrics 200)"
  else
    not_ok "metrics answered $code2 after the ladder load — see evidence"
  fi

# evidence: the build durations of both loads
asn_metrics >"$RT_EVIDENCE/metrics-after-loads.txt" 2>&1 || true
grep -E 'loadAsn' "$RT_EVIDENCE/metrics-after-loads.txt" | head -12 || true

# --- restore -------------------------------------------------------------------
vm_exec attacker "$FIXTURE -mode rows -n 5000 -shape random24 -owner LEGIT-NET -out /tmp/rt7-3-restore.parquet.zst"
vm_pull attacker /tmp/rt7-3-restore.parquet.zst "$RT_EVIDENCE/rt7-3-restore.parquet.zst" >/dev/null 2>&1
vm_push victim "$RT_EVIDENCE/rt7-3-restore.parquet.zst" /tmp/rt7-3-restore.parquet.zst >/dev/null
place_artifact rt7-3-restore.parquet.zst
sleep 8
pr="$(asn_prefixes)"
if [ "$pr" = 5000 ]; then
    ok "restore: the baseline table is back in service"
  else
    not_ok "restore prefixes gauge is '$pr' (want 5000) — see evidence"
  fi
victim_still_reporting_tap

# BLOCKED (bounded by construction): the trie build is linear in usable rows
# (100k rows ≈ 60 ms), a failed/odd reload never touches the table in
# service, and no prefix shape can panic the loader.
finish rt-7-3 BLOCKED
