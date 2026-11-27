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
# Expects PROM_SOCKET, TOKEN_FILE, BUCKET, CONTAINERS, SOCKETS, FLAVOR,
# TCP_CLIENT and BULK_CC from the wrapper, which prepends golden-common.sh (ERR_TAG=GOLDEN_ERR).
# Any unexpected exit reports itself, so the runner fails fast instead of
# waiting out its timeout.
trap 'echo "GOLDEN_ERR $0: line $LINENO exited $?"' ERR

OUT=/tmp/golden
# From boot: the workload (image load, containers) comes up before xtcp2.
FLUSH_SEC="${GOLDEN_FLUSH_SEC:-300}"
rm -rf "$OUT"
mkdir -p "$OUT"

ip_bin=$(command -v ip)
# spawn NAME NETNS CMD...: a transient unit, as in the workload.
spawn() {
  local unit=$1 ns=$2
  shift 2
  systemd-run --unit="xtcp2-golden-$unit" --collect "$ip_bin" netns exec "$ns" "$@" >/dev/null
}
count_state() { ip netns exec "$1" ss -Htan state "$2" | wc -l; }

# After poll 1, so poll 2 holds exactly these: K_TW TIME_WAIT sockets
# (TIME_WAIT lasts 60 s and polls are 30 s apart) and K_SYN SYN_SENT ones
# (the SYN retries run about 2 min) in golden-states-a. Both are in a named
# netns, so no host TCP.
K_TW=3
K_SYN=2
after_first_poll() {
  local tw=0 syn=0
  spawn states-close golden-states-a "$TCP_CLIENT" -count "$K_TW" -connect 10.3.0.3 -close -startsleep 0 -pads 0
  spawn states-syn golden-states-a "$TCP_CLIENT" -count "$K_SYN" -connect 192.0.2.1 -dialTimeout 10m -dialr 1 -startsleep 0 -pads 0
  for _ in $(seq 1 50); do
    tw=$(count_state golden-states-a time-wait)
    syn=$(count_state golden-states-a syn-sent)
    if [ "$tw" -eq "$K_TW" ] && [ "$syn" -eq "$K_SYN" ]; then return 0; fi
    sleep 0.1
  done
  fail "after poll 1: $tw TIME_WAIT (want $K_TW), $syn SYN_SENT (want $K_SYN)"
}

echo "GOLDEN_START"
wait_second_poll "$FLUSH_SEC" after_first_poll

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
# golden-ernic-b, -c and -d have ids: ionic1-3 were moved there from the
# root namespace.
n=$(q "SELECT count(*) FROM $P WHERE container_name = '' AND netns NOT IN ('golden-ns', 'golden-ernic-b', 'golden-ernic-c', 'golden-ernic-d') AND nsid_assigned")
ok=0
[ "$n" -eq 0 ] && ok=1
row host_unassigned corner "$ok" "$n host rows with nsid_assigned (the root namespace has no id for itself)"

# The netns pairs (golden-workload.sh).
mapfile -t cas </run/xtcp2-golden-cong-available
missing=""
for ca in "${cas[@]}"; do
  for fam in 2 10; do
    n=$(q "SELECT count(*) FROM $P WHERE netns = 'golden-cc-a' AND inet_diag_msg_state = 1 AND inet_diag_msg_family = $fam AND inet_diag_cong = '$ca'")
    [ "$n" -eq 1 ] || missing="$missing $ca/$fam($n)"
  done
done
ok=0
[ -z "$missing" ] && ok=1
row every_cc positive "$ok" "${#cas[@]} algorithms x 2 families, one row each in golden-cc-a; off:${missing:- none}"
# Each algorithm with its own info struct fills it.
missing=""
info() {
  local n
  n=$(q "SELECT count(*) FROM $P WHERE netns IN ('golden-cc-a', 'golden-bulk-a') AND inet_diag_cong = '$1' AND $2")
  [ "$n" -ge 1 ] || missing="$missing $1"
}
for ca in "${cas[@]}"; do
  case "$ca" in
    bbr) info "$ca" "bbr_info_pacing_gain > 0" ;;
    dctcp) info "$ca" "dctcp_info_enabled > 0" ;;
    vegas | nv | illinois | westwood | yeah) info "$ca" "vegas_info_enabled > 0" ;;
  esac
