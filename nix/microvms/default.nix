# nix/microvms/default.nix
#
# Entry point for xtcp2 microvm infrastructure.
#
# Exports per-arch attribute sets:
#   vms.${arch}                          the runnable minimal microvm
#   vmsVector.${arch}                    the runnable Vector-flavor microvm
#   lifecycle.${arch}.fullTest           host-side launcher (minimal)
#   lifecycleVector.${arch}.fullTest     host-side launcher (vector)
#   checks.${arch}.lifecycle             flake-check-compatible (minimal)
#   checksVector.${arch}.lifecycle       flake-check-compatible (vector)
#
# Currently supportedArchs = [ "x86_64" ]. To add another, edit constants.nix.
#
{
  pkgs,
  lib,
  microvm,
  nixpkgs,
  xtcp2Package,
  xtcp2AllPackage,
  # Optional: descriptor-set derivation needed by the Vector flavor. When
  # null, the Vector flavor attrs are not exposed (so callers that don't
  # have the descriptor set built yet still get the minimal flavor).
  protoDescPackage ? null,
  # Optional: the streamLayeredImage script for oci-xtcp2-tcp-stress.
  # Phase C ("tcp-stress" sink) loads this into the in-VM docker daemon
  # at boot and spawns N containers from it. When null, the tcp-stress
  # flavor attrs are not exposed.
  tcpStressImage ? null,
  # Optional: a coverage-instrumented xtcp2 build (see nix/binaries.nix
  # xtcp2-cover). When non-null, the coverage flavor is exposed. The
  # microvm runs the cover binary with GOCOVERDIR set to a tmpfs path,
  # then the self-test stops xtcp2 to flush counter data and tar+base64s
  # it out via the serial console for the host lifecycle runner to scrape.
  xtcp2CoverPackage ? null,
}:

