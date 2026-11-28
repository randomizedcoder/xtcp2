# Golden-record capture workload (the s3parquet-golden flavor). Starts a
# fixed set of sockets and returns only once every one is established, so
# xtcp2, ordered after this unit, sees the same workload every run:
#
#   golden-1..3  Docker containers, each TCP_COUNT=$SOCKETS listeners plus
#                as many loopback connections, idle (TCP_SLEEP=1h)
#   golden-ns    a named netns with one listener and one loopback connection
#   golden-cc-a/b      veth, MTU 1500: one idle connection per congestion
#                      algorithm the kernel offers (every tcp_* module is
#                      loaded first), IPv4 and IPv6
#   golden-bulk-a/b    veth, MTU 1500, netem delay/loss/reorder both ways:
#                      a 4 MiB echo per BULK_CC algorithm, finished before
#                      xtcp2 starts, so its counters are settled
#   golden-opts-a/b    veth, MTU 1450: TOS/traffic class, priority, the
#                      sockopt bits, three receive buffers, SO_BINDTODEVICE
#   golden-states-a/b  veth, MTU 1300: a FIN_WAIT2/CLOSE_WAIT pair; the
#                      capture adds TIME_WAIT and SYN_SENT after poll 1
#   golden-ernic-b     ionic1 (rocm-ernic) at 10.0.4.16-17: a 4 MiB echo per
#                      BULK_CC algorithm from the root namespace over ionic0,
#                      with the emulators' deterministic drops
#   golden-ernic-c/d   ionic2 (10.0.5.15) and ionic3 (10.0.5.16): one cubic
#                      4 MiB one-way transfer to a discarding server,
#                      through ionic2's fixed drop sequence
#   golden-loc-a/b     veth, MTU 1500, and golden-remote behind golden-loc-b:
#   golden-remote      one connection per locality (self, subnet, remote
#                      over IPv4 and IPv6 to documentation addresses)
#   golden-metric-a/b  two veths, m1 and m2, and a default route over each
#                      (metric 100 and 200): one connection, whose egress
#                      is the lower metric's m1
#   the host     as it is
#
# Every pair is a named netns, so root picks any algorithm and option
# without container capabilities, and no host TCP is opened.
#
# Idle sockets pass -keepalive 1h (the containers via TCP_CLIENT_ARGS and
# TCP_SERVER_ARGS). With Go's default of 15 s a poll could catch a probe
# before its ACK: the probe sets icsk_probes_out (net/ipv4/tcp_timer.c:844),
# which inet_diag reports as idiag_retrans (net/ipv4/inet_diag.c:295), and
# the diff saw a retransmit on an idle socket. With 1 h the keepalive timer
# still shows, but no probe is sent during a capture.
#
# Expects IMAGE, CONTAINERS, SOCKETS, TCP_SERVER and TCP_CLIENT from the
# wrapper. A failure prints GOLDEN_ERR, which the host runner reports.
# Any unexpected exit reports itself, so the runner fails fast instead of
# waiting out its timeout.
trap 'echo "GOLDEN_ERR $0: line $LINENO exited $?"' ERR

READY_SEC="${GOLDEN_READY_SEC:-60}"

fail() {
  echo "GOLDEN_ERR $*"
  exit 1
}

for _ in $(seq 1 30); do
  if docker info >/dev/null 2>&1; then break; fi
  sleep 1
done
docker info >/dev/null 2>&1 || fail "docker not ready"
docker image inspect "$IMAGE" >/dev/null 2>&1 || fail "image $IMAGE not loaded"

for i in $(seq 1 "$CONTAINERS"); do
  docker rm --force "golden-$i" >/dev/null 2>&1 || true
  docker run --detach --name "golden-$i" \
    --env TCP_MODE=both --env "TCP_COUNT=$SOCKETS" --env TCP_SLEEP=1h --env TCP_PADS=0 \
    --env "TCP_CLIENT_ARGS=-keepalive 1h" --env "TCP_SERVER_ARGS=-keepalive 1h" \
    "$IMAGE" >/dev/null || fail "golden-$i did not start"
done

# spawn NAME NETNS CMD...: a transient unit, so the process outlives this
# oneshot.
spawn() {
  local unit=$1 ns=$2
  shift 2
  systemd-run --unit="xtcp2-golden-$unit" --collect "$ip_bin" netns exec "$ns" "$@" >/dev/null
}
ip_bin=$(command -v ip)

