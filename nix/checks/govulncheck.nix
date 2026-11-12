# nix/checks/govulncheck.nix
#
# Known vulnerabilities reachable from xtcp2's code, by govulncheck against an
# offline snapshot of the Go vulnerability database (nix/lib/govulndb.nix),
# so the check is pure and its result changes only when the code, the Go
# toolchain or the snapshot does. A finding the code reaches fails the build
# unless it is in `accepted` below; a finding in a required module the code
# does not call is reported but does not fail.
#
# Two passes: no build tags (the min flavor), and every destination,
# enrichment and goip_write tag, so code behind a tag is scanned too.
#
# The snapshot ages; `nix run .#govulncheck-fresh` checks the working tree
# against the live database, and moving the pin in nix/lib/govulndb.nix brings
# what it finds into this check.
{
  pkgs,
  vendoredSource,
}:

let
  versions = import ../versions.nix { inherit pkgs; };
  db = import ../lib/govulndb.nix { inherit pkgs; };
  allTags = pkgs.lib.concatStringsSep "," (
    map (d: "dest_${d}") versions.allLibraryDestinations
    ++ map (e: "enrich_${e}") versions.allEnrichmentFeatures
    ++ versions.goipWriteTags
  );

  # Reachable findings accepted for now, each with why and what removes it.
  # Keep this short: an entry is a known exposure, not a suppression.
  accepted = {
    "GO-2026-6859" =
      "net/http, fixed in go1.27.4; the pinned nixpkgs has go1.27.3. Remove with the toolchain bump.";
  };
  acceptedJSON = builtins.toJSON (builtins.attrNames accepted);
in
pkgs.runCommand "xtcp2-govulncheck"
  {
    nativeBuildInputs = [
      versions.go
      versions.govulncheck
      pkgs.jq
    ];
    inherit vendoredSource;
    # For the govulncheck-fresh app, so both scan the same tag sets.
    passthru = { inherit allTags; };
  }
  ''
    cp -r $vendoredSource ./xtcp2 && chmod -R +w ./xtcp2
    cd ./xtcp2
    export HOME=$(mktemp -d)
    export CGO_ENABLED=0
    export GOFLAGS=-mod=vendor
    export GOTOOLCHAIN=local
    mkdir -p $out
    # One pass per tag set: the text report for people, the JSON stream for
    # the gate. A finding is reachable when its trace starts at a function.
    scan() {
      govulncheck -db file://${db} "$@" ./... > "$out/$name.txt" 2>&1 || true
      govulncheck -db file://${db} -format json "$@" ./... > "$out/$name.json"
      jq -r 'select(.finding and .finding.trace[0].function) | .finding.osv' \
        "$out/$name.json" | sort -u > "$out/$name.reachable"
    }
    name=default scan
    name=all-tags scan -tags ${allTags}

    echo '${acceptedJSON}' | jq -r '.[]' | sort -u > accepted
    sort -u $out/*.reachable > reachable
    comm -23 reachable accepted > $out/unaccepted
    comm -13 reachable accepted > $out/stale

    cat $out/default.txt
    if [ -s $out/stale ]; then
      echo "accepted but no longer reachable, remove from nix/checks/govulncheck.nix:"
      cat $out/stale
    fi
    if [ -s $out/unaccepted ]; then
      echo "FAIL: reachable vulnerabilities not in the accepted list:"
      cat $out/unaccepted
      cat $out/all-tags.txt
      exit 1
    fi
  ''
