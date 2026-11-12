# nix/nlcap-lwt-bpf/default.nix
#
# The workbench's BPF lwt object, $out/lwt_bpf.o: three pass-through
# programs in sections lwt_in, lwt_out and lwt_xmit (lwt_bpf.c). Handed to
# the guest by `nlcap-ws batch --bpf $(nix build --print-out-paths
# .#nlcap-lwt-bpf)/lwt_bpf.o`, which links it at /run/nlcap-ws/lwt_bpf.o for
# the bpfroute rows (NC8-T02 of docs/netlink/goip-parity-next.md). The
# unwrapped clang: the wrapper's hardening flags mean nothing to -target bpf.
{ pkgs }:
pkgs.stdenvNoCC.mkDerivation {
  pname = "nlcap-lwt-bpf";
  version = "1";
  src = ./.;
  nativeBuildInputs = [
    pkgs.llvmPackages.clang-unwrapped
    pkgs.llvmPackages.bintools-unwrapped
  ];
  buildPhase = ''
    runHook preBuild
    clang -target bpf -O2 -g -Wall -Werror -nostdinc \
      -isystem ${pkgs.linuxHeaders}/include \
      -c lwt_bpf.c -o lwt_bpf.o
    runHook postBuild
  '';
  doCheck = true;
  checkPhase = ''
    runHook preCheck
    for s in lwt_in lwt_out lwt_xmit license; do
      llvm-objdump -h lwt_bpf.o | grep -qw "$s" || { echo "no section $s"; exit 1; }
    done
    runHook postCheck
  '';
  installPhase = ''
    runHook preInstall
    install -Dm444 lwt_bpf.o $out/lwt_bpf.o
    runHook postInstall
  '';
}
