package goip

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// routeGetCorpusDir is the C1-T02 `route get` rows, one subdirectory each with
// ip.{pcap,out,err,rc} (scripts/rtnl-write-corpus.exp).
const routeGetCorpusDir = "../../pkg/xtcpnl/testdata/7_1_4/link-write/rtnl"

func routeGetDst(s string) xtcpnl.RouteAttr {
	a := netip.MustParseAddr(s)
	return xtcpnl.RouteAttrBytes(uint16(unix.RTA_DST), a.AsSlice())
}

func routeGetSrc(s string) xtcpnl.RouteAttr {
	a := netip.MustParseAddr(s)
	return xtcpnl.RouteAttrBytes(uint16(unix.RTA_SRC), a.AsSlice())
}

// TestParseRouteGetArgs is iproute_get's argument loop: which arm each word
// takes in the chain's order, the attributes in argv order, and the errors it
// stops on before any request is sent.
//
// go test ./internal/goip/ -run TestParseRouteGetArgs
func TestParseRouteGetArgs(t *testing.T) {
	v4 := uint8(unix.AF_INET)
	tests := []struct {
		description string
		family      uint8
		args        string
		expected    routeGetState
		wantWarn    string
		wantErr     error
		wantErrText string
	}{
		{
			description: "positive: a bare destination is RTA_DST with dst_len 32",
			args:        "192.0.2.5",
			expected: routeGetState{addressFound: true, g: xtcpnl.RouteGet{
				Family: v4, DstLen: 32, Attrs: []xtcpnl.RouteAttr{routeGetDst("192.0.2.5")}}},
		},
		{
			description: "positive: `to` before the destination is the same request",
			args:        "to 192.0.2.5",
			expected: routeGetState{addressFound: true, g: xtcpnl.RouteGet{
				Family: v4, DstLen: 32, Attrs: []xtcpnl.RouteAttr{routeGetDst("192.0.2.5")}}},
		},
		{
			description: "positive: -6 keeps AF_INET6 and dst_len 128",
			family:      unix.AF_INET6,
			args:        "2001:db8::5",
			expected: routeGetState{addressFound: true, g: xtcpnl.RouteGet{
				Family: unix.AF_INET6, DstLen: 128, Attrs: []xtcpnl.RouteAttr{routeGetDst("2001:db8::5")}}},
		},
		{
			description: "positive: `from` before the destination puts RTA_SRC first, in argv order",
			args:        "from 198.51.100.2 192.0.2.5",
			expected: routeGetState{addressFound: true, g: xtcpnl.RouteGet{
				Family: v4, DstLen: 32, SrcLen: 32,
				Attrs: []xtcpnl.RouteAttr{routeGetSrc("198.51.100.2"), routeGetDst("192.0.2.5")}}},
		},
		{
			description: "positive: `from` after the destination puts RTA_SRC second",
			args:        "192.0.2.5 from 198.51.100.2",
			expected: routeGetState{addressFound: true, g: xtcpnl.RouteGet{
				Family: v4, DstLen: 32, SrcLen: 32,
				Attrs: []xtcpnl.RouteAttr{routeGetDst("192.0.2.5"), routeGetSrc("198.51.100.2")}}},
		},
		{
			description: "positive: `f` is from, which the chain tests before fibmatch",
			args:        "f 198.51.100.2 192.0.2.5",
			expected: routeGetState{addressFound: true, g: xtcpnl.RouteGet{
				Family: v4, DstLen: 32, SrcLen: 32,
				Attrs: []xtcpnl.RouteAttr{routeGetSrc("198.51.100.2"), routeGetDst("192.0.2.5")}}},
		},
		{
			description: "positive: `i d1` is iif, which the chain tests before ipproto",
			args:        "i d1 192.0.2.5",
			expected: routeGetState{idev: "d1", idevSet: true, addressFound: true, g: xtcpnl.RouteGet{
				Family: v4, DstLen: 32, Attrs: []xtcpnl.RouteAttr{routeGetDst("192.0.2.5")}}},
		},
		{
			description: "positive: `o` is oif and `dev` is its exact synonym",
			args:        "o d0 192.0.2.5 dev d1",
			expected: routeGetState{odev: "d1", odevSet: true, addressFound: true, g: xtcpnl.RouteGet{
				Family: v4, DstLen: 32, Attrs: []xtcpnl.RouteAttr{routeGetDst("192.0.2.5")}}},
		},
		{
			description: "positive: `fibmatch` and `n` (notify) set their flags",
			args:        "fibmatch 192.0.2.5 n",
			expected: routeGetState{fibMatch: true, addressFound: true, g: xtcpnl.RouteGet{
				Family: v4, DstLen: 32, RtmFlags: unix.RTM_F_NOTIFY,
				Attrs: []xtcpnl.RouteAttr{routeGetDst("192.0.2.5")}}},
		},
		{
			description: "positive: uid is RTA_UID where it is parsed; mark waits for the end",
			args:        "mark 0x10 uid 1000 192.0.2.5",
			expected: routeGetState{mark: 16, addressFound: true, g: xtcpnl.RouteGet{
				Family: v4, DstLen: 32,
				Attrs: []xtcpnl.RouteAttr{
					xtcpnl.RouteAttrU32(uint16(unix.RTA_UID), 1000), routeGetDst("192.0.2.5")}}},
		},
		{
			description: "boundary: a /24 destination warns and is sent as /32",
			args:        "192.0.2.0/24",
			wantWarn:    "Warning: /24 as prefix is invalid, only /32 (or none) is supported.\n",
			expected: routeGetState{addressFound: true, g: xtcpnl.RouteGet{
				Family: v4, DstLen: 32, Attrs: []xtcpnl.RouteAttr{routeGetDst("192.0.2.0")}}},
		},
		{
			description: "boundary: an explicit /32 does not warn",
			args:        "192.0.2.5/32",
			expected: routeGetState{addressFound: true, g: xtcpnl.RouteGet{
				Family: v4, DstLen: 32, Attrs: []xtcpnl.RouteAttr{routeGetDst("192.0.2.5")}}},
		},
		{
			description: "boundary: a v6 /64 warns with /128",
			family:      unix.AF_INET6,
			args:        "2001:db8::/64",
			wantWarn:    "Warning: /64 as prefix is invalid, only /128 (or none) is supported.\n",
			expected: routeGetState{addressFound: true, g: xtcpnl.RouteGet{
				Family: unix.AF_INET6, DstLen: 128, Attrs: []xtcpnl.RouteAttr{routeGetDst("2001:db8::")}}},
		},
		{
			description: "negative: no destination at all",
			args:        "",
			wantErr:     ErrInvalidArgument,
			wantErrText: "need at least a destination address",
		},
		{
			description: "negative: flags but no destination",
			args:        "fibmatch notify",
			wantErr:     ErrInvalidArgument,
			wantErrText: "need at least a destination address",
		},
		{
			description: "negative: `from` with no value",
			args:        "192.0.2.5 from",
			wantErr:     ErrInvalidArgument,
		},
		{
			description: "negative: a mark that is not a number",
			args:        "mark x 192.0.2.5",
			wantErr:     ErrInvalidArgument,
			wantErrText: "invalid mark value",
		},
		{
			description: "negative: a destination that is not an address",
			args:        "not-an-address",
			wantErr:     ErrInvalidArgument,
		},
		{
			description: "negative: `help` in the destination's place is refused as route help",
			args:        "help",
			wantErr:     ErrNotImplemented,
		},
		{
			description: "corner: `d` is dsfield, not dport, so it is refused as dsfield",
			args:        "d 0x10 192.0.2.5",
			wantErr:     ErrNotImplemented,
			wantErrText: `"dsfield"`,
		},
		{
			description: "corner: `tos` must be spelled out; it is refused by name",
			args:        "tos 0x10 192.0.2.5",
			wantErr:     ErrNotImplemented,
			wantErrText: `"tos"`,
		},
		{
			description: "corner: `connected` is refused before the destination is read",
			args:        "192.0.2.5 connected",
			wantErr:     ErrNotImplemented,
			wantErrText: `"connected"`,
		},
		{
			description: "corner: `ip` is ipproto, which is refused",
			args:        "ip tcp 192.0.2.5",
			wantErr:     ErrNotImplemented,
			wantErrText: `"ipproto"`,
		},
		{
			description: "corner: `vrf`, `as`, `sport` and `flowlabel` are refused by name",
			args:        "vrf red 192.0.2.5",
			wantErr:     ErrNotImplemented,
			wantErrText: `"vrf"`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var warn strings.Builder
			got, err := parseRouteGetArgs(tc.family, strings.Fields(tc.args), func(s string) { warn.WriteString(s) })
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if err != nil {
				if !strings.Contains(err.Error(), tc.wantErrText) {
					t.Errorf("err = %q, want it to contain %q", err, tc.wantErrText)
				}
				return
			}
			if warn.String() != tc.wantWarn {
				t.Errorf("warning = %q, want %q", warn.String(), tc.wantWarn)
			}
			if !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("state =\n  %+v\nwant\n  %+v", got, tc.expected)
			}
		})
	}
}

