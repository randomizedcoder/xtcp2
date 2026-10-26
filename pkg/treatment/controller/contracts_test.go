package controller

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"
)

type trackedBody struct {
	io.Reader
	closed bool
}

func (b *trackedBody) Close() error { b.closed = true; return nil }

type brokenReader struct{}

func (brokenReader) Read([]byte) (int, error) { return 0, errors.New("broken response") }

func TestClickHouseTransportContracts(t *testing.T) {
	for _, tc := range []struct {
		name, description, table, url, body, wantError string
		status                                         int
		transportError, readError                      bool
	}{
		{name: "valid", description: "query is parameterized and authenticated", status: 200, body: "ok"},
		{name: "table", description: "table names cannot inject SQL", table: "db.rows;DROP TABLE rows", wantError: "database.table"},
		{name: "scheme", description: "only HTTP(S) transports are accepted", url: "file:///tmp/rows", wantError: "HTTP(S)"},
		{name: "http-error", description: "server errors are surfaced", status: 500, body: "query failed", wantError: "status 500"},
		{name: "network", description: "transport errors are surfaced", transportError: true, wantError: "offline"},
		{name: "read", description: "partial response errors fail analysis", status: 200, readError: true, wantError: "broken response"},
		{name: "size-limit", description: "oversized responses fail closed", status: 200, body: strings.Repeat("x", 8<<20), wantError: "exceeds"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			table := tc.table
			if table == "" {
				table = "db.rows"
			}
			endpoint := tc.url
			if endpoint == "" {
				endpoint = "https://fixture.invalid/"
			}
			body := &trackedBody{Reader: strings.NewReader(tc.body)}
			if tc.readError {
				body.Reader = brokenReader{}
			}
			called := false
			c := &ClickHouse{URL: endpoint, Table: table, Username: "user", Password: "test-secret", Client: &http.Client{Transport: discoveryTransport(func(r *http.Request) (*http.Response, error) {
				called = true
				if tc.transportError {
					return nil, errors.New("offline")
				}
				u, p, ok := r.BasicAuth()
				if !ok || u != "user" || p != "test-secret" {
					t.Fatal("missing authentication")
				}
				query, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatal(err)
				}
				if string(query) != "SELECT * FROM db.rows" || r.Method != "POST" || r.URL.Query().Get("param_location") != "dc'quote" || r.URL.Query().Get("result_overflow_mode") != "throw" {
					t.Fatal("query contract changed")
				}
				return &http.Response{StatusCode: tc.status, Body: body, Header: make(http.Header)}, nil
			})}}
			got, err := c.query(context.Background(), "SELECT * FROM {{TABLE}}", map[string]string{"location": "dc'quote"})
			if tc.wantError == "" {
				if err != nil || string(got) != tc.body {
					t.Fatalf("%s: %q %v", tc.description, got, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("%s: want %q got %v", tc.description, tc.wantError, err)
			}
			if called && !tc.transportError && !body.closed {
				t.Fatal("response body leaked")
			}
			if (tc.table != "" || tc.url != "") && called {
				t.Fatal("invalid configuration reached transport")
			}
		})
	}
}

func TestClickHouseEvidenceContracts(t *testing.T) {
	const host = `{"agent_id":"host","sockets":30,"bytes_acked":1048576,"goodput_bps":1000,"rtt_p95_ms":5,"collection_healthy":true}`
	now := time.Unix(1700000000, 0)
	for _, tc := range []struct {
		name, description, body string
		destination             netip.Prefix
		agents                  []string
		window                  time.Duration
		invalidDestination      bool
		wantError               string
	}{
		{name: "valid", description: "complete response records provenance", body: host},
		{name: "empty", description: "empty response cannot renew a lease", wantError: "population"},
		{name: "malformed", description: "malformed JSON is rejected", body: "{", wantError: "EOF"},
		{name: "duplicate", description: "duplicate hosts cannot inflate sample size", body: host + "\n" + host, wantError: "duplicate"},
		{name: "bad-window", description: "empty analysis interval is rejected", window: -time.Second, wantError: "window"},
		{name: "long-window", description: "analysis is limited to seven days", window: 7*24*time.Hour + time.Second, wantError: "window"},
		{name: "bad-agent", description: "agent IDs cannot inject query parameters", agents: []string{"host'"}, wantError: "invalid agent"},
		{name: "bad-destination", description: "an invalid destination fails before the query", destination: netip.Prefix{}, invalidDestination: true, wantError: "invalid destination"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			window := tc.window
			if window == 0 {
				window = time.Minute
			}
			agents := tc.agents
			if agents == nil {
				agents = []string{"host"}
			}
			dest := tc.destination
			if !tc.invalidDestination {
				dest = netip.MustParsePrefix("192.0.2.1/32")
			}
			c := &ClickHouse{URL: "http://fixture.invalid", Table: "db.rows", Now: func() time.Time { return now }, Client: &http.Client{Transport: discoveryTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Query().Get("param_destination_hex") != "C0000201000000000000000000000000" {
					t.Fatal("wrong destination encoding")
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header)}, nil
			})}}
			ev, err := c.Evidence(context.Background(), "dc", dest, "assignment", agents, now.Add(-window), now)
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("%s: want %q got %v", tc.description, tc.wantError, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(ev.Hosts) != 1 || len(ev.ArtifactSHA256) != 64 || ev.QueryVersion != "tcp-host-deltas-v1" || !ev.AnalyzedAt.Equal(now) {
				t.Fatalf("%s: bad evidence %v", tc.description, ev)
			}
		})
	}
}
