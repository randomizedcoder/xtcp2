package linkmonitor

import (
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

func TestRDMAIdentity(t *testing.T) {
	port := model.DeviceKey{Namespace: 1, RDMADevice: "mlx5_0", Port: 1}
	native := port
	native.Kind = model.DeviceNativeRDMA
	ethernet := model.DeviceKey{Namespace: 1, Kind: model.DeviceEthernet, Index: 4}
	other := ethernet
	other.Index = 5
	for _, tc := range []struct {
		name, category, description, expected string
		layer                                 rdmaLayer
		hardware                              model.Optional[bool]
		associations                          []rdmaAssociation
		want                                  rdmaIdentity
	}{
		{"native", "positive", "native port without IPoIB", "one independent native identity", rdmaNative, present(true), nil, rdmaIdentity{canonical: native, eligibility: model.Eligible, countSeparately: true}},
		{"PKeys", "corner", "native port with several IPoIB aliases", "same native identity", rdmaNative, present(true), []rdmaAssociation{{key: ethernet}, {key: other}}, rdmaIdentity{canonical: native, eligibility: model.Eligible, countSeparately: true}},
		{"RoCE", "positive", "one kernel-associated Ethernet device", "Ethernet identity, no extra count", rdmaEthernet, present(true), []rdmaAssociation{{ethernet, model.Eligible}}, rdmaIdentity{canonical: ethernet, eligibility: model.Eligible}},
		{"aliases", "corner", "several GID aliases resolve to same lower link", "one Ethernet identity", rdmaEthernet, present(true), []rdmaAssociation{{ethernet, model.Eligible}, {ethernet, model.Eligible}}, rdmaIdentity{canonical: ethernet, eligibility: model.Eligible}},
		{"ambiguous", "negative", "two physical targets", "unknown, no invented count", rdmaEthernet, present(true), []rdmaAssociation{{ethernet, model.Eligible}, {other, model.Eligible}}, rdmaIdentity{}},
		{"missing", "negative", "missing RoCE association", "unknown", rdmaEthernet, present(true), nil, rdmaIdentity{}},
		{"software", "negative", "RXE or SIW without hardware evidence", "excluded", rdmaEthernet, present(false), nil, rdmaIdentity{eligibility: model.Excluded}},
		{"unknown hardware", "negative", "metadata unavailable", "unknown", rdmaNative, model.Optional[bool]{}, nil, rdmaIdentity{}},
		{"unknown layer", "boundary", "future or missing link layer", "unknown", rdmaLayerUnknown, present(true), nil, rdmaIdentity{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			got := resolveRDMAIdentity(rdmaIdentityEvidence{key: port, layer: tc.layer, hardware: tc.hardware, associations: tc.associations})
			if got != tc.want {
				t.Fatalf("identity = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestRDMAStates(t *testing.T) {
	for _, tc := range []struct {
		name, description, expected string
		state, physical             model.Optional[uint8]
		up                          model.Optional[bool]
		readiness                   model.Check
	}{
		{"active", "ACTIVE and LINK_UP", "native up and RoCE ready", present(uint8(4)), present(uint8(5)), present(true), model.CheckPass},
		{"init", "INIT with physical carrier", "down and not ready", present(uint8(2)), present(uint8(5)), present(false), model.CheckFail},
		{"armed", "ARMED with physical carrier", "down and not ready", present(uint8(3)), present(uint8(5)), present(false), model.CheckFail},
		{"physical down", "ACTIVE but physical link polling", "native down; RoCE readiness follows logical state", present(uint8(4)), present(uint8(2)), present(false), model.CheckPass},
		{"absent", "missing required port state", "unknown", model.Optional[uint8]{}, present(uint8(5)), model.Optional[bool]{}, model.CheckUnknown},
		{"future", "unknown logical encoding", "unknown", present(uint8(255)), present(uint8(5)), model.Optional[bool]{}, model.CheckUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s; expected: %s", tc.description, tc.expected)
			if got := nativeRDMAUp(tc.state, tc.physical); got != tc.up {
				t.Fatalf("native state = %+v", got)
			}
			if got := roceReadiness(tc.state); got != tc.readiness {
				t.Fatalf("readiness = %v", got)
			}
		})
	}
}

func TestNativeSpeedWidth(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected string
		active                                rdmaMode
		caps                                  rdmaCapabilities
		speed, width                          model.Check
		maximum                               uint64
	}{
		{"NDR", "positive", "4x NDR active and supported", "400G, both pass", rdmaMode{128, 4}, rdmaCapabilities{true, []rdmaMode{{128, 4}, {64, 4}}}, model.CheckPass, model.CheckPass, 400000000000},
		{"narrow", "negative", "2x NDR active on 4x port", "speed and width fail", rdmaMode{128, 2}, rdmaCapabilities{true, []rdmaMode{{128, 4}}}, model.CheckFail, model.CheckFail, 400000000000},
		{"restricted", "negative", "enabled HDR active on NDR-capable port", "supported maximum still NDR", rdmaMode{64, 4}, rdmaCapabilities{true, []rdmaMode{{128, 4}}}, model.CheckFail, model.CheckPass, 400000000000},
		{"constrained", "corner", "4x HDR or 2x NDR, no 4x NDR combination", "200G maximum, width independently fails", rdmaMode{128, 2}, rdmaCapabilities{true, []rdmaMode{{64, 4}, {128, 2}}}, model.CheckPass, model.CheckFail, 200000000000},
		{"unknown active", "boundary", "future speed with known narrowed width", "speed unknown, width fails", rdmaMode{512, 2}, rdmaCapabilities{true, []rdmaMode{{128, 4}}}, model.CheckUnknown, model.CheckFail, 400000000000},
		{"unknown capability", "boundary", "future supported speed", "rate unknown, width still checked", rdmaMode{128, 2}, rdmaCapabilities{true, []rdmaMode{{512, 4}}}, model.CheckUnknown, model.CheckFail, 0},
		{"missing capabilities", "negative", "UMAD inaccessible", "unknown maximum, no active-as-maximum fallback", rdmaMode{128, 4}, rdmaCapabilities{}, model.CheckUnknown, model.CheckUnknown, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			got := checkNativeRDMA(present(true), tc.active, tc.caps)
			if got.speed != tc.speed || got.width != tc.width || got.duplex != model.CheckNotApplicable {
				t.Fatalf("checks = %+v", got)
			}
			if got.maximumBits.Present != (tc.maximum != 0) || (got.maximumBits.Present && got.maximumBits.Value != tc.maximum) {
				t.Fatalf("maximum = %+v", got.maximumBits)
			}
		})
	}
	for _, tc := range []struct {
		name        string
		speed       uint32
		oneLaneMbps uint64
	}{
		{"SDR", 1, 2500}, {"DDR", 2, 5000}, {"QDR", 4, 10000}, {"FDR10", 8, 10000}, {"FDR", 16, 14000}, {"EDR", 32, 25000}, {"HDR", 64, 50000}, {"NDR", 128, 100000}, {"XDR", 256, 200000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Log("positive/boundary: nominal per-lane speed; expected exact kernel-convention aggregate rate")
			for _, width := range []uint8{1, 2, 4, 8, 12} {
				if got := nativeRate(rdmaMode{tc.speed, width}); got != present(tc.oneLaneMbps*uint64(width)*1000000) {
					t.Fatalf("rate = %+v", got)
				}
			}
		})
	}
}

func TestExceptionResolution(t *testing.T) {
	first := exceptionTarget{key: model.DeviceKey{Namespace: 1, Kind: model.DeviceEthernet, Index: 4}, generation: 1}
	second := first
	second.key.Index = 5
	for _, tc := range []struct {
		name, category, description, expected string
		selector                              string
		inventory                             []exceptionDevice
		status                                exceptionStatus
		target                                exceptionTarget
	}{
		{"exact", "positive", "exact Ethernet name", "match current lifetime", "eno1", []exceptionDevice{{first, model.Eligible, []string{"eno1"}}}, exceptionMatched, first},
		{"RDMA selector", "positive", "RoCE selector aliases underlying Ethernet", "match Ethernet", "rdma:mlx5_0:1", []exceptionDevice{{first, model.Eligible, []string{"eno1", "rdma:mlx5_0:1"}}}, exceptionMatched, first},
		{"IPoIB alias", "positive", "unique IPoIB alias", "match canonical port", "ib0", []exceptionDevice{{first, model.Eligible, []string{"ib0", "ib0.8001"}}}, exceptionMatched, first},
		{"ambiguous", "negative", "same alias points at two devices", "exempt neither", "ib0", []exceptionDevice{{first, model.Eligible, []string{"ib0"}}, {second, model.Eligible, []string{"ib0"}}}, exceptionAmbiguous, exceptionTarget{}},
		{"unresolved", "negative", "name collides with unresolved identity", "exempt neither", "ib0", []exceptionDevice{{first, model.Eligible, []string{"ib0"}}, {second, model.EligibilityUnknown, []string{"ib0"}}}, exceptionAmbiguous, exceptionTarget{}},
		{"rename", "corner", "same ifindex now has another name", "old exception no longer matches", "eno1", []exceptionDevice{{first, model.Eligible, []string{"eno2"}}}, exceptionUnmatched, exceptionTarget{}},
		{"excluded", "negative", "exception names an excluded device", "unmatched", "eno1", []exceptionDevice{{first, model.Excluded, []string{"eno1"}}}, exceptionUnmatched, exceptionTarget{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			got := resolveExceptions([]string{tc.selector}, tc.inventory)
			if len(got) != 1 || got[0].selector != tc.selector || got[0].status != tc.status || got[0].target != tc.target {
				t.Fatalf("resolution = %+v", got)
			}
		})
	}
	old := resolveExceptions([]string{"eno1"}, []exceptionDevice{{first, model.Eligible, []string{"eno1"}}})
	first.generation++
	current := resolveExceptions([]string{"eno1"}, []exceptionDevice{{first, model.Eligible, []string{"eno1"}}})
	if old[0].target == current[0].target {
		t.Fatal("reused ifindex retained old generation")
	}
}
