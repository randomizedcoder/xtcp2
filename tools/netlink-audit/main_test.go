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

func TestIsByteSliceExpr(t *testing.T) {
	// Behavioral: a function body that indexes `b` should be flagged
	// when no len() guard exists. Tests below cover both branches.
}

func TestRunMain_clean(t *testing.T) {
	dir := t.TempDir()
	writeGo(t, dir, "x.go", `
package x
func f(b []byte) byte {
	if len(b) < 4 { return 0 }
	return b[0]
}
`)
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

func TestIsByteSliceExpr_unitDispatch(t *testing.T) {
	// Direct calls cover all branches of isByteSliceExpr.
	for _, name := range []string{"b", "buf", "buffer", "data", "msg", "raw", "p", "payload"} {
		if !isByteSliceExpr(&ast.Ident{Name: name}) {
			t.Errorf("Ident(%q) should be byte-slice", name)
		}
	}
	// Ident with unknown name → false
	if isByteSliceExpr(&ast.Ident{Name: "x"}) {
		t.Error("Ident('x') should NOT be byte-slice")
	}
	// Non-Ident → false
	if isByteSliceExpr(&ast.BasicLit{Value: "1"}) {
		t.Error("BasicLit should NOT be byte-slice")
	}
}

func TestAuditTree_lenGuardedIsClean(t *testing.T) {
	dir := t.TempDir()
	writeGo(t, dir, "guarded.go", `
package x
func f(b []byte) byte {
	if len(b) < 4 { return 0 }
	return b[0]
}
`)
	findings, err := auditTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Errorf("guarded fn should produce 0 findings; got %d: %+v",
			len(findings), findings)
	}
}

func TestAuditTree_unguardedIndexAccess(t *testing.T) {
	dir := t.TempDir()
	writeGo(t, dir, "unguarded.go", `
package x
func f(buf []byte) byte {
	return buf[0]
}
`)
	findings, err := auditTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding; got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].msg, "index access") {
		t.Errorf("msg = %q, want \"index access\"", findings[0].msg)
	}
}

func TestAuditTree_unguardedSliceExpr(t *testing.T) {
	dir := t.TempDir()
	writeGo(t, dir, "unguarded.go", `
package x
func f(data []byte) []byte {
	return data[0:4]
}
`)
	findings, err := auditTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding; got %d", len(findings))
	}
	if !strings.Contains(findings[0].msg, "slice expression") {
		t.Errorf("msg = %q, want \"slice expression\"", findings[0].msg)
	}
}

// go test ./tools/netlink-audit/ -run TestAuditTreeSliceForms
//
// The bound-free `x[:]` was a false-positive source: pkg/xtcpnl's request
// builders each hold a fixed-size scratch array and hand `raw[:]` to an
// AttrBuilder, which the audit flagged and no len() call could have fixed.
// Every other slice form carries a caller-supplied bound and stays in scope,
// so this table is what pins the line between the two.
func TestAuditTreeSliceForms(t *testing.T) {
	tests := []struct {
		description  string
		src          string
		wantFindings int
	}{
		{
			description: "positive: a whole-slice raw[:] over a fixed-size array is not a finding",
			src: `package x
func f() []byte {
	var raw [16]byte
	return raw[:]
}`,
			wantFindings: 0,
		},
		{
			description: "positive: a whole-slice b[:] over a slice parameter is not a finding either, being an identity reslice",
			src: `package x
func f(b []byte) []byte { return b[:] }`,
			wantFindings: 0,
		},
		{
			description: "negative: a high bound is caller-supplied, so data[:n] is still a finding",
			src: `package x
func f(data []byte, n int) []byte { return data[:n] }`,
			wantFindings: 1,
		},
		{
			description: "negative: a low bound is caller-supplied too, so buf[n:] is still a finding",
			src: `package x
func f(buf []byte, n int) []byte { return buf[n:] }`,
			wantFindings: 1,
		},
		{
			description: "boundary: the full slice form p[:n:m] has Low absent but two bounds present, so it is a finding",
			src: `package x
func f(p []byte, n, m int) []byte { return p[:n:m] }`,
			wantFindings: 1,
		},
		{
			description: "boundary: a constant bound is still a bound; msg[:4] is a finding",
			src: `package x
func f(msg []byte) []byte { return msg[:4] }`,
			wantFindings: 1,
		},
		{
			description: "corner: an index access is unaffected by the whole-slice rule",
			src: `package x
func f() byte {
	var raw [16]byte
	return raw[0]
}`,
			wantFindings: 1,
		},
		{
			description: "corner: a len() anywhere still silences the whole function, bounds or not",
			src: `package x
func f(data []byte, n int) []byte {
	if len(data) < n { return nil }
	return data[:n]
}`,
			wantFindings: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			dir := t.TempDir()
			writeGo(t, dir, "x.go", tt.src)
			findings, err := auditTree(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(findings) != tt.wantFindings {
				t.Errorf("findings = %d, want %d: %+v",
					len(findings), tt.wantFindings, findings)
			}
		})
	}
}

func TestAuditTree_skipsTestFiles(t *testing.T) {
	dir := t.TempDir()
	writeGo(t, dir, "fixture_test.go", `
package x
func bad(buf []byte) byte { return buf[0] }
`)
	findings, err := auditTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Errorf("_test.go files should be skipped; got %d findings", len(findings))
	}
}

func TestAuditTree_skipsPBFiles(t *testing.T) {
	dir := t.TempDir()
	writeGo(t, dir, "foo.pb.go", `
package x
func bad(buf []byte) byte { return buf[0] }
`)
	findings, err := auditTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Errorf(".pb.go files should be skipped; got %d findings", len(findings))
	}
}

func TestRunAudit_clean(t *testing.T) {
	dir := t.TempDir()
	writeGo(t, dir, "x.go", `package x
func f(b []byte) byte {
	if len(b) < 4 { return 0 }
	return b[0]
}
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

func TestRunAudit_withFindings(t *testing.T) {
	dir := t.TempDir()
	writeGo(t, dir, "x.go", `package x
func bad(buf []byte) byte { return buf[0] }
`)
	var stdout, stderr bytes.Buffer
	rc := runAudit(dir, &stdout, &stderr)
	if rc != 1 {
		t.Errorf("findings rc = %d, want 1", rc)
	}
	if !strings.Contains(stderr.String(), "finding(s)") {
		t.Errorf("stderr missing diagnostic; got %q", stderr.String())
	}
}

func TestRunAudit_walkError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if rc := runAudit("/no/such/path", &stdout, &stderr); rc != 2 {
		t.Errorf("missing root rc = %d, want 2", rc)
	}
}
