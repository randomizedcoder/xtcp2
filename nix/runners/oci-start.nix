# nix/runners/oci-start.nix
#
# Loads an xtcp2 OCI image into the local Docker daemon and starts a
# container from it. Exposed as `nix run .#oci-xtcp2-start`.
#
# `image` is a `dockerTools.streamLayeredImage` derivation: its store path
# IS an executable that streams a docker-loadable tarball on stdout, so we
# can pipe it straight to `docker load` without a prior `nix build`.
#
# Socket visibility — xtcp2 only polls namespaces it discovers as files
# under /run/netns and /run/docker/netns (it setns() into each every poll;
# its transient "default" netlinker is reconciled away after the first
# cycle, so a bare/host-network container sees sockets only once). To watch
# real sockets *continuously*:
#
#   hostVisibility = true  → bind-mount the host's /run/netns and
#     /run/docker/netns into the container. xtcp2 then polls the host root
#     namespace (exposed via `ip netns attach`) plus every container/pod
#     namespace Docker creates. This needs one-time privileged host setup,
#     which the runner PRINTS (it never calls sudo itself); it refuses to
#     start until the host root netns has been attached.
#
#   hostVisibility = false → empty tmpfs watch dirs; a no-sudo boot smoke
#     test that observes only the (near-empty) container namespace.
#
# Parameterized (image / tag / name / ports / args / visibility) so a
# runner for another image flavor is a one-line addition in ./default.nix.
{
  pkgs,
  lib,
  image,
  name ? "xtcp2-oci-start",
  tag ? "min",
  containerName ? "xtcp2",
  metricsPort ? 9088,
  grpcPort ? 8889,
  daemonArgs ? "-dest null -d 333",
  hostVisibility ? false,
  hostNsName ? "xtcp2host",
}:

let
  portArgs = "-p ${toString metricsPort}:9088 -p ${toString grpcPort}:8889";
  watchDirArgs =
    if hostVisibility then
      "-v /run/netns:/run/netns:ro -v /run/docker/netns:/run/docker/netns:ro"
    else
      "--tmpfs /run/netns --tmpfs /run/docker/netns";
in
pkgs.writeShellApplication {
  inherit name;
  runtimeInputs = with pkgs; [
    docker
    coreutils
  ];
  text = ''
    ${lib.optionalString hostVisibility ''
      echo "=== xtcp2 host + container socket visibility ==="
      echo ""
      echo "One-time privileged prerequisites (run these as root, then re-run this command):"
      echo "  sudo mkdir -p /run/netns"
      echo "  sudo ip netns attach ${hostNsName} 1   # expose the host root netns so xtcp2 polls host sockets"
      echo "  # /run/docker/netns is created by Docker automatically; per-container"
      echo "  # namespaces appear there as soon as a non-host-network container runs."
      echo ""
      if [ ! -e /run/netns/${hostNsName} ]; then
        echo "ERROR: /run/netns/${hostNsName} not found — the host root netns must be"
        echo "attached BEFORE the container starts. Run the commands above, then re-run."
        exit 1
      fi
    ''}
    echo "==> removing any previous '${containerName}' container"
    docker rm -f ${containerName} >/dev/null 2>&1 || true

    echo "==> loading xtcp2:${tag} image into docker"
    ${image} | docker load

    echo "==> starting container '${containerName}'"
    docker run -d --name ${containerName} \
      ${portArgs} \
      --cap-add NET_ADMIN \
      --cap-add SYS_ADMIN \
      ${watchDirArgs} \
      xtcp2:${tag} ${daemonArgs} "$@"

    echo ""
    echo "xtcp2 started as container '${containerName}'."
    echo "  metrics: http://127.0.0.1:${toString metricsPort}/metrics"
    echo "  gRPC:    127.0.0.1:${toString grpcPort}"
    echo "  logs:    docker logs -f ${containerName}"
    echo "  verify:  nix run .#oci-xtcp2-verify"
    echo "  stop:    docker rm -f ${containerName}"
  '';
}
