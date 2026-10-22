# nix/modules/garage-bucket-bootstrap.nix
#
# NixOS module: runs a single-node Garage S3 server inside the xtcp2 microvm
# plus a oneshot that lays out the node, imports the test key and creates the
# bucket xtcp2's parquet sink writes to.
#
# Garage replaced MinIO when nixpkgs dropped the minio package and its NixOS
# module (abandoned upstream; minio-client is marked insecure for the same
# reason). The S3 API stays on port 9000 with region us-east-1, so xtcp2's
# minio-go client, the self-test's checks and ClickHouse's s3() read path see
# the same endpoint as before.
#
# Storage is a tmpfs at /var/lib/s3 — survives only for the VM's lifetime,
# which is exactly the test budget. Credentials and the RPC secret are static
# and committed to /nix/store; this is a test fixture, not a production
# deployment. The RPC and admin listeners bind loopback only.
#
# Bucket bootstrap is its own oneshot rather than ExecStartPre on xtcp2 so
# that:
#   - it can retry independently while Garage starts,
#   - consumers have a clean After=/Requires= edge on a "ready" unit,
#   - failures are attributable to bucket setup vs. xtcp2 start.
#
{
  bucket ? "xtcp2-records",
  accessKey ? "xtcp2test",
  # Garage refuses secrets shorter than 16 characters, which is why this is
  # not MinIO's old "xtcp2testsecret".
  secretKey ? "xtcp2testsecretkey",
  dataSize ? "512M",
  # When the caller provides a dedicated /var/lib/s3 block device
  # (e.g. microvm.volumes), skip the module's tmpfs declaration. The
  # tmpfs is fine for short smokes; a 24h mixed flavor soak fills the
  # default 512 MiB and starts losing parquet uploads.
  useTmpfs ? true,
}:

{
  lib,
  pkgs,
  ...
}:

let
  garage = pkgs.garage_2;
  root = "/var/lib/s3";

  # The CLI reads the same file the server does: /etc/garage.toml, which the
  # services.garage module writes from settings, rpc_secret included.
  bootstrapScript = pkgs.writeShellApplication {
    name = "xtcp2-bucket-bootstrap";
    runtimeInputs = [
      garage
      pkgs.coreutils # seq, sleep
      pkgs.gnugrep
    ];
    text = ''
      # `garage status` answers once the RPC listener is up. The admin
      # /health endpoint is no readiness gate here: it returns 503 until a
      # layout exists, and creating the layout is this script's job.
      for _ in $(seq 1 60); do
        if garage status >/dev/null 2>&1; then
          break
        fi
        sleep 1
      done
      if ! garage status >/dev/null 2>&1; then
        echo "xtcp2-bucket-bootstrap: garage never answered on RPC after 60 s" >&2
        exit 1
      fi

      # One node in one zone. Every step is skipped when already done, so a
      # data disk that outlives the VM (the microvm.volumes images) boots
      # cleanly. The capacity is a placement weight, not a quota.
      if garage layout show | grep -qx "Current cluster layout version: 0"; then
        node=$(garage node id -q)
        garage layout assign -z dc1 -c 1G "''${node%@*}"
        garage layout apply --version 1
      fi

      # Garage never re-admits a deleted key ID, so import only when absent.
      if ! garage key info xtcp2 >/dev/null 2>&1; then
        garage key import --yes -n xtcp2 ${accessKey} ${secretKey}
      fi
      if ! garage bucket info ${bucket} >/dev/null 2>&1; then
        garage bucket create ${bucket}
      fi
      garage bucket allow --read --write --owner ${bucket} --key xtcp2

      echo "xtcp2-bucket-bootstrap: bucket ${bucket} ready"
    '';
  };
in
{
  # tmpfs for the Garage metadata and data. Skipped when the caller provides
  # a dedicated block device for /var/lib/s3.
  fileSystems = lib.mkIf useTmpfs {
    ${root} = {
      device = "tmpfs";
      fsType = "tmpfs";
      options = [
        "size=${dataSize}"
        "mode=0750"
      ];
    };
  };

  # A static user rather than the module's DynamicUser: the directories live
  # on a mount (tmpfs or an ext4 volume) outside /var/lib/garage, so systemd's
  # StateDirectory cannot own them, and tmpfiles needs a fixed name to chown
  # them to.
  users.users.garage = {
    isSystemUser = true;
    group = "garage";
  };
  users.groups.garage = { };

  systemd.tmpfiles.rules = [
    "d ${root} 0750 garage garage -"
    "d ${root}/meta 0700 garage garage -"
    "d ${root}/data 0700 garage garage -"
  ];

  services.garage = {
    enable = true;
    package = garage;
    settings = {
      metadata_dir = "${root}/meta";
      data_dir = "${root}/data";
      db_engine = "lmdb";
      replication_factor = 1;
      rpc_bind_addr = "127.0.0.1:3901";
      rpc_public_addr = "127.0.0.1:3901";
      # Fixture secret: the RPC listener is loopback-only and single-node.
      rpc_secret = "b7a53629beea0bbf04928147befff27a80f7b8709e6bbb5dd68360b3198886b2";
      s3_api = {
        s3_region = "us-east-1";
        # All interfaces, not 127.0.0.1, so QEMU usermode hostfwd (host:9000
        # → VM eth0:9000) can reach it. Inside the VM, xtcp2 still talks to
        # 127.0.0.1:9000.
        api_bind_addr = "0.0.0.0:9000";
      };
      admin.api_bind_addr = "127.0.0.1:3903";
    };
  };

  systemd.services.garage = {
    after = [ "systemd-tmpfiles-setup.service" ];
    serviceConfig = {
      DynamicUser = lib.mkForce false;
      User = "garage";
      Group = "garage";
      # DynamicUser implied ProtectSystem=strict; keep it now that it's off.
      # The module already lists metadata_dir and data_dir in ReadWritePaths.
      ProtectSystem = "strict";
    };
  };

  systemd.services.xtcp2-bucket-bootstrap = {
    description = "Pre-create the Garage bucket for the xtcp2 parquet sink";
    after = [ "garage.service" ];
    requires = [ "garage.service" ];
    wantedBy = [ "multi-user.target" ];

    serviceConfig = {
      Type = "oneshot";
      RemainAfterExit = true;
      # "${pkg}/bin/<name>" and not the bare "${pkg}": writeShellApplication
      # sets destination = "/bin/${name}", so the derivation is a directory.
      ExecStart = "${bootstrapScript}/bin/xtcp2-bucket-bootstrap";
      StandardOutput = "journal+console";
      StandardError = "journal+console";
    };
  };
}
