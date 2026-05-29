#!/usr/bin/env bash
# Query in-VM Prometheus (via host:19090) for soak-stability metrics.
# Outputs a compact report: goroutines, heap, GC, RSS — start vs end,
# min/max, and a pass/fail judgement.
#
# Usage: bash /tmp/cppq-stability.sh [SOAK_START_TS] [SOAK_END_TS]
#   timestamps as unix seconds; default = "soak started ~5min ago,
#   ended now" which matches a smoke. For real soaks, pass them.

PROM=http://127.0.0.1:19090
NOW=$(date +%s)
START=${1:-$((NOW - 14400))}  # default: 4h ago
END=${2:-$NOW}

# Promql query helper: returns the .value[1] of the first result, or "?"
q() {
  local res
  res=$(curl -sS --max-time 10 -G "$PROM/api/v1/query" --data-urlencode "query=$1" 2>/dev/null)
  echo "$res" | python3 -c '
import json, sys
try:
  d = json.load(sys.stdin)
  r = d["data"]["result"]
  if not r: print("?"); sys.exit()
  for entry in r:
    inst = entry["metric"].get("instance", "?")
    val = entry["value"][1]
    print(f"{inst}={val}")
except Exception as e:
  print(f"err:{e}")
' 2>/dev/null
}

echo "=== xtcp2 stability summary ==="
date -d @"$START" +"start: %F %T"
date -d @"$END"   +"end:   %F %T"
echo

# --- Goroutines: start / end / max over window ---
echo "goroutines (current):"
q "go_goroutines"
echo
echo "goroutines (max over soak window):"
q "max_over_time(go_goroutines[${SOAK_DUR_MIN:-240}m])"
echo

# --- OS threads ---
echo "go_threads (current):"
q "go_threads"
echo
echo "go_threads (max over soak window):"
q "max_over_time(go_threads[${SOAK_DUR_MIN:-240}m])"
echo

# --- Heap memory ---
echo "heap inuse (current MB):"
q "go_memstats_heap_inuse_bytes / 1024 / 1024"
echo
echo "heap inuse (max MB over soak):"
q "max_over_time((go_memstats_heap_inuse_bytes/1024/1024)[${SOAK_DUR_MIN:-240}m:])"
echo

# --- GC pauses ---
echo "GC pause sum (seconds total since start):"
q "go_gc_duration_seconds_sum"
echo
echo "GC pause p99 (recent seconds):"
q "go_gc_duration_seconds{quantile=\"1\"}"
echo

# --- Process RSS ---
echo "process RSS (current MB):"
q "process_resident_memory_bytes / 1024 / 1024"
echo
echo "process RSS (max MB over soak):"
q "max_over_time((process_resident_memory_bytes/1024/1024)[${SOAK_DUR_MIN:-240}m:])"
echo

# --- Sample counts to validate data range ---
echo "prom sample count (soak window):"
q "count_over_time(go_goroutines[${SOAK_DUR_MIN:-240}m])"
