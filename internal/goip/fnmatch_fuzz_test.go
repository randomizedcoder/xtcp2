package goip

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// refMatcher is a reference for fnmatch0: glibc's rules written as plain
// recursion over the pattern string, memoized, with no compile step and no
// backtrack point. Only the bracket expression is shared, through
// fnmatchBracket, since it is a parse rather than a match.
type refMatcher struct {
	p, s string
	memo map[[2]int]bool
}

func refFnmatch(p, s string) bool {
	m := refMatcher{p: p, s: s, memo: make(map[[2]int]bool)}
	return m.match(0, 0)
}

func (m *refMatcher) match(i, j int) bool {
	key := [2]int{i, j}
	if v, ok := m.memo[key]; ok {
		return v
	}
	v := m.step(i, j)
	m.memo[key] = v
	return v
}

func (m *refMatcher) step(i, j int) bool {
	p, s := m.p, m.s
	if i == len(p) {
		return j == len(s)
	}
	more := j < len(s)
	switch p[i] {
	case '*':
		return m.match(i+1, j) || (more && m.match(i, j+1))
	case '?':
		return more && m.match(i+1, j+1)
	case '\\':
		// Trailing \ loses: the pattern matches nothing.
		if i+1 == len(p) {
			return false
		}
		return more && s[j] == p[i+1] && m.match(i+2, j+1)
	case '[':
		tok, next, ok, err := fnmatchBracket(p, i+1)
		if err == nil && ok {
			return more && tok.matchByte(s[j]) && m.match(next, j+1)
		}
	}
	return more && s[j] == p[i] && m.match(i+1, j+1)
}

// fnmatchFuzzMax bounds a fuzzed pattern and name. A label pattern is one
// path token, at most 255 bytes, and a label at most IFNAMSIZ-1.
const fnmatchFuzzMax = 255

// FuzzFnmatch holds fnmatch0 to refMatcher on arbitrary patterns and names
// (fuzz_fnmatch): it never panics, refuses only POSIX bracket classes, and
// otherwise answers as the reference does.
//
// go test ./internal/goip/ -run '^$' -fuzz '^FuzzFnmatch$' -fuzztime 60s
func FuzzFnmatch(f *testing.F) {
	for _, tc := range fnmatchTests {
		f.Add(tc.pattern, tc.s)
	}
	for _, tc := range fnmatchCostTests {
		f.Add(tc.pattern, tc.s)
	}
	f.Fuzz(func(t *testing.T, pattern, s string) {
		if len(pattern) > fnmatchFuzzMax || len(s) > fnmatchFuzzMax {
			return
		}
		got, err := fnmatch0(pattern, s)
		if err != nil {
			if !errors.Is(err, ErrNotImplemented) || !strings.Contains(pattern, "[") {
				t.Fatalf("fnmatch0(%q, %q): %v", pattern, s, err)
			}
			return
		}
		if want := refFnmatch(pattern, s); got != want {
			t.Fatalf("fnmatch0(%q, %q) = %v, reference %v", pattern, s, got, want)
		}
	})
}

// fnmatchCostLimit is the most one label match may take
// (fnmatch_star_storm).
const fnmatchCostLimit = time.Millisecond

// fnmatchCostTests are hostile label patterns at the path token's 255-byte
// cap against a 15-byte name, the longest a label is.
var fnmatchCostTests = []struct {
	name        string
	category    string
	description string
	pattern     string
	s           string
	expected    bool
}{
	{
		name: "star_storm", category: "boundary",
		description: "64 stars and a miss: the stars compile to one",
		pattern:     strings.Repeat("*", 64) + "x", s: strings.Repeat("a", 15), expected: false,
	},
	{
		name: "star_storm_255", category: "boundary",
		description: "255 stars match anything",
		pattern:     strings.Repeat("*", 255), s: strings.Repeat("a", 15), expected: true,
	},
	{
		name: "star_literal_alternation", category: "boundary",
		description: "*a repeated to 254 bytes then b: every star backtracks, and the answer is no",
		pattern:     strings.Repeat("*a", 127) + "b", s: strings.Repeat("a", 15), expected: false,
	},
	{
		name: "star_question_alternation", category: "boundary",
		description: "*? repeated: more ?s than the name has bytes",
		pattern:     strings.Repeat("*?", 127), s: strings.Repeat("a", 15), expected: false,
	},
	{
		name: "bracket_storm", category: "corner",
		description: "42 ranges between stars, then a miss",
		pattern:     strings.Repeat("*[a-z]", 42) + "*x", s: strings.Repeat("a", 15), expected: false,
	},
	{
		name: "plain_label", category: "positive",
		description: "an everyday pattern",
		pattern:     "eth0:*", s: "eth0:1", expected: true,
	},
	{
		name: "literal_miss", category: "negative",
		description: "a 255-byte literal against a 15-byte name fails",
		pattern:     strings.Repeat("a", 255), s: strings.Repeat("a", 15), expected: false,
	},
}

// TestFnmatchCost bounds a label match: each hostile pattern answers
// correctly in under fnmatchCostLimit. The fastest of 20 runs is timed, so a
// busy host does not fail it. No pattern-length cap is needed beyond the
// path token's 255 bytes.
//
// go test ./internal/goip/ -run TestFnmatchCost
func TestFnmatchCost(t *testing.T) {
	for _, tc := range fnmatchCostTests {
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			best := time.Duration(1<<63 - 1)
			for range 20 {
				start := time.Now()
				got, err := fnmatch0(tc.pattern, tc.s)
				best = min(best, time.Since(start))
				if err != nil || got != tc.expected {
					t.Fatalf("%s: fnmatch0 = (%v, %v), want %v", tc.description, got, err, tc.expected)
				}
			}
			if best > fnmatchCostLimit {
				t.Fatalf("%s: %v, want under %v", tc.description, best, fnmatchCostLimit)
			}
			if want := refFnmatch(tc.pattern, tc.s); want != tc.expected {
				t.Fatalf("%s: reference says %v", tc.description, want)
			}
		})
	}
}
