package linkmonitor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/linuxio"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/rdmacaps"
	"golang.org/x/sys/unix"
)

func optionalRDMAFixture(t testing.TB) (*rdmaOptionalCollector, model.Job, string) {
	t.Helper()
	i, _, path := rdmaFixture(t, rdmaNativeLayer)
	candidate, err := i.Dump(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	p := candidate.RDMAPorts[0]
	job := model.Job{Key: model.JobKey{Namespace: 1, Device: p.Canonical, Collector: model.CollectorRDMACounters}, Token: model.Token{Generation: 1},
		Device: model.Device{Key: p.Canonical, Eligibility: model.Eligible, Up: presentValue(true)}, RDMA: &model.RDMARequest{Ports: []model.RDMAPort{p}}}
	return newRDMAOptionalCollector(nil, i.files), job, path
}

func TestRDMACapabilityFailures(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		failure                                      error
		support                                      model.Support
		reason                                       model.ErrorReason
	}{
		{"success", "positive", "local capability reply", "speed and width checks pass", nil, model.Supported, model.ErrorNone},
		{"denied", "negative", "UMAD permission denied", "unknown capability with permission diagnostic", unix.EACCES, model.SupportUnknown, model.ErrorPermission},
		{"timeout", "boundary", "UMAD deadline expires", "timeout diagnostic without zero gauges", unix.ETIMEDOUT, model.SupportUnknown, model.ErrorTimeout},
		{"unsupported", "corner", "SMP unavailable", "explicit unsupported", unix.EOPNOTSUPP, model.Unsupported, model.ErrorNone},
		{"core", "corner", "no cgo build", "explicit unsupported", rdmacaps.ErrUnavailable, model.Unsupported, model.ErrorNone},
		{"bad", "negative", "mismatched response", "malformed diagnostic", rdmacaps.ErrReply, model.SupportUnknown, model.ErrorMalformed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			c, job, path := optionalRDMAFixture(t)
			c.query = func(context.Context, string, uint32) (rdmacaps.Capabilities, error) {
				return rdmacaps.Capabilities{Complete: true, Supported: 128, Enabled: 64, Active: 128, WidthSupported: 2, WidthEnabled: 1, WidthActive: 2}, tc.failure
			}
			job.Key.Collector = model.CollectorRDMACapabilities
			r := c.Collect(t.Context(), job)
			if r.Support != tc.support || r.Reason != tc.reason {
				t.Fatalf("%s: %+v", tc.expectedOutcome, r)
			}
			if tc.failure == nil {
				if r.Settings == nil || r.Settings.Speed != model.CheckPass || r.Settings.Width != model.CheckPass {
					t.Fatal(tc.expectedOutcome)
				}
			} else if len(r.Samples) != 0 || r.Settings != nil {
				t.Fatal("failed source published data")
			}
			// Counter access is independent of denied/unsupported capabilities.
			rdmaWrite(t, filepath.Join(path, "counters", "link_downed"), "3")
			job.Key.Collector = model.CollectorRDMACounters
			if r := c.Collect(t.Context(), job); r.Err != nil || r.Support != model.Supported {
				t.Fatal("counter independence", r.Err)
			}
		})
	}
}

func TestRDMAOptionalIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		change                                       string
		want                                         error
	}{
		{"stable", "positive", "identity unchanged", "samples accepted", "", nil},
		{"hardware", "negative", "hardware differs before query", "no stale samples", "hardware", linuxio.ErrEpoch},
		{"layer", "negative", "port mode changed during query", "no stale samples", "layer", linuxio.ErrEpoch},
		{"replacement", "corner", "same pathname new port inode", "no stale samples", "replacement", linuxio.ErrEpoch},
		{"cancel", "boundary", "canceled during query", "late data discarded", "cancel", context.Canceled},
		{"roce", "positive", "RoCE uses Ethernet capabilities", "not applicable and no UMAD query", "roce", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			c, job, path := optionalRDMAFixture(t)
			job.Key.Collector = model.CollectorRDMACapabilities
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			c.query = func(context.Context, string, uint32) (rdmacaps.Capabilities, error) {
				calls++
				switch tc.change {
				case "layer":
					rdmaWrite(t, filepath.Join(path, "link_layer"), rdmaEthernetLayer)
				case "replacement":
					if err := os.Rename(path, path+"-old"); err != nil {
						t.Fatal(err)
					}
					rdmaWrite(t, filepath.Join(path, "link_layer"), rdmaNativeLayer)
				case "cancel":
					cancel()
				}
				return rdmacaps.Capabilities{}, nil
			}
			if tc.change == "hardware" {
				job.RDMA.Ports[0].Hardware += "-other"
			}
			if tc.change == "roce" {
				job.Device.Key.Kind = model.DeviceEthernet
			}
			r := c.Collect(ctx, job)
			if !errors.Is(r.Err, tc.want) {
				t.Fatal(tc.expectedOutcome, r.Err)
			}
			if tc.want != nil && len(r.Samples) != 0 {
				t.Fatal("stale samples published")
			}
			if tc.change == "roce" && (calls != 0 || r.Support != model.NotApplicable) {
				t.Fatal(tc.expectedOutcome)
			}
		})
	}
}
