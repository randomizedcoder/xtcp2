# nix/checks/proto-lint.nix
#
# Re-export of nix/protos/buf-lint.nix for the canonical checks/ namespace.
#
{
  pkgs,
  lib,
  src,
}:

import ../protos/buf-lint.nix { inherit pkgs lib src; }
