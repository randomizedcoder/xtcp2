#!/usr/bin/env bash
PROM=http://127.0.0.1:19090
fmt() {
  curl -sS --max-time 5 -G "$PROM/api/v1/query" --data-urlencode "query=$1" 2>/dev/null | \
    python3 -c "
import json, sys
d = json.load(sys.stdin)
parts = []
for r in d.get('data',{}).get('result',[]):
  inst = r['metric'].get('instance','?')
  val = r['value'][1]
  parts.append(inst + '=' + val)
print(' '.join(parts))
"
}
g=$(fmt 'go_goroutines{job="xtcp2"}')
h=$(fmt 'floor(go_memstats_heap_inuse_bytes{job="xtcp2"}/1048576)')
t=$(fmt 'go_threads{job="xtcp2"}')
echo "go_routines=[$g] heap_MiB=[$h] go_threads=[$t]"
