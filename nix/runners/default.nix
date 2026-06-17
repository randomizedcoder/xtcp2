# nix/runners/default.nix
#
# Local Docker runner apps for the xtcp2 OCI images. These wrap the
# build → docker load → docker run → verify loop as `nix run` targets so
# anyone can smoke-test an image without remembering the docker incantation.
#
# The factories in ./oci-start.nix and ./oci-verify.nix are parameterized,
# so adding a runner for another flavor (e.g. kafka) is a one-line addition
# here: point `image`/`tag` at the matching `containers.oci-xtcp2-<flavor>`.
{
  pkgs,
  lib,
  containers,
}:

{
  oci-start = import ./oci-start.nix {
    inherit pkgs lib;
    image = containers.oci-xtcp2-min;
    tag = "min";
  };

  oci-verify = import ./oci-verify.nix {
    inherit pkgs lib;
  };
}
