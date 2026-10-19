package goiphttp

import (
	"errors"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/internal/goip"
)

// argvPath is target's escaped path after its root, without the query: what
// servePath hands the tokenizer. The roots themselves are "".
func argvPath(target string) string {
	target, _, _ = strings.Cut(target, "?")
	for _, root := range []string{Prefix, ShortPrefix} {
		if rest, ok := strings.CutPrefix(target, root+"/"); ok {
			return rest
		}
		if target == root {
			return ""
		}
	}
	return target
}

// tokenPaths are TestTokens' paths.
func tokenPaths() []string {
	p := make([]string, 0, len(tokenTests))
	for i := range tokenTests {
		p = append(p, tokenTests[i].path)
	}
	return p
}

// handlerPaths are TestHandler's targets, after their roots.
func handlerPaths(tb testing.TB) []string {
	tests := handlerTests(tb)
	p := make([]string, 0, len(tests))
	for i := range tests {
		p = append(p, argvPath(tests[i].target))
	}
	return p
}

// examplePaths are the index examples, after their roots.
func examplePaths() []string {
	p := make([]string, 0, len(examples))
	for _, ex := range examples {
		p = append(p, argvPath(ex.Path))
	}
	return p
}

// argvSeeds is FuzzArgvPath's seed corpus: every TestTokens path, every
// TestHandler target and every index example.
func argvSeeds(tb testing.TB) []string {
	return slices.Concat(tokenPaths(), handlerPaths(tb), examplePaths())
}

// sameRequest compares two show requests, an empty selector list equal to
// none.
func sameRequest(a, b goip.ShowRequest) bool {
	if len(a.Selectors) == 0 && len(b.Selectors) == 0 {
		a.Selectors, b.Selectors = nil, nil
	}
	return reflect.DeepEqual(a, b)
}

// showVerbs are the spellings Resolved.Verb may hold: a show verb, never a
// write.
var showVerbs = []string{"show", "list", "lst"}

// FuzzArgvPath feeds arbitrary escaped paths through the tokenizer and the
// resolver (fuzz_never_writes, fuzz_argv_round_trip):
//
//   - a tokenizer error is errParam, a 400;
//   - a resolver error is one statusOf classifies, never a 500;
//   - an accepted path names a show verb, never a write;
//   - its full spelling (Resolved.Argv) and its canonical path (the
//     Content-Location, when location serves one) both resolve to the same
//     ShowRequest again.
//
// go test ./internal/goip/goiphttp/ -run '^$' -fuzz '^FuzzArgvPath$' -fuzztime 60s
func FuzzArgvPath(f *testing.F) {
	for _, seed := range argvSeeds(f) {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, escaped string) {
		toks, err := tokens(escaped)
		if err != nil {
			if !errors.Is(err, errParam) {
				t.Fatalf("tokens(%q): %v is not errParam", escaped, err)
			}
			return
		}
		if len(toks) == 0 {
			return
		}
		s, res, err := goip.ResolveShow(toks)
		if err != nil {
			if statusOf(err) == http.StatusInternalServerError {
				t.Fatalf("ResolveShow(%q): unclassified error %v", toks, err)
			}
			return
		}
		if !slices.Contains(showVerbs, res.Verb) {
			t.Fatalf("ResolveShow(%q): verb %q is not a show verb", toks, res.Verb)
		}

		s2, res2, err := goip.ResolveShow(res.Argv())
		if err != nil || !sameRequest(s, s2) || res2.Path() != res.Path() {
			t.Fatalf("%q: argv %q re-resolves to (%+v, %q, %v), want (%+v, %q)",
				toks, res.Argv(), s2, res2.Path(), err, s, res.Path())
		}

		canon, ok := location(s)
		if !ok {
			return
		}
		toks3, err := tokens(argvPath(canon))
		if err != nil {
			t.Fatalf("%q: canonical path %q does not tokenize: %v", toks, canon, err)
		}
		s3, _, err := goip.ResolveShow(toks3)
		if err != nil || !sameRequest(s, s3) {
			t.Fatalf("%q: canonical path %q resolves to (%+v, %v), want %+v", toks, canon, s3, err, s)
		}
	})
}

// TestArgvSeeds checks seed_corpus: every row of the IA2 tables is a seed.
//
// go test ./internal/goip/goiphttp/ -run TestArgvSeeds
func TestArgvSeeds(t *testing.T) {
	seeds := argvSeeds(t)
	tests := []struct {
		name        string
		category    string
		description string
		// expected: every one of paths is a seed.
		expected []string
	}{
		{name: "token_rows", category: "positive", description: "every TestTokens path", expected: tokenPaths()},
		{name: "handler_rows", category: "positive", description: "every TestHandler target, after its root", expected: handlerPaths(t)},
		{name: "index_examples", category: "positive", description: "every index example", expected: examplePaths()},
		{name: "roots", category: "boundary", description: "the bare roots seed the empty path", expected: []string{""}},
		{name: "query_stripped", category: "corner", description: "/i/a?dev=goip0 seeds a, without its query", expected: []string{"a"}},
		{name: "pct2f_kept", category: "corner", description: "a seed stays escaped, as servePath reads it", expected: []string{"route/show/10.0.0.0%2F8"}},
	}
	for _, tt := range tests {
		t.Run(tt.category+"/"+tt.name, func(t *testing.T) {
			for _, p := range tt.expected {
				if !slices.Contains(seeds, p) {
					t.Errorf("%s: %q is not a seed", tt.description, p)
				}
			}
		})
	}
}
