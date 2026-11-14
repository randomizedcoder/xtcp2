# vagrant/lib/ssh.sh — host-side VM access without provider magic.
#
# The box image ships no rsync (no synced folder — see the Vagrantfile), and
# `vagrant upload` support varies by provider version; ssh/scp over `vagrant
# ssh-config`'s generated config is the one path that works everywhere. The
# config is regenerated per call — VMs can move IPs across reboots.
# shellcheck shell=bash

vm_ssh_config() {
  local vm="$1"
  local cfg="$XTCP2_REPO_ROOT/vagrant/.gen/ssh-config-$vm"
  (cd "$VAGRANT_CWD" && vagrant ssh-config "$vm") >"$cfg"
  echo "$cfg"
}

# vm_exec <vm> <cmd…>: run cmd on the VM as the vagrant user.
vm_exec() {
  local vm="$1"
  shift
  ssh -F "$(vm_ssh_config "$vm")" "$vm" "$@"
}

# vm_exec_root <vm> <cmd…>: same, via sudo (the cloud-image box's vagrant
# user has passwordless sudo).
vm_exec_root() {
  local vm="$1"
  shift
  ssh -F "$(vm_ssh_config "$vm")" "$vm" "sudo bash -c $(printf '%q ' "$@")"
}

# vm_push <vm> <local-path> <remote-path>: scp a file into the VM.
vm_push() {
  local vm="$1" local_path="$2" remote_path="$3"
  scp -q -F "$(vm_ssh_config "$vm")" "$local_path" "$vm:""$remote_path"
}

# vm_up [--no-provision] <vm…>: idempotent boot. First boot lands on the
# unprovisioned image on purpose — the scenario scps the tarballs to
# /tmp/xtcp2-redteam BEFORE `vagrant provision` consumes them.
vm_up() {
  local no_provision=""
  if [ "${1:-}" = "--no-provision" ]; then
    no_provision="--no-provision"
    shift
  fi
  # shellcheck disable=SC2086
  # (the empty-flag case must expand to nothing)
  (cd "$VAGRANT_CWD" && vagrant up $no_provision "$@")
}

vm_provision() {
  (cd "$VAGRANT_CWD" && vagrant provision "$@")
}
