package controller

import (
	"context"
	"testing"
	"time"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/evidence"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/store"
	"google.golang.org/protobuf/proto"
)

// capturingSource returns fixed evidence and records the windows it saw.
type capturingSource struct {
	value   *treatment.Evidence
	windows []evidence.Window
}

func (c *capturingSource) Evidence(_ context.Context, w evidence.Window) (*treatment.Evidence, error) {
	c.windows = append(c.windows, w)
	return c.value, nil
}

// TestEvidenceWindowsCarrySessions: every evidence window the controller
// asks for names the sessions its population reported, so the warehouse can
// drop records stamped by anyone else; housekeeping prunes old sessions.
func TestEvidenceWindowsCarrySessions(t *testing.T) {
	const restarted = "fedcba9876543210fedcba9876543210"
	for _, tc := range []struct {
		name, description string
		prepare           func(t *testing.T, s *Server, now *time.Time)
		expected          int // sessions in the analysis window
	}{
		{"one-each", "positive: each cohort host contributes the session it checked in with", func(*testing.T, *Server, *time.Time) {}, 8},
		{"restart", "positive: a host that restarted mid-window contributes both sessions", func(t *testing.T, s *Server, now *time.Time) {
			*now = now.Add(time.Minute)
			c := capabilities()
			c.CollectionId = restarted
			if _, e := s.CheckIn(testIdentity("agent/dc1/host-0"), &pb.CheckInRequest{AgentId: "host-0", Location: "dc1", Capabilities: c}); e != nil {
				t.Fatal(e)
			}
		}, 9},
		{"pruned", "corner: sessions older than the retention are pruned and no longer bind evidence", func(t *testing.T, s *Server, now *time.Time) {
			*now = now.Add(sessionRetention + time.Hour)
			if _, e := s.Housekeep(context.Background(), nil, nil, nil); e != nil {
				t.Fatal(e)
			}
		}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eachStore(t, func(t *testing.T, st store.Store) {
				now := time.Now().UTC().Truncate(time.Microsecond)
				s := testServer(st, &now)
				operator := testIdentity("operator/alice")
				enrollFleet(t, s, 8)
				p := proposal(now)
				if _, e := s.Propose(operator, p); e != nil {
					t.Fatal(e)
				}
				tc.prepare(t, s, &now)
				ev := proto.Clone(p.Experiment.Baseline).(*pb.Evidence)
				ev.Id = "bound-1"
				source := &capturingSource{value: parseEvidence(t, ev)}
				s.EvidenceSource = source
				// Analysis outlives a pruned population too; only the window matters here.
				_, _ = s.Analyze(operator, &pb.AnalyzeRequest{ExperimentId: p.Experiment.Id, ExpectedRevision: 1})
				if len(source.windows) != 1 {
					t.Fatalf("%s: expected one window, got %d", tc.description, len(source.windows))
				}
				w := source.windows[0]
				if len(w.Sessions) != tc.expected {
					t.Fatalf("%s: window carries %d sessions, want %d: %+v", tc.description, len(w.Sessions), tc.expected, w.Sessions)
				}
				if err := w.Validate(); err != nil {
					t.Fatalf("%s: bound window invalid: %v", tc.description, err)
				}
			})
		})
	}
}
