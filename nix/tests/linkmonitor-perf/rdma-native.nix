# nix/tests/linkmonitor-perf/rdma-native.nix
#
# `nix run path:.#bench-linkmonitor-rdma-native -- --output DIR` - the go-native
# RDMA benchmark runner (cmd/go-link-monitor/GO-NATIVE-RDMA-IMPLEMENTATION-PLAN.md
# R0-T05, R1-T06, R2-T01, R2-T03, R4-T03). It runs, from the compiled
# performance artifact:
#
#   rdmanative.test  BenchmarkTranslateRtnl, BenchmarkDecodeAsyncEvent
#   rdmanative.test  BenchmarkEventPath, the netlink source from a written
#                    datagram to delivery (batch 1, 8, 32, 64)
#   nlconn.test      BenchmarkReceive, the netlink receive layer alone, with
#                    a cancelable and a background context (batch 1, 8,
#                    32, 64); the event path and the rtnetlink
#                    listener both receive through it
#   xtcpnl.test      BenchmarkParseChardev
#   rdmacaps-native.test  BenchmarkEncodeUMADRequest, BenchmarkDecodeUMADReply,
#                    BenchmarkEncodeRegisterAgent2, the umad codec, and
#                    BenchmarkUMADExchange, the native transport over a
#                    socketpair fake device
#   core.test        BenchmarkPerformanceDecode, the monitor's existing
#                    rtnetlink and ethtool decoders, which R1-T03's
#                    translator must not be slower than
#
# and writes, per benchmark, the raw `go test` output, CPU and heap profiles,
# `pprof -top` of each, and a benchstat summary, plus manifest.txt (kernel,
# CPU, Go version). Nothing here needs RDMA hardware or root: every benchmark
# drives simulated input.
#
#   bench-linkmonitor-rdma-native [--output DIR] [--count N] [--benchtime T]
#                                 [--smoke] [--native] [--baseline DIR]
#                                 [--evidence DIR]
#   bench-linkmonitor-rdma-native --from DIR [--baseline DIR] --evidence DIR
#
# --smoke is one iteration of each, for checking the runner itself. --native
# is accepted for the commands the status document records; since R4-T02 the
# native side is the only one. The cgo backends are gone, so their numbers
# come from committed evidence instead: --baseline DIR names an earlier
# evidence directory holding cgo-events.txt and cgo-umad.txt (R0-T05's, e.g.
# cmd/go-link-monitor/rdma-native-bench/r0-t05/vm), and the event path and
# the umad exchange are benchstat-compared against it in
# event-path-benchstat.txt and umad-benchstat.txt.
#
# --evidence DIR writes the text evidence the status document cites (manifest,
# raw output, pprof tops, benchstat from this flake's goperf) to DIR, replacing
# it; the binary profiles stay in --output. --from promotes an earlier run's
# output instead of benchmarking, e.g. the performance VM's rdma-native/:
#
#   nix run path:.#test-linkmonitor-performance-vm -- /tmp/vm
#   nix run path:.#bench-linkmonitor-rdma-native -- \
#     --from /tmp/vm/linkmonitor-performance/rdma-native \
#     --baseline cmd/go-link-monitor/rdma-native-bench/r0-t05/vm \
#     --evidence cmd/go-link-monitor/rdma-native-bench/r4-t03/vm
{
  pkgs,
  artifact,
}:
let
  versions = import ../../versions.nix { inherit pkgs; };
