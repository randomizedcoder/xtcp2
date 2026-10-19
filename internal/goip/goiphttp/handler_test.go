package goiphttp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/randomizedcoder/xtcp2/internal/goip"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

const dumpsDir = "../../../pkg/xtcpnl/testdata/7_1_4/dumps/"

// counter counts opens and closes, so every row can assert that each source
// that was opened was closed.
type counter struct{ opens, closes atomic.Int32 }

func (c *counter) wrap(open Opener) Opener {
	return func() (goip.Source, func() error, error) {
		c.opens.Add(1)
		src, closeSrc, err := open()
		if err != nil {
			return nil, func() error { return nil }, err
		}
		return src, func() error { c.closes.Add(1); return closeSrc() }, nil
	}
}

func replayOpener(pcap string) Opener {
	return func() (goip.Source, func() error, error) {
		src, err := goip.OpenReplay(pcap)
		if err != nil {
			return nil, nil, err
		}
		return src, func() error { return nil }, nil
	}
}

// errSource fails every dump with the next error in errs (the last one
// repeating), or answers with no replies once errs is exhausted and last is
// nil.
type errSource struct {
	mu   sync.Mutex
	errs []error
}

func (e *errSource) next() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.errs) == 0 {
		return nil
	}
	err := e.errs[0]
	if len(e.errs) > 1 {
		e.errs = e.errs[1:]
	}
	return err
}

func (e *errSource) Dump([]byte, uint16) ([][]byte, error) { return nil, e.next() }

func errOpener(src goip.Source) Opener {
	return func() (goip.Source, func() error, error) { return src, func() error { return nil }, nil }
}

func want(t *testing.T, pcap string, r goip.ShowRequest) []byte {
	t.Helper()
	src, err := goip.OpenReplay(pcap)
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := goip.Show(src, r, &b); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func serve(t *testing.T, h *Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h.Register(mux, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), method, target, nil))
	return rec
}

