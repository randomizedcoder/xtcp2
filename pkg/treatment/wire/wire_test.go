package wire_test

import (
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/internal/fixtures"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/wire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func ptr[T any](v T) *T { return &v }

// TestEnumsMirrorWire pins the one-to-one correspondence between each domain
// enumeration and its protobuf counterpart that the checked casts rely on.
func TestEnumsMirrorWire(t *testing.T) {
	statuses, modes, applies, outcomes := treatment.EnumCounts()
	for _, tc := range []struct {
		description string
		domain      int
		wire        map[int32]string
		prefix      string
		name        func(int32) string
	}{
		{"experiment status", statuses, pb.ExperimentStatus_name, "EXPERIMENT_STATUS_", func(i int32) string { return treatment.Status(i).String() }},
		{"assignment mode", modes, pb.AssignmentMode_name, "ASSIGNMENT_MODE_", func(i int32) string { return treatment.AssignmentMode(i).String() }},
		{"apply status", applies, pb.ApplyStatus_name, "APPLY_STATUS_", func(i int32) string { return treatment.ApplyStatus(i).String() }},
		{"outcome", outcomes, pb.Outcome_name, "OUTCOME_", func(i int32) string { return treatment.Outcome(i).String() }},
	} {
		if len(tc.wire) != tc.domain {
			t.Errorf("%s: proto defines %d values, Go defines %d", tc.description, len(tc.wire), tc.domain)
			continue
		}
		for i := range int32(tc.domain) {
			want := tc.prefix + strings.ToUpper(tc.name(i))
			if got := tc.wire[i]; got != want {
				t.Errorf("%s %d: domain name %q mirrors wire %q", tc.description, i, tc.name(i), got)
			}
		}
	}
}

func TestEnumParsers(t *testing.T) {
	statuses, modes, applies, outcomes := treatment.EnumCounts()
	for _, tc := range []struct {
		name, description string
		run               func() (any, error)
		expected          any
		expectError       bool
	}{
		{"status-running", "positive: a defined status converts", func() (any, error) { return wire.ParseStatus(pb.ExperimentStatus_EXPERIMENT_STATUS_RUNNING) }, treatment.StatusRunning, false},
		{"mode-trial", "positive: a defined mode converts", func() (any, error) { return wire.ParseMode(pb.AssignmentMode_ASSIGNMENT_MODE_TRIAL) }, treatment.ModeTrial, false},
		{"apply-failed", "positive: the last apply status converts", func() (any, error) { return wire.ParseApplyStatus(pb.ApplyStatus_APPLY_STATUS_FAILED) }, treatment.ApplyFailed, false},
		{"outcome-observing", "positive: the newest outcome converts", func() (any, error) { return wire.ParseOutcome(pb.Outcome_OUTCOME_OBSERVING) }, treatment.OutcomeObserving, false},
		{"status-zero", "boundary: the zero status is the explicit unspecified value", func() (any, error) { return wire.ParseStatus(0) }, treatment.StatusUnspecified, false},
		{"status-sentinel", "boundary: the first undefined status is rejected", func() (any, error) { return wire.ParseStatus(pb.ExperimentStatus(statuses)) }, treatment.Status(0), true},
		{"mode-sentinel", "boundary: the first undefined mode is rejected", func() (any, error) { return wire.ParseMode(pb.AssignmentMode(modes)) }, treatment.AssignmentMode(0), true},
		{"apply-sentinel", "boundary: the first undefined apply status is rejected", func() (any, error) { return wire.ParseApplyStatus(pb.ApplyStatus(applies)) }, treatment.ApplyStatus(0), true},
		{"outcome-sentinel", "boundary: the first undefined outcome is rejected", func() (any, error) { return wire.ParseOutcome(pb.Outcome(outcomes)) }, treatment.Outcome(0), true},
		{"outcome-high", "corner: a value from a newer peer is rejected with its number", func() (any, error) { return wire.ParseOutcome(255) }, treatment.Outcome(0), true},
		{"outcome-negative", "corner: negative int32 enum values are rejected", func() (any, error) { return wire.ParseOutcome(-1) }, treatment.Outcome(0), true},
		{"status-negative", "corner: negative status values are rejected", func() (any, error) { return wire.ParseStatus(-1) }, treatment.Status(0), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.run()
			if (err != nil) != tc.expectError || got != tc.expected {
				t.Fatalf("%s: got %v err=%v; expected %v error=%t", tc.description, got, err, tc.expected, tc.expectError)
			}
		})
	}
}

