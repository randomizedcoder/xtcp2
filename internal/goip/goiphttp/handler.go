package goiphttp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/randomizedcoder/xtcp2/internal/goip"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// Defaults for Options fields left zero.
const (
	DefaultInFlight    = 2
	DefaultMaxBytes    = 32 << 20
	DefaultRecvTimeout = 5 * time.Second
)

// ArgvHeader carries the `ip` command a response is equivalent to.
const ArgvHeader = "X-Goip-Argv"

// ShortPrefix is the abbreviated root, as `ip` itself abbreviates nothing
// at that level: /i/a/s is /ip/address/show. Nothing else outside the /ip
// tree abbreviates.
const ShortPrefix = "/i"

// Opener opens one netlink source for one request. The closer is always safe
// to call.
type Opener func() (goip.Source, func() error, error)

// NetlinkOpener is the production Opener: a fresh NETLINK_ROUTE socket per
// request with SO_RCVTIMEO set. A socket per request rather than a shared
// one, because goip's NetlinkSource and LLTab are not safe to share and the
// socket costs microseconds next to the dump it carries.
func NetlinkOpener(recvTimeout time.Duration) Opener {
	return func() (goip.Source, func() error, error) {
		s, err := goip.OpenNetlinkTimeout(recvTimeout)
		if err != nil {
			return nil, func() error { return nil }, err
		}
		return s, s.Close, nil
	}
}

// Options configures a Handler. Zero fields take the defaults above.
type Options struct {
	// Open is the source for each request; nil means NetlinkOpener with
	// DefaultRecvTimeout.
	Open Opener
	// MaxBytes caps one response body. A larger one is a 500, never a
	// truncated 200.
	MaxBytes int
	// InFlight is how many show requests may run at once across every
	// /ip path. Further requests get 503 at once rather than queueing.
	InFlight int
}

// Handler serves the /ip paths.
type Handler struct {
	open     Opener
	maxBytes int
	slots    chan struct{}
	index    []byte
}

// ErrBadOptions is New refusing its Options.
var ErrBadOptions = errors.New("goiphttp: bad options")

// New builds a Handler. Everything that can be decided before a request —
// the registry, the index document — is decided here.
func New(o Options) (*Handler, error) {
	if o.Open == nil {
		o.Open = NetlinkOpener(DefaultRecvTimeout)
	}
	if o.MaxBytes == 0 {
		o.MaxBytes = DefaultMaxBytes
	}
	if o.InFlight == 0 {
		o.InFlight = DefaultInFlight
	}
	if o.MaxBytes < 0 || o.InFlight < 0 {
		return nil, fmt.Errorf("%w: MaxBytes %d, InFlight %d", ErrBadOptions, o.MaxBytes, o.InFlight)
	}
	index, err := indexDocument()
	if err != nil {
		return nil, err
	}
	return &Handler{
		open:     o.Open,
		maxBytes: o.MaxBytes,
		slots:    make(chan struct{}, o.InFlight),
		index:    index,
	}, nil
}

// Paths lists every registered pattern's path, for startup logging.
func (h *Handler) Paths() []string {
	return []string{Prefix, Prefix + "/", ShortPrefix, ShortPrefix + "/"}
}

// Register adds the /ip and /i routes to mux, each passed through wrap
// (auth, timeout) first. wrap may be nil. Only GET and HEAD are served;
// anything else is 405.
//
// Each root is the index and one subtree, which servePath splits into the
// command line and the query form. The subtree is read from
// r.URL.EscapedPath(), so a %2F stays inside its token. It is a plain
// subtree pattern (/ip/) and not a wildcard (/ip/{argv...}): ServeMux
// routes either with allocations an exact pattern avoids, three for the
// subtree and five for the wildcard, which captures a value nothing reads.
//
// The patterns carry no method: one method pattern anywhere on a ServeMux
// costs an allocation on every request it routes, /metrics included, so the
// method is checked here instead.
func (h *Handler) Register(mux *http.ServeMux, wrap func(http.Handler) http.Handler) {
	if wrap == nil {
		wrap = func(x http.Handler) http.Handler { return x }
	}
	idx := wrap(readOnly(http.HandlerFunc(h.serveIndex)))
	paths := wrap(readOnly(http.HandlerFunc(h.servePath)))
	for _, root := range []string{Prefix, ShortPrefix} {
		mux.Handle(root, idx)
		mux.Handle(root+"/{$}", idx)
		mux.Handle(root+"/", paths)
	}
}

