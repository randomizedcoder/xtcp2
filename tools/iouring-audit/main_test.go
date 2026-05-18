package main

import (
	"bytes"
	"go/ast"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeGo is a tiny helper that drops a .go file into dir/name with the
// given source body.
func writeGo(t *testing.T, dir, name, src string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAuditTree_emptyDir(t *testing.T) {
	dir := t.TempDir()
	res, err := auditTree(dir)
	if err != nil {
		t.Fatalf("auditTree empty: %v", err)
	}
	if res.GetSQE != 0 || res.Submit != 0 {
		t.Errorf("empty tree should yield 0/0; got %+v", res)
	}
}

func TestAuditTree_clean(t *testing.T) {
	dir := t.TempDir()
	writeGo(t, dir, "ring.go", `
package ring
type R struct{}
func (r *R) GetSQE() {}
func (r *R) Submit() {}
func use(r *R) {
	r.GetSQE()
	r.Submit()
}
`)
	res, err := auditTree(dir)
	if err != nil {
		t.Fatalf("auditTree: %v", err)
	}
	if res.GetSQE != 1 {
		t.Errorf("GetSQE count = %d, want 1", res.GetSQE)
	}
	if res.Submit != 1 {
		t.Errorf("Submit count = %d, want 1", res.Submit)
	}
}

func TestAuditTree_skipsTestFiles(t *testing.T) {
	dir := t.TempDir()
	writeGo(t, dir, "ring_test.go", `
package ring
type fake struct{}
func (f *fake) GetSQE() {}
`)
	res, err := auditTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	if res.GetSQE != 0 {
		t.Errorf("_test.go file should be skipped; got GetSQE=%d", res.GetSQE)
	}
}

func TestAuditTree_walkError(t *testing.T) {
	_, err := auditTree("/no/such/path")
	if err == nil {
		t.Error("missing root should produce an error")
	}
}

func TestRunAudit_clean(t *testing.T) {
	dir := t.TempDir()
	writeGo(t, dir, "ring.go", `
package ring
type R struct{}
func (r *R) GetSQE() {}
func (r *R) Submit() {}
func use(r *R) { r.GetSQE(); r.Submit() }
`)
	var stdout, stderr bytes.Buffer
	rc := runAudit(dir, &stdout, &stderr)
	if rc != 0 {
		t.Errorf("clean tree rc = %d, want 0", rc)
	}
	if !strings.Contains(stdout.String(), "no findings") {
		t.Errorf("stdout should report clean; got %q", stdout.String())
	}
}

func TestRunAudit_unmatchedGetSQE(t *testing.T) {
	dir := t.TempDir()
	writeGo(t, dir, "ring.go", `
package ring
type R struct{}
func (r *R) GetSQE() {}
func use(r *R) { r.GetSQE(); r.GetSQE() }
`)
	var stdout, stderr bytes.Buffer
	rc := runAudit(dir, &stdout, &stderr)
	if rc != 1 {
		t.Errorf("unmatched GetSQE rc = %d, want 1", rc)
	}
	if !strings.Contains(stderr.String(), "no submission call") {
		t.Errorf("stderr missing diagnostic; got %q", stderr.String())
	}
}

func TestRunAudit_walkError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runAudit("/no/such/path", &stdout, &stderr)
	if rc != 2 {
		t.Errorf("missing path rc = %d, want 2", rc)
	}
}

func TestSelectorName(t *testing.T) {
	// Build small ast.Expr instances and probe selectorName.
	// SelectorExpr → "Name"
	// Ident → "Name"
	// Anything else → ""
	// We use auditTree on a synthetic file to exercise both branches.
	dir := t.TempDir()
	writeGo(t, dir, "x.go", `
package x
func ident()  {}
func use() { ident() } // direct call → Ident path
type R struct{}
func (r R) M() {}
func use2(r R) { r.M() } // SelectorExpr path
`)
	if _, err := auditTree(dir); err != nil {
		t.Fatal(err)
	}
}

func TestSelectorName_unitDispatch(t *testing.T) {
	// Direct calls cover all three branches of selectorName.
	if got := selectorName(&ast.Ident{Name: "x"}); got != "x" {
		t.Errorf("Ident branch: got %q, want x", got)
	}
	if got := selectorName(&ast.SelectorExpr{Sel: &ast.Ident{Name: "M"}}); got != "M" {
		t.Errorf("SelectorExpr branch: got %q, want M", got)
	}
	// BasicLit is neither — exercises the catch-all return "".
	if got := selectorName(&ast.BasicLit{Value: "1"}); got != "" {
		t.Errorf("BasicLit catch-all: got %q, want \"\"", got)
	}
}
