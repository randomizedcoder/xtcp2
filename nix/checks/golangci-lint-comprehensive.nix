# nix/checks/golangci-lint-comprehensive.nix
#
# Tier 2: Comprehensive lint. Tier 1 + exhaustive, prealloc, funlen, goconst,
# dupl, unconvert, nakedret. Target wall time: ~10 minutes.
#
# gocyclo and misspell used to be in that list and are not any more: both were
# promoted into Tier 1 once their findings reached 0, so they are inherited here
# rather than unique to here. forbidigo went the other way on 2026-10-06 — it
# was enabled in Tier 1 only, which broke the nesting the lint-baseline ratchet
# relies on, and was added to this tier's config.
#
# This header used to say "Not part of default `nix flake check` — invoke
# explicitly". That was false, and it was the last surviving copy of a claim
# nix/checks/default.nix's own header was rewritten to refute: every attribute
# in that file is a flake-check target, this one included. A reviewer who
# believed it concluded a gocyclo regression could not have been caught by the
# one command CONTRIBUTING.md tells contributors to run, when in fact the
# command built this tier and reported the finding.
#
# Building it alone is still the useful command, because it skips the microVM
# and the per-flavor test builds. Note the system component, which the old
# version of this comment also omitted:
#   nix build .#checks.x86_64-linux.golangci-lint-comprehensive
#
{
  pkgs,
  vendoredSource,
}:

let
  versions = import ../versions.nix { inherit pkgs; };
in
pkgs.runCommand "xtcp2-golangci-lint-comprehensive"
  {
    nativeBuildInputs = [
      versions.go
      versions.golangci-lint
    ];
    inherit vendoredSource;
  }
  ''
    cp -r $vendoredSource ./xtcp2 && chmod -R +w ./xtcp2
    cd ./xtcp2
    export HOME=$(mktemp -d)
    export CGO_ENABLED=0
    export GOFLAGS=-mod=vendor
    # Timeout comes from .golangci-comprehensive.yml (`run.timeout`); a CLI
    # --timeout would override it silently. See golangci-lint-quick.nix.
    golangci-lint run --config .golangci-comprehensive.yml ./... > $out 2>&1 \
      || (cat $out && exit 1)
  ''
