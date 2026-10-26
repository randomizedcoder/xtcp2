// Package storetest is the conformance suite every store.Store implementation
// must pass. The memory and postgres packages both run it, so a handler
// tested against the memory store sees the same contract production sees.
package storetest

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/store"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/internal/fixtures"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/wire"
	"google.golang.org/protobuf/proto"
)

// Fixture values shared by many rows.
const (
	locationCst      = "dc1"
	host1Cst         = "host-1"
	host2Cst         = "host-2"
	destinationCst   = "192.0.2.1/32"
	agentScopeCst    = "agent:h"
	missingCst       = "missing"
	fleetScopeCst    = "fleet"
	locationScopeCst = "location:" + locationCst
)

// Opener returns a fresh, empty store for one test.
type Opener func(t *testing.T) store.Store

// now is microsecond-aligned and in UTC, the precision timestamptz keeps.
var now = time.Unix(1700000000, 0).UTC()

func ptr[T any](v T) *T { return &v }

// Run executes the suite against stores from open.
func Run(t *testing.T, open Opener) {
	t.Run("Agents", func(t *testing.T) { agents(t, open) })
	t.Run("Experiments", func(t *testing.T) { experiments(t, open) })
	t.Run("Eligibility", func(t *testing.T) { eligibility(t, open) })
	t.Run("Evidence", func(t *testing.T) { evidence(t, open) })
	t.Run("Holds", func(t *testing.T) { holds(t, open) })
	t.Run("Audit", func(t *testing.T) { audit(t, open) })
	t.Run("Tx", func(t *testing.T) { tx(t, open) })
	t.Run("Lease", func(t *testing.T) { lease(t, open) })
	t.Run("Metrics", func(t *testing.T) { metrics(t, open) })
}

func report(id, location string) *treatment.AgentReport {
	d := fixtures.Desired(now)
	return &treatment.AgentReport{
		AgentID:          id,
		Location:         location,
		Capabilities:     treatment.Capabilities{BootID: "boot-1", CollectionID: fixtures.CollectionID, KernelRelease: "6.12.0", CongestionAlgorithms: []string{"cubic", "bbr", "dctcp"}, SimpleHostRouting: true, ActuatorEnabled: true},
		DesiredRevision:  3,
		LocallySuspended: true,
		Records: []treatment.ApplicationRecord{{
			Assignment: d.Assignments[0], DesiredRevision: 3, Status: treatment.ApplyVerified,
			ReceivedAt: now, AttemptedAt: now.Add(time.Second), AppliedAt: now.Add(2 * time.Second), VerifiedAt: now.Add(3 * time.Second),
		}, {
			Assignment:      treatment.Assignment{ExperimentID: "old", Generation: 2, Mode: treatment.ModeRetired, Treatment: fixtures.Treatment()},
			DesiredRevision: 2, Status: treatment.ApplyFailed, Error: "ENETUNREACH", ExpiredLatched: true, RemovedAt: now,
		}},
	}
}

func sameDesired(t *testing.T, description string, got, want *treatment.DesiredState) {
	t.Helper()
	if !proto.Equal(wire.Desired(got), wire.Desired(want)) {
		t.Fatalf("%s:\n got %+v\nwant %+v", description, got, want)
	}
}

func sameReport(t *testing.T, description string, got, want *treatment.AgentReport) {
	t.Helper()
	if !proto.Equal(wire.Report(got), wire.Report(want)) {
		t.Fatalf("%s:\n got %+v\nwant %+v", description, got, want)
	}
}

func sameExperiment(t *testing.T, description string, got, want *treatment.Experiment) {
	t.Helper()
	if !proto.Equal(wire.Experiment(got), wire.Experiment(want)) {
		t.Fatalf("%s:\n got %+v\nwant %+v", description, got, want)
	}
}

func sameEvidence(t *testing.T, description string, got, want *treatment.Evidence) {
	t.Helper()
	if !proto.Equal(wire.Evidence(got), wire.Evidence(want)) {
		t.Fatalf("%s:\n got %+v\nwant %+v", description, got, want)
	}
}

// agentCase is one Agents contract row.
type agentCase struct {
	name, description string
	run               func(t *testing.T, a store.Agents)
}

func agents(t *testing.T, open Opener) {
	ctx := context.Background()
	initial := &treatment.DesiredState{AgentID: host1Cst, Revision: 1, Suspended: true, IssuedAt: now}
	for _, tc := range slices.Concat(agentEnrollCases(ctx, initial), agentDesiredSinceCases(ctx, initial), agentLookupCases(ctx, initial), agentSessionCases(ctx, initial)) {
		t.Run(tc.name, func(t *testing.T) {
			t.Log(tc.description)
			tc.run(t, open(t).Repos().Agents)
		})
	}
}

// observeSession checks host-1 in with the given boot and nonce at seen.
func observeSession(ctx context.Context, t *testing.T, a store.Agents, boot, collection string, seen time.Time) {
	t.Helper()
	r := report(host1Cst, locationCst)
	r.Capabilities.BootID, r.Capabilities.CollectionID = boot, collection
	must(t, a.Observe(ctx, r, seen))
}

// sessionKeys renders sessions as boot/nonce@first..last for comparison.
func sessionKeys(sessions []treatment.Session) []string {
	out := make([]string, len(sessions))
	for i, s := range sessions {
		out[i] = fmt.Sprintf("%s/%s/%s@%d..%d", s.AgentID, s.BootID, s.CollectionID[len(s.CollectionID)-1:], s.FirstSeen.Unix()-now.Unix(), s.LastSeen.Unix()-now.Unix())
	}
	return out
}

