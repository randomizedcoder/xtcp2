# nix/lint-baseline-measure.nix
#
# Runs all three golangci-lint tiers for their JSON finding lists and, crucially,
# SUCCEEDS whatever they report. The output feeds both checks.lint-baseline and
# `nix run .#update-lint-baseline`.
#
# Why this is a separate derivation and not part of the check:
#
#   1. A failing derivation produces no $out. If the measurement lived inside
#      checks.lint-baseline, then the moment a gated tier gained a finding there
#      would be no artifact to regenerate the baseline from — exactly when you
#      need one. Splitting it means the expensive half always produces output and
#      the cheap half is the thing allowed to go red.
#   2. It cannot reuse checks.golangci-lint{,-quick,-comprehensive}. Those write
#      a text log, not JSON, and they fail on findings; Tier 1 and Tier 2 are red
#      today, so a derivation depending on them could not build at all.
#   3. Nix caches it, so the check and the updater share one ~13-minute run
#      rather than taking one each.
#
# COST, stated plainly because it is not visible from the one-line import: this
# re-runs every tier that `nix flake check` already runs, so it roughly doubles
# the lint wall time of a cold check. The alternative is teaching the three tier
# checks to emit JSON and keeping them green, which is what Phases 3-5 of
# docs/static-analysis.md are for; until then the duplication buys a tier whose
# red is readable, and that is the whole point.
#
# No --timeout on any invocation. Each config sets its own `run.timeout` and a
# CLI --timeout silently OVERRIDES it — the bug that let the quality report
# publish a timed-out Tier 0 as "0 issues." (nix/lint-tiers.nix says the same).
# Instead each tier's own exit code is recorded to $out/<tier>.exit, and
# tools/lint-baseline refuses to ratchet when one is above 1. That is the ONLY
# protection against baselining a timeout as a clean tree, so the .exit files are
# load-bearing output, not diagnostics.
#
{
  pkgs,
  vendoredSource,
}:

let
  versions = import ./versions.nix { inherit pkgs; };

  # tier name -> config file. The names match the `tier` type in
  # tools/lint-baseline/main.go; the check passes them straight through as
  # -findings <tier>=<path>, so a rename here is a rename there.
  tiers = {
    tier0 = ".golangci-quick.yml";
    tier1 = ".golangci.yml";
    tier2 = ".golangci-comprehensive.yml";
  };

  # Pre-computed as a flat string in this `let` rather than interpolated as a
  # multi-line ${...} inside the '' '' body below: nixfmt reflows an entire
  # shell body around a multi-line interpolation, which has cost this repo a
  # 200-line reindent before now.
  runTiers = pkgs.lib.concatStringsSep "\n" (
    pkgs.lib.mapAttrsToList (name: config: ''
      echo "==> ${name} (${config})"
      rc=0
      golangci-lint run --config ${config} --output.json.path "$out/${name}.json" \
        > "$out/${name}.log" 2>&1 || rc=$?
      echo "$rc" > "$out/${name}.exit"
      echo "    exit $rc"
    '') tiers
  );
in
pkgs.runCommand "xtcp2-lint-baseline-measure"
  {
    nativeBuildInputs = [
      versions.go
      versions.golangci-lint
    ];
    inherit vendoredSource;
    # The tier names this derivation actually measured, so a consumer can iterate
    # them instead of restating them. checks/lint-baseline.nix held a second
    # hardcoded copy of this list, and the failure mode was silent in the one
    # direction that matters: tools/lint-baseline rejects an UNKNOWN tier loudly,
    # but a tier the check simply never passes is never compared and nobody is
    # told. passthru is a Nix-level attribute and not an input to the build, so
    # this costs no rebuild.
    passthru.tierNames = builtins.attrNames tiers;
  }
  ''
    mkdir -p $out
    cp -r $vendoredSource ./xtcp2 && chmod -R +w ./xtcp2
    cd ./xtcp2
    export HOME=$(mktemp -d)
    export GOPATH=$HOME/go
    export GOMODCACHE=$HOME/go/pkg/mod
    export GOCACHE=$HOME/go-build
    export GOPROXY=off
    export CGO_ENABLED=0
    export GOFLAGS=-mod=vendor
    ${runTiers}
    # Absent JSON is a hard failure here rather than something for the consumer
    # to interpret: it means the invocation changed shape (a renamed flag, a
    # config that no longer writes the file), and a consumer that tolerated it
    # would read "no findings" off a tier that never ran.
    for f in $out/*.json; do
      test -s "$f" || { echo "lint-baseline-measure: $f is empty or missing" >&2; exit 1; }
    done
  ''
