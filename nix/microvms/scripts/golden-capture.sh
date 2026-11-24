# Golden-record capture (the s3parquet-golden flavor): keep xtcp2's second
# poll, check it, then emit it with a manifest between
# XTCP2_GOLDEN_DUMP_START/_END on the console for the host runner, which
# scrubs it. The raw object holds the guest's real addresses; it never
# leaves the runner's temporary directory.
#
# Why the second poll, and why no host TCP before it: golden-common.sh.
# xtcp2 polls every 30 s and flushes Parquet every 5 s, so poll 1 lands in
# its own object; poll 2 is the object that appears after it.
#
# Expects PROM_SOCKET, TOKEN_FILE, BUCKET, CONTAINERS, SOCKETS and FLAVOR from
# the wrapper, which prepends golden-common.sh (ERR_TAG=GOLDEN_ERR).
# Any unexpected exit reports itself, so the runner fails fast instead of
# waiting out its timeout.
trap 'echo "GOLDEN_ERR $0: line $LINENO exited $?"' ERR

OUT=/tmp/golden
# From boot: the workload (image load, containers) comes up before xtcp2.
FLUSH_SEC="${GOLDEN_FLUSH_SEC:-300}"
rm -rf "$OUT"
mkdir -p "$OUT"

echo "GOLDEN_START"
wait_second_poll "$FLUSH_SEC"

# The AF_INET sockets each container holds now, for the row-count check
# below. xtcp2 dumps AF_INET only, so the Go servers' dual-stack listeners
# (0.0.0.0 opens an AF_INET6 socket) and the connections they accept are
# not recorded; ss -4 leaves them out too.
for i in $(seq 1 "$CONTAINERS"); do
  pid=$(docker inspect --format '{{.State.Pid}}' "golden-$i")
  nsenter -t "$pid" -n ss -4 -Htan >"$OUT/ss-golden-$i"
done
caps=$(systemctl show xtcp2 -p CapabilityBoundingSet --value)

stop_and_fetch_second_poll "$OUT/raw.parquet"

P="read_parquet('$OUT/raw.parquet')"
rows=$(q "SELECT count(*) FROM $P")
for i in $(seq 1 "$CONTAINERS"); do
  c="golden-$i"
  n=$(q "SELECT count(*) FROM $P WHERE container_name = '$c'")
  [ "$n" -ge 1 ] || fail "$c: no rows"
  est=$(q "SELECT count(*) FROM $P WHERE container_name = '$c' AND inet_diag_msg_state = 1")
  [ "$est" -ge "$SOCKETS" ] || fail "$c: $est established rows, expected at least $SOCKETS"
  bad=$(q "SELECT count(*) FROM $P WHERE container_name = '$c' AND (container_runtime <> 'docker' OR NOT regexp_full_match(container_id, '[0-9a-f]{64}'))")
  [ "$bad" -eq 0 ] || fail "$c: $bad rows without docker runtime and a 64-hex id"
  want=$(grep -c . "$OUT/ss-golden-$i" || true)
  [ "$n" -eq "$want" ] || fail "$c: $n rows, ss -4 lists $want sockets"
done
ns=$(q "SELECT count(*) FROM $P WHERE netns = 'golden-ns'")
[ "$ns" -ge 1 ] || fail "golden-ns: no rows"
host=$(q "SELECT count(*) FROM $P WHERE container_name = '' AND netns <> 'golden-ns'")
[ "$host" -ge 1 ] || fail "host namespace: no rows"
version=$(q "SELECT any_value(daemon_version) FROM $P")

uname -r >"$OUT/uname"
jq -n \
  --arg flavor "$FLAVOR" --arg version "$version" --arg kernel "$(uname -r)" \
  --arg caps "$caps" --argjson containers "$CONTAINERS" --argjson sockets "$SOCKETS" \
  --argjson rows "$rows" --arg captured "$(date -u +%Y-%m-%d)" \
  '{flavor: $flavor, daemon_version: $version, kernel: $kernel,
    capabilities: ($caps | split(" ")),
    workload: {containers: $containers, sockets_per_container: $sockets, named_netns: ["golden-ns"]},
    rows: $rows, captured: $captured}' >"$OUT/manifest.json"
rm -f "$OUT"/ss-golden-*

echo "GOLDEN_ROWS $rows"
echo "XTCP2_GOLDEN_DUMP_START"
tar c -C "$OUT" . | gzip -n | base64 -w0
echo
echo "XTCP2_GOLDEN_DUMP_END"
echo "GOLDEN_DONE"
