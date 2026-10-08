package linkmonitor

import (
	"context"
	"encoding/binary"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"golang.org/x/sys/unix"
)

func TestStatisticSourceIndependence(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		failed                                       model.CollectorKind
		err                                          error
		status                                       model.Support
	}{
		{"unsupported PHY", "positive", "PHY unsupported while driver succeeds", "independent support", model.CollectorPHY, unix.EOPNOTSUPP, model.Unsupported},
		{"unsupported driver", "positive", "driver unsupported while PHY succeeds", "independent support", model.CollectorDriver, unix.EOPNOTSUPP, model.Unsupported},
		{"denied PHY", "negative", "PHY denied while driver succeeds", "failure never becomes unsupported", model.CollectorPHY, unix.EPERM, model.SupportUnknown},
		{"gone driver", "corner", "device disappears during driver query", "error preserved", model.CollectorDriver, unix.ENODEV, model.SupportUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			f := &statisticFixture{names: []string{"raw"}, values: []uint64{42}}
			f.mutate = func(cmd uint32, _ []byte) error {
				if (tc.failed == model.CollectorPHY && cmd == unix.ETHTOOL_GPHYSTATS) || (tc.failed == model.CollectorDriver && cmd == unix.ETHTOOL_GSTATS) {
					return tc.err
				}
				return nil
			}
			c := &statisticsCollector{source: statisticTestSource(t, f)}
			for _, kind := range statisticCollectors {
				result := c.Collect(t.Context(), statisticTestJob(kind))
				if kind == tc.failed {
					if result.Support != tc.status || len(result.Samples) != 0 || (result.Err != nil) != (tc.status == model.SupportUnknown) {
						t.Fatal(tc.expectedOutcome)
					}
				} else if result.Support != model.Supported || len(result.Samples) != 1 {
					t.Fatal(tc.expectedOutcome)
				}
			}
		})
	}
}

func TestStatisticSourceRouting(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		kind                                         model.DeviceKind
		eligibility                                  model.Eligibility
		status                                       model.Support
	}{
		{"eligible down", "positive", "down Ethernet including RoCE", "read succeeds", model.DeviceEthernet, model.Eligible, model.Supported},
		{"unknown", "boundary", "eligibility not established", "unknown without IO", model.DeviceEthernet, model.EligibilityUnknown, model.SupportUnknown},
		{"excluded", "negative", "virtual or excluded netdev", "not applicable without IO", model.DeviceEthernet, model.Excluded, model.NotApplicable},
		{"native", "corner", "native RDMA port", "not applicable without IO", model.DeviceNativeRDMA, model.Eligible, model.NotApplicable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			f := &statisticFixture{}
			c := &statisticsCollector{source: statisticTestSource(t, f)}
			job := statisticTestJob(model.CollectorDriver)
			job.Device.Key.Kind, job.Device.Eligibility = tc.kind, tc.eligibility
			result := c.Collect(t.Context(), job)
			if result.Support != tc.status || result.Err != nil || (len(f.calls) > 0) != (tc.status == model.Supported) {
				t.Fatal(tc.expectedOutcome)
			}
			if f.calls[unix.ETHTOOL_GSTRINGS] != 0 || f.calls[unix.ETHTOOL_GSTATS] != 0 {
				t.Fatal("zero count invoked variable IO")
			}
		})
	}
}

func TestStatisticCancellationAndIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		cancelAt                                     uint32
		before, replaced                             bool
	}{
		{"before", "boundary", "already canceled context", "no ioctl", 0, true, false},
		{"after count", "corner", "cancel during count", "no strings or values", unix.ETHTOOL_GDRVINFO, false, false},
		{"after names", "corner", "cancel during strings", "no values", unix.ETHTOOL_GSTRINGS, false, false},
		{"replacement", "negative", "identity changes after data read", "reject otherwise successful values", 0, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.before {
				cancel()
			}
			f := &statisticFixture{names: []string{"rx"}, values: []uint64{8}}
			f.mutate = func(cmd uint32, _ []byte) error {
				if cmd == tc.cancelAt {
					cancel()
				}
				return nil
			}
			c := statisticTestSource(t, f)
			checks := 0
			c.ioctl.validate = func(ctx context.Context, _ string, _ uint32) error {
				checks++
				if tc.replaced && checks > 1 {
					return unix.ENODEV
				}
				return ctx.Err()
			}
			_, _, err := c.read(ctx, statisticTestJob(model.CollectorDriver))
			if err == nil || (tc.before && len(f.calls) != 0) || (!tc.replaced && f.calls[unix.ETHTOOL_GSTATS] != 0) {
				t.Fatal(tc.expectedOutcome)
			}
		})
	}
}

func TestStatisticNoMatchStillValidates(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		malformed                                    bool
	}{
		{"valid", "boundary", "filter excludes all names", "empty supported publication after values query", false},
		{"invalid", "negative", "filtered source has malformed value count", "failed publication", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			f := &statisticFixture{names: []string{"rx"}, values: []uint64{1}}
			if tc.malformed {
				f.mutate = func(cmd uint32, b []byte) error {
					if cmd == unix.ETHTOOL_GSTATS {
						binary.NativeEndian.PutUint32(b[4:], 0)
					}
					return nil
				}
			}
			c := statisticTestSource(t, f)
			c.exclude = c.include
			samples, _, err := c.read(t.Context(), statisticTestJob(model.CollectorDriver))
			if (err != nil) != tc.malformed || len(samples) != 0 || f.calls[unix.ETHTOOL_GSTATS] == 0 {
				t.Fatal(tc.expectedOutcome)
			}
		})
	}
}