done
ok=0
[ -z "$missing" ] && ok=1
row cc_info positive "$ok" "bbr/dctcp/vegas info filled; missing:${missing:- none}"
O="netns IN ('golden-opts-a', 'golden-opts-b')"
got=$(q "SELECT concat_ws(',', bool_or(inet_diag_tos = 8), bool_or(inet_diag_tos = 184), bool_or(inet_diag_tclass = 184), bool_or(inet_diag_class_id = 6), bool_or(inet_diag_sockopt & 4 > 0), count(DISTINCT tcp_info_rcv_wscale) FILTER (netns = 'golden-opts-a') >= 3) FROM $P WHERE $O")
ok=0
[ "$got" = "true,true,true,true,true,true" ] && ok=1
row opts_seen positive "$ok" "tos 8, tos 184, tclass 184, class_id 6, freebind, 3 rcv_wscale: $got"
n=$(q "SELECT count(*) FROM $P WHERE netns = 'golden-bulk-a' AND inet_diag_msg_socket_destination_port = 4000 AND tcp_info_total_retrans > 0")
ok=0
[ "$n" -eq "$(wc -w <<<"$BULK_CC")" ] && ok=1
per_cc=$(q "SELECT string_agg(inet_diag_cong || '=' || tcp_info_total_retrans || '/state' || inet_diag_msg_state || '/segs_out' || tcp_info_segs_out || '/bytes_acked' || tcp_info_bytes_acked, ',' ORDER BY inet_diag_cong) FROM $P WHERE netns = 'golden-bulk-a' AND inet_diag_msg_socket_destination_port = 4000")
# netem's own sent and dropped counts on each side's veth0, to tell "netem
# dropped nothing" from "TCP saw no loss" when this row fails, and, beside
# the namespace's OutSegs + RetransSegs, whether every segment went
# through netem at all (sent + dropped also counts ARP and IPv6 ND).
netem_drops=""
for ns in golden-bulk-a golden-bulk-b; do
  netem_drops="$netem_drops $ns=$(ip netns exec "$ns" tc -s qdisc show dev veth0 | awk '/dropped/ { sub(",", "", $7); print "sent" $4 "/dropped" $7; exit }')"
