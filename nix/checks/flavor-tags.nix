# nix/checks/flavor-tags.nix
#
# Every slim (production) xtcp2 flavor must leave out the debug writers: the
# file destination (`dest_file`, full flavor only) and the netlink capture
# writer (`capture`, the dedicated xtcp2-capture attr only). The full flavor
# must carry `dest_file`, and xtcp2-capture must carry `capture`, so the check
# also proves the tags are wired at all.
#
# Reads the build tags Go records in each binary (`go version -m`), which
# survive -s -w and strip, rather than symbol names.
#
{
  pkgs,
  lib,
  binaries,
}:

let
  versions = import ../versions.nix { inherit pkgs; };

  debugTags = map (d: "dest_${d}") versions.debugDestinations ++ versions.optInOnlyTags;

  slim = lib.concatMap (
    dest:
    map (enrich: {
      name = "${dest}-${enrich}";
      bin = binaries.xtcp2OnlyByFlavor.${dest}.${enrich};
    }) (builtins.attrNames versions.enrichmentFlavors)
  ) (lib.remove "full" (builtins.attrNames versions.destinationFlavors));
in
pkgs.runCommand "xtcp2-flavor-tags"
  {
    nativeBuildInputs = [ versions.go ];
  }
  ''
    export HOME=$(mktemp -d)
    fail=0
    tags_of() {
      go version -m "$1/bin/xtcp2" | sed -n 's/^[[:space:]]*build[[:space:]]*-tags=//p'
    }
    has_tag() {
      case ",$1," in *",$2,"*) return 0 ;; esac
      return 1
    }

    ${lib.concatMapStrings (f: ''
      t=$(tags_of ${f.bin})
      for d in ${lib.concatStringsSep " " debugTags}; do
        if has_tag "$t" "$d"; then
          echo "FAIL: slim flavor ${f.name} carries debug tag $d (tags: $t)"
          fail=1
        fi
      done
    '') slim}

    t=$(tags_of ${binaries.xtcp2})
    has_tag "$t" dest_file || { echo "FAIL: full flavor lacks dest_file (tags: $t)"; fail=1; }
    has_tag "$t" capture && { echo "FAIL: full flavor carries capture (tags: $t)"; fail=1; }

    t=$(tags_of ${binaries.xtcp2-capture})
    has_tag "$t" capture || { echo "FAIL: xtcp2-capture lacks capture (tags: $t)"; fail=1; }

    [ "$fail" = 0 ] || exit 1
    echo "ok: ${toString (builtins.length slim)} slim flavors free of: ${lib.concatStringsSep " " debugTags}" > $out
  ''
