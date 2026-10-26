package treatment

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

// The treatment feature is split by audience, and the split only means
// something if it is enforced. This test walks first-party imports with the
// parser (no toolchain, no module cache, works outside nix exactly like
// internal/goip/imports_test.go) and asserts the layering below.
//
//	pkg/treatment                  domain types, limits, validators: proto-free   [everyone]
//	pkg/treatment/wire             domain <-> generated proto conversion          [gRPC boundary]
//	pkg/treatment/identity         SPIFFE identity + TLS                          [agent, controller, tools]
//	pkg/treatment/reconcile        journal (xtcp_treatment_journal.v1) + reconciler + Backend; no RPC proto, no gRPC   [agent, watchdog]
//	pkg/treatment/local            host config + goip-backed reconciler; no TLS, no gRPC    [agent, watchdog, treatmentctl]
//	pkg/treatment/attribution      ExposureIndex + telemetry stamping from a journal snapshot [daemon, agent]
//	pkg/treatment/experiment       statistics + state machine, pure               [controller]
//	pkg/treatment/agent            check-in loop + watch over local + attribution [daemon (treatment tag), agent binary]
//	pkg/treatment/controller       gRPC handlers over store ports                 [controller binary only]
//	pkg/treatment/controller/store ports; postgres/ and memory/ implement them    [controller]
//
// The daemon (cmd/xtcp2) must never reach the controller, a SQL driver or the
// ClickHouse client: that was the regression that prompted this file.
const (
	modulePath = "github.com/randomizedcoder/xtcp2"
	repoRoot   = "../.."
)

// boundaryRule is one root whose transitive first-party closure must not
// contain any forbidden package, and whose third-party edges must not match
// any forbidden substring.
type boundaryRule struct {
	description string
	root        string
	forbidden   []string // first-party package paths
	forbidden3p []string // substrings of third-party import paths
	expectAny   []string // first-party packages that must be present, so the walk is not vacuous
}

