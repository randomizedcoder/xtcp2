package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// main_test.go drives runMain end to end: real files on disk, real flag
// parsing, real exit codes. helpers_test.go pins the unit-level contracts.
//
// Rows use sentence-style `description` fields with the class prefixed, as
// tools/netlink-audit/main_test.go does. The sibling helpers_test.go in this
// directory follows the OTHER local convention (name + category with
// t.Run(category+"/"+name)) because that is what tools/netlink-audit's
// helpers_test.go does, and matching the file beats matching the directory.

// issue is one golangci finding as it appears in the JSON, before
// normalization. Line and col are carried precisely so a row can prove they
// are dropped.
type issue struct {
	path   string
	line   int
	col    int
	text   string
	linter string
}

// issuesJSON renders issues the way golangci-lint's JSON output does. Built
// through encoding/json rather than string concatenation so a row holding a
// quote or a backslash in a path cannot produce malformed input and pass for
// the wrong reason.
func issuesJSON(issues ...issue) string {
	type pos struct {
		Filename string
		Line     int
		Column   int
	}
	type entry struct {
		FromLinter string
		Text       string
		Pos        pos
	}
	doc := struct{ Issues []entry }{Issues: make([]entry, 0, len(issues))}
	for _, is := range issues {
		doc.Issues = append(doc.Issues, entry{
			FromLinter: is.linter,
			Text:       is.text,
			Pos:        pos{Filename: is.path, Line: is.line, Column: is.col},
		})
	}
	out, err := json.Marshal(doc)
	if err != nil {
		panic(fmt.Sprintf("issuesJSON: %v", err))
	}
	return string(out)
}

// baselineOf renders a baseline file body from already-normalized keys, with
// the same header the tool writes, so a row exercises the comment-skipping
// path as well as the data.
func baselineOf(lines ...string) string {
	var b strings.Builder
	b.WriteString("# test baseline\n#\n")
	for _, l := range lines {
		b.WriteString(l)
		b.WriteString("\n")
	}
	return b.String()
}

// tierInput is one tier's contribution to a row: the JSON its run produced and,
// optionally, the exit code that run returned.
type tierInput struct {
	// json is written to a file and passed via -findings. A tier absent from
	// the row's map gets no -findings flag at all, which is how a row covers
	// one tier without saying anything about the other two.
	json string
	// exit is passed via -exit when non-empty. Empty means the caller did not
	// report the tier's exit code, which is the pre-ratchet-guard state.
	exit string
}

// expectation is the whole observable outcome of one runMain call: the exit
// code plus what the two streams must and must not contain. A row asserting
// only the code could not tell "exit 1 for the right finding" from "exit 1 for
// some other finding".
type expectation struct {
	code           int
	wantStdout     []string
	wantStderr     []string
	notWantStdout  []string
	wantAddedCount int // asserted via the "N added" summary line when >= 0
}

// writeFindings materializes one tier's JSON and returns its path.
func writeFindings(t *testing.T, dir string, tr tier, body string) string {
	t.Helper()
	path := filepath.Join(dir, string(tr)+".json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write findings: %v", err)
	}
	return path
}