// agentSessionCases covers the (boot, collection) history evidence binds to.
func agentSessionCases(ctx context.Context, initial *treatment.DesiredState) []agentCase {
	const nonce1, nonce2 = "00000000000000000000000000000001", "00000000000000000000000000000002"
	sessions := func(t *testing.T, a store.Agents, from, to time.Time, expected ...string) {
		t.Helper()
		got, err := a.Sessions(ctx, []string{host1Cst, "ghost"}, from, to)
		must(t, err)
		if keys := sessionKeys(got); !slices.Equal(keys, expected) {
			t.Fatalf("expected sessions %v, got %v", expected, keys)
		}
	}
	return []agentCase{
		{"session-recorded", "positive: a check-in records its session and the agent's current nonce", func(t *testing.T, a store.Agents) {
			must(t, a.Enroll(ctx, host1Cst, locationCst, initial))
			observeSession(ctx, t, a, "boot-1", nonce1, now)
			sessions(t, a, now.Add(-time.Hour), now.Add(time.Hour), "host-1/boot-1/1@0..0")
			got, err := a.Get(ctx, host1Cst)
			must(t, err)
			if got.Report.Capabilities.CollectionID != nonce1 {
				t.Fatalf("expected the reported nonce on the agent, got %q", got.Report.Capabilities.CollectionID)
			}
		}},
		{"rotation", "positive: a restarted agent adds a session and both overlap a window spanning the restart", func(t *testing.T, a store.Agents) {
			must(t, a.Enroll(ctx, host1Cst, locationCst, initial))
			observeSession(ctx, t, a, "boot-1", nonce1, now)
			observeSession(ctx, t, a, "boot-1", nonce2, now.Add(10*time.Minute))
			sessions(t, a, now.Add(-time.Minute), now.Add(time.Hour), "host-1/boot-1/1@0..0", "host-1/boot-1/2@600..600")
		}},
		{"outside-window", "negative: sessions last seen before the window or first seen after it are not returned", func(t *testing.T, a store.Agents) {
			must(t, a.Enroll(ctx, host1Cst, locationCst, initial))
			observeSession(ctx, t, a, "boot-1", nonce1, now)
			observeSession(ctx, t, a, "boot-2", nonce2, now.Add(2*time.Hour))
			sessions(t, a, now.Add(time.Minute), now.Add(time.Hour))
			sessions(t, a, now, now, "host-1/boot-1/1@0..0")
		}},
		{"heartbeat-throttle", "boundary: last_seen moves only once it is SessionRefresh old", func(t *testing.T, a store.Agents) {
			must(t, a.Enroll(ctx, host1Cst, locationCst, initial))
			observeSession(ctx, t, a, "boot-1", nonce1, now)
			observeSession(ctx, t, a, "boot-1", nonce1, now.Add(store.SessionRefresh-time.Second))
			sessions(t, a, now.Add(-time.Hour), now.Add(time.Hour), "host-1/boot-1/1@0..0")
			observeSession(ctx, t, a, "boot-1", nonce1, now.Add(store.SessionRefresh))
			sessions(t, a, now.Add(-time.Hour), now.Add(time.Hour), "host-1/boot-1/1@0..300")
		}},
		{"retention", "corner: pruning deletes only sessions last seen before the cutoff", func(t *testing.T, a store.Agents) {
			must(t, a.Enroll(ctx, host1Cst, locationCst, initial))
			observeSession(ctx, t, a, "boot-1", nonce1, now)
			observeSession(ctx, t, a, "boot-1", nonce2, now.Add(time.Hour))
			n, err := a.PruneSessions(ctx, now.Add(time.Minute))
			must(t, err)
			if n != 1 {
				t.Fatalf("expected one pruned session, got %d", n)
			}
			sessions(t, a, now.Add(-time.Hour), now.Add(2*time.Hour), "host-1/boot-1/2@3600..3600")
		}},
		{"sessions-unenrolled", "negative: an unknown agent has no sessions and a report for it records none", func(t *testing.T, a store.Agents) {
			r := report(host1Cst, locationCst)
			if err := a.Observe(ctx, r, now); !errors.Is(err, store.ErrNotEnrolled) {
				t.Fatalf("expected ErrNotEnrolled, got %v", err)
			}
			got, err := a.Sessions(ctx, []string{host1Cst}, now.Add(-time.Hour), now.Add(time.Hour))
			must(t, err)
			if len(got) != 0 {
				t.Fatalf("expected no sessions, got %d", len(got))
			}
		}},
	}
}

// agentEnrollCases covers enrollment, reports and the desired-state round trip.
func agentEnrollCases(ctx context.Context, initial *treatment.DesiredState) []agentCase {
	return []agentCase{
		{"enroll-roundtrip", "positive: enrollment stores location and the initial desired state", func(t *testing.T, a store.Agents) {
			must(t, a.Enroll(ctx, host1Cst, locationCst, initial))
			got, err := a.Get(ctx, host1Cst)
			must(t, err)
			if got.ID != host1Cst || got.Location != locationCst || got.EnrolledAt.IsZero() || !got.LastSeen.IsZero() || got.Report.AgentID != host1Cst || got.Report.Location != locationCst {
				t.Fatalf("expected an enrolled agent without a report, got %+v", got)
			}
			d, err := a.Desired(ctx, host1Cst)
			must(t, err)
			sameDesired(t, "expected the initial desired state", d, initial)
		}},
		{"observe-roundtrip", "positive: a report is stored with every capability and record column", func(t *testing.T, a store.Agents) {
			must(t, a.Enroll(ctx, host1Cst, locationCst, initial))
			r := report(host1Cst, locationCst)
			must(t, a.Observe(ctx, r, now.Add(time.Minute)))
			got, err := a.Get(ctx, host1Cst)
			must(t, err)
			if !got.LastSeen.Equal(now.Add(time.Minute)) || !got.Seen(now.Add(2*time.Minute), 2*time.Minute) || got.Seen(now.Add(4*time.Minute), 2*time.Minute) {
				t.Fatalf("expected last_seen %s, got %s", now.Add(time.Minute), got.LastSeen)
			}
			sameReport(t, "expected the observed report", &got.Report, r)
		}},
		{"desired-roundtrip", "positive: every assignment column, predecessor and lease survives", func(t *testing.T, a store.Agents) {
			must(t, a.Enroll(ctx, host1Cst, locationCst, initial))
			d := fixtures.Desired(now)
			d.Revision = 7
			pred := fixtures.Treatment()
			pred.Revision = 2
			pred.Settings = treatment.TCPSettings{CongestionControl: ptr("bbr"), CongestionLock: true, RTOMinMs: ptr(uint32(5)), InitialCongestionWindow: ptr(uint32(100)), InitialReceiveWindow: ptr(uint32(0)), QuickAck: ptr(false), ECN: ptr(true)}
			d.Assignments = append(d.Assignments, treatment.Assignment{ExperimentID: "stable-1", Generation: 4, Mode: treatment.ModeStable, Treatment: treatment.Treatment{ID: "v6", Revision: 3, Destination: netip.MustParsePrefix("2001:db8::1/128"), Settings: pred.Settings, ECNFabricVerified: true}, Predecessor: &pred})
			must(t, a.SaveDesired(ctx, d))
			got, err := a.Desired(ctx, host1Cst)
			must(t, err)
			sameDesired(t, "expected the saved desired state", got, d)
			revs, err := a.DesiredSince(ctx, time.Time{})
			must(t, err)
			if len(revs) != 1 || revs[0].AgentID != host1Cst || revs[0].Revision != 7 || revs[0].UpdatedAt.IsZero() {
				t.Fatalf("expected a stamped revision 7 for host-1, got %+v", revs)
			}
		}},
	}
}

