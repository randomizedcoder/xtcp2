package xtcpnl

import (
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// ilaRender shows a decoded ila body as its command, then each field
// present, a locator or identifier in ip's four-word form, or the error.
func ilaRender(m ILA, err error) string {
	if err != nil {
		return "error: " + err.Error()
	}
	out := []string{"cmd=" + strconv.Itoa(int(m.Command)), "ver=" + strconv.Itoa(int(m.Version))}
	loc := func(name string, v *uint64) {
		if v == nil {
			return
		}
		words := make([]string, 0, 4)
		for shift := 48; shift >= 0; shift -= 16 {
			words = append(words, strconv.FormatUint(*v>>uint(shift)&0xffff, 16))
		}
		out = append(out, name+"="+strings.Join(words, ":"))
	}
	u8 := func(name string, v *uint8) {
		if v != nil {
			out = append(out, name+"="+strconv.Itoa(int(*v)))
		}
	}
	loc("loc", m.Locator)
	loc("identifier", m.Identifier)
	loc("match", m.LocatorMatch)
	if m.Ifindex != nil {
		out = append(out, "ifindex="+strconv.Itoa(int(*m.Ifindex)))
	}
	if m.Dir != nil {
		out = append(out, "dir="+strconv.FormatUint(uint64(*m.Dir), 10))
	}
	u8("csum", m.CsumMode)
	u8("ident", m.IdentType)
	u8("hook", m.HookType)
	return strings.Join(out, " ")
}

// TestParseILA decodes ila bodies. The positive rows are bodies transcribed
// from link-write/genl/ila_*; the rest are hand-built wire edges the corpus
// has none of.
//
// go test ./pkg/xtcpnl/ -run 'TestParseILA$'
func TestParseILA(t *testing.T) {
	add := []byte{ILACmdAdd, 1, 0, 0}
	genlErr := "error: " + ErrGenericNetlink.Error()
	tests := []struct {
		description string
		body        []byte
		expected    string
	}{
		{
			description: "positive: ip's add, the match before the locator (ila_add)",
			body:        mustHex(t, "010100000c00030020010db8000100020c00010020010db800030004"),
			expected:    "cmd=1 ver=1 loc=2001:db8:3:4 match=2001:db8:1:2",
		},
		{
			description: "positive: the kernel's GET answer, with the ifindex, checksum mode and identifier type it always sends (ila_add)",
			body:        mustHex(t, "030100000c00010020010db8000300040c00030020010db800010002080004000000000005000700020000000500080020000000"),
			expected:    "cmd=3 ver=1 loc=2001:db8:3:4 match=2001:db8:1:2 ifindex=0 csum=2 ident=32",
		},
		{
			description: "positive: ip's add on a device (ila_add_dev)",
			body:        mustHex(t, "010100000c00030020010db8000100020c00010020010db8000300040800040003000000"),
			expected:    "cmd=1 ver=1 loc=2001:db8:3:4 match=2001:db8:1:2 ifindex=3",
		},
		{
			description: "positive: ip's add with a checksum mode and identifier type (ila_add_opts)",
			body:        mustHex(t, "010100000c00030020010db8000100020c00010020010db80003000405000700000000000500080001000000"),
			expected:    "cmd=1 ver=1 loc=2001:db8:3:4 match=2001:db8:1:2 csum=0 ident=1",
		},
		{
			description: "positive: ip's del by match and device (ila_del_dev)",
			body:        mustHex(t, "020100000c00030020010db8000100020800040003000000"),
			expected:    "cmd=2 ver=1 match=2001:db8:1:2 ifindex=3",
		},
		{
			description: "boundary: ip's add with no locator is the genl header alone (ila_add_no_loc)",
			body:        mustHex(t, "01010000"),
			expected:    "cmd=1 ver=1",
		},
		{
			description: "boundary: which the kernel accepts, answering with zero locators (ila_add_no_loc)",
			body:        mustHex(t, "030100000c00010000000000000000000c0003000000000000000000080004000000000005000700020000000500080020000000"),
			expected:    "cmd=3 ver=1 loc=0:0:0:0 match=0:0:0:0 ifindex=0 csum=2 ident=32",
		},
		{
			description: "boundary: an all-ones locator is the largest",
			body:        cat(add, rtattr(ILAAttrLocator, []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff})),
			expected:    "cmd=1 ver=1 loc=ffff:ffff:ffff:ffff",
		},
		{
			description: "boundary: a body one byte short of the genl header is an error",
			body:        add[:3],
			expected:    genlErr + ": short genlmsghdr",
		},
		{
			description: "corner: the identifier, direction and hook type no ip command sends decode",
			body: cat(add, rtattr(ILAAttrIdentifier, []byte{0, 0, 0, 0, 0, 0, 0x12, 0x34}),
				rtattr(ILAAttrDir, le32(3)), rtattr(ILAAttrHookType, []byte{1})),
			expected: "cmd=1 ver=1 identifier=0:0:0:1234 dir=3 hook=1",
		},
		{
			description: "corner: the pad before a u64, an unknown attribute and an unknown command are not errors",
			body:        cat([]byte{9, 1, 0, 0}, rtattr(6, nil), rtattr(42, le32(1)), rtattr(ILAAttrCsumMode, []byte{3})),
			expected:    "cmd=9 ver=1 csum=3",
		},
		{
			description: "corner: a negative ifindex keeps its sign",
			body:        cat(add, rtattr(ILAAttrIfindex, le32(0xffffffff))),
			expected:    "cmd=1 ver=1 ifindex=-1",
		},
		{
			description: "negative: a locator narrower than a u64 is an error",
			body:        cat(add, rtattr(ILAAttrLocator, le32(1))),
			expected:    genlErr + ": attribute 1 needs an unflagged 8-byte scalar",
		},
		{
			description: "negative: a match wider than a u64 is an error",
			body:        cat(add, rtattr(ILAAttrLocatorMatch, make([]byte, 12))),
			expected:    genlErr + ": attribute 3 needs an unflagged 8-byte scalar",
		},
		{
			description: "negative: an ifindex narrower than an s32 is an error",
			body:        cat(add, rtattr(ILAAttrIfindex, []byte{3, 0})),
			expected:    genlErr + ": attribute 4 needs an unflagged 4-byte scalar",
		},
		{
			description: "negative: a direction narrower than a u32 is an error",
			body:        cat(add, rtattr(ILAAttrDir, []byte{1})),
			expected:    genlErr + ": attribute 5 needs an unflagged 4-byte scalar",
		},
		{
			description: "negative: a checksum mode wider than a u8 is an error",
			body:        cat(add, rtattr(ILAAttrCsumMode, le32(2))),
			expected:    genlErr + ": attribute 7 needs an unflagged 1-byte scalar",
		},
		{
			description: "negative: an identifier type wider than a u8 is an error",
			body:        cat(add, rtattr(ILAAttrIdentType, le32(32))),
			expected:    genlErr + ": attribute 8 needs an unflagged 1-byte scalar",
		},
		{
			description: "negative: a repeated locator is an error",
			body:        cat(add, rtattr(ILAAttrLocator, make([]byte, 8)), rtattr(ILAAttrLocator, make([]byte, 8))),
			expected:    genlErr + ": duplicate attribute 1",
		},
		{
			description: "negative: an attribute longer than the body is an error",
			body:        cat(add, rtattrOverrunning(ILAAttrLocator, 40)),
			expected:    genlErr + ": attribute length 40",
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := ilaRender(ParseILA(tt.body)); got != tt.expected {
				t.Errorf("got %q, expected %q", got, tt.expected)
			}
		})
	}
}

