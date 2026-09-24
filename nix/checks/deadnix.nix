# nix/checks/deadnix.nix
#
# `deadnix` over every *.nix in the repo — unused lambda arguments, unused
# `let` bindings, dead `with` scopes.
#
# Why this exists: until 2026-09-23 nothing linted the Nix tree at all, which
# is how nix/containers/oci-xtcp2.nix sat unreferenced for months without
# anyone noticing. The Go side has four tiers of linting; the Nix side that
# *drives* those tiers had none.
#
# Not auto-fixable in CI on purpose. `deadnix --edit` rewrites the lambda
# pattern but not the call sites, so dropping an unused `lib` from a pattern
# that has no `...` turns every `import ./foo.nix { inherit pkgs lib …; }`
# into an "unexpected argument lib" eval error. Fix findings by hand, or mark
# a genuinely-required-but-unused argument with a leading underscore
# (`_prev` in nix/overlays.nix) — deadnix treats that as intentional.
#
# Same exclusion set as nix-fmt.nix. ./build/ matters: the vendored
# not.docker-compose.nix lives there and is not ours to lint.
#
{
  pkgs,
  src,
}:

let
  versions = import ../versions.nix { inherit pkgs; };
in
pkgs.runCommand "xtcp2-deadnix"
  {
    nativeBuildInputs = [
      versions.deadnix
      pkgs.findutils
    ];
    inherit src;
  }
  ''
    cp -r $src/. ./xtcp2 && chmod -R +w ./xtcp2
    cd ./xtcp2
    mapfile -t files < <(find . -type f -name '*.nix' \
      -not -path './vendor/*' -not -path './.git/*' \
      -not -path './build/*' | sort)
    # --fail turns findings into a non-zero exit; without it deadnix reports
    # and still succeeds, which would make this check permanently green.
    if ! deadnix --fail "''${files[@]}"; then
      echo "deadnix: dead Nix code found — remove the binding, or rename it" >&2
      echo "         with a leading underscore if it must stay in the pattern." >&2
      echo "         Removing a lambda argument also means removing it from" >&2
      echo "         every 'inherit' at the call sites; check 'nix flake show'" >&2
      echo "         still evaluates afterwards." >&2
      exit 1
    fi
    echo "deadnix: no dead code in ''${#files[@]} *.nix file(s)" > $out
  ''
