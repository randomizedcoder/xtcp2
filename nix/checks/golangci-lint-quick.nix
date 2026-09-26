# nix/checks/golangci-lint-quick.nix
#
# Tier 0: gofmt, goimports, govet, errcheck, ineffassign, unused, staticcheck.
# Timeout is 180s; see the note in .golangci-quick.yml — the build-tag-gated
# files are in scope now, so the old 60s cap was hit during type-checking.
#
{
  pkgs,
  vendoredSource,
}:

let
  versions = import ../versions.nix { inherit pkgs; };
in
pkgs.runCommand "xtcp2-golangci-lint-quick"
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
    export GOPATH=$HOME/go
    export GOMODCACHE=$HOME/go/pkg/mod
    export GOCACHE=$HOME/go-build
    export GOPROXY=off
    export CGO_ENABLED=0
    export GOFLAGS=-mod=vendor
    # The timeout lives in .golangci-quick.yml (`run.timeout`), not here. A
    # CLI --timeout overrides the config silently, which is how the
    # quality-report copy of this command ended up stuck at the old 60s and
    # reporting a timed-out run as "0 issues."
    golangci-lint run --config .golangci-quick.yml ./... > $out 2>&1 \
      || (cat $out && exit 1)
  ''