func TestRunMain(t *testing.T) {
	// Keys reused across rows. Spelled out rather than built by normalizeKey so
	// the table states the exact on-disk text and a change to normalizeKey's
	// format shows up here as a failure rather than being tracked silently.
	const (
		keyA = "cmd/a/main.go | A is unused (unused)"
		keyB = "pkg/b/b.go | B shadows the builtin (gocritic)"
	)

	tests := []struct {
		description string
		// baseline is the file body. nil means the file is never created, which
		// is a different state from an empty one.
		baseline *string
		inputs   map[tier]tierInput
		gated    string
		expected expectation
	}{
		{
			description: "positive: findings identical to the baseline are clean in both directions",
			baseline:    ptr(baselineOf("tier0\t" + keyA)),
			inputs: map[tier]tierInput{
				tier0: {json: issuesJSON(issue{"cmd/a/main.go", 12, 3, "A is unused", "unused"})},
			},
			gated: "tier0",
			expected: expectation{
				code:           0,
				wantStdout:     []string{"0 added, 0 removed", "no added findings"},
				wantAddedCount: 0,
			},
		},
		{
			description: "positive: a finding removed relative to the baseline is an improvement and never fails",
			baseline:    ptr(baselineOf("tier0\t"+keyA, "tier0\t"+keyB)),
			inputs: map[tier]tierInput{
				tier0: {json: issuesJSON(issue{"cmd/a/main.go", 12, 3, "A is unused", "unused"})},
			},
			gated: "tier0",
			expected: expectation{
				code:           0,
				wantStdout:     []string{"0 added, 1 removed", "- " + keyB},
				wantAddedCount: 0,
			},
		},
		{
			description: "negative: a finding added in a gated tier fails and the output names the added key",
			baseline:    ptr(baselineOf("tier0\t" + keyA)),
			inputs: map[tier]tierInput{
				tier0: {json: issuesJSON(
					issue{"cmd/a/main.go", 12, 3, "A is unused", "unused"},
					issue{"pkg/b/b.go", 7, 1, "B shadows the builtin", "gocritic"},
				)},
			},
			gated: "tier0",
			expected: expectation{
				code:           1,
				wantStdout:     []string{"[GATED]", "+ " + keyB},
				wantStderr:     []string{"a gated tier gained findings"},
				wantAddedCount: 1,
			},
		},
		{
			description: "negative: a finding added in an advisory tier is printed but does not fail, so the next phase can still read its delta",
			baseline:    ptr(baselineOf("tier1\t" + keyA)),
			// tier0 is gated, so it is also supplied — clean and empty, which is
			// the real shape of this phase's invocation. Gating a tier that was
			// never measured is itself an error, so the row cannot omit it.
			inputs: map[tier]tierInput{
				tier0: {json: issuesJSON()},
				tier1: {json: issuesJSON(
					issue{"cmd/a/main.go", 12, 3, "A is unused", "unused"},
					issue{"pkg/b/b.go", 7, 1, "B shadows the builtin", "gocritic"},
				)},
			},
			gated: "tier0",
			expected: expectation{
				code:           0,
				wantStdout:     []string{"[advisory]", "+ " + keyB, "no added findings"},
				wantAddedCount: 1,
			},
		},
		{
			description: "negative: one finding swapped for another with the count unchanged still fails, which is the case a count-based bar cannot see",
			baseline:    ptr(baselineOf("tier0\t" + keyA)),
			inputs: map[tier]tierInput{
				tier0: {json: issuesJSON(issue{"pkg/b/b.go", 7, 1, "B shadows the builtin", "gocritic"})},
			},
			gated: "tier0",
			expected: expectation{
				code:           1,
				wantStdout:     []string{"1 finding(s), 1 added, 1 removed", "+ " + keyB, "- " + keyA},
				wantAddedCount: 1,
			},
		},
		{
			description: "negative: a missing baseline file is an internal error, not a silent pass",
			baseline:    nil,
			inputs: map[tier]tierInput{
				tier0: {json: issuesJSON()},
			},
			gated: "tier0",
			expected: expectation{
				code:       2,
				wantStderr: []string{"baseline file does not exist"},
			},
		},
		{
			description: "negative: an unparseable baseline line fails closed rather than being skipped",
			baseline:    ptr("# header\nthis line has no tab at all\n"),
			inputs: map[tier]tierInput{
				tier0: {json: issuesJSON()},
			},
			gated: "tier0",
			expected: expectation{
				code:       2,
				wantStderr: []string{"is not <tier>"},
			},
		},
		{
			description: "negative: a baseline line naming an unknown tier fails closed, so a typo cannot create a fourth tier nothing gates",
			baseline:    ptr(baselineOf("tier9\t" + keyA)),
			inputs: map[tier]tierInput{
				tier0: {json: issuesJSON()},
			},
			gated: "tier0",
			expected: expectation{
				code:       2,
				wantStderr: []string{"unknown tier"},
			},
		},
		{
			description: "negative: a tier whose JSON cannot be decoded yields an unsuppressible parse-error finding, so a gated tier goes red instead of passing on unreadable output",
			baseline:    ptr(baselineOf()),
			inputs: map[tier]tierInput{
				tier0: {json: "{not json at all"},
			},
			gated: "tier0",
			expected: expectation{
				code:           1,
				wantStdout:     []string{parseErrorLinterCst},
				wantAddedCount: 1,
			},
		},
		{
			description: "boundary: an empty baseline with zero findings is clean",
			baseline:    ptr(baselineOf()),
			inputs: map[tier]tierInput{
				tier0: {json: issuesJSON()},
			},
			gated: "tier0",
			expected: expectation{
				code:           0,
				wantStdout:     []string{"0 finding(s), 0 added, 0 removed"},
				wantAddedCount: 0,
			},
		},
		{
			description: "boundary: an empty baseline with one finding fails, which is the state every tier starts in before -update",
			baseline:    ptr(baselineOf()),
			inputs: map[tier]tierInput{
				tier0: {json: issuesJSON(issue{"cmd/a/main.go", 12, 3, "A is unused", "unused"})},
			},
			gated: "tier0",
			expected: expectation{
				code:           1,
				wantStdout:     []string{"+ " + keyA},
				wantAddedCount: 1,
			},
		},
		{
			description: "boundary: a finding whose line and column moved but whose text did not is unchanged, because the key drops line:col",
			baseline:    ptr(baselineOf("tier0\t" + keyA)),
			inputs: map[tier]tierInput{
				// Same path, text and linter as keyA; line 12 -> 4096, col 3 -> 77.
				tier0: {json: issuesJSON(issue{"cmd/a/main.go", 4096, 77, "A is unused", "unused"})},
			},
			gated: "tier0",
			expected: expectation{
				code:           0,
				wantStdout:     []string{"1 finding(s), 0 added, 0 removed"},
				notWantStdout:  []string{"+ "},
				wantAddedCount: 0,
			},
		},
		{
			description: "corner: a tier that exited 4 (golangci run.timeout, printed after \"0 issues.\") refuses to ratchet even though its findings match the baseline",
			baseline:    ptr(baselineOf()),
			inputs: map[tier]tierInput{
				tier0: {json: issuesJSON(), exit: "4"},
			},
			gated: "tier0",
			expected: expectation{
				code:          3,
				wantStderr:    []string{"refusing to ratchet", "exited 4", "golangci-lint-quick"},
				notWantStdout: []string{"no added findings"},
			},
		},
		{
			description: "corner: a tier that exited 1 is a normal findings run and does ratchet",
			baseline:    ptr(baselineOf("tier0\t" + keyA)),
			inputs: map[tier]tierInput{
				tier0: {json: issuesJSON(issue{"cmd/a/main.go", 12, 3, "A is unused", "unused"}), exit: "1"},
			},
			gated: "tier0",
			expected: expectation{
				code:           0,
				wantStdout:     []string{"no added findings"},
				wantAddedCount: 0,
			},
		},
		{
			description: "corner: duplicate baseline lines fail closed, because nothing in the file would say which copy applied",
			baseline:    ptr(baselineOf("tier0\t"+keyA, "tier0\t"+keyA)),
			inputs: map[tier]tierInput{
				tier0: {json: issuesJSON()},
			},
			gated: "tier0",
			expected: expectation{
				code:       2,
				wantStderr: []string{"duplicate line"},
			},
		},
		{
			description: "corner: an out-of-order baseline fails closed, so a line cannot be hidden by moving it",
			baseline:    ptr(baselineOf("tier0\t"+keyB, "tier0\t"+keyA)),
			inputs: map[tier]tierInput{
				tier0: {json: issuesJSON()},
			},
			gated: "tier0",
			expected: expectation{
				code:       2,
				wantStderr: []string{"not sorted"},
			},
		},
		{
			description: "corner: a baseline line carrying the internal/parse-error linter fails closed, because a broken-output finding may never be suppressed",
			baseline:    ptr(baselineOf("tier0\ttier0.json | could not parse JSON: boom (" + parseErrorLinterCst + ")")),
			inputs: map[tier]tierInput{
				tier0: {json: issuesJSON()},
			},
			gated: "tier0",
			expected: expectation{
				code:       2,
				wantStderr: []string{"unsuppressible"},
			},
		},
		{
			description: "adversarial: leading garbage before the opening brace is tolerated, as parseGolangci already does",
			baseline:    ptr(baselineOf("tier0\t" + keyA)),
			inputs: map[tier]tierInput{
				tier0: {json: "level=warning msg=\"something on the stream\"\n" +
					issuesJSON(issue{"cmd/a/main.go", 12, 3, "A is unused", "unused"})},
			},
			gated: "tier0",
			expected: expectation{
				code:           0,
				wantStdout:     []string{"1 finding(s), 0 added, 0 removed"},
				wantAddedCount: 0,
			},
		},
		{
			description: "adversarial: a path containing the pipe that appears inside the key round-trips and matches, rather than splitting into the wrong key",
			baseline:    ptr(baselineOf("tier0\tcmd/we|rd/ma|n.go | A is unused (unused)")),
			inputs: map[tier]tierInput{
				tier0: {json: issuesJSON(issue{"cmd/we|rd/ma|n.go", 12, 3, "A is unused", "unused"})},
			},
			gated: "tier0",
			expected: expectation{
				code:           0,
				wantStdout:     []string{"1 finding(s), 0 added, 0 removed"},
				notWantStdout:  []string{"+ "},
				wantAddedCount: 0,
			},
		},
		{
			description: "adversarial: a baseline key containing a tab is rejected, since it would re-split differently than it was written",
			baseline:    ptr(baselineOf("tier0\tcmd/a/main.go | A is\tunused (unused)")),
			inputs: map[tier]tierInput{
				tier0: {json: issuesJSON()},
			},
			gated: "tier0",
			expected: expectation{
				code:       2,
				wantStderr: []string{"key contains a tab"},
			},
		},
		{
			// The fail-open this tool would otherwise have, and the worst one
			// available to it: no -findings for a gated tier is an EMPTY
			// findings set, so every baseline line reads as removed, removals
			// never fail, and the tier exits 0 while being nominally gated and
			// actually unchecked. The next row is the same output shape arrived
			// at honestly, which is why the guard tests for a MEASUREMENT and
			// not for a non-empty list.
			description: "negative: a tier that is gated but was given no -findings input is an error, not a vacuous pass",
			baseline:    ptr(baselineOf("tier0\t"+keyA, "tier1\t"+keyB)),
			inputs: map[tier]tierInput{
				tier0: {json: issuesJSON(issue{"cmd/a/main.go", 12, 3, "A is unused", "unused"})},
			},
			gated: "tier0,tier1",
			expected: expectation{
				code:       2,
				wantStderr: []string{"gated but has no -findings input", "tier1"},
			},
		},
		{
			description: "corner: a gated tier measured as entirely clean against a populated baseline reports every line removed and still passes",
			baseline:    ptr(baselineOf("tier0\t"+keyA, "tier0\t"+keyB)),
			inputs: map[tier]tierInput{
				tier0: {json: issuesJSON()},
			},
			gated: "tier0",
			expected: expectation{
				code:           0,
				wantStdout:     []string{"0 finding(s), 0 added, 2 removed", "- " + keyA, "- " + keyB},
				wantAddedCount: 0,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			dir := t.TempDir()
			baselinePath := filepath.Join(dir, "lint-baseline.txt")
			if tc.baseline != nil {
				if err := os.WriteFile(baselinePath, []byte(*tc.baseline), 0o600); err != nil {
					t.Fatalf("write baseline: %v", err)
				}
			}

			args := []string{"-baseline", baselinePath}
			if tc.gated != "" {
				args = append(args, "-gated", tc.gated)
			}
			// allTiers rather than a map range, so argument order is
			// deterministic and a failure message is reproducible.
			for _, tr := range allTiers {
				in, ok := tc.inputs[tr]
				if !ok {
					continue
				}
				args = append(args, "-findings", string(tr)+"="+writeFindings(t, dir, tr, in.json))
				if in.exit != "" {
					args = append(args, "-exit", string(tr)+"="+in.exit)
				}
			}

			var stdout, stderr bytes.Buffer
			code := runMain(args, &stdout, &stderr)
			if code != tc.expected.code {
				t.Errorf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s",
					code, tc.expected.code, stdout.String(), stderr.String())
			}
			for _, want := range tc.expected.wantStdout {
				if !strings.Contains(stdout.String(), want) {
					t.Errorf("stdout missing %q\ngot:\n%s", want, stdout.String())
				}
			}
			for _, want := range tc.expected.wantStderr {
				if !strings.Contains(stderr.String(), want) {
					t.Errorf("stderr missing %q\ngot:\n%s", want, stderr.String())
				}
			}
			for _, unwanted := range tc.expected.notWantStdout {
				if strings.Contains(stdout.String(), unwanted) {
					t.Errorf("stdout should not contain %q\ngot:\n%s", unwanted, stdout.String())
				}
			}
			if tc.expected.code == 0 || tc.expected.code == 1 {
				want := fmt.Sprintf("%d added", tc.expected.wantAddedCount)
				if !strings.Contains(stdout.String(), want) {
					t.Errorf("stdout missing summary %q\ngot:\n%s", want, stdout.String())
				}
			}
		})
	}
}

