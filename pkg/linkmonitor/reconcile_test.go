package linkmonitor

import (
	"context"
	"errors"
	"math"
	"syscall"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/testkit"
)

// The manual executors isolate owner transitions. Subscription lifecycle and
// real goroutine delivery are exercised separately through reconciler.run.
func testReconciler(t *testing.T) (*reconciler, *testkit.Clock) {
	t.Helper()
	s, clock := testScheduler(t)
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	inbox := newEventInbox(1)
	inventory := &inventoryExecutor{ctx: ctx, cancel: cancel, inbox: make(chan inventoryAssignment, 1), results: make(chan inventoryCompletion, 1)}
	events := &eventExecutor{ctx: ctx, cancel: cancel, inbox: inbox, requests: make(chan subscriptionRequest, 1), results: make(chan subscriptionStatus, 2)}
	c, err := newReconciler(s, inventory, events, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	// This fixture represents a factory which already joined the required groups.
	s.reducer.routeEvents = true
	return c, clock
}

func advanceReconcile(t *testing.T, c *reconciler) {
	t.Helper()
	if err := c.advance(c.scheduler.clock.Now()); err != nil {
		t.Fatal(err)
	}
}

func takeInventory(t *testing.T, c *reconciler) inventoryAssignment {
	t.Helper()
	select {
	case work := <-c.inventory.inbox:
		return work
	default:
		t.Fatal("missing inventory request")
		return inventoryAssignment{}
	}
}

func completeInventory(t *testing.T, c *reconciler, work inventoryAssignment, result inventoryCompletion) {
	t.Helper()
	c.inventory.completed()
	result.request = work.request
	if err := c.result(result); err != nil {
		t.Fatal(err)
	}
	advanceReconcile(t, c)
}

func deliverEvent(t *testing.T, c *reconciler, kind model.EventKind, observation model.Observation) {
	t.Helper()
	if !c.inbox.push(c.scheduler.reducer.epoch, model.Event{Kind: kind, Observation: observation}) {
		t.Fatal("event rejected")
	}
	if err := c.event(<-c.inbox.events); err != nil {
		t.Fatal(err)
	}
}

func TestReconcileCandidateValidation(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected string
		candidate                             func(*reducer) model.Candidate
		err                                   error
		success                               bool
		count                                 uint64
	}{
		{"empty complete", "boundary", "authoritative complete empty dump", "remove known link and record reconciliation", func(*reducer) model.Candidate { return model.Candidate{Complete: true} }, nil, true, 0},
		{"complete", "positive", "valid complete replacement inventory", "commit additions and removals together", func(r *reducer) model.Candidate {
			return model.Candidate{Complete: true, Devices: []model.Observation{observed(r, 2, true)}}
		}, nil, true, 1},
		{"partial", "negative", "empty incomplete dump", "keep known link, do not establish readiness", func(*reducer) model.Candidate { return model.Candidate{} }, nil, false, 1},
		{"error suffix", "negative", "data and complete bit followed by dump error", "discard entire candidate", func(r *reducer) model.Candidate {
			return model.Candidate{Complete: true, Devices: []model.Observation{observed(r, 2, true)}}
		}, syscall.EINTR, false, 1},
		{"duplicate", "negative", "duplicate device keys", "reject before mutation", func(r *reducer) model.Candidate {
			return model.Candidate{Complete: true, Devices: []model.Observation{observed(r, 2, true), observed(r, 2, false)}}
		}, nil, false, 1},
		{"invalid suffix", "negative", "valid new identity followed by invalid identity", "no partially applied addition", func(r *reducer) model.Candidate {
			return model.Candidate{Complete: true, Devices: []model.Observation{observed(r, 2, true), observed(r, 0, false)}}
		}, nil, false, 1},
		{"oversize", "boundary", "more than 65536 records", "reject before map allocation and mutation", func(*reducer) model.Candidate {
			return model.Candidate{Complete: true, Devices: make([]model.Observation, maxInventoryDevices+1)}
		}, nil, false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			c, _ := testReconciler(t)
			r := c.scheduler.reducer
			mustObserve(t, r, observed(r, 1, true))
			revision := r.revision
			advanceReconcile(t, c)
			work := takeInventory(t, c)
			if work.request.query {
				t.Fatal("startup did not request dump")
			}
			completeInventory(t, c, work, inventoryCompletion{candidate: tc.candidate(r), err: tc.err})
			if r.upCount != tc.count || r.lastResync.Present != tc.success || r.publicationHealth(true, true).Ready != tc.success {
				t.Fatalf("count/health=%d/%+v", r.upCount, r.publicationHealth(true, true))
			}
			if !tc.success && (r.revision != revision || len(r.slots) != 1 || c.lastError == nil) {
				t.Fatal("failed candidate mutated state or hid failure")
			}
			if tc.success && (c.candidate != nil || c.dirty != nil || c.lastError != nil) {
				t.Fatal("successful candidate retained transient state")
			}
		})
	}
}

