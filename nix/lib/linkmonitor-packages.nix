# Standalone artifacts use the same pinned source/toolchain/runtime as the gates.
{
  pkgs,
  lib,
  src,
  vendoredSource,
}:
let
  versions = import ../versions.nix { inherit pkgs; };
  rdma = import ./linkmonitor-rdma.nix { inherit pkgs; };
  mkMonitor =
    full:
    pkgs.runCommand (if full then "go-link-monitor" else "go-link-monitor-core")
      {
        nativeBuildInputs = [ versions.go ] ++ lib.optionals full rdma.nativeBuildInputs;
        buildInputs = lib.optionals full rdma.buildInputs;
        meta = {
          mainProgram = "go-link-monitor";
          platforms = lib.platforms.linux;
          description = "Physical network link monitor with cached Prometheus metrics";
        };
      }
      ''
        set -euo pipefail
        cp -r ${vendoredSource} ./source
        chmod -R u+w ./source
        cd source
        export GOTOOLCHAIN=local GOFLAGS=-mod=vendor CGO_ENABLED=${if full then "1" else "0"}
        export GOCACHE="$TMPDIR/go-cache" GOMODCACHE="$TMPDIR/go-modcache"
        mkdir -p "$out/bin" "$out/share/go-link-monitor"
        go build ${lib.optionalString full "-tags rdma"} -trimpath \
          -ldflags '-X main.version=${lib.fileContents ../../VERSION} -X main.commit=${src.rev or "nix"} -X main.date=1970-01-01-00:00' \
          -o "$out/bin/go-link-monitor" ./cmd/go-link-monitor
        printf '%s\n' '${src}' > "$out/share/go-link-monitor/source.txt"
        ${lib.optionalString full ''ln -s ${rdma.runtime} "$out/share/go-link-monitor/rdma-runtime"''}
      '';
in
{
  go-link-monitor = mkMonitor true;
  go-link-monitor-core = mkMonitor false;
}