// TestRunMainUpdate covers -update, the tool's only mutating path. It is its
// own function rather than a row above because its outcome is a file on disk
// rather than an exit code, and because it must be shown to refuse to run when
// the measurement it would write is untrustworthy.
func TestRunMainUpdate(t *testing.T) {
	tests := []struct {
		description   string
		exit          string
		expectedCode  int
		expectedLines []string
		expectWritten bool
	}{
		{
			description:   "positive: -update writes a sorted baseline that then loads and compares clean",
			exit:          "1",
			expectedCode:  0,
			expectedLines: []string{"tier0\tcmd/a/main.go | A is unused (unused)"},
			expectWritten: true,
		},
		{
			description:   "corner: -update on a tier that exited 4 refuses to write, because baking a timed-out run is the worst thing this tool could do",
			exit:          "4",
			expectedCode:  3,
			expectWritten: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			dir := t.TempDir()
			baselinePath := filepath.Join(dir, "lint-baseline.txt")
			body := issuesJSON(issue{"cmd/a/main.go", 12, 3, "A is unused", "unused"})
			args := []string{
				"-baseline", baselinePath,
				"-findings", "tier0=" + writeFindings(t, dir, tier0, body),
				"-exit", "tier0=" + tc.exit,
				"-update",
			}
			var stdout, stderr bytes.Buffer
			if code := runMain(args, &stdout, &stderr); code != tc.expectedCode {
				t.Fatalf("exit code = %d, want %d; stderr=%s", code, tc.expectedCode, stderr.String())
			}

			written, err := os.ReadFile(baselinePath)
			if !tc.expectWritten {
				if err == nil {
					t.Fatalf("baseline should not have been written, got:\n%s", written)
				}
				return
			}
			if err != nil {
				t.Fatalf("read written baseline: %v", err)
			}
			for _, want := range tc.expectedLines {
				if !strings.Contains(string(written), want) {
					t.Errorf("written baseline missing %q\ngot:\n%s", want, written)
				}
			}
			// The round trip is the real assertion: whatever -update writes must
			// satisfy the loader's sorted/unique/known-tier rules, or the tool
			// would generate files it then rejects.
			if _, err := loadBaseline(written); err != nil {
				t.Errorf("written baseline does not load: %v", err)
			}
			var stdout2, stderr2 bytes.Buffer
			code := runMain([]string{
				"-baseline", baselinePath,
				"-findings", "tier0=" + writeFindings(t, dir, tier0, body),
				"-gated", "tier0",
			}, &stdout2, &stderr2)
			if code != 0 {
				t.Errorf("re-run against the written baseline = %d, want 0\nstdout:\n%s\nstderr:\n%s",
					code, stdout2.String(), stderr2.String())
			}
		})
	}
}