let
  constants = import ./constants.nix;
  microvmLib = import ./lib.nix { inherit pkgs lib constants; };

  mkOne =
    arch:
    import ./mkVm.nix {
      inherit
        pkgs
        lib
        microvm
        nixpkgs
        arch
        xtcp2Package
        xtcp2AllPackage
        ;
      sink = "minimal";
    };

  mkOneVector =
    arch:
    import ./mkVm.nix {
      inherit
        pkgs
        lib
        microvm
        nixpkgs
        arch
        xtcp2Package
        xtcp2AllPackage
        protoDescPackage
        ;
      sink = "vector";
    };

  mkOneCoverage =
    arch:
    import ./mkVm.nix {
      inherit
        pkgs
        lib
        microvm
        nixpkgs
        arch
        xtcp2AllPackage
        ;
      xtcp2Package = xtcp2CoverPackage;
      sink = "coverage";
    };

  mkOneCoverageIoUring =
    arch:
    import ./mkVm.nix {
      inherit
        pkgs
        lib
        microvm
        nixpkgs
        arch
        xtcp2AllPackage
        ;
      xtcp2Package = xtcp2CoverPackage;
      sink = "coverage-iouring";
    };

  mkOneSoak =
    arch:
    import ./mkVm.nix {
      inherit
        pkgs
        lib
        microvm
        nixpkgs
        arch
        xtcp2Package
        xtcp2AllPackage
        ;
      sink = "soak";
    };

  mkOneTcpStress =
    arch:
    import ./mkVm.nix {
      inherit
        pkgs
        lib
        microvm
        nixpkgs
        arch
        xtcp2Package
        xtcp2AllPackage
        tcpStressImage
        ;
      sink = "tcp-stress";
    };

  vms = lib.genAttrs constants.supportedArchs mkOne;

  vmsVector = lib.optionalAttrs (protoDescPackage != null) (
    lib.genAttrs constants.supportedArchs mkOneVector
  );

  vmsCoverage = lib.optionalAttrs (xtcp2CoverPackage != null) (
    lib.genAttrs constants.supportedArchs mkOneCoverage
  );

  vmsCoverageIoUring = lib.optionalAttrs (xtcp2CoverPackage != null) (
    lib.genAttrs constants.supportedArchs mkOneCoverageIoUring
  );

  vmsSoak = lib.genAttrs constants.supportedArchs mkOneSoak;

  vmsTcpStress = lib.optionalAttrs (tcpStressImage != null) (
    lib.genAttrs constants.supportedArchs mkOneTcpStress
  );

  lifecycle = lib.genAttrs constants.supportedArchs (arch: {
    fullTest = microvmLib.mkLifecycleFullTest {
      inherit arch;
      vm = vms.${arch};
      # Surface every sentinel the self-test emits so a real failure in
      # Check 4+ (BINARIES_HELP, GRPC_ROUNDTRIP, NS_*) doesn't hide
      # behind an unhelpful OVERALL_FAIL with no breadcrumbs.
      sentinelRe = "SYSTEMD|METRICS|NETLINK|BINARIES_HELP|GRPC_ROUNDTRIP|NS_INSPECT|NSTEST|NS_LIFECYCLE|NS_TRAFFIC|NS_DOCKER|OVERALL";
    };
  });

  lifecycleVector = lib.optionalAttrs (protoDescPackage != null) (
    lib.genAttrs constants.supportedArchs (arch: {
      fullTest = microvmLib.mkLifecycleFullTest {
        inherit arch;
        vm = vmsVector.${arch};
        suffix = "-vector";
        sentinelRe = "SYSTEMD|METRICS|VECTOR|MINIO|PARQUET|BINARIES_HELP|GRPC_ROUNDTRIP|NS_INSPECT|NSTEST|OVERALL";
        timeoutSec = 240;
      };
    })
  );

  lifecycleCoverage = lib.optionalAttrs (xtcp2CoverPackage != null) (
    lib.genAttrs constants.supportedArchs (arch: {
      fullTest = microvmLib.mkLifecycleFullTest {
        inherit arch;
        vm = vmsCoverage.${arch};
        suffix = "-coverage";
        scrapeCoverage = true;
        # Surface the new NS_LIFECYCLE + NS_TRAFFIC sentinels from
        # self-test.nix Checks 8+9 so the lifecycle output makes their
        # outcome visible. Without this the default filter hides
        # them; the checks still execute (and the daemon exercises the
        # corresponding code paths) but the harness output is misleading.
        sentinelRe = "SYSTEMD|METRICS|NETLINK|BINARIES_HELP|GRPC_ROUNDTRIP|NS_INSPECT|NSTEST|NS_LIFECYCLE|NS_TRAFFIC|NS_DOCKER|OVERALL";
      };
    })
  );

  lifecycleCoverageIoUring = lib.optionalAttrs (xtcp2CoverPackage != null) (
    lib.genAttrs constants.supportedArchs (arch: {
      fullTest = microvmLib.mkLifecycleFullTest {
        inherit arch;
        vm = vmsCoverageIoUring.${arch};
        suffix = "-coverage-iouring";
        scrapeCoverage = true;
        sentinelRe = "SYSTEMD|METRICS|NETLINK|BINARIES_HELP|GRPC_ROUNDTRIP|NS_INSPECT|NSTEST|NS_LIFECYCLE|NS_TRAFFIC|NS_DOCKER|OVERALL";
      };
    })
  );

  soak = lib.genAttrs constants.supportedArchs (arch: {
    runner = microvmLib.mkSoakRunner {
      inherit arch;
      vm = vmsSoak.${arch};
    };
  });

  # nix flake check compatible derivations. Builds the launcher (cheap) and
  # invokes the VM. Note: requires KVM access — CI runners without /dev/kvm
  # will need to mark this check as host-only or use --keep-going.
  checks = lib.genAttrs constants.supportedArchs (
    arch:
    pkgs.runCommand "xtcp2-microvm-lifecycle-${arch}"
      {
        nativeBuildInputs = [ lifecycle.${arch}.fullTest ];
      }
      ''
        xtcp2-lifecycle-full-test-${arch} > $out 2>&1 || (cat $out && exit 1)
      ''
  );

  checksVector = lib.optionalAttrs (protoDescPackage != null) (
    lib.genAttrs constants.supportedArchs (
      arch:
      pkgs.runCommand "xtcp2-microvm-lifecycle-${arch}-vector"
        {
          nativeBuildInputs = [ lifecycleVector.${arch}.fullTest ];
        }
        ''
          xtcp2-lifecycle-full-test-${arch}-vector > $out 2>&1 || (cat $out && exit 1)
        ''
    )
  );
in
{
  inherit
    vms
    vmsVector
    vmsCoverage
    vmsCoverageIoUring
    vmsSoak
    vmsTcpStress
    lifecycle
    lifecycleVector
    lifecycleCoverage
    lifecycleCoverageIoUring
    soak
    checks
    checksVector
    ;
}
