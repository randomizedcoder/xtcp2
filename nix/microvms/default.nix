# nix/microvms/default.nix
#
# Entry point for xtcp2 microvm infrastructure.
#
# Exports per-arch attribute sets:
#   vms.${arch}                          the runnable microvm
#   lifecycle.${arch}.fullTest           the host-side launcher + sentinel scrape
#   checks.${arch}.lifecycle             flake-check-compatible derivation
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
    };

  vms = lib.genAttrs constants.supportedArchs mkOne;

  lifecycle = lib.genAttrs constants.supportedArchs (arch: {
    fullTest = microvmLib.mkLifecycleFullTest {
      inherit arch;
      vm = vms.${arch};
    };
  });

  # nix flake check compatible derivation. Builds the launcher (cheap) and
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
in
{
  inherit vms lifecycle checks;
}
