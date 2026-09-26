# nix/checks/golangci-lint.nix
#
# Tier 1: CI gating lint. Tier 0 + gosec, gocritic, revive, noctx, contextcheck,
# durationcheck. Target wall time: ~2 minutes.
#
{
  pkgs,
  vendoredSource,
}:

let
  versions = import ../versions.nix { inherit pkgs; };
in
pkgs.runCommand "xtcp2-golangci-lint"
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
    # Timeout comes from .golangci.yml (`run.timeout`); a CLI --timeout
    # would override it silently. See nix/checks/golangci-lint-quick.nix.
    golangci-lint run --config .golangci.yml ./... > $out 2>&1 \
      || (cat $out && exit 1)
  ''
