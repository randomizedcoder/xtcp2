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
        lib = nixpkgs.lib;

        aggregator = import ./nix {
          inherit
            pkgs
            lib
            microvm
            nixpkgs
            giouring
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
