# nix/microvms/netlink-capture.nix
#
# Guest-side mechanism for the two driven netlink microVM flavors:
# netlink-dump-capture, which records the decoder corpus, and goip-parity,
# which records the ip/goip/ip triples the parity comparator reads.
#
# This exports ONE thing: `xtcp2-nlcap`, a helper installed on the guest's
# PATH and invoked one subcommand at a time by a host driver
# (nix/microvms/scripts/capture-netlink-dumps.exp and goip-parity.exp) over
# the serial console.
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
# it and the fixture filter `ether[14:2]==0` drops it without a second pass.
# The fourth requirement is the one that decided WHICH ss: measured alone in a
# namespace, `ss -x` emits `total=2 proto0=0 proto4=2`, while `ss -a` emits 4
# NETLINK_ROUTE datagrams of its own because it resolves interface names. A
# sentinel that adds rtnetlink traffic to a fixture whose whole purpose is to
# contain one transaction is not a sentinel, it is pollution.
#
{ pkgs }:

pkgs.writeShellApplication {
  name = "xtcp2-nlcap";
  runtimeInputs = with pkgs; [
    coreutils
    gnugrep
    gnutar
    gzip
    iproute2
    kmod
    tcpdump
  ];
  text = ''
    set -euo pipefail

    OUT=/tmp/nlcap
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

    mkdir -p "$OUT"

    die() {
      echo "NLCAP_ERR $*" >&2
      exit 2
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
    cmd_topo() {
      local ns=$1 subdir=$2
      shift 2
      mkdir -p "$OUT/$subdir"
      if ip -n "$ns" "$@" 2>/dev/null; then
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

    # capture — the shared body of cap and capio.
    #
    # <sout> is a file name to record the generator's stdout to, or `-` to
    # discard it. Factored out rather than duplicated because everything that
    # makes a capture correct lives in here: the bind wait, immediate mode, the
    # stop sentinel, the NETLINK_ROUTE filter and the floor. A second copy of
    # that for the parity captures would be a second place for the sentinel
    # logic to drift, and the symptom of drift is an empty pcap that looks
    # like a clean run.
    capture() {
      local ns=$1 subdir=$2 name=$3 min=$4 command=$5 sout=$6
      local dir="$OUT/$subdir"
      mkdir -p "$dir"

      local raw="$dir/.$name.raw.pcap"
      local new="$dir/.$name.new.pcap"
      # Staged under a dot name and moved into place only with the pcap it
      # belongs to. A .out left behind by a discarded attempt would pair a
      # fresh text with a stale or absent capture, and the comparator reads
      # the two as one observation.
      local outnew="$dir/.$name.new.out"
      local attempt err tp n=0

      for attempt in 1 2; do
        err="$(mktemp)"
        # --immediate-mode: without it libpcap's TPACKET_V3 ring holds a block
        # for up to its 1000 ms retire timeout, so a low-rate capture is not
        # readable by tcpdump when SIGINT arrives and the pcap comes back
        # empty. See the header - this is the difference between 2 of 18
        # captures passing and all of them passing.
        ip netns exec "$ns" \
          tcpdump -i "$IFACE" -w "$raw" -U -q --immediate-mode >/dev/null 2>"$err" &
        tp=$!

        if ! wait_for_line "$err" "listening on" "$tp"; then
          kill -INT "$tp" 2>/dev/null || true
          wait "$tp" 2>/dev/null || true
          echo "NLCAP_CAP_NOBIND $name attempt=$attempt" >&2
          cat "$err" >&2 || true
          rm -f "$err" "$raw" "$outnew"
          continue
        fi
        rm -f "$err"

        # The generator's own status is deliberately ignored: `ip neigh show`
        # on an empty table succeeds, `ip -6 route show` on a v4-only setup
        # succeeds, and a genuine failure shows up as a missed floor below,
        # which is a better signal than a shell exit code.
        #
        # stderr goes to /dev/null in both branches, never into the recorded
        # text. The comparator diffs stdout structurally - line counts and
        # extracted name/CIDR/MAC sets - so a warning on stderr folded into
        # the file would read as a rendered object that the other side lacks.
        if [ "$sout" = "-" ]; then
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

        # Filter to NETLINK_ROUTE, which now has two jobs: it removes the stop
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
        # sll_protocol=0 at offset 14. `ether[N:2]` is a raw offset from the
        # start of the link-layer header, so it reads that field whether the
        # filter is compiled against a live device or a savefile.
        #
        # Note this is a filter, NOT a rescue: a fixture is meant to hold one
        # transaction, so anything it drops other than the sentinel would be a
        # capture-hygiene problem worth knowing about rather than hiding.
        tcpdump -r "$raw" -w "$new" 'ether[14:2]==0' >/dev/null 2>&1 || true
        rm -f "$raw"

        n="$(pkt_count "$new")"
        if [ "$n" -ge "$min" ]; then
          mv -f "$new" "$dir/$name.pcap"
          if [ "$sout" != "-" ]; then
            # Both halves land together or neither does. `mv` after the pcap's
            # mv, so a crash between them leaves a pcap with no text - which
            # the comparator reports as MISSING - rather than a text with no
            # pcap, which it would have to guess about.
            mv -f "$outnew" "$dir/$sout"
            echo "NLCAP_CAP_OK $subdir/$name datagrams=$n floor=$min stdout=$subdir/$sout bytes=$(wc -c <"$dir/$sout")"
            return 0
          fi
          echo "NLCAP_CAP_OK $subdir/$name datagrams=$n floor=$min"
          return 0
        fi
        rm -f "$new" "$outnew"
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
      die "usage: xtcp2-nlcap <ns-add|ns-del|topo|cap|capio|side|dir|inventory|pack> ..."
    fi

    sub=$1
    shift
    case "$sub" in
      ns-add)    cmd_ns_add "$@" ;;
      ns-del)    cmd_ns_del "$@" ;;
      topo)      cmd_topo "$@" ;;
      cap)       cmd_cap "$@" ;;
      capio)     cmd_capio "$@" ;;
      side)      cmd_side "$@" ;;
      dir)       cmd_dir "$@" ;;
      inventory) cmd_inventory "$@" ;;
      pack)      cmd_pack "$@" ;;
      *)         die "unknown subcommand: $sub" ;;
    esac
  '';
}
