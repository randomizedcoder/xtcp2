package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureFile is one file written into a row's temp tree, at a path relative to
// the audit root. The path matters as much as the content: three of the rules
// under test are path-scoped.
type fixtureFile struct {
	path    string
	content string
}

// expectedFinding is a finding located by the root-relative path and line that
// produced it, rather than by count. A count cannot tell "the British prose was
// caught" from "the kernel citation was caught by mistake", and those two are
// one wrong character apart.
type expectedFinding struct {
	path string
	line int
}

// writeTree materializes files under a fresh temp directory and returns it.
func writeTree(t *testing.T, files []fixtureFile) string {
	t.Helper()
	root := t.TempDir()
	for _, f := range files {
		full := filepath.Join(root, f.path)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatalf("mkdir for %q: %v", f.path, err)
		}
		if err := os.WriteFile(full, []byte(f.content), 0o600); err != nil {
			t.Fatalf("write %q: %v", f.path, err)
		}
	}
	return root
}

// goFile wraps body in a minimal compilable file, since auditTree parses.
func goFile(body string) string {
	return "package p\n\n" + body
}

// TestAuditTree_table is the audit's behavior table: each row is a tree and the
// exact set of findings it must produce.
//
// `expected` is the whole finding set, located by path and line, in order. A
// row expecting none is as load-bearing as a row expecting one — half these
// rows exist because a rule that fires too often is just as broken as a rule
// that never fires, and the widened misspell exclusion would then have bought
// nothing.
//
// This table uses `description` + `expected`, with the class as the
// description's prefix, which is the convention
// tools/netlink-audit/main_test.go already follows.
func TestAuditTree_table(t *testing.T) {
	tests := []struct {
		description string
		files       []fixtureFile
		expected    []expectedFinding
	}{
		{
			description: "positive: a net/core citation is not a finding, because that is the kernel's own spelling of its own file",
			files: []fixtureFile{{
				path:    "pkg/xtcpnl/nd.go",
				content: goFile("// pneigh_dump_table and neigh_dump_table (net/core/neighbour.c:2956).\nconst A = 1\n"),
			}},
		},
		{
			description: "positive: an include/uapi/linux citation is not a finding either, which is the second of the two prefixes",
			files: []fixtureFile{{
				path:    "pkg/xtcpnl/nd.go",
				content: goFile("// NDA_FLAGS_EXT is from include/uapi/linux/neighbour.h:52.\nconst A = 1\n"),
			}},
		},
		{
			description: "positive: the torvalds/linux URL form is not a finding, because linux/ precedes the word there too",
			files: []fixtureFile{{
				path:    "pkg/xtcpnl/nd.go",
				content: goFile("// Reference: https://github.com/torvalds/linux/blob/master/include/uapi/linux/neighbour.h\nconst A = 1\n"),
			}},
		},
		{
			description: "negative: British prose IS a finding, which is the row that proves widening the misspell exclusion did not open a hole",
			files: []fixtureFile{{
				path:    "pkg/xtcpnl/nd.go",
				content: goFile("// the neighbour table is dumped lazily.\nconst A = 1\n"),
			}},
			expected: []expectedFinding{{path: "pkg/xtcpnl/nd.go", line: 3}},
		},
		{
			description: "negative: a bare filename with no directory prefix is a finding, because a citation is a path and not a name",
			files: []fixtureFile{{
				path:    "pkg/xtcpnl/nd.go",
				content: goFile("// neighbour.c explains the two tables.\nconst A = 1\n"),
			}},
			expected: []expectedFinding{{path: "pkg/xtcpnl/nd.go", line: 3}},
		},
		{
			description: "boundary: the word as the very first token of a comment is a finding, so there is no off-by-one escape at offset zero",
			files: []fixtureFile{{
				path:    "pkg/xtcpnl/nd.go",
				content: goFile("//neighbour handling lives here.\nconst A = 1\n"),
			}},
			expected: []expectedFinding{{path: "pkg/xtcpnl/nd.go", line: 3}},
		},
		{
			description: "boundary: a _test.go file is scanned rather than skipped, unlike in both sibling audits — six of the sixteen real citations are in tests",
			files: []fixtureFile{{
				path:    "pkg/xtcpnl/nd_test.go",
				content: goFile("// the neighbour table is dumped lazily.\nconst A = 1\n"),
			}},
			expected: []expectedFinding{{path: "pkg/xtcpnl/nd_test.go", line: 3}},
		},
		{
			description: "corner: the US spelling is never a finding anywhere, because this tool owns one word in one direction",
			files: []fixtureFile{{
				path:    "pkg/xtcpnl/nd.go",
				content: goFile("// the neighbor table is dumped lazily, and so is its neighbor.\nconst A = 1\n"),
			}},
		},
		{
			description: "corner: a capitalized Neighbour mid-sentence is a finding, since misspell is case-insensitive and this must not become the loophole",
			files: []fixtureFile{{
				path:    "pkg/xtcpnl/nd.go",
				content: goFile("// Neighbour entries arrive unsolicited.\nconst A = 1\n"),
			}},
			expected: []expectedFinding{{path: "pkg/xtcpnl/nd.go", line: 3}},
		},
		{
			description: "corner: a .pb.go file is skipped, as the sibling audits do, because its prose is generated and not ours to edit",
			files: []fixtureFile{{
				path:    "gen/go/thing.pb.go",
				content: goFile("// the neighbour table is dumped lazily.\nconst A = 1\n"),
			}},
		},
		{
			description: "corner: this tool's own directory is skipped, which is what keeps a correct repo from reporting the findings inside the tool that reports them",
			files: []fixtureFile{{
				path:    "tools/kernel-citation-audit/other.go",
				content: goFile("// the neighbour table is dumped lazily.\nconst A = 1\n"),
			}},
		},
		{
			description: "adversarial: the word inside a string literal is a finding in a file with no exemption, so prose cannot be laundered through a quote",
			files: []fixtureFile{{
				path:    "pkg/xtcpnl/nd.go",
				content: goFile("var s = \"neighbour\"\n"),
			}},
			expected: []expectedFinding{{path: "pkg/xtcpnl/nd.go", line: 3}},
		},
		{
			description: "adversarial: the same literal in internal/goip/dispatch.go is exempt, because there it is the object name a user types and iproute2 accepts",
			files: []fixtureFile{{
				path:    "internal/goip/dispatch.go",
				content: goFile("var table = []string{\"neighbour\"}\n"),
			}},
		},
		{
			description: "adversarial: unquoted British prose in internal/goip/dispatch.go IS still a finding, so the argv exemption is not a file-level blanket",
			files: []fixtureFile{{
				path:    "internal/goip/dispatch.go",
				content: goFile("// the neighbour table is dumped lazily.\nvar table = []string{\"neighbour\"}\n"),
			}},
			expected: []expectedFinding{{path: "internal/goip/dispatch.go", line: 3}},
		},
		{
			description: "corner: obj_neigh_test.go keeps its citation and its argv while its prose is still checked, which is the distinction a file-level exemption would have lost",
			files: []fixtureFile{{
				path: "internal/goip/obj_neigh_test.go",
				content: goFile("// (net/core/neighbour.c:2955-2957) are disjoint tables.\n" +
					"// the neighbour table is dumped lazily.\n" +
					"var args = []string{\"neighbour\", \"show\"}\n"),
			}},
			expected: []expectedFinding{{path: "internal/goip/obj_neigh_test.go", line: 4}},
		},
		{
			description: "boundary: two findings in one file are reported in source order, since the report is sorted by offset and not by map iteration",
			files: []fixtureFile{{
				path: "pkg/xtcpnl/nd.go",
				content: goFile("// the neighbour table is dumped lazily.\n" +
					"// (net/core/neighbour.c:2956) is fine.\n" +
					"// a second neighbour in prose.\nconst A = 1\n"),
			}},
			expected: []expectedFinding{
				{path: "pkg/xtcpnl/nd.go", line: 3},
				{path: "pkg/xtcpnl/nd.go", line: 5},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			root := writeTree(t, tc.files)
			findings, err := auditTree(root)
			if err != nil {
				t.Fatalf("auditTree: %v", err)
			}
			if len(findings) != len(tc.expected) {
				t.Fatalf("got %d finding(s), want %d:\n%s", len(findings), len(tc.expected), formatFindings(root, findings))
			}
			for i, want := range tc.expected {
				gotPath, relErr := filepath.Rel(root, findings[i].pos.Filename)
				if relErr != nil {
					t.Fatalf("relativise %q: %v", findings[i].pos.Filename, relErr)
				}
				if filepath.ToSlash(gotPath) != want.path || findings[i].pos.Line != want.line {
					t.Fatalf("finding %d at %s:%d, want %s:%d", i, gotPath, findings[i].pos.Line, want.path, want.line)
				}
			}
		})
	}
}

