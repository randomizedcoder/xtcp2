package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// helpers_test.go covers the pieces runMain wires together: normalizeKey,
// relpath, dedupeSorted, parseGated, pairFlag.Set, parseBaselineLine, diffTier
// and refuseToRatchet. main_test.go drives the end-to-end exit codes; these
// tests pin the unit-level contracts, so a failure names the function rather
// than the exit code.
//
// This file follows tools/netlink-audit/helpers_test.go's convention: `name` +
// `category` fields with t.Run(category+"/"+name, ...) and t.Parallel() at both
// levels. The sibling main_test.go uses sentence-style `description` fields
// instead, matching tools/netlink-audit/main_test.go. Two conventions in one
// directory is deliberate and mirrors the directory this tool is modeled on;
// neither file mixes them.

func TestNormalizeKey_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		category string
		path     string
		message  string
		linter   string
		expected string
	}{
		{
			"positive_plain", "positive",
			"cmd/zstd-probe/main.go", "string `file` has 7 occurrences", "goconst",
			"cmd/zstd-probe/main.go | string `file` has 7 occurrences (goconst)",
		},
		{
			"positive_relative_subdir", "positive",
			"internal/goip/render/route.go", "at least one file in a package should have a package comment", "staticcheck",
			"internal/goip/render/route.go | at least one file in a package should have a package comment (staticcheck)",
		},
		{
			"negative_empty_message", "negative",
			"a.go", "", "unused",
			"a.go |  (unused)",
		},
		{
			"negative_empty_linter", "negative",
			"a.go", "something", "",
			"a.go | something ()",
		},
		{
			"boundary_all_empty", "boundary",
			"", "", "",
			" |  ()",
		},
		{
			// The separator is " | " with spaces; a message already containing a
			// bare pipe does not create a second field, because the key is never
			// split back apart.
			"corner_pipe_in_message", "corner",
			"a.go", "want a | b", "revive",
			"a.go | want a | b (revive)",
		},
		{
			"corner_pipe_in_path", "corner",
			"cmd/we|rd/main.go", "msg", "revive",
			"cmd/we|rd/main.go | msg (revive)",
		},
		{
			"corner_parens_in_message", "corner",
			"a.go", "func f(x int) is too long (80 > 70)", "funlen",
			"a.go | func f(x int) is too long (80 > 70) (funlen)",
		},
		{
			"adversarial_newline_in_message", "adversarial",
			"a.go", "line one\nline two", "revive",
			"a.go | line one\nline two (revive)",
		},
		{
			"adversarial_very_long_path", "adversarial",
			strings.Repeat("deep/", 200) + "x.go", "m", "l",
			strings.Repeat("deep/", 200) + "x.go | m (l)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			if got := normalizeKey(tc.path, tc.message, tc.linter); got != tc.expected {
				t.Errorf("normalizeKey(%q, %q, %q) = %q, want %q",
					tc.path, tc.message, tc.linter, got, tc.expected)
			}
		})
	}
}

func TestRelpath_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		category string
		path     string
		root     string
		expected string
	}{
		{
			// The normal case: golangci reports relative to its run directory,
			// so there is nothing to strip.
			"positive_already_relative", "positive",
			"cmd/a/main.go", "/build/xtcp2", "cmd/a/main.go",
		},
		{
			"positive_absolute_under_root", "positive",
			"/build/xtcp2/cmd/a/main.go", "/build/xtcp2", "cmd/a/main.go",
		},
		{
			"negative_no_root_given", "negative",
			"/build/xtcp2/cmd/a/main.go", "", "/build/xtcp2/cmd/a/main.go",
		},
		{
			// A path outside the root is left absolute rather than rendered as
			// ../../: a baseline key full of parent traversals would be unreadable
			// and would move with the root.
			"negative_absolute_outside_root", "negative",
			"/nix/store/abc/other.go", "/build/xtcp2", "/nix/store/abc/other.go",
		},
		{
			"boundary_path_equals_root", "boundary",
			"/build/xtcp2", "/build/xtcp2", ".",
		},
		{
			"boundary_empty_path", "boundary",
			"", "/build/xtcp2", "",
		},
		{
			"corner_root_with_trailing_slash", "corner",
			"/build/xtcp2/cmd/a/main.go", "/build/xtcp2/", "cmd/a/main.go",
		},
		{
			// A sibling directory whose name merely starts with the root's name
			// must not be treated as being inside it.
			"corner_sibling_prefix_not_inside", "corner",
			"/build/xtcp2-other/cmd/a.go", "/build/xtcp2", "/build/xtcp2-other/cmd/a.go",
		},
		{
			"adversarial_pipe_in_absolute_path", "adversarial",
			"/build/xtcp2/cmd/we|rd/a.go", "/build/xtcp2", "cmd/we|rd/a.go",
		},
	}
	for _, tc := range cases {
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			if got := relpath(tc.path, tc.root); got != tc.expected {
				t.Errorf("relpath(%q, %q) = %q, want %q", tc.path, tc.root, got, tc.expected)
			}
		})
	}
}

