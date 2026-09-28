package goip

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The module path and the repo root, relative to this package's directory.
const (
	modulePath = "github.com/randomizedcoder/xtcp2"
	repoRoot   = "../.."
)

// goipPackages are the packages goip itself is made of — the ones this repo
// controls the imports of, and the scope the plan's import rule applies to.
var goipPackages = []string{
	modulePath + "/cmd/goip",
	modulePath + "/internal/goip",
	modulePath + "/internal/goip/model",
	modulePath + "/internal/goip/render",
	modulePath + "/internal/goip/req",
	modulePath + "/internal/goip/service",
}

// goipMayImport is what a goip package is allowed to import directly, beyond
// the standard library and the other goip packages.
//
// Each entry carries the reason it is there, because an import allowlist with
// no reasons becomes a place regressions go to be forgotten — the discipline
// nix/checks/proto-audit-netlink-allowlist.json applies to offsets.
var goipMayImport = map[string]string{
	"golang.org/x/sys/unix":    "the AF_*/IFLA_*/IFF_*/ARPHRD_* constants and the socket syscalls",
	modulePath + "/pkg/xtcpnl": "the subject under test: the wire primitives, the request builders and the attribute decoders",
	modulePath + "/pkg/nlparity": "the tolerant walker the ReplaySource needs. xtcpnl.WalkNlMsgs cannot drive a replay — " +
		"it filters on the caller's own nlmsg_seq with no \"any seq\" sentinel, and its own doc comment names " +
		"pkg/nlparity as the answer. The plan's import set listed only xtcpnl; this is the one addition, and it " +
		"satisfies the same §16 property.",
}

// forbiddenFirstParty are the first-party packages whose presence anywhere in
// goip's build closure would reintroduce TODO-SOON §16 — they import giouring,
// which reaches for syscall.munmap and does not link outside nix.
//
// Named as packages rather than as the giouring import path because that is
// how the regression would actually arrive: nobody will import giouring into
// goip directly, but reaching for a helper in pkg/xtcp is an easy mistake and
// has exactly the same effect.
var forbiddenFirstParty = map[string]string{
	modulePath + "/pkg/xtcp":     "pkg/xtcp/netlinker_iouring.go imports giouring",
	modulePath + "/pkg/io_uring": "pkg/io_uring/ring.go imports giouring",
}

