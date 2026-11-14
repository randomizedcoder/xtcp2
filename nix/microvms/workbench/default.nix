# nix/microvms/workbench/default.nix
#
# The nl_mon capture workbench's two halves (docs/netlink/coverage-status.md,
# "Workbench and generated rows"):
#
#   batch  xtcp2-nlcap-batch, the guest-side rows runner (nlcap_batch.py)
#   cli    xtcp2-nlcap-ws, the host CLI (xtcp2-nlcap-ws.sh), `nix run .#nlcap-ws`
#
# Neither is in the VM's closure. The guest mounts the host's /nix/store, so
# the CLI hands it store paths: the batch runner and the capture helper
# (netlink-capture.nix) can change and be picked up by the next call without
# a reboot. See mkVm.nix isNlcapWorkbench.
{
  pkgs,
  lib,
  vm,
  netlinkCaptureHelper,
  # constants.architectures.<arch>
  cfg,
}:
let
  batch = pkgs.writers.writePython3Bin "xtcp2-nlcap-batch" { } (builtins.readFile ./nlcap_batch.py);

  cli = pkgs.writeShellApplication {
    name = "xtcp2-nlcap-ws";
    runtimeInputs = with pkgs; [
      coreutils
      gnugrep
      gnused
      gnutar
      gzip
      iproute2
      jq
      openssh
      util-linux
    ];
    text =
      lib.replaceStrings
        [
          "@VM_RUN@"
          "@NLCAP@"
          "@BATCH@"
          "@SSH_PORT@"
          "@SERIAL_PORT@"
          "@VIRTIO_PORT@"
          "@HOSTNAME@"
        ]
        [
          "${vm}/bin/microvm-run"
          "${netlinkCaptureHelper}"
          "${batch}"
          (toString cfg.nlcapWs.sshPort)
          (toString cfg.nlcapWs.serialPort)
          (toString cfg.nlcapWs.virtioPort)
          cfg.nlcapWs.hostname
        ]
        (builtins.readFile ./xtcp2-nlcap-ws.sh);
  };

  # Hermetic tests: the batch runner's table (test_nlcap_batch.py, stubs for
  # the helper and ip) and the CLI's argument handling, which refuses a bad
  # `run` before it needs a VM. No VM, no root, no network.
  check =
    pkgs.runCommand "nlcap-ws-check"
      {
        nativeBuildInputs = [
          pkgs.python3
          pkgs.jq
          cli
        ];
        src = lib.fileset.toSource {
          root = ./.;
          fileset = lib.fileset.unions [
            ./nlcap_batch.py
            ./test_nlcap_batch.py
            ./cli-test.sh
          ];
        };
      }
      ''
        cp -r $src/. .
        chmod -R u+w .
        export HOME=$PWD
        python3 -I test_nlcap_batch.py
        bash cli-test.sh
        touch $out
      '';
in
{
  inherit batch cli check;
}
