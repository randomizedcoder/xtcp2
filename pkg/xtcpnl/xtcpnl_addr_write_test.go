package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// This file tests the address write encoder (addr-write-path.md Phase 5,
// table 2). The positive rows are the requests `ip` 7.1 sent in the captured
// corpus (testdata/7_1_4/link-write/addr/), so the encoder is checked against
// bytes it did not produce. The layout rows reach what the corpus cannot, and
// their expected bytes come from the rtattr test helper, a second encoder.

// addrIP is an address's wire bytes, 4 for IPv4 and 16 for IPv6.
func addrIP(s string) []byte { return netip.MustParseAddr(s).AsSlice() }

func addrLocal(s string) AddrAttr { return AddrAttrBytes(uint16(unix.IFA_LOCAL), addrIP(s)) }
func addrAddress(s string) AddrAttr {
	return AddrAttrBytes(uint16(unix.IFA_ADDRESS), addrIP(s))
}
func addrBroadcast(s string) AddrAttr {
	return AddrAttrBytes(uint16(unix.IFA_BROADCAST), addrIP(s))
}
func addrLabel(s string) AddrAttr { return AddrAttrString(uint16(unix.IFA_LABEL), s) }
func addrMetric(v uint32) AddrAttr {
	return AddrAttrU32(uint16(unix.IFA_RT_PRIORITY), v)
}

const (
	addrNew = uint16(unix.RTM_NEWADDR)
	addrDel = uint16(unix.RTM_DELADDR)
	// The verbs' flags as do_ipaddr sets them (ip/ipaddress.c:2679-2687),
	// with the NLM_F_ACK rtnl_talk adds.
	addrFlagsAdd     = uint16(unix.NLM_F_CREATE | unix.NLM_F_EXCL | unix.NLM_F_ACK)
	addrFlagsChange  = uint16(unix.NLM_F_REPLACE | unix.NLM_F_ACK)
	addrFlagsReplace = uint16(unix.NLM_F_CREATE | unix.NLM_F_REPLACE | unix.NLM_F_ACK)
	addrFlagsDelete  = uint16(unix.NLM_F_ACK)
	// addrD0 is d0's ifindex in every corpus namespace (lo=1, nlmon0=2, d0=3).
	addrD0 = 3
)

// addrV4Add is `ip addr add LOCAL/plen dev d0` with nothing else: the local in
// argv, its IFA_ADDRESS copy after the loop.
func addrV4Add(local string, plen uint8) AddrWrite {
	return AddrWrite{
		MsgType: addrNew, Flags: addrFlagsAdd,
		Family: unix.AF_INET, Prefixlen: plen, Index: addrD0,
		Attrs: []AddrAttr{addrLocal(local)}, PostAttrs: []AddrAttr{addrAddress(local)},
	}
}

// addrV6Add is addrV4Add for IPv6, with the flags word.
func addrV6Add(local string, plen uint8, flags uint32) AddrWrite {
	return AddrWrite{
		MsgType: addrNew, Flags: addrFlagsAdd,
		Family: unix.AF_INET6, Prefixlen: plen, Index: addrD0, IfaFlags: flags,
		Attrs: []AddrAttr{addrLocal(local)}, PostAttrs: []AddrAttr{addrAddress(local)},
	}
}

// addrCorpusRequest is the address write a corpus row captured, seq and pid
// zeroed as the comparator zeroes them. `ip` sends each request in its own
// datagram, and a row writes at most once, so the write is the one datagram
// that opens with an RTM_NEWADDR or RTM_DELADDR request. ok is false for a row
// `ip` refused before writing, which holds the lookup at most and no
// NLMSG_ERROR, so lastNlMsgErr cannot read it.
func addrCorpusRequest(t *testing.T, slug string) (req []byte, ok bool) {
	t.Helper()
	path := filepath.Join(tdLinkWrite_7_1_4, "addr", slug, "ip.pcap")
	for _, p := range pcapDatagrams(t, path) {
		if len(p) < NlMsgHdrSizeCst {
			continue
		}
		typ := binary.LittleEndian.Uint16(p[4:6])
		flags := binary.LittleEndian.Uint16(p[6:8])
		if (typ != addrNew && typ != addrDel) || flags&unix.NLM_F_REQUEST == 0 {
			continue
		}
		if req != nil {
			t.Fatalf("%s: more than one address write", path)
		}
		mlen := int(binary.LittleEndian.Uint32(p[0:4]))
		if mlen < NlMsgHdrSizeCst || mlen > len(p) {
			t.Fatalf("%s: nlmsg_len %d in a %d-byte datagram", path, mlen, len(p))
		}
		req = append([]byte(nil), p[:mlen]...)
		clear(req[8:16])
	}
	return req, req != nil
}