// TestGoipImportSetIsHermetic enforces the plan's "import set: pkg/xtcpnl +
// x/sys/unix + stdlib only" as a check rather than as a comment.
//
// # Why this property is load-bearing and not hygiene
//
// TODO-SOON §16: pkg/xtcp, cmd/xtcp2 and cmd/ns do not link outside nix,
// because giouring reaches for syscall.munmap, which does not exist on this Go
// version. Every test in this package and in render/ and req/ is hermetic — no
// socket, no root, no VM — and that is worth nothing if the package cannot be
// compiled without nix in the first place. `go build ./cmd/goip && go test
// ./internal/goip/...` on a developer's machine is the property, and one
// careless pkg/xtcp import to reuse a helper takes it away silently: the nix
// build would stay green, because inside nix everything links.
//
// # Two different scopes, asserted separately, because they are different claims
//
// The plan's wording — "import set: pkg/xtcpnl + x/sys/unix + stdlib only" — is
// true of the packages goip is made of and **not** true of goip's transitive
// closure, and conflating the two produces a check that reports 17 findings
// about generated protobuf code on its first run. (It did. That is how this
// comment came to be written.)
//
// `go list -deps ./cmd/goip` is 327 packages, including all of google.golang.org/grpc
// and google.golang.org/protobuf. They arrive through pkg/xtcpnl: the inet_diag
// decoders — xtcpnl_inet_diag_tcpinfo.go and its dozen siblings — write into
// gen/go/xtcp_flat_record, which is generated protobuf. That is not a problem,
// because protobuf and grpc are pure Go and link fine outside nix; it is only
// worth knowing so that "stdlib only" is not read as a closure property.
//
// So:
//
//   - Rule A, over goipPackages, is the direct-import rule. It is the one a
//     person can violate, and it fails naming the file and the import.
//   - Rule B, over the transitive *first-party* closure, is the §16 rule. It
//     holds because giouring is only ever reachable through a first-party
//     package, so excluding pkg/xtcp and pkg/io_uring from the closure is
//     equivalent to excluding giouring from it.
//
// One caveat on Rule B, so that its silence is not mistaken for a pass: an
// actual pkg/xtcp import would stop this package *compiling* outside nix, so
// outside nix the failure arrives as a build error rather than as this row.
// That is a louder signal, not a quieter one, but it does mean the row can only
// be seen firing inside nix — or by pointing forbiddenFirstParty at a package
// that does compile, which is how it was confirmed non-vacuous.
//
// # Why a parser walk rather than `go list -deps`
//
// The plan named `go list -deps`, and for the third-party half it is the more
// complete check. It also needs a usable Go toolchain, a module cache, and —
// as this worktree demonstrated — a git checkout `go list` is willing to stat,
// since a detached or mid-rebase tree makes it exit 1 on VCS stamping before
// printing anything. A parser walk has none of those dependencies, which is
// the right trade for a test whose entire point is not needing an environment.
// `go build ./cmd/goip` outside nix, the plan's Verification step 1, remains
// the real statement of the property; this test is what makes a violation
// legible instead of a link error in a wall of output.
//
// go test ./internal/goip/ -run TestGoipImportSetIsHermetic
func TestGoipImportSetIsHermetic(t *testing.T) {
	t.Run("positive: every direct import of a goip package is stdlib, another goip package, or allowlisted", func(t *testing.T) {
		isGoip := map[string]bool{}
		for _, p := range goipPackages {
			isGoip[p] = true
		}

		for _, pkg := range goipPackages {
			// Test files are included deliberately. A test-only import of
			// pkg/xtcp would break `go test ./internal/goip/...` outside nix
			// just as thoroughly as a non-test one, and that half of the
			// property is the half this file lives in.
			for _, imp := range packageImports(t, pkg, true) {
				switch {
				case isStdlib(imp.path), isGoip[imp.path]:
				default:
					if _, ok := goipMayImport[imp.path]; !ok {
						t.Errorf("%s imports %q, which is not in goipMayImport; if it is genuinely "+
							"needed, add it with a reason and confirm `go build ./cmd/goip` still "+
							"works outside nix", imp.file, imp.path)
					}
				}
			}
		}
	})

	// The transitive first-party closure, non-test files only. A dependency's
	// own tests are not part of anything that links into goip, so including
	// them here would report gen/go/xtcp_flat_record's conformance test as if
	// it were a goip dependency.
	closure, thirdParty := firstPartyClosure(t, goipPackages)

	t.Run("positive: the transitive first-party closure is exactly the expected set", func(t *testing.T) {
		// Pinning the closure — not just its forbidden members — is what
		// catches the subtler regression: a new first-party import that is
		// clean today but drags a whole subsystem into goip's graph, where the
		// next person to add an io_uring call to it breaks goip remotely.
		want := map[string]string{
			modulePath + "/cmd/goip":              "the binary",
			modulePath + "/internal/goip":         "dispatch, lltab, source, objects",
			modulePath + "/internal/goip/model":   "transport-neutral rtnetlink resource models and stable ordering",
			modulePath + "/internal/goip/render":  "view structs and the text/json renderers",
			modulePath + "/internal/goip/req":     "the pure request builders",
			modulePath + "/internal/goip/service": "shared request, decode, filter and ordering application layer",
			modulePath + "/pkg/xtcpnl":            "the subject under test",
			modulePath + "/pkg/nlparity":          "the tolerant walker, for ReplaySource",
			modulePath + "/gen/go/xtcp_flat_record": "reached through pkg/xtcpnl's inet_diag decoders, which " +
				"write into the generated proto. This is where grpc and protobuf enter the closure.",
		}
		for pkg := range closure {
			if _, ok := want[pkg]; !ok {
				t.Errorf("unexpected first-party package in goip's closure: %q", pkg)
			}
		}
		for pkg := range want {
			if !closure[pkg] {
				t.Errorf("expected first-party package %q is no longer in goip's closure; "+
					"if that is intentional, remove it from want", pkg)
			}
		}
	})

	t.Run("negative: no giouring-importing first-party package is in the closure", func(t *testing.T) {
		// **The §16 assertion.** Kept separate from the closure-equality row
		// above even though that row would also catch it, because the two fail
		// for different reasons and only this one says what went wrong: the
		// equality row would report "unexpected package pkg/xtcp", which reads
		// like a tidiness complaint rather than "goip no longer builds outside
		// nix".
		for pkg, why := range forbiddenFirstParty {
			if closure[pkg] {
				t.Errorf("goip's closure reached %q (%s): goip must not link io_uring, "+
					"or `go build ./cmd/goip` stops working outside nix", pkg, why)
			}
		}
	})

	t.Run("negative: giouring is not a direct third-party edge of the closure either", func(t *testing.T) {
		// The belt to the braces above: if giouring were ever imported by a
		// first-party package this check does not know about, it would show up
		// here as a third-party edge rather than slipping through.
		for imp, from := range thirdParty {
			if strings.Contains(imp, "giouring") || strings.Contains(imp, "io_uring") {
				t.Errorf("first-party package %q imports %q: goip must not link io_uring", from, imp)
			}
		}
	})

	t.Run("boundary: every allowlist entry is used and carries a reason", func(t *testing.T) {
		// An entry for an import nobody makes is a standing permission for a
		// dependency the code does not have, and an entry with no reason is
		// the thing this allowlist exists to prevent.
		used := map[string]bool{}
		for _, pkg := range goipPackages {
			for _, imp := range packageImports(t, pkg, true) {
				if _, ok := goipMayImport[imp.path]; ok {
					used[imp.path] = true
				}
			}
		}
		var unused []string
		for imp, reason := range goipMayImport {
			if strings.TrimSpace(reason) == "" {
				t.Errorf("allowlist entry %q has no reason", imp)
			}
			if !used[imp] {
				unused = append(unused, imp)
			}
		}
		sort.Strings(unused)
		if len(unused) != 0 {
			t.Errorf("allowlist entries nothing imports: %v", unused)
		}
	})

	t.Run("boundary: the walk is not vacuous", func(t *testing.T) {
		// Both halves of this test are loops over things read off the disk, so
		// a wrong path makes every row above pass by examining nothing.
		// packageImports t.Fatalf's on a missing directory, which covers a
		// deleted package; this covers the subtler case of a package that
		// parses to no imports at all.
		for _, pkg := range goipPackages {
			if n := len(packageImports(t, pkg, true)); n == 0 {
				t.Errorf("package %q yielded no imports, so the rows above checked nothing", pkg)
			}
		}
		if len(closure) < len(goipPackages) {
			t.Errorf("closure has %d packages, fewer than the %d roots", len(closure), len(goipPackages))
		}
	})
}