// agentDesiredSinceCases covers the notification watermark query.
func agentDesiredSinceCases(ctx context.Context, initial *treatment.DesiredState) []agentCase {
	return []agentCase{
		{"desired-since-all", "positive: a zero watermark returns every agent, enrolled or saved, ordered by stamp then ID", func(t *testing.T, a store.Agents) {
			must(t, a.Enroll(ctx, host2Cst, locationCst, &treatment.DesiredState{AgentID: host2Cst, Revision: 1, IssuedAt: now}))
			settle()
			must(t, a.Enroll(ctx, host1Cst, locationCst, initial))
			settle()
			must(t, a.SaveDesired(ctx, &treatment.DesiredState{AgentID: host2Cst, Revision: 2, IssuedAt: now}))
			got, err := a.DesiredSince(ctx, time.Time{})
			must(t, err)
			if len(got) != 2 || got[0].AgentID != host1Cst || got[0].Revision != 1 || got[1].AgentID != host2Cst || got[1].Revision != 2 || !got[0].UpdatedAt.Before(got[1].UpdatedAt) {
				t.Fatalf("expected host-1 rev 1 then host-2 rev 2 in stamp order, got %+v", got)
			}
		}},
		{"desired-since-changed", "positive: a watermark after the older stamp returns only the agent saved since", func(t *testing.T, a store.Agents) {
			must(t, a.Enroll(ctx, host1Cst, locationCst, initial))
			must(t, a.Enroll(ctx, host2Cst, locationCst, initial))
			all, err := a.DesiredSince(ctx, time.Time{})
			must(t, err)
			newest := all[len(all)-1].UpdatedAt
			settle()
			must(t, a.SaveDesired(ctx, &treatment.DesiredState{AgentID: host1Cst, Revision: 5, IssuedAt: now}))
			got, err := a.DesiredSince(ctx, newest.Add(time.Microsecond))
			must(t, err)
			if len(got) != 1 || got[0].AgentID != host1Cst || got[0].Revision != 5 || !got[0].UpdatedAt.After(newest) {
				t.Fatalf("expected only host-1 at revision 5, got %+v", got)
			}
		}},
		{"desired-since-equal", "boundary: a row stamped exactly at the watermark is included", func(t *testing.T, a store.Agents) {
			must(t, a.Enroll(ctx, host1Cst, locationCst, initial))
			settle()
			must(t, a.Enroll(ctx, host2Cst, locationCst, initial))
			all, err := a.DesiredSince(ctx, time.Time{})
			must(t, err)
			got, err := a.DesiredSince(ctx, all[1].UpdatedAt)
			must(t, err)
			if len(got) != 1 || got[0].AgentID != all[1].AgentID || !got[0].UpdatedAt.Equal(all[1].UpdatedAt) {
				t.Fatalf("expected exactly the row stamped at the watermark, got %+v", got)
			}
		}},
		{"desired-since-none", "boundary: a watermark past the newest stamp returns nothing without error", func(t *testing.T, a store.Agents) {
			must(t, a.Enroll(ctx, host1Cst, locationCst, initial))
			all, err := a.DesiredSince(ctx, time.Time{})
			must(t, err)
			got, err := a.DesiredSince(ctx, all[0].UpdatedAt.Add(time.Microsecond))
			must(t, err)
			if len(got) != 0 {
				t.Fatalf("expected no rows, got %+v", got)
			}
		}},
		{"desired-since-stamp-is-store-clock", "corner: the stamp is the store's clock, not the desired state's issued_at", func(t *testing.T, a store.Agents) {
			must(t, a.Enroll(ctx, host1Cst, locationCst, initial))
			must(t, a.SaveDesired(ctx, &treatment.DesiredState{AgentID: host1Cst, Revision: 2, IssuedAt: now.Add(-24 * time.Hour)}))
			got, err := a.DesiredSince(ctx, now)
			must(t, err)
			if len(got) != 1 || got[0].UpdatedAt.Before(now) {
				t.Fatalf("expected a current stamp despite an old issued_at, got %+v", got)
			}
		}},
	}
}

// agentLookupCases covers idempotence, identity fences, bounds and listing.
func agentLookupCases(ctx context.Context, initial *treatment.DesiredState) []agentCase {
	return []agentCase{
		{"enroll-idempotent", "corner: a second enrollment never changes location or desired state", func(t *testing.T, a store.Agents) {
			must(t, a.Enroll(ctx, host1Cst, locationCst, initial))
			must(t, a.Enroll(ctx, host1Cst, "dc2", &treatment.DesiredState{AgentID: host1Cst, Revision: 9, IssuedAt: now}))
			got, err := a.Get(ctx, host1Cst)
			must(t, err)
			d, err := a.Desired(ctx, host1Cst)
			must(t, err)
			if got.Location != locationCst || d.Revision != 1 || !d.Suspended {
				t.Fatalf("expected the first enrollment to win, got %s revision %d", got.Location, d.Revision)
			}
		}},
		{"observe-location-change", "negative: a host cannot move location by reporting a new one", func(t *testing.T, a store.Agents) {
			must(t, a.Enroll(ctx, host1Cst, locationCst, initial))
			if err := a.Observe(ctx, report(host1Cst, "dc2"), now); !errors.Is(err, store.ErrNotEnrolled) {
				t.Fatalf("expected ErrNotEnrolled, got %v", err)
			}
			got, err := a.Get(ctx, host1Cst)
			must(t, err)
			if !got.LastSeen.IsZero() || len(got.Report.Records) != 0 {
				t.Fatal("expected the rejected report to leave no trace")
			}
		}},
		{"unknown-agent", "negative: lookups of an unenrolled agent return the documented sentinels", func(t *testing.T, a store.Agents) {
			if _, err := a.Get(ctx, "ghost"); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("expected ErrNotFound from Get, got %v", err)
			}
			if _, err := a.Desired(ctx, "ghost"); !errors.Is(err, store.ErrNotEnrolled) {
				t.Fatalf("expected ErrNotEnrolled from Desired, got %v", err)
			}
			if err := a.SaveDesired(ctx, &treatment.DesiredState{AgentID: "ghost", Revision: 1, IssuedAt: now}); !errors.Is(err, store.ErrNotEnrolled) {
				t.Fatalf("expected ErrNotEnrolled from SaveDesired, got %v", err)
			}
			if err := a.Observe(ctx, report("ghost", locationCst), now); !errors.Is(err, store.ErrNotEnrolled) {
				t.Fatalf("expected ErrNotEnrolled from Observe, got %v", err)
			}
		}},
		{"empty-desired", "boundary: a desired state with no assignments replaces a populated one", func(t *testing.T, a store.Agents) {
			must(t, a.Enroll(ctx, host1Cst, locationCst, fixtures.Desired(now)))
			empty := &treatment.DesiredState{AgentID: host1Cst, Revision: 2, IssuedAt: now.Add(time.Minute)}
			must(t, a.SaveDesired(ctx, empty))
			got, err := a.Desired(ctx, host1Cst)
			must(t, err)
			sameDesired(t, "expected no assignments", got, empty)
		}},
		{"max-assignments", "boundary: MaxAssignments assignments are stored in order", func(t *testing.T, a store.Agents) {
			must(t, a.Enroll(ctx, host1Cst, locationCst, initial))
			d := &treatment.DesiredState{AgentID: host1Cst, Revision: 2, IssuedAt: now}
			for i := range treatment.MaxAssignments {
				d.Assignments = append(d.Assignments, treatment.Assignment{ExperimentID: fmt.Sprintf("exp-%05d", i), Generation: uint64(i), Mode: treatment.ModeTrial, LeaseExpiresAt: now.Add(time.Hour), Treatment: fixtures.Treatment()})
			}
			must(t, a.SaveDesired(ctx, d))
			got, err := a.Desired(ctx, host1Cst)
			must(t, err)
			if len(got.Assignments) != treatment.MaxAssignments {
				t.Fatalf("expected %d assignments, got %d", treatment.MaxAssignments, len(got.Assignments))
			}
			for i := range got.Assignments {
				if got.Assignments[i].ExperimentID != d.Assignments[i].ExperimentID {
					t.Fatalf("expected position %d to hold %s, got %s", i, d.Assignments[i].ExperimentID, got.Assignments[i].ExperimentID)
				}
			}
		}},
		{"list-by-location", "positive: List filters by location and orders by ID; empty location lists everyone", func(t *testing.T, a store.Agents) {
			for _, h := range []struct{ id, loc string }{{"c", "dc2"}, {"a", locationCst}, {"b", locationCst}} {
				must(t, a.Enroll(ctx, h.id, h.loc, &treatment.DesiredState{AgentID: h.id, Revision: 1, IssuedAt: now}))
			}
			dc1, err := a.List(ctx, locationCst)
			must(t, err)
			all, err := a.List(ctx, "")
			must(t, err)
			none, err := a.List(ctx, "dc3")
			must(t, err)
			want := []store.AgentRef{{ID: "a", Location: locationCst}, {ID: "b", Location: locationCst}}
			if !slices.Equal(dc1, want) || len(all) != 3 || all[2].ID != "c" || len(none) != 0 {
				t.Fatalf("expected %v / 3 / 0, got %v / %d / %d", want, dc1, len(all), len(none))
			}
		}},
	}
}

