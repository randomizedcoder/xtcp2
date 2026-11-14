# vagrant/lib/assert.sh — TAP output + the scenario verdict contract
# (docs/redteam-implementation.md, "Conventions every phase follows").
#
# Every scenario sources all four lib/ helpers (assert.sh included last is
# fine; they have no cross-deps beyond PATH), emits one TAP line per
# assertion, and exits 0 iff the observed outcome equals EXPECT and no
# assertion failed. The scenario declares EXPECT at the top; the runner env
# can override it (XTCP2_EXPECT) — the matrix-flip DoD runs exactly that:
#   XTCP2_MODE=bridge XTCP2_AUTH=raw XTCP2_EXPECT=BLOCKED nix run .#redteam-rt-1-1

# shellcheck shell=bash

RT_TEST_NUM=0
RT_FAILED=0

ok() {
  RT_TEST_NUM=$((RT_TEST_NUM + 1))
  echo "ok $RT_TEST_NUM - $*"
}

not_ok() {
  RT_TEST_NUM=$((RT_TEST_NUM + 1))
  echo "not ok $RT_TEST_NUM - $*"
  RT_FAILED=$((RT_FAILED + 1))
}

# wait_for <timeout_s> <description> <cmd…>: run cmd until it exits 0.
# ok on success; a single not_ok on timeout.
wait_for() {
  local timeout_s="$1" desc="$2"
  shift 2
  local deadline=$((SECONDS + timeout_s))
  while true; do
    if "$@" >/dev/null 2>&1; then
      ok "$desc"
      return 0
    fi
    if [ "$SECONDS" -ge "$deadline" ]; then
      not_ok "$desc (waited ${timeout_s}s)"
      return 1
    fi
    sleep 2
  done
}

# finish <scenario-id> <actual-outcome>: the summary + exit code.
# Blocks the scenario until actual == EXPECT (docs/redteam-implementation.md
# contract) and no assertion failed.
finish() {
  local id="$1" actual="$2"
  echo "1..$RT_TEST_NUM"
  echo "RESULT $id $actual severity= evidence=$RT_EVIDENCE"
  if [ "$RT_FAILED" -eq 0 ] && [ "$actual" = "$XTCP2_EXPECT" ]; then
    echo "RESULT $id $actual (expected $XTCP2_EXPECT): PASS"
    exit 0
  fi
  echo "RESULT $id $actual (expected $XTCP2_EXPECT, $RT_FAILED failed assertion(s)): FAIL"
  exit 1
}
