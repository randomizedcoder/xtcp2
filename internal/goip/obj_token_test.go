package goip

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// runTokenCtx runs `ip token` args on a fresh context over src.
func runTokenCtx(src Source, json bool, args string) (string, string, error) {
	var out, errOut bytes.Buffer
	c := &runCtx{src: src, lltab: NewLLTab(), out: &out, errOut: &errOut, json: json}
	err := runToken(c, strings.Fields(args))
	return out.String(), errOut.String(), err
}

// TestTokenMatchesIp replays the two `ip token` listing captures: goip
// sends ll_init_map's link dump, then the AF_INET6 link dump, byte for byte
// with seq and pid zeroed, and prints the sidecar.
//
// go test ./internal/goip/ -run TestTokenMatchesIp
func TestTokenMatchesIp(t *testing.T) {
	const getTok = routeRtnlDir + "token_set/"
	tests := []struct {
		description string
		pcap        string
		sidecar     string
		args        string
	}{
		{"positive: show, v0's token and nine ::, loopback and NOARP links skipped",
			linkReadsDir + "netlink_route_getlink_token.pcap", linkReadsDir + "ip_token", "show"},
		{"positive: no verb is the listing", linkReadsDir + "netlink_route_getlink_token.pcap", linkReadsDir + "ip_token", ""},
		{"boundary: `s` is show, `l` list, `ls` lst", linkReadsDir + "netlink_route_getlink_token.pcap",
			linkReadsDir + "ip_token", "ls"},
		{"positive: get dev d0, after the write", getTok + "netlink_route_gettoken.pcap", getTok + "ip_gettoken",
			"get dev d0"},
		{"corner: a word before dev is skipped, and one after its name", getTok + "netlink_route_gettoken.pcap",
			getTok + "ip_gettoken", "list bogus dev d0 also"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := newStatsReplay(t, tc.pcap)
			stdout, _, err := runTokenCtx(src, false, tc.args)
			if err != nil {
				t.Fatalf("runToken: %v", err)
			}
			want, err := os.ReadFile(tc.sidecar)
			if err != nil {
				t.Fatal(err)
			}
			if stdout != string(want) {
				t.Errorf("stdout differs from %s\n got %q\nwant %q", tc.sidecar, stdout, want)
			}
			capt, err := nlparity.ParseRouteCaptureFile(tc.pcap)
			if err != nil {
				t.Fatal(err)
			}
			ip := capt.Requests()
			if len(src.sent) != 2 || len(ip) != 2 {
				t.Fatalf("goip sent %d requests, ip %d; want 2 each", len(src.sent), len(ip))
			}
			for i := range ip {
				if !bytes.Equal(src.sent[i], capturedRequest(ip[i])) {
					t.Errorf("request %d differs\ngoip %x\nip   %x", i+1, src.sent[i], capturedRequest(ip[i]))
				}
			}
		})
	}
}

// tokenFake answers ll_init_map's AF_UNSPEC link dump with d0 (3) and lo
// (1), and the AF_INET6 one with tokens, or fails either. sent is each
// request's family byte.
type tokenFake struct {
	tokens     [][]byte
	failLinks  bool
	failTokens bool
	sent       []uint8
}

func (f *tokenFake) Dump(request []byte, _ uint16) ([][]byte, error) {
	fam := request[unix.NLMSG_HDRLEN]
	f.sent = append(f.sent, fam)
	switch {
	case fam == unix.AF_UNSPEC && f.failLinks, fam == unix.AF_INET6 && f.failTokens:
		return nil, unix.EINVAL
	case fam == unix.AF_UNSPEC:
		return [][]byte{newLinkBody(1, "lo"), newLinkBody(3, "d0")}, nil
	}
	return f.tokens, nil
}

// tokenBody is an AF_INET6 RTM_NEWLINK of index and flags carrying
// IFLA_PROTINFO { IFLA_INET6_TOKEN tok }; a nil tok leaves the token out,
// and noProtinfo the nest.
func tokenBody(index int32, flags uint32, tok []byte, noProtinfo bool) []byte {
	b := xtcpnl.EncodeIfInfomsg(unix.AF_INET6, index, flags, 0)
	if noProtinfo {
		return b
	}
	var inner []byte
	if tok != nil {
		inner = nla(unix.IFLA_INET6_TOKEN, tok)
	}
	return catBytes(b, nla(unix.IFLA_PROTINFO, nla(unix.IFLA_INET6_FLAGS, []byte{0, 0, 0, 0}), inner))
}

// tok16 is a 16-byte token whose last bytes are tail.
func tok16(tail ...byte) []byte {
	b := make([]byte, 16)
	copy(b[16-len(tail):], tail)
	return b
}

