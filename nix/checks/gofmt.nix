# nix/checks/gofmt.nix
#
# `gofmt -l .` — fails on any unformatted file.
#
{
  pkgs,
  lib,
  src,
}:

let
  versions = import ../versions.nix { inherit pkgs; };
in
pkgs.runCommand "xtcp2-gofmt"
  {
    nativeBuildInputs = [ versions.go ];
    inherit src;
  }
  ''
    cp -r $src/. ./xtcp2 && chmod -R +w ./xtcp2
    cd ./xtcp2
    unformatted=$(gofmt -l . 2>&1 | grep -v -E '(^vendor/|\.pb\.go$|\.pb\.gw\.go$|^gen/|^dart/|^python/)' || true)
    if [ -n "$unformatted" ]; then
      echo "gofmt: the following files are not formatted:" >&2
      echo "$unformatted" >&2
      exit 1
    fi
    echo "gofmt: all files formatted" > $out
  ''
