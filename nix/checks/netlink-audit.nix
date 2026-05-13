# nix/checks/netlink-audit.nix
#
# Runs the custom Go analyzer at tools/netlink-audit against pkg/xtcpnl.
#
{
  pkgs,
  lib,
  vendoredSource,
}:

let
  versions = import ../versions.nix { inherit pkgs; };
in
pkgs.runCommand "xtcp2-netlink-audit"
  {
    nativeBuildInputs = [ versions.go ];
    inherit vendoredSource;
  }
  ''
    cp -r $vendoredSource ./xtcp2 && chmod -R +w ./xtcp2
    cd ./xtcp2
    export HOME=$(mktemp -d)
    export CGO_ENABLED=0
    export GOFLAGS=-mod=vendor
    go run ./tools/netlink-audit -root pkg/xtcpnl > $out 2>&1 || (cat $out && exit 1)
  ''
