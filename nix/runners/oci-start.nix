# nix/runners/oci-start.nix
#
# Loads an xtcp2 OCI image into the local Docker daemon and starts a
# container from it. Exposed as `nix run .#oci-xtcp2-start`.
#
# `image` is a `dockerTools.streamLayeredImage` derivation: its store path
# IS an executable that streams a docker-loadable tarball on stdout, so we
# can pipe it straight to `docker load` without a prior `nix build`.
#
# Parameterized (image / tag / containerName / ports / daemonArgs) so a new
# flavor runner is a one-line addition in ./default.nix.
{
  pkgs,
  lib,
  image,
  tag ? "min",
  containerName ? "xtcp2",
  metricsPort ? 9088,
  grpcPort ? 8889,
  daemonArgs ? "-dest null -d 333",
}:

pkgs.writeShellApplication {
  name = "xtcp2-oci-start";
  runtimeInputs = with pkgs; [
    docker
    coreutils
  ];
  text = ''
    echo "==> removing any previous '${containerName}' container"
    docker rm -f ${containerName} >/dev/null 2>&1 || true

    echo "==> loading xtcp2:${tag} image into docker"
    ${image} | docker load

    # xtcp2 refuses to start unless at least one of its network-namespace
    # watch directories exists. A fresh container has neither, so we mount
    # empty tmpfs dirs to let the daemon boot. It will watch them and find
    # zero namespaces — enough for a boot + metrics smoke test. To observe
    # real host/container sockets, bind-mount the host's /run/netns and
    # /run/docker/netns instead (needs --network host + root; out of scope).
    echo "==> starting container '${containerName}'"
    docker run -d --name ${containerName} \
      -p ${toString metricsPort}:9088 \
      -p ${toString grpcPort}:8889 \
      --cap-add NET_ADMIN \
      --cap-add SYS_ADMIN \
      --tmpfs /run/netns \
      --tmpfs /run/docker/netns \
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
