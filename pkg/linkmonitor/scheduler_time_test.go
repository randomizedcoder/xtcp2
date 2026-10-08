package linkmonitor

import (
	"math"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

func TestSchedulerPollClock(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected string
		kind                                  model.CollectorKind
		interval                              time.Duration
	}{
		{"device", "positive", "periodic driver collection", "immediate startup then stable device phase", model.CollectorDriver, 15 * time.Second},
		{"host", "positive", "host collection", "one namespace job every interval", model.CollectorNetstat, 15 * time.Second},
		{"explicit", "corner", "configuration collection with no interval", "startup only until explicit request", model.CollectorChannels, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			s, clock := testScheduler(t)
			key := registerTestJob(t, s, 1, tc.kind, schedulePolicy{interval: tc.interval})
			dispatchTest(t, s)
			finishTest(t, s, 0)
			pollKey := deadlineKey{kind: deadlinePoll, job: key}
			i, exists := s.reducer.deadlines.index[pollKey]
			if exists != (tc.interval != 0) {
				t.Fatal("wrong poll registration")
			}
			if !exists {
				return
			}
			first := s.reducer.deadlines.entries[i].at
			if first < tc.interval || first >= 2*tc.interval {
				t.Fatalf("first deadline=%v", first)
			}
			clock.ShiftWall(-24 * time.Hour)
			s.expire(clock.Now())
			if s.jobs[key].pending {
				t.Fatal("wall clock shift scheduled work")
			}
			advanceSchedulerClock(t, clock, first)
			s.expire(clock.Now())
			if !s.jobs[key].pending {
				t.Fatal("poll did not become ready")
			}
			dispatchTest(t, s)
			// Many elapsed ticks join the active attempt rather than queueing catch-up work.
			advanceSchedulerClock(t, clock, 1*time.Second)
			s.request(key, urgencyPeriodic)
			if s.jobs[key].pending {
				t.Fatal("compatible active poll duplicated")
			}
			finishTest(t, s, 0)
			advanceSchedulerClock(t, clock, 24*time.Hour)
			s.expire(clock.Now())
			next := s.reducer.deadlines.entries[s.reducer.deadlines.index[pollKey]].at
			if next <= clock.Now().Monotonic || next-clock.Now().Monotonic > tc.interval || next%tc.interval != first%tc.interval {
				t.Fatal("phase or skipped-tick arithmetic failed")
			}
			if s.queues[urgencyPeriodic].devices.Len() != 1 {
				t.Fatal("missed polls created backlog")
			}
		})
	}
}

func TestSchedulerPollBoundsAndIdentity(t *testing.T) {
	s, clock := testScheduler(t)
	advanceSchedulerClock(t, clock, math.MaxInt64-10)
	key := registerTestJob(t, s, 1, model.CollectorDriver, schedulePolicy{interval: time.Second})
	pollKey := deadlineKey{kind: deadlinePoll, job: key}
	if got := s.reducer.deadlines.entries[s.reducer.deadlines.index[pollKey]].at; got != math.MaxInt64 {
		t.Fatalf("overflow deadline=%v", got)
	}
	advanceSchedulerClock(t, clock, 10)
	s.expire(clock.Now())
	if _, exists := s.reducer.deadlines.index[pollKey]; exists {
		t.Fatal("saturated deadline would busy-loop")
	}
	phases := make(map[time.Duration]bool)
	for i := uint32(1); i <= 32; i++ {
		identity := model.DeviceKey{Namespace: 1, Kind: model.DeviceEthernet, Index: i}
		phase := pollPhase(identity, time.Second)
		if phase != pollPhase(identity, time.Second) || phase < 0 || phase >= time.Second {
			t.Fatal("unstable phase")
		}
		phases[phase] = true
	}
	if len(phases) != 32 {
		t.Fatal("test devices were not spread")
	}
	native := model.DeviceKey{Namespace: 1, Kind: model.DeviceNativeRDMA, RDMADevice: "mlx5_0", Port: 1}
	other := native
	other.Port++
	if pollPhase(native, time.Second) == pollPhase(other, time.Second) {
		t.Fatal("RDMA port was omitted from phase")
	}
}

