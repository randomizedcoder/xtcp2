# nix/tests/oci-link-monitor-env.nix
#
# The xtcp2-daemon images run the embedded link monitor on demand: their image
# config sets LINK_MONITOR_COLLECTION_MODE=on-demand. The client images, which
# run no monitor, must not carry it. Reads each image's config JSON (the
# `conf` passthru of streamLayeredImage), so no image is streamed.
{
  pkgs,
  lib,
  containers,
}:

let
  want = "LINK_MONITOR_COLLECTION_MODE=on-demand";
  # The fat image, the slim stdlib cell and the historical unsuffixed cell.
  daemons = [
    "oci-xtcp2"
    "oci-xtcp2-min"
    "oci-xtcp2-s3parquet"
  ];
  clients = [
    "oci-xtcp2client"
    "oci-xtcp2ctl"
  ];
  check =
    expect: name:
    let
      found = "jq -e --arg want '${want}' '.config.Env | index($want) != null' ${containers.${name}.conf} > /dev/null";
    in
    if expect then
      ''${found} || { echo "${name}: image Env lacks ${want}" >&2; exit 1; }''
    else
      ''! ${found} || { echo "${name}: client image sets ${want}" >&2; exit 1; }'';
in
pkgs.runCommand "xtcp2-test-oci-link-monitor-env" { nativeBuildInputs = [ pkgs.jq ]; } ''
  set -eu
  ${lib.concatMapStringsSep "\n" (check true) daemons}
  ${lib.concatMapStringsSep "\n" (check false) clients}
  touch "$out"
''
