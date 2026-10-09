package linkmonitor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/linuxio"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/testkit"
	"golang.org/x/sys/unix"
)

type fakeRDMARequests struct {
	devices, ports []linuxio.RDMAPort
	err            error
}

func (f *fakeRDMARequests) DumpRDMADevices(context.Context) (linuxio.Result[linuxio.RDMAPort], error) {
	return linuxio.Result[linuxio.RDMAPort]{Values: f.devices}, f.err
}
func (f *fakeRDMARequests) DumpRDMAPorts(context.Context, uint32) (linuxio.Result[linuxio.RDMAPort], error) {
	return linuxio.Result[linuxio.RDMAPort]{Values: f.ports}, f.err
}
func (f *fakeRDMARequests) GetRDMAPort(context.Context, uint32, uint32) (linuxio.Result[linuxio.RDMAPort], error) {
	return linuxio.Result[linuxio.RDMAPort]{Values: f.ports}, f.err
}
func (*fakeRDMARequests) Close() error { return nil }

type rdmaFixtureInventory struct{ candidate model.Candidate }

func (f rdmaFixtureInventory) Dump(context.Context) (model.Candidate, error) {
	c := f.candidate
	c.Devices = append([]model.Observation(nil), c.Devices...)
	return c, nil
}
func (rdmaFixtureInventory) Query(context.Context, model.DeviceKey) (model.Observation, error) {
	return model.Observation{}, unix.ENODEV
}
func (rdmaFixtureInventory) Close() error { return nil }

func rdmaWrite(t testing.TB, path, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
}

func rdmaFixture(t testing.TB, layer string) (*rdmaInventory, *fakeRDMARequests, string) {
	t.Helper()
	root := t.TempDir()
	files := rdmaFilesystem{root: filepath.Join(root, "class", "infiniband"), netRoot: filepath.Join(root, "class", "net")}
	port := filepath.Join(files.root, "mlx5_0", "ports", "1")
	for name, value := range map[string]string{"link_layer": layer, "state": "4: ACTIVE", "phys_state": "5: LinkUp", "gid_attrs/types/0": "RoCE v2", "gid_attrs/ndevs/0": "eth1"} {
		rdmaWrite(t, filepath.Join(port, name), value)
	}
	hardware := filepath.Join(root, "devices", "pci0000:00", "0000:01:00.0")
	if err := os.MkdirAll(hardware, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(hardware, filepath.Join(files.root, "mlx5_0", "device")); err != nil {
		t.Fatal(err)
	}
	r := newReducer(1)
	o := observed(r, 1, true)
	for name, value := range map[string]string{"ifindex": "1", "iflink": "1", "type": "1", "dev_port": "0"} {
		rdmaWrite(t, filepath.Join(files.netRoot, "eth1", name), value)
	}
	if err := os.Symlink(hardware, filepath.Join(files.netRoot, "eth1", "device")); err != nil {
		t.Fatal(err)
	}
	p := linuxio.RDMAPort{Name: "mlx5_0", Port: 1, State: 4, Physical: 5, HasState: true, HasPhysical: true}
	client := &fakeRDMARequests{devices: []linuxio.RDMAPort{p}, ports: []linuxio.RDMAPort{p}}
	s := &rdmaInventory{ethernet: rdmaFixtureInventory{model.Candidate{Complete: true, Devices: []model.Observation{o}}}, client: client, files: files, clock: testkit.NewClock(time.Unix(100, 0)), namespace: 1}
	return s, client, port
}

func TestRDMADiscoveryTable(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		layer                                        string
		change                                       func(*testing.T, *rdmaInventory, *fakeRDMARequests, string)
		bad                                          bool
		devices, ports                               int
		unknown                                      uint64
	}{
		{"roce", "positive", "hardware Ethernet RDMA association", "one counted Ethernet identity", "Ethernet", nil, false, 1, 1, 0},
		{"native", "positive", "native port without IPoIB", "native identity independent of Ethernet", "InfiniBand", nil, false, 2, 1, 0},
		{"empty", "boundary", "complete empty RDMA dump", "Ethernet retained", "Ethernet", func(_ *testing.T, _ *rdmaInventory, f *fakeRDMARequests, _ string) { f.devices = nil }, false, 1, 0, 0},
		{"duplicate", "negative", "duplicate device identity", "reject whole candidate", "Ethernet", func(_ *testing.T, _ *rdmaInventory, f *fakeRDMARequests, _ string) {
			f.devices = append(f.devices, f.devices[0])
		}, true, 0, 0, 0},
		{"portDuplicate", "negative", "duplicate port identity", "reject whole candidate", "Ethernet", func(_ *testing.T, _ *rdmaInventory, f *fakeRDMARequests, _ string) {
			f.ports = append(f.ports, f.ports[0])
		}, true, 0, 0, 0},
		{"permission", "negative", "netlink access denied", "no fallback inventory", "Ethernet", func(_ *testing.T, s *rdmaInventory, f *fakeRDMARequests, _ string) {
			f.err = unix.EACCES
			s.files.namespaceVerified = true
		}, true, 0, 0, 0},
		{"fallback", "positive", "unsupported netlink and verified sysfs namespace", "sysfs discovers state and port", "InfiniBand", func(_ *testing.T, s *rdmaInventory, f *fakeRDMARequests, _ string) {
			f.err = unix.EOPNOTSUPP
			s.files.namespaceVerified = true
		}, false, 2, 1, 0},
		{"unverified", "negative", "unsupported netlink and unverified namespace", "no false empty success", "InfiniBand", func(_ *testing.T, _ *rdmaInventory, f *fakeRDMARequests, _ string) { f.err = unix.EOPNOTSUPP }, true, 0, 0, 0},
		{"unknownLayer", "corner", "future transport layer", "discovery uncertainty without count", "future", nil, false, 1, 1, 1},
		{"missingAssociation", "negative", "GID points to absent netdev", "unknown association without extra count", "Ethernet", func(t *testing.T, _ *rdmaInventory, _ *fakeRDMARequests, p string) {
			rdmaWrite(t, filepath.Join(p, "gid_attrs/ndevs/0"), "missing0")
		}, false, 1, 1, 1},
		{"largeAdvertised", "boundary", "device claims maximum uint32 ports but emits one", "allocation based on received records", "Ethernet", func(_ *testing.T, _ *rdmaInventory, f *fakeRDMARequests, _ string) { f.devices[0].Port = ^uint32(0) }, false, 1, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			s, f, path := rdmaFixture(t, tc.layer)
			if tc.change != nil {
				tc.change(t, s, f, path)
			}
			c, err := s.Dump(t.Context())
			if (err != nil) != tc.bad || len(c.Devices) != tc.devices || len(c.RDMAPorts) != tc.ports || c.RDMAUncertain != tc.unknown {
				t.Fatalf("%s: devices=%d ports=%d unknown=%d err=%v", tc.expectedOutcome, len(c.Devices), len(c.RDMAPorts), c.RDMAUncertain, err)
			}
			if !tc.bad && tc.ports != 0 && tc.unknown == 0 {
				p := c.RDMAPorts[0]
				if p.Eligibility != model.Eligible || p.Canonical.Namespace != 1 {
					t.Fatal(tc.expectedOutcome)
				}
				if tc.layer == "InfiniBand" && c.Devices[1].Device.Name != "rdma:mlx5_0:1" {
					t.Fatal("native canonical identity missing")
				}
			}
		})
	}
}