func TestDomainEnumParsers(t *testing.T) {
	for _, tc := range []struct {
		name, description string
		run               func() (any, error)
		expected          any
		expectError       bool
	}{
		{"status", "positive: snake_case status parses", func() (any, error) { return treatment.ParseStatus("rolled_back") }, treatment.StatusRolledBack, false},
		{"mode", "positive: snake_case mode parses", func() (any, error) { return treatment.ParseAssignmentMode("stable") }, treatment.ModeStable, false},
		{"apply", "positive: snake_case apply status parses", func() (any, error) { return treatment.ParseApplyStatus("verified") }, treatment.ApplyVerified, false},
		{"outcome", "positive: snake_case outcome parses", func() (any, error) { return treatment.ParseOutcome("beneficial") }, treatment.OutcomeBeneficial, false},
		{"legacy-free-text", "negative: a free string from the pre-enum format is an error, not unspecified", func() (any, error) { return treatment.ParseOutcome("promoted") }, treatment.OutcomeUnspecified, true},
		{"wire-spelling", "negative: the proto spelling is not accepted by the domain parser", func() (any, error) { return treatment.ParseOutcome("OUTCOME_BENEFICIAL") }, treatment.OutcomeUnspecified, true},
		{"empty", "boundary: empty string is an error", func() (any, error) { return treatment.ParseStatus("") }, treatment.StatusUnspecified, true},
		{"unspecified-name", "corner: the unspecified name parses to the zero value", func() (any, error) { return treatment.ParseAssignmentMode("unspecified") }, treatment.ModeUnspecified, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.run()
			if (err != nil) != tc.expectError || got != tc.expected {
				t.Fatalf("%s: got %v err=%v; expected %v error=%t", tc.description, got, err, tc.expected, tc.expectError)
			}
		})
	}
}

func TestOutcomeClassification(t *testing.T) {
	_, _, _, outcomes := treatment.EnumCounts()
	for _, tc := range []struct {
		description string
		outcome     treatment.Outcome
		promotable  bool
		withdraws   bool
		renews      bool
		valid       bool
		str         string
	}{
		{"positive: beneficial promotes and renews", treatment.OutcomeBeneficial, true, false, true, true, "beneficial"},
		{"positive: safety-only promotes small cohorts", treatment.OutcomeSafetyOnly, true, false, true, true, "safety_only"},
		{"positive: final safety promotes the 100% phase", treatment.OutcomeFinalSafety, true, false, true, true, "final_safety"},
		{"negative: harmful withdraws and never promotes", treatment.OutcomeHarmful, false, true, true, true, "harmful"},
		{"negative: expired withdraws", treatment.OutcomeExpired, false, true, true, true, "expired"},
		{"negative: stale evidence cannot renew a lease", treatment.OutcomeStale, false, false, false, true, "stale"},
		{"negative: missing data cannot renew a lease", treatment.OutcomeInsufficientData, false, false, false, true, "insufficient_data"},
		{"boundary: zero value is valid, inert, and renews nothing it decides", treatment.OutcomeUnspecified, false, false, true, true, "unspecified"},
		{"boundary: last defined value", treatment.OutcomeObserving, false, false, true, true, "observing"},
		{"corner: sentinel is invalid and prints its number", treatment.Outcome(outcomes), false, false, true, false, "outcome(19)"},
		{"corner: stable regression is a review flag, not an automatic withdrawal", treatment.OutcomeStableRegressionReviewRequired, false, false, true, true, "stable_regression_review_required"},
	} {
		o := tc.outcome
		if o.Promotable() != tc.promotable || o.Withdraws() != tc.withdraws || o.RenewsLease() != tc.renews || o.Valid() != tc.valid || o.String() != tc.str {
			t.Errorf("%s: promotable=%t withdraws=%t renews=%t valid=%t str=%q", tc.description, o.Promotable(), o.Withdraws(), o.RenewsLease(), o.Valid(), o.String())
		}
	}
}

