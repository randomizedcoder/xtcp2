# Standalone artifacts use the same pinned source/toolchain as the gates.
# One CGO_ENABLED=0 build: RDMA events come from netlink and InfiniBand
# PortInfo from the umad device, so nothing links rdma-core. The core name is
# kept as an alias of the same derivation for existing references.
{
  pkgs,
  lib,
  src,
  vendoredSource,
}:
let
  versions = import ../versions.nix { inherit pkgs; };
  monitor =
    pkgs.runCommand "go-link-monitor"
      {
        nativeBuildInputs = [ versions.go ];
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
        export GOTOOLCHAIN=local GOFLAGS=-mod=vendor CGO_ENABLED=0
        export GOCACHE="$TMPDIR/go-cache" GOMODCACHE="$TMPDIR/go-modcache"
        mkdir -p "$out/bin" "$out/share/go-link-monitor"
        go build -trimpath \
          -ldflags '-X main.version=${lib.fileContents ../../VERSION} -X main.commit=${src.rev or "nix"} -X main.date=1970-01-01-00:00' \
          -o "$out/bin/go-link-monitor" ./cmd/go-link-monitor
        # Provenance only: the path is written without its store reference,
        # so the source tree stays out of the runtime closure.
        printf '%s\n' '${builtins.unsafeDiscardStringContext "${src}"}' > "$out/share/go-link-monitor/source.txt"
      '';
in
{
  go-link-monitor = monitor;
  go-link-monitor-core = monitor;
}
