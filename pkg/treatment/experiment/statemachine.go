package experiment

import (
	"errors"
	"fmt"

	"github.com/randomizedcoder/xtcp2/pkg/treatment"
)

// Event is something that can change an experiment's status. Review, Control
// and Maintain all go through Transition, so the complete set of legal status
// changes is the table below and nothing else.
type Event uint8

const (
	eventUnspecified Event = iota
	// EventApprove: a reviewer approved the proposal or the current phase;
	// the next phase is issued.
	EventApprove
	// EventApproveFinal: a reviewer approved the last (100%) phase.
	EventApproveFinal
	// EventReject: a reviewer declined; the treatment is retired.
	EventReject
	// EventPause: an operator paused a running trial; routes stay in place.
	EventPause
	// EventSuspend: an operator suspended; routes are withdrawn but the
	// experiment may be resumed.
	EventSuspend
	// EventResume: an operator resumed a held trial; a fresh phase of the
	// same size is issued.
	EventResume
	// EventResumeStable: an operator resumed a held completed treatment.
	EventResumeStable
	// EventRollback: an operator or the safety controller withdrew the
	// treatment and restored any predecessor.
	EventRollback
	// EventRetire: an operator ended the experiment without restoring a
	// predecessor.
	EventRetire
	// EventExpire: the phase exceeded the policy's maximum duration.
	EventExpire
	// EventHarm: evidence crossed a guardrail.
	EventHarm
	eventCount
)

var eventNames = [eventCount]string{"unspecified", "approve", "approve_final", "reject", "pause", "suspend", "resume", "resume_stable", "rollback", "retire", "expire", "harm"}

func (e Event) String() string {
	if e < eventCount {
		return eventNames[e]
	}
	return fmt.Sprintf("event(%d)", uint8(e))
}

// Events lists every defined event, for exhaustive tests.
func Events() []Event {
	out := make([]Event, 0, eventCount-1)
	for e := EventApprove; e < eventCount; e++ {
		out = append(out, e)
	}
	return out
}

// ErrIllegalTransition is returned for any (status, event) pair not in the
// table. Callers map it to FailedPrecondition.
var ErrIllegalTransition = errors.New("illegal experiment transition")

type transitionKey struct {
	from  treatment.Status
	event Event
}

const (
	proposed   = treatment.StatusProposed
	running    = treatment.StatusRunning
	paused     = treatment.StatusPaused
	suspended  = treatment.StatusSuspended
	complete   = treatment.StatusComplete
	retired    = treatment.StatusRetired
	rolledBack = treatment.StatusRolledBack
)

// transitions is the whole state machine. Retired and rolled back are
// terminal: nothing reactivates them. A new experiment is proposed instead.
var transitions = map[transitionKey]treatment.Status{
	{proposed, EventApprove}:  running,
	{proposed, EventReject}:   retired,
	{proposed, EventSuspend}:  suspended,
	{proposed, EventRollback}: rolledBack,
	{proposed, EventRetire}:   retired,

	{running, EventApprove}:      running,
	{running, EventApproveFinal}: complete,
	{running, EventReject}:       retired,
	{running, EventPause}:        paused,
	{running, EventSuspend}:      suspended,
	{running, EventRollback}:     rolledBack,
	{running, EventRetire}:       retired,
	{running, EventExpire}:       rolledBack,
	{running, EventHarm}:         rolledBack,

	{paused, EventReject}:       retired,
	{paused, EventSuspend}:      suspended,
	{paused, EventResume}:       running,
	{paused, EventResumeStable}: complete,
	{paused, EventRollback}:     rolledBack,
	{paused, EventRetire}:       retired,
	{paused, EventExpire}:       rolledBack,

	{suspended, EventReject}:       retired,
	{suspended, EventSuspend}:      suspended, // re-suspend with a new reason is a no-op change
	{suspended, EventResume}:       running,
	{suspended, EventResumeStable}: complete,
	{suspended, EventRollback}:     rolledBack,
	{suspended, EventRetire}:       retired,

	{complete, EventReject}:   retired,
	{complete, EventSuspend}:  suspended,
	{complete, EventRollback}: rolledBack,
	{complete, EventRetire}:   retired,
}

// Transition returns the status that follows from applying event to from, or
// ErrIllegalTransition. It is pure; callers bump the revision and persist.
func Transition(from treatment.Status, event Event) (treatment.Status, error) {
	to, ok := transitions[transitionKey{from, event}]
	if !ok {
		return from, fmt.Errorf("%w: %s on %s", ErrIllegalTransition, event, from)
	}
	return to, nil
}
