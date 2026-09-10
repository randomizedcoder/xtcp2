# nix/capture-netlink-fixtures.nix
#
# Reproducible capture of REAL rtnetlink DUMP replies for the pkg/xtcpnl
# testdata harness, using the kernel's `nlmon` monitor interface. This is
# the source step behind the RTM_GETLINK / RTM_GETADDR / RTM_GETROUTE
# fixtures that pkg/xtcpnl and pkg/localnet parse; keeping it in-tree means
# the fixtures can be regenerated later on any kernel to the same standard
# as the existing sock_diag captures (testdata/<ver>/…).
#
# Usage (run from the xtcp2 repo root):
#   nix run .#capture-netlink-fixtures
#   nix run .#capture-netlink-fixtures -- <output-dir>
#
# It loads `nlmon`, creates a temporary `nlmon0`, and captures three tight
# per-type dumps. `nlmon` mirrors EVERY netlink datagram in the namespace,
# so the raw capture also contains unrelated NETLINK_GENERIC traffic
# (nl80211, etc.). Each capture is therefore filtered to NETLINK_ROUTE
# only via the BPF expression `ether[14:2]==0` — the netlink family lives
# at offset 14-15 of the 16-byte Linux-SLL cooked header, and
# NETLINK_ROUTE == 0 (cf. the committed testdata/*/netlink_cooked_header,
# whose sock_diag capture ends in 0x0004 == NETLINK_SOCK_DIAG).
#
# Privileged steps (modprobe / ip link / tcpdump) go through `sudo`, so the
# script itself runs unprivileged under `nix run`; outputs are chowned back
# to the invoking user at the end. The version dir is derived from
# `uname -r` (e.g. 7.1.8 -> testdata/7_1_8), matching the harness naming.
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
    IFACE="nlmon0"
    WARMUP=1     # seconds to let tcpdump bind before triggering the dump

    # Absolute nix-store paths so `sudo` (which resets PATH to secure_path)
    # still runs our pinned binaries. modprobe is left bare so sudo's
    # secure_path picks the system wrapper that knows the NixOS module dir.
    IP="$(command -v ip)"
    TCPDUMP="$(command -v tcpdump)"
    CHOWN="$(command -v chown)"
    RM="$(command -v rm)"

    USER_NAME="$(id -un)"
    GROUP_NAME="$(id -gn)"

    mkdir -p "$OUT"

    echo "== loading nlmon =="
    sudo modprobe nlmon
    # Read lsmod via a here-string, not a pipe: `grep -q` closes the pipe on
    # first match and lsmod's (large) output then dies with SIGPIPE, which
    # `set -o pipefail` would report as a failure.
    grep -qw nlmon <<<"$(lsmod)" || { echo "nlmon failed to load" >&2; exit 1; }

    echo "== (re)creating $IFACE =="
    sudo "$IP" link del "$IFACE" 2>/dev/null || true
    sudo "$IP" link add "$IFACE" type nlmon
    sudo "$IP" link set dev "$IFACE" up

    cleanup() { sudo "$IP" link del "$IFACE" 2>/dev/null || true; }
    trap cleanup EXIT

    # Capture one dump type into a raw pcap, then filter to NETLINK_ROUTE.
    # The generator (RTM_GET*) command runs unprivileged and opens a
    # NETLINK_ROUTE socket that nlmon mirrors; a SIGINT to tcpdump after
    # the dump completes stops it cleanly so the pcap is flushed.
    cap() {
      name="$1"; shift
      raw="$OUT/.$name.raw.pcap"
      echo "== capturing $name =="
      sudo "$TCPDUMP" -i "$IFACE" -w "$raw" -U -q >/dev/null 2>&1 &
      tp=$!
      sleep "$WARMUP"
      "$@" >/dev/null 2>&1 || true
      sleep 1
      sudo kill -INT "$tp" 2>/dev/null || true
      wait "$tp" 2>/dev/null || true
      sudo "$TCPDUMP" -r "$raw" -w "$OUT/$name.pcap" 'ether[14:2]==0' >/dev/null 2>&1
      sudo "$RM" -f "$raw"
      n="$(sudo "$TCPDUMP" -nnq -r "$OUT/$name.pcap" 2>/dev/null | grep -cE '^[0-9]{2}:' || true)"
      echo "   -> $OUT/$name.pcap ($n NETLINK_ROUTE packets)"
    }

    gen_addr() { "$IP" -4 addr show; "$IP" -6 addr show; }
    gen_route() { "$IP" route show table all; }
    gen_link() { "$IP" link show; }

    cap netlink_route_getaddr  gen_addr
    cap netlink_route_getroute gen_route
    cap netlink_route_getlink  gen_link

    # Write sidecars via `sudo tee`: the pcaps were captured by sudo'd
    # tcpdump (root-owned), so $OUT may be root-owned until the final chown;
    # tee-as-root sidesteps a permission error on the plain `>` redirect.
    echo "== source-of-truth sidecars =="
    uname -a                        | sudo tee "$OUT/uname" >/dev/null
    "$IP" -d addr show              | sudo tee "$OUT/ip_addr_n" >/dev/null
    "$IP" -d route show table all   | sudo tee "$OUT/ip_route_table_all_n" >/dev/null
    "$IP" -d link show              | sudo tee "$OUT/ip_link_n" >/dev/null

    echo "== chown $OUT -> $USER_NAME:$GROUP_NAME =="
    sudo "$CHOWN" -R "$USER_NAME:$GROUP_NAME" "$OUT"

    echo "== done =="
    ls -la "$OUT"
  '';
}
