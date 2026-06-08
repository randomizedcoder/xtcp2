# nix/protos/buf-generate.nix
#
# Wraps `buf generate` as a runnable shell application.
#
# Impure: buf fetches remote plugins (buf.build/protocolbuffers/go,
# buf.build/grpc-ecosystem/gateway:v2.26.3, …) over the network. Live in the
# dev shell; not part of `nix flake check`.
#
# Use:
#   nix run .#regen-protos
#   # or from inside `nix develop`:
#   regen-protos
#
{ pkgs, lib }:

let
  versions = import ../versions.nix { inherit pkgs; };
in
pkgs.writeShellApplication {
  name = "regen-protos";
  runtimeInputs = [ versions.buf ];
  text = ''
    set -euo pipefail

    if [ ! -f buf.yaml ]; then
      echo "regen-protos: must be run from the xtcp2 repo root (no buf.yaml here)" >&2
      exit 2
    fi

    echo "==> buf dep update"
    buf dep update

    echo "==> buf lint"
    buf lint

    echo "==> buf build"
    buf build

    echo "==> buf generate"
    buf generate

    echo "regen-protos: done — review and commit any *.pb.go / *.pb.* drift"
  '';
}
