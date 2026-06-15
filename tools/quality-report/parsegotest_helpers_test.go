package main

import (
	"sync"
	"testing"
)

// parsegotest_helpers_test.go covers the three helpers extracted from
// parseGoTest in the gocyclo-16 → 5 refactor (applyTestEvent +
// recordTerminalAction + finalizeTestResults). End-to-end coverage of
// parseGoTest itself lives in main_test.go; these tests pin the units.

// ───────────────────────────────────────────────────────────────────────
// applyTestEvent
// ───────────────────────────────────────────────────────────────────────

func TestApplyTestEvent_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		category string
		events   []goTestEvent
		known    map[string]bool
		wantKey  string // key to inspect after events
		// Asserted fields (zero means "ignore"):
		wantAction   string
		wantElapsed  float64
		wantOutput   string
		wantPreexist bool
		wantMissing  bool // expect key absent
	}{
		{
			name:     "positive_run_then_pass",
			category: "positive",
			events: []goTestEvent{
				{Action: "run", Package: "pkg", Test: "T"},
				{Action: testActionPass, Package: "pkg", Test: "T", Elapsed: 0.5},
			},
			wantKey:     "pkg/T",
			wantAction:  testActionPass,
			wantElapsed: 0.5,
		},
		{
			name:     "positive_fail_with_accumulated_output",
			category: "positive",
			events: []goTestEvent{
				{Action: "run", Package: "pkg", Test: "T"},
				{Action: "output", Package: "pkg", Test: "T", Output: "line1\n"},
				{Action: "output", Package: "pkg", Test: "T", Output: "line2\n"},
				{Action: testActionFail, Package: "pkg", Test: "T", Elapsed: 1.0},
			},
			wantKey:     "pkg/T",
			wantAction:  testActionFail,
			wantElapsed: 1.0,
			wantOutput:  "line1\nline2\n",
		},
		{
			name:     "positive_known_preexisting_fail",
			category: "positive",
			events: []goTestEvent{
				{Action: "run", Package: "pkg", Test: "T"},
				{Action: testActionFail, Package: "pkg", Test: "T"},
			},
			known:        map[string]bool{"pkg.T": true},
			wantKey:      "pkg/T",
			wantAction:   testActionFail,
			wantPreexist: true,
		},
		{
			name:     "positive_preexist_match_by_test_name_only",
			category: "positive",
			events: []goTestEvent{
				{Action: testActionFail, Package: "pkg/diff", Test: "T"},
			},
			known:        map[string]bool{"T": true}, // matches the second `||` branch
			wantKey:      "pkg/diff/T",
			wantAction:   testActionFail,
			wantPreexist: true,
		},
		{
			name:     "negative_empty_action_is_noop",
			category: "negative",
			events: []goTestEvent{
				{Action: "", Package: "pkg", Test: "T"},
			},
			wantKey:     "pkg/T",
			wantMissing: true,
		},
		{
			name:     "negative_output_with_empty_test_name_discarded",
			category: "negative",
			events: []goTestEvent{
				{Action: "output", Package: "pkg", Test: "", Output: "package-level"},
				{Action: testActionFail, Package: "pkg", Test: "T"},
			},
			wantKey:    "pkg/T",
			wantAction: testActionFail,
			wantOutput: "", // package-level output wasn't accumulated for the test
		},
		{
			name:     "negative_unknown_action",
			category: "negative",
			events: []goTestEvent{
				{Action: "pause", Package: "pkg", Test: "T"},
			},
			wantKey:     "pkg/T",
			wantMissing: true,
		},
		{
			name:     "boundary_terminal_without_run",
			category: "boundary",
			events: []goTestEvent{
				{Action: testActionPass, Package: "pkg", Test: "T", Elapsed: 0.1},
			},
			// recordTerminalAction creates the entry on the fly when there's
			// no preceding "run" — pin this.
			wantKey:     "pkg/T",
			wantAction:  testActionPass,
			wantElapsed: 0.1,
		},
		{
			name:     "boundary_skip_action",
			category: "boundary",
			events: []goTestEvent{
				{Action: testActionSkip, Package: "pkg", Test: "T"},
			},
			wantKey:    "pkg/T",
			wantAction: testActionSkip,
		},
		{
			name:     "corner_run_run_then_pass_uses_second_entry",
			category: "corner",
			events: []goTestEvent{
				{Action: "run", Package: "pkg", Test: "T"},
				{Action: "run", Package: "pkg", Test: "T"}, // second run overwrites
				{Action: testActionPass, Package: "pkg", Test: "T", Elapsed: 0.2},
			},
			wantKey:     "pkg/T",
			wantAction:  testActionPass,
			wantElapsed: 0.2,
		},
		{
			name:     "corner_fail_with_no_output_events_empty_output",
			category: "corner",
			events: []goTestEvent{
				{Action: testActionFail, Package: "pkg", Test: "T"},
			},
			wantKey:    "pkg/T",
			wantAction: testActionFail,
			wantOutput: "",
		},
		{
			name:     "adversarial_many_output_then_fail",
			category: "adversarial",
			events: appendOutputs(
				[]goTestEvent{{Action: "run", Package: "pkg", Test: "T"}},
				100, "pkg", "T", "x",
			),
			wantKey:    "pkg/T",
			wantOutput: repeatStr("x", 100),
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			results := map[string]*TestResult{}
			failOutput := map[string]string{}
			known := tc.known
			if known == nil {
				known = map[string]bool{}
			}
			// Append a terminal fail for adversarial so output is observable.
			events := tc.events
			if tc.name == "adversarial_many_output_then_fail" {
				events = append(events, goTestEvent{Action: testActionFail, Package: "pkg", Test: "T"})
			}
			for _, e := range events {
				applyTestEvent(results, failOutput, e, known)
			}
			r, present := results[tc.wantKey]
			if tc.wantMissing {
				if present {
					t.Errorf("key %q present, want missing", tc.wantKey)
				}
				return
			}
			if !present {
				t.Fatalf("key %q missing", tc.wantKey)
			}
			if tc.wantAction != "" && r.Action != tc.wantAction {
				t.Errorf("Action = %q, want %q", r.Action, tc.wantAction)
			}
			if tc.wantElapsed != 0 && r.Elapsed != tc.wantElapsed {
				t.Errorf("Elapsed = %v, want %v", r.Elapsed, tc.wantElapsed)
			}
			if tc.wantOutput != "" && r.Output != tc.wantOutput {
				t.Errorf("Output = %q, want %q", r.Output, tc.wantOutput)
			}
			if tc.wantPreexist != r.Preexist {
				t.Errorf("Preexist = %v, want %v", r.Preexist, tc.wantPreexist)
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// finalizeTestResults
// ───────────────────────────────────────────────────────────────────────

func TestFinalizeTestResults_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		category string
		input    map[string]*TestResult
		wantLen  int
		// wantFirst[0,1] = (Package, Test) of first sorted entry
		wantFirstPkg  string
		wantFirstTest string
	}{
		{
			name:     "positive_two_packages_sorted_alpha",
			category: "positive",
			input: map[string]*TestResult{
				"pkg/z/Test1": {Package: "pkg/z", Test: "Test1"},
				"pkg/a/Test2": {Package: "pkg/a", Test: "Test2"},
			},
			wantLen: 2, wantFirstPkg: "pkg/a", wantFirstTest: "Test2",
		},
		{
			name:     "positive_same_package_sorted_by_test",
			category: "positive",
			input: map[string]*TestResult{
				"pkg/x/Z": {Package: "pkg/x", Test: "Z"},
				"pkg/x/A": {Package: "pkg/x", Test: "A"},
			},
			wantLen: 2, wantFirstPkg: "pkg/x", wantFirstTest: "A",
		},
		{
			name:     "negative_empty_input",
			category: "negative",
			input:    map[string]*TestResult{},
			wantLen:  0,
		},
		{
			name:     "boundary_single_entry",
			category: "boundary",
			input: map[string]*TestResult{
				"pkg/x/T": {Package: "pkg/x", Test: "T"},
			},
			wantLen: 1, wantFirstPkg: "pkg/x", wantFirstTest: "T",
		},
		{
			name:     "corner_empty_test_field_sorts_first",
			category: "corner",
			input: map[string]*TestResult{
				"pkg/x/":  {Package: "pkg/x", Test: ""},
				"pkg/x/B": {Package: "pkg/x", Test: "B"},
			},
			wantLen: 2, wantFirstPkg: "pkg/x", wantFirstTest: "",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			got := finalizeTestResults(tc.input)
			if len(got) != tc.wantLen {
				t.Fatalf("len = %d, want %d", len(got), tc.wantLen)
			}
			if tc.wantLen == 0 {
				return
			}
			if got[0].Package != tc.wantFirstPkg || got[0].Test != tc.wantFirstTest {
				t.Errorf("first = (%q, %q), want (%q, %q)",
					got[0].Package, got[0].Test, tc.wantFirstPkg, tc.wantFirstTest)
			}
			// Verify monotonic order.
			for i := 1; i < len(got); i++ {
				if got[i-1].Package > got[i].Package ||
					(got[i-1].Package == got[i].Package && got[i-1].Test > got[i].Test) {
					t.Errorf("not sorted at index %d: (%q,%q) > (%q,%q)",
						i, got[i-1].Package, got[i-1].Test, got[i].Package, got[i].Test)
				}
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// Race
// ───────────────────────────────────────────────────────────────────────

func TestParseGoTestHelpers_concurrent(t *testing.T) {
	const goroutines = 16
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				results := map[string]*TestResult{}
				failOutput := map[string]string{}
				known := map[string]bool{"pkg.T": true}
				for _, e := range []goTestEvent{
					{Action: "run", Package: "pkg", Test: "T"},
					{Action: "output", Package: "pkg", Test: "T", Output: "x"},
					{Action: testActionFail, Package: "pkg", Test: "T"},
				} {
					applyTestEvent(results, failOutput, e, known)
				}
				_ = finalizeTestResults(results)
			}
		}()
	}
	wg.Wait()
}

// ───────────────────────────────────────────────────────────────────────
// Benchmarks
// ───────────────────────────────────────────────────────────────────────

func BenchmarkApplyTestEvent_pass(b *testing.B) {
	b.ReportAllocs()
	known := map[string]bool{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		results := map[string]*TestResult{}
		failOutput := map[string]string{}
		applyTestEvent(results, failOutput,
			goTestEvent{Action: "run", Package: "pkg", Test: "T"}, known)
		applyTestEvent(results, failOutput,
			goTestEvent{Action: testActionPass, Package: "pkg", Test: "T", Elapsed: 0.1}, known)
	}
}

func BenchmarkFinalizeTestResults_hundred(b *testing.B) {
	b.ReportAllocs()
	input := map[string]*TestResult{}
	for i := 0; i < 100; i++ {
		key := "pkg/T" + intStr(i)
		input[key] = &TestResult{Package: "pkg", Test: "T" + intStr(i)}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = finalizeTestResults(input)
	}
}

// helpers ----------------------------------------------------------------

func appendOutputs(base []goTestEvent, n int, pkg, test, output string) []goTestEvent {
	for i := 0; i < n; i++ {
		base = append(base, goTestEvent{Action: "output", Package: pkg, Test: test, Output: output})
	}
	return base
}

func repeatStr(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}
