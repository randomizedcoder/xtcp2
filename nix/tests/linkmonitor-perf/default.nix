{
  pkgs,
  artifact,
  src,
}:
let
  versions = import ../../versions.nix { inherit pkgs; };
  runner = import ../linkmonitor-benchmark-runner.nix { inherit pkgs artifact; };
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
    bench-linkmonitor-smoke = {
      type = "app";
      program = "${mkRunner "bench-linkmonitor-smoke" "--smoke"}/bin/bench-linkmonitor-smoke";
    };
    bench-linkmonitor-soak = {
      type = "app";
      program = "${mkRunner "bench-linkmonitor-soak" "--soak"}/bin/bench-linkmonitor-soak";
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
