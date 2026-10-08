package linkmonitor

import (
	"context"
	"errors"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/linuxio"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

type fakeSettingsRequests struct {
	discoverError, getError error
	message                 xtcpnl.EthtoolMessage
	discovers, gets         int
}

func (f *fakeSettingsRequests) DiscoverFamily(context.Context, string) (linuxio.Family, error) {
	f.discovers++
	return linuxio.Family{}, f.discoverError
}
func (f *fakeSettingsRequests) GetEthtool(context.Context, linuxio.Family, xtcpnl.EthtoolKind, uint32) (linuxio.Result[xtcpnl.EthtoolMessage], error) {
	f.gets++
	return linuxio.Result[xtcpnl.EthtoolMessage]{Values: []xtcpnl.EthtoolMessage{f.message}}, f.getError
}
func (*fakeSettingsRequests) Close() error { return nil }

type fakeSettingsFallback struct {
	err            error
	message        xtcpnl.EthtoolMessage
	reads, drivers int
}

func (f *fakeSettingsFallback) read(context.Context, string, uint32, xtcpnl.EthtoolKind) (xtcpnl.EthtoolMessage, error) {
	f.reads++
	return f.message, f.err
}
func (f *fakeSettingsFallback) driver(context.Context, string, uint32) ([]model.Sample, error) {
	f.drivers++
	return nil, f.err
}
func (*fakeSettingsFallback) Close() error { return nil }

func TestSettingsSourceFallback(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		discover, get, fallback                      error
		wantReads                                    int
		wantSupport                                  model.Support
		wantReason                                   model.ErrorReason
	}{
		{"netlink", "positive", "netlink link modes succeed", "no ioctl fallback", nil, nil, nil, 0, model.Supported, model.ErrorNone},
		{"missing family", "negative", "family discovery ENOENT", "modern ioctl fallback", unix.ENOENT, nil, nil, 1, model.Supported, model.ErrorNone},
		{"unsupported operation", "negative", "operation returns EOPNOTSUPP", "fallback once", nil, unix.EOPNOTSUPP, nil, 1, model.Supported, model.ErrorNone},
		{"permission", "negative", "netlink permission denied", "permission failure with no fallback", nil, unix.EPERM, nil, 0, model.SupportUnknown, model.ErrorPermission},
		{"timeout", "boundary", "request deadline expired", "timeout with no fallback", nil, context.DeadlineExceeded, nil, 0, model.SupportUnknown, model.ErrorTimeout},
		{"malformed", "negative", "malformed netlink response", "reject with no fallback", nil, linuxio.ErrReply, nil, 0, model.SupportUnknown, model.ErrorMalformed},
		{"disappeared", "corner", "device removed during GET", "I/O failure with no fallback", nil, unix.ENODEV, nil, 0, model.SupportUnknown, model.ErrorIO},
		{"both unsupported", "corner", "modern netlink and ioctl unavailable", "unsupported without fabricated maximum", nil, unix.EOPNOTSUPP, unix.EOPNOTSUPP, 1, model.Unsupported, model.ErrorNone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			m := xtcpnl.EthtoolMessage{LinkModes: settingsModes(10000, 1, 1<<12, 1<<12)}
			n := &fakeSettingsRequests{discoverError: tc.discover, getError: tc.get, message: m}
			f := &fakeSettingsFallback{err: tc.fallback, message: m}
			c := &settingsCollector{client: n, ioctl: f}
			d := observed(newReducer(1), 1, true).Device
			job := model.Job{Key: model.JobKey{Namespace: 1, Device: d.Key, Collector: model.CollectorSettings}, Device: d}
			result := c.Collect(t.Context(), job)
			if result.Support != tc.wantSupport || result.Reason != tc.wantReason || f.reads != tc.wantReads {
				t.Fatalf("%s: support %v reason %v reads %d error %v", tc.expectedOutcome, result.Support, result.Reason, f.reads, result.Err)
			}
			if result.Support == model.Supported && (result.Settings == nil || result.Settings.Speed != model.CheckPass) {
				t.Fatal(tc.expectedOutcome)
			}
		})
	}
}

