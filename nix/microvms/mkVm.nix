# nix/microvms/mkVm.nix
#
# Parameterized NixOS-microvm definition for xtcp2 lifecycle testing.
#
# Mirrors xdp2's mkVm pattern but slimmed for v1:
#   - x86_64-linux only (KVM accelerated)
#   - imports modules/xtcp2-service.nix as the single systemd-unit source
#   - bundles the self-test as a oneshot service triggered after xtcp2
#   - shares /nix/store with the host via 9p
#
{
  pkgs,
  lib,
  microvm,
  nixpkgs,
  arch,
  xtcp2Package,
  # xtcp2AllPackage is the symlinkJoin of every cmd/* binary (default
  # variant). Installed into environment.systemPackages so the in-VM
  # self-test can `-help` smoke every binary, not just xtcp2.
  xtcp2AllPackage,
}:

let
  constants = import ./constants.nix;
  cfg = constants.architectures.${arch};
  selfTest = import ./self-test.nix {
    inherit pkgs;
    promPort = cfg.promPort;
    grpcPort = cfg.grpcPort;
  };
  vmConfig = ./xtcp2-vm-config.json;
in
(nixpkgs.lib.nixosSystem {
  inherit pkgs;

  modules = [
    microvm.nixosModules.microvm
    ../modules/xtcp2-service.nix

    (
      { config, ... }:
      {
        system.stateVersion = "26.05";
        networking.hostName = cfg.hostname;

        # Trim VM surface area
        documentation.enable = false;
        documentation.man.enable = false;
        documentation.doc.enable = false;
        documentation.info.enable = false;
        documentation.nixos.enable = false;
        security.polkit.enable = false;
        services.udisks2.enable = false;
        programs.command-not-found.enable = false;
        fonts.fontconfig.enable = false;
        nix.enable = false;
        xdg.mime.enable = false;
        hardware.enableRedistributableFirmware = false;
        boot.supportedFilesystems = lib.mkForce [
          "vfat"
          "ext4"
        ];

        microvm = {
          hypervisor = "qemu";
          mem = cfg.mem;
          vcpu = cfg.vcpu;
          cpu = if cfg.useKvm then null else cfg.qemuCpu;
          volumes = [ ];
          interfaces = [
            {
              type = "user";
              id = "eth0";
              mac = "02:00:00:00:10:01";
            }
          ];
          shares = [
            {
              source = "/nix/store";
              mountPoint = "/nix/store";
              tag = "nix-store";
              proto = "9p";
            }
          ];
          qemu = {
            serialConsole = false;
            machine = cfg.qemuMachine;
            package = pkgs.qemu_kvm;
            extraArgs = [
              "-name"
              "${cfg.hostname},process=${cfg.hostname}"
              "-serial"
              "tcp:127.0.0.1:${toString cfg.serialPort},server,nowait"
              "-device"
              "virtio-serial-pci"
              "-chardev"
              "socket,id=virtcon,port=${toString cfg.virtioPort},host=127.0.0.1,server=on,wait=off"
              "-device"
              "virtconsole,chardev=virtcon"
              "-append"
              (builtins.concatStringsSep " " (
                [
                  "console=ttyS0,115200"
                  "console=hvc0"
                  "reboot=t"
                  "panic=-1"
                  "loglevel=4"
                  "init=${config.system.build.toplevel}/init"
                ]
                ++ config.boot.kernelParams
              ))
            ];
          };
        };

        boot.kernelPackages = pkgs.linuxPackages_latest;
        boot.kernelParams = [
          "console=ttyS0,115200"
          "console=hvc0"
          "systemd.default_standard_error=journal+console"
          "systemd.show_status=true"
        ];
        boot.initrd.availableKernelModules = [
          "9p"
          "9pnet"
          "9pnet_virtio"
          "virtio_pci"
          "virtio_console"
        ];
        boot.initrd.systemd.emergencyAccess = true;

        # netlink + io_uring sysctls
        boot.kernel.sysctl = {
          # io_uring availability is gated by kernel.io_uring_disabled on newer kernels
          "kernel.io_uring_disabled" = 0;
        };

        services.getty.autologinUser = "root";
        systemd.enableEmergencyMode = false;

        # The reason we're here: xtcp2 as a systemd unit
        services.xtcp2 = {
          enable = true;
          package = xtcp2Package;
          configFile = vmConfig;
        };

        # Self-test oneshot, fires after xtcp2 has started
        systemd.services.xtcp2-self-test = {
          description = "xtcp2 microvm self-test";
          after = [
            "xtcp2.service"
            "multi-user.target"
          ];
          wants = [ "xtcp2.service" ];
          wantedBy = [ "multi-user.target" ];
          serviceConfig = {
            Type = "oneshot";
            RemainAfterExit = true;
            ExecStart = "${selfTest}/bin/xtcp2-self-test";
            StandardOutput = "journal+console";
            StandardError = "journal+console";
          };
        };

        environment.systemPackages =
          (with pkgs; [
            coreutils
            iproute2
            netcat-gnu
            tcpdump
            curl
            jq
            procps
            util-linux
            systemd
          ])
          ++ [ xtcp2AllPackage ];
      }
    )
  ];
}).config.microvm.declaredRunner
