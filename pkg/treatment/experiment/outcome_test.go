package experiment

import (
	"strings"
	"testing"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_treatment"
)

// TestOutcomeMirrorsWire pins the one-to-one correspondence between the Go
// Outcome constants and xtcp_treatment.v1.Outcome that Wire and
// OutcomeFromWire rely on being a plain cast.
func TestOutcomeMirrorsWire(t *testing.T) {
	if len(pb.Outcome_name) != int(outcomeCount) {
		t.Fatalf("proto defines %d outcomes, Go defines %d", len(pb.Outcome_name), outcomeCount)
	}
	for o := OutcomeUnspecified; o < outcomeCount; o++ {
		name := o.String()
		if name == "" || strings.HasPrefix(name, "outcome(") {
			t.Errorf("Outcome(%d) has no name", uint8(o))
		}
		if wire := pb.Outcome_name[int32(o)]; wire != "OUTCOME_"+strings.ToUpper(name) {
			t.Errorf("Outcome %s mirrors wire value %q", name, wire)
		}
		if parsed, err := ParseOutcome(name); err != nil || parsed != o {
			t.Errorf("ParseOutcome(%q)=%s,%v", name, parsed, err)
		}
		if back, err := OutcomeFromWire(o.Wire()); err != nil || back != o {
			t.Errorf("OutcomeFromWire(Wire(%s))=%s,%v", name, back, err)
		}
		if !o.Valid() {
			t.Errorf("%s must be valid", name)
		}
	}
}

func TestOutcomeConversions(t *testing.T) {
	for _, tc := range []struct {
		name, description string
		run               func() (Outcome, error)
		expected          Outcome
		expectError       bool
	}{
		{"parse-beneficial", "positive: documented name parses", func() (Outcome, error) { return ParseOutcome("beneficial") }, OutcomeBeneficial, false},
		{"wire-observing", "positive: the newest wire value converts", func() (Outcome, error) { return OutcomeFromWire(pb.Outcome_OUTCOME_OBSERVING) }, OutcomeObserving, false},
		{"parse-legacy-free-text", "negative: a free string from the pre-enum format is an error, not unspecified", func() (Outcome, error) { return ParseOutcome("promoted") }, OutcomeUnspecified, true},
		{"parse-wire-spelling", "negative: the proto spelling is not accepted by the domain parser", func() (Outcome, error) { return ParseOutcome("OUTCOME_BENEFICIAL") }, OutcomeUnspecified, true},
		{"parse-empty", "boundary: empty string is an error", func() (Outcome, error) { return ParseOutcome("") }, OutcomeUnspecified, true},
		{"zero-value", "boundary: the zero wire value is the explicit unspecified outcome", func() (Outcome, error) { return OutcomeFromWire(0) }, OutcomeUnspecified, false},
		{"wire-count-sentinel", "boundary: the first undefined wire value is rejected", func() (Outcome, error) { return OutcomeFromWire(pb.Outcome(outcomeCount)) }, OutcomeUnspecified, true},
		{"wire-high", "corner: a value from a newer peer is rejected with its number", func() (Outcome, error) { return OutcomeFromWire(255) }, OutcomeUnspecified, true},
		{"wire-negative", "corner: negative int32 enum values are rejected", func() (Outcome, error) { return OutcomeFromWire(-1) }, OutcomeUnspecified, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.run()
			if (err != nil) != tc.expectError || got != tc.expected {
				t.Fatalf("%s: got %s err=%v; expected %s error=%t", tc.description, got, err, tc.expected, tc.expectError)
			}
		})
	}
}

func TestOutcomeClassification(t *testing.T) {
	for _, tc := range []struct {
		description string
		outcome     Outcome
		promotable  bool
		withdraws   bool
		renews      bool
		valid       bool
		str         string
	}{
		{"positive: beneficial promotes and renews", OutcomeBeneficial, true, false, true, true, "beneficial"},
		{"positive: safety-only promotes small cohorts", OutcomeSafetyOnly, true, false, true, true, "safety_only"},
		{"positive: final safety promotes the 100% phase", OutcomeFinalSafety, true, false, true, true, "final_safety"},
		{"negative: harmful withdraws and never promotes", OutcomeHarmful, false, true, true, true, "harmful"},
		{"negative: expired withdraws", OutcomeExpired, false, true, true, true, "expired"},
		{"negative: stale evidence cannot renew a lease", OutcomeStale, false, false, false, true, "stale"},
		{"negative: missing data cannot renew a lease", OutcomeInsufficientData, false, false, false, true, "insufficient_data"},
		{"boundary: zero value is valid, inert, and renews nothing it decides", OutcomeUnspecified, false, false, true, true, "unspecified"},
		{"boundary: last defined value", OutcomeObserving, false, false, true, true, "observing"},
		{"corner: sentinel is invalid and prints its number", outcomeCount, false, false, true, false, "outcome(19)"},
		{"corner: stable regression is a review flag, not an automatic withdrawal", OutcomeStableRegressionReviewRequired, false, false, true, true, "stable_regression_review_required"},
	} {
		o := tc.outcome
		if o.Promotable() != tc.promotable || o.Withdraws() != tc.withdraws || o.RenewsLease() != tc.renews || o.Valid() != tc.valid || o.String() != tc.str {
			t.Errorf("%s: promotable=%t withdraws=%t renews=%t valid=%t str=%q", tc.description, o.Promotable(), o.Withdraws(), o.RenewsLease(), o.Valid(), o.String())
		}
	}
}