// anImport is one import, with the file it was found in so a finding can name
// it.
type anImport struct {
	path string
	file string
}

// packageImports returns every import in a first-party package's .go files.
func packageImports(t *testing.T, pkgPath string, includeTests bool) []anImport {
	t.Helper()

	dir := filepath.Join(repoRoot, strings.TrimPrefix(pkgPath, modulePath+"/"))
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read package dir %s (for import %q): %v", dir, pkgPath, err)
	}

	var out []anImport
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		if !includeTests && strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		f, perr := parser.ParseFile(fset, path, nil, parser.ImportsOnly|parser.SkipObjectResolution)
		if perr != nil {
			t.Fatalf("parse %s: %v", path, perr)
		}
		for _, spec := range f.Imports {
			imp, uerr := strconv.Unquote(spec.Path.Value)
			if uerr != nil {
				t.Fatalf("%s: unquote import %s: %v", path, spec.Path.Value, uerr)
			}
			out = append(out, anImport{path: imp, file: path})
		}
	}
	return out
}

// firstPartyClosure walks first-party imports from the given roots, following
// only non-test files, and returns the set of first-party packages reached
// along with the third-party packages they import mapped to one importer.
func firstPartyClosure(t *testing.T, roots []string) (map[string]bool, map[string]string) {
	t.Helper()

	closure := map[string]bool{}
	thirdParty := map[string]string{}

	var walk func(string)
	walk = func(pkgPath string) {
		if closure[pkgPath] {
			return
		}
		closure[pkgPath] = true

		// A forbidden package is recorded as reached but not descended into:
		// its own dependencies are not goip's problem, and walking pkg/xtcp
		// would flood the third-party map with the whole daemon's graph.
		if _, forbidden := forbiddenFirstParty[pkgPath]; forbidden {
			return
		}

		for _, imp := range packageImports(t, pkgPath, false) {
			switch {
			case isStdlib(imp.path):
			case imp.path == modulePath || strings.HasPrefix(imp.path, modulePath+"/"):
				walk(imp.path)
			default:
				if _, seen := thirdParty[imp.path]; !seen {
					thirdParty[imp.path] = pkgPath
				}
			}
		}
	}

	for _, r := range roots {
		walk(r)
	}
	return closure, thirdParty
}

