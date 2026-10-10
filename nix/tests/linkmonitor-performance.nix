{
  pkgs,
  src,
  vendoredSource,
}:
let
  versions = import ../versions.nix { inherit pkgs; };
  rdma = import ../lib/linkmonitor-rdma.nix { inherit pkgs; };
in
pkgs.runCommand "xtcp2-test-linkmonitor-performance"
  {
    nativeBuildInputs = [
      versions.go
      versions.golangci-lint
      pkgs.python3
    ]
    ++ rdma.nativeBuildInputs;
    inherit (rdma) buildInputs;
  }
  ''
    set -euo pipefail
    cp -r ${vendoredSource} ./xtcp2
    chmod -R u+w ./xtcp2
    cd xtcp2
    export GOTOOLCHAIN=local GOFLAGS=-mod=vendor
    export GOCACHE="$TMPDIR/go-cache" GOMODCACHE="$TMPDIR/go-modcache"
    export GOLANGCI_LINT_CACHE="$TMPDIR/lint-cache"
    mkdir -p "$out/bin"
    printf '%s\n' '${src}' > "$out/source.txt"
    go version > "$out/go-version.txt"
    for variant in core rdma; do
      tags=monitor_bench
      export CGO_ENABLED=0
      if [ "$variant" = rdma ]; then tags=monitor_bench,rdma; export CGO_ENABLED=1; fi
      go test -tags "$tags" -c -o "$out/bin/$variant.test" ./pkg/linkmonitor
      "$out/bin/$variant.test" -test.run '^TestPerformance' -test.v > "$out/$variant-smoke.txt"
      "$out/bin/$variant.test" -test.run '^$' -test.bench '^BenchmarkPerformance' -test.benchtime=1x > "$out/$variant-bench-smoke.txt"
    done
    CGO_ENABLED=1 go test -tags monitor_bench,rdma -c -o "$out/bin/rdmaevents.test" ./pkg/linkmonitor/internal/rdmaevents
    CGO_ENABLED=1 go test -tags monitor_bench,rdma -c -o "$out/bin/rdmacaps.test" ./pkg/linkmonitor/internal/rdmacaps
    "$out/bin/rdmaevents.test" -test.run '^TestPerformance' -test.v > "$out/rdmaevents-smoke.txt"
    CGO_ENABLED=1 go test -tags monitor_bench,rdma -race -count=3 ./pkg/linkmonitor/... -run '^TestPerformance' > "$out/race.txt"
    CGO_ENABLED=1 golangci-lint run --config .golangci-comprehensive.yml --build-tags monitor_bench,rdma ./pkg/linkmonitor/... > "$out/lint.txt" 2>&1 || { cat "$out/lint.txt"; exit 1; }
    python3 -m unittest discover -s nix/tests -p test_linkmonitor_performance.py
    cp nix/tests/linkmonitor_performance.py "$out/runner.py"
    ln -s ${rdma.runtime} "$out/runtime"
  ''
