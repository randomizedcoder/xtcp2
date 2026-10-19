# nix/microvms/netlink-capture.nix
#
# Guest-side mechanism for the four driven netlink microVM flavors:
# netlink-dump-capture, which records the decoder corpus, goip-parity,
# which records the ip/goip/ip triples the parity comparator reads,
# link-write-capture, which records bond and LACP readback state off a
# topology in which LACP actually negotiates (docs/netlink/link-write-path.md),
# and lacp-a, which records the same readback over virtio-net ports with the
# partner in a second VM (Tier 2 of the same document).
#
# This exports ONE thing: `xtcp2-nlcap`, a helper installed on the guest's
# PATH (with `xtcp2-nl-mcdump` and `xtcp2-nl-ipmon`, below, on its own PATH)
# and invoked one subcommand at a time by a host driver
# (nix/microvms/scripts/capture-netlink-dumps.exp, goip-parity.exp,
# capture-link-writes.exp and capture-lacp-pair.exp) over the serial console.
#
# Both flavors capture the same way, from the same namespace topology, and
# that is deliberate rather than incidental: D_control - the diff between the
# two `ip` sides that the comparator subtracts - only means what it claims if
# the parity captures come off the topology the decoder fixtures came off.
# Two capture mechanisms would be two answers to "what was on the wire".
#
# WHY THE SPLIT IS THIS WAY ROUND
#
# The existing nlmon-capture flavor puts the whole capture sequence in a
# systemd oneshot baked into the VM. That is the right shape for a
# fire-and-forget event capture, and the wrong shape here, for two reasons:
#
#   - Changing the sequence means rebuilding the VM. A capture set is
#     something you iterate on - "does `ip link show dev lo` dump or
#     single-get?" is answered by trying it - and a rebuild per attempt makes
#     that a chore rather than a question.
#   - A baked-in script cannot be told to stop. The host knows things the
#     guest does not: which fixtures already exist, whether the previous
#     capture cleared its floor, whether the operator asked for the advisory
#     set. Policy belongs where that knowledge is.
#
# So: MECHANISM here, POLICY in the driver. This file knows how to create a
# namespace, run a capture with a floor, and write a sidecar. It does not know
# which captures exist or in what order, and it deliberately holds no list of
# them.
#
# NO SLEEPS, ANYWHERE
#
# Every wait in here is a poll for the condition that actually matters. The
# capture window in particular is opened by waiting for tcpdump to print
# `listening on` to stderr, which it does once the packet socket is bound -
# not by the `WARMUP=3` guess the older scripts use. That guess is why a
# capture could silently miss its first phase, and sleep-based synchronization
# is the recorded root cause of several intermittent failures elsewhere in
# this repo's suite.
#
# THE STOP SIDE NEEDS A WAIT, AND THE REASON IS NOT WHAT IT LOOKS LIKE
#
# This file previously argued that no wait was needed: `ip <object> show`
# returns only after reading NLMSG_DONE, nlmon mirrors in the same delivery
# path, so once the generator exits everything it provoked is already in
# tcpdump's hands. Every step of that is true about the KERNEL and it is still
# the wrong conclusion, because it stops one layer too early. Measured in the
# guest, the captures came back like this:
#
#     0 packets captured
#     3 packets received by filter
#     0 packets dropped by kernel
#
# The tap delivered all three datagrams and the packet socket counted them.
# What had not happened was tcpdump READING them: libpcap here is built with
# TPACKET_V3 (`libpcap version 1.10.6 ... with TPACKET_V3`), whose ring hands a
# block to userspace only once the block is full or its retire timeout expires,
# and tcpdump sets that timeout to 1000 ms. So a low-rate capture is invisible
# to tcpdump for up to a second, `-U` does not help because the packets have
# not reached tcpdump at all yet, and SIGINT breaks the loop before any of them
# are written. 16 of 18 captures came back empty; the 2 that passed were the
# ones where the retire happened to land before the signal. This is also what
# the older host script's `sleep 1` / `sleep 2` was silently covering for.
#
# Two changes, because they fix different halves:
#
#   - `--immediate-mode` on the capture, which turns off the block-retire delay
#     so a datagram is readable when it is delivered. On its own this made five
#     consecutive runs of the same capture return 3 datagrams each.
#   - A STOP SENTINEL, so the window closes on evidence rather than on
#     immediate-mode being fast enough. After the generator exits we emit a
#     NETLINK_SOCK_DIAG datagram (`ss -x`) and poll the capture file until it
#     appears. The ring is FIFO, so the sentinel being written proves every
#     datagram ahead of it has been written too - which is the actual property
#     wanted, and one no amount of waiting can assert.
#
# WHY `ss -x` IS THE SENTINEL
#
# It has to be tapped, countable on its own, removable from the fixture, and
# silent on NETLINK_ROUTE. `netlink_filter_tap()` whitelists NETLINK_SOCK_DIAG,
# so it is mirrored; it lands with sll_protocol 4, so `ether[14:2]==4` counts
# it and the fixture filter `ether[14:2]==<proto>` drops it without a second
# pass. <proto> is the row's netlink protocol: 0 for NETLINK_ROUTE unless the
# row passes `--proto` (see PROTOCOLS below), and never 4.
# The fourth requirement is the one that decided WHICH ss: measured alone in a
# namespace, `ss -x` emits `total=2 proto0=0 proto4=2`, while `ss -a` emits 4
# NETLINK_ROUTE datagrams of its own because it resolves interface names. A
# sentinel that adds rtnetlink traffic to a fixture whose whole purpose is to
# contain one transaction is not a sentinel, it is pollution.
#
# THE RING HAS EIGHT FRAMES, AND A BURST CAN OUTRUN IT
#
# The sentinel proves every datagram the tap DELIVERED is in the file. It
# cannot prove the packet socket kept them, and with tcpdump's defaults it
# does not always. `--immediate-mode` makes libpcap 1.10 use TPACKET_V2
# rather than V3 (pcap-linux.c, "if the user has requested immediate mode,
# we don't use TPACKET_V3"), and for a link type that is not Ethernet, as
# DLT_NETLINK is not, V2 sizes every frame by the snaplen: 262144 bytes plus
# the header, one datagram per frame however small. tcpdump's default 2 MiB
# buffer is then a ring of EIGHT frames. tcpdump writes and flushes each
# packet (-U), so a generator that gets eight datagrams ahead of it loses
# the rest to the kernel. Measured in the goip-parity guest, `ip addr restore`
# (one link dump, then 16 writes and acks in about 200 us) came back
#
#     19 packets captured
#     22 packets received by filter
#     3 packets dropped by kernel
#
# with `ip`'s exit status 0: the last writes were sent and acked, and the
# fixture held the first five of eight. 1 of 90 such captures did this.
#
# Two changes, again for two halves:
#
#   - `-B 32768`, a 32 MiB buffer, which is 128 frames. Each ~256 KiB frame
#     takes a 512 KiB block, so the ring maps 64 MiB; one tcpdump runs at a
#     time. The snaplen stays, so no datagram can be truncated.
#   - The drop count is read back. tcpdump prints it on exit, and a capture
#     that dropped anything, or whose count cannot be read, is discarded as
#     NLCAP_CAP_DROPPED rather than kept. libpcap shrinks the ring and retries
#     on ENOMEM without saying so, so the buffer alone is not a guarantee.
#
{ pkgs }:

