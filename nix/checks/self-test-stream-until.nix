# nix/checks/self-test-stream-until.nix
#
# Hermetic check for the self-test's stream_until function
# (nix/microvms/self-test-stream-until.nix), run under bash against small
# `sh -c` stand-ins for a streaming xtcp2client. Each row asserts the rc, the
# elapsed wall clock and, where it matters, the log and that nothing is left
# running. It does not cover xtcp2client itself; the lifecycle VM runs do.
{ pkgs }:

let
  streamUntil = import ../microvms/self-test-stream-until.nix;
in
pkgs.runCommand "xtcp2-self-test-stream-until"
  {
    nativeBuildInputs = with pkgs; [
      bash
      coreutils
      gnugrep
      procps
      util-linux
    ];
  }
  ''
    bash ${pkgs.writeText "stream-until-test.bash" ''
      set -u
      ${streamUntil}

      fails=0
      log=$(mktemp)

      # row <description> <want_rc> <min_ms> <max_ms> <log_grep|-> <secs> <regex> <cmd...>
      row() {
        local desc=$1 want_rc=$2 min_ms=$3 max_ms=$4 want_log=$5 secs=$6 re=$7 t0 t1 rc ms
        shift 7
        t0=$(date +%s%N)
        stream_until "$secs" "$log" "$re" "$@"
        rc=$?
        t1=$(date +%s%N)
        ms=$(((t1 - t0) / 1000000))
        if [ "$rc" -ne "$want_rc" ] || [ "$ms" -lt "$min_ms" ] || [ "$ms" -gt "$max_ms" ]; then
          echo "FAIL $desc: rc=$rc (want $want_rc), elapsed=''${ms}ms (want $min_ms..$max_ms)"
          fails=$((fails + 1))
        elif [ "$want_log" != - ] && ! grep -qF -- "$want_log" "$log"; then
          echo "FAIL $desc: log lacks '$want_log':"
          cat "$log"
          fails=$((fails + 1))
        elif pgrep -f 'stream-until-marker' >/dev/null; then
          echo "FAIL $desc: a process was left running"
          pkill -KILL -f 'stream-until-marker'
          fails=$((fails + 1))
        else
          echo "ok   $desc (rc=$rc, ''${ms}ms)"
        fi
      }

      # Every stand-in carries the marker so the leftover check can find it,
      # and so does its long-running child. `hold` is an inner bash whose $0 is
      # the marker. It is not the last command (bash would exec a last command
      # in place), so it is a separate child of the stand-in: one that only a
      # kill of the whole process group reaches.
      m='true stream-until-marker;'
      hold=' bash -c "sleep 30; true" stream-until-marker; true'

      row "positive: stops at the first match" 124 0 3000 '{a}' \
        10 '^\{' bash -c "$m"' echo "{a}";'"$hold"
      row "positive: a debug line does not count" 124 1000 4000 '{b}' \
        10 '^\{' bash -c "$m"' echo "2026/10/13 dbg"; sleep 1; echo "{b}";'"$hold"
      row "positive: a command that exits by itself keeps its rc" 0 0 3000 hi \
        10 NEVER bash -c "$m"' echo hi; exit 0'
      row "negative: a failing command's rc passes through" 3 0 3000 - \
        10 NEVER bash -c "$m"' exit 3'
      row "negative: no match within the budget" 124 2000 5000 - \
        2 '^\{' bash -c "$m""$hold"
      row "boundary: a match before the first poll" 124 0 2000 '{x}' \
        10 '^\{' bash -c "$m"' echo "{x}";'"$hold"
      row "corner: a child that ignores TERM is killed" 124 2000 5000 '{z}' \
        10 '^\{' bash -c "$m"' trap "" TERM; echo "{z}";'"$hold"
      row "corner: regex . matches any output" 124 0 3000 x \
        10 . bash -c "$m"' echo x;'"$hold"

      # A loaded host: every poll's sleep takes 1 s, not 0.2 s. A budget that
      # counted polls would run 10 s plus a 10 s grace; a wall-clock one stays
      # near its 2 s plus a 2 s grace.
      sleep() { command sleep 1; }
      row "corner: slow polls do not stretch the budget" 124 2000 6000 - \
        2 '^\{' bash -c "$m""$hold"
      unset -f sleep

      rm -f -- "$log"
      if [ "$fails" -ne 0 ]; then
        echo "$fails row(s) failed"
        exit 1
      fi
    ''}
    touch $out
  ''
