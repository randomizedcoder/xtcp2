package goip

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/internal/goip/model"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// The route captures are the gated microVM's, one command each, taken with no
// other netlink traffic on the host:
//
//	netlink_route_getroute.pcap            `ip route show`
//	netlink_route_getroute6.pcap           `ip -6 route show`
//	netlink_route_getroute_table_all.pcap  `ip route show table all`
//	mesh/…                                 the same three on the linkdown topology
//
// # Why these replays set GOIP_REPLAY and deliberately NOT GOIP_REPLAY_PORTID
//
// A route capture is not one conversation. `ip route show` runs its dump on the
// main socket and then opens a FRESH socket per lazy name lookup
// (ll_link_get, lib/ll_map.c:280), so the RTM_NEWLINK replies carry a different
// portid from the RTM_NEWROUTE replies — in netlink_route_getroute.pcap, 2603126454
// against the dump's 812. ReplaySource.Dump filters on a single portid, so
// setting one to the dump's value would hide every RTM_NEWLINK reply and every
// `dev` token with it. Leaving it unset replays the whole file, which is
// correct here precisely because the capture is single-command clean.
//
// That is the opposite of the neigh and bulk-addr captures, which were taken on
// a busy desktop and NEED the portid filter. The difference is a property of
// the capture, not a preference.
const (
	routeDumpPcap     = "../../pkg/xtcpnl/testdata/7_1_4/dumps/netlink_route_getroute.pcap"
	routeDump6Pcap    = "../../pkg/xtcpnl/testdata/7_1_4/dumps/netlink_route_getroute6.pcap"
	routeDumpAllPcap  = "../../pkg/xtcpnl/testdata/7_1_4/dumps/netlink_route_getroute_table_all.pcap"
	routeMeshPcap     = "../../pkg/xtcpnl/testdata/7_1_4/dumps/mesh/netlink_route_getroute.pcap"
	routeMesh6Pcap    = "../../pkg/xtcpnl/testdata/7_1_4/dumps/mesh/netlink_route_getroute6.pcap"
	routeMeshAllPcap  = "../../pkg/xtcpnl/testdata/7_1_4/dumps/mesh/netlink_route_getroute_table_all.pcap"
	routeSidecarDir   = "../../pkg/xtcpnl/testdata/7_1_4/dumps/"
	routeMeshSidecars = "../../pkg/xtcpnl/testdata/7_1_4/dumps/mesh/"
)

// TestRouteShowMatchesCapturedSidecars replays each committed route dump and
// compares goip's stdout with the `ip route show` sidecar captured beside it.
//
// The comparison target is the plain sidecar, never the `_n` one: `_n` in this
// corpus means `ip -d` (nix/microvms/mkVm.nix writes it with `ip -d route
// show`), which adds `unicast`, `table main` and `scope global` to every line,
// and goip has no -d surface to compare against.
//
// go test ./internal/goip/ -run TestRouteShowMatchesCapturedSidecars
func TestRouteShowMatchesCapturedSidecars(t *testing.T) {
	tests := []struct {
		description string
		pcap        string
		args        []string
		sidecar     string
		// jsonEquivalent compares decoded JSON instead of raw bytes, because
		// `ip -j -p` pretty-prints and goip emits one compact line.
		jsonEquivalent bool
	}{
		{
			description: "positive: `route show` reproduces ip_route_main line for line, trailing spaces included",
			pcap:        routeDumpPcap,
			args:        []string{"route", "show"},
			sidecar:     "ip_route_main",
		},
		{
			// do_iproute falls through to a list when argc is 0
			// (ip/iproute.c:2420), so a bare object must not be a usage error.
			description: "positive: a bare `route` is a list",
			pcap:        routeDumpPcap,
			args:        []string{"route"},
			sidecar:     "ip_route_main",
		},
		{
			description: "positive: `-6 route show` reproduces ip_route6, whose lines end flush at `pref medium`",
			pcap:        routeDump6Pcap,
			args:        []string{"-6", "route", "show"},
			sidecar:     "ip_route6",
		},
		{
			// The only form that prints a `table` token, and the only one that
			// reaches a second device — `lo` — and so a second single-get.
			description: "positive: `route show table all` reproduces ip_route_table_all, across both families and two tables",
			pcap:        routeDumpAllPcap,
			args:        []string{"route", "show", "table", "all"},
			sidecar:     "ip_route_table_all",
		},
		{
			description:    "positive: `-json route show` reproduces ip_route_main_json's keys and values",
			pcap:           routeDumpPcap,
			args:           []string{"-json", "route", "show"},
			sidecar:        "ip_route_main_json",
			jsonEquivalent: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			want, err := os.ReadFile(routeSidecarDir + tc.sidecar)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("GOIP_REPLAY", tc.pcap)
			var stdout, stderr bytes.Buffer
			if code := Run(tc.args, &stdout, &stderr); code != ExitOK {
				t.Fatalf("Run(%q) = %d, stderr=%s", tc.args, code, stderr.String())
			}
			if tc.jsonEquivalent {
				assertJSONEntriesEqual(t, stdout.Bytes(), want)
				return
			}
			if !bytes.Equal(stdout.Bytes(), want) {
				t.Fatalf("output mismatch\n got: %q\nwant: %q", stdout.Bytes(), want)
			}
		})
	}
}

