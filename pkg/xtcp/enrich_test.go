package xtcp

import (
	"testing"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_flat_record"
	"github.com/randomizedcoder/xtcp2/pkg/lldp"
	"github.com/randomizedcoder/xtcp2/pkg/nicinfo"
)

// TestBuildUplinkStamp covers the fold of per-interface NIC + LLDP lookups into
// the flat per-slot columns: positive (both sources, both slots), partial (NIC
// only / LLDP only), boundary (more ifnames than slots, empty ifnames), and
// corner (interface present with no matching NIC or neighbor).
func TestBuildUplinkStamp(t *testing.T) {
	nics := []nicinfo.NIC{
		{Ifname: "eth0", Driver: "mlx5_core", Model: "ConnectX-6", BusInfo: "0000:01:00.0", FwVersion: "22.31", PCIVendor: 0x15b3, PCIDevice: 0x101d, SpeedMbps: 25000},
		{Ifname: "eth1", Driver: "ice", Model: "E810", BusInfo: "0000:02:00.0", FwVersion: "4.0", PCIVendor: 0x8086, PCIDevice: 0x1593, SpeedMbps: 100000},
	}
	neigh := map[string]lldp.Neighbor{
		"eth0": {Ifname: "eth0", ChassisName: "sw-a", ChassisID: "aa:bb:cc:dd:ee:ff", MgmtIP: "10.0.0.1", PortID: "Ethernet1", PortDescr: "to-host"},
		"eth1": {Ifname: "eth1", ChassisName: "sw-b", ChassisID: "11:22:33:44:55:66", MgmtIP: "10.0.0.2", PortID: "Ethernet2", PortDescr: "to-host2"},
	}

	tests := []struct {
		name    string
		ifnames []string
		nics    []nicinfo.NIC
		neigh   map[string]lldp.Neighbor
		check   func(t *testing.T, s uplinkStamp)
	}{
		{
			name:    "both sources both slots",
			ifnames: []string{"eth0", "eth1"},
			nics:    nics,
			neigh:   neigh,
			check: func(t *testing.T, s uplinkStamp) {
				if !s.enabled {
					t.Fatal("expected enabled")
				}
				if s.slots[0].ifname != "eth0" || s.slots[0].nicDriver != "mlx5_core" || s.slots[0].nicPCIVendor != 0x15b3 || s.slots[0].nicSpeedMbps != 25000 {
					t.Fatalf("slot0 nic wrong: %+v", s.slots[0])
				}
				if s.slots[0].lldpChassisName != "sw-a" || s.slots[0].lldpPortID != "Ethernet1" {
					t.Fatalf("slot0 lldp wrong: %+v", s.slots[0])
				}
				if s.slots[1].ifname != "eth1" || s.slots[1].nicDriver != "ice" || s.slots[1].lldpChassisName != "sw-b" {
					t.Fatalf("slot1 wrong: %+v", s.slots[1])
				}
			},
		},
		{
			name:    "nic only (no neighbors)",
			ifnames: []string{"eth0"},
			nics:    nics,
			neigh:   nil,
			check: func(t *testing.T, s uplinkStamp) {
				if s.slots[0].nicDriver != "mlx5_core" || s.slots[0].lldpChassisName != "" {
					t.Fatalf("expected nic set, lldp empty: %+v", s.slots[0])
				}
				if s.slots[1].ifname != "" {
					t.Fatalf("slot1 should be empty: %+v", s.slots[1])
				}
			},
		},
		{
			name:    "lldp only (no nics)",
			ifnames: []string{"eth0"},
			nics:    nil,
			neigh:   neigh,
			check: func(t *testing.T, s uplinkStamp) {
				if s.slots[0].nicDriver != "" || s.slots[0].lldpChassisName != "sw-a" {
					t.Fatalf("expected lldp set, nic empty: %+v", s.slots[0])
				}
			},
		},
		{
			name:    "interface with no matching nic/neighbor",
			ifnames: []string{"eth9"},
			nics:    nics,
			neigh:   neigh,
			check: func(t *testing.T, s uplinkStamp) {
				if !s.enabled {
					t.Fatal("enabled should be true once a slot ifname is set")
				}
				if s.slots[0].ifname != "eth9" || s.slots[0].nicDriver != "" || s.slots[0].lldpChassisName != "" {
					t.Fatalf("ifname set, columns empty expected: %+v", s.slots[0])
				}
			},
		},
		{
			name:    "more ifnames than slots (capped at maxUplinks)",
			ifnames: []string{"eth0", "eth1", "eth2"},
			nics:    nics,
			neigh:   neigh,
			check: func(t *testing.T, s uplinkStamp) {
				// Only two slots exist; eth2 must be dropped without panic.
				if s.slots[0].ifname != "eth0" || s.slots[1].ifname != "eth1" {
					t.Fatalf("first two slots wrong: %+v %+v", s.slots[0], s.slots[1])
				}
			},
		},
		{
			name:    "empty ifnames disables",
			ifnames: nil,
			nics:    nics,
			neigh:   neigh,
			check: func(t *testing.T, s uplinkStamp) {
				if s.enabled {
					t.Fatal("expected disabled with no ifnames")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := buildUplinkStamp(tt.ifnames, tt.nics, tt.neigh)
			tt.check(t, s)
		})
	}
}

// TestUplinkStamp_apply verifies apply copies every slot column onto the record,
// and is a no-op when disabled.
func TestUplinkStamp_apply(t *testing.T) {
	s := buildUplinkStamp(
		[]string{"eth0", "eth1"},
		[]nicinfo.NIC{
			{Ifname: "eth0", Driver: "mlx5_core", Model: "CX6", BusInfo: "0000:01:00.0", FwVersion: "22", PCIVendor: 0x15b3, PCIDevice: 0x101d, SpeedMbps: 25000},
			{Ifname: "eth1", Driver: "ice", Model: "E810", BusInfo: "0000:02:00.0", FwVersion: "4", PCIVendor: 0x8086, PCIDevice: 0x1593, SpeedMbps: 100000},
		},
		map[string]lldp.Neighbor{
			"eth0": {ChassisName: "sw-a", ChassisID: "aa", MgmtIP: "10.0.0.1", PortID: "Et1", PortDescr: "d1"},
			"eth1": {ChassisName: "sw-b", ChassisID: "bb", MgmtIP: "10.0.0.2", PortID: "Et2", PortDescr: "d2"},
		},
	)

	r := &xtcp_flat_record.XtcpFlatRecord{}
	s.apply(r)

	if r.Uplink1Ifname != "eth0" || r.Uplink1NicDriver != "mlx5_core" || r.Uplink1NicModel != "CX6" ||
		r.Uplink1NicBusInfo != "0000:01:00.0" || r.Uplink1NicFwVersion != "22" ||
		r.Uplink1NicPciVendor != 0x15b3 || r.Uplink1NicPciDevice != 0x101d || r.Uplink1NicSpeedMbps != 25000 ||
		r.Uplink1LldpChassisName != "sw-a" || r.Uplink1LldpChassisId != "aa" || r.Uplink1LldpMgmtIp != "10.0.0.1" ||
		r.Uplink1LldpPortId != "Et1" || r.Uplink1LldpPortDescr != "d1" {
		t.Fatalf("uplink1 not fully stamped: %+v", r)
	}
	if r.Uplink2Ifname != "eth1" || r.Uplink2NicDriver != "ice" || r.Uplink2LldpChassisName != "sw-b" ||
		r.Uplink2NicPciVendor != 0x8086 || r.Uplink2NicSpeedMbps != 100000 || r.Uplink2LldpPortId != "Et2" {
		t.Fatalf("uplink2 not fully stamped: %+v", r)
	}

	// Disabled stamp leaves the record untouched.
	var empty uplinkStamp
	r2 := &xtcp_flat_record.XtcpFlatRecord{Uplink1Ifname: "keep"}
	empty.apply(r2)
	if r2.Uplink1Ifname != "keep" {
		t.Fatalf("disabled apply must be a no-op, got %q", r2.Uplink1Ifname)
	}
}