ip netns del golden-ns 2>/dev/null || true
ip netns add golden-ns
ip -n golden-ns link set lo up
# Transient units, so the pair outlives this oneshot.
systemd-run --unit=xtcp2-golden-ns-server --collect \
  "$ip_bin" netns exec golden-ns "$TCP_SERVER" -count 1 -bind 127.0.0.1 -keepalive 1h
sleep 1
systemd-run --unit=xtcp2-golden-ns-client --collect \
  "$ip_bin" netns exec golden-ns "$TCP_CLIENT" -count 1 -connect 127.0.0.1 -sleep 1h -pads 0 -keepalive 1h

# pair NAME N MTU: netns golden-NAME-a and -b joined by veth0 at MTU, a at
# 10.N.0.1/24 and fd00:N::1/64, b at .2 and ::2. Each end has a permanent
# neighbor entry for the other, so no first connection waits on neighbor
# discovery. Right after link-up the first IPv6 solicitation went
# unanswered in about one pair in eight, and its 1 s retransmit
# (ND_RETRANS_TIMER, include/net/ndisc.h:49) outlasted tcp_client's 1 s
# dial timeout. The client's RST for the abandoned attempt then reached
# its half-open request: tcp_check_req resets it (embryonic_reset,
# net/ipv4/tcp_minisocks.c:964) and tcp_v6_rcv counts the segment on the
# listener (discard_and_relse, net/ipv6/tcp_ipv6.c:1944), so
# golden-opts-b's listener read 1 drop or 0.
pair() {
  local a="golden-$1-a" b="golden-$1-b" n=$2 mtu=$3 ns
  for ns in "$a" "$b"; do
    ip netns del "$ns" 2>/dev/null || true
    ip netns add "$ns"
    ip -n "$ns" link set lo up
  done
  ip link add veth0 netns "$a" mtu "$mtu" type veth peer name veth0 netns "$b" mtu "$mtu"
  ip -n "$a" addr add "10.$n.0.1/24" dev veth0
  ip -n "$a" addr add "fd00:$n::1/64" dev veth0 nodad
  ip -n "$b" addr add "10.$n.0.2/24" dev veth0
  ip -n "$b" addr add "fd00:$n::2/64" dev veth0 nodad
  ip -n "$a" link set veth0 up
  ip -n "$b" link set veth0 up
  local mac_a mac_b
  mac_a=$(ip netns exec "$a" cat /sys/class/net/veth0/address)
  mac_b=$(ip netns exec "$b" cat /sys/class/net/veth0/address)
  ip -n "$a" neigh replace "10.$n.0.2" lladdr "$mac_b" dev veth0 nud permanent
  ip -n "$a" neigh replace "fd00:$n::2" lladdr "$mac_b" dev veth0 nud permanent
  ip -n "$b" neigh replace "10.$n.0.1" lladdr "$mac_a" dev veth0 nud permanent
  ip -n "$b" neigh replace "fd00:$n::1" lladdr "$mac_a" dev veth0 nud permanent
}

# wait_listen NETNS PORT [N]: N listeners (default 1) on PORT, since the
# clients' dials don't retry a refusal.
wait_listen() {
  for _ in $(seq 1 50); do
    if [ "$(ip netns exec "$1" ss -Hltn "sport = :$2" | wc -l)" -ge "${3:-1}" ]; then return 0; fi
    sleep 0.1
  done
  fail "$1: fewer than ${3:-1} listeners on port $2"
}

# count_state NETNS STATE: the TCP sockets in that state (ss state names).
count_state() { ip netns exec "$1" ss -Htan state "$2" | wc -l; }
# bulk_done NS: sockets in NS that have received at least 4 MiB. awk rather
# than grep: before the first byte arrives ss prints no bytes_received, and
# a grep that matches nothing exits 1, which pipefail turns into an abort.
received_4mib() {
  awk '{ for (i = 1; i <= NF; i++) if ($i ~ /^bytes_received:/ && substr($i, 16) + 0 >= 4194304) n++ } END { print n + 0 }'
}
bulk_done() { ip netns exec "$1" ss -Htin state established | received_4mib; }
# acked_4mib NS: sockets in NS whose peer has acknowledged 4 MiB (plus the
# SYN) with nothing left unacknowledged; ss prints unacked only when nonzero.
acked_4mib() {
  ip netns exec "$1" ss -Htin state established |
    awk '{ a = 0; u = 0; for (i = 1; i <= NF; i++) { if ($i ~ /^bytes_acked:/ && substr($i, 13) + 0 > 4194304) a = 1; if ($i ~ /^unacked:/) u = 1 } if (a && !u) n++ } END { print n + 0 }'
}

