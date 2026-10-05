# nix/tests/ipmeta-bootstrap.nix
#
# Focused bootstrap IP metadata checks. These stay fully local: a tiny fixture
# artifact is normalized through the production Nix path, then a bootstrap OCI
# image is inspected as a tar archive without needing Docker.
{
  pkgs,
  lib,
  src,
  vendoredSource,
  binaries,
}:

let
  versions = import ../versions.nix { inherit pkgs; };

  fixtureLockFile = ../testdata/ipmeta-bootstrap/ipmeta-bootstrap-lock.json;
  fixtureArtifact = import ../ipmeta-bootstrap {
    inherit pkgs lib;
    ipmetaBootstrapTool = binaries.ipmeta-bootstrap;
    lockFile = fixtureLockFile;
  };

  bootstrapImagePath = "share/xtcp2/ipmeta/bootstrap.lookup.parquet.zst";

  verifyArtifact =
    name: artifact:
    pkgs.runCommand name
      {
        nativeBuildInputs = [ versions.go ];
      }
      ''
        set -eu
        export HOME="$TMPDIR/home"
        export CGO_ENABLED=0
        export GOFLAGS=-mod=vendor
        mkdir -p "$HOME"

        cp -R ${vendoredSource} source
        chmod -R u+w source
        cd source

        cat > pkg/ipasn/nix_bootstrap_fixture_test.go <<'EOF'
        package ipasn_test

        import (
        	"net/netip"
        	"os"
        	"testing"

        	"github.com/randomizedcoder/xtcp2/pkg/ipasn"
        )

        func TestNixBootstrapFixture(t *testing.T) {
        	path := os.Getenv("XTCP2_IPMETA_ARTIFACT")
        	if path == "" {
        		t.Fatal("XTCP2_IPMETA_ARTIFACT is empty")
        	}
        	ix, err := ipasn.New(path)
        	if err != nil {
        		t.Fatalf("load fixture artifact: %v", err)
        	}
        	if got := ix.Len(); got != 1 {
        		t.Fatalf("Len = %d, want 1", got)
        	}
        	got, ok := ix.Lookup(netip.MustParseAddr("203.0.113.42"))
        	if !ok || got.ASN != 64500 || got.NetworkOwner != "fixture-net" {
        		t.Fatalf("Lookup(203.0.113.42) = (%+v,%v), want fixture-net ASN 64500", got, ok)
        	}
        	if got, ok := ix.Lookup(netip.MustParseAddr("198.51.100.1")); ok {
        		t.Fatalf("Lookup(198.51.100.1) = (%+v,true), want miss", got)
        	}
        	if st := ix.Stats(); st.SourceKind != "parquet.zst" || st.CompressedBytes <= 0 || st.DecompressedBytes <= 0 {
        		t.Fatalf("Stats = %+v, want compressed bootstrap artifact", st)
        	}
        }
        EOF

        cp ${artifact} "$TMPDIR/bootstrap.lookup.parquet.zst"
        XTCP2_IPMETA_ARTIFACT="$TMPDIR/bootstrap.lookup.parquet.zst" go test ./pkg/ipasn -run TestNixBootstrapFixture -count=1

        mkdir -p "$out"
        cp "$TMPDIR/bootstrap.lookup.parquet.zst" "$out/bootstrap.lookup.parquet.zst"
      '';

  containersWithFixture = import ../containers {
    inherit
      pkgs
      lib
      src
      binaries
      ;
    ipmetaBootstrapArtifact = fixtureArtifact;
    daemonAttrSuffix = "-bootstrap-fixture";
    daemonTagSuffix = "-bootstrap-fixture";
  };

  fixtureBootstrapImage = containersWithFixture.oci-xtcp2-min-asn-bootstrap-fixture;
in
{
  artifact = verifyArtifact "xtcp2-test-ipmeta-bootstrap-artifact" fixtureArtifact;

  oci-contents = pkgs.runCommand "xtcp2-test-oci-ipmeta-bootstrap-contents"
    {
      nativeBuildInputs = [
        pkgs.coreutils
        pkgs.diffutils
        pkgs.findutils
        pkgs.gnugrep
        pkgs.gnutar
      ];
    }
    ''
      set -eu

      image="$TMPDIR/image.tar"
      image_dir="$TMPDIR/image"
      extract_dir="$TMPDIR/extracted"
      ${fixtureBootstrapImage} > "$image"

      mkdir -p "$image_dir" "$extract_dir"
      tar -xf "$image" -C "$image_dir"

      found=""
      while IFS= read -r layer; do
        if tar -tf "$layer" | grep -Eq '^(\./)?${bootstrapImagePath}$'; then
          tar -xf "$layer" -C "$extract_dir" "${bootstrapImagePath}" \
            || tar -xf "$layer" -C "$extract_dir" "./${bootstrapImagePath}"
          found="$extract_dir/${bootstrapImagePath}"
          break
        fi
      done < <(find "$image_dir" -type f -name layer.tar)

      if [ -z "$found" ]; then
        echo "missing /${bootstrapImagePath} in bootstrap OCI image" >&2
        exit 1
      fi
      test -s "$found"
      cmp ${fixtureArtifact} "$found"

      mkdir -p "$out"
      cp "$found" "$out/bootstrap.lookup.parquet.zst"
    '';
}