func populated() *treatment.Experiment {
	e, after := fixtures.Experiment(now, treatment.DefaultPolicy())
	e.ProposedAt = now.Add(-2 * time.Hour)
	e.UpdatedAt = now
	e.LatestEvidence = after
	e.LatestOutcome = treatment.OutcomeBeneficial
	e.Phases[0].EvaluatedAt = now
	e.Phases[0].Evidence = after
	e.Phases[0].Outcome = treatment.OutcomeBeneficial
	e.Phases[0].Reviewer = "operator/alice"
	e.Phases[0].Reason = "reviewed"
	e.Phases = append(e.Phases, treatment.Phase{TargetHosts: 8, IssuedAt: now})
	return e
}

// repoCase is one Experiments contract row.
type repoCase struct {
	name, description string
	run               func(t *testing.T, rp store.Repos)
}

func experiments(t *testing.T, open Opener) {
	ctx := context.Background()
	for _, tc := range slices.Concat(experimentRoundTripCases(ctx), experimentQueryCases(ctx), experimentListCases(ctx)) {
		t.Run(tc.name, func(t *testing.T) {
			t.Log(tc.description)
			tc.run(t, open(t).Repos())
		})
	}
}

// experimentRoundTripCases covers what Save writes and Get reads back.
func experimentRoundTripCases(ctx context.Context) []repoCase {
	return []repoCase{
		{"insert-relational", "positive: cohort, phases, baseline and evidence references reconstruct the experiment exactly", func(t *testing.T, rp store.Repos) {
			e := populated()
			must(t, rp.Experiments.Save(ctx, e))
			got, err := rp.Experiments.Get(ctx, e.ID)
			must(t, err)
			sameExperiment(t, "expected the saved experiment", got, e)
			exists, err := rp.Experiments.Exists(ctx, e.ID)
			must(t, err)
			if !exists {
				t.Fatal("expected Exists to be true")
			}
			baseline, err := rp.Evidence.Get(ctx, "baseline")
			must(t, err)
			sameEvidence(t, "expected the baseline stored once in the evidence repository", baseline, e.Baseline)
		}},
		{"update-replaces", "positive: saving again replaces status, revision, phases and evidence references", func(t *testing.T, rp store.Repos) {
			e := populated()
			must(t, rp.Experiments.Save(ctx, e))
			n := e.Clone()
			n.Status = treatment.StatusComplete
			n.Revision = 3
			n.UpdatedAt = now.Add(time.Hour)
			n.Phases = n.Phases[:1]
			n.Phases[0].Outcome = treatment.OutcomeStableObserved
			must(t, rp.Experiments.Save(ctx, n))
			got, err := rp.Experiments.Get(ctx, e.ID)
			must(t, err)
			sameExperiment(t, "expected the replacement with the trailing phase removed", got, n)
		}},
		{"cohort-immutable", "corner: the cohort order written first is kept even if a later save differs", func(t *testing.T, rp store.Repos) {
			e := populated()
			must(t, rp.Experiments.Save(ctx, e))
			n := e.Clone()
			n.CohortOrder = n.CohortOrder[:2]
			must(t, rp.Experiments.Save(ctx, n))
			got, err := rp.Experiments.Get(ctx, e.ID)
			must(t, err)
			if len(got.CohortOrder) != 16 {
				t.Fatalf("expected the 16-host cohort to survive, got %d", len(got.CohortOrder))
			}
		}},
		{"no-evidence", "boundary: an experiment without baseline, latest evidence or phases round-trips", func(t *testing.T, rp store.Repos) {
			e := &treatment.Experiment{ID: "bare", Location: locationCst, Hypothesis: "h", Treatment: fixtures.Treatment(), Policy: treatment.DefaultPolicy(), Status: treatment.StatusProposed, Revision: 1, ProposedAt: now, UpdatedAt: now}
			must(t, rp.Experiments.Save(ctx, e))
			got, err := rp.Experiments.Get(ctx, "bare")
			must(t, err)
			sameExperiment(t, "expected the bare experiment", got, e)
		}},
		{"max-cohort", "boundary: a cohort of MaxAgents positions is stored in order", func(t *testing.T, rp store.Repos) {
			e := &treatment.Experiment{ID: "big", Location: locationCst, Hypothesis: "h", Treatment: fixtures.Treatment(), Policy: treatment.DefaultPolicy(), Status: treatment.StatusProposed, Revision: 1, ProposedAt: now, UpdatedAt: now}
			for i := range treatment.MaxAgents {
				e.CohortOrder = append(e.CohortOrder, fmt.Sprintf("host-%05d", i))
			}
			must(t, rp.Experiments.Save(ctx, e))
			got, err := rp.Experiments.Get(ctx, "big")
			must(t, err)
			if !slices.Equal(got.CohortOrder, e.CohortOrder) {
				t.Fatalf("expected %d ordered cohort members, got %d", len(e.CohortOrder), len(got.CohortOrder))
			}
		}},
	}
}

// Numbered returns populated() renamed to exp-<i>, with evidence IDs of its
// own so many copies can share one store.
func Numbered(i int) *treatment.Experiment {
	e := populated()
	e.ID = fmt.Sprintf("exp-%03d", i)
	e.Baseline.ID = e.ID + "-baseline"
	after := e.LatestEvidence.Clone()
	after.ID = e.ID + "-after"
	e.LatestEvidence, e.Phases[0].Evidence = after, after.Clone()
	return e
}