func TestDedupeSorted_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		category string
		input    []string
		expected []string
	}{
		{"positive_already_sorted", "positive", []string{"a", "b", "c"}, []string{"a", "b", "c"}},
		{"positive_unsorted", "positive", []string{"c", "a", "b"}, []string{"a", "b", "c"}},
		{
			// Dropping line:col can make two genuinely distinct issues share a
			// key. A set is the right model: the question is presence, not count.
			"negative_duplicates_collapse", "negative",
			[]string{"a", "a", "a"}, []string{"a"},
		},
		{"boundary_empty", "boundary", []string{}, []string{}},
		{"boundary_single", "boundary", []string{"only"}, []string{"only"}},
		{"corner_all_duplicates_unsorted", "corner", []string{"b", "a", "b", "a"}, []string{"a", "b"}},
		{"corner_empty_string_sorts_first", "corner", []string{"a", ""}, []string{"", "a"}},
	}
	for _, tc := range cases {
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			got := dedupeSorted(append([]string(nil), tc.input...))
			if len(got) != len(tc.expected) {
				t.Fatalf("dedupeSorted(%q) = %q, want %q", tc.input, got, tc.expected)
			}
			for i := range got {
				if got[i] != tc.expected[i] {
					t.Fatalf("dedupeSorted(%q) = %q, want %q", tc.input, got, tc.expected)
				}
			}
		})
	}
}

func TestParseGated_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		category  string
		input     string
		expected  []tier
		expectErr bool
	}{
		{"positive_single", "positive", "tier0", []tier{tier0}, false},
		{"positive_all_three", "positive", "tier0,tier1,tier2", []tier{tier0, tier1, tier2}, false},
		{
			// Empty is valid and means every tier is advisory, which is the honest
			// state for a tier whose findings have not been emptied yet.
			"boundary_empty_means_none_gated", "boundary",
			"", nil, false,
		},
		{"boundary_whitespace_only", "boundary", "   ", nil, false},
		{"corner_spaces_around_names", "corner", " tier0 , tier1 ", []tier{tier0, tier1}, false},
		{"corner_trailing_comma", "corner", "tier0,", []tier{tier0}, false},
		{"corner_repeated_name_is_idempotent", "corner", "tier0,tier0", []tier{tier0}, false},
		{"negative_unknown_tier", "negative", "tier7", nil, true},
		{"negative_unknown_among_known", "negative", "tier0,quick", nil, true},
		// A tab around a name is trimmed exactly like a space, which is what
		// corner_spaces_around_names already establishes for the generated -gated
		// value. The tab that MUST be rejected is one inside a baseline key, where
		// it would collide with the line separator; that is a readBaseline concern
		// and has its own row in TestParseBaselineLine_table.
		{"adversarial_tier_name_trailing_tab_is_trimmed", "adversarial", "tier0\t", []tier{tier0}, false},
		// But TrimSpace does not touch interior whitespace, so a tab wedged into
		// the middle of a name must not be collapsed into a match.
		{"adversarial_tab_inside_name", "adversarial", "tier\t0", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseGated(tc.input)
			if tc.expectErr {
				if err == nil {
					t.Fatalf("parseGated(%q) = %v, want an error", tc.input, got)
				}
				if !errors.Is(err, errFlagUnknownTier) {
					t.Errorf("parseGated(%q) error = %v, want errFlagUnknownTier", tc.input, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseGated(%q): unexpected error %v", tc.input, err)
			}
			if len(got) != len(tc.expected) {
				t.Fatalf("parseGated(%q) has %d tiers, want %d (%v)", tc.input, len(got), len(tc.expected), got)
			}
			for _, want := range tc.expected {
				if !got[want] {
					t.Errorf("parseGated(%q) missing %s", tc.input, want)
				}
			}
		})
	}
}

