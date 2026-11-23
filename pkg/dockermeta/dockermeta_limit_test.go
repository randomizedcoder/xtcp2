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

// inspectJSON is a usable inspect body for aaaa, padded with a label to at
// least size bytes.
func inspectJSON(size int) string {
	const head = `{"Id":"aaaa","Name":"/web","Config":{"Image":"nginx","Labels":{"pad":"`
	const tail = `"}},"State":{"Pid":1},"NetworkSettings":{"SandboxKey":"/var/run/docker/netns/aaaahash"}}`
	return head + strings.Repeat("x", max(0, size-len(head)-len(tail))) + tail
}

func TestInspectBodyCap(t *testing.T) {
	cases := []struct {
		name        string
		cat         string
		description string
		size        int
		expected    bool // aaaa indexed
	}{
		{"small", "P", "an ordinary inspect body", 0, true},
		{"big_labels", "B", "900 KiB of labels, under the 1 MiB cap", 900 << 10, true},
		{"at_cap", "B", "exactly maxInspectBody bytes", maxInspectBody, true},
		{"over_cap", "N", "maxInspectBody+1 bytes: aaaa skipped, the rest indexed", maxInspectBody + 1, false},
		{"far_over_cap", "N", "8 MiB of labels: aaaa skipped", 8 << 20, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs, srv := newFixtureServer(t)
			fs.mu.Lock()
			fs.inspects["aaaa111111111111111111111111111111111111111111111111111111111111"] = inspectJSON(tc.size)
			fs.mu.Unlock()

			ix, err := newIndexFromClient(context.Background(), srv.Client(), srv.URL, fakeStat)
			if err != nil {
				t.Fatalf("newIndexFromClient: %v", err)
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