func TestRDMASysfsTable(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		input                                        string
		bad                                          bool
		value                                        uint8
	}{
		{"active", "positive", "documented state format", "parse ACTIVE", "4: ACTIVE\n", false, 4},
		{"zero", "boundary", "zero enum", "preserve unknown numeric zero", "0: future", false, 0},
		{"future", "corner", "future enum", "preserve value for unknown policy", "255", false, 255},
		{"overflow", "negative", "enum above uint8", "reject malformed scalar", "256", true, 0},
		{"negative", "negative", "negative enum", "reject malformed scalar", "-1", true, 0},
		{"empty", "boundary", "empty state", "reject absent numeric field", "", true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			v, err := parseRDMAState(tc.input)
			if (err != nil) != tc.bad || (!tc.bad && (!v.Present || v.Value != tc.value)) {
				t.Fatalf("%s: %+v %v", tc.expectedOutcome, v, err)
			}
		})
	}
	for _, n := range []int{0, rdmaScalarLimit, rdmaScalarLimit + 1} {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			t.Log("boundary: bounded file read; expected exact limit accepted, excess rejected")
			path := filepath.Join(t.TempDir(), "value")
			rdmaWrite(t, path, strings.Repeat("x", n))
			value, err := rdmaScalar(t.Context(), path)
			if n > rdmaScalarLimit {
				if !errors.Is(err, linuxio.ErrLimit) {
					t.Fatal(err)
				}
			} else if err != nil || len(value) != n {
				t.Fatal(value, err)
			}
		})
	}
}

func TestRDMAGIDVersions(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		values, want                                 []string
	}{
		{"v1", "positive", "only v1 GID type", "only v1 evidence", []string{"IB/RoCE v1"}, []string{"v1"}},
		{"both", "positive", "v1 and v2 GID types", "both versions without duplicate aliases", []string{"IB/RoCE v1", "RoCE v2"}, []string{"v1", "v2"}},
		{"future", "corner", "unrecognized GID type", "unknown version", []string{"future"}, []string{"unknown"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			s, _, path := rdmaFixture(t, "Ethernet")
			for i, value := range tc.values {
				rdmaWrite(t, filepath.Join(path, "gid_attrs/types", strconv.Itoa(i)), value)
				rdmaWrite(t, filepath.Join(path, "gid_attrs/ndevs", strconv.Itoa(i)), "eth1")
			}
			c, err := s.Dump(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			p := c.RDMAPorts[0]
			if !reflect.DeepEqual(p.Versions, tc.want) || len(p.Aliases) != 1 {
				t.Fatalf("%s: %+v", tc.expectedOutcome, p)
			}
		})
	}
}

func FuzzRDMASysfsState(f *testing.F) {
	f.Add("4: ACTIVE")
	f.Add("256")
	f.Fuzz(func(t *testing.T, text string) {
		v, err := parseRDMAState(text)
		if err == nil && !v.Present {
			t.Fatal("success without presence")
		}
	})
}
