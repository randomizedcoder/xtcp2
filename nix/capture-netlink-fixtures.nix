# nix/capture-netlink-fixtures.nix
#
# DIAGNOSTIC FALLBACK. This is no longer how committed dump fixtures are made.
#
# The primary path is `nix run .#microvm-x86_64-netlink-dump-capture`
# (nix/microvms/netlink-capture.nix plus nix/microvms/scripts/
# capture-netlink-dumps.exp), which writes pkg/xtcpnl/testdata/<ver>/dumps/.
# Prefer it for anything that will be committed. Three reasons, and the third
# is the one that settles it:
#
#   1. No `sudo` on the developer's machine. Everything privileged happens
#      inside a guest that is discarded afterwards, so a capture run cannot
#      touch the host's interfaces, routes or neighbor table by mistake.
#   2. The guest kernel, iproute2 and topology are all pinned by the flake,
#      so two runs on two machines produce the same fixture. This script
#      pins the tools but not the kernel, and the version directory it writes
#      to is whichever kernel the developer happens to be booted into.
#   3. The guest is driven over a serial console by an expect script that
#      closes each capture window on a POSITIVE handshake — it waits for the
#      exit-code marker of the command it ran. Here the window is a `WARMUP`
#      sleep, which is a guess. The floors below exist because of that guess,
#      and a floor can only catch a window that opened too LATE.
#
# What this script is still the right tool for:
#
#   * Capturing what a REAL, messy host puts on the wire. The guest topology
#     is three devices by construction; a workstation has bonds, bridges,
#     SLAAC addresses and other processes talking to NETLINK_ROUTE. The
#     committed 7_1_8 corpus came from here and stays — pkg/xtcpnl/
#     testdata_test.go records which citations belong to which set and why.
#   * Reproducing a decode failure on the kernel you are actually running,
#     without building a VM first.
#   * Answering "does MY kernel do that?", since it runs against the host
#     kernel rather than a pinned one.
#
# Reproducible capture of REAL rtnetlink DUMP replies for the pkg/xtcpnl
# testdata harness, using the kernel's `nlmon` monitor interface. This is
# the source step behind the RTM_GETLINK / RTM_GETADDR / RTM_GETROUTE /
# RTM_GETNEIGH fixtures that pkg/xtcpnl, pkg/localnet and internal/goip
# parse; keeping it in-tree means the fixtures can be regenerated later on
# any kernel to the same standard as the existing sock_diag captures
# (testdata/<ver>/…).
#
# Usage (run from the xtcp2 repo root):
#   nix run .#capture-netlink-fixtures
#   nix run .#capture-netlink-fixtures -- <output-dir>
#
# It loads `nlmon`, creates a temporary `nlmon0`, and captures tight
# per-command dumps. `nlmon` mirrors EVERY netlink datagram in the namespace,
# so the raw capture also contains unrelated NETLINK_GENERIC traffic
# (nl80211, etc.). Each capture is therefore filtered to NETLINK_ROUTE
# only via the BPF expression `ether[14:2]==0` — the netlink family lives
# at offset 14-15 of the 16-byte Linux-SLL cooked header, and
# NETLINK_ROUTE == 0 (cf. the committed testdata/*/netlink_cooked_header,
# whose sock_diag capture ends in 0x0004 == NETLINK_SOCK_DIAG).
#
# TWO CAPTURE SETS, and the difference between them is the point.
#
#   1. HOST captures, written to $OUT. These are what the host happens to
#      look like. Their value is the REQUEST side: request bytes are
#      tool-controlled and host-independent, so `ip neigh show`'s
#      RTM_GETNEIGH is the same 28 bytes everywhere. Their weakness is the
#      reply side — a host with no IPv6, no ECMP route and an empty
#      neighbor table yields a fixture with nothing in it. Worse, nlmon
#      mirrors the WHOLE namespace, so the host set is polluted by every
#      other process that touches NETLINK_ROUTE. The committed
#      netlink_route_getaddr.pcap carries six distinct portids for exactly
#      that reason.
#
#   2. TOPOLOGY captures, written to $OUT/topo, taken inside a throwaway
#      network namespace holding one dummy device and a hand-built set of
#      addresses, routes and neighbors. Two properties fall out:
#
#        - Determinism. The ECMP route, the RFC 5549 IPv6-via-IPv4 route
#          and the route carrying RTAX_MTU exist because this script
#          created them, so RTA_MULTIPATH / RTA_VIA / RTA_METRICS get real
#          captured positives on any host rather than being decoded from
#          hand-written bytes.
#        - Cleanliness, structurally rather than by luck. Netlink taps are
#          per-netns — af_netlink.c registers `netlink_tap_net_ops` as
#          pernet_operations and netlink_deliver_tap() picks the tap list
#          out of net_generic(net, netlink_tap_net_id) — so an nlmon0
#          created INSIDE the namespace sees only that namespace's netlink
#          traffic. The pollution problem the host set has is not mitigated
#          here, it is absent.
#
#      The namespace is deleted on exit, so the host's own addresses,
#      routes and neighbor table are never touched. That also keeps the
#      existing host sidecars stable: tests cite ip_addr_n and ip_link_n by
#      line number, and adding a device to the host would shift every one
#      of those citations.
#
# FLOORS. Every capture declares the minimum number of NETLINK_ROUTE
# datagrams it must contain, and a capture that comes in under its floor is
# retried once and then DISCARDED rather than written. Without that, a
# capture window that opened too late silently replaces a good committed
# fixture with a short one, and the failure surfaces later as a confusing
# decoder error. The floors are deliberately structural minima ("a request
# plus at least one reply datagram per command"), not host-specific
# expectations, so they stay valid on a host with fewer interfaces.
#
# Privileged steps (modprobe / ip link / ip netns / tcpdump) go through
# `sudo`, so the script itself runs unprivileged under `nix run`; outputs
# are chowned back to the invoking user at the end. The version dir is
# derived from `uname -r` (e.g. 7.1.8 -> testdata/7_1_8), matching the
# harness naming, and `ip -V` is recorded alongside it because request
# bytes depend on the iproute2 version as well as the kernel — see the
# iproute2 entry in nix/upstream-pins.json.
#
{ pkgs }:

