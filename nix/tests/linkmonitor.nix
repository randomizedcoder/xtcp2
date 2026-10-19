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
  rdmaNativeCapture = import ./rdma-native-capture.nix { inherit pkgs; };
  # pkg/nlconn is linkmonitor's netlink transport, moved out of
  # pkg/linkmonitor/internal so pkg/rtnlmon can share it; its gates stay here.
  paths = "./cmd/go-link-monitor ./pkg/linkmonitor/... ./pkg/nlconn ./pkg/xtcpnl ./nix/tests/linkmonitor-smoke";
  # Fuzz targets as "<package>:<target>"; each runs for 30s in the fuzz leaf.
  fuzzTargets = [
    "./pkg/xtcpnl:FuzzParseEthtool"
    "./pkg/xtcpnl:FuzzWalkNetlinkEnvelopes"
    "./pkg/xtcpnl:FuzzParseMonitorLink"
    "./pkg/xtcpnl:FuzzParseChardev"
    "./pkg/linkmonitor/internal/linuxio:FuzzRDMANotifications"
    "./pkg/linkmonitor/internal/rdmanative:FuzzTranslateRtnl"
    "./pkg/linkmonitor/internal/rdmacaps:FuzzUMADHeader"
  ];
  # go-native RDMA decoders held to the parsing rules in
  # cmd/go-link-monitor/GO-NATIVE-RDMA-DESIGN.md. Each decoder task appends
  # its files; the parse-guards leaf fails if a listed file is missing.
  rdmaNativePackages = "./pkg/linkmonitor/internal/rdmanative ./pkg/linkmonitor/internal/rdmacaps";
  rdmaDecoderFiles = [
    "pkg/linkmonitor/internal/rdmanative/abi.go"
    "pkg/linkmonitor/internal/rdmacaps/umadhdr.go"
    "pkg/xtcpnl/xtcpnl_rdma_chardev.go"
    "pkg/linkmonitor/internal/rdmanative/rtnl_decode.go"
    "pkg/linkmonitor/internal/rdmanative/async.go"
  ];
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
    test-linkmonitor-performance = import ./linkmonitor-performance.nix {
      inherit pkgs src vendoredSource;
    };
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
        for entry in ${lib.escapeShellArgs fuzzTargets}; do
          pkg=''${entry%%:*} target=''${entry#*:}
          go test "$pkg" -run '^$' -fuzz "^$target$" \
            -fuzztime=30s -parallel=2 -timeout=5m > "$out/$target.log" 2>&1 || {
            rc=$?
            cat "$out/$target.log"
            exit "$rc"
          }
        done
      '';
    };
    # ABI layout guard (GO-NATIVE-RDMA-IMPLEMENTATION-PLAN.md R0-T01): the C
    # program prints kernel sizes, offsets and ioctl numbers from the pinned
    # headers; the committed copy must match it exactly, and each package's
    # TestABILayout compares its Go constants with that copy.
    test-linkmonitor-rdma-abi = mkCheck "rdma-abi" {
      extraInputs = [ pkgs.gcc ];
      command = ''
        printf 'linuxHeaders %s\nrdma-core %s\n' '${pkgs.linuxHeaders.version}' \
          '${pkgs.rdma-core.version}' > "$out/headers.txt"
        gcc -Wall -Wextra -Werror -isystem ${pkgs.linuxHeaders}/include \
          -I${lib.getDev pkgs.rdma-core}/include \
          -o "$TMPDIR/linkmonitor-abi" nix/tests/linkmonitor-abi.c
        "$TMPDIR/linkmonitor-abi" > "$out/abi_layout.txt"
        diff -u pkg/linkmonitor/internal/rdmanative/testdata/abi_layout.txt "$out/abi_layout.txt"
        go test -count=1 -v -run '^TestABILayout$' ${rdmaNativePackages}
      '';
    };
    # Parse guards (R0-T04): the monitor has no cgo files (rdmanative since
    # R0-T04; every package, under every build tag, since R4-T02 removed the
    # verbs and libibumad backends), no bounds check beyond the length hint
    # in any listed decoder file, the -m escape report kept as escape.txt,
    # and the zero-allocation and manifest tests.
    test-linkmonitor-parse-guards = mkCheck "parse-guards" {
      command = ''
        # Every package of the monitor and its tests, under every build tag.
        for tags in "" rdma rdma_vm ionic_vm nlrecord monitor_bench embedding_vm; do
          cgo=$(CGO_ENABLED=1 go list -tags "$tags" -test -deps \
            -f '{{if .CgoFiles}}{{.ImportPath}}: {{join .CgoFiles " "}}{{end}}' \
            ./cmd/go-link-monitor ./pkg/linkmonitor/... | grep -E 'xtcp2/(cmd/go-link-monitor|pkg/linkmonitor)' || true)
          if [ -n "$cgo" ]; then
            echo "go-link-monitor must stay cgo-free (tags '$tags'): $cgo"
            exit 1
          fi
        done
        ${pkgs.bash}/bin/bash nix/tests/linkmonitor-parse-guards.sh "$out" \
          ${lib.escapeShellArgs rdmaDecoderFiles}
        go test -count=1 -v -run 'Allocs|Manifest' ${rdmaNativePackages} ./pkg/xtcpnl \
          > "$out/allocs.txt"
      '';
    };
    # Capture tooling (R0-T02): the redaction and manifest tests, then a dry
    # run of the capture script through the manifest tool. No real value may
    # survive the redaction, the result must pass TestCaptureManifests, and a
    # tampered copy must fail verify.
    test-linkmonitor-rdma-capture = mkCheck "rdma-capture" {
      extraInputs = [ rdmaNativeCapture ];
      command = ''
        go test -count=1 -v ./pkg/linkmonitor/internal/rdmacapture/... > "$out/rdmacapture-tests.txt"
        go build -o "$TMPDIR/rdma-capture-manifest" ./pkg/linkmonitor/internal/rdmacapture/cmd/rdma-capture-manifest
        stage="$TMPDIR/stage"
        values="$TMPDIR/values"
        rdma-native-capture --dry-run --out "$stage" --dev mlx5_0 --dev mlx5_1 --values "$values" \
          --triggers gid,flap --iface eth0 --umad --umad-live loaded --umad-tests /nonexistent \
          --driver mlx5_core > "$out/dry-run.txt"
        test -e "$stage/umad/native_loaded-mlx5_1.strace"
        # Since R4-T02 there is no libibumad build to trace.
        test ! -e "$stage/umad/libibumad_loaded-mlx5_1.strace"
        "$TMPDIR/rdma-capture-manifest" write -dir "$stage" -source dry-run -values "$values"
        if [ ! -s "$values" ]; then
          echo "the dry run recorded no values to redact"
          exit 1
        fi
        while read -r kind value; do
          if grep -rqiF -- "$value" "$stage"; then
            echo "$kind $value survived the redaction"
            exit 1
          fi
        done < "$values"
        cp "$stage/manifest.json" "$out/manifest.json"
        LINKMONITOR_CAPTURE_DIRS="$stage" go test -count=1 -v -run '^TestCaptureManifests$' \
          ./pkg/linkmonitor/internal/rdmanative > "$out/manifests.txt"
        cp -r "$stage" "$TMPDIR/tampered"
        echo x >> "$TMPDIR/tampered/sysfs/mlx5_0/node_guid"
        if "$TMPDIR/rdma-capture-manifest" verify "$TMPDIR/tampered" > "$out/tampered.txt" 2>&1; then
          echo "a tampered capture passed verify"
          exit 1
        fi
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
