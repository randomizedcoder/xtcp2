package treatment

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/internal/fixtures"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Each row states the contract, not just whether the implementation returned.
type validationCase[T any] struct {
	name, description string
	change            func(*T)
	wantError         string // empty means acceptance; otherwise a required error substring
}

func checkValidation[T any](t *testing.T, fresh func() T, validate func(T) error, cases []validationCase[T]) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := fresh()
			tc.change(&input)
			err := validate(input)
			if tc.wantError == "" {
				if err != nil {
					t.Fatalf("%s: expected acceptance, got %v", tc.description, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("%s: expected error containing %q, got %v", tc.description, tc.wantError, err)
			}
		})
	}
}

func TestValidIDBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, description, input string
		want                     bool
	}{
		{"minimum", "one alphanumeric character is sufficient", "a", true},
		{"maximum", "128 characters are accepted", strings.Repeat("a", 128), true},
		{"too-long", "129 characters are rejected", strings.Repeat("a", 129), false},
		{"punctuation", "safe internal punctuation is accepted", "host-1.dc_2", true},
		{"empty", "empty identities are rejected", "", false},
		{"leading-dot", "identities must start with an alphanumeric", ".host", false},
		{"path", "path traversal is not an identity", "../host", false},
		{"newline", "a trailing newline is not ignored", "host\n", false},
		{"unicode", "identities are ASCII", "höst", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ValidID(tc.input); got != tc.want {
				t.Fatalf("%s: got %t want %t", tc.description, got, tc.want)
			}
		})
	}
}

func TestTreatmentValidation(t *testing.T) {
	cases := []validationCase[*pb.Treatment]{
		{"valid", "the supported nearby profile is accepted", func(**pb.Treatment) {}, ""},
		{"nil", "missing treatments are rejected", func(p **pb.Treatment) { *p = nil }, "requires"},
		{"revision", "zero revision is invalid", func(p **pb.Treatment) { (*p).Revision = 0 }, "requires"},
		{"missing-settings", "settings are required", func(p **pb.Treatment) { (*p).Settings = nil }, "requires"},
	}
	for _, tc := range []struct {
		destination string
		valid       bool
	}{
		{"192.168.1.1/32", true}, {"2001:db8::1/128", true}, {"192.0.2.0/24", false}, {"192.0.2.1/24", false}, {"::ffff:192.0.2.1/128", false}, {"127.0.0.1/32", false}, {"::1/128", false}, {"224.0.0.1/32", false}, {"fe80::1/128", false}, {"0.0.0.0/32", false}, {"invalid", false},
	} {
		want := ""
		if !tc.valid {
			want = "canonical unicast"
		}
		cases = append(cases, validationCase[*pb.Treatment]{tc.destination, "only unicast host destinations may be treated", func(p **pb.Treatment) { (*p).Destination = tc.destination }, want})
	}
	checkValidation(t, func() *pb.Treatment { return fixtures.Desired(time.Unix(1700000000, 0)).Assignments[0].Treatment }, ValidateTreatment, cases)
}

func TestTCPSettingsValidation(t *testing.T) {
	type input struct {
		s        *pb.TCPSettings
		verified bool
	}
	cases := []validationCase[input]{
		{"cubic", "ordinary CUBIC is accepted", func(*input) {}, ""},
		{"nil", "nil settings are rejected", func(p *input) { p.s = nil }, "required"},
		{"empty", "at least one explicit metric is required", func(p *input) { p.s = &pb.TCPSettings{} }, "empty"},
		{"explicit-false", "false quickack is a real setting", func(p *input) { p.s = &pb.TCPSettings{QuickAck: proto.Bool(false)} }, ""},
		{"lock-without-algorithm", "locking requires an algorithm", func(p *input) { p.s = &pb.TCPSettings{CongestionLock: true} }, "requires"},
		{"algorithm-max", "15 byte kernel name is accepted", func(p *input) { p.s.CongestionControl = proto.String(strings.Repeat("a", 15)) }, ""},
		{"algorithm-long", "16 byte kernel name is rejected", func(p *input) { p.s.CongestionControl = proto.String(strings.Repeat("a", 16)) }, "kernel name"},
		{"algorithm-empty", "explicit empty algorithm is invalid", func(p *input) { p.s.CongestionControl = proto.String("") }, "invalid congestion"},
		{"dctcp-unverified", "ECN fabric verification is required", func(p *input) { p.s.CongestionControl = proto.String("dctcp"); p.s.Ecn = proto.Bool(true) }, "DCTCP"},
		{"dctcp-no-ecn", "verification alone is insufficient", func(p *input) { p.s.CongestionControl = proto.String("dctcp"); p.verified = true }, "DCTCP"},
		{"dctcp-verified", "verified fabric and explicit ECN permit DCTCP", func(p *input) {
			p.s.CongestionControl = proto.String("dctcp")
			p.s.Ecn = proto.Bool(true)
			p.verified = true
		}, ""},
	}
	for _, metric := range []string{"rto", "cwnd", "rwnd"} {
		maximum := uint32(1024)
		errorText := "initial window"
		if metric == "rto" {
			maximum = 120000
			errorText = "rto_min"
		}
		for _, value := range []uint32{0, 1, maximum, maximum + 1} {
			want := ""
			if value == 0 || value > maximum {
				want = errorText
			}
			cases = append(cases, validationCase[input]{fmt.Sprintf("%s-%d", metric, value), "metric bounds are inclusive", func(p *input) {
				switch metric {
				case "rto":
					p.s.RtoMinMs = proto.Uint32(value)
				case "cwnd":
					p.s.InitialCongestionWindow = proto.Uint32(value)
				case "rwnd":
					p.s.InitialReceiveWindow = proto.Uint32(value)
				}
			}, want})
		}
	}
	checkValidation(t, func() input { return input{s: &pb.TCPSettings{CongestionControl: proto.String("cubic")}} }, func(p input) error { return ValidateTCPSettings(p.s, p.verified) }, cases)
}

