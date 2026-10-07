# nix/checks/kernel-citation-audit.nix
#
# Runs the custom Go analyzer at tools/kernel-citation-audit across the repo.
#
# This check is what makes the widened `neighbour` misspell exclusion in
# .golangci.yml and .golangci-comprehensive.yml something other than a
# suppression. Those exclusions can only match misspell's message, which is the
# same whether the word is part of net/core/neighbour.c or part of British
# prose; this audit fails unless every occurrence in the tree is directly
# preceded by a kernel path. Both halves have to be present for either to be
# defensible, so if this check is ever removed the exclusions have to go with
# it.
#
# -root . matches metrics-audit, the other audit that covers the whole tree.
# Unlike netlink-audit this one reads _test.go files, because six of the sixteen
# kernel citations are in tests.
{
  pkgs,
  vendoredSource,
}:

let
  versions = import ../versions.nix { inherit pkgs; };
in
pkgs.runCommand "xtcp2-kernel-citation-audit"
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
    go run ./tools/kernel-citation-audit -root . > $out 2>&1 || (cat $out && exit 1)
  ''
