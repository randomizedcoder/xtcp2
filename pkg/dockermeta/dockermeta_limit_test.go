package dockermeta

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// versionJSON returns a /version body of exactly n bytes (n >= 17).
func versionJSON(n int) string {
	const pre, suf = `{"ApiVersion":"`, `"}`
	return pre + strings.Repeat("9", n-len(pre)-len(suf)) + suf
}

func TestGetJSONLimit(t *testing.T) {
	const limit = 64
	type outcome int
	const (
		ok outcome = iota
		tooLarge
		decodeErr
		statusErr
	)
	cases := []struct {
		name        string
		cat         string
		description string
		status      int
		body        string
		expected    outcome
	}{
		{"under_cap", "P", "a 21-byte body decodes",
			http.StatusOK, `{"ApiVersion":"1.44"}`, ok},
		{"at_cap", "B", "a body of exactly the cap decodes",
			http.StatusOK, versionJSON(limit), ok},
		{"over_by_one", "B", "cap+1 bytes is refused before decoding",
			http.StatusOK, versionJSON(limit + 1), tooLarge},
		{"huge", "N", "a 1 MiB body is refused, only cap+1 bytes read",
			http.StatusOK, versionJSON(1 << 20), tooLarge},
		{"malformed_under_cap", "N", "truncated JSON under the cap is a decode error, not a size error",
			http.StatusOK, `{"ApiVersion":`, decodeErr},
		{"empty_body", "C", "an empty 200 body is a decode error (EOF)",
			http.StatusOK, "", decodeErr},
		{"error_status_huge", "C", "a 500 with a 1 MiB body is a status error; the body is not read",
			http.StatusInternalServerError, versionJSON(1 << 20), statusErr},
		{"trailing_data", "C", "data after the first value is ignored, as json.Decoder did before the cap",
			http.StatusOK, `{"ApiVersion":"1.44"} trailing`, ok},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()

			var vi versionInfo
			err := getJSON(context.Background(), srv.Client(), srv.URL+"/version", limit, &vi)
			var got outcome
			switch {
			case err == nil:
				got = ok
			case errors.Is(err, errBodyTooLarge):
				got = tooLarge
			case strings.Contains(err.Error(), "status"):
				got = statusErr
			default:
				got = decodeErr
			}
			if got != tc.expected {
				t.Fatalf("%s: outcome %d (err %v), expected %d", tc.description, got, err, tc.expected)
			}
			if got == ok && vi.APIVersion == "" {
				t.Fatalf("%s: decoded no ApiVersion", tc.description)
			}
		})
	}
}

// inspectJSON is a usable inspect body for aaaa with label bytes of padding
// and the given Id and SandboxKey.
func inspectJSON(labels int, id, sandbox string) string {
	return `{"Id":"` + id + `","Name":"/web","Config":{"Image":"nginx","Labels":{"pad":"` +
		strings.Repeat("x", labels) + `"}},"State":{"Pid":1},"NetworkSettings":{"SandboxKey":"` + sandbox + `"}}`
}

// listJSON is a /containers/json body for aaaa and bbbb, aaaa's entry padded
// with label bytes and carrying the given Id.
func listJSON(labels int, aaaaID string) string {
	return `[{"Id":"` + aaaaID + `","Labels":{"pad":"` + strings.Repeat("x", labels) + `"}},` +
		`{"Id":"bbbb222222222222222222222222222222222222222222222222222222222222"}]`
}