// experimentListCases covers List: every row it returns must equal Get.
func experimentListCases(ctx context.Context) []repoCase {
	return []repoCase{
		{"list-many", "positive: List of 50 experiments returns each in ID order, equal to Get", func(t *testing.T, rp store.Repos) {
			for i := range 50 {
				must(t, rp.Experiments.Save(ctx, Numbered(49-i)))
			}
			list, err := rp.Experiments.List(ctx, "", 1000)
			must(t, err)
			if len(list) != 50 {
				t.Fatalf("expected 50 experiments, got %d", len(list))
			}
			for i, got := range list {
				sameExperiment(t, got.ID, got, Numbered(i))
			}
		}},
		{"shared-evidence", "positive: latest evidence and phase 0 share an ID and each gets its own copy", func(t *testing.T, rp store.Repos) {
			must(t, rp.Experiments.Save(ctx, populated()))
			list, err := rp.Experiments.List(ctx, "exp", 10)
			must(t, err)
			if len(list) != 1 {
				t.Fatalf("expected one experiment, got %d", len(list))
			}
			e := list[0]
			if e.LatestEvidence == nil || e.Phases[0].Evidence == nil || e.LatestEvidence == e.Phases[0].Evidence {
				t.Fatal("expected two distinct copies of the shared evidence")
			}
			sameEvidence(t, "shared evidence", e.Phases[0].Evidence, e.LatestEvidence)
		}},
		{"limit-one", "boundary: limit 1 returns only the lowest ID", func(t *testing.T, rp store.Repos) {
			for i := range 4 {
				must(t, rp.Experiments.Save(ctx, Numbered(3-i)))
			}
			list, err := rp.Experiments.List(ctx, "", 1)
			must(t, err)
			if len(list) != 1 || list[0].ID != "exp-000" {
				t.Fatalf("expected only exp-000, got %d", len(list))
			}
		}},
		{"no-children", "boundary: an experiment without phases, cohort or evidence lists with nil evidence", func(t *testing.T, rp store.Repos) {
			e := &treatment.Experiment{ID: "bare", Location: locationCst, Hypothesis: "h", Treatment: fixtures.Treatment(), Policy: treatment.DefaultPolicy(), Status: treatment.StatusProposed, Revision: 1, ProposedAt: now, UpdatedAt: now}
			must(t, rp.Experiments.Save(ctx, e))
			list, err := rp.Experiments.List(ctx, "", 10)
			must(t, err)
			if len(list) != 1 {
				t.Fatalf("expected one experiment, got %d", len(list))
			}
			got := list[0]
			if got.Baseline != nil || got.LatestEvidence != nil || len(got.Phases) != 0 || len(got.CohortOrder) != 0 {
				t.Fatalf("expected no children, got %+v", got)
			}
			sameExperiment(t, "bare experiment", got, e)
		}},
	}
}

// experimentQueryCases covers lookups, conflicts, listing and status filters.
func experimentQueryCases(ctx context.Context) []repoCase {
	return []repoCase{
		{missingCst, "negative: an unknown experiment is ErrNotFound and does not exist", func(t *testing.T, rp store.Repos) {
			if _, err := rp.Experiments.Get(ctx, "ghost"); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("expected ErrNotFound, got %v", err)
			}
			exists, err := rp.Experiments.Exists(ctx, "ghost")
			must(t, err)
			if exists {
				t.Fatal("expected Exists to be false")
			}
			list, err := rp.Experiments.List(ctx, "ghost", 10)
			must(t, err)
			if len(list) != 0 {
				t.Fatalf("expected an empty list, got %d", len(list))
			}
		}},
		{"evidence-conflict", "negative: an experiment cannot claim evidence another experiment stored", func(t *testing.T, rp store.Repos) {
			e := populated()
			must(t, rp.Experiments.Save(ctx, e))
			other := e.Clone()
			other.ID = "other"
			if err := rp.Experiments.Save(ctx, other); !errors.Is(err, store.ErrEvidenceConflict) {
				t.Fatalf("expected ErrEvidenceConflict, got %v", err)
			}
			exists, err := rp.Experiments.Exists(ctx, "other")
			must(t, err)
			if exists {
				t.Fatal("expected the conflicting save to leave no row")
			}
		}},
		{"list-and-ids", "positive: List orders by ID and honors the limit; IDs filters by status", func(t *testing.T, rp store.Repos) {
			for i, s := range []treatment.Status{treatment.StatusRunning, treatment.StatusProposed, treatment.StatusRetired, treatment.StatusRunning} {
				e := &treatment.Experiment{ID: fmt.Sprintf("e%d", 3-i), Location: locationCst, Hypothesis: "h", Treatment: fixtures.Treatment(), Policy: treatment.DefaultPolicy(), Status: s, Revision: 1, ProposedAt: now, UpdatedAt: now}
				must(t, rp.Experiments.Save(ctx, e))
			}
			list, err := rp.Experiments.List(ctx, "", 3)
			must(t, err)
			ids := make([]string, len(list))
			for i, e := range list {
				ids[i] = e.ID
			}
			if !slices.Equal(ids, []string{"e0", "e1", "e2"}) {
				t.Fatalf("expected e0,e1,e2, got %v", ids)
			}
			one, err := rp.Experiments.List(ctx, "e3", 3)
			must(t, err)
			if len(one) != 1 || one[0].ID != "e3" {
				t.Fatalf("expected only e3, got %d", len(one))
			}
			running, err := rp.Experiments.IDs(ctx, treatment.StatusRunning, treatment.StatusPaused)
			must(t, err)
			none, err := rp.Experiments.IDs(ctx)
			must(t, err)
			if !slices.Equal(running, []string{"e0", "e3"}) || len(none) != 0 {
				t.Fatalf("expected [e0 e3] and nothing for no statuses, got %v %v", running, none)
			}
		}},
	}
}

// eligibility is the discovery contract: which prior experiments reserve a
// destination or consume the location's single-experiment budget.
func eligibility(t *testing.T, open Opener) {
	ctx := context.Background()
	dst := netip.MustParsePrefix(destinationCst)
	for _, tc := range []struct {
		name, description     string
		status                treatment.Status
		age                   time.Duration
		location, destination string
		expectBlocked         bool
		expectActive          bool
	}{
		{"proposed", "positive: pending review reserves the destination", treatment.StatusProposed, 48 * time.Hour, locationCst, destinationCst, true, true},
		{"running", "positive: an active trial is not rediscovered", treatment.StatusRunning, 48 * time.Hour, locationCst, destinationCst, true, true},
		{"paused", "positive: paused trials remain reserved", treatment.StatusPaused, 48 * time.Hour, locationCst, destinationCst, true, true},
		{"suspended", "positive: temporary holds are not bypassed", treatment.StatusSuspended, 48 * time.Hour, locationCst, destinationCst, true, true},
		{"stable", "positive: completed stable treatments stay reserved and free the budget", treatment.StatusComplete, 48 * time.Hour, locationCst, destinationCst, true, false},
		{"recent-rollback", "negative: a recent withdrawal is in cooldown", treatment.StatusRolledBack, 23 * time.Hour, locationCst, destinationCst, true, false},
		{"recent-retirement", "negative: a recent retirement is in cooldown", treatment.StatusRetired, 23 * time.Hour, locationCst, destinationCst, true, false},
		{"cooldown-boundary", "boundary: cooldown ends at exactly 24 hours", treatment.StatusRolledBack, 24 * time.Hour, locationCst, destinationCst, false, false},
		{"old-retirement", "boundary: an old retirement does not reserve the destination", treatment.StatusRetired, 25 * time.Hour, locationCst, destinationCst, false, false},
		{"other-location", "corner: reservation is location specific", treatment.StatusRunning, time.Hour, "dc2", destinationCst, false, false},
		{"other-destination", "corner: another destination consumes the budget only", treatment.StatusRunning, time.Hour, locationCst, "192.0.2.2/32", false, true},
		{"covering-prefix", "corner: a /24 experiment does not reserve a /32 inside it", treatment.StatusRunning, time.Hour, locationCst, "192.0.2.0/24", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rp := open(t).Repos()
			tr := fixtures.Treatment()
			tr.Destination = netip.MustParsePrefix(tc.destination)
			must(t, rp.Experiments.Save(ctx, &treatment.Experiment{ID: "prior", Location: tc.location, Hypothesis: "h", Treatment: tr, Policy: treatment.DefaultPolicy(), Status: tc.status, Revision: 1, ProposedAt: now.Add(-tc.age), UpdatedAt: now.Add(-tc.age)}))
			blocked, err := rp.Experiments.DestinationBlocked(ctx, locationCst, dst, now.Add(-24*time.Hour))
			must(t, err)
			active, err := rp.Experiments.HasActive(ctx, locationCst)
			must(t, err)
			if blocked != tc.expectBlocked || active != tc.expectActive {
				t.Fatalf("%s: blocked=%t active=%t, expected %t/%t", tc.description, blocked, active, tc.expectBlocked, tc.expectActive)
			}
		})
	}
	t.Run("v6-destination", func(t *testing.T) {
		t.Log("corner: a /128 destination is reserved by exact match")
		rp := open(t).Repos()
		tr := fixtures.Treatment()
		tr.Destination = netip.MustParsePrefix("2001:db8::1/128")
		must(t, rp.Experiments.Save(ctx, &treatment.Experiment{ID: "v6", Location: locationCst, Hypothesis: "h", Treatment: tr, Policy: treatment.DefaultPolicy(), Status: treatment.StatusRunning, Revision: 1, ProposedAt: now, UpdatedAt: now}))
		hit, err := rp.Experiments.DestinationBlocked(ctx, locationCst, tr.Destination, now)
		must(t, err)
		miss, err := rp.Experiments.DestinationBlocked(ctx, locationCst, netip.MustParsePrefix("2001:db8::2/128"), now)
		must(t, err)
		if !hit || miss {
			t.Fatalf("expected exact /128 match only, got hit=%t miss=%t", hit, miss)
		}
	})
}