// readOnly answers anything but GET and HEAD with 405, as a GET method
// pattern would.
func readOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) serveIndex(w http.ResponseWriter, r *http.Request) {
	q, err := query(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err, "", false)
		return
	}
	if len(q) != 0 {
		if len(q) != 1 || q.Get(formatParam) != "json" || len(q[formatParam]) != 1 {
			writeError(w, http.StatusBadRequest, paramErr("the index takes no parameters but format=json"), "", false)
			return
		}
	}
	writeBody(w, r, http.StatusOK, "application/json", h.index, "")
}

// servePath serves every path under either root, as one of two forms.
//
// A path is a command line (`ip` argv, one token per segment) when it starts
// with an option, or when the token after the object resolves to any verb of
// that object. A write verb counts, so /i/l/s is refused as `ip link set`
// rather than served as the interface s. A longer path is a command line
// too, so goip names what it cannot read.
//
// Otherwise it is the query form: /ip/OBJECT, or /ip/OBJECT/IFACE as `dev
// IFACE` on an object that takes one. The object is abbreviated as `ip`
// abbreviates it, so /i/a?dev=lo is /ip/addr?dev=lo.
func (h *Handler) servePath(w http.ResponseWriter, r *http.Request) {
	rest, ok := strings.CutPrefix(r.URL.EscapedPath(), Prefix+"/")
	if !ok {
		rest = strings.TrimPrefix(r.URL.EscapedPath(), ShortPrefix+"/")
	}
	toks, err := tokens(rest)
	switch {
	case err != nil:
		writeError(w, statusOf(err), err, "", true)
		return
	case len(toks) == 0:
		h.serveIndex(w, r)
		return
	case strings.HasPrefix(toks[0], "-"):
		h.serveCommand(w, r, toks)
		return
	}
	name, err := goip.LookupObject(toks[0])
	if err != nil {
		writeError(w, statusOf(err), err, "", true)
		return
	}
	o := byObject[name]
	switch {
	case o == nil:
		writeError(w, http.StatusInternalServerError, fmt.Errorf("goiphttp: no query form for %s", name), "", true)
	case len(toks) == 1:
		h.serveQuery(w, r, o, "")
	case len(toks) == 2 && o.devPath && !goip.IsVerb(name, toks[1]):
		h.serveQuery(w, r, o, toks[1])
	default:
		h.serveCommand(w, r, toks)
	}
}

// serveCommand serves a command line. Its output is literal, as the
// command's is: text unless -j is among the options.
func (h *Handler) serveCommand(w http.ResponseWriter, r *http.Request, toks []string) {
	if r.URL.RawQuery != "" {
		writeError(w, http.StatusBadRequest, paramErr("a command line takes no query parameters: the options are in the path"), "", true)
		return
	}
	s, res, err := goip.ResolveShow(toks)
	if err != nil {
		writeError(w, statusOf(err), err, "", true)
		return
	}
	argv := "ip " + strings.Join(res.Argv(), " ")
	if err := refusedBeforeSource(s); err != nil {
		writeError(w, statusOf(err), err, argv, !s.JSON)
		return
	}
	h.serve(w, r, s, argv, !s.JSON)
}

// errProbe is every probeSource reply.
var errProbe = errors.New("goiphttp: dry run, no source")

// probeSource is the source of a dry run: it answers every message with
// errProbe and counts them. It has each optional method goip asserts for,
// but its answers are never used: a dry run that sends anything is
// discarded.
type probeSource struct{ calls int }

