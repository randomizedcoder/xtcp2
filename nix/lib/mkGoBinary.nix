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
  # Subdirectory containing the main package. Defaults to "cmd/${name}"
  # for the historical/production binaries; explicit override lets us
  # build tools/* helpers (tcp_server, tcp_client, …) through the same
  # variant + flavor machinery without moving them out of tools/.
  subPath ? "cmd/${name}",
  variant ? "default",
  # Library destinations to compile into the binary. `null` (default) →
  # every destination (matches pre-build-tag behaviour). `[]` → stdlib
  # destinations only. A list of strings like `[ "kafka" ]` → just those.
  # Stdlib destinations (null/udp/unix/unixgram) are always compiled in.
  destinations ? null,
  vendorHash ? versions.goVendorHash,
  commit ? "nix",
  date ? "1970-01-01-00:00",
  version ? "0.0.0-nix",
  extraLdflags ? [ ],
  doCheck ? false,
  # When true, compile with `-cover` so the binary writes Go coverage
  # data to $GOCOVERDIR on exit. Used by the microvm lifecycle
  # coverage harness (nix/microvms/) to capture integration-test
  # coverage that unit tests alone can't reach.
  coverage ? false,
  # When coverage=true, the comma-separated package patterns whose code
  # gets instrumented. Defaults to the full xtcp2 namespace.
  coverPkg ? "github.com/randomizedcoder/xtcp2/...",
}:

let
  variantCfg =
    versions.buildVariants.${variant}
      or (throw "mkGoBinary: unknown variant '${variant}'; expected one of ${toString (builtins.attrNames versions.buildVariants)}");

  # Resolve the effective destination list.
  effectiveDestinations =
    if destinations == null then versions.allLibraryDestinations else destinations;

  # `dest_kafka`, `dest_nats`, … — one tag per included library destination.
  destinationTags = map (s: "dest_${s}") effectiveDestinations;

  # Short suffix encoding the destination set, used in pname so different
  # flavors produce distinct derivations. Empty for the default ("full") set
  # so the legacy `xtcp2-<variant>` names stay stable.
  destSuffix =
    if destinations == null then
      ""
    else if destinations == [ ] then
      "-min"
    else
      "-" + lib.concatStringsSep "-" destinations;
in
buildGoModule {
  pname = "${name}${destSuffix}${variantCfg.tagSuffix}${lib.optionalString coverage "-cover"}";
  inherit
    version
    src
    vendorHash
    doCheck
    ;

  subPackages = [ subPath ];

  postPatch = patchGoMod;

  env = {
    CGO_ENABLED = if versions.cgoEnabled then "1" else "0";
  };

  tags = versions.buildTags ++ destinationTags;

  ldflags =
    variantCfg.extraLdflags
    ++ [
      "-X main.commit=${commit}"
      "-X main.date=${date}"
      "-X main.version=${version}"
    ]
    ++ extraLdflags;

  # Strip and trim paths. When coverage=true, also append `-cover` +
  # `-coverpkg=<patterns>` so the binary writes per-package coverage
  # profiles to $GOCOVERDIR on clean exit.
  preBuild = ''
    export GOFLAGS="-trimpath ''${GOFLAGS:-}"
  ''
  + lib.optionalString coverage ''
    export GOFLAGS="-cover -coverpkg=${coverPkg} ''${GOFLAGS:-}"
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
