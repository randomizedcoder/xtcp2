package experiment

import (
	"errors"
	"testing"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_treatment"
)

// legal is the expected state machine written out independently of the
// implementation table. A pair missing here must return ErrIllegalTransition.
var legal = map[pb.ExperimentStatus]map[Event]pb.ExperimentStatus{
	proposed: {
		EventApprove:  running,
		EventReject:   retired,
		EventSuspend:  suspended,
		EventRollback: rolledBack,
		EventRetire:   retired,
	},
	running: {
		EventApprove:      running,
		EventApproveFinal: complete,
		EventReject:       retired,
		EventPause:        paused,
		EventSuspend:      suspended,
		EventRollback:     rolledBack,
		EventRetire:       retired,
		EventExpire:       rolledBack,
		EventHarm:         rolledBack,
	},
	paused: {
		EventReject:       retired,
		EventSuspend:      suspended,
		EventResume:       running,
		EventResumeStable: complete,
		EventRollback:     rolledBack,
		EventRetire:       retired,
		EventExpire:       rolledBack,
	},
	suspended: {
		EventReject:       retired,
		EventSuspend:      suspended,
		EventResume:       running,
		EventResumeStable: complete,
		EventRollback:     rolledBack,
		EventRetire:       retired,
	},
	complete: {
		EventReject:   retired,
		EventSuspend:  suspended,
		EventRollback: rolledBack,
		EventRetire:   retired,
	},
	retired:    {},
	rolledBack: {},
	pb.ExperimentStatus_EXPERIMENT_STATUS_UNSPECIFIED: {},
}

// TestTransitionExhaustive visits every (status, event) pair the generated
// enums and Events() can produce and checks it against the legal table, so
// adding a status, an event or a transition without updating both fails.
func TestTransitionExhaustive(t *testing.T) {
	statuses, events := Statuses(), Events()
	if len(statuses) != len(legal) {
		t.Fatalf("legal table covers %d statuses, proto defines %d", len(legal), len(statuses))
	}
	if len(events) != int(eventCount)-1 {
		t.Fatalf("Events() returned %d, want %d", len(events), eventCount-1)
	}
	visited, legalCount := 0, 0
	for _, from := range statuses {
		row, ok := legal[from]
		if !ok {
			t.Fatalf("status %s has no row in the legal table", from)
		}
		for _, ev := range events {
			visited++
			got, err := Transition(from, ev)
			want, allowed := row[ev]
			switch {
			case allowed:
				legalCount++
				if err != nil || got != want {
					t.Errorf("%s on %s: got %s err=%v; want %s", ev, from, got, err, want)
				}
			case !errors.Is(err, ErrIllegalTransition):
				t.Errorf("%s on %s: want ErrIllegalTransition, got %s err=%v", ev, from, got, err)
			case got != from:
				t.Errorf("%s on %s: illegal transition must leave the status unchanged, got %s", ev, from, got)
			}
		}
	}
	if visited != len(statuses)*len(events) {
		t.Fatalf("visited %d pairs, want %d", visited, len(statuses)*len(events))
	}
	if legalCount != len(transitions) {
		t.Fatalf("implementation has %d transitions, legal table has %d", len(transitions), legalCount)
	}
}

func TestTransitionCases(t *testing.T) {
	for _, tc := range []struct {
		name, description string
		from              pb.ExperimentStatus
		event             Event
		expected          pb.ExperimentStatus
		expectError       bool
	}{
		{"proposed-approve", "positive: first review starts the trial", proposed, EventApprove, running, false},
		{"running-approve", "positive: phase approval keeps running while cohorts remain", running, EventApprove, running, false},
		{"running-approve-final", "positive: approving the 100% phase completes", running, EventApproveFinal, complete, false},
		{"paused-resume-stable", "positive: a paused completed treatment resumes to complete", paused, EventResumeStable, complete, false},
		{"retired-approve", "negative: terminal status cannot be approved", retired, EventApprove, retired, true},
		{"rolled-back-resume", "negative: rollback is final", rolledBack, EventResume, rolledBack, true},
		{"proposed-pause", "negative: nothing to pause before the first phase", proposed, EventPause, proposed, true},
		{"complete-expire", "negative: completed treatments have no lease to expire", complete, EventExpire, complete, true},
		{"unspecified-status", "boundary: zero status accepts no event", pb.ExperimentStatus_EXPERIMENT_STATUS_UNSPECIFIED, EventApprove, pb.ExperimentStatus_EXPERIMENT_STATUS_UNSPECIFIED, true},
		{"unspecified-event", "boundary: zero event is never legal", running, eventUnspecified, running, true},
		{"event-count-sentinel", "boundary: the sentinel just past the last event is illegal", running, eventCount, running, true},
		{"unknown-event", "corner: an event value from a newer build is rejected, not defaulted", running, Event(200), running, true},
		{"suspended-suspend", "corner: re-suspending with a new reason is a self loop", suspended, EventSuspend, suspended, false},
		{"suspended-harm", "corner: no routes are installed while suspended, so harm cannot withdraw", suspended, EventHarm, suspended, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Transition(tc.from, tc.event)
			if (err != nil) != tc.expectError || got != tc.expected {
				t.Fatalf("%s: got %s err=%v; expected %s error=%t", tc.description, got, err, tc.expected, tc.expectError)
			}
			if tc.expectError && !errors.Is(err, ErrIllegalTransition) {
				t.Fatalf("%s: error %v is not ErrIllegalTransition", tc.description, err)
			}
		})
	}
}

func TestTerminal(t *testing.T) {
	for _, s := range Statuses() {
		got := Terminal(s)
		expected := len(legal[s]) == 0 && s != pb.ExperimentStatus_EXPERIMENT_STATUS_UNSPECIFIED
		if got != expected {
			t.Errorf("Terminal(%s)=%t, expected %t", s, got, expected)
		}
	}
}

func TestEventString(t *testing.T) {
	for _, tc := range []struct {
		description string
		event       Event
		expected    string
	}{
		{"positive: first event", EventApprove, "approve"},
		{"positive: last event", EventHarm, "harm"},
		{"boundary: zero value", eventUnspecified, "unspecified"},
		{"boundary: sentinel", eventCount, "event(12)"},
		{"corner: far out of range", Event(255), "event(255)"},
	} {
		if got := tc.event.String(); got != tc.expected {
			t.Errorf("%s: got %q expected %q", tc.description, got, tc.expected)
		}
	}
}