// TestRouteShowMatchesMeshSidecars is the same comparison against the second
// topology, which exists for one reason: it is the only one whose carrier is
// down, so it is the only place `linkdown` appears in a golden.
//
// go test ./internal/goip/ -run TestRouteShowMatchesMeshSidecars
func TestRouteShowMatchesMeshSidecars(t *testing.T) {
	tests := []struct {
		description string
		pcap        string
		args        []string
		sidecar     string
	}{
		{
			description: "positive: mesh `route show` reproduces the linkdown token on both v4 routes",
			pcap:        routeMeshPcap,
			args:        []string{"route", "show"},
			sidecar:     "ip_route_main",
		},
		{
			// The ordering proof that a unit table cannot give: print_rt_flags
			// runs before print_rt_pref, so the real `ip` wrote `metric 256
			// linkdown pref medium` and goip has to as well.
			description: "positive: mesh `-6 route show` puts linkdown before pref",
			pcap:        routeMesh6Pcap,
			args:        []string{"-6", "route", "show"},
			sidecar:     "ip_route6",
		},
		{
			description: "positive: mesh `route show table all` reproduces both families and the local table",
			pcap:        routeMeshAllPcap,
			args:        []string{"route", "show", "table", "all"},
			sidecar:     "ip_route_table_all",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			want, err := os.ReadFile(routeMeshSidecars + tc.sidecar)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("GOIP_REPLAY", tc.pcap)
			var stdout, stderr bytes.Buffer
			if code := Run(tc.args, &stdout, &stderr); code != ExitOK {
				t.Fatalf("Run(%q) = %d, stderr=%s", tc.args, code, stderr.String())
			}
			if !bytes.Equal(stdout.Bytes(), want) {
				t.Fatalf("output mismatch\n got: %q\nwant: %q", stdout.Bytes(), want)
			}
		})
	}
}

// routeReplay is a ReplaySource that also implements TalkSource, and records
// every request it is handed.
//
// Recording is the whole point. `route show`'s transaction shape — one dump
// plus one lazy RTM_GETLINK single-get per DISTINCT ifindex — is invisible in
// stdout: a run that re-resolved the same device six times would print exactly
// the same six lines. The parity harness catches it on the wire, and this type
// is how a unit test catches it without a microVM.
//
// ReplaySource alone would not do, because it has no Talk method, so
// service.LinkByIndex silently falls back to a dump-and-filter
// (service.go:121) — the very shortcut obj_route.go exists to avoid. Supplying
// Talk here is what puts the code under test on the single-get path.
type routeReplay struct {
	inner *ReplaySource

	// dumps counts Dump calls and gets records the ifi_index of each
	// single-get, in the order they were sent.
	dumps int
	gets  []int32

	// unresolvable names indexes the replayed kernel answers nothing for, so a
	// test can exercise ll_index_to_name's does-not-cache-a-failure path.
	unresolvable map[int32]bool
}

func newRouteReplay(t *testing.T, path string) *routeReplay {
	t.Helper()
	s, err := OpenReplay(path)
	if err != nil {
		t.Fatalf("OpenReplay(%s): %v", path, err)
	}
	return &routeReplay{inner: s, unresolvable: map[int32]bool{}}
}

func (s *routeReplay) Dump(request []byte, msgType uint16) ([][]byte, error) {
	s.dumps++
	return s.inner.Dump(request, msgType)
}

