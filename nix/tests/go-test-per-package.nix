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

  # name → { path; race; }. `path` is the relative test path passed to
  # `go test`. Keep the set focused on packages that have non-trivial test
  # surface; tools/demo binaries that already get coverage via their existing
  # `_test.go` files are listed too.
  #
  # `race` is opt-in per package and defaults off. `nix/tests/go-test-race.nix`
  # already runs `go test -race ./...`, so every package here is covered by the
  # race detector *somewhere*; what that whole-repo run is not, is fast or
  # localised. pkg/io_uring is the one package where the concurrency contract
  # (io_uring's IORING_SETUP_SINGLE_ISSUER — the submitter thread is fixed at
  # ring creation and the kernel rejects submissions from any other) is the
  # thing most likely to be broken by a change, so it gets its own cheap race
  # run to reach for while iterating. The other eleven stay CGO_ENABLED=0 and
  # fast.
  packages = {
    "pkg-xtcp" = {
      path = "./pkg/xtcp/...";
      race = false;
    };
    "pkg-xtcpnl" = {
      path = "./pkg/xtcpnl/...";
      race = false;
    };
    "pkg-io-uring" = {
      path = "./pkg/io_uring/...";
      race = true;
    };
    "pkg-misc" = {
      path = "./pkg/misc/...";
      race = false;
    };
    "tools-quality-report" = {
      path = "./tools/quality-report/...";
      race = false;
    };
    "tools-netlink-audit" = {
      path = "./tools/netlink-audit/...";
      race = false;
    };
    "tools-iouring-audit" = {
      path = "./tools/iouring-audit/...";
      race = false;
    };
    "tools-metrics-audit" = {
      path = "./tools/metrics-audit/...";
      race = false;
    };
    "tools-proto-field-audit" = {
      path = "./tools/proto-field-audit/...";
      race = false;
    };
    "cmd-xtcp2" = {
      path = "./cmd/xtcp2/...";
      race = false;
    };
    "cmd-xtcp2client" = {
      path = "./cmd/xtcp2client/...";
      race = false;
    };
    "cmd-xtcp2ctl" = {
      path = "./cmd/xtcp2ctl/...";
      race = false;
    };
  };

  mkPkgTest =
    name:
    { path, race }:
    pkgs.runCommand "xtcp2-test-${name}"
      {
        # The race detector is implemented in C, so it needs cgo and a
        # compiler — same reason go-test-race.nix carries gcc.
        nativeBuildInputs = [
          versions.go
        ]
        ++ lib.optional race pkgs.gcc;
        inherit vendoredSource;
      }
      ''
        cp -r $vendoredSource ./xtcp2 && chmod -R +w ./xtcp2
        cd ./xtcp2
        export HOME=$(mktemp -d)
        export CGO_ENABLED=${if race then "1" else "0"}
        export GOFLAGS=-mod=vendor

        mkdir -p $out
        set +e
        go test -v ${lib.optionalString race "-race "}\
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
        echo "test-${name} OK (path: ${path}${lib.optionalString race ", -race"})" >&2
      '';
in
lib.mapAttrs' (name: spec: {
  name = "test-${name}";
  value = mkPkgTest name spec;
}) packages