func (p *probeSource) Dump([]byte, uint16) ([][]byte, error) {
	p.calls++
	return nil, errProbe
}

func (p *probeSource) Talk([]byte, uint16) ([]byte, error) {
	p.calls++
	return nil, errProbe
}

func (p *probeSource) DumpMsgs([]byte, uint16) ([][]byte, error) {
	p.calls++
	return nil, errProbe
}

// refusedBeforeSource is the error goip.Show returns for s before it sends
// a message, or nil. goip parses a command's selectors inside Show, so
// without this a selector it refuses (`route show proto bgp`, a /33) would
// open a socket before its 400 or 501. Only that case is taken from the dry
// run: if the run sent anything, the selectors needed the kernel (a device
// lookup), and the real run decides. A request with no selectors has
// nothing to refuse and skips the dry run.
func refusedBeforeSource(s goip.ShowRequest) error {
	if len(s.Selectors) == 0 {
		return nil
	}
	var p probeSource
	err := goip.Show(&p, s, io.Discard)
	if p.calls > 0 {
		return nil
	}
	return err
}

// serveQuery serves the query form: JSON unless format=text.
func (h *Handler) serveQuery(w http.ResponseWriter, r *http.Request, o *object, iface string) {
	q, err := query(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err, "", false)
		return
	}
	req, err := o.parse(q, iface)
	if err != nil {
		writeError(w, http.StatusBadRequest, err, "", req.text)
		return
	}
	h.serve(w, r, req.show, "ip "+strings.Join(req.show.Argv(), " "), req.text)
}

// query is r's query string, refused whole when any of it does not parse.
// r.URL.Query drops what it cannot parse, so a bad escape (dev=%zz), a ;
// or a parameter past net/url's limit would otherwise leave a request
// with fewer filters than it asked for: a 200 listing everything.
func query(r *http.Request) (url.Values, error) {
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return nil, paramErr("the query string does not parse: %v", err)
	}
	return q, nil
}

// serve runs s in one of the in-flight slots and writes the result. Both
// forms land here, so they share the slots, and a success carries the
// canonical command-line path of s as its Content-Location: every spelling
// of one request, in either form, names the same location (see location
// for the one exception).
func (h *Handler) serve(w http.ResponseWriter, r *http.Request, s goip.ShowRequest, argv string, text bool) {
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	default:
		writeError(w, http.StatusServiceUnavailable,
			fmt.Errorf("limit of concurrent requests reached (%d), try again later", cap(h.slots)), argv, text)
		return
	}

	body, err := h.show(s)
	if err != nil {
		writeError(w, statusOf(err), err, argv, text)
		return
	}
	ct := "application/json"
	if text {
		ct = "text/plain; charset=utf-8"
	}
	if loc, ok := location(s); ok {
		w.Header().Set("Content-Location", loc)
	}
	writeBody(w, r, http.StatusOK, ct, body, argv)
}

// location is the canonical path of s, and whether it is one the handler
// serves. Full spellings are longer than abbreviations (-s is -stats, a is
// address), so a request near the tokenizer's cap can have a canonical path
// over it; that response carries no Content-Location rather than one that
// is a 400.
func location(s goip.ShowRequest) (string, bool) {
	loc := s.Resolved().Path()
	return loc, len(loc)-len(Prefix)-1 <= argvPathMax
}

// show runs one request on a fresh source, retrying once when the kernel
// reports the dump was interrupted by a concurrent change.
func (h *Handler) show(s goip.ShowRequest) ([]byte, error) {
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		var body []byte
		body, err = h.showOnce(s)
		if !errors.Is(err, xtcpnl.ErrDumpInterrupted) {
			return body, err
		}
	}
	return nil, err
}

