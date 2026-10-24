# The repository without the files no Go build or guest test artifact reads:
# Markdown and Nix expressions outside testdata/ (bar the one document a test
# reads), the flake lock, docs/, editor and CI directories, and result links. Derivations built from it,
# and the vm-* guest checks that boot them, stay cached across a docs- or
# Nix-only change.
#
# Only the binaries, go-link-monitor and the test artifacts the guests run
# use it. Repo-wide checks (TestRepoIsClean, the docs and Nix policy checks)
# keep the full tree, since what they audit is the whole repository.
{ lib, src }:
let
  root = toString src;
  # pkg/linkmonitor's TestRDMACounterCatalog reads the metric reference.
  readByTests = [ "cmd/go-link-monitor/METRICS.md" ];
  # Top-level paths no build reads; .git is the worktree's gitdir pointer.
  topLevel = [
    "flake.lock"
    "docs"
    ".git"
    ".github"
    ".vscode"
    ".ruff_cache"
  ];
  keep =
    path: type:
    let
      rel = lib.removePrefix "${root}/" (toString path);
      base = baseNameOf rel;
      inTestdata = lib.hasInfix "/testdata/" "/${rel}";
    in
    !(
      builtins.elem rel topLevel
      || (!(lib.hasInfix "/" rel) && lib.hasPrefix "result" base)
      || (
        type != "directory"
        && !inTestdata
        && !(builtins.elem rel readByTests)
        && (lib.hasSuffix ".md" base || lib.hasSuffix ".nix" base)
      )
    );
in
lib.cleanSourceWith {
  inherit src;
  filter = keep;
  name = "xtcp2-go-source";
}
