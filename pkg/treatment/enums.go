package treatment

import "fmt"

// The enumerations below are the domain's own. Their numeric values mirror
// the xtcp_treatment.v1 protobuf enums one-for-one so the wire package can
// convert with a range-checked cast; wire's mirror tests pin that
// correspondence. Names are the snake_case forms used in SQL, logs and docs.

// unspecifiedNameCst names the zero value of every enumeration.
const unspecifiedNameCst = "unspecified"

// Status is an experiment's lifecycle state. Legal changes are the state
// machine in package experiment.
type Status uint8

const (
	StatusUnspecified Status = iota
	StatusProposed
	StatusRunning
	StatusPaused
	StatusSuspended
	StatusComplete
	StatusRetired
	StatusRolledBack
	statusCount
)

var statusNames = [statusCount]string{unspecifiedNameCst, "proposed", "running", "paused", "suspended", "complete", "retired", "rolled_back"}

func (s Status) String() string { return enumName(uint8(s), statusNames[:], "status") }

// Valid reports whether s is a defined, non-zero status.
func (s Status) Valid() bool { return s > StatusUnspecified && s < statusCount }

// Active reports whether the experiment still consumes the location's budget.
func (s Status) Active() bool {
	return s == StatusProposed || s == StatusRunning || s == StatusPaused || s == StatusSuspended
}

// Terminal reports whether no event can move the experiment again.
func (s Status) Terminal() bool { return s == StatusRetired || s == StatusRolledBack }

// ParseStatus accepts the snake_case name.
func ParseStatus(s string) (Status, error) {
	i, err := parseEnum(s, statusNames[:], "status")
	return Status(i), err
}

// Statuses lists every defined status including unspecified, for exhaustive tests.
func Statuses() []Status {
	out := make([]Status, 0, statusCount)
	for s := StatusUnspecified; s < statusCount; s++ {
		out = append(out, s)
	}
	return out
}

// AssignmentMode says what an agent must do with an assignment.
type AssignmentMode uint8

const (
	ModeUnspecified AssignmentMode = iota
	// ModeTrial installs the treatment until the lease expires.
	ModeTrial
	// ModeStable installs the treatment without a lease.
	ModeStable
	// ModeSuspended withdraws the treatment but keeps the assignment.
	ModeSuspended
	// ModeRetired withdraws the treatment; the assignment is a tombstone.
	ModeRetired
	modeCount
)

var modeNames = [modeCount]string{unspecifiedNameCst, "trial", "stable", "suspended", "retired"}

func (m AssignmentMode) String() string { return enumName(uint8(m), modeNames[:], "mode") }

// Valid reports whether m is a defined, non-zero mode.
func (m AssignmentMode) Valid() bool { return m > ModeUnspecified && m < modeCount }

// Installs reports whether the mode asks the agent to keep a route present.
func (m AssignmentMode) Installs() bool { return m == ModeTrial || m == ModeStable }

// ParseAssignmentMode accepts the snake_case name.
func ParseAssignmentMode(s string) (AssignmentMode, error) {
	i, err := parseEnum(s, modeNames[:], "assignment mode")
	return AssignmentMode(i), err
}

// ApplyStatus is an agent's report of one assignment's kernel state.
type ApplyStatus uint8

const (
	ApplyUnspecified ApplyStatus = iota
	ApplyPending
	ApplyVerified
	ApplyRemoved
	ApplyExpired
	ApplyFailed
	applyCount
)

var applyNames = [applyCount]string{unspecifiedNameCst, "pending", "verified", "removed", "expired", "failed"}

func (a ApplyStatus) String() string { return enumName(uint8(a), applyNames[:], "apply status") }

// Valid reports whether a is a defined value. Unspecified is accepted: an
// agent may report an assignment it has not acted on yet.
func (a ApplyStatus) Valid() bool { return a < applyCount }

// ParseApplyStatus accepts the snake_case name.
func ParseApplyStatus(s string) (ApplyStatus, error) {
	i, err := parseEnum(s, applyNames[:], "apply status")
	return ApplyStatus(i), err
}

// Outcome is the result of evaluating evidence against a phase, or of drift
// monitoring a completed treatment.
type Outcome uint8

const (
	OutcomeUnspecified Outcome = iota
	OutcomeBaseline
	OutcomeAwaitingApplication
	OutcomeAwaitingExposure
	OutcomeInsufficientDuration
	OutcomeInsufficientData
	OutcomeInsufficientControls
	OutcomeStale
	OutcomeUnavailable
	OutcomeInconclusive
	OutcomeSafetyOnly
	OutcomeFinalSafety
	OutcomeBeneficial
	OutcomeHarmful
	OutcomeExpired
	OutcomeStableObserved
	OutcomeStableEvidenceIncomplete
	OutcomeStableRegressionReviewRequired
	OutcomeObserving
	outcomeCount
)

var outcomeNames = [outcomeCount]string{
	unspecifiedNameCst,
	"baseline",
	"awaiting_application",
	"awaiting_exposure",
	"insufficient_duration",
	"insufficient_data",
	"insufficient_controls",
	"stale",
	"unavailable",
	"inconclusive",
	"safety_only",
	"final_safety",
	"beneficial",
	"harmful",
	"expired",
	"stable_observed",
	"stable_evidence_incomplete",
	"stable_regression_review_required",
	"observing",
}

func (o Outcome) String() string { return enumName(uint8(o), outcomeNames[:], "outcome") }

// Valid reports whether o is a known value. Unspecified is valid: it is the
// state of an experiment nothing has evaluated yet.
func (o Outcome) Valid() bool { return o < outcomeCount }

// Promotable reports whether a reviewer may advance the phase on this
// outcome. Every other outcome blocks promotion.
func (o Outcome) Promotable() bool {
	return o == OutcomeBeneficial || o == OutcomeSafetyOnly || o == OutcomeFinalSafety
}

// Withdraws reports whether the safety controller must roll the experiment
// back on this outcome without waiting for a reviewer.
func (o Outcome) Withdraws() bool { return o == OutcomeHarmful || o == OutcomeExpired }

// RenewsLease reports whether healthy evidence with this outcome may renew
// the current phase's trial leases. Stale or missing data cannot.
func (o Outcome) RenewsLease() bool { return o != OutcomeStale && o != OutcomeInsufficientData }

// ParseOutcome accepts the snake_case names used in documentation, logs and
// the pre-enum journal format.
func ParseOutcome(s string) (Outcome, error) {
	i, err := parseEnum(s, outcomeNames[:], "outcome")
	return Outcome(i), err
}

// Outcomes lists every defined outcome including unspecified.
func Outcomes() []Outcome {
	out := make([]Outcome, 0, outcomeCount)
	for o := OutcomeUnspecified; o < outcomeCount; o++ {
		out = append(out, o)
	}
	return out
}

// EnumCounts reports how many values each domain enumeration defines, so the
// wire package can assert the protobuf enums have exactly the same size.
func EnumCounts() (statuses, modes, applyStatuses, outcomes int) {
	return int(statusCount), int(modeCount), int(applyCount), int(outcomeCount)
}

func enumName(v uint8, names []string, kind string) string {
	if int(v) < len(names) {
		return names[v]
	}
	return fmt.Sprintf("%s(%d)", kind, v)
}

func parseEnum(s string, names []string, kind string) (uint8, error) {
	for i, n := range names {
		if n == s {
			return uint8(i), nil
		}
	}
	return 0, fmt.Errorf("unknown %s %q", kind, s)
}
