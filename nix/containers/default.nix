# nix/containers/default.nix
#
# Entry point for container images. Three OCI variants matching the three Go
# build variants (see versions.nix → buildVariants):
#
#   oci-xtcp2          — production default (-s -w),       tag :latest
#   oci-xtcp2-debug    — full debug info,                  tag :debug
#   oci-xtcp2-stripped — production default + strip,       tag :stripped
#
# Each image carries every cmd/<x>/ binary (built with the matching variant)
# under /bin/, with ENTRYPOINT=/bin/xtcp2.
#
{
  pkgs,
  lib,
  src,
  binaries,
}:

let
  mkImage =
    {
      attr,
      tag,
    }:
    import ./oci-xtcp2.nix {
      inherit pkgs lib src;
      binaries = binaries.${attr};
      inherit tag;
    };
in
{
  oci-xtcp2 = mkImage {
    attr = "xtcp2-all";
    tag = "latest";
  };
  oci-xtcp2-debug = mkImage {
    attr = "xtcp2-all-debug";
    tag = "debug";
  };
  oci-xtcp2-stripped = mkImage {
    attr = "xtcp2-all-stripped";
    tag = "stripped";
  };
}