func evidence(t *testing.T, open Opener) {
	ctx := context.Background()
	base := func() *treatment.Evidence {
		return &treatment.Evidence{ID: "ev-1", AnalyzedAt: now, WindowStart: now.Add(-time.Hour), WindowEnd: now, QueryVersion: "v1", EnrichmentVersion: "e1", ArtifactSHA256: strings.Repeat("b", 64), Hosts: []treatment.HostEvidence{{AgentID: host1Cst, Sockets: 10, BytesAcked: 1 << 20, GoodputBps: 1e6, RetransmissionRatio: .001, RttP95Ms: 2.5, ExposedSockets: 9, CollectionHealthy: true}}}
	}
	for _, tc := range []struct {
		name, description string
		run               func(t *testing.T, v store.Evidence)
	}{
		{"roundtrip", "positive: every column and host row survives", func(t *testing.T, v store.Evidence) {
			e := base()
			must(t, v.Put(ctx, "exp", e))
			got, err := v.Get(ctx, "ev-1")
			must(t, err)
			sameEvidence(t, "expected the stored evidence", got, e)
		}},
		{"idempotent", "positive: storing identical content again under the same owner is a no-op", func(t *testing.T, v store.Evidence) {
			must(t, v.Put(ctx, "exp", base()))
			must(t, v.Put(ctx, "exp", base()))
		}},
		{"content-conflict", "negative: the same ID with different content is rejected", func(t *testing.T, v store.Evidence) {
			must(t, v.Put(ctx, "exp", base()))
			e := base()
			e.Hosts[0].GoodputBps++
			if err := v.Put(ctx, "exp", e); !errors.Is(err, store.ErrEvidenceConflict) {
				t.Fatalf("expected ErrEvidenceConflict, got %v", err)
			}
			got, err := v.Get(ctx, "ev-1")
			must(t, err)
			sameEvidence(t, "expected the original content to remain", got, base())
		}},
		{"owner-conflict", "negative: another experiment cannot adopt the evidence", func(t *testing.T, v store.Evidence) {
			must(t, v.Put(ctx, "exp", base()))
			if err := v.Put(ctx, "other", base()); !errors.Is(err, store.ErrEvidenceConflict) {
				t.Fatalf("expected ErrEvidenceConflict, got %v", err)
			}
		}},
		{"nanosecond-clock", "corner: evidence stamped with a nanosecond clock re-saves after a microsecond read back", func(t *testing.T, v store.Evidence) {
			e := base()
			e.AnalyzedAt = e.AnalyzedAt.Add(1500 * time.Nanosecond)
			e.WindowEnd = e.WindowEnd.Add(999 * time.Nanosecond)
			must(t, v.Put(ctx, "exp", e))
			got, err := v.Get(ctx, "ev-1")
			must(t, err)
			must(t, v.Put(ctx, "exp", got))
			must(t, v.Put(ctx, "exp", e))
		}},
		{missingCst, "negative: unknown evidence is ErrNotFound", func(t *testing.T, v store.Evidence) {
			if _, err := v.Get(ctx, "ghost"); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("expected ErrNotFound, got %v", err)
			}
		}},
		{"no-hosts", "boundary: evidence with an empty population stores and reads back empty", func(t *testing.T, v store.Evidence) {
			e := base()
			e.Hosts = nil
			must(t, v.Put(ctx, "exp", e))
			got, err := v.Get(ctx, "ev-1")
			must(t, err)
			if len(got.Hosts) != 0 {
				t.Fatalf("expected no hosts, got %d", len(got.Hosts))
			}
		}},
		{"max-hosts", "boundary: MaxAgents host rows are stored in order in one call", func(t *testing.T, v store.Evidence) {
			e := base()
			e.Hosts = nil
			for i := range treatment.MaxAgents {
				e.Hosts = append(e.Hosts, treatment.HostEvidence{AgentID: fmt.Sprintf("host-%05d", i), Sockets: uint64(i), BytesAcked: 1 << 20, GoodputBps: float64(i), RttP95Ms: 1, CollectionHealthy: i%2 == 0})
			}
			must(t, v.Put(ctx, "exp", e))
			got, err := v.Get(ctx, "ev-1")
			must(t, err)
			if len(got.Hosts) != treatment.MaxAgents {
				t.Fatalf("expected %d hosts, got %d", treatment.MaxAgents, len(got.Hosts))
			}
			for i := range got.Hosts {
				if got.Hosts[i] != e.Hosts[i] {
					t.Fatalf("expected host %d to be %+v, got %+v", i, e.Hosts[i], got.Hosts[i])
				}
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Log(tc.description)
			tc.run(t, open(t).Repos().Evidence)
		})
	}
}

func holds(t *testing.T, open Opener) {
	ctx := context.Background()
	for _, tc := range []struct {
		name, description string
		set, clear, ask   []string
		expected          bool
	}{
		{fleetScopeCst, "positive: a fleet hold answers any scope list containing it", []string{fleetScopeCst}, nil, []string{fleetScopeCst, locationScopeCst, agentScopeCst}, true},
		{"narrower", "positive: an agent hold is seen when asked for that agent", []string{agentScopeCst}, nil, []string{fleetScopeCst, locationScopeCst, agentScopeCst}, true},
		{"cleared", "negative: a cleared hold no longer answers", []string{fleetScopeCst}, []string{fleetScopeCst}, []string{fleetScopeCst}, false},
		{"other-scope", "negative: holds on other scopes do not answer", []string{"location:dc2"}, nil, []string{fleetScopeCst, locationScopeCst}, false},
		{"no-scopes", "boundary: asking for no scopes is never held", []string{fleetScopeCst}, nil, nil, false},
		{"clear-missing", "corner: clearing an absent scope is not an error and leaves others set", []string{fleetScopeCst}, []string{"agent:ghost"}, []string{fleetScopeCst}, true},
		{"reset-reason", "corner: setting a held scope again keeps it held", []string{fleetScopeCst, fleetScopeCst}, nil, []string{fleetScopeCst}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Log(tc.description)
			h := open(t).Repos().Holds
			for _, s := range tc.set {
				must(t, h.Set(ctx, s, "reason "+s))
			}
			for _, s := range tc.clear {
				must(t, h.Clear(ctx, s))
			}
			got, err := h.Held(ctx, tc.ask...)
			must(t, err)
			if got != tc.expected {
				t.Fatalf("expected held=%t, got %t", tc.expected, got)
			}
		})
	}
}