func treatmentBoundaryRules() []boundaryRule {
	tp := func(p string) string { return modulePath + "/" + p }
	return []boundaryRule{
		{
			description: "positive: the daemon links neither the controller, a SQL driver nor ClickHouse",
			root:        tp("cmd/xtcp2"),
			forbidden:   []string{tp("pkg/treatment/controller"), tp("pkg/treatment/experiment")},
			forbidden3p: []string{"database/sql", "jackc/pgx", "clickhouse"},
			expectAny:   []string{tp("pkg/xtcp")},
		},
		{
			description: "positive: the agent library stands on identity and the contract, never on the controller",
			root:        tp("pkg/treatment/agent"),
			forbidden:   []string{tp("pkg/treatment/controller"), tp("pkg/treatment/experiment")},
			forbidden3p: []string{"database/sql", "jackc/pgx"},
			expectAny:   []string{tp("pkg/treatment/identity"), tp("pkg/treatment")},
		},
		{
			description: "positive: the domain package is proto-free and depends on nothing first-party",
			root:        tp("pkg/treatment"),
			forbidden:   []string{tp("gen/go/xtcp_treatment"), tp("pkg/treatment/wire"), tp("pkg/treatment/identity"), tp("pkg/treatment/controller")},
			forbidden3p: []string{"google.golang.org/protobuf", "google.golang.org/grpc", "database/sql"},
			expectAny:   []string{tp("pkg/treatment")},
		},
		{
			description: "negative: the identity package is a leaf below the domain and gRPC credentials, never the proto",
			root:        tp("pkg/treatment/identity"),
			forbidden:   []string{tp("gen/go/xtcp_treatment"), tp("pkg/treatment/wire"), tp("pkg/treatment/controller"), tp("pkg/treatment/agent")},
			forbidden3p: []string{"database/sql"},
			expectAny:   []string{tp("pkg/treatment")},
		},
		{
			// TODO(flat-record stubs): also forbid google.golang.org/grpc here
			// and for pkg/treatment/local. Today pkg/xtcpnl imports
			// gen/go/xtcp_flat_record, whose Go package carries the gRPC and
			// gateway stubs of that proto, so every goip user links gRPC
			// through the netlink library. The treatment packages themselves
			// are clean: the reconcile rule below forbids gRPC.
			description: "boundary: the watchdog never reaches the controller, a database, TLS identity, the agent or the RPC proto",
			root:        tp("cmd/xtcp2-treatmentctl"),
			forbidden:   []string{tp("pkg/treatment/controller"), tp("pkg/treatment/identity"), tp("pkg/treatment/agent"), tp("pkg/treatment/attribution"), tp("pkg/treatment/wire"), tp("pkg/treatment/experiment"), tp("gen/go/xtcp_treatment")},
			forbidden3p: []string{"database/sql", "jackc/pgx", "clickhouse", "google.golang.org/grpc/credentials"},
			expectAny:   []string{tp("pkg/treatment"), tp("pkg/treatment/local"), tp("pkg/treatment/reconcile")},
		},
		{
			description: "positive: the reconcile package is host-side only: journal proto and domain, never the RPC proto, gRPC, identity, goip or experiment statistics",
			root:        tp("pkg/treatment/reconcile"),
			forbidden:   []string{tp("pkg/treatment/identity"), tp("pkg/treatment/experiment"), tp("pkg/treatment/agent"), tp("pkg/treatment/controller"), tp("pkg/treatment/wire"), tp("gen/go/xtcp_treatment"), tp("internal/goip")},
			forbidden3p: []string{"database/sql", "google.golang.org/grpc"},
			expectAny:   []string{tp("pkg/treatment"), tp("gen/go/xtcp_treatment_journal")},
		},
		{
			description: "positive: the local package builds the kernel-backed reconciler without TLS or the RPC proto",
			root:        tp("pkg/treatment/local"),
			forbidden:   []string{tp("pkg/treatment/identity"), tp("pkg/treatment/agent"), tp("pkg/treatment/attribution"), tp("pkg/treatment/controller"), tp("pkg/treatment/experiment"), tp("pkg/treatment/wire"), tp("gen/go/xtcp_treatment")},
			forbidden3p: []string{"database/sql", "google.golang.org/grpc/credentials"},
			expectAny:   []string{tp("pkg/treatment/reconcile"), tp("internal/goip")},
		},
		{
			description: "positive: attribution reads journal snapshots and the flat record; never identity, goip, the agent or the RPC proto",
			root:        tp("pkg/treatment/attribution"),
			forbidden:   []string{tp("pkg/treatment/identity"), tp("pkg/treatment/agent"), tp("pkg/treatment/local"), tp("pkg/treatment/controller"), tp("pkg/treatment/experiment"), tp("pkg/treatment/wire"), tp("gen/go/xtcp_treatment"), tp("internal/goip")},
			forbidden3p: []string{"database/sql", "jackc/pgx"},
			expectAny:   []string{tp("pkg/treatment/reconcile"), tp("gen/go/xtcp_flat_record")},
		},
		{
			description: "positive: the experiment package is pure: no proto, no gRPC, no journal, no identity, no syscalls",
			root:        tp("pkg/treatment/experiment"),
			forbidden:   []string{tp("gen/go/xtcp_treatment"), tp("pkg/treatment/wire"), tp("pkg/treatment/reconcile"), tp("pkg/treatment/identity"), tp("pkg/treatment/agent"), tp("pkg/treatment/controller")},
			forbidden3p: []string{"google.golang.org/protobuf", "google.golang.org/grpc", "database/sql", "golang.org/x/sys"},
			expectAny:   []string{tp("pkg/treatment")},
		},
		{
			description: "positive: the store ports and the memory store know no driver and no proto",
			root:        tp("pkg/treatment/controller/store/memory"),
			forbidden:   []string{tp("gen/go/xtcp_treatment"), tp("pkg/treatment/wire"), tp("pkg/treatment/controller"), tp("pkg/treatment/controller/store/postgres")},
			forbidden3p: []string{"database/sql", "jackc/pgx", "google.golang.org/protobuf", "google.golang.org/grpc"},
			expectAny:   []string{tp("pkg/treatment/controller/store"), tp("pkg/treatment")},
		},
		{
			description: "corner: every SQL string lives in the postgres package, which never reaches the handlers",
			root:        tp("pkg/treatment/controller/store/postgres"),
			forbidden:   []string{tp("pkg/treatment/controller"), tp("pkg/treatment/wire"), tp("gen/go/xtcp_treatment"), tp("pkg/treatment/experiment")},
			expectAny:   []string{tp("pkg/treatment/controller/store")},
		},
		{
			description: "negative: the controller never reaches the host-side reconcile, local or attribution packages, goip or the journal proto",
			root:        tp("pkg/treatment/controller"),
			forbidden:   []string{tp("pkg/treatment/reconcile"), tp("pkg/treatment/local"), tp("pkg/treatment/attribution"), tp("internal/goip"), tp("gen/go/xtcp_treatment_journal")},
			expectAny:   []string{tp("pkg/treatment/experiment")},
		},
		{
			description: "corner: the controller never imports the agent (the dependency points one way)",
			root:        tp("pkg/treatment/controller"),
			forbidden:   []string{tp("pkg/treatment/agent"), tp("internal/goip")},
			expectAny:   []string{tp("pkg/treatment/identity")},
		},
	}
}

