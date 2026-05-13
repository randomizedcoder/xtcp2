# nix/checks/metrics-audit.nix
#
# Runs the custom Go analyzer at tools/metrics-audit across the repo.
#
{
  pkgs,
  lib,
  vendoredSource,
}:

let
  versions = import ../versions.nix { inherit pkgs; };
in
pkgs.runCommand "xtcp2-metrics-audit"
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
    go run ./tools/metrics-audit -root . > $out 2>&1 || (cat $out && exit 1)
  ''
