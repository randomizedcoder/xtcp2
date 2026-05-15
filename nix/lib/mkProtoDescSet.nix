# nix/lib/mkProtoDescSet.nix
#
# Build a protobuf FileDescriptorSet (.desc) from a .proto file. Consumers
# that need to decode protobuf bytes at runtime — notably Vector's
# `decoding.protobuf.desc_file` — load this descriptor set to resolve
# message types from a wire-format payload.
#
# Output layout:
#   $out/share/xtcp2/<name>.desc
#
{
  pkgs,
  lib,
  src,
}:

{
  # Display name; also the basename of the produced .desc file.
  name,
  # Proto file path relative to repo root, e.g. "proto/xtcp_flat_record/v1/xtcp_flat_record.proto".
  protoFile,
  # Proto import roots passed to protoc -I. Defaults to the repo's top-level proto/ dir.
  protoPaths ? [ "proto" ],
}:

pkgs.stdenvNoCC.mkDerivation {
  pname = "${name}-desc";
  version = "1";
  inherit src;
  nativeBuildInputs = [ pkgs.protobuf ];

  buildPhase = ''
    runHook preBuild
    mkdir -p $out/share/xtcp2
    protoc \
      ${lib.concatStringsSep " " (map (p: "-I${p}") protoPaths)} \
      --include_imports \
      --include_source_info \
      --descriptor_set_out=$out/share/xtcp2/${name}.desc \
      ${protoFile}
    runHook postBuild
  '';

  dontInstall = true;
  dontFixup = true;
}
