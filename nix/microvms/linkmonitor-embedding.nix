# Minimal disposable guest. QEMU's pinned launcher supports KVM and TCG.
{
  pkgs,
  lib,
  nixpkgs,
  artifact,
}:
let
  namespaceTest = pkgs.writeShellApplication {
    name = "linkmonitor-embedding-namespace";
    runtimeInputs = with pkgs; [
      iproute2
      util-linux
    ];
    text = ''
      mount --make-rprivate /
      mount -t sysfs sysfs /sys
      ip link set lo up
      "$1/core.test" -test.run '^TestEmbedding(VirtualLinux|HostProcess)$' -test.v -test.timeout=180s
      "$1/xtcp.test" -test.run '^TestLinkMonitorOnDemandScrape$' -test.v -test.timeout=180s
    '';
  };
  guestTest = pkgs.writeShellApplication {
    name = "linkmonitor-embedding-guest";
    runtimeInputs = with pkgs; [
      coreutils
      iproute2
      util-linux
      systemd
    ];
    text = ''
      trap 'echo LINKMONITOR_EMBEDDING_FAIL; systemctl poweroff --no-block' ERR
      # New network, mount and PID namespaces; procfs/sysfs match socket scope.
      unshare --net --mount --pid --fork --mount-proc \
        ${namespaceTest}/bin/linkmonitor-embedding-namespace "${artifact}/bin"
      echo LINKMONITOR_EMBEDDING_core_PASS
      echo LINKMONITOR_EMBEDDING_xtcp_PASS
      echo LINKMONITOR_EMBEDDING_PASS
      systemctl poweroff --no-block
    '';
  };
  vm =
    (nixpkgs.lib.nixosSystem {
      system = pkgs.stdenv.hostPlatform.system;
      modules = [
        "${nixpkgs}/nixos/modules/virtualisation/qemu-vm.nix"
        {
          nixpkgs.pkgs = pkgs;
          system.stateVersion = "26.05";
          networking.hostName = "linkmonitor-embedding";
          networking.useDHCP = false;
          boot.kernelModules = [ "veth" ];
          virtualisation = {
            graphics = false;
            memorySize = 2048;
            cores = 2;
            diskImage = null;
            qemu.networkingOptions = lib.mkForce [ ];
            qemu.options = [ "-no-reboot" ];
          };
          systemd.services.linkmonitor-embedding = {
            wantedBy = [ "multi-user.target" ];
            after = [ "systemd-modules-load.service" ];
            serviceConfig = {
              Type = "oneshot";
              ExecStart = "${guestTest}/bin/linkmonitor-embedding-guest";
              StandardOutput = "journal+console";
              StandardError = "journal+console";
            };
          };
        }
      ];
    }).config.system.build.vm;
  # checks.vm-linkmonitor-embedding and test-linkmonitor-embedding-vm
  # (mkVmCheck.nix).
  vmCheck = import ./mkVmCheck.nix { inherit pkgs lib; } {
    name = "linkmonitor-embedding";
    launch = "${vm}/bin/run-linkmonitor-embedding-vm";
    sentinels = [
      "LINKMONITOR_EMBEDDING_core_PASS"
      "LINKMONITOR_EMBEDDING_xtcp_PASS"
      "LINKMONITOR_EMBEDDING_PASS"
      "--- PASS: TestLinkMonitorOnDemandScrape "
    ];
    failPattern = "LINKMONITOR_EMBEDDING_FAIL|--- SKIP:|--- FAIL:";
    evidence = {
      inherit artifact;
      guest = vm;
    };
  };
in
# test-linkmonitor-embedding-vm [--accel=auto|kvm|tcg] [--keep=N]: evidence under
# ~/.cache/xtcp2-vm/linkmonitor-embedding; checks.vm-linkmonitor-embedding is the same run in the build sandbox.
vmCheck.app
// {
  inherit vmCheck;
}
