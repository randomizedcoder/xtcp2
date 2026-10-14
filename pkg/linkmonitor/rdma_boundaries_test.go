package linkmonitor

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/linuxio"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"golang.org/x/sys/unix"
)

func TestRDMACandidateBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		count                                        int
		bad                                          bool
	}{
		{"empty", "boundary", "zero ports", "complete empty candidate accepted", 0, false},
		{"one", "positive", "one native identity", "matching canonical port accepted", 1, false},
		{"limit", "boundary", "65536 native ports", "exact inventory bound accepted", maxInventoryDevices, false},
		{"excess", "negative", "65537 native ports", "reject before processing oversized candidate", maxInventoryDevices + 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			c := model.Candidate{RDMAPorts: make([]model.RDMAPort, tc.count)}
			devices := make(map[model.DeviceKey]*model.Observation, tc.count)
			for i := range c.RDMAPorts {
				key := model.DeviceKey{Namespace: 1, Kind: model.DeviceNativeRDMA, RDMADevice: "hca", Port: uint32(i + 1)}
				c.RDMAPorts[i] = model.RDMAPort{Device: "hca", Port: key.Port, Layer: rdmaNativeLayer, Canonical: key, Eligibility: model.Eligible}
				devices[key] = &model.Observation{Device: model.Device{Key: key, Eligibility: model.Eligible}}
			}
			if err := validateRDMACandidate(&c, devices); (err != nil) != tc.bad {
				t.Fatal(tc.expectedOutcome, err)
			}
		})
	}
}

func TestRDMAStateReaderTable(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		change                                       string
		want                                         error
	}{
		{"state", "positive", "matching device/port/hardware state", "ACTIVE and LINK_UP", "", nil},
		{"missing", "corner", "optional netlink state fields absent", "read sysfs state for verified device", "missing", nil},
		{"reassociated", "negative", "same port now attached to another netdev", "reject old association", "reassociated", linuxio.ErrEpoch},
		{"renamed", "negative", "response has changed RDMA name", "reject mismatched reply", "renamed", linuxio.ErrReply},
		{"denied", "negative", "netlink permission denied", "no fallback", "denied", unix.EACCES},
		{"fallback", "positive", "sysfs-origin port and verified namespace", "read sysfs without treating index zero as assigned", "fallback", nil},
		{"canceled", "corner", "canceled before acquisition", "context error without reading", "canceled", context.Canceled},
		{"steady-no-sysfs", "positive", "matching netlink reply with the sysfs tree gone", "state from netlink alone: a steady read touches no sysfs file", "no-sysfs", nil},
		{"removed", "negative", "device removed: GET_PORT fails with EINVAL and its class entry is gone", "ENOENT from the identity, which asks for a resync", "removed", fs.ErrNotExist},
		{"replaced", "negative", "device re-registered on other hardware: GET_PORT on the old index fails", "ErrEpoch from the identity", "replaced", linuxio.ErrEpoch},
		{"renamed-removed", "corner", "reply has another name and the class entry is gone", "ENOENT: the identity classifies a mismatched reply", "renamed-removed", fs.ErrNotExist},
		{"denied-removed", "negative", "netlink permission denied and the device removed", "ENOENT rather than EACCES", "denied-removed", fs.ErrNotExist},
		{"replaced-reply-matches", "corner", "hardware link changed but the reply matches name, index and port", "accepted: a matching registration cannot change hardware, so it is not rechecked", "replaced-matching", nil},
		{"missing-replaced", "negative", "optional netlink state absent and the hardware changed", "ErrEpoch from the guard around the sysfs read", "missing-replaced", linuxio.ErrEpoch},
		{"fallback-replaced", "negative", "sysfs-origin port whose hardware changed", "ErrEpoch before reading sysfs", "fallback-replaced", linuxio.ErrEpoch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			inventory, client, _ := rdmaFixture(t, rdmaNativeLayer)
			c, err := inventory.Dump(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			p := c.RDMAPorts[0]
			reader := &rdmaStateReader{client: client, files: inventory.files}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch tc.change {
			case "missing":
				client.ports[0].HasState, client.ports[0].HasPhysical = false, false
			case "reassociated":
				client.ports[0].HasNetdev, client.ports[0].Netdev, client.ports[0].NetdevName = true, 2, "eth2"
			case "renamed":
				client.ports[0].Name = "other"
			case "denied":
				client.err = unix.EACCES
			case "fallback":
				p.Sysfs, reader.files.namespaceVerified = true, true
				client.err = unix.EACCES
			case "canceled":
				cancel()
			case "no-sysfs":
				rdmaRemoveAll(t, reader.files.root)
			case "removed":
				rdmaRemoveAll(t, filepath.Join(reader.files.root, p.Device))
				client.err = unix.EINVAL
			case "replaced":
				rdmaRelink(t, reader.files, p.Device)
				client.err = unix.EINVAL
			case "renamed-removed":
				client.ports[0].Name = "other"
				rdmaRemoveAll(t, filepath.Join(reader.files.root, p.Device))
			case "denied-removed":
				client.err = unix.EACCES
				rdmaRemoveAll(t, filepath.Join(reader.files.root, p.Device))
			case "replaced-matching":
				rdmaRelink(t, reader.files, p.Device)
			case "missing-replaced":
				client.ports[0].HasState, client.ports[0].HasPhysical = false, false
				rdmaRelink(t, reader.files, p.Device)
			case "fallback-replaced":
				p.Sysfs, reader.files.namespaceVerified = true, true
				rdmaRelink(t, reader.files, p.Device)
			}
			got, err := reader.Read(ctx, p)
			if !errors.Is(err, tc.want) {
				t.Fatalf("%s: %v", tc.expectedOutcome, err)
			}
			if err == nil && nativeRDMAUp(got.State, got.Physical) != presentValue(true) {
				t.Fatal(tc.expectedOutcome)
			}
		})
	}
}

func rdmaRemoveAll(t *testing.T, path string) {
	t.Helper()
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
}

// rdmaRelink points device's hardware link at another PCI function, as a
// device re-registered on other hardware under the same name has.
func rdmaRelink(t *testing.T, files rdmaFilesystem, device string) {
	t.Helper()
	hardware := filepath.Join(filepath.Dir(filepath.Dir(files.root)), "devices", "pci0000:00", "0000:02:00.0")
	link := filepath.Join(files.root, device, "device")
	if err := os.MkdirAll(hardware, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(hardware, link); err != nil {
		t.Fatal(err)
	}
}

func TestRDMASampleBound(t *testing.T) {
	for _, size := range []int{maximumSamples - 9, maximumSamples - 8} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			t.Log("boundary: projected sample count at/above limit; expected bounded result before allocating samples")
			p := model.RDMAPort{Aliases: make([]string, size)}
			if boundedRDMASamples([]model.RDMAPort{p}) != (size+9 <= maximumSamples) {
				t.Fatal("sample bound")
			}
		})
	}
}