// Talk answers a single-get from the RTM_NEWLINK replies the capture recorded,
// selecting on the ifi_index the request carries. That is a faithful replay:
// the recorded replies ARE the answers `ip` got to the same gets.
func (s *routeReplay) Talk(request []byte, msgType uint16) ([]byte, error) {
	var ifi xtcpnl.IfInfomsg
	if _, err := xtcpnl.DeserializeIfInfomsg(request[xtcpnl.NlMsgHdrSizeCst:], &ifi); err != nil {
		return nil, err
	}
	s.gets = append(s.gets, ifi.Index)
	if s.unresolvable[ifi.Index] {
		return nil, fmt.Errorf("%w: index %d", ErrNoReplay, ifi.Index)
	}
	for _, m := range s.inner.cap.Msgs() {
		if m.IsRequest() || m.Hdr.Type != msgType {
			continue
		}
		li, err := xtcpnl.ParseNewLink(m.Body)
		if err != nil {
			continue
		}
		if li.Index == ifi.Index {
			return xtcpnl.CopyBytes(m.Body), nil
		}
	}
	return nil, fmt.Errorf("%w: RTM_GETLINK index %d", ErrNoReplay, ifi.Index)
}

// runRouteWith drives runRoute over a source directly, bypassing Run so the
// test can supply a TalkSource and read the counters back afterwards.
func runRouteWith(t *testing.T, src Source, family uint8, args []string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	c := &runCtx{
		src:    src,
		lltab:  NewLLTab(),
		out:    &out,
		errOut: io.Discard,
		family: family,
	}
	err := runRoute(c, args)
	return out.String(), err
}

// TestRouteShowTransactionShape is the assertion stdout cannot make: how many
// netlink transactions `route show` produces, and for which indexes.
//
// `ip route show` sends NO up-front link dump — iproute_list_flush_or_save
// (ip/iproute.c:1818) never calls ll_init_map — and resolves each name while
// printing, with one RTM_GETLINK single-get per index the cache has never seen
// (lib/ll_map.c:320). An implementation that dumped links instead would print
// identical output and fail the harness's L2 request equality, which is the
// divergence this table exists to catch early.
//
// go test ./internal/goip/ -run TestRouteShowTransactionShape
func TestRouteShowTransactionShape(t *testing.T) {
	tests := []struct {
		description  string
		pcap         string
		family       uint8
		args         []string
		unresolvable []int32
		wantDumps    int
		wantGets     []int32
		wantStdout   string // when non-empty, the exact expected output
	}{
		{
			// Six routes, every one of them on ifindex 3, and exactly one get.
			// This is the lltab cache assertion: routes 2 through 6 must send
			// nothing, and the ECMP route's two nexthops — which also name
			// index 3 — must not send anything either.
			description: "positive: six routes on one device produce one dump and exactly one single-get",
			pcap:        routeDumpPcap,
			family:      unix.AF_UNSPEC,
			args:        []string{"show"},
			wantDumps:   1,
			wantGets:    []int32{3},
		},
		{
			// `table all` reaches the loopback routes too, so a SECOND distinct
			// index appears — and it appears after index 3, because resolution
			// walks routes in dump order. Two gets is exactly what the capture
			// recorded: an 816-byte reply for goip0 and a 796-byte one for lo.
			description: "positive: `table all` resolves a second device, in dump order, for two single-gets",
			pcap:        routeDumpAllPcap,
			family:      unix.AF_UNSPEC,
			args:        []string{"show", "table", "all"},
			wantDumps:   1,
			wantGets:    []int32{3, 1},
		},
		{
			description: "positive: the v6 dump names one device and sends one single-get",
			pcap:        routeDump6Pcap,
			family:      unix.AF_INET6,
			args:        []string{"show"},
			wantDumps:   1,
			wantGets:    []int32{3},
		},
		{
			// ll_index_to_name caches NOTHING when the get fails
			// (lib/ll_map.c:321-327), so the next reference retries. Seven
			// references to index 3 — five RTA_OIFs plus the ECMP route's two
			// nexthops — therefore send seven gets, and every device renders
			// as the `if%u` fallback. Reproducing the retry matters more than
			// saving it: the retry is what a parity capture would show.
			description:  "negative: a single-get that resolves nothing is not cached, so every reference retries",
			pcap:         routeDumpPcap,
			family:       unix.AF_UNSPEC,
			args:         []string{"show"},
			unresolvable: []int32{3},
			wantDumps:    1,
			wantGets:     []int32{3, 3, 3, 3, 3, 3, 3},
			wantStdout: "192.0.2.0/24 dev if3 proto kernel scope link src 192.0.2.1 \n" +
				"198.18.0.0/24 via 192.0.2.10 dev if3 \n" +
				"198.18.1.0/24 via 192.0.2.10 dev if3 mtu 1400 advmss 1300 \n" +
				"198.18.2.0/24 via inet6 2001:db8::2 dev if3 \n" +
				"198.51.100.0/24 dev if3 scope link \n" +
				"203.0.113.0/24 " +
				"\n\tnexthop via 192.0.2.10 dev if3 weight 1 " +
				"\n\tnexthop via 192.0.2.11 dev if3 weight 3 \n",
		},
		{
			// The loopback routes still print, `lo` still resolves, and only
			// the goip0 references retry — a failed lookup must not poison the
			// whole listing.
			description:  "corner: one unresolvable index does not stop the other from resolving",
			pcap:         routeDumpAllPcap,
			family:       unix.AF_UNSPEC,
			args:         []string{"show", "table", "all"},
			unresolvable: []int32{3},
			wantDumps:    1,
			// Five v4 RTA_OIFs on index 3 plus the ECMP route's two nexthops,
			// then lo — which resolves, and so is asked for ONCE even though
			// three loopback routes name it — then the remaining eleven
			// references to index 3, each of which retries.
			wantGets: []int32{
				3, 3, 3, 3, 3, 3, 3,
				1,
				3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3,
			},
		},
		{
			description: "boundary: `route list` produces the same transactions as `route show`",
			pcap:        routeDumpPcap,
			family:      unix.AF_UNSPEC,
			args:        []string{"list"},
			wantDumps:   1,
			wantGets:    []int32{3},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := newRouteReplay(t, tc.pcap)
			for _, idx := range tc.unresolvable {
				src.unresolvable[idx] = true
			}
			got, err := runRouteWith(t, src, tc.family, tc.args)
			if err != nil {
				t.Fatalf("runRoute(%q): %v", tc.args, err)
			}
			if src.dumps != tc.wantDumps {
				t.Errorf("dumps = %d, want %d", src.dumps, tc.wantDumps)
			}
			if !equalIndexes(src.gets, tc.wantGets) {
				t.Errorf("single-gets = %v, want %v", src.gets, tc.wantGets)
			}
			if tc.wantStdout != "" && got != tc.wantStdout {
				t.Errorf("stdout = %q\nwant     %q", got, tc.wantStdout)
			}
		})
	}
}