// TestRequireGatedMeasured_table pins the distinction the guard turns on: it
// demands that a gated tier was MEASURED, never that it has findings. Those two
// read identically in the report — both print "0 added" and exit 0 — so every
// row here names which of the two it is establishing.
//
// measured is keyed by tier with the value standing in for the findings list;
// nil is a tier that was measured and found clean, and an ABSENT key is a tier
// that was never measured at all. That is the same distinction collectFindings
// records, and getting it backwards is the fail-open this guard exists for.
func TestRequireGatedMeasured_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		category  string
		gated     []tier
		measured  map[tier][]string
		expectErr bool
		// expectedTier is the tier the error must name, so a multi-tier fault
		// cannot pass by reporting the wrong one.
		expectedTier tier
	}{
		{
			"positive_gated_tier_measured_with_findings", "positive",
			[]tier{tier0}, map[tier][]string{tier0: {"a | b (c)"}}, false, "",
		},
		{
			// The goal state. A clean gated tier must pass, which is why the
			// guard cannot simply require a non-empty list.
			"positive_gated_tier_measured_and_clean", "positive",
			[]tier{tier0}, map[tier][]string{tier0: nil}, false, "",
		},
		{
			"positive_all_three_gated_and_all_measured", "positive",
			[]tier{tier0, tier1, tier2},
			map[tier][]string{tier0: nil, tier1: nil, tier2: nil}, false, "",
		},
		{
			"negative_gated_tier_absent_from_measurements", "negative",
			[]tier{tier0}, map[tier][]string{}, true, tier0,
		},
		{
			"negative_one_of_three_gated_tiers_unmeasured", "negative",
			[]tier{tier0, tier1, tier2},
			map[tier][]string{tier0: nil, tier2: nil}, true, tier1,
		},
		{
			// An unmeasured tier that nothing gates is the normal advisory
			// state during Phases 3-5 and must not be an error.
			"boundary_unmeasured_tier_is_fine_when_not_gated", "boundary",
			[]tier{tier0}, map[tier][]string{tier0: nil}, false, "",
		},
		{
			"boundary_nothing_gated_and_nothing_measured", "boundary",
			nil, map[tier][]string{}, false, "",
		},
		{
			// Reported in allTiers order rather than map order, so the message
			// for a multi-tier fault is the same on every run.
			"corner_two_unmeasured_gated_tiers_report_the_first_in_tier_order", "corner",
			[]tier{tier1, tier2}, map[tier][]string{}, true, tier1,
		},
		{
			// Measuring a tier nobody gated is not a fault either; the check
			// measures all three and gates one on purpose.
			"corner_measured_tiers_beyond_the_gated_set", "corner",
			[]tier{tier0},
			map[tier][]string{tier0: nil, tier1: {"x | y (z)"}, tier2: nil}, false, "",
		},
		{
			// A nil map is not a map with nil values. Both are "nothing
			// measured", and a gated tier must fail against either.
			"adversarial_nil_measured_map_with_a_gated_tier", "adversarial",
			[]tier{tier0}, nil, true, tier0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			gated := make(map[tier]bool, len(tc.gated))
			for _, g := range tc.gated {
				gated[g] = true
			}
			err := requireGatedMeasured(gated, tc.measured)
			if !tc.expectErr {
				if err != nil {
					t.Fatalf("requireGatedMeasured(%v, %v): unexpected error %v", tc.gated, tc.measured, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("requireGatedMeasured(%v, %v) = nil, want an error naming %s",
					tc.gated, tc.measured, tc.expectedTier)
			}
			if !errors.Is(err, errFlagGatedUnmeasured) {
				t.Errorf("error = %v, want errFlagGatedUnmeasured", err)
			}
			if !strings.Contains(err.Error(), string(tc.expectedTier)) {
				t.Errorf("error = %q, want it to name %s", err, tc.expectedTier)
			}
		})
	}
}

