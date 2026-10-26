package wire_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"buf.build/go/protovalidate"
	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/internal/fixtures"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The buf.validate rules on proto/xtcp_treatment/v1 mirror the bounds in
// validate.go. These rows pin that mirror: a bound changed in one place and
// not the other fails here. Contextual checks (duplicate IDs, overlapping
// destinations, cohort/baseline agreement) stay Go-only and are covered by
// validate_test.go.
//
// go test ./pkg/treatment -run TestProtoRulesMirrorValidators
func TestProtoRulesMirrorValidators(t *testing.T) {
	now := time.Unix(1700000000, 0)
	id128 := strings.Repeat("a", 128)
	experiment := func(mut func(*pb.Experiment)) proto.Message {
		e, _ := fixtures.ExperimentWire(now, treatment.DefaultPolicy())
		mut(e)
		return e
	}
	desired := func(mut func(*pb.DesiredState)) proto.Message {
		d := fixtures.DesiredWire(now)
		mut(d)
		return d
	}
	control := func(mut func(*pb.ControlRequest)) proto.Message {
		c := &pb.ControlRequest{ExperimentId: "exp", Action: pb.ControlAction_CONTROL_ACTION_PAUSE, Reason: "why", RequestId: "req-1"}
		mut(c)
		return c
	}
	tests := []struct {
		description string
		msg         proto.Message
		expected    string // substring of the violation rule id, or "" for valid
	}{
		{"positive: the desired-state fixture is valid", desired(func(*pb.DesiredState) {}), ""},
		{"positive: the experiment fixture is valid", experiment(func(*pb.Experiment) {}), ""},
		{"positive: a propose request wrapping the fixture is valid", &pb.ProposeRequest{Experiment: experiment(func(*pb.Experiment) {}).(*pb.Experiment), RequestId: "req-1"}, ""},
		{"positive: a minimal check-in is valid", &pb.CheckInRequest{AgentId: "host-1", Location: "dc1", Capabilities: &pb.Capabilities{BootId: "boot", CollectionId: fixtures.CollectionID}}, ""},
		{"positive: a control request with exactly one selector is valid", control(func(*pb.ControlRequest) {}), ""},
		{"positive: fleet control with no other selector is valid", control(func(c *pb.ControlRequest) { c.ExperimentId = ""; c.Fleet = true }), ""},

		{"negative: identifier with a leading dash", desired(func(d *pb.DesiredState) { d.AgentId = "-host" }), "string.pattern"},
		{"negative: a check-in without a collection nonce", &pb.CheckInRequest{AgentId: "host-1", Location: "dc1", Capabilities: &pb.Capabilities{BootId: "boot"}}, "string.pattern"},
		{"negative: an uppercase collection nonce", &pb.CheckInRequest{AgentId: "host-1", Location: "dc1", Capabilities: &pb.Capabilities{BootId: "boot", CollectionId: strings.ToUpper(fixtures.CollectionID)}}, "string.pattern"},
		{"negative: revision zero", desired(func(d *pb.DesiredState) { d.Revision = 0 }), "uint64.gt"},
		{"negative: missing issued_at", desired(func(d *pb.DesiredState) { d.IssuedAt = nil }), "required"},
		{"negative: empty TCP settings", desired(func(d *pb.DesiredState) { d.Assignments[0].Treatment.Settings = &pb.TCPSettings{} }), "tcp_settings.non_empty"},
		{"negative: congestion_lock without an algorithm", desired(func(d *pb.DesiredState) {
			d.Assignments[0].Treatment.Settings = &pb.TCPSettings{CongestionLock: true, QuickAck: proto.Bool(true)}
		}), "tcp_settings.lock_requires_algorithm"},
		{"negative: DCTCP without verified ECN fabric", desired(func(d *pb.DesiredState) {
			d.Assignments[0].Treatment.Settings.CongestionControl = proto.String("dctcp")
		}), "treatment.dctcp_requires_ecn"},
		{"negative: destination that is not a prefix", desired(func(d *pb.DesiredState) { d.Assignments[0].Treatment.Destination = "192.0.2.1" }), "string.ip_prefix"},
		{"negative: trial without a lease", desired(func(d *pb.DesiredState) { d.Assignments[0].LeaseExpiresAt = nil }), "assignment.trial_requires_lease"},
		{"negative: unspecified assignment mode", desired(func(d *pb.DesiredState) { d.Assignments[0].Mode = pb.AssignmentMode_ASSIGNMENT_MODE_UNSPECIFIED }), "enum"},
		{"negative: predecessor on another destination", desired(func(d *pb.DesiredState) {
			p := proto.Clone(d.Assignments[0].Treatment).(*pb.Treatment)
			p.Destination = "192.0.2.2/32"
			d.Assignments[0].Predecessor = p
		}), "assignment.predecessor_destination"},
		{"negative: evidence window inverted", experiment(func(e *pb.Experiment) {
			e.Baseline.WindowStart, e.Baseline.WindowEnd = e.Baseline.WindowEnd, e.Baseline.WindowStart
		}), "evidence.interval"},
		{"negative: evidence analyzed before its window closed", experiment(func(e *pb.Experiment) {
			e.Baseline.AnalyzedAt = timestamppb.New(e.Baseline.WindowEnd.AsTime().Add(-time.Second))
		}), "evidence.interval"},
		{"negative: artifact digest of the wrong length", experiment(func(e *pb.Experiment) { e.Baseline.ArtifactSha256 = "abc" }), "string.pattern"},
		{"negative: exposed sockets exceed sockets", experiment(func(e *pb.Experiment) { e.Baseline.Hosts[0].ExposedSockets = e.Baseline.Hosts[0].Sockets + 1 }), "host_evidence.exposed_within_sockets"},
		{"negative: observation longer than the phase", experiment(func(e *pb.Experiment) {
			e.Policy.MinimumObservation = durationpb.New(25 * time.Hour)
		}), "policy.observation_within_phase"},
		{"negative: too few cohort hosts", experiment(func(e *pb.Experiment) { e.CohortOrder = e.CohortOrder[:2] }), "repeated.min_items"},
		{"negative: duplicate cohort hosts", experiment(func(e *pb.Experiment) { e.CohortOrder[1] = e.CohortOrder[0] }), "repeated.unique"},
		{"negative: empty hypothesis", experiment(func(e *pb.Experiment) { e.Hypothesis = "" }), "string.min_len"},
		{"negative: two control selectors", control(func(c *pb.ControlRequest) { c.AgentId = "host-1" }), "control.one_selector"},
		{"negative: no control selector", control(func(c *pb.ControlRequest) { c.ExperimentId = "" }), "control.one_selector"},
		{"negative: unspecified control action", control(func(c *pb.ControlRequest) { c.Action = pb.ControlAction_CONTROL_ACTION_UNSPECIFIED }), "enum"},
		{"negative: empty control reason", control(func(c *pb.ControlRequest) { c.Reason = "" }), "string.min_len"},
		{"negative: request_id reserved suffix", control(func(c *pb.ControlRequest) { c.RequestId = "req.result" }), "control.request_id_suffix"},

		{"boundary: identifier of exactly 128 bytes", desired(func(d *pb.DesiredState) { d.AgentId = id128 }), ""},
		{"boundary: identifier of 129 bytes", desired(func(d *pb.DesiredState) { d.AgentId = id128 + "a" }), "string.pattern"},
		{"boundary: rto_min_ms at the upper bound", desired(func(d *pb.DesiredState) { d.Assignments[0].Treatment.Settings.RtoMinMs = proto.Uint32(120000) }), ""},
		{"boundary: rto_min_ms one over the upper bound", desired(func(d *pb.DesiredState) { d.Assignments[0].Treatment.Settings.RtoMinMs = proto.Uint32(120001) }), "uint32.gte_lte"},
		{"boundary: initial window at the upper bound", desired(func(d *pb.DesiredState) {
			d.Assignments[0].Treatment.Settings.InitialCongestionWindow = proto.Uint32(1024)
		}), ""},
		{"boundary: initial window of zero", desired(func(d *pb.DesiredState) {
			d.Assignments[0].Treatment.Settings.InitialCongestionWindow = proto.Uint32(0)
		}), "uint32.gte"},
		{"boundary: reason of exactly 4096 bytes", control(func(c *pb.ControlRequest) { c.Reason = strings.Repeat("r", 4096) }), ""},
		{"boundary: reason of 4097 bytes", control(func(c *pb.ControlRequest) { c.Reason = strings.Repeat("r", 4097) }), "string.max_len"},
		{"boundary: policy duration at seven days", experiment(func(e *pb.Experiment) { e.Policy.MaximumPhase = durationpb.New(7 * 24 * time.Hour) }), ""},
		{"boundary: policy duration over seven days", experiment(func(e *pb.Experiment) { e.Policy.MaximumPhase = durationpb.New(7*24*time.Hour + time.Second) }), "duration.gt_lte"},
		{"boundary: minimum_treated_hosts of one", experiment(func(e *pb.Experiment) { e.Policy.MinimumTreatedHosts = 1 }), "uint32.gte"},

		{"corner: inspect with no selectors is valid (ignore on zero value)", &pb.InspectRequest{}, ""},
		{"corner: inspect with a malformed selector is rejected", &pb.InspectRequest{AgentId: "bad id"}, "string.pattern"},
		{"corner: upper-case hex digest is rejected", experiment(func(e *pb.Experiment) { e.Baseline.ArtifactSha256 = strings.ToUpper(e.Baseline.ArtifactSha256) }), "string.pattern"},
		{"corner: a 15-byte congestion algorithm name is accepted, 16 is not", desired(func(d *pb.DesiredState) {
			d.Assignments[0].Treatment.Settings.CongestionControl = proto.String(strings.Repeat("c", 16))
		}), "string.max_len"},
		{"corner: an IPv6 host prefix is accepted", desired(func(d *pb.DesiredState) { d.Assignments[0].Treatment.Destination = "2001:db8::1/128" }), ""},
		{"corner: NaN guardrail is rejected", experiment(func(e *pb.Experiment) { e.Policy.MaximumRttIncrease = nan() }), "double.finite"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			err := protovalidate.Validate(tc.msg)
			if tc.expected == "" {
				if err != nil {
					t.Fatalf("expected valid, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected a violation matching %q", tc.expected)
			}
			var ve *protovalidate.ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("expected a ValidationError, got %T: %v", err, err)
			}
			var ids []string
			for _, v := range ve.Violations {
				ids = append(ids, v.Proto.GetRuleId())
			}
			if !slices.ContainsFunc(ids, func(id string) bool { return strings.Contains(id, tc.expected) }) {
				t.Fatalf("expected a violation of %q, got %v (%v)", tc.expected, ids, err)
			}
		})
	}
}

