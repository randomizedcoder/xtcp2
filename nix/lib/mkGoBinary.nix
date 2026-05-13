# nix/lib/mkGoBinary.nix
#
# Builds a single Go binary from ./cmd/<name>/.
#
# Parameters:
#   name           — binary name (matches cmd/<name>/)
#   src            — source tree (typically the repo root)
#   commit, date,
#   version        — injected into main.{commit,date,version} via -ldflags -X
#   extraLdflags   — additional -ldflags entries
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
  vendorHash ? versions.goVendorHash,
  commit ? "nix",
  date ? "1970-01-01-00:00",
  version ? "0.0.0-nix",
  extraLdflags ? [ ],
  doCheck ? false,
}:

buildGoModule {
  pname = name;
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
    versions.ldflagsBase
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

  meta = with lib; {
    description = "xtcp2 ${name} — TCP socket introspection tooling";
    homepage = "https://github.com/randomizedcoder/xtcp2";
    license = licenses.mit;
    platforms = platforms.linux;
    mainProgram = name;
  };
}
