package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadProtobufFromFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.proto")
	if err := os.WriteFile(p, []byte("syntax = \"proto3\";"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readProtobufFromFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "proto3") {
		t.Errorf("readProtobufFromFile = %q", got)
	}
}

func TestReadProtobufFromFile_missing(t *testing.T) {
	_, err := readProtobufFromFile("/no/such/file.proto")
	if err == nil {
		t.Error("missing file should error")
	}
}

func TestRegisterProtobufSchemaAt_happy(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body) //nolint:errcheck // test plumbing
		gotBody = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	if err := registerProtobufSchemaAt(srv.Client(), srv.URL, "test-subject", "schema-body"); err != nil {
		t.Fatalf("register err: %v", err)
	}
	var got SchemaRequest
	if err := json.Unmarshal([]byte(gotBody), &got); err != nil {
		t.Fatalf("server body not JSON: %v", err)
	}
	if got.Schema != "schema-body" || got.SchemaType != "PROTOBUF" {
		t.Errorf("body mismatch: %+v", got)
	}
}

func TestRegisterProtobufSchemaAt_serverError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	if err := registerProtobufSchemaAt(srv.Client(), srv.URL, "s", "schema"); err == nil {
		t.Error("500 response should produce an error")
	}
}

func TestRegisterProtobufSchemaAt_connRefused(t *testing.T) {
	// Spin up + immediately close to get a known-bad URL.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()
	if err := registerProtobufSchemaAt(http.DefaultClient, url, "s", "x"); err == nil {
		t.Error("conn-refused should produce error")
	}
}

func TestGetLatestSchemaIDAt_happy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":42}`)) //nolint:errcheck // test plumbing
	}))
	defer srv.Close()
	got, err := getLatestSchemaIDAt(srv.Client(), srv.URL, "subject")
	if err != nil {
		t.Fatalf("get err: %v", err)
	}
	if got != 42 {
		t.Errorf("id = %d, want 42", got)
	}
}

func TestGetLatestSchemaIDAt_badJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json")) //nolint:errcheck // test plumbing
	}))
	defer srv.Close()
	if _, err := getLatestSchemaIDAt(srv.Client(), srv.URL, "subject"); err == nil {
		t.Error("bad JSON should produce error")
	}
}

func TestGetLatestSchemaIDAt_connRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()
	if _, err := getLatestSchemaIDAt(http.DefaultClient, url, "subject"); err == nil {
		t.Error("conn-refused should produce error")
	}
}