in
pkgs.writeShellApplication {
  name = "bench-linkmonitor-rdma-native";
  runtimeInputs = [
    pkgs.coreutils
    pkgs.gnugrep
    pkgs.gnused
    pkgs.goperf
    pkgs.pprof
    versions.go
  ];
  text = ''
    usage() {
      echo "usage: bench-linkmonitor-rdma-native [--output DIR] [--count N] [--benchtime T] [--smoke] [--native] [--baseline DIR] [--evidence DIR]" >&2
      echo "       bench-linkmonitor-rdma-native --from DIR [--baseline DIR] --evidence DIR" >&2
      exit 2
    }

    names=(native-rtnl native-events native-receive native-chardev native-umad decode-baseline)
    output=""
    baseline=""
    from=""
    evidence=""
    count=10
    benchtime=1s
    while [ "$#" -gt 0 ]; do
      case "$1" in
        --output) [ "$#" -ge 2 ] || usage; output=$2; shift 2 ;;
        --from) [ "$#" -ge 2 ] || usage; from=$2; shift 2 ;;
        --evidence) [ "$#" -ge 2 ] || usage; evidence=$2; shift 2 ;;
        --baseline) [ "$#" -ge 2 ] || usage; baseline=$2; shift 2 ;;
        --count) [ "$#" -ge 2 ] || usage; count=$2; shift 2 ;;
        --benchtime) [ "$#" -ge 2 ] || usage; benchtime=$2; shift 2 ;;
        --smoke) count=1; benchtime=1x; shift ;;
        --native) shift ;;
        -h | --help) usage ;;
        *) echo "bench-linkmonitor-rdma-native: unknown argument $1" >&2; usage ;;
      esac
    done
    case "$count" in
      "" | *[!0-9]* | 0) echo "bench-linkmonitor-rdma-native: --count must be a positive integer" >&2; exit 2 ;;
    esac
    if [ -n "$from" ] && { [ -z "$evidence" ] || [ -n "$output" ]; }; then
      echo "bench-linkmonitor-rdma-native: --from needs --evidence and no --output" >&2
      exit 2
    fi
    if [ -n "$baseline" ]; then
      for f in cgo-events.txt cgo-umad.txt; do
        [ -s "$baseline/$f" ] || { echo "bench-linkmonitor-rdma-native: --baseline $baseline has no $f" >&2; exit 2; }
      done
    fi

    # The event path against the baseline's cgo batches it replaced: the cgo
    # rows are renamed so benchstat pairs them by batch size.
    # The umad exchange likewise: the cgo lane against the native transport
    # over the fake, uncontended and contended. The cgo side simulates buffer
    # handling without I/O and the native side makes real socketpair
    # syscalls, so B/op and allocs/op compare; sec/op shows the syscall cost.
    compare() {
      local dir=$1
      [ -n "$baseline" ] || return 0
      # The two sides are in different packages (rdmaevents, rdmanative), and
      # benchstat only pairs rows under the same pkg: line, so both drop it.
      sed '/^pkg:/d; s/^BenchmarkPerformanceCGO/BenchmarkEventPath/' "$baseline/cgo-events.txt" > "$dir/cgo-events-renamed.txt"
      sed '/^pkg:/d' "$dir/native-events.txt" > "$dir/native-events-nopkg.txt"
      # Neither cgo benchmark is the same measurement as its native one (the
      # findings in GO-NATIVE-RDMA-STATUS.md): the cgo side simulates, with
      # no system call, so only B/op and allocs/op compare. Each file says so.
      note='Not like for like: the cgo side copies or simulates in memory with no system call, and the native side makes real ones. Compare B/op and allocs/op only; sec/op is not a regression measure.'
      { echo "$note"; echo; } > "$dir/event-path-benchstat.txt"
      (cd "$dir" && benchstat cgo=cgo-events-renamed.txt native=native-events-nopkg.txt >> event-path-benchstat.txt)
      rm -f -- "$dir/cgo-events-renamed.txt" "$dir/native-events-nopkg.txt"
      sed 's/^BenchmarkPerformanceUMAD/BenchmarkUMADExchange/' "$baseline/cgo-umad.txt" > "$dir/cgo-umad-renamed.txt"
      grep -E '^(goos|goarch|pkg|cpu):|^BenchmarkUMADExchange/' "$dir/native-umad.txt" > "$dir/native-umad-exchange.txt"
      { echo "$note"; echo; } > "$dir/umad-benchstat.txt"
      (cd "$dir" && benchstat cgo=cgo-umad-renamed.txt native=native-umad-exchange.txt >> umad-benchstat.txt)
      rm -f -- "$dir/cgo-umad-renamed.txt" "$dir/native-umad-exchange.txt"
    }

    # Text evidence from a run directory: everything but the binary profiles,
    # with benchstat recomputed here so its version is this flake's.
    promote() {
      local src=$1 dst=$2 name f
      for name in "''${names[@]}"; do
        for f in "$name.txt" "$name-cpu-top.txt" "$name-heap-top.txt"; do
          [ -s "$src/$f" ] || { echo "bench-linkmonitor-rdma-native: $src/$f missing" >&2; exit 1; }
        done
      done
      [ -s "$src/manifest.txt" ] || { echo "bench-linkmonitor-rdma-native: $src/manifest.txt missing" >&2; exit 1; }
      rm -rf -- "$dst"
      mkdir -p "$dst"
      grep -v '^benchstat=' "$src/manifest.txt" > "$dst/manifest.txt"
      echo "benchstat=goperf-${pkgs.goperf.version}" >> "$dst/manifest.txt"
      if [ -n "$baseline" ]; then
        echo "baseline=$baseline" >> "$dst/manifest.txt"
      fi
      for name in "''${names[@]}"; do
        cp "$src/$name.txt" "$src/$name-cpu-top.txt" "$src/$name-heap-top.txt" "$dst/"
        (cd "$dst" && benchstat "$name.txt" > "$name-benchstat.txt")
      done
      compare "$dst"
      echo "bench-linkmonitor-rdma-native: evidence in $dst"
    }

    if [ -n "$from" ]; then
      promote "$from" "$evidence"
      exit 0
    fi

    if [ -z "$output" ]; then
      output=$(mktemp -d -t bench-linkmonitor-rdma-native-XXXXXX)
    fi
    mkdir -p "$output"
    export GOTOOLCHAIN=local

    {
      echo "kernel=$(uname -srm)"
      echo "cpu=$(grep -m1 '^model name' /proc/cpuinfo | cut -d: -f2- | sed 's/^ *//')"
      echo "nproc=$(nproc)"
      go_version=$(go version "${artifact}/bin/rdmanative.test")
      echo "go=''${go_version##* }"
      echo "count=$count"
      echo "benchtime=$benchtime"
      echo "benchstat=goperf-${pkgs.goperf.version}"
      echo "artifact=${artifact}"
    } > "$output/manifest.txt"

    # name, test binary, benchmark pattern (anchored here)
    run() {
      local name=$1 bin=$2 bench=$3 profile
      echo "bench-linkmonitor-rdma-native: $name ($bench, count $count, benchtime $benchtime)"
      "$bin" -test.run '^$' -test.bench "^($bench)\$" -test.benchmem \
        -test.count "$count" -test.benchtime "$benchtime" \
        -test.cpuprofile "$output/$name-cpu.pprof" \
        -test.memprofile "$output/$name-heap.pprof" > "$output/$name.txt" 2>&1 || {
        cat "$output/$name.txt" >&2
        return 1
      }
      grep -q '^PASS$' "$output/$name.txt"
      for profile in cpu heap; do
        pprof -top -nodecount=40 -sample_index="$([ "$profile" = heap ] && echo alloc_space || echo cpu)" \
          "$bin" "$output/$name-$profile.pprof" > "$output/$name-$profile-top.txt" 2>&1 || {
          cat "$output/$name-$profile-top.txt" >&2
          return 1
        }
      done
      (cd "$output" && benchstat "$name.txt" > "$name-benchstat.txt")
    }
    run native-rtnl "${artifact}/bin/rdmanative.test" 'BenchmarkTranslateRtnl|BenchmarkDecodeAsyncEvent'
    run native-events "${artifact}/bin/rdmanative.test" BenchmarkEventPath
    run native-receive "${artifact}/bin/nlconn.test" BenchmarkReceive
    run native-chardev "${artifact}/bin/xtcpnl.test" BenchmarkParseChardev
    run native-umad "${artifact}/bin/rdmacaps-native.test" 'BenchmarkEncodeUMADRequest|BenchmarkDecodeUMADReply|BenchmarkEncodeRegisterAgent2|BenchmarkUMADExchange'
    run decode-baseline "${artifact}/bin/core.test" BenchmarkPerformanceDecode

    compare "$output"
    for name in "''${names[@]}"; do
      cat "$output/$name-benchstat.txt"
    done
    for f in event-path-benchstat.txt umad-benchstat.txt; do
      if [ -s "$output/$f" ]; then cat "$output/$f"; fi
    done
    if [ -n "$evidence" ]; then
      promote "$output" "$evidence"
    fi
    echo "PASS: bench-linkmonitor-rdma-native evidence in $output"
  '';
}