func equalIndexes(got, want []int32) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// TestRunRouteArgs covers the CLI surface: the verbs runRoute accepts and the
// selectors it refuses rather than silently ignores. Ignoring a selector would
// be worse than refusing it — the parity harness drives `ip` and `goip` with
// the same argv, so a dropped filter has the two tools answering different
// questions while the comparator reports a stdout divergence it cannot explain.
//
// go test ./internal/goip/ -run TestRunRouteArgs
func TestRunRouteArgs(t *testing.T) {
	const firstLine = "192.0.2.0/24 dev goip0 "

	tests := []struct {
		description      string
		pcap             string
		args             []string
		wantCode         int
		wantStdoutPrefix string
		wantStderrSubstr string
	}{
		{
			description:      "positive: `route show` renders the first captured route",
			pcap:             routeDumpPcap,
			args:             []string{"route", "show"},
			wantCode:         ExitOK,
			wantStdoutPrefix: firstLine,
		},
		{
			// `r` reaches route rather than rule only because route precedes
			// rule in the object table; see dispatch.go's comment. The row is
			// here because deleting the unimplemented `rule` entry would break
			// it silently.
			description:      "positive: `r` abbreviates to route, not rule",
			pcap:             routeDumpPcap,
			args:             []string{"r", "s"},
			wantCode:         ExitOK,
			wantStdoutPrefix: firstLine,
		},
		{
			// do_iproute tests the list group before "save", so a bare `s` is
			// show. `sa` is NOT show — it is save — and is covered below.
			description:      "boundary: `route l` and `route lst` are both the same listing",
			pcap:             routeDumpPcap,
			args:             []string{"route", "lst"},
			wantCode:         ExitOK,
			wantStdoutPrefix: firstLine,
		},
		{
			description:      "boundary: `route ls` matches lst, the third spelling of list",
			pcap:             routeDumpPcap,
			args:             []string{"route", "ls"},
			wantCode:         ExitOK,
			wantStdoutPrefix: firstLine,
		},
		{
			// "sa" is not a prefix of "show", so matches() sends it to save —
			// which goip does not implement. Accepting it as a show would make
			// goip answer a question `ip` would answer differently.
			description:      "negative: `route sa` is save, not show, and is refused",
			pcap:             routeDumpPcap,
			args:             []string{"route", "sa"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			// A write verb must stay unreachable: goip is read-only, and
			// `flush` reaching a handler at all would be the wrong kind of bug.
			description:      "negative: the write verb `flush` is refused",
			pcap:             routeDumpPcap,
			args:             []string{"route", "flush"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			// The selector exists in `ip` and not here. Refusing it names the
			// gap; ignoring it would list every route and look like success.
			description:      "negative: `route show dev goip0` is refused, not ignored",
			pcap:             routeDumpPcap,
			args:             []string{"route", "show", "dev", "goip0"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			description:      "negative: `route show proto kernel` is refused, not ignored",
			pcap:             routeDumpPcap,
			args:             []string{"route", "show", "proto", "kernel"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			// A selector with no verb is an error in `ip` too: do_iproute
			// matches "table" against no verb and exits. goip collapses that
			// into ErrNotImplemented, which is the outcome the harness acts on.
			description:      "corner: `route table all` with no verb is refused, as it is in ip",
			pcap:             routeDumpAllPcap,
			args:             []string{"route", "table", "all"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			// NEXT_ARG() exits with a usage error when the keyword is last.
			description:      "negative: `route show table` with no id is a usage error",
			pcap:             routeDumpPcap,
			args:             []string{"route", "show", "table"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "argument expected",
		},
		{
			// Options precede the object in iproute2, so this is a selector
			// named "-4", not a family option.
			description:      "corner: an option after the object is not an option",
			pcap:             routeDumpPcap,
			args:             []string{"route", "show", "-4"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Setenv("GOIP_REPLAY", tc.pcap)
			var stdout, stderr bytes.Buffer
			code := Run(tc.args, &stdout, &stderr)
			if code != tc.wantCode {
				t.Errorf("Run(%q) = %d, want %d; stderr: %s", tc.args, code, tc.wantCode, stderr.String())
			}
			if tc.wantStdoutPrefix != "" && !strings.HasPrefix(stdout.String(), tc.wantStdoutPrefix) {
				t.Errorf("stdout = %q, want prefix %q", stdout.String(), tc.wantStdoutPrefix)
			}
			if tc.wantStderrSubstr != "" && !strings.Contains(stderr.String(), tc.wantStderrSubstr) {
				t.Errorf("stderr = %q, want to contain %q", stderr.String(), tc.wantStderrSubstr)
			}
		})
	}
}

// TestParseRouteShowArgs pins filter.tb, which is not cosmetic: it decides both
// the RTA_TABLE on the wire and whether every rendered line carries a `table`
// token.
//
// go test ./internal/goip/ -run TestParseRouteShowArgs
func TestParseRouteShowArgs(t *testing.T) {
	tests := []struct {
		description string
		args        []string
		want        uint32
		wantErr     bool
		// wantErrIs, when set, is the sentinel errors.Is must find. Asserting
		// only that "an error happened" would let a refused keyword and a
		// malformed id collapse into one another, and they are different
		// mistakes: routeTableID wraps strconv's ErrSyntax and ErrRange
		// precisely so a caller can tell `table wombat` from `table 2^32`.
		wantErrIs error
	}{
		{
			// ip/iproute.c:1835 assigns RT_TABLE_MAIN before reading any
			// argument, which is why no line of ip_route_main has a table token.
			description: "positive: no selectors leaves filter.tb at RT_TABLE_MAIN",
			args:        nil,
			want:        unix.RT_TABLE_MAIN,
		},
		{
			description: "positive: `table main` is the same as the default",
			args:        []string{"table", "main"},
			want:        unix.RT_TABLE_MAIN,
		},
		{
			// "all" is not a table name: rtnl_rttable_a2n fails on it and the
			// fallback at ip/iproute.c:1850 sets filter.tb to 0.
			description: "positive: `table all` clears the filter to 0",
			args:        []string{"table", "all"},
			want:        unix.RT_TABLE_UNSPEC,
		},
		{
			// The two spellings reach 0 by different routes — "0" parses as an
			// id, "all" does not — and must land in the same place.
			description: "boundary: `table 0` is spelled differently but means `table all`",
			args:        []string{"table", "0"},
			want:        unix.RT_TABLE_UNSPEC,
		},
		{
			description: "positive: `table local` resolves through the built-in rt_tables hash",
			args:        []string{"table", "local"},
			want:        unix.RT_TABLE_LOCAL,
		},
		{
			description: "positive: `table default` is the third and last built-in name",
			args:        []string{"table", "default"},
			want:        unix.RT_TABLE_DEFAULT,
		},
		{
			description: "boundary: table 255 is the largest id that fits the 8-bit rtm_table field",
			args:        []string{"table", "255"},
			want:        255,
		},
		{
			// Beyond 255 the id no longer fits rtm_table, which is exactly why
			// the request carries RTA_TABLE. See BuildDumpRouteRequestTable.
			description: "boundary: table 256 is the first id that needs RTA_TABLE to be expressed",
			args:        []string{"table", "256"},
			want:        256,
		},
		{
			description: "corner: RT_TABLE_MAX, 4294967295, is a legal table id",
			args:        []string{"table", "4294967295"},
			want:        0xFFFFFFFF,
		},
		{
			// strtoul(arg, &end, 0) — base 0, so the 0x form is accepted.
			description: "corner: a 0x-prefixed id is accepted, because strtoul uses base 0",
			args:        []string{"table", "0xff"},
			want:        255,
		},
		{
			// iproute2 simply assigns filter.tb again (ip/iproute.c:1843-1858),
			// so repeating the selector is last-wins — not an error and not an
			// intersection. Worth pinning rather than leaving unspecified.
			description: "corner: a repeated `table` selector is last-wins",
			args:        []string{"table", "all", "table", "main"},
			want:        unix.RT_TABLE_MAIN,
		},
		{
			description: "corner: last-wins in the other direction too",
			args:        []string{"table", "main", "table", "all"},
			want:        unix.RT_TABLE_UNSPEC,
		},
		{
			description: "negative: `table` with no id is a usage error",
			args:        []string{"table"},
			wantErr:     true,
			wantErrIs:   ErrNotImplemented,
		},
		{
			// `route show cache` is a real iproute2 form that lists cloned
			// routes. goip has none, so it is refused by name rather than being
			// read as a malformed id.
			description: "negative: `table cache` is refused by name, not misread as an id",
			args:        []string{"table", "cache"},
			wantErr:     true,
			wantErrIs:   ErrNotImplemented,
		},
		{
			description: "negative: `table help` is refused by name",
			args:        []string{"table", "help"},
			wantErr:     true,
			wantErrIs:   ErrNotImplemented,
		},
		{
			description: "negative: a table name with no built-in entry is an error, since no rt_tables file is read",
			args:        []string{"table", "mgmt"},
			wantErr:     true,
			wantErrIs:   strconv.ErrSyntax,
		},
		{
			description: "negative: an id past 32 bits does not fit rtm_table or RTA_TABLE",
			args:        []string{"table", "4294967296"},
			wantErr:     true,
			wantErrIs:   strconv.ErrRange,
		},
		{
			description: "negative: an unimplemented selector is refused rather than skipped",
			args:        []string{"dev", "goip0"},
			wantErr:     true,
			wantErrIs:   ErrNotImplemented,
		},
		{
			// `tab`, `tabl` and `t` all reach "table" through matches().
			description: "boundary: the table keyword abbreviates, as every iproute2 keyword does",
			args:        []string{"t", "local"},
			want:        unix.RT_TABLE_LOCAL,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := parseRouteShowArgs(tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseRouteShowArgs(%q) = %d, want an error", tc.args, got)
				}
				if tc.wantErrIs != nil && !errors.Is(err, tc.wantErrIs) {
					t.Fatalf("parseRouteShowArgs(%q) error = %v, want errors.Is(_, %v)",
						tc.args, err, tc.wantErrIs)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseRouteShowArgs(%q): %v", tc.args, err)
			}
			if got != tc.want {
				t.Errorf("parseRouteShowArgs(%q) = %d, want %d", tc.args, got, tc.want)
			}
		})
	}
}

// TestRouteFilter covers the three rules of filter_nlmsg that a `show` with no
// selector other than `table` still applies. They are not redundant with the
// request: the kernel answers a table-filtered dump on a best-effort basis and
// `ip` re-checks every reply.
//
// go test ./internal/goip/ -run TestRouteFilter
func TestRouteFilter(t *testing.T) {
	v4Main := model.Route{Family: unix.AF_INET, Table: unix.RT_TABLE_MAIN, Type: unix.RTN_UNICAST, DstLen: 24}
	v4Local := model.Route{Family: unix.AF_INET, Table: unix.RT_TABLE_LOCAL, Type: unix.RTN_LOCAL, DstLen: 32}
	v6Main := model.Route{Family: unix.AF_INET6, Table: unix.RT_TABLE_MAIN, Type: unix.RTN_UNICAST, DstLen: 64}
	v6Local := model.Route{Family: unix.AF_INET6, Table: unix.RT_TABLE_LOCAL, Type: unix.RTN_LOCAL, DstLen: 128}

	tests := []struct {
		description     string
		in              []model.Route
		preferredFamily uint8
		table           uint32
		wantLen         int
	}{
		{
			description:     "positive: `route show` keeps the main-table v4 routes",
			in:              []model.Route{v4Main, v4Main},
			preferredFamily: unix.AF_UNSPEC,
			table:           unix.RT_TABLE_MAIN,
			wantLen:         2,
		},
		{
			// The client-side family filter is preferred_family — the
			// UNPROMOTED one — so a plain `route show` (AF_UNSPEC) drops
			// nothing here even though its request asked for AF_INET.
			description:     "boundary: an AF_UNSPEC preferred family drops nothing, whatever the request asked for",
			in:              []model.Route{v4Main, v6Main},
			preferredFamily: unix.AF_UNSPEC,
			table:           unix.RT_TABLE_UNSPEC,
			wantLen:         2,
		},
		{
			description:     "positive: `-6` drops every v4 reply",
			in:              []model.Route{v4Main, v6Main, v4Main},
			preferredFamily: unix.AF_INET6,
			table:           unix.RT_TABLE_MAIN,
			wantLen:         1,
		},
		{
			description:     "positive: `-4` drops every v6 reply",
			in:              []model.Route{v4Main, v6Main},
			preferredFamily: unix.AF_INET,
			table:           unix.RT_TABLE_MAIN,
			wantLen:         1,
		},
		{
			// `filter.cloned == !(rtm_flags & RTM_F_CLONED)` reads backwards:
			// with filter.cloned zero it is TRUE exactly when the route IS
			// cloned, so a cloned route is dropped. `route show cache` is the
			// inverse, and goip refuses it.
			description: "negative: a cloned route is dropped, which is the inverse of `route show cache`",
			in: []model.Route{
				v4Main,
				{Family: unix.AF_INET, Table: unix.RT_TABLE_MAIN, Type: unix.RTN_UNICAST, Flags: unix.RTM_F_CLONED},
			},
			preferredFamily: unix.AF_UNSPEC,
			table:           unix.RT_TABLE_MAIN,
			wantLen:         1,
		},
		{
			description:     "positive: a table filter drops the routes of every other table",
			in:              []model.Route{v4Main, v4Local},
			preferredFamily: unix.AF_UNSPEC,
			table:           unix.RT_TABLE_MAIN,
			wantLen:         1,
		},
		{
			description:     "boundary: `table all` keeps both tables and both families",
			in:              []model.Route{v4Main, v4Local, v6Main, v6Local},
			preferredFamily: unix.AF_UNSPEC,
			table:           unix.RT_TABLE_UNSPEC,
			wantLen:         4,
		},
		{
			// Before ip6_multiple_tables latches, `ip` emulates the v6 tables
			// by route TYPE, because on a kernel without
			// CONFIG_IPV6_MULTIPLE_TABLES they do not exist: `table main`
			// means "not RTN_LOCAL" and `table local` means "RTN_LOCAL".
			// v6Local is RTN_LOCAL, so a v6 `table main` drops it.
			description:     "corner: before the latch, a v6 `table main` filter is emulated by route type",
			in:              []model.Route{v6Local},
			preferredFamily: unix.AF_INET6,
			table:           unix.RT_TABLE_MAIN,
			wantLen:         0,
		},
		{
			description:     "corner: before the latch, a v6 `table local` filter keeps only RTN_LOCAL",
			in:              []model.Route{v6Main, v6Local},
			preferredFamily: unix.AF_INET6,
			table:           unix.RT_TABLE_LOCAL,
			wantLen:         1,
		},
		{
			// The latch is set by the FIRST v6 route whose table is not main,
			// and it changes how every route AFTER it is filtered — so the same
			// two routes in the other order give a different answer. The
			// ordering dependence is real and is reproduced rather than tidied.
			description: "corner: the ip6_multiple_tables latch makes the filter order-dependent",
			in: []model.Route{
				v6Local, // latches, then is itself filtered by type
				v6Main,  // filtered by table id, and its table IS main
			},
			preferredFamily: unix.AF_INET6,
			table:           unix.RT_TABLE_MAIN,
			wantLen:         1,
		},
		{
			// Without CONFIG_IPV6_MULTIPLE_TABLES there is no table to emulate
			// beyond main and local, so `ip` refuses every other id outright
			// and the listing comes back empty rather than unfiltered.
			description:     "negative: before the latch, a v6 filter on any other table drops everything",
			in:              []model.Route{v6Main, v6Local},
			preferredFamily: unix.AF_INET6,
			table:           100,
			wantLen:         0,
		},
		{
			description:     "boundary: an empty dump filters to an empty listing, not to an error",
			in:              nil,
			preferredFamily: unix.AF_UNSPEC,
			table:           unix.RT_TABLE_MAIN,
			wantLen:         0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := routeFilter(tc.in, tc.preferredFamily, tc.table)
			if len(got) != tc.wantLen {
				t.Errorf("routeFilter() kept %d routes, want %d", len(got), tc.wantLen)
			}
		})
	}
}

// TestRouteShowFamilyPromotion pins ip/iproute.c:2000-2001, which is easy to
// mistake for the -4/-6 option and is not: it changes only what the REQUEST
// asks for, and only when a table filter is in force.
//
// The observable consequence is on the wire, so the assertion is on the request
// the source was handed rather than on stdout.
//
// go test ./internal/goip/ -run TestRouteShowFamilyPromotion
func TestRouteShowFamilyPromotion(t *testing.T) {
	tests := []struct {
		description string
		pcap        string
		family      uint8
		args        []string
		wantFamily  uint8
	}{
		{
			// AF_UNSPEC with filter.tb set is promoted to AF_INET, which is why
			// plain `ip route show` lists only IPv4 without ever being told to.
			description: "positive: a default `route show` promotes AF_UNSPEC to AF_INET in the request",
			pcap:        routeDumpPcap,
			family:      unix.AF_UNSPEC,
			args:        []string{"show"},
			wantFamily:  unix.AF_INET,
		},
		{
			// `table all` clears filter.tb, so the promotion's second condition
			// fails and the request stays AF_UNSPEC — which is how one command
			// returns both families.
			description: "boundary: `table all` clears filter.tb, so no promotion happens",
			pcap:        routeDumpAllPcap,
			family:      unix.AF_UNSPEC,
			args:        []string{"show", "table", "all"},
			wantFamily:  unix.AF_UNSPEC,
		},
		{
			// An explicit family is never promoted: the first condition is
			// `dump_family == AF_UNSPEC`.
			description: "negative: an explicit -6 is not promoted",
			pcap:        routeDump6Pcap,
			family:      unix.AF_INET6,
			args:        []string{"show"},
			wantFamily:  unix.AF_INET6,
		},
		{
			description: "corner: an explicit -4 with `table all` also stays as it was",
			pcap:        routeDumpAllPcap,
			family:      unix.AF_INET,
			args:        []string{"show", "table", "all"},
			wantFamily:  unix.AF_INET,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := &captureFirstRequest{inner: newRouteReplay(t, tc.pcap)}
			if _, err := runRouteWith(t, src, tc.family, tc.args); err != nil {
				t.Fatalf("runRoute(%q): %v", tc.args, err)
			}
			if src.first == nil {
				t.Fatal("no request was sent")
			}
			// rtm_family is the first byte of the rtmsg, which follows the
			// 16-byte nlmsghdr.
			var rtm xtcpnl.RtMsg
			if _, err := xtcpnl.DeserializeRtMsg(src.first[xtcpnl.NlMsgHdrSizeCst:], &rtm); err != nil {
				t.Fatalf("DeserializeRtMsg: %v", err)
			}
			if rtm.Family != tc.wantFamily {
				t.Errorf("request rtm_family = %d, want %d", rtm.Family, tc.wantFamily)
			}
		})
	}
}

// captureFirstRequest keeps the bytes of the first Dump request, which is the
// RTM_GETROUTE one — the lazy single-gets go through Talk.
type captureFirstRequest struct {
	inner *routeReplay
	first []byte
}

func (s *captureFirstRequest) Dump(request []byte, msgType uint16) ([][]byte, error) {
	if s.first == nil {
		s.first = xtcpnl.CopyBytes(request)
	}
	return s.inner.Dump(request, msgType)
}

func (s *captureFirstRequest) Talk(request []byte, msgType uint16) ([]byte, error) {
	return s.inner.Talk(request, msgType)
}
