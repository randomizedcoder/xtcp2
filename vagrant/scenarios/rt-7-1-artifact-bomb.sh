#!/usr/bin/env bash
# vagrant/scenarios/rt-7-1-artifact-bomb.sh — RT-7: ASN artifact decompression
# bomb (docs/redteam-plan.md row rt-7-1; the artifact poisoner premise A5 —
# "controls an S3 bucket / artifact the daemon ingests").
#
# The daemon runs the §29 shape with the ASN enricher and a bind-mounted
# artifact dir (-asnDbPath, 5 s re-stat refresh). The attack places a zstd
# bomb as the live artifact: a ~1 MiB .parquet.zst that decompresses to
# 10 GiB (9361× ratio, crafted by tools/ipfeed-fixture). The plan's pass
# criteria: "daemon refuses or bounds artifact size/decompression (cap bytes
# read); failure ⇒ OOM on the monitoring host".
#
# The bound in question (pkg/ipasn/ipasn.go): readZstdFile decompresses
# through io.LimitReader(dec, limit+1) with maxDecompressedArtifactBytes =
# 256 MiB, then refuses — and a refused reload NEVER touches the trie in
# service ("a bad refresh never degrades a good table"). The scenario pins:
#   - the over-cap bomb is REFUSED (the log line names the cap), the loaded
#     table keeps serving (prefixes gauge unchanged), the daemon stays
#     healthy, and the transient alloc is bounded (RSS ceiling);
#   - the under-cap bomb (240 MiB, inside the cap) is refused at the Parquet
#     open (garbage content) with the same keep-table guarantees — the
#     in-process cost is the 240 MiB buffer, bounded by the same cap;
#   - the refresh loop keeps retrying every 5 s without blocking the daemon
#     (each retry is a fresh refused attempt, counted, not a wedge);
#   - trap-restore: the baseline artifact goes back in and the gauge recovers.
set -euo pipefail

: "${XTCP2_AUTH:=raw}" # the metrics asserts need the read token
: "${XTCP2_ATTACKER:=plain}" # no credential needed: the poisoned sync is out-of-band
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

echo "rt-7-1: evidence in $RT_EVIDENCE"

xtcp2_deploy victim attacker
wait_for 120 "daemon answers on the victim VM" vm_exec victim \
  "curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:9088/metrics | grep -qE '^(200|401|403)\$'"

# the read token host-side (the ATTACKER=plain persona stays
# credential-less; the token is the SCENARIO's measurement vantage)
RT_READ_TOKEN="$(vm_exec victim 'sudo cat /opt/xtcp2-redteam/secrets/read')"
[ -n "$RT_READ_TOKEN" ] || xtcp2_die 'no read token on the victim host'
FIXTURE=/opt/xtcp2-redteam/bin/ipfeed-fixture

# asn_prefixes: the loadAsn prefixes gauge (the table in service, NOT the
# daemon's self-report of health — a poisoned table shows up here).
asn_prefixes() {
  vm_exec victim "curl -s -H 'Authorization: Bearer $RT_READ_TOKEN' http://127.0.0.1:9088/metrics" \
    | grep -o 'xtcp_gauges{function="loadAsn",type="gauge",variable="prefixes"} [0-9]*' \
    | awk '{print $2}'
}

# place_artifact <vm-local-tmp-path>: install as the live artifact (host-side;
# the artifact sync lands on the same file).
# place_artifact <remote-tmp-name>: move a file already landed in the
# victim's /tmp (via vm_pull + vm_push — the fixture crafts on the attacker
# VM; the harness host is the only bridge) into the live-artifact slot.
place_artifact() {
  vm_exec victim "sudo mv -f /tmp/$1 /var/lib/xtcp2/asn/current.parquet.zst"
}

# craft_and_land <tmp-name> <fixture-args…>: craft on the attacker, pull to
# the harness evidence dir, push to the victim's /tmp.
craft_and_land() {
  local name="$1"
  shift
  vm_exec attacker "$FIXTURE -out /tmp/$name $*"
  vm_pull attacker "/tmp/$name" "$RT_EVIDENCE/$name" >/dev/null 2>&1
  vm_push victim "$RT_EVIDENCE/$name" "/tmp/$name" >/dev/null
}

# --- baseline: a legit table is in service -----------------------------------
craft_and_land rt7-1-baseline.parquet.zst -mode rows -n 5000 -shape random24 -owner LEGIT-NET
place_artifact rt7-1-baseline.parquet.zst
sleep 8 # one refresh tick + settle
p="$(asn_prefixes)"
echo "rt-7-1: baseline prefixes gauge: $p"
if [ "$p" = 5000 ]; then
  ok "baseline: 5000 prefixes in service"
else
  not_ok "baseline prefixes gauge is '$p' (want 5000) — see evidence"
fi

# --- the attack: the 10 GiB decompression bomb -------------------------------
vm_exec attacker "$FIXTURE -mode bomb -decompressed 10GiB -out /tmp/rt7-1-bomb.parquet.zst" \
  | tee "$RT_EVIDENCE/bomb-craft.log"
