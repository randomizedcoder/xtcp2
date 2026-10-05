# nix/containers/default.nix
#
# Entry point for container images. Three axes:
#
#   1. Build variant (debug / default / stripped) — three "fat" OCI images
#      that carry every cmd/* binary built with the named variant, with every
#      destination AND every enricher. Used for production deployments that
#      need every tool in one image.
#
#   2. Destination flavor (min / kafka / nats / nsq / valkey / s3parquet) —
#      which message-destination client is compiled in.
#
#   3. Enrichment flavor (none / asn / locality / enrich) — whether the two
#      heavyweight enrichers are compiled in. `asn` links parquet-go + bart;
#      `locality` links bart. Both are off at runtime by default even when
#      present; the tag only decides whether the code is in the image at all.
#      See nix/versions.nix and pkg/xtcp/enrich_core.go.
#
#   Axes 2 and 3 cross-produce into 6 x 4 = 24 slim single-binary scratch
#   images named oci-xtcp2-<dest>[-<enrich>], generated below rather than
#   hand-listed. The `none` cell keeps the historical unsuffixed name
#   (oci-xtcp2-s3parquet), so existing consumers are unmoved — but note that
#   those images no longer carry ASN/locality; the `-enrich` suffix is the
#   equivalent of what they were before the enrichment axis existed.
#
#   oci-xtcp2                   variant=default, full dests + all enrichers, every cmd
#   oci-xtcp2-debug             variant=debug,   same contents, full symbols
#   oci-xtcp2-stripped          variant=stripped,same contents
#   oci-xtcp2-min               single xtcp2 binary, stdlib destinations, no enrichers
#   oci-xtcp2-min-enrich        ... plus both enrichers
#   oci-xtcp2-kafka             single xtcp2 binary, kafka only, no enrichers
#   oci-xtcp2-kafka-asn         ... plus the ASN enricher
#   oci-xtcp2-kafka-locality    ... plus the locality enricher
#   oci-xtcp2-kafka-enrich      ... plus both
#   (likewise for nats / nsq / valkey / s3parquet)
#
#   Sizes are in docs/build-flavors.md, which is re-measured rather than
#   guessed; they are deliberately not duplicated here.
#
#   4. Client binaries — slim single-binary scratch images for the gRPC
#      clients, for users who want just the client (not the fat image).
#
#   oci-xtcp2client     single xtcp2client binary (FlatRecords / poll)
#   oci-xtcp2ctl        single xtcp2ctl binary (runtime control)
#
{
  pkgs,
  lib,
  src,
  binaries,
  ipmetaBootstrapArtifact ? null,
  daemonAttrSuffix ? "",
  daemonTagSuffix ? "",
}:

let
  mkOciImage = import ../lib/mkOciImage.nix { inherit pkgs lib; };
  versions = import ../versions.nix { inherit pkgs; };

  ipmetaBootstrapContents = lib.optional (ipmetaBootstrapArtifact != null) (
    pkgs.runCommand "xtcp2-ipmeta-bootstrap" { } ''
      mkdir -p $out/share/xtcp2/ipmeta
      cp ${ipmetaBootstrapArtifact} $out/share/xtcp2/ipmeta/bootstrap.lookup.parquet.zst
    ''
  );

  # Self-contained container HEALTHCHECK for the xtcp2-daemon images (scratch,
  # no shell/curl): the binary probes its own /readyz via `-healthcheck`.
  # Durations are integer nanoseconds. NOT applied to the tcp-stress image
  # (different entrypoint, no daemon).
  xtcp2Healthcheck = {
    Test = [
      "CMD"
      "/bin/xtcp2"
      "-healthcheck"
    ];
    Interval = 30000000000; # 30s
    Timeout = 5000000000; # 5s
    StartPeriod = 15000000000; # 15s — grace while the daemon reaches ready
    Retries = 3;
  };

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
      #   TCP_SRCADDR  bind clients' source IP         (default: kernel picks)
      #   TCP_IFACE    bind clients to this interface  (default: kernel picks)
      #                (SO_BINDTODEVICE — drives xtcp2 interface-name enrichment)
      MODE="''${TCP_MODE:-both}"
      COUNT="''${TCP_COUNT:-100}"
      SLEEP="''${TCP_SLEEP:-5s}"
      PADS="''${TCP_PADS:-2048}"
      CONNECT="''${TCP_CONNECT:-127.0.0.1}"
      BIND="''${TCP_BIND:-0.0.0.0}"
      SRCADDR="''${TCP_SRCADDR:-}"
      IFACE="''${TCP_IFACE:-}"

      # Optional source-address / interface binds for the client half. Left out
      # entirely when unset so the kernel keeps choosing (original behaviour).
      CLIENT_EXTRA=()
      if [ -n "$SRCADDR" ]; then CLIENT_EXTRA+=(-srcaddr "$SRCADDR"); fi
      if [ -n "$IFACE" ]; then CLIENT_EXTRA+=(-iface "$IFACE"); fi

      echo "tcp-stress: mode=$MODE count=$COUNT sleep=$SLEEP pads=$PADS connect=$CONNECT bind=$BIND srcaddr=''${SRCADDR:-<default>} iface=''${IFACE:-<default>}"

      case "$MODE" in
        server)
          exec /bin/tcp_server -count "$COUNT" -bind "$BIND"
          ;;
        client)
          exec /bin/tcp_client -count "$COUNT" -connect "$CONNECT" \
            -sleep "$SLEEP" -pads "$PADS" "''${CLIENT_EXTRA[@]}"
          ;;
        both)
          # In single-container mode we run both halves: server in
          # background, client in foreground. The 2s sleep gives the
          # server's Accept loop time to come up before clients dial.
          /bin/tcp_server -count "$COUNT" -bind "$BIND" &
          sleep 2
          exec /bin/tcp_client -count "$COUNT" -connect "$CONNECT" \
            -sleep "$SLEEP" -pads "$PADS" "''${CLIENT_EXTRA[@]}"
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
      healthcheck = xtcp2Healthcheck;
      extraContents = ipmetaBootstrapContents;
    };

  # Slim single-binary daemon image for one {destination, enrichment} cell.
  # `enrich = "none"` keeps the historical unsuffixed attr name and image tag
  # (oci-xtcp2-s3parquet → xtcp2:s3parquet), so existing consumers — including
  # the downstream runpod/xtcp2 pin and its config drift guard — are unmoved.
  # The other cells append their enrichment flavor to both.
  mkFlavorImage =
    { dest, enrich }:
    mkOciImage {
      name = "xtcp2";
      tag = "${dest}${lib.optionalString (enrich != "none") "-${enrich}"}${daemonTagSuffix}";
      binaries = binaries.xtcp2OnlyByFlavor.${dest}.${enrich};
      protoFile = src + "/proto/xtcp_flat_record/v1/xtcp_flat_record.proto";
      exposedPorts = [
        9088
        8889
      ];
      entrypoint = "/bin/xtcp2";
      healthcheck = xtcp2Healthcheck;
      extraContents = ipmetaBootstrapContents;
    };

  # The slim daemon images: 6 destination flavors × 4 enrichment flavors = 24.
  # `full` is excluded because the fat oci-xtcp2 images already cover the
  # full-destination build. Generated rather than hand-listed so a new flavor
  # in versions.nix produces its images (and, via the "oci-" prefix filter in
  # nix/default.nix, its flake attrs) without being declared three times.
  slimDaemonImages = lib.listToAttrs (
    lib.concatMap (
      dest:
      map (enrich: {
        name = "oci-xtcp2-${dest}${lib.optionalString (enrich != "none") "-${enrich}"}${daemonAttrSuffix}";
        value = mkFlavorImage { inherit dest enrich; };
      }) (builtins.attrNames versions.enrichmentFlavors)
    ) (lib.remove "full" (builtins.attrNames versions.destinationFlavors))
  );

  # Slim single-binary images for the gRPC clients (xtcp2client, xtcp2ctl).
  # Same scratch + CA-bundle base as the flavor images, carrying just the one
  # client binary with its own entrypoint. No proto payload, no exposed ports,
  # and no HEALTHCHECK: these dial the daemon and exit, they aren't long-running
  # services. (The clients also ship inside the fat `oci-xtcp2` image — reach
  # them there via `--entrypoint /bin/xtcp2client`; these images are for users
  # who only want the client.)
  mkClientImage =
    name:
    mkOciImage {
      inherit name;
      tag = "latest";
      binaries = binaries.${name};
      entrypoint = "/bin/${name}";
    };

  # Self-contained HEALTHCHECK for the ipfeed-collector daemon image (scratch,
  # no shell/curl): the binary probes its own /readyz via `-healthcheck`.
  ipfeedHealthcheck = {
    Test = [
      "CMD"
      "/bin/ipfeed-collector"
      "-healthcheck"
    ];
    Interval = 30000000000; # 30s
    Timeout = 5000000000; # 5s
    StartPeriod = 15000000000; # 15s — grace while the first cycle runs
    Retries = 3;
  };
in
slimDaemonImages
// {
  "oci-xtcp2${daemonAttrSuffix}" = mkFatImage {
    attr = "xtcp2-all";
    tag = "latest${daemonTagSuffix}";
  };
  "oci-xtcp2-debug${daemonAttrSuffix}" = mkFatImage {
    attr = "xtcp2-all-debug";
    tag = "debug${daemonTagSuffix}";
  };
  "oci-xtcp2-stripped${daemonAttrSuffix}" = mkFatImage {
    attr = "xtcp2-all-stripped";
    tag = "stripped${daemonTagSuffix}";
  };

  # The 24 slim per-flavor daemon images (oci-xtcp2-<dest>[-<enrich>]) are
  # merged in from `slimDaemonImages` above rather than listed here.

  # Slim per-client images (gRPC clients that talk to the daemon).
  oci-xtcp2client = mkClientImage "xtcp2client";
  oci-xtcp2ctl = mkClientImage "xtcp2ctl";

  # Slim single-binary image for the ipfeed-collector daemon: scratch + CA
  # bundle, runs as a daemon on :8080 with a self-probe HEALTHCHECK. Feed
  # definitions are provided at runtime (mount a dir and set -sources-dir /
  # IPFEED_SOURCES_DIR); they are not baked into the image.
  oci-ipfeed-collector = mkOciImage {
    name = "ipfeed-collector";
    tag = "latest";
    binaries = binaries."ipfeed-collector";
    entrypoint = "/bin/ipfeed-collector";
    cmd = [
      "-daemon"
      "-http-addr"
      ":8080"
    ];
    exposedPorts = [ 8080 ];
    healthcheck = ipfeedHealthcheck;
  };

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
