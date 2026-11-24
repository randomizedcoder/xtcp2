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

# The TCP sockets each container holds now, both families, for the
# row-count check below. The Go servers' 0.0.0.0 listeners are dual-stack
# (AF_INET6), as are the connections they accept.
for i in $(seq 1 "$CONTAINERS"); do
  pid=$(docker inspect --format '{{.State.Pid}}' "golden-$i")
  nsenter -t "$pid" -n ss -Htan >"$OUT/ss-golden-$i"
  # The kernel's nsid for the container's namespace, for the nsid rows
  # below: eth0's peer is the host end of its veth, whose link-netnsid is
  # that id ("none" when the host has none for it).
  peer=$(nsenter -t "$pid" -n ip -j link show eth0 | jq '.[0].link_index')
  ip -j link show | jq -r --argjson i "$peer" '.[] | select(.ifindex == $i) | .link_netnsid // "none"' >"$OUT/nsid-golden-$i"
done
# golden-ns has no veth, so normally no id: `ip netns list` shows "(id: N)"
# only once one is assigned.
golden_ns_id=$(ip netns list | sed -n 's/^golden-ns (id: \([0-9]*\))$/\1/p')
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
  [ "$n" -eq "$want" ] || fail "$c: $n rows, ss lists $want sockets"
done
ns=$(q "SELECT count(*) FROM $P WHERE netns = 'golden-ns'")
[ "$ns" -ge 1 ] || fail "golden-ns: no rows"
host=$(q "SELECT count(*) FROM $P WHERE container_name = '' AND netns <> 'golden-ns'")
[ "$host" -ge 1 ] || fail "host namespace: no rows"

# The nsid rows: nsid_assigned says whether nsid is set, so the kernel's
# first id (0) reads apart from no id at all.
row_failed=0
row() {
  if [ "$3" = 1 ]; then
    echo "ROW $1 $2 PASS $4"
  else
    echo "ROW $1 $2 FAIL $4"
    row_failed=1
  fi
}
zeros=0
for i in $(seq 1 "$CONTAINERS"); do
  c="golden-$i"
  want=$(cat "$OUT/nsid-golden-$i")
  [ "$want" = 0 ] && zeros=$((zeros + 1))
  n=$(q "SELECT count(*) FROM $P WHERE container_name = '$c' AND NOT nsid_assigned")
  ok=0
  [ "$want" != none ] && [ "$n" -eq 0 ] && ok=1
  row container_assigned positive "$ok" "$c: $n rows without nsid_assigned, link-netnsid $want"
  got=$(q "SELECT string_agg(DISTINCT nsid::VARCHAR, ',') FROM $P WHERE container_name = '$c'")
  ok=0
  [ "$got" = "$want" ] && ok=1
  row matches_link_netnsid positive "$ok" "$c: nsid $got, link-netnsid $want"
done
got=$(q "SELECT string_agg(DISTINCT if(nsid_assigned, nsid::VARCHAR, 'none'), ',') FROM $P WHERE netns = 'golden-ns'")
ok=0
[ "$got" = "${golden_ns_id:-none}" ] && ok=1
row matches_link_netnsid positive "$ok" "golden-ns: nsid $got, ip netns list ${golden_ns_id:-none}"
n=$(q "SELECT count(*) FROM $P WHERE nsid <> 0 AND NOT nsid_assigned")
ok=0
[ "$n" -eq 0 ] && ok=1
row no_stray negative "$ok" "$n rows with an nsid but no nsid_assigned"
ok=0
[ "$zeros" -eq 1 ] && ok=1
row zero_present boundary "$ok" "$zeros container namespaces with kernel id 0"
n=$(q "SELECT count(*) FROM $P WHERE container_name = '' AND netns <> 'golden-ns' AND nsid_assigned")
ok=0
[ "$n" -eq 0 ] && ok=1
row host_unassigned corner "$ok" "$n host rows with nsid_assigned (the root namespace has no id for itself)"
[ "$row_failed" = 0 ] || fail "nsid rows failed"

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
rm -f "$OUT"/ss-golden-* "$OUT"/nsid-golden-*

echo "GOLDEN_ROWS $rows"
echo "XTCP2_GOLDEN_DUMP_START"
tar c -C "$OUT" . | gzip -n | base64 -w0
echo
echo "XTCP2_GOLDEN_DUMP_END"
echo "GOLDEN_DONE"