pkgs.writeShellApplication {
  name = "xtcp2-capture-netlink-fixtures";
  runtimeInputs = with pkgs; [
    coreutils
    gnugrep
    iproute2
    tcpdump
    kmod
  ];
  text = ''
    set -euo pipefail

    # `sudo` is the NixOS setuid wrapper, not a runtimeInput; make it
    # resolvable while still preferring our pinned tools for everything else.
    export PATH="/run/wrappers/bin:$PATH"

    if [ ! -f flake.nix ] || [ ! -d pkg/xtcpnl ]; then
      echo "capture-netlink-fixtures: run from the xtcp2 repo root" >&2
      exit 2
    fi

    # Derive the testdata version dir from the running kernel: 7.1.8 -> 7_1_8.
    VER="$(uname -r | cut -d- -f1 | tr . _)"
    OUT="''${1:-pkg/xtcpnl/testdata/$VER}"
    TOPO="$OUT/topo"
    IFACE="nlmon0"
    NS="xtcp2fix"       # throwaway netns for the deterministic topology set
    DUMMY="goip0"       # dummy device inside $NS; never created on the host
    WARMUP=1            # seconds to let tcpdump bind before triggering the dump

    # Absolute nix-store paths so `sudo` (which resets PATH to secure_path)
    # still runs our pinned binaries. modprobe is left bare so sudo's
    # secure_path picks the system wrapper that knows the NixOS module dir.
    IP="$(command -v ip)"
    TCPDUMP="$(command -v tcpdump)"
    CHOWN="$(command -v chown)"
    RM="$(command -v rm)"
    MV="$(command -v mv)"
    TEE="$(command -v tee)"

    USER_NAME="$(id -un)"
    GROUP_NAME="$(id -gn)"

    # Accumulated problems, reported together at the end rather than one at a
    # time in the middle of the output where they scroll past.
    SHORT=""            # captures that missed their floor
    TOPO_FAILED=0       # topology commands the kernel refused

    # The topology transcript is built in a user-owned temp file and installed
    # at the end: $OUT can be root-owned from a previous run's sudo'd tcpdump,
    # so appending to it directly with `>>` may fail on permissions.
    TOPO_LOG="$(mktemp)"

    mkdir -p "$OUT" "$TOPO"

    echo "== loading nlmon and dummy =="
    sudo modprobe nlmon
    sudo modprobe dummy
    # Read lsmod via a here-string, not a pipe: `grep -q` closes the pipe on
    # first match and lsmod's (large) output then dies with SIGPIPE, which
    # `set -o pipefail` would report as a failure.
    grep -qw nlmon <<<"$(lsmod)" || { echo "nlmon failed to load" >&2; exit 1; }

    echo "== (re)creating $IFACE on the host =="
    sudo "$IP" link del "$IFACE" 2>/dev/null || true
    sudo "$IP" link add "$IFACE" type nlmon
    sudo "$IP" link set dev "$IFACE" up

    # The trap also chowns $OUT back: tcpdump/tee run as root, so an abort
    # halfway (Ctrl-C, a failed dump) would otherwise leave root-owned files
    # the unprivileged caller cannot delete or overwrite on the next run. The
    # netns delete takes the dummy device, its addresses, its routes and its
    # neighbor entries with it, so an abort leaves no host state behind.
    cleanup() {
      sudo "$IP" netns del "$NS" 2>/dev/null || true
      sudo "$IP" link del "$IFACE" 2>/dev/null || true
      "$RM" -f "$TOPO_LOG" 2>/dev/null || true
      sudo "$CHOWN" -R "$USER_NAME:$GROUP_NAME" "$OUT" 2>/dev/null || true
    }
    trap cleanup EXIT

    # --- helpers ---------------------------------------------------------

    # Datagram count, straight from tcpdump rather than counted by line.
    # `--count` writes "N packets" to stdout with its "reading from file"
    # banner on stderr, so one `cut` gets the number. Counting lines of `-q`
    # output would also work on DLT_NETLINK today — measured 76 timestamped
    # lines out of 4919 total for netlink_route_getaddr.pcap, which matches
    # --count exactly — but it depends on the dissector printing exactly one
    # timestamped line per packet, which is not a promise tcpdump makes.
    pkt_count() {
      local n
      n="$(sudo "$TCPDUMP" --count -r "$1" 2>/dev/null | cut -d' ' -f1)" || n=""
      case "$n" in
        "" | *[!0-9]*) echo 0 ;;
        *) echo "$n" ;;
      esac
    }

    # Run a command inside the topology namespace, as root.
    nsx() { sudo "$IP" netns exec "$NS" "$@"; }

    # Build one piece of the topology, recording the exact command and whether
    # the kernel took it. A step is allowed to fail: an `ip route add ... via
    # inet6` needs RFC 5549 support and an on-link nexthop, and a host without
    # it should still get every other fixture rather than aborting the run.
    # The transcript is what makes the resulting pcap self-describing.
    topo_step() {
      if nsx "$IP" "$@" 2>/dev/null; then
        printf 'ok    ip %s\n' "$*" >>"$TOPO_LOG"
      else
        printf 'FAIL  ip %s\n' "$*" >>"$TOPO_LOG"
        echo "   !! topology step refused: ip $*" >&2
        TOPO_FAILED=$((TOPO_FAILED + 1))
      fi
    }

    # Capture one command's netlink traffic into a raw pcap, filter it to
    # NETLINK_ROUTE, and install it only if it clears its floor.
    #
    #   cap <name> <min-datagrams> <dir> <netns|-> <command...>
    #
    # The generator (RTM_GET*) command opens a NETLINK_ROUTE socket that nlmon
    # mirrors; a SIGINT to tcpdump after the dump completes stops it cleanly so
    # the pcap is flushed. For a namespaced capture, tcpdump runs inside the
    # namespace too, because that is where the mirroring nlmon0 lives.
    cap() {
      local name=$1 min=$2 dir=$3 ns=$4
      shift 4
      local raw="$dir/.$name.raw.pcap"
      local new="$dir/.$name.new.pcap"
      local attempt tp n
      echo "== capturing $name =="
      for attempt in 1 2; do
        if [ "$ns" = "-" ]; then
          sudo "$TCPDUMP" -i "$IFACE" -w "$raw" -U -q >/dev/null 2>&1 &
        else
          sudo "$IP" netns exec "$ns" "$TCPDUMP" -i "$IFACE" -w "$raw" -U -q >/dev/null 2>&1 &
        fi
        tp=$!
        sleep "$WARMUP"
        "$@" >/dev/null 2>&1 || true
        sleep 1
        sudo kill -INT "$tp" 2>/dev/null || true
        wait "$tp" 2>/dev/null || true
        sudo "$TCPDUMP" -r "$raw" -w "$new" 'ether[14:2]==0' >/dev/null 2>&1
        sudo "$RM" -f "$raw"
        n="$(pkt_count "$new")"
        if [ "$n" -ge "$min" ]; then
          sudo "$MV" -f "$new" "$dir/$name.pcap"
          echo "   -> $dir/$name.pcap ($n NETLINK_ROUTE datagrams, floor $min)"
          return 0
        fi
        sudo "$RM" -f "$new"
        echo "   .. attempt $attempt saw $n datagrams, floor is $min" >&2
      done
      echo "   !! $name missed its floor twice; leaving any existing fixture alone" >&2
      SHORT="$SHORT $name"
      return 0
    }

    # Write a sidecar via `sudo tee`: the pcaps were captured by sudo'd
    # tcpdump (root-owned), so $OUT may be root-owned until the final chown;
    # tee-as-root sidesteps a permission error on the plain `>` redirect.
    # A generator that fails still leaves a file, so a missing rendering shows
    # up as an empty sidecar rather than as an aborted run.
    side() {
      local path=$1
      shift
      "$@" 2>/dev/null | sudo "$TEE" "$path" >/dev/null || true
    }

    # --- part 1: host captures -------------------------------------------
    #
    # Note: `ip addr show` issues an RTM_GETLINK dump before RTM_GETADDR, so the
    # getaddr pcaps also carry RTM_NEWLINK replies; parsers filter by type. The
    # same is true of `ip route show` and `ip neigh show`, both of which call
    # ll_init_map() first.

    gen_addr() { "$IP" -4 addr show; "$IP" -6 addr show; }
    gen_route() { "$IP" route show table all; }
    gen_link() { "$IP" link show; }
    gen_neigh() { "$IP" neigh show; }

    # The three default forms goip actually implements, none of which had a
    # fixture. Plain `addr show` is AF_UNSPEC and so carries IFLA_EXT_MASK on
    # its link dump, where the -4/-6 form does not: rtnl_linkdump_req_filter_fn
    # skips filter_fn unless the family is AF_UNSPEC or AF_PACKET
    # (lib/libnetlink.c:595). Default `route show` sends AF_INET plus
    # RTA_TABLE=254 rather than the all-zero rtmsg of `table all`
    # (ip/iproute.c:1836,1997).
    gen_addr_unspec() { "$IP" addr show; }
    gen_route_main() { "$IP" route show; }

    # `ip link show dev lo` was captured to SETTLE what it does rather than to
    # confirm it: whether iproute2 resolves the name through a full ll_init_map
    # dump and then filters, or issues a single non-dump RTM_GETLINK, decided
    # whether goip needs TalkRtnetlink here at all.
    #
    # Settled, and the answer was neither of the two on offer: it issues TWO
    # non-dump RTM_GETLINKs. ll_link_get resolves the name to an index on a
    # throwaway socket (ip/ipaddress.c:2254), and iplink_get then re-fetches
    # the same link on the main socket, whose reply is the one print_linkinfo
    # renders (:2293). Hence a floor of four datagrams — two transactions,
    # each a request and a reply — and not the two this carried while the
    # question was open, which a window catching only the first transaction
    # would have cleared.
    gen_link_dev() { "$IP" link show dev lo; }

    cap netlink_route_getaddr        8 "$OUT" - gen_addr
    cap netlink_route_getroute       4 "$OUT" - gen_route
    cap netlink_route_getlink        2 "$OUT" - gen_link
    cap netlink_route_getneigh       4 "$OUT" - gen_neigh
    cap netlink_route_getaddr_unspec 4 "$OUT" - gen_addr_unspec
    cap netlink_route_getroute_main  4 "$OUT" - gen_route_main
    cap netlink_route_getlink_dev_lo 4 "$OUT" - gen_link_dev

    # --- part 2: the deterministic topology namespace --------------------

    echo "== building the topology namespace $NS =="
    sudo "$IP" netns del "$NS" 2>/dev/null || true
    sudo "$IP" netns add "$NS"
    nsx "$IP" link set lo up
    nsx "$IP" link add "$IFACE" type nlmon
    nsx "$IP" link set dev "$IFACE" up

    # One dummy and nothing else. The device deliberately has no IFLA_LINK and
    # no IFLA_MASTER, because both make iproute2 issue a side ll_link_get
    # single-get while rendering; a fixture with no side traffic is what lets
    # the parity harness assert transaction counts positionally. The messy
    # case — veth pair plus a bridge master — belongs in a separate advisory
    # fixture set, not here.
    topo_step link add "$DUMMY" type dummy
    topo_step addr add 192.0.2.1/24 dev "$DUMMY"
    # nodad: without it the address is IFA_F_TENTATIVE for a moment and the
    # capture becomes a race against duplicate address detection.
    topo_step -6 addr add 2001:db8::1/64 nodad dev "$DUMMY"
    topo_step link set "$DUMMY" up

    # Routes, one per decoder feature that has no captured positive today.
    topo_step route add 198.51.100.0/24 dev "$DUMMY"
    topo_step route add 198.18.0.0/24 via 192.0.2.10 dev "$DUMMY"
    # RTA_METRICS, two nested values so the decoder cannot pass by returning
    # the first one it finds.
    topo_step route add 198.18.1.0/24 via 192.0.2.10 dev "$DUMMY" mtu 1400 advmss 1300
    # RTA_MULTIPATH. The weights differ on purpose: rtnh_hops is weight-1, so
    # equal weights would let a decoder that ignores the field look correct.
    topo_step route add 203.0.113.0/24 nexthop via 192.0.2.10 dev "$DUMMY" weight 1 nexthop via 192.0.2.11 dev "$DUMMY" weight 3
    # RTA_VIA — an IPv4 destination with an IPv6 nexthop (RFC 5549), the only
    # thing that puts a struct rtvia on the wire.
    topo_step route add 198.18.2.0/24 via inet6 2001:db8::2 dev "$DUMMY"
    topo_step -6 route add 2001:db8:1::/64 via 2001:db8::2 dev "$DUMMY"
    topo_step -6 route add 2001:db8:2::/64 nexthop via 2001:db8::2 dev "$DUMMY" weight 1 nexthop via 2001:db8::3 dev "$DUMMY" weight 3

    # Neighbors in three NUD states plus one with no lladdr at all, which is
    # the case that decides whether an absent NDA_LLADDR is handled or
    # rendered as an empty MAC.
    topo_step neigh add 192.0.2.50 lladdr 02:00:00:00:00:01 dev "$DUMMY" nud permanent
    topo_step neigh add 192.0.2.51 lladdr 02:00:00:00:00:02 dev "$DUMMY" nud stale
    topo_step neigh add 192.0.2.52 dev "$DUMMY" nud incomplete
    topo_step -6 neigh add 2001:db8::50 lladdr 02:00:00:00:00:03 dev "$DUMMY" nud permanent

    gen_topo_link() { nsx "$IP" link show; }
    gen_topo_addr() { nsx "$IP" addr show; }
    gen_topo_route() { nsx "$IP" route show; }
    gen_topo_route6() { nsx "$IP" -6 route show; }
    gen_topo_neigh() { nsx "$IP" neigh show; }

    # Floors are one request plus one reply datagram per dump the command
    # issues. `link show` is a single dump, so 2. Everything else calls
    # ll_init_map() first and is therefore two dumps, so 4 — a floor of 2 there
    # would pass on a capture that missed the second dump entirely, which is
    # exactly the failure worth catching.
    cap netlink_route_getlink   2 "$TOPO" "$NS" gen_topo_link
    cap netlink_route_getaddr   4 "$TOPO" "$NS" gen_topo_addr
    cap netlink_route_getroute  4 "$TOPO" "$NS" gen_topo_route
    cap netlink_route_getroute6 4 "$TOPO" "$NS" gen_topo_route6
    cap netlink_route_getneigh  4 "$TOPO" "$NS" gen_topo_neigh

    # --- sidecars --------------------------------------------------------
    #
    # PLAIN AND -d, IN PAIRS. The previous version of this script captured the
    # pcaps with plain `ip` but wrote only `ip -d` sidecars, so diffing a
    # renderer's output against ip_link_n showed a large difference made
    # entirely of -d-only attributes the renderer never decodes. Both forms are
    # written now, under distinct names: the _n suffix keeps meaning `-d`, so
    # the line-number citations already in the Go tests stay valid.
    #
    # The -4 and -6 forms are here for the same reason. They had to be
    # reconstructed from the -d AF_UNSPEC sidecar by hand, which is a
    # reconstruction that can now be checked against ground truth and then
    # deleted.
    echo "== source-of-truth sidecars =="
    side "$OUT/uname"                   uname -a
    side "$OUT/ip_version"              "$IP" -V
    side "$OUT/ip_addr_n"               "$IP" -d addr show
    side "$OUT/ip_addr"                 "$IP" addr show
    side "$OUT/ip_addr_v4_n"            "$IP" -d -4 addr show
    side "$OUT/ip_addr_v4"              "$IP" -4 addr show
    side "$OUT/ip_addr_v6_n"            "$IP" -d -6 addr show
    side "$OUT/ip_addr_v6"              "$IP" -6 addr show
    side "$OUT/ip_route_table_all_n"    "$IP" -d route show table all
    side "$OUT/ip_route_table_all"      "$IP" route show table all
    side "$OUT/ip_route_main_n"         "$IP" -d route show
    side "$OUT/ip_route_main"           "$IP" route show
    side "$OUT/ip_link_n"               "$IP" -d link show
    side "$OUT/ip_link"                 "$IP" link show
    side "$OUT/ip_link_dev_lo"          "$IP" link show dev lo
    side "$OUT/ip_neigh"                "$IP" neigh show

    # JSON sidecars, which is what turns "does goip -json use ip -j's key
    # names?" from an eyeball question into a diff. `-p` pretty-prints so the
    # committed file is reviewable line by line.
    side "$OUT/ip_link_json"            "$IP" -j -p link show
    side "$OUT/ip_addr_json"            "$IP" -j -p addr show
    side "$OUT/ip_route_main_json"      "$IP" -j -p route show
    side "$OUT/ip_neigh_json"           "$IP" -j -p neigh show

    echo "== topology sidecars =="
    side "$TOPO/uname"                  uname -a
    side "$TOPO/ip_version"             "$IP" -V
    side "$TOPO/ip_link_n"              nsx "$IP" -d link show
    side "$TOPO/ip_link"                nsx "$IP" link show
    side "$TOPO/ip_addr_n"              nsx "$IP" -d addr show
    side "$TOPO/ip_addr"                nsx "$IP" addr show
    side "$TOPO/ip_route_main_n"        nsx "$IP" -d route show
    side "$TOPO/ip_route_main"          nsx "$IP" route show
    side "$TOPO/ip_route6_n"            nsx "$IP" -d -6 route show
    side "$TOPO/ip_route6"              nsx "$IP" -6 route show
    side "$TOPO/ip_neigh"               nsx "$IP" neigh show
    side "$TOPO/ip_link_json"           nsx "$IP" -j -p link show
    side "$TOPO/ip_addr_json"           nsx "$IP" -j -p addr show
    side "$TOPO/ip_route_main_json"     nsx "$IP" -j -p route show
    side "$TOPO/ip_neigh_json"          nsx "$IP" -j -p neigh show

    # The transcript of how the namespace was built, so the pcaps next to it
    # describe their own provenance instead of relying on this file. Installed
    # through `side` rather than a redirect: $TOPO_LOG is readable without
    # privilege, only the destination needs root, and `sudo cmd > file` would
    # redirect as the unprivileged caller.
    side "$TOPO/topology" cat "$TOPO_LOG"

    echo "== chown $OUT -> $USER_NAME:$GROUP_NAME =="
    sudo "$CHOWN" -R "$USER_NAME:$GROUP_NAME" "$OUT"

    echo
    echo "== summary =="
    echo "kernel   : $(uname -r)"
    echo "iproute2 : $("$IP" -V)"
    echo "output   : $OUT"
    if [ "$TOPO_FAILED" -ne 0 ]; then
      echo
      echo "$TOPO_FAILED topology step(s) were refused by the kernel." >&2
      echo "See $TOPO/topology for which, and expect the fixture that step" >&2
      echo "was there to produce to be missing its positive case." >&2
    fi
    if [ -n "$SHORT" ]; then
      echo
      echo "MISSED FLOOR:$SHORT" >&2
      echo "Those captures came in short twice and were discarded rather than" >&2
      echo "written, so any committed fixture of the same name is unchanged." >&2
      echo "Usually the capture window opened late — re-run, and raise WARMUP" >&2
      echo "if it persists." >&2
      exit 1
    fi

    echo
    echo "== done =="
    ls -la "$OUT" "$TOPO"
  '';
}
