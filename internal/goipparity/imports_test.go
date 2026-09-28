package goipparity

import (
	"go/ast"
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

// parityPackages are the packages goip-parity is made of.
var parityPackages = []string{
	modulePath + "/cmd/goip-parity",
	modulePath + "/internal/goipparity",
}

// parityMayImport is what a goip-parity package may import directly, beyond
// the standard library and the other parity packages.
//
// Each entry carries the reason it is there. An import allowlist with no
// reasons becomes a place regressions go to be forgotten, which is the same
// discipline the netlink offset allowlist applies.
//
// Notice what is absent: golang.org/x/sys/unix. The comparator has no use for
// a socket constant, and leaving the package out is most of the plan's Risk 8
// on its own — code that cannot name AF_NETLINK cannot open a netlink socket.
var parityMayImport = map[string]string{
	modulePath + "/pkg/nlparity": "the comparator: the tolerant walker, the segmenter, the differ, " +
		"the normalizer and the embedded allowlist. This is the only thing goip-parity needs, " +
		"and it is a pure function of bytes.",
}

// forbiddenFirstParty are the first-party packages whose presence anywhere in
// goip-parity's build closure would break a property this binary exists to
// have.
//
// internal/goip is here for the plan's Risk 8 — see the header of
// TestParityCannotOpenASocket. The two io_uring packages are here for
// TODO-SOON §16, the same reason internal/goip forbids them: giouring reaches
// for syscall.munmap and does not link outside nix, and the comparator's whole
// value is that it runs anywhere a capture file does.
var forbiddenFirstParty = map[string]string{
	modulePath + "/internal/goip": "goip opens netlink sockets; a comparator that could reach " +
		"its code could put traffic inside a capture window and have it attributed to goip",
	modulePath + "/pkg/xtcp":     "pkg/xtcp/netlinker_iouring.go imports giouring",
	modulePath + "/pkg/io_uring": "pkg/io_uring/ring.go imports giouring",
}

// socketOpeners are call expressions that would give this package a socket.
//
// Spelled as `package.Func` suffixes and matched on the selector, because that
// is how the call would actually be written. The list is the union of the
// three ways it could arrive: the syscall layer directly, x/sys/unix, and
// pkg/xtcpnl's own entry points — which matter most, since reaching for
// xtcpnl.DumpRtnetlink to "just check something" is the plausible mistake, not
// hand-rolling a syscall.
var socketOpeners = []string{
	"syscall.Socket", "syscall.Connect", "syscall.Bind", "syscall.Sendto",
	"syscall.Recvfrom", "syscall.SetsockoptInt",
	"unix.Socket", "unix.Connect", "unix.Bind", "unix.Sendto", "unix.Recvfrom",
	"net.Dial", "net.DialTimeout", "net.Listen", "net.ListenPacket",
	"xtcpnl.OpenNetlinkSocketWithTimeout", "xtcpnl.DumpRtnetlink",
	"xtcpnl.TalkRtnetlink", "xtcpnl.SendNetlinkDumpRequest",
}

// TestParityCannotOpenASocket is the plan's Risk 8, as a check rather than as
// a comment.
//
// # The property
//
// "Keep cmd/goip-parity a separate binary from cmd/goip, so no future
// refactor gives the comparator a netlink socket inside a capture window."
//
// The failure it prevents is nasty precisely because it does not look like a
// harness bug. The comparator runs while the guest is capturing. If it ever
// emitted a netlink datagram of its own, that datagram lands in whichever
// capture window is open, and the differ attributes it to whichever tool the
// capture belongs to — so a comparator bug is reported as an extra goip
// transaction, an L1 finding, the single highest-value assertion in the whole
// harness. Someone would go looking for it in internal/goip and not find it.
//
// # Why three separate assertions
//
// Being a separate binary is necessary and not sufficient: a separate binary
// that imported internal/goip would have exactly the same problem. So the
// property is asserted three ways, weakest to strongest:
//
//   - the direct-import rule, which is the one a person violates;
//   - the closure rule, which catches reaching internal/goip through
//     something else;
//   - the call rule, which catches the case where none of that happened and
//     someone simply wrote a syscall in this package.
//
// The third is the one that would still fire if the first two were satisfied,
// which is why it is not redundant.
//
// go test ./internal/goipparity/ -run TestParityCannotOpenASocket
func TestParityCannotOpenASocket(t *testing.T) {
	isParity := map[string]bool{}
	for _, p := range parityPackages {
		isParity[p] = true
	}

	t.Run("positive: every direct import is stdlib, another parity package, or allowlisted", func(t *testing.T) {
		for _, pkg := range parityPackages {
			// Test files included: a test-only socket import would not put
			// traffic in a capture window, but it would mean this package had
			// acquired the means to, and the next refactor could move it.
			for _, imp := range packageImports(t, pkg, true) {
				switch {
				case isStdlib(imp.path), isParity[imp.path]:
				default:
					if _, ok := parityMayImport[imp.path]; !ok {
						t.Errorf("%s imports %q, which is not in parityMayImport",
							imp.file, imp.path)
					}
				}
			}
		}
	})

	t.Run("negative: golang.org/x/sys/unix is not imported at all", func(t *testing.T) {
		// Asserted by name rather than left to the allowlist row above,
		// because this is the load-bearing absence and a reader should be
		// able to find it by searching for the import path. Adding it to
		// parityMayImport would make the row above pass; this row would
		// still fail, which is the intent.
		for _, pkg := range parityPackages {
			for _, imp := range packageImports(t, pkg, true) {
				if imp.path == "golang.org/x/sys/unix" || imp.path == "syscall" {
					t.Errorf("%s imports %q; the comparator has no use for a socket "+
						"constant and not having the package is most of Risk 8",
						imp.file, imp.path)
				}
			}
		}
	})

	closure, thirdParty := firstPartyClosure(t, parityPackages)

	t.Run("positive: the transitive first-party closure is exactly the expected set", func(t *testing.T) {
		// Pinning the whole closure, not only its forbidden members, is what
		// catches a new import that is clean today but drags a subsystem in.
		want := map[string]string{
			modulePath + "/cmd/goip-parity":     "the binary",
			modulePath + "/internal/goipparity": "the command table, the stdout comparator and the driver",
			modulePath + "/pkg/nlparity":        "the netlink comparator",
			modulePath + "/pkg/xtcpnl":          "reached through pkg/nlparity, which decodes attributes with it",
			modulePath + "/gen/go/xtcp_flat_record": "reached through pkg/xtcpnl's inet_diag decoders, which " +
				"write into the generated proto",
		}
		for pkg := range closure {
			if _, ok := want[pkg]; !ok {
				t.Errorf("unexpected first-party package in goip-parity's closure: %q", pkg)
			}
		}
		for pkg := range want {
			if !closure[pkg] {
				t.Errorf("expected first-party package %q is no longer in the closure; "+
					"if that is intentional, remove it from want", pkg)
			}
		}
	})

	t.Run("negative: internal/goip is not in the closure, which is Risk 8 itself", func(t *testing.T) {
		// Kept separate from the closure-equality row even though that row
		// would also catch it, because the two fail for different reasons and
		// only this one says what went wrong. "Unexpected package
		// internal/goip" reads like a tidiness complaint; this reads like the
		// design decision it is.
		for pkg, why := range forbiddenFirstParty {
			if closure[pkg] {
				t.Errorf("goip-parity's closure reached %q (%s)", pkg, why)
			}
		}
	})

	t.Run("negative: no giouring edge, so the comparator links outside nix", func(t *testing.T) {
		for imp, from := range thirdParty {
			if strings.Contains(imp, "giouring") || strings.Contains(imp, "io_uring") {
				t.Errorf("first-party package %q imports %q", from, imp)
			}
		}
	})

	t.Run("negative: no parity package calls anything that opens a socket", func(t *testing.T) {
		// The assertion that would still fire with both rules above
		// satisfied. pkg/xtcpnl IS in the closure — nlparity decodes
		// attributes with it — so "does not import the socket layer" cannot
		// be a closure property. What can be a property is that no call in
		// THIS package's own code reaches one of its socket entry points.
		for _, pkg := range parityPackages {
			for _, c := range packageCalls(t, pkg) {
				for _, bad := range socketOpeners {
					if c.name == bad {
						t.Errorf("%s calls %s; the comparator must not open a socket",
							c.file, c.name)
					}
				}
			}
		}
	})

	t.Run("boundary: every allowlist entry is used and carries a reason", func(t *testing.T) {
		used := map[string]bool{}
		for _, pkg := range parityPackages {
			for _, imp := range packageImports(t, pkg, true) {
				if _, ok := parityMayImport[imp.path]; ok {
					used[imp.path] = true
				}
			}
		}
		var unused []string
		for imp, reason := range parityMayImport {
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

	t.Run("boundary: the walks are not vacuous", func(t *testing.T) {
		// Every row above is a loop over things read off the disk, so a wrong
		// path makes all of them pass by examining nothing. packageImports
		// fatals on a missing directory; this covers a package that parses to
		// nothing.
		for _, pkg := range parityPackages {
			if n := len(packageImports(t, pkg, true)); n == 0 {
				t.Errorf("package %q yielded no imports", pkg)
			}
			if n := len(packageCalls(t, pkg)); n == 0 {
				t.Errorf("package %q yielded no calls, so the socket row checked nothing", pkg)
			}
		}
		if len(closure) < len(parityPackages) {
			t.Errorf("closure has %d packages, fewer than the %d roots",
				len(closure), len(parityPackages))
		}
	})

	t.Run("corner: the socket-opener matcher recognizes the calls it is meant to", func(t *testing.T) {
		// The matcher is a string comparison against a selector, and a
		// selector rendering that produced something else — "Socket" without
		// the package, say — would make the row above pass unconditionally
		// while the check it guards was green. So it is exercised on a
		// synthetic file that does make the calls.
		dir := t.TempDir()
		src := `package x

import (
	"net"
	"golang.org/x/sys/unix"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

func f() {
	_, _ = unix.Socket(0, 0, 0)
	_, _ = net.Dial("tcp", "127.0.0.1:1")
	_ = xtcpnl.DumpRtnetlink(0, nil, nil, nil)
}
`
		if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
		found := map[string]bool{}
		for _, c := range parseCalls(t, dir, false) {
			found[c.name] = true
		}
		for _, want := range []string{"unix.Socket", "net.Dial", "xtcpnl.DumpRtnetlink"} {
			if !found[want] {
				t.Errorf("the call walker did not see %q; it saw %v", want, found)
			}
		}
	})
}

// anImport is one import, with the file it was found in so a finding can name
// it.
type anImport struct {
	path string
	file string
}

// aCall is one call expression's rendered `package.Func`, with its file.
type aCall struct {
	name string
	file string
}

// pkgDir maps a first-party import path to its directory on disk.
func pkgDir(pkgPath string) string {
	return filepath.Join(repoRoot, strings.TrimPrefix(pkgPath, modulePath+"/"))
}

// packageImports returns every import in a first-party package's .go files.
func packageImports(t *testing.T, pkgPath string, includeTests bool) []anImport {
	t.Helper()

	dir := pkgDir(pkgPath)
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
		f, perr := parser.ParseFile(fset, path, nil,
			parser.ImportsOnly|parser.SkipObjectResolution)
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

// packageCalls returns every `package.Func(...)` call in a first-party
// package, test files included.
func packageCalls(t *testing.T, pkgPath string) []aCall {
	t.Helper()
	return parseCalls(t, pkgDir(pkgPath), true)
}

// parseCalls walks every .go file in dir and renders each selector call as
// `receiver.Selector`.
//
// Only single-identifier receivers are rendered, which is exactly the form a
// package-qualified call takes. A method call on a value — `r.Netlink.Failed()`
// — has a non-identifier receiver and is skipped, which is why the forbidden
// list can be a flat set of strings without matching method names by accident.
func parseCalls(t *testing.T, dir string, includeTests bool) []aCall {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}

	var out []aCall
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		if !includeTests && strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		f, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if perr != nil {
			t.Fatalf("parse %s: %v", path, perr)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			recv, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			out = append(out, aCall{name: recv.Name + "." + sel.Sel.Name, file: path})
			return true
		})
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
		// its dependencies are not goip-parity's problem, and walking
		// internal/goip would flood the third-party map pointlessly.
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
// The rule the go command itself uses: a standard-library import path has no
// dot in its first segment, because a module path's first segment is a domain
// name. It needs no toolchain, no network and no module cache.
func isStdlib(imp string) bool {
	first := imp
	if i := strings.IndexByte(imp, '/'); i >= 0 {
		first = imp[:i]
	}
	return !strings.Contains(first, ".")
}

// TestIsStdlibParity tests the classifier every row above rests on.
//
// A classifier that called everything stdlib would make
// TestParityCannotOpenASocket pass unconditionally, and the failure would be
// invisible because the test it guards would be green.
//
// go test ./internal/goipparity/ -run TestIsStdlibParity
func TestIsStdlibParity(t *testing.T) {
	tests := []struct {
		description string
		imp         string
		want        bool
	}{
		{description: "positive: a single-segment stdlib package", imp: "errors", want: true},
		{description: "positive: a multi-segment stdlib package", imp: "path/filepath", want: true},
		{
			description: "negative: golang.org/x/sys/unix is not stdlib, which is the import this file is about",
			imp:         "golang.org/x/sys/unix", want: false,
		},
		{
			description: "negative: this module's own packages are not stdlib",
			imp:         modulePath + "/pkg/nlparity", want: false,
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
			description: "corner: `internal/...` is stdlib-shaped and classified as stdlib",
			imp:         "internal/abi", want: true,
		},
		{
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
