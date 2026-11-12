# nix/lib/sbom.nix
#
# A CycloneDX SBOM for a Go binary package: one <binary>.cdx.json per file in
# its bin/, read by `cyclonedx-gomod bin` from the module list the Go linker
# embeds, so it names exactly the module versions that were linked in.
#
#   nix build .#sbom-xtcp2-s3parquet
#
{
  pkgs,
  versions,
}:

drv:
pkgs.runCommand "${drv.pname or drv.name}-sbom"
  {
    nativeBuildInputs = [ versions.cyclonedx-gomod ];
  }
  ''
    mkdir -p $out
    for b in ${drv}/bin/*; do
      cyclonedx-gomod bin -json -version ${drv.version or "devel"} \
        -output "$out/$(basename "$b").cdx.json" "$b"
    done
  ''
