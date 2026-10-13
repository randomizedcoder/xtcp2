# nix/microvms/self-test-wait-metric.nix
#
# The `wait_metric_ge` bash function the self-test's netns checks (8, 9, 10,
# 16) use to wait for a counter, as a string so
# nix/checks/self-test-wait-metric.nix can drive the exact same text without a
# VM.
#
# Checks 10 and 16 used to `sleep 6` and sample once. Check 8 had already
# replaced that with a retry loop, because the daemon's reconcile fires once
# per jittered poll cycle and a fixed wait races it (the NS_LIFECYCLE flake),
# but its loop counted iterations of `curl --max-time 2` + `sleep 1`, which on
# a loaded host is not the budget it claims. This polls against bash's SECONDS
# instead.
#
#   wait_metric_ge <seconds> <target> <metric_value args...>
#
# Calls `metric_value <args>` about once a second until it prints a number
# >= <target>, or until more than <seconds> of wall clock have passed. Prints
# the first value >= <target>, else the last number it saw, else <target - 1>,
# so a caller's `-ge target` test fails when nothing was seen. Output that is
# not a number (an empty scrape while the daemon is down) counts as "not yet".
# Always returns 0; callers compare the printed value. metric_value is
# whatever function the caller has in scope.
''
  wait_metric_ge() {
    local secs=$1 target=$2 v last deadline
    shift 2
    last=$((target - 1))
    deadline=$((SECONDS + secs))
    while :; do
      v=$(metric_value "$@")
      if [ "$v" -ge "$target" ] 2>/dev/null; then
        echo "$v"
        return 0
      fi
      if [ "$v" -eq "$v" ] 2>/dev/null; then
        last=$v
      fi
      if [ "$SECONDS" -gt "$deadline" ]; then
        break
      fi
      sleep 1
    done
    echo "$last"
  }
''
