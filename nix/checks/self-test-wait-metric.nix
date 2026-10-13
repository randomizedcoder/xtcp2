# nix/checks/self-test-wait-metric.nix
#
# Hermetic check for the self-test's wait_metric_ge function
# (nix/microvms/self-test-wait-metric.nix), run under bash against a stub
# metric_value that reads its value from a file. Each row asserts the printed
# value, the elapsed wall clock, an empty stderr and, where it matters, how
# many scrapes were made. It does not cover the daemon's /metrics; the
# lifecycle VM runs do.
{ pkgs }:

let
  waitMetric = import ../microvms/self-test-wait-metric.nix;
in
pkgs.runCommand "xtcp2-self-test-wait-metric"
  {
    nativeBuildInputs = with pkgs; [
      bash
      coreutils
    ];
  }
  ''
    bash ${pkgs.writeText "wait-metric-test.bash" ''
      set -u
      ${waitMetric}

      fails=0
      dir=$(mktemp -d)
      val=$dir/val
      calls=$dir/calls
      err=$dir/err
      stub_sleep=0

      # The stub: optionally slow, counts its calls, prints the file's value.
      metric_value() {
        if [ "$stub_sleep" != 0 ]; then sleep "$stub_sleep"; fi
        echo $(($(cat "$calls") + 1)) >"$calls"
        cat "$val"
      }

      # set_val <value> [<after_seconds> <later_value>]
      set_val() {
        printf '%s' "$1" >"$val"
        if [ "$#" -eq 3 ]; then
          (sleep "$2"; printf '%s' "$3" >"$val") &
        fi
      }

      # row <description> <want_out> <min_ms> <max_ms> <max_calls|-> <secs> <target>
      row() {
        local desc=$1 want=$2 min_ms=$3 max_ms=$4 max_calls=$5 secs=$6 target=$7 t0 t1 ms out n
        echo 0 >"$calls"
        t0=$(date +%s%N)
        out=$(wait_metric_ge "$secs" "$target" xtcp_counts 'function="x"' 'variable="y"' 2>"$err")
        t1=$(date +%s%N)
        ms=$(((t1 - t0) / 1000000))
        n=$(cat "$calls")
        wait
        if [ "$out" != "$want" ] || [ "$ms" -lt "$min_ms" ] || [ "$ms" -gt "$max_ms" ]; then
          echo "FAIL $desc: printed '$out' (want '$want'), elapsed=''${ms}ms (want $min_ms..$max_ms)"
          fails=$((fails + 1))
        elif [ -s "$err" ]; then
          echo "FAIL $desc: stderr not empty:"
          cat "$err"
          fails=$((fails + 1))
        elif [ "$max_calls" != - ] && [ "$n" -gt "$max_calls" ]; then
          echo "FAIL $desc: $n scrapes (want at most $max_calls)"
          fails=$((fails + 1))
        else
          echo "ok   $desc (printed $out, ''${ms}ms, $n scrapes)"
        fi
      }

      set_val 3 1 4
      row "positive: returns as soon as the value reaches the target" 4 900 3000 - 10 4
      set_val 7
      row "positive: already past the target at the first sample" 7 0 1000 1 10 4
      set_val 3
      row "negative: never reaches the target" 3 2000 4500 - 2 4
      set_val ""
      row "negative: the scrape prints nothing (daemon down) -> target - 1" 3 2000 4500 - 2 4
      set_val 5
      row "boundary: equal to the target counts" 5 0 1000 1 10 5
      set_val 4 1 5
      row "boundary: one below the target does not count" 5 900 3000 - 10 5
      set_val NaN 1 4
      row "corner: non-numeric output is ignored" 4 900 3000 - 10 4

      # A slow scrape (1.5 s per call, plus the 1 s pause) must not stretch
      # the budget: a wall-clock 6 s ends after at most 4 calls and about 9 s
      # (SECONDS ticks in whole seconds), where a count of 6 iterations would
      # take 15 s and 6 calls.
      stub_sleep=1.5
      set_val 3
      row "corner: a slow scrape does not stretch a short budget" 3 2000 6000 - 2 4
      set_val 3
      row "corner: the budget is wall clock, not a count" 3 6000 11500 4 6 4
      stub_sleep=0

      rm -rf -- "$dir"
      if [ "$fails" -ne 0 ]; then
        echo "$fails row(s) failed"
        exit 1
      fi
    ''}
    touch $out
  ''
