package evidence

import (
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/internal/fixtures"
)

func TestWindowFor(t *testing.T) {
	now := time.Unix(1700000000, 0).UTC()
	base := func() *treatment.Experiment {
		return &treatment.Experiment{ID: "exp", Location: "dc1", Treatment: treatment.Treatment{Destination: netip.MustParsePrefix("192.0.2.1/32")}, CohortOrder: []string{"a", "b"}}
	}
	for _, tc := range []struct {
		name, description string
		experiment        func() *treatment.Experiment
		wantStart         time.Time
	}{
		{"no-phase", "positive: before review the baseline window is the hour before the settled end", base, now.Add(-SettleDelay - time.Hour)},
		{"phase-issued", "positive: a phase without observation measures from issue", func() *treatment.Experiment {
			e := base()
			e.Phases = []treatment.Phase{{IssuedAt: now.Add(-40 * time.Minute)}}
			return e
		}, now.Add(-40 * time.Minute)},
		{"phase-observing", "positive: once observation started the window begins there", func() *treatment.Experiment {
			e := base()
			e.Phases = []treatment.Phase{{IssuedAt: now.Add(-40 * time.Minute), ObservationStartedAt: now.Add(-35 * time.Minute)}}
			return e
		}, now.Add(-35 * time.Minute)},
		{"latest-phase", "boundary: only the current phase counts", func() *treatment.Experiment {
			e := base()
			e.Phases = []treatment.Phase{{IssuedAt: now.Add(-3 * time.Hour), ObservationStartedAt: now.Add(-3 * time.Hour)}, {IssuedAt: now.Add(-10 * time.Minute)}}
			return e
		}, now.Add(-10 * time.Minute)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := tc.experiment()
			w := WindowFor(e, now)
			if !w.Start.Equal(tc.wantStart) || !w.End.Equal(now.Add(-SettleDelay)) {
				t.Fatalf("%s: window %v..%v, want start %v", tc.description, w.Start, w.End, tc.wantStart)
			}
			if w.Location != "dc1" || w.ExperimentID != "exp" || w.Destination != e.Treatment.Destination || len(w.Agents) != 2 {
				t.Fatalf("%s: identity not carried: %+v", tc.description, w)
			}
			if err := w.Validate(); err != nil {
				t.Fatalf("%s: policy window must validate: %v", tc.description, err)
			}
		})
	}
}

// session is a well-formed session of agent.
func session(agent string) treatment.Session {
	return treatment.Session{AgentID: agent, BootID: "boot-1", CollectionID: fixtures.CollectionID}
}

