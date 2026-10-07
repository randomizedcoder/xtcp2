package main

import (
	"strings"
	"testing"
)

// This file tables the predicates individually, where main_test.go tables the
// audit end to end. Both use `description` + `expected`, with the class as the
// description's prefix. The sibling audits' helpers_test.go files use `name` +
// `category` + `want` instead; these are new files, so they follow the current
// standard rather than inheriting that one.

// TestIsKernelCitation_table covers the rule the whole tool rests on: the word
// is licensed by a kernel path immediately before it, and by nothing else.
//
// Each row gives the text and finds the word's index itself, so no row can pass
// by pointing the predicate at the wrong offset.
func TestIsKernelCitation_table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		description string
		text        string
		expected    bool
	}{
		{
			description: "positive: net/core/ immediately before the word is a citation",
			text:        "// pneigh_dump_table (net/core/neighbour.c:2956)",
			expected:    true,
		},
		{
			description: "positive: include/uapi/linux/ is a citation, matching on its linux/ component",
			text:        "// NDA_FLAGS_EXT from include/uapi/linux/neighbour.h:52",
			expected:    true,
		},
		{
			description: "positive: the torvalds URL is a citation, because linux/ precedes the word there as well",
			text:        "// https://github.com/torvalds/linux/blob/master/include/uapi/linux/neighbour.h",
			expected:    true,
		},
		{
			description: "positive: a bare linux/ opening the text is a citation, since a prefix at offset zero has no preceding character to disqualify it",
			text:        "linux/neighbour.h",
			expected:    true,
		},
		{
			description: "negative: British prose is not a citation",
			text:        "// the neighbour table is dumped lazily",
			expected:    false,
		},
		{
			description: "negative: a citation mentioned earlier in the same sentence does not license a later occurrence, which is why the prefix must be immediate",
			text:        "// net/core/ holds the code, and the neighbour table is lazy",
			expected:    false,
		},
		{
			description: "negative: a bare filename is not a citation, because a citation is a path",
			text:        "// neighbour.c explains the two tables",
			expected:    false,
		},
		{
			description: "boundary: the word at offset zero is not a citation, there being nothing before it",
			text:        "neighbour.c",
			expected:    false,
		},
		{
			description: "boundary: a separator directly before the prefix is a real component boundary",
			text:        "// see include/uapi/linux/neighbour.h",
			expected:    true,
		},
		{
			description: "adversarial: notlinux/ is not a kernel path, and a plain suffix match would have accepted it",
			text:        "// third_party/notlinux/neighbour.h",
			expected:    false,
		},
		{
			description: "adversarial: xnet/core/ is not a kernel path either, for the same reason",
			text:        "// vendor/xnet/core/neighbour.c",
			expected:    false,
		},
		{
			description: "corner: a hyphen before the prefix is inside a component, so my-linux/ does not count",
			text:        "// forks/my-linux/neighbour.h",
			expected:    false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Parallel()
			idx := strings.Index(foldASCII(tc.text), auditedWordCst)
			if idx < 0 {
				t.Fatalf("the row's text does not contain the audited word: %q", tc.text)
			}
			if got := isKernelCitation(tc.text, idx); got != tc.expected {
				t.Fatalf("isKernelCitation(%q, %d) = %v, want %v", tc.text, idx, got, tc.expected)
			}
		})
	}
}

// TestIsQuotedCommandToken_table covers the second half of the argv exemption.
// Quoting alone licenses nothing — the file still has to be in
// argvExemptions — but inside those three files it is what separates the
// command word a user types from prose about it.
func TestIsQuotedCommandToken_table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		description string
		text        string
		expected    bool
	}{
		{
			description: "positive: a Go string literal's own opening quote counts, which is the form in the alias table and the argv row",
			text:        `"neighbour"`,
			expected:    true,
		},
		{
			description: "positive: a backtick counts, which is the form in the alias-pair comment and the test description",
			text:        "// `neighbour show` spells the object out",
			expected:    true,
		},
		{
			description: "negative: a preceding space does not count, so prose in an exempt file is still prose",
			text:        "// the neighbour table is dumped lazily",
			expected:    false,
		},
		{
			description: "boundary: offset zero does not count, since there is no quote to have opened",
			text:        "neighbour",
			expected:    false,
		},
		{
			description: "corner: a single quote does not count, because Go has no single-quoted string and a rune literal cannot hold a word",
			text:        "// 'neighbour'",
			expected:    false,
		},
		{
			description: "corner: an opening parenthesis does not count, which keeps a parenthesised aside from reading as a quoted token",
			text:        "// (neighbour)",
			expected:    false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Parallel()
			idx := strings.Index(foldASCII(tc.text), auditedWordCst)
			if idx < 0 {
				t.Fatalf("the row's text does not contain the audited word: %q", tc.text)
			}
			if got := isQuotedCommandToken(tc.text, idx); got != tc.expected {
				t.Fatalf("isQuotedCommandToken(%q, %d) = %v, want %v", tc.text, idx, got, tc.expected)
			}
		})
	}
}

