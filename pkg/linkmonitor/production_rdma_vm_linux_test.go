//go:build rdma_vm

package linkmonitor

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

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
		// The production rdma_state path uses the single-port GET, whose kernel
		// reply type differs from the request's; the dumps above do not cover it.
		got, err := c.GetRDMAPort(t.Context(), d.Index, 1)
		if err != nil || len(got.Values) != 1 || got.Values[0].Port != 1 || !got.Values[0].HasState || !got.Values[0].HasPhysical {
			t.Fatal("single-port GET", got, err)
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

// TestSoftwareRDMAEventsSurviveRename renames the RoCE netdev under a live NLDEV
// subscription. The kernel's NETDEV_RENAME notification carries only the netdev
// index and name, so a decoder that requires the device handle ends the
// subscription and with it RDMA event coverage.
func TestSoftwareRDMAEventsSurviveRename(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	n, err := linuxio.OpenRDMANotifications(ctx)
	if err != nil {
		t.Fatal("open notifications", err)
	}
	t.Cleanup(func() { _ = n.Close() })
	hints := make(chan struct{}, 64)
	done := make(chan error, 1)
	go func() {
		done <- n.Run(ctx, func() bool {
			select {
			case hints <- struct{}{}:
			default:
			}
			return true
		})
	}()
	rename := func(from, to string) [][]string {
		return [][]string{{"link", "set", from, "down"}, {"link", "set", from, "name", to}, {"link", "set", to, "up"}}
	}
	for _, tc := range []struct {
		category, description, expected string
		commands                        [][]string
		wantHint                        bool
	}{
		{"positive", "rename the RoCE netdev lm0 to lmr0", "NETDEV_RENAME hint; subscription stays open", rename("lm0", "lmr0"), true},
		{"boundary", "rename to a 15-byte name, the IFNAMSIZ maximum", "hint; subscription stays open", rename("lmr0", "lmr0123456789ab"), true},
		{"negative", "carrier down and up without a rename", "no notification required; subscription stays open",
			[][]string{{"link", "set", "lmr0123456789ab", "down"}, {"link", "set", "lmr0123456789ab", "up"}}, false},
		{"corner", "rename back to lm0", "hint; association restored under the original name", rename("lmr0123456789ab", "lm0"), true},
	} {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			for len(hints) > 0 {
				<-hints
			}
			for _, args := range tc.commands {
				if out, err := exec.CommandContext(ctx, "ip", args...).CombinedOutput(); err != nil {
					t.Fatalf("ip %v: %v: %s", args, err, out)
				}
			}
			if tc.wantHint {
				select {
				case <-hints:
				case err := <-done:
					t.Fatal("subscription ended", err)
				case <-time.After(5 * time.Second):
					t.Fatal("no notification for the rename")
				}
			}
			select {
			case err := <-done:
				t.Fatal("subscription ended", err)
			case <-time.After(500 * time.Millisecond):
			}
		})
	}
	softwareDevice(t, true)
}
