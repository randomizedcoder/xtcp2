package linkmonitor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/testkit"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

func TestSettingsLiveReadOnly(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		kind                                         xtcpnl.EthtoolKind
	}{
		{"modes", "positive", "read loopback modern link settings", "supported response or explicit unsupported", xtcpnl.EthtoolLinkModesKind},
		{"channels", "positive", "read loopback channel configuration", "supported response or explicit unsupported", xtcpnl.EthtoolChannelsKind},
		{"rings", "positive", "read loopback ring configuration", "supported response or explicit unsupported", xtcpnl.EthtoolRingsKind},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			c, err := newEthtoolIoctl()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := c.Close(); err != nil {
					t.Error(err)
				}
			})
			ifr, err := unix.NewIfreq("lo")
			if err != nil {
				t.Fatal(err)
			}
			if err := unix.IoctlIfreq(c.fd, unix.SIOCGIFINDEX, ifr); err != nil {
				t.Fatal(err)
			}
			_, err = c.read(t.Context(), "lo", ifr.Uint32(), tc.kind)
			if err != nil && !unsupportedEthtool(err) {
				t.Fatalf("%s: %v", tc.expectedOutcome, err)
			}
			_, err = c.read(t.Context(), "lo", ifr.Uint32()+1, tc.kind)
			if !errors.Is(err, unix.ENODEV) {
				t.Fatalf("mismatched identity accepted: %v", err)
			}
		})
	}
}

func TestSettingsEnabledSession(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		traffic                                      bool
	}{
		{"settings", "positive", "settings resources with empty inventory", "startup and joined shutdown", false},
		{"with traffic", "corner", "settings and traffic decorators share four workers", "startup and joined shutdown", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			s, m, _ := sessionFixture(t)
			s.settings, s.statistics = true, tc.traffic
			cancel, done := runSession(t, m)
			awaitSnapshot(t, m, func(s Snapshot) bool { return s.Health().Ready })
			cancel()
			if err := receiveTest(t, done); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestIdentityLiveReadOnly(t *testing.T) {
	t.Log("positive: real read-only inventory; expected: complete inventory with matching owned statistics, without hardware count assumptions")
	s, err := newEthernetInventory(1, "/sys/class/net", testkit.NewClock(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	candidate, err := s.Dump(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !candidate.Complete || len(candidate.Devices) != len(candidate.Statistics) {
		t.Fatal("incomplete inventory")
	}
	for _, observation := range candidate.Devices {
		if observation.Device.Key.Kind != model.DeviceEthernet {
			t.Fatal("incorrect inventory key")
		}
	}
}
