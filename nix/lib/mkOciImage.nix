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
  }
  // lib.optionalAttrs (healthcheck != null) { Healthcheck = healthcheck; };
}
