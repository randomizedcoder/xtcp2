# nix/lib/goModules.nix
#
# Produces a derivation containing the xtcp2 Go module dependencies as a
# vendor/ tree. Reused by every Nix check that needs Go deps in the sandbox.
#
# The `vendorHash` MUST be updated after the first build. On a fresh checkout:
#   nix build .#xtcp2 2>&1 | grep 'got:.*sha256-' | head -1
# then paste the value into versions.nix's `goVendorHash` slot.
#
# Set vendorHash = null to skip vendoring (only works if vendor/ is committed).
#
{
  pkgs,
  lib,
  src,
  giouring,
  vendorHash,
}:

let
  patchGoMod = import ./patchGoMod.nix { inherit giouring; };
  versions = import ../versions.nix { inherit pkgs; };

  # buildGoModule exposes `goModules` — a derivation containing the populated
  # vendor/ tree.
  #
  # No `subPackages` restriction: we need the FULL module graph (every package
  # in the repo, including pkg/clickhouse_protolist, pkg/xtcp_config, etc.) so
  # that lint checks can type-check them. Restricting subPackages omits deps
  # of unbuilt packages from vendor/.
  parent = (pkgs.buildGoModule.override { inherit (versions) go; }) {
    pname = "xtcp2";
    version = "vendored";
    inherit src vendorHash;
    postPatch = patchGoMod;
    env.CGO_ENABLED = "0";
    doCheck = false;
  };
in
{
  inherit (parent) goModules;
  # Convenience: a writable source tree with vendor/ already populated.
  vendoredSource = pkgs.runCommand "xtcp2-vendored-source" { } ''
    cp -r ${src}/. $out
    chmod -R +w $out
    cp -r ${parent.goModules} $out/vendor
    cd $out
    ${patchGoMod}
  '';
}
