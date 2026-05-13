# nix/microvms/constants.nix
#
# Architecture and VM-runtime constants.
#
# To add a new architecture later: append to `supportedArchs` and add a matching
# entry to `architectures`. The rest of the microvm/ tree consumes this purely
# data-driven.
#
{
  # v1: x86_64-linux only. io_uring (kernel 6.0+) and netlink work on all three
  # arches; adding aarch64/riscv64 is one line here + an architectures entry.
  supportedArchs = [ "x86_64" ];

  # Polling cadence used by lifecycle scripts (seconds between probes)
  pollInterval = 2;

  architectures = {
    x86_64 = {
      hostname = "xtcp2-vm-x86_64";
      qemuMachine = "pc";
      qemuCpu = null; # null => microvm.nix selects -enable-kvm -cpu host
      useKvm = true;
      mem = 1024;
      vcpu = 2;
      serialPort = 12055;
      virtioPort = 12056;
      promPort = 9088;
      grpcPort = 8889;
    };
  };
}
