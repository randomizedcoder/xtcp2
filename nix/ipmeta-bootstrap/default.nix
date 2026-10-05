{
  pkgs,
  lib,
  ipmetaBootstrapTool,
  lockFile ? ../ipmeta-bootstrap-lock.json,
}:

let
  lockExists = builtins.pathExists lockFile;
  lock = if lockExists then builtins.fromJSON (builtins.readFile lockFile) else { };
  artifact = lock.artifact or null;
  lockDir = builtins.dirOf lockFile;
  missing =
    message:
    pkgs.runCommand "xtcp2-ipmeta-bootstrap-artifact-missing" { } ''
      echo ${lib.escapeShellArg message} >&2
      exit 1
    '';
in
if !lockExists then
  missing ''
    missing ${toString lockFile}

    Create it with an artifact entry pointing at a checked-in file:
      {
        "version": 1,
        "artifact": {
          "path": "./ipmeta/bootstrap.lookup.parquet.zst"
        }
      }

    Or point at a remote immutable/fixed-hash artifact:
      {
        "version": 1,
        "artifact": {
          "url": "https://github.com/randomizedcoder/xtcp2/releases/download/ipmeta-bootstrap-YYYYMMDD/bootstrap.lookup.parquet.zst",
          "hash": "sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
        }
      }
  ''
else if artifact == null then
  missing "ipmeta bootstrap lock has no artifact entry: ${toString lockFile}"
else
  let
    source =
      if artifact ? path then
        builtins.path {
          path = lockDir + "/${artifact.path}";
          name = baseNameOf artifact.path;
        }
      else if artifact ? url && artifact ? hash then
        pkgs.fetchurl {
          inherit (artifact) url hash;
        }
      else if artifact ? url && artifact ? sha256 then
        pkgs.fetchurl {
          inherit (artifact) url sha256;
        }
      else
        missing "ipmeta bootstrap artifact needs either { path = ...; } or { url = ...; hash = ...; }";
  in
  pkgs.runCommand "xtcp2-ipmeta-bootstrap-artifact"
    {
      nativeBuildInputs = [ ipmetaBootstrapTool ];
      passthru = {
        inherit lockFile;
        source = artifact;
      };
    }
    ''
      tmp="$TMPDIR/bootstrap.lookup.parquet.zst"
      ipmeta-bootstrap -in ${source} -out "$tmp"
      cp "$tmp" "$out"
    ''
