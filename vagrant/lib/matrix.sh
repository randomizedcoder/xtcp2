# vagrant/lib/matrix.sh — the deployment matrix: defaults, validation, and
# the shared repo-root/VAGRANT_* plumbing, so scenarios work identically via
# the flake apps (which export these) and via plain bash from vagrant/.
# shellcheck shell=bash

# Repo root = two levels up from this file (vagrant/lib -> repo).
XTCP2_REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
export XTCP2_REPO_ROOT

# The nix apps set VAGRANT_CWD/VAGRANT_HOME; bare bash gets the same values so
# `vagrant` behaves identically either way. No state in ~/.vagrant.d.
export VAGRANT_CWD="$XTCP2_REPO_ROOT/vagrant"
export VAGRANT_HOME="$VAGRANT_CWD/.gen/.vagrant.d"
mkdir -p "$VAGRANT_HOME"

xtcp2_die() {
  echo "rt: $*" >&2
  exit 2
}

# xtcp2_matrix_defaults: validate/normalize the XTCP2_* env the scenario apps
# export. Values mirror the Vagrantfile (which re-validates) and
# provision/victim.sh's own defense-in-depth check.
xtcp2_matrix_defaults() {
  : "${XTCP2_MODE:=host}"
  : "${XTCP2_AUTH:=off}"
  : "${XTCP2_CAPS:=priv}"
  : "${XTCP2_ATTACKER:=plain}"
  : "${XTCP2_EXPECT:=CONFIRMED}"

  case "$XTCP2_MODE" in
    host | bridge) ;;
    *) xtcp2_die "XTCP2_MODE='$XTCP2_MODE' (want host|bridge)" ;;
  esac
  case "$XTCP2_AUTH" in
    off | raw | hmac) ;;
    *) xtcp2_die "XTCP2_AUTH='$XTCP2_AUTH' (want off|raw|hmac)" ;;
  esac
  case "$XTCP2_CAPS" in
    priv | min) ;;
    *) xtcp2_die "XTCP2_CAPS='$XTCP2_CAPS' (want priv|min)" ;;
  esac
  case "$XTCP2_ATTACKER" in
    plain | read-cred | privileged | admin) ;;
    *) xtcp2_die "XTCP2_ATTACKER='$XTCP2_ATTACKER' (want plain|read-cred|privileged|admin)" ;;
  esac
  case "$XTCP2_EXPECT" in
    BLOCKED | MITIGATED | CONFIRMED | NA) ;;
    *) xtcp2_die "XTCP2_EXPECT='$XTCP2_EXPECT' (want BLOCKED|MITIGATED|CONFIRMED|NA)" ;;
  esac

  echo "rt: matrix MODE=$XTCP2_MODE AUTH=$XTCP2_AUTH CAPS=$XTCP2_CAPS ATTACKER=$XTCP2_ATTACKER EXPECT=$XTCP2_EXPECT"
}

# The co-tenant vantage: the docker0 gateway in the victim VM's host netns.
# Host mode: the daemon binds :9088 there. Bridge mode: -p publishes it there.
# Both shapes answer on the same address from inside a bridge-networked
# container — that symmetry is part of what rt-9-1 measures.
xtcp2_cotenant_prom_url() {
  echo 'http://172.17.0.1:9088/metrics'
}

xtcp2_preflight() {
  command -v vagrant >/dev/null 2>&1 \
    || xtcp2_die "vagrant not on PATH (nix run .#redteam-* provides it)"
  command -v ssh >/dev/null 2>&1 || xtcp2_die "ssh not on PATH"
  virsh -c qemu:///system version >/dev/null 2>&1 \
    || xtcp2_die "qemu:///system unreachable (libvirtd down, or not in the libvirt group)"
  [ -n "${XTCP2_OCI_TAR:-}" ] && [ -n "${XTCP2_TCP_STRESS_TAR:-}" ] \
    || xtcp2_die "XTCP2_OCI_TAR/XTCP2_TCP_STRESS_TAR unset — run via nix run .#redteam-* (the app exports them)"
}
