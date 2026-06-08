# nix/tests/go-unit.nix
#
# Runs the Go unit test suite — matches today's `make test` (xtcpnl only).
#
{
  pkgs,
  lib,
  vendoredSource,
}:

let
  versions = import ../versions.nix { inherit pkgs; };
in
pkgs.runCommand "xtcp2-go-unit"
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
    go test -v ./pkg/xtcpnl/... > $out 2>&1 || (cat $out && exit 1)
  ''
