# nix/runners/oci-verify.nix
#
# Verifies a running xtcp2 container started by `oci-xtcp2-start`:
#   1. the container is running,
#   2. it did not abort on the startup capability check, and
#   3. it is serving Prometheus metrics (an `xtcp_` family) on :metricsPort.
#
# Exposed as `nix run .#oci-xtcp2-verify`. Exits non-zero on any failure so
# it is usable as a scripted/CI gate.
{
  pkgs,
  lib,
  containerName ? "xtcp2",
  metricsPort ? 9088,
  timeoutSec ? 15,
}:

pkgs.writeShellApplication {
  name = "xtcp2-oci-verify";
  runtimeInputs = with pkgs; [
    docker
    curl
    coreutils
    gnugrep
  ];
  text = ''
    name="${containerName}"
    url="http://127.0.0.1:${toString metricsPort}/metrics"

    fail() {
      echo "FAIL: $1" >&2
      exit 1
    }

    echo "==> checking container '$name' is running"
    running="$(docker inspect -f '{{.State.Running}}' "$name" 2>/dev/null || true)"
    if [ "$running" != "true" ]; then
      fail "container '$name' is not running — start it first with: nix run .#oci-xtcp2-start"
    fi

    echo "==> checking the startup capability check passed"
    if docker logs "$name" 2>&1 | grep -q "cannot start"; then
      docker logs "$name" 2>&1 | grep -A4 "cannot start" >&2 || true
      fail "xtcp2 reported a fatal capability error (see above)"
    fi

    echo "==> polling $url for up to ${toString timeoutSec}s"
    deadline=$(( $(date +%s) + ${toString timeoutSec} ))
    while [ "$(date +%s)" -le "$deadline" ]; do
      if body="$(curl -fsS "$url" 2>/dev/null)"; then
        if printf '%s\n' "$body" | grep -q '^xtcp_'; then
          echo ""
          echo "PASS: container '$name' is up and serving xtcp_ metrics."
          printf '%s\n' "$body" | grep '^xtcp_' | head -n 3
          exit 0
        fi
      fi
      sleep 1
    done

    fail "metrics endpoint did not return xtcp_ metrics within ${toString timeoutSec}s"
  '';
}
