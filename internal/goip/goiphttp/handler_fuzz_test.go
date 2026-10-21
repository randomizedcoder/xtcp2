package goiphttp

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/internal/goip"
	"golang.org/x/sys/unix"
)

// fuzzFixtures are the captures behind recordingSource, the first holding
// replies of a type answering for it. Together they hold every object goip
// serves, so no request is a missing fixture (a 500).
var fuzzFixtures = []string{
	"netlink_route_getlink.pcap",
	"netlink_route_getaddr.pcap",
	"netlink_route_getroute_table_all.pcap",
	"netlink_route_getneigh.pcap",
	"netlink_route_getrule.pcap",
	"netlink_route_getneightbl.pcap",
	"netlink_route_getnetconf.pcap",
	"netlink_route_getnexthop.pcap",
	"netlink_route_getstats.pcap",
	"netlink_route_getaddrlabel.pcap",
	"netlink_route_getvrf.pcap",
}

// errWrite is recordingSource refusing a message the kernel would treat as
// a change.
var errWrite = errors.New("recordingSource: not a read request")

// isReadRequest reports whether msg is a request the kernel serves without
// CAP_NET_ADMIN: rtnetlink_rcv_msg (net/core/rtnetlink.c) takes kind =
// type & 3, and only kind 2, the RTM_GET* of each family, is a read. It must
// also carry NLM_F_REQUEST, or the kernel ignores it.
func isReadRequest(msg []byte) bool {
	if len(msg) < unix.SizeofNlMsghdr {
		return false
	}
	typ := binary.LittleEndian.Uint16(msg[4:6])
	flags := binary.LittleEndian.Uint16(msg[6:8])
	return typ >= unix.RTM_BASE && typ&3 == 2 && flags&unix.NLM_F_REQUEST != 0
}

// sendLog is what every recordingSource of one handler was asked to send.
type sendLog struct {
	mu     sync.Mutex
	sent   int
	writes []string
}

func (l *sendLog) record(msg []byte) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sent++
	if isReadRequest(msg) {
		return nil
	}
	var typ, flags uint16
	if len(msg) >= unix.SizeofNlMsghdr {
		typ = binary.LittleEndian.Uint16(msg[4:6])
		flags = binary.LittleEndian.Uint16(msg[6:8])
	}
	l.writes = append(l.writes, fmt.Sprintf("type %d flags %#x (%d bytes)", typ, flags, len(msg)))
	return errWrite
}

// take returns and clears the log.
func (l *sendLog) take() (sent int, writes []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	sent, writes = l.sent, l.writes
	l.sent, l.writes = 0, nil
	return sent, writes
}

// recordingSource is the fuzz fake: it records every message before
// answering it, refuses any that is not a read, and answers a read from the
// fixtures, or with an empty dump when none holds its reply type. A single
// get is a missing device, so a dev path is a 404.
type recordingSource struct {
	log      *sendLog
	fixtures []*goip.ReplaySource
}

func (s *recordingSource) Dump(request []byte, msgType uint16) ([][]byte, error) {
	if err := s.log.record(request); err != nil {
		return nil, err
	}
	for _, f := range s.fixtures {
		if out, err := f.Dump(request, msgType); err == nil && len(out) > 0 {
			return out, nil
		}
	}
	return nil, nil
}

func (s *recordingSource) Talk(request []byte, _ uint16) ([]byte, error) {
	if err := s.log.record(request); err != nil {
		return nil, err
	}
	return nil, unix.ENODEV
}

// fuzzServer is one handler on a real ServeMux, with the fake behind it.
type fuzzServer struct {
	mux *http.ServeMux
	c   *counter
	log *sendLog
}

func newFuzzServer(tb testing.TB) *fuzzServer {
	tb.Helper()
	fixtures := make([]*goip.ReplaySource, 0, len(fuzzFixtures))
	for _, name := range fuzzFixtures {
		f, err := goip.OpenReplay(dumpsDir + name)
		if err != nil {
			tb.Fatal(err)
		}
		fixtures = append(fixtures, f)
	}
	s := &fuzzServer{mux: http.NewServeMux(), c: new(counter), log: new(sendLog)}
	open := func() (goip.Source, func() error, error) {
		return &recordingSource{log: s.log, fixtures: fixtures}, func() error { return nil }, nil
	}
	h, err := New(Options{Open: s.c.wrap(open)})
	if err != nil {
		tb.Fatal(err)
	}
	h.Register(s.mux, nil)
	return s
}

// fuzzStatuses are the statuses a request may get from the fake: the
// handler's, and ServeMux's redirects for path cleaning. A 500 or a 504
// needs a failing source, which the fake is not.
var fuzzStatuses = []int{
	http.StatusOK, http.StatusMovedPermanently, http.StatusTemporaryRedirect, http.StatusPermanentRedirect,
	http.StatusBadRequest, http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented,
	http.StatusServiceUnavailable,
}

