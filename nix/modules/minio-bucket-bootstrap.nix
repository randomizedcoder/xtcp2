# nix/modules/minio-bucket-bootstrap.nix
#
# NixOS module: runs a single-node MinIO server inside the xtcp2 microvm
# plus a oneshot that pre-creates the bucket Vector's aws_s3 sink writes to.
#
# Storage is a tmpfs at /var/lib/minio — survives only for the VM's lifetime,
# which is exactly the test budget. Credentials are static and committed to
# /nix/store; this is a test fixture, not a production deployment.
#
# Bucket bootstrap is its own oneshot rather than ExecStartPre on Vector so
# that:
#   - it can retry independently while MinIO warms up,
#   - Vector.service has a clean After=/Requires= edge on a "ready" unit,
#   - failures are attributable to bucket setup vs. Vector start.
#
{
  bucket ? "xtcp2-records",
  accessKey ? "xtcp2test",
  secretKey ? "xtcp2testsecret",
  dataSize ? "512M",
  # When the caller provides a dedicated /var/lib/minio block device
  # (e.g. microvm.volumes), skip the module's tmpfs declaration. The
  # tmpfs is fine for short smokes; a 24h mixed flavor soak fills the
  # default 512 MiB and starts losing parquet uploads.
  useTmpfs ? true,
}:

{
  config,
  lib,
  pkgs,
  ...
}:

let
  credentialsFile = pkgs.writeText "minio-credentials" ''
    MINIO_ROOT_USER=${accessKey}
    MINIO_ROOT_PASSWORD=${secretKey}
  '';

  bootstrapScript = pkgs.writeShellScript "xtcp2-bucket-bootstrap" ''
    set -eu
    export MC_CONFIG_DIR=/var/lib/xtcp2-bucket-bootstrap/.mc
    mkdir -p "$MC_CONFIG_DIR"

    MC=${pkgs.minio-client}/bin/mc
    CURL=${pkgs.curl}/bin/curl

    # MinIO returns 200 OK on /minio/health/live once the API socket is
    # bound and the disk pool is formatted. `systemctl is-active minio`
    # turns "active" earlier, while formatting is still in progress, so we
    # rely on the live endpoint as the real readiness gate.
    for _ in $(${pkgs.coreutils}/bin/seq 1 60); do
      if "$CURL" --silent --fail --max-time 2 \
           "http://127.0.0.1:9000/minio/health/live" >/dev/null 2>&1; then
        break
      fi
      sleep 1
    done

    if ! "$CURL" --silent --fail --max-time 2 \
         "http://127.0.0.1:9000/minio/health/live" >/dev/null 2>&1; then
      echo "xtcp2-bucket-bootstrap: MinIO /health/live never returned 200 after 60 s" >&2
      exit 1
    fi

    # `mc alias set` does a credentialed probe against the server, so it
    # must run after MinIO is ready.
    if ! "$MC" alias set local http://127.0.0.1:9000 ${accessKey} ${secretKey} \
         >/dev/null; then
      echo "xtcp2-bucket-bootstrap: mc alias set failed" >&2
      exit 1
    fi

    # `mb --ignore-existing` is idempotent.
    if "$MC" mb --ignore-existing local/${bucket}; then
      echo "xtcp2-bucket-bootstrap: bucket ${bucket} ready"
      exit 0
    fi

    echo "xtcp2-bucket-bootstrap: failed to create bucket ${bucket}" >&2
    exit 1
  '';
in
{
  # tmpfs for MinIO data. services.minio dataDir defaults to /var/lib/minio/data;
  # mounting the parent as tmpfs covers it and avoids fighting the module.
  # Skipped when the caller provides a dedicated block device for /var/lib/minio.
  fileSystems = lib.mkIf useTmpfs {
    "/var/lib/minio" = {
      device = "tmpfs";
      fsType = "tmpfs";
      options = [
        "size=${dataSize}"
        "mode=0755"
      ];
    };
  };

  services.minio = {
    enable = true;
    rootCredentialsFile = "${credentialsFile}";
    region = "us-east-1";
    browser = false;
    # Bind on all interfaces, not 127.0.0.1, so QEMU usermode hostfwd
    # (which routes host:9000 → VM eth0:9000) can reach MinIO. Inside
    # the VM, xtcp2 still talks to MinIO via 127.0.0.1 (the loopback
    # path is identical regardless of bind address); the wider bind
    # only adds the eth0 path that hostfwd needs.
    listenAddress = "0.0.0.0:9000";
    consoleAddress = "0.0.0.0:9001";
    dataDir = [ "/var/lib/minio/data" ];
  };

  systemd.services.xtcp2-bucket-bootstrap = {
    description = "Pre-create MinIO bucket for xtcp2 parquet sink";
    after = [ "minio.service" ];
    requires = [ "minio.service" ];
    wantedBy = [ "multi-user.target" ];

    # `mc` shells out to `getent` to resolve the user's config directory.
    # In nixpkgs that binary lives in its own `getent` package (not in
    # glibc.bin which surprisingly omits it). Without this PATH addition
    # mc exits with `Unable to get mcConfigDir. exec: "getent":
    # executable file not found in $PATH` before doing anything useful.
    path = [
      pkgs.getent
      pkgs.coreutils
    ];

    serviceConfig = {
      Type = "oneshot";
      RemainAfterExit = true;
      ExecStart = "${bootstrapScript}";
      StateDirectory = "xtcp2-bucket-bootstrap";
      StateDirectoryMode = "0700";
      StandardOutput = "journal+console";
      StandardError = "journal+console";
    };
  };
}
