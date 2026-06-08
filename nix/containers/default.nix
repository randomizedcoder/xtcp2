# nix/containers/default.nix
#
# Entry point for container images. Two axes:
#
#   1. Build variant (debug / default / stripped) — three "fat" OCI images
#      that carry every cmd/* binary built with the named variant. Used
#      for production deployments that need every tool in one image.
#
#   2. Destination flavor (min / kafka / nats / nsq / valkey) — five
#      single-binary scratch images, each carrying just the matching
#      `xtcp2-<flavor>` binary. Used for slim production deployments
#      that only need one destination.
#
#   oci-xtcp2          variant=default, full destinations, all 10 cmds   (~119 MiB)
#   oci-xtcp2-debug    variant=debug,   full destinations, all 10 cmds   (~171 MiB)
#   oci-xtcp2-stripped variant=stripped,full destinations, all 10 cmds   (~119 MiB)
#   oci-xtcp2-min      single xtcp2 binary, stdlib destinations only     (~22 MiB)
#   oci-xtcp2-kafka    single xtcp2 binary, kafka only                   (~26 MiB)
#   oci-xtcp2-nats     single xtcp2 binary, nats only                    (~26 MiB)
#   oci-xtcp2-nsq      single xtcp2 binary, nsq only                     (~25 MiB)
#   oci-xtcp2-valkey   single xtcp2 binary, valkey only                  (~26 MiB)
#
{
  pkgs,
  lib,
  src,
  binaries,
}:

let
  mkOciImage = import ../lib/mkOciImage.nix { inherit pkgs lib; };

  mkFatImage =
    {
      attr,
      tag,
    }:
    mkOciImage {
      name = "xtcp2";
      inherit tag;
      binaries = binaries.${attr};
      protoFile = src + "/proto/xtcp_flat_record/v1/xtcp_flat_record.proto";
      exposedPorts = [
        9088
        8889
      ];
      entrypoint = "/bin/xtcp2";
    };

  mkFlavorImage =
    flavor:
    mkOciImage {
      name = "xtcp2";
      tag = flavor;
      binaries = binaries.xtcp2OnlyByFlavor.${flavor};
      protoFile = src + "/proto/xtcp_flat_record/v1/xtcp_flat_record.proto";
      exposedPorts = [
        9088
        8889
      ];
      entrypoint = "/bin/xtcp2";
    };
in
{
  oci-xtcp2 = mkFatImage {
    attr = "xtcp2-all";
    tag = "latest";
  };
  oci-xtcp2-debug = mkFatImage {
    attr = "xtcp2-all-debug";
    tag = "debug";
  };
  oci-xtcp2-stripped = mkFatImage {
    attr = "xtcp2-all-stripped";
    tag = "stripped";
  };

  oci-xtcp2-min = mkFlavorImage "min";
  oci-xtcp2-kafka = mkFlavorImage "kafka";
  oci-xtcp2-nats = mkFlavorImage "nats";
  oci-xtcp2-nsq = mkFlavorImage "nsq";
  oci-xtcp2-valkey = mkFlavorImage "valkey";
}
