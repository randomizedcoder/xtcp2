# nix/fuzz-all.nix
#
# Every Go fuzz target, found by `go test -list` (FP-7), in two forms:
#
#   script  `fuzz-all` on PATH: the dev shell and `nix run .#fuzz-all` both run
#           it from a checkout, with FUZZTIME etc. set by the caller. Crashers
#           land in the checkout's testdata/fuzz/, ready to commit.
#   check   the same script in the sandbox at a short budget per target, so
#           `nix flake check` fuzzes again whenever the source changes. A
#           cached derivation does not fuzz again for unchanged source; use
#           the app for long runs.
#
# Source: nix/scripts/fuzz-all.bash (the shebang is dropped because
# writeShellApplication adds its own).
{
  pkgs,
  vendoredSource ? null,
  # Per-target budget for the check. 10 s over about 50 targets is 10-16 min,
  # most of it the instrumented build of each package.
  checkFuzztime ? "10s",
}:

let
  versions = import ./versions.nix { inherit pkgs; };
  script = pkgs.writeShellApplication {
    name = "fuzz-all";
    runtimeInputs = [
      versions.go
      pkgs.coreutils
      pkgs.findutils
      pkgs.gawk
    ];
    text = pkgs.lib.removePrefix "#!/usr/bin/env bash\n" (builtins.readFile ./scripts/fuzz-all.bash);
  };
in
{
  inherit script;
  check =
    pkgs.runCommand "xtcp2-test-fuzz-all"
      {
        # go too: the builder runs `go version` itself, and only the script's
        # own wrapper puts its runtimeInputs on PATH.
        nativeBuildInputs = [
          script
          versions.go
        ];
        inherit vendoredSource;
      }
      ''
        set -euo pipefail
        cp -r "$vendoredSource" ./xtcp2
        chmod -R u+w ./xtcp2
        cd ./xtcp2
        export HOME="$TMPDIR/home" GOTOOLCHAIN=local GOFLAGS=-mod=vendor CGO_ENABLED=0
        export GOCACHE="$TMPDIR/go-cache" GOMODCACHE="$TMPDIR/go-modcache"
        mkdir -p "$HOME" "$out/logs"
        go version > "$out/go-version.txt"
        FUZZTIME=${checkFuzztime} FUZZ_LOG_DIR="$out/logs" fuzz-all > "$out/fuzz-all.log" 2>&1 || {
          rc=$?
          cat "$out/fuzz-all.log" >&2
          exit "$rc"
        }
        tail -n 1 "$out/fuzz-all.log"
      '';
}