// TestBuildAddrWriteRequestMatchesCorpus builds every address write in the
// corpus from an AddrWrite and compares it with what `ip` sent, byte for byte.
//
// go test ./pkg/xtcpnl/ -run TestBuildAddrWriteRequestMatchesCorpus
func TestBuildAddrWriteRequestMatchesCorpus(t *testing.T) {
	v4 := addrV4Add
	with := func(w AddrWrite, mod func(*AddrWrite)) AddrWrite { mod(&w); return w }

	tests := []struct {
		description string
		slug        string
		write       AddrWrite
	}{
		{description: "positive: IPv4 local then its address copy (design row 1)", slug: "add_v4",
			write: v4("192.0.2.10", 24)},
		{description: "positive: a secondary on a subnet that already has a primary", slug: "add_v4_secondary",
			write: v4("192.0.2.2", 24)},
		{description: "positive: an existing address is still sent", slug: "add_existing",
			write: v4("192.0.2.1", 24)},
		{description: "positive: peer is IFA_ADDRESS in argv order, prefixlen 32, no copy", slug: "add_v4_peer",
			write: with(v4("10.0.0.1", 32), func(w *AddrWrite) {
				w.Attrs = []AddrAttr{addrLocal("10.0.0.1"), addrAddress("10.0.0.2")}
				w.PostAttrs = nil
			})},
		{description: "positive: brd + computed broadcast is post-loop", slug: "add_v4_brd_plus",
			write: with(v4("192.0.2.10", 24), func(w *AddrWrite) {
				w.PostAttrs = append(w.PostAttrs, addrBroadcast("192.0.2.255"))
			})},
		{description: "positive: brd - clears the host bits", slug: "add_v4_brd_minus",
			write: with(v4("192.0.2.10", 24), func(w *AddrWrite) {
				w.PostAttrs = append(w.PostAttrs, addrBroadcast("192.0.2.0"))
			})},
		{description: "boundary: /31 with brd + carries no broadcast", slug: "add_v4_brd_31",
			write: v4("192.0.2.10", 31)},
		{description: "positive: the label goes ahead of the broadcast (rows 7 and 13)", slug: "add_v4_label",
			write: with(v4("192.0.2.10", 24), func(w *AddrWrite) {
				w.Attrs = append(w.Attrs, addrLabel("d0:1"))
				w.PostAttrs = append(w.PostAttrs, addrBroadcast("192.0.2.255"))
			})},
		{description: "positive: metric is IFA_RT_PRIORITY u32", slug: "add_v4_metric",
			write: with(v4("192.0.2.10", 24), func(w *AddrWrite) { w.Attrs = append(w.Attrs, addrMetric(50)) })},
		{description: "corner: pref is the metric", slug: "add_v4_pref_is_metric",
			write: with(v4("192.0.2.10", 24), func(w *AddrWrite) { w.Attrs = append(w.Attrs, addrMetric(7)) })},
		{description: "positive: proto is a u8, rta_len 5", slug: "add_v4_proto_name",
			write: with(v4("192.0.2.10", 24), func(w *AddrWrite) {
				w.Attrs = append(w.Attrs, AddrAttrU8(IfaProto, 1))
			})},
		{description: "positive: lifetimes are post-loop IFA_CACHEINFO {preferred, valid, 0, 0}", slug: "add_v4_lifetimes",
			write: with(v4("192.0.2.10", 24), func(w *AddrWrite) {
				w.PostAttrs = append(w.PostAttrs, AddrAttrCacheinfo(1800, 3600))
			})},
		{description: "positive: loopback scope is in the header", slug: "add_v4_loopback_scope",
			write: with(v4("127.0.0.2", 8), func(w *AddrWrite) { w.Scope = unix.RT_SCOPE_HOST })},
		{description: "boundary: autojoin 0x400 spills, IFA_FLAGS ahead of the copy", slug: "add_v4_autojoin",
			write: with(v4("224.0.0.251", 32), func(w *AddrWrite) { w.IfaFlags = unix.IFA_F_MCAUTOJOIN })},
		{description: "positive: nodad fits the header byte, no IFA_FLAGS (row 2)", slug: "add_v6_nodad",
			write: addrV6Add("2001:db8::5", 64, unix.IFA_F_NODAD)},
		{description: "positive: the worked example, nodad noprefixroute 0x202 (row 4)", slug: "add_v6_flags_wide",
			write: addrV6Add("2001:db8::5", 64, unix.IFA_F_NODAD|unix.IFA_F_NOPREFIXROUTE)},
		{description: "boundary: mngtmpaddr 0x100 spills (row 6)", slug: "add_v6_mngtmpaddr_48",
			write: addrV6Add("2001:db8::5", 48, unix.IFA_F_MANAGETEMPADDR)},
		{description: "positive: nodad optimistic is 0x06 in the header", slug: "add_v6_nodad_optimistic",
			write: addrV6Add("2001:db8::5", 64, unix.IFA_F_NODAD|unix.IFA_F_OPTIMISTIC)},
		{description: "corner: a v6-only flag dropped before the family was known is absent", slug: "add_v6_nodad_first",
			write: addrV6Add("2001:db8::5", 64, 0)},
		{description: "positive: an IPv6 multicast /128", slug: "v6_mcast_noautojoin",
			write: addrV6Add("ff02::fb", 128, 0)},
		{description: "positive: change is 0x105 (row 3)", slug: "change_missing_creates",
			write: with(v4("192.0.2.10", 24), func(w *AddrWrite) { w.Flags = addrFlagsChange })},
		{description: "positive: replace is 0x505 (row 3)", slug: "replace_existing",
			write: with(v4("192.0.2.1", 24), func(w *AddrWrite) {
				w.Flags = addrFlagsReplace
				w.Attrs = append(w.Attrs, addrMetric(9))
			})},
		{description: "positive: delete is RTM_DELADDR 0x005, scope 0 (row 3)", slug: "del_v4",
			write: with(v4("192.0.2.1", 24), func(w *AddrWrite) { w.MsgType, w.Flags = addrDel, addrFlagsDelete })},
		{description: "positive: deleting a missing address is still sent", slug: "del_missing",
			write: with(v4("192.0.2.99", 24), func(w *AddrWrite) { w.MsgType, w.Flags = addrDel, addrFlagsDelete })},
		{description: "corner: the wildcard delete has no IFA_ADDRESS", slug: "del_v4_wildcard",
			write: with(v4("192.0.2.1", 32), func(w *AddrWrite) {
				w.MsgType, w.Flags = addrDel, addrFlagsDelete
				w.PostAttrs = nil
			})},
		{description: "corner: a 16-character label is built; the kernel refuses it", slug: "label_16",
			write: with(v4("192.0.2.10", 24), func(w *AddrWrite) {
				w.Attrs = append(w.Attrs, addrLabel("d0:0123456789abc"))
			})},
		{description: "positive: scope link is in the header", slug: "scope_conflict",
			write: with(v4("192.0.2.2", 24), func(w *AddrWrite) { w.Scope = unix.RT_SCOPE_LINK })},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			expected, ok := addrCorpusRequest(t, tc.slug)
			if !ok {
				t.Fatalf("%s holds no address write", tc.slug)
			}
			got, err := BuildAddrWriteRequest(tc.write, 0)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, expected) {
				t.Errorf("request =\n% x\nwant\n% x", got, expected)
			}
		})
	}

	t.Run("corner: every corpus slug whose pcap holds an address write has a row", func(t *testing.T) {
		ents, err := os.ReadDir(filepath.Join(tdLinkWrite_7_1_4, "addr"))
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, e := range ents {
			if _, ok := addrCorpusRequest(t, e.Name()); ok {
				got = append(got, e.Name())
			}
		}
		expected := make([]string, 0, len(tests))
		for _, tc := range tests {
			expected = append(expected, tc.slug)
		}
		sort.Strings(expected)
		if strings.Join(got, " ") != strings.Join(expected, " ") {
			t.Errorf("slugs with an address write:\n%v\nrows:\n%v", got, expected)
		}
	})
}

