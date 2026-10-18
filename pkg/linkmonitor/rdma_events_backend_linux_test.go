package linkmonitor

import (
	"errors"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/linuxio"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/rdmaevents"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/rdmanative"
)

func backendPort(device string, index, port, netdev uint32, eligible bool) model.RDMAPort {
	p := model.RDMAPort{Device: device, Index: index, Port: port, Netdev: model.Optional[uint32]{Value: netdev, Present: netdev != 0}}
	if eligible {
		p.Eligibility = model.Eligible
	}
	return p
}

// TestRDMAPortMap is the inventory-to-port-map conversion the netlink
// backend uses, rendered as rdmanative.PortMap.String.
func TestRDMAPortMap(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected string
		ports                                 []model.RDMAPort
		links                                 []model.NetdevLink
		want                                  string
	}{
		{"eligible", "positive", "an eligible port and its netdev", "one link and address ref",
			[]model.RDMAPort{backendPort("mlx5_0", 3, 1, 2, true)}, nil, "2:mlx5_0/1la"},
		{"lag", "positive", "two ports enslaved to a bond, a VLAN over the bond", "master refs for both, VLAN address refs",
			[]model.RDMAPort{backendPort("mlx5_0", 3, 1, 2, true), backendPort("mlx5_1", 4, 1, 5, true)},
			[]model.NetdevLink{{Index: 2, Master: 10}, {Index: 5, Master: 10}, {Index: 11, Lower: 10}},
			"2:mlx5_0/1la 5:mlx5_1/1la 10:mlx5_0/1la 10:mlx5_1/1la 11:mlx5_0/1a 11:mlx5_1/1a"},
		{"ineligible", "negative", "a port the policy does not monitor", "no ref",
			[]model.RDMAPort{backendPort("mlx5_0", 3, 1, 2, false)}, nil, ""},
		{"foreign", "negative", "an upper whose lower is in another namespace", "ignored: its IFLA_LINK is not our ifindex",
			[]model.RDMAPort{backendPort("mlx5_0", 3, 1, 2, true)}, []model.NetdevLink{{Index: 9, Lower: 2, Foreign: true}}, "2:mlx5_0/1la"},
		{"no_netdev", "boundary", "a native InfiniBand port", "device listed, no ref",
			[]model.RDMAPort{backendPort("mlx5_2", 6, 1, 0, true)}, nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			m := rdmaPortMap(tc.ports, tc.links)
			if m == nil || m.String() != tc.want {
				t.Fatalf("%s: got %v", tc.expected, m)
			}
		})
	}
	many := make([]model.RDMAPort, rdmanative.MaxPorts+1)
	for i := range many {
		many[i] = backendPort("d"+string(rune('a'+i%26))+string(rune('a'+i/26)), uint32(i+1), 1, 2, true)
	}
	if rdmaPortMap(many, nil) != nil {
		t.Fatal("boundary: more than MaxPorts ports on one netdev; expected: nil map, which Open refuses")
	}
}

// TestRDMAEventNotification is the NLDEV half of each backend: with verbs
// every notification is a Topology hint; with netlink an UNREGISTER of a held
// device is Fatal for it.
func TestRDMAEventNotification(t *testing.T) {
	ports := rdmaPortMap([]model.RDMAPort{backendPort("mlx5_0", 3, 1, 2, true)}, nil)
	unregister := linuxio.RDMANotification{Kind: linuxio.RDMANotifyUnregister, EventType: 1, DevIndex: 3, HasDevIndex: true}
	// The kernel's first RDMA device has NLDEV index 0; a port found in sysfs
	// alone has no index at all.
	first := rdmaPortMap([]model.RDMAPort{backendPort("rxe0", 0, 1, 2, true)}, nil)
	sysfsOnly := backendPort("siw0", 0, 1, 2, true)
	sysfsOnly.Sysfs = true
	unindexed := rdmaPortMap([]model.RDMAPort{sysfsOnly}, nil)
	unregisterFirst := linuxio.RDMANotification{Kind: linuxio.RDMANotifyUnregister, EventType: 1, DevIndex: 0, HasDevIndex: true}
	for _, tc := range []struct {
		name, category, description, expected string
		ports                                 *rdmanative.PortMap
		n                                     linuxio.RDMANotification
		want                                  rdmaevents.Event
	}{
		{"netlink_unregister", "positive", "netlink: UNREGISTER of a held device", "Fatal for mlx5_0", ports, unregister,
			rdmaevents.Event{Device: "mlx5_0", Kind: rdmaevents.Fatal, Err: errRDMAUnregistered}},
		{"netlink_attach", "positive", "netlink: NETDEV_ATTACH", "Topology", ports,
			linuxio.RDMANotification{Kind: linuxio.RDMANotifyNetdevAttach, DevIndex: 3, HasDevIndex: true}, rdmaevents.Event{Kind: rdmaevents.Topology}},
		{"netlink_unmapped", "corner", "netlink: UNREGISTER of a device not held", "Topology only", ports,
			linuxio.RDMANotification{Kind: linuxio.RDMANotifyUnregister, DevIndex: 9, HasDevIndex: true}, rdmaevents.Event{Kind: rdmaevents.Topology}},
		{"netlink_unknown", "negative", "netlink: an unknown event type", "Topology", ports,
			linuxio.RDMANotification{Kind: linuxio.RDMANotifyUnknown, EventType: 255}, rdmaevents.Event{Kind: rdmaevents.Topology}},
		{"netlink_unregister_index0", "boundary", "netlink: UNREGISTER of the device with NLDEV index 0", "Fatal for rxe0", first, unregisterFirst,
			rdmaevents.Event{Device: "rxe0", Kind: rdmaevents.Fatal, Err: errRDMAUnregistered}},
		{"netlink_unregister_unindexed", "corner", "netlink: UNREGISTER of index 0 when the held port has no index (sysfs only)", "Topology only",
			unindexed, unregisterFirst, rdmaevents.Event{Kind: rdmaevents.Topology}},
		{"verbs_unregister", "boundary", "verbs: UNREGISTER of a held device", "Topology; the verbs queue reports its own fatal", nil, unregister,
			rdmaevents.Event{Kind: rdmaevents.Topology}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			got := (&rdmaEventSource{ports: tc.ports}).notification(tc.n)
			if got.Device != tc.want.Device || got.Kind != tc.want.Kind || !errors.Is(got.Err, tc.want.Err) {
				t.Fatalf("%s: got %+v", tc.expected, got)
			}
		})
	}
}

// TestRDMAEventPortMapChange: with netlink, a commit whose port map differs
// (a netdev moved) reopens the source, as a changed identity set does; with
// verbs the map is not kept and the same commit does not.
func TestRDMAEventPortMapChange(t *testing.T) {
	forEachRDMAEventsBackend(t, func(t *testing.T, backend RDMAEventsBackend) {
		t.Logf("corner: %s, the port's netdev changes between commits; expected: reopen only for netlink", backend)
		s, p := eventScheduleFixture(t, backend)
		s.committed([]model.RDMAPort{p}, nil, s.barrier+1)
		if s.dirty {
			t.Fatal("unchanged commit marked the source dirty")
		}
		moved := p
		moved.Netdev = model.Optional[uint32]{Value: p.Netdev.Value + 100, Present: true}
		s.committed([]model.RDMAPort{moved}, nil, s.barrier+1)
		if s.dirty != (backend == RDMAEventsNetlink) {
			t.Fatalf("dirty=%v after the netdev moved", s.dirty)
		}
	})
}