func TestPairFlagSet_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		category  string
		inputs    []string
		expected  map[tier]string
		expectErr bool
	}{
		{
			"positive_single", "positive",
			[]string{"tier0=/tmp/a.json"},
			map[tier]string{tier0: "/tmp/a.json"}, false,
		},
		{
			"positive_three_tiers", "positive",
			[]string{"tier0=a", "tier1=b", "tier2=c"},
			map[tier]string{tier0: "a", tier1: "b", tier2: "c"}, false,
		},
		{
			// Last wins, and the tier is not double-registered in order.
			"corner_repeated_tier_overwrites", "corner",
			[]string{"tier0=first", "tier0=second"},
			map[tier]string{tier0: "second"}, false,
		},
		{
			// A findings path may legally contain "=", so only the first
			// separator counts.
			"corner_value_contains_equals", "corner",
			[]string{"tier0=/tmp/a=b.json"},
			map[tier]string{tier0: "/tmp/a=b.json"}, false,
		},
		{"negative_no_equals", "negative", []string{"tier0"}, nil, true},
		{"negative_empty_name", "negative", []string{"=/tmp/a.json"}, nil, true},
		{"negative_empty_value", "negative", []string{"tier0="}, nil, true},
		{"negative_unknown_tier", "negative", []string{"tier7=/tmp/a.json"}, nil, true},
		{"boundary_empty_string", "boundary", []string{""}, nil, true},
		{"adversarial_only_equals", "adversarial", []string{"="}, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			p := &pairFlag{}
			var err error
			for _, in := range tc.inputs {
				if err = p.Set(in); err != nil {
					break
				}
			}
			if tc.expectErr {
				if err == nil {
					t.Fatalf("Set(%q) = nil error, want one", tc.inputs)
				}
				return
			}
			if err != nil {
				t.Fatalf("Set(%q): unexpected error %v", tc.inputs, err)
			}
			if len(p.order) != len(tc.expected) {
				t.Fatalf("order has %d tiers, want %d: %v", len(p.order), len(tc.expected), p.order)
			}
			for tr, want := range tc.expected {
				if p.vals[tr] != want {
					t.Errorf("vals[%s] = %q, want %q", tr, p.vals[tr], want)
				}
			}
		})
	}
}