func nan() float64 {
	zero := 0.0
	return zero / zero
}

// Every RPC must declare a role; a forgotten annotation is refused for all
// callers, so this test turns a silent outage into a build-time failure.
//
// go test ./pkg/treatment -run TestEveryRPCDeclaresARole
func TestEveryRPCDeclaresARole(t *testing.T) {
	files := []protoreflect.FileDescriptor{pb.File_xtcp_treatment_v1_agent_proto, pb.File_xtcp_treatment_v1_operator_proto}
	want := map[protoreflect.FullName]pb.Role{
		"xtcp_treatment.v1.AgentControlService": pb.Role_ROLE_AGENT,
		"xtcp_treatment.v1.OperatorService":     pb.Role_ROLE_OPERATOR,
	}
	seen := 0
	for _, f := range files {
		services := f.Services()
		for i := 0; i < services.Len(); i++ {
			svc := services.Get(i)
			expected, ok := want[svc.FullName()]
			if !ok {
				t.Errorf("service %s has no expected role in this test; add it", svc.FullName())
				continue
			}
			methods := svc.Methods()
			for j := 0; j < methods.Len(); j++ {
				m := methods.Get(j)
				seen++
				role := proto.GetExtension(m.Options(), pb.E_Role).(pb.Role)
				if role == pb.Role_ROLE_UNSPECIFIED {
					t.Errorf("%s.%s declares no (xtcp_treatment.v1.role)", svc.FullName(), m.Name())
				}
				if role != expected {
					t.Errorf("%s.%s declares %s, expected %s", svc.FullName(), m.Name(), role, expected)
				}
			}
		}
	}
	if seen != 8 {
		t.Errorf("walked %d methods, expected 8; a service was added or removed", seen)
	}
}