func audit(t *testing.T, open Opener) {
	ctx := context.Background()
	for _, tc := range []struct {
		name, description string
		run               func(t *testing.T, a store.Audit)
	}{
		{"append-lookup", "positive: an event is found by request ID with its payload intact", func(t *testing.T, a store.Audit) {
			must(t, a.Append(ctx, "operator/alice", "req-1", "review", []byte(`{"approve":true}`)))
			got, err := a.Lookup(ctx, "req-1")
			must(t, err)
			if got.Actor != "operator/alice" || got.Kind != "review" || got.RequestID != "req-1" || got.Sequence < 1 || got.OccurredAt.IsZero() || !strings.Contains(string(got.Payload), `"approve"`) {
				t.Fatalf("expected the appended event, got %+v", got)
			}
		}},
		{"duplicate-request", "negative: a request ID can be appended once", func(t *testing.T, a store.Audit) {
			must(t, a.Append(ctx, "x", "req-1", "review", []byte(`{}`)))
			if err := a.Append(ctx, "x", "req-1", "control", []byte(`{}`)); !errors.Is(err, store.ErrExists) {
				t.Fatalf("expected ErrExists, got %v", err)
			}
			n, err := a.Count(ctx)
			must(t, err)
			if n != 1 {
				t.Fatalf("expected one event, got %d", n)
			}
		}},
		{"anonymous-events", "boundary: events without a request ID never collide", func(t *testing.T, a store.Audit) {
			must(t, a.Append(ctx, "maintenance", "", "renew", []byte(`{}`)))
			must(t, a.Append(ctx, "maintenance", "", "renew", []byte(`{}`)))
			n, err := a.Count(ctx)
			must(t, err)
			if n != 2 {
				t.Fatalf("expected two events, got %d", n)
			}
			if _, err := a.Lookup(ctx, ""); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("expected ErrNotFound for the empty request ID, got %v", err)
			}
		}},
		{missingCst, "negative: an unknown request ID is ErrNotFound", func(t *testing.T, a store.Audit) {
			if _, err := a.Lookup(ctx, "ghost"); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("expected ErrNotFound, got %v", err)
			}
		}},
		{"monotonic", "corner: sequence numbers increase in append order", func(t *testing.T, a store.Audit) {
			var last int64
			for i := range 5 {
				id := fmt.Sprintf("req-%d", i)
				must(t, a.Append(ctx, "x", id, "k", []byte(`{}`)))
				got, err := a.Lookup(ctx, id)
				must(t, err)
				if got.Sequence <= last {
					t.Fatalf("expected sequence above %d, got %d", last, got.Sequence)
				}
				last = got.Sequence
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Log(tc.description)
			tc.run(t, open(t).Repos().Audit)
		})
	}
}

func tx(t *testing.T, open Opener) {
	ctx := context.Background()
	t.Run("commit", func(t *testing.T) {
		t.Log("positive: writes inside a successful Tx are visible afterwards")
		s := open(t)
		must(t, s.Tx(ctx, func(rp store.Repos) error {
			if err := rp.Holds.Set(ctx, fleetScopeCst, "incident"); err != nil {
				return err
			}
			return rp.Agents.Enroll(ctx, host1Cst, locationCst, fixtures.Desired(now))
		}))
		held, err := s.Repos().Holds.Held(ctx, fleetScopeCst)
		must(t, err)
		if _, err := s.Repos().Agents.Get(ctx, host1Cst); err != nil || !held {
			t.Fatalf("expected committed hold and agent, got held=%t err=%v", held, err)
		}
	})
	t.Run("rollback", func(t *testing.T) {
		t.Log("negative: a failing Tx leaves no trace of its writes")
		s := open(t)
		boom := errors.New("boom")
		err := s.Tx(ctx, func(rp store.Repos) error {
			if err := rp.Holds.Set(ctx, fleetScopeCst, "incident"); err != nil {
				return err
			}
			if err := rp.Audit.Append(ctx, "x", "req-1", "k", []byte(`{}`)); err != nil {
				return err
			}
			return boom
		})
		if !errors.Is(err, boom) {
			t.Fatalf("expected the callback error, got %v", err)
		}
		held, err := s.Repos().Holds.Held(ctx, fleetScopeCst)
		must(t, err)
		n, err := s.Repos().Audit.Count(ctx)
		must(t, err)
		if held || n != 0 {
			t.Fatalf("expected nothing persisted, got held=%t events=%d", held, n)
		}
	})
	t.Run("read-your-writes", func(t *testing.T) {
		t.Log("positive: a Tx sees its own uncommitted writes")
		s := open(t)
		must(t, s.Tx(ctx, func(rp store.Repos) error {
			if err := rp.Experiments.Save(ctx, populated()); err != nil {
				return err
			}
			got, err := rp.Experiments.Get(ctx, "exp")
			if err != nil {
				return err
			}
			if got.Revision != 2 {
				return fmt.Errorf("expected revision 2 inside the transaction, got %d", got.Revision)
			}
			return nil
		}))
	})
	t.Run("list-in-tx", func(t *testing.T) { listInTx(t, open) })
	t.Run("canceled-context", func(t *testing.T) {
		t.Log("boundary: a canceled context fails the Tx and persists nothing")
		s := open(t)
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		if err := s.Tx(canceled, func(rp store.Repos) error { return rp.Holds.Set(ctx, fleetScopeCst, "x") }); err == nil {
			t.Fatal("expected an error from a canceled context")
		}
		held, err := s.Repos().Holds.Held(ctx, fleetScopeCst)
		must(t, err)
		if held {
			t.Fatal("expected no hold after a canceled transaction")
		}
	})
	t.Run("serializes", func(t *testing.T) {
		t.Log("corner: two concurrent Tx calls run one after the other, both commit, sequences stay monotonic")
		s := open(t)
		entered := make(chan struct{})
		var mu sync.Mutex
		var order []string
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			must(t, s.Tx(ctx, func(rp store.Repos) error {
				close(entered)
				mu.Lock()
				order = append(order, "first-start")
				mu.Unlock()
				time.Sleep(200 * time.Millisecond)
				mu.Lock()
				order = append(order, "first-end")
				mu.Unlock()
				return rp.Audit.Append(ctx, "x", "first", "k", []byte(`{}`))
			}))
		}()
		go func() {
			defer wg.Done()
			<-entered
			must(t, s.Tx(ctx, func(rp store.Repos) error {
				mu.Lock()
				order = append(order, "second-start")
				mu.Unlock()
				return rp.Audit.Append(ctx, "x", "second", "k", []byte(`{}`))
			}))
		}()
		wg.Wait()
		if !slices.Equal(order, []string{"first-start", "first-end", "second-start"}) {
			t.Fatalf("expected the second transaction to wait for the first, got %v", order)
		}
		first, err := s.Repos().Audit.Lookup(ctx, "first")
		must(t, err)
		second, err := s.Repos().Audit.Lookup(ctx, "second")
		must(t, err)
		if first.Sequence >= second.Sequence {
			t.Fatalf("expected sequence %d < %d", first.Sequence, second.Sequence)
		}
	})
}

