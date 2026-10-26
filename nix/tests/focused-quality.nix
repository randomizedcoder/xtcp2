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

  # gordma's corpus replay reads pkg/xtcpnl/testdata/*/rdma, which the
  # vendored source carries.
  focused-gordma = mkFocusedTest "gordma" {
    paths = [
      "./internal/gordma/..."
      "./cmd/gordma"
      "./pkg/nlstrace/..."
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

  # cmd/xtcp2 with the embedded treatment agent (`-tags treatment`): the
  # default `go test ./cmd/xtcp2` compiles treatments_embedded.go out, so
  # without this leg a regression in the tagged file is invisible until the
  # fat Nix build. pkg/treatment itself is covered by test-tcp-controller-unit.
  focused-xtcp-treatment = mkFocusedTest "xtcp-treatment" {
    tags = "treatment";
    ldflags = "-checklinkname=0";
    paths = [
      "./cmd/xtcp2"
      "./cmd/xtcp2-treatment-agent"
    ];
  };
}
