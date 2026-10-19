{
  pkgs,
  artifact,
  src,
}:
let
  versions = import ../../versions.nix { inherit pkgs; };
  runner = import ../linkmonitor-benchmark-runner.nix { inherit pkgs artifact; };
  rdmaNative = import ./rdma-native.nix { inherit pkgs artifact; };
  # Start-up of whole go-link-monitor builds, e.g. this tree against an
  # earlier revision's package (R4-T03):
  #   nix run path:.#bench-linkmonitor-startup -- --output DIR \
  #     native=$(nix build path:.#go-link-monitor --print-out-paths)/bin/go-link-monitor \
  #     cgo=$(nix build 'git+file://$PWD?rev=REV#go-link-monitor' --print-out-paths)/bin/go-link-monitor
  startup = pkgs.writeShellApplication {
    name = "bench-linkmonitor-startup";
    runtimeInputs = [ pkgs.goperf ];
    text = ''
      exec ${pkgs.python3}/bin/python3 -I ${./startup.py} "$@"
    '';
  };
  # The runner's own gate: one smoke run that must produce every evidence
  # file, and the argument table.
  rdmaNativeCheck =
    pkgs.runCommand "xtcp2-test-linkmonitor-rdma-native-bench" { nativeBuildInputs = [ rdmaNative ]; }
      ''
            set -euo pipefail
            export HOME=$TMPDIR
            mkdir -p "$out"
            baseline=${src}/cmd/go-link-monitor/rdma-native-bench/r0-t05/vm
            bench-linkmonitor-rdma-native --smoke --output "$out/smoke" > "$out/smoke.log" 2>&1 || { cat "$out/smoke.log"; exit 1; }
            # Since R4-T02 the native side is the only one: no cgo run.
            for name in native-rtnl native-events native-receive native-chardev native-umad decode-baseline; do
              for f in "$name.txt" "$name-cpu-top.txt" "$name-heap-top.txt" "$name-benchstat.txt"; do
                [ -s "$out/smoke/$f" ] || { echo "missing $f" >&2; exit 1; }
              done
            done
            [ -s "$out/smoke/manifest.txt" ] || { echo "missing manifest.txt" >&2; exit 1; }
            if [ -n "$(find "$out/smoke" -name 'cgo-*')" ]; then echo "a cgo run is left" >&2; exit 1; fi
            # Without --baseline there is nothing to compare against.
            if [ -e "$out/smoke/event-path-benchstat.txt" ]; then echo "compared without a baseline" >&2; exit 1; fi
            grep -q '^go=go1' "$out/smoke/manifest.txt"
            grep -q '^BenchmarkTranslateRtnl' "$out/smoke/native-rtnl.txt"
            grep -q '^BenchmarkDecodeAsyncEvent' "$out/smoke/native-rtnl.txt"
            grep -q '^BenchmarkEventPath/batch=64' "$out/smoke/native-events.txt"
            grep -q '^BenchmarkReceive/ctx=cancelable/batch=64' "$out/smoke/native-receive.txt"
            grep -q '^BenchmarkParseChardev' "$out/smoke/native-chardev.txt"
            grep -q '^BenchmarkDecodeUMADReply' "$out/smoke/native-umad.txt"
            grep -q '^BenchmarkEncodeRegisterAgent2' "$out/smoke/native-umad.txt"
            grep -q '^BenchmarkUMADExchange/contended' "$out/smoke/native-umad.txt"
            grep -q '^BenchmarkPerformanceDecode/route' "$out/smoke/decode-baseline.txt"
            # --from promotes the smoke run's text evidence without the profiles,
            # compared against the committed R0-T05 cgo baseline.
            bench-linkmonitor-rdma-native --native --from "$out/smoke" --baseline "$baseline" --evidence "$out/evidence" > "$out/promote.log" 2>&1 || { cat "$out/promote.log"; exit 1; }
            [ -s "$out/evidence/native-rtnl-benchstat.txt" ] || { echo "promote wrote no benchstat" >&2; exit 1; }
            if [ -n "$(find "$out/evidence" -name '*.pprof')" ]; then echo "promote copied a binary profile" >&2; exit 1; fi
            if grep -rq "$out" "$out/evidence"; then echo "evidence names the run directory" >&2; exit 1; fi
            # The cgo baseline and the native event path are paired by batch
            # size, and the umad lane and native exchange by contention.
            # Paired, not two separate tables: each has a "vs base" column.
            # (Rows pair only at equal GOMAXPROCS suffixes, as in the
            # performance VM; the sandbox's may differ from the baseline's.)
            grep -q 'EventPath/batch=64' "$out/evidence/event-path-benchstat.txt"
            grep -q 'vs base' "$out/evidence/event-path-benchstat.txt"
            grep -q 'UMADExchange/contended' "$out/evidence/umad-benchstat.txt"
            grep -q 'vs base' "$out/evidence/umad-benchstat.txt"
            grep -q '^Not like for like' "$out/evidence/event-path-benchstat.txt"
            grep -q '^Not like for like' "$out/evidence/umad-benchstat.txt"
            grep -q "^baseline=$baseline$" "$out/evidence/manifest.txt"
            # A run with --baseline compares as it goes.
            bench-linkmonitor-rdma-native --smoke --baseline "$baseline" --output "$out/compared" > "$out/compared.log" 2>&1 || { cat "$out/compared.log"; exit 1; }
            [ -s "$out/compared/event-path-benchstat.txt" ] || { echo "no event path comparison" >&2; exit 1; }
            [ -s "$out/compared/umad-benchstat.txt" ] || { echo "no umad comparison" >&2; exit 1; }
            # name|category|description|expected exit|arguments
            while IFS='|' read -r name category description expected args; do
              echo "$category: $name: $description; expected: exit $expected"
              args=''${args//SMOKE/$out/smoke}
              read -ra argv <<< "''${args//BASELINE/$baseline}"
              rc=0
              bench-linkmonitor-rdma-native "''${argv[@]}" > "$out/args-$name.log" 2>&1 || rc=$?
              [ "$rc" -eq "$expected" ] || { echo "$name: exit $rc, want $expected" >&2; cat "$out/args-$name.log"; exit 1; }
            done <<'ROWS'
        from_cgo_only|negative|--from an R0-T05 directory, which has only the cgo side|1|--from BASELINE --evidence /tmp/never-written
        baseline_missing|negative|--baseline names a directory with no cgo output|2|--smoke --baseline /nonexistent
        baseline_no_value|corner|--baseline without its value|2|--baseline
        baseline_is_run|negative|--baseline names a native run, which has no cgo output|2|--from SMOKE --baseline SMOKE --evidence /tmp/never-written
        unknown|negative|an unknown argument|2|--bogus
        count_zero|boundary|--count 0|2|--count 0
        count_text|negative|--count is not a number|2|--count ten
        output_missing|corner|--output without its value|2|--output
        help|corner|--help prints usage|2|--help
        from_alone|negative|--from without --evidence|2|--from /nonexistent
        from_output|negative|--from with --output|2|--from /a --evidence /b --output /c
        from_missing|negative|--from a directory with no run in it|1|--from /nonexistent --evidence /tmp/never-written
        ROWS
            echo PASS > "$out/result.txt"
      '';
  mkRunner =
    name: arguments:
    pkgs.writeShellApplication {
      inherit name;
      runtimeInputs = [ runner ];
      text = ''
        exec ${runner}/bin/bench-linkmonitor ${arguments} "$@"
      '';
    };
  siege = pkgs.writeShellApplication {
    name = "siege-linkmonitor";
    runtimeInputs = [
      pkgs.siege
      pkgs.python3
    ];
    text = ''
            set -euo pipefail
            url=""; output=""; duration="60s"; concurrency="1"; all_weight="0"
            while [ "$#" -gt 0 ]; do
              case "$1" in
                --url) url="$2"; shift 2;;
                --output) output="$2"; shift 2;;
                --duration) duration="$2"; shift 2;;
                --concurrency) concurrency="$2"; shift 2;;
                --all-weight) all_weight="$2"; shift 2;;
                *) echo "unknown argument: $1" >&2; exit 2;;
              esac
            done
            if [ -z "$url" ] || [ -z "$output" ]; then
              echo 'usage: siege-linkmonitor --url URL --output DIR [--duration 60s] [--concurrency N] [--all-weight 0-90]' >&2
              exit 2
            fi
            mkdir -p "$output"
            python3 - "$url" "$output/urls.txt" "$all_weight" <<'PY'
      import sys
      from pathlib import Path
      from urllib.parse import urljoin
      base, path, full = sys.argv[1], Path(sys.argv[2]), int(sys.argv[3])
      if not base.startswith(("http://", "https://")):
          raise SystemExit("base URL must use http or https")
      if not 0 <= full <= 90:
          raise SystemExit("--all-weight must be within 0..90")
      # /metrics-all scrapes replace core scrapes so the mix stays at 100 lines.
      rows = [("/metrics", 90 - full), ("/healthz", 5), ("/readyz", 5), ("/metrics-all", full)]
      path.write_text("".join(urljoin(base.rstrip("/") + "/", item) + "\n" for item, weight in rows for _ in range(weight)))
      PY
            siege --quiet --internet --time="$duration" --concurrent="$concurrency" --file="$output/urls.txt" >"$output/siege.txt" 2>&1
            printf '%s\n' '{"status":"complete"}' > "$output/manifest.json"
    '';
  };
  profile = pkgs.writeShellApplication {
    name = "profile-linkmonitor";
    runtimeInputs = [ pkgs.python3 ];
    text = ''
      exec python3 ${src}/nix/tests/linkmonitor_http.py --profile "$@"
    '';
  };
  pgo = pkgs.writeShellApplication {
    name = "pgo-linkmonitor";
    runtimeInputs = [ versions.go ];
    text = ''
      set -euo pipefail
      profile=""; output=""; package="./cmd/go-link-monitor"
      while [ "$#" -gt 0 ]; do
        case "$1" in
          --profile) profile="$2"; shift 2;;
          --output) output="$2"; shift 2;;
          --package) package="$2"; shift 2;;
          *) echo "unknown argument: $1" >&2; exit 2;;
        esac
      done
      if [ -z "$profile" ] || [ -z "$output" ] || [ ! -s "$profile" ]; then
        echo 'usage: pgo-linkmonitor --profile CPU.pprof --output BIN [--package ./cmd/go-link-monitor]' >&2
        exit 2
      fi
      export GOTOOLCHAIN=local
      go tool pprof -proto "$profile" > "$TMPDIR/default.pgo"
      go build -trimpath -pgo="$TMPDIR/default.pgo" -o "$output" "$package"
      go version > "$output.go-version.txt"
    '';
  };
  compare = pkgs.writeShellApplication {
    name = "compare-linkmonitor-pgo";
    runtimeInputs = [ pkgs.goperf ];
    text = ''
      if [ "$#" -ne 2 ]; then
        echo 'usage: compare-linkmonitor-pgo BASELINE.txt OPTIMIZED.txt' >&2
        exit 2
      fi
      benchstat "$1" "$2"
    '';
  };