// TestTenantBodySize: label padding, the part of a body a tenant image
// controls, drops at most its own container, never another's, and never
// fails the list.
func TestTenantBodySize(t *testing.T) {
	const aaaaID = "aaaa111111111111111111111111111111111111111111111111111111111111"
	const key = "/var/run/docker/netns/aaaahash"
	long := strings.Repeat("a", maxIDRaw+1)
	// Label padding that makes the inspect body, or aaaa's list entry, exactly
	// its limit.
	inspectPad := maxInspectBody - len(inspectJSON(0, aaaaID, key))
	entryPad := maxListEntry - (len(listJSON(0, aaaaID)) - len(listJSON(0, "")) + len(`{"Id":"","Labels":{"pad":""}}`))
	// Seventeen padded entries: past the 64 MiB a list total would have had.
	many := "[" + strings.Repeat(`{"Id":"p","Labels":{"pad":"`+strings.Repeat("x", 4<<20-64)+`"}},`, 17) + listJSON(0, aaaaID)[1:]
	cases := []struct {
		name        string
		cat         string
		description string
		list        string
		inspect     string
		expected    bool // aaaa indexed; bbbb must be indexed in every row
	}{
		{"small", "P", "an ordinary list and inspect",
			listJSON(0, aaaaID), inspectJSON(0, aaaaID, key), true},
		{"inspect_labels_3MiB", "P", "3 MiB of labels in inspect, under maxInspectBody",
			listJSON(0, aaaaID), inspectJSON(3<<20, aaaaID, key), true},
		{"inspect_at_limit", "B", "an inspect body of exactly maxInspectBody",
			listJSON(0, aaaaID), inspectJSON(inspectPad, aaaaID, key), true},
		{"inspect_over_limit", "B", "maxInspectBody+1: aaaa skipped (and logged)",
			listJSON(0, aaaaID), inspectJSON(inspectPad+1, aaaaID, key), false},
		{"entry_at_limit", "B", "aaaa's list entry of exactly maxListEntry",
			listJSON(entryPad, aaaaID), inspectJSON(0, aaaaID, key), true},
		{"entry_over_limit", "B", "maxListEntry+1: aaaa's entry dropped, the list still builds",
			listJSON(entryPad+1, aaaaID), inspectJSON(0, aaaaID, key), false},
		{"many_padded_entries", "C", "17 entries of ~4 MiB (68 MiB) do not fail the list",
			many, inspectJSON(0, aaaaID, key), true},
		{"inspect_id_at_cap", "B", "an inspect Id of exactly maxIDRaw bytes is kept",
			listJSON(0, aaaaID), inspectJSON(0, strings.Repeat("a", maxIDRaw), key), true},
		{"inspect_id_over_cap", "N", "a daemon-set Id over maxIDRaw: aaaa skipped",
			listJSON(0, aaaaID), inspectJSON(0, long, key), false},
		{"sandbox_over_cap", "N", "a SandboxKey over maxIDRaw: aaaa skipped",
			listJSON(0, aaaaID), inspectJSON(0, aaaaID, "/"+long), false},
		{"list_id_over_cap", "C", "an over-long Id in one list entry drops that entry only",
			listJSON(0, long), inspectJSON(0, aaaaID, key), false},
		{"malformed_label", "N", "a raw control byte in a skipped label is still a syntax error: aaaa skipped",
			listJSON(0, aaaaID), strings.Replace(inspectJSON(4, aaaaID, key), "xxxx", "x\x01x", 1), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs, srv := newFixtureServer(t)
			fs.mu.Lock()
			fs.containers = tc.list
			fs.inspects[aaaaID] = tc.inspect
			fs.inspects[long] = tc.inspect
			fs.mu.Unlock()

			ix, err := newIndexFromClient(context.Background(), srv.Client(), srv.URL, fakeStat)
			if err != nil {
				t.Fatalf("%s: newIndexFromClient: %v", tc.description, err)
			}
			defer ix.Close()
			if _, got := ix.Lookup(4026531001); got != tc.expected {
				t.Fatalf("%s: aaaa indexed %v, expected %v", tc.description, got, tc.expected)
			}
			if _, ok := ix.Lookup(4026531002); !ok {
				t.Fatalf("%s: bbbb must stay indexed", tc.description)
			}
		})
	}
}

