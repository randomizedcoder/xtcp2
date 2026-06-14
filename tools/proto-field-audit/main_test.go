package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name, src string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSnakeToCamel(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"hello", "Hello"},
		{"hello_world", "HelloWorld"},
		{"tcp_info_state", "TcpInfoState"},
		{"a_b_c_d", "ABCD"},
		{"", ""},
		{"_leading", "Leading"},
		{"trailing_", "Trailing"},
	}
	for _, tc := range cases {
		if got := snakeToCamel(tc.in); got != tc.want {
			t.Errorf("snakeToCamel(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCollectProtoFields(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "schema.proto", `
syntax = "proto3";
message Foo {
  string hostname = 1;
  uint64 timestamp_ns = 2;
  // a comment line that should be skipped
  uint32 tcp_info_state = 3;
}
`)
	fields, err := collectProtoFields(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(fields) < 3 {
		t.Fatalf("expected ≥3 fields; got %d: %+v", len(fields), fields)
	}
	names := map[string]bool{}
	for _, f := range fields {
		names[f.name] = true
	}
	for _, want := range []string{"hostname", "timestamp_ns", "tcp_info_state"} {
		if !names[want] {
			t.Errorf("expected to find field %q in %v", want, names)
		}
	}
}

func TestCollectProtoFields_missing(t *testing.T) {
	_, err := collectProtoFields("/no/such/path")
	if err == nil {
		t.Error("missing protoRoot should error")
	}
}

func TestCollectProtoFields_nestedMessages(t *testing.T) {
	dir := t.TempDir()
	// Nested + commented + non-message body covers the inMessage counter
	// branches: "{ " on a separate line increments, "}" decrements, comments
	// and empty lines short-circuit before the regex.
	writeFile(t, dir, "nested.proto", `
syntax = "proto3";
message Outer {
  message Inner
  {
    string nested_field = 1;
  }
  string outer_field = 2;
  //
}
`)
	fields, err := collectProtoFields(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, f := range fields {
		names[f.name] = true
	}
	for _, want := range []string{"nested_field", "outer_field"} {
		if !names[want] {
			t.Errorf("expected to find %q; got %v", want, names)
		}
	}
}

func TestCollectGoReferences_parseError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "bad.go", "this is not valid Go")
	_, err := collectGoReferences(dir)
	if err == nil {
		t.Error("malformed .go should propagate parse error")
	}
}

func TestRunMain_clean(t *testing.T) {
	protoDir := t.TempDir()
	goDir := t.TempDir()
	writeFile(t, protoDir, "schema.proto", "syntax = \"proto3\";")
	writeFile(t, goDir, "x.go", "package x")
	var stdout, stderr bytes.Buffer
	if rc := runMain([]string{"-proto-root", protoDir, "-go-root", goDir}, &stdout, &stderr); rc != 0 {
		t.Errorf("rc = %d, want 0; stderr=%s", rc, stderr.String())
	}
}

func TestRunMain_invalidFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if rc := runMain([]string{"-not-a-flag"}, &stdout, &stderr); rc != 2 {
		t.Errorf("invalid flag rc = %d, want 2", rc)
	}
}

func TestCollectGoReferences(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "x.go", `
package x
type T struct { Hostname string }
func (t T) SetHostname(s string) { t.Hostname = s }
func use(t T) { _ = t.Hostname; t.SetHostname("foo") }
`)
	refs, err := collectGoReferences(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !refs["Hostname"] {
		t.Error("Hostname selector should be picked up")
	}
	if !refs["SetHostname"] {
		t.Error("SetHostname call should be picked up")
	}
}

func TestCollectGoReferences_skipsVendor(t *testing.T) {
	dir := t.TempDir()
	vendor := filepath.Join(dir, "vendor", "x")
	if err := os.MkdirAll(vendor, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, vendor, "v.go", `
package x
type T struct { ShouldBeSkipped string }
`)
	refs, err := collectGoReferences(dir)
	if err != nil {
		t.Fatal(err)
	}
	if refs["ShouldBeSkipped"] {
		t.Error("vendor/ should be skipped")
	}
}

func TestRunAudit_clean(t *testing.T) {
	dir := t.TempDir()
	protoDir := filepath.Join(dir, "proto")
	goDir := filepath.Join(dir, "go")
	if err := os.MkdirAll(protoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(goDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, protoDir, "x.proto", `
syntax = "proto3";
message Foo {
  string hostname = 1;
}
`)
	writeFile(t, goDir, "x.go", `
package x
type T struct { Hostname string }
func use(t T) { _ = t.Hostname }
`)
	var stdout, stderr bytes.Buffer
	if rc := runAudit(protoDir, goDir, &stdout, &stderr); rc != 0 {
		t.Errorf("clean rc = %d, want 0", rc)
	}
}

func TestRunAudit_unsetField(t *testing.T) {
	dir := t.TempDir()
	protoDir := filepath.Join(dir, "proto")
	goDir := filepath.Join(dir, "go")
	if err := os.MkdirAll(protoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(goDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, protoDir, "x.proto", `
syntax = "proto3";
message Foo {
  string never_written = 1;
}
`)
	writeFile(t, goDir, "x.go", `
package x
type T struct{}
`)
	var stdout, stderr bytes.Buffer
	rc := runAudit(protoDir, goDir, &stdout, &stderr)
	if rc != 1 {
		t.Errorf("unset rc = %d, want 1", rc)
	}
	if !strings.Contains(stdout.String(), "never_written") {
		t.Errorf("stdout missing field name; got %q", stdout.String())
	}
}

func TestRunAudit_protoError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runAudit("/no/such/proto/path", t.TempDir(), &stdout, &stderr)
	if rc != 2 {
		t.Errorf("missing protoRoot rc = %d, want 2", rc)
	}
}

func TestRunAudit_goError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "x.proto", "syntax = \"proto3\";\nmessage X {\n  string n = 1;\n}\n")
	var stdout, stderr bytes.Buffer
	rc := runAudit(dir, "/no/such/go/path", &stdout, &stderr)
	if rc != 2 {
		t.Errorf("missing goRoot rc = %d, want 2", rc)
	}
}
