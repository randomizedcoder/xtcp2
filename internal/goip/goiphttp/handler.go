package goiphttp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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
	paths := []string{Prefix}
	for _, o := range objects {
		paths = append(paths, Prefix+"/"+o.path)
		if o.devPath {
			paths = append(paths, Prefix+"/"+o.path+"/{interface}")
		}
	}
	return paths
}

// Register adds every /ip route to mux, each passed through wrap (auth,
// timeout) first. wrap may be nil. Only GET and HEAD are served; anything
// else is 405.
//
// The patterns carry no method: one method pattern anywhere on a ServeMux
// costs an allocation on every request it routes, /metrics included, so the
// method is checked here instead.
func (h *Handler) Register(mux *http.ServeMux, wrap func(http.Handler) http.Handler) {
	if wrap == nil {
		wrap = func(x http.Handler) http.Handler { return x }
	}
	idx := wrap(readOnly(http.HandlerFunc(h.serveIndex)))
	mux.Handle(Prefix, idx)
	mux.Handle(Prefix+"/{$}", idx)
	for _, o := range objects {
		sh := wrap(readOnly(h.objectHandler(o)))
		mux.Handle(Prefix+"/"+o.path, sh)
		if o.devPath {
			mux.Handle(Prefix+"/"+o.path+"/{interface}", sh)
		}
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
	if q := r.URL.Query(); len(q) != 0 {
		if len(q) != 1 || q.Get(formatParam) != "json" || len(q[formatParam]) != 1 {
			writeError(w, http.StatusBadRequest, paramErr("the index takes no parameters but format=json"), "", false)
			return
		}
	}
	writeBody(w, r, http.StatusOK, "application/json", h.index, "")
}

func (h *Handler) objectHandler(o *object) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, err := o.parse(r.URL.Query(), r.PathValue("interface"))
		if err != nil {
			writeError(w, http.StatusBadRequest, err, "", req.text)
			return
		}
		argv := "ip " + strings.Join(req.show.Argv(), " ")

		select {
		case h.slots <- struct{}{}:
			defer func() { <-h.slots }()
		default:
			writeError(w, http.StatusServiceUnavailable,
				fmt.Errorf("limit of concurrent requests reached (%d), try again later", cap(h.slots)), argv, req.text)
			return
		}

		body, err := h.show(req.show)
		if err != nil {
			writeError(w, statusOf(err), err, argv, req.text)
			return
		}
		ct := "application/json"
		if req.text {
			ct = "text/plain; charset=utf-8"
		}
		writeBody(w, r, http.StatusOK, ct, body, argv)
	})
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
func statusOf(err error) int {
	switch {
	case errors.Is(err, errParam):
		return http.StatusBadRequest
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

func writeBody(w http.ResponseWriter, r *http.Request, status int, contentType string, body []byte, argv string) {
	hdr := w.Header()
	hdr.Set("Content-Type", contentType)
	hdr.Set("Cache-Control", "no-store")
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

// indexEntry is one object in the /ip index.
type indexEntry struct {
	Path          string   `json:"path"`
	InterfacePath string   `json:"interface_path,omitempty"`
	Command       string   `json:"command"`
	Params        []string `json:"params"`
}

func indexDocument() ([]byte, error) {
	entries := make([]indexEntry, 0, len(objects))
	for _, o := range objects {
		e := indexEntry{
			Path:    Prefix + "/" + o.path,
			Command: "ip " + strings.Join(goip.ShowRequest{Object: o.object, JSON: true}.Argv(), " "),
			Params:  []string{formatParam},
		}
		if o.devPath {
			e.InterfacePath = e.Path + "/{interface}"
		}
		for _, p := range o.params {
			e.Params = append(e.Params, p.name)
		}
		entries = append(entries, e)
	}
	b, err := json.Marshal(entries)
	if err != nil {
		return nil, fmt.Errorf("goiphttp: index: %w", err)
	}
	return append(b, '\n'), nil
}
