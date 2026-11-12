# nix/govulncheck-fresh.nix
#
# Networked half of the vulnerability check: govulncheck on the working tree
# against the live Go vulnerability database, plus `go mod verify`. The flake
# check (nix/checks/govulncheck.nix) is pure, so it only knows the pinned
# snapshot in nix/lib/govulndb.nix; this runner says what the snapshot does
# not know yet. A RUNNER, not a check, for the same reason as
# check-upstream-pins: the check sandbox has no network.
#
#   nix run .#govulncheck-fresh             # report, exit 1 on any finding
#
# It reports every reachable finding, accepted or not: moving the snapshot pin
# brings them into the check, where the accepted list applies.
#
{
  pkgs,
  versions,
  allTags,
}:

pkgs.writeShellApplication {
  name = "xtcp2-govulncheck-fresh";
  runtimeInputs = [
    versions.go
    versions.govulncheck
  ];
  text = ''
    if [[ ! -f go.mod ]]; then
      echo "run from the xtcp2 repository root" >&2
      exit 2
    fi
    export GOTOOLCHAIN=local
    rc=0
    echo "==> go mod verify"
    go mod verify || rc=1
    echo "==> govulncheck (no tags)"
    govulncheck ./... || rc=1
    echo "==> govulncheck (-tags ${allTags})"
    govulncheck -tags ${allTags} ./... || rc=1
    exit "''${rc}"
  '';
}