// TestRunMainFlagErrors pins that bad flags are exit 2 and never a pass.
func TestRunMainFlagErrors(t *testing.T) {
	tests := []struct {
		description string
		args        []string
		expected    int
	}{
		{
			description: "negative: an unrecognized flag is an internal error",
			args:        []string{"-not-a-flag"},
			expected:    2,
		},
		{
			description: "negative: a -findings value without an equals sign is an internal error",
			args:        []string{"-findings", "tier0"},
			expected:    2,
		},
		{
			description: "negative: a -findings value naming an unknown tier is an internal error",
			args:        []string{"-findings", "tier7=/dev/null"},
			expected:    2,
		},
		{
			description: "negative: an unknown tier in -gated is an internal error",
			args:        []string{"-gated", "tier7"},
			expected:    2,
		},
		{
			description: "negative: a non-numeric -exit value is an internal error rather than being treated as zero",
			args:        []string{"-exit", "tier0=banana"},
			expected:    2,
		},
		{
			description: "negative: a -findings path that does not exist is an internal error, because the check itself is broken",
			args:        []string{"-findings", "tier0=/nonexistent/findings.json"},
			expected:    2,
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := runMain(tc.args, &stdout, &stderr); code != tc.expected {
				t.Errorf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s",
					code, tc.expected, stdout.String(), stderr.String())
			}
		})
	}
}

