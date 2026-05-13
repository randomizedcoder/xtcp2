# nix/checks/golangci-lint-comprehensive.nix
#
# Tier 2: Comprehensive lint. Tier 1 + exhaustive, prealloc, gocyclo, funlen,
# goconst, dupl, unconvert, nakedret, misspell. Target wall time: ~10 minutes.
#
# Not part of default `nix flake check` — invoke explicitly via
#   nix build .#checks.golangci-lint-comprehensive
#
{
  pkgs,
  lib,
  vendoredSource,
}:

let
  versions = import ../versions.nix { inherit pkgs; };
in
pkgs.runCommand "xtcp2-golangci-lint-comprehensive"
  {
    nativeBuildInputs = [
      versions.go
      versions.golangci-lint
    ];
    inherit vendoredSource;
  }
  ''
    cp -r $vendoredSource ./xtcp2 && chmod -R +w ./xtcp2
    cd ./xtcp2
    export HOME=$(mktemp -d)
    export CGO_ENABLED=0
    export GOFLAGS=-mod=vendor
    golangci-lint run --config .golangci-comprehensive.yml --timeout 15m ./... > $out 2>&1 \
      || (cat $out && exit 1)
  ''
