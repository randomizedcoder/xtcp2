# nix/lint-tiers.nix
#
# The five golangci-lint tier helpers, as writeShellApplication packages.
#
# These used to be bash functions defined inline in nix/devshell.nix's
# shellHook. Functions in a shellHook are never shellcheck'd — writeShellApplication's
# build-time shellcheck is the only shell linting in this repo — and they were
# reachable only from `nix develop`, so there was no way to run a tier from a
# flake app without re-typing the command and letting the two copies drift.
# Defining them once here and consuming the same derivations from both places
# is the pattern nix/devshell.nix already uses for regen-protos.
#
# Naming: these are named for the command the user types (`lint-quick`, not
# `xtcp2-lint-quick`). That is the sanctioned exception to the xtcp2-<kebab>
# convention, the same one `regen-protos` and `tcp-stress-entrypoint` take —
# the name IS the user-facing command.
#
# No --timeout on any invocation. Each tier config sets its own `run.timeout`
# (.golangci-quick.yml 180s, .golangci.yml 5m, .golangci-comprehensive.yml
# 15m) and a CLI --timeout silently OVERRIDES it. That is exactly how the
# quality report ended up publishing a timed-out Tier 0 as "0 issues" — see
# TODO-SOON.md §5.
#
{ pkgs }:

let
  versions = import ./versions.nix { inherit pkgs; };

  # Every tier passes a relative --config path and `./...`, so all five are
  # cwd-sensitive. As bash functions they inherited the shell's cwd and failed
  # confusingly when the user had cd'd into a subdirectory; as PATH binaries
  # they can be invoked from anywhere, which makes the guard load-bearing
  # rather than decorative. Same guard, same exit 2, as
  # lintFixOne (nix/default.nix).
  rootGuard = name: ''
    if [ ! -f flake.nix ]; then
      echo "${name}: must be run from the xtcp2 repo root" >&2
      exit 2
    fi
  '';

  # `exec` so the tier's exit status is the wrapper's exit status with no
  # intervening shell — a pre-commit hook or CI step reads golangci-lint's
  # code directly (1 = findings, >1 = the tool itself failed; see
  # statusLabel in tools/quality-report/main.go).
  mkTier =
    {
      name,
      config,
      extraArgs ? [ ],
      extraInputs ? [ ],
    }:
    pkgs.writeShellApplication {
      inherit name;
      runtimeInputs = [ versions.golangci-lint ] ++ extraInputs;
      text = ''
        ${rootGuard name}
        exec golangci-lint run --config ${config} ${pkgs.lib.concatStringsSep " " extraArgs} ./...
      '';
    };
in
rec {
  # Tier 0 — ~90 s, pre-commit.
  lint-quick = mkTier {
    name = "lint-quick";
    config = ".golangci-quick.yml";
  };

  # Tier 1 — ~2 min, the gating tier (nix/checks/golangci-lint.nix).
  lint = mkTier {
    name = "lint";
    config = ".golangci.yml";
  };

  # Tier 2 — ~10 min, nightly.
  lint-comprehensive = mkTier {
    name = "lint-comprehensive";
    config = ".golangci-comprehensive.yml";
  };

  # Tier 1 with --fix. Writes to the working tree, so it is deliberately the
  # gating config and not the comprehensive one: auto-fixes land only for
  # findings CI would have blocked on anyway.
  lint-fix = mkTier {
    name = "lint-fix";
    config = ".golangci.yml";
    extraArgs = [ "--fix" ];
  };

  # Tier 1 restricted to the diff since HEAD~1. golangci-lint shells out to
  # git to resolve --new-from-rev, hence git in runtimeInputs — as a shellHook
  # function it borrowed the dev shell's git, which a PATH binary cannot.
  lint-new = mkTier {
    name = "lint-new";
    config = ".golangci.yml";
    extraArgs = [ "--new-from-rev=HEAD~1" ];
    extraInputs = [ pkgs.git ];
  };

  # Convenience list for consumers that want all five (the dev shell's
  # `packages`, so a new tier added above shows up in `nix develop` without a
  # second edit).
  all = [
    lint-quick
    lint
    lint-comprehensive
    lint-fix
    lint-new
  ];
}
