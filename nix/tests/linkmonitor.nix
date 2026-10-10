# Focused, offline monitor gates. Use path:. while sources are untracked.
# Every leaf retains logs and provenance; the aggregate links all leaf outputs.
{
  pkgs,
  lib,
  src,
  vendoredSource,
  linkmonitorPackages,
}:
let
  versions = import ../versions.nix { inherit pkgs; };
  paths = "./cmd/go-link-monitor ./pkg/linkmonitor/... ./pkg/xtcpnl ./nix/tests/linkmonitor-smoke";
  mkCheck =
    name:
    {
      command,
      race ? false,
      extraInputs ? [ ],
    }:
    pkgs.runCommand "xtcp2-test-linkmonitor-${name}"
      {
        nativeBuildInputs = [ versions.go ] ++ lib.optional race pkgs.gcc ++ extraInputs;
        inherit vendoredSource;
      }
      ''
        set -euo pipefail
        cp -r "$vendoredSource" ./xtcp2
        chmod -R u+w ./xtcp2
        cd ./xtcp2
        export GOTOOLCHAIN=local GOFLAGS=-mod=vendor
        export CGO_ENABLED=${if race then "1" else "0"}
        export GOCACHE="$TMPDIR/go-cache" GOMODCACHE="$TMPDIR/go-modcache"
        export GOLANGCI_LINT_CACHE="$TMPDIR/lint-cache"
        mkdir -p "$out"
        cp ${pkgs.writeText "linkmonitor-${name}-command.sh" command} "$out/command.sh"
        printf '%s\n' '${src}' > "$out/source.txt"
        printf '%s\n' "$vendoredSource" > "$out/vendored-source.txt"
        go version > "$out/go-version.txt"
        uname -srmo > "$out/kernel.txt"
        printf 'CGO_ENABLED=%s\nGOFLAGS=%s\nGOTOOLCHAIN=%s\n' \
          "$CGO_ENABLED" "$GOFLAGS" "$GOTOOLCHAIN" > "$out/environment.txt"
        ${pkgs.bash}/bin/bash -euo pipefail "$out/command.sh" > "$out/check.log" 2>&1 || {
          rc=$?
          cat "$out/check.log" >&2
          exit "$rc"
        }
        echo 'PASS: test-linkmonitor-${name}'
      '';
  rdma = import ./linkmonitor-rdma.nix {
    inherit
      pkgs
      src
      vendoredSource
      linkmonitorPackages
      ;
  };
  leaves = {
    test-linkmonitor-embedding = import ./linkmonitor-embedding.nix {
      inherit pkgs src vendoredSource;
    };
    test-linkmonitor-unit = mkCheck "unit" {
      command = ''
        go test -json -count=1 -timeout=5m ${paths}
        go test ./pkg/linkmonitor -run '^$' -bench '^BenchmarkPrometheus' \
          -benchmem -benchtime=100ms > "$out/prometheus-benchmarks.txt"
      '';
    };
    test-linkmonitor-race = mkCheck "race" {
      race = true;
      command = "go test -race -json -count=1 -timeout=5m ${paths}";
    };
    test-linkmonitor-vet = mkCheck "vet" {
      command = "go vet ${paths}";
    };
    test-linkmonitor-lint = mkCheck "lint" {
      extraInputs = [ versions.golangci-lint ];
      command = ''
        golangci-lint version > "$out/lint-version.txt"
        golangci-lint run --config .golangci-comprehensive.yml ${paths}
      '';
    };
    test-linkmonitor-format = mkCheck "format" {
      command = ''
        unformatted=$(gofmt -l cmd/go-link-monitor pkg/linkmonitor pkg/xtcpnl nix/tests/linkmonitor-smoke)
        if [ -n "$unformatted" ]; then
          printf '%s\n' "$unformatted"
          exit 1
        fi
      '';
    };
    test-linkmonitor-replay = mkCheck "replay" {
      extraInputs = [ pkgs.python3 ];
      command = ''
        go test -json -count=1 -timeout=5m ./pkg/xtcpnl \
          -run '^TestLinkState(Route)?KernelFixtures$' > "$out/replay.jsonl" || {
          rc=$?
          cat "$out/replay.jsonl"
          exit "$rc"
        }
        python3 nix/tests/linkmonitor_report.py replay "$out/replay.jsonl"
      '';
    };
    test-linkmonitor-fuzz = mkCheck "fuzz" {
      command = ''
        export GOMAXPROCS=2
        for target in FuzzParseEthtool FuzzWalkNetlinkEnvelopes FuzzParseMonitorLink; do
          go test ./pkg/xtcpnl -run '^$' -fuzz "^$target$" \
            -fuzztime=30s -parallel=2 -timeout=5m > "$out/$target.log" 2>&1 || {
            rc=$?
            cat "$out/$target.log"
            exit "$rc"
          }
        done
      '';
    };
    test-linkmonitor-docs = mkCheck "docs" {
      extraInputs = [ pkgs.python3 ];
      command = ''
        python3 -m unittest discover -s nix/tests -p test_linkmonitor_report.py
        python3 nix/tests/linkmonitor_report.py docs .
      '';
    };
  }
  // rdma;
in
leaves
// {
  test-linkmonitor = pkgs.linkFarm "xtcp2-test-linkmonitor" (
    lib.mapAttrsToList (name: path: { inherit name path; }) leaves
  );
}
