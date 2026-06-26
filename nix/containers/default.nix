# nix/containers/default.nix
#
# Entry point for container images. Two axes:
#
#   1. Build variant (debug / default / stripped) — three "fat" OCI images
#      that carry every cmd/* binary built with the named variant. Used
#      for production deployments that need every tool in one image.
#
#   2. Destination flavor (min / kafka / nats / nsq / valkey / s3parquet) — six
#      single-binary scratch images, each carrying just the matching
#      `xtcp2-<flavor>` binary. Used for slim production deployments
#      that only need one destination.
#
#   oci-xtcp2           variant=default, full destinations, all 10 cmds   (~119 MiB)
#   oci-xtcp2-debug     variant=debug,   full destinations, all 10 cmds   (~171 MiB)
#   oci-xtcp2-stripped  variant=stripped,full destinations, all 10 cmds   (~119 MiB)
#   oci-xtcp2-min       single xtcp2 binary, stdlib destinations only     (~22 MiB)
#   oci-xtcp2-kafka     single xtcp2 binary, kafka only                   (~26 MiB)
#   oci-xtcp2-nats      single xtcp2 binary, nats only                    (~26 MiB)
#   oci-xtcp2-nsq       single xtcp2 binary, nsq only                     (~25 MiB)
#   oci-xtcp2-valkey    single xtcp2 binary, valkey only                  (~26 MiB)
#   oci-xtcp2-s3parquet single xtcp2 binary, s3parquet only               (~26 MiB)
#
{
  pkgs,
  lib,
  src,
  binaries,
}:

let
  mkOciImage = import ../lib/mkOciImage.nix { inherit pkgs lib; };

  # tcp-stress-only image: just tcp_server + tcp_client + an entrypoint
  # shell script that dispatches on TCP_MODE. Used by the Phase C
  # docker-in-VM lifecycle harness to spin up N containers with
  # configurable per-container socket counts. Much smaller than the fat
  # xtcp2-all image because it ships only the two test binaries.
  tcpStressBinaries = pkgs.symlinkJoin {
    name = "xtcp2-tcp-stress-binaries";
    paths = [
      binaries.tcp_server
      binaries.tcp_client
    ];
  };

  tcpStressEntrypoint = pkgs.writeShellApplication {
    name = "tcp-stress-entrypoint";
    runtimeInputs = with pkgs; [ coreutils ];
    text = ''
      # Environment knobs (all optional, sensible defaults):
      #   TCP_MODE     server | client | both  (default both)
      #   TCP_COUNT    number of listeners or clients  (default 100)
      #   TCP_SLEEP    pause between client writes     (default 5s)
      #   TCP_PADS     bytes of zero-pad per message   (default 2048)
      #   TCP_CONNECT  host the clients dial           (default 127.0.0.1)
      #   TCP_BIND     iface the server listens on     (default 0.0.0.0)
      MODE="''${TCP_MODE:-both}"
      COUNT="''${TCP_COUNT:-100}"
      SLEEP="''${TCP_SLEEP:-5s}"
      PADS="''${TCP_PADS:-2048}"
      CONNECT="''${TCP_CONNECT:-127.0.0.1}"
      BIND="''${TCP_BIND:-0.0.0.0}"

      echo "tcp-stress: mode=$MODE count=$COUNT sleep=$SLEEP pads=$PADS connect=$CONNECT bind=$BIND"

      case "$MODE" in
        server)
          exec /bin/tcp_server -count "$COUNT" -bind "$BIND"
          ;;
        client)
          exec /bin/tcp_client -count "$COUNT" -connect "$CONNECT" \
            -sleep "$SLEEP" -pads "$PADS"
          ;;
        both)
          # In single-container mode we run both halves: server in
          # background, client in foreground. The 2s sleep gives the
          # server's Accept loop time to come up before clients dial.
          /bin/tcp_server -count "$COUNT" -bind "$BIND" &
          sleep 2
          exec /bin/tcp_client -count "$COUNT" -connect "$CONNECT" \
            -sleep "$SLEEP" -pads "$PADS"
          ;;
        *)
          echo "unknown TCP_MODE: $MODE (want: server | client | both)" >&2
          exit 1
          ;;
      esac
    '';
  };

  tcpStressContents = pkgs.symlinkJoin {
    name = "xtcp2-tcp-stress-image-contents";
    paths = [
      tcpStressBinaries
      tcpStressEntrypoint
      # bash + coreutils are the runtime the entrypoint script needs.
      # Without them the writeShellApplication wrapper can't exec.
      pkgs.bashInteractive
      pkgs.coreutils
    ];
  };

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
  oci-xtcp2-s3parquet = mkFlavorImage "s3parquet";

  # Phase B: tcp_server + tcp_client image, dispatched by TCP_MODE env.
  # Built so the Phase C docker-in-vm lifecycle harness can spin up
  # N containers (default 20) with M sockets each (default 100), with
  # each container getting its own netns courtesy of docker's bridge
  # network, exercising xtcp2's /run/docker/netns/ watch path under
  # real socket load.
  oci-xtcp2-tcp-stress = mkOciImage {
    name = "xtcp2-tcp-stress";
    tag = "latest";
    binaries = tcpStressContents;
    exposedPorts =
      # tcp_server binds 4000..4099 with -count 100. Expose the full
      # block so a docker run -P or explicit -p mapping works.
      lib.range 4000 4099;
    entrypoint = "/bin/tcp-stress-entrypoint";
  };
}
