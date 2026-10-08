package linkmonitor

import (
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

func statisticScheduler(t *testing.T) (*scheduler, model.JobKey) {
	t.Helper()
	s, _ := testScheduler(t)
	s.statisticsInterval = time.Second
	o := observed(s.reducer, 1, true)
	mustObserve(t, s.reducer, o)
	s.refreshDevice(o.Device.Key)
	return s, model.JobKey{Namespace: 1, Device: o.Device.Key, Collector: model.CollectorDriver}
}

func TestStatisticScheduling(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		change                                       func(*model.Device)
		jobs                                         int
	}{
		{"startup", "positive", "eligible Ethernet", "two independent periodic jobs", func(*model.Device) {}, 2},
		{"down", "corner", "carrier goes down", "statistics continue", func(d *model.Device) { d.Up = presentValue(false) }, 2},
		{"unknown", "negative", "eligibility becomes unknown", "jobs and cache removed", func(d *model.Device) { d.Eligibility = model.EligibilityUnknown }, 0},
		{"excluded", "negative", "interface becomes excluded", "jobs and cache removed", func(d *model.Device) { d.Eligibility = model.Excluded }, 0},
		{"rename", "corner", "same device renamed", "schema refreshed without extra jobs", func(d *model.Device) { d.Name = "renamed0" }, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			s, key := statisticScheduler(t)
			state, _, err := s.reducer.collector(key)
			if err != nil {
				t.Fatal(err)
			}
			state.statisticSchema = &model.StatisticSchema{}
			revision := state.schemaRevision
			o := observed(s.reducer, 1, true)
			tc.change(&o.Device)
			mustObserve(t, s.reducer, o)
			s.refreshDevice(key.Device)
			if len(s.jobs) != tc.jobs || state.statisticSchema != nil || state.schemaRevision <= revision {
				t.Fatal(tc.expectedOutcome)
			}
			for _, job := range s.jobs {
				if job.policy.interval != time.Second {
					t.Fatal("not periodic")
				}
			}
		})
	}
}

func TestStatisticLateSchema(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		invalidate                                   func(*scheduler, model.JobKey)
	}{
		{"resync", "corner", "resync overlaps a worker", "late schema rejected and refresh pending", func(s *scheduler, _ model.JobKey) { s.resyncStatistics() }},
		{"configuration", "corner", "channels changed during a worker", "late schema rejected and refresh pending", func(s *scheduler, key model.JobKey) { s.refreshStatistics(key.Device) }},
		{"identity", "negative", "device rename before response", "late schema rejected and refresh pending", func(s *scheduler, key model.JobKey) {
			o := observed(s.reducer, 1, true)
			o.Device.Name = "new0"
			mustObserve(t, s.reducer, o)
			s.refreshDevice(key.Device)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			s, key := statisticScheduler(t)
			dispatchTest(t, s)
			var worker int
			for i, active := range s.active {
				if active != nil && active.job.Key == key {
					worker = i
				}
			}
			job := s.active[worker].job
			tc.invalidate(s, key)
			if !s.jobs[key].pending {
				t.Fatal("resync coalesced away")
			}
			s.complete(workerCompletion{worker: worker, result: model.Result{Job: job, Finished: s.clock.Now(), Support: model.Supported, StatisticSchema: &model.StatisticSchema{Names: []string{"old"}}}})
			state, _, err := s.reducer.collector(key)
			if err != nil || state.statisticSchema != nil || state.hasSuccess || s.jobs[key].ready == nil {
				t.Fatal(tc.expectedOutcome)
			}
		})
	}
}