// fuzzDeadline is the longest one request may take.
const fuzzDeadline = time.Second

// parseRequest parses method, path and query as a server reads a request line,
// or reports false for one no server would hand a handler.
func parseRequest(method, path, query string) (*http.Request, bool) {
	target := path
	if query != "" {
		target += "?" + query
	}
	r, err := http.ReadRequest(bufio.NewReader(strings.NewReader(method + " " + target + " HTTP/1.1\r\nHost: x\r\n\r\n")))
	if err != nil {
		return nil, false
	}
	return r, true
}

// result is one served request.
type result struct {
	code    int
	header  http.Header
	body    []byte
	opens   int32
	sent    int
	writes  []string
	elapsed time.Duration
}

func (s *fuzzServer) do(r *http.Request) result {
	s.log.take()
	before := s.c.opens.Load()
	rec := httptest.NewRecorder()
	start := time.Now()
	s.mux.ServeHTTP(rec, r)
	elapsed := time.Since(start)
	sent, writes := s.log.take()
	return result{
		code: rec.Code, header: rec.Header(), body: rec.Body.Bytes(),
		opens: s.c.opens.Load() - before, sent: sent, writes: writes, elapsed: elapsed,
	}
}

// get serves a GET for target, an escaped path and query.
func (s *fuzzServer) get(tb testing.TB, target string) result {
	tb.Helper()
	path, query, _ := strings.Cut(target, "?")
	r, ok := parseRequest(http.MethodGet, path, query)
	if !ok {
		tb.Fatalf("GET %q does not parse", target)
	}
	return s.do(r)
}

// safeHeader reports whether v holds no control byte, so no CR or LF.
func safeHeader(v string) bool {
	return !strings.ContainsFunc(v, func(r rune) bool { return r < 0x20 || r == 0x7f })
}

// argvRequest is the ShowRequest an X-Goip-Argv names. The query form
// spells options as goip's Argv does (-j) and a command line in full
// (-json); both resolve to one request.
func argvRequest(argv string) (goip.ShowRequest, error) {
	toks, ok := strings.CutPrefix(argv, "ip ")
	if !ok {
		return goip.ShowRequest{}, fmt.Errorf("%q does not start with ip", argv)
	}
	s, _, err := goip.ResolveShow(strings.Fields(toks))
	return s, err
}

// checkHandler serves one request and holds the handler to FuzzIPHandler's
// invariants, returning the result for a caller's own checks.
func checkHandler(t *testing.T, s *fuzzServer, method, path, query string) (result, bool) {
	t.Helper()
	r, ok := parseRequest(method, path, query)
	if !ok {
		return result{}, false
	}
	target := r.RequestURI
	got := s.do(r)

	// Never a write.
	if len(got.writes) != 0 {
		t.Fatalf("%s %q sent a non-read message: %v", method, target, got.writes)
	}
	// Bounded statuses.
	if !slices.Contains(fuzzStatuses, got.code) {
		t.Fatalf("%s %q: status %d; body %q", method, target, got.code, got.body)
	}
	// Rejections are cheap: a 405 opens nothing, and a 400 sends nothing.
	// The source opens before goip parses the selectors, so a selector a
	// show refuses is a 400 after the open.
	if got.code == http.StatusMethodNotAllowed && got.opens != 0 {
		t.Fatalf("%s %q: 405 after %d opens", method, target, got.opens)
	}
	if got.code == http.StatusBadRequest && got.sent != 0 {
		t.Fatalf("%s %q: 400 after sending %d messages", method, target, got.sent)
	}
	if got.elapsed > fuzzDeadline {
		t.Fatalf("%s %q took %v", method, target, got.elapsed)
	}
	// Headers are safe.
	argv, loc := got.header.Get(ArgvHeader), got.header.Get("Content-Location")
	if !safeHeader(argv) || !safeHeader(loc) {
		t.Fatalf("%s %q: unsafe header: %s %q, Content-Location %q", method, target, ArgvHeader, argv, loc)
	}
	if loc != "" && (got.code != http.StatusOK || !strings.HasPrefix(loc, Prefix+"/")) {
		t.Fatalf("%s %q: Content-Location %q on a %d", method, target, loc, got.code)
	}
	if method != http.MethodGet && method != http.MethodHead {
		return got, true
	}

	// GET and HEAD agree.
	other := http.MethodHead
	if method == http.MethodHead {
		other = http.MethodGet
	}
	r2, _ := parseRequest(other, path, query)
	got2 := s.do(r2)
	get, head := got, got2
	if method == http.MethodHead {
		get, head = got2, got
	}
	if get.code != head.code || !equalHeaders(get.header, head.header) {
		t.Fatalf("%q: GET %d %v, HEAD %d %v", target, get.code, get.header, head.code, head.header)
	}
	// A server drops whatever a HEAD handler writes; the recorder keeps
	// it, so only writeBody's own HEAD rule is checked: a 200 sends none.
	if head.code == http.StatusOK && len(head.body) != 0 {
		t.Fatalf("HEAD %q has a %d-byte body", target, len(head.body))
	}

	if get.code != http.StatusOK || loc == "" {
		return got, true
	}
	// Canonical round trip: the location serves the same body and the same
	// request, and names itself.
	canon := s.get(t, loc)
	if canon.code != http.StatusOK || !bytes.Equal(canon.body, get.body) {
		t.Fatalf("%q: Content-Location %q gave %d %q, want 200 %q", target, loc, canon.code, canon.body, get.body)
	}
	if l := canon.header.Get("Content-Location"); l != loc {
		t.Fatalf("%q: Content-Location %q names %q", target, loc, l)
	}
	a, err1 := argvRequest(argv)
	b, err2 := argvRequest(canon.header.Get(ArgvHeader))
	if err1 != nil || err2 != nil || !sameRequest(a, b) {
		t.Fatalf("%q: %s %q and its location's %q differ (%v, %v)", target, ArgvHeader, argv, canon.header.Get(ArgvHeader), err1, err2)
	}
	return got, true
}