func populatedExperiment(now time.Time) *treatment.Experiment {
	e, v := fixtures.Experiment(now, treatment.DefaultPolicy())
	e.Treatment.Settings = treatment.TCPSettings{CongestionControl: ptr("dctcp"), CongestionLock: true, RTOMinMs: ptr[uint32](20), InitialCongestionWindow: ptr[uint32](10), InitialReceiveWindow: ptr[uint32](0), QuickAck: ptr(false), ECN: ptr(true)}
	e.Treatment.ECNFabricVerified = true
	e.ProposedAt = now.Add(-3 * time.Hour)
	e.UpdatedAt = now
	e.LatestEvidence = v
	e.LatestOutcome = treatment.OutcomeBeneficial
	e.Phases[0].Evidence = v.Clone()
	e.Phases[0].Outcome = treatment.OutcomeBeneficial
	e.Phases[0].EvaluatedAt = now
	e.Phases[0].Reviewer = "op"
	e.Phases[0].Reason = "ok"
	e.Phases = append(e.Phases, treatment.Phase{TargetHosts: 8, IssuedAt: now})
	e.Baseline.EnrichmentVersion = "xtcp-schema-2"
	e.Baseline.Hosts[0].CollectionHealthy = false
	return e
}

func populatedReport(now time.Time) *treatment.AgentReport {
	d := fixtures.Desired(now)
	pred := d.Assignments[0].Treatment
	pred.ID = "old"
	d.Assignments[0].Predecessor = &pred
	return &treatment.AgentReport{AgentID: "host-1", Location: "dc1", Capabilities: treatment.Capabilities{BootID: "b", CollectionID: fixtures.CollectionID, KernelRelease: "6.12", CongestionAlgorithms: []string{"cubic", "bbr"}, SimpleHostRouting: true, ActuatorEnabled: true}, DesiredRevision: 7, LocallySuspended: true, Records: []treatment.ApplicationRecord{{Assignment: d.Assignments[0], DesiredRevision: 7, Status: treatment.ApplyVerified, ReceivedAt: now, AttemptedAt: now.Add(time.Second), AppliedAt: now.Add(2 * time.Second), VerifiedAt: now.Add(3 * time.Second), ExpiredLatched: false}, {Assignment: treatment.Assignment{ExperimentID: "gone", Treatment: fixtures.Treatment(), Mode: treatment.ModeRetired, Generation: 3}, Status: treatment.ApplyRemoved, RemovedAt: now, Error: "x", ExpiredLatched: true}}}
}

