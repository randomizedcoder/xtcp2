package experiment

import (
	"fmt"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_treatment"
)

// Outcome is the domain result of evaluating evidence against a phase, or of
// drift monitoring a completed treatment. Values are numerically identical to
// xtcp_treatment.v1.Outcome so that Wire and OutcomeFromWire are checked
// casts; TestOutcomeMirrorsWire pins that correspondence.
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
	outcomeCount // sentinel
)

var outcomeNames = [outcomeCount]string{
	"unspecified",
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

func (o Outcome) String() string {
	if o < outcomeCount {
		return outcomeNames[o]
	}
	return fmt.Sprintf("outcome(%d)", uint8(o))
}

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
	for i, n := range outcomeNames {
		if n == s {
			return Outcome(i), nil
		}
	}
	return OutcomeUnspecified, fmt.Errorf("unknown outcome %q", s)
}

// Wire converts to the protobuf enum.
func (o Outcome) Wire() pb.Outcome { return pb.Outcome(o) }

// OutcomeFromWire converts from the protobuf enum, rejecting values this
// build does not know so a newer peer cannot smuggle an unclassified result.
func OutcomeFromWire(w pb.Outcome) (Outcome, error) {
	if w < 0 || int(w) >= int(outcomeCount) {
		return OutcomeUnspecified, fmt.Errorf("unknown wire outcome %d", int32(w))
	}
	return Outcome(w), nil
}