func TestParseBaselineLine_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		category    string
		line        string
		expectTier  tier
		expectKey   string
		expectErrIs error
	}{
		{
			"positive_tier0", "positive",
			"tier0\tcmd/a/main.go | msg (unused)",
			tier0, "cmd/a/main.go | msg (unused)", nil,
		},
		{
			"positive_tier2", "positive",
			"tier2\tcmd/xtcp2/xtcp2.go | defineFlags is too long (79 > 70) (funlen)",
			tier2, "cmd/xtcp2/xtcp2.go | defineFlags is too long (79 > 70) (funlen)", nil,
		},
		{
			"negative_no_tab", "negative",
			"tier0 cmd/a/main.go | msg (unused)",
			"", "", errBaselineBadLine,
		},
		{
			"negative_unknown_tier", "negative",
			"tier9\tcmd/a/main.go | msg (unused)",
			"", "", errBaselineUnknownTier,
		},
		{
			"negative_empty_key", "negative",
			"tier0\t",
			"", "", errBaselineEmptyKey,
		},
		{
			// The one assumption keySepCst rests on, checked rather than assumed:
			// a key with a tab would re-split differently than it was written.
			"negative_key_contains_tab", "negative",
			"tier0\tcmd/a.go | has\ta tab (unused)",
			"", "", errBaselineBadLine,
		},
		{
			// The unsuppressible class. A parse error may never be baselined,
			// mirroring nlparity.DivergenceClass.Suppressible.
			"corner_parse_error_linter_rejected", "corner",
			"tier0\ttier0.json | could not parse JSON: boom (" + parseErrorLinterCst + ")",
			"", "", errBaselineUnsuppressible,
		},
		{
			// Only the linter SUFFIX is the unsuppressible marker; the same text
			// inside a message is a legitimate finding about this tool's own code.
			"corner_parse_error_in_message_allowed", "corner",
			"tier0\ttools/lint-baseline/main.go | mentions " + parseErrorLinterCst + " here (revive)",
			tier0, "tools/lint-baseline/main.go | mentions " + parseErrorLinterCst + " here (revive)", nil,
		},
		{
			"boundary_minimal_key", "boundary",
			"tier0\tx",
			tier0, "x", nil,
		},
		{
			"adversarial_pipe_in_path", "adversarial",
			"tier0\tcmd/we|rd/a.go | msg (revive)",
			tier0, "cmd/we|rd/a.go | msg (revive)", nil,
		},
		{
			// Only the FIRST tab splits, so a key is returned whole; this row
			// pairs with negative_key_contains_tab, which rejects it.
			"adversarial_tier_name_empty", "adversarial",
			"\tcmd/a.go | msg (revive)",
			"", "", errBaselineUnknownTier,
		},
	}
	for _, tc := range cases {
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			gotTier, gotKey, err := parseBaselineLine(tc.line, 1)
			if tc.expectErrIs != nil {
				if err == nil {
					t.Fatalf("parseBaselineLine(%q) = (%s, %q, nil), want error %v",
						tc.line, gotTier, gotKey, tc.expectErrIs)
				}
				if !errors.Is(err, tc.expectErrIs) {
					t.Errorf("parseBaselineLine(%q) error = %v, want %v", tc.line, err, tc.expectErrIs)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseBaselineLine(%q): unexpected error %v", tc.line, err)
			}
			if gotTier != tc.expectTier || gotKey != tc.expectKey {
				t.Errorf("parseBaselineLine(%q) = (%s, %q), want (%s, %q)",
					tc.line, gotTier, gotKey, tc.expectTier, tc.expectKey)
			}
		})
	}
}

// TestDiffTier_table pins the both-direction comparison. `expected` is the full
// pair of slices, not a count: a row asserting only len(added) could not catch
// the added and removed lists being swapped.
func TestDiffTier_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name            string
		category        string
		baselineKeys    []string
		measured        []string
		expectedAdded   []string
		expectedRemoved []string
	}{
		{
			"positive_identical", "positive",
			[]string{"a", "b"}, []string{"a", "b"}, nil, nil,
		},
		{
			"positive_one_removed", "positive",
			[]string{"a", "b"}, []string{"a"}, nil, []string{"b"},
		},
		{
			"negative_one_added", "negative",
			[]string{"a"}, []string{"a", "b"}, []string{"b"}, nil,
		},
		{
			// The case a count cannot see, and the reason both directions are
			// computed even though only `added` can fail.
			"negative_swapped_same_count", "negative",
			[]string{"a"}, []string{"b"}, []string{"b"}, []string{"a"},
		},
		{
			"boundary_empty_baseline_empty_measured", "boundary",
			nil, nil, nil, nil,
		},
		{
			"boundary_empty_baseline_one_measured", "boundary",
			nil, []string{"a"}, []string{"a"}, nil,
		},
		{
			"boundary_baseline_one_measured_empty", "boundary",
			[]string{"a"}, nil, nil, []string{"a"},
		},
		{
			"corner_wholesale_replacement", "corner",
			[]string{"a", "b"}, []string{"c", "d"}, []string{"c", "d"}, []string{"a", "b"},
		},
		{
			// Removed keys come back in sorted order regardless of baseline map
			// iteration order, so the printed report is stable across runs.
			"corner_removed_is_sorted", "corner",
			[]string{"z", "m", "a"}, nil, nil, []string{"a", "m", "z"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			base := &baseline{keys: map[tier]map[string]bool{tier0: {}, tier1: {}, tier2: {}}}
			for _, k := range tc.baselineKeys {
				base.keys[tier0][k] = true
			}
			got := diffTier(tier0, true, tc.measured, base)
			assertKeys(t, "added", got.added, tc.expectedAdded)
			assertKeys(t, "removed", got.removed, tc.expectedRemoved)
		})
	}
}

