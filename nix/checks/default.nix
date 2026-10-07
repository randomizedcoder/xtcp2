# nix/checks/default.nix
#
# Aggregates every `nix flake check` target for xtcp2.
#
# EVERY attribute below is a `nix flake check` target, Tier 2 included.
#
# This comment used to claim Tier 2 was excluded and had to be invoked
# explicitly. It was false, and the falsehood was load-bearing: a reviewer who
# believed it concluded a gocyclo regression could not have been caught by the
# one command CONTRIBUTING.md tells contributors to run, when in fact the
# command built the tier and reported the finding. `nix flake check` still has
# other reds, so an exit code that is already 1 cannot announce a new finding -
# that, not the check set, is why Tier 2 findings slip through. (Which reds, and
# how many, is deliberately not recorded here: it said "eight checks at
# baseline" and went stale the moment a phase fixed one.) See the Tier 2 section
# of TODO-SOON.md for the current set, and for how to read the tier - diff the
# finding list; the count and the exit status are both uninformative.
#
# `nix build .#checks.x86_64-linux.golangci-lint-comprehensive` is still the
# useful command, because it builds that one tier without the microVM and the
# per-flavor test builds. It is a faster path to the same result, not the only
# path to it.
#
{
  pkgs,
  lib,
  src,
  vendoredSource,
  binaries,
  xdp2,
  lintBaselineMeasure,
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
  # The expect library behind the netlink capture driver. Hermetic despite
  # being microVM plumbing, because it can be driven against a local pty
  # shell; see vm-lib-exp.nix's header for why that is a faithful stand-in and
  # what it consequently does not cover.
  vm-lib-exp = import ./vm-lib-exp.nix { inherit pkgs src; };
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

  # The lint ratchet, and the only check here that is GREEN at baseline. The
  # three tier checks above report a tier's whole finding list, which has been
  # non-empty for long enough that their red carries no information; this one
  # compares against docs/lint-baseline.txt and goes red only on an ADDITION.
  #
  # gatedTiers grew one tier at a time, and only after a phase emptied that
  # tier — the same one-at-a-time discipline as gatedProtocols below. Gating a
  # tier that still holds findings would make this check permanently red, which
  # is the same as turning it off.
  #
  # All three are gated as of 2026-10-06, each earned by measurement and in that
  # order. Tier 1: the errcheck/misspell/contextcheck pass closed its last 26
  # findings, `nix run .#update-lint-baseline` measured it at 0 and removed its
  # 19 baseline lines, and only then was it listed. The setRuleAttr split then
  # took gocyclo's last finding from 48 to 6 and gocyclo was promoted INTO
  # Tier 1, with Tier 1 re-measured at 0 WITH the new linter running in it.
  # Tier 2 last: the cmd/xtcp2 flag split closed its three funlen findings and
  # forbidigo was added to its config so the tiers genuinely nest.
  #
  # So docs/lint-baseline.txt now holds zero finding lines, and this check goes
  # red on an addition in any tier. That is the end state this was built for: a
  # green check whose red means exactly one thing.
  #
  # Gating Tier 2 also gates prealloc, dupl, goconst, nakedret, exhaustive and
  # unconvert, and CONTRIBUTING.md argues prealloc makes a poor GATE because a
  # single `continue` anywhere in a file silences every prealloc hint in that
  # file. That objection is about promoting prealloc into Tier 1, where it would
  # block on its own finding list. The ratchet is a different instrument: it
  # fails only on an ADDITION and never on a removal, so prealloc's weakness
  # makes findings vanish — never a failure — and reappear later as legitimately
  # new ones. The objection does not transfer, which is worth saying out loud
  # next to the line that appears to contradict it.
  lint-baseline = import ./lint-baseline.nix {
    inherit pkgs vendoredSource lintBaselineMeasure;
    gatedTiers = [
      "tier0"
      "tier1"
      "tier2"
    ];
  };

  netlink-audit = import ./netlink-audit.nix { inherit pkgs vendoredSource; };
  iouring-audit = import ./iouring-audit.nix { inherit pkgs vendoredSource; };
  metrics-audit = import ./metrics-audit.nix { inherit pkgs vendoredSource; };
  proto-field-audit = import ./proto-field-audit.nix { inherit pkgs vendoredSource; };

  # Paired with the widened `neighbour` misspell exclusions in .golangci.yml and
  # .golangci-comprehensive.yml: those stop misspell reporting sixteen kernel
  # citations, and this is what still refuses British prose in the same files.
  # Removing this check means removing those exclusions.
  kernel-citation-audit = import ./kernel-citation-audit.nix { inherit pkgs vendoredSource; };

  # The netlink layout oracle: are xtcp2's Go structs the shape the kernel
  # actually sends? Every other netlink check starts *from* the struct and so
  # cannot ask that.
  #
  # Gating is scoped to the protocols that have been triaged, one at a time.
  # NL_Diag_TCPInfo is the first and currently the only one: its deltas are
  # fully accounted for by the 22-entry allowlist, so its unallowlisted count is
  # 0 and a delta appearing there is a real finding. The other 17 audited
  # protocols hold 179 untriaged deltas and stay advisory — turning them on
  # today would make this check permanently red, which is the same as turning
  # it off. Each later phase adds the protocol it covers to this list once it
  # has written that protocol's allowlist entries with reasons.
  #
  # COST, because it is not obvious from the one line: this pulls xdp2's
  # proto-audit closure — a Rust build plus a large pinned source set (kernel
  # tarball, DPDK, nDPI, suricata, tshark, a scapy python). On a cold cache it
  # dominates `nix flake check` wall time by a wide margin. If that becomes a
  # problem, move this one attribute out of the returned set and into
  # `packages` in nix/default.nix — the precedent is proto-lint above, which is
  # kept out of the default set for its own infrastructural reason. Note that
  # doing so now genuinely loses coverage: since NL_Diag_TCPInfo is gated, this
  # attribute is the thing that fails CI on a layout regression.
  proto-audit-netlink = import ./proto-audit-netlink.nix {
    inherit
      pkgs
      lib
      src
      xdp2
      ;
    gatedProtocols = [ "NL_Diag_TCPInfo" ];
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
