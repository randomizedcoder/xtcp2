# nix/ernic/default.nix
#
# Everything xtcp2 takes from rocm-ernic, in one attrset, so the input is
# read in one place and every VM shares the same store paths (built once,
# cached for all). See docs/design-rocm-ernic-nics.md.
#
#   server      the rocm-ernic vfio-user server
#   run         ernic-run: one server around a command (ERNIC_SOCKET)
#   rdmaErrors  ernic-rdma-errors, the guest's RDMA error counter check
#   guestModule nixosModules.ernic-guest, for qemu-vm.nix guests on q35
#   profiles    upstream's NIC profiles (generic-100g.ini, ...)
#   ownProfiles xtcp2's NIC profiles (./profiles)
#   nics        NICs as data: qemuArgs, serverArgs, guestConfig (nics.nix)
#   runMany     { name, nics, fabric } -> ernic-run-many-<name> (run-many.nix)
#   oracle      ernic-oracle: guest NIC counters vs the emulator's fault counts
{
  pkgs,
  lib,
  rocmErnic,
}:

let
  packages = rocmErnic.packages.${pkgs.stdenv.hostPlatform.system};
in
{
  server = packages.rocm-ernic;
  run = packages.ernic-run;
  rdmaErrors = packages.ernic-rdma-errors;
  guestModule = rocmErnic.nixosModules.ernic-guest;
  profiles = "${rocmErnic}/profiles";
  ownProfiles = ./profiles;
  nics = import ./nics.nix { inherit lib; };
  oracle = import ./oracle.nix { inherit pkgs lib; };
  runMany = import ./run-many.nix {
    inherit pkgs lib;
    server = packages.rocm-ernic;
  };
}