// TestBuildAddrWriteRequestWorkedExample is the design's worked example,
// transcribed from the document rather than read from the capture.
//
// go test ./pkg/xtcpnl/ -run TestBuildAddrWriteRequestWorkedExample
func TestBuildAddrWriteRequestWorkedExample(t *testing.T) {
	tests := []struct {
		description string
		write       AddrWrite
		expected    string
	}{
		{
			description: "positive: `ip -6 addr add 2001:db8::5/64 nodad noprefixroute dev d0`, 72 bytes",
			write:       addrV6Add("2001:db8::5", 64, unix.IFA_F_NODAD|unix.IFA_F_NOPREFIXROUTE),
			expected: `
				48000000 14000506 00000000 00000000
				0a400000 03000000
				14000200 20010db8 00000000 00000000 00000005
				08000800 02020000
				14000100 20010db8 00000000 00000000 00000005`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := BuildAddrWriteRequest(tc.write, 0)
			if err != nil {
				t.Fatal(err)
			}
			if want := hexBytes(t, tc.expected); !bytes.Equal(got, want) {
				t.Errorf("request =\n% x\nwant\n% x", got, want)
			}
		})
	}
}

// TestBuildAddrWriteRequest is the layout table: design table 2's rows that
// the corpus does not reach.
//
// go test ./pkg/xtcpnl/ -run TestBuildAddrWriteRequest$
func TestBuildAddrWriteRequest(t *testing.T) {
	const (
		local     = uint16(unix.IFA_LOCAL)
		address   = uint16(unix.IFA_ADDRESS)
		label     = uint16(unix.IFA_LABEL)
		broadcast = uint16(unix.IFA_BROADCAST)
		anycast   = uint16(unix.IFA_ANYCAST)
		flagsAttr = uint16(unix.IFA_FLAGS)
	)
	v4 := addrIP("192.0.2.10")
	req := func(typ uint16, hdr []byte, attrs ...[]byte) []byte {
		return nlmsg(typ, uint16(unix.NLM_F_REQUEST), 7, concat(append([][]byte{hdr}, attrs...)...))
	}
	hdr4 := func(plen, flags uint8) []byte { return EncodeIfAddrmsg(unix.AF_INET, plen, flags, 0, addrD0) }
	base := func(mod func(*AddrWrite)) AddrWrite {
		w := AddrWrite{MsgType: addrNew, Family: unix.AF_INET, Prefixlen: 24, Index: addrD0,
			Attrs: []AddrAttr{AddrAttrBytes(local, v4)}, PostAttrs: []AddrAttr{AddrAttrBytes(address, v4)}}
		if mod != nil {
			mod(&w)
		}
		return w
	}
	nul := func(s string) []byte { return append([]byte(s), 0) }
	long := func(n int) string { return strings.Repeat("a", n) }

	tests := []struct {
		description string
		write       AddrWrite
		expected    []byte
		wantErr     error
	}{
		{
			description: "boundary: flags exactly 0xff stay in the header (row 5)",
			write:       base(func(w *AddrWrite) { w.IfaFlags = 0xff }),
			expected:    req(addrNew, hdr4(24, 0xff), rtattr(local, v4), rtattr(address, v4)),
		},
		{
			description: "boundary: flags 0x100 spill between the lists, header 0 (row 6)",
			write:       base(func(w *AddrWrite) { w.IfaFlags = 0x100 }),
			expected:    req(addrNew, hdr4(24, 0), rtattr(local, v4), rtattr(flagsAttr, le32(0x100)), rtattr(address, v4)),
		},
		{
			description: "boundary: a label's rta_len counts its NUL and is padded to 12 (row 7)",
			write:       base(func(w *AddrWrite) { w.Attrs = append(w.Attrs, AddrAttrString(label, "d0:1")) }),
			expected: req(addrNew, hdr4(24, 0), rtattr(local, v4),
				[]byte{9, 0, byte(label), 0, 'd', '0', ':', '1', 0, 0, 0, 0}, rtattr(address, v4)),
		},
		{
			description: "boundary: a /32 IPv4 prefix is carried in the header (row 8)",
			write:       base(func(w *AddrWrite) { w.Prefixlen = 32 }),
			expected:    req(addrNew, hdr4(32, 0), rtattr(local, v4), rtattr(address, v4)),
		},
		{
			description: "boundary: a /128 IPv6 prefix is carried in the header (row 8)",
			write: AddrWrite{MsgType: addrNew, Family: unix.AF_INET6, Prefixlen: 128, Index: addrD0,
				Attrs: []AddrAttr{AddrAttrBytes(local, addrIP("2001:db8::5"))}},
			expected: req(addrNew, EncodeIfAddrmsg(unix.AF_INET6, 128, 0, 0, addrD0), rtattr(local, addrIP("2001:db8::5"))),
		},
		{
			description: "boundary: a 15-character label, the policy maximum, is rta_len 20 (row 9)",
			write:       base(func(w *AddrWrite) { w.PostAttrs, w.Attrs = nil, []AddrAttr{AddrAttrString(label, "d0:0123456789ab")} }),
			expected:    req(addrNew, hdr4(24, 0), rtattr(label, nul("d0:0123456789ab"))),
		},
		{
			description: "boundary: attributes filling exactly the 256-byte buffer build",
			write:       base(func(w *AddrWrite) { w.PostAttrs, w.Attrs = nil, []AddrAttr{AddrAttrString(label, long(251))} }),
			expected:    req(addrNew, hdr4(24, 0), rtattr(label, nul(long(251)))),
		},
		{
			description: "negative: one attribute byte past the 256-byte buffer is refused (row 11)",
			write:       base(func(w *AddrWrite) { w.PostAttrs, w.Attrs = nil, []AddrAttr{AddrAttrString(label, long(252))} }),
			wantErr:     ErrAttrNoSpace,
		},
		{
			description: "negative: a route type is refused (row 10)",
			write:       base(func(w *AddrWrite) { w.MsgType = uint16(unix.RTM_NEWROUTE) }),
			wantErr:     ErrAddrWriteType,
		},
		{
			description: "negative: RTM_GETADDR is a read, refused",
			write:       base(func(w *AddrWrite) { w.MsgType = uint16(unix.RTM_GETADDR) }),
			wantErr:     ErrAddrWriteType,
		},
		{
			description: "negative: an IFA_FLAGS in Attrs is refused",
			write:       base(func(w *AddrWrite) { w.Attrs = append(w.Attrs, AddrAttrU32(flagsAttr, 0x100)) }),
			wantErr:     ErrAddrWriteFlagsAttr,
		},
		{
			description: "negative: an IFA_FLAGS in PostAttrs is refused",
			write:       base(func(w *AddrWrite) { w.PostAttrs = append(w.PostAttrs, AddrAttrU32(flagsAttr, 0x2)) }),
			wantErr:     ErrAddrWriteFlagsAttr,
		},
		{
			description: "corner: a delete with the header only is 24 bytes (row 12)",
			write:       AddrWrite{MsgType: addrDel, Family: unix.AF_INET, Index: addrD0},
			expected:    req(addrDel, EncodeIfAddrmsg(unix.AF_INET, 0, 0, 0, addrD0)),
		},
		{
			description: "corner: attributes go out in the order given, not sorted (row 13)",
			write: base(func(w *AddrWrite) {
				w.PostAttrs = nil
				w.Attrs = []AddrAttr{AddrAttrBytes(broadcast, addrIP("192.0.2.255")), AddrAttrString(label, "d0:1"),
					AddrAttrBytes(local, v4)}
			}),
			expected: req(addrNew, hdr4(24, 0), rtattr(broadcast, addrIP("192.0.2.255")), rtattr(label, nul("d0:1")),
				rtattr(local, v4)),
		},
		{
			description: "corner: anycast is the address's raw bytes",
			write: AddrWrite{MsgType: addrNew, Family: unix.AF_INET6, Prefixlen: 64, Index: addrD0,
				Attrs: []AddrAttr{AddrAttrBytes(anycast, addrIP("2001:db8::1"))}},
			expected: req(addrNew, EncodeIfAddrmsg(unix.AF_INET6, 64, 0, 0, addrD0), rtattr(anycast, addrIP("2001:db8::1"))),
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := BuildAddrWriteRequest(tc.write, 7)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				if got != nil {
					t.Fatalf("bytes = % x with an error, want nil", got)
				}
				return
			}
			if !bytes.Equal(got, tc.expected) {
				t.Errorf("request =\n% x\nwant\n% x", got, tc.expected)
			}
		})
	}
}
