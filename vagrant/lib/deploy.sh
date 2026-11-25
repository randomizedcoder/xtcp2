# vagrant/lib/deploy.sh — the scenario deployment preamble: boot the VMs
# unprovisioned, deliver the image/tool tarballs (scp — the box ships no
# rsync, so there is no synced folder), then provision the matrix row.
# Every Phase 1+ scenario calls xtcp2_deploy instead of repeating rt-1-1's
# inline steps.
# shellcheck shell=bash

xtcp2_deploy() { # xtcp2_deploy <vm>… e.g. xtcp2_deploy victim attacker
  [ "$#" -ge 1 ] || xtcp2_die "xtcp2_deploy: need at least one VM name"
  local vms=("$@")
  vm_up --no-provision "${vms[@]}"
  local vm
  for vm in "${vms[@]}"; do
    # The delivery dir exists only after common.sh; create it now so the scp
    # that precedes the first `vagrant provision` has somewhere to land.
    vm_exec "$vm" 'mkdir -p xtcp2-redteam'
    case "$vm" in
      victim)
        vm_push victim "$XTCP2_OCI_TAR" xtcp2-redteam/xtcp2-oci.tar.gz
        vm_push victim "$XTCP2_TCP_STRESS_TAR" xtcp2-redteam/xtcp2-tcp-stress.tar.gz
        # The co-tenant vantage (rt-4-1/rt-4-2) docker-cp's the static tools
        # INTO the rt-attacker container on this host, so the victim needs the
        # same tar the attacker VM gets.
        vm_push victim "$XTCP2_ATTACKER_TOOLS_TAR" xtcp2-redteam/xtcp2-redteam-attacker-tools.tar
        ;;
      attacker)
        vm_push attacker "$XTCP2_ATTACKER_TOOLS_TAR" xtcp2-redteam/xtcp2-redteam-attacker-tools.tar
        ;;
      *)
        xtcp2_die "xtcp2_deploy: unknown VM '$vm'"
        ;;
    esac
  done
  vm_provision "${vms[@]}"
}