func TestReconcileStartupChanges(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected string
		action                                model.EventKind
		queryError                            error
		want                                  uint64
	}{
		{"down during dump", "corner", "down event overtakes an up dump record", "query down and never resurrect stale up", model.EventChange, nil, 0},
		{"delete during dump", "corner", "delete event overtakes old dump record", "ENODEV confirms absence", model.EventRemove, syscall.ENODEV, 0},
		{"permission", "negative", "dirty identity query is denied", "no successful reconciliation or guessed deletion", model.EventRefresh, syscall.EACCES, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			c, _ := testReconciler(t)
			r := c.scheduler.reducer
			before := observed(r, 1, true)
			mustObserve(t, r, before)
			advanceReconcile(t, c)
			dump := takeInventory(t, c)
			after := observed(r, 1, false)
			deliverEvent(t, c, tc.action, after)
			completeInventory(t, c, dump, inventoryCompletion{candidate: model.Candidate{Complete: true, Devices: []model.Observation{before}}})
			query := takeInventory(t, c)
			if !query.request.query || query.request.key != before.Device.Key || r.lastResync.Present {
				t.Fatal("dirty identity not requeried before commit")
			}
			completeInventory(t, c, query, inventoryCompletion{observation: after, err: tc.queryError})
			if r.upCount != tc.want {
				t.Fatalf("count=%d", r.upCount)
			}
			wantSuccess := !errors.Is(tc.queryError, syscall.EACCES)
			if r.lastResync.Present != wantSuccess {
				t.Fatal("query failure classification incorrect")
			}
		})
	}
}

func TestReconcileStaleQuery(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected string
		missing                               bool
	}{
		{"old data", "corner", "delete/recreate during a query", "old data rejected by generation and version", false},
		{"old ENODEV", "negative", "old query reports absence after recreation", "new device is not deleted", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			c, _ := testReconciler(t)
			r := c.scheduler.reducer
			old := observed(r, 1, true)
			old.Device.HardwareID = "old"
			mustObserve(t, r, old)
			advanceReconcile(t, c)
			dump := takeInventory(t, c)
			deliverEvent(t, c, model.EventRefresh, old)
			completeInventory(t, c, dump, inventoryCompletion{candidate: model.Candidate{Complete: true, Devices: []model.Observation{old}}})
			query := takeInventory(t, c)
			deliverEvent(t, c, model.EventRemove, old)
			next := old
			next.Device.HardwareID = "new"
			next.Device.Name = "renamed"
			deliverEvent(t, c, model.EventChange, next)
			result := inventoryCompletion{observation: old}
			if tc.missing {
				result.err = syscall.ENODEV
			}
			completeInventory(t, c, query, result)
			if r.lastResync.Present || slotAt(r, 1).device.HardwareID != "new" {
				t.Fatal("stale query committed")
			}
			fresh := takeInventory(t, c)
			if fresh.request.token.Generation == query.request.token.Generation {
				t.Fatal("generation not refreshed")
			}
			completeInventory(t, c, fresh, inventoryCompletion{observation: next})
			if !r.lastResync.Present || r.upCount != 1 || slotAt(r, 1).device.Name != "renamed" {
				t.Fatal("replacement did not converge")
			}
			if err := c.result(inventoryCompletion{request: query.request, observation: old}); err != nil {
				t.Fatal(err)
			}
			advanceReconcile(t, c)
			if slotAt(r, 1).device.HardwareID != "new" {
				t.Fatal("late duplicate query changed committed device")
			}
		})
	}
}

