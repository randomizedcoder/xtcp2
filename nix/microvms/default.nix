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

  vms = lib.genAttrs constants.supportedArchs mkOne;

  vmsVector = lib.optionalAttrs (protoDescPackage != null) (
    lib.genAttrs constants.supportedArchs mkOneVector
  );

  lifecycle = lib.genAttrs constants.supportedArchs (arch: {
    fullTest = microvmLib.mkLifecycleFullTest {
      inherit arch;
      vm = vms.${arch};
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
    lifecycle
    lifecycleVector
    checks
    checksVector
    ;
}
