# nix/lib/govulndb.nix
#
# An offline Go vulnerability database for govulncheck's `-db file://`, built
# from a pinned commit of github.com/golang/vulndb. vuln.go.dev serves the same
# OSV entries, but from mutable URLs a fixed-output fetch cannot pin; the repo
# carries them at data/osv, one GO-YYYY-NNNN.json each, so a commit is a
# reproducible snapshot.
#
# govulncheck reads the v1 layout (https://go.dev/security/vuln/database):
#   index/db.json       {"modified": the snapshot's date}
#   index/modules.json  [{path, vulns: [{id, modified}]}], one per module
#   index/vulns.json    [{id, modified, aliases}]
#   ID/<id>.json        the OSV entry
# modules.json leaves out each vuln's optional "fixed": the client uses it only
# to skip fetching entries already fixed in the scanned version, so without it
# every entry for a module in the build is read and matched in full.
#
# Refresh: move `rev` to golang/vulndb's current master and update `hash`
# (`nix build .#checks.x86_64-linux.govulncheck` prints the new one). `nix run
# .#govulncheck-fresh` checks against the live database in between.
{ pkgs }:

let
  # The commit's date: the repo's entries carry zero timestamps (vuln.go.dev
  # stamps them when it publishes), so the index says when the snapshot is from.
  snapshot = "2026-11-11T00:00:00Z";
  vulndb = pkgs.fetchFromGitHub {
    owner = "golang";
    repo = "vulndb";
    # master on 2026-11-11.
    rev = "e91d8491530fd8782760dd3916cd2708182b9f10";
    hash = "sha256-QjH81HGQZ8yR/kYtK6pXtReRzc+uloTDSB90qxhFSck=";
    sparseCheckout = [ "data/osv" ];
  };
in
pkgs.runCommand "go-vulndb"
  {
    nativeBuildInputs = [ pkgs.jq ];
    passthru = { inherit vulndb; };
  }
  ''
    mkdir -p $out/ID $out/index
    cp ${vulndb}/data/osv/GO-*.json $out/ID/
    count=$(ls $out/ID | wc -l)
    if [ "$count" -lt 1000 ]; then
      echo "go-vulndb: only $count entries in data/osv; the layout changed?" >&2
      exit 1
    fi
    jq -cn --arg m ${snapshot} '{modified: $m}' > $out/index/db.json
    jq -cs 'map({id, modified, aliases: (.aliases // [])})' $out/ID/*.json > $out/index/vulns.json
    jq -cs '
      [ .[] as $v | ($v.affected // [])[] | {path: .package.name, id: $v.id, modified: $v.modified} ]
      | group_by(.path)
      | map({path: .[0].path, vulns: (map({id, modified}) | unique_by(.id))})
    ' $out/ID/*.json > $out/index/modules.json
    echo "go-vulndb: $count entries, $(jq length $out/index/modules.json) modules, modified $(jq -r .modified $out/index/db.json)"
  ''