func TestStatisticAcceptedCacheAndExpiry(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		failed                                       bool
	}{
		{"success", "positive", "worker returns immutable candidate", "next attempt reuses accepted cache", false},
		{"failure", "negative", "later attempt invalidates candidate", "cache dropped and prior values expire normally", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			s, key := statisticScheduler(t)
			s.reducer.freshness.poll = 3 * time.Second
			job, err := s.reducer.startCollection(key, s.clock.Now())
			if err != nil {
				t.Fatal(err)
			}
			schema := &model.StatisticSchema{Names: []string{"rx"}}
			s.recordResult(model.Result{Job: job, Finished: s.clock.Now(), Support: model.Supported, StatisticSchema: schema})
			next, err := s.reducer.startCollection(key, s.clock.Now())
			if err != nil || next.StatisticSchema != schema {
				t.Fatal("cache not shared")
			}
			state, _, err := s.reducer.collector(key)
			if err != nil {
				t.Fatal(err)
			}
			if tc.failed {
				s.recordResult(model.Result{Job: next, Finished: s.clock.Now(), Err: errStatisticShape, InvalidateSchema: true})
				if state.statisticSchema != nil || !state.fresh {
					t.Fatal(tc.expectedOutcome)
				}
			}
			s.expire(model.Stamp{Monotonic: s.clock.Now().Monotonic + 3*time.Second})
			if state.fresh {
				t.Fatal("stale statistics remained fresh")
			}
		})
	}
}

func TestStatisticCacheSourceEpoch(t *testing.T) {
	t.Log("corner: event recovery before the next poll; expected: new epoch cannot reuse accepted names")
	s, key := statisticScheduler(t)
	job, err := s.reducer.startCollection(key, s.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	s.recordResult(model.Result{Job: job, Finished: s.clock.Now(), Support: model.Supported, StatisticSchema: &model.StatisticSchema{Names: []string{"old"}}})
	if err := s.reducer.loseEvents(); err != nil {
		t.Fatal(err)
	}
	next, err := s.reducer.startCollection(key, s.clock.Now())
	if err != nil || next.StatisticSchema != nil || next.Token.SourceEpoch == job.Token.SourceEpoch {
		t.Fatalf("new epoch cache: %v", err)
	}
}

func TestStatisticResultTokens(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		change                                       func(*model.Token)
	}{
		{"generation", "negative", "old hardware generation", "schema and samples rejected", func(token *model.Token) { token.Generation++ }},
		{"revision", "negative", "old observation revision", "schema and samples rejected", func(token *model.Token) { token.Revision++ }},
		{"epoch", "negative", "old source epoch", "schema and samples rejected", func(token *model.Token) { token.SourceEpoch++ }},
		{"attempt", "corner", "wrong worker attempt", "schema and samples rejected", func(token *model.Token) { token.Attempt++ }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			s, key := statisticScheduler(t)
			job, err := s.reducer.startCollection(key, s.clock.Now())
			if err != nil {
				t.Fatal(err)
			}
			tc.change(&job.Token)
			s.recordResult(model.Result{Job: job, Finished: s.clock.Now(), Support: model.Supported, StatisticSchema: &model.StatisticSchema{Names: []string{"old"}}})
			state, _, err := s.reducer.collector(key)
			if err != nil || state.statisticSchema != nil || state.hasSuccess {
				t.Fatal(tc.expectedOutcome)
			}
		})
	}
}

func TestStatisticConfigurationRefresh(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		value                                        uint64
		refresh                                      bool
	}{
		{"stable", "positive", "unchanged channel configuration", "keep accepted schema", 4, false},
		{"changed", "corner", "channel configuration changes without link event", "invalidate accepted schema", 8, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			s, key := statisticScheduler(t)
			state, _, err := s.reducer.collector(key)
			if err != nil {
				t.Fatal(err)
			}
			configKey := key
			configKey.Collector = model.CollectorChannels
			for i, value := range []uint64{4, tc.value} {
				job, err := s.reducer.startCollection(configKey, s.clock.Now())
				if err != nil {
					t.Fatal(err)
				}
				s.recordResult(model.Result{Job: job, Finished: s.clock.Now(), Support: model.Supported, Samples: []model.Sample{interfaceGauge("channels", value)}})
				if i == 0 {
					state.statisticSchema = &model.StatisticSchema{}
				}
			}
			if (state.statisticSchema == nil) != tc.refresh {
				t.Fatal(tc.expectedOutcome)
			}
		})
	}
}
