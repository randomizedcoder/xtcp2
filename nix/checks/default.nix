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
  xdp2,
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
  go-vet = import ./go-vet.nix { inherit pkgs vendoredSource; };
  gofmt = import ./gofmt.nix { inherit pkgs src; };
  nix-fmt = import ./nix-fmt.nix { inherit pkgs src; };
  # Nix static analysis, gating since 2026-09-23. nix-fmt only checks layout;
  # these two check content — deadnix for unused bindings/arguments, statix for
  # antipatterns. Lint scope for statix lives in the repo-root statix.toml.
  deadnix = import ./deadnix.nix { inherit pkgs src; };
  statix = import ./statix.nix { inherit pkgs src; };
  # proto-lint: NOT in the default check set. `buf lint` reaches out to
  # buf.build for module deps (protovalidate, googleapis), which the hermetic
  # Nix sandbox blocks. Run `buf lint` directly from `nix develop`, where buf
  # is on PATH via versions.buf (nix/packages.nix) — there is no shell function
  # wrapping it, despite what this comment used to claim. The file
  # proto-lint.nix is preserved for future hermetic use once buf module deps
  # are pre-fetched as Nix sources.

  golangci-lint-quick = import ./golangci-lint-quick.nix { inherit pkgs vendoredSource; };
  golangci-lint = import ./golangci-lint.nix { inherit pkgs vendoredSource; };
  golangci-lint-comprehensive = import ./golangci-lint-comprehensive.nix {
    inherit pkgs vendoredSource;
  };
  go-sec = import ./go-sec.nix { inherit pkgs vendoredSource; };

  netlink-audit = import ./netlink-audit.nix { inherit pkgs vendoredSource; };
  iouring-audit = import ./iouring-audit.nix { inherit pkgs vendoredSource; };
  metrics-audit = import ./metrics-audit.nix { inherit pkgs vendoredSource; };
  proto-field-audit = import ./proto-field-audit.nix { inherit pkgs vendoredSource; };

  # The netlink layout oracle: are xtcp2's Go structs the shape the kernel
  # actually sends? Every other netlink check starts *from* the struct and so
  # cannot ask that. Advisory in Phase 0 (always exits 0), gating from Phase 2
  # via `gating = true`.
  #
  # COST, because it is not obvious from the one line: this pulls xdp2's
  # proto-audit closure — a Rust build plus a large pinned source set (kernel
  # tarball, DPDK, nDPI, suricata, tshark, a scapy python). On a cold cache it
  # dominates `nix flake check` wall time by a wide margin. If that becomes a
  # problem, move this one attribute out of the returned set and into
  # `packages` in nix/default.nix — the precedent is proto-lint above, which is
  # kept out of the default set for its own infrastructural reason. Nothing
  # else needs to change, because advisory mode never gates anyway.
  proto-audit-netlink = import ./proto-audit-netlink.nix {
    inherit
      pkgs
      lib
      src
      xdp2
      ;
    gating = false;
  };

  # Keeps nix/upstream-pins.json honest: asserts the revs it records are still
  # the revs this build uses. Cheap — jq over flake.lock and one sed over the
  # xdp2 source that is already a flake input. It deliberately does NOT try to
  # contact GitHub; `nix run .#check-upstream-pins` does that.
  upstream-pins = import ./upstream-pins.nix {
    inherit
      pkgs
      src
      xdp2
      ;
  };
}
// helpSmokes
// capChecks
