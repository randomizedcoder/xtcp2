# nix/checks/lint-baseline.nix
#
# The lint ratchet: compares each tier's findings against docs/lint-baseline.txt
# and goes red when a GATED tier gains one.
#
# This is the one check in the set that is GREEN at baseline, and that is its
# entire purpose. `nix flake check` has other reds, so its exit code has been 1
# for longer than any individual finding and cannot announce a new one —
# which is how PR #146 landed +14 Tier 1 / +16 Tier 2 findings unannounced. The
# tier checks are not unwatched; their red is unreadable. A check that is green
# today and red tomorrow is readable. See docs/static-analysis.md and
# TODO-SOON.md §23.
#
# Shape follows nix/checks/go-sec.nix (versions import, runCommand, copy-and-chmod
# vendoredSource, `|| { rc=$?; cat $out; exit "$rc"; }`) with one deliberate
# difference: the golangci runs are NOT here. They live in
# nix/lint-baseline-measure.nix, a package that succeeds whatever the tiers
# report, because a failing derivation produces no $out and the baseline would
# then be unregenerable exactly when it needed regenerating. That file's header
# has the full argument.
#
# GATED TIERS, one at a time, following gatedProtocols in
# proto-audit-netlink.nix and the discipline recorded at nix/checks/default.nix:
# turning everything on at once would make this check permanently red, which is
# the same as turning it off. A tier is promoted only after a pass has MEASURED
# it at 0 — Tier 0 after the Tier 0 pass, Tier 1 after the
# errcheck/misspell/contextcheck pass closed its last 26 findings. Promotion is
# earned by measurement, never asserted. A tier that still holds findings is
# measured, printed and diffed, but does not fail the build until the phase that
# empties it promotes it.
#
# Which tiers are gated TODAY is not written here. It is the gatedTiers argument
# below, set at the call site in nix/checks/default.nix, where the decision is
# reviewable alongside every other check's. This comment said "Tier 0 only" and
# went stale on the commit that promoted Tier 1, which is the argument for
# keeping the policy here and the list in exactly one place.
#
# Exit codes come from tools/lint-baseline and each means something different:
# 1 = a gated tier gained a finding, 2 = the run is unusable and fails CLOSED
# (an unparseable baseline, or a tier that is gated but was never measured —
# an unmeasured tier would otherwise read as "every finding removed", which
# passes), 3 = a tier's own golangci exit code was above 1, so the run was a
# timeout or a crash and this check refuses to certify it. The per-tier .exit
# files the measurement records exist for that third case alone.
#
{
  pkgs,
  vendoredSource,
  lintBaselineMeasure,
  # Tiers whose ADDED findings fail the build. Deliberately a parameter rather
  # than a constant in the script so promoting a tier is a one-word diff at the
  # call site in nix/checks/default.nix, where the gating decision is reviewable
  # alongside every other check's.
  gatedTiers ? [ "tier0" ],
}:

let
  versions = import ../versions.nix { inherit pkgs; };

  gated = pkgs.lib.concatStringsSep "," gatedTiers;

  # Both lists are built here rather than interpolated as a multi-line ${...}
  # inside the '' '' body: a multi-line interpolation makes nixfmt reflow the
  # whole shell body.
  #
  # The names come FROM the measurement rather than being restated here. A second
  # hardcoded copy would drift in the one direction nothing catches: passing an
  # unknown tier is an error in tools/lint-baseline, but a tier the measurement
  # produces and this check never passes is simply never compared, and silently
  # not gating a tier is the same as turning the ratchet off for it.
  inherit (lintBaselineMeasure) tierNames;
  findingsArgs = pkgs.lib.concatMapStringsSep " " (
    t: "-findings ${t}=${lintBaselineMeasure}/${t}.json"
  ) tierNames;
  # The exit code is read from the file at run time, not interpolated, so the
  # value this check acts on is the one the measurement actually recorded.
  exitArgs = pkgs.lib.concatMapStringsSep " " (
    t: "-exit ${t}=\"$(cat ${lintBaselineMeasure}/${t}.exit)\""
  ) tierNames;
in
pkgs.runCommand "xtcp2-lint-baseline"
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
    # -root is a no-op as things stand, and is passed deliberately anyway.
    # golangci-lint reports paths relative to its run directory, which is what
    # the measurement's JSON contains, so there is nothing to relativize. It
    # earns its place as a tripwire: if a config change ever made golangci emit
    # ABSOLUTE paths, every key would embed the measurement's build directory
    # and the whole baseline would read as changed. Note the limit honestly —
    # the absolute paths would be rooted at the MEASUREMENT's build directory,
    # which this derivation cannot name, so `.` would not strip them and this
    # would need revisiting rather than silently working.
    go run ./tools/lint-baseline \
      -baseline docs/lint-baseline.txt \
      -gated ${gated} \
      -root . \
      ${findingsArgs} \
      ${exitArgs} \
      > $out 2>&1 || {
      rc=$?
      cat $out
      exit "$rc"
    }
  ''
