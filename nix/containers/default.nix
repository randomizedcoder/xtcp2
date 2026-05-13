# nix/containers/default.nix
#
# Entry point for container images.
#
{
  pkgs,
  lib,
  src,
  xtcp2All,
}:

{
  oci-xtcp2 = import ./oci-xtcp2.nix {
    inherit
      pkgs
      lib
      src
      xtcp2All
      ;
  };
}
