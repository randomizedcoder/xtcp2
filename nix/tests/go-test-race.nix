# nix/tests/go-test-race.nix
#
# Whole-repo `go test -race ./...` runner.
#
# The Go race detector requires cgo, so this derivation enables
# CGO_ENABLED=1 and adds gcc to nativeBuildInputs (the rest of the
# repo's Nix builds default to CGO_ENABLED=0 per nix/versions.nix).
#
# Real bugs caught during refactor waves:
#   * F4 backoff-factor data race (concurrent ns-add reading
#     backoffFactorCst while a test mutated it).
#   * F9 socketpair fd recycle (cleanup double-closing an fd the kernel
#     reused for another goroutine's socket).
#
# Making this a first-class Nix check prevents the next race from
# leaking into main.
#
{
  pkgs,
  lib,
  vendoredSource,
}:

let
  versions = import ../versions.nix { inherit pkgs; };
in
pkgs.runCommand "xtcp2-test-go-race"
  {
    nativeBuildInputs = [
      versions.go
      pkgs.gcc # race detector needs cgo
    ];
    inherit vendoredSource;
  }
  ''
    cp -r $vendoredSource ./xtcp2 && chmod -R +w ./xtcp2
    cd ./xtcp2
    export HOME=$(mktemp -d)
    export CGO_ENABLED=1
    export GOFLAGS=-mod=vendor

    set +e
    go test -race -count=1 -timeout 5m ./... > $out 2>&1
    rc=$?
    set -e
    if [ "$rc" -ne 0 ]; then
      cat $out >&2
      exit "$rc"
    fi
  ''
