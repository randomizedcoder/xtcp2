# nix/versions.nix
#
# Pinned tool versions for the xtcp2 Nix flake.
#
# Single source of truth — every other module reads from here.
# Changing a version here propagates to dev shell, build derivations, and checks.
#
{ pkgs }:

{
  # Go toolchain. Must satisfy go.mod's `go 1.25` directive.
  # nixpkgs unstable should have go_1_25; fall back to `go` (latest) if not.
  go = pkgs.go_1_25 or pkgs.go;

  # protobuf tooling
  buf = pkgs.buf;
  protoc = pkgs.protobuf;

  # Static analysis
  golangci-lint = pkgs.golangci-lint;
  gosec = pkgs.gosec;
  nixfmt = pkgs.nixfmt-rfc-style or pkgs.nixfmt;

  # gRPC / proto inspection
  grpcurl = pkgs.grpcurl;

  # Build-flag knobs (consumed by mkGoBinary / oci image)
  ldflagsBase = [
    "-s"
    "-w"
  ];
  buildTags = [
    "netgo"
    "osusergo"
  ];
  cgoEnabled = false;

  # Go vendor hash. Update by running `nix build .#xtcp2` and pasting the
  # `got:` value from the hash mismatch error. Used by every Nix check that
  # needs deps in the sandbox (see nix/lib/goModules.nix).
  goVendorHash = "sha256-+zTEBV/qkepj8eCgoPLAnp4+phmUHl/eV1OPEKVfUi4=";
}