// assertKeys compares two key slices elementwise, treating nil and empty as the
// same thing — the tool never distinguishes them and a row should not have to.
func assertKeys(t *testing.T, label string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %q, want %q", label, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s = %q, want %q", label, got, want)
		}
	}
}

// TestRefuseToRatchet_table pins the exit-3 guard in isolation. Without this
// behavior the tool certifies a timed-out golangci run as a clean tree, which
// is the one outcome it exists to prevent.
func TestRefuseToRatchet_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		category     string
		codes        []string
		expectedRC   int
		expectedBad  bool
		wantInStderr string
	}{
		{
			"positive_clean_run", "positive",
			[]string{"tier0=0"}, 0, false, "",
		},
		{
			"positive_findings_run", "positive",
			[]string{"tier0=1"}, 0, false, "",
		},
		{
			"positive_no_codes_supplied", "positive",
			nil, 0, false, "",
		},
		{
			// golangci-lint's run.timeout exit, printed after "0 issues."
			"negative_timeout_exit_4", "negative",
			[]string{"tier0=4"}, 3, true, "exited 4",
		},
		{
			"negative_internal_error_exit_3", "negative",
			[]string{"tier0=3"}, 3, true, "exited 3",
		},
		{
			"boundary_exit_2_is_already_too_high", "boundary",
			[]string{"tier0=2"}, 3, true, "exited 2",
		},
		{
			"corner_one_bad_among_good", "corner",
			[]string{"tier0=0", "tier1=4", "tier2=1"}, 3, true, "tier1",
		},
		{
			"corner_non_numeric_is_internal_error", "corner",
			[]string{"tier0=banana"}, 2, true, "is not a number",
		},
		{
			// A negative code cannot come from a real wait status, but accepting
			// it silently would be a hole; it is below the >1 bar so it passes,
			// and this row records that choice rather than leaving it accidental.
			"adversarial_negative_code_passes_the_bar", "adversarial",
			[]string{"tier0=-1"}, 0, false, "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			p := &pairFlag{}
			for _, c := range tc.codes {
				if err := p.Set(c); err != nil {
					t.Fatalf("Set(%q): %v", c, err)
				}
			}
			var stderr bytes.Buffer
			rc, bad := refuseToRatchet(p, &stderr)
			if rc != tc.expectedRC || bad != tc.expectedBad {
				t.Errorf("refuseToRatchet(%q) = (%d, %v), want (%d, %v); stderr=%s",
					tc.codes, rc, bad, tc.expectedRC, tc.expectedBad, stderr.String())
			}
			if tc.wantInStderr != "" && !strings.Contains(stderr.String(), tc.wantInStderr) {
				t.Errorf("stderr missing %q, got: %s", tc.wantInStderr, stderr.String())
			}
		})
	}
}

// TestLoadBaselineHeaderRoundTrip asserts the generated header is itself a
// valid baseline body. It is a separate test because the header is prose that
// a future edit could break: a line in it missing its leading "#" would turn
// documentation into a silently-accepted finding.
func TestLoadBaselineHeaderRoundTrip(t *testing.T) {
	t.Parallel()
	base, err := loadBaseline([]byte(baselineHeaderCst))
	if err != nil {
		t.Fatalf("the generated header must load as an empty baseline: %v", err)
	}
	for _, tr := range allTiers {
		if got := base.sortedKeys(tr); len(got) != 0 {
			t.Errorf("tier %s parsed %d keys out of the header alone: %q", tr, len(got), got)
		}
	}
}
