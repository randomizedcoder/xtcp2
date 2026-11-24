#!/usr/bin/env bash
# vagrant/scenarios/rt-7-2-bootstrap-swap.sh — RT-7: bootstrap artifact swap
# (docs/redteam-plan.md row rt-7-2).
#
# The deployment shape the plan calls out: a FILE-BASED deploy trusts the
# mount. The daemon runs with -ipmetaBootstrapPath on the bind-mounted
# artifact dir; the artifact poisoner (premise A5) replaces the same-name
# bootstrap with different content, and the operator restarts the daemon.
# The swap is CONFIRMED by design:
#   - the daemon loads the swapped file without complaint — there is no
#     integrity check at load (a Nix deploy pins the bootstrap at BUILD time
#     via nix/ipmeta-bootstrap-lock.json; a file bind-mount has nothing);
#   - the poison is SEMANTIC, not just structural: the crafted table covers
#     the docker bridge (172.17.0.0/16), so every work-container record the
#     daemon reports now carries the attacker's ASN/network_owner — the
#     corrupted enrichment is visible in the telemetry itself (the catcher);
#   - trap-restore: the legit bootstrap goes back, the restart is repeated,
#     the gauges and the telemetry recover.
#
# The pass criteria (the plan's expected outcome): "lockfile-integrity
# behavior documented for non-Nix deploys; consider embedded hash check at
# load" — the finding is the documentation + the follow-up (an embedded
# hash check at load is a product finding, filed separately).
set -euo pipefail

: "${XTCP2_AUTH:=raw}" # the metrics asserts need the read token
: "${XTCP2_ATTACKER:=plain}" # no credential needed: the swap is the sync path
: "${XTCP2_ENRICH:=asn}"
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

echo "rt-7-2: evidence in $RT_EVIDENCE"

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
    | awk '{print $2}' | grep . || echo 0
}

# steady REMOTE traffic for the enrichment: the ASN enricher populates only
# REMOTE destinations (proto: "only for REMOTE" — on-link dests are
# LOCAL_SUBNET and skip the ASN lookup), so the loop dials a beyond-the-
# gateway address (1.1.1.1) from the victim host; the sockets' dest is
# remote, and the poison covers exactly it.
vm_exec victim 'pkill -f "[r]t7-2-remote-loop" >/dev/null 2>&1 || true'
vm_exec victim 'setsid bash -c "while true; do curl -s -m 2 -o /dev/null http://1.1.1.1/ || true; sleep 1; done" >/dev/null 2>&1 < /dev/null &'

# --- the file-based deploy: no live sync -------------------------------------
# This scenario's shape is the plan's "file-based deploys trust the mount":
# a baked bootstrap and NO live artifact (the operator did not configure a
# sync). Earlier scenarios' leftovers must not act as one (the live refresh
# path would overwrite the bootstrap table within one 5 s tick — measured:
# the poison lasted exactly one tick before the stale live artifact won).
vm_exec victim 'sudo rm -f /var/lib/xtcp2/asn/current.parquet.zst /var/lib/xtcp2/asn/previous.lookup.parquet.zst'

# --- the operator deploy: the legit bootstrap is in place ---------------------
craft_and_land() {
  local name="$1"
  shift
  vm_exec attacker "$FIXTURE -out /tmp/$name $*"
  vm_pull attacker "/tmp/$name" "$RT_EVIDENCE/$name" >/dev/null 2>&1
  vm_push victim "$RT_EVIDENCE/$name" "/tmp/$name" >/dev/null
}
craft_and_land rt7-2-bootstrap.parquet.zst -mode rows -n 5000 -shape dup32 -prefix 1.1.1.1/32 -owner LEGIT-NET
vm_exec victim "sudo mv -f /tmp/rt7-2-bootstrap.parquet.zst /var/lib/xtcp2/asn/bootstrap.parquet.zst"
# The operator restart: the daemon now boots FROM the bootstrap file.
vm_exec victim 'sudo docker restart rt-xtcp2' >/dev/null
wait_for 60 "the daemon is back after the restart" vm_exec victim \
  "curl -s -o /dev/null -w '%{http_code}' -H 'Authorization: Bearer $RT_READ_TOKEN' http://127.0.0.1:9088/metrics | grep -q 200"
