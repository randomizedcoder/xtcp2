# nix/microvms/golden-ernic.nix
#
# The golden-record VM's rocm-ernic NICs, read by mkVm.nix (QEMU devices and
# guest network config) and by the capture runner (ernic-run-many). See
# docs/design-rocm-ernic-nics.md.
#
# - ionic0 is uplink 1: its default route (metric 512) ranks ahead of the
#   e1000e's (2048) and virtio eth0's (4096), so DefaultUplinks(2) records
#   ionic0 and eth1.
# - ionic1 is unmanaged; the workload configures it.
# - Both TAPs join br-ernic in the fabric namespace, which holds the
#   gateway, so the two NICs reach each other only through the emulator.
# - MACs and PCI slots are fixed (high slots, clear of QEMU's automatic
#   ones), so the NIC columns are the same on every capture.
{ ernic }:

let
  profiles = [ "${ernic.ownProfiles}/golden-uplink.ini" ];
in
{
  nics = [
    {
      name = "ionic0";
      mac = "02:00:00:00:20:01";
      slot = "0x1d";
      inherit profiles;
      tap = "tap-ionic0";
      bridge = "br-ernic";
      network = {
        address = "10.0.4.15/24";
        gateway = "10.0.4.1";
        metric = 512;
      };
    }
    {
      name = "ionic1";
      mac = "02:00:00:00:20:02";
      slot = "0x1e";
      inherit profiles;
      tap = "tap-ionic1";
      bridge = "br-ernic";
    }
  ];
  fabric.br-ernic = {
    address = "10.0.4.1/24";
    mac = "02:00:00:00:20:fe";
  };
}