// TestRoundTrip converts domain → wire → domain and requires deep equality,
// and wire → domain → wire and requires proto equality, so neither direction
// drops or smears a field.
func TestRoundTrip(t *testing.T) {
	now := time.Unix(1700000000, 123456789).UTC()
	for _, tc := range []struct {
		name, description string
		run               func(t *testing.T)
	}{
		{"experiment", "positive: a populated experiment with phases, evidence and every optional setting survives", func(t *testing.T) {
			in := populatedExperiment(now)
			w := wire.Experiment(in)
			out, err := wire.ParseExperiment(w)
			if err != nil || !reflect.DeepEqual(in, out) {
				t.Fatalf("round trip differs: %v\nin  %+v\nout %+v", err, in, out)
			}
			if again := wire.Experiment(out); !proto.Equal(w, again) {
				t.Fatal("wire form differs after a second trip")
			}
		}},
		{"report", "positive: a check-in with records, predecessor and every timestamp survives", func(t *testing.T) {
			in := populatedReport(now)
			w := wire.Report(in)
			out, err := wire.ParseReport(w)
			if err != nil || !reflect.DeepEqual(in, out) {
				t.Fatalf("round trip differs: %v\nin  %+v\nout %+v", err, in, out)
			}
			if again := wire.Report(out); !proto.Equal(w, again) {
				t.Fatal("wire form differs after a second trip")
			}
		}},
		{"desired", "positive: desired state survives", func(t *testing.T) {
			in := fixtures.Desired(now)
			out, err := wire.ParseDesired(wire.Desired(in))
			if err != nil || !reflect.DeepEqual(in, out) {
				t.Fatalf("round trip differs: %v", err)
			}
		}},
		{"unset-optional", "boundary: nil optional settings and zero timestamps stay unset rather than becoming zero values", func(t *testing.T) {
			in := fixtures.Desired(now)
			in.Assignments[0].Mode = treatment.ModeStable
			in.Assignments[0].LeaseExpiresAt = time.Time{}
			w := wire.Desired(in)
			if w.Assignments[0].LeaseExpiresAt != nil {
				t.Fatal("zero lease became a timestamp")
			}
			s := w.Assignments[0].Treatment.Settings
			if s.RtoMinMs != nil || s.InitialCongestionWindow != nil || s.InitialReceiveWindow != nil || s.QuickAck != nil || s.Ecn != nil {
				t.Fatal("unset settings became zero values on the wire")
			}
			out, err := wire.ParseDesired(w)
			if err != nil || !reflect.DeepEqual(in, out) {
				t.Fatalf("round trip differs: %v", err)
			}
		}},
		{"explicit-zero", "boundary: an explicit zero window and false flag are preserved as set", func(t *testing.T) {
			s := treatment.TCPSettings{InitialReceiveWindow: ptr[uint32](0), QuickAck: ptr(false)}
			out := wire.ParseSettings(wire.Settings(s))
			if out.InitialReceiveWindow == nil || *out.InitialReceiveWindow != 0 || out.QuickAck == nil || *out.QuickAck {
				t.Fatalf("explicit zeros lost: %+v", out)
			}
		}},
		{"empty-collections", "boundary: an experiment with no phases, cohort or evidence survives", func(t *testing.T) {
			in := &treatment.Experiment{ID: "e", Location: "dc1", Treatment: fixtures.Treatment(), Policy: treatment.DefaultPolicy(), Status: treatment.StatusProposed, Revision: 1}
			out, err := wire.ParseExperiment(wire.Experiment(in))
			if err != nil || !reflect.DeepEqual(in, out) {
				t.Fatalf("round trip differs: %v\nin  %+v\nout %+v", err, in, out)
			}
		}},
		{"alias-free", "corner: parsed optional settings do not alias the message", func(t *testing.T) {
			w := wire.Settings(treatment.TCPSettings{RTOMinMs: ptr[uint32](5)})
			out := wire.ParseSettings(w)
			*w.RtoMinMs = 9
			if *out.RTOMinMs != 5 {
				t.Fatal("domain value aliases the message")
			}
		}},
		{"bad-destination", "corner: an unparsable destination becomes the zero prefix and fails validation, not decoding", func(t *testing.T) {
			tr := wire.ParseTreatment(&pb.Treatment{Id: "t", Revision: 1, Destination: "nonsense", Settings: &pb.TCPSettings{CongestionControl: ptr("cubic")}})
			if tr.Destination != (netip.Prefix{}) {
				t.Fatal("destination should be unset")
			}
			if err := treatment.ValidateTreatment(&tr); err == nil || !strings.Contains(err.Error(), "canonical unicast") {
				t.Fatalf("expected destination error, got %v", err)
			}
		}},
	} {
		t.Run(tc.name, tc.run)
	}
}

