# nix/devshell.nix
#
# Developer environment. `nix develop` lands here.
#
# Goals:
#   - Every contributor tool already on PATH (Go, buf, golangci-lint, gosec,
#     qemu, expect, etc.)
#   - Helper commands (regen-protos, lint-quick, lint, lint-comprehensive,
#     lint-fix, lint-new) discoverable via `xtcp2-help` in the shell.
#   - No magic env vars — keep the shell predictable.
#
# Those helpers used to be bash functions defined in the shellHook below. They
# are now writeShellApplication packages on `packages`, because a shellHook
# function is never shellcheck'd — writeShellApplication's build-time check is
# the only shell linting in this repo — and it is unreachable from anywhere
# but an interactive `nix develop`. The shellHook is down to one export and
# one call as a result.
#
{ pkgs }:

let
  packages = import ./packages.nix { inherit pkgs; };
  # Same generator as `nix run .#regen-protos` — a single offline, nix-pinned
  # implementation so the dev-shell helper and the flake app can't drift. The
  # derivation is already named regen-protos, so putting it on `packages` puts
  # the command on PATH directly; the shell function that used to wrap it was
  # pure indirection.
  regenProtos = import ./protos/buf-generate.nix { inherit pkgs; };
  # The five golangci-lint tiers, shared verbatim with nix/default.nix's
  # `packages` so the shell and the flake cannot drift.
  lintTiers = import ./lint-tiers.nix { inherit pkgs; };

  # Shell-entry banner. A writeShellApplication rather than a shellHook
  # function so its (admittedly trivial) bash is shellcheck'd like everything
  # else, and so `xtcp2-help` still resolves after the user runs `exec bash`.
  xtcp2Help = pkgs.writeShellApplication {
    name = "xtcp2-help";
    runtimeInputs = [ pkgs.coreutils ]; # cat
    text = ''
      cat <<'EOF'

      xtcp2 dev shell
      ===============
      Build:
        nix build .#xtcp2                       Build the main binary
        nix build .#xtcp2-all                   Build every cmd/* binary
        nix build .#oci-xtcp2                   Build the scratch OCI image

      Protos:
        regen-protos                            Re-run `buf generate` (needs network)
        buf lint                                Proto lint (fetches module deps)

      Static analysis (fix issues, do not ignore):
        lint-quick                              Tier 0  (~90s, pre-commit)
        lint                                    Tier 1  (~2min, CI gating)
        lint-comprehensive                      Tier 2  (~10min, nightly)
        lint-fix                                Apply auto-fixable findings
        lint-new                                Lint only the diff since HEAD~1
                                                All five take relative config
                                                paths, so they must run from the
                                                repo root; they exit 2 otherwise.

      Quality report (every tier + audits, aggregated):
        nix run .#quality-report                Print the latest report to stdout
        nix run .#update-quality-report         Refresh docs/quality-report.md
        nix build .#quality-report              Build the report artifact (result/)
        nix run .#lint-fix-one -- <linter>      Auto-fix one linter at a time

      Lint ratchet (the one check that is green at baseline, so its red means a
      regression landed rather than inherited debt):
        nix build .#checks.x86_64-linux.lint-baseline
                                                Diff every tier against
                                                docs/lint-baseline.txt. Fails
                                                only when a GATED tier gains a
                                                finding; Tier 1 and 2 print as
                                                advisory until a phase empties
                                                them.
        nix run .#update-lint-baseline          Regenerate docs/lint-baseline.txt.
                                                Not doable by hand: the tier
                                                configs use vendor mode and there
                                                is no committed vendor/ tree, so
                                                golangci-lint only runs inside
                                                the Nix sandbox.

      Tests:
        go test ./...                           Unit tests
        go test -ldflags=-checklinkname=0 ./pkg/xtcp/ ./cmd/xtcp2/
                                                Local workaround when the toolchain
                                                rejects giouring's syscall linkname
                                                ("invalid reference to syscall.munmap")
        go test -tags 'enrich_asn enrich_locality' ./pkg/xtcp/ ./cmd/xtcp2/
                                                The gated enrichers. Untagged runs
                                                skip enrich_{asn,locality}.go
                                                entirely, so their tests never
                                                compile without this. Combine with
                                                -ldflags=-checklinkname=0 above.
                                                Same for dest_* — see
                                                nix/tests/go-test-flavors.nix,
                                                which nix flake check runs for you.
        nix build .#test-microvm-lifecycle-x86_64
                                                Boot xtcp2 in a VM and verify.
                                                (NOT .#tests.microvm-lifecycle —
                                                flake.nix does not export `tests`,
                                                so that name has never resolved.)

      Nix:
        nix flake check                         Tier 0+1 lint + all custom audits
        nixfmt --check **/*.nix                 Verify nix formatting

      EOF
    '';
  };
in
pkgs.mkShell {
  name = "xtcp2-dev";

  packages =
    packages.allDevPackages
    ++ lintTiers.all
    ++ [
      regenProtos
      xtcp2Help
    ];

  shellHook = ''
    export CGO_ENABLED=0
    # Resolves from PATH (the xtcp2Help package), not as a shell function.
    xtcp2-help
  '';
}
