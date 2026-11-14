# vagrant/lib/invariant.sh — the RT-11 "victim still reporting" invariant.
#
# The pass criterion of every RT-11 attack (docs/redteam-plan.md RT-11): the
# work population's records keep arriving at the CATCHER within the poll
# cadence — consumed host-side, not from a daemon gauge, so a blinded or
# attacker-controlled daemon cannot forge it. The catcher file is root-owned;
# the scenario reads it via sudo.
# shellcheck shell=bash

CATCHER_FILE=/var/lib/xtcp2-catcher/records.jsonl
CATCHER_WAIT_SECS=6 # 3× the 2s poll cadence provision/victim.sh configures

# catcher_lines: number of jsonl records the daemon has delivered.
catcher_lines() {
  vm_exec_root victim "wc -l < $CATCHER_FILE 2>/dev/null || echo 0"
}

# victim_still_reporting: records arrived over the last CATCHER_WAIT_SECS.
victim_still_reporting() {
  local before after
  before="$(catcher_lines)"
  sleep "$CATCHER_WAIT_SECS"
  after="$(catcher_lines)"
  [ "$after" -gt "$before" ]
}

# victim_still_reporting_tap: the invariant as a TAP assertion.
victim_still_reporting_tap() {
  if victim_still_reporting; then
    ok "victim still reporting (catcher grew over ${CATCHER_WAIT_SECS}s)"
  else
    not_ok "victim still reporting (catcher did not grow over ${CATCHER_WAIT_SECS}s)"
  fi
}
