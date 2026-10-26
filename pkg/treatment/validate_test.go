package treatment_test

import (
	"fmt"
	"math"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/internal/fixtures"
)

func ptr[T any](v T) *T { return &v }

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
			if got := treatment.ValidID(tc.input); got != tc.want {
				t.Fatalf("%s: got %t want %t", tc.description, got, tc.want)
			}
		})
	}
}

func TestTreatmentValidation(t *testing.T) {
	fixed := []validationCase[*treatment.Treatment]{
		{"valid", "the supported nearby profile is accepted", func(**treatment.Treatment) {}, ""},
		{"nil", "missing treatments are rejected", func(p **treatment.Treatment) { *p = nil }, "requires"},
		{"revision", "zero revision is invalid", func(p **treatment.Treatment) { (*p).Revision = 0 }, "requires"},
		{"empty-settings", "a treatment with no parameter set is rejected", func(p **treatment.Treatment) { (*p).Settings = treatment.TCPSettings{} }, "empty"},
		{"zero-destination", "the zero prefix is not a destination", func(p **treatment.Treatment) { (*p).Destination = netip.Prefix{} }, "canonical unicast"},
	}
	cases := append(make([]validationCase[*treatment.Treatment], 0, len(fixed)+10), fixed...)
	for _, tc := range []struct {
		destination string
		valid       bool
	}{
		{"192.168.1.1/32", true}, {"2001:db8::1/128", true}, {"192.0.2.0/24", false}, {"192.0.2.1/24", false}, {"::ffff:192.0.2.1/128", false}, {"127.0.0.1/32", false}, {"::1/128", false}, {"224.0.0.1/32", false}, {"fe80::1/128", false}, {"0.0.0.0/32", false},
	} {
		want := ""
		if !tc.valid {
			want = "canonical unicast"
		}
		cases = append(cases, validationCase[*treatment.Treatment]{tc.destination, "destinations are canonical unicast host prefixes", func(p **treatment.Treatment) { (*p).Destination = netip.MustParsePrefix(tc.destination) }, want})
	}
	checkValidation(t, func() *treatment.Treatment {
		return &fixtures.Desired(time.Unix(1700000000, 0)).Assignments[0].Treatment
	}, treatment.ValidateTreatment, cases)
}

func TestTCPSettingsValidation(t *testing.T) {
	type input struct {
		s        treatment.TCPSettings
		verified bool
	}
	fixed := []validationCase[input]{
		{"cubic", "ordinary CUBIC is accepted", func(*input) {}, ""},
		{"empty", "at least one explicit metric is required", func(p *input) { p.s = treatment.TCPSettings{} }, "empty"},
		{"explicit-false", "false quickack is a real setting", func(p *input) { p.s = treatment.TCPSettings{QuickAck: ptr(false)} }, ""},
		{"lock-without-algorithm", "locking requires an algorithm", func(p *input) { p.s = treatment.TCPSettings{CongestionLock: true} }, "requires"},
		{"algorithm-max", "15 byte kernel name is accepted", func(p *input) { p.s.CongestionControl = ptr(strings.Repeat("a", 15)) }, ""},
		{"algorithm-long", "16 byte kernel name is rejected", func(p *input) { p.s.CongestionControl = ptr(strings.Repeat("a", 16)) }, "kernel name"},
		{"algorithm-empty", "explicit empty algorithm is invalid", func(p *input) { p.s.CongestionControl = ptr("") }, "invalid congestion"},
		{"dctcp-unverified", "ECN fabric verification is required", func(p *input) { p.s.CongestionControl = ptr("dctcp"); p.s.ECN = ptr(true) }, "DCTCP"},
		{"dctcp-no-ecn", "verification alone is insufficient", func(p *input) { p.s.CongestionControl = ptr("dctcp"); p.verified = true }, "DCTCP"},
		{"dctcp-ecn-false", "an explicit false ECN does not satisfy DCTCP", func(p *input) {
			p.s.CongestionControl = ptr("dctcp")
			p.s.ECN = ptr(false)
			p.verified = true
		}, "DCTCP"},
		{"dctcp-verified", "verified fabric and explicit ECN permit DCTCP", func(p *input) {
			p.s.CongestionControl = ptr("dctcp")
			p.s.ECN = ptr(true)
			p.verified = true
		}, ""},
	}
	cases := append(make([]validationCase[input], 0, len(fixed)+3*4), fixed...)
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
					p.s.RTOMinMs = ptr(value)
				case "cwnd":
					p.s.InitialCongestionWindow = ptr(value)
				case "rwnd":
					p.s.InitialReceiveWindow = ptr(value)
				}
			}, want})
		}
	}
	checkValidation(t, func() input { return input{s: treatment.TCPSettings{CongestionControl: ptr("cubic")}} }, func(p input) error { return treatment.ValidateTCPSettings(p.s, p.verified) }, cases)
}