func TestWindowValidate(t *testing.T) {
	now := time.Unix(1700000000, 0)
	valid := func() Window {
		return Window{Location: "dc1", Destination: netip.MustParsePrefix("192.0.2.1/32"), ExperimentID: "exp", Agents: []string{"a"}, Start: now.Add(-time.Minute), End: now}
	}
	many := make([]string, treatment.MaxAgents)
	for i := range many {
		many[i] = "h" + strings.Repeat("x", i%3) + string(rune('a'+i%26)) + string(rune('a'+(i/26)%26)) + string(rune('a'+(i/676)%26))
	}
	for _, tc := range []struct {
		name, description string
		change            func(*Window)
		expected          string
	}{
		{"valid", "positive: a bounded window with one agent passes", func(*Window) {}, ""},
		{"max-agents", "boundary: MaxAgents hosts are accepted", func(w *Window) { w.Agents = many }, ""},
		{"too-many-agents", "boundary: one host over MaxAgents is refused", func(w *Window) { w.Agents = append(append([]string(nil), many...), "extra") }, "population"},
		{"max-window", "boundary: exactly seven days is accepted", func(w *Window) { w.Start = w.End.Add(-MaxWindow) }, ""},
		{"long-window", "boundary: a second over seven days is refused", func(w *Window) { w.Start = w.End.Add(-MaxWindow - time.Second) }, "window"},
		{"empty-window", "negative: end must follow start", func(w *Window) { w.Start = w.End }, "window"},
		{"no-agents", "negative: an empty population cannot be measured", func(w *Window) { w.Agents = nil }, "population"},
		{"bad-agent", "negative: an agent ID that could inject a parameter is refused", func(w *Window) { w.Agents = []string{"host'"} }, "invalid agent"},
		{"bad-destination", "negative: the zero prefix is refused", func(w *Window) { w.Destination = netip.Prefix{} }, "invalid destination"},
		{"bad-location", "corner: the location is an identifier too", func(w *Window) { w.Location = "dc 1" }, "location"},
		{"session", "positive: a session of a population agent passes", func(w *Window) { w.Sessions = []treatment.Session{session("a")} }, ""},
		{"session-outsider", "negative: a session for an agent outside the population is refused", func(w *Window) { w.Sessions = []treatment.Session{session("b")} }, "invalid session"},
		{"session-bad-nonce", "negative: a nonce that is not 32 lowercase hex digits could inject a parameter", func(w *Window) {
			s := session("a")
			s.CollectionID = strings.ToUpper(s.CollectionID)
			w.Sessions = []treatment.Session{s}
		}, "invalid session"},
		{"session-bad-boot", "negative: a boot ID with a quote is refused", func(w *Window) {
			s := session("a")
			s.BootID = "boot'"
			w.Sessions = []treatment.Session{s}
		}, "invalid session"},
		{"too-many-sessions", "boundary: one session over MaxSessions is refused", func(w *Window) {
			w.Sessions = make([]treatment.Session, MaxSessions+1)
			for i := range w.Sessions {
				w.Sessions[i] = session("a")
			}
		}, "too many sessions"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := valid()
			tc.change(&w)
			err := w.Validate()
			if tc.expected == "" && err != nil || tc.expected != "" && (err == nil || !strings.Contains(err.Error(), tc.expected)) {
				t.Fatalf("%s: got %v, want %q", tc.description, err, tc.expected)
			}
		})
	}
}

func TestCandidateQueryValidate(t *testing.T) {
	now := time.Unix(1700000000, 0)
	valid := func() CandidateQuery {
		return CandidateQuery{Location: "dc1", Start: now.Add(-time.Hour), End: now, MinimumRTTMs: 0, MaximumRTTMs: 15, MinimumRetransmissionRatio: 0}
	}
	for _, tc := range []struct {
		name, description string
		change            func(*CandidateQuery)
		expected          string
	}{
		{"valid", "positive: a template's bounds pass", func(*CandidateQuery) {}, ""},
		{"max-window", "boundary: seven days is the longest scan", func(q *CandidateQuery) { q.Start = q.End.Add(-MaxWindow) }, ""},
		{"long-window", "boundary: over seven days is refused", func(q *CandidateQuery) { q.Start = q.End.Add(-MaxWindow - time.Nanosecond) }, "window"},
		{"rtt-order", "negative: the RTT band must be non-empty", func(q *CandidateQuery) { q.MaximumRTTMs = q.MinimumRTTMs }, "bounds"},
		{"negative-rtt", "negative: a negative RTT floor is refused", func(q *CandidateQuery) { q.MinimumRTTMs = -1 }, "bounds"},
		{"retrans-over-one", "negative: a retransmission ratio above one is meaningless", func(q *CandidateQuery) { q.MinimumRetransmissionRatio = 1.01 }, "bounds"},
		{"retrans-one", "boundary: a ratio of exactly one is allowed", func(q *CandidateQuery) { q.MinimumRetransmissionRatio = 1 }, ""},
		{"bad-location", "corner: the location is an identifier", func(q *CandidateQuery) { q.Location = "" }, "window"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := valid()
			tc.change(&q)
			err := q.Validate()
			if tc.expected == "" && err != nil || tc.expected != "" && (err == nil || !strings.Contains(err.Error(), tc.expected)) {
				t.Fatalf("%s: got %v, want %q", tc.description, err, tc.expected)
			}
		})
	}
}