func TestSettingsIndependentSources(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		failed                                       model.CollectorKind
	}{
		{"driver", "corner", "driver ioctl fails but link modes succeed", "speed check remains usable", model.CollectorInventory},
		{"channels", "corner", "channels fail but rings succeed", "ring configuration independent", model.CollectorChannels},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			n := &fakeSettingsRequests{getError: unix.EPERM}
			f := &fakeSettingsFallback{err: unix.EPERM}
			c := &settingsCollector{client: n, ioctl: f}
			d := observed(newReducer(1), 1, true).Device
			job := model.Job{Key: model.JobKey{Namespace: 1, Device: d.Key, Collector: tc.failed}, Device: d}
			if result := c.Collect(t.Context(), job); !errors.Is(result.Err, unix.EPERM) {
				t.Fatal("missing independent failure")
			}
			n.getError = nil
			if tc.failed == model.CollectorInventory {
				job.Key.Collector = model.CollectorSettings
				n.message.LinkModes = settingsModes(10000, 1, 1<<12, 1<<12)
			} else {
				job.Key.Collector = model.CollectorRings
				n.message.Rings = &xtcpnl.EthtoolRings{}
			}
			if result := c.Collect(t.Context(), job); result.Support != model.Supported || result.Err != nil {
				t.Fatalf("%s: %+v", tc.expectedOutcome, result)
			}
		})
	}
}

func TestSettingsMessageValidation(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		kind                                         xtcpnl.EthtoolKind
		message                                      xtcpnl.EthtoolMessage
		wantError                                    bool
	}{
		{"missing", "negative", "GET supplies no settings payload", "reject as malformed", xtcpnl.EthtoolLinkModesKind, xtcpnl.EthtoolMessage{}, true},
		{"autoneg", "boundary", "autoneg boolean exceeds one", "reject invalid boolean", xtcpnl.EthtoolLinkModesKind, xtcpnl.EthtoolMessage{LinkModes: &xtcpnl.EthtoolLinkModes{Autoneg: pointer(uint8(2))}}, true},
		{"push", "negative", "ring push boolean exceeds one", "reject invalid boolean", xtcpnl.EthtoolRingsKind, xtcpnl.EthtoolMessage{Rings: &xtcpnl.EthtoolRings{TXPush: pointer(uint8(255))}}, true},
		{"split enum", "corner", "ring data split enum is not boolean", "preserve enum", xtcpnl.EthtoolRingsKind, xtcpnl.EthtoolMessage{Rings: &xtcpnl.EthtoolRings{TCPDataSplit: pointer(uint8(2))}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			if err := validateSettingsMessage(tc.kind, &tc.message); (err != nil) != tc.wantError {
				t.Fatalf("%s: %v", tc.expectedOutcome, err)
			}
		})
	}
}

func TestSettingsEligibilityRouting(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		kind                                         model.DeviceKind
		eligibility                                  model.Eligibility
		want                                         model.Support
	}{
		{"native RDMA", "corner", "native port reaches Ethernet settings adapter", "not applicable, no Ethernet I/O", model.DeviceNativeRDMA, model.Eligible, model.NotApplicable},
		{"excluded", "negative", "excluded Ethernet interface", "not applicable, no I/O", model.DeviceEthernet, model.Excluded, model.NotApplicable},
		{"unknown", "boundary", "unresolved Ethernet eligibility", "unknown, no I/O", model.DeviceEthernet, model.EligibilityUnknown, model.SupportUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			c := &settingsCollector{} // Any unexpected I/O would dereference a nil source.
			job := model.Job{Key: model.JobKey{Collector: model.CollectorSettings}, Device: model.Device{Key: model.DeviceKey{Kind: tc.kind}, Eligibility: tc.eligibility}}
			if result := c.Collect(t.Context(), job); result.Support != tc.want || result.Err != nil {
				t.Fatalf("%s: %+v", tc.expectedOutcome, result)
			}
		})
	}
}
