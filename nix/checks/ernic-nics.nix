# nix/checks/ernic-nics.nix
#
# Pure-evaluation table test for nix/ernic/nics.nix, the functions that turn
# a VM's list of ionic NICs into QEMU args, rocm-ernic server args and guest
# network config. Row keys are "test <category>: <description>"; the comment
# above each row is its name.
{ pkgs, lib }:

let
  nics = import ../ernic/nics.nix { inherit lib; };
  profile = "/profiles/golden-uplink.ini";
  ionic0 = {
    name = "ionic0";
    mac = "02:00:00:00:20:01";
    slot = "0x0a";
    profiles = [ profile ];
    network = {
      address = "10.0.4.15/24";
      gateway = "10.0.4.1";
      metric = 512;
    };
  };
  ionic1 = ionic0 // {
    name = "ionic1";
    mac = "02:00:00:00:20:02";
    slot = "0x0b";
    network = null;
  };
  throws = e: !(builtins.tryEval (builtins.deepSeq e e)).success;
  # The message of a throw, via the validation result rather than tryEval
  # (which hides it).
  problems = l: nics.problems l;
  failures = lib.runTests {
    # one_nic
    "test positive: one NIC gives one JSON vfio-user-pci device" = {
      expr = nics.qemuArgs [ ionic0 ];
      expected = [
        "-device"
        ''{"addr":"0x0a","driver":"vfio-user-pci","id":"ionic0","socket":{"path":"ernic-ionic0.sock","type":"unix"}}''
      ];
    };
    # server_args
    "test positive: MAC, profiles, tap and faults reach the server in order" = {
      expr = nics.serverArgs (
        ionic0
        // {
          tap = "tap-ionic0";
          bridge = "br-ernic";
          inject = [ "eth_tx: every 50 effect drop" ];
        }
      );
      expected = [
        "--socket"
        "ernic-ionic0.sock"
        "--mac"
        "02:00:00:00:20:01"
        "--nic-profile"
        profile
        "--tap"
        "tap-ionic0"
        "--inject"
        "eth_tx: every 50 effect drop"
      ];
    };
    # guest_link
    "test positive: the guest names the NIC by MAC and routes at its metric" = {
      expr =
        let
          c = nics.guestConfig [ ionic0 ];
          n = c.systemd.network.networks."10-ernic-ionic0";
        in
        {
          modules = c.boot.kernelModules;
          link = c.systemd.network.links."10-ernic-ionic0";
          inherit (n) address routes;
          lll = n.networkConfig.LinkLocalAddressing;
        };
      expected = {
        modules = [ "ionic" ];
        link = {
          matchConfig.PermanentMACAddress = "02:00:00:00:20:01";
          linkConfig.Name = "ionic0";
        };
        address = [ "10.0.4.15/24" ];
        routes = [
          {
            Gateway = "10.0.4.1";
            Metric = 512;
          }
        ];
        lll = "no";
      };
    };
    # duplicate_name
    "test negative: names are unique" = {
      expr = problems [
        ionic0
        (ionic1 // { name = "ionic0"; })
      ];
      expected = [ "duplicate ernic NIC name ionic0" ];
    };
    # duplicate_slot
    "test negative: PCI slots are unique" = {
      expr = problems [
        ionic0
        (ionic1 // { slot = "0x0a"; })
      ];
      expected = [ "ernic NICs ionic0 and ionic1 share PCI slot 0x0a" ];
    };
    # duplicate_mac
    "test negative: MACs are unique" = {
      expr = problems [
        ionic0
        (ionic1 // { inherit (ionic0) mac; })
      ];
      expected = [ "ernic NICs ionic0 and ionic1 share MAC 02:00:00:00:20:01" ];
    };
    # bad_name
    "test negative: a name must be a short ifname and one shell word" = {
      expr = map (n: problems [ (ionic0 // { name = n; }) ]) [
        "a b"
        "ionic0123456789x"
        "Ionic0"
      ];
      expected = [
        [ "invalid ernic NIC name \"a b\"" ]
        [ "invalid ernic NIC name \"ionic0123456789x\"" ]
        [ "invalid ernic NIC name \"Ionic0\"" ]
      ];
    };
    # invalid_throws
    "test negative: qemuArgs refuses an invalid list" = {
      expr = throws (nics.qemuArgs [
        ionic0
        ionic0
      ]);
      expected = true;
    };
    # empty
    "test boundary: no NICs adds nothing" = {
      expr = {
        args = nics.qemuArgs [ ];
        config = nics.guestConfig [ ];
      };
      expected = {
        args = [ ];
        config = { };
      };
    };
    # name_15
    "test boundary: a 15-character name (IFNAMSIZ - 1) is accepted" = {
      expr = problems [ (ionic0 // { name = "ionic0123456789"; }) ];
      expected = [ ];
    };
    # no_tap
    "test corner: a NIC without a peer has no --tap" = {
      expr = builtins.elem "--tap" (nics.serverArgs ionic0);
      expected = false;
    };
    # bridge_without_tap
    "test corner: a bridge needs a tap" = {
      expr = problems [ (ionic0 // { bridge = "br-ernic"; }) ];
      expected = [ "ernic NIC ionic0 has a bridge but no tap" ];
    };
    # unmanaged. Without its own .network, NixOS's 99-ethernet-default-dhcp
    # would run DHCP on it.
    "test corner: network = null names the NIC and marks it unmanaged" = {
      expr =
        let
          c = nics.guestConfig [ ionic1 ];
        in
        {
          link = c.systemd.network.links ? "10-ernic-ionic1";
          network = c.systemd.network.networks."10-ernic-ionic1";
        };
      expected = {
        link = true;
        network = {
          matchConfig.Name = "ionic1";
          linkConfig.Unmanaged = true;
        };
      };
    };
    # bridges
    "test corner: bridges lists each bridge once with its taps" = {
      expr = nics.bridges [
        (ionic0 // {
          tap = "tap-ionic0";
          bridge = "br-ernic";
        })
        (ionic1 // {
          tap = "tap-ionic1";
          bridge = "br-ernic";
        })
      ];
      expected = {
        br-ernic = [
          "tap-ionic0"
          "tap-ionic1"
        ];
      };
    };
  };
in
if failures != [ ] then
  throw "ernic-nics: ${builtins.toJSON failures}"
else
  pkgs.runCommand "xtcp2-ernic-nics" { } "touch $out"