in
{
  packages = {
    bench-linkmonitor = runner;
    bench-linkmonitor-rdma-native = rdmaNative;
    bench-linkmonitor-startup = startup;
    test-linkmonitor-rdma-native-bench = rdmaNativeCheck;
    bench-linkmonitor-smoke = mkRunner "bench-linkmonitor-smoke" "--smoke";
    bench-linkmonitor-soak = mkRunner "bench-linkmonitor-soak" "--soak";
    siege-linkmonitor = siege;
    profile-linkmonitor = profile;
    pgo-linkmonitor = pgo;
    compare-linkmonitor-pgo = compare;
  };
  apps = {
    bench-linkmonitor = {
      type = "app";
      program = "${runner}/bin/bench-linkmonitor";
    };
    bench-linkmonitor-startup = {
      type = "app";
      program = "${startup}/bin/bench-linkmonitor-startup";
    };
    bench-linkmonitor-smoke = {
      type = "app";
      program = "${mkRunner "bench-linkmonitor-smoke" "--smoke"}/bin/bench-linkmonitor-smoke";
    };
    bench-linkmonitor-soak = {
      type = "app";
      program = "${mkRunner "bench-linkmonitor-soak" "--soak"}/bin/bench-linkmonitor-soak";
    };
    bench-linkmonitor-rdma-native = {
      type = "app";
      program = "${rdmaNative}/bin/bench-linkmonitor-rdma-native";
    };
    siege-linkmonitor = {
      type = "app";
      program = "${siege}/bin/siege-linkmonitor";
    };
    profile-linkmonitor = {
      type = "app";
      program = "${profile}/bin/profile-linkmonitor";
    };
    pgo-linkmonitor = {
      type = "app";
      program = "${pgo}/bin/pgo-linkmonitor";
    };
    compare-linkmonitor-pgo = {
      type = "app";
      program = "${compare}/bin/compare-linkmonitor-pgo";
    };
  };
}
