# nix/packages.nix
#
# Package definitions for xtcp2.
#
# Three categories:
#   - nativeBuildInputs: build-time tools (compilers, codegen)
#   - buildInputs: libraries needed at link/runtime (xtcp2 is pure Go, so empty)
#   - devTools: extras for the developer shell only
#
{ pkgs }:

let
  versions = import ./versions.nix { inherit pkgs; };
in
{
  # Build-time only (used inside derivations)
  nativeBuildInputs = [
    versions.go
    pkgs.git
    pkgs.cacert
  ];

  # Link/runtime deps. xtcp2 is pure Go (CGO_ENABLED=0) so this stays empty.
  buildInputs = [ ];

  # Developer shell only. Goal: every contributor command works out of the box.
  devTools = with pkgs; [
    # Go ecosystem
    versions.go
    gopls
    gotools
    delve
    go-tools # staticcheck etc.
    versions.golangci-lint
    versions.gosec

    # protobuf / gRPC
    versions.buf
    versions.protoc
    versions.grpcurl

    # netlink / TCP debugging.
    # ss ships inside iproute2 — no separate nixpkgs attr exists for it.
    iproute2
    tcpdump
    strace
    ltrace

    # io_uring observability
    liburing # provides io_uring-test / liburing headers for native probing

    # HTTP / data plumbing
    curl
    jq
    netcat-gnu

    # Nix tooling
    versions.nixfmt

    # MicroVM tooling
    qemu_kvm
    expect # drives the vm-verify-*.exp scripts
    openssh
    tmux

    # Container plumbing
    skopeo # inspect dockerTools-built images without docker

    # Vector / MinIO pipeline (microvm vector flavor; also useful for
    # interactive debugging of the protobuf → parquet path on the host).
    vector
    minio
    minio-client
    duckdb
  ];

  # Combined list (everything for the dev shell)
  allDevPackages =
    let
      self = import ./packages.nix { inherit pkgs; };
    in
    self.nativeBuildInputs ++ self.buildInputs ++ self.devTools;
}
