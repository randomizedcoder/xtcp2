package xtcpnl

import (
	"net"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// prefixRender shows a decoded RTM_NEWPREFIX body as its header, then each
// attribute present, or the error.
func prefixRender(pi PrefixInfo, err error) string {
	if err != nil {
		return "error: " + err.Error()
	}
	out := []string{
		"family=" + strconv.Itoa(int(pi.Family)),
		"ifindex=" + strconv.Itoa(int(pi.Ifindex)),
		"type=" + strconv.Itoa(int(pi.Type)),
		"len=" + strconv.Itoa(int(pi.Len)),
		"flags=0x" + strconv.FormatUint(uint64(pi.Flags), 16),
	}
	if pi.Address != nil {
		out = append(out, "addr="+net.IP(pi.Address).String())
	}
	if pi.HasCacheinfo {
		out = append(out, "preferred="+strconv.FormatUint(uint64(pi.PreferredTime), 10),
			"valid="+strconv.FormatUint(uint64(pi.ValidTime), 10))
	}
	return strings.Join(out, " ")
}

// TestParsePrefix decodes RTM_NEWPREFIX bodies. The positive rows are the
// kernel's notifications transcribed from link-write/rtnl/prefix_event and
// monitor_all; the rest are hand-built wire edges the corpus has none of.
//
// go test ./pkg/xtcpnl/ -run 'TestParsePrefix$'
func TestParsePrefix(t *testing.T) {
	// hdr is a prefixmsg for ifindex 5, a /64 prefix option with flags.
	hdr := func(flags uint8) []byte {
		return cat([]byte{unix.AF_INET6, 0, 0, 0}, le32(5), []byte{3, 64, flags, 0})
	}
	addr := rtattr(PrefixAddress, net.ParseIP("2001:db8:5::"))
	tests := []struct {
		description string
		body        []byte
		expected    string
	}{
		{
			description: "positive: the kernel's NEWPREFIX for r0's router advertisement (prefix_event)",
			body:        mustHex(t, "0a00000005000000034080001400010020010db80005000000000000000000000c0002002c01000058020000"),
			expected:    "family=10 ifindex=5 type=3 len=64 flags=0x80 addr=2001:db8:5:: preferred=300 valid=600",
		},
		{
			description: "positive: the same advertisement seen by ip monitor all, on ifindex 6 (monitor_all)",
			body:        mustHex(t, "0a00000006000000034080001400010020010db80005000000000000000000000c0002002c01000058020000"),
			expected:    "family=10 ifindex=6 type=3 len=64 flags=0x80 addr=2001:db8:5:: preferred=300 valid=600",
		},
		{
			description: "boundary: a header-only body decodes the prefixmsg and nothing else",
			body:        hdr(0),
			expected:    "family=10 ifindex=5 type=3 len=64 flags=0x0",
		},
		{
			description: "boundary: a body one byte short of the prefixmsg is an error",
			body:        hdr(0)[:PrefixMsgSizeCst-1],
			expected:    "error: " + ErrPrefixMsgSmall.Error(),
		},
		{
			description: "corner: the flags are the advertisement's own byte, on-link and autonomous, passed through raw",
			body:        cat(hdr(0xc0), addr),
			expected:    "family=10 ifindex=5 type=3 len=64 flags=0xc0 addr=2001:db8:5::",
		},
		{
			description: "corner: a negative ifindex keeps its sign",
			body:        cat([]byte{unix.AF_INET6, 0, 0, 0}, le32(0xffffffff), []byte{3, 64, 0, 0}),
			expected:    "family=10 ifindex=-1 type=3 len=64 flags=0x0",
		},
		{
			description: "corner: a cacheinfo shorter than a prefix_cacheinfo is not decoded",
			body:        cat(hdr(0), addr, rtattr(PrefixCacheinfo, le32(300))),
			expected:    "family=10 ifindex=5 type=3 len=64 flags=0x0 addr=2001:db8:5::",
		},
		{
			description: "corner: a cacheinfo of exactly eight bytes decodes, preferred first",
			body:        cat(hdr(0), rtattr(PrefixCacheinfo, cat(le32(1), le32(0xffffffff)))),
			expected:    "family=10 ifindex=5 type=3 len=64 flags=0x0 preferred=1 valid=4294967295",
		},
		{
			description: "corner: an unknown attribute is skipped",
			body:        cat(hdr(0), rtattr(9, le32(7)), addr),
			expected:    "family=10 ifindex=5 type=3 len=64 flags=0x0 addr=2001:db8:5::",
		},
		{
			description: "negative: an attribute longer than the body is a parse error",
			body:        cat(hdr(0), rtattrOverrunning(PrefixAddress, 40)),
			expected:    "error: " + ErrRTAttrSmall.Error(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := prefixRender(ParsePrefix(tt.body)); got != tt.expected {
				t.Errorf("got %q, expected %q", got, tt.expected)
			}
		})
	}
}

// TestParsePrefixAddressCopied checks that Address does not alias the body.
func TestParsePrefixAddressCopied(t *testing.T) {
	body := mustHex(t, "0a00000005000000034080001400010020010db80005000000000000000000000c0002002c01000058020000")
	pi, err := ParsePrefix(body)
	if err != nil {
		t.Fatal(err)
	}
	body[16] = 0xff
	tests := []struct {
		description string
		got         string
		expected    string
	}{
		{description: "corner: overwriting the body leaves the decoded address intact", got: net.IP(pi.Address).String(), expected: "2001:db8:5::"},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("got %q, expected %q", tt.got, tt.expected)
			}
		})
	}
}

// TestParsePrefixCorpus decodes every RTM_NEWPREFIX message in the
// committed corpus.
//
// go test ./pkg/xtcpnl/ -run TestParsePrefixCorpus
func TestParsePrefixCorpus(t *testing.T) {
	// tally decodes every RTM_NEWPREFIX, the kernel's or ip's requests,
	// and lists each one's ifindex, prefix and lifetimes.
	tally := func(request bool) string {
		var n, errs int
		var seen []string
		for _, c := range corpusAll(t) {
			m := c.m
			if m.proto != unix.NETLINK_ROUTE || m.h.Type != unix.RTM_NEWPREFIX || (m.h.Flags&unix.NLM_F_REQUEST != 0) != request {
				continue
			}
			n++
			pi, err := ParsePrefix(m.body)
			if err != nil {
				errs++
				continue
			}
			seen = append(seen, strconv.Itoa(int(pi.Ifindex))+":"+net.IP(pi.Address).String()+"/"+strconv.Itoa(int(pi.Len))+
				":"+strconv.FormatUint(uint64(pi.PreferredTime), 10)+"/"+strconv.FormatUint(uint64(pi.ValidTime), 10))
		}
		return strconv.Itoa(n) + " decoded, " + strconv.Itoa(errs) + " errors, [" + strings.Join(seen, " ") + "]"
	}
	tests := []struct {
		description string
		got         string
		expected    string
	}{
		{
			description: "positive: the kernel's three NEWPREFIX notifications decode, one per monitoring row",
			got:         tally(false),
			expected:    "3 decoded, 0 errors, [6:2001:db8:5::/64:300/600 6:2001:db8:5::/64:300/600 5:2001:db8:5::/64:300/600]",
		},
		{
			description: "negative: ip sends no NEWPREFIX",
			got:         tally(true),
			expected:    "0 decoded, 0 errors, []",
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("got %q, expected %q", tt.got, tt.expected)
			}
		})
	}
}
