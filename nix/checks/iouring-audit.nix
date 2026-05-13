# nix/checks/iouring-audit.nix
#
# Runs the custom Go analyzer at tools/iouring-audit against pkg/io_uring.
#
{
  pkgs,
  lib,
  vendoredSource,
}:

let
  versions = import ../versions.nix { inherit pkgs; };
in
pkgs.runCommand "xtcp2-iouring-audit"
  {
    nativeBuildInputs = [ versions.go ];
    inherit vendoredSource;
  }
  ''
    cp -r $vendoredSource ./xtcp2 && chmod -R +w ./xtcp2
    cd ./xtcp2
    export HOME=$(mktemp -d)
    export CGO_ENABLED=0
    export GOFLAGS=-mod=vendor
    go run ./tools/iouring-audit -root pkg/io_uring > $out 2>&1 || (cat $out && exit 1)
  ''