// TestHandler drives the registered routes end to end over replay captures
// and failing sources.
//
// go test ./internal/goip/goiphttp/ -run TestHandler
func TestHandler(t *testing.T) {
	addrPcap := dumpsDir + "netlink_route_getaddr.pcap"
	linkPcap := dumpsDir + "netlink_route_getlink.pcap"
	addrJSON := want(t, addrPcap, goip.ShowRequest{Object: "address", JSON: true})

	tests := []struct {
		name        string
		description string
		open        Opener
		maxBytes    int
		method      string
		target      string
		wantStatus  int
		wantType    string
		wantBody    []byte // exact body, when set
		wantInBody  string // substring, when set
		wantArgv    string
		wantOpens   int32 // -1: don't check
	}{
		{name: "addr_json", description: "positive: /ip/addr is ip -j address show",
			open: replayOpener(addrPcap), method: "GET", target: "/ip/addr",
			wantStatus: 200, wantType: "application/json", wantBody: addrJSON, wantArgv: "ip -j address show", wantOpens: 1},
		{name: "addr_text_dev", description: "positive: /ip/addr/goip0?format=text is ip address show dev goip0",
			open: replayOpener(dumpsDir + "netlink_route_getaddr_dev.pcap"), method: "GET", target: "/ip/addr/goip0?format=text",
			wantStatus: 200, wantType: "text/plain; charset=utf-8",
			wantBody: want(t, dumpsDir+"netlink_route_getaddr_dev.pcap", goip.ShowRequest{Object: "address", Selectors: []string{"dev", "goip0"}}),
			wantArgv: "ip address show dev goip0", wantOpens: 1},
		{name: "index", description: "positive: /ip lists every object, no netlink",
			open: replayOpener(linkPcap), method: "GET", target: "/ip",
			wantStatus: 200, wantType: "application/json", wantInBody: `"path":"/ip/addr"`, wantOpens: 0},
		{name: "head", description: "positive: HEAD runs the show and sends no body",
			open: replayOpener(linkPcap), method: "HEAD", target: "/ip/link",
			wantStatus: 200, wantType: "application/json", wantBody: []byte{}, wantOpens: 1},

		{name: "unknown_dev", description: "negative: a missing device is 404 with the argv",
			open: errOpener(&errSource{}), method: "GET", target: "/ip/neigh/nosuch0",
			wantStatus: 404, wantInBody: `"argv":"ip -j neighbor show dev nosuch0"`, wantOpens: 1},
		{name: "post", description: "negative: POST is 405",
			open: replayOpener(linkPcap), method: "POST", target: "/ip/addr", wantStatus: 405, wantOpens: 0},
		{name: "unknown_object", description: "negative: /ip/tunnel is 404",
			open: replayOpener(linkPcap), method: "GET", target: "/ip/tunnel", wantStatus: 404, wantOpens: 0},
		{name: "bad_param", description: "negative: a bad parameter is 400 and opens nothing",
			open: replayOpener(linkPcap), method: "GET", target: "/ip/link?table=main", wantStatus: 400, wantOpens: 0},
		{name: "open_fails", description: "negative: a socket that won't open is 500",
			open: func() (goip.Source, func() error, error) { return nil, nil, unix.EMFILE }, method: "GET", target: "/ip/link",
			wantStatus: 500, wantInBody: "open netlink", wantOpens: 1},
		{name: "index_param", description: "negative: the index takes no parameters",
			open: replayOpener(linkPcap), method: "GET", target: "/ip?dev=x", wantStatus: 400, wantOpens: 0},

		{name: "max_bytes_exact", description: "boundary: a body exactly MaxBytes long is served",
			open: replayOpener(addrPcap), maxBytes: len(addrJSON), method: "GET", target: "/ip/addr",
			wantStatus: 200, wantBody: addrJSON, wantOpens: 1},
		{name: "max_bytes_minus_one", description: "boundary: one byte over is a 500, never a truncated 200",
			open: replayOpener(addrPcap), maxBytes: len(addrJSON) - 1, method: "GET", target: "/ip/addr",
			wantStatus: 500, wantInBody: "size limit", wantOpens: 1},
		{name: "index_trailing_slash", description: "boundary: /ip/ is the index too",
			open: replayOpener(linkPcap), method: "GET", target: "/ip/", wantStatus: 200, wantOpens: 0},

		{name: "not_implemented", description: "corner: a goip gap is 501 and names it",
			open: errOpener(&errSource{errs: []error{fmt.Errorf("x: %w", goip.ErrNotImplemented)}}), method: "GET", target: "/ip/link",
			wantStatus: 501, wantInBody: "not implemented", wantOpens: 1},
		{name: "dump_intr_retry", description: "corner: an interrupted dump is retried once on a fresh source",
			open: errOpener(&errSource{errs: []error{xtcpnl.ErrDumpInterrupted, nil}}), method: "GET", target: "/ip/rule",
			wantStatus: 200, wantOpens: 2},
		{name: "dump_intr_twice", description: "corner: interrupted twice is 503",
			open: errOpener(&errSource{errs: []error{xtcpnl.ErrDumpInterrupted}}), method: "GET", target: "/ip/rule",
			wantStatus: 503, wantOpens: 2},
		{name: "recv_timeout", description: "corner: SO_RCVTIMEO expiry is 504",
			open: errOpener(&errSource{errs: []error{fmt.Errorf("goip: dump: %w", unix.EAGAIN)}}), method: "GET", target: "/ip/rule",
			wantStatus: 504, wantOpens: 1},
		{name: "text_error", description: "corner: format=text errors are plain text",
			open: errOpener(&errSource{}), method: "GET", target: "/ip/neigh/nosuch0?format=text",
			wantStatus: 404, wantType: "text/plain; charset=utf-8", wantInBody: "cannot find device", wantOpens: 1},
		{name: "dotdot_path", description: "corner: /ip/addr/.. is cleaned by the mux, never a dev",
			open: replayOpener(linkPcap), method: "GET", target: "/ip/addr/..", wantStatus: 307, wantOpens: 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var c counter
			h, err := New(Options{Open: c.wrap(tc.open), MaxBytes: tc.maxBytes})
			if err != nil {
				t.Fatal(err)
			}
			rec := serve(t, h, tc.method, tc.target)
			if rec.Code != tc.wantStatus {
				t.Fatalf("%s: status %d, want %d; body %s", tc.description, rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantType != "" && rec.Header().Get("Content-Type") != tc.wantType {
				t.Errorf("%s: Content-Type %q, want %q", tc.description, rec.Header().Get("Content-Type"), tc.wantType)
			}
			if tc.wantBody != nil && !bytes.Equal(rec.Body.Bytes(), tc.wantBody) {
				t.Errorf("%s: body\n%s\nwant\n%s", tc.description, rec.Body.String(), tc.wantBody)
			}
			if tc.wantInBody != "" && !strings.Contains(rec.Body.String(), tc.wantInBody) {
				t.Errorf("%s: body %q lacks %q", tc.description, rec.Body.String(), tc.wantInBody)
			}
			if tc.wantArgv != "" && rec.Header().Get(ArgvHeader) != tc.wantArgv {
				t.Errorf("%s: %s %q, want %q", tc.description, ArgvHeader, rec.Header().Get(ArgvHeader), tc.wantArgv)
			}
			if tc.wantStatus == 200 && rec.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("%s: Cache-Control %q", tc.description, rec.Header().Get("Cache-Control"))
			}
			if got := c.opens.Load(); tc.wantOpens >= 0 && got != tc.wantOpens {
				t.Errorf("%s: opens %d, want %d", tc.description, got, tc.wantOpens)
			}
			if tc.wantStatus != 500 || !strings.Contains(rec.Body.String(), "open netlink") {
				if c.opens.Load() != c.closes.Load() {
					t.Errorf("%s: %d opens but %d closes", tc.description, c.opens.Load(), c.closes.Load())
				}
			}
		})
	}
}

