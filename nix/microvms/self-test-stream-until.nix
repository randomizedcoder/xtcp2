# nix/microvms/self-test-stream-until.nix
#
# The `stream_until` bash function the self-test uses to run a streaming
# xtcp2client, as a string so nix/checks/self-test-stream-until.nix can drive
# the exact same text without a VM.
#
# A stream never ends by itself, so `timeout <N>s xtcp2client …` always spent
# its whole window, and the checks accepted rc 124 and then looked at the log.
# Scaling those windows by WAIT_SCALE would have made every passing run wait
# out 4x the old budget. stream_until instead stops the client as soon as the
# log shows what the check needs, so the scaled value is only a ceiling: a
# pass costs the time to the first record and only a real failure waits it out.
#
#   stream_until <seconds> <log> <regex> <cmd...>
#
# Runs cmd with stdout and stderr to <log>, in its own session. Stops the whole
# session (TERM, then KILL after 2-3 s) once a line of <log> matches the
# extended <regex>, or once <seconds> elapse. Returns cmd's own rc if it exited
# by itself first, else 124, which is what `timeout` returned, so the callers'
# rc checks are unchanged. A cmd that matches and exits at the same moment may
# report either; every caller accepts both 0 and 124.
#
# Both deadlines are wall clock (bash's SECONDS), not a count of 0.2 s polls.
# A count is what this first did, and on a host at load ~55 each poll's forks
# took about 1 s, so a 48 s ceiling ran for 247 s. SECONDS ticks in whole
# seconds, hence -le: a window of N lasts between N and N+1 seconds.
#
# Needs bash (local), coreutils (sleep), grep and util-linux (setsid).
''
  stream_until() {
    local secs=$1 log=$2 re=$3 pid deadline grace
    shift 3
    # A background job of a non-interactive shell is not a process group
    # leader, so setsid execs in place and $! is cmd's own pid and pgid.
    setsid "$@" >"$log" 2>&1 &
    pid=$!
    deadline=$((SECONDS + secs))
    while [ "$SECONDS" -le "$deadline" ]; do
      if ! kill -0 "$pid" 2>/dev/null; then
        wait "$pid"
        return $?
      fi
      if grep -qE -- "$re" "$log" 2>/dev/null; then
        break
      fi
      sleep 0.2
    done
    kill -TERM -- "-$pid" 2>/dev/null
    grace=$((SECONDS + 2))
    while kill -0 "$pid" 2>/dev/null && [ "$SECONDS" -le "$grace" ]; do
      sleep 0.2
    done
    kill -KILL -- "-$pid" 2>/dev/null
    wait "$pid" 2>/dev/null
    return 124
  }
''
