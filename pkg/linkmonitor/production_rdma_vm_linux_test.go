//go:build rdma_vm

package linkmonitor

import (
	"os"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/linuxio"
	"golang.org/x/sys/unix"
)

func softwareDevice(t *testing.T, present bool) {
	t.Helper()
	name := os.Getenv("LINKMONITOR_SOFTWARE_RDMA")
	if name == "" {
		t.Fatal("software RDMA fixture name is required")
	}
	c, err := linuxio.NewClient(unix.NETLINK_RDMA)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Error(err)
		}
	})
	devices, err := c.DumpRDMADevices(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range devices.Values {
		if d.Name != name {
			continue
		}
		found = true
		ports, err := c.DumpRDMAPorts(t.Context(), d.Index)
		if err != nil || len(ports.Values) != 1 {
			t.Fatal("expected one software port", ports, err)
		}
		p := ports.Values[0]
		if p.Port != 1 || !p.HasNetdev || p.NetdevName != "lm0" {
			t.Fatal("missing software Ethernet association", p)
		}
	}
	if found != present {
		t.Fatalf("device present=%v, expected %v", found, present)
	}
}

func TestSoftwareRDMADiscovery(t *testing.T) {
	t.Log("positive/corner: software device creation/recreation; expected: device and port with Ethernet association")
	softwareDevice(t, true)
}

func TestSoftwareRDMARemoved(t *testing.T) {
	t.Log("negative/boundary: deleted software device; expected: no remaining NLDEV identity")
	softwareDevice(t, false)
}