// TestFoldASCII_table pins the one property the fold exists for: the result is
// byte-for-byte the same length as the input, so an index into it is an index
// into the original.
func TestFoldASCII_table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		description string
		in          string
		expected    string
	}{
		{
			description: "positive: an uppercase word folds, which is how a capitalized occurrence is caught",
			in:          "Neighbour",
			expected:    "neighbour",
		},
		{
			description: "positive: mixed case folds throughout",
			in:          "NeIgHbOuR.h",
			expected:    "neighbour.h",
		},
		{
			description: "negative: lowercase input is returned unchanged",
			in:          "neighbour",
			expected:    "neighbour",
		},
		{
			description: "boundary: the empty string folds to the empty string",
			in:          "",
			expected:    "",
		},
		{
			description: "corner: non-letters are untouched, including the slashes and digits a citation is made of",
			in:          "NET/CORE/NEIGHBOUR.C:2956",
			expected:    "net/core/neighbour.c:2956",
		},
		{
			description: "corner: U+0130 is left alone rather than folded, which is the whole reason this is not strings.ToLower — ToLower expands it to two runes and shifts every later index",
			in:          "İNeighbour",
			expected:    "İneighbour",
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Parallel()
			got := foldASCII(tc.in)
			if got != tc.expected {
				t.Fatalf("foldASCII(%q) = %q, want %q", tc.in, got, tc.expected)
			}
			if len(got) != len(tc.in) {
				t.Fatalf("foldASCII(%q) changed the length from %d to %d, so byte offsets no longer map",
					tc.in, len(tc.in), len(got))
			}
		})
	}
}

// TestShouldSkipFile_table pins this audit's one deliberate difference from its
// siblings, which is that it reads tests.
func TestShouldSkipFile_table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		description string
		path        string
		expected    bool
	}{
		{
			description: "negative: a Go source file is scanned",
			path:        "pkg/xtcpnl/xtcpnl_ndmsg.go",
			expected:    false,
		},
		{
			description: "boundary: a _test.go file is scanned, not skipped — six of the sixteen citations live in tests, and both sibling audits skip these",
			path:        "pkg/xtcpnl/xtcpnl_ndmsg_test.go",
			expected:    false,
		},
		{
			description: "positive: a generated .pb.go file is skipped, since its prose is not ours to edit",
			path:        "gen/go/xtcp_flat_record/flat.pb.go",
			expected:    true,
		},
		{
			description: "positive: a non-Go file is skipped",
			path:        "docs/static-analysis.md",
			expected:    true,
		},
		{
			description: "corner: a file merely containing .go is skipped, because the suffix is what matters",
			path:        "notes/about.go.txt",
			expected:    true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Parallel()
			if got := shouldSkipFile(tc.path); got != tc.expected {
				t.Fatalf("shouldSkipFile(%q) = %v, want %v", tc.path, got, tc.expected)
			}
		})
	}
}

// TestShouldSkipDir_table covers the skip set and the self-skip, the latter
// being what stops the tool from reporting its own documentation.
func TestShouldSkipDir_table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		description string
		path        string
		expected    bool
	}{
		{
			description: "positive: gen is skipped, as in metrics-audit's set",
			path:        "gen",
			expected:    true,
		},
		{
			description: "positive: a nested vendor directory is skipped on its base name",
			path:        "tools/quality-report/vendor",
			expected:    true,
		},
		{
			description: "positive: this tool's own directory is skipped at the repo-relative path the check uses",
			path:        "tools/kernel-citation-audit",
			expected:    true,
		},
		{
			description: "positive: and at an absolute path, which is what the tests root at",
			path:        "/tmp/audit-123/tools/kernel-citation-audit",
			expected:    true,
		},
		{
			description: "negative: a sibling tool directory is scanned",
			path:        "tools/netlink-audit",
			expected:    false,
		},
		{
			description: "negative: a normal source directory is scanned",
			path:        "internal/goip",
			expected:    false,
		},
		{
			description: "adversarial: a directory whose name merely starts with this tool's is scanned, so the self-skip cannot be widened by renaming",
			path:        "tools/kernel-citation-audit-extra",
			expected:    false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Parallel()
			if got := shouldSkipDir(tc.path); got != tc.expected {
				t.Fatalf("shouldSkipDir(%q) = %v, want %v", tc.path, got, tc.expected)
			}
		})
	}
}

// TestExemptionFor_table covers the path side of the argv exemption, including
// the distinction the plan for this change insisted on: obj_neigh.go and
// obj_neigh_test.go are listed for opposite reasons, and only one of them is
// listed at all.
func TestExemptionFor_table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		description string
		path        string
		expected    bool
	}{
		{
			description: "positive: dispatch.go is exempt, because it holds the alias itself",
			path:        "internal/goip/dispatch.go",
			expected:    true,
		},
		{
			description: "positive: an absolute path to the same file is exempt, which is what lets the tests build a tree anywhere",
			path:        "/tmp/audit-123/internal/goip/dispatch.go",
			expected:    true,
		},
		{
			description: "positive: obj_neigh_test.go is exempt, because its argv row types the command",
			path:        "internal/goip/obj_neigh_test.go",
			expected:    true,
		},
		{
			description: "negative: obj_neigh.go is NOT exempt, and it is one character from a file that is — it holds a citation, not an argv",
			path:        "internal/goip/obj_neigh.go",
			expected:    false,
		},
		{
			description: "negative: a file with citations elsewhere in the tree is not exempt",
			path:        "pkg/xtcpnl/xtcpnl_ndmsg.go",
			expected:    false,
		},
		{
			description: "adversarial: a path whose component merely ends with internal does not match, so the suffix test cannot be widened by directory naming",
			path:        "x/notinternal/goip/dispatch.go",
			expected:    false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Parallel()
			_, got := exemptionFor(tc.path)
			if got != tc.expected {
				t.Fatalf("exemptionFor(%q) exempt = %v, want %v", tc.path, got, tc.expected)
			}
		})
	}
}
