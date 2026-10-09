package linkmonitor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"golang.org/x/sys/unix"
)

func TestRDMALowerGraph(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		lower                                        uint32
		foreign, eligible                            bool
		want                                         model.Eligibility
	}{
		{"vlan", "positive", "VLAN lower points to local hardware", "one eligible physical target", 1, false, true, model.Eligible},
		{"foreign", "negative", "foreign namespace lower with colliding index", "unknown association", 1, true, true, model.EligibilityUnknown},
		{"cycle", "corner", "self-referential excluded device", "excluded rather than invented hardware", 2, false, true, model.Excluded},
		{"missing", "negative", "missing lower target", "unknown association", 3, false, true, model.EligibilityUnknown},
		{"representor", "negative", "lower is excluded representor", "excluded lower is not physical evidence", 1, false, false, model.Excluded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			r := newReducer(1)
			d := observed(r, 1, true).Device
			if !tc.eligible {
				d.Eligibility = model.Excluded
			}
			upper := observed(r, 2, true).Device
			upper.Eligibility = model.Excluded
			graph := newRDMAGraph(map[string]rdmaNetdev{"eth1": {index: 1, lower: 1}, "vlan1": {index: 2, lower: tc.lower}}, []model.NetdevLink{{Index: 2, Lower: tc.lower, Foreign: tc.foreign}})
			got := resolveRDMALower("vlan1", graph, map[uint32]model.Device{1: d, 2: upper})
			if got.eligibility != tc.want || (tc.want == model.Eligible && got.key != d.Key) {
				t.Fatalf("%s: %+v", tc.expectedOutcome, got)
			}
		})
	}
}

func TestRDMANativeAliases(t *testing.T) {
	t.Log("corner: parent IPoIB and P_Key child; expected one native port and two aliases, unrelated Ethernet alias omitted")
	s, _, _ := rdmaFixture(t, rdmaNativeLayer)
	for i, name := range []string{"ib0", "ib0.8001"} {
		path := filepath.Join(s.files.netRoot, name)
		for field, value := range map[string]string{"ifindex": fmt.Sprint(i + 2), "iflink": "2", "type": "32", "dev_port": "0"} {
			rdmaWrite(t, filepath.Join(path, field), value)
		}
		hardware, err := filepath.EvalSymlinks(filepath.Join(s.files.netRoot, "eth1", "device"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(hardware, filepath.Join(path, "device")); err != nil {
			t.Fatal(err)
		}
	}
	c, err := s.Dump(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Devices) != 2 || len(c.RDMAPorts[0].Aliases) != 2 || c.RDMAPorts[0].Aliases[0] != "ib0" || c.RDMAPorts[0].Aliases[1] != "ib0.8001" {
		t.Fatal("aliases multiplied count or included unrelated netdev")
	}
}

func TestRDMASoftwareAndPathEscape(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		escape                                       bool
	}{
		{"software", "negative", "class entry under devices/virtual", "excluded software provider", false},
		{"escape", "negative", "class entry points outside configured sysfs", "reject discovery", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			s, _, path := rdmaFixture(t, rdmaEthernetLayer)
			old := filepath.Dir(filepath.Dir(path))
			base := filepath.Dir(filepath.Dir(s.files.root))
			if tc.escape {
				base = t.TempDir()
			}
			destination := filepath.Join(base, "devices", "virtual", "infiniband", "mlx5_0")
			if err := os.MkdirAll(filepath.Dir(destination), 0750); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(old, destination); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(destination, old); err != nil {
				t.Fatal(err)
			}
			c, err := s.Dump(t.Context())
			if tc.escape {
				if err == nil {
					t.Fatal(tc.expectedOutcome)
				}
				return
			}
			if err != nil || c.RDMAPorts[0].Eligibility != model.Excluded || c.RDMAUncertain != 0 || len(c.Devices) != 1 {
				t.Fatalf("%s: %v", tc.expectedOutcome, err)
			}
		})
	}
}

func TestRDMAOptionalSaturation(t *testing.T) {
	t.Log("corner: all four optional workers occupied; expected independent RDMA completion and bounded occupancy")
	scheduler, clock := testScheduler(t)
	for i := range collectorWorkers {
		registerTestJob(t, scheduler, uint32(i+1), model.CollectorDriver, schedulePolicy{})
	}
	dispatchTest(t, scheduler)
	for i := range collectorWorkers {
		if scheduler.active[i] == nil {
			t.Fatal("worker not occupied")
		}
	}
	s, _, p := rdmaScheduled(t, false)
	s.executor.ctx = context.Background()
	if err := s.advance(clock.Now()); err != nil {
		t.Fatal(err)
	}
	work := <-s.executor.inbox
	if err := s.complete(rdmaCompletion{work: work, ports: []model.RDMAPort{p}, finished: clock.Now()}); err != nil {
		t.Fatal(err)
	}
	if !s.r.slots[0].collectors[model.CollectorRDMAState].fresh {
		t.Fatal("required state blocked by optional pool")
	}
	for i := range collectorWorkers {
		if scheduler.active[i] == nil {
			t.Fatal("optional occupancy changed")
		}
	}
}

func BenchmarkRDMAAssociation(b *testing.B) {
	for _, count := range []int{0, 1, 32, 256} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			devices := make(map[uint32]model.Device)
			links := make(map[string]rdmaNetdev)
			for i := range count {
				index := uint32(i + 1)
				devices[index] = model.Device{Key: model.DeviceKey{Namespace: 1, Kind: model.DeviceEthernet, Index: index}, Eligibility: model.Eligible}
				links[fmt.Sprint(i)] = rdmaNetdev{index: index, lower: index, linkType: unix.ARPHRD_ETHER}
			}
			graph := newRDMAGraph(links, nil)
			b.ReportAllocs()
			for b.Loop() {
				for name := range links {
					if resolveRDMALower(name, graph, devices).eligibility != model.Eligible {
						b.Fatal("association failed")
					}
				}
			}
		})
	}
}