func TestParseRejections(t *testing.T) {
	now := time.Unix(1700000000, 0)
	e, _ := fixtures.ExperimentWire(now, treatment.DefaultPolicy())
	for _, tc := range []struct {
		name, description string
		run               func() error
		expected          string
	}{
		{"invalid-timestamp", "negative: a malformed timestamp is a decoding error", func() error {
			m := proto.Clone(e).(*pb.Experiment)
			m.ProposedAt = &timestamppb.Timestamp{Nanos: 1e9}
			_, err := wire.ParseExperiment(m)
			return err
		}, "proposed_at"},
		{"invalid-phase-outcome", "negative: an unknown phase outcome is rejected", func() error {
			m := proto.Clone(e).(*pb.Experiment)
			m.Phases[0].Outcome = 250
			_, err := wire.ParseExperiment(m)
			return err
		}, "unknown wire outcome 250"},
		{"unknown-mode", "negative: an unknown assignment mode is rejected", func() error {
			d := fixtures.DesiredWire(now)
			d.Assignments[0].Mode = 77
			_, err := wire.ParseDesired(d)
			return err
		}, "unknown wire assignment mode 77"},
		{"kernel-bytes", "negative: a record carrying kernel route bytes is rejected", func() error {
			r := &pb.CheckInRequest{AgentId: "a", Location: "l", Records: []*pb.ApplicationRecord{{Assignment: fixtures.DesiredWire(now).Assignments[0], InstalledRoute: []byte{1}}}}
			_, err := wire.ParseReport(r)
			return err
		}, "local kernel state"},
		{"nil-experiment", "boundary: nil is a decoding error", func() error { _, err := wire.ParseExperiment(nil); return err }, "missing experiment"},
		{"nil-policy", "corner: a missing policy decodes to the zero policy and is left to validation", func() error {
			m := proto.Clone(e).(*pb.Experiment)
			m.Policy = nil
			out, err := wire.ParseExperiment(m)
			if err != nil {
				return err
			}
			return treatment.ValidatePolicy(out.Policy)
		}, "policy duration"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run()
			if err == nil || !strings.Contains(err.Error(), tc.expected) {
				t.Fatalf("%s: expected error containing %q, got %v", tc.description, tc.expected, err)
			}
		})
	}
}

func TestWireValidatorsMatchDomain(t *testing.T) {
	now := time.Unix(1700000000, 0)
	for _, tc := range []struct {
		name, description string
		err               error
		expected          string
	}{
		{"desired-valid", "positive: the fixture passes the wire validator", wire.ValidateDesired(fixtures.DesiredWire(now)), ""},
		{"treatment-valid", "positive: the fixture treatment passes", wire.ValidateTreatment(fixtures.DesiredWire(now).Assignments[0].Treatment), ""},
		{"evidence-valid", "positive: fixture evidence passes", func() error {
			_, v := fixtures.ExperimentWire(now, treatment.DefaultPolicy())
			return wire.ValidateEvidence(v)
		}(), ""},
		{"policy-valid", "positive: the default policy passes", wire.ValidatePolicy(wire.Policy(treatment.DefaultPolicy())), ""},
		{"treatment-nil-settings", "negative: nil settings report the same message as the old validator", wire.ValidateTreatment(&pb.Treatment{Id: "t", Revision: 1, Destination: "192.0.2.1/32"}), "requires"},
		{"desired-nil-assignment", "negative: a nil assignment is rejected before conversion", wire.ValidateDesired(&pb.DesiredState{AgentId: "a", Revision: 1, IssuedAt: timestamppb.Now(), Assignments: []*pb.Assignment{nil}}), "requires"},
		{"policy-nil", "boundary: nil policy is rejected", wire.ValidatePolicy(nil), "policy required"},
		{"evidence-nil", "boundary: nil evidence is rejected", wire.ValidateEvidence(nil), "interval"},
		{"desired-bad-lease", "corner: an invalid lease timestamp is a decoding error", func() error {
			d := fixtures.DesiredWire(now)
			d.Assignments[0].LeaseExpiresAt = &timestamppb.Timestamp{Nanos: -1}
			return wire.ValidateDesired(d)
		}(), "lease_expires_at"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.expected == "" {
				if tc.err != nil {
					t.Fatalf("%s: expected acceptance, got %v", tc.description, tc.err)
				}
			} else if tc.err == nil || !strings.Contains(tc.err.Error(), tc.expected) {
				t.Fatalf("%s: expected error containing %q, got %v", tc.description, tc.expected, tc.err)
			}
		})
	}
}