# Every congestion module, so tcp_available_congestion_control lists every
# algorithm this kernel has; a new kernel's new one is picked up unasked.
mods="/run/booted-system/kernel-modules/lib/modules/$(uname -r)/kernel/net/ipv4"
for m in "$mods"/tcp_*.ko*; do
  name=$(basename "$m")
  modprobe "${name%%.ko*}" || fail "modprobe ${name%%.ko*}"
done
modprobe sch_netem || fail "modprobe sch_netem"
read -r -a cas </proc/sys/net/ipv4/tcp_available_congestion_control
[ "${#cas[@]}" -ge 2 ] || fail "only ${cas[*]} available"
# The largest -rcvbuf below; SO_RCVBUF is capped at rmem_max.
sysctl -qw net.core.rmem_max=33554432

# servers NAME N: on golden-NAME-b, a default-algorithm listener on .2/::2
# and a dctcp one on .3/::3. A SYN that dctcp sends ECT gets ECN back only
# from a listener whose algorithm needs ECN (tcp_ecn_create_request,
# net/ipv4/tcp_input.c); without it dctcp_init falls back to dctcp-reno
# (net/ipv4/tcp_dctcp.c). Specific addresses, since a wildcard listener
# would hold the port for every address.
servers() {
  local b="golden-$1-b" n=$2
  ip -n "$b" addr add "10.$n.0.3/24" dev veth0
  ip -n "$b" addr add "fd00:$n::3/64" dev veth0 nodad
  spawn "$1-server-4" "$b" "$TCP_SERVER" -count 1 -bind "10.$n.0.2" -keepalive 1h
  spawn "$1-server-6" "$b" "$TCP_SERVER" -count 1 -bind "fd00:$n::2" -keepalive 1h
  spawn "$1-server-dctcp-4" "$b" "$TCP_SERVER" -count 1 -bind "10.$n.0.3" -cong dctcp -keepalive 1h
  spawn "$1-server-dctcp-6" "$b" "$TCP_SERVER" -count 1 -bind "fd00:$n::3" -cong dctcp -keepalive 1h
  wait_listen "$b" 4000 4
}

# host CA: the server address suffix for algorithm CA (servers above).
host() { if [ "$1" = dctcp ]; then echo 3; else echo 2; fi; }

pair cc 1 1500
servers cc 1
for ca in "${cas[@]}"; do
  h=$(host "$ca")
  spawn "cc-$ca-4" golden-cc-a "$TCP_CLIENT" -count 1 -connect "10.1.0.$h" -network tcp4 -cong "$ca" -sleep 1h -pads 0 -keepalive 1h
  spawn "cc-$ca-6" golden-cc-a "$TCP_CLIENT" -count 1 -connect "fd00:1::$h" -network tcp6 -cong "$ca" -sleep 1h -pads 0 -keepalive 1h
done

pair bulk 4 1500
# One segment per skb, so netem's 2 % is drawn per segment. netem drops a
# GSO skb whole (it splits one only to corrupt it, net/sched/sch_netem.c:519)
# and counts it as one drop (:490-493), and turning the device's TSO/GSO
# off does not stop TCP building them: a TCP socket always gets software
# GSO (sk_setup_caps, net/core/sock.c:2680-2684) and the skb is split after
# the qdisc. A flow sending large skbs then draws loss a few hundred times
# for its 4 MiB, and one in about twenty captures a client lost none
# (loss_seen). gso_max_segs 1 caps every skb at one segment
# (sk_gso_max_segs, tcp_tso_segs in net/ipv4/tcp_output.c:2350).
# No ECN fallback on the clients' side: a netem-dropped SYN is resent
# without ECE/CWR (net/ipv4/tcp_output.c:3698-3705, tcp_ecn_clear_syn in
# include/net/tcp_ecn.h), and dctcp then falls back to dctcp-reno
# (net/ipv4/tcp_dctcp.c:112-115), one run in twenty or so. A lost SYN-ACK
# is covered too: the listener's first resend still sets ECE
# (tcp_rtx_synack before num_timeout++, net/ipv4/inet_connection_sock.c).
ip netns exec golden-bulk-a sysctl -qw net.ipv4.tcp_ecn_fallback=0
for ns in golden-bulk-a golden-bulk-b; do
  ip netns exec "$ns" ethtool -K veth0 tso off gso off gro off
  ip -n "$ns" link set veth0 gso_max_segs 1
  tc -n "$ns" qdisc add dev veth0 root netem delay 5ms loss 2% reorder 1%