// isStdlib reports whether an import path names a standard-library package.
//
// The rule is the one the go command itself uses for this distinction: a
// standard-library import path has no dot in its first segment, because a
// module path's first segment is a domain name. It needs no toolchain, no
// network and no module cache, which is why it is used here rather than
// `go list std`.
func isStdlib(imp string) bool {
	first := imp
	if i := strings.IndexByte(imp, '/'); i >= 0 {
		first = imp[:i]
	}
	return !strings.Contains(first, ".")
}

// TestIsStdlib tests the classifier the check above rests on.
//
// It matters more than its size suggests: a classifier that called everything
// stdlib would make TestGoipImportSetIsHermetic pass unconditionally, and the
// failure would be invisible, because the test it guards would be green.
//
// go test ./internal/goip/ -run TestIsStdlib
func TestIsStdlib(t *testing.T) {
	tests := []struct {
		description string
		imp         string
		want        bool
	}{
		{
			description: "positive: a single-segment stdlib package",
			imp:         "errors", want: true,
		},
		{
			description: "positive: a multi-segment stdlib package",
			imp:         "encoding/json", want: true,
		},
		{
			description: "positive: a three-segment stdlib package",
			imp:         "go/parser", want: true,
		},
		{
			description: "negative: golang.org/x/sys/unix is not stdlib, despite shipping alongside the toolchain",
			imp:         "golang.org/x/sys/unix", want: false,
		},
		{
			description: "negative: this module's own packages are not stdlib",
			imp:         modulePath + "/pkg/xtcpnl", want: false,
		},
		{
			// The import that would actually appear if someone pulled io_uring
			// in, so the classifier is asserted on the real string rather than
			// on a stand-in.
			description: "negative: the giouring import path is not stdlib",
			imp:         "github.com/pawelgaczynski/giouring", want: false,
		},
		{
			description: "boundary: a single-segment path containing a dot is not stdlib",
			imp:         "example.com", want: false,
		},
		{
			description: "boundary: the empty import path classifies as stdlib rather than panicking",
			imp:         "", want: true,
		},
		{
			// "internal" and "vendor" are real stdlib first segments
			// (internal/abi, vendor/golang.org/x/...), and both are dotless,
			// so the dot rule gets them right without a special case.
			description: "corner: `internal/...` is stdlib-shaped and classified as stdlib",
			imp:         "internal/abi", want: true,
		},
		{
			// A relative import, which the go command rejects in a module but
			// the parser hands over happily. Its first segment is "." so it is
			// classified third-party and would be reported rather than
			// silently accepted.
			description: "corner: a relative import path is not treated as stdlib",
			imp:         "./local", want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := isStdlib(tc.imp); got != tc.want {
				t.Errorf("isStdlib(%q) = %v, want %v", tc.imp, got, tc.want)
			}
		})
	}
}
