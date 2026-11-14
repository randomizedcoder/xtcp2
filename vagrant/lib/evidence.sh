# vagrant/lib/evidence.sh — run-id evidence directories under
# vagrant/redteam/evidence/ (gitignored). One dir per scenario run; findings.md
# quotes the relevant bytes, never the whole dump.
# shellcheck shell=bash

# evidence_init: sets RT_EVIDENCE and prints it.
evidence_init() {
  local sha
  sha="$(git -C "$XTCP2_REPO_ROOT" rev-parse --short HEAD 2>/dev/null || echo dirty)"
  RT_EVIDENCE="$XTCP2_REPO_ROOT/vagrant/redteam/evidence/$(date -u +%Y%m%dT%H%M%S)-$sha-$$"
  mkdir -p "$RT_EVIDENCE"
  echo "$RT_EVIDENCE"
}

# capture <name> <cmd…>: run cmd, capture stdout+stderr to
# $RT_EVIDENCE/<name>.log, echo stdout. Commands must not need a tty.
capture() {
  local name="$1"
  shift
  "$@" >"$RT_EVIDENCE/$name.log" 2>&1
  cat "$RT_EVIDENCE/$name.log"
}
