# nix/devshell.nix
#
# Developer environment. `nix develop` lands here.
#
# Goals:
#   - Every contributor tool already on PATH (Go, buf, golangci-lint, gosec,
#     qemu, expect, etc.)
#   - Helper functions (build, regen-protos, lint-quick, lint, lint-comprehensive,
#     lint-fix, lint-new, vm-up) discoverable via `xtcp2-help` in the shell.
#   - No magic env vars — keep the shell predictable.
#
{ pkgs }:

let
  versions = import ./versions.nix { inherit pkgs; };
  packages = import ./packages.nix { inherit pkgs; };
  # Same generator as `nix run .#regen-protos` — a single offline, nix-pinned
  # implementation so the dev-shell helper and the flake app can't drift.
  regenProtos = import ./protos/buf-generate.nix { inherit pkgs; };
in
pkgs.mkShell {
  name = "xtcp2-dev";

  packages = packages.allDevPackages;

  shellHook = ''
        export CGO_ENABLED=0

        xtcp2-help() {
          cat <<'EOF'

    xtcp2 dev shell
    ===============
    Build:
      nix build .#xtcp2                       Build the main binary
      nix build .#xtcp2-all                   Build every cmd/* binary
      nix build .#oci-xtcp2                   Build the scratch OCI image

    Protos:
      regen-protos                            Re-run `buf generate` (needs network)
      buf lint                                Hermetic proto lint

    Static analysis (fix issues, do not ignore):
      lint-quick                              Tier 0  (~90s, pre-commit)
      lint                                    Tier 1  (~2min, CI gating)
      lint-comprehensive                      Tier 2  (~10min, nightly)
      lint-fix                                Apply auto-fixable findings
      lint-new                                Lint only the diff since HEAD~1

    Quality report (every tier + audits, aggregated):
      nix run .#quality-report                Print the latest report to stdout
      nix run .#update-quality-report         Refresh docs/quality-report.md
      nix build .#quality-report              Build the report artifact (result/)
      nix run .#lint-fix-one -- <linter>      Auto-fix one linter at a time

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
      nix build .#tests.microvm-lifecycle     Boot xtcp2 in a VM and verify

    Nix:
      nix flake check                         Tier 0+1 lint + all custom audits
      nixfmt --check **/*.nix                 Verify nix formatting

    EOF
        }

        regen-protos() {
          ${regenProtos}/bin/regen-protos
        }

        lint-quick() {
          ${versions.golangci-lint}/bin/golangci-lint run \
            --config .golangci-quick.yml --timeout 180s ./...
        }

        lint() {
          ${versions.golangci-lint}/bin/golangci-lint run \
            --config .golangci.yml --timeout 5m ./...
        }

        lint-comprehensive() {
          ${versions.golangci-lint}/bin/golangci-lint run \
            --config .golangci-comprehensive.yml --timeout 15m ./...
        }

        lint-fix() {
          ${versions.golangci-lint}/bin/golangci-lint run \
            --config .golangci.yml --fix ./...
        }

        lint-new() {
          ${versions.golangci-lint}/bin/golangci-lint run \
            --config .golangci.yml --new-from-rev=HEAD~1 ./...
        }

        xtcp2-help
  '';
}
