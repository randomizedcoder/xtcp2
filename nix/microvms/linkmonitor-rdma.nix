# Opt-in software-RDMA guest, reusing the existing VM and serial-log runner.
{
  pkgs,
  lib,
  microvm,
  nixpkgs,
  binaries,
  artifact,
}:
let
  constants = import ./constants.nix;
  runners = import ./lib.nix { inherit pkgs lib constants; };
  guestTest = pkgs.writeShellApplication {
    name = "linkmonitor-rdma-guest-test";
    runtimeInputs = with pkgs; [
      coreutils
      iproute2
      kmod
      rdma-core
      util-linux
      systemd
    ];
    text = ''
      trap 'echo XTCP2_SELF_TEST_OVERALL_FAIL' ERR
      modprobe rdma_rxe
      modprobe ib_uverbs
      ip link add lm0 type veth peer name lm1
      ip address add 192.0.2.1/24 dev lm0
      ip address add 192.0.2.2/24 dev lm1
      ip link set lm0 up
      ip link set lm1 up
      rdma link add lm_rxe type rxe netdev lm0
      udevadm settle
      export LINKMONITOR_SOFTWARE_RDMA=lm_rxe
      ${artifact}/bin/rdma-vm.test -test.run '^TestSoftwareRDMADiscovery$' -test.v
      ibv_devinfo -d lm_rxe
      echo XTCP2_SELF_TEST_RDMA_DISCOVERY_PASS
      # Permission manipulation is confined to disposable guest fixture nodes.
      for node in /dev/infiniband/uverbs*; do chmod 600 "$node"; done
      if runuser -u rdma-denied -- ibv_devinfo -d lm_rxe; then
        echo 'denied user unexpectedly opened the software device' >&2
        exit 1
      fi
      echo XTCP2_SELF_TEST_RDMA_PERMISSION_PASS
      rdma link delete lm_rxe
      ${artifact}/bin/rdma-vm.test -test.run '^TestSoftwareRDMARemoved$' -test.v
      rdma link add lm_rxe type rxe netdev lm0
      udevadm settle
      ${artifact}/bin/rdma-vm.test -test.run '^TestSoftwareRDMADiscovery$' -test.v
      ibv_devinfo -d lm_rxe
      ${artifact}/bin/linkmonitor-smoke
      echo XTCP2_SELF_TEST_RDMA_RECOVERY_PASS
      echo 'UNVERIFIED: real verbs event delivery and native InfiniBand UMAD/speed/duplex; synthetic adapter tests are separate.'
      echo XTCP2_SELF_TEST_OVERALL_PASS
    '';
  };
  vm = import ./mkVm.nix {
    inherit
      pkgs
      lib
      microvm
      nixpkgs
      ;
    arch = "x86_64";
    xtcp2Package = binaries.xtcp2;
    xtcp2AllPackage = artifact;
    ipfeedCollectorPackage = binaries.ipfeed-collector;
    extraModules = [
      {
        services.xtcp2.enable = lib.mkForce false;
        systemd.services.xtcp2-self-test.enable = lib.mkForce false;
        boot.kernelModules = [
          "rdma_rxe"
          "ib_uverbs"
        ];
        users.users.rdma-denied = {
          isSystemUser = true;
          group = "rdma-denied";
        };
        users.groups.rdma-denied = { };
        systemd.services.linkmonitor-rdma-test = {
          wantedBy = [ "multi-user.target" ];
          after = [ "multi-user.target" ];
          serviceConfig = {
            Type = "oneshot";
            ExecStart = "${guestTest}/bin/linkmonitor-rdma-guest-test";
            StandardOutput = "journal+console";
            StandardError = "journal+console";
          };
        };
      }
    ];
  };
  runner = runners.mkLifecycleFullTest {
    arch = "x86_64";
    inherit vm;
    suffix = "-linkmonitor-rdma";
    extraSentinels = [
      "RDMA_DISCOVERY"
      "RDMA_PERMISSION"
      "RDMA_RECOVERY"
    ];
  };
in
pkgs.writeShellApplication {
  name = "test-linkmonitor-rdma-vm";
  text = ''
    if [ ! -r /dev/kvm ] || [ ! -w /dev/kvm ]; then
      echo 'UNVERIFIED: RDMA guest requires accessible /dev/kvm' >&2
      exit 1
    fi
    exec ${runner}/bin/xtcp2-lifecycle-full-test-x86_64-linkmonitor-rdma "$@"
  '';
}