// TestRouteGetFinish is iproute_get after its loop: RTA_IIF, RTA_OIF and
// RTA_MARK appended in that order, the AF_INET default, and the flags.
//
// go test ./internal/goip/ -run TestRouteGetFinish
func TestRouteGetFinish(t *testing.T) {
	resolve := func(name string) (int32, error) {
		switch name {
		case "d0":
			return 3, nil
		case "d1":
			return 4, nil
		case "broken":
			return 0, errors.New("talk failed")
		}
		return 0, nil
	}
	dst := routeGetDst("192.0.2.5")
	tests := []struct {
		description string
		in          routeGetState
		expected    xtcpnl.RouteGet
		wantErr     error
	}{
		{
			description: "positive: AF_INET gets RTM_F_LOOKUP_TABLE",
			in:          routeGetState{g: xtcpnl.RouteGet{Family: unix.AF_INET, DstLen: 32, Attrs: []xtcpnl.RouteAttr{dst}}},
			expected: xtcpnl.RouteGet{Family: unix.AF_INET, DstLen: 32, RtmFlags: unix.RTM_F_LOOKUP_TABLE,
				Attrs: []xtcpnl.RouteAttr{dst}},
		},
		{
			description: "positive: iif, oif and mark follow the loop's attributes, in that order",
			in: routeGetState{idev: "d1", idevSet: true, odev: "d0", odevSet: true, mark: 7,
				g: xtcpnl.RouteGet{Family: unix.AF_INET, Attrs: []xtcpnl.RouteAttr{dst}}},
			expected: xtcpnl.RouteGet{Family: unix.AF_INET, RtmFlags: unix.RTM_F_LOOKUP_TABLE,
				Attrs: []xtcpnl.RouteAttr{dst,
					xtcpnl.RouteAttrU32(uint16(unix.RTA_IIF), 4),
					xtcpnl.RouteAttrU32(uint16(unix.RTA_OIF), 3),
					xtcpnl.RouteAttrU32(uint16(unix.RTA_MARK), 7)}},
		},
		{
			description: "positive: fibmatch adds RTM_F_FIB_MATCH beside the lookup-table flag",
			in:          routeGetState{fibMatch: true, g: xtcpnl.RouteGet{Family: unix.AF_INET}},
			expected:    xtcpnl.RouteGet{Family: unix.AF_INET, RtmFlags: unix.RTM_F_LOOKUP_TABLE | unix.RTM_F_FIB_MATCH},
		},
		{
			description: "boundary: AF_INET6 gets no lookup-table flag",
			in:          routeGetState{g: xtcpnl.RouteGet{Family: unix.AF_INET6}},
			expected:    xtcpnl.RouteGet{Family: unix.AF_INET6},
		},
		{
			description: "boundary: mark 0 sends no RTA_MARK",
			in:          routeGetState{g: xtcpnl.RouteGet{Family: unix.AF_INET}},
			expected:    xtcpnl.RouteGet{Family: unix.AF_INET, RtmFlags: unix.RTM_F_LOOKUP_TABLE},
		},
		{
			description: "corner: AF_UNSPEC becomes AF_INET, and so takes the lookup-table flag",
			in:          routeGetState{g: xtcpnl.RouteGet{}},
			expected:    xtcpnl.RouteGet{Family: unix.AF_INET, RtmFlags: unix.RTM_F_LOOKUP_TABLE},
		},
		{
			description: "negative: an unknown iif is no such device",
			in:          routeGetState{idev: "nosuch0", idevSet: true},
			wantErr:     ErrNoSuchDevice,
		},
		{
			description: "negative: an unknown oif is no such device, and an invalid argument (exit 1)",
			in:          routeGetState{odev: "nosuch0", odevSet: true},
			wantErr:     ErrInvalidArgument,
		},
		{
			description: "negative: a failed lookup is returned as it is",
			in:          routeGetState{odev: "broken", odevSet: true},
			wantErr:     errors.New("talk failed"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			st := tc.in
			err := st.finish(resolve)
			switch {
			case tc.wantErr == nil && err != nil:
				t.Fatalf("finish: %v", err)
			case tc.wantErr != nil:
				if err == nil || (!errors.Is(err, tc.wantErr) && err.Error() != tc.wantErr.Error()) {
					t.Fatalf("finish err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if !reflect.DeepEqual(st.g, tc.expected) {
				t.Errorf("request =\n  %+v\nwant\n  %+v", st.g, tc.expected)
			}
		})
	}
}

// routeGetReplay is a statsReplay that also answers a recorded request with
// the kernel's NLMSG_ERROR as a syscall.Errno, as TalkRtnetlink does, so the
// unreachable row fails the way a live get fails.
type routeGetReplay struct{ *statsReplay }

func (s routeGetReplay) Talk(request []byte, msgType uint16) ([]byte, error) {
	body, err := s.statsReplay.Talk(request, msgType)
	if !errors.Is(err, ErrNoReplay) {
		return body, err
	}
	want := zeroSeqPidCopy(request)
	msgs := s.inner.cap.Msgs()
	for i, m := range msgs {
		if !m.IsRequest() || !bytes.Equal(capturedRequest(m), want) || i+1 >= len(msgs) {
			continue
		}
		r := msgs[i+1]
		if r.Hdr.Type == unix.NLMSG_ERROR && len(r.Body) >= 4 {
			if errno := -int32(binary.LittleEndian.Uint32(r.Body[0:4])); errno != 0 {
				return nil, fmt.Errorf("talk: %w", syscall.Errno(errno))
			}
		}
	}
	return body, err
}

// TestRouteGetMatchesIp replays each `route get` capture through routeGet and
// compares what goip sends and prints with what ip sent and printed: every
// request byte for byte and in order (the get, then the GETLINK by index that
// names the reply's device, with the from/iif row's GETLINK by name first),
// and stdout with ip.out.
//
// go test ./internal/goip/ -run TestRouteGetMatchesIp
func TestRouteGetMatchesIp(t *testing.T) {
	const (
		getlink  = 18 // RTM_GETLINK
		getroute = 26 // RTM_GETROUTE
	)
	tests := []struct {
		description string
		slug        string
		family      uint8
		args        string
		expected    []uint16 // request types, in order
		wantErr     error
	}{
		{"positive: `route get 192.0.2.5`, a cloned reply with uid 0 and an empty cache line",
			"route_get", unix.AF_UNSPEC, "192.0.2.5", []uint16{getroute, getlink}, nil},
		{"positive: `route get fibmatch 192.0.2.5` prints the matched /24",
			"route_get_fibmatch", unix.AF_UNSPEC, "fibmatch 192.0.2.5", []uint16{getroute, getlink}, nil},
		{"positive: `route get 192.0.2.5 notify` prints notify and cache <notify>",
			"route_get_notify", unix.AF_UNSPEC, "192.0.2.5 notify", []uint16{getroute, getlink}, nil},
		{"positive: `-6 route get 2001:db8::5`, no cache line for IPv6",
			"route_get_v6", unix.AF_INET6, "2001:db8::5", []uint16{getroute, getlink}, nil},
		{"corner: `from … iif d1` resolves d1 by name first and names it from that lookup",
			"route_get_from_iif", unix.AF_UNSPEC, "192.0.2.5 from 198.51.100.2 iif d1",
			[]uint16{getlink, getroute, getlink}, nil},
		{"negative: an unreachable destination is the kernel's ENETUNREACH, and nothing on stdout",
			"route_get_unreachable", unix.AF_UNSPEC, "203.0.113.1", []uint16{getroute}, unix.ENETUNREACH},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			dir := routeGetCorpusDir + "/" + tc.slug
			pcap := dir + "/ip.pcap"
			src := routeGetReplay{newStatsReplay(t, pcap)}
			var out bytes.Buffer
			c := &runCtx{src: src, lltab: NewLLTab(), out: &out, errOut: io.Discard, family: tc.family}
			err := routeGet(c, strings.Fields(tc.args))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("routeGet = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil && exitCodeOf(err) != ExitFailure {
				t.Errorf("exit %d, want %d (ip.rc)", exitCodeOf(err), ExitFailure)
			}

			want, rerr := os.ReadFile(dir + "/ip.out")
			if rerr != nil {
				t.Fatal(rerr)
			}
			if out.String() != string(want) {
				t.Errorf("stdout differs from ip.out\n got %q\nwant %q", out.String(), want)
			}

			capt, perr := nlparity.ParseRouteCaptureFile(pcap)
			if perr != nil {
				t.Fatal(perr)
			}
			var ip [][]byte
			for _, m := range capt.Requests() {
				ip = append(ip, capturedRequest(m))
			}
			if len(src.sent) != len(tc.expected) || len(ip) != len(tc.expected) {
				t.Fatalf("goip sent %d requests, ip %d; want %d each", len(src.sent), len(ip), len(tc.expected))
			}
			for i := range ip {
				if typ := requestTypeOf(src.sent[i]); typ != tc.expected[i] {
					t.Errorf("request %d type %d, want %d", i+1, typ, tc.expected[i])
				}
				if !bytes.Equal(src.sent[i], ip[i]) {
					t.Errorf("request %d differs\ngoip %x\nip   %x", i+1, src.sent[i], ip[i])
				}
			}
		})
	}

	// corner: filter_nlmsg's family test. A -4 get answered in another family
	// prints nothing and still exits 0. The kernel answers in the request's
	// family, so no capture has this; the stub hands the v6 reply to a v4 get.
	t.Run("corner: a reply in a family other than the one -4 asked for prints nothing", func(t *testing.T) {
		capt, err := nlparity.ParseRouteCaptureFile(routeGetCorpusDir + "/route_get_v6/ip.pcap")
		if err != nil {
			t.Fatal(err)
		}
		var v6 []byte
		for _, m := range capt.Msgs() {
			if !m.IsRequest() && m.Hdr.Type == unix.RTM_NEWROUTE {
				v6 = m.Body
			}
		}
		if v6 == nil {
			t.Fatal("route_get_v6 holds no RTM_NEWROUTE reply")
		}
		src := fixedTalk{body: v6}
		var out bytes.Buffer
		c := &runCtx{src: src, lltab: NewLLTab(), out: &out, errOut: io.Discard, family: unix.AF_INET}
		if err := routeGet(c, []string{"192.0.2.5"}); err != nil {
			t.Fatalf("routeGet: %v", err)
		}
		if out.Len() != 0 {
			t.Errorf("stdout = %q, want nothing", out.String())
		}
	})
}

// fixedTalk answers every get with one body and every dump with nothing.
type fixedTalk struct{ body []byte }

func (f fixedTalk) Dump([]byte, uint16) ([][]byte, error) { return nil, nil }

func (f fixedTalk) Talk([]byte, uint16) ([]byte, error) { return f.body, nil }
