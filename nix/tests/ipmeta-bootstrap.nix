# nix/tests/ipmeta-bootstrap.nix
#
# Focused bootstrap IP metadata checks. These stay fully local: an artifact is
# normalized through the production Nix path, loaded by pkg/ipasn, then a
# bootstrap OCI image is inspected as a tar archive without needing Docker.
#
# Two artifacts go through the same checks:
#   fixture   a one-prefix artifact from nix/testdata, so the wiring is tested
#             with data whose every row is known.
#   snapshot  the real checked-in snapshot (nix/ipmeta-bootstrap-lock.json)
#             that ships in the *-bootstrap images, so a truncated or stale
#             refresh fails here rather than on a host.
{
  pkgs,
  lib,
  src,
  vendoredSource,
  binaries,
  ipmetaBootstrapArtifact,
  snapshotImage,
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

  fixtureTest = ''
    package ipasn_test

    import (
    	"net/netip"
    	"os"
    	"testing"

    	"github.com/randomizedcoder/xtcp2/pkg/ipasn"
    )

    func TestNixBootstrapArtifact(t *testing.T) {
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
  '';

  # The addresses are ones the providers have published for years; a refresh
  # that changes one of these answers is worth a look before it ships.
  snapshotTest = ''
    package ipasn_test

    import (
    	"net/netip"
    	"os"
    	"testing"

    	"github.com/randomizedcoder/xtcp2/pkg/ipasn"
    )

    // minSnapshotPrefixes is well under the ~430k prefixes of a full refresh,
    // and well over what is left when a large feed (Apple, Azure) is missing.
    const minSnapshotPrefixes = 100000

    func TestNixBootstrapArtifact(t *testing.T) {
    	path := os.Getenv("XTCP2_IPMETA_ARTIFACT")
    	if path == "" {
    		t.Fatal("XTCP2_IPMETA_ARTIFACT is empty")
    	}
    	ix, err := ipasn.New(path)
    	if err != nil {
    		t.Fatalf("load snapshot: %v", err)
    	}
    	if got := ix.Len(); got < minSnapshotPrefixes {
    		t.Fatalf("Len = %d, want at least %d", got, minSnapshotPrefixes)
    	}
    	if st := ix.Stats(); st.SourceKind != "parquet.zst" || st.CompressedBytes <= 0 || st.DecompressedBytes <= 0 {
    		t.Fatalf("Stats = %+v, want compressed bootstrap artifact", st)
    	}

    	tests := []struct {
    		category    string
    		description string
    		addr        string
    		expected    ipasn.Attr
    		expectedOK  bool
    	}{
    		{"positive", "Cloudflare, cloudflare-v4", "104.16.0.1", ipasn.Attr{ASN: 13335, NetworkOwner: "cloudflare"}, true},
    		{"positive", "Cloudflare, cloudflare-v6", "2606:4700::1", ipasn.Attr{ASN: 13335, NetworkOwner: "cloudflare"}, true},
    		{"positive", "Google public DNS, gcp-goog", "8.8.8.8", ipasn.Attr{ASN: 15169, NetworkOwner: "google"}, true},
    		{"positive", "GitHub, github-meta", "140.82.112.1", ipasn.Attr{ASN: 36459, NetworkOwner: "github"}, true},
    		{"negative", "TEST-NET-1 is in no feed", "192.0.2.1", ipasn.Attr{}, false},
    		{"negative", "RFC 1918 is in no feed", "10.0.0.1", ipasn.Attr{}, false},
    		{"negative", "IPv6 documentation prefix is in no feed", "2001:db8::1", ipasn.Attr{}, false},
    		{"boundary", "first address of Cloudflare's 104.16.0.0/13", "104.16.0.0", ipasn.Attr{ASN: 13335, NetworkOwner: "cloudflare"}, true},
    		{"boundary", "last address of Cloudflare's 104.16.0.0/13", "104.23.255.255", ipasn.Attr{ASN: 13335, NetworkOwner: "cloudflare"}, true},
    		{"corner", "IPv4-mapped IPv6 answers as its IPv4 address", "::ffff:8.8.8.8", ipasn.Attr{ASN: 15169, NetworkOwner: "google"}, true},
    		{"corner", "unspecified address", "0.0.0.0", ipasn.Attr{}, false},
    	}
    	for _, tc := range tests {
    		t.Run(tc.category+"/"+tc.description, func(t *testing.T) {
    			got, ok := ix.Lookup(netip.MustParseAddr(tc.addr))
    			if ok != tc.expectedOK || got != tc.expected {
    				t.Fatalf("Lookup(%s) = (%+v,%v), expected (%+v,%v)", tc.addr, got, ok, tc.expected, tc.expectedOK)
    			}
    		})
    	}
    }
  '';

  verifyArtifact =
    name: artifact: testSource:
    pkgs.runCommand name
      {
        nativeBuildInputs = [ versions.go ];
        passAsFile = [ "testSource" ];
        inherit testSource;
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

        cp "$testSourcePath" pkg/ipasn/nix_bootstrap_artifact_test.go

        cp ${artifact} "$TMPDIR/bootstrap.lookup.parquet.zst"
        XTCP2_IPMETA_ARTIFACT="$TMPDIR/bootstrap.lookup.parquet.zst" go test ./pkg/ipasn -run TestNixBootstrapArtifact -count=1 -v

        mkdir -p "$out"
        cp "$TMPDIR/bootstrap.lookup.parquet.zst" "$out/bootstrap.lookup.parquet.zst"
      '';

  ociContents =
    name: image: artifact:
    pkgs.runCommand name
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
        ${image} > "$image"

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
        cmp ${artifact} "$found"

        mkdir -p "$out"
        cp "$found" "$out/bootstrap.lookup.parquet.zst"
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
  artifact = verifyArtifact "xtcp2-test-ipmeta-bootstrap-artifact" fixtureArtifact fixtureTest;
  oci-contents =
    ociContents "xtcp2-test-oci-ipmeta-bootstrap-contents" fixtureBootstrapImage
      fixtureArtifact;

  snapshot =
    verifyArtifact "xtcp2-test-ipmeta-bootstrap-snapshot" ipmetaBootstrapArtifact
      snapshotTest;
  oci-snapshot-contents =
    ociContents "xtcp2-test-oci-ipmeta-bootstrap-snapshot-contents" snapshotImage
      ipmetaBootstrapArtifact;
}
