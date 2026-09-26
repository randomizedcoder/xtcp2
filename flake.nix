#
# flake.nix — xtcp2
#
# Thin orchestrator. Every concern lives under ./nix/ and is wired up here.
# See ./nix/default.nix for the per-system aggregator.
#
# Quick references:
#   nix develop                          # dev shell
#   nix build .#xtcp2                    # main binary
#   nix build .#xtcp2-all                # every cmd/* binary
#   nix build .#oci-xtcp2                # OCI image (load via `./result | docker load`)
#   nix run    .#regen-protos            # `buf generate` (needs network)
#   nix flake check                      # Tier 0+1 lint + go-vet + audits + smokes
#   nix run    .#microvm-x86_64-lifecycle  # boot xtcp2 in a VM, run 3-check self-test
#   nix run    .#microvm-x86_64-discovery-bench  # ns-discovery A/B (dir vs /proc scan)
#
# Overriding the giouring source (local fork):
#   nix develop --override-input giouring path:/home/das/Downloads/giouring
#
{
  description = "xtcp2 — TCP socket introspection via netlink";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";

    microvm = {
      url = "github:astro/microvm.nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };

    # Local fork of iceber/iouring-go. Pin rev — overridable via
    # `--override-input giouring path:/path/to/local`.
    giouring = {
      url = "github:randomizedcoder/giouring/9e96b7216bf07ce3c97281092444e85311f7b2e4";
      flake = false;
    };

    # xdp2 provides `proto-audit`, the netlink layout oracle. It normalises the
    # kernel UAPI headers and 15 other sources to one IR indexed by wire bit
    # offset, which is what lets it find a field xtcp2's Go structs are
    # *missing* rather than merely mis-reading one they have. Consumed by
    # nix/checks/proto-audit-netlink.nix; see docs/netlink/coverage-status.md.
    #
    # Deliberately NOT `inputs.nixpkgs.follows = "nixpkgs"`. proto-audit is a
    # Rust build over a large pinned source set (a kernel tarball, DPDK, nDPI,
    # suricata, tshark, a scapy python) and is built against the nixpkgs it was
    # tested with. Following xtcp2's nixpkgs would trade one duplicated nixpkgs
    # evaluation for a build that may simply not work.
    #
    # Override to a local checkout when iterating on the oracle itself:
    #   nix build .#proto-audit-netlink --override-input xdp2 path:/home/das/Downloads/xdp2
    xdp2.url = "github:randomizedcoder/xdp2/47d3a425bb4f3a03701848f0579630e1019d3d51";
  };

  nixConfig = {
    extra-substituters = [ "https://microvm.cachix.org" ];
    extra-trusted-public-keys = [
      "microvm.cachix.org-1:oXnBc6hRE3eX5rSYdRyMYXnfzcCxC7yKPTbZXALsqys="
    ];
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
      microvm,
      giouring,
      xdp2,
    }:
    flake-utils.lib.eachSystem [ "x86_64-linux" ] (
      system:
      let
        pkgs = import nixpkgs {
          inherit system;
          # MinIO ships in nixpkgs marked insecure (upstream cadence vs.
          # nixpkgs vulnerability tracking). The Vector flavor of the
          # microvm uses it as a local test fixture, never exposed beyond
          # the VM. Pin the exact version we accept so accidental nixpkgs
          # bumps fail loudly instead of silently sliding to a new CVE.
          config.permittedInsecurePackages = [
            "minio-2025-10-15T17-29-55Z"
          ];
        };
        inherit (nixpkgs) lib;

        aggregator = import ./nix {
          inherit
            pkgs
            lib
            microvm
            nixpkgs
            giouring
            xdp2
            ;
          src = ./.;
        };
      in
      {
        inherit (aggregator)
          packages
          devShells
          checks
          apps
          ;
      }
    )
    // {
      overlays.default = import ./nix/overlays.nix { inherit self; };
    };
}