// equalHeaders compares two responses' headers.
func equalHeaders(a, b http.Header) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if !slices.Equal(v, b[k]) {
			return false
		}
	}
	return true
}

// handlerSeed is one seed: a method, an escaped path and a raw query.
type handlerSeed struct{ method, path, query string }

// muxEdgePaths are ServeMux's path-cleaning corners.
var muxEdgePaths = []string{
	"//ip/a", "/i//a", "/ip/./link", "/ip/../metrics", "/i/%2e%2e/metrics", "/ip/link/..",
	"/ip/route/show/10.0.0.0%2F8", "/ip/%2F", "/i/" + segments("a", argvTokensMax+1),
	"/ip/" + strings.Repeat("a", argvPathMax+1), "/ip/link/show/dev/a%0d%0aX-Evil:%201",
}

// handlerSeeds is FuzzIPHandler's seed corpus: every TestHandler row with
// its method, every index example, every TestTokens path under both roots,
// and the ServeMux edge cases.
func handlerSeeds(tb testing.TB) []handlerSeed {
	tests := handlerTests(tb)
	seeds := make([]handlerSeed, 0, len(tests)+len(examples)+2*len(tokenTests)+len(muxEdgePaths))
	for i := range tests {
		path, query, _ := strings.Cut(tests[i].target, "?")
		seeds = append(seeds, handlerSeed{tests[i].method, path, query})
	}
	for _, ex := range examples {
		seeds = append(seeds, handlerSeed{http.MethodGet, ex.Path, ""})
	}
	for i := range tokenTests {
		seeds = append(seeds,
			handlerSeed{http.MethodGet, Prefix + "/" + tokenTests[i].path, ""},
			handlerSeed{http.MethodHead, ShortPrefix + "/" + tokenTests[i].path, ""})
	}
	for _, p := range muxEdgePaths {
		seeds = append(seeds, handlerSeed{http.MethodGet, p, ""})
	}
	return seeds
}

// FuzzIPHandler serves fuzzed requests through a real ServeMux with
// Register, over a fake that refuses and records any message that is not a
// read (never_writes), and holds every response to checkHandler's
// invariants: bounded statuses, cheap rejections, safe headers, GET and HEAD
// agreeing, and the canonical round trip.
//
// go test ./internal/goip/goiphttp/ -run '^$' -fuzz '^FuzzIPHandler$' -fuzztime 60s
func FuzzIPHandler(f *testing.F) {
	for _, s := range handlerSeeds(f) {
		f.Add(s.method, s.path, s.query)
	}
	s := newFuzzServer(f)
	f.Fuzz(func(t *testing.T, method, path, query string) {
		checkHandler(t, s, method, path, query)
	})
}