func TestPolicyValidation(t *testing.T) {
	cases := []validationCase[*pb.ExperimentPolicy]{
		{"default", "production defaults are valid", func(**pb.ExperimentPolicy) {}, ""},
		{"nil", "policy is required", func(p **pb.ExperimentPolicy) { *p = nil }, "required"},
		{"observation-after-phase", "observation cannot exceed phase", func(p **pb.ExperimentPolicy) { (*p).MaximumPhase = durationpb.New(time.Minute) }, "sample bounds"},
		{"zero-sockets", "sample count must be positive", func(p **pb.ExperimentPolicy) { (*p).MinimumSocketsPerHost = 0 }, "sample bounds"},
		{"zero-bytes", "byte count must be positive", func(p **pb.ExperimentPolicy) { (*p).MinimumBytesPerHost = 0 }, "sample bounds"},
	}
	for _, n := range []uint32{1, 2, MaxAgents, MaxAgents + 1} {
		want := ""
		if n < 2 || n > MaxAgents {
			want = "sample bounds"
		}
		cases = append(cases, validationCase[*pb.ExperimentPolicy]{fmt.Sprintf("hosts-%d", n), "treated population bounds are inclusive", func(p **pb.ExperimentPolicy) { (*p).MinimumTreatedHosts = n }, want})
	}
	for _, field := range []string{"observation", "phase", "age"} {
		for _, tc := range []struct {
			name string
			d    *durationpb.Duration
			want string
		}{
			{"nil", nil, "duration"}, {"zero", durationpb.New(0), "duration"}, {"negative", durationpb.New(-time.Second), "duration"}, {"maximum", durationpb.New(7 * 24 * time.Hour), ""}, {"over", durationpb.New(7*24*time.Hour + time.Nanosecond), "duration"}, {"invalid", &durationpb.Duration{Nanos: 1e9}, "duration"},
		} {
			cases = append(cases, validationCase[*pb.ExperimentPolicy]{field + "-" + tc.name, "durations must be valid and within seven days", func(p **pb.ExperimentPolicy) {
				(*p).MaximumPhase = durationpb.New(7 * 24 * time.Hour)
				switch field {
				case "observation":
					(*p).MinimumObservation = tc.d
				case "phase":
					(*p).MaximumPhase = tc.d
				case "age":
					(*p).MaximumEvidenceAge = tc.d
				}
			}, tc.want})
		}
	}
	for _, field := range []string{"effect", "loss", "rtt"} {
		for _, v := range []float64{-1, 0, 1, 1.0001, math.NaN(), math.Inf(1), math.Inf(-1)} {
			want := ""
			if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 1 {
				want = "guardrail"
			}
			cases = append(cases, validationCase[*pb.ExperimentPolicy]{fmt.Sprintf("%s-%g", field, v), "effect bounds must be finite fractions in [0,1]", func(p **pb.ExperimentPolicy) {
				switch field {
				case "effect":
					(*p).MinimumImprovement = v
				case "loss":
					(*p).MaximumRetransmissionIncrease = v
				case "rtt":
					(*p).MaximumRttIncrease = v
				}
			}, want})
		}
	}
	checkValidation(t, DefaultPolicy, ValidatePolicy, cases)
}

