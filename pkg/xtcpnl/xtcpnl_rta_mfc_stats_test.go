package xtcpnl

import (
	"encoding/binary"
	"errors"
	"fmt"
	"testing"

	"golang.org/x/sys/unix"
)

// TestDeserializeRtaMfcStats drives the struct read on its own. The one
// captured RTA_MFC_STATS is 24 zero bytes, a fresh (S,G) entry nothing has
// crossed, so the positive rows set one member at a time with a distinct
// value: a transposed pair cannot hide behind zeros.
//
// go test ./pkg/xtcpnl/ -run TestDeserializeRtaMfcStats
func TestDeserializeRtaMfcStats(t *testing.T) {
	type result struct {
		stats RtaMfcStats
		n     int
		err   error
	}
	tests := []struct {
		description string
		data        []byte
		expected    result
	}{
		{"positive: all three members, in kernel field order", u64le(11, 22, 33),
			result{stats: RtaMfcStats{Packets: 11, Bytes: 22, WrongIf: 33}, n: 24}},
		{"positive: packets alone, offset 0", u64le(7, 0, 0), result{stats: RtaMfcStats{Packets: 7}, n: 24}},
		{"positive: bytes alone, offset 8", u64le(0, 7, 0), result{stats: RtaMfcStats{Bytes: 7}, n: 24}},
		{"positive: wrong_if alone, offset 16", u64le(0, 0, 7), result{stats: RtaMfcStats{WrongIf: 7}, n: 24}},
		{"boundary: all zero, as captured", make([]byte, 24), result{n: 24}},
		{"boundary: the u64 maximum survives, no truncation to u32", u64le(^uint64(0), 1<<32, 0),
			result{stats: RtaMfcStats{Packets: ^uint64(0), Bytes: 1 << 32}, n: 24}},
		{"corner: a longer payload reads 24 and ignores the rest", append(u64le(1, 2, 3), 0xff, 0xff, 0xff, 0xff),
			result{stats: RtaMfcStats{Packets: 1, Bytes: 2, WrongIf: 3}, n: 24}},
		{"negative: 23 bytes is too small", make([]byte, 23), result{err: ErrRtaMfcStatsSmall}},
		{"negative: empty", nil, result{err: ErrRtaMfcStatsSmall}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var got result
			got.n, got.err = DeserializeRtaMfcStats(tc.data, &got.stats)
			if !errors.Is(got.err, tc.expected.err) {
				t.Fatalf("err = %v, want %v", got.err, tc.expected.err)
			}
			if got.err != nil {
				got.err = tc.expected.err
			}
			if got != tc.expected {
				t.Errorf("got %+v, want %+v", got, tc.expected)
			}
		})
	}
}

// mcastRouteBody is a minimal RTNL_FAMILY_IPMR rtmsg followed by attrs.
func mcastRouteBody(attrs ...[]byte) []byte {
	b := []byte{RtnlFamilyIpmrCst, 32, 32, 0, unix.RT_TABLE_DEFAULT, 0x11, 0, unix.RTN_MULTICAST, 0, 0, 0, 0}
	for _, a := range attrs {
		b = append(b, a...)
	}
	return b
}

// mcastRta is one route attribute, padded to 4 bytes.
func mcastRta(typ uint16, val []byte) []byte {
	b := make([]byte, 4, 4+len(val)+3)
	binary.LittleEndian.PutUint16(b[0:2], uint16(4+len(val)))
	binary.LittleEndian.PutUint16(b[2:4], typ)
	b = append(b, val...)
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	return b
}