let
  # xtcp2-nl-mcdump <inet|inet6|unspec> <multicast|anycast>
  #
  # One RTM_GETMULTICAST or RTM_GETANYCAST dump, for the addr-mcdump
  # captures (docs/netlink/addr-write-path.md, table 6). Those are the only
  # replies that carry IFA_MULTICAST, IFA_ANYCAST and IFA_MC_USERS, and the
  # pinned `ip` never asks for them: `ip maddr` reads /proc until iproute2
  # b8cdda55, which is in no release, and nothing in iproute2 sends
  # RTM_GETANYCAST. So this sends the request itself, the way
  # rtnl_mcaddrdump_req (lib/libnetlink.c:338) builds it: an nlmsghdr with
  # NLM_F_REQUEST | NLM_F_DUMP and a zeroed ifaddrmsg naming the family.
  #
  # What is captured is the kernel's reply, off nlmon0; this request is the
  # only byte string the fixture owes to us. stdout is one `<type> <count>`
  # line per message type, which `capio` keeps as the row's sidecar. A
  # NLMSG_ERROR answer is printed with its errno and exits 1.
  mcdump = pkgs.writers.writePython3Bin "xtcp2-nl-mcdump" { } ''
    import os
    import socket
    import struct
    import sys

    NETLINK_ROUTE = 0
    NLMSG_ERROR = 2
    NLMSG_DONE = 3
    NLM_F_REQUEST = 0x1
    NLM_F_DUMP = 0x300
    RTM_GETMULTICAST = 58
    RTM_GETANYCAST = 62

    FAMILIES = {"unspec": 0, "inet": socket.AF_INET, "inet6": socket.AF_INET6}
    KINDS = {"multicast": RTM_GETMULTICAST, "anycast": RTM_GETANYCAST}
    NAMES = {
        NLMSG_ERROR: "NLMSG_ERROR",
        NLMSG_DONE: "NLMSG_DONE",
        RTM_GETMULTICAST: "RTM_GETMULTICAST",
        RTM_GETANYCAST: "RTM_GETANYCAST",
    }


    def main():
        if len(sys.argv) != 3 or sys.argv[1] not in FAMILIES \
                or sys.argv[2] not in KINDS:
            sys.exit("usage: xtcp2-nl-mcdump <inet|inet6|unspec> "
                     "<multicast|anycast>")
        family = FAMILIES[sys.argv[1]]
        msg_type = KINDS[sys.argv[2]]

        s = socket.socket(socket.AF_NETLINK, socket.SOCK_RAW, NETLINK_ROUTE)
        s.bind((0, 0))
        seq = 1
        ifaddrmsg = struct.pack("=BBBBI", family, 0, 0, 0, 0)
        hdr = struct.pack("=IHHII", 16 + len(ifaddrmsg), msg_type,
                          NLM_F_REQUEST | NLM_F_DUMP, seq, os.getpid())
        s.send(hdr + ifaddrmsg)

        counts = {}
        done = False
        while not done:
            buf = s.recv(65536)
            off = 0
            while off + 16 <= len(buf):
                mlen, mtype, _flags, mseq, _pid = struct.unpack_from(
                    "=IHHII", buf, off)
                if mlen < 16 or mseq != seq:
                    break
                name = NAMES.get(mtype, str(mtype))
                counts[name] = counts.get(name, 0) + 1
                if mtype == NLMSG_ERROR:
                    (errno,) = struct.unpack_from("=i", buf, off + 16)
                    print("NLMSG_ERROR %d" % errno)
                    sys.exit(1)
                if mtype == NLMSG_DONE:
                    done = True
                    break
                off += (mlen + 3) & ~3
            if off + 16 > len(buf) and not buf:
                break
        for name in sorted(counts):
            print("%s %d" % (name, counts[name]))


    main()
  '';

  # xtcp2-nl-ipmon <canary-dev> <ip-args> <trigger> <until-ERE>...
  #
  # `ip <ip-args>` (a `monitor maddress acaddress`) in the background while
  # <trigger> runs, for the addr-monitor captures (A1-T02;
  # docs/netlink/addr-write-path.md). `ip monitor` is the only `ip` that
  # prints an RTM_NEWMULTICAST or RTM_NEWANYCAST message: accept_msg hands
  # them to print_addrinfo (ip/ipmonitor.c:156-160), so its stdout is the
  # oracle for `mcast ADDR` and `any ADDR`.
  #
  # Ready means subscribed, and that cannot be read off /proc/net/netlink:
  # RTNLGRP_IPV4_MCADDR, IPV6_MCADDR and IPV6_ACADDR are groups 37-39, past
  # the 32 bits its Groups column shows. So a canary group, 224.0.0.250 on
  # <canary-dev>, is joined until the monitor prints it, and left and joined
  # again if it does not within a second. Its events stay in the output; the
  # golden test pairs every notification with every block either way.
  #
  # Then <trigger> runs with `sh -c`, and the monitor is stopped once its
  # output matches every <until-ERE>. ip fflushes after each message
  # (lib/libnetlink.c:1278), so a line in the file is a message read.
  # SIGTERM, not SIGINT: a non-interactive shell starts a background job
  # with SIGINT ignored. stdout is the monitor's output; a timeout is
  # reported on stderr and exits 1.
  ipmon = pkgs.writeShellApplication {
    name = "xtcp2-nl-ipmon";
    runtimeInputs = with pkgs; [
      coreutils
      gnugrep
      iproute2
    ];
    text = ''
      [ $# -ge 4 ] || {
        echo "usage: xtcp2-nl-ipmon <canary-dev> <ip-args> <trigger> <until-ERE>..." >&2
        exit 2
      }
      canary_dev=$1
      read -ra ip_args <<<"$2"
      trigger=$3
      shift 3

      out=$(mktemp)
      trap 'rm -f "$out"' EXIT
      ip "''${ip_args[@]}" >"$out" 2>/dev/null &
      mpid=$!

      # wait_match <ERE> <tenths>: poll the monitor's output.
      wait_match() {
        local i
        for ((i = 0; i < $2 * 2; i++)); do
          grep -Eq -- "$1" "$out" && return 0
          sleep 0.05
        done
        return 1
      }

      canary=224.0.0.250
      ready=0
      for _ in 1 2 3 4 5 6 7 8 9 10; do
        ip addr add "$canary/32" dev "$canary_dev" autojoin
        if wait_match "mcast $canary " 10; then
          ready=1
          break
        fi
        ip addr del "$canary/32" dev "$canary_dev"
      done

      rc=0
      if [ "$ready" -ne 1 ]; then
        echo "xtcp2-nl-ipmon: the monitor never printed the canary" >&2
        rc=1
      else
        sh -c "$trigger" || rc=1
        for re in "$@"; do
          if ! wait_match "$re" 100; then
            echo "xtcp2-nl-ipmon: no match for $re" >&2
            rc=1
          fi
        done
      fi

      kill -TERM "$mpid" 2>/dev/null || true
      wait "$mpid" 2>/dev/null || true
      cat "$out"
      exit "$rc"
    '';
  };
