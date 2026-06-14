package main

import (
	"bytes"
	"go/ast"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeGo(t *testing.T, dir, name, src string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAuditTree_collectsDefinitions(t *testing.T) {
	dir := t.TempDir()
	writeGo(t, dir, "m.go", `
package x
import "github.com/prometheus/client_golang/prometheus"

var (
	used = prometheus.NewCounter(prometheus.CounterOpts{Name: "used"})
	orphan = prometheus.NewCounter(prometheus.CounterOpts{Name: "orphan"})
)

func use() {
	used.Inc()
}
`)
	defs, refs, err := auditTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 2 {
		t.Fatalf("expected 2 defs; got %d", len(defs))
	}
	if refs["used"] <= 1 {
		t.Errorf("used should be referenced > once; got %d", refs["used"])
	}
	if refs["orphan"] > 1 {
		t.Errorf("orphan should have only the def-site ref; got %d", refs["orphan"])
	}
}

func TestAuditTree_promauto(t *testing.T) {
	dir := t.TempDir()
	writeGo(t, dir, "m.go", `
package x
import "github.com/prometheus/client_golang/prometheus/promauto"
import "github.com/prometheus/client_golang/prometheus"

var c = promauto.NewCounterVec(prometheus.CounterOpts{Name: "x"}, []string{"a"})
func use() { c.WithLabelValues("a").Inc() }
`)
	defs, _, err := auditTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 || defs[0].metric != "NewCounterVec" {
		t.Errorf("expected NewCounterVec def; got %+v", defs)
	}
}

func TestAuditTree_skipsVendor(t *testing.T) {
	dir := t.TempDir()
	vendor := filepath.Join(dir, "vendor", "x")
	if err := os.MkdirAll(vendor, 0o755); err != nil {
		t.Fatal(err)
	}
	writeGo(t, vendor, "m.go", `
package x
import "github.com/prometheus/client_golang/prometheus"
var v = prometheus.NewCounter(prometheus.CounterOpts{Name: "v"})
`)
	defs, _, err := auditTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 0 {
		t.Errorf("vendor/ should be skipped; got %d defs", len(defs))
	}
}

func TestAuditTree_skipsTestAndPBFiles(t *testing.T) {
	dir := t.TempDir()
	writeGo(t, dir, "x_test.go", `
package x
import "github.com/prometheus/client_golang/prometheus"
var v = prometheus.NewCounter(prometheus.CounterOpts{Name: "v"})
`)
	writeGo(t, dir, "x.pb.go", `
package x
import "github.com/prometheus/client_golang/prometheus"
var v = prometheus.NewCounter(prometheus.CounterOpts{Name: "v"})
`)
	defs, _, err := auditTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 0 {
		t.Errorf("_test.go + .pb.go should be skipped; got %d defs", len(defs))
	}
}

func TestRunAudit_clean(t *testing.T) {
	dir := t.TempDir()
	writeGo(t, dir, "x.go", `
package x
import "github.com/prometheus/client_golang/prometheus"
var c = prometheus.NewCounter(prometheus.CounterOpts{Name: "c"})
func use() { c.Inc() }
`)
	var stdout, stderr bytes.Buffer
	rc := runAudit(dir, &stdout, &stderr)
	if rc != 0 {
		t.Errorf("clean rc = %d, want 0", rc)
	}
	if !strings.Contains(stdout.String(), "no findings") {
		t.Errorf("expected clean message; got %q", stdout.String())
	}
}

func TestRunAudit_orphan(t *testing.T) {
	dir := t.TempDir()
	writeGo(t, dir, "x.go", `
package x
import "github.com/prometheus/client_golang/prometheus"
var orphan = prometheus.NewCounter(prometheus.CounterOpts{Name: "orphan"})
`)
	var stdout, stderr bytes.Buffer
	rc := runAudit(dir, &stdout, &stderr)
	if rc != 1 {
		t.Errorf("orphan rc = %d, want 1", rc)
	}
	if !strings.Contains(stdout.String(), "orphan") {
		t.Errorf("expected orphan diagnostic; got %q", stdout.String())
	}
}

func TestRunAudit_walkError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if rc := runAudit("/no/such/path", &stdout, &stderr); rc != 2 {
		t.Errorf("missing root rc = %d, want 2", rc)
	}
}

func TestPromNewKind_unknownPackage(t *testing.T) {
	// Exercised via auditTree on a file that uses a non-prometheus package.
	dir := t.TempDir()
	writeGo(t, dir, "x.go", `
package x
import "fmt"
var c = fmt.Sprintf("x")
`)
	defs, _, err := auditTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 0 {
		t.Errorf("non-prometheus call should not be captured; got %+v", defs)
	}
}

func TestRunMain_clean(t *testing.T) {
	dir := t.TempDir()
	writeGo(t, dir, "x.go", `package x`)
	var stdout, stderr bytes.Buffer
	if rc := runMain([]string{"-root", dir}, &stdout, &stderr); rc != 0 {
		t.Errorf("rc = %d, want 0; stderr=%s", rc, stderr.String())
	}
}

func TestRunMain_invalidFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if rc := runMain([]string{"-not-a-flag"}, &stdout, &stderr); rc != 2 {
		t.Errorf("invalid flag rc = %d, want 2", rc)
	}
}

func TestPromNewKind_unitDispatch(t *testing.T) {
	// Non-CallExpr (e.g. plain Ident) → ""
	if got := promNewKind(&ast.Ident{Name: "x"}); got != "" {
		t.Errorf("non-CallExpr: got %q, want \"\"", got)
	}
	// CallExpr with non-SelectorExpr Fun → ""
	if got := promNewKind(&ast.CallExpr{Fun: &ast.Ident{Name: "f"}}); got != "" {
		t.Errorf("CallExpr/Ident Fun: got %q, want \"\"", got)
	}
	// CallExpr with SelectorExpr but non-Ident X → ""
	if got := promNewKind(&ast.CallExpr{Fun: &ast.SelectorExpr{
		X:   &ast.SelectorExpr{}, // not an Ident
		Sel: &ast.Ident{Name: "NewCounter"},
	}}); got != "" {
		t.Errorf("nested SelectorExpr X: got %q, want \"\"", got)
	}
	// Known prometheus.NewCounter call → "NewCounter"
	if got := promNewKind(&ast.CallExpr{Fun: &ast.SelectorExpr{
		X:   &ast.Ident{Name: "prometheus"},
		Sel: &ast.Ident{Name: "NewCounter"},
	}}); got != "NewCounter" {
		t.Errorf("prometheus.NewCounter: got %q, want NewCounter", got)
	}
	// Known promauto.NewGauge → "NewGauge"
	if got := promNewKind(&ast.CallExpr{Fun: &ast.SelectorExpr{
		X:   &ast.Ident{Name: "promauto"},
		Sel: &ast.Ident{Name: "NewGauge"},
	}}); got != "NewGauge" {
		t.Errorf("promauto.NewGauge: got %q, want NewGauge", got)
	}
	// Unsupported prometheus.Foo → ""
	if got := promNewKind(&ast.CallExpr{Fun: &ast.SelectorExpr{
		X:   &ast.Ident{Name: "prometheus"},
		Sel: &ast.Ident{Name: "NotAConstructor"},
	}}); got != "" {
		t.Errorf("prometheus.NotAConstructor: got %q, want \"\"", got)
	}
}
