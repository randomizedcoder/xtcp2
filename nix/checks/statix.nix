# nix/checks/statix.nix
#
# `statix check` over every *.nix in the repo — Nix antipatterns
# (manual inherit, useless parens, redundant `rec`, …).
#
# Companion to nix/checks/deadnix.nix; both landed 2026-09-23 for the same
# reason (the Nix tree had no linter of any kind). deadnix finds what is
# unused, statix finds what is written the long way round.
#
# Lint scope is configured in the repo-root statix.toml, which statix picks up
# from the working directory. `repeated_keys` (W20) is disabled there, with the
# reason written out in full — see that file. Everything else is on, and
# findings are fixed rather than silenced: there are no `# statix: ignore`
# comments in this tree.
#
# statix takes its ignore list as repeated `-i` flags; `-i vendor .git build`
# is a usage error, not three exclusions. Same three paths as nix-fmt.nix.
#
{
  pkgs,
  src,
}:

let
  versions = import ../versions.nix { inherit pkgs; };
in
pkgs.runCommand "xtcp2-statix"
  {
    nativeBuildInputs = [ versions.statix ];
    inherit src;
  }
  ''
    cp -r $src/. ./xtcp2 && chmod -R +w ./xtcp2
    cd ./xtcp2
    # `statix check` exits non-zero when it reports anything, so the config's
    # `disabled` list is what decides pass/fail — no grep-filtering here.
    if ! statix check -i vendor -i .git -i build -o errfmt .; then
      echo "statix: Nix antipatterns found — rewrite the flagged expressions." >&2
      echo "        'statix fix <file>' applies the mechanical ones; re-run" >&2
      echo "        nixfmt afterwards. Do NOT add '# statix: ignore'; if a" >&2
      echo "        whole lint is wrong for this repo, disable it in" >&2
      echo "        statix.toml with a written reason." >&2
      exit 1
    fi
    echo "statix: no antipatterns" > $out
  ''
