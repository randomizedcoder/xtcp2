#!/bin/sh
# vagrant/lib/brutepace.sh — the co-tenant brute-force probe run inside the
# attacker container (sh: the ubuntu:24.04 base has no bash until installed).
#
# usage: brutepace.sh <prom-url> <valid-token> <mode> [args...]
#
# modes:
#   jitter              25 wrong tokens; prints fail_min/fail_max/fail_n (ms)
#   hammer <n>          n wrong tokens, then the valid one; prints last_ok
#   rate <secs> <work>   wrong tokens for <secs> with <work> parallel workers;
#                        prints guesses_per_s (one decimal)
#
# Every wrong token is a distinct 64-hex guess (the valid token's shape), so
# the daemon's constant-time compare cannot shortcut them apart.
set -eu

PROM="$1"
TOKEN="$2"
MODE="$3"

code_of() { # code_of <header-value>: HTTP code of one scrape
  curl -s -o /dev/null -w '%{http_code}' -m 10 -H "Authorization: $1" "$PROM"
}

ms_of() { # ms_of <header-value>: total time of one scrape, in ms
  curl -s -o /dev/null -m 10 -w '%{time_total}' -H "Authorization: $1" "$PROM"
}

case "$MODE" in
jitter)
  lo=999999
  hi=0
  n=0
  i=1
  while [ "$i" -le 25 ]; do
    t=$(ms_of "Bearer wrong-guess-$i-0123456789abcdef0123456789abcdef")
    # %time_total is in seconds with 6 decimals: ms = x1000
    ms=$(awk -v s="$t" 'BEGIN{printf "%d", s*1000}')
    [ "$ms" -lt "$lo" ] && lo=$ms
    [ "$ms" -gt "$hi" ] && hi=$ms
    n=$((n + 1))
    i=$((i + 1))
  done
  echo "fail_min=$lo"
  echo "fail_max=$hi"
  echo "fail_n=$n"
  ;;
hammer)
  N=${4:-150}
  i=1
  while [ "$i" -le "$N" ]; do
    code_of "Bearer wrong-guess-$i-0123456789abcdef0123456789abcdef" >/dev/null
    i=$((i + 1))
  done
  echo "last_ok=$(code_of "Bearer $TOKEN")"
  ;;
rate)
  SECS=${4:-60}
  WORK=${5:-1}
  counter=/tmp/brutepace.count
  : >"$counter"
  worker() {
    end=$(( $(date +%s) + SECS ))
    while [ "$(date +%s)" -lt "$end" ]; do
      code_of "Bearer wrong-guess-$$-$(date +%s%N)-0123456789abcdef" >/dev/null
      echo x >>"$counter"
    done
  }
  w=1
  while [ "$w" -le "$WORK" ]; do
    worker &
    w=$((w + 1))
  done
  wait
  n=$(wc -l <"$counter")
  awk -v n="$n" -v s="$SECS" 'BEGIN{printf "guesses_per_s=%.1f\n", n/s}'
  ;;
*)
  echo "brutepace: unknown mode $MODE" >&2
  exit 2
  ;;
esac
