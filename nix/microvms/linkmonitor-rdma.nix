# Software-RDMA (rxe) guest on the microVM constructor and lifecycle runner,
# run through mkVmCheck: checks.vm-linkmonitor-rdma and the app
# test-linkmonitor-rdma-vm, with the guest's netlink recordings in the
# evidence as nlrecord/.
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
      strace
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
      # The netlink source against the events the verbs backend recorded for
      # the same triggers in R1-T05; lm1 is lm0's veth peer, so its down drops
      # lm0's carrier.
      export LINKMONITOR_EVENTS_NETDEV=lm0 LINKMONITOR_EVENTS_RDMA=lm_rxe LINKMONITOR_EVENTS_PEER=lm1
      ${artifact}/bin/rdma-vm.test -test.run '^TestRDMAEventsRecorded$' -test.v -test.timeout=5m
      ${artifact}/bin/rdma-vm.test -test.run '^TestRDMAEventsFaults$' -test.v -test.timeout=5m
      echo XTCP2_SELF_TEST_RDMA_EVENTS_PASS
      # The monitor runs RDMA events from netlink, and never looks for a verbs
      # provider: the packaging table's no_provider_dlopen row.
      opens=$(mktemp)
      strace -f -qq -e trace=open,openat -o "$opens" ${artifact}/bin/linkmonitor-smoke -rdma-events netlink
      if grep -E 'libibverbs|libibumad|rdmav' "$opens"; then
        echo 'the monitor looked for an rdma-core library or provider' >&2
        exit 1
      fi
      echo XTCP2_SELF_TEST_RDMA_SMOKE_EVENTS_PASS
      # The live umad rows (R2-T03): first with ib_umad removed, which a query
      # must not load, then with it loaded.
      modprobe -r ib_umad
      for phase in unloaded loaded; do
        if [ "$phase" = loaded ]; then modprobe ib_umad; udevadm settle; fi
        LINKMONITOR_UMAD_DEVICE=lm_rxe LINKMONITOR_UMAD_PHASE=$phase \
          ${artifact}/bin/rdmacaps-vm.test -test.run '^TestUMADLive$' -test.v
      done
      echo XTCP2_SELF_TEST_RDMA_UMAD_PASS
      # Permission manipulation is confined to disposable guest fixture nodes.
      for node in /dev/infiniband/uverbs*; do chmod 600 "$node"; done
      if runuser -u rdma-denied -- ibv_devinfo -d lm_rxe; then
        echo 'denied user unexpectedly opened the software device' >&2
        exit 1
      fi
      echo XTCP2_SELF_TEST_RDMA_PERMISSION_PASS
      # The device is deleted while the event source holds it. rxe has no
      # disassociate_ucontext, so unregister waits for open uverbs files; the
      # netlink source holds none, so the delete completes.
      LINKMONITOR_EVENTS_REMOVE='rdma link delete lm_rxe' \
        ${artifact}/bin/rdma-vm.test -test.run '^TestRDMAEventsRemove$' -test.v -test.timeout=5m
      ${artifact}/bin/rdma-vm.test -test.run '^TestSoftwareRDMARemoved$' -test.v
      rdma link add lm_rxe type rxe netdev lm0
      udevadm settle
      ${artifact}/bin/rdma-vm.test -test.run '^TestSoftwareRDMADiscovery$' -test.v
      ibv_devinfo -d lm_rxe
      ${artifact}/bin/linkmonitor-smoke
      # The rescan row: events resume on the new device.
      ${artifact}/bin/rdma-vm.test -test.run '^TestRDMAEventsRecorded$' -test.v -test.timeout=5m
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
      echo 'UNVERIFIED: native InfiniBand UMAD PortInfo replies/speed/duplex; rxe covers umad registration only.'
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
    extraSentinels = steps;
    scrapeArchives = [
      {
        name = "nlrecord";
        marker = "XTCP2_NLREC_DUMP";
        identifier = "linkmonitor-rdma-guest-test";
        dirVar = "XTCP2_NLREC_DIR";
      }
    ];
  };
  steps = [
    "RDMA_DISCOVERY"
    "RDMA_RENAME"
    "RDMA_EVENTS"
    "RDMA_SMOKE_EVENTS"
    "RDMA_UMAD"
    "RDMA_PERMISSION"
    "RDMA_RECOVERY"
    "RDMA_CONTRACTS"
  ];
  # The lifecycle runner writes its transcript to $TMPDIR (the work
  # directory) and its verdict to stdout; serial.log gets the transcript,
  # then the verdict, so it reads like the other guests' logs and its tail
  # says why a run failed. The recordings land in the evidence as nlrecord/.
  # microvm.nix's QEMU takes no QEMU_OPTS, so --accel has no effect here.
  vmCheck = import ./mkVmCheck.nix { inherit pkgs lib; } {
    name = "linkmonitor-rdma";
    runtimeInputs = [ pkgs.coreutils ];
    launch = ''
      if [ ! -r /dev/kvm ] || [ ! -w /dev/kvm ]; then
        echo 'UNVERIFIED: RDMA guest requires accessible /dev/kvm'
        exit 1
      fi
      rc=0
      XTCP2_NLREC_DIR="$VM_EVIDENCE/nlrecord" \
        ${runner}/bin/xtcp2-lifecycle-full-test-x86_64-linkmonitor-rdma > runner.txt 2>&1 || rc=$?
      for log in "$TMPDIR"/xtcp2-vm-*.log; do
        if [ -e "$log" ]; then cat "$log"; fi
      done
      cat runner.txt
      exit "$rc"
    '';
    sentinels = map (step: "XTCP2_SELF_TEST_${step}_PASS") steps ++ [
      "XTCP2_SELF_TEST_OVERALL_PASS"
      "nlrecord: extracted"
    ];
    failPattern = "XTCP2_SELF_TEST_[A-Z_]+_FAIL|--- FAIL:|^FAIL:";
    timeout = "30m";
    evidence = {
      guest = vm;
      inherit artifact;
    };
    postCheck = ''
      if [ ! -s "$run_dir/nlrecord/kernel.txt" ]; then
        fail 'the nlrecord archive has no kernel.txt'
      fi
    '';
  };
in
vmCheck.app // { inherit vmCheck; }
