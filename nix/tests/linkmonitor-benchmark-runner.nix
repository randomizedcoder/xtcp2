{ pkgs, artifact }:
pkgs.writeShellApplication {
  name = "bench-linkmonitor";
  runtimeInputs = [
    pkgs.python3
    pkgs.goperf
    pkgs.coreutils
  ];
  text = ''
    export LINKMONITOR_BENCH_ARTIFACT=${artifact}
    exec python3 ${artifact}/runner.py "$@"
  '';
}