done
servers bulk 4
# A headers-only tap on golden-bulk-a's veth0 for the bulk transfers. It
# sees what left netem (dev_queue_xmit_nit runs when the qdisc hands a
# packet to the device) and what arrived through the servers' netem, so
# per flow, segs_out minus the tapped count is netem's drops. Stopped once
# the transfers are done; the capture reads the counts it leaves in /run.
bulk_pcap=/run/xtcp2-golden-bulk-a.pcap
bulk_tap_err=/run/xtcp2-golden-bulk-tap.err
rm -f "$bulk_pcap" "$bulk_tap_err" /run/xtcp2-golden-bulk-tap /run/xtcp2-golden-bulk-tap-dropped
systemd-run --unit=xtcp2-golden-bulk-tap --collect -p "StandardError=file:$bulk_tap_err" \
  "$ip_bin" netns exec golden-bulk-a "$(command -v tcpdump)" -i veth0 -nn -s 96 -B 8192 -w "$bulk_pcap" 'tcp port 4000' >/dev/null
for _ in $(seq 1 50); do
  if grep -qs 'listening on' "$bulk_tap_err"; then break; fi
  sleep 0.1
done
grep -qs 'listening on' "$bulk_tap_err" || fail "bulk tap: tcpdump did not start: $(cat "$bulk_tap_err" 2>/dev/null)"
read -r -a bulk_algos <<<"$BULK_CC"
for ca in "${bulk_algos[@]}"; do
  spawn "bulk-$ca" golden-bulk-a "$TCP_CLIENT" -count 1 -connect "10.4.0.$(host "$ca")" -network tcp4 -cong "$ca" -bulk 4MiB -sleep 1h -pads 0 -keepalive 1h -dialTimeout 30s
done

