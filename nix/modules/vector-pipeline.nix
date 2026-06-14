# nix/modules/vector-pipeline.nix
#
# NixOS module: runs Vector as the host agent inside the xtcp2 microvm.
#
#   xtcp2 (unixgram, protobufSingle) ──► /run/xtcp2/output.sock
#                                         │
#                                  Vector source: socket / unix_datagram
#                                  Decoder: protobuf via FileDescriptorSet
#                                         │
#                                  Transform: VRL — decode base64 bytes
#                                  IP fields and re-encode as hex so the
#                                  parquet column is queryable without
#                                  Arrow base64 acrobatics.
#                                         │
#                                  Sink: aws_s3 (parquet, snappy) → MinIO
#
# Inputs:
#   protoDescPackage  derivation that provides
#                     share/xtcp2/xtcp_flat_record.desc (see
#                     nix/lib/mkProtoDescSet.nix)
#   bucket            S3 bucket name MinIO is pre-seeded with
#   endpoint          MinIO endpoint URL (e.g. http://127.0.0.1:9000)
#   accessKey/secret  static MinIO credentials (test only)
#
# This module does *not* configure MinIO itself — see
# nix/modules/minio-bucket-bootstrap.nix.
#
{
  protoDescPackage,
  bucket ? "xtcp2-records",
  endpoint ? "http://127.0.0.1:9000",
  accessKey ? "xtcp2test",
  secretKey ? "xtcp2testsecret",
}:

{
  config,
  lib,
  pkgs,
  ...
}:

let
  descPath = "${protoDescPackage}/share/xtcp2/xtcp_flat_record.desc";

  vectorSettings = {
    data_dir = "/var/lib/vector";

    sources.xtcp2 = {
      type = "socket";
      mode = "unix_datagram";
      path = "/run/xtcp2/output.sock";
      socket_file_mode = 438; # 0o666
      decoding = {
        codec = "protobuf";
        protobuf = {
          desc_file = descPath;
          message_type = "xtcp_flat_record.v1.XtcpFlatRecord";
        };
      };
    };

    transforms.normalize_ips = {
      type = "remap";
      inputs = [ "xtcp2" ];
      source = ''
        # Vector's protobuf decoder emits `bytes` fields as base64 strings. The
        # source and destination IPs land in `inet_diag_msg_socket_source` /
        # `_destination`. Decode the base64 back to bytes and re-encode as hex
        # so the parquet column is a deterministic ASCII string that downstream
        # consumers can decode without Arrow base64 gymnastics.
        src_b64, src_err = string(.inet_diag_msg_socket_source)
        if src_err == null {
          src_bytes, derr = decode_base64(src_b64)
          if derr == null {
            .src_ip_hex = encode_base16(src_bytes)
          }
        }
        dst_b64, dst_err = string(.inet_diag_msg_socket_destination)
        if dst_err == null {
          dst_bytes, derr = decode_base64(dst_b64)
          if derr == null {
            .dst_ip_hex = encode_base16(dst_bytes)
          }
        }
      '';
    };

    sinks.minio = {
      type = "aws_s3";
      inputs = [ "normalize_ips" ];
      bucket = bucket;
      endpoint = endpoint;
      region = "us-east-1";
      force_path_style = true;
      key_prefix = "date=%F/hour=%H/";
      filename_time_format = "%s";
      filename_append_uuid = true;
      auth = {
        access_key_id = accessKey;
        secret_access_key = secretKey;
      };
      compression = "none";
      encoding.codec = "json";
      batch = {
        max_bytes = 1000000;
        timeout_secs = 5;
      };
      healthcheck.enabled = false;
    };
  };

  vectorConfigFile = (pkgs.formats.toml { }).generate "vector.toml" vectorSettings;
in
{
  environment.etc."vector/vector.toml".source = vectorConfigFile;
  environment.etc."vector/xtcp_flat_record.desc".source = descPath;

  systemd.services.vector = {
    description = "Vector — protobuf → parquet host agent for xtcp2";
    after = [
      "network.target"
      "xtcp2-bucket-bootstrap.service"
    ];
    requires = [ "xtcp2-bucket-bootstrap.service" ];
    wantedBy = [ "multi-user.target" ];

    serviceConfig = {
      Type = "simple";
      ExecStartPre = [
        "-${pkgs.coreutils}/bin/rm -f /run/xtcp2/output.sock"
        "${pkgs.vector}/bin/vector validate --no-environment ${vectorConfigFile}"
      ];
      ExecStart = "${pkgs.vector}/bin/vector --config ${vectorConfigFile}";
      Restart = "on-failure";
      RestartSec = "2s";
      User = "root";
      RuntimeDirectory = "xtcp2";
      RuntimeDirectoryMode = "0755";
      StateDirectory = "vector";
      StateDirectoryMode = "0700";
      StandardOutput = "journal+console";
      StandardError = "journal+console";
    };
  };
}
