# nix/tests/focused-quality.nix
#
# Focused test runners for the documentation-alignment / enrichment quality
# path. These are intentionally narrower than quality-report and flake check:
# they give repeatable, cacheable handles for the package sets we reach for
# while iterating on ASN/locality metadata and goip parity work.
#
{
  pkgs,
  lib,
  vendoredSource,
}:

let
  versions = import ../versions.nix { inherit pkgs; };

  mkFocusedTest =
    name:
    {
      paths,
      tags ? "",
      ldflags ? "",
    }:
    pkgs.runCommand "xtcp2-test-focused-${name}"
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
          ${lib.optionalString (ldflags != "") "-ldflags='${ldflags}' "}\
          ${lib.optionalString (tags != "") "-tags '${tags}' "}\
          ${lib.concatStringsSep " " paths} \
          > $out/test.log 2>&1
        rc=$?
        set -e
        if [ "$rc" -ne 0 ]; then
          echo "===== test.log =====" >&2
          cat $out/test.log >&2
          exit "$rc"
        fi
        echo "test-focused-${name} OK" >&2
      '';
in
{
  focused-asn-locality = mkFocusedTest "asn-locality" {
    paths = [
      "./pkg/ipasn"
      "./pkg/localnet"
    ];
  };

  focused-goip = mkFocusedTest "goip" {
    paths = [
      "./internal/goip/..."
      "./cmd/goip"
      "./cmd/goip-parity"
    ];
  };

  focused-xtcp-enrich = mkFocusedTest "xtcp-enrich" {
    tags = "enrich_asn enrich_locality";
    ldflags = "-checklinkname=0";
    paths = [
      "./pkg/xtcp"
      "./cmd/xtcp2"
    ];
  };
}