func TestPolicyValidation(t *testing.T) {
	fixed := []validationCase[treatment.Policy]{
		{"default", "production defaults are valid", func(*treatment.Policy) {}, ""},
		{"zero", "the zero policy is rejected", func(p *treatment.Policy) { *p = treatment.Policy{} }, "duration"},
		{"observation-after-phase", "observation cannot exceed phase", func(p *treatment.Policy) { p.MaximumPhase = time.Minute }, "sample bounds"},
		{"zero-sockets", "sample count must be positive", func(p *treatment.Policy) { p.MinimumSocketsPerHost = 0 }, "sample bounds"},
		{"zero-bytes", "byte count must be positive", func(p *treatment.Policy) { p.MinimumBytesPerHost = 0 }, "sample bounds"},
	}
	cases := append(make([]validationCase[treatment.Policy], 0, len(fixed)+4+3*4+3*7), fixed...)
	for _, n := range []uint32{1, 2, treatment.MaxAgents, treatment.MaxAgents + 1} {
		want := ""
		if n < 2 || n > treatment.MaxAgents {
			want = "sample bounds"
		}
		cases = append(cases, validationCase[treatment.Policy]{fmt.Sprintf("hosts-%d", n), "treated population bounds are inclusive", func(p *treatment.Policy) { p.MinimumTreatedHosts = n }, want})
	}
	for _, field := range []string{"observation", "phase", "age"} {
		for _, tc := range []struct {
			name string
			d    time.Duration
			want string
		}{
			{"zero", 0, "duration"}, {"negative", -time.Second, "duration"}, {"maximum", 7 * 24 * time.Hour, ""}, {"over", 7*24*time.Hour + time.Nanosecond, "duration"},
		} {
			cases = append(cases, validationCase[treatment.Policy]{field + "-" + tc.name, "durations must be positive and within seven days", func(p *treatment.Policy) {
				p.MaximumPhase = 7 * 24 * time.Hour
				switch field {
				case "observation":
					p.MinimumObservation = tc.d
				case "phase":
					p.MaximumPhase = tc.d
				case "age":
					p.MaximumEvidenceAge = tc.d
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
			cases = append(cases, validationCase[treatment.Policy]{fmt.Sprintf("%s-%g", field, v), "effect bounds must be finite fractions in [0,1]", func(p *treatment.Policy) {
				switch field {
				case "effect":
					p.MinimumImprovement = v
				case "loss":
					p.MaximumRetransmissionIncrease = v
				case "rtt":
					p.MaximumRTTIncrease = v
				}
			}, want})
		}
	}
	checkValidation(t, treatment.DefaultPolicy, treatment.ValidatePolicy, cases)
}

func TestEvidenceValidation(t *testing.T) {
	now := time.Unix(1700000000, 0)
	fixed := []validationCase[*treatment.Evidence]{
		{"valid", "complete finite evidence is accepted", func(**treatment.Evidence) {}, ""},
		{"nil", "evidence is required", func(p **treatment.Evidence) { *p = nil }, "interval"},
		{"empty-window", "a zero length window is invalid", func(p **treatment.Evidence) { (*p).WindowStart = (*p).WindowEnd }, "interval"},
		{"analysis-before-end", "analysis cannot predate data", func(p **treatment.Evidence) { (*p).AnalyzedAt = (*p).WindowStart }, "interval"},
		{"missing-time", "all timestamps are required", func(p **treatment.Evidence) { (*p).WindowEnd = time.Time{} }, "interval"},
		{"analysis-equals-end", "analysis at the window end is the earliest valid instant", func(p **treatment.Evidence) { (*p).AnalyzedAt = (*p).WindowEnd }, ""},
		{"query-version", "query provenance is required", func(p **treatment.Evidence) { (*p).QueryVersion = "" }, "provenance"},
		{"long-version", "query version length is bounded", func(p **treatment.Evidence) { (*p).QueryVersion = strings.Repeat("v", 257) }, "provenance"},
		{"digest-length", "digest is exactly 64 characters", func(p **treatment.Evidence) { (*p).ArtifactSHA256 = "a" }, "provenance"},
		{"digest-hex", "digest is lower case hexadecimal", func(p **treatment.Evidence) { (*p).ArtifactSHA256 = strings.Repeat("z", 64) }, "sha256"},
		{"empty-hosts", "empty evidence is not healthy evidence", func(p **treatment.Evidence) { (*p).Hosts = nil }, "population"},
		{"zero-host", "a host without an identity is rejected", func(p **treatment.Evidence) { (*p).Hosts[0] = treatment.HostEvidence{} }, "host"},
		{"duplicate-host", "hosts are independent units", func(p **treatment.Evidence) { (*p).Hosts[1] = (*p).Hosts[0] }, "host"},
		{"overexposure", "exposed sockets cannot exceed total sockets", func(p **treatment.Evidence) { (*p).Hosts[0].ExposedSockets = 101 }, "host"},
	}
	cases := append(make([]validationCase[*treatment.Evidence], 0, len(fixed)+3*5+2), fixed...)
	for _, field := range []string{"goodput", "loss", "rtt"} {
		for _, v := range []float64{0, -1, math.NaN(), math.Inf(1), math.Inf(-1)} {
			want := ""
			if v != 0 {
				want = "measurement"
			}
			cases = append(cases, validationCase[*treatment.Evidence]{fmt.Sprintf("%s-%g", field, v), "measurements must be finite and nonnegative", func(p **treatment.Evidence) {
				h := &(*p).Hosts[0]
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
	for _, n := range []int{treatment.MaxAgents, treatment.MaxAgents + 1} {
		want := ""
		if n > treatment.MaxAgents {
			want = "population"
		}
		cases = append(cases, validationCase[*treatment.Evidence]{fmt.Sprintf("population-%d", n), "fleet population is bounded", func(p **treatment.Evidence) {
			(*p).Hosts = nil
			for i := range n {
				(*p).Hosts = append((*p).Hosts, treatment.HostEvidence{AgentID: fmt.Sprintf("host-%d", i)})
			}
		}, want})
	}
	checkValidation(t, func() *treatment.Evidence { _, v := fixtures.Experiment(now, treatment.DefaultPolicy()); return v }, treatment.ValidateEvidence, cases)
}

func TestDesiredValidation(t *testing.T) {
	now := time.Unix(1700000000, 0)
	fixed := []validationCase[*treatment.DesiredState]{
		{"valid", "leased trials are accepted", func(**treatment.DesiredState) {}, ""},
		{"nil", "desired header is required", func(p **treatment.DesiredState) { *p = nil }, "header"},
		{"empty", "empty assignment set withdraws treatments", func(p **treatment.DesiredState) { (*p).Assignments = nil }, ""},
		{"zero-revision", "revisions start at one", func(p **treatment.DesiredState) { (*p).Revision = 0 }, "header"},
		{"missing-time", "issuance time is required", func(p **treatment.DesiredState) { (*p).IssuedAt = time.Time{} }, "header"},
		{"zero-assignment", "an assignment without identity is rejected", func(p **treatment.DesiredState) { (*p).Assignments[0] = treatment.Assignment{} }, "identity"},
		{"zero-generation", "generations start at one", func(p **treatment.DesiredState) { (*p).Assignments[0].Generation = 0 }, "identity"},
		{"duplicate-id", "assignment IDs must be unique", func(p **treatment.DesiredState) {
			(*p).Assignments = append((*p).Assignments, (*p).Assignments[0])
		}, "identity"},
		{"duplicate-destination", "active routes cannot overlap", func(p **treatment.DesiredState) {
			a := (*p).Assignments[0]
			a.ExperimentID = "other"
			(*p).Assignments = append((*p).Assignments, a)
		}, "overlapping"},
		{"retired-overlap", "retired routes may overlap active routes", func(p **treatment.DesiredState) {
			a := (*p).Assignments[0]
			a.ExperimentID = "other"
			a.Mode = treatment.ModeRetired
			(*p).Assignments = append((*p).Assignments, a)
		}, ""},
		{"invalid-mode", "unknown modes are rejected", func(p **treatment.DesiredState) { (*p).Assignments[0].Mode = 99 }, "mode"},
		{"unspecified-mode", "the zero mode is not an instruction", func(p **treatment.DesiredState) { (*p).Assignments[0].Mode = treatment.ModeUnspecified }, "mode"},
		{"missing-lease", "trials require an expiry", func(p **treatment.DesiredState) { (*p).Assignments[0].LeaseExpiresAt = time.Time{} }, "expiry"},
		{"stable-no-lease", "stable routes need no lease", func(p **treatment.DesiredState) {
			a := &(*p).Assignments[0]
			a.Mode = treatment.ModeStable
			a.LeaseExpiresAt = time.Time{}
		}, ""},
		{"predecessor", "matching predecessor is accepted", func(p **treatment.DesiredState) {
			a := &(*p).Assignments[0]
			pred := a.Treatment
			a.Predecessor = &pred
		}, ""},
		{"wrong-predecessor", "predecessor must cover the same destination", func(p **treatment.DesiredState) {
			a := &(*p).Assignments[0]
			pred := a.Treatment
			pred.Destination = netip.MustParsePrefix("192.0.2.2/32")
			a.Predecessor = &pred
		}, "differs"},
	}
	cases := append(make([]validationCase[*treatment.DesiredState], 0, len(fixed)+2), fixed...)
	for _, n := range []int{treatment.MaxAssignments, treatment.MaxAssignments + 1} {
		want := ""
		if n > treatment.MaxAssignments {
			want = "header"
		}
		cases = append(cases, validationCase[*treatment.DesiredState]{fmt.Sprintf("assignments-%d", n), "assignment count is bounded", func(p **treatment.DesiredState) {
			base := (*p).Assignments[0]
			(*p).Assignments = nil
			for i := range n {
				a := base
				a.ExperimentID = fmt.Sprintf("a-%d", i)
				a.Treatment.Destination = netip.MustParsePrefix(fmt.Sprintf("198.18.%d.%d/32", i/256, i%256))
				(*p).Assignments = append((*p).Assignments, a)
			}
		}, want})
	}
	checkValidation(t, func() *treatment.DesiredState { return fixtures.Desired(now) }, treatment.ValidateDesired, cases)
}

func TestProposalValidation(t *testing.T) {
	now := time.Unix(1700000000, 0)
	checkValidation(t, func() *treatment.Experiment { e, _ := fixtures.Experiment(now, treatment.DefaultPolicy()); return e }, treatment.ValidateProposal, []validationCase[*treatment.Experiment]{
		{"valid", "complete proposal is reviewable", func(**treatment.Experiment) {}, ""},
		{"nil", "proposal is required", func(p **treatment.Experiment) { *p = nil }, "identity"},
		{"no-hypothesis", "proposal must state a hypothesis", func(p **treatment.Experiment) { (*p).Hypothesis = "" }, "identity"},
		{"long-hypothesis", "hypothesis is bounded", func(p **treatment.Experiment) { (*p).Hypothesis = strings.Repeat("x", 8193) }, "identity"},
		{"three-hosts", "one canary and two controls suffice", func(p **treatment.Experiment) { (*p).CohortOrder = (*p).CohortOrder[:3] }, ""},
		{"two-hosts", "two hosts cannot provide a canary and two controls", func(p **treatment.Experiment) { (*p).CohortOrder = (*p).CohortOrder[:2] }, "population"},
		{"duplicate", "duplicate hosts cannot inflate the cohort", func(p **treatment.Experiment) { (*p).CohortOrder[1] = (*p).CohortOrder[0] }, "cohort"},
		{"missing-baseline-host", "every cohort member needs a baseline", func(p **treatment.Experiment) { (*p).CohortOrder[0] = "absent" }, "baseline missing"},
		{"invalid-treatment", "proposal validates treatment", func(p **treatment.Experiment) { (*p).Treatment = treatment.Treatment{} }, "requires"},
		{"invalid-policy", "proposal validates policy", func(p **treatment.Experiment) { (*p).Policy = treatment.Policy{} }, "policy"},
		{"invalid-evidence", "proposal validates evidence", func(p **treatment.Experiment) { (*p).Baseline = nil }, "interval"},
	})
}

func TestReportValidation(t *testing.T) {
	now := time.Unix(1700000000, 0)
	fresh := func() *treatment.AgentReport {
		d := fixtures.Desired(now)
		return &treatment.AgentReport{AgentID: "host-1", Location: "dc1", Capabilities: treatment.Capabilities{BootID: "boot-1", CollectionID: fixtures.CollectionID, KernelRelease: "6.12", CongestionAlgorithms: []string{"cubic", "bbr"}, SimpleHostRouting: true, ActuatorEnabled: true}, DesiredRevision: 1, Records: []treatment.ApplicationRecord{{Assignment: d.Assignments[0], DesiredRevision: 1, Status: treatment.ApplyVerified, AppliedAt: now}}}
	}
	fixed := []validationCase[*treatment.AgentReport]{
		{"valid", "a verified report is accepted", func(**treatment.AgentReport) {}, ""},
		{"nil", "a report is required", func(p **treatment.AgentReport) { *p = nil }, "agent report"},
		{"no-records", "a fresh host reports nothing", func(p **treatment.AgentReport) { (*p).Records = nil }, ""},
		{"bad-boot-id", "boot IDs are identifiers", func(p **treatment.AgentReport) { (*p).Capabilities.BootID = "" }, "agent report"},
		{"missing-collection-id", "a report must carry the agent's collection nonce", func(p **treatment.AgentReport) { (*p).Capabilities.CollectionID = "" }, "agent report"},
		{"short-collection-id", "a 31-digit nonce is refused", func(p **treatment.AgentReport) { (*p).Capabilities.CollectionID = fixtures.CollectionID[1:] }, "agent report"},
		{"upper-collection-id", "the nonce is lowercase hex, so it can never carry a quote", func(p **treatment.AgentReport) { (*p).Capabilities.CollectionID = strings.ToUpper(fixtures.CollectionID) }, "agent report"},
		{"long-kernel", "kernel release is bounded", func(p **treatment.AgentReport) { (*p).Capabilities.KernelRelease = strings.Repeat("k", 257) }, "agent report"},
		{"long-algorithm", "algorithm names respect the kernel limit", func(p **treatment.AgentReport) {
			(*p).Capabilities.CongestionAlgorithms = []string{strings.Repeat("a", 16)}
		}, "algorithm"},
		{"duplicate-record", "one record per experiment", func(p **treatment.AgentReport) { (*p).Records = append((*p).Records, (*p).Records[0]) }, "application record"},
		{"long-error", "error text is bounded", func(p **treatment.AgentReport) { (*p).Records[0].Error = strings.Repeat("e", 4097) }, "application record"},
		{"unknown-status", "an apply status this build does not know is rejected", func(p **treatment.AgentReport) { (*p).Records[0].Status = 200 }, "application record"},
		{"unspecified-status", "an unacted assignment may be reported", func(p **treatment.AgentReport) { (*p).Records[0].Status = treatment.ApplyUnspecified }, ""},
		{"bad-treatment", "records carry valid treatments", func(p **treatment.AgentReport) { (*p).Records[0].Assignment.Treatment.Revision = 0 }, "requires"},
	}
	cases := append(make([]validationCase[*treatment.AgentReport], 0, len(fixed)+2), fixed...)
	for _, n := range []int{64, 65} {
		want := ""
		if n > 64 {
			want = "agent report"
		}
		cases = append(cases, validationCase[*treatment.AgentReport]{fmt.Sprintf("algorithms-%d", n), "advertised algorithm count is bounded", func(p **treatment.AgentReport) {
			(*p).Capabilities.CongestionAlgorithms = nil
			for i := range n {
				(*p).Capabilities.CongestionAlgorithms = append((*p).Capabilities.CongestionAlgorithms, fmt.Sprintf("cc%d", i))
			}
		}, want})
	}
	checkValidation(t, fresh, treatment.ValidateReport, cases)
}