// TestLintBaselineCommitted checks the real docs/lint-baseline.txt, modeled on
// TestAllowlistCommitted in pkg/nlparity/nlparity_allowlist_test.go: the
// committed artifact is validated by a test rather than by the reviewer
// remembering to.
//
// The assertions on the gated tiers are the ones that matter. Tiers 0 and 1 are
// gated, and gating a tier that still has findings would make the check
// permanently red, which is the same as turning it off — the discipline
// nix/checks/default.nix already records for proto-audit-netlink's
// gatedProtocols.
//
// A promoted tier's ceiling comes down to 0 in the same commit as the
// promotion. Otherwise this test keeps certifying a baseline allowed to hold
// lines for a tier the build now gates, and a line in a gated tier's section is
// an accepted finding — a decision that belongs in a diff, not in a ceiling
// nobody lowered.
func TestLintBaselineCommitted(t *testing.T) {
	const path = "../../docs/lint-baseline.txt"
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the committed baseline must exist and be readable: %v", err)
	}
	base, err := loadBaseline(data)
	if err != nil {
		t.Fatalf("the committed baseline must load: %v", err)
	}

	// expectedMax is a CEILING, not an equality, and it is the only thing
	// enforcing the header's claim that the file "only ever goes DOWN". An
	// advisory tier is allowed to shrink freely as a phase lands fixes; growing
	// requires editing the number here, which puts the decision in the diff
	// next to the lines it admits. A gated tier's ceiling is 0, which makes it
	// an equality by construction.
	//
	// These are KEY counts, not finding counts: keys drop line:col, so findings
	// of one class in one file collapse to a single line. That mattered while
	// the tiers were full — 26 and 30 findings were 19 and 23 lines, because
	// three files held the same flagged spelling twice and errcheck's 12
	// collapsed to 8 — and it does not today, since tier2's three remaining
	// findings are three distinct keys. Note they are all in ONE file, which is
	// the case the collapse would bite: three funlen findings in cmd/xtcp2.go
	// stay three keys only because their messages name different functions.
	// See normalizeKey.
	tests := []struct {
		description string
		tier        tier
		expectedMax int
	}{
		{
			description: "positive: the gated tier0 section is empty, so the gate is honest rather than permanently red",
			tier:        tier0,
			expectedMax: 0,
		},
		{
			description: "positive: the gated tier1 section is empty too, which is what its promotion on 2026-10-06 asserted and what this ceiling now holds it to",
			tier:        tier1,
			expectedMax: 0,
		},
		{
			description: "boundary: advisory tier2 holds no more than the 3 keys measured when the baseline was last regenerated — funlen x3, all in cmd/xtcp2, with gocyclo gone from here because it is gated in tier1 now",
			tier:        tier2,
			expectedMax: 3,
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := base.sortedKeys(tc.tier)
			if len(got) > tc.expectedMax {
				t.Errorf("tier %s has %d keys, want at most %d; a baseline only ever goes down, "+
					"so either fix the new finding or raise this ceiling in the same reviewed commit:\n%s",
					tc.tier, len(got), tc.expectedMax, strings.Join(got, "\n"))
			}
		})
	}
}

