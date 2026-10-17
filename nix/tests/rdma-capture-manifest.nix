# rdma-capture-manifest: redacts an RDMA capture stage and writes or verifies
# its manifest.json (pkg/linkmonitor/internal/rdmacapture). Standard library
# only, built from the vendored source like the monitor.
{
  pkgs,
  vendoredSource,
}:
let
  versions = import ../versions.nix { inherit pkgs; };
in
pkgs.runCommand "rdma-capture-manifest"
  {
    nativeBuildInputs = [ versions.go ];
    meta.mainProgram = "rdma-capture-manifest";
  }
  ''
    cp -r ${vendoredSource} ./source
    chmod -R u+w ./source
    cd source
    export GOTOOLCHAIN=local GOFLAGS=-mod=vendor CGO_ENABLED=0
    export GOCACHE="$TMPDIR/go-cache" GOMODCACHE="$TMPDIR/go-modcache"
    mkdir -p "$out/bin"
    go build -trimpath -o "$out/bin/rdma-capture-manifest" \
      ./pkg/linkmonitor/internal/rdmacapture/cmd/rdma-capture-manifest
  ''
