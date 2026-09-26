# nix/protos/default.nix
#
# Entry point for proto-related derivations.
#
{
  pkgs,
  src,
}:

{
  lint = import ./buf-lint.nix { inherit pkgs src; };
  regenerate = import ./buf-generate.nix { inherit pkgs; };
}
