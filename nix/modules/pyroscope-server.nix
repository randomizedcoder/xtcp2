#
# In-VM Pyroscope server for continuous-profiling integration tests.
#
# Brings up the Grafana Pyroscope OSS server bound to 0.0.0.0:4040 so
# both the in-VM xtcp2 agent and (when hostfwd works) host-side
# operators can reach it. Data lives on tmpfs — the VM's lifetime is
# the data lifetime, which matches the soak-test budget.
#
# Used by the s3parquet-long microvm flavor. Operators wanting a
# durable Pyroscope deployment should run pyroscope under
# docker-compose or Grafana Cloud Pyroscope instead.
#
{
  port ? 14040,
  dataDir ? "/var/lib/pyroscope",
}:

{
  config,
  lib,
  pkgs,
  ...
}:

{
  services.pyroscope = {
    enable = true;
    settings = {
      server = {
        http_listen_address = "0.0.0.0";
        http_listen_port = port;
      };
      # Single-node "all-in-one" config — keeps the binary self-
      # contained without needing external object storage. Suitable
      # for short-lived soak runs.
      target = "all";
      # Filesystem storage — default is S3-like blocks-storage which
      # needs external object-store config; without storage.backend
      # set, pyroscope fails on startup with no actionable error.
      storage = {
        backend = "filesystem";
        filesystem.dir = "${dataDir}/blocks";
      };
    };
  };

  # Override the unit:
  #   - Drop DynamicUser so writes to /var/lib/pyroscope/blocks
  #     succeed without ownership choreography.
  #   - Loosen ProtectSystem so pyroscope can create its data dir.
  #   - Surface stderr/stdout on the serial console (the nixpkgs
  #     unit defaults to journal-only, hiding the crash reason).
  #   - Add a brief RestartSec so a 100 ms restart loop doesn't
  #     burn through systemd's start-rate-limit before pyroscope
  #     can finish its ~5 s startup sequence.
  systemd.services.pyroscope.serviceConfig = {
    DynamicUser = lib.mkForce false;
    User = lib.mkForce "root";
    ProtectSystem = lib.mkForce "full";
    StandardOutput = lib.mkForce "journal+console";
    StandardError = lib.mkForce "journal+console";
    RestartSec = lib.mkForce "5s";
  };
}
