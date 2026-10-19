# RDMA checks, kept separate from the pure-Go gates. Since R4-T02 every
# build is CGO_ENABLED=0: RDMA events come from netlink and InfiniBand
# PortInfo from the umad device, so no binary links rdma-core. rdma-core
# remains only as test tooling (libibmad's PortInfo decoder cross-check).
{
  pkgs,
  src,
  vendoredSource,
  linkmonitorPackages,
}:
let
  versions = import ../versions.nix { inherit pkgs; };
  monitor = linkmonitorPackages.go-link-monitor;
  # The packaging table's no_runtime row: nothing in the closure is rdma-core.
  closure = pkgs.closureInfo { rootPaths = [ monitor ]; };
  mkCheck =
    name: extraInputs: command:
    pkgs.runCommand "xtcp2-test-linkmonitor-rdma-${name}"
      {
        nativeBuildInputs = [
          versions.go
          versions.golangci-lint
          pkgs.binutils
        ]
        ++ extraInputs;
      }
      ''
        set -euo pipefail
        cp -r ${vendoredSource} ./xtcp2
        chmod -R u+w ./xtcp2
        cd xtcp2
        export GOTOOLCHAIN=local GOFLAGS=-mod=vendor CGO_ENABLED=0
        export GOCACHE="$TMPDIR/go-cache" GOMODCACHE="$TMPDIR/go-modcache"
        export GOLANGCI_LINT_CACHE="$TMPDIR/lint-cache"
        mkdir -p "$out"
        printf '%s\n' '${src}' > "$out/source.txt"
        printf '%s\n' '${vendoredSource}' > "$out/vendored-source.txt"
        go version > "$out/go-version.txt"
        uname -srmo > "$out/kernel.txt"
        printf 'CGO_ENABLED=%s\nGOFLAGS=%s\nGOTOOLCHAIN=%s\n' \
          "$CGO_ENABLED" "$GOFLAGS" "$GOTOOLCHAIN" > "$out/environment.txt"
        cp ${pkgs.writeText "linkmonitor-rdma-${name}.sh" command} "$out/command.sh"
        bash -euo pipefail "$out/command.sh" > "$out/check.log" 2>&1 || {
          cat "$out"/*.log >&2
          exit 1
        }
      '';
  # no_needed: a static binary, with no NEEDED entries in its dynamic section.
  noNeeded = ''
    no_needed() {
      if readelf -d "$1" | grep NEEDED; then
        echo "$1 has dynamic dependencies" >&2
        exit 1
      fi
    }
  '';
in
{
  test-linkmonitor-rdma-build = mkCheck "build" [ ] ''
    mkdir -p "$out/bin"
    go build -trimpath -o "$out/bin/linkmonitor-smoke" ./nix/tests/linkmonitor-smoke
    go test -tags rdma_vm -c -o "$out/bin/rdma-vm.test" ./pkg/linkmonitor
    go test -tags ionic_vm -c -o "$out/bin/ionic-vm.test" ./pkg/linkmonitor
    go test -tags nlrecord -c -o "$out/bin/linuxio-nlrecord.test" ./pkg/linkmonitor/internal/linuxio
    # The live umad rows (R2-T03).
    go test -tags rdma_vm -c -o "$out/bin/rdmacaps-vm.test" ./pkg/linkmonitor/internal/rdmacaps
    ${noNeeded}
    for binary in "$out"/bin/*; do
      no_needed "$binary"
    done
    readelf -d "$out/bin/linkmonitor-smoke" > "$out/elf.txt"
    env -i HOME="$TMPDIR" TMPDIR="$TMPDIR" "$out/bin/linkmonitor-smoke" -sandbox > "$out/smoke.log" 2>&1
    go test ./pkg/linkmonitor/... -run 'TestProduction|TestRunFailures|Unavailable' -count=1
    # cgo_on_still_builds, as amended by R4-T02: every remaining tag
    # combination builds without cgo, and the retired rdma tag is inert.
    for tags in "" rdma rdma_vm ionic_vm nlrecord monitor_bench embedding_vm; do
      go test -tags "$tags" -run '^$' -count=1 ./cmd/go-link-monitor ./pkg/linkmonitor/... > /dev/null
    done
  '';
  test-linkmonitor-rdma-unit = mkCheck "unit" [ pkgs.gcc ] ''
    go test -json -count=1 -timeout=5m ./cmd/go-link-monitor ./pkg/linkmonitor/... | tee "$out/unit.jsonl"
    # The race detector needs cgo; nothing under test uses it.
    CGO_ENABLED=1 go test -race -json -count=1 -timeout=5m ./cmd/go-link-monitor ./pkg/linkmonitor/... | tee "$out/race.jsonl"
    CGO_ENABLED=1 go test -race -count=10 -timeout=5m ./pkg/linkmonitor/... \
      -run 'Prometheus|Production|Lifecycle|RDMAEvent|RDMANotif|RDMAMonitorMode|RDMAOptional|RDMACapability|RDMACounter|RDMAHWCounters|RDMAHandle|RDMAIdentityOnReopen|RDMAParallelReads|RDMACollectorsShared|RDMASteadyState|RDMAStat|RDMAScalar|LocalQuery|CapabilityEncoding|UMAD|Summary|Problem|Category|Evidence|Interface|SnapshotDevice|OnDemand|Tier|CollectionMode|RDMAWatchPartition|RDMAPartitionHandles|PollModeUnchanged' > "$out/repeated-race.log" 2>&1
    GOMAXPROCS=2 go test ./pkg/linkmonitor/internal/linuxio -run '^$' \
      -fuzz '^FuzzRDMANotifications$' -fuzztime=30s -parallel=2 -timeout=5m > "$out/fuzz.log" 2>&1
    GOMAXPROCS=2 go test ./pkg/linkmonitor/internal/linuxio -run '^$' \
      -fuzz '^FuzzRDMAStat$' -fuzztime=30s -parallel=2 -timeout=5m > "$out/stat-fuzz.log" 2>&1
    GOMAXPROCS=2 go test ./pkg/linkmonitor/internal/rdmacaps -run '^$' \
      -fuzz '^FuzzCapabilityReply$' -fuzztime=30s -parallel=2 -timeout=5m > "$out/capability-fuzz.log" 2>&1
    GOMAXPROCS=2 go test ./pkg/linkmonitor -run '^$' \
      -bench '^BenchmarkRDMAEventTargets$' -benchmem -benchtime=100ms > "$out/bench.log" 2>&1
    GOMAXPROCS=2 go test ./pkg/linkmonitor -run '^$' \
      -bench '^(BenchmarkRDMACounterRead|BenchmarkRDMAGIDScan)$' -benchmem -benchtime=100ms >> "$out/bench.log" 2>&1
  '';
  test-linkmonitor-rdma-lint = mkCheck "lint" [ ] ''
    go vet ./cmd/go-link-monitor ./pkg/linkmonitor/... ./nix/tests/linkmonitor-smoke
    go vet -tags rdma_vm,nlrecord ./pkg/linkmonitor/...
    go vet -tags ionic_vm ./pkg/linkmonitor/...
    golangci-lint run --config .golangci-comprehensive.yml ./cmd/go-link-monitor ./pkg/linkmonitor/... ./nix/tests/linkmonitor-smoke
  '';
  test-linkmonitor-command = mkCheck "command" [ ] ''
    binary=${monitor}/bin/go-link-monitor
    env -i "$binary" -help > "$out/help.txt"
    env -i "$binary" -version > "$out/version.txt"
    readelf -d "$binary" > "$out/elf.txt"
    ${noNeeded}
    no_needed "$binary"
    # no_runtime: no rdma-core in the closure and no runtime link.
    cp ${closure}/store-paths "$out/closure.txt"
    if grep rdma-core "$out/closure.txt"; then exit 1; fi
    # source.txt names the source tree without referencing it.
    if grep -E -- '-source$' "$out/closure.txt"; then exit 1; fi
    grep -qx '${src}' ${monitor}/share/go-link-monitor/source.txt
    stat -c '%s %n' "$binary" > "$out/size.txt"
    du -sbL $(cat "$out/closure.txt") | awk '{ total += $1 } END { print total, "closure bytes" }' >> "$out/size.txt"
    test ! -e ${monitor}/share/go-link-monitor/rdma-runtime
    # go-link-monitor-core is an alias of the same package.
    test ${monitor} = ${linkmonitorPackages.go-link-monitor-core}
    ln -sfn "$binary" cmd/go-link-monitor/go-link-monitor-artifact
    LINKMONITOR_TEST_ARTIFACT=1 go test -v -count=1 -timeout=90s \
      ./cmd/go-link-monitor -run '^TestExecutable' > "$out/process.log" 2>&1
    go test ./cmd/go-link-monitor -count=1 -timeout=90s
    ln -s ${monitor} "$out/full"
    ln -s ${monitor} "$out/core"
  '';
  test-linkmonitor-rdma-runtime =
    mkCheck "runtime"
      [
        pkgs.gcc
        pkgs.pkg-config
        pkgs.rdma-core
      ]
      ''
        # Every constant pkg/xtcpnl declares from the header, which are all the
        # nldev numbers linkmonitor uses (internal/linuxio/rdma*_linux.go).
        cc -I${pkgs.linuxHeaders}/include -x c -fsyntax-only \
          -include rdma/rdma_netlink.h pkg/xtcpnl/testdata/rdma_consts.h
        mkdir -p "$out/bin"
        # libibmad's PortInfo decoder, the reference rdmacaps' decode is
        # checked against. Test tooling only: nothing the monitor links.
        cc -Wall -Wextra -Werror $(pkg-config --cflags libibmad) \
          -I${pkgs.linuxHeaders}/include nix/tests/linkmonitor-portinfo-test.c \
          $(pkg-config --libs libibmad) -o "$out/bin/portinfo-decoder-test"
        "$out/bin/portinfo-decoder-test"
        printf '%s\n' '${pkgs.rdma-core}' > "$out/rdma-core.txt"
      '';
}
