package linkmonitor

import (
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

func present[T any](value T) model.Optional[T] { return model.Optional[T]{Value: value, Present: true} }
func pointer[T any](value T) *T                { return &value }

func TestEthernetEligibility(t *testing.T) {
	base := []struct {
		name, category, description, expected string
		change                                func(*ethernetEvidence)
		want                                  model.Eligibility
	}{
		{"physical", "positive", "physical PCI device, not wireless or representor", "include", func(*ethernetEvidence) {}, model.Eligible},
		{"USB", "positive", "USB ancestry established by adapter", "include", func(*ethernetEvidence) {}, model.Eligible},
		{"guest", "positive", "guest virtio device ancestry", "include", func(*ethernetEvidence) {}, model.Eligible},
		{"VF", "positive", "hardware VF ancestry", "include", func(*ethernetEvidence) {}, model.Eligible},
		{"bond member", "positive", "hardware slave is not the bond master kind", "include", func(*ethernetEvidence) {}, model.Eligible},
		{"loopback", "negative", "loopback hardware type", "exclude", func(e *ethernetEvidence) { e.linkType = present(uint16(unix.ARPHRD_LOOPBACK)) }, model.Excluded},
		{"native IB", "corner", "native RDMA is handled by its own inventory", "exclude from Ethernet inventory", func(e *ethernetEvidence) { e.linkType = present(uint16(unix.ARPHRD_INFINIBAND)) }, model.Excluded},
		{"wireless", "negative", "Ethernet type with wireless metadata", "exclude", func(e *ethernetEvidence) { e.wireless = present(true) }, model.Excluded},
		{"representor", "negative", "physical ancestry but switchdev representor", "exclude", func(e *ethernetEvidence) { e.representor = present(true) }, model.Excluded},
		{"missing ancestry", "negative", "Ethernet alone is not proof of hardware", "unknown", func(e *ethernetEvidence) { e.hardware = model.Optional[bool]{} }, model.EligibilityUnknown},
		{"wireless unknown", "negative", "wireless probe failed", "unknown", func(e *ethernetEvidence) { e.wireless = model.Optional[bool]{} }, model.EligibilityUnknown},
		{"switchdev unknown", "negative", "representor classification unresolved", "unknown", func(e *ethernetEvidence) { e.representor = model.Optional[bool]{} }, model.EligibilityUnknown},
		{"conflicting", "corner", "metadata observations contradict one another", "unknown", func(e *ethernetEvidence) { e.conflicting = true }, model.EligibilityUnknown},
		{"new kind", "boundary", "unknown future kind", "unknown", func(e *ethernetEvidence) { e.kind = "future" }, model.EligibilityUnknown},
	}
	kinds := []string{"veth", "vlan", "bridge", "bond", "tun", "vxlan", "gretap", "macvlan", "ipvlan"}
	tests := make([]struct {
		name, category, description, expected string
		change                                func(*ethernetEvidence)
		want                                  model.Eligibility
	}, 0, len(base)+len(kinds))
	tests = append(tests, base...)
	for _, kind := range kinds {
		tests = append(tests, struct {
			name, category, description, expected string
			change                                func(*ethernetEvidence)
			want                                  model.Eligibility
		}{kind, "negative", "software link kind even with device parent", "exclude", func(e *ethernetEvidence) { e.kind = kind }, model.Excluded})
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			e := ethernetEvidence{linkType: present(uint16(unix.ARPHRD_ETHER)), hardware: present(true), wireless: present(false), representor: present(false)}
			tc.change(&e)
			if got := ethernetEligibility(e); got != tc.want {
				t.Fatalf("eligibility = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestEthernetOperationalState(t *testing.T) {
	tests := []struct {
		name, category, description, expected string
		flags                                 model.Optional[uint32]
		state                                 model.Optional[uint8]
		want                                  model.Optional[bool]
	}{
		{"up", "positive", "admin up and operstate UP without RUNNING", "up", present(uint32(unix.IFF_UP)), present(xtcpnl.IfOperUp), present(true)},
		{"admin down", "negative", "operstate UP cannot override admin down", "down", present(uint32(unix.IFF_RUNNING)), present(xtcpnl.IfOperUp), present(false)},
		{"legacy up", "positive", "UNKNOWN with UP and RUNNING", "up", present(uint32(unix.IFF_UP | unix.IFF_RUNNING)), present(xtcpnl.IfOperUnknown), present(true)},
		{"legacy down", "negative", "UNKNOWN without RUNNING", "down", present(uint32(unix.IFF_UP)), present(xtcpnl.IfOperUnknown), present(false)},
		{"dormant", "corner", "RUNNING with DORMANT", "down", present(uint32(unix.IFF_UP | unix.IFF_RUNNING)), present(xtcpnl.IfOperDormant), present(false)},
		{"down", "negative", "operstate DOWN despite compatibility flags", "down", present(uint32(unix.IFF_UP | unix.IFF_RUNNING)), present(xtcpnl.IfOperDown), present(false)},
		{"missing flags", "negative", "flags absent", "unknown", model.Optional[uint32]{}, present(xtcpnl.IfOperUp), model.Optional[bool]{}},
		{"missing state", "negative", "operstate absent on admin-up interface", "unknown", present(uint32(unix.IFF_UP)), model.Optional[uint8]{}, model.Optional[bool]{}},
		{"future state", "boundary", "unrecognized operstate value", "unknown", present(uint32(unix.IFF_UP)), present(uint8(255)), model.Optional[bool]{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			if got := ethernetUp(tc.flags, tc.state); got != tc.want {
				t.Fatalf("up = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func compactModes(indices ...uint32) *xtcpnl.EthtoolBitset {
	set := &xtcpnl.EthtoolBitset{Size: pointer(uint32(160)), Compact: true, Value: make([]uint32, 5), Mask: make([]uint32, 5)}
	for _, index := range indices {
		set.Mask[index/32] |= 1 << (index % 32)
	}
	return set
}

func TestEthernetChecks(t *testing.T) {
	tests := []struct {
		name, category, description, expected string
		up                                    model.Optional[bool]
		modes                                 *xtcpnl.EthtoolLinkModes
		speed, duplex                         model.Check
	}{
		{"maximum", "positive", "100GE supported and negotiated", "both pass", present(true), &xtcpnl.EthtoolLinkModes{Speed: pointer(uint32(100000)), Duplex: pointer(uint8(1)), Ours: compactModes(12, 36)}, model.CheckPass, model.CheckPass},
		{"advertisement", "negative", "100GE supported but not advertised, 10GE active", "speed fails even with empty advertisement", present(true), &xtcpnl.EthtoolLinkModes{Speed: pointer(uint32(10000)), Duplex: pointer(uint8(1)), Ours: compactModes(12, 36)}, model.CheckFail, model.CheckPass},
		{"half duplex", "negative", "maximum speed but half duplex", "independent duplex failure", present(true), &xtcpnl.EthtoolLinkModes{Speed: pointer(uint32(10000)), Duplex: pointer(uint8(0)), Ours: compactModes(12)}, model.CheckPass, model.CheckFail},
		{"unknown duplex", "boundary", "unknown duplex sentinel", "speed passes, duplex unknown", present(true), &xtcpnl.EthtoolLinkModes{Speed: pointer(uint32(10000)), Duplex: pointer(uint8(255)), Ours: compactModes(12)}, model.CheckPass, model.CheckUnknown},
		{"zero speed", "boundary", "zero on an up port", "unknown speed, measured duplex passes", present(true), &xtcpnl.EthtoolLinkModes{Speed: pointer(uint32(0)), Duplex: pointer(uint8(1)), Ours: compactModes(12)}, model.CheckUnknown, model.CheckPass},
		{"unknown speed", "boundary", "unknown uint32 speed sentinel", "unknown speed", present(true), &xtcpnl.EthtoolLinkModes{Speed: pointer(uint32(0xffffffff)), Ours: compactModes(12)}, model.CheckUnknown, model.CheckUnknown},
		{"peer", "corner", "peer mask only", "unknown local maximum", present(true), &xtcpnl.EthtoolLinkModes{Speed: pointer(uint32(10000)), Peer: compactModes(12)}, model.CheckUnknown, model.CheckUnknown},
		{"expired", "negative", "expired settings withheld by reducer", "both unknown", present(true), nil, model.CheckUnknown, model.CheckUnknown},
		{"down", "corner", "down port without settings", "both not applicable", present(false), nil, model.CheckNotApplicable, model.CheckNotApplicable},
		{"state unknown", "negative", "no authoritative up state", "both unknown", model.Optional[bool]{}, nil, model.CheckUnknown, model.CheckUnknown},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			got := checkEthernet(tc.up, tc.modes)
			if got.speed != tc.speed || got.duplex != tc.duplex {
				t.Fatalf("checks = %+v; want speed %v duplex %v", got, tc.speed, tc.duplex)
			}
		})
	}
}

func TestEthernetMaximumModes(t *testing.T) {
	for _, tc := range []struct {
		name        string
		index, mbps uint32
	}{
		{"10M", 0, 10}, {"100M", 2, 100}, {"1G", 5, 1000}, {"10G", 12, 10000}, {"100G", 36, 100000}, {"200G", 62, 200000}, {"400G", 69, 400000}, {"800G", 93, 800000}, {"1600G", 121, 1600000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Log("positive/boundary: known UAPI mode; expected exact widened bits/second for compact and verbose masks")
			sets := []*xtcpnl.EthtoolBitset{compactModes(tc.index), {Bits: []xtcpnl.EthtoolBit{{Index: pointer(tc.index), Value: false}}}}
			for _, set := range sets {
				if got := maximumEthernetBits(set); got != present(uint64(tc.mbps)*1000000) {
					t.Fatalf("maximum = %+v", got)
				}
			}
		})
	}
	for _, tc := range []struct {
		name, description, expected string
		set                         *xtcpnl.EthtoolBitset
		want                        model.Optional[uint64]
	}{
		{"future bit", "known 10GE plus unknown mode", "unknown maximum", compactModes(12, 159), model.Optional[uint64]{}},
		{"non-speed only", "10G FEC capability is not a speed", "unknown maximum", compactModes(20), model.Optional[uint64]{}},
		{"ignore non-speed", "10Mbps plus 10G FEC bit", "10Mbps maximum", compactModes(0, 20), present(uint64(10000000))},
		{"nomask", "value-only list has no supported mask", "unknown maximum", &xtcpnl.EthtoolBitset{NoMask: true, Bits: []xtcpnl.EthtoolBit{{Index: pointer(uint32(12))}}}, model.Optional[uint64]{}},
		{"name-only", "verbose name without verified UAPI index", "unknown maximum", &xtcpnl.EthtoolBitset{Bits: []xtcpnl.EthtoolBit{{Name: "100000baseKR4/Full"}, {Name: "RS"}}}, model.Optional[uint64]{}},
		{"bad name", "unknown verbose capability", "unknown maximum", &xtcpnl.EthtoolBitset{Bits: []xtcpnl.EthtoolBit{{Name: "future"}}}, model.Optional[uint64]{}},
		{"duplicate", "same verbose index repeated", "unknown maximum", &xtcpnl.EthtoolBitset{Bits: []xtcpnl.EthtoolBit{{Index: pointer(uint32(12))}, {Index: pointer(uint32(12))}}}, model.Optional[uint64]{}},
		{"missing mask", "compact values without capability mask", "unknown maximum", &xtcpnl.EthtoolBitset{Compact: true, Size: pointer(uint32(32)), Value: []uint32{1 << 12}}, model.Optional[uint64]{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s; expected: %s", tc.description, tc.expected)
			got := maximumEthernetBits(tc.set)
			if got.Present != tc.want.Present || (got.Present && got.Value != tc.want.Value) {
				t.Fatalf("maximum = %+v, want %+v", got, tc.want)
			}
		})
	}
}
