package linkmonitor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/linuxio"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/testkit"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

type fakeIdentityRoute struct {
	links       []xtcpnl.LinkInfo
	dumps, gets int
}

func (f *fakeIdentityRoute) DumpLinks(context.Context) (linuxio.Result[xtcpnl.LinkInfo], error) {
	f.dumps++
	return linuxio.Result[xtcpnl.LinkInfo]{Values: f.links}, nil
}
func (f *fakeIdentityRoute) GetLink(_ context.Context, index int32) (linuxio.Result[xtcpnl.LinkInfo], error) {
	f.gets++
	for i := range f.links {
		link := &f.links[i]
		if link.Index == index {
			return linuxio.Result[xtcpnl.LinkInfo]{Values: []xtcpnl.LinkInfo{*link}}, nil
		}
	}
	return linuxio.Result[xtcpnl.LinkInfo]{}, unix.ENODEV
}
func (*fakeIdentityRoute) Close() error { return nil }

type fakeDevlink struct {
	ports []linuxio.DevlinkPort
	err   error
	dumps int
}

func (*fakeDevlink) DiscoverFamily(context.Context, string) (linuxio.Family, error) {
	return linuxio.Family{}, nil
}
func (f *fakeDevlink) DumpDevlinkPorts(context.Context, linuxio.Family) (linuxio.Result[linuxio.DevlinkPort], error) {
	f.dumps++
	return linuxio.Result[linuxio.DevlinkPort]{Values: f.ports}, f.err
}
func (*fakeDevlink) Close() error { return nil }

func TestIdentityInventory(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		metadataError, devlinkError                  error
		duplicate                                    bool
		wantError                                    bool
	}{
		{"complete", "positive", "two physical links and targeted refresh", "one bulk devlink read and reused route statistics", nil, nil, false, false},
		{"unavailable", "corner", "devlink unsupported with conclusive ancestry", "eligible without devlink", nil, unix.EOPNOTSUPP, false, false},
		{"permission", "negative", "hardware metadata inaccessible", "no complete candidate or healthy removal", unix.EACCES, nil, false, true},
		{"devlink failure", "negative", "devlink dump fails", "no partial evidence", nil, unix.EPERM, false, true},
		{"duplicate", "corner", "two ports claim same netdevice", "reject conflicting association", nil, nil, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			route := &fakeIdentityRoute{links: []xtcpnl.LinkInfo{{Index: 1, Name: "eth0", Type: unix.ARPHRD_ETHER, Flags: unix.IFF_UP, OperState: xtcpnl.IfOperUp, HasOperState: true}, {Index: 2, Name: "eth1", Type: unix.ARPHRD_ETHER}}}
			devlink := &fakeDevlink{err: tc.devlinkError}
			if tc.duplicate {
				devlink.ports = []linuxio.DevlinkPort{{Netdev: 1}, {Netdev: 1}}
			}
			s := &ethernetInventory{namespace: 1, clock: testkit.NewClock(time.Unix(100, 0)), route: route, devlink: devlink,
				metadata: func(context.Context, string, uint32) (hardwareMetadata, error) {
					return hardwareMetadata{path: "/sys/devices/physical"}, tc.metadataError
				}}
			candidate, err := s.Dump(t.Context())
			if (err != nil) != tc.wantError || candidate.Complete == tc.wantError {
				t.Fatalf("%s: complete %v err %v", tc.expectedOutcome, candidate.Complete, err)
			}
			if tc.wantError {
				return
			}
			if len(candidate.Devices) != 2 || len(candidate.Statistics) != 2 || candidate.Devices[0].Device.Eligibility != model.Eligible {
				t.Fatal(tc.expectedOutcome)
			}
			key := candidate.Devices[0].Device.Key
			observation, stats, err := s.QueryStatistics(t.Context(), key)
			if err != nil || observation.Device.Key != key || !stats.Observed.Present || route.dumps != 1 || devlink.dumps != 1 {
				t.Fatalf("%s: query %v", tc.expectedOutcome, err)
			}
			if _, err := s.Query(t.Context(), model.DeviceKey{Namespace: 1, Kind: model.DeviceEthernet, Index: 3}); !errors.Is(err, unix.ENODEV) {
				t.Fatalf("missing interface: %v", err)
			}
		})
	}
}

func TestIdentityAssociationCache(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		rename, replace, portChange                  bool
		want                                         model.Eligibility
	}{
		{"rename", "positive", "same hardware renamed after dump", "preserve verified association", true, false, false, model.Eligible},
		{"replacement", "corner", "ifindex reused with different ancestry", "old association cannot classify replacement", false, true, false, model.EligibilityUnknown},
		{"port change", "corner", "physical port evidence changes after dump", "unknown until fresh devlink dump", false, false, true, model.EligibilityUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			clock := testkit.NewClock(time.Unix(100, 0))
			route := &fakeIdentityRoute{links: []xtcpnl.LinkInfo{{Index: 1, Name: "eth0", Type: unix.ARPHRD_ETHER}}}
			devlink := &fakeDevlink{ports: []linuxio.DevlinkPort{{Netdev: 1, HasFlavor: true}}}
			path := "/sys/devices/original"
			s := &ethernetInventory{namespace: 1, clock: clock, route: route, devlink: devlink,
				metadata: func(context.Context, string, uint32) (hardwareMetadata, error) {
					if err := clock.Advance(time.Second); err != nil {
						t.Fatal(err)
					}
					return hardwareMetadata{path: path}, nil
				}}
			first, err := s.Dump(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if first.Statistics[0].Observed.Value.Monotonic != 0 {
				t.Fatal("metadata latency renewed route statistics")
			}
			if tc.rename {
				route.links[0].Name = "renamed0"
			}
			if tc.replace {
				path = "/sys/devices/replacement"
			}
			if tc.portChange {
				route.links[0].Detail.PhysPortName = "new-port"
			}
			o, err := s.Query(t.Context(), first.Devices[0].Device.Key)
			if err != nil || o.Device.Eligibility != tc.want || devlink.dumps != 1 {
				t.Fatalf("%s: eligibility %v err %v", tc.expectedOutcome, o.Device.Eligibility, err)
			}
			if (first.Devices[0].Device.HardwareID != o.Device.HardwareID) != tc.replace {
				t.Fatal("hardware identity did not track ancestry")
			}
			fresh, err := s.Dump(t.Context())
			if err != nil || fresh.Devices[0].Device.Eligibility != model.Eligible || devlink.dumps != 2 {
				t.Fatalf("resync did not replace association snapshot: %v", err)
			}
		})
	}
}
