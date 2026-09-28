# nix/checks/upstream-pins.nix
#
# Hermetic half of the upstream-pin guard. See nix/upstream-pins.json for what
# is pinned and why it matters.
#
# WHAT THIS CAN AND CANNOT DO, stated plainly because the distinction is the
# whole design: a `nix flake check` derivation runs in a sandbox with no
# network. It therefore CANNOT ask GitHub whether a branch has moved. Any check
# claiming to do that from inside the sandbox would either be lying or be
# silently baking a stale answer into a cached derivation — which is worse than
# no check, because it would report "up to date" forever.
#
# So this check answers the question it actually can, hermetically:
#
#   "Do the revs recorded in nix/upstream-pins.json still match the revs this
#    build is actually using?"
#
# That catches a pin being MOVED without the manifest being updated — i.e. it
# keeps the manifest honest, which is what makes the networked runner's drift
# report trustworthy. `nix run .#check-upstream-pins` answers the other
# question, "has upstream moved?", where it can: outside the sandbox.
#
# Both git pins are verifiable in here because both are in the store already:
# flake.lock is part of the source, and xdp2's own tree is a flake input.
#
# The manifest's `package_pins` key is a different shape and is checked here
# too, on better evidence than the git pins get. A nixpkgs package version is
# known at EVAL time, so `pkgs.iproute2.version` below is by construction the
# version this build uses — there is no lockfile to read and no way for the
# answer to be stale. The networked runner has nothing to add for that key,
# since there is no remote ref to ask about.
#
{
  pkgs,
  src,
  xdp2,
}:

let
  manifest = ../upstream-pins.json;
in
pkgs.runCommand "xtcp2-upstream-pins"
  {
    nativeBuildInputs = [ pkgs.jq ];
    inherit src;
    xdp2Src = xdp2;
    iproute2Version = pkgs.iproute2.version;
  }
  ''
    set -eu

    manifest=${manifest}
    fail=0

    echo "=== upstream pin manifest check ==="
    echo

    # --- pin 1: xtcp2's xdp2 flake input, verified against flake.lock -----
    want_xdp2=$(jq -r '.pins.xdp2.rev' "$manifest")
    got_xdp2=$(jq -r '.nodes.xdp2.locked.rev // "ABSENT"' "$src/flake.lock")

    echo "xdp2 (xtcp2's flake input)"
    echo "  manifest  : $want_xdp2"
    echo "  flake.lock: $got_xdp2"
    if [ "$want_xdp2" != "$got_xdp2" ]; then
      echo "  MISMATCH" >&2
      fail=1
    else
      echo "  ok"
    fi
    echo

    # --- pin 2: xdp2's own pin of xtcp2, read out of the xdp2 source -------
    #
    # This is proto-audit's default --xtcp2-src. It is EXPECTED to be stale
    # (see stale_note in the manifest); what must not happen is it changing
    # value without anyone noticing, because a bump would silently change what
    # every non-overridden proto-audit invocation audits.
    want_emb=$(jq -r '.pins["xdp2-embedded-xtcp2"].rev' "$manifest")
    sources="$xdp2Src/nix/proto-audit-sources.nix"

    echo "xtcp2 (xdp2's embedded snapshot — proto-audit's default source)"
    if [ ! -f "$sources" ]; then
      echo "  CANNOT VERIFY: $sources not found." >&2
      echo "  xdp2 has moved this file; update nix/checks/upstream-pins.nix." >&2
      fail=1
    else
      # xtcp2Rev = "<40 hex>";  — take the first such assignment.
      got_emb=$(sed -n 's/.*xtcp2Rev[[:space:]]*=[[:space:]]*"\([0-9a-f]\{40\}\)".*/\1/p' \
        "$sources" | head -n1)
      : "''${got_emb:=ABSENT}"

      echo "  manifest : $want_emb"
      echo "  xdp2 tree: $got_emb"
      if [ "$got_emb" = "ABSENT" ]; then
        echo "  CANNOT VERIFY: no xtcp2Rev assignment found in $sources." >&2
        fail=1
      elif [ "$want_emb" != "$got_emb" ]; then
        echo "  MISMATCH — xdp2 has bumped its xtcp2 pin." >&2
        echo "  This changes what proto-audit audits by default. Confirm the" >&2
        echo "  oracle still overrides --xtcp2-src, then update the manifest." >&2
        fail=1
      else
        echo "  ok (stale by design — see stale_note in the manifest)"
      fi
    fi
    echo

    # --- pin 3: iproute2, the netlink parity target ------------------------
    #
    # Not a git rev: a nixpkgs package version, compared against the value the
    # evaluator resolved for this very build. What it catches is a nixpkgs bump
    # moving `ip` underneath the goip request expectations, which are byte-level
    # and derived from iproute2's source rather than the kernel's.
    want_ip=$(jq -r '.package_pins.iproute2.version' "$manifest")

    echo "iproute2 (the netlink parity target)"
    echo "  manifest    : $want_ip"
    echo "  pkgs.iproute2: $iproute2Version"
    if [ "$want_ip" = "null" ]; then
      echo "  CANNOT VERIFY: .package_pins.iproute2.version is absent." >&2
      fail=1
    elif [ "$want_ip" != "$iproute2Version" ]; then
      echo "  MISMATCH — nixpkgs has moved iproute2." >&2
      echo "  The recorded request bytes and every ip_* sidecar under" >&2
      echo "  pkg/xtcpnl/testdata/ were produced by $want_ip. Read this pin's" >&2
      echo "  on_bump note, regenerate the fixtures, and diff the sidecars" >&2
      echo "  before changing any Go expectation." >&2
      fail=1
    else
      echo "  ok"
    fi
    echo

    if [ "$fail" != "0" ]; then
      echo "FAIL: nix/upstream-pins.json is out of date with the actual pins." >&2
      echo >&2
      echo "A pin moved without the manifest being updated. Update the rev in" >&2
      echo "nix/upstream-pins.json, re-measure the drift with" >&2
      echo "  nix run .#check-upstream-pins" >&2
      echo "and record the new numbers in its stale_note." >&2
      exit 1
    fi

    echo "All pins match the manifest."
    echo
    echo "This check cannot see whether upstream main has MOVED — the sandbox"
    echo "has no network. For that, run:  nix run .#check-upstream-pins"

    mkdir -p $out
    cp "$manifest" $out/upstream-pins.json
    {
      echo "xdp2=$got_xdp2"
      echo "xdp2-embedded-xtcp2=$want_emb"
      echo "iproute2=$iproute2Version"
    } > $out/verified-pins.txt
  ''