boot_line="$(vm_exec victim 'sudo docker logs --since 5m rt-xtcp2 2>&1 | grep -cE "ASN enrichment enabled \\(source:bootstrap.*prefixes:5000\\)" || true')"
if [ "${boot_line:-0}" -ge 1 ]; then
  ok "the daemon booted FROM the bootstrap file (log: source:bootstrap prefixes:5000)"
else
  not_ok "no source:bootstrap boot line — see evidence"
fi
# PRODUCT FINDING (pinned): a bootstrap-only load publishes NO loadAsn gauges —
# loadInitialAsn calls ReloadArtifact directly, bypassing loadAsn's gauge
# publish. The table in service is invisible in metrics until a live load.
if [ "$(asn_prefixes)" = 0 ]; then
  ok "finding pinned: the bootstrap-only table is INVISIBLE in the loadAsn gauges (an observability gap, not a security one)"
else
  not_ok "prefixes gauge is '$(asn_prefixes)' — the gauges DID publish (finding refuted)"
fi
sleep 8
legit_records_ge_1() {
  [ "$(vm_exec victim 'sudo grep -c LEGIT-NET /var/lib/xtcp2-catcher/records.jsonl || true')" -ge 1 ]
}
wait_for 45 "the legit bootstrap's enrichment reaches the records" legit_records_ge_1
legit_rec="$(vm_exec victim 'sudo grep -c LEGIT-NET /var/lib/xtcp2-catcher/records.jsonl || true')"
echo "rt-7-2: LEGIT-NET record lines: $legit_rec"

# --- the attack: same name, different content, restart -----------------------
# The poison covers the remote dial target — every polled record for those
# sockets now carries the attacker's ASN/network_owner.
craft_and_land rt7-2-poison.parquet.zst -mode rows -n 37 -shape dup32 -prefix 1.1.1.1/32 -owner ATTACKER-NET
vm_exec victim "sudo mv -f /tmp/rt7-2-poison.parquet.zst /var/lib/xtcp2/asn/bootstrap.parquet.zst"
vm_exec victim 'sudo docker restart rt-xtcp2' >/dev/null
wait_for 60 "the daemon is back after the poisoned restart" vm_exec victim \
  "curl -s -o /dev/null -w '%{http_code}' -H 'Authorization: Bearer $RT_READ_TOKEN' http://127.0.0.1:9088/metrics | grep -q 200"

swap_line="$(vm_exec victim 'sudo docker logs --since 3m rt-xtcp2 2>&1 | grep -cE "ASN enrichment enabled \\(source:bootstrap.*prefixes:37\\)" || true')"
if [ "${swap_line:-0}" -ge 1 ]; then
  ok "the swapped bootstrap loaded WITHOUT complaint (log: source:bootstrap prefixes:37 — no integrity check at load)"
else
  not_ok "no source:bootstrap prefixes:37 boot line after the swap — see evidence"
fi
# and the poison STAYS in service (no live artifact to overwrite it now)
sleep 8
still="$(vm_exec victim 'sudo docker logs --since 30s rt-xtcp2 2>&1 | grep -c "ASN artifact reloaded" || true')"
if [ "${still:-0}" -eq 0 ]; then
  ok "the poisoned table STAYS in service (no live artifact overwrote it — the file-based shape)"
else
  not_ok "a live reload overwrote the poisoned table — see evidence"
fi
rejected="$(vm_exec victim 'sudo docker logs rt-xtcp2 2>&1 | grep -c "ASN bootstrap artifact rejected" || true')"
if [ "$rejected" -eq 0 ]; then
    ok "no rejection line for the swapped bootstrap — the loader cannot tell it from a legit artifact"
  else
    not_ok "the swap WAS rejected ($rejected line(s)) — the finding would not hold"
  fi

