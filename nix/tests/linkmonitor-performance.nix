{
  pkgs,
  src,
  vendoredSource,
}:
let
  versions = import ../versions.nix { inherit pkgs; };
in
pkgs.runCommand "xtcp2-test-linkmonitor-performance"
  {
    nativeBuildInputs = [
      versions.go
      versions.golangci-lint
      pkgs.python3
      # The race detector needs cgo; nothing under test uses it.
      pkgs.gcc
    ];
  }
  ''
    set -euo pipefail
    cp -r ${vendoredSource} ./xtcp2
    chmod -R u+w ./xtcp2
    cd xtcp2
    export GOTOOLCHAIN=local GOFLAGS=-mod=vendor CGO_ENABLED=0
    export GOCACHE="$TMPDIR/go-cache" GOMODCACHE="$TMPDIR/go-modcache"
    export GOLANGCI_LINT_CACHE="$TMPDIR/lint-cache"
    mkdir -p "$out/bin"
    printf '%s\n' '${src}' > "$out/source.txt"
    go version > "$out/go-version.txt"
    # One variant since R4-T02 removed the cgo rdma build.
    go test -tags monitor_bench -c -o "$out/bin/core.test" ./pkg/linkmonitor
    "$out/bin/core.test" -test.run '^TestPerformance' -test.v > "$out/core-smoke.txt"
    "$out/bin/core.test" -test.run '^$' -test.bench '^BenchmarkPerformance' -test.benchtime=1x > "$out/core-bench-smoke.txt"
    # The go-native decoders for bench-linkmonitor-rdma-native.
    go test -tags monitor_bench -c -o "$out/bin/rdmanative.test" ./pkg/linkmonitor/internal/rdmanative
    go test -tags monitor_bench -c -o "$out/bin/xtcpnl.test" ./pkg/xtcpnl
    go test -tags monitor_bench -c -o "$out/bin/nlconn.test" ./pkg/nlconn
    go test -tags monitor_bench -c -o "$out/bin/rdmacaps-native.test" ./pkg/linkmonitor/internal/rdmacaps
    "$out/bin/rdmanative.test" -test.run '^$' -test.bench . -test.benchtime=1x > "$out/rdmanative-bench-smoke.txt"
    "$out/bin/xtcpnl.test" -test.run '^$' -test.bench '^BenchmarkParseChardev$' -test.benchtime=1x > "$out/xtcpnl-bench-smoke.txt"
    "$out/bin/nlconn.test" -test.run '^$' -test.bench '^BenchmarkReceive$' -test.benchtime=1x > "$out/nlconn-bench-smoke.txt"
    "$out/bin/rdmacaps-native.test" -test.run '^$' -test.bench 'UMAD|RegisterAgent2' -test.benchtime=1x > "$out/rdmacaps-native-bench-smoke.txt"
    CGO_ENABLED=1 go test -tags monitor_bench -race -count=3 ./pkg/linkmonitor/... -run '^TestPerformance' > "$out/race.txt"
    golangci-lint run --config .golangci-comprehensive.yml --build-tags monitor_bench ./pkg/linkmonitor/... > "$out/lint.txt" 2>&1 || { cat "$out/lint.txt"; exit 1; }
    python3 -m unittest discover -s nix/tests -p test_linkmonitor_performance.py
    cp nix/tests/linkmonitor_performance.py "$out/runner.py"
  ''
