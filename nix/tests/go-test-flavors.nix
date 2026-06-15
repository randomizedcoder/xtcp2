# nix/tests/go-test-flavors.nix
#
# Per-build-tag Go test runners. The default `go test ./...` compiles
# without any `dest_*` build tags, so pkg/xtcp/destinations_{kafka,nats,
# nsq,valkey}.go (each guarded by `//go:build dest_<name>`) are excluded
# from coverage. This module produces one derivation per flavor + one
# "all" target that exercises every flavor at once.
#
# Output per derivation:
#   $out/test.log         — full `go test -v` output
#   $out/coverage.out     — coverage profile (covermode=atomic) over
#                           ./pkg/xtcp/... only; the flavor tag set is
#                           what makes the destination files visible
#
# Result: nix/quality-report/default.nix can merge each flavor's
# coverage.out into the headline coverage number so destination flavors
# stop reading 0%.
#
{
  pkgs,
  lib,
  vendoredSource,
}:

let
  versions = import ../versions.nix { inherit pkgs; };

  # One entry per flavor target. `tags` is the space-separated build-tag
  # string passed to `go test -tags`. `all` enables every flavor in one
  # binary so cross-flavor symbols (registry init order, marshaller
  # dispatch) get exercised.
  flavors = {
    kafka  = { tags = "dest_kafka";  };
    nats   = { tags = "dest_nats";   };
    nsq    = { tags = "dest_nsq";    };
    valkey = { tags = "dest_valkey"; };
    all    = { tags = "dest_kafka dest_nats dest_nsq dest_valkey"; };
  };

  mkFlavorTest =
    name: { tags }:
    pkgs.runCommand "xtcp2-test-go-flavor-${name}"
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

        mkdir -p $out
        set +e
        go test -v -tags '${tags}' \
          -covermode=atomic \
          -coverprofile=$out/coverage.out \
          ./pkg/xtcp/... \
          > $out/test.log 2>&1
        rc=$?
        set -e
        if [ "$rc" -ne 0 ]; then
          echo "===== test.log =====" >&2
          cat $out/test.log >&2
          exit "$rc"
        fi
        # Summary line so `nix log` is informative.
        echo "test-go-flavor-${name} OK (tags: ${tags})" >&2
      '';
in
lib.mapAttrs' (name: spec: {
  name = "test-go-flavor-${name}";
  value = mkFlavorTest name spec;
}) flavors