// TestTokenList is iptoken_list and print_token on constructed replies.
//
// go test ./internal/goip/ -run TestTokenList
func TestTokenList(t *testing.T) {
	const d0Line = "token ::1:2:3:4 dev d0\n"
	d0 := tokenBody(3, unix.IFF_UP, tok16(0, 1, 0, 2, 0, 3, 0, 4), false)
	type result struct {
		stdout, stderr string
		sent           string
		err            error
	}
	tests := []struct {
		description string
		src         *tokenFake
		json        bool
		args        string
		expected    result
	}{
		{description: "positive: one token", src: &tokenFake{tokens: [][]byte{d0}}, args: "show",
			expected: result{stdout: d0Line, sent: "0,10"}},
		{description: "positive: an index not in the cache is ifN", src: &tokenFake{tokens: [][]byte{
			tokenBody(9, 0, tok16(), false)}}, args: "", expected: result{stdout: "token :: dev if9\n", sent: "0,10"}},
		{description: "corner: ::a:b is glibc's ::0.10.0.11", src: &tokenFake{tokens: [][]byte{
			tokenBody(3, 0, tok16(0, 0xa, 0, 0xb), false)}}, args: "list",
			expected: result{stdout: "token ::0.10.0.11 dev d0\n", sent: "0,10"}},
		{description: "negative: loopback is skipped", src: &tokenFake{tokens: [][]byte{
			tokenBody(1, unix.IFF_LOOPBACK, tok16(), false), d0}}, args: "show", expected: result{stdout: d0Line, sent: "0,10"}},
		{description: "negative: NOARP is skipped", src: &tokenFake{tokens: [][]byte{
			tokenBody(4, unix.IFF_NOARP, tok16(), false), d0}}, args: "show", expected: result{stdout: d0Line, sent: "0,10"}},
		{description: "negative: ifindex 0 is skipped", src: &tokenFake{tokens: [][]byte{
			tokenBody(0, 0, tok16(), false), d0}}, args: "show", expected: result{stdout: d0Line, sent: "0,10"}},
		{description: "negative: a reply not AF_INET6 is skipped, even with no IFLA_PROTINFO",
			src: &tokenFake{tokens: [][]byte{newLinkBody(5, "x"), d0}}, args: "show", expected: result{stdout: d0Line, sent: "0,10"}},
		{description: "positive: dev d0 keeps its line alone", src: &tokenFake{tokens: [][]byte{
			tokenBody(7, 0, tok16(), false), d0}}, args: "get dev d0", expected: result{stdout: d0Line, sent: "0,10"}},
		{description: "negative: a skipped reply is not checked for a token", src: &tokenFake{tokens: [][]byte{
			tokenBody(7, 0, nil, true), d0}}, args: "show dev d0", expected: result{stdout: d0Line, sent: "0,10"}},
		{description: "negative: dev of no device, before the dump", src: &tokenFake{tokens: [][]byte{d0}},
			args: "show dev nope", expected: result{sent: "0", err: ErrInvalidArgument}},
		{description: "negative: dev with no name", src: &tokenFake{tokens: [][]byte{d0}}, args: "show dev",
			expected: result{sent: "0", err: ErrInvalidArgument}},
		{description: "corner: the loop stops at the first dev, so a second is not looked up",
			src: &tokenFake{tokens: [][]byte{d0}}, args: "show dev d0 dev nope", expected: result{stdout: d0Line, sent: "0,10"}},
		{description: "boundary: `de` is not dev, strcmp, so it and the next word are skipped",
			src: &tokenFake{tokens: [][]byte{d0}}, args: "show de nope", expected: result{stdout: d0Line, sent: "0,10"}},
		{description: "boundary: `devx` is not dev, so skipped", src: &tokenFake{tokens: [][]byte{d0}}, args: "show devx nope",
			expected: result{stdout: d0Line, sent: "0,10"}},
		{description: "negative: no IFLA_PROTINFO ends the dump after the lines before it",
			src: &tokenFake{tokens: [][]byte{d0, tokenBody(7, 0, nil, true), d0}}, args: "show",
			expected: result{stdout: d0Line, sent: "0,10", err: ErrDumpTerminated}},
		{description: "negative: no token says so on stderr and ends the dump",
			src: &tokenFake{tokens: [][]byte{tokenBody(7, 0, nil, false), d0}}, args: "show",
			expected: result{stderr: iftokNoSupportCst + "\n", sent: "0,10", err: ErrDumpTerminated}},
		{description: "corner: an 8-byte token is refused", src: &tokenFake{tokens: [][]byte{
			tokenBody(3, 0, make([]byte, 8), false)}}, args: "show", expected: result{sent: "0,10", err: ErrDumpTerminated}},
		{description: "positive: -j", src: &tokenFake{tokens: [][]byte{d0}}, json: true, args: "show",
			expected: result{stdout: `[{"token":"::1:2:3:4","ifname":"d0"}]` + "\n", sent: "0,10"}},
		{description: "boundary: -j with nothing printed is an empty array", src: &tokenFake{}, json: true,
			args: "show", expected: result{stdout: "[]\n", sent: "0,10"}},
		{description: "negative: the link dump fails", src: &tokenFake{failLinks: true}, args: "show",
			expected: result{sent: "0", err: ErrDumpTerminated}},
		{description: "negative: the AF_INET6 dump fails", src: &tokenFake{failTokens: true}, args: "show",
			expected: result{sent: "0,10", err: ErrDumpTerminated}},
		{description: "negative: help, after the link dump", src: &tokenFake{}, args: "help",
			expected: result{sent: "0", err: ErrNotImplemented}},
		{description: "negative: an unknown verb, after the link dump", src: &tokenFake{}, args: "foo",
			expected: result{sent: "0", err: ErrInvalidArgument}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			stdout, stderr, err := runTokenCtx(tc.src, tc.json, tc.args)
			if !errors.Is(err, tc.expected.err) {
				t.Fatalf("err = %v, want %v", err, tc.expected.err)
			}
			sent := make([]string, 0, len(tc.src.sent))
			for _, f := range tc.src.sent {
				sent = append(sent, map[uint8]string{0: "0", 10: "10"}[f])
			}
			got := result{stdout: stdout, stderr: stderr, sent: strings.Join(sent, ","), err: tc.expected.err}
			if got != tc.expected {
				t.Errorf("got %+v, want %+v", got, tc.expected)
			}
		})
	}
}
