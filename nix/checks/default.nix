# nix/checks/default.nix
#
# Aggregates every `nix flake check` target for xtcp2.
#
# Two categories:
#   - Tier 0+1 + audits → run by default `nix flake check`
#   - Tier 2 (comprehensive) → invoke explicitly:
#       nix build .#checks.golangci-lint-comprehensive
#
{
  pkgs,
  lib,
  src,
  vendoredSource,
  binaries,
}:

let
  # Per-binary -help smoke matrix. Each cmd binary gets its own check attr so
  # CI logs name the failing binary cleanly.
  helpSmokes = import ./cli-help-smoke.nix { inherit pkgs lib binaries; };
  # Capability-check smoke matrix. Verifies xtcp2 refuses to start when
  # required Linux caps are missing AND that the diagnostic names the
  # cap + provides remediation. Sub-second per check; lighter-weight
  # alternative to the microvm-x86_64-capcheck-fail flavor.
  capChecks = import ./capability-check.nix { inherit pkgs lib binaries; };
in
{
  go-vet = import ./go-vet.nix { inherit pkgs lib vendoredSource; };
  gofmt = import ./gofmt.nix { inherit pkgs lib src; };
  nix-fmt = import ./nix-fmt.nix { inherit pkgs lib src; };
  # proto-lint: NOT in the default check set. `buf lint` reaches out to
  # buf.build for module deps (protovalidate, googleapis), which the hermetic
  # Nix sandbox blocks. Run it from `nix develop` via the `buf lint` shell
  # function instead. The file proto-lint.nix is preserved for future hermetic
  # use once buf module deps are pre-fetched as Nix sources.

  golangci-lint-quick = import ./golangci-lint-quick.nix { inherit pkgs lib vendoredSource; };
  golangci-lint = import ./golangci-lint.nix { inherit pkgs lib vendoredSource; };
  golangci-lint-comprehensive = import ./golangci-lint-comprehensive.nix {
    inherit pkgs lib vendoredSource;
  };
  go-sec = import ./go-sec.nix { inherit pkgs lib vendoredSource; };

  netlink-audit = import ./netlink-audit.nix { inherit pkgs lib vendoredSource; };
  iouring-audit = import ./iouring-audit.nix { inherit pkgs lib vendoredSource; };
  metrics-audit = import ./metrics-audit.nix { inherit pkgs lib vendoredSource; };
  proto-field-audit = import ./proto-field-audit.nix { inherit pkgs lib vendoredSource; };
}
// helpSmokes
// capChecks