// TestHandlerInFlight fills the in-flight slots and checks the next request
// is refused at once, and that the slot is released afterwards.
func TestHandlerInFlight(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{})
	blocking := func() (goip.Source, func() error, error) {
		entered <- struct{}{}
		<-release
		return &errSource{}, func() error { return nil }, nil
	}
	h, err := New(Options{Open: blocking, InFlight: 1})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.Register(mux, nil)

	done := make(chan int)
	go func() {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", "/ip/rule", nil))
		done <- rec.Code
	}()
	<-entered

	// boundary: the second request while the only slot is held.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), "GET", "/ip/rule", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("second request: %d, want 503", rec.Code)
	}
	// positive: the index is not limited.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), "GET", "/ip", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("index while full: %d, want 200", rec.Code)
	}
	close(release)
	if code := <-done; code != http.StatusOK {
		t.Fatalf("first request: %d, want 200", code)
	}
	// corner: the slot is free again.
	go func() { <-entered }()
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), "GET", "/ip/rule", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("after release: %d, want 200", rec.Code)
	}
}

// TestNew covers Options validation.
func TestNew(t *testing.T) {
	tests := []struct {
		name        string
		description string
		opts        Options
		wantErr     bool
		wantSlots   int
	}{
		{name: "defaults", description: "positive: zero Options take the defaults", wantSlots: DefaultInFlight},
		{name: "negative_bytes", description: "negative: MaxBytes < 0", opts: Options{MaxBytes: -1}, wantErr: true},
		{name: "one_slot", description: "boundary: InFlight 1", opts: Options{InFlight: 1}, wantSlots: 1},
		{name: "negative_inflight", description: "corner: InFlight < 0", opts: Options{InFlight: -1}, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h, err := New(tc.opts)
			if tc.wantErr {
				if !errors.Is(err, ErrBadOptions) {
					t.Fatalf("%s: err %v, want ErrBadOptions", tc.description, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("%s: %v", tc.description, err)
			}
			if cap(h.slots) != tc.wantSlots {
				t.Fatalf("%s: slots %d, want %d", tc.description, cap(h.slots), tc.wantSlots)
			}
		})
	}
}

// TestIndex checks the index document against the registry.
func TestIndex(t *testing.T) {
	h, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	var entries []indexEntry
	if err := json.Unmarshal(h.index, &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(objects) {
		t.Fatalf("index has %d entries, registry %d", len(entries), len(objects))
	}
	for i, e := range entries {
		o := objects[i]
		if e.Path != Prefix+"/"+o.path || (e.InterfacePath != "") != o.devPath || len(e.Params) != len(o.params)+1 {
			t.Errorf("entry %d %+v does not match %s", i, e, o.path)
		}
	}
	if paths := h.Paths(); paths[0] != Prefix {
		t.Errorf("Paths()[0] = %q", paths[0])
	}
}

// BenchmarkIPHandler measures one request through the mux over a replay
// source. The replay open dominates; what it guards is that the handler adds
// no per-request work beyond the parse and the buffer.
//
// go test ./internal/goip/goiphttp/ -run x -bench IPHandler -benchmem
func BenchmarkIPHandler(b *testing.B) {
	cases := []struct {
		name, pcap, target string
	}{
		{"addr_json", dumpsDir + "netlink_route_getaddr.pcap", "/ip/addr"},
		{"route_text", dumpsDir + "netlink_route_getroute.pcap", "/ip/route?format=text"},
		{"bad_param", dumpsDir + "netlink_route_getlink.pcap", "/ip/link?table=main"},
		{"index", dumpsDir + "netlink_route_getlink.pcap", "/ip"},
	}
	for _, bc := range cases {
		b.Run(bc.name, func(b *testing.B) {
			h, err := New(Options{Open: replayOpener(bc.pcap)})
			if err != nil {
				b.Fatal(err)
			}
			mux := http.NewServeMux()
			h.Register(mux, nil)
			req := httptest.NewRequest("GET", bc.target, nil)
			b.ReportAllocs()
			for b.Loop() {
				mux.ServeHTTP(httptest.NewRecorder(), req)
			}
		})
	}
}

// TestRejectAllocs is the regression guard for the refusal path: a bad
// request must stay cheap and must never open a socket.
func TestRejectAllocs(t *testing.T) {
	var c counter
	h, err := New(Options{Open: c.wrap(replayOpener(dumpsDir + "netlink_route_getlink.pcap"))})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.Register(mux, nil)
	req := httptest.NewRequest("GET", "/ip/link?table=main", nil)
	allocs := testing.AllocsPerRun(100, func() {
		mux.ServeHTTP(httptest.NewRecorder(), req)
	})
	if allocs > 40 {
		t.Errorf("bad-parameter request: %.0f allocs, want <= 40", allocs)
	}
	if c.opens.Load() != 0 {
		t.Errorf("bad-parameter request opened %d sources", c.opens.Load())
	}

	// The tokenizer takes no opener, so a path it refuses never reaches a
	// socket; these rows bound what refusing costs.
	for _, tc := range []struct {
		name, category, description, path string
		expected                          float64
	}{
		{"argv_control_byte", "negative", "a %00 is refused", "link/show/dev/a%00", 8},
		{"argv_empty_segment", "negative", "an empty segment is refused", "link//show", 8},
		{"argv_join_range", "boundary", "an IPv4 /33 is refused", "route/show/10.0.0.0/33", 8},
		{"argv_accepted", "positive", "an accepted path costs its token slice", "addr/show/dev/eth0", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			allocs := testing.AllocsPerRun(100, func() { _, _ = tokens(tc.path) })
			if allocs > tc.expected {
				t.Errorf("%s: %s: %.0f allocs, want <= %.0f", tc.category, tc.description, allocs, tc.expected)
			}
		})
	}
}

// TestRegisterMethods covers the method check Register does in place of a
// method pattern, and guards why: mounting /ip must cost a neighbor route on
// the same mux nothing.
func TestRegisterMethods(t *testing.T) {
	var c counter
	h, err := New(Options{Open: c.wrap(replayOpener(dumpsDir + "netlink_route_getlink.pcap"))})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, category, description string
		method, target              string
		expected                    int
	}{
		{"get", "positive", "GET is served", http.MethodGet, "/ip/link", http.StatusOK},
		{"head", "positive", "HEAD is served", http.MethodHead, "/ip/link", http.StatusOK},
		{"put", "negative", "PUT is refused", http.MethodPut, "/ip/link", http.StatusMethodNotAllowed},
		{"delete_dev", "negative", "DELETE on an interface path is refused", http.MethodDelete, "/ip/link/eth0", http.StatusMethodNotAllowed},
		{"options_index", "boundary", "OPTIONS on the index is refused", http.MethodOptions, "/ip", http.StatusMethodNotAllowed},
		{"post_bad_param", "corner", "POST with a bad parameter is 405, not 400", http.MethodPost, "/ip/addr?foo=1", http.StatusMethodNotAllowed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %d", tc.category, tc.description, tc.expected)
			mux := http.NewServeMux()
			h.Register(mux, nil)
			r := httptest.NewRecorder()
			mux.ServeHTTP(r, httptest.NewRequestWithContext(t.Context(), tc.method, tc.target, nil))
			if r.Code != tc.expected {
				t.Fatalf("%s %s: %d, want %d", tc.method, tc.target, r.Code, tc.expected)
			}
			if tc.expected == http.StatusMethodNotAllowed && r.Header().Get("Allow") != "GET, HEAD" {
				t.Errorf("Allow = %q", r.Header().Get("Allow"))
			}
		})
	}
	opens := c.opens.Load()

	t.Run("neighbor_route_allocs", func(t *testing.T) {
		t.Log("corner (guard): /metrics on a mux with /ip mounted allocates exactly as without it")
		noop := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
		allocs := func(mount bool) float64 {
			mux := http.NewServeMux()
			mux.Handle("/metrics", noop)
			if mount {
				h.Register(mux, nil)
			}
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", nil)
			w := httptest.NewRecorder()
			return testing.AllocsPerRun(200, func() { mux.ServeHTTP(w, req) })
		}
		if without, with := allocs(false), allocs(true); with != without {
			t.Fatalf("/metrics allocs: %v with /ip, %v without", with, without)
		}
		if c.opens.Load() != opens {
			t.Fatal("/metrics opened netlink")
		}
	})
}