// formatFindings renders findings with root-relative paths, for failure output
// that can be read without counting temp-directory components.
func formatFindings(root string, findings []finding) string {
	var b strings.Builder
	for _, f := range findings {
		rel, err := filepath.Rel(root, f.pos.Filename)
		if err != nil {
			rel = f.pos.Filename
		}
		fmt.Fprintf(&b, "  %s:%d (%s) %s\n", filepath.ToSlash(rel), f.pos.Line, f.kind, f.msg)
	}
	if b.Len() == 0 {
		return "  (none)\n"
	}
	return b.String()
}

// TestRunMain_table covers the 0/1/2 exit contract and the one line the quality
// report greps for, which is the part of this tool's interface that other
// things depend on.
//
// This table uses `description` + `expected`.
func TestRunMain_table(t *testing.T) {
	tests := []struct {
		description string
		files       []fixtureFile
		// args are appended after -root, which every row needs.
		extraArgs []string
		// useMissingRoot replaces the temp root with a path that does not
		// exist, which is the walk-error path rather than a finding.
		useMissingRoot bool
		expectedExit   int
		expectedStdout string
		expectedStderr string
	}{
		{
			description: "positive: a clean tree exits 0 and says so in the line the quality report greps for",
			files: []fixtureFile{{
				path:    "pkg/xtcpnl/nd.go",
				content: goFile("// net/core/neighbour.c:2956 is the table.\nconst A = 1\n"),
			}},
			expectedExit:   0,
			expectedStdout: "kernel-citation-audit: no findings",
		},
		{
			description: "negative: a finding exits 1, names the position on stdout and the count on stderr",
			files: []fixtureFile{{
				path:    "pkg/xtcpnl/nd.go",
				content: goFile("// the neighbour table is dumped lazily.\nconst A = 1\n"),
			}},
			expectedExit:   1,
			expectedStdout: "nd.go:3:",
			expectedStderr: "kernel-citation-audit: 1 finding(s)",
		},
		{
			description:  "negative: an unparseable flag exits 2 rather than reporting a clean tree, so a mistyped invocation cannot read as a pass",
			extraArgs:    []string{"-nosuchflag"},
			expectedExit: 2,
		},
		{
			description:    "corner: a root that does not exist exits 2, because an unreadable tree is not an audited tree",
			useMissingRoot: true,
			expectedExit:   2,
			expectedStderr: "kernel-citation-audit: walk failed",
		},
		{
			description: "boundary: an unparseable Go file exits 2 rather than skipping the file, so a syntax error cannot hide prose from the audit",
			files: []fixtureFile{{
				path:    "pkg/xtcpnl/nd.go",
				content: "package p\n\nfunc broken( {\n",
			}},
			expectedExit:   2,
			expectedStderr: "kernel-citation-audit: walk failed",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			root := writeTree(t, tc.files)
			if tc.useMissingRoot {
				root = filepath.Join(root, "nope")
			}
			args := append([]string{"-root", root}, tc.extraArgs...)

			var stdout, stderr bytes.Buffer
			got := runMain(args, &stdout, &stderr)

			if got != tc.expectedExit {
				t.Fatalf("runMain = %d, want %d\nstdout: %s\nstderr: %s", got, tc.expectedExit, stdout.String(), stderr.String())
			}
			if tc.expectedStdout != "" && !strings.Contains(stdout.String(), tc.expectedStdout) {
				t.Fatalf("stdout = %q, want it to contain %q", stdout.String(), tc.expectedStdout)
			}
			if tc.expectedStderr != "" && !strings.Contains(stderr.String(), tc.expectedStderr) {
				t.Fatalf("stderr = %q, want it to contain %q", stderr.String(), tc.expectedStderr)
			}
		})
	}
}

// TestRepoIsClean audits the real tree, which is the assertion that makes the
// widened misspell exclusion honest: every one of the sixteen occurrences the
// exclusion stops misspell from reporting is a kernel citation or a quoted
// command token, and this fails the moment one of them stops being either.
//
// Modeled on nlparity_allowlist_test.go, which checks the committed allowlist
// against the real tree for the same reason.
func TestRepoIsClean(t *testing.T) {
	// The test binary runs in the package directory, so the repo root is two
	// levels up from tools/kernel-citation-audit.
	root := filepath.Join("..", "..")
	findings, err := auditTree(root)
	if err != nil {
		t.Fatalf("auditTree(%s): %v", root, err)
	}
	if len(findings) != 0 {
		t.Fatalf("the repo has %d non-citation occurrence(s):\n%s", len(findings), formatFindings(root, findings))
	}
}
