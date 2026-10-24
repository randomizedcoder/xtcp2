package xtcpnl

import (
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// linkPropRender shows a decoded RTM_*LINKPROP body as its index, then each
// selector and the list present, or the error.
func linkPropRender(lp LinkPropInfo, err error) string {
	if err != nil {
		return "error: " + err.Error()
	}
	out := []string{"family=" + strconv.Itoa(int(lp.Family)), "index=" + strconv.Itoa(int(lp.Index))}
	if lp.Type != 0 || lp.Flags != 0 || lp.Change != 0 {
		out = append(out, "type="+strconv.Itoa(int(lp.Type)),
			"flags=0x"+strconv.FormatUint(uint64(lp.Flags), 16), "change=0x"+strconv.FormatUint(uint64(lp.Change), 16))
	}
	if lp.IfName != "" {
		out = append(out, "ifname="+lp.IfName)
	}
	if lp.AltIfName != "" {
		out = append(out, "altifname="+lp.AltIfName)
	}
	if lp.HasPropList {
		out = append(out, "altnames=["+strings.Join(lp.AltNames, ",")+"]")
	}
	return strings.Join(out, " ")
}

// TestParseLinkProp decodes RTM_*LINKPROP bodies. The positive rows are ip's
// requests transcribed from link-write/rtnl/link_altname_*; the rest are
// hand-built wire edges the corpus has none of.
//
// go test ./pkg/xtcpnl/ -run 'TestParseLinkProp$'
func TestParseLinkProp(t *testing.T) {
	hdr := func(index int32) []byte {
		return cat([]byte{unix.AF_UNSPEC, 0, 0, 0}, le32(uint32(index)), le32(0), le32(0))
	}
	alt := func(name string) []byte { return rtattr(unix.IFLA_ALT_IFNAME, append([]byte(name), 0)) }
	props := func(entries ...[]byte) []byte {
		return rtattr(uint16(unix.NLA_F_NESTED)|unix.IFLA_PROP_LIST, cat(entries...))
	}
	tests := []struct {
		description string
		body        []byte
		expected    string
	}{
		{
			description: "positive: ip's NEWLINKPROP adding altname d0alt to ifindex 3 (link_altname_add)",
			body:        mustHex(t, "00000000030000000000000000000000100034800a0035006430616c74000000"),
			expected:    "family=0 index=3 altnames=[d0alt]",
		},
		{
			description: "positive: ip's DELLINKPROP has the same body (link_altname_del)",
			body:        mustHex(t, "00000000030000000000000000000000100034800a0035006430616c74000000"),
			expected:    "family=0 index=3 altnames=[d0alt]",
		},
		{
			description: "positive: ip's NEWLINKPROP for a name the kernel refuses as taken, lo (link_altname_taken)",
			body:        mustHex(t, "000000000300000000000000000000000c003480070035006c6f0000"),
			expected:    "family=0 index=3 altnames=[lo]",
		},
		{
			description: "boundary: a header-only body decodes the ifinfomsg and no list",
			body:        hdr(3),
			expected:    "family=0 index=3",
		},
		{
			description: "boundary: a body one byte short of the ifinfomsg is an error",
			body:        hdr(3)[:IfInfomsgSizeCst-1],
			expected:    "error: " + ErrIfInfomsgSmall.Error(),
		},
		{
			description: "corner: the ifinfomsg type, flags and change are decoded as sent",
			body:        cat([]byte{unix.AF_UNSPEC, 0, 1, 0}, le32(3), le32(0x1), le32(0xffffffff)),
			expected:    "family=0 index=3 type=1 flags=0x1 change=0xffffffff",
		},
		{
			description: "corner: a list of two names keeps both, in wire order",
			body:        cat(hdr(3), props(alt("a1"), alt("a2"))),
			expected:    "family=0 index=3 altnames=[a1,a2]",
		},
		{
			description: "corner: with no index, the link is selected by name or by altname",
			body:        cat(hdr(0), rtattr(unix.IFLA_IFNAME, []byte("eth0\x00")), rtattr(unix.IFLA_ALT_IFNAME, []byte("e0\x00")), props(alt("x"))),
			expected:    "family=0 index=0 ifname=eth0 altifname=e0 altnames=[x]",
		},
		{
			description: "corner: a repeated selector keeps the last, as the kernel's nla_parse does",
			body:        cat(hdr(0), rtattr(unix.IFLA_IFNAME, []byte("one\x00")), rtattr(unix.IFLA_IFNAME, []byte("two\x00"))),
			expected:    "family=0 index=0 ifname=two",
		},
		{
			description: "corner: an entry other than IFLA_ALT_IFNAME in the list is skipped",
			body:        cat(hdr(3), props(rtattr(unix.IFLA_MTU, le32(1500)), alt("b"))),
			expected:    "family=0 index=3 altnames=[b]",
		},
		{
			description: "corner: an empty list is a list of no names",
			body:        cat(hdr(3), props()),
			expected:    "family=0 index=3 altnames=[]",
		},
		{
			description: "corner: a malformed entry ends the list, keeping the names before it",
			body:        cat(hdr(3), props(alt("c"), rtattrOverrunning(unix.IFLA_ALT_IFNAME, 40))),
			expected:    "family=0 index=3 altnames=[c]",
		},
		{
			description: "negative: an attribute longer than the body is a parse error",
			body:        cat(hdr(3), rtattrOverrunning(unix.IFLA_PROP_LIST, 40)),
			expected:    "error: " + ErrRTAttrSmall.Error(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := linkPropRender(ParseLinkProp(tt.body)); got != tt.expected {
				t.Errorf("got %q, expected %q", got, tt.expected)
			}
		})
	}
}

// TestParseLinkPropCorpus decodes every RTM_*LINKPROP message in the
// committed corpus.
//
// go test ./pkg/xtcpnl/ -run TestParseLinkPropCorpus
func TestParseLinkPropCorpus(t *testing.T) {
	// tally decodes every message of type typ, ip's requests or the
	// kernel's, and lists the alternative names they carry.
	tally := func(typ uint16, request bool) string {
		var n, errs int
		var names []string
		for _, c := range corpusAll(t) {
			m := c.m
			if m.proto != unix.NETLINK_ROUTE || m.h.Type != typ || (m.h.Flags&unix.NLM_F_REQUEST != 0) != request {
				continue
			}
			n++
			lp, err := ParseLinkProp(m.body)
			if err != nil {
				errs++
				continue
			}
			names = append(names, strconv.Itoa(int(lp.Index))+":"+strings.Join(lp.AltNames, ","))
		}
		return strconv.Itoa(n) + " decoded, " + strconv.Itoa(errs) + " errors, names [" + strings.Join(names, " ") + "]"
	}
	tests := []struct {
		description string
		got         string
		expected    string
	}{
		{
			description: "positive: ip's two NEWLINKPROP requests decode, d0alt and the refused lo, both on ifindex 3",
			got:         tally(unix.RTM_NEWLINKPROP, true),
			expected:    "2 decoded, 0 errors, names [3:d0alt 3:lo]",
		},
		{
			description: "positive: ip's DELLINKPROP request decodes",
			got:         tally(unix.RTM_DELLINKPROP, true),
			expected:    "1 decoded, 0 errors, names [3:d0alt]",
		},
		{
			description: "negative: the kernel sends no NEWLINKPROP",
			got:         tally(unix.RTM_NEWLINKPROP, false),
			expected:    "0 decoded, 0 errors, names []",
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