func TestEvidenceValidation(t *testing.T) {
	now := time.Unix(1700000000, 0)
	cases := []validationCase[*pb.Evidence]{
		{"valid", "complete finite evidence is accepted", func(**pb.Evidence) {}, ""},
		{"nil", "evidence is required", func(p **pb.Evidence) { *p = nil }, "interval"},
		{"empty-window", "a zero length window is invalid", func(p **pb.Evidence) { (*p).WindowStart = (*p).WindowEnd }, "interval"},
		{"analysis-before-end", "analysis cannot predate data", func(p **pb.Evidence) { (*p).AnalyzedAt = (*p).WindowStart }, "interval"},
		{"missing-time", "all timestamps are required", func(p **pb.Evidence) { (*p).WindowEnd = nil }, "interval"},
		{"invalid-time", "malformed protobuf timestamps fail", func(p **pb.Evidence) { (*p).WindowEnd = &timestamppb.Timestamp{Nanos: 1e9} }, "interval"},
		{"query-version", "query provenance is required", func(p **pb.Evidence) { (*p).QueryVersion = "" }, "provenance"},
		{"long-version", "query version length is bounded", func(p **pb.Evidence) { (*p).QueryVersion = strings.Repeat("v", 257) }, "provenance"},
		{"digest-length", "digest is exactly 64 characters", func(p **pb.Evidence) { (*p).ArtifactSha256 = "a" }, "provenance"},
		{"digest-hex", "digest is lower case hexadecimal", func(p **pb.Evidence) { (*p).ArtifactSha256 = strings.Repeat("z", 64) }, "sha256"},
		{"empty-hosts", "empty evidence is not healthy evidence", func(p **pb.Evidence) { (*p).Hosts = nil }, "population"},
		{"nil-host", "nil host entries are rejected", func(p **pb.Evidence) { (*p).Hosts[0] = nil }, "host"},
		{"duplicate-host", "hosts are independent units", func(p **pb.Evidence) { (*p).Hosts[1] = (*p).Hosts[0] }, "host"},
		{"overexposure", "exposed sockets cannot exceed total sockets", func(p **pb.Evidence) { (*p).Hosts[0].ExposedSockets = 101 }, "host"},
	}
	for _, field := range []string{"goodput", "loss", "rtt"} {
		for _, v := range []float64{0, -1, math.NaN(), math.Inf(1), math.Inf(-1)} {
			want := ""
			if v != 0 {
				want = "measurement"
			}
			cases = append(cases, validationCase[*pb.Evidence]{fmt.Sprintf("%s-%g", field, v), "measurements must be finite and nonnegative", func(p **pb.Evidence) {
				h := (*p).Hosts[0]
				switch field {
				case "goodput":
					h.GoodputBps = v
				case "loss":
					h.RetransmissionRatio = v
				case "rtt":
					h.RttP95Ms = v
				}
			}, want})
		}
	}
	for _, n := range []int{MaxAgents, MaxAgents + 1} {
		want := ""
		if n > MaxAgents {
			want = "population"
		}
		cases = append(cases, validationCase[*pb.Evidence]{fmt.Sprintf("population-%d", n), "fleet population is bounded", func(p **pb.Evidence) {
			(*p).Hosts = nil
			for i := range n {
				(*p).Hosts = append((*p).Hosts, &pb.HostEvidence{AgentId: fmt.Sprintf("host-%d", i)})
			}
		}, want})
	}
	checkValidation(t, func() *pb.Evidence { _, v := fixtures.Experiment(now, DefaultPolicy()); return v }, ValidateEvidence, cases)
}

