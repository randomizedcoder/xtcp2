package experiment

import (
	"errors"
	"fmt"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_treatment"
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

// Statuses lists every defined status, for exhaustive tests.
func Statuses() []pb.ExperimentStatus {
	out := make([]pb.ExperimentStatus, 0, len(pb.ExperimentStatus_name))
	for i := range int32(len(pb.ExperimentStatus_name)) {
		out = append(out, pb.ExperimentStatus(i))
	}
	return out
}

// ErrIllegalTransition is returned for any (status, event) pair not in the
// table. Callers map it to FailedPrecondition.
var ErrIllegalTransition = errors.New("illegal experiment transition")

type transitionKey struct {
	from  pb.ExperimentStatus
	event Event
}

const (
	proposed   = pb.ExperimentStatus_EXPERIMENT_STATUS_PROPOSED
	running    = pb.ExperimentStatus_EXPERIMENT_STATUS_RUNNING
	paused     = pb.ExperimentStatus_EXPERIMENT_STATUS_PAUSED
	suspended  = pb.ExperimentStatus_EXPERIMENT_STATUS_SUSPENDED
	complete   = pb.ExperimentStatus_EXPERIMENT_STATUS_COMPLETE
	retired    = pb.ExperimentStatus_EXPERIMENT_STATUS_RETIRED
	rolledBack = pb.ExperimentStatus_EXPERIMENT_STATUS_ROLLED_BACK
)

// transitions is the whole state machine. Retired and rolled back are
// terminal: nothing reactivates them. A new experiment is proposed instead.
var transitions = map[transitionKey]pb.ExperimentStatus{
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
func Transition(from pb.ExperimentStatus, event Event) (pb.ExperimentStatus, error) {
	to, ok := transitions[transitionKey{from, event}]
	if !ok {
		return from, fmt.Errorf("%w: %s on %s", ErrIllegalTransition, event, from)
	}
	return to, nil
}

// Terminal reports whether no event can move the experiment again.
func Terminal(s pb.ExperimentStatus) bool {
	return s == retired || s == rolledBack
}
