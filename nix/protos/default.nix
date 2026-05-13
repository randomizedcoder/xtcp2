# nix/protos/default.nix
#
# Entry point for proto-related derivations.
#
{
  pkgs,
  lib,
  src,
}:

{
  lint = import ./buf-lint.nix { inherit pkgs lib src; };
  regenerate = import ./buf-generate.nix { inherit pkgs lib; };
}