// TestParseILACorpus decodes every ila message in the committed corpus.
//
// go test ./pkg/xtcpnl/ -run TestParseILACorpus
func TestParseILACorpus(t *testing.T) {
	// tally decodes every ila message of command cmd, ip's requests or the
	// kernel's answers, and counts the fields they carry.
	tally := func(cmd uint8, request bool) string {
		var n, errs, loc, match, ifindex, csum int
		count := func(ok bool, c *int) {
			if ok {
				*c++
			}
		}
		for _, c := range corpusAll(t) {
			m := c.m
			if c.family != ILAGenlName || (m.h.Flags&unix.NLM_F_REQUEST != 0) != request || len(m.body) == 0 || m.body[0] != cmd {
				continue
			}
			n++
			il, err := ParseILA(m.body)
			if err != nil {
				errs++
				continue
			}
			count(il.Locator != nil, &loc)
			count(il.LocatorMatch != nil, &match)
			count(il.Ifindex != nil, &ifindex)
			count(il.CsumMode != nil, &csum)
		}
		return strconv.Itoa(n) + " decoded, " + strconv.Itoa(errs) + " errors, " + strconv.Itoa(loc) + " loc, " +
			strconv.Itoa(match) + " match, " + strconv.Itoa(ifindex) + " ifindex, " + strconv.Itoa(csum) + " csum"
	}
	tests := []struct {
		description string
		got         string
		expected    string
	}{
		{description: "positive: ip's ADD requests decode, all but the one with no locator carrying both", got: tally(ILACmdAdd, true), expected: "7 decoded, 0 errors, 6 loc, 6 match, 2 ifindex, 2 csum"},
		{description: "positive: ip's DEL requests decode, each with a match", got: tally(ILACmdDel, true), expected: "4 decoded, 0 errors, 0 loc, 4 match, 1 ifindex, 0 csum"},
		{description: "positive: ip's GET requests decode, every one a dump of the genl header alone", got: tally(ILACmdGet, true), expected: "12 decoded, 0 errors, 0 loc, 0 match, 0 ifindex, 0 csum"},
		{description: "positive: every mapping the kernel answered decodes with both locators, an ifindex and a checksum mode", got: tally(ILACmdGet, false), expected: "11 decoded, 0 errors, 11 loc, 11 match, 11 ifindex, 11 csum"},
		{description: "negative: the kernel answers no ADD or DEL, and ip sends no FLUSH", got: tally(ILACmdAdd, false) + "|" + tally(ILACmdDel, false) + "|" + tally(ILACmdFlush, true), expected: "0 decoded, 0 errors, 0 loc, 0 match, 0 ifindex, 0 csum|0 decoded, 0 errors, 0 loc, 0 match, 0 ifindex, 0 csum|0 decoded, 0 errors, 0 loc, 0 match, 0 ifindex, 0 csum"},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("got %q, expected %q", tt.got, tt.expected)
			}
		})
	}
}
