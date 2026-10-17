# The go-native RDMA capture script (rdma-native-capture.sh), with the tools it
# runs pinned. The rxe and ionic guests put it on their PATH and the host app
# capture-rdma-testdata runs it on `l`; see the script's header.
{ pkgs }:
pkgs.writeShellApplication {
  name = "rdma-native-capture";
  runtimeInputs = with pkgs; [
    coreutils
    gawk
    gnugrep
    gnused
    iproute2
    procps
    rdma-core
    strace
    tcpdump
  ];
  runtimeEnv.RDMA_CORE_VERSION = pkgs.rdma-core.version;
  text = builtins.readFile ./rdma-native-capture.sh;
}