// countingBody is a ReadCloser of n bytes (n < 0: endless) that records how
// much was read and whether it was closed; failAfter > 0 returns an error
// once that many bytes are read.
type countingBody struct {
	n, read, failAfter int
	closed             bool
}

var errBodyRead = errors.New("read: connection reset")

func (b *countingBody) Read(p []byte) (int, error) {
	if b.failAfter > 0 && b.read >= b.failAfter {
		return 0, errBodyRead
	}
	if b.n >= 0 && b.read >= b.n {
		return 0, io.EOF
	}
	k := len(p)
	if b.n >= 0 {
		k = min(k, b.n-b.read)
	}
	if b.failAfter > 0 {
		k = min(k, b.failAfter-b.read)
	}
	b.read += k
	return k, nil
}

func (b *countingBody) Close() error { b.closed = true; return nil }

func TestDrainCloseBounded(t *testing.T) {
	cases := []struct {
		name        string
		cat         string
		description string
		body        countingBody
		expected    int // bytes read
	}{
		{"short", "P", "a short body is drained to EOF", countingBody{n: 100}, 100},
		{"empty", "B", "an empty body", countingBody{n: 0}, 0},
		{"at_drain_cap", "B", "exactly maxDrain bytes", countingBody{n: maxDrain}, maxDrain},
		{"over_drain_cap", "N", "maxDrain+1 bytes: the last byte is left to Close", countingBody{n: maxDrain + 1}, maxDrain},
		{"endless", "C", "an endless stream (/events) returns after maxDrain", countingBody{n: -1}, maxDrain},
		{"read_error", "N", "a read error stops the drain; the body is still closed", countingBody{n: -1, failAfter: 10}, 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := tc.body
			drainClose(&b)
			if b.read != tc.expected || !b.closed {
				t.Fatalf("%s: read %d closed %v, expected read %d closed true", tc.description, b.read, b.closed, tc.expected)
			}
		})
	}
}

// TestEventsOversizedLineReturns: an /events line over the scanner's 1 MiB
// buffer ends the stream with bufio.ErrTooLong, and streamEvents returns
// promptly so refresh can fall back to re-listing. With an unbounded drain it
// blocked on the endless stream until ctx ended.
func TestEventsOversizedLineReturns(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fl, _ := w.(http.Flusher)
		if _, err := fmt.Fprintf(w, "{\"Type\":\"container\",\"pad\":\"%s\"}\n", strings.Repeat("x", 2<<20)); err != nil {
			return
		}
		for r.Context().Err() == nil {
			if _, err := io.WriteString(w, "{}\n"); err != nil {
				return
			}
			if fl != nil {
				fl.Flush()
			}
		}
	}))
	defer srv.Close()

	ix := &Index{client: srv.Client(), baseURL: srv.URL, stat: fakeStat}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	start := time.Now()
	err := ix.streamEvents(ctx)
	if !errors.Is(err, bufio.ErrTooLong) {
		t.Fatalf("streamEvents: %v, expected bufio.ErrTooLong", err)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Fatalf("streamEvents took %v: the drain did not stop", d)
	}
}

// TestRequestTimeout: a list body that stalls after its first byte fails the
// request at requestTimeout instead of holding the refresh goroutine.
func TestRequestTimeout(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			_, _ = io.WriteString(w, `{"ApiVersion":"1.44","MinAPIVersion":"1.24"}`)
			return
		}
		_, _ = io.WriteString(w, "[")
		if fl, ok := w.(http.Flusher); ok {
			fl.Flush()
		}
		<-r.Context().Done()
	}))
	defer srv.Close()

	start := time.Now()
	_, err := newIndexFromClient(context.Background(), srv.Client(), srv.URL, fakeStat)
	d := time.Since(start)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("newIndexFromClient: %v, expected a deadline error", err)
	}
	if d < requestTimeout || d > requestTimeout+5*time.Second {
		t.Fatalf("took %v, expected about requestTimeout (%v)", d, requestTimeout)
	}
}
