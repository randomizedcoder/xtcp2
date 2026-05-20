package main

import (
	"sync"
	"testing"
)

// emit_helpers_test.go covers the eight helpers extracted from emit in
// the gocyclo-27 → 1 refactor. Each helper has a positive / negative /
// boundary / corner / adversarial table where the behavior is
// meaningfully bounded, plus race + benchmarks.

// ───────────────────────────────────────────────────────────────────────
// bumpTierCount
// ───────────────────────────────────────────────────────────────────────

func TestBumpTierCount_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		category string
		f        Finding
		wantT0   int
		wantT1   int
		wantT2   int
		wantNT   int
	}{
		{"positive_t0_golangci_to_T0", "positive", Finding{Tool: "golangci-lint", Tier: 0}, 1, 0, 0, 0},
		{"positive_t1_to_T1", "positive", Finding{Tool: "anything", Tier: 1}, 0, 1, 0, 0},
		{"positive_t2_to_T2", "positive", Finding{Tool: "anything", Tier: 2}, 0, 0, 1, 0},
		{"negative_t0_non_golangci_to_NT", "negative", Finding{Tool: "gosec", Tier: 0}, 0, 0, 0, 1},
		{"boundary_unknown_tier_silent", "boundary", Finding{Tool: "x", Tier: 99}, 0, 0, 0, 0},
		{"boundary_negative_tier_silent", "boundary", Finding{Tool: "x", Tier: -1}, 0, 0, 0, 0},
		{"corner_empty_tool_name_t0", "corner", Finding{Tool: "", Tier: 0}, 0, 0, 0, 1}, // empty tool isn't tiered
		{"adversarial_golangci_in_middle_of_name", "adversarial", Finding{Tool: "Xgolangci-lint", Tier: 0}, 0, 0, 0, 1},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			var tc2 tierCounts
			bumpTierCount(&tc2, tc.f)
			if tc2.T0 != tc.wantT0 || tc2.T1 != tc.wantT1 || tc2.T2 != tc.wantT2 || tc2.NT != tc.wantNT {
				t.Errorf("counts = {T0:%d T1:%d T2:%d NT:%d}, want {%d %d %d %d}",
					tc2.T0, tc2.T1, tc2.T2, tc2.NT, tc.wantT0, tc.wantT1, tc.wantT2, tc.wantNT)
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// bumpQuickFixable
// ───────────────────────────────────────────────────────────────────────

func TestBumpQuickFixable_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                   string
		category               string
		tier                   int
		wantT0, wantT1, wantT2 int
	}{
		{"positive_tier0", "positive", 0, 1, 0, 0},
		{"positive_tier1", "positive", 1, 0, 1, 0},
		{"positive_tier2", "positive", 2, 0, 0, 1},
		{"boundary_unknown_tier_silent", "boundary", 99, 0, 0, 0},
		{"corner_negative_tier_silent", "corner", -1, 0, 0, 0},
		{"adversarial_max_int_tier_silent", "adversarial", 1 << 30, 0, 0, 0},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			var qf quickFixableCounts
			bumpQuickFixable(&qf, tc.tier)
			if qf.T0 != tc.wantT0 || qf.T1 != tc.wantT1 || qf.T2 != tc.wantT2 {
				t.Errorf("qf = %+v, want T0=%d T1=%d T2=%d",
					qf, tc.wantT0, tc.wantT1, tc.wantT2)
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// accumulateFindingCounts
// ───────────────────────────────────────────────────────────────────────

func TestAccumulateFindingCounts_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		category      string
		findings      []Finding
		wantFiles     int
		wantHasErr    bool
		wantTierTotal int
	}{
		{
			name:          "positive_three_findings_distinct_files",
			category:      "positive",
			findings:      []Finding{{File: "a.go", Tier: 0, Tool: "golangci-lint"}, {File: "b.go", Tier: 1}, {File: "c.go", Tier: 2}},
			wantFiles:     3,
			wantTierTotal: 3,
		},
		{
			name:          "positive_error_severity_sets_flag",
			category:      "positive",
			findings:      []Finding{{File: "a.go", Severity: severityError, Tier: 1}},
			wantFiles:     1,
			wantHasErr:    true,
			wantTierTotal: 1,
		},
		{
			name:       "negative_empty_findings",
			category:   "negative",
			findings:   nil,
			wantFiles:  0,
			wantHasErr: false,
		},
		{
			name:          "boundary_finding_no_file",
			category:      "boundary",
			findings:      []Finding{{File: "", Tier: 1}},
			wantFiles:     0,
			wantTierTotal: 1,
		},
		{
			name:          "corner_same_file_multiple_findings",
			category:      "corner",
			findings:      []Finding{{File: "x.go", Tier: 0, Tool: "golangci-lint"}, {File: "x.go", Tier: 1}, {File: "x.go", Tier: 2}},
			wantFiles:     1,
			wantTierTotal: 3,
		},
		{
			name:          "adversarial_thousand_findings",
			category:      "adversarial",
			findings:      manyFindings(1000),
			wantFiles:     1000,
			wantTierTotal: 1000,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			var r renderInput
			got := accumulateFindingCounts(&r, tc.findings)
			if got != tc.wantFiles {
				t.Errorf("files = %d, want %d", got, tc.wantFiles)
			}
			if r.HasErrSeverity != tc.wantHasErr {
				t.Errorf("HasErrSeverity = %v, want %v", r.HasErrSeverity, tc.wantHasErr)
			}
			totalTier := r.TierCounts.T0 + r.TierCounts.T1 + r.TierCounts.T2 + r.TierCounts.NT
			if totalTier != tc.wantTierTotal {
				t.Errorf("total tier counts = %d, want %d", totalTier, tc.wantTierTotal)
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// splitFindingsByTool
// ───────────────────────────────────────────────────────────────────────

func TestSplitFindingsByTool_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		category string
		findings []Finding
		wantL    int
		wantA    int
		wantG    int
	}{
		{"positive_mixed_three_tools", "positive",
			[]Finding{{Tool: "golangci-lint"}, {Tool: "netlink-audit"}, {Tool: toolGosec}},
			1, 1, 1},
		{"positive_all_audits", "positive",
			[]Finding{{Tool: "netlink-audit"}, {Tool: "iouring-audit"}, {Tool: "metrics-audit"}, {Tool: "proto-field-audit"}},
			0, 4, 0},
		{"negative_empty_findings", "negative", nil, 0, 0, 0},
		{"boundary_only_gosec", "boundary", []Finding{{Tool: toolGosec}}, 0, 0, 1},
		{"corner_unknown_tool_defaults_to_linter", "corner",
			[]Finding{{Tool: "mystery-tool"}, {Tool: "another-mystery"}},
			2, 0, 0},
		{"adversarial_huge_mixed", "adversarial",
			mixedFindings(500),
			334, 0, 166},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			l, a, g := splitFindingsByTool(tc.findings)
			if len(l) != tc.wantL || len(a) != tc.wantA || len(g) != tc.wantG {
				t.Errorf("got (%d,%d,%d) (l,a,g), want (%d,%d,%d)",
					len(l), len(a), len(g), tc.wantL, tc.wantA, tc.wantG)
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// sortGosecBySeverityFileLine
// ───────────────────────────────────────────────────────────────────────

func TestSortGosecBySeverityFileLine_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		category string
		input    []Finding
		want     []Finding // expected order (just severity for brevity)
	}{
		{
			name:     "positive_high_before_low",
			category: "positive",
			input: []Finding{
				{Severity: severityInfo, File: "a", Line: 1},
				{Severity: severityError, File: "a", Line: 2},
			},
			want: []Finding{
				{Severity: severityError, File: "a", Line: 2},
				{Severity: severityInfo, File: "a", Line: 1},
			},
		},
		{
			name:     "positive_same_severity_file_alpha",
			category: "positive",
			input: []Finding{
				{Severity: severityError, File: "z", Line: 1},
				{Severity: severityError, File: "a", Line: 1},
			},
			want: []Finding{
				{Severity: severityError, File: "a", Line: 1},
				{Severity: severityError, File: "z", Line: 1},
			},
		},
		{
			name:     "boundary_single_finding",
			category: "boundary",
			input:    []Finding{{Severity: severityWarning, File: "x", Line: 5}},
			want:     []Finding{{Severity: severityWarning, File: "x", Line: 5}},
		},
		{
			name:     "boundary_empty_slice",
			category: "boundary",
			input:    nil,
			want:     nil,
		},
		{
			name:     "corner_same_severity_same_file_line_asc",
			category: "corner",
			input: []Finding{
				{Severity: severityError, File: "x", Line: 5},
				{Severity: severityError, File: "x", Line: 1},
			},
			want: []Finding{
				{Severity: severityError, File: "x", Line: 1},
				{Severity: severityError, File: "x", Line: 5},
			},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			got := append([]Finding(nil), tc.input...) // copy
			sortGosecBySeverityFileLine(got)
			if len(got) != len(tc.want) {
				t.Fatalf("len = %d, want %d", len(got), len(tc.want))
			}
			for i := range got {
				if got[i].Severity != tc.want[i].Severity ||
					got[i].File != tc.want[i].File ||
					got[i].Line != tc.want[i].Line {
					t.Errorf("got[%d] = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// accumulateTestStats + recordTestFailure
// ───────────────────────────────────────────────────────────────────────

func TestAccumulateTestStats_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name            string
		category        string
		tests           []TestResult
		wantTot         int
		wantPass        int
		wantFailNew     int
		wantFailPre     int
		wantSkip        int
		wantFailingList int
	}{
		{
			name:     "positive_mix_pass_fail_skip",
			category: "positive",
			tests: []TestResult{
				{Action: testActionPass, Test: "T1"},
				{Action: testActionFail, Test: "T2"},
				{Action: testActionSkip, Test: "T3"},
			},
			wantTot: 3, wantPass: 1, wantFailNew: 1, wantSkip: 1, wantFailingList: 1,
		},
		{
			name:     "positive_preexist_failure",
			category: "positive",
			tests: []TestResult{
				{Action: testActionFail, Test: "T1", Preexist: true},
				{Action: testActionFail, Test: "T2", Preexist: false},
			},
			wantTot: 2, wantFailNew: 1, wantFailPre: 1, wantFailingList: 2,
		},
		{
			name:     "negative_empty_tests",
			category: "negative",
			tests:    nil,
		},
		{
			name:     "boundary_package_level_failure_no_test_name",
			category: "boundary",
			tests:    []TestResult{{Action: testActionFail, Test: ""}},
			wantTot:  1, wantFailNew: 1, wantFailingList: 0, // empty test name → not appended
		},
		{
			name:     "corner_unknown_action_only_increments_total",
			category: "corner",
			tests:    []TestResult{{Action: "build-fail", Test: "T1"}},
			wantTot:  1,
		},
		{
			name:     "adversarial_hundred_pass",
			category: "adversarial",
			tests:    manyTests(100, testActionPass),
			wantTot:  100, wantPass: 100,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			var r renderInput
			accumulateTestStats(&r, tc.tests)
			if r.TestStats.Total != tc.wantTot {
				t.Errorf("Total = %d, want %d", r.TestStats.Total, tc.wantTot)
			}
			if r.TestStats.Pass != tc.wantPass {
				t.Errorf("Pass = %d, want %d", r.TestStats.Pass, tc.wantPass)
			}
			if r.TestStats.FailNew != tc.wantFailNew {
				t.Errorf("FailNew = %d, want %d", r.TestStats.FailNew, tc.wantFailNew)
			}
			if r.TestStats.FailPre != tc.wantFailPre {
				t.Errorf("FailPre = %d, want %d", r.TestStats.FailPre, tc.wantFailPre)
			}
			if r.TestStats.Skip != tc.wantSkip {
				t.Errorf("Skip = %d, want %d", r.TestStats.Skip, tc.wantSkip)
			}
			if len(r.FailingTests) != tc.wantFailingList {
				t.Errorf("FailingTests len = %d, want %d", len(r.FailingTests), tc.wantFailingList)
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// buildCoverageRows
// ───────────────────────────────────────────────────────────────────────

func TestBuildCoverageRows_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		category  string
		cov       Coverage
		wantLen   int
		wantBelow int // count of rows with Below=true
	}{
		{
			name:     "positive_two_packages_one_below",
			category: "positive",
			cov: Coverage{
				Available:  true,
				PerPackage: map[string]float64{"pkg/a": 95.0, "pkg/b": 50.0},
			},
			wantLen: 2, wantBelow: 1,
		},
		{
			name:     "negative_unavailable_returns_nil",
			category: "negative",
			cov:      Coverage{Available: false, PerPackage: map[string]float64{"a": 95.0}},
			wantLen:  0,
		},
		{
			name:     "boundary_at_threshold_not_below",
			category: "boundary",
			cov: Coverage{
				Available:  true,
				PerPackage: map[string]float64{"pkg/exact": CoverageThreshold},
			},
			wantLen: 1, wantBelow: 0,
		},
		{
			name:     "boundary_just_under_threshold",
			category: "boundary",
			cov: Coverage{
				Available:  true,
				PerPackage: map[string]float64{"pkg/almost": CoverageThreshold - 0.01},
			},
			wantLen: 1, wantBelow: 1,
		},
		{
			name:     "corner_zero_pct_package",
			category: "corner",
			cov: Coverage{
				Available:  true,
				PerPackage: map[string]float64{"pkg/dead": 0},
			},
			wantLen: 1, wantBelow: 1,
		},
		{
			name:     "adversarial_many_packages_sorted",
			category: "adversarial",
			cov: Coverage{
				Available:  true,
				PerPackage: buildCoverageMap(50),
			},
			wantLen: 50, wantBelow: 25, // half above, half below threshold
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			rows := buildCoverageRows(tc.cov)
			if len(rows) != tc.wantLen {
				t.Errorf("rows len = %d, want %d", len(rows), tc.wantLen)
			}
			below := 0
			for _, r := range rows {
				if r.Below {
					below++
				}
			}
			if below != tc.wantBelow {
				t.Errorf("below count = %d, want %d", below, tc.wantBelow)
			}
			// Verify sorted order.
			for i := 1; i < len(rows); i++ {
				if rows[i-1].Pkg > rows[i].Pkg {
					t.Errorf("rows[%d] = %q > rows[%d] = %q (must be sorted)",
						i-1, rows[i-1].Pkg, i, rows[i].Pkg)
				}
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// topHotspots
// ───────────────────────────────────────────────────────────────────────

func TestTopHotspots_truncation(t *testing.T) {
	t.Parallel()
	// 15 unique files → only top 10 should survive.
	findings := make([]Finding, 0, 15)
	for i := 0; i < 15; i++ {
		findings = append(findings, Finding{File: string(rune('a'+i)) + ".go"})
	}
	got := topHotspots(findings, 10)
	if len(got) > 10 {
		t.Errorf("len = %d, want <= 10", len(got))
	}
}

// ───────────────────────────────────────────────────────────────────────
// Race
// ───────────────────────────────────────────────────────────────────────

func TestEmitHelpers_concurrent(t *testing.T) {
	const goroutines = 32
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				var tc tierCounts
				bumpTierCount(&tc, Finding{Tool: "golangci-lint", Tier: j % 3})
				var qf quickFixableCounts
				bumpQuickFixable(&qf, j%3)
				var r renderInput
				accumulateFindingCounts(&r, mixedFindings(20))
				_, _, _ = splitFindingsByTool(mixedFindings(20))
				rows := buildCoverageRows(Coverage{
					Available:  true,
					PerPackage: map[string]float64{"pkg/x": 50.0},
				})
				_ = rows
			}
		}()
	}
	wg.Wait()
}

// ───────────────────────────────────────────────────────────────────────
// Benchmarks
// ───────────────────────────────────────────────────────────────────────

func BenchmarkAccumulateFindingCounts(b *testing.B) {
	b.ReportAllocs()
	findings := manyFindings(100)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var r renderInput
		_ = accumulateFindingCounts(&r, findings)
	}
}

func BenchmarkSplitFindingsByTool(b *testing.B) {
	b.ReportAllocs()
	findings := mixedFindings(100)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = splitFindingsByTool(findings)
	}
}

func BenchmarkBuildCoverageRows(b *testing.B) {
	b.ReportAllocs()
	cov := Coverage{
		Available:  true,
		PerPackage: buildCoverageMap(50),
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = buildCoverageRows(cov)
	}
}

// ────────── helpers ──────────────────────────────────────────────────

func manyFindings(n int) []Finding {
	out := make([]Finding, n)
	for i := 0; i < n; i++ {
		out[i] = Finding{
			Tool: "golangci-lint",
			File: "pkg/x/file" + intStr(i) + ".go",
			Tier: i % 3,
		}
	}
	return out
}

// mixedFindings yields a deterministic mix of (linter, gosec) with no
// audit tools — used by the adversarial split case where the expected
// (l, a, g) = (2/3, 0, 1/3).
func mixedFindings(n int) []Finding {
	out := make([]Finding, n)
	for i := 0; i < n; i++ {
		switch i % 3 {
		case 0, 1:
			out[i] = Finding{Tool: "golangci-lint", Tier: 0}
		case 2:
			out[i] = Finding{Tool: toolGosec, Severity: severityWarning, File: "x", Line: i}
		}
	}
	return out
}

func manyTests(n int, action string) []TestResult {
	out := make([]TestResult, n)
	for i := 0; i < n; i++ {
		out[i] = TestResult{Action: action, Test: "T" + intStr(i)}
	}
	return out
}

// buildCoverageMap yields n packages where exactly n/2 are below the
// CoverageThreshold (50%-ish), and the rest are above.
func buildCoverageMap(n int) map[string]float64 {
	out := make(map[string]float64, n)
	for i := 0; i < n; i++ {
		pct := 95.0
		if i < n/2 {
			pct = 50.0
		}
		out["pkg/p"+intStr(i)] = pct
	}
	return out
}

// intStr is a tiny strconv shim so this test file stays imports-tight.
func intStr(i int) string {
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
