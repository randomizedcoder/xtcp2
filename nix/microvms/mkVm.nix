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
# Two flavors selected by `sink`:
#   - "minimal" (default): xtcp2 alone, JSONL configFile (currently a no-op
#                          stub; the netlink-readout check tolerates a missing
#                          file). Cheap CI smoke.
#   - "vector":            xtcp2 → unixgram UDS → Vector → parquet → MinIO,
#                          all inside the VM. Uses memVector budget. Self-test
#                          checks VECTOR/MINIO/PARQUET sentinels in addition
#                          to the rest of the suite.
#
{
  pkgs,
  lib,
  microvm,
  nixpkgs,
  arch,
  xtcp2Package,
  xtcp2AllPackage,
  sink ? "minimal",
  # Required when sink == "vector". A derivation that provides
  # share/xtcp2/xtcp_flat_record.desc. See nix/lib/mkProtoDescSet.nix.
  protoDescPackage ? null,
}:

let
  constants = import ./constants.nix;
  cfg = constants.architectures.${arch};

  isVector = sink == "vector";
  isCoverage = sink == "coverage" || sink == "coverage-iouring";
  isCoverageIoUring = sink == "coverage-iouring";
  effectiveMem = if isVector then cfg.memVector else cfg.mem;

  coverDir = "/var/lib/xtcp2cov";

  selfTest =
    if isVector then
      import ./self-test-vector.nix {
        inherit pkgs;
        promPort = cfg.promPort;
        grpcPort = cfg.grpcPort;
      }
    else
      import ./self-test.nix {
        inherit pkgs lib;
        promPort = cfg.promPort;
        grpcPort = cfg.grpcPort;
        coverageEnabled = isCoverage;
        inherit coverDir;
      };

  vmConfig = ./xtcp2-vm-config.json;

  vectorModules =
    assert lib.assertMsg (
      protoDescPackage != null
    ) "mkVm.nix: sink=\"vector\" requires protoDescPackage";
    [
      (import ../modules/vector-pipeline.nix {
        inherit protoDescPackage;
      })
      (import ../modules/minio-bucket-bootstrap.nix { })
      ../modules/xtcp2-vector-path.nix
    ];

  xtcp2VectorArgs = [
    "-dest"
    "unixgram:/run/xtcp2/output.sock"
    "-marshal"
    "protobufSingle"
    "-frequency"
    "2s"
    # xtcp2 requires `-timeout < -frequency`; defaults are 5 s / 10 s. With
    # frequency dropped to 2 s for fast lifecycle-test cycles, timeout must
    # come down too.
    "-timeout"
    "1s"
  ];

  # Coverage flavor uses `-dest null` so the kafka destination factory
  # doesn't try to open /xtcp_flat_record.proto (which lives only in the
  # source tree, not in the VM's stripped binary). Same goal as the
  # plan's wave-10-step-5 fix for the basic VM.
  xtcp2CoverageArgs = [
    "-dest"
    "null"
    "-frequency"
    "2s"
    "-timeout"
    "1s"
  ]
  # sink=coverage-iouring adds -ioUring so the netlinkerIoUring code
  # path runs (otherwise 0% covered; the syscall variant runs by default).
  ++ lib.optionals isCoverageIoUring [ "-ioUring" ];
in
(nixpkgs.lib.nixosSystem {
  inherit pkgs;

  modules = [
    microvm.nixosModules.microvm
    ../modules/xtcp2-service.nix
  ]
  ++ lib.optionals isVector vectorModules
  ++ [
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
          mem = effectiveMem;
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

        # xtcp2 enumerates network namespaces by listing /run/netns/ and
        # /run/docker/netns/. If neither exists it fatal-exits with
        # "neither network namespace directory exists.  ??!"
        # (pkg/xtcp/init.go:130). Pre-create the linux one so xtcp2 starts
        # cleanly in a fresh microvm where no namespaces have been added.
        # When sink=coverage, also create the coverage output directory
        # the xtcp2-cover binary writes counter+meta files into.
        systemd.tmpfiles.rules = [
          "d /run/netns 0755 root root -"
        ]
        ++ lib.optional isCoverage "d ${coverDir} 0755 root root -";

        # GOCOVERDIR for the coverage-instrumented xtcp2 build. The runtime
        # writes covcounters.* + covmeta files into this directory on clean
        # exit (SIGTERM via systemctl stop). The self-test scrapes those
        # files between XTCP2_COVERAGE_DUMP_{START,END} markers.
        systemd.services.xtcp2 = lib.mkIf isCoverage {
          environment.GOCOVERDIR = coverDir;
        };

        services.getty.autologinUser = "root";
        systemd.enableEmergencyMode = false;

        # The reason we're here: xtcp2 as a systemd unit
        services.xtcp2 = {
          enable = true;
          package = xtcp2Package;
          configFile = vmConfig;
          extraArgs =
            if isVector then
              xtcp2VectorArgs
            else if isCoverage then
              xtcp2CoverageArgs
            else
              [ ];
        };

        # Self-test oneshot. The self-test's check 1 retries `systemctl
        # is-active xtcp2` for 30 s, so it is robust to xtcp2 starting via
        # the systemd.path gate (vector flavor) vs. directly at boot
        # (minimal flavor).
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
          ++ lib.optionals isVector (
            with pkgs;
            [
              vector
              minio
              minio-client
              duckdb
            ]
          )
          ++ [ xtcp2AllPackage ];
      }
    )
  ];
}).config.microvm.declaredRunner