func (h *Handler) showOnce(s goip.ShowRequest) (body []byte, err error) {
	src, closeSrc, err := h.open()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errOpen, err)
	}
	defer func() {
		if cerr := closeSrc(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	lw := &limitWriter{max: h.maxBytes}
	if err := goip.Show(src, s, lw); err != nil {
		if lw.over {
			return nil, errTooLarge
		}
		return nil, err
	}
	if lw.over {
		return nil, errTooLarge
	}
	return lw.buf.Bytes(), nil
}

var (
	errOpen     = errors.New("open netlink")
	errTooLarge = errors.New("response exceeds the configured size limit")
)

// statusOf maps a show failure to its status. ErrNoSuchDevice is tested
// before ErrNotImplemented because netconf's missing device carries both.
// A refused write verb or option is the caller's request, so a 400, as is a
// device name no kernel takes (ip's "not a valid ifname"); a read goip does
// not serve is a 501.
func statusOf(err error) int {
	switch {
	case errors.Is(err, errParam), errors.Is(err, goip.ErrWriteVerb), errors.Is(err, goip.ErrRefusedOption),
		errors.Is(err, xtcpnl.ErrBadIfName):
		return http.StatusBadRequest
	case errors.Is(err, goip.ErrUnknownObject):
		return http.StatusNotFound
	case errors.Is(err, goip.ErrNoSuchDevice), errors.Is(err, unix.ENODEV):
		return http.StatusNotFound
	case errors.Is(err, goip.ErrNotImplemented):
		return http.StatusNotImplemented
	case errors.Is(err, goip.ErrInvalidArgument):
		return http.StatusBadRequest
	case errors.Is(err, xtcpnl.ErrDumpInterrupted):
		return http.StatusServiceUnavailable
	case errors.Is(err, unix.EAGAIN):
		return http.StatusGatewayTimeout
	}
	return http.StatusInternalServerError
}

// limitWriter buffers up to max bytes and fails the write that would exceed
// it, so goip stops rendering instead of filling memory.
type limitWriter struct {
	buf  bytes.Buffer
	max  int
	over bool
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if l.buf.Len()+len(p) > l.max {
		l.over = true
		return 0, errTooLarge
	}
	return l.buf.Write(p)
}

// nosniff keeps a browser from reading a text/plain body as HTML: a command
// line's output and errors are `ip`'s bytes, which can quote the path back
// (`Device "<x>" does not exist`), and escaping them would break parity.
const nosniff = "nosniff"

func writeBody(w http.ResponseWriter, r *http.Request, status int, contentType string, body []byte, argv string) {
	hdr := w.Header()
	hdr.Set("Content-Type", contentType)
	hdr.Set("Cache-Control", "no-store")
	hdr.Set("X-Content-Type-Options", nosniff)
	hdr.Set("Content-Length", strconv.Itoa(len(body)))
	if argv != "" {
		hdr.Set(ArgvHeader, argv)
	}
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}
	send(w, body)
}

// send writes a response body. A write error means the client went away
// after the header was sent; there is no one left to tell, so the error is
// checked here, once, and dropped rather than ignored at every call site.
func send(w http.ResponseWriter, b []byte) {
	if _, err := w.Write(b); err != nil {
		return
	}
}

// errorBody is the JSON error document.
type errorBody struct {
	Error string `json:"error"`
	Argv  string `json:"argv,omitempty"`
}

func writeError(w http.ResponseWriter, status int, err error, argv string, text bool) {
	hdr := w.Header()
	hdr.Set("Cache-Control", "no-store")
	hdr.Set("X-Content-Type-Options", nosniff)
	if argv != "" {
		hdr.Set(ArgvHeader, argv)
	}
	if text {
		hdr.Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(status)
		send(w, []byte(err.Error()+"\n"))
		return
	}
	b, merr := json.Marshal(errorBody{Error: err.Error(), Argv: argv})
	if merr != nil {
		b = []byte(`{"error":"internal error"}`)
	}
	hdr.Set("Content-Type", "application/json")
	w.WriteHeader(status)
	send(w, append(b, '\n'))
}
