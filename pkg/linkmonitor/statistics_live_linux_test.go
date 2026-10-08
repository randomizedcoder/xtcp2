package linkmonitor

import (
	"errors"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"golang.org/x/sys/unix"
)

func TestStatisticLiveReadOnly(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		kind                                         model.CollectorKind
	}{
		{"driver", "positive", "loopback driver query", "valid response or explicit unsupported", model.CollectorDriver},
		{"phy", "positive", "loopback PHY query", "valid response or explicit unsupported", model.CollectorPHY},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			cfg, err := validateConfig(DefaultConfig())
			if err != nil {
				t.Fatal(err)
			}
			ioctl, err := newEthtoolIoctl()
			if err != nil {
				t.Fatal(err)
			}
			c := &statisticsIoctl{ioctl: ioctl, include: cfg.include, exclude: cfg.exclude}
			t.Cleanup(func() {
				if err := c.Close(); err != nil {
					t.Error(err)
				}
			})
			ifr, err := unix.NewIfreq("lo")
			if err != nil {
				t.Fatal(err)
			}
			if err := unix.IoctlIfreq(ioctl.fd, unix.SIOCGIFINDEX, ifr); err != nil {
				t.Fatal(err)
			}
			job := statisticTestJob(tc.kind)
			job.Device.Name, job.Key.Device.Index = "lo", ifr.Uint32()
			if _, _, err := c.read(t.Context(), job); err != nil && !unsupportedEthtool(err) {
				t.Fatal(err)
			}
			job.Key.Device.Index++
			if _, _, err := c.read(t.Context(), job); !errors.Is(err, unix.ENODEV) {
				t.Fatalf("identity mismatch: %v", err)
			}
		})
	}
}

func TestStatisticEnabledSession(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		other                                        bool
	}{
		{"statistics", "positive", "statistics resources with empty inventory", "startup and joined shutdown", false},
		{"combined", "corner", "all implemented Ethernet adapters", "four shared workers and joined shutdown", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			s, m, _ := sessionFixture(t)
			s.driverStatistics, s.statistics, s.settings = true, tc.other, tc.other
			cancel, done := runSession(t, m)
			awaitSnapshot(t, m, func(s Snapshot) bool { return s.Health().Ready })
			cancel()
			if err := receiveTest(t, done); err != nil {
				t.Fatal(err)
			}
		})
	}
}