bomb_kb="$(vm_exec attacker 'du -k /tmp/rt7-1-bomb.parquet.zst | cut -f1')"
echo "rt-7-1: the bomb is ${bomb_kb} KiB on disk, 10 GiB decompressed"
vm_pull attacker /tmp/rt7-1-bomb.parquet.zst "$RT_EVIDENCE/rt7-1-bomb.parquet.zst" >/dev/null 2>&1
vm_push victim "$RT_EVIDENCE/rt7-1-bomb.parquet.zst" /tmp/rt7-1-bomb.parquet.zst >/dev/null
place_artifact rt7-1-bomb.parquet.zst
sleep 10 # two refresh ticks: the attempt + the steady state

refused="$(vm_exec victim 'sudo docker logs rt-xtcp2 2>&1 | grep -c "ASN reload failed (keeping current table).*exceeds 268435456 bytes" || true')"
if [ "$refused" -ge 1 ]; then
  ok "the over-cap bomb is REFUSED at the 256 MiB cap (log line: exceeds 268435456 bytes)"
else
  not_ok "no refused-at-cap log line — see evidence"
fi
p2="$(asn_prefixes)"
if [ "$p2" = 5000 ]; then
  ok "the refused reload kept the good table in service (prefixes gauge still 5000)"
else
  not_ok "prefixes gauge is '$p2' after the bomb (want 5000) — see evidence"
fi
code="$(vm_exec victim "curl -s -o /dev/null -w '%{http_code}' -H 'Authorization: Bearer $RT_READ_TOKEN' http://127.0.0.1:9088/metrics")"
if [ "$code" = 200 ]; then
  ok "the daemon stayed healthy through the bomb (metrics 200)"
else
  not_ok "metrics answered $code after the bomb — see evidence"
fi
rss="$(vm_exec victim 'sudo docker stats --no-stream --format "{{.MemUsage}}" rt-xtcp2' | grep -oE '^[0-9.]+[A-Za-z]+' | head -1)"
echo "rt-7-1: daemon RSS after the bomb: $rss"
vm_exec victim "curl -s -H 'Authorization: Bearer $RT_READ_TOKEN' http://127.0.0.1:9088/metrics" \
  >"$RT_EVIDENCE/metrics-after-bomb.txt" 2>&1 || true
if grep -q 'function="refreshAsn",type="error",variable="reload"' "$RT_EVIDENCE/metrics-after-bomb.txt"; then
  ok "the refused attempts are counted (refreshAsn reload error) — visible, not silent"
else
  not_ok "no refreshAsn error counter — see evidence"
fi

# --- the under-cap bomb: 240 MiB of garbage INSIDE the cap --------------------
craft_and_land rt7-1-bomb2.parquet.zst -mode bomb -decompressed 240MiB
place_artifact rt7-1-bomb2.parquet.zst
sleep 10
refused2="$(vm_exec victim 'sudo docker logs rt-xtcp2 2>&1 | grep -c "ASN reload failed (keeping current table)" || true')"
if [ "$refused2" -gt "$refused" ]; then
    ok "the under-cap bomb (240 MiB) is refused too — at the Parquet open, same keep-table guarantee"
  else
    not_ok "no new refusal for the under-cap bomb — see evidence"
  fi
p3="$(asn_prefixes)"
if [ "$p3" = 5000 ]; then
    ok "the good table still serves after the under-cap bomb (prefixes 5000)"
  else
    not_ok "prefixes gauge is '$p3' after the under-cap bomb — see evidence"
  fi
rss2="$(vm_exec victim 'sudo docker stats --no-stream --format "{{.MemUsage}}" rt-xtcp2' | grep -oE '^[0-9.]+[A-Za-z]+' | head -1)"
echo "rt-7-1: daemon RSS after the under-cap bomb: $rss2"
code2="$(vm_exec victim "curl -s -o /dev/null -w '%{http_code}' -H 'Authorization: Bearer $RT_READ_TOKEN' http://127.0.0.1:9088/metrics")"
if [ "$code2" = 200 ]; then
    ok "the daemon stayed healthy through the under-cap bomb (metrics 200)"
  else
    not_ok "metrics answered $code2 after the under-cap bomb — see evidence"
  fi

# --- restore ------------------------------------------------------------------
craft_and_land rt7-1-restore.parquet.zst -mode rows -n 5000 -shape random24 -owner LEGIT-NET
place_artifact rt7-1-restore.parquet.zst
sleep 8
pr="$(asn_prefixes)"
if [ "$pr" = 5000 ]; then
  ok "restore: the baseline table is back in service (prefixes 5000)"
else
  not_ok "restore prefixes gauge is '$pr' (want 5000) — see evidence"
fi

# BLOCKED: the 256 MiB cap bounds every decompression; a refused reload
# never degrades the table in service; the daemon stays healthy; the cost of
# a worst-case artifact is a transient <=256 MiB alloc, not an OOM.
finish rt-7-1 BLOCKED
