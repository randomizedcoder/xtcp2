# nix/checks/proto-field-audit.nix
#
# Cross-checks proto field declarations against Go-side writers.
#
{
  pkgs,
  lib,
  vendoredSource,
}:

let
  versions = import ../versions.nix { inherit pkgs; };
in
pkgs.runCommand "xtcp2-proto-field-audit"
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
    go run ./tools/proto-field-audit -proto-root proto -go-root pkg > $out 2>&1 \
      || (cat $out && exit 1)
  ''