# --- the semantic corruption reaches the telemetry ----------------------------
# The work containers live on the docker bridge; with the poisoned table every
# record's ASN enrichment resolves to the attacker's owner string.
wait_for 30 "the catcher sees the poisoned enrichment" vm_exec victim \
  "sudo grep -c ATTACKER-NET /var/lib/xtcp2-catcher/records.jsonl | grep -qv ^0\$"
n="$(vm_exec victim 'sudo grep -c ATTACKER-NET /var/lib/xtcp2-catcher/records.jsonl || true')"
echo "rt-7-2: poisoned-enrichment record lines at the catcher: $n"
if [ "${n:-0}" -ge 1 ]; then
  ok "CONFIRMED: the corrupted enrichment is in the reported telemetry (networkOwner=ATTACKER-NET in $n record line(s))"
else
  not_ok "no poisoned enrichment reached the catcher — see evidence"
fi
# ...and the legit owner is gone from new records (the poison replaced the view).
vm_exec victim 'sudo tail -200 /var/lib/xtcp2-catcher/records.jsonl' >"$RT_EVIDENCE/catcher-tail.txt" 2>&1 || true
if ! grep -q "LEGIT-NET" "$RT_EVIDENCE/catcher-tail.txt"; then
  ok "the fresh records carry no legit enrichment — the attacker owns the semantics now"
else
  not_ok "LEGIT-NET still appears in fresh records — see evidence"
fi

# --- the Nix-deploy contrast (documentation of the pinned path) --------------
if [ -f "$XTCP2_REPO_ROOT/nix/ipmeta-bootstrap-lock.json" ]; then
  grep -m1 -o '"[a-f0-9]\{64\}"' "$XTCP2_REPO_ROOT/nix/ipmeta-bootstrap-lock.json" \
    >"$RT_EVIDENCE/nix-lock-sha256.txt" || true
  ok "documented: a Nix deploy pins the bootstrap by hash at build time (nix/ipmeta-bootstrap-lock.json, sha256 in evidence)"
else
  echo "rt-7-2: no nix/ipmeta-bootstrap-lock.json in the repo root (harness-side note only)" | tee -a "$RT_EVIDENCE/notes.txt"
fi

# --- restore ------------------------------------------------------------------
craft_and_land rt7-2-restore.parquet.zst -mode rows -n 5000 -shape dup32 -prefix 1.1.1.1/32 -owner LEGIT-NET
vm_exec victim "sudo mv -f /tmp/rt7-2-restore.parquet.zst /var/lib/xtcp2/asn/bootstrap.parquet.zst"
vm_exec victim 'sudo docker restart rt-xtcp2' >/dev/null
wait_for 60 "the daemon is back after the restore restart" vm_exec victim \
  "curl -s -o /dev/null -w '%{http_code}' -H 'Authorization: Bearer $RT_READ_TOKEN' http://127.0.0.1:9088/metrics | grep -q 200"
sleep 8
restored_rec="$(vm_exec victim 'sudo tail -300 /var/lib/xtcp2-catcher/records.jsonl | grep -c LEGIT-NET || true')"
if [ "${restored_rec:-0}" -ge 1 ]; then
  ok "restore: the legit enrichment is back in the fresh records"
else
  not_ok "no LEGIT-NET in the fresh records after the restore — see evidence"
fi
# teardown: the remote-traffic loop
vm_exec victim 'pkill -f "[r]t7-2-remote-loop" >/dev/null 2>&1 || true' >/dev/null 2>&1 || true
victim_still_reporting_tap

# CONFIRMED: the file-based deploy trusts the mount (by design; the Nix lock
# is the build-time answer). Pass criteria = the finding documented + the
# embedded-hash-check-at-load follow-up filed.
finish rt-7-2 CONFIRMED
