# nix/containers/oci-xtcp2.nix
#
# OCI image factory. Produces a scratch-based image carrying every xtcp2
# binary built with the requested variant.
#
# Three variants are wired up in containers/default.nix:
#   oci-xtcp2          — default build (-s -w)
#   oci-xtcp2-debug    — full debug info; useful for in-container delve
#   oci-xtcp2-stripped — default + binutils strip; smallest
#
# Load:
#   nix build .#oci-xtcp2 && ./result | docker load
#   docker run --rm xtcp2:latest --help
#
# Switch entrypoint:
#   docker run --rm --entrypoint /bin/register_schema xtcp2:latest --help
#
{
  pkgs,
  lib,
  src,
  binaries, # joined /bin tree (e.g., binaries.xtcp2-all-debug)
  tag ? "latest",
}:

let
  mkOciImage = import ../lib/mkOciImage.nix { inherit pkgs lib; };
in
mkOciImage {
  name = "xtcp2";
  inherit tag;
  inherit binaries;
  protoFile = src + "/proto/xtcp_flat_record/v1/xtcp_flat_record.proto";
  exposedPorts = [
    9088
    8889
  ]; # 9088 = prometheus, 8889 = gRPC
  entrypoint = "/bin/xtcp2";
}