func TestSchedulerSettingsRetries(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected string
		successAt                             int
		invalidate                            bool
	}{
		{"exhaust", "negative", "settings never negotiate", "three retries at 1, 2, 4 seconds then periodic", -1, false},
		{"success", "positive", "first retry negotiates", "remaining retries canceled", 1, false},
		{"revision", "corner", "new observation before retry", "old retry sequence canceled", -1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			s, clock := testScheduler(t)
			calls := 0
			key := registerTestJob(t, s, 1, model.CollectorSettings, schedulePolicy{retrySettings: func(model.Result) bool {
				retry := calls != tc.successAt
				calls++
				return retry
			}})
			s.settingsTransition(key)
			dispatchTest(t, s)
			finishTest(t, s, 0)
			retryKey := deadlineKey{kind: deadlineRetry, job: key}
			if tc.invalidate {
				mustObserve(t, s.reducer, observed(s.reducer, 1, false))
				s.refreshDevice(key.Device)
				if s.jobs[key].retryActive {
					t.Fatal("new observation retained retry sequence")
				}
			} else {
				for retry := range 3 {
					if tc.successAt >= 0 && calls > tc.successAt {
						break
					}
					entry := s.reducer.deadlines.entries[s.reducer.deadlines.index[retryKey]]
					delay := time.Second << retry
					if entry.at-clock.Now().Monotonic != delay {
						t.Fatalf("retry %d delay=%v", retry, entry.at-clock.Now().Monotonic)
					}
					advanceSchedulerClock(t, clock, delay)
					s.expire(clock.Now())
					dispatchTest(t, s)
					finishTest(t, s, 0)
				}
			}
			if _, exists := s.reducer.deadlines.index[retryKey]; exists || s.jobs[key].retryActive {
				t.Fatal("retry sequence did not stop")
			}
			wantCalls := 4
			if tc.successAt >= 0 {
				wantCalls = tc.successAt + 1
			}
			if tc.invalidate {
				wantCalls = 1
			}
			if calls != wantCalls {
				t.Fatalf("policy calls=%d, want %d", calls, wantCalls)
			}
		})
	}
}

func TestSchedulerIdentityInvalidation(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected string
		replace, epoch                        bool
	}{
		{"remove reuse", "corner", "remove device and reuse index during work", "old result cannot touch replacement", false, false},
		{"hardware", "corner", "verified hardware identity changes during work", "generation isolation without overlap", true, false},
		{"epoch", "negative", "event loss while work is running", "new source epoch required", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			s, _ := testScheduler(t)
			o := observed(s.reducer, 1, true)
			o.Device.HardwareID = "old"
			mustObserve(t, s.reducer, o)
			key := registerTestJob(t, s, 1, model.CollectorDriver, schedulePolicy{interval: time.Second})
			dispatchTest(t, s)
			old := s.active[0].job
			switch {
			case tc.epoch:
				if err := s.reducer.loseEvents(); err != nil {
					t.Fatal(err)
				}
			case tc.replace:
				o.Device.HardwareID = "new"
				mustObserve(t, s.reducer, o)
			default:
				if ok, err := s.reducer.remove(key.Device, o.Device.Token); !ok || err != nil {
					t.Fatal("remove failed")
				}
				s.refreshDevice(key.Device)
				if len(s.jobs) != 0 {
					t.Fatal("removed registration retained")
				}
				registerTestJob(t, s, 1, model.CollectorDriver, schedulePolicy{})
			}
			s.refreshDevice(key.Device)
			dispatchTest(t, s)
			if s.active[1] != nil {
				t.Fatal("old syscall overlapped replacement")
			}
			finishTest(t, s, 0)
			state, _, err := s.reducer.collector(key)
			if err != nil || state.hasSuccess {
				t.Fatal("obsolete result was published")
			}
			dispatchTest(t, s)
			if s.active[0] == nil || sameRevision(s.active[0].job.Token, old.Token) {
				t.Fatal("fresh replacement not dispatched")
			}
			finishTest(t, s, 0)
		})
	}
}

