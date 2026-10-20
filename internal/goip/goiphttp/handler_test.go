package goiphttp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
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

// noDevSource is an errSource whose single gets fail as a kernel without
// the device fails them.
type noDevSource struct{ *errSource }

func (noDevSource) Talk([]byte, uint16) ([]byte, error) { return nil, unix.ENODEV }

func errOpener(src goip.Source) Opener {
	return func() (goip.Source, func() error, error) { return src, func() error { return nil }, nil }
}

func want(tb testing.TB, pcap string, r goip.ShowRequest) []byte {
	tb.Helper()
	src, err := goip.OpenReplay(pcap)
	if err != nil {
		tb.Fatal(err)
	}
	var b bytes.Buffer
	if err := goip.Show(src, r, &b); err != nil {
		tb.Fatal(err)
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

// noLocation is a handlerTest's wantLocation for a 200 without one.
const noLocation = "(none)"

// labels is `label P` for each pattern length in n, P that many copies of
// c, as path segments.
func labels(c string, n ...int) string {
	var b strings.Builder
	for _, k := range n {
		b.WriteString("/label/" + strings.Repeat(c, k))
	}
	return b.String()
}

// handlerTest is one TestHandler row.
type handlerTest struct {
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
	// wantLocation is the Content-Location, when set; noLocation is none.
	wantLocation string
	// wantAllow is the Allow header, when set.
	wantAllow string
	// sameAs is a target whose body must equal this one's.
	sameAs    string
	wantOpens int32 // -1: don't check
}

// handlerTests are TestHandler's rows. FuzzArgvPath and FuzzIPHandler seed
// from their targets, so every row is in the seed corpus.
func handlerTests(tb testing.TB) []handlerTest {
	addrPcap := dumpsDir + "netlink_route_getaddr.pcap"
	linkPcap := dumpsDir + "netlink_route_getlink.pcap"
	routePcap := dumpsDir + "netlink_route_getroute.pcap"
	devPcap := dumpsDir + "netlink_route_getaddr_dev.pcap"
	briefPcap := "../../../pkg/xtcpnl/testdata/7_1_4/link-write/addr-show/mixed_links/netlink_route_getaddr_br.pcap"
	addrJSON := want(tb, addrPcap, goip.ShowRequest{Object: "address", JSON: true})
	addrText := want(tb, addrPcap, goip.ShowRequest{Object: "address"})
	const textPlain = "text/plain; charset=utf-8"

	return []handlerTest{
		{name: "addr_json", description: "positive: /ip/addr is ip -j address show",
			open: replayOpener(addrPcap), method: "GET", target: "/ip/addr",
			wantStatus: 200, wantType: "application/json", wantBody: addrJSON, wantArgv: "ip -j address show", wantOpens: 1},
		{name: "addr_text_dev", description: "positive: /ip/addr/goip0?format=text is ip address show dev goip0",
			open: replayOpener(dumpsDir + "netlink_route_getaddr_dev.pcap"), method: "GET", target: "/ip/addr/goip0?format=text",
			wantStatus: 200, wantType: "text/plain; charset=utf-8",
			wantBody: want(tb, dumpsDir+"netlink_route_getaddr_dev.pcap", goip.ShowRequest{Object: "address", Selectors: []string{"dev", "goip0"}}),
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
		{name: "unimplemented_tunnel", description: "negative: /ip/tunnel is an object goip cannot show yet: 501 naming the command",
			open: replayOpener(linkPcap), method: "GET", target: "/ip/tunnel", wantStatus: 501, wantInBody: "ip tunnel show:", wantOpens: 0},
		{name: "bad_param", description: "negative: a bad parameter is 400 and opens nothing",
			open: replayOpener(linkPcap), method: "GET", target: "/ip/link?table=main", wantStatus: 400, wantOpens: 0},
		{name: "query_bad_escape", description: "negative: dev=%zz does not parse: 400, never a listing without the filter",
			open: replayOpener(linkPcap), method: "GET", target: "/ip/link?dev=%zz", wantStatus: 400, wantInBody: "does not parse", wantOpens: 0},
		{name: "query_semicolon", description: "negative: a ; separator does not parse: 400, never dev=lo dropped",
			open: replayOpener(linkPcap), method: "GET", target: "/ip/link?dev=lo;x=1", wantStatus: 400, wantInBody: "does not parse", wantOpens: 0},
		{name: "query_over_limit", description: "boundary: one parameter past net/url's 10,000 is 400, never an unfiltered 200",
			open: replayOpener(linkPcap), method: "GET", target: "/ip/link?" + strings.Repeat("dev=lo&", 10000) + "dev=lo", wantStatus: 400, wantInBody: "does not parse", wantOpens: 0},
		{name: "index_bad_escape", description: "negative: the index refuses a query that does not parse",
			open: replayOpener(linkPcap), method: "GET", target: "/ip?format=%zz", wantStatus: 400, wantInBody: "does not parse", wantOpens: 0},
		{name: "dev_name_too_long", description: "boundary: a 16-byte device name is ip's \"not a valid ifname\": 400, not 500",
			open: errOpener(noDevSource{&errSource{}}), method: "GET", target: "/ip/link/show/dev/" + strings.Repeat("x", 16), wantStatus: 400, wantOpens: 0},
		{name: "dev_name_at_max", description: "boundary: a 15-byte device name is valid, and missing: 404",
			open: errOpener(noDevSource{&errSource{}}), method: "GET", target: "/ip/link/show/dev/" + strings.Repeat("x", 15), wantStatus: 404, wantOpens: 1},
		{name: "dev_name_too_long_query", description: "boundary: the query form's 16-byte device name is 400 too",
			open: errOpener(noDevSource{&errSource{}}), method: "GET", target: "/ip/link/" + strings.Repeat("x", 16), wantStatus: 400, wantOpens: -1},
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

		// The command-line form (IA2-T02).
		{name: "link_show", description: "positive: /ip/link/show is the text of goip link show",
			open: replayOpener(linkPcap), method: "GET", target: "/ip/link/show",
			wantStatus: 200, wantType: textPlain, wantBody: want(tb, linkPcap, goip.ShowRequest{Object: "link"}),
			wantArgv: "ip link show", wantLocation: "/ip/link/show", wantOpens: 1},
		{name: "i_a_s", description: "positive: /i/a/s is /ip/address/show",
			open: replayOpener(addrPcap), method: "GET", target: "/i/a/s",
			wantStatus: 200, wantType: textPlain, wantBody: addrText, wantArgv: "ip address show", sameAs: "/ip/address/show", wantOpens: 1},
		{name: "i_ad_s", description: "positive: ad is address too",
			open: replayOpener(addrPcap), method: "GET", target: "/i/ad/s", wantStatus: 200, wantBody: addrText, wantArgv: "ip address show", wantOpens: 1},
		{name: "i_a_show", description: "positive: the verb in full",
			open: replayOpener(addrPcap), method: "GET", target: "/i/a/show", wantStatus: 200, wantBody: addrText, wantArgv: "ip address show", wantOpens: 1},
		{name: "i_l_sh", description: "positive: sh is link's shortest show",
			open: replayOpener(linkPcap), method: "GET", target: "/i/l/sh", wantStatus: 200, wantArgv: "ip link show", wantOpens: 1},
		{name: "json_option", description: "positive: -j makes the output JSON, the bytes of goip -j address show",
			open: replayOpener(addrPcap), method: "GET", target: "/ip/-j/addr/show",
			wantStatus: 200, wantType: "application/json", wantBody: addrJSON,
			wantArgv: "ip -json address show", wantLocation: "/ip/-json/address/show", wantOpens: 1},
		{name: "brief", description: "positive: /ip/-br/a is goip -br address show",
			open: replayOpener(briefPcap), method: "GET", target: "/ip/-br/a",
			wantStatus: 200, wantType: textPlain, wantBody: want(tb, briefPcap, goip.ShowRequest{Object: "address", Brief: true}),
			wantArgv: "ip -brief address show", wantLocation: "/ip/-brief/address/show", wantOpens: 1},
		{name: "full_grammar", description: "positive: options, an abbreviated object and verb, and selectors all reach goip",
			open: replayOpener(routePcap), method: "GET", target: "/i/-4/r/s/table/all",
			wantStatus: 200, wantType: textPlain,
			wantBody: want(tb, routePcap, goip.ShowRequest{Object: "route", Family: unix.AF_INET, Selectors: []string{"table", "all"}}),
			wantArgv: "ip -4 route show table all", wantLocation: "/ip/-4/route/show/table/all", wantOpens: 1},
		{name: "selector_gap", description: "negative: scope reaches goip's route parser, which does not implement it: 501 with the argv",
			open: replayOpener(routePcap), method: "GET", target: "/ip/-4/route/show/table/all/scope/link",
			wantStatus: 501, wantType: textPlain, wantInBody: `route show "scope"`, wantArgv: "ip -4 route show table all scope link", wantOpens: 0},
		{name: "route_proto", description: "positive: proto is parsed by goip's route show and served",
			open: replayOpener(routePcap), method: "GET", target: "/ip/-4/route/show/table/all/proto/bgp",
			wantStatus: 200, wantType: textPlain, wantArgv: "ip -4 route show table all proto bgp", wantOpens: 1},
		{name: "query_form_kept", description: "positive: the query form is unchanged, and gains the canonical location",
			open: replayOpener(devPcap), method: "GET", target: "/ip/addr?dev=goip0",
			wantStatus: 200, wantType: "application/json",
			wantBody: want(tb, devPcap, goip.ShowRequest{Object: "address", JSON: true, Selectors: []string{"dev", "goip0"}}),
			wantArgv: "ip -j address show dev goip0", wantLocation: "/ip/-json/address/show/dev/goip0", wantOpens: 1},
		{name: "query_form_abbrev", description: "positive: /i/a?dev= is /ip/addr?dev=",
			open: replayOpener(devPcap), method: "GET", target: "/i/a?dev=goip0",
			wantStatus: 200, wantType: "application/json", sameAs: "/ip/addr?dev=goip0", wantArgv: "ip -j address show dev goip0", wantOpens: 1},
		{name: "shortcut_kept", description: "positive: /ip/link/lo is still the interface shortcut; the kernel's ENODEV is a 404",
			open: errOpener(&errSource{errs: []error{unix.ENODEV}}), method: "GET", target: "/ip/link/lo",
			wantStatus: 404, wantArgv: "ip -j link show dev lo", wantOpens: 1},
		{name: "content_location", description: "positive: list and lst converge on the show location",
			open: replayOpener(addrPcap), method: "GET", target: "/i/a/lst",
			wantStatus: 200, wantArgv: "ip address lst", wantLocation: "/ip/address/show", wantOpens: 1},
		{name: "pct2f_prefix", description: "positive: a %2F stays inside its token and equals the joined form",
			open: replayOpener(routePcap), method: "GET", target: "/ip/route/show/10.0.0.0%2F8",
			wantStatus: 200, wantType: textPlain,
			wantBody: want(tb, routePcap, goip.ShowRequest{Object: "route", Selectors: []string{"10.0.0.0/8"}}),
			wantArgv: "ip route show 10.0.0.0/8", sameAs: "/ip/route/show/10.0.0.0/8", wantOpens: 1},
		{name: "default_route_prefix", description: "positive: 0.0.0.0/0 joins and is only the default route",
			open: replayOpener(routePcap), method: "GET", target: "/ip/-j/route/show/0.0.0.0/0",
			wantStatus: 200, wantType: "application/json",
			wantBody: want(tb, routePcap, goip.ShowRequest{Object: "route", JSON: true, Selectors: []string{"0.0.0.0/0"}}),
			wantArgv: "ip -json route show 0.0.0.0/0", wantOpens: 1},
		{name: "route_prefix_listed", description: "positive: an exact prefix lists that route and no other",
			open: replayOpener(routePcap), method: "GET", target: "/i/r/s/198.18.1.0/24",
			wantStatus: 200, wantType: textPlain,
			wantBody: []byte("198.18.1.0/24 via 192.0.2.10 dev goip0 mtu 1400 advmss 1300 \n"),
			wantArgv: "ip route show 198.18.1.0/24", wantOpens: 1},
		{name: "bad_route_prefix", description: "negative: a length past 32 is ip's get_prefix error, a 400",
			open: replayOpener(routePcap), method: "GET", target: "/ip/route/show/10.0.0.0%2F33",
			wantStatus: 400, wantInBody: `any valid prefix is expected rather than "10.0.0.0/33"`, wantOpens: 0},

		{name: "i_l_s_set", description: "negative: ip link s is set, refused before any socket opens",
			open: replayOpener(linkPcap), method: "GET", target: "/i/l/s",
			wantStatus: 400, wantType: textPlain, wantInBody: "ip link set:", wantOpens: 0},
		{name: "i_a_sa_save", description: "negative: ip address sa is save, which reads but is not served",
			open: replayOpener(addrPcap), method: "GET", target: "/i/a/sa", wantStatus: 501, wantInBody: "ip address save:", wantOpens: 0},
		{name: "write_flush", description: "negative: flush is a write",
			open: replayOpener(addrPcap), method: "GET", target: "/ip/addr/flush", wantStatus: 400, wantInBody: "ip address flush:", wantOpens: 0},
		{name: "netns", description: "negative: -n is -netns, refused",
			open: replayOpener(linkPcap), method: "GET", target: "/ip/-n/x/link", wantStatus: 400, wantInBody: "-netns", wantOpens: 0},
		{name: "stats_two", description: "negative: -s -s is refused with goip's message",
			open: replayOpener(linkPcap), method: "GET", target: "/ip/-s/-s/link", wantStatus: 400, wantInBody: "use a single -s", wantOpens: 0},
		{name: "unknown_object", description: "negative: zz is no object",
			open: replayOpener(linkPcap), method: "GET", target: "/ip/zz/show", wantStatus: 404, wantInBody: "unknown object", wantOpens: 0},
		{name: "unimplemented", description: "negative: /i/m is maddress, named in full",
			open: replayOpener(linkPcap), method: "GET", target: "/i/m", wantStatus: 501, wantInBody: "ip maddress show:", wantOpens: 0},
		{name: "query_on_argv", description: "negative: a command line takes no query string",
			open: replayOpener(linkPcap), method: "GET", target: "/ip/link/show?x=1", wantStatus: 400, wantInBody: "no query parameters", wantOpens: 0},
		{name: "bad_selector", description: "negative: dev does not abbreviate, so address show d lo is a 400 with ip's message",
			open: replayOpener(addrPcap), method: "GET", target: "/ip/addr/show/d/lo",
			wantStatus: 400, wantType: textPlain, wantInBody: `either "dev" is duplicate, or "lo" is garbage`, wantArgv: "ip address show d lo", wantOpens: 0},
		{name: "bad_table", description: "negative: a table that is no id is ip's invarg, a 400",
			open: replayOpener(routePcap), method: "GET", target: "/ip/route/show/table/wombat",
			wantStatus: 400, wantInBody: `argument "wombat" is wrong: table id value is invalid`, wantArgv: "ip route show table wombat", wantOpens: 0},
		{name: "bad_token", description: "negative: a tokenizer refusal is a 400 before any socket",
			open: replayOpener(linkPcap), method: "GET", target: "/ip/link/show/dev/a%00", wantStatus: 400, wantInBody: "control character", wantOpens: 0},
		{name: "no_dev_object", description: "negative: rule takes no interface, so /ip/rule/x is a command line ip rejects",
			open: replayOpener(linkPcap), method: "GET", target: "/ip/rule/x", wantStatus: 400, wantInBody: `command "x" is unknown for ip rule`, wantOpens: 0},
		{name: "post_argv", description: "negative: POST on a command line is 405",
			open: replayOpener(addrPcap), method: "POST", target: "/i/a/s", wantStatus: 405, wantAllow: "GET, HEAD", wantOpens: 0},

		{name: "root_m", description: "boundary: /m is not /metrics; only the root abbreviates to /i",
			open: replayOpener(linkPcap), method: "GET", target: "/m", wantStatus: 404, wantOpens: 0},
		{name: "root_in", description: "boundary: /in is not a root",
			open: replayOpener(linkPcap), method: "GET", target: "/in", wantStatus: 404, wantOpens: 0},
		{name: "root_ipx", description: "boundary: /ipx is not a root",
			open: replayOpener(linkPcap), method: "GET", target: "/ipx", wantStatus: 404, wantOpens: 0},
		{name: "index_short_root", description: "boundary: /i is the index",
			open: replayOpener(linkPcap), method: "GET", target: "/i", wantStatus: 200, sameAs: "/ip/", wantOpens: 0},
		{name: "index_short_root_slash", description: "boundary: /i/ is the index",
			open: replayOpener(linkPcap), method: "GET", target: "/i/", wantStatus: 200, sameAs: "/ip", wantOpens: 0},
		{name: "join_v4_33", description: "boundary: an IPv4 /33 is a 400 naming the range",
			open: replayOpener(routePcap), method: "GET", target: "/ip/route/show/10.0.0.0/33", wantStatus: 400, wantInBody: "0-32", wantOpens: 0},

		{name: "iface_named_show", description: "corner: an interface named show needs dev",
			open: errOpener(&errSource{errs: []error{unix.ENODEV}}), method: "GET", target: "/ip/link/show/dev/show", wantStatus: 404, wantArgv: "ip link show dev show", wantOpens: 1},
		{name: "link_bare_device", description: "positive: ip link show NAME is dev NAME, so a missing one is ENODEV's 404, not a 501",
			open: errOpener(&errSource{errs: []error{unix.ENODEV}}), method: "GET", target: "/ip/link/show/nosuch0", wantStatus: 404, wantArgv: "ip link show nosuch0", wantOpens: 1},
		{name: "link_bare_device_short", description: "positive: the same through /i",
			open: errOpener(&errSource{errs: []error{unix.ENODEV}}), method: "GET", target: "/i/l/sh/nosuch0", wantStatus: 404, wantArgv: "ip link show nosuch0", wantOpens: 1},
		{name: "link_bare_keyword", description: "negative: up is a keyword of the same loop, still a 501",
			open: errOpener(&errSource{}), method: "GET", target: "/ip/link/show/up", wantStatus: 501, wantInBody: `link show "up"`, wantOpens: 0},
		{name: "iface_named_s", description: "corner: /ip/link/s is set, not the interface s",
			open: replayOpener(linkPcap), method: "GET", target: "/ip/link/s", wantStatus: 400, wantInBody: "ip link set:", wantOpens: 0},
		{name: "head_argv", description: "corner: HEAD on a command line sends the headers and no body",
			open: replayOpener(addrPcap), method: "HEAD", target: "/i/a/s",
			wantStatus: 200, wantType: textPlain, wantBody: []byte{}, wantArgv: "ip address show", wantLocation: "/ip/address/show", wantOpens: 1},
		{name: "alias_location", description: "corner: ntbl stays as typed in the argv; the location is canonical",
			open: errOpener(&errSource{}), method: "GET", target: "/i/ntbl/show", wantStatus: 200, wantArgv: "ip ntbl show", wantLocation: "/ip/ntable/show", wantOpens: 1},
		{name: "label_star_storm", description: "boundary: a label of 64 stars is served; the stars compile to one",
			open: replayOpener(addrPcap), method: "GET", target: "/ip/addr/show/label/" + strings.Repeat("*", 64),
			wantStatus: 200, wantArgv: "ip address show label " + strings.Repeat("*", 64), wantOpens: 1},
		{name: "pattern_over_cap", description: "negative: a label pattern over the 255-byte token cap is a 400 before any socket",
			open: replayOpener(addrPcap), method: "GET", target: "/ip/addr/show/label/" + strings.Repeat("*", 256),
			wantStatus: 400, wantInBody: "longer than 255 bytes", wantOpens: 0},
		{name: "location_sub_delims", description: "corner: a label of commas keeps them raw in the location, which serves the same body",
			open: replayOpener(addrPcap), method: "GET", target: "/i/a/s" + labels(",", 255, 255, 255),
			wantStatus: 200, wantLocation: "/ip/address/show" + labels(",", 255, 255, 255),
			sameAs: "/ip/address/show" + labels(",", 255, 255, 255), wantOpens: 1},
		{name: "location_at_cap", description: "boundary: a canonical path of exactly 1024 bytes after /ip/ is the location",
			open: replayOpener(addrPcap), method: "GET", target: "/i/a/s" + labels("x", 255, 255, 255, 219),
			wantStatus: 200, wantLocation: "/ip/address/show" + labels("x", 255, 255, 255, 219), wantOpens: 1},
		{name: "location_over_cap", description: "boundary: one byte more and the 200 carries no location, rather than one that is a 400",
			open: replayOpener(addrPcap), method: "GET", target: "/i/a/s" + labels("x", 255, 255, 255, 220),
			wantStatus: 200, wantLocation: noLocation, wantOpens: 1},
		{name: "json_error", description: "corner: with -j, a show failure is a JSON error",
			open: errOpener(&errSource{errs: []error{unix.ENODEV}}), method: "GET", target: "/ip/-j/link/show/dev/nosuch0",
			wantStatus: 404, wantType: "application/json", wantInBody: `"argv":"ip -json link show dev nosuch0"`, wantOpens: 1},
	}
}

// TestHandler drives the registered routes end to end over replay captures
// and failing sources.
//
// go test ./internal/goip/goiphttp/ -run TestHandler
func TestHandler(t *testing.T) {
	tests := handlerTests(t)
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
			if tc.wantLocation == noLocation && rec.Header().Get("Content-Location") != "" {
				t.Errorf("%s: Content-Location %q, want none", tc.description, rec.Header().Get("Content-Location"))
			}
			if tc.wantLocation != "" && tc.wantLocation != noLocation && rec.Header().Get("Content-Location") != tc.wantLocation {
				t.Errorf("%s: Content-Location %q, want %q", tc.description, rec.Header().Get("Content-Location"), tc.wantLocation)
			}
			if tc.wantStatus != 200 && rec.Header().Get("Content-Location") != "" {
				t.Errorf("%s: Content-Location %q on a %d", tc.description, rec.Header().Get("Content-Location"), rec.Code)
			}
			if tc.wantAllow != "" && rec.Header().Get("Allow") != tc.wantAllow {
				t.Errorf("%s: Allow %q, want %q", tc.description, rec.Header().Get("Allow"), tc.wantAllow)
			}
			if tc.sameAs != "" {
				h2, err := New(Options{Open: tc.open, MaxBytes: tc.maxBytes})
				if err != nil {
					t.Fatal(err)
				}
				if other := serve(t, h2, tc.method, tc.sameAs); other.Code != rec.Code || !bytes.Equal(other.Body.Bytes(), rec.Body.Bytes()) {
					t.Errorf("%s: %s gave %d %q, want %s's %d %q", tc.description, tc.sameAs, other.Code, other.Body.String(), tc.target, rec.Code, rec.Body.String())
				}
			}
			if rec.Code != http.StatusTemporaryRedirect && rec.Code != http.StatusNotFound && rec.Header().Get("X-Content-Type-Options") != nosniff {
				t.Errorf("%s: X-Content-Type-Options %q on a %d", tc.description, rec.Header().Get("X-Content-Type-Options"), rec.Code)
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

// TestHandlerSlotsShared checks that the two forms share one pool of
// in-flight slots: a command line and a query fill it, and a third request
// of either form is refused.
func TestHandlerSlotsShared(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{})
	blocking := func() (goip.Source, func() error, error) {
		entered <- struct{}{}
		<-release
		return &errSource{}, func() error { return nil }, nil
	}
	h, err := New(Options{Open: blocking, InFlight: 2})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.Register(mux, nil)

	done := make(chan int, 2)
	for _, target := range []string{"/ip/rule", "/i/ru/s"} {
		go func() {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest("GET", target, nil))
			done <- rec.Code
		}()
		<-entered
	}
	for _, tc := range []struct {
		name, category, description, target string
		expected                            int
	}{
		{"argv_third", "boundary", "a command line while both slots are held", "/i/l/sh", http.StatusServiceUnavailable},
		{"query_third", "boundary", "a query while both slots are held", "/ip/link", http.StatusServiceUnavailable},
		{"refusal_unlimited", "corner", "a refused path needs no slot, so it is still refused as itself", "/i/l/s", http.StatusBadRequest},
		{"index_unlimited", "positive", "the short index needs no slot", "/i", http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), "GET", tc.target, nil))
			if rec.Code != tc.expected {
				t.Fatalf("%s: %s: %s gave %d, want %d", tc.category, tc.description, tc.target, rec.Code, tc.expected)
			}
		})
	}
	close(release)
	for range 2 {
		if code := <-done; code != http.StatusOK {
			t.Fatalf("held request: %d, want 200", code)
		}
	}
}

