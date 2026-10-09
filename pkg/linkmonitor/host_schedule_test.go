package linkmonitor

import (
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

func TestHostScheduling(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		resync, epoch                                bool
	}{
		{"periodic", "positive", "repeated requests during host read", "coalesced with active read", false, false},
		{"resync", "corner", "full resync during host read", "one follow-up and obsolete result rejected", true, false},
		{"epoch", "negative", "source loss during host read", "old epoch rejected", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			s, clock := testScheduler(t)
			key := registerTestJob(t, s, 0, model.CollectorNetstat, schedulePolicy{interval: time.Second})
			dispatchTest(t, s)
			job := s.active[0].job
			<-s.pool.inbox[0]
			if tc.epoch {
				if err := s.reducer.loseEvents(); err != nil {
					t.Fatal(err)
				}
			}
			for range 100 {
				if tc.resync {
					s.resyncHost()
				} else {
					s.request(key, urgencyPeriodic)
				}
				dispatchTest(t, s)
			}
			if len(s.jobs) != 1 || s.active[1] != nil || s.jobs[key].pending != (tc.resync || tc.epoch) {
				t.Fatal(tc.expectedOutcome)
			}
			s.complete(workerCompletion{worker: 0, result: model.Result{Job: job, Finished: clock.Now(), Support: model.Supported}})
			if s.reducer.host.hasSuccess == (tc.resync || tc.epoch) {
				t.Fatal(tc.expectedOutcome)
			}
			advanceSchedulerClock(t, clock, time.Second)
			s.expire(clock.Now())
			dispatchTest(t, s)
			if s.active[0] == nil || s.active[0].job.Token.Attempt == job.Token.Attempt {
				t.Fatal("follow-up missing")
			}
		})
	}
}

func TestHostInventoryIndependence(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		devices                                      int
	}{
		{"empty", "boundary", "no devices", "host job survives resync", 0},
		{"many", "positive", "several devices", "still one host job", 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			c, _ := testReconciler(t)
			s := c.scheduler
			key := registerTestJob(t, s, 0, model.CollectorNetstat, schedulePolicy{interval: time.Second})
			dispatchTest(t, s)
			job := s.active[0].job
			for i := range tc.devices {
				o := observed(s.reducer, uint32(i+1), true)
				mustObserve(t, s.reducer, o)
				s.refreshDevice(o.Device.Key)
				o.Device.Name = "renamed"
				o.Device.Eligibility = model.Excluded
				mustObserve(t, s.reducer, o)
				s.refreshDevice(o.Device.Key)
			}
			if s.jobs[key].pending || s.reducer.host.schemaRevision != job.SchemaRevision {
				t.Fatal("NIC changes affected host")
			}
			advanceReconcile(t, c)
			completeInventory(t, c, takeInventory(t, c), inventoryCompletion{candidate: model.Candidate{Complete: true}})
			if len(s.jobs) != 1 || !s.jobs[key].pending {
				t.Fatal(tc.expectedOutcome)
			}
		})
	}
}

func TestHostAtomicPublication(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		next                                         string
		failed                                       bool
	}{
		{"update", "positive", "next valid values", "schema shared old values immutable", "Tcp: A\nTcp: 2", false},
		{"failure", "negative", "next file malformed", "old values retained until expiry", "Tcp: A\nTcp: bad", true},
		{"empty", "corner", "next schema empty", "old values removed from new snapshot", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			s, clock := testScheduler(t)
			s.reducer.freshness.poll = 3 * time.Second
			files := map[string]string{"snmp": "Tcp: A\nTcp: 9007199254740993", "netstat": ""}
			c := hostTestCollector(t, files)
			collectHost := func() {
				job, err := s.reducer.startCollection(hostTestJob().Key, clock.Now())
				if err != nil {
					t.Fatal(err)
				}
				result := c.Collect(t.Context(), job)
				result.Finished = clock.Now()
				s.recordResult(result)
			}
			collectHost()
			m := new(Monitor)
			before := publishNow(t, s.reducer, m, clock, false)
			files["snmp"] = tc.next
			collectHost()
			after := publishNow(t, s.reducer, m, clock, false)
			if before.root.host.block.values[0] != model.Unsigned(1<<53+1) || len(s.reducer.host.history) != 0 {
				t.Fatal("old exact untyped value lost")
			}
			if tc.failed && after.root.host.block != before.root.host.block {
				t.Fatal(tc.expectedOutcome)
			}
			if tc.name == "update" && after.root.host.block.schema != before.root.host.block.schema {
				t.Fatal(tc.expectedOutcome)
			}
			if tc.name == "empty" && len(after.root.host.block.values) != 0 {
				t.Fatal(tc.expectedOutcome)
			}
			before.RangeSamples(func(sample SampleView) bool {
				sample.RangeLabels(func(string, string) bool { t.Error("host label present"); return true })
				return true
			})
			advanceSchedulerClock(t, clock, 3*time.Second-time.Nanosecond)
			s.expire(clock.Now())
			if !s.reducer.host.fresh {
				t.Fatal("expired early")
			}
			advanceSchedulerClock(t, clock, time.Nanosecond)
			s.expire(clock.Now())
			expired := publishNow(t, s.reducer, m, clock, false)
			if expired.HostCollector().Fresh() {
				t.Fatal("not expired")
			}
			expired.RangeSamples(func(SampleView) bool { t.Error("expired sample exposed"); return true })
			if _, ok := expired.HostCollector().LastSuccess(); !ok {
				t.Fatal("last success lost")
			}
		})
	}
}
