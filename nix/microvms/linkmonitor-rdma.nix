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
  # The same capture that produced pkg/linkmonitor/testdata/rdmasysfs, for
  # TestRDMACountersRealSysfs; it rides out in the nlrecord archive.
  sysfsCapture = pkgs.writeShellApplication {
    name = "rdmasysfs-capture";
    runtimeInputs = with pkgs; [
      coreutils
      findutils
    ];
    text = builtins.readFile ../../pkg/linkmonitor/testdata/rdmasysfs/capture.sh;
  };
  guestTest = pkgs.writeShellApplication {
    name = "linkmonitor-rdma-guest-test";
    runtimeInputs = with pkgs; [
      bash
      coreutils
      gnutar
      gzip
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
      ${artifact}/bin/rdma-vm.test -test.run '^TestSoftwareRDMAEventsSurviveRename$' -test.v
      echo XTCP2_SELF_TEST_RDMA_RENAME_PASS
      # The go-native differential (R1-T05): verbs and netlink sources side by
      # side; lm1 is lm0's veth peer, so its down drops lm0's carrier.
      export LINKMONITOR_EVENTS_NETDEV=lm0 LINKMONITOR_EVENTS_RDMA=lm_rxe LINKMONITOR_EVENTS_PEER=lm1
      ${artifact}/bin/rdma-vm.test -test.run '^TestDifferentialRDMAEvents$' -test.v -test.timeout=5m
      ${artifact}/bin/rdma-vm.test -test.run '^TestDifferentialRDMAFaults$' -test.v -test.timeout=5m
      echo XTCP2_SELF_TEST_RDMA_DIFFERENTIAL_PASS
      # The cgo-free build's monitor runs RDMA events from netlink.
      ${artifact}/bin/linkmonitor-core-smoke -rdma-events netlink
      echo XTCP2_SELF_TEST_RDMA_CORE_EVENTS_PASS
      # Permission manipulation is confined to disposable guest fixture nodes.
      for node in /dev/infiniband/uverbs*; do chmod 600 "$node"; done
      if runuser -u rdma-denied -- ibv_devinfo -d lm_rxe; then
        echo 'denied user unexpectedly opened the software device' >&2
        exit 1
      fi
      echo XTCP2_SELF_TEST_RDMA_PERMISSION_PASS
      # The device is deleted while the netlink side holds it. rxe has no
      # disassociate_ucontext, so unregister waits for open uverbs files and
      # delivers no DEVICE_FATAL: the verbs side would block the delete. The
      # ionic guest runs this row with both sides.
      LINKMONITOR_EVENTS_REMOVE='rdma link delete lm_rxe' LINKMONITOR_EVENTS_REMOVE_BACKENDS=netlink \
        ${artifact}/bin/rdma-vm.test -test.run '^TestDifferentialRDMARemove$' -test.v -test.timeout=5m
      ${artifact}/bin/rdma-vm.test -test.run '^TestSoftwareRDMARemoved$' -test.v
      rdma link add lm_rxe type rxe netdev lm0
      udevadm settle
      ${artifact}/bin/rdma-vm.test -test.run '^TestSoftwareRDMADiscovery$' -test.v
      ibv_devinfo -d lm_rxe
      ${artifact}/bin/linkmonitor-smoke
      # The rescan row: events resume from both sides on the new device.
      ${artifact}/bin/rdma-vm.test -test.run '^TestDifferentialRDMAEvents$' -test.v -test.timeout=5m
      echo XTCP2_SELF_TEST_RDMA_RECOVERY_PASS
      # Record every linuxio request shape against the guest kernel; the
      # runner unpacks the dump into replay fixtures (TestRecordedContracts).
      nlrec=$(mktemp -d)
      LINKMONITOR_NLRECORD="$nlrec" LINKMONITOR_LIVE_INTERFACE=lm0 \
        ${artifact}/bin/linuxio-nlrecord.test -test.run '^TestLiveRequestShapes$' -test.v
      ${sysfsCapture}/bin/rdmasysfs-capture rxe-vm lm_rxe > "$nlrec/rdmasysfs-lm_rxe.txt"
      uname -r > "$nlrec/kernel.txt"
      echo XTCP2_NLREC_DUMP_START
      tar c -C "$nlrec" . | gzip -n | base64 -w0
      echo
      echo XTCP2_NLREC_DUMP_END
      echo XTCP2_SELF_TEST_RDMA_CONTRACTS_PASS
      echo 'UNVERIFIED: native InfiniBand UMAD PortInfo replies/speed/duplex.'
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
      "RDMA_RENAME"
      "RDMA_DIFFERENTIAL"
      "RDMA_CORE_EVENTS"
      "RDMA_PERMISSION"
      "RDMA_RECOVERY"
      "RDMA_CONTRACTS"
    ];
    scrapeArchives = [
      {
        name = "nlrecord";
        marker = "XTCP2_NLREC_DUMP";
        identifier = "linkmonitor-rdma-guest-test";
        dirVar = "XTCP2_NLREC_DIR";
      }
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