done
# The kernel's own view of the clients (ss, by local port, with the ECN
# flags) and of each namespace (nstat; a fresh netns starts its MIBs at 0).
# ss_bulk prints PORT=TOTAL_RETRANS per client, comma-separated as q's
# string_agg (q strips spaces); ss shows retrans only when nonzero. The
# detail adds the algorithm, ECN flags, segs_out and rtt (netem's 5 ms
# each way makes it 10 ms or more). ss_bulk detail SEP server does the
# same for the servers on golden-bulk-b, keyed by the client's port.
ss_bulk() {
  local ns=golden-bulk-a filter='( dport = :4000 )' key=3
  if [ "${3:-}" = server ]; then ns=golden-bulk-b filter='( sport = :4000 )' key=4; fi
  ip netns exec "$ns" ss -HtinO state established "$filter" |
    awk -v detail="${1:-}" -v key="$key" '{ p = $key; sub(/.*:/, "", p); r = 0; e = ""; t = "?"
      for (i = 5; i <= NF; i++) { if ($i ~ /^retrans:/) { r = $i; sub(/.*\//, "", r) } if ($i ~ /^rtt:/) { t = substr($i, 5); sub(/\/.*/, "", t) } if ($i == "ecn" || $i == "ecnseen") e = e "+" $i }
      if (detail) print p "=" r "/" $5 e "/segs_out" seg($0) "/rtt" t; else print p "=" r }
      function seg(l) { if (match(l, /segs_out:[0-9]+/)) return substr(l, RSTART + 9, RLENGTH - 9); return "?" }' |
    sort -n | paste -sd "${2:-,}"
}
nstat_of() { ip netns exec "$1" nstat -asz "$2" | awk -v k="$2" '$1 == k { print $2; found = 1 } END { if (!found) print "?" }'; }
mibs=""
for ns in golden-bulk-a golden-bulk-b; do
  mibs="$mibs $ns=RetransSegs$(nstat_of "$ns" TcpRetransSegs)/OutSegs$(nstat_of "$ns" TcpOutSegs)"
done
row loss_seen negative "$ok" "$n bulk clients with retransmits, of $(wc -w <<<"$BULK_CC"); per client, retransmits/state/segs_out/bytes_acked: ${per_cc:-none}; netem sent/dropped:$netem_drops; kernel per port, retrans/cong/segs_out/rtt: $(ss_bulk detail " "); servers by client port: $(ss_bulk detail " " server);$mibs"
# The dctcp bulk client keeps dctcp: a SYN lost to netem must not make it
# fall back to dctcp-reno (tcp_ecn_fallback=0 in golden-bulk-a, the
# workload). Only when BULK_CC has dctcp.
if [[ " $BULK_CC " == *" dctcp "* ]]; then
  got=$(q "SELECT coalesce(string_agg(inet_diag_cong, ',' ORDER BY inet_diag_cong), 'none') FROM $P WHERE netns = 'golden-bulk-a' AND inet_diag_msg_socket_destination_port = 4000 AND inet_diag_cong LIKE 'dctcp%'")
  ok=0
  [ "$got" = dctcp ] && ok=1
  row bulk_dctcp_ecn negative "$ok" "the dctcp bulk client's algorithm: $got, want dctcp (dctcp-reno is the ECN fallback)"
fi
# A fresh netns's TcpRetransSegs is the sum of its sockets' total_retrans:
# both count each retransmitted segment (net/ipv4/tcp_output.c:3710,
# :3713), SYN-ACK retransmits count in both too (tcp_output.c:4722,
# net/ipv4/tcp_minisocks.c:591). Only a socket closed before the poll
# would take its share away, and the bulk sockets stay open.
for side in a b; do
  ns=golden-bulk-$side
  sum=$(q "SELECT coalesce(sum(tcp_info_total_retrans), 0) FROM $P WHERE netns = '$ns'")
  mib=$(nstat_of "$ns" TcpRetransSegs)
  ok=0
  [ "$sum" = "$mib" ] && ok=1
  row "netem_retrans_$side" positive "$ok" "$ns: xtcp2's sum of total_retrans $sum, the namespace's TcpRetransSegs $mib"
done
want=$(ss_bulk)
got=$(q "SELECT string_agg(inet_diag_msg_socket_source_port || '=' || tcp_info_total_retrans, ',' ORDER BY inet_diag_msg_socket_source_port) FROM $P WHERE netns = 'golden-bulk-a' AND inet_diag_msg_socket_destination_port = 4000 AND inet_diag_msg_state = 1")
ok=0
[ -n "$want" ] && [ "$got" = "$want" ] && ok=1
row netem_kernel_agrees negative "$ok" "per client port=total_retrans, xtcp2: ${got:-none}; kernel (ss): ${want:-none}"
n=$(q "SELECT count(*) FROM $P WHERE netns IN ('golden-cc-a', 'golden-cc-b', 'golden-opts-a', 'golden-opts-b') AND tcp_info_total_retrans > 0")
ok=0
[ "$n" -eq 0 ] && ok=1
row no_loss_idle negative "$ok" "$n idle rows with retransmits"
ernic_counters=""
if [ -n "${ERNIC_TX_EVERY:-}" ]; then
  # The root namespace's bulk clients to golden-ernic-b, over ionic0.
  E="netns = 'default' AND inet_diag_msg_socket_destination_port = 4000 AND inet_diag_msg_state = 1"
  nbulk=$(wc -w <<<"$BULK_CC")
  n=$(q "SELECT count(*) FROM $P WHERE $E AND tcp_info_total_retrans > 0")
  per_cc=$(q "SELECT string_agg(inet_diag_cong || '=' || tcp_info_total_retrans, ',' ORDER BY inet_diag_cong) FROM $P WHERE $E")
  ok=0
  [ "$n" -eq "$nbulk" ] && ok=1
  row ernic_loss positive "$ok" "$n ionic bulk clients with retransmits, of $nbulk; retransmits per client: ${per_cc:-none}"
  got=$(q "SELECT count(*) || ':' || coalesce(string_agg(DISTINCT enrich_socket_dest_egress_ifname, ','), '') FROM $P WHERE $E")
  ok=0
  [ "$got" = "$nbulk:ionic0" ] && ok=1
  row ernic_egress positive "$ok" "ionic bulk clients and their egress: $got, want $nbulk:ionic0"
  tx=$(cat /sys/class/net/ionic0/statistics/tx_packets)
  dropped=$(cat /sys/class/net/ionic0/statistics/tx_dropped)
  # The emulator sees every frame the guest queues; tx_packets counts only
  # those it sent, so its events are tx_packets + tx_dropped.
  want=$(((tx + dropped) / ERNIC_TX_EVERY))
  ok=0
  [ "$dropped" -gt 0 ] && [ "$dropped" -eq "$want" ] && ok=1
  row fault_count boundary "$ok" "ionic0 tx_packets $tx, tx_dropped $dropped, want (tx_packets + tx_dropped) / $ERNIC_TX_EVERY = $want"
  before=$(cat /run/xtcp2-golden-ernic-before 2>/dev/null || echo missing)
  ok=0
  [ "$before" = "0 0" ] && ok=1
  row no_noise corner "$ok" "ionic0 tx_packets, ionic1 rx_packets before the transfers: $before, want 0 0"
  # Settled: the counters do not move in a second (no keepalives, xtcp2
  # stopped).
  rx=$(ip netns exec golden-ernic-b cat /sys/class/net/ionic1/statistics/rx_packets)
  sleep 1
  tx2=$(cat /sys/class/net/ionic0/statistics/tx_packets)
  rx2=$(ip netns exec golden-ernic-b cat /sys/class/net/ionic1/statistics/rx_packets)
  ok=0
  [ "$tx2" = "$tx" ] && [ "$rx2" = "$rx" ] && ok=1
  row quiescent corner "$ok" "ionic0 tx_packets $tx then $tx2, ionic1 rx_packets $rx then $rx2, 1 s apart"
  # The segments xtcp2 recorded: sent by the root namespace's sockets out of
  # ionic0, received by golden-ernic-b's sockets on ionic1 (every state, so
  # a retransmitted SYN counted on a listener is in).
  segs_out=$(q "SELECT coalesce(sum(tcp_info_segs_out), 0) FROM $P WHERE netns = 'default' AND enrich_socket_dest_egress_ifname = 'ionic0'")
  segs_in=$(q "SELECT coalesce(sum(tcp_info_segs_in), 0) FROM $P WHERE netns = 'golden-ernic-b'")
  # The final counters for the host's ernic-oracle, then both links down,
  # so nothing more reaches the emulator before its fault report.
  # Diagnostic: the same sum from the kernel now, per socket, so a
  # disagreement tells frames sent after xtcp2's poll from frames no socket
  # counted.
  echo "GOLDEN_ERNIC_SS_SEGS_OUT $(ss -Htin state all dst 10.0.4.16/31 | awk '{ for (i = 1; i <= NF; i++) if ($i ~ /^segs_out:/) { n += substr($i, 10); printf "%s ", substr($i, 10) } } END { print "sum " n + 0 }')"
  ernic_counters="ionic0_tx=$tx ionic0_tx_dropped=$dropped ionic1_rx=$rx ionic0_segs_out=$segs_out ionic1_segs_in=$segs_in"
  ip link set ionic0 down
  ip -n golden-ernic-b link set ionic1 down
fi
if [ -n "${ERNIC_SEQ_DROPS:-}" ]; then
  # The sequence-pattern flow, golden-ernic-c to golden-ernic-d over ionic2,
  # whose emulator drops a fixed ERNIC_SEQ_DROPS frames of the transfer.
  C="netns = 'golden-ernic-c' AND inet_diag_msg_socket_destination_port = 4000 AND inet_diag_msg_state = 1"
  stx() { ip netns exec golden-ernic-c cat "/sys/class/net/ionic2/statistics/$1"; }
  srx() { ip netns exec golden-ernic-d cat /sys/class/net/ionic3/statistics/rx_packets; }
  tx=$(stx tx_packets)
  dropped=$(stx tx_dropped)
  rx=$(srx)
  ok=0
  [ "$dropped" -eq "$ERNIC_SEQ_DROPS" ] && ok=1
  row seq_fired boundary "$ok" "ionic2 tx_dropped $dropped, want the sequence's $ERNIC_SEQ_DROPS"
  got=$(q "SELECT count(*) || ':' || coalesce(string_agg(DISTINCT enrich_socket_dest_egress_ifname, ','), '') || ':' || coalesce(max(tcp_info_total_retrans), 0) || ':' || coalesce(max(tcp_info_total_rto), 0) || ':' || coalesce(max(tcp_info_segs_out), 0) FROM $P WHERE $C")
  IFS=: read -r n egress retrans rto segs_out <<<"$got"
  ok=0
  [ "$n" = 1 ] && [ "$egress" = ionic2 ] && ok=1
  row seq_egress negative "$ok" "the sequence flow's client and egress: $n:$egress, want 1:ionic2 (not uplink ionic0)"
  # The flow is one-way, so every dropped frame is a data segment and is
  # retransmitted exactly once.
  ok=0
  [ "$retrans" -eq "$ERNIC_SEQ_DROPS" ] && ok=1
  row seq_retrans positive "$ok" "the sequence flow's total_retrans $retrans (total_rto $rto), want the sequence's $ERNIC_SEQ_DROPS"
  # The server sends only ACKs, over NICs without faults: nothing to resend.
  got=$(q "SELECT count(*) || ':' || coalesce(max(tcp_info_total_retrans), 0) FROM $P WHERE netns = 'golden-ernic-d' AND inet_diag_msg_socket_source_port = 4000 AND inet_diag_msg_state = 1")
  IFS=: read -r n sretrans <<<"$got"
  ok=0
  [ "$n" = 1 ] && [ "$sretrans" = 0 ] && ok=1
  row seq_server_no_retrans negative "$ok" "the sequence flow's server: $n:$sretrans connections:total_retrans, want 1:0"
  # The bursts are short enough for fast recovery: no timeout in six of six
  # captures when this row was added.
  ok=0
  [ "$rto" = 0 ] && ok=1
  row seq_no_rto corner "$ok" "the sequence flow's total_rto $rto, want 0 (bursts of 1, 3 and 8 recover without a timeout)"
  ok=0
  [ "$segs_out" -eq $((tx + dropped)) ] && ok=1
  row seq_segs_out positive "$ok" "the sequence flow's segs_out $segs_out, want ionic2 tx_packets $tx + tx_dropped $dropped"
  before=$(cat /run/xtcp2-golden-ernic-seq-before 2>/dev/null || echo missing)
  ok=0
  [ "$before" = "0 0" ] && ok=1
  row seq_no_noise corner "$ok" "ionic2 tx_packets, ionic3 rx_packets before the transfer: $before, want 0 0"
  segs_in=$(q "SELECT coalesce(sum(tcp_info_segs_in), 0) FROM $P WHERE netns = 'golden-ernic-d'")
  sleep 1
  ok=0
  [ "$(stx tx_packets)" = "$tx" ] && [ "$(srx)" = "$rx" ] && ok=1
  row seq_quiescent corner "$ok" "ionic2 tx_packets $tx, ionic3 rx_packets $rx, unchanged 1 s later"
  ernic_counters="$ernic_counters ionic2_tx=$tx ionic2_tx_dropped=$dropped ionic3_rx=$rx ionic2_segs_out=$segs_out ionic3_segs_in=$segs_in"
  ip -n golden-ernic-c link set ionic2 down
  ip -n golden-ernic-d link set ionic3 down
fi
# For the host's ernic-oracle, one line (it reads the last).
if [ -n "$ernic_counters" ]; then echo "GOLDEN_ERNIC_COUNTERS ${ernic_counters# }"; fi
got=$(q "SELECT concat_ws(',', count(*) FILTER (netns = 'golden-states-a' AND inet_diag_msg_state = 6), count(*) FILTER (netns = 'golden-states-a' AND inet_diag_msg_state = 2), count(*) FILTER (netns = 'golden-states-a' AND inet_diag_msg_state = 5), count(*) FILTER (netns = 'golden-states-b' AND inet_diag_msg_state = 8)) FROM $P")
ok=0
[ "$got" = "$K_TW,$K_SYN,1,1" ] && ok=1
row state_counts boundary "$ok" "TIME_WAIT,SYN_SENT,FIN_WAIT2,CLOSE_WAIT = $got, want $K_TW,$K_SYN,1,1"
got=$(q "SELECT string_agg(DISTINCT netns || '=' || tcp_info_pmtu || '/' || tcp_info_snd_mss, ',' ORDER BY netns || '=' || tcp_info_pmtu || '/' || tcp_info_snd_mss) FROM $P WHERE netns IN ('golden-cc-a', 'golden-opts-a', 'golden-states-a') AND inet_diag_msg_family = 2 AND inet_diag_msg_state IN (1, 5)")
ok=0
[ "$got" = "golden-cc-a=1500/1448,golden-opts-a=1450/1398,golden-states-a=1300/1248" ] && ok=1
row mtu_paths corner "$ok" "IPv4 pmtu/snd_mss per pair: $got"

# The NICs, locality and ASN (goldenNicArgs, goldenErnicNics, goldenAsnDb,
# golden-loc-*). Uplink 1 is the rocm-ernic ionic NIC, uplink 2 the e1000e;
# virtio's default route ranks third (docs/design-rocm-ernic-nics.md).
got=$(q "SELECT concat_ws(',', max(uplink1_ifname), max(uplink1_nic_driver), max(uplink1_nic_pci_vendor), max(uplink1_nic_pci_device), bool_or(uplink1_nic_bus_info <> '')) FROM $P")
ok=0
[ "$got" = "ionic0,ionic,7640,4098,true" ] && ok=1
row uplink1_ionic positive "$ok" "uplink1 ifname, driver, PCI vendor, device, bus set: $got"
got=$(q "SELECT concat_ws(',', max(uplink2_nic_driver), max(uplink2_nic_pci_vendor), bool_or(uplink2_nic_fw_version <> ''), bool_or(uplink2_nic_bus_info <> '')) FROM $P")
ok=0
[ "$got" = "e1000e,32902,true,true" ] && ok=1
row uplink2 positive "$ok" "uplink2 driver, PCI vendor, firmware set, bus set: $got"
n=$(q "SELECT count(*) FROM $P WHERE uplink1_nic_driver = 'virtio_net' OR uplink2_nic_driver = 'virtio_net'")
ok=0
[ "$n" -eq 0 ] && ok=1
row virtio_not_uplink negative "$ok" "$n rows with virtio_net as an uplink"
got=$(q "SELECT string_agg(DISTINCT enrich_socket_dest_locality::VARCHAR, ',' ORDER BY enrich_socket_dest_locality::VARCHAR) FROM $P WHERE netns = 'golden-loc-a' AND inet_diag_msg_state = 1")
ok=0
[ "$got" = "1,2,3" ] && ok=1
row locality_all positive "$ok" "localities of golden-loc-a's connections: $got"
n=$(q "SELECT count(*) FROM $P WHERE netns = 'golden-opts-a' AND enrich_socket_interface_name = 'veth0'")
ok=0
[ "$n" -eq 1 ] && ok=1
row bound_ifname positive "$ok" "$n rows bound to veth0 (SO_BINDTODEVICE) named"
got=$(q "SELECT concat_ws(',', count(*) FILTER (enrich_socket_dest_asn <> 0), count(*) FILTER (enrich_socket_dest_asn <> 0 AND (enrich_socket_dest_locality <> 3 OR enrich_socket_dest_network_owner = ''))) FROM $P")
ok=0
[ "$got" = "2,0" ] && ok=1
row asn_remote negative "$ok" "rows with an ASN, and of those not REMOTE or without an owner: $got, want 2,0"
n=$(q "SELECT count(*) FROM $P WHERE netns = 'golden-states-a' AND inet_diag_msg_state = 2 AND enrich_socket_dest_locality = 3 AND enrich_socket_dest_asn = 0")
ok=0
[ "$n" -eq "$K_SYN" ] && ok=1
row no_asn_unlisted negative "$ok" "$n REMOTE SYN_SENT rows to 192.0.2.1 (not in the artifact) without an ASN, want $K_SYN"
got=$(q "SELECT concat_ws(',', string_agg(DISTINCT uplink1_nic_speed_mbps::VARCHAR, '/'), string_agg(DISTINCT uplink2_nic_speed_mbps::VARCHAR, '/')) FROM $P")
ok=0
[ "$got" = "100000,1000" ] && ok=1
row nic_speed boundary "$ok" "uplink1, uplink2 speed: $got"
n=$(q "SELECT count(*) FROM $P WHERE netns = 'golden-loc-a' AND inet_diag_msg_family = 10 AND enrich_socket_dest_locality = 3 AND enrich_socket_dest_asn = 64501 AND enrich_socket_dest_network_owner = 'fixture-net6'")
ok=0
[ "$n" -eq 1 ] && ok=1
row v6_remote corner "$ok" "$n IPv6 rows to 2001:db8:113::10 with AS64501"
got=$(q "SELECT string_agg(DISTINCT enrich_socket_dest_egress_ifname, ',') FROM $P WHERE netns = 'golden-loc-a' AND enrich_socket_dest_locality = 3")
ok=0
[ "$got" = "veth0" ] && ok=1
row egress_named corner "$ok" "egress of golden-loc-a's REMOTE rows: $got"
got=$(q "SELECT string_agg(DISTINCT enrich_socket_dest_egress_ifname, ',') FROM $P WHERE netns = 'golden-metric-a' AND inet_diag_msg_state = 1")
ok=0
[ "$got" = "m1" ] && ok=1
row egress_metric positive "$ok" "egress of golden-metric-a's connection over two default routes (m1 metric 100, m2 metric 200): $got, want m1"
got=$(q "SELECT string_agg(DISTINCT uplink1_nic_fw_version, ',') FROM $P")
ok=0
[ "$got" = "xtcp2-golden-1.0" ] && ok=1
row fw_from_profile corner "$ok" "uplink1 firmware, from the NIC profile's [identity]: $got"
[ "$row_failed" = 0 ] || fail "rows failed"

version=$(q "SELECT any_value(daemon_version) FROM $P")

uname -r >"$OUT/uname"
jq -n \
  --arg flavor "$FLAVOR" --arg version "$version" --arg kernel "$(uname -r)" \
  --arg caps "$caps" --argjson containers "$CONTAINERS" --argjson sockets "$SOCKETS" \
  --argjson rows "$rows" --arg captured "$(date -u +%Y-%m-%d)" \
  --argjson cong "$(printf '%s\n' "${cas[@]}" | jq -R . | jq -s 'sort')" \
  '{flavor: $flavor, daemon_version: $version, kernel: $kernel,
    capabilities: ($caps | split(" ")),
    workload: {containers: $containers, sockets_per_container: $sockets,
      named_netns: ["golden-ns", "golden-cc-a", "golden-cc-b", "golden-bulk-a", "golden-bulk-b",
        "golden-opts-a", "golden-opts-b", "golden-states-a", "golden-states-b",
        "golden-loc-a", "golden-loc-b", "golden-remote", "golden-metric-a", "golden-metric-b"]},
    cong_available: $cong,
    rows: $rows, captured: $captured}' >"$OUT/manifest.json"
rm -f "$OUT"/ss-golden-* "$OUT"/nsid-golden-*

echo "GOLDEN_ROWS $rows"
echo "XTCP2_GOLDEN_DUMP_START"
tar c -C "$OUT" . | gzip -n | base64 -w0
echo
echo "XTCP2_GOLDEN_DUMP_END"
echo "GOLDEN_DONE"