// TestRegisterWrap checks that wrap (go-link-monitor's read auth) guards
// both roots and both forms, and that nothing is read before it answers.
func TestRegisterWrap(t *testing.T) {
	var c counter
	h, err := New(Options{Open: c.wrap(replayOpener(dumpsDir + "netlink_route_getlink.pcap"))})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.Register(mux, func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		})
	})
	for _, tc := range []struct {
		name, category, description, target string
	}{
		{"argv", "positive", "a command line with no credential", "/i/a/s"},
		{"query_short", "positive", "the query form under /i", "/i/a?dev=lo"},
		{"query_long", "positive", "the query form under /ip", "/ip/link"},
		{"index_short", "boundary", "the /i index", "/i"},
		{"index_long_slash", "boundary", "the /ip/ index", "/ip/"},
		{"write_verb", "corner", "a write-verb path is refused by auth first, not by the resolver", "/i/l/s"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), "GET", tc.target, nil))
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("%s: %s: %s gave %d, want 401", tc.category, tc.description, tc.target, rec.Code)
			}
		})
	}
	if c.opens.Load() != 0 {
		t.Errorf("unauthorized requests opened %d sources", c.opens.Load())
	}
}

// TestRegistryCoversGoip checks that every object goip shows, in any of its
// spellings, has a query form: servePath answers 500 for one that does not.
func TestRegistryCoversGoip(t *testing.T) {
	for key := range goip.Shortest() {
		if strings.Contains(key, " ") {
			continue
		}
		name, err := goip.LookupObject(key)
		if err != nil {
			t.Errorf("%s: %v", key, err)
			continue
		}
		if byObject[name] == nil {
			t.Errorf("%s (%s) has no registry entry", key, name)
		}
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

// TestIndex checks the index document: the registry's objects, the
// shortest spellings, the refused query and a stable build.
//
// go test ./internal/goip/goiphttp/ -run TestIndex
func TestIndex(t *testing.T) {
	h, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	var doc indexDoc
	if err := json.Unmarshal(h.index, &doc); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name        string
		category    string
		description string
		// expected: check returns "" when the index holds.
		expected func() string
	}{
		{
			name: "index_lists_shortest", category: "positive",
			description: "/ip lists address: a and link show: sh, as goip.Shortest has them",
			expected: func() string {
				if doc.Shortest["address"] != "a" || doc.Shortest["link show"] != "sh" {
					return fmt.Sprintf("address %q, link show %q", doc.Shortest["address"], doc.Shortest["link show"])
				}
				if !maps.Equal(doc.Shortest, goip.Shortest()) {
					return "shortest differs from goip.Shortest()"
				}
				return ""
			},
		},
		{
			name: "index_every_object", category: "positive",
			description: "every object goip shows is an entry, in registry order, with its parameters",
			expected: func() string {
				if len(doc.Objects) != len(objects) || len(objects) != len(goip.ShowObjects()) {
					return fmt.Sprintf("index %d, registry %d, goip %d", len(doc.Objects), len(objects), len(goip.ShowObjects()))
				}
				for i, e := range doc.Objects {
					o := objects[i]
					if e.Path != Prefix+"/"+o.path || (e.InterfacePath != "") != o.devPath || len(e.Params) != len(o.params)+1 {
						return fmt.Sprintf("entry %d %+v does not match %s", i, e, o.path)
					}
				}
				return ""
			},
		},
		{
			name: "index_rules", category: "positive",
			description: "both forms' rules and every example are in the index",
			expected: func() string {
				if doc.Rule != commandRule || doc.QueryRule != queryRule || !slices.Equal(doc.Examples, examples) {
					return fmt.Sprintf("rule %q, query rule %q, %d examples", doc.Rule, doc.QueryRule, len(doc.Examples))
				}
				return ""
			},
		},
		{
			name: "index_query_refused", category: "negative",
			description: "/ip?x=1 is a 400, as before",
			expected: func() string {
				mux := http.NewServeMux()
				h.Register(mux, nil)
				rec := httptest.NewRecorder()
				mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ip?x=1", nil))
				if rec.Code != http.StatusBadRequest {
					return fmt.Sprintf("status %d", rec.Code)
				}
				return ""
			},
		},
		{
			name: "index_stable", category: "boundary",
			description: "two builds are byte-identical: the shortest map is written in key order",
			expected: func() string {
				again, err := indexDocument()
				if err != nil || !bytes.Equal(again, h.index) {
					return fmt.Sprintf("rebuild differs (%v)", err)
				}
				return ""
			},
		},
		{
			name: "index_paths", category: "corner",
			description: "Paths()[0] is the long root, for the startup log",
			expected: func() string {
				if paths := h.Paths(); paths[0] != Prefix {
					return fmt.Sprintf("Paths()[0] = %q", paths[0])
				}
				return ""
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.category+"/"+tt.name, func(t *testing.T) {
			if msg := tt.expected(); msg != "" {
				t.Errorf("%s: %s", tt.description, msg)
			}
		})
	}
}

// TestIndexExamples drives every example in the index through the handler
// (index_examples_resolve): each carries its stated command as X-Goip-Argv,
// and runs, or is the 501 its NotServed names. The source answers every
// dump with nothing and every get with ENODEV, as a kernel without the
// device does, so a dev selector is a 404.
//
// go test ./internal/goip/goiphttp/ -run TestIndexExamples
func TestIndexExamples(t *testing.T) {
	h, err := New(Options{Open: errOpener(noDevSource{&errSource{}})})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.Register(mux, nil)
	for _, ex := range examples {
		t.Run(strings.TrimPrefix(ex.Path, "/"), func(t *testing.T) {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, ex.Path, nil))
			if got := rec.Header().Get(ArgvHeader); got != ex.Command {
				t.Errorf("%s: %s = %q, want %q (status %d: %s)", ex.Path, ArgvHeader, got, ex.Command, rec.Code, rec.Body)
			}
			served := rec.Code == http.StatusOK || rec.Code == http.StatusNotFound
			switch {
			case ex.NotServed != "" && rec.Code != http.StatusNotImplemented:
				t.Errorf("%s: status %d, want 501 (%s): goip serves it now, so drop NotServed", ex.Path, rec.Code, ex.NotServed)
			case ex.NotServed == "" && !served:
				t.Errorf("%s: status %d: %s", ex.Path, rec.Code, rec.Body)
			}
		})
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
		{"argv_addr_json", dumpsDir + "netlink_route_getaddr.pcap", "/i/-j/a/s"},
		{"argv_route_text", dumpsDir + "netlink_route_getroute.pcap", "/ip/route/show"},
		{"argv_route_table", dumpsDir + "netlink_route_getroute.pcap", "/ip/route/show/table/all"},
		{"argv_write_verb", dumpsDir + "netlink_route_getlink.pcap", "/i/l/s"},
		{"argv_bad_token", dumpsDir + "netlink_route_getlink.pcap", "/ip/link/show/dev/a%00"},
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

	// A refused command line costs no more than a refused query and opens
	// nothing (reject_allocs).
	for _, tc := range []struct {
		name, category, description, target string
		expected                            float64
	}{
		{"argv_write_verb", "negative", "/i/l/s is ip link set", "/i/l/s", 40},
		{"argv_refused_option", "negative", "-netns is refused", "/ip/-netns/x/link/show", 40},
		{"argv_bad_token", "negative", "a %00 is refused by the tokenizer", "/ip/link/show/dev/a%00", 40},
		{"argv_query_on_command", "corner", "a command line with a query string", "/ip/link/show?dev=lo", 40},
		{"argv_selector_gap", "negative", "a selector goip does not parse, refused by the dry run", "/ip/route/show/scope/link", 40},
		{"argv_bad_prefix", "negative", "a /33 behind %2F, ip's get_prefix error from the dry run", "/ip/route/show/10.0.0.0%2F33", 45},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tc.target, nil)
			before := c.opens.Load()
			allocs := testing.AllocsPerRun(100, func() {
				mux.ServeHTTP(httptest.NewRecorder(), req)
			})
			t.Logf("%s: %s: %.0f allocs", tc.category, tc.description, allocs)
			if allocs > tc.expected {
				t.Errorf("%s: %s: %.0f allocs, want <= %.0f", tc.category, tc.description, allocs, tc.expected)
			}
			if n := c.opens.Load() - before; n != 0 {
				t.Errorf("%s: %s: opened %d sources", tc.category, tc.description, n)
			}
		})
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
		{"get_argv", "positive", "GET on a command line is served", http.MethodGet, "/i/l/sh", http.StatusOK},
		{"head_argv", "positive", "HEAD on a command line is served", http.MethodHead, "/ip/link/show", http.StatusOK},
		{"put_argv", "negative", "PUT on a command line is refused", http.MethodPut, "/ip/link/show", http.StatusMethodNotAllowed},
		{"delete_write_verb", "corner", "DELETE on a write-verb path is 405 before the verb is read", http.MethodDelete, "/i/l/s", http.StatusMethodNotAllowed},
		{"options_short_index", "boundary", "OPTIONS on /i is refused", http.MethodOptions, "/i", http.StatusMethodNotAllowed},
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