func TestReconcileWatermark(t *testing.T) {
	c, _ := testReconciler(t)
	r := c.scheduler.reducer
	mustObserve(t, r, observed(r, 1, true))
	advanceReconcile(t, c)
	dump := takeInventory(t, c)
	for i := range 100 {
		if !c.inbox.push(1, model.Event{Kind: model.EventChange, Observation: observed(r, 1, i%2 != 0)}) {
			t.Fatal("enqueue")
		}
	}
	completeInventory(t, c, dump, inventoryCompletion{candidate: model.Candidate{Complete: true, Devices: []model.Observation{observed(r, 1, true)}}})
	if c.waiting == nil || r.lastResync.Present {
		t.Fatal("reply overtook queued events")
	}
	loop := &schedulerLoop{scheduler: c.scheduler, inventory: c.inventory}
	c.bind(loop)
	if err := loop.turn(schedulerWake{}); err != nil {
		t.Fatal(err)
	}
	if c.processed != 64 || c.request == nil || c.request.query || r.lastResync.Present {
		t.Fatal("first event budget did not retain candidate barrier")
	}
	if err := loop.turn(schedulerWake{}); err != nil {
		t.Fatal(err)
	}
	query := takeInventory(t, c)
	if c.processed != 100 || !query.request.query {
		t.Fatal("watermark did not release dirty query")
	}
	completeInventory(t, c, query, inventoryCompletion{observation: observed(r, 1, true)})
	slot := slotAt(r, 1)
	if !r.lastResync.Present || slot.upTransitions != 50 || slot.downTransitions != 50 {
		t.Fatal("watermark coalesced live transitions")
	}
}

func TestReconcilePersistentChurn(t *testing.T) {
	c, _ := testReconciler(t)
	r := c.scheduler.reducer
	initial := observed(r, 1, true)
	mustObserve(t, r, initial)
	advanceReconcile(t, c)
	dump := takeInventory(t, c)
	deliverEvent(t, c, model.EventRefresh, initial)
	completeInventory(t, c, dump, inventoryCompletion{candidate: model.Candidate{Complete: true, Devices: []model.Observation{initial}}})
	for i := range 20 {
		query := takeInventory(t, c)
		deliverEvent(t, c, model.EventChange, observed(r, 1, i%2 != 0))
		completeInventory(t, c, query, inventoryCompletion{observation: initial})
		if r.lastResync.Present || len(c.dirty) != 1 || c.queries.Len() != 0 || len(c.candidate) != 1 {
			t.Fatal("churn falsely converged or grew work")
		}
	}
	if slotAt(r, 1).downTransitions != 10 || slotAt(r, 1).upTransitions != 10 {
		t.Fatal("live event processing stalled")
	}
	query := takeInventory(t, c)
	completeInventory(t, c, query, inventoryCompletion{observation: observed(r, 1, true)})
	if !r.lastResync.Present {
		t.Fatal("stable inventory did not converge")
	}
}