// TestFuzzIPHandlerRows are the plan's named cases, each through
// checkHandler, with its own expectation on top.
func TestFuzzIPHandlerRows(t *testing.T) {
	s := newFuzzServer(t)
	notServed := func(code int) bool { return code != http.StatusOK }
	is := func(codes ...int) func(int) bool {
		return func(code int) bool { return slices.Contains(codes, code) }
	}
	redirectOr := func(codes ...int) func(int) bool {
		return is(append([]int{http.StatusMovedPermanently, http.StatusTemporaryRedirect, http.StatusPermanentRedirect}, codes...)...)
	}
	manyParams := strings.TrimSuffix(strings.Repeat("dev=lo&", 10000), "&")

	tests := []struct {
		name        string
		category    string
		description string
		method      string
		path        string
		query       string
		// expected is the status check.
		expected func(int) bool
		// noOpen requires the fake never opened.
		noOpen bool
	}{
		{name: "dotdot_escape", category: "negative", description: "/ip/../metrics is cleaned by ServeMux, never served by /ip",
			method: http.MethodGet, path: "/ip/../metrics", expected: notServed},
		{name: "dotdot_escaped", category: "negative", description: "/i/%2e%2e/metrics: an escaped .. is cleaned too",
			method: http.MethodGet, path: "/i/%2e%2e/metrics", expected: notServed},
		{name: "crlf_in_argv", category: "negative", description: "%0d%0a in a token is a 400 and reaches no header",
			method: http.MethodGet, path: "/ip/link/show/dev/a%0d%0aX-Evil:%201", expected: is(http.StatusBadRequest), noOpen: true},
		{name: "crlf_in_query", category: "negative", description: "%0d%0a in a query value is a 400",
			method: http.MethodGet, path: "/ip/link", query: "dev=a%0d%0aX-Evil:%201", expected: is(http.StatusBadRequest)},
		{name: "max_path", category: "boundary", description: "1024 bytes after /ip/: the tokenizer decides",
			method: http.MethodGet, path: Prefix + "/" + argvLongPath(32), expected: is(http.StatusOK)},
		{name: "max_path_over", category: "boundary", description: "1025 bytes after /ip/: a 400 before any open",
			method: http.MethodGet, path: Prefix + "/" + argvLongPath(32) + "a", expected: is(http.StatusBadRequest), noOpen: true},
		{name: "huge_query", category: "boundary", description: "10,000 query parameters: a 400 before any open",
			method: http.MethodGet, path: "/ip/link", query: manyParams, expected: is(http.StatusBadRequest), noOpen: true},
		{name: "huge_query_over", category: "boundary", description: "10,001 query parameters: a 400 before any open",
			method: http.MethodGet, path: "/ip/link", query: manyParams + "&dev=lo", expected: is(http.StatusBadRequest), noOpen: true},
		{name: "double_slash_root", category: "corner", description: "//ip/a: cleaned by ServeMux",
			method: http.MethodGet, path: "//ip/a", expected: redirectOr(http.StatusBadRequest)},
		{name: "double_slash_inner", category: "corner", description: "/i//a: cleaned by ServeMux",
			method: http.MethodHead, path: "/i//a", expected: redirectOr(http.StatusBadRequest)},
		{name: "method_fuzz", category: "corner", description: "an unknown method token is a 405",
			method: "PROPFIND", path: "/i/a/s", expected: is(http.StatusMethodNotAllowed), noOpen: true},
		{name: "method_lowercase", category: "corner", description: "methods are case-sensitive: get is not GET",
			method: "get", path: "/ip/link", expected: is(http.StatusMethodNotAllowed), noOpen: true},
		{name: "write_verb", category: "negative", description: "/i/l/s is ip link set: a 400 before any open",
			method: http.MethodGet, path: "/i/l/s", expected: is(http.StatusBadRequest), noOpen: true},
	}
	for _, tc := range tests {
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			got, ok := checkHandler(t, s, tc.method, tc.path, tc.query)
			if !ok {
				t.Fatalf("%s: the request does not parse", tc.description)
			}
			if !tc.expected(got.code) {
				t.Fatalf("%s: status %d; body %q", tc.description, got.code, got.body)
			}
			if tc.noOpen && got.opens != 0 {
				t.Fatalf("%s: opened %d sources", tc.description, got.opens)
			}
			if got.code == http.StatusMethodNotAllowed && got.header.Get("Allow") != "GET, HEAD" {
				t.Fatalf("%s: Allow %q", tc.description, got.header.Get("Allow"))
			}
		})
	}
}

// TestRoundTripExamples is round_trip_examples: every index example through
// checkHandler. A served one holds the canonical round trip; a not_served
// one is goip's 501, and a dev path is the fake's missing device.
func TestRoundTripExamples(t *testing.T) {
	s := newFuzzServer(t)
	for _, ex := range examples {
		t.Run(strings.TrimPrefix(ex.Path, "/"), func(t *testing.T) {
			got, ok := checkHandler(t, s, http.MethodGet, ex.Path, "")
			if !ok {
				t.Fatalf("%s does not parse", ex.Path)
			}
			var expected int
			switch {
			case ex.NotServed != "":
				expected = http.StatusNotImplemented
			case strings.Contains(ex.Command, " dev "):
				expected = http.StatusNotFound
			default:
				expected = http.StatusOK
			}
			if got.code != expected {
				t.Fatalf("%s (%s): status %d, want %d; body %q", ex.Path, ex.Command, got.code, expected, got.body)
			}
			if expected == http.StatusOK && got.header.Get("Content-Location") == "" {
				t.Fatalf("%s: no Content-Location", ex.Path)
			}
		})
	}
}