// TestRefusedBeforeSource covers the dry run: only an error goip.Show
// returns before sending anything is taken, so a selector that needs the
// kernel always gets the real run.
func TestRefusedBeforeSource(t *testing.T) {
	route := func(sel ...string) goip.ShowRequest { return goip.ShowRequest{Object: "route", Selectors: sel} }
	for _, tc := range []struct {
		name, category, description string
		req                         goip.ShowRequest
		expected                    string // a substring of the error, or "" for nil
	}{
		{"no_selectors", "positive", "nothing to refuse, so no dry run", route(), ""},
		{"accepted_selector", "positive", "a valid prefix reaches the source, so the real run decides", route("10.0.0.0/8"), ""},
		{"needs_kernel", "corner", "dev NAME is a lookup, which the probe answers and the real run repeats", goip.ShowRequest{Object: "address", Selectors: []string{"dev", "eth0"}}, ""},
		{"selector_gap", "negative", "scope is refused by name before any message", route("scope", "link"), `route show "scope"`},
		{"bad_prefix", "negative", "get_prefix's error before any message", route("10.0.0.0/33"), `any valid prefix is expected rather than "10.0.0.0/33"`},
		{"bad_table", "negative", "a table that is no id", route("table", "nosuchtable"), "table"},
		{"link_keyword", "boundary", "up is a keyword of link show's loop, not a device", goip.ShowRequest{Object: "link", Selectors: []string{"up"}}, `link show "up"`},
	} {
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			err := refusedBeforeSource(tc.req)
			switch {
			case tc.expected == "" && err != nil:
				t.Errorf("%s: %v, want nil", tc.description, err)
			case tc.expected != "" && (err == nil || !strings.Contains(err.Error(), tc.expected)):
				t.Errorf("%s: %v, want an error containing %q", tc.description, err, tc.expected)
			case errors.Is(err, errProbe):
				t.Errorf("%s: the probe's own error escaped: %v", tc.description, err)
			}
		})
	}
}

// BenchmarkRefusedBeforeSource is what the dry run adds to a served command
// line with selectors; one without selectors pays nothing.
//
// go test ./internal/goip/goiphttp/ -run x -bench RefusedBeforeSource -benchmem
func BenchmarkRefusedBeforeSource(b *testing.B) {
	for _, bc := range []struct {
		name string
		req  goip.ShowRequest
	}{
		{"none", goip.ShowRequest{Object: "route"}},
		{"route_table", goip.ShowRequest{Object: "route", Selectors: []string{"table", "all"}}},
		{"route_prefix", goip.ShowRequest{Object: "route", Selectors: []string{"10.0.0.0/8"}}},
		{"addr_dev", goip.ShowRequest{Object: "address", Selectors: []string{"dev", "eth0"}}},
		{"link_dev", goip.ShowRequest{Object: "link", Selectors: []string{"eth0"}}},
	} {
		b.Run(bc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = refusedBeforeSource(bc.req)
			}
		})
	}
}