in
pkgs.writeShellApplication {
  name = "xtcp2-nlcap";
  runtimeInputs = with pkgs; [
    mcdump
    ipmon
    coreutils
    gawk
    gnugrep
    gnutar
    gzip
    iproute2
    kmod
    rdma-core
    strace
    tcpdump
    util-linux
  ];
  text = ''
    set -euo pipefail

    # NLCAP_OUT exists for nix/checks/vm-lib-exp.nix, which runs this helper
    # in a build sandbox where /tmp is not the place to write. No guest sets it.
    OUT=''${NLCAP_OUT:-/tmp/nlcap}
    IFACE=nlmon0

    # Seconds to wait for tcpdump's socket to come up before giving up on a
    # capture attempt. Generous: this is a bound on a pathology, not a tuning
    # knob, and nothing waits the full time in the normal case.
    BIND_TIMEOUT=10

    # Seconds to wait for the stop sentinel to appear in the capture file.
    # Also a bound on a pathology rather than a tuning knob: measured in the
    # guest the sentinel is readable on the first poll.
    SENTINEL_TIMEOUT=10

    # The sentinel command, and the pcap filter that finds what it emitted.
    # Kept next to each other because they are one decision: change the
    # command and the protocol number changes with it. See the header for why
    # this particular command.
    SENTINEL_CMD='ss -x'
    SENTINEL_FILTER='ether[14:2]==4'

    # PROTOCOLS
    #
    # The netlink protocol a fixture keeps, as sll_protocol at offset 14 (see
    # capture() for how that offset was measured). `cap`, `capio`, `write` and
    # `filter` take `--proto <name>` ahead of their arguments; without it a
    # row is NETLINK_ROUTE, which is what every row before generic netlink and
    # XFRM was. The names are the ones a fixture's file name carries,
    # netlink_<name>_<what>.pcap, and capture() holds a row to that.
    #
    # The numbers are include/uapi/linux/netlink.h's: NETLINK_ROUTE 0,
    # NETLINK_XFRM 6, NETLINK_GENERIC 16. All three pass netlink_filter_tap()
    # (net/netlink/af_netlink.c), so nlmon sees them. None is 4, so the
    # sentinel is dropped from every fixture whatever its protocol.
    PROTO=0
    PROTO_NAME=route

    mkdir -p "$OUT"

    die() {
      echo "NLCAP_ERR $*" >&2
      exit 2
    }

    # set_proto <name> - set PROTO and PROTO_NAME, or die on a name that is
    # not one of the three.
    set_proto() {
      case "$1" in
        route) PROTO=0 ;;
        xfrm) PROTO=6 ;;
        generic) PROTO=16 ;;
        *) die "unknown netlink protocol '$1'; want route, generic or xfrm" ;;
      esac
      PROTO_NAME=$1
    }

    # nl_filter <in> <out> - keep only the records of protocol PROTO.
    nl_filter() {
      tcpdump -r "$1" -w "$2" "ether[14:2]==$PROTO" >/dev/null 2>&1 || true
    }

    # Poll a file until it contains a pattern, or the holder process dies, or
    # the deadline passes. Returns 0 only on a match.
    wait_for_line() {
      local file=$1 pattern=$2 pid=$3 deadline
      deadline=$(( $(date +%s) + BIND_TIMEOUT ))
      while [ "$(date +%s)" -lt "$deadline" ]; do
        if grep -q "$pattern" "$file" 2>/dev/null; then
          return 0
        fi
        if ! kill -0 "$pid" 2>/dev/null; then
          return 1
        fi
        # 50 ms, not a second: this is the inner loop of every capture and the
        # condition usually holds on the first or second pass.
        sleep 0.05
      done
      return 1
    }

    pkt_count() {
      local n
      n="$(tcpdump --count -r "$1" "''${2-}" 2>/dev/null | cut -d' ' -f1)" || n=""
      case "$n" in
        "" | *[!0-9]*) echo 0 ;;
        *) echo "$n" ;;
      esac
    }

    # tcpdump_drops <stderr-file>
    #
    # Print the count from tcpdump's exit line `N packet(s) dropped by
    # kernel`. Returns non-zero unless exactly one such line is there: a
    # tcpdump that died before printing its statistics has no count, and that
    # is not the same as 0. `dropped by interface`, which some builds also
    # print, is the driver's counter and not this one.
    tcpdump_drops() {
      awk 'NF == 5 && $1 ~ /^[0-9]+$/ && $2 ~ /^packets?$/ && $3 == "dropped" &&
        $4 == "by" && $5 == "kernel" { n = $1; c++ }
        END { if (c != 1) exit 1; print n }' "$1"
    }

    # Poll a pcap that tcpdump is still writing until it holds at least one
    # datagram matching <filter>. Returns 0 only on a match.
    #
    # Reading a file under an active writer is safe here because tcpdump is run
    # with -U, which flushes after each packet, so a record is either wholly
    # present or wholly absent. A read that catches the file mid-record makes
    # `tcpdump -r` fail, pkt_count returns 0, and this polls again - so the
    # torn-record case costs one iteration rather than a wrong answer.
    wait_for_pkts() {
      local file=$1 filter=$2 deadline n
      deadline=$(( $(date +%s) + SENTINEL_TIMEOUT ))
      while [ "$(date +%s)" -lt "$deadline" ]; do
        n="$(pkt_count "$file" "$filter")"
        if [ "$n" -ge 1 ]; then
          return 0
        fi
        sleep 0.05
      done
      return 1
    }

    # --- subcommands ------------------------------------------------------

    # ns-add <ns>
    #
    # A throwaway namespace with its own nlmon0, and the belt-and-braces half
    # of the design. The guest is already quiet, so isolation is not strictly
    # required - but netlink taps are PER-NETNS (af_netlink.c registers
    # netlink_tap_net_ops as pernet_operations, and netlink_deliver_tap()
    # takes its tap list from net_generic(net, netlink_tap_net_id)), so an
    # nlmon0 created in here cannot see the guest's own systemd/networkd
    # traffic at all. Capture pollution is absent rather than filtered, and it
    # stays absent even if a later flavor change makes the guest chattier.
    cmd_ns_add() {
      local ns=$1
      modprobe nlmon 2>/dev/null || true
      modprobe dummy 2>/dev/null || true
      modprobe veth 2>/dev/null || true
      modprobe bridge 2>/dev/null || true

      ip netns del "$ns" 2>/dev/null || true
      ip netns add "$ns" || die "cannot create netns $ns"
      ip -n "$ns" link set lo up || die "cannot bring up lo in $ns"
      ip -n "$ns" link add "$IFACE" type nlmon || die "cannot create $IFACE in $ns"
      ip -n "$ns" link set dev "$IFACE" up || die "cannot bring up $IFACE in $ns"
      echo "NLCAP_NS_OK $ns"
    }

    # ns-del <ns>
    cmd_ns_del() {
      local ns=$1
      ip netns del "$ns" 2>/dev/null || true
      echo "NLCAP_NS_DELETED $ns"
    }

    # topo <ns> <subdir> <ip-args...>
    #
    # One piece of topology, recorded in a transcript that ships with the
    # pcaps so they describe their own provenance. A step is allowed to fail
    # and says so in its exit status: `route add ... via inet6` needs RFC 5549
    # support, and a kernel without it should still yield every other fixture.
    # The driver decides whether a given failure matters.
    #
    # THE SUBDIR IS A PARAMETER BECAUSE ONE SHARED TRANSCRIPT WAS WRONG
    #
    # This used to append every namespace's steps to a single $OUT/topology,
    # leaving the driver to copy it into place per capture set with
    # `side <ns> <subdir> topology 'cat /tmp/nlcap/topology'`. That is broken
    # twice over. The copy is self-referential for the clean set, whose subdir
    # is `.`: cmd_side redirects into $OUT/./topology, which IS the file being
    # cat'ed, so the shell truncates it before cat reads a byte. The clean
    # transcript was destroyed, the mesh phase then appended its own steps to
    # the emptied file, and the clean fixtures shipped a provenance record
    # describing a bridge-and-veth topology they were not captured on. A
    # sidecar that confidently describes the wrong thing is worse than a
    # missing one, and nothing about it looks like a failure.
    #
    # Writing to $OUT/$subdir/topology puts each transcript where it belongs
    # on the first pass, so there is no copy to get wrong and no window in
    # which two capture sets share a file.
    #
    # <ns> `-` is the root namespace, as for side. lacp-pair-capture needs it
    # for one kind of step: moving a NIC, which exists only in the root
    # namespace, into the one the capture runs in.
    cmd_topo() {
      local ns=$1 subdir=$2
      shift 2
      mkdir -p "$OUT/$subdir"
      local nsarg=(-n "$ns")
      if [ "$ns" = "-" ]; then
        nsarg=()
      fi
      if ip "''${nsarg[@]}" "$@" 2>/dev/null; then
        printf 'ok    [%s] ip %s\n' "$ns" "$*" >>"$OUT/$subdir/topology"
        echo "NLCAP_TOPO_OK $*"
        return 0
      fi
      printf 'FAIL  [%s] ip %s\n' "$ns" "$*" >>"$OUT/$subdir/topology"
      echo "NLCAP_TOPO_FAIL $*"
      return 1
    }

    # cap <ns> <subdir> <name> <min-datagrams> <command>
    #
    # <command> is one shell word, run with `sh -c` INSIDE <ns>. A capture
    # that comes in under its floor is retried once and then DISCARDED rather
    # than written, so a short capture cannot silently replace a good fixture.
    cmd_cap() {
      capture "$1" "$2" "$3" "$4" "$5" -
    }

    # capio <ns> <subdir> <name> <min-datagrams> <command> <stdout-name>
    #
    # `cap` plus the generator's stdout, recorded to <stdout-name> in the same
    # directory.
    #
    # THE POINT IS THAT IT IS THE SAME RUN
    #
    # The parity comparator needs both halves of each side: the netlink the
    # tool sent, and what it rendered from the replies. Netlink parity alone
    # is reply-independent - a tool that sends byte-identical requests and then
    # discards every reply is a perfect green on the wire - which is why
    # internal/goipparity compares stdout structurally as well, and why it
    # wants <slug>.<side>.pcap and <slug>.<side>.out to describe ONE
    # invocation.
    #
    # Capturing them separately would mean running the command twice, against
    # a kernel that had moved on in between, so a difference between the pcap
    # and the text would be indistinguishable from a rendering bug. Recording
    # both from one run removes that question rather than bounding it.
    #
    # Writing the file does not pollute the capture: an open/write/close on a
    # tmpfs path produces no netlink traffic, so the window still contains
    # exactly what the tool asked the kernel.
    cmd_capio() {
      local ns=$1 subdir=$2 name=$3 min=$4 command=$5 sout=$6
      if [ "$sout" = "-" ]; then
        die "capio needs a stdout name; use cap to discard stdout"
      fi
      capture "$ns" "$subdir" "$name" "$min" "$command" "$sout"
    }

    # write <ns> <subdir> <name> <min-datagrams> <command>
    #
    # One write row of the link-write corpus: `capio` plus the generator's
    # stderr and exit status, as <name>.out, <name>.err and <name>.rc beside
    # <name>.pcap. The status and stderr are the point: a write row's
    # expected outcome is what the kernel answered, and a rejected write's
    # extack message reaches the operator only on `ip`'s stderr.
    #
    # ONE ATTEMPT, NOT TWO
    #
    # A read can be retried because running it again asks the same question
    # of the same kernel. A write cannot: the first attempt has already
    # changed the namespace, so a second `link add bond0` meets -EEXIST and
    # records a different transaction under the same name. A write that
    # comes in under its floor is discarded and reported, and the driver
    # rebuilds the namespace if it wants another go.
    #
    # THE COMMAND RUNS FROM THE ROW'S OWN DIRECTORY
    #
    # So a relative path in it names a file in the fixture: `ip addr save >
    # ip.save` leaves the saved bytes beside the pcap of the dump that made
    # them, and `ip addr restore < ip.save` reads the copy the row ships.
    # Run from the console's directory, the save would land outside $OUT and
    # never be packed. A plain `ip` write opens no file, so for the link and
    # route rows this changes nothing.
    cmd_write() {
      capture "$1" "$2" "$3" "$4" "$5" "$3.out" write
    }

    # capture — the shared body of cap, capio and write.
    #
    # <sout> is a file name to record the generator's stdout to, or `-` to
    # discard it. <mode> is `write` for cmd_write, which also keeps stderr and
    # the exit status and takes one attempt; anything else is a read. Factored out rather than duplicated because everything that
    # makes a capture correct lives in here: the bind wait, immediate mode, the
    # stop sentinel, the NETLINK_ROUTE filter and the floor. A second copy of
    # that for the parity captures would be a second place for the sentinel
    # logic to drift, and the symptom of drift is an empty pcap that looks
    # like a clean run.
    capture() {
      local ns=$1 subdir=$2 name=$3 min=$4 command=$5 sout=$6 mode=''${7:-read}
      local dir="$OUT/$subdir"

      # A fixture named for one protocol and filtered to another would come
      # back empty or, worse, hold the wrong family's records under the right
      # name. Names outside the netlink_ scheme (a write row's `ip`, a parity
      # row's <slug>.<side>) say nothing about the protocol and are not held.
      case "$name" in
        netlink_"$PROTO_NAME"_*) ;;
        netlink_*) die "fixture $name does not match --proto $PROTO_NAME; want netlink_''${PROTO_NAME}_<what>" ;;
      esac
      mkdir -p "$dir"

      local raw="$dir/.$name.raw.pcap"
      local new="$dir/.$name.new.pcap"
      # Staged under a dot name and moved into place only with the pcap it
      # belongs to. A .out left behind by a discarded attempt would pair a
      # fresh text with a stale or absent capture, and the comparator reads
      # the two as one observation.
      local outnew="$dir/.$name.new.out"
      local errnew="$dir/.$name.new.err"
      local attempt=0 attempts=2 err tp n=0 rc=0 drops
      if [ "$mode" = write ]; then
        attempts=1
      fi

      while [ "$attempt" -lt "$attempts" ]; do
        attempt=$((attempt + 1))
        err="$(mktemp)"
        # --immediate-mode: without it libpcap's TPACKET_V3 ring holds a block
        # for up to its 1000 ms retire timeout, so a low-rate capture is not
        # readable by tcpdump when SIGINT arrives and the pcap comes back
        # empty. See the header - this is the difference between 2 of 18
        # captures passing and all of them passing.
        # -B 32768: a ring of 128 frames, not 8. See the header.
        ip netns exec "$ns" \
          tcpdump -i "$IFACE" -w "$raw" -U -q --immediate-mode -B 32768 >/dev/null 2>"$err" &
        tp=$!

        if ! wait_for_line "$err" "listening on" "$tp"; then
          kill -INT "$tp" 2>/dev/null || true
          wait "$tp" 2>/dev/null || true
          echo "NLCAP_CAP_NOBIND $name attempt=$attempt" >&2
          cat "$err" >&2 || true
          rm -f "$err" "$raw" "$outnew" "$errnew"
          continue
        fi
        # $err stays: tcpdump prints its drop count there on exit.

        # The generator's own status is deliberately ignored: `ip neigh show`
        # on an empty table succeeds, `ip -6 route show` on a v4-only setup
        # succeeds, and a genuine failure shows up as a missed floor below,
        # which is a better signal than a shell exit code.
        #
        # stderr goes to /dev/null in both branches, never into the recorded
        # text. The comparator diffs stdout structurally - line counts and
        # extracted name/CIDR/MAC sets - so a warning on stderr folded into
        # the file would read as a rendered object that the other side lacks.
        #
        # A write keeps both, in separate files, and its status: see cmd_write.
        rc=0
        if [ "$mode" = write ]; then
          ( cd "$dir" && ip netns exec "$ns" sh -c "$command" ) >"$outnew" 2>"$errnew" || rc=$?
        elif [ "$sout" = "-" ]; then
          ip netns exec "$ns" sh -c "$command" >/dev/null 2>&1 || true
        else
          ip netns exec "$ns" sh -c "$command" >"$outnew" 2>/dev/null || true
        fi

        # Close the window on evidence, not on elapsed time. The sentinel is
        # emitted after the generator has exited, so the ring being FIFO means
        # that once the sentinel is in the file, every datagram the generator
        # provoked is in the file ahead of it. A failure here is reported
        # rather than swallowed: it degrades this attempt to "stop whenever",
        # which is the behavior that produced empty pcaps, so it must not look
        # like a clean run.
        ip netns exec "$ns" sh -c "$SENTINEL_CMD" >/dev/null 2>&1 || true
        if ! wait_for_pkts "$raw" "$SENTINEL_FILTER"; then
          echo "NLCAP_CAP_NOSENTINEL $name attempt=$attempt" >&2
        fi

        kill -INT "$tp" 2>/dev/null || true
        wait "$tp" 2>/dev/null || true
        drops="$(tcpdump_drops "$err")" || drops=unknown
        rm -f "$err"

        # Filter to the row's protocol (NETLINK_ROUTE unless --proto said
        # otherwise; see PROTOCOLS), which has two jobs: it removes the stop
        # sentinel, and it still asserts that nothing ELSE was on the wire in
        # here.
        #
        # The offset is measured, not assumed. libpcap maps ARPHRD_NETLINK to
        # DLT_NETLINK in cooked mode, so each record carries a 16-byte
        # Linux-SLL header ahead of the nlmsghdr, and a captured first record
        # begins
        #
        #     0004 0338 0000 0000 0000 0000 0000 0000
        #
        # which is sll_pkttype=4 (PACKET_OUTGOING), sll_hatype=0x338
        # (ARPHRD_NETLINK), sll_halen=0, eight address bytes, then
        # sll_protocol at offset 14 (0 here). `ether[N:2]` is a raw offset from the
        # start of the link-layer header, so it reads that field whether the
        # filter is compiled against a live device or a savefile.
        #
        # Note this is a filter, NOT a rescue: a fixture is meant to hold one
        # transaction, so anything it drops other than the sentinel would be a
        # capture-hygiene problem worth knowing about rather than hiding.
        nl_filter "$raw" "$new"
        rm -f "$raw"

        n="$(pkt_count "$new")"
        # A dropped datagram is a hole in the transaction, wherever it fell,
        # so the attempt is discarded even if it cleared its floor. A write
        # has one attempt and so ends NLCAP_CAP_SHORT.
        if [ "$drops" != 0 ]; then
          rm -f "$new" "$outnew" "$errnew"
          echo "NLCAP_CAP_DROPPED $subdir/$name attempt=$attempt datagrams=$n dropped=$drops" >&2
          continue
        fi
        if [ "$n" -ge "$min" ]; then
          mv -f "$new" "$dir/$name.pcap"
          if [ "$sout" != "-" ]; then
            # Both halves land together or neither does. `mv` after the pcap's
            # mv, so a crash between them leaves a pcap with no text - which
            # the comparator reports as MISSING - rather than a text with no
            # pcap, which it would have to guess about.
            mv -f "$outnew" "$dir/$sout"
            if [ "$mode" = write ]; then
              mv -f "$errnew" "$dir/$name.err"
              echo "$rc" >"$dir/$name.rc"
              echo "NLCAP_WRITE_OK $subdir/$name datagrams=$n floor=$min rc=$rc"
              return 0
            fi
            echo "NLCAP_CAP_OK $subdir/$name datagrams=$n floor=$min stdout=$subdir/$sout bytes=$(wc -c <"$dir/$sout")"
            return 0
          fi
          echo "NLCAP_CAP_OK $subdir/$name datagrams=$n floor=$min"
          return 0
        fi
        rm -f "$new" "$outnew" "$errnew"
        echo "NLCAP_CAP_RETRY $name attempt=$attempt datagrams=$n floor=$min" >&2
      done

      echo "NLCAP_CAP_SHORT $subdir/$name datagrams=$n floor=$min"
      return 1
    }

    # side <ns> <subdir> <name> <command>
    #
    # <ns> may be `-` for the guest's root namespace, which is what `uname`
    # and `ip -V` want.
    cmd_side() {
      local ns=$1 subdir=$2 name=$3 command=$4
      local dir="$OUT/$subdir"
      mkdir -p "$dir"
      if [ "$ns" = "-" ]; then
        sh -c "$command" >"$dir/$name" 2>/dev/null || true
      else
        ip netns exec "$ns" sh -c "$command" >"$dir/$name" 2>/dev/null || true
      fi
      echo "NLCAP_SIDE_OK $subdir/$name bytes=$(wc -c <"$dir/$name")"
    }

    # wait-lacp <ns> <port> <actor-mask> <actor-want> <partner-mask> <partner-want> <timeout-sec>
    #
    # Poll one bond port's LACP state until
    #
    #     (actor & actor-mask) == actor-want && (partner & partner-mask) == partner-want
    #
    # or the deadline passes. The masks and values are the driver's: this
    # knows how to read a port's state, not which state any capture wants.
    #
    # It is the link-write flavor's version of the stop sentinel above. A
    # bond's LACP state is a function of timers - current_while expiry before
    # DEFAULTED, the aggregator wait before ATTACHED, one LACPDU a second each
    # way with lacp_rate fast - so a readback taken after a fixed sleep is a
    # readback of whichever state the race landed on. This closes the window
    # on the state itself.
    #
    # The state is read from SYSFS, not from `ip -d -j link show`, for two
    # reasons. It keeps the poll off the wire: sysfs reads produce no netlink
    # traffic, so a poll can never leak into a capture window. And it is an
    # oracle independent of the thing under test - iproute2 reads
    # IFLA_BOND_SLAVE_AD_PARTNER_OPER_PORT_STATE one byte wide, so on a
    # big-endian guest `ip` would report partner state 0 forever and this
    # would time out on a converged bond (see
    # docs/netlink/upstream/iproute2-bond-partner-state-width.md). sysfs
    # prints the u16 struct field directly.
    #
    # `ip netns exec` remounts /sys for the target namespace, which is what
    # makes /sys/class/net/<port> resolve to that namespace's port.
    cmd_wait_lacp() {
      local ns=$1 port=$2 amask=$3 awant=$4 pmask=$5 pwant=$6 timeout=$7
      local dir="/sys/class/net/$port/bonding_slave"
      local start deadline a p
      start=$(date +%s)
      deadline=$(( start + timeout ))
      a=""
      p=""
      while :; do
        a="$(ip netns exec "$ns" cat "$dir/ad_actor_oper_port_state" 2>/dev/null)" || a=""
        p="$(ip netns exec "$ns" cat "$dir/ad_partner_oper_port_state" 2>/dev/null)" || p=""
        case "$a$p" in
          "" | *[!0-9]*) ;;
          *)
            if [ $(( a & amask )) -eq $(( awant )) ] && [ $(( p & pmask )) -eq $(( pwant )) ]; then
              printf 'NLCAP_LACP_OK %s/%s actor=0x%02x partner=0x%02x elapsed=%ss\n' \
                "$ns" "$port" "$a" "$p" "$(( $(date +%s) - start ))"
              return 0
            fi
            ;;
        esac
        if [ "$(date +%s)" -ge "$deadline" ]; then
          break
        fi
        # 100 ms: the bond's state machines tick every 100 ms
        # (AD_TIMER_INTERVAL), so polling faster cannot see a change sooner.
        sleep 0.1
      done
      echo "NLCAP_LACP_TIMEOUT $ns/$port actor=$a partner=$p want actor&$amask==$awant partner&$pmask==$pwant after ''${timeout}s"
      return 1
    }

    # wait-sysfs <ns> <file> <want> <timeout-sec>
    #
    # Poll one sysfs file in <ns> until its content is exactly <want> or the
    # deadline passes. wait-lacp's sibling for the states the LACP bits do
    # not show: a port that loses carrier keeps the actor and partner state
    # it had (nothing on the carrier-down path rewrites them), so "la2 is out
    # of the aggregate" has to be read from the port's mii_status and slave
    # state instead. Same reasons for sysfs as wait-lacp: no netlink traffic,
    # and no dependence on the decoder under test.
    cmd_wait_sysfs() {
      local ns=$1 file=$2 want=$3 timeout=$4
      local start deadline v
      start=$(date +%s)
      deadline=$(( start + timeout ))
      v=""
      while :; do
        v="$(ip netns exec "$ns" cat "$file" 2>/dev/null)" || v=""
        if [ "$v" = "$want" ]; then
          echo "NLCAP_SYSFS_OK $ns:$file=$v elapsed=$(( $(date +%s) - start ))s"
          return 0
        fi
        if [ "$(date +%s)" -ge "$deadline" ]; then
          break
        fi
        sleep 0.1
      done
      echo "NLCAP_SYSFS_TIMEOUT $ns:$file=$v want $want after ''${timeout}s"
      return 1
    }

    # wait-addr-flag <ns> <dev> <addr32|*> <mask> <want> <timeout-sec>
    #
    # Poll /proc/net/if_inet6 in <ns> until a line for <dev> - and for
    # <addr32>, or any address with `*` - has (flags & mask) == want, or the
    # deadline passes. <mask> and <want> are hex; <addr32> is the file's own
    # form, 32 lowercase hex digits with no colons, which the driver computes
    # (addrtopo::v6hex) so nothing here parses an IPv6 literal.
    #
    # The DAD states' sibling of wait-lacp and wait-sysfs, and /proc for the
    # same two reasons: no netlink traffic, so a poll can never land in a
    # capture window, and no dependence on the decoder under test. The fifth
    # field is `(u8)ifp->flags` (net/ipv6/addrconf.c if6_seq_show), which is
    # exactly the byte that carries TEMPORARY 0x01, OPTIMISTIC 0x04, DADFAILED
    # 0x08 and TENTATIVE 0x40 - every flag a DAD state waits on.
    #
    # Always polls at least once, so a timeout of 0 is "is it true now".
    # Device and address are compared as whole fields: v0 never matches v01.
    #
    # NLCAP_IF_INET6 replaces the path for nix/checks/vm-lib-exp.nix, which
    # has no IPv6 addresses of its own to read. <ns> `-` is the root
    # namespace, as for topo; that is the only way the check can use it.
    cmd_wait_addr_flag() {
      local usage="usage: wait-addr-flag <ns> <dev> <addr32|*> <mask> <want> <timeout-sec>"
      [ $# -eq 6 ] || die "$usage"
      local ns=$1 dev=$2 addr=$3 mask=''${4#0x} want=''${5#0x} timeout=$6
      local file=''${NLCAP_IF_INET6:-/proc/net/if_inet6}
      case "$mask" in "" | *[!0-9a-fA-F]*) die "wait-addr-flag: mask not hex: $4" ;; esac
      case "$want" in "" | *[!0-9a-fA-F]*) die "wait-addr-flag: want not hex: $5" ;; esac
      case "$timeout" in "" | *[!0-9]*) die "wait-addr-flag: timeout not a number: $6" ;; esac
      case "$addr" in
        "*") ;;
        *[!0-9a-f]*) die "wait-addr-flag: address not 32 lowercase hex digits: $addr" ;;
        *) [ "''${#addr}" -eq 32 ] || die "wait-addr-flag: address not 32 lowercase hex digits: $addr" ;;
      esac
      local run=(ip netns exec "$ns")
      if [ "$ns" = "-" ]; then
        run=()
      fi
      local start deadline lines seen a f name
      start=$(date +%s)
      deadline=$(( start + timeout ))
      while :; do
        seen=absent
        lines="$("''${run[@]}" cat "$file" 2>/dev/null)" || lines=""
        while read -r a _ _ _ f name; do
          if [ "$name" != "$dev" ]; then
            continue
          fi
          if [ "$addr" != "*" ] && [ "$a" != "$addr" ]; then
            continue
          fi
          case "$f" in "" | *[!0-9a-fA-F]*) continue ;; esac
          seen="0x$f"
          if [ $(( 16#$f & 16#$mask )) -eq $(( 16#$want )) ]; then
            echo "NLCAP_ADDR_FLAG_OK $ns/$dev $addr flags=$seen elapsed=$(( $(date +%s) - start ))s"
            return 0
          fi
        done <<<"$lines"
        if [ "$(date +%s)" -ge "$deadline" ]; then
          break
        fi
        # 100 ms, as wait-sysfs: DAD's own timer is a second per probe
        # (ND_RETRANS_TIMER, include/net/ndisc.h), so polling faster cannot
        # see a change sooner.
        sleep 0.1
      done
      echo "NLCAP_DAD_TIMEOUT $ns/$dev $addr flags=$seen want flags&0x$mask==0x$want after ''${timeout}s"
      return 1
    }

    # sysctl <ns> <subdir> <key> <value>
    #
    # Write one sysctl inside <ns> and record it in $OUT/<subdir>/topology in
    # topo's format, so a transcript lists every step a state was built from
    # and not only the `ip` ones. The optimistic and temporary address states
    # need it (use_tempaddr, optimistic_dad, dad_transmits); `ip` has no verb
    # for any of them.
    #
    # <key> is dotted, as sysctl(8) takes it, and maps to /proc/sys by
    # turning every dot into a slash - so an interface name with a dot in it
    # (a VLAN like eth0.100) cannot be addressed this way. None of the states
    # use one. Written with `ip netns exec`, which is what makes /proc/sys/net
    # resolve to <ns>'s copy; `-` is the root namespace.
    cmd_sysctl() {
      [ $# -eq 4 ] || die "usage: sysctl <ns> <subdir> <key> <value>"
      local ns=$1 subdir=$2 key=$3 value=$4
      case "$key" in "" | .* | *..* | *[!A-Za-z0-9._-]*) die "sysctl: bad key: $key" ;; esac
      mkdir -p "$OUT/$subdir"
      local run=(ip netns exec "$ns")
      if [ "$ns" = "-" ]; then
        run=()
      fi
      if printf '%s\n' "$value" | "''${run[@]}" tee "/proc/sys/''${key//.//}" >/dev/null 2>&1; then
        printf 'ok    [%s] sysctl %s=%s\n' "$ns" "$key" "$value" >>"$OUT/$subdir/topology"
        echo "NLCAP_SYSCTL_OK $key=$value"
        return 0
      fi
      printf 'FAIL  [%s] sysctl %s=%s\n' "$ns" "$key" "$value" >>"$OUT/$subdir/topology"
      echo "NLCAP_SYSCTL_FAIL $key=$value"
      return 1
    }

    # modules <subdir> <module>...
    #
    # Load each module in turn and record what was loaded in
    # $OUT/<subdir>/modules, one `<module> loaded|builtin` line each. A module
    # modprobe accepts but /proc/modules does not list is built in, which is
    # still a pass: the kernel has the code either way.
    #
    # A driver calls this once, after every row that existing fixtures came
    # from, and never at boot. A module is kernel-wide: one that creates a
    # per-namespace fallback device (fou, ip6_tunnel, ...) creates it in every
    # namespace, including ones already captured, and loading the tunnel
    # modules at boot was measured to move goip0 from ifindex 3 to 10 (see
    # boot.kernelModules in nix/microvms/mkVm.nix).
    #
    # The first failure stops the load: it prints NLCAP_MOD_FAIL <module>,
    # exits 1 and writes no sidecar, so a sidecar that exists lists every
    # module asked for. Names are what /proc/modules prints, underscores and
    # not dashes, so the loaded check can match them exactly.
    cmd_modules() {
      [ $# -ge 2 ] || die "usage: modules <subdir> <module>..."
      local subdir=$1 mod lines=""
      shift
      for mod in "$@"; do
        case "$mod" in "" | *[!a-z0-9_]*) die "modules: bad module name: $mod" ;; esac
      done
      for mod in "$@"; do
        if ! modprobe "$mod" >/dev/null 2>&1; then
          echo "NLCAP_MOD_FAIL $mod"
          return 1
        fi
        if grep -q "^$mod " /proc/modules; then
          lines+="$mod loaded"$'\n'
        else
          lines+="$mod builtin"$'\n'
        fi
      done
      mkdir -p "$OUT/$subdir"
      printf '%s' "$lines" >"$OUT/$subdir/modules"
      echo "NLCAP_MOD_OK $subdir/modules n=$#"
    }

    # note <subdir> <text...>
    #
    # One free-text line in $OUT/<subdir>/topology. The driver writes one when
    # it drops a state the kernel would not produce, so the transcript says
    # why a fixture is missing instead of leaving a silent gap.
    cmd_note() {
      [ $# -ge 2 ] || die "usage: note <subdir> <text...>"
      local subdir=$1
      shift
      mkdir -p "$OUT/$subdir"
      printf 'note  %s\n' "$*" >>"$OUT/$subdir/topology"
      echo "NLCAP_NOTE_OK"
    }

    # wait-churn <ns> <ports> <timeout-sec> [none|settled]
    #
    # Poll /proc/net/bonding/bond0 in <ns> until all <ports> ports show both
    # churn machines settled. With `none`, the default, settled is
    # "Churn State: none" (AD_NO_CHURN) on every machine; with `settled` it is
    # anything but "monitoring", so a machine that ended at AD_CHURN
    # ("churned"), as a port whose partner never syncs does, counts too. A port that
    # joins, or comes back from EXPIRED or DEFAULTED, re-arms the machine to
    # MONITOR for AD_CHURN_DETECTION_TIME, 60 s (bond_3ad.c ad_churn_machine),
    # so a capture taken inside that window reads MONITOR and one taken after
    # it reads NO_CHURN, and which one you get depends on how long the run
    # took. Waiting it out makes the field the same on every run. The churn
    # state is in netlink and /proc only, not sysfs; /proc, so nothing here
    # reads it through the decoder under test.
    cmd_wait_churn() {
      local ns=$1 ports=$2 timeout=$3 mode=''${4:-none}
      local start deadline f total settled
      case "$mode" in
        none|settled) ;;
        *) die "wait-churn: mode must be none or settled, not $mode" ;;
      esac
      start=$(date +%s)
      deadline=$(( start + timeout ))
      total=0
      settled=0
      while :; do
        f="$(ip netns exec "$ns" cat /proc/net/bonding/bond0 2>/dev/null)" || f=""
        total=$(printf '%s\n' "$f" | grep -c 'Churn State:' || true)
        if [ "$mode" = none ]; then
          settled=$(printf '%s\n' "$f" | grep -c 'Churn State: none' || true)
        else
          settled=$(printf '%s\n' "$f" | grep 'Churn State:' | grep -vc 'Churn State: monitoring' || true)
        fi
        if [ "$total" -eq $(( 2 * ports )) ] && [ "$settled" -eq "$total" ]; then
          echo "NLCAP_CHURN_OK $ns ports=$ports mode=$mode elapsed=$(( $(date +%s) - start ))s"
          return 0
        fi
        if [ "$(date +%s)" -ge "$deadline" ]; then
          break
        fi
        sleep 0.5
      done
      echo "NLCAP_CHURN_TIMEOUT $ns $settled of $total churn machines settled ($mode), want $(( 2 * ports )), after ''${timeout}s"
      return 1
    }

    # THE RDMA CAPTURES ARE STRACE, NOT NLMON
    #
    # nlmon cannot see NETLINK_RDMA. netlink_filter_tap()
    # (net/netlink/af_netlink.c) mirrors a fixed list of protocols - ROUTE,
    # USERSOCK, SOCK_DIAG, NFLOG, XFRM, FIB_LOOKUP, NETFILTER, GENERIC - and
    # every other protocol is dropped before it reaches a tap, so a tcpdump on
    # nlmon0 around `rdma dev show` records nothing at all. The bytes the tool
    # sent and received are instead taken from its own syscalls: strace's
    # `-e write=all -e read=all` hex-dumps every sendmsg/recvmsg buffer, and
    # pkg/xtcpnl reads those dumps back into datagrams. Same oracle - what
    # iproute2 put on the socket and what the kernel answered - recorded one
    # layer up.
    #
    # `socket` is traced too: it is what ties a file descriptor to
    # NETLINK_RDMA, so a reader can keep that protocol's traffic and nothing
    # else.
    STRACE_ARGS=(-qq -f -e "trace=socket,sendto,sendmsg,recvfrom,recvmsg" -e write=all -e read=all)

    in_ns() {
      local ns=$1
      shift
      if [ "$ns" = "-" ]; then
        "$@"
      else
        ip netns exec "$ns" "$@"
      fi
    }

    # strace <ns> <subdir> <name> <command>
    #
    # One RDMA row: <command> run under strace in <ns> (`-` is the VM's own
    # namespace), recorded as <name>.strace, <name>.out, <name>.err and
    # <name>.rc. One attempt: a read row's outcome, error or not, is the
    # observation.
    cmd_strace() {
      local ns=$1 subdir=$2 name=$3 command=$4
      local dir="$OUT/$subdir" rc=0 n
      mkdir -p "$dir"
      in_ns "$ns" strace "''${STRACE_ARGS[@]}" -o "$dir/$name.strace" \
        sh -c "$command" >"$dir/$name.out" 2>"$dir/$name.err" || rc=$?
      echo "$rc" >"$dir/$name.rc"
      n=$(grep -c 'bytes in buffer' "$dir/$name.strace" || true)
      echo "NLCAP_STRACE_OK $subdir/$name rc=$rc buffers=$n"
    }

    # strace-monitor <ns> <subdir> <name> <trigger> <until>
    #
    # `rdma monitor` under strace while <trigger> runs, stopped once its
    # stdout shows <until>. Both waits are polls on the condition that
    # matters: the trigger starts only once a NETLINK_RDMA socket in the
    # namespace holds group RDMA_NL_GROUP_NOTIFY (4, so bit 0x8 of
    # /proc/net/netlink's Groups column), and the monitor is stopped only once
    # it has printed the last event the trigger provokes. rdma fflushes after
    # every event (rdma/monitor.c), so the line being there means the
    # datagram has been read.
    #
    # The trigger itself is not traced: only the monitor's own socket is the
    # row.
    cmd_strace_monitor() {
      local ns=$1 subdir=$2 name=$3 trigger=$4 until=$5
      local dir="$OUT/$subdir" spid child deadline joined=0 seen=0 rc=0 n
      mkdir -p "$dir"
      # Not through in_ns: a backgrounded function is a subshell, and $! would
      # name it rather than strace. `ip netns exec` execs, so here $! is
      # strace and rdma is its child.
      if [ "$ns" = "-" ]; then
        strace "''${STRACE_ARGS[@]}" -o "$dir/$name.strace" \
          rdma monitor >"$dir/$name.out" 2>"$dir/$name.err" &
      else
        ip netns exec "$ns" strace "''${STRACE_ARGS[@]}" -o "$dir/$name.strace" \
          rdma monitor >"$dir/$name.out" 2>"$dir/$name.err" &
      fi
      spid=$!

      deadline=$(( $(date +%s) + BIND_TIMEOUT ))
      while [ "$(date +%s)" -lt "$deadline" ] && kill -0 "$spid" 2>/dev/null; do
        if in_ns "$ns" cat /proc/net/netlink | grep -Eq '^[0-9a-f]+ +20 +[0-9]+ +[0-9a-f]*[89abcdef]( |$)'; then
          joined=1
          break
        fi
        sleep 0.05
      done

      # A kernel before RDMA_NLDEV_SYS_ATTR_MONITOR_MODE (6.6, say) sends
      # SYS_GET's reply without it, and rdma refuses before joining the group
      # (rdma/monitor.c). That refusal is the row on such a kernel.
      if [ "$joined" -ne 1 ] && ! kill -0 "$spid" 2>/dev/null &&
        grep -qxF "RDMA monitoring is not supported by the kernel" "$dir/$name.err"; then
        wait "$spid" || rc=$?
        echo "$rc" >"$dir/$name.rc"
        echo "NLCAP_MONITOR_UNSUPPORTED $subdir/$name rc=$rc"
        return 0
      fi
      if [ "$joined" -ne 1 ]; then
        echo "NLCAP_MONITOR_NOJOIN $subdir/$name" >&2
      fi

      in_ns "$ns" timeout 20 sh -c "$trigger" >/dev/null 2>&1 || true

      deadline=$(( $(date +%s) + SENTINEL_TIMEOUT ))
      while [ "$(date +%s)" -lt "$deadline" ]; do
        if grep -qF "$until" "$dir/$name.out" 2>/dev/null; then
          seen=1
          break
        fi
        sleep 0.05
      done

      # SIGTERM to rdma, not to strace: strace would detach and leave the
      # monitor running. Not SIGINT: a non-interactive shell starts a
      # background job with SIGINT ignored, and exec keeps it ignored, so
      # rdma would never see it. The children are found by parent pid in
      # /proc/*/stat rather than /proc/<pid>/task/<pid>/children, which
      # needs CONFIG_PROC_CHILDREN. The wait is bounded; a monitor that
      # outlives SIGTERM is killed with strace, and the row reports short.
      child=$(awk -v p="$spid" '$4 == p { print $1 }' /proc/[0-9]*/stat 2>/dev/null || true)
      for c in $child; do
        kill -TERM "$c" 2>/dev/null || true
      done
      deadline=$(( $(date +%s) + 5 ))
      while kill -0 "$spid" 2>/dev/null && [ "$(date +%s)" -lt "$deadline" ]; do
        sleep 0.05
      done
      if kill -0 "$spid" 2>/dev/null; then
        echo "NLCAP_MONITOR_KILLED $subdir/$name" >&2
        kill -KILL "$spid" 2>/dev/null || true
        for c in $child; do
          kill -KILL "$c" 2>/dev/null || true
        done
        seen=0
      fi
      wait "$spid" || rc=$?
      echo "$rc" >"$dir/$name.rc"
      n=$(grep -c 'bytes in buffer' "$dir/$name.strace" || true)
      if [ "$joined" -ne 1 ] || [ "$seen" -ne 1 ]; then
        echo "NLCAP_MONITOR_SHORT $subdir/$name joined=$joined seen=$seen buffers=$n"
        return 1
      fi
      echo "NLCAP_STRACE_OK $subdir/$name rc=$rc buffers=$n"
    }

    # wait-rdma-port <ns> <dev/port> <state> <timeout>
    #
    # Polls `rdma link show` until the port reports <state>, so no capture
    # races rxe's port coming up.
    cmd_wait_rdma_port() {
      local ns=$1 link=$2 want=$3 timeout=$4 start deadline v=""
      start=$(date +%s)
      deadline=$(( start + timeout ))
      while :; do
        v="$(in_ns "$ns" rdma link show "$link" 2>/dev/null)" || v=""
        if printf '%s\n' "$v" | grep -q "state $want "; then
          echo "NLCAP_RDMA_PORT_OK $link=$want elapsed=$(( $(date +%s) - start ))s"
          return 0
        fi
        if [ "$(date +%s)" -ge "$deadline" ]; then
          break
        fi
        sleep 0.1
      done
      echo "NLCAP_RDMA_PORT_TIMEOUT $link want $want got: $v"
      return 1
    }

    # RDMA object holders. The RES_* object rows need user-owned QPs, CQs,
    # MRs, PDs, contexts, an SRQ and a CM ID to exist while `rdma res show`
    # runs. rdma-core's pingpong servers allocate all of them before they
    # block waiting for a peer, and an rping server holds a listening CM ID,
    # so each is started with no peer and left waiting. They are not traced:
    # only the `rdma` command of a row is the row.
    #
    # Each holder runs in its own session, so its process group is its pid
    # and one kill reaches everything it started. Their state lives outside
    # $OUT so pack never ships it.
    HOLD=/tmp/nlcap-hold

    # rdma-hold <name> <command>
    cmd_rdma_hold() {
      local name=$1 command=$2
      mkdir -p "$HOLD"
      setsid sh -c "$command" >"$HOLD/$name.log" 2>&1 </dev/null &
      echo "$!" >"$HOLD/$name.pid"
      echo "NLCAP_RDMA_HOLD_STARTED $name pid=$!"
    }

    # rdma-hold-wait <timeout> <resource>=<min>...
    #
    # Polls `rdma res show` until every named resource's count is at least
    # its minimum, so no row races a holder still allocating. The driver
    # waits on each holder before starting the next, which makes the object
    # numbers, a CQN for instance, the same on every run.
    cmd_rdma_hold_wait() {
      local timeout=$1 start deadline v="" r n ok
      shift
      start=$(date +%s)
      deadline=$(( start + timeout ))
      while :; do
        v="$(rdma res show 2>/dev/null)" || v=""
        ok=1
        for r in "$@"; do
          n=$(printf '%s\n' "$v" | sed -n "s/.* ''${r%=*} \([0-9]*\) .*/\1/p")
          if [ -z "$n" ] || [ "$n" -lt "''${r#*=}" ]; then
            ok=0
          fi
        done
        if [ "$ok" -eq 1 ]; then
          echo "NLCAP_RDMA_HOLD_OK elapsed=$(( $(date +%s) - start ))s $v"
          return 0
        fi
        if [ "$(date +%s)" -ge "$deadline" ]; then
          break
        fi
        sleep 0.1
      done
      echo "NLCAP_RDMA_HOLD_TIMEOUT want $* got: $v"
      for r in "$HOLD"/*.log; do
        [ -e "$r" ] && sed "s|^|$(basename "$r" .log): |" "$r"
      done
      return 1
    }

    # rdma-hold-stop
    #
    # SIGTERM to every holder's process group, a bounded wait, then SIGKILL.
    cmd_rdma_hold_stop() {
      local f pid deadline left
      for f in "$HOLD"/*.pid; do
        [ -e "$f" ] || continue
        kill -TERM -- "-$(cat "$f")" 2>/dev/null || true
      done
      deadline=$(( $(date +%s) + 5 ))
      while :; do
        left=0
        for f in "$HOLD"/*.pid; do
          [ -e "$f" ] || continue
          if kill -0 "$(cat "$f")" 2>/dev/null; then
            left=1
          fi
        done
        if [ "$left" -eq 0 ] || [ "$(date +%s)" -ge "$deadline" ]; then
          break
        fi
        sleep 0.05
      done
      for f in "$HOLD"/*.pid; do
        [ -e "$f" ] || continue
        pid=$(cat "$f")
        if kill -0 "$pid" 2>/dev/null; then
          echo "NLCAP_RDMA_HOLD_KILLED $(basename "$f" .pid)" >&2
          kill -KILL -- "-$pid" 2>/dev/null || true
        fi
        rm -f -- "$f"
      done
      echo "NLCAP_RDMA_HOLD_STOPPED"
    }

    # rdma-user-cqn <subdir> <row>
    #
    # The number of the first user-owned CQ in a captured `rdma res show cq`
    # row's stdout, so a doit row can name a CQ that exists. Here rather than
    # in the driver because an awk program survives no quoting over the serial
    # console.
    cmd_rdma_user_cqn() {
      local f="$OUT/$1/$2.out" n
      n=$(awk '/ pid /{for(i=1;i<NF;i++) if($i=="cqn"){print $(i+1); exit}}' "$f")
      case "$n" in
        "" | *[!0-9]*)
          echo "NLCAP_RDMA_NO_USER_CQN in $f" >&2
          return 1 ;;
      esac
      echo "$n"
    }

    # dir [subdir]
    #
    # Where captures live, so a driver can hand the path to another program
    # without knowing it. The parity driver needs exactly this: it runs
    # `goip-parity compare -dir <that>` in the guest, and a driver carrying its
    # own copy of `/tmp/nlcap` is one edit away from comparing an empty
    # directory - which, before GOIP_PARITY_NOTHING_COMPARED existed, reported
    # every sentinel green.
    cmd_dir() {
      if [ $# -ge 1 ] && [ -n "$1" ]; then
        echo "$OUT/$1"
      else
        echo "$OUT"
      fi
    }

    # inventory
    #
    # What is about to be shipped, so the driver can assert it got everything
    # rather than trusting the tar.
    cmd_inventory() {
      ( cd "$OUT" && find . -type f | sort | sed 's|^\./||' )
      echo "NLCAP_INVENTORY_OK"
    }

    # tcpdump-drops <stderr-file>
    #
    # tcpdump_drops as a subcommand, for nix/checks/vm-lib-exp.nix, which
    # checks the parse against tcpdump's exit lines with no capture running.
    # Prints the count and exits 0, or exits 1.
    cmd_tcpdump_drops() {
      [ $# -eq 1 ] || die "usage: tcpdump-drops <stderr-file>"
      tcpdump_drops "$1"
    }

    # filter [--proto <name>] <in.pcap> <out.pcap>
    #
    # capture()'s protocol filter as a subcommand, for nix/checks/vm-lib-exp.nix,
    # which runs it on a hand-built savefile of mixed protocols with no
    # capture running. Writes <out.pcap> and prints how many records it kept.
    cmd_filter() {
      [ $# -eq 2 ] || die "usage: filter [--proto route|generic|xfrm] <in.pcap> <out.pcap>"
      nl_filter "$1" "$2"
      pkt_count "$2"
    }

    # pack
    #
    # tar | gzip -n | base64, between sentinels, the same exfil path coverage
    # and nlmon-capture use. `gzip -n` omits the mtime so the blob is
    # byte-reproducible for an unchanged capture.
    #
    # base64 is WRAPPED, not `-w0`. The host side reads the console with
    # expect, which matches one whole line at a time against a bounded buffer;
    # a single multi-hundred-kilobyte line would exceed it and be silently
    # truncated. 76 columns is just the conventional width.
    cmd_pack() {
      echo "XTCP2_NLCAP_DUMP_START"
      tar c -C "$OUT" . | gzip -n | base64 -w 76
      echo "XTCP2_NLCAP_DUMP_END"
    }

    if [ $# -lt 1 ]; then
      die "usage: xtcp2-nlcap <ns-add|ns-del|topo|cap|capio|write|side|wait-lacp|wait-sysfs|wait-addr-flag|sysctl|modules|note|wait-churn|strace|strace-monitor|wait-rdma-port|rdma-hold|rdma-hold-wait|rdma-hold-stop|rdma-user-cqn|dir|inventory|pack|tcpdump-drops|filter> [--proto route|generic|xfrm] ..."
    fi

    sub=$1
    shift
    # --proto applies to the subcommands that filter a capture, and only to
    # them: anywhere else it would be silently ignored, and a row that thinks
    # it asked for XFRM should not get a NETLINK_ROUTE fixture.
    if [ "''${1-}" = --proto ]; then
      case "$sub" in
        cap | capio | write | filter) ;;
        *) die "--proto applies to cap, capio, write and filter, not $sub" ;;
      esac
      [ $# -ge 2 ] || die "--proto needs a value: route, generic or xfrm"
      set_proto "$2"
      shift 2
    fi
    case "$sub" in
      ns-add)    cmd_ns_add "$@" ;;
      ns-del)    cmd_ns_del "$@" ;;
      topo)      cmd_topo "$@" ;;
      cap)       cmd_cap "$@" ;;
      capio)     cmd_capio "$@" ;;
      write)     cmd_write "$@" ;;
      side)      cmd_side "$@" ;;
      wait-lacp) cmd_wait_lacp "$@" ;;
      wait-sysfs) cmd_wait_sysfs "$@" ;;
      wait-addr-flag) cmd_wait_addr_flag "$@" ;;
      sysctl)    cmd_sysctl "$@" ;;
      modules)   cmd_modules "$@" ;;
      note)      cmd_note "$@" ;;
      wait-churn) cmd_wait_churn "$@" ;;
      strace)    cmd_strace "$@" ;;
      strace-monitor) cmd_strace_monitor "$@" ;;
      wait-rdma-port) cmd_wait_rdma_port "$@" ;;
      rdma-hold) cmd_rdma_hold "$@" ;;
      rdma-hold-wait) cmd_rdma_hold_wait "$@" ;;
      rdma-hold-stop) cmd_rdma_hold_stop "$@" ;;
      rdma-user-cqn) cmd_rdma_user_cqn "$@" ;;
      dir)       cmd_dir "$@" ;;
      inventory) cmd_inventory "$@" ;;
      pack)      cmd_pack "$@" ;;
      tcpdump-drops) cmd_tcpdump_drops "$@" ;;
      filter)    cmd_filter "$@" ;;
      *)         die "unknown subcommand: $sub" ;;
    esac
  '';
}