// TestParseNewRouteMcast is the two multicast attributes through
// ParseNewRoute: present, absent, and short.
//
// go test ./pkg/xtcpnl/ -run TestParseNewRouteMcast
func TestParseNewRouteMcast(t *testing.T) {
	type result struct {
		stats      string
		expires    uint64
		hasExpires bool
		err        error
	}
	tests := []struct {
		description string
		body        []byte
		expected    result
	}{
		{"positive: both, as a resolved entry carries them",
			mcastRouteBody(mcastRta(unix.RTA_MFC_STATS, u64le(5, 600, 2)), mcastRta(unix.RTA_EXPIRES, u64le(250))),
			result{stats: "{Packets:5 Bytes:600 WrongIf:2}", expires: 250, hasExpires: true}},
		{"positive: neither, as an unresolved entry", mcastRouteBody(), result{stats: "<nil>"}},
		{"boundary: RTA_EXPIRES of zero is present, not absent",
			mcastRouteBody(mcastRta(unix.RTA_EXPIRES, u64le(0))), result{stats: "<nil>", hasExpires: true}},
		{"corner: a 4-byte RTA_EXPIRES is suppressed, as a short scalar is",
			mcastRouteBody(mcastRta(unix.RTA_EXPIRES, []byte{1, 0, 0, 0})), result{stats: "<nil>"}},
		{"negative: a 16-byte RTA_MFC_STATS is an error",
			mcastRouteBody(mcastRta(unix.RTA_MFC_STATS, u64le(1, 2))), result{stats: "<nil>", err: ErrRtaMfcStatsSmall}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			ri, err := ParseNewRoute(tc.body)
			if !errors.Is(err, tc.expected.err) {
				t.Fatalf("err = %v, want %v", err, tc.expected.err)
			}
			if err != nil {
				return
			}
			got := result{stats: "<nil>", expires: ri.Expires, hasExpires: ri.HasExpires}
			if ri.MfcStats != nil {
				got.stats = fmt.Sprintf("%+v", *ri.MfcStats)
			}
			if got != tc.expected {
				t.Errorf("got %+v, want %+v", got, tc.expected)
			}
		})
	}
}

// TestParseNewRouteMcastCorpus decodes every RTNL_FAMILY_IPMR and
// RTNL_FAMILY_IP6MR reply in the committed corpus. There is one, the
// smcrouted (192.0.2.10,239.1.1.1) entry `ip mroute show` prints from
// netlink_route_getroute_mroute.pcap.
//
// go test ./pkg/xtcpnl/ -run TestParseNewRouteMcastCorpus
func TestParseNewRouteMcastCorpus(t *testing.T) {
	var got []string
	for _, c := range corpusAll(t) {
		m := c.m
		if m.proto != unix.NETLINK_ROUTE || m.h.Type != unix.RTM_NEWROUTE || m.h.Flags&unix.NLM_F_REQUEST != 0 ||
			len(m.body) == 0 || m.body[0] < RtnlFamilyIpmrCst {
			continue
		}
		ri, err := ParseNewRoute(m.body)
		if err != nil {
			t.Fatalf("%s: %v", c.path, err)
		}
		s := fmt.Sprintf("fam %d table %d type %d src %v dst %v iif %d", ri.Family, ri.Table, ri.Type,
			ri.Src, ri.Dst, ri.Iif)
		for _, nh := range ri.Multipath {
			s += fmt.Sprintf(" oif %d hops %d flags %d", nh.Ifindex, nh.Hops, nh.Flags)
		}
		if ri.MfcStats != nil {
			s += fmt.Sprintf(" stats %+v", *ri.MfcStats)
		}
		got = append(got, s+fmt.Sprintf(" expires %d/%v", ri.Expires, ri.HasExpires))
	}
	tests := []struct {
		description string
		got         string
		expected    string
	}{
		{"positive: exactly one multicast route in the corpus", fmt.Sprint(len(got)), "1"},
		{"positive: the smcrouted entry, every field ip mroute prints",
			fmt.Sprint(got),
			"[fam 128 table 253 type 5 src [192 0 2 10] dst [239 1 1 1] iif 11 oif 13 hops 1 flags 0" +
				" stats {Packets:0 Bytes:0 WrongIf:0} expires 50/true]"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if tc.got != tc.expected {
				t.Errorf("got %q, expected %q", tc.got, tc.expected)
			}
		})
	}
}