// settle separates two stamps by more than a timestamptz microsecond so the
// watermark tests order them deterministically on either store.
// listInTx: List inside a Tx locks its rows and returns what List outside
// returns.
func listInTx(t *testing.T, open Opener) {
	ctx := context.Background()
	t.Log("corner: List inside a Tx locks its rows and returns what List outside returns")
	s := open(t)
	for i := range 3 {
		must(t, s.Repos().Experiments.Save(ctx, Numbered(i)))
	}
	outside, err := s.Repos().Experiments.List(ctx, "", 10)
	must(t, err)
	must(t, s.Tx(ctx, func(rp store.Repos) error {
		inside, err := rp.Experiments.List(ctx, "", 10)
		if err != nil {
			return err
		}
		if len(inside) != len(outside) {
			return fmt.Errorf("expected %d experiments inside the transaction, got %d", len(outside), len(inside))
		}
		for i := range inside {
			sameExperiment(t, inside[i].ID, inside[i], outside[i])
		}
		return nil
	}))
}

func settle() { time.Sleep(2 * time.Millisecond) }

func lease(t *testing.T, open Opener) {
	ctx := context.Background()
	t.Run("free", func(t *testing.T) {
		t.Log("positive: a free lease runs fn and reports held")
		s := open(t)
		ran := false
		held, err := s.Lease(ctx, "housekeeping", func() error { ran = true; return nil })
		must(t, err)
		if !held || !ran {
			t.Fatalf("expected held and run, got held=%t ran=%t", held, ran)
		}
	})
	t.Run("held-elsewhere", func(t *testing.T) {
		t.Log("corner: while one caller holds the lease a second caller is told so and fn never runs")
		s := open(t)
		entered, release := make(chan struct{}), make(chan struct{})
		var wg sync.WaitGroup
		wg.Go(func() {
			held, err := s.Lease(ctx, "housekeeping", func() error { close(entered); <-release; return nil })
			if err != nil || !held {
				t.Errorf("first holder: held=%t err=%v", held, err)
			}
		})
		<-entered
		held, err := s.Lease(ctx, "housekeeping", func() error { t.Error("second caller ran under a held lease"); return nil })
		must(t, err)
		if held {
			t.Fatal("expected the second caller to be refused")
		}
		close(release)
		wg.Wait()
		held, err = s.Lease(ctx, "housekeeping", func() error { return nil })
		must(t, err)
		if !held {
			t.Fatal("expected the lease to be free once the holder returned")
		}
	})
	t.Run("released-on-error", func(t *testing.T) {
		t.Log("negative: fn's error is returned with held=true and the lease is released")
		s := open(t)
		boom := errors.New("boom")
		held, err := s.Lease(ctx, "housekeeping", func() error { return boom })
		if !held || !errors.Is(err, boom) {
			t.Fatalf("expected held with the callback error, got held=%t err=%v", held, err)
		}
		held, err = s.Lease(ctx, "housekeeping", func() error { return nil })
		must(t, err)
		if !held {
			t.Fatal("expected the lease to be free after a failed pass")
		}
	})
	t.Run("independent-names", func(t *testing.T) {
		t.Log("boundary: different names are different leases")
		s := open(t)
		held, err := s.Lease(ctx, "a", func() error {
			inner, err := s.Lease(ctx, "b", func() error { return nil })
			if err != nil || !inner {
				return fmt.Errorf("expected lease b while holding a, got held=%t err=%v", inner, err)
			}
			return nil
		})
		must(t, err)
		if !held {
			t.Fatal("expected lease a")
		}
	})
	t.Run("canceled-context", func(t *testing.T) {
		t.Log("negative: a canceled context fails before fn runs")
		s := open(t)
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		held, err := s.Lease(canceled, "housekeeping", func() error { t.Error("fn ran with a canceled context"); return nil })
		if err == nil || held {
			t.Fatalf("expected an error and held=false, got held=%t err=%v", held, err)
		}
	})
}

func metrics(t *testing.T, open Opener) {
	ctx := context.Background()
	t.Run("empty", func(t *testing.T) {
		t.Log("boundary: an empty store reports every gauge as zero")
		got, err := open(t).Repos().Metrics.Snapshot(ctx, now)
		must(t, err)
		for _, k := range []string{"agents", "agents_connected", "agents_suspended", "assignments", "routes_verified", "routes_failed", "routes_expired", "experiments_pending", "experiments_running", "experiments_complete", "audit_events"} {
			v, ok := got[k]
			if !ok || v != 0 {
				t.Fatalf("expected %s=0, got %v (present=%t)", k, v, ok)
			}
		}
	})
	t.Run("populated", func(t *testing.T) {
		t.Log("positive: gauges reflect agents, records, experiments and events")
		s := open(t)
		rp := s.Repos()
		must(t, rp.Agents.Enroll(ctx, host1Cst, locationCst, fixtures.Desired(now)))
		must(t, rp.Agents.Observe(ctx, report(host1Cst, locationCst), now.Add(-time.Minute)))
		must(t, rp.Agents.Enroll(ctx, host2Cst, locationCst, &treatment.DesiredState{AgentID: host2Cst, Revision: 1, Suspended: true, IssuedAt: now}))
		must(t, rp.Agents.Enroll(ctx, "host-3", locationCst, &treatment.DesiredState{AgentID: "host-3", Revision: 1, IssuedAt: now}))
		must(t, rp.Agents.Observe(ctx, &treatment.AgentReport{AgentID: "host-3", Location: locationCst, Capabilities: treatment.Capabilities{BootID: "b"}, Records: []treatment.ApplicationRecord{{Assignment: fixtures.Desired(now).Assignments[0], Status: treatment.ApplyExpired, ExpiredLatched: true}}}, now.Add(-10*time.Minute)))
		for id, st := range map[string]treatment.Status{"p": treatment.StatusProposed, "r": treatment.StatusRunning, "c": treatment.StatusComplete, "x": treatment.StatusRetired} {
			must(t, rp.Experiments.Save(ctx, &treatment.Experiment{ID: id, Location: locationCst, Hypothesis: "h", Treatment: fixtures.Treatment(), Policy: treatment.DefaultPolicy(), Status: st, Revision: 1, ProposedAt: now, UpdatedAt: now}))
		}
		must(t, rp.Audit.Append(ctx, "x", "", "k", []byte(`{}`)))
		got, err := rp.Metrics.Snapshot(ctx, now)
		must(t, err)
		expected := map[string]float64{"agents": 3, "agents_connected": 1, "agents_suspended": 2, "assignments": 1, "routes_verified": 1, "routes_failed": 1, "routes_expired": 1, "experiments_pending": 1, "experiments_running": 1, "experiments_complete": 1, "audit_events": 1}
		for k, v := range expected {
			if got[k] != v {
				t.Errorf("expected %s=%g, got %g", k, v, got[k])
			}
		}
	})
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
