# nix/tests/proto-deserialize-golden.nix
#
# Runs the deserialize golden test (was env-gated; now always runs).
#
{
  pkgs,
  lib,
  vendoredSource,
}:

let
  versions = import ../versions.nix { inherit pkgs; };
in
pkgs.runCommand "xtcp2-proto-deserialize-golden"
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
    # The test is in pkg/xtcp/deserialize per the recent commit
    # "Fix TestDeserialize + BenchmarkDeserialize (broken since initial commit)"
    if [ -d pkg/xtcp/deserialize ]; then
      go test -v -run TestDeserialize ./pkg/xtcp/deserialize/... > $out 2>&1 \
        || (cat $out && exit 1)
    else
      echo "proto-deserialize-golden: no pkg/xtcp/deserialize directory; skipping" > $out
    fi
  ''
