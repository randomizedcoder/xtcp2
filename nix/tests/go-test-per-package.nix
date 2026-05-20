# nix/tests/go-test-per-package.nix
#
# Per-package Go test runners. Each target runs `go test` against one
# subtree so failures localise cleanly and per-package coverage profiles
# are independent. Today's `nix/tests/go-unit.nix` only covers
# pkg/xtcpnl; this module covers the rest.
#
# These targets are NOT in `nix flake check` (the quality-report
# derivation already runs `go test ./...` end-to-end, so adding them to
# the default check set would be duplicate work). They're buildable on
# demand for fast localised re-runs:
#
#   nix build .#test-pkg-xtcp
#   nix build .#test-pkg-io-uring
#   nix build .#test-tools-quality-report
#
# Output per derivation:
#   $out/test.log         — `go test -v` output
#   $out/coverage.out     — per-package coverage profile
#
{
  pkgs,
  lib,
  vendoredSource,
}:

let
  versions = import ../versions.nix { inherit pkgs; };

  # name → relative test path (passed to `go test`). Keep the set
  # focused on packages that have non-trivial test surface; tools/demo
  # binaries that already get coverage via their existing
  # `_test.go` files are listed too.
  packages = {
    "pkg-xtcp" = "./pkg/xtcp/...";
    "pkg-xtcpnl" = "./pkg/xtcpnl/...";
    "pkg-io-uring" = "./pkg/io_uring/...";
    "pkg-misc" = "./pkg/misc/...";
    "tools-quality-report" = "./tools/quality-report/...";
    "tools-netlink-audit" = "./tools/netlink-audit/...";
    "tools-iouring-audit" = "./tools/iouring-audit/...";
    "tools-metrics-audit" = "./tools/metrics-audit/...";
    "tools-proto-field-audit" = "./tools/proto-field-audit/...";
    "cmd-xtcp2" = "./cmd/xtcp2/...";
    "cmd-xtcp2client" = "./cmd/xtcp2client/...";
  };

  mkPkgTest =
    name: path:
    pkgs.runCommand "xtcp2-test-${name}"
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

        mkdir -p $out
        set +e
        go test -v \
          -covermode=atomic \
          -coverprofile=$out/coverage.out \
          ${path} \
          > $out/test.log 2>&1
        rc=$?
        set -e
        if [ "$rc" -ne 0 ]; then
          echo "===== test.log =====" >&2
          cat $out/test.log >&2
          exit "$rc"
        fi
        echo "test-${name} OK (path: ${path})" >&2
      '';
in
lib.mapAttrs' (name: path: {
  name = "test-${name}";
  value = mkPkgTest name path;
}) packages
