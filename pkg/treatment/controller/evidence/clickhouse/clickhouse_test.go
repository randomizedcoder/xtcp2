package clickhouse

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/evidence"
)

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type trackedBody struct {
	io.Reader
	closed bool
}

func (b *trackedBody) Close() error { b.closed = true; return nil }

type brokenReader struct{}

func (brokenReader) Read([]byte) (int, error) { return 0, errors.New("broken response") }

// fixture answers every query with body and records the request it saw.
func fixture(now time.Time, body string, saw func(*http.Request)) *Client {
	return &Client{URL: "http://fixture.invalid", Table: "db.rows", Now: func() time.Time { return now }, HTTP: &http.Client{Transport: roundTripper(func(r *http.Request) (*http.Response, error) {
		if saw != nil {
			saw(r)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}}
}

func TestClickHouseTransportContracts(t *testing.T) {
	for _, tc := range []struct {
		name, description, table, url, body, wantError string
		status                                         int
		transportError, readError                      bool
	}{
		{name: "valid", description: "positive: query is parameterized and authenticated", status: 200, body: "ok"},
		{name: "table", description: "negative: table names cannot inject SQL", table: "db.rows;DROP TABLE rows", wantError: "database.table"},
		{name: "scheme", description: "negative: only HTTP(S) transports are accepted", url: "file:///tmp/rows", wantError: "HTTP(S)"},
		{name: "http-error", description: "negative: server errors are surfaced", status: 500, body: "query failed", wantError: "status 500"},
		{name: "network", description: "negative: transport errors are surfaced", transportError: true, wantError: "offline"},
		{name: "read", description: "corner: partial response errors fail analysis", status: 200, readError: true, wantError: "broken response"},
		{name: "size-limit", description: "boundary: oversized responses fail closed", status: 200, body: strings.Repeat("x", 8<<20), wantError: "exceeds"},
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
			c := &Client{URL: endpoint, Table: table, Username: "user", Password: "test-secret", HTTP: &http.Client{Transport: roundTripper(func(r *http.Request) (*http.Response, error) {
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
		// beforeQuery says the error must come from Window.Validate, so the
		// warehouse is never contacted.
		beforeQuery bool
		wantError   string
	}{
		{name: "valid", description: "positive: complete response records provenance", body: host},
		{name: "empty", description: "negative: empty response cannot renew a lease", wantError: "population"},
		{name: "malformed", description: "negative: malformed JSON is rejected", body: "{", wantError: "EOF"},
		{name: "duplicate", description: "negative: duplicate hosts cannot inflate sample size", body: host + "\n" + host, wantError: "duplicate"},
		{name: "bad-window", description: "boundary: empty analysis interval is rejected", window: -time.Second, beforeQuery: true, wantError: "window"},
		{name: "long-window", description: "boundary: analysis is limited to seven days", window: evidence.MaxWindow + time.Second, beforeQuery: true, wantError: "window"},
		{name: "bad-agent", description: "negative: agent IDs cannot inject query parameters", agents: []string{"host'"}, beforeQuery: true, wantError: "invalid agent"},
		{name: "bad-destination", description: "corner: an invalid destination fails before the query", destination: netip.Prefix{}, invalidDestination: true, beforeQuery: true, wantError: "invalid destination"},
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
			called := false
			c := fixture(now, tc.body, func(r *http.Request) {
				called = true
				q := r.URL.Query()
				if q.Get("param_destination_hex") != "C0000201000000000000000000000000" || q.Get("param_assignment") != "assignment" || q.Get("param_agents") != "['host']" {
					t.Fatalf("wrong query parameters: %s", r.URL.RawQuery)
				}
			})
			ev, err := c.Evidence(context.Background(), evidence.Window{Location: "dc", Destination: dest, ExperimentID: "assignment", Agents: agents, Start: now.Add(-window), End: now})
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("%s: want %q got %v", tc.description, tc.wantError, err)
				}
				if tc.beforeQuery && called {
					t.Fatalf("%s: invalid window reached the warehouse", tc.description)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(ev.Hosts) != 1 || len(ev.ArtifactSHA256) != 64 || ev.QueryVersion != QueryVersion || ev.EnrichmentVersion != EnrichmentVersion || !ev.AnalyzedAt.Equal(now) || !ev.WindowEnd.Equal(now) {
				t.Fatalf("%s: bad evidence %+v", tc.description, ev)
			}
		})
	}
}

func TestClickHouseCandidateContracts(t *testing.T) {
	now := time.Unix(1700000000, 0)
	agents := `["h1","h2","h3","h4","h5","h6"]`
	v4 := `{"destination_hex":"C0000201000000000000000000000000","family":2,"asn":64512,"agents":` + agents + `,"rtt_ms":5.5,"retransmission_ratio":0.01}`
	v6 := `{"destination_hex":"20010DB8000000000000000000000001","family":10,"asn":64513,"agents":` + agents + `,"rtt_ms":80,"retransmission_ratio":0}`
	for _, tc := range []struct {
		name, description, body string
		query                   evidence.CandidateQuery
		want                    []string
		wantASN                 uint64
		wantError               string
	}{
		{name: "v4", description: "positive: an IPv4 row decodes to a /32 with its population", body: v4, want: []string{"192.0.2.1/32"}, wantASN: 64512},
		{name: "v6", description: "positive: an IPv6 row decodes to a /128", body: v6, want: []string{"2001:db8::1/128"}, wantASN: 64513},
		{name: "ordered", description: "positive: rows keep the warehouse's best-first order", body: v6 + "\n" + v4, want: []string{"2001:db8::1/128", "192.0.2.1/32"}, wantASN: 64513},
		{name: "empty", description: "boundary: no rows is no candidates, not an error", body: ""},
		{name: "malformed", description: "negative: malformed JSON is an error", body: "{", wantError: "EOF"},
		{name: "bad-hex", description: "negative: an undecodable destination is a schema error, not a skipped row", body: `{"destination_hex":"ZZ","family":2,"agents":` + agents + `}`, wantError: "candidate destination"},
		{name: "short", description: "corner: too few bytes for the family is a schema error", body: `{"destination_hex":"C000","family":10,"agents":` + agents + `}`, wantError: "not an address"},
		{name: "bad-agent", description: "negative: an agent id that cannot be a parameter poisons the row", body: `{"destination_hex":"C0000201000000000000000000000000","family":2,"agents":["ok","bad'"]}`, wantError: "invalid agent id"},
		{name: "bad-bounds", description: "negative: inverted RTT bounds never reach the warehouse", body: v4, query: evidence.CandidateQuery{Location: "dc", Start: now.Add(-time.Hour), End: now, MinimumRTTMs: 10, MaximumRTTMs: 5}, wantError: "discovery bounds"},
		{name: "bad-window", description: "boundary: a window over seven days never reaches the warehouse", body: v4, query: evidence.CandidateQuery{Location: "dc", Start: now.Add(-evidence.MaxWindow - time.Second), End: now, MaximumRTTMs: 15}, wantError: "discovery window"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := tc.query
			if q.Location == "" {
				q = evidence.CandidateQuery{Location: "dc", Start: now.Add(-time.Hour), End: now, MaximumRTTMs: 15}
			}
			called := false
			c := fixture(now, tc.body, func(r *http.Request) {
				called = true
				p := r.URL.Query()
				if p.Get("param_location") != "dc" || p.Get("param_maximum_rtt") != "15" || p.Get("param_minimum_rtt") != "0" {
					t.Fatalf("wrong query parameters: %s", r.URL.RawQuery)
				}
			})
			got, err := c.Candidates(context.Background(), q)
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("%s: want %q got %v", tc.description, tc.wantError, err)
				}
				if tc.query.Location != "" && called {
					t.Fatalf("%s: invalid query reached the warehouse", tc.description)
				}
				return
			}
			if err != nil {
				t.Fatalf("%s: %v", tc.description, err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("%s: got %d candidates, want %d", tc.description, len(got), len(tc.want))
			}
			for i, want := range tc.want {
				if got[i].Destination.String() != want || len(got[i].Agents) != 6 {
					t.Fatalf("%s: candidate %d = %+v, want %s", tc.description, i, got[i], want)
				}
			}
			if len(got) > 0 && (got[0].ASN != tc.wantASN || got[0].RTTMs == 0) {
				t.Fatalf("%s: traffic shape lost: %+v", tc.description, got[0])
			}
		})
	}
}

// Execute the production SQL against an isolated ClickHouse local database.
// No live service or production telemetry is accessed.
func TestClickHouseAnalysisCounterDeltas(t *testing.T) {
	binary, lerr := exec.LookPath("clickhouse")
	if lerr != nil {
		t.Skip("clickhouse is not on PATH; the Nix quality check supplies it")
	}
	setup := `CREATE TABLE default.sockets (
 agent_id String,host_boot_id String,netns_inode UInt64,inet_diag_msg_socket_cookie UInt64,
 timestamp_ns DateTime64(9),tcp_info_bytes_acked UInt64,tcp_info_bytes_sent UInt64,
 tcp_info_bytes_retrans UInt64,tcp_info_rtt UInt32,tcp_info_min_rtt UInt32,
 treatment_algorithm_exposed Bool,treatment_assignment_id String,location String,
 inet_diag_msg_state UInt8,inet_diag_msg_socket_destination FixedString(16),
 inet_diag_msg_family UInt8,enrich_socket_dest_asn UInt64) ENGINE=Memory;
 INSERT INTO default.sockets SELECT 'host-1','boot-1',1,1,
 fromUnixTimestamp64Nano(1700000000000000000+toInt64(number)*10000000000),
 1000000+number*1000,1000000+number*1000,number*10,1000,500,true,'trial','dc1',1,
 unhex('C0000201000000000000000000000000'),2,64512 FROM numbers(6);
 INSERT INTO default.sockets SELECT * FROM default.sockets;
 `
	query := strings.ReplaceAll(analysisSQL, "{{TABLE}}", "default.sockets")
	run := func(sql string) []byte {
		t.Helper()
		dir := t.TempDir()
		file := filepath.Join(dir, "query.sql")
		if e := os.WriteFile(file, []byte(sql), 0600); e != nil {
			t.Fatal(e)
		}
		cmd := exec.Command(binary, "local", "--path", filepath.Join(dir, "data"), "--queries-file", file, "--multiquery",
			"--param_start=1700000000000000000", "--param_end=1700000060000000000", "--param_location=dc1",
			"--param_agents=['host-1']", "--param_destination_hex=C0000201000000000000000000000000", "--param_assignment=trial",
			"--param_minimum_rtt=0", "--param_maximum_rtt=10", "--param_minimum_retransmission=0",
			"--output_format_json_quote_64bit_integers=0")
		out, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatalf("ClickHouse SQL: %v\n%s", e, out)
		}
		return out
	}
	var h hostRow
	if e := json.Unmarshal(run(setup+query), &h); e != nil {
		t.Fatal(e)
	}
	if h.Sockets != 1 || h.BytesAcked != 5000 || h.ExposedSockets != 1 || !h.CollectionHealthy || h.RttP95Ms != 1 || h.RetransmissionRatio != .01 {
		t.Fatalf("bad window deltas/deduplication: %+v", h)
	}
	reset := `INSERT INTO default.sockets SELECT 'host-1','boot-1',1,1,
 fromUnixTimestamp64Nano(1700000055000000000),1,1,0,1000,500,true,'trial','dc1',1,
 unhex('C0000201000000000000000000000000'),2,64512;`
	if e := json.Unmarshal(run(setup+reset+query), &h); e != nil {
		t.Fatal(e)
	}
	if h.CollectionHealthy {
		t.Fatal("counter reset accepted as healthy")
	}
	// Syntax/type-check candidate discovery too; a one-host fixture cannot meet
	// its six-host eligibility threshold.
	if out := run(setup + strings.ReplaceAll(discoverySQL, "{{TABLE}}", "default.sockets")); len(out) != 0 {
		t.Fatalf("unexpected discovery: %s", out)
	}
}