func TestDesiredValidation(t *testing.T) {
	now := time.Unix(1700000000, 0)
	cases := []validationCase[*pb.DesiredState]{
		{"valid", "leased trials are accepted", func(**pb.DesiredState) {}, ""},
		{"nil", "desired header is required", func(p **pb.DesiredState) { *p = nil }, "header"},
		{"empty", "empty assignment set withdraws treatments", func(p **pb.DesiredState) { (*p).Assignments = nil }, ""},
		{"zero-revision", "revisions start at one", func(p **pb.DesiredState) { (*p).Revision = 0 }, "header"},
		{"missing-time", "issuance time is required", func(p **pb.DesiredState) { (*p).IssuedAt = nil }, "header"},
		{"nil-assignment", "assignment cannot be nil", func(p **pb.DesiredState) { (*p).Assignments[0] = nil }, "identity"},
		{"zero-generation", "generations start at one", func(p **pb.DesiredState) { (*p).Assignments[0].Generation = 0 }, "identity"},
		{"duplicate-id", "assignment IDs must be unique", func(p **pb.DesiredState) {
			(*p).Assignments = append((*p).Assignments, proto.Clone((*p).Assignments[0]).(*pb.Assignment))
		}, "identity"},
		{"duplicate-destination", "active routes cannot overlap", func(p **pb.DesiredState) {
			a := proto.Clone((*p).Assignments[0]).(*pb.Assignment)
			a.Id = "other"
			(*p).Assignments = append((*p).Assignments, a)
		}, "overlapping"},
		{"retired-overlap", "retired routes may overlap active routes", func(p **pb.DesiredState) {
			a := proto.Clone((*p).Assignments[0]).(*pb.Assignment)
			a.Id = "other"
			a.Mode = pb.AssignmentMode_ASSIGNMENT_MODE_RETIRED
			(*p).Assignments = append((*p).Assignments, a)
		}, ""},
		{"invalid-mode", "unknown modes are rejected", func(p **pb.DesiredState) { (*p).Assignments[0].Mode = 99 }, "mode"},
		{"missing-lease", "trials require an expiry", func(p **pb.DesiredState) { (*p).Assignments[0].LeaseExpiresAt = nil }, "expiry"},
		{"stable-no-lease", "stable routes need no lease", func(p **pb.DesiredState) {
			a := (*p).Assignments[0]
			a.Mode = pb.AssignmentMode_ASSIGNMENT_MODE_STABLE
			a.LeaseExpiresAt = nil
		}, ""},
		{"predecessor", "matching predecessor is accepted", func(p **pb.DesiredState) {
			a := (*p).Assignments[0]
			a.Predecessor = proto.Clone(a.Treatment).(*pb.Treatment)
		}, ""},
		{"wrong-predecessor", "predecessor must cover the same destination", func(p **pb.DesiredState) {
			a := (*p).Assignments[0]
			a.Predecessor = proto.Clone(a.Treatment).(*pb.Treatment)
			a.Predecessor.Destination = "192.0.2.2/32"
		}, "differs"},
	}
	for _, n := range []int{MaxAssignments, MaxAssignments + 1} {
		want := ""
		if n > MaxAssignments {
			want = "header"
		}
		cases = append(cases, validationCase[*pb.DesiredState]{fmt.Sprintf("assignments-%d", n), "assignment count is bounded", func(p **pb.DesiredState) {
			base := (*p).Assignments[0]
			(*p).Assignments = nil
			for i := range n {
				a := proto.Clone(base).(*pb.Assignment)
				a.Id = fmt.Sprintf("a-%d", i)
				a.Treatment.Destination = fmt.Sprintf("198.18.%d.%d/32", i/256, i%256)
				(*p).Assignments = append((*p).Assignments, a)
			}
		}, want})
	}
	checkValidation(t, func() *pb.DesiredState { return fixtures.Desired(now) }, ValidateDesired, cases)
}

func TestProposalValidation(t *testing.T) {
	now := time.Unix(1700000000, 0)
	checkValidation(t, func() *pb.Experiment { e, _ := fixtures.Experiment(now, DefaultPolicy()); return e }, ValidateProposal, []validationCase[*pb.Experiment]{
		{"valid", "complete proposal is reviewable", func(**pb.Experiment) {}, ""},
		{"nil", "proposal is required", func(p **pb.Experiment) { *p = nil }, "identity"},
		{"no-hypothesis", "proposal must state a hypothesis", func(p **pb.Experiment) { (*p).Hypothesis = "" }, "identity"},
		{"long-hypothesis", "hypothesis is bounded", func(p **pb.Experiment) { (*p).Hypothesis = strings.Repeat("x", 8193) }, "identity"},
		{"three-hosts", "one canary and two controls suffice", func(p **pb.Experiment) { (*p).CohortOrder = (*p).CohortOrder[:3] }, ""},
		{"two-hosts", "two hosts cannot provide a canary and two controls", func(p **pb.Experiment) { (*p).CohortOrder = (*p).CohortOrder[:2] }, "population"},
		{"duplicate", "duplicate hosts cannot inflate the cohort", func(p **pb.Experiment) { (*p).CohortOrder[1] = (*p).CohortOrder[0] }, "cohort"},
		{"missing-baseline-host", "every cohort member needs a baseline", func(p **pb.Experiment) { (*p).CohortOrder[0] = "absent" }, "baseline missing"},
		{"invalid-treatment", "proposal validates treatment", func(p **pb.Experiment) { (*p).Treatment = nil }, "requires"},
		{"invalid-policy", "proposal validates policy", func(p **pb.Experiment) { (*p).Policy = nil }, "policy"},
		{"invalid-evidence", "proposal validates evidence", func(p **pb.Experiment) { (*p).Baseline = nil }, "interval"},
	})
}
