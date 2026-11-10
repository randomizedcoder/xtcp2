# nix/iproute2-7-3-0.nix
#
# iproute2 7.3.0, the netlink parity target (nix/upstream-pins.json,
# package_pins.iproute2). nixpkgs still ships 7.2.0, and overriding
# pkgs.iproute2 everywhere would rebuild every package that depends on it,
# so this is a package of its own, used in two places:
#
#   - the goip-parity guest's `ip` and its capture helper (mkVm.nix
#     parityIproute2), so the matrix runs ip 7.3.0 against goip;
#   - the workbench's `nlcap-ws batch --ip`, via the flake's
#     `.#iproute2_7_3_0` (NC0-T01 of docs/netlink/goip-parity-next.md).
#
# Every other VM's `ip` stays on nixpkgs'. nixpkgs' recipe has no patches,
# so only the tarball changes. Drop this file when nixpkgs reaches 7.3.0.
{ pkgs }:
pkgs.iproute2.overrideAttrs (_: {
  version = "7.3.0";
  src = pkgs.fetchurl {
    url = "mirror://kernel/linux/utils/net/iproute2/iproute2-7.3.0.tar.xz";
    hash = "sha256-XBeJ0pC7oEimE17Rq4GwQC3Q+1KNHxQhdlxCsfw9Yok=";
  };
})
