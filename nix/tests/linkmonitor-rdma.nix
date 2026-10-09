# Tagged verbs checks and runtime closure, kept separate from pure-Go gates.
{
  pkgs,
  src,
  vendoredSource,
}:
let
  versions = import ../versions.nix { inherit pkgs; };
  runtime = pkgs.symlinkJoin {
    name = "xtcp2-linkmonitor-rdma-runtime";
    paths = [ pkgs.rdma-core ];
  };
  mkCheck =
    name: command:
    pkgs.runCommand "xtcp2-test-linkmonitor-rdma-${name}"
      {
        nativeBuildInputs = [
          versions.go
          pkgs.gcc
          pkgs.pkg-config
          versions.golangci-lint
          pkgs.binutils
        ];
        buildInputs = [ pkgs.rdma-core ];
      }
      ''
        set -euo pipefail
        cp -r ${vendoredSource} ./xtcp2
        chmod -R u+w ./xtcp2
        cd xtcp2
        export GOTOOLCHAIN=local GOFLAGS=-mod=vendor CGO_ENABLED=1
        export GOCACHE="$TMPDIR/go-cache" GOMODCACHE="$TMPDIR/go-modcache"
        export GOLANGCI_LINT_CACHE="$TMPDIR/lint-cache"
        mkdir -p "$out"
        printf '%s\n' '${src}' > "$out/source.txt"
        printf '%s\n' '${vendoredSource}' > "$out/vendored-source.txt"
        printf '%s\n' '${pkgs.rdma-core}' > "$out/rdma-core.txt"
        go version > "$out/go-version.txt"
        uname -srmo > "$out/kernel.txt"
        printf 'CGO_ENABLED=%s\nGOFLAGS=%s\nGOTOOLCHAIN=%s\nTAGS=rdma\n' \
          "$CGO_ENABLED" "$GOFLAGS" "$GOTOOLCHAIN" > "$out/environment.txt"
        cp ${pkgs.writeText "linkmonitor-rdma-${name}.sh" command} "$out/command.sh"
        bash -euo pipefail "$out/command.sh" > "$out/check.log" 2>&1 || {
          cat "$out"/*.log >&2
          exit 1
        }
      '';
in
{
  test-linkmonitor-rdma-unit = mkCheck "unit" ''
    go test -tags rdma -json -count=1 -timeout=5m ./pkg/linkmonitor/... | tee "$out/unit.jsonl"
    go test -tags rdma -race -json -count=1 -timeout=5m ./pkg/linkmonitor/... | tee "$out/race.jsonl"
    go test -tags rdma -race -count=10 -timeout=5m ./pkg/linkmonitor/... \
      -run 'RDMAEvent|PollSource|FatalContext|VerbsCopy|RDMANotif|RDMAMonitorMode|RDMAOptional|RDMACapability|RDMACounter|LocalQuery|CapabilityEncoding|UMAD' > "$out/repeated-race.log" 2>&1
    GOMAXPROCS=2 go test ./pkg/linkmonitor/internal/linuxio -run '^$' \
      -fuzz '^FuzzRDMANotifications$' -fuzztime=30s -parallel=2 -timeout=5m > "$out/fuzz.log" 2>&1
    GOMAXPROCS=2 go test ./pkg/linkmonitor/internal/rdmacaps -run '^$' \
      -fuzz '^FuzzCapabilityReply$' -fuzztime=30s -parallel=2 -timeout=5m > "$out/capability-fuzz.log" 2>&1
    GOMAXPROCS=2 go test ./pkg/linkmonitor/internal/rdmaevents -run '^$' \
      -bench '^BenchmarkEventDelivery$' -benchmem -benchtime=100ms > "$out/bench.log" 2>&1
    GOMAXPROCS=2 go test ./pkg/linkmonitor -run '^$' \
      -bench '^BenchmarkRDMAEventTargets$' -benchmem -benchtime=100ms >> "$out/bench.log" 2>&1
    GOMAXPROCS=2 go test ./pkg/linkmonitor -run '^$' \
      -bench '^BenchmarkRDMACounterRead$' -benchmem -benchtime=100ms >> "$out/bench.log" 2>&1
  '';
  test-linkmonitor-rdma-lint = mkCheck "lint" ''
    go vet -tags rdma ./pkg/linkmonitor/...
    golangci-lint run --config .golangci-comprehensive.yml --build-tags rdma ./pkg/linkmonitor/...
  '';
  test-linkmonitor-rdma-runtime = mkCheck "runtime" ''
    cc -I${pkgs.linuxHeaders}/include -x c -fsyntax-only - <<'HEADER'
    #include <rdma/rdma_netlink.h>
    _Static_assert(RDMA_NL_GROUP_NOTIFY == 4, "notify group");
    _Static_assert(RDMA_NLDEV_CMD_MONITOR == 28, "monitor command");
    _Static_assert(RDMA_NLDEV_CMD_SYS_GET == 6, "system get");
    _Static_assert(RDMA_NLDEV_ATTR_EVENT_TYPE == 102, "event attribute");
    _Static_assert(RDMA_NLDEV_SYS_ATTR_MONITOR_MODE == 103, "monitor mode");
    HEADER
    mkdir -p "$out/bin"
    cc -Wall -Wextra -Werror $(pkg-config --cflags libibumad) \
      -Ipkg/linkmonitor/internal/rdmacaps nix/tests/linkmonitor-umad-test.c \
      -o "$out/bin/umad-ownership-test"
    "$out/bin/umad-ownership-test"
    cc -Wall -Wextra -Werror $(pkg-config --cflags libibmad) \
      -I${pkgs.linuxHeaders}/include nix/tests/linkmonitor-portinfo-test.c \
      $(pkg-config --libs libibmad) -o "$out/bin/portinfo-decoder-test"
    "$out/bin/portinfo-decoder-test"
    go test -tags rdma -c -o "$out/bin/rdmacaps.test" ./pkg/linkmonitor/internal/rdmacaps
    ldd "$out/bin/rdmacaps.test" > "$out/umad-ldd.txt"
    if grep -q 'not found' "$out/umad-ldd.txt"; then cat "$out/umad-ldd.txt"; exit 1; fi
    go test -tags rdma -c -o "$out/bin/rdmaevents.test" ./pkg/linkmonitor/internal/rdmaevents
    ln -s ${runtime} "$out/runtime"
    ldd "$out/bin/rdmaevents.test" > "$out/ldd.txt"
    if grep -q 'not found' "$out/ldd.txt"; then cat "$out/ldd.txt"; exit 1; fi
    test -e ${runtime}/lib/libibverbs.so.1
    test -e ${runtime}/lib/libibumad.so.3
    test -d ${runtime}/etc/libibverbs.d
    find -L ${runtime}/lib -name '*-rdmav*.so' > "$out/providers.txt"
    test -s "$out/providers.txt"
    export LINKMONITOR_RDMA_RUNTIME=${runtime}
    "$out/bin/rdmaevents.test" -test.run '^TestVerbsRuntime$' -test.v
  '';
}
