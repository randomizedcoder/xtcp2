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
//	pkg/treatment             contract: IDs, limits, validators      [everyone]
//	pkg/treatment/identity    SPIFFE identity + TLS                  [agent, controller, tools]
//	pkg/treatment/reconcile   journal + reconciler + Backend         [agent, watchdog]
//	pkg/treatment/experiment  statistics + state machine             [controller]
//	pkg/treatment/agent       check-in loop + stamping               [daemon, agent binary]
//	pkg/treatment/controller  gRPC handlers, store, evidence         [controller binary only]
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
			forbidden3p: []string{"database/sql", "lib/pq", "jackc/pgx", "clickhouse"},
			expectAny:   []string{tp("pkg/xtcp")},
		},
		{
			description: "positive: the agent library stands on identity and the contract, never on the controller",
			root:        tp("pkg/treatment/agent"),
			forbidden:   []string{tp("pkg/treatment/controller"), tp("pkg/treatment/experiment")},
			forbidden3p: []string{"database/sql", "lib/pq", "jackc/pgx"},
			expectAny:   []string{tp("pkg/treatment/identity"), tp("pkg/treatment")},
		},
		{
			// TODO(item 2): add gen/go/xtcp_controller to forbidden once the
			// contract package stops importing the proto (domain types land).
			description: "negative: the identity package is a leaf below the contract and gRPC credentials",
			root:        tp("pkg/treatment/identity"),
			forbidden:   []string{tp("pkg/treatment/controller"), tp("pkg/treatment/agent")},
			forbidden3p: []string{"database/sql"},
			expectAny:   []string{tp("pkg/treatment")},
		},
		{
			// TODO(item 2): forbid pkg/treatment/identity and google.golang.org/grpc
			// once the watchdog builds on pkg/treatment/reconcile instead of agent.
			description: "boundary: the watchdog never reaches the controller or a database",
			root:        tp("cmd/xtcp2-treatmentctl"),
			forbidden:   []string{tp("pkg/treatment/controller")},
			forbidden3p: []string{"database/sql", "lib/pq", "jackc/pgx"},
			expectAny:   []string{tp("pkg/treatment")},
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
