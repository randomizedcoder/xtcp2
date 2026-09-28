# nix/check-upstream-pins.nix
#
# Networked half of the upstream-pin guard: asks the remotes where `main`
# actually is, and reports how far behind each pin has fallen.
#
# This is a RUNNER, not a `nix flake check`, and deliberately so. A check
# derivation runs sandboxed with no network, so it cannot ask GitHub anything;
# see the long comment in nix/checks/upstream-pins.nix. Freshness of an
# external ref is inherently a network question, so it lives here where the
# network exists. The same split as `capture-netlink-fixtures`, which is a
# runner because a check cannot write fixtures into the working tree.
#
#   nix run .#check-upstream-pins             # warn on drift, exit 0
#   nix run .#check-upstream-pins -- --strict # exit 1 on drift, for CI
#
# `git ls-remote` on a public repo needs no credentials, so this does not touch
# the gh profiles or any token.
#
{
  pkgs,
}:

pkgs.writeShellApplication {
  name = "xtcp2-check-upstream-pins";
  runtimeInputs = with pkgs; [
    coreutils
    git
    jq
  ];
  text = ''
        set -eu

        STRICT=0
        while [ $# -gt 0 ]; do
          case "$1" in
            --strict) STRICT=1; shift ;;
            -h|--help)
              cat <<'USAGE'
    usage: check-upstream-pins [--strict]

    Compares every pin in nix/upstream-pins.json against the current tip of its
    upstream branch, and reports drift.

      (default)  report drift, exit 0 — a warning, not a gate
      --strict   exit 1 if any pin not marked known_stale has drifted

    Pins marked "known_stale": true are reported but never fail, even under
    --strict. Those are expected to be behind; the point of printing them is that
    the distance should be a known number rather than a surprise.

    Only the manifest's `pins` key is checked here. Its `package_pins` key holds
    nixpkgs package versions rather than branch tips, so there is no remote ref
    to ask about and nothing this runner can add; those are verified at eval time
    by the hermetic check instead, which for once is the stronger half.

    The companion hermetic check,
    `nix build .#checks.x86_64-linux.upstream-pins`, verifies the manifest still
    matches the pins actually in use. It cannot check upstream freshness, because
    the nix sandbox has no network.
    USAGE
              exit 0
              ;;
            *) echo "unknown arg: $1" >&2; exit 2 ;;
          esac
        done

        if [ ! -f flake.nix ] || [ ! -f nix/upstream-pins.json ]; then
          echo "check-upstream-pins: must be run from the xtcp2 repo root" >&2
          exit 2
        fi

        manifest=nix/upstream-pins.json
        drifted=0
        gating_drift=0

        # Iterate the manifest rather than hardcoding pin names, so adding a pin
        # to the JSON is the only edit needed to have it checked here too.
        for name in $(jq -r '.pins | keys[]' "$manifest"); do
          repo=$(jq -r --arg n "$name" '.pins[$n].repo' "$manifest")
          branch=$(jq -r --arg n "$name" '.pins[$n].branch' "$manifest")
          pinned=$(jq -r --arg n "$name" '.pins[$n].rev' "$manifest")
          role=$(jq -r --arg n "$name" '.pins[$n].role' "$manifest")
          stale=$(jq -r --arg n "$name" '.pins[$n].known_stale' "$manifest")

          echo "=== $name ($repo @ $branch) ==="
          echo "  role   : $role"
          echo "  pinned : $pinned"

          if ! tip=$(git ls-remote "https://github.com/$repo" "refs/heads/$branch" 2>/dev/null \
                       | awk '{print $1}' | head -n1); then
            tip=""
          fi

          if [ -z "$tip" ]; then
            echo "  remote : UNREACHABLE (no network, or repo/branch renamed)"
            echo "  result : cannot determine drift"
            echo
            continue
          fi

          echo "  remote : $tip"

          if [ "$tip" = "$pinned" ]; then
            echo "  result : up to date"
            echo
            continue
          fi

          drifted=$((drifted + 1))

          # Commit distance is only available when the objects are local. For the
          # xtcp2 pin that is the common case (this IS xtcp2); for xdp2 it usually
          # is not, so report the rev change without inventing a count.
          behind=""
          if git cat-file -e "$pinned^{commit}" 2>/dev/null \
             && git cat-file -e "$tip^{commit}" 2>/dev/null; then
            behind=$(git rev-list --count "$pinned..$tip" 2>/dev/null || echo "")
            pinned_date=$(git log -1 --format=%ci "$pinned" 2>/dev/null || echo "")
            [ -n "$pinned_date" ] && echo "  pinned dated: $pinned_date"
          fi

          if [ -n "$behind" ]; then
            echo "  result : DRIFTED — $behind commit(s) behind $branch"
          else
            echo "  result : DRIFTED — remote tip differs from the pin"
            echo "           (commit distance needs the objects locally; clone"
            echo "            $repo and re-run to get a count)"
          fi

          if [ "$stale" = "true" ]; then
            note=$(jq -r --arg n "$name" '.pins[$n].stale_note // ""' "$manifest")
            echo "  NOTE   : marked known_stale, so this does not gate."
            [ -n "$note" ] && echo "           $note"
          else
            gating_drift=$((gating_drift + 1))
            echo "  ACTION : bump the pin in flake.nix, run \`nix flake lock\`, then"
            echo "           update .pins.$name.rev in $manifest"
          fi
          echo
        done

        # Named rather than silently skipped: a reader who added a package pin
        # and then saw this runner report nothing about it would reasonably
        # conclude the manifest was being ignored.
        # `// {}` rather than `keys[]?`: the postfix `?` swallows an error from
        # iterating the result, but `null | keys` fails before it gets there, so
        # a manifest with no package_pins key would print a jq error.
        pkg_pins=$(jq -r '(.package_pins // {}) | keys[]' "$manifest" | tr '\n' ' ')
        if [ -n "$pkg_pins" ]; then
          echo "=== package pins (not branch tips; not checked here) ==="
          echo "  $pkg_pins"
          echo "  Verified at eval time by .#checks.<system>.upstream-pins."
          echo
        fi

        echo "=== summary ==="
        echo "pins drifted            : $drifted"
        echo "of those, gating        : $gating_drift"

        if [ "$gating_drift" -gt 0 ]; then
          echo
          echo "A drifted pin is not automatically a problem — it means the oracle is"
          echo "running against an older upstream than exists. Decide deliberately"
          echo "rather than bumping reflexively: a proto-audit bump can change what"
          echo "the layout oracle reports, which is exactly the kind of change that"
          echo "should land on its own rather than inside unrelated work."
          if [ "$STRICT" = "1" ]; then
            echo
            echo "FAIL: --strict and $gating_drift gating pin(s) drifted." >&2
            exit 1
          fi
        fi

        exit 0
  '';
}
