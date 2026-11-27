# nix/microvms/golden-ernic.nix
#
# The golden-record VM's rocm-ernic NICs, read by mkVm.nix (QEMU devices and
# guest network config) and by the capture runner (ernic-run-many). See
# docs/design-rocm-ernic-nics.md.
#
# - ionic0 is uplink 1: its default route (metric 512) ranks ahead of the
#   e1000e's (2048) and virtio eth0's (4096), so DefaultUplinks(2) records
#   ionic0 and eth1.
# - ionic1 is unmanaged; the workload moves it into netns golden-ernic-b
#   (10.0.4.16-17) for a bulk transfer from the root namespace over ionic0.
# - Deterministic loss: ionic0 drops every 50th frame it transmits and
#   ionic1 every 64th frame it would receive. rocm-ernic counts events,
#   not time, so the same frames give the same drops.
# - Both TAPs join br-ernic in the fabric namespace, which holds the
#   gateway, so the two NICs reach each other only through the emulator.
# - ionic2 and ionic3, both unmanaged, carry one cubic 4 MiB echo from
#   netns golden-ernic-c to golden-ernic-d, on their own bridge br-ernic2
#   so no frame of the first pair reaches them. ionic2 drops a fixed
#   sequence of the frames it transmits: frame 501 alone, 1002-1004 and
#   1505-1512, 12 in all, and nothing after for the rest of the transfer
#   (about 3100 frames).
#   A sequence's 1xK and 0xK repeat K (decimal) times (docs/ionic.rst
#   upstream).
# - MACs and PCI slots are fixed (high slots, clear of QEMU's automatic
#   ones), so the NIC columns are the same on every capture.
{ ernic }:

let
  profiles = [ "${ernic.ownProfiles}/golden-uplink.ini" ];
  macA = "02:00:00:00:20:01";
  macB = "02:00:00:00:20:02";
  macC = "02:00:00:00:20:03";
  macD = "02:00:00:00:20:04";
  txEvery = 50;
  rxEvery = 64;
  seq = "0x500,1x1,0x500,1x3,0x500,1x8,0x60000";
  seqDrops = 12;
in
{
  nics = [
    {
      name = "ionic0";
      mac = macA;
      slot = "0x1d";
      inherit profiles;
      tap = "tap-ionic0";
      bridge = "br-ernic";
      inject = [ "eth_tx: every ${toString txEvery} effect drop" ];
      network = {
        address = "10.0.4.15/24";
        gateway = "10.0.4.1";
        metric = 512;
      };
    }
    {
      name = "ionic1";
      mac = macB;
      slot = "0x1e";
      inherit profiles;
      tap = "tap-ionic1";
      bridge = "br-ernic";
      inject = [ "eth_rx: every ${toString rxEvery} effect drop" ];
    }
    {
      name = "ionic2";
      mac = macC;
      slot = "0x1b";
      inherit profiles;
      tap = "tap-ionic2";
      bridge = "br-ernic2";
      inject = [ "eth_tx: sequence \"${seq}\" effect drop" ];
    }
    {
      name = "ionic3";
      mac = macD;
      slot = "0x1c";
      inherit profiles;
      tap = "tap-ionic3";
      bridge = "br-ernic2";
    }
  ];
  fabric.br-ernic = {
    address = "10.0.4.1/24";
    mac = "02:00:00:00:20:fe";
  };
  # Variables for golden-workload.sh and golden-capture.sh.
  workloadEnv = {
    ERNIC_A_MAC = macA;
    ERNIC_B_MAC = macB;
    ERNIC_C_MAC = macC;
    ERNIC_D_MAC = macD;
  };
  captureEnv = {
    ERNIC_TX_EVERY = toString txEvery;
    ERNIC_SEQ_DROPS = toString seqDrops;
  };
  # ernic-oracle, after the VM stops (nix/ernic/oracle.nix): each fault site
  # against the guest's counters, and the frames against the TCP segments
  # xtcp2 recorded.
  oracleArgs = [
    "--tx"
    "ionic0"
    "--rx"
    "ionic1"
    "--segs-out"
    "ionic0"
    "--segs-in"
    "ionic1"
    "--tx"
    "ionic2"
    "--segs-out"
    "ionic2"
    "--segs-in"
    "ionic3"
  ];
}
