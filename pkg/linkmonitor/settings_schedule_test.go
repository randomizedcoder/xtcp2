package linkmonitor

import (
	"context"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

func settingsScheduler(t *testing.T) (*scheduler, model.JobKey) {
	t.Helper()
	s, _ := testScheduler(t)
	s.settings = &settingsSchedule{interval: time.Second, devices: make(map[model.DeviceKey]model.Device)}
	o := observed(s.reducer, 1, true)
	mustObserve(t, s.reducer, o)
	s.refreshDevice(o.Device.Key)
	return s, model.JobKey{Namespace: 1, Device: o.Device.Key, Collector: model.CollectorSettings}
}

func TestSettingsScheduling(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		change                                       func(*model.Device)
		jobs                                         int
	}{
		{"startup", "positive", "new eligible up Ethernet", "four collectors, bounded startup jobs", func(*model.Device) {}, 4},
		{"down", "corner", "down observation after startup", "configuration retained, negotiation canceled", func(d *model.Device) { d.Up = presentValue(false) }, 4},
		{"excluded", "negative", "metadata excludes previously eligible interface", "unregister collectors", func(d *model.Device) { d.Eligibility = model.Excluded }, 0},
		{"unknown", "boundary", "eligibility becomes unknown", "unregister and invalidate gauges", func(d *model.Device) { d.Eligibility = model.EligibilityUnknown }, 0},
		{"rename", "corner", "name changes with stable hardware", "same generation and refreshed jobs", func(d *model.Device) { d.Name = "renamed0" }, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			s, key := settingsScheduler(t)
			generation := s.reducer.slots[0].device.Token.Generation
			o := observed(s.reducer, 1, true)
			tc.change(&o.Device)
			mustObserve(t, s.reducer, o)
			s.refreshDevice(key.Device)
			if len(s.jobs) != tc.jobs || s.reducer.slots[0].device.Token.Generation != generation {
				t.Fatalf("%s: jobs %d", tc.expectedOutcome, len(s.jobs))
			}
			if !o.Device.Up.Value && s.jobs[key].retryActive {
				t.Fatal("down retained negotiation retries")
			}
		})
	}
}

func TestSettingsResultFencing(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		mutate                                       func(*scheduler, *model.Result)
		accepted                                     bool
	}{
		{"success", "positive", "matching successful settings", "samples and checks accepted atomically", func(*scheduler, *model.Result) {}, true},
		{"generation", "corner", "old generation result", "no checks or samples installed", func(_ *scheduler, r *model.Result) { r.Job.Token.Generation++ }, false},
		{"revision", "corner", "old revision result", "no checks or samples installed", func(_ *scheduler, r *model.Result) { r.Job.Token.Revision++ }, false},
		{"epoch", "corner", "old source epoch result", "no checks or samples installed", func(_ *scheduler, r *model.Result) { r.Job.Token.SourceEpoch++ }, false},
		{"attempt", "corner", "wrong attempt result", "no checks or samples installed", func(_ *scheduler, r *model.Result) { r.Job.Token.Attempt++ }, false},
		{"error", "negative", "failed settings with accidental payload", "cannot install healthy checks", func(_ *scheduler, r *model.Result) { r.Err = context.DeadlineExceeded }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			s, key := settingsScheduler(t)
			job, err := s.reducer.startCollection(key, s.clock.Now())
			if err != nil {
				t.Fatal(err)
			}
			samples, checks := settingsSamples(job.Device.Up, settingsModes(10000, 1, 1<<12, 1<<12))
			result := model.Result{Job: job, Finished: s.clock.Now(), Support: model.Supported, Samples: samples, Settings: checks}
			tc.mutate(s, &result)
			s.recordResult(result)
			slot := s.reducer.slots[0]
			if (slot.checks.maximumSpeed == model.CheckPass) != tc.accepted || (slot.collectors[model.CollectorSettings].block != nil) != tc.accepted {
				t.Fatal(tc.expectedOutcome)
			}
		})
	}
}

func TestSettingsDownInvalidation(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		inFlight                                     bool
	}{
		{"accepted", "positive", "down follows successful settings", "negotiated gauges invalidated immediately", false},
		{"in flight", "corner", "down overtakes settings collection", "late success cannot restore gauges", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			s, key := settingsScheduler(t)
			job, err := s.reducer.startCollection(key, s.clock.Now())
			if err != nil {
				t.Fatal(err)
			}
			samples, checks := settingsSamples(job.Device.Up, settingsModes(10000, 1, 1<<12, 1<<12))
			result := model.Result{Job: job, Finished: s.clock.Now(), Support: model.Supported, Samples: samples, Settings: checks}
			if !tc.inFlight {
				s.recordResult(result)
			}
			mustObserve(t, s.reducer, observed(s.reducer, 1, false))
			s.refreshDevice(key.Device)
			if tc.inFlight {
				s.recordResult(result)
			}
			slot := s.reducer.slots[0]
			if slot.collectors[model.CollectorSettings].block != nil || effectiveChecks(slot).maximumSpeed != model.CheckNotApplicable || s.jobs[key].retryActive {
				t.Fatal(tc.expectedOutcome)
			}
		})
	}
}

func TestSettingsRetryPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		support                                      model.Support
		speed, duplex                                model.Check
		want                                         bool
	}{
		{"unresolved", "positive", "up negotiation unresolved", "retry", model.Supported, model.CheckUnknown, model.CheckUnknown, true},
		{"below maximum", "corner", "up at provisional low speed", "retry within bounded sequence", model.Supported, model.CheckFail, model.CheckPass, true},
		{"settled", "positive", "full duplex at maximum", "cancel retries", model.Supported, model.CheckPass, model.CheckPass, false},
		{"unsupported", "negative", "settings unavailable", "no retries", model.Unsupported, model.CheckUnknown, model.CheckUnknown, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			r := model.Result{Job: model.Job{Device: model.Device{Up: presentValue(true)}}, Support: tc.support, Settings: &model.SettingsChecks{Speed: tc.speed, Duplex: tc.duplex}}
			if retryNegotiation(r) != tc.want {
				t.Fatal(tc.expectedOutcome)
			}
		})
	}
}
