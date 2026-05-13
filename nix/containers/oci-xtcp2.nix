# nix/containers/oci-xtcp2.nix
#
# OCI image: a single fat scratch-based image carrying every xtcp2 binary.
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
  xtcp2All,
}:

let
  mkOciImage = import ../lib/mkOciImage.nix { inherit pkgs lib; };
in
mkOciImage {
  name = "xtcp2";
  tag = "latest";
  binaries = xtcp2All;
  protoFile = src + "/proto/xtcp_flat_record/v1/xtcp_flat_record.proto";
  exposedPorts = [
    9088
    8889
  ]; # 9088 = prometheus, 8889 = gRPC
  entrypoint = "/bin/xtcp2";
}