// ptr is a tiny helper so a table row can distinguish "an empty baseline file"
// from "no baseline file at all" without a second bool.
func ptr(s string) *string { return &s }

// BenchmarkLoadBaseline measures the validating loader against a baseline the
// size this repo would have if no phase had run: every one of the 46 findings
// in all three tiers.
func BenchmarkLoadBaseline(b *testing.B) {
	var sb strings.Builder
	sb.WriteString(baselineHeaderCst)
	for _, tr := range allTiers {
		for i := 0; i < 46; i++ {
			fmt.Fprintf(&sb, "%s\tpkg/p%03d/file.go | finding number %03d (staticcheck)\n", tr, i, i)
		}
	}
	data := []byte(sb.String())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := loadBaseline(data); err != nil {
			b.Fatalf("loadBaseline: %v", err)
		}
	}
}

// BenchmarkDiffTier measures the both-direction comparison, which is the part
// that runs once per tier on every check.
func BenchmarkDiffTier(b *testing.B) {
	base := &baseline{keys: map[tier]map[string]bool{tier0: {}, tier1: {}, tier2: {}}}
	measured := make([]string, 0, 46)
	for i := 0; i < 46; i++ {
		key := fmt.Sprintf("pkg/p%03d/file.go | finding number %03d (staticcheck)", i, i)
		measured = append(measured, key)
		// Half the measured findings are in the baseline, so the benchmark
		// walks both the added and the unchanged path rather than one of them.
		if i%2 == 0 {
			base.keys[tier0][key] = true
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d := diffTier(tier0, true, measured, base)
		if len(d.added) == 0 {
			b.Fatal("expected added findings")
		}
	}
}
