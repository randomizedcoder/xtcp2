# nix/tests/go-bench.nix
#
# Runs the Go benchmarks. Not part of `nix flake check` (slow + impure).
#
{
  pkgs,
  lib,
  vendoredSource,
}:

let
  versions = import ../versions.nix { inherit pkgs; };
in
pkgs.writeShellApplication {
  name = "xtcp2-go-bench";
  runtimeInputs = [ versions.go ];
  text = ''
    set -euo pipefail
    workdir=$(mktemp -d)
    trap 'rm -rf "$workdir"' EXIT
    cp -r ${vendoredSource}/. "$workdir"
    chmod -R +w "$workdir"
    cd "$workdir"
    export CGO_ENABLED=0
    export GOFLAGS=-mod=vendor
    go test -bench=. -benchmem ./pkg/xtcpnl/...
  '';
}