// go test ./pkg/treatment -run TestTreatmentImportBoundaries
func TestTreatmentImportBoundaries(t *testing.T) {
	for _, r := range treatmentBoundaryRules() {
		t.Run(r.description, func(t *testing.T) {
			closure, thirdParty := firstPartyClosure(t, r.root)
			for _, p := range r.forbidden {
				if closure[p] {
					t.Errorf("%s reaches %s; the chain is: %s", r.root, p, importChain(t, r.root, p))
				}
			}
			for imp, from := range thirdParty {
				for _, bad := range r.forbidden3p {
					if strings.Contains(imp+"\"", bad) {
						t.Errorf("%s reaches third-party %q via %s", r.root, imp, from)
					}
				}
			}
			for _, p := range r.expectAny {
				if !closure[p] {
					t.Errorf("%s no longer reaches %s; the walk may be vacuous, update expectAny if intentional", r.root, p)
				}
			}
		})
	}
}

type anImport struct{ path, file string }

func packageImports(t *testing.T, pkgPath string) []anImport {
	t.Helper()
	dir := filepath.Join(repoRoot, strings.TrimPrefix(pkgPath, modulePath+"/"))
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read package dir %s (for import %q): %v", dir, pkgPath, err)
	}
	var out []anImport
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
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

func isFirstParty(imp string) bool {
	return imp == modulePath || strings.HasPrefix(imp, modulePath+"/")
}

// firstPartyClosure follows non-test first-party imports from root and returns
// the packages reached plus third-party edges mapped to one importer.
func firstPartyClosure(t *testing.T, root string) (map[string]bool, map[string]string) {
	t.Helper()
	closure := map[string]bool{}
	thirdParty := map[string]string{}
	var walk func(string)
	walk = func(p string) {
		if closure[p] {
			return
		}
		closure[p] = true
		for _, imp := range packageImports(t, p) {
			switch {
			case isFirstParty(imp.path):
				walk(imp.path)
			case !strings.Contains(strings.SplitN(imp.path, "/", 2)[0], "."):
				// stdlib: only database/sql is interesting, record it as a third-party edge.
				if imp.path == "database/sql" {
					thirdParty[imp.path] = p
				}
			default:
				if _, seen := thirdParty[imp.path]; !seen {
					thirdParty[imp.path] = p
				}
			}
		}
	}
	walk(root)
	return closure, thirdParty
}

// importChain finds one path root -> ... -> target so a failure says where the
// edge came from instead of only that it exists.
func importChain(t *testing.T, root, target string) string {
	t.Helper()
	parent := map[string]string{root: ""}
	queue := []string{root}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		if p == target {
			var chain []string
			for q := p; q != ""; q = parent[q] {
				chain = append(chain, strings.TrimPrefix(q, modulePath+"/"))
			}
			sort.SliceStable(chain, func(i, j int) bool { return i > j })
			return strings.Join(chain, " -> ")
		}
		for _, imp := range packageImports(t, p) {
			if isFirstParty(imp.path) {
				if _, seen := parent[imp.path]; !seen {
					parent[imp.path] = p
					queue = append(queue, imp.path)
				}
			}
		}
	}
	return "(no chain found)"
}
