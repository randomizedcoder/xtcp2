# nix/overlays.nix
#
# Overlay so downstream consumers can `inputs.xtcp2.overlays.default` and pick
# up the xtcp2 binaries and OCI image from their own pkgs set.
#
# Usage in a consumer flake:
#   inputs.xtcp2.url = "github:randomizedcoder/xtcp2";
#   outputs = { nixpkgs, xtcp2, ... }: let
#     pkgs = import nixpkgs { overlays = [ xtcp2.overlays.default ]; system = "x86_64-linux"; };
#   in { packages.default = pkgs.xtcp2; };
#
{ self }:

final: prev: {
  xtcp2 = self.packages.${final.system}.xtcp2 or null;
  xtcp2-all = self.packages.${final.system}.xtcp2-all or null;
  xtcp2-oci = self.packages.${final.system}.oci-xtcp2 or null;
}
