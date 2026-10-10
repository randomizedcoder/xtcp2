package linkmonitor

import (
	"errors"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

func TestPrometheusResyncOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		failure                                      bool
		want                                         [2]uint64
	}{
		{"success", "positive", "one startup dump and coalesced requests", "one startup success", false, [2]uint64{1, 0}},
		{"failure", "negative", "one failing startup dump", "one startup error", true, [2]uint64{0, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			c, _ := testReconciler(t)
			advanceReconcile(t, c)
			work := takeInventory(t, c)
			for range 100 {
				c.requestResync()
			}
			result := inventoryCompletion{candidate: model.Candidate{Complete: true}}
			if tc.failure {
				result.err = errors.New("dump failed")
			}
			completeInventory(t, c, work, result)
			advanceReconcile(t, c)
			if got := c.scheduler.reducer.resyncs[resyncStartup]; got != tc.want {
				t.Fatalf("counts %v want %v", got, tc.want)
			}
			c.finishResync(!tc.failure)
			if got := c.scheduler.reducer.resyncs[resyncStartup]; got != tc.want {
				t.Fatal("duplicate completion counted")
			}
		})
	}
}

func TestPrometheusPendingResyncPriority(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		reasons                                      []resyncReason
		want                                         resyncReason
	}{
		{"startup", "positive", "periodic request joins startup", "startup retained", []resyncReason{resyncPeriodic}, resyncStartup},
		{"loss", "negative", "loss before startup dispatch", "loss takes priority", []resyncReason{resyncLoss, resyncPeriodic}, resyncLoss},
		{"rebaseline", "corner", "loss and rebaseline pending", "rebaseline takes priority", []resyncReason{resyncRebaseline, resyncLoss}, resyncRebaseline},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			c, _ := testReconciler(t)
			for _, reason := range tc.reasons {
				c.requestReason(reason)
			}
			advanceReconcile(t, c)
			if c.activeReason != tc.want {
				t.Fatalf("reason %v want %v", c.activeReason, tc.want)
			}
			c.abort() // shutdown cleanup does not fabricate an outcome.
			if c.scheduler.reducer.resyncs != ([4][2]uint64{}) {
				t.Fatal("shutdown counted failure")
			}
		})
	}
}

func TestPrometheusRDMAResyncBarrier(t *testing.T) {
	t.Log("corner: RDMA invalidates an in-flight startup dump; expected: one startup error and pending loss resync")
	c, _ := testReconciler(t)
	advanceReconcile(t, c)
	s := rdmaEventSchedule{c: c}
	s.resync()
	if c.resyncActive || !c.pending || c.pendingReason != resyncLoss || c.scheduler.reducer.resyncs[resyncStartup][1] != 1 {
		t.Fatal("RDMA barrier did not account for canceled attempt")
	}
	s.resync()
	if c.scheduler.reducer.resyncs[resyncStartup][1] != 1 {
		t.Fatal("duplicate barrier inflated attempts")
	}
}
