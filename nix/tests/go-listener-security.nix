# nix/tests/go-listener-security.nix
#
# Focused test runner for listener security hardening work: UDS listener
# binding, client-side UDS dialing, listener protobuf validation, and config
# redaction/preservation coverage in the daemon package.
#
{
  pkgs,
  lib,
  vendoredSource,
}:

let
  versions = import ../versions.nix { inherit pkgs; };
in
pkgs.runCommand "xtcp2-test-listener-security"
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
    go test -v -count=1 \
      ./pkg/listener \
      ./pkg/xtcp \
      ./cmd/xtcp2 \
      ./cmd/xtcp2client \
      ./cmd/xtcp2ctl \
      > $out/test.log 2>&1
    rc=$?
    set -e
    if [ "$rc" -ne 0 ]; then
      echo "===== test.log =====" >&2
      cat $out/test.log >&2
      exit "$rc"
    fi
    echo "listener-security tests OK" >&2
  ''