func TestReconcilePeriodicAndCoalescing(t *testing.T) {
	c, clock := testReconciler(t)
	advanceReconcile(t, c)
	dump := takeInventory(t, c)
	for range 100 {
		c.requestResync()
		advanceReconcile(t, c)
	}
	completeInventory(t, c, dump, inventoryCompletion{candidate: model.Candidate{Complete: true}})
	advanceReconcile(t, c)
	if c.request != nil || c.pending {
		t.Fatal("compatible resync queued a duplicate")
	}
	advanceSchedulerClock(t, clock, 25*time.Second)
	advanceReconcile(t, c)
	next := takeInventory(t, c)
	if next.request.token.Attempt == dump.request.token.Attempt {
		t.Fatal("periodic request reused ownership")
	}
	completeInventory(t, c, next, inventoryCompletion{candidate: model.Candidate{Complete: true}})
	if c.nextResync != 35*time.Second {
		t.Fatal("periodic catch-up loop instead of one resync")
	}
}

func TestReconcileSequencePreflight(t *testing.T) {
	for _, sequence := range []string{"revision", "generation", "request"} {
		t.Run(sequence, func(t *testing.T) {
			c, _ := testReconciler(t)
			r := c.scheduler.reducer
			initial := observed(r, 1, true)
			mustObserve(t, r, initial)
			if sequence == "request" {
				c.serial = math.MaxUint64
				if err := c.advance(c.scheduler.clock.Now()); !errors.Is(err, errSequenceExhausted) {
					t.Fatal(err)
				}
				return
			}
			advanceReconcile(t, c)
			dump := takeInventory(t, c)
			c.inventory.completed()
			if err := c.result(inventoryCompletion{request: dump.request, candidate: model.Candidate{Complete: true, Devices: []model.Observation{observed(r, 2, true)}}}); err != nil {
				t.Fatal(err)
			}
			if sequence == "revision" {
				r.revision = math.MaxUint64
			} else {
				r.generation = math.MaxUint64
			}
			if err := c.advance(c.scheduler.clock.Now()); !errors.Is(err, errSequenceExhausted) {
				t.Fatal(err)
			}
			if len(r.slots) != 1 || slotAt(r, 1).device.Key != initial.Device.Key || r.lastResync.Present {
				t.Fatal("overflow partly committed inventory")
			}
		})
	}
}

func TestReconcileNativeRDMA(t *testing.T) {
	c, _ := testReconciler(t)
	r := c.scheduler.reducer
	native := model.Observation{Device: model.Device{Key: model.DeviceKey{Namespace: 1, Kind: model.DeviceNativeRDMA, RDMADevice: "mlx5_0", Port: 1},
		Name: "rdma:mlx5_0:1", Eligibility: model.Eligible, Up: presentValue(true)}}
	advanceReconcile(t, c)
	dump := takeInventory(t, c)
	deliverEvent(t, c, model.EventChange, native)
	completeInventory(t, c, dump, inventoryCompletion{candidate: model.Candidate{Complete: true}})
	query := takeInventory(t, c)
	if query.request.key != native.Device.Key {
		t.Fatal("native port was not queried")
	}
	completeInventory(t, c, query, inventoryCompletion{observation: native})
	if !r.lastResync.Present || r.upCount != 1 || r.requiredRDMA != 1 || r.publicationHealth(true, true).Ready {
		t.Fatal("native identity lost or missing RDMA evidence incorrectly healthy")
	}
}

func TestReconcileSettingsTransition(t *testing.T) {
	c, _ := testReconciler(t)
	r := c.scheduler.reducer
	mustObserve(t, r, observed(r, 1, false))
	key := registerTestJob(t, c.scheduler, 1, model.CollectorSettings, schedulePolicy{retrySettings: func(model.Result) bool { return true }})
	deliverEvent(t, c, model.EventChange, observed(r, 1, true))
	if !c.scheduler.jobs[key].retryActive || c.scheduler.jobs[key].urgency != urgencyEvent {
		t.Fatal("up transition did not request settings retries")
	}
	deliverEvent(t, c, model.EventChange, observed(r, 1, false))
	if c.scheduler.jobs[key].retryActive {
		t.Fatal("down transition kept obsolete settings retries")
	}
}
