# Shared Linux RDMA build inputs and runtime closure for library/command builds.
{ pkgs }:
{
  nativeBuildInputs = [
    pkgs.gcc
    pkgs.pkg-config
  ];
  buildInputs = [ pkgs.rdma-core ];
  tags = [ "rdma" ];
  runtime = pkgs.symlinkJoin {
    name = "xtcp2-linkmonitor-rdma-runtime";
    paths = [ pkgs.rdma-core ];
  };
}