# ernic (ERNIC_A_MAC set): ionic1 goes into golden-ernic-b, and one 4 MiB
# echo per BULK_CC algorithm runs from the root namespace over ionic0, whose
# emulator drops every ERNIC_TX_EVERY-th frame it transmits (ionic1's drops
# frames it receives; nix/microvms/golden-ernic.nix). Static neighbors, no
# IPv6 and no segmentation offloads, so each drop is one MTU frame and only
# the transfers' own frames reach the counters. No TCP keepalive either: an
# idle connection then sends nothing after xtcp2's poll, so the emulator's
# frame counts equal the segment counts xtcp2 records. A long dial timeout
# too: ionic1 can drop a SYN, and the kernel resends it after 1 s
# (TCP_TIMEOUT_INIT, include/net/tcp.h), when tcp_client's default 1 s
# timeout would close that socket and dial again, taking the SYN's
# segs_out with it.
ernic_bulk=0
if [ -n "${ERNIC_A_MAC:-}" ]; then
  # What ionic0 has sent since boot, before this script touches it.
  echo "GOLDEN_ERNIC_BEFORE $(ethtool -S ionic0 | awk '$1 ~ /^tx_/ && $2 != 0 { printf "%s%s ", $1, $2 }')neigh: $(ip neigh show dev ionic0 | tr '\n' ';') snmp6: $(awk '$2 != 0 { printf "%s=%s ", $1, $2 }' /proc/net/dev_snmp6/ionic0 2>/dev/null) maddr: $(ip maddr show dev ionic0 | tr -s ' \n' ' ')"
  e=golden-ernic-b
  ip netns del "$e" 2>/dev/null || true
  ip netns add "$e"
  ip -n "$e" link set lo up
  ip netns exec "$e" sysctl -qw net.ipv6.conf.default.disable_ipv6=1 net.ipv6.conf.all.disable_ipv6=1
  sysctl -qw net.ipv6.conf.ionic0.disable_ipv6=1
  ethtool -K ionic0 tso off gso off gro off
  ip link set ionic1 netns "$e"
  ip netns exec "$e" ethtool -K ionic1 tso off gso off gro off
  ip -n "$e" addr add 10.0.4.16/24 dev ionic1
  ip -n "$e" addr add 10.0.4.17/24 dev ionic1
  ip -n "$e" link set ionic1 up
  ip -n "$e" neigh replace 10.0.4.15 lladdr "$ERNIC_A_MAC" dev ionic1 nud permanent
  ip neigh replace 10.0.4.16 lladdr "$ERNIC_B_MAC" dev ionic0 nud permanent
  ip neigh replace 10.0.4.17 lladdr "$ERNIC_B_MAC" dev ionic0 nud permanent
  # The frames before any transfer, for the capture's no_noise ROW.
  printf '%s %s\n' "$(cat /sys/class/net/ionic0/statistics/tx_packets)" \
    "$(ip netns exec "$e" cat /sys/class/net/ionic1/statistics/rx_packets)" >/run/xtcp2-golden-ernic-before
  spawn ernic-server-4 "$e" "$TCP_SERVER" -count 1 -bind 10.0.4.16 -keepalive -1s
  spawn ernic-server-dctcp-4 "$e" "$TCP_SERVER" -count 1 -bind 10.0.4.17 -cong dctcp -keepalive -1s
  wait_listen "$e" 4000 2
  # The dctcp client is in this namespace: no ECN fallback here either (see
  # golden-bulk-a above), since the emulator can drop its SYN.
  sysctl -qw net.ipv4.tcp_ecn_fallback=0
  for ca in "${bulk_algos[@]}"; do
    dst=10.0.4.16
    if [ "$ca" = dctcp ]; then dst=10.0.4.17; fi
    systemd-run --unit="xtcp2-golden-ernic-bulk-$ca" --collect \
      "$TCP_CLIENT" -count 1 -connect "$dst" -network tcp4 -cong "$ca" -bulk 4MiB -sleep 1h -pads 0 -keepalive -1s -dialTimeout 30s >/dev/null
  done
  ernic_bulk=${#bulk_algos[@]}
fi

# The sequence-pattern pair (ERNIC_C_MAC set): ionic2 into golden-ernic-c,
# ionic3 into golden-ernic-d, and one cubic 4 MiB transfer between them, the
# only traffic on either, set up as the first pair is. It is one-way: the
# server discards (-discard) and the client reads no echo (-bulkOneWay), so
# every frame ionic2 sends after the handshake is data, and each of the
# sequence's drops is exactly one retransmit.
ernic_seq=0
if [ -n "${ERNIC_C_MAC:-}" ]; then
  for e in golden-ernic-c golden-ernic-d; do
    ip netns del "$e" 2>/dev/null || true
    ip netns add "$e"
    ip -n "$e" link set lo up
    ip netns exec "$e" sysctl -qw net.ipv6.conf.default.disable_ipv6=1 net.ipv6.conf.all.disable_ipv6=1
  done
  ip link set ionic2 netns golden-ernic-c
  ip link set ionic3 netns golden-ernic-d
  ip netns exec golden-ernic-c ethtool -K ionic2 tso off gso off gro off
  ip netns exec golden-ernic-d ethtool -K ionic3 tso off gso off gro off
  ip -n golden-ernic-c addr add 10.0.5.15/24 dev ionic2
  ip -n golden-ernic-d addr add 10.0.5.16/24 dev ionic3
  ip -n golden-ernic-c link set ionic2 up
  ip -n golden-ernic-d link set ionic3 up
  ip -n golden-ernic-c neigh replace 10.0.5.16 lladdr "$ERNIC_D_MAC" dev ionic2 nud permanent
  ip -n golden-ernic-d neigh replace 10.0.5.15 lladdr "$ERNIC_C_MAC" dev ionic3 nud permanent
  printf '%s %s\n' "$(ip netns exec golden-ernic-c cat /sys/class/net/ionic2/statistics/tx_packets)" \
    "$(ip netns exec golden-ernic-d cat /sys/class/net/ionic3/statistics/rx_packets)" >/run/xtcp2-golden-ernic-seq-before
  spawn ernic-seq-server golden-ernic-d "$TCP_SERVER" -count 1 -bind 10.0.5.16 -cong cubic -keepalive -1s -discard
  wait_listen golden-ernic-d 4000
  spawn ernic-seq-client golden-ernic-c "$TCP_CLIENT" -count 1 -connect 10.0.5.16 -network tcp4 -cong cubic -bulk 4MiB -bulkOneWay -pads 0 -keepalive -1s -dialTimeout 30s
  ernic_seq=1
fi

pair opts 2 1450
spawn opts-server golden-opts-b "$TCP_SERVER" -count 1 -bind :: -tos 0xb8 -priority 6 -freebind -transparent -keepalive 1h
wait_listen golden-opts-b 4000
spawn opts-tos4 golden-opts-a "$TCP_CLIENT" -count 1 -connect 10.2.0.2 -network tcp4 -tos 0x08 -priority 6 -sleep 1h -pads 0 -keepalive 1h
spawn opts-tos6 golden-opts-a "$TCP_CLIENT" -count 1 -connect fd00:2::2 -network tcp6 -tos 0xb8 -sleep 1h -pads 0 -keepalive 1h
spawn opts-nopor golden-opts-a "$TCP_CLIENT" -count 1 -connect 10.2.0.2 -network tcp4 -srcaddr 10.2.0.1 -bindNoPort -freebind -sleep 1h -pads 0 -keepalive 1h
spawn opts-iface golden-opts-a "$TCP_CLIENT" -count 1 -connect 10.2.0.2 -network tcp4 -iface veth0 -sleep 1h -pads 0 -keepalive 1h
for buf in 64KiB 1MiB 16MiB; do
  spawn "opts-rcvbuf-$buf" golden-opts-a "$TCP_CLIENT" -count 1 -connect 10.2.0.2 -network tcp4 -rcvbuf "$buf" -sleep 1h -pads 0 -keepalive 1h
done
opts_clients=7

# states: 10.3.0.2 holds after EOF; 10.3.0.3 closes, for the capture's
# TIME_WAIT clients; 192.0.2.0/24 is dropped silently past golden-states-b
# (a blackhole, not an unreachable, which would end a SYN_SENT), for its
# SYN_SENT clients.
pair states 3 1300
ip -n golden-states-b addr add 10.3.0.3/24 dev veth0
ip -n golden-states-a route add 192.0.2.0/24 via 10.3.0.2
ip netns exec golden-states-b sysctl -qw net.ipv4.ip_forward=1
ip -n golden-states-b route add blackhole 192.0.2.0/24
spawn states-hold golden-states-b "$TCP_SERVER" -count 1 -bind 10.3.0.2 -holdEOF -keepalive 1h
spawn states-close-server golden-states-b "$TCP_SERVER" -count 3 -bind 10.3.0.3 -keepalive 1h
wait_listen golden-states-b 4000 2
wait_listen golden-states-b 4002
spawn states-half golden-states-a "$TCP_CLIENT" -count 1 -connect 10.3.0.2 -halfclose -pads 0 -keepalive 1h

# loc: one destination of each locality from golden-loc-a. Its own address
# (self), golden-loc-b's (subnet), and golden-remote's documentation
# addresses through golden-loc-b as a router (remote; goldenAsnDb in mkVm.nix
# maps them to ASNs).
pair loc 5 1500
ip netns del golden-remote 2>/dev/null || true
ip netns add golden-remote
ip -n golden-remote link set lo up
ip link add veth1 netns golden-loc-b type veth peer name veth0 netns golden-remote
ip -n golden-loc-b addr add 203.0.113.1/24 dev veth1
ip -n golden-loc-b addr add 2001:db8:113::1/64 dev veth1 nodad
ip -n golden-remote addr add 203.0.113.10/24 dev veth0
ip -n golden-remote addr add 2001:db8:113::10/64 dev veth0 nodad
ip -n golden-loc-b link set veth1 up
ip -n golden-remote link set veth0 up
ip netns exec golden-loc-b sysctl -qw net.ipv4.ip_forward=1 net.ipv6.conf.all.forwarding=1
ip -n golden-remote route add default via 203.0.113.1
ip -n golden-remote -6 route add default via 2001:db8:113::1
ip -n golden-loc-a route add 203.0.113.0/24 via 10.5.0.2
ip -n golden-loc-a -6 route add 2001:db8:113::/48 via fd00:5::2
spawn loc-self-server golden-loc-a "$TCP_SERVER" -count 1 -bind 10.5.0.1 -keepalive 1h
spawn loc-subnet-server golden-loc-b "$TCP_SERVER" -count 1 -bind 10.5.0.2 -keepalive 1h
spawn loc-remote-server-4 golden-remote "$TCP_SERVER" -count 1 -bind 203.0.113.10 -keepalive 1h
spawn loc-remote-server-6 golden-remote "$TCP_SERVER" -count 1 -bind 2001:db8:113::10 -keepalive 1h
wait_listen golden-loc-a 4000
wait_listen golden-loc-b 4000
wait_listen golden-remote 4000 2
# Resolve every hop's neighbors first: a SYN that waits on neighbor
# discovery at golden-loc-b is sometimes retransmitted, and its RTO shows in
# one capture and not the next.
for dst in 10.5.0.2 203.0.113.10; do
  ip netns exec golden-loc-a ping -q -c 1 -W 5 "$dst" >/dev/null || fail "golden-loc-a: no reply from $dst"
done
for dst in fd00:5::2 2001:db8:113::10; do
  ip netns exec golden-loc-a ping -6 -q -c 1 -W 5 "$dst" >/dev/null || fail "golden-loc-a: no reply from $dst"
done
spawn loc-self golden-loc-a "$TCP_CLIENT" -count 1 -connect 10.5.0.1 -sleep 1h -pads 0 -keepalive 1h
spawn loc-subnet golden-loc-a "$TCP_CLIENT" -count 1 -connect 10.5.0.2 -sleep 1h -pads 0 -keepalive 1h
spawn loc-remote-4 golden-loc-a "$TCP_CLIENT" -count 1 -connect 203.0.113.10 -sleep 1h -pads 0 -keepalive 1h
spawn loc-remote-6 golden-loc-a "$TCP_CLIENT" -count 1 -connect 2001:db8:113::10 -sleep 1h -pads 0 -keepalive 1h
# The self connection shows twice in golden-loc-a, once per end.
loc_established=5

# metric: golden-metric-a reaches 192.0.2.80 (on golden-metric-b's lo) only
# through its two default routes, m1 at metric 100 and m2 at 200. The kernel
# sends over m1; the higher metric is added first, so the listing order is
# the kernel's sorting and not the order they were added.
for ns in golden-metric-a golden-metric-b; do
  ip netns del "$ns" 2>/dev/null || true
  ip netns add "$ns"
  ip -n "$ns" link set lo up
done
for i in 1 2; do
  ip link add "m$i" netns golden-metric-a type veth peer name "m$i" netns golden-metric-b
  ip -n golden-metric-a addr add "10.21.$i.1/24" dev "m$i"
  ip -n golden-metric-b addr add "10.21.$i.2/24" dev "m$i"
  ip -n golden-metric-a link set "m$i" up
  ip -n golden-metric-b link set "m$i" up
done
ip -n golden-metric-b addr add 192.0.2.80/32 dev lo
ip -n golden-metric-a route add default via 10.21.2.2 dev m2 metric 200
ip -n golden-metric-a route add default via 10.21.1.2 dev m1 metric 100
spawn metric-server golden-metric-b "$TCP_SERVER" -count 1 -bind 192.0.2.80 -keepalive 1h
wait_listen golden-metric-b 4000
ip netns exec golden-metric-a ping -q -c 1 -W 5 192.0.2.80 >/dev/null || fail "golden-metric-a: no reply from 192.0.2.80"
spawn metric golden-metric-a "$TCP_CLIENT" -count 1 -connect 192.0.2.80 -sleep 1h -pads 0 -keepalive 1h

# Each container's connections show twice, once per end.
need=$((2 * SOCKETS))
deadline=$((SECONDS + READY_SEC))
while :; do
  pending=""
  for i in $(seq 1 "$CONTAINERS"); do
    pid=$(docker inspect --format '{{.State.Pid}}' "golden-$i" 2>/dev/null || echo 0)
    n=0
    if [ "$pid" -gt 0 ]; then
      n=$(nsenter -t "$pid" -n ss -Htn state established | wc -l)
    fi
    if [ "$n" -lt "$need" ]; then pending="$pending golden-$i($n/$need)"; fi
  done
  n=$(ip netns exec golden-ns ss -Htn state established | wc -l)
  if [ "$n" -lt 2 ]; then pending="$pending golden-ns($n/2)"; fi
  n=$(count_state golden-cc-a established)
  if [ "$n" -lt $((2 * ${#cas[@]})) ]; then pending="$pending golden-cc-a($n/$((2 * ${#cas[@]})))"; fi
  # A bulk connection is done once both ends have received its 4 MiB.
  want=$((2 * ${#bulk_algos[@]}))
  n=$(bulk_done golden-bulk-a)
  m=$(bulk_done golden-bulk-b)
  if [ $((n + m)) -lt "$want" ]; then pending="$pending golden-bulk($((n + m))/$want)"; fi
  if [ "$ernic_bulk" -gt 0 ]; then
    n=$(received_4mib < <(ss -Htin state established dst 10.0.4.16/31))
    m=$(bulk_done golden-ernic-b)
    if [ $((n + m)) -lt $((2 * ernic_bulk)) ]; then pending="$pending golden-ernic($((n + m))/$((2 * ernic_bulk)))"; fi
  fi
  # The one-way transfer is done once the server has received the 4 MiB
  # and the client has its ACKs for all of it.
  if [ "$ernic_seq" -gt 0 ]; then
    n=$(acked_4mib golden-ernic-c)
    m=$(bulk_done golden-ernic-d)
    if [ $((n + m)) -lt 2 ]; then pending="$pending golden-ernic-seq($((n + m))/2)"; fi
  fi
  n=$(count_state golden-opts-a established)
  if [ "$n" -lt "$opts_clients" ]; then pending="$pending golden-opts-a($n/$opts_clients)"; fi
  n=$(count_state golden-loc-a established)
  if [ "$n" -lt "$loc_established" ]; then pending="$pending golden-loc-a($n/$loc_established)"; fi
  n=$(count_state golden-metric-a established)
  if [ "$n" -lt 1 ]; then pending="$pending golden-metric-a($n/1)"; fi
  n=$(count_state golden-states-a fin-wait-2)
  m=$(count_state golden-states-b close-wait)
  if [ "$n" -lt 1 ] || [ "$m" -lt 1 ]; then pending="$pending golden-states(fin-wait-2 $n, close-wait $m)"; fi
  if [ -z "$pending" ]; then break; fi
  if [ "$SECONDS" -ge "$deadline" ]; then
    fail "workload not ready after ${READY_SEC}s:$pending"
  fi
  sleep 1
done
# Stop the bulk tap (SIGINT flushes it) a second after the transfers, for
# their last delayed ACKs, and leave per client port the segments tapped
# from the client and from its server (one segment each: gso_max_segs 1),
# and tcpdump's kernel drops.
sleep 1
systemctl kill -s INT xtcp2-golden-bulk-tap || true
for _ in $(seq 1 50); do
  if ! systemctl is-active --quiet xtcp2-golden-bulk-tap; then break; fi
  sleep 0.1
done
tcpdump -nn -q -r "$bulk_pcap" 2>/dev/null |
  awk '{ s = $3; d = $5; sub(/:$/, "", d); sp = s; sub(/.*\./, "", sp); dp = d; sub(/.*\./, "", dp)
    if (dp == 4000) c[sp]++; else if (sp == 4000) v[dp]++ }
    END { for (p in c) print p "=" c[p] "/" v[p] + 0 }' |
  sort -n | paste -sd , >/run/xtcp2-golden-bulk-tap
awk '/packets dropped by kernel/ { print $1; found = 1 } END { if (!found) print "?" }' "$bulk_tap_err" >/run/xtcp2-golden-bulk-tap-dropped
# The capture's manifest lists the algorithms, for coverage.json's
# "@manifest.cong_available".
printf '%s\n' "${cas[@]}" >/run/xtcp2-golden-cong-available
echo "GOLDEN_WORKLOAD_READY cong_available=${cas[*]}"
