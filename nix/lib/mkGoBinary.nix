# nix/lib/mkGoBinary.nix
#
# Builds a single Go binary from ./cmd/<name>/.
#
# Parameters:
#   name           — binary name (matches cmd/<name>/)
#   src            — source tree (typically the repo root)
#   variant        — one of "debug", "default", "stripped" (see versions.nix
#                    buildVariants for the trade-offs). Drives:
#                      - ldflags (whether `-s -w` are appended)
#                      - postFixup strip(1) pass
#                      - the derivation `pname` suffix
#   commit, date,
#   version        — injected into main.{commit,date,version} via -ldflags -X
#   extraLdflags   — additional -ldflags entries appended after the variant's
#   doCheck        — run `go test ./...` during build (default: false; we run
#                    tests as separate Nix checks instead, mirroring gosrt)
#
{
  pkgs,
  lib,
  giouring,
}:

let
  versions = import ../versions.nix { inherit pkgs; };
  patchGoMod = import ./patchGoMod.nix { inherit giouring; };

  buildGoModule = pkgs.buildGoModule;
in
{
  name,
  src,
  variant ? "default",
  vendorHash ? versions.goVendorHash,
  commit ? "nix",
  date ? "1970-01-01-00:00",
  version ? "0.0.0-nix",
  extraLdflags ? [ ],
  doCheck ? false,
}:

let
  variantCfg =
    versions.buildVariants.${variant}
      or (throw "mkGoBinary: unknown variant '${variant}'; expected one of ${
        toString (builtins.attrNames versions.buildVariants)
      }");
in
buildGoModule {
  pname = "${name}${variantCfg.tagSuffix}";
  inherit
    version
    src
    vendorHash
    doCheck
    ;

  subPackages = [ "cmd/${name}" ];

  postPatch = patchGoMod;

  env = {
    CGO_ENABLED = if versions.cgoEnabled then "1" else "0";
  };

  tags = versions.buildTags;

  ldflags =
    variantCfg.extraLdflags
    ++ [
      "-X main.commit=${commit}"
      "-X main.date=${date}"
      "-X main.version=${version}"
    ]
    ++ extraLdflags;

  # Strip and trim paths
  preBuild = ''
    export GOFLAGS="-trimpath ''${GOFLAGS:-}"
  '';

  # Filippo's trick: `strip` after -s -w shaves a bit more off. Only applied
  # to the "stripped" variant. Other variants explicitly disable Nix's
  # automatic strip so the debug variant keeps its symbols.
  dontStrip = !variantCfg.doStrip;
  postFixup = lib.optionalString variantCfg.doStrip ''
    for bin in $out/bin/*; do
      ${pkgs.binutils-unwrapped}/bin/strip --strip-all "$bin"
    done
  '';

  meta = with lib; {
    description = "xtcp2 ${name} (${variant}) — TCP socket introspection tooling";
    homepage = "https://github.com/randomizedcoder/xtcp2";
    license = licenses.mit;
    platforms = platforms.linux;
    mainProgram = name;
  };
}
