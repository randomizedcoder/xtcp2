# nix/lib/mkOciImage.nix
#
# Wraps `pkgs.dockerTools.streamLayeredImage` with our standard layout for
# xtcp2 OCI images.
#
# Conventions:
#   - All binaries land under /bin/
#   - Entrypoint defaults to /bin/xtcp2; override per-container at runtime with
#     `--entrypoint /bin/<other>`.
#   - The xtcp_flat_record.proto ships at /xtcp_flat_record.proto so
#     register_schema can load it without an extra mount.
#
{ pkgs, lib }:

{
  name,
  tag ? "latest",
  binaries, # derivation containing /bin/*
  protoFile ? null, # path to the .proto file to ship at /<basename>
  exposedPorts ? [ ],
  entrypoint ? "/bin/xtcp2",
  # Optional Docker HEALTHCHECK image-config block. Durations are integer
  # nanoseconds (Docker image-config convention), e.g.
  #   { Test = [ "CMD" "/bin/xtcp2" "-healthcheck" ]; Interval = 30000000000; }
  healthcheck ? null,
}:

let
  contents = [
    binaries
    # CA trust bundle. The image is otherwise scratch (binaries only), so
    # without this Go's crypto/x509 has no roots and HTTPS to real endpoints
    # — e.g. the s3parquet destination uploading to AWS S3 — fails with
    # "x509: certificate signed by unknown authority". dockerTools.caCertificates
    # installs the bundle at every standard path, including Go's default Linux
    # location /etc/ssl/certs/ca-certificates.crt. (The microVM tests never
    # caught this: they upload to in-VM MinIO over plain HTTP, so TLS is never
    # exercised.)
    pkgs.dockerTools.caCertificates
  ]
  ++ lib.optional (protoFile != null) (
    pkgs.runCommand "xtcp2-proto-payload" { } ''
      mkdir -p $out
      cp ${protoFile} $out/${baseNameOf (toString protoFile)}
    ''
  );

  exposedPortsAttr = lib.listToAttrs (
    map (p: {
      name = "${toString p}/tcp";
      value = { };
    }) exposedPorts
  );
in
pkgs.dockerTools.streamLayeredImage {
  inherit name tag contents;

  config = {
    Entrypoint = [ entrypoint ];
    ExposedPorts = exposedPortsAttr;
    # Point Go's crypto/x509 explicitly at the bundle shipped above — belt-and-
    # suspenders alongside the standard /etc/ssl/certs path, so TLS verification
    # works even if Go's default search paths ever change.
    Env = [ "SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt" ];
  }
  // lib.optionalAttrs (healthcheck != null) { Healthcheck = healthcheck; };
}
