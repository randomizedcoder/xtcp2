package main

import (
	"strings"
	"testing"
)

// ───────────────────────────────────────────────────────────────────────
// severityOrder: cover all 4 buckets including the default (return 3)
// ───────────────────────────────────────────────────────────────────────

func TestSeverityOrder_allBuckets(t *testing.T) {
	cases := map[string]int{
		"high":              0,
		severityError:       0,
		"HIGH":              0,
		"medium":            1,
		severityWarning:     1,
		"low":               2,
		severityInfo:        2,
		"unknown-severity":  3,
		"":                  3,
	}
	for s, want := range cases {
		if got := severityOrder(s); got != want {
			t.Errorf("severityOrder(%q) = %d, want %d", s, got, want)
		}
	}
}

// ───────────────────────────────────────────────────────────────────────
// synthRecommendations: exercise every recommendation branch
// ───────────────────────────────────────────────────────────────────────

func TestSynthRecommendations_allBranches(t *testing.T) {
	r := renderInput{
		reportInput: reportInput{
			Findings: []Finding{
				{Tool: "golangci-lint", Rule: "govet", File: "a.go", Tier: 0, Severity: severityError},
				{Tool: "golangci-lint", Rule: "errcheck", File: "a.go", Tier: 0},
			},
			Exclusions: []ConfigExclusion{
				{Rule: "no-justification", Justified: false},
				{Rule: "with-justification", Justified: true},
			},
			GofmtFiles: []string{"a.go"},
		},
		TotalFindings:     2,
		HasErrSeverity:    true,
		PreexistTestFails: 3,
		ByLinter: []linterAgg{
			{Tool: "golangci-lint", Linter: "govet", Count: 5},
		},
		QuickFixable: quickFixableCounts{T0: 4},
		Hotspots: []fileAgg{
			{File: "hot.go", Count: 7, Top: []string{"govet"}},
		},
	}
	recs := synthRecommendations(r)
	if len(recs) < 4 {
		t.Errorf("expected ≥4 recommendations, got %d: %v", len(recs), recs)
	}

	// Verify specific recommendations appear
	joined := strings.Join(recs, "\n")
	for _, want := range []string{"error-severity", "Top contributor", "quick-fixable", "Hotspot", "exclusion", "pre-existing", "Format files"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected recommendation containing %q in:\n%s", want, joined)
		}
	}
}

func TestSynthRecommendations_clean(t *testing.T) {
	// Empty renderInput → falls through to "No specific recommendations".
	r := renderInput{}
	recs := synthRecommendations(r)
	if len(recs) != 1 {
		t.Errorf("clean: expected 1 recommendation; got %d: %v", len(recs), recs)
	}
	if !strings.Contains(recs[0], "No specific") {
		t.Errorf("clean recommendation = %q, want 'No specific...'", recs[0])
	}
}

// ───────────────────────────────────────────────────────────────────────
// emit: populated input exercises more of the template branches
// (Hotspots, Gosec findings, FailingTests, Audits)
// ───────────────────────────────────────────────────────────────────────

func TestEmit_richInput(t *testing.T) {
	in := reportInput{
		Versions: map[string]string{"go": "go1.25"},
		Findings: []Finding{
			{Tool: "golangci-lint", Rule: "govet", File: "a.go", Line: 1, Severity: severityWarning, Tier: 0},
			{Tool: "netlink-audit", Rule: "unguarded-index", File: "b.go", Line: 2, Tier: 1},
			{Tool: "iouring-audit", Rule: "sqe-leak", File: "c.go", Line: 3, Tier: 1},
			{Tool: toolGosec, Rule: "G103", File: "d.go", Line: 4, Severity: "high"},
			{Tool: toolGosec, Rule: "G104", File: "d.go", Line: 5, Severity: "low"},
		},
		Tests: []TestResult{
			{Package: "pkg/x", Test: "TestA", Action: testActionPass},
			{Package: "pkg/x", Test: "TestB", Action: testActionFail, Preexist: false},
			{Package: "pkg/x", Test: "TestC", Action: testActionFail, Preexist: true},
			{Package: "pkg/x", Test: "TestD", Action: testActionSkip},
		},
		Coverage: Coverage{
			Total:      88.0,
			PerPackage: map[string]float64{"pkg/xtcp": 17.0, "pkg/xtcpnl": 92.0},
			Available:  true,
		},
	}
	var sb strings.Builder
	if err := emit(&sb, in); err != nil {
		t.Fatalf("emit: %v", err)
	}
	out := sb.String()
	for _, want := range []string{"## 1. Executive summary", "## 13. Test coverage", "pkg/xtcp", "G103", "netlink-audit", "iouring-audit"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output", want)
		}
	}
}
