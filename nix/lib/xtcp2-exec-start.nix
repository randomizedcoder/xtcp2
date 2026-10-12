# nix/lib/xtcp2-exec-start.nix
#
# The ExecStart= line for an xtcp2 systemd unit: the binary plus its flags,
# each one a single systemd word.
#
# The flags used to be joined with spaces, which is not how systemd reads an
# Exec line. It splits on whitespace, honors quotes and expands % specifiers
# and $VARIABLES, so an empty value vanished, a value with a space became two
# words, and % or $ was rewritten. The interface-naming flavor's
# `-ipmetaBootstrapPath ""` (no bootstrap) lost its value, the flag took
# `-ipmetaCachePath` as its path, and the cache path became a positional
# argument the daemon ignored: the ASN cache went to the default directory and
# self-test check 5g failed with cache_ok=0.
#
# nixpkgs' escapeSystemdExecArgs quotes every word (a JSON string is a valid
# systemd quoted word) and doubles % and $, so each list element reaches xtcp2
# as exactly one argv entry, byte for byte. It needs none of utils.nix's config
# arguments, so this works outside a NixOS module too.
{ pkgs, lib }:
let
  utils = import "${pkgs.path}/nixos/lib/utils.nix" {
    inherit pkgs lib;
    config = { };
  };
in
{
  # mkExecStart package args → `"<package>/bin/xtcp2" "<arg>" ...`.
  mkExecStart = package: args: utils.escapeSystemdExecArgs ([ "${package}/bin/xtcp2" ] ++ args);

  # The quoting itself, for the check.
  inherit (utils) escapeSystemdExecArgs;
}