func TestSchedulerChurnAndPublicationDeadline(t *testing.T) {
	s, clock := testScheduler(t)
	for index := uint32(1); index <= 1024; index++ {
		registerTestJob(t, s, index, model.CollectorDriver, schedulePolicy{interval: time.Second})
	}
	for index := uint32(1); index <= 1024; index++ {
		key := observed(s.reducer, index, true).Device.Key
		if _, err := s.reducer.remove(key, model.Token{SourceEpoch: s.reducer.epoch}); err != nil {
			t.Fatal(err)
		}
		s.refreshDevice(key)
	}
	if len(s.jobs) != 0 || s.peak != 0 || len(s.reducer.deadlines.entries) != 0 {
		t.Fatal("scheduler retained historical work")
	}
	for i := range s.queues {
		if len(s.queues[i].index) != 0 || s.queues[i].peak != 0 || s.queues[i].devices.Len() != 0 {
			t.Fatal("ready queue retained devices")
		}
	}
	key := registerTestJob(t, s, 1, model.CollectorDriver, schedulePolicy{interval: time.Second})
	advanceSchedulerClock(t, clock, 2*time.Second)
	s.reducer.expire(clock.Now().Monotonic)
	if _, exists := s.reducer.deadlines.index[deadlineKey{kind: deadlinePoll, job: key}]; !exists {
		t.Fatal("publication consumed a scheduling deadline")
	}
	s.expire(clock.Now())
}

func TestSchedulerRetryAndPollCollision(t *testing.T) {
	s, clock := testScheduler(t)
	key := registerTestJob(t, s, 1, model.CollectorSettings, schedulePolicy{interval: time.Nanosecond, retrySettings: func(model.Result) bool { return true }})
	s.settingsTransition(key)
	dispatchTest(t, s)
	finishTest(t, s, 0)
	advanceSchedulerClock(t, clock, time.Second/2)
	s.expire(clock.Now())
	dispatchTest(t, s)
	if s.active[0] != nil || s.jobs[key].pending {
		t.Fatal("periodic tick bypassed settings backoff")
	}
	advanceSchedulerClock(t, clock, time.Second/2)
	s.expire(clock.Now())
	dispatchTest(t, s)
	if s.active[0] == nil {
		t.Fatal("scheduled retry was lost")
	}
	finishTest(t, s, 0)
}

func TestSchedulerObsoleteDeadlineAndMissingPoll(t *testing.T) {
	s, clock := testScheduler(t)
	key := registerTestJob(t, s, 1, model.CollectorDriver, schedulePolicy{interval: time.Second})
	dispatchTest(t, s)
	old := s.active[0].job
	finishTest(t, s, 0)
	s.request(key, urgencyEvent)
	dispatchTest(t, s)
	active := s.active[0]
	s.reducer.deadlines.set(deadlineEntry{key: deadlineKey{kind: deadlineAttempt, job: key}, at: 0, token: old.Token})
	s.expire(clock.Now())
	if active.timedOut || s.active[0] != active {
		t.Fatal("old deadline timed out new attempt")
	}
	finishTest(t, s, 0)
	if _, err := s.reducer.remove(key.Device, model.Token{SourceEpoch: 1}); err != nil {
		t.Fatal(err)
	}
	// Even if lifecycle integration has not explicitly refreshed this key yet,
	// its due poll must retire registration instead of resurrecting a deadline.
	advanceSchedulerClock(t, clock, 2*time.Second)
	s.expire(clock.Now())
	if len(s.jobs) != 0 || len(s.reducer.deadlines.entries) != 0 {
		t.Fatal("missing device poll left stale scheduling state")
	}
}
