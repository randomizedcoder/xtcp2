# nix/protos/buf-lint.nix
#
# Hermetic `buf lint` check.
#
# Reads sources only; no plugin downloads. Suitable for `nix flake check`.
#
{
  pkgs,
  lib,
  src,
}:

let
  versions = import ../versions.nix { inherit pkgs; };
in
pkgs.runCommand "xtcp2-buf-lint"
  {
    nativeBuildInputs = [ versions.buf ];
    inherit src;
  }
  ''
    cp -r $src/. ./xtcp2 && chmod -R +w ./xtcp2
    cd ./xtcp2

    export BUF_CACHE_DIR=$TMPDIR/.buf-cache
    mkdir -p "$BUF_CACHE_DIR"

    # buf lint does not need network if buf.lock is present and modules are vendored.
    # If buf.lock is missing or stale, this check will report it.
    buf lint > $out 2>&1 || (cat $out && exit 1)
  ''
