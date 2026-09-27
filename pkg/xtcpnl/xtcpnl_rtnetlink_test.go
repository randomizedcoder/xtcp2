package xtcpnl

// WARNING: this file contains Go reflection (binary.Read / reflect).
//
// The reflection code here is only for performance comparison, and it is
// strongly recommended that it is NOT used in production. It lives in a
// _test.go file so that it never reaches the shipped library: pkg/xtcpnl
// ships zero reflection, and every production Deserialize* reads fields at
// fixed byte offsets instead.
//
// If reflection is ever measured as even close to a manual decoder, that
// indicates a problem rather than a license to use it. See
// xtcpnl_reflection_twins_test.go for the rationale and
// xtcpnl_perf_gate_test.go for the gate that fails on convergence.

import (
	"encoding/binary"
	"errors"
	"net/netip"
	"reflect"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// ---- byte builders -----------------------------------------------------------
//
// These synthesize rtnetlink wire bytes so the parser tests can express exact
// positive/negative/boundary/corner inputs without a live kernel. They mirror
// the layout DumpRtnetlink and the Parse* functions consume: a family header
// followed by 4-byte-aligned RTAttr TLVs, and (for the transport test) a
// 16-byte nlmsghdr in front of each message.

// rtattr encodes one RTAttr TLV (4-byte header + value) padded to 4 bytes.
func rtattr(atype uint16, val []byte) []byte {
	alen := RTAttrSizeCst + len(val)
	pad := FourByteAlignPadding(alen)
	b := make([]byte, alen+pad)
	binary.LittleEndian.PutUint16(b[0:2], uint16(alen))
	binary.LittleEndian.PutUint16(b[2:4], atype)
	copy(b[RTAttrSizeCst:], val)
	return b
}

// ifaddrmsgHdr encodes an 8-byte ifaddrmsg family header.
func ifaddrmsgHdr(family, prefixlen, flags, scope uint8, index uint32) []byte {
	b := make([]byte, IfAddrmsgSizeCst)
	b[0] = family
	b[1] = prefixlen
	b[2] = flags
	b[3] = scope
	binary.LittleEndian.PutUint32(b[4:8], index)
	return b
}

// rtmsgHdr encodes a 12-byte rtmsg family header.
func rtmsgHdr(family, dstLen, tos, table, protocol, scope, rtype uint8, flags uint32) []byte {
	b := make([]byte, RtMsgSizeCst)
	b[0] = family
	b[1] = dstLen
	// b[2] src_len = 0
	b[3] = tos
	b[4] = table
	b[5] = protocol
	b[6] = scope
	b[7] = rtype
	binary.LittleEndian.PutUint32(b[8:12], flags)
	return b
}

// ifinfomsgHdr encodes a 16-byte ifinfomsg family header.
func ifinfomsgHdr(family uint8, itype uint16, index int32, flags uint32) []byte {
	b := make([]byte, IfInfomsgSizeCst)
	b[0] = family
	binary.LittleEndian.PutUint16(b[2:4], itype)
	binary.LittleEndian.PutUint32(b[4:8], uint32(index))
	binary.LittleEndian.PutUint32(b[8:12], flags)
	return b
}

func v4b(a, b, c, d byte) []byte { return []byte{a, b, c, d} }

// ---- deserializer tests ------------------------------------------------------

// TestDeserializeIfAddrmsg checks the 8-byte ifaddrmsg header decoder against
// both the manual and reflection implementations, with positive/boundary/corner
// rows.
//
// go test ./pkg/xtcpnl/ -run TestDeserializeIfAddrmsg
func TestDeserializeIfAddrmsg(t *testing.T) {
	tests := []struct {
		description string
		data        []byte
		want        IfAddrmsg
		wantErr     error
	}{
		{
			description: "positive: IPv4 /24 scope-universe on ifindex 2",
			data:        ifaddrmsgHdr(unix.AF_INET, 24, 0, unix.RT_SCOPE_UNIVERSE, 2),
			want:        IfAddrmsg{Family: unix.AF_INET, Prefixlen: 24, Scope: unix.RT_SCOPE_UNIVERSE, Index: 2},
		},
		{
			description: "boundary: /0 prefix, ifindex 0",
			data:        ifaddrmsgHdr(unix.AF_INET6, 0, 0, 0, 0),
			want:        IfAddrmsg{Family: unix.AF_INET6},
		},
		{
			description: "boundary: trailing attribute bytes are ignored by the header decoder",
			data:        append(ifaddrmsgHdr(unix.AF_INET, 32, 0, unix.RT_SCOPE_HOST, 1), 0xde, 0xad),
			want:        IfAddrmsg{Family: unix.AF_INET, Prefixlen: 32, Scope: unix.RT_SCOPE_HOST, Index: 1},
		},
		{
			description: "corner: one byte short -> ErrIfAddrmsgSmall",
			data:        make([]byte, IfAddrmsgSizeCst-1),
			wantErr:     ErrIfAddrmsgSmall,
		},
		{
			description: "corner: empty input -> ErrIfAddrmsgSmall",
			data:        nil,
			wantErr:     ErrIfAddrmsgSmall,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var manual, refl IfAddrmsg
			_, errM := DeserializeIfAddrmsg(tc.data, &manual)
			if !errors.Is(errM, tc.wantErr) {
				t.Fatalf("manual err = %v, want %v", errM, tc.wantErr)
			}
			if tc.wantErr != nil {
				return
			}
			if manual != tc.want {
				t.Errorf("manual = %+v, want %+v", manual, tc.want)
			}
			// The reflection decoder needs exactly the struct's worth of bytes.
			if _, errR := deserializeIfAddrmsgReflection(tc.data[:IfAddrmsgSizeCst], &refl); errR != nil {
				t.Fatalf("reflection err = %v", errR)
			}
			if refl != tc.want {
				t.Errorf("reflection = %+v, want %+v", refl, tc.want)
			}
		})
	}
}

// TestDeserializeRtMsg checks the 12-byte rtmsg header decoder.
//
// go test ./pkg/xtcpnl/ -run TestDeserializeRtMsg
func TestDeserializeRtMsg(t *testing.T) {
	tests := []struct {
		description string
		data        []byte
		want        RtMsg
		wantErr     error
	}{
		{
			description: "positive: connected /24 unicast scope-link in main table",
			data:        rtmsgHdr(unix.AF_INET, 24, 0, unix.RT_TABLE_MAIN, unix.RTPROT_KERNEL, unix.RT_SCOPE_LINK, unix.RTN_UNICAST, 0),
			want: RtMsg{
				Family: unix.AF_INET, DstLen: 24, Table: unix.RT_TABLE_MAIN,
				Protocol: unix.RTPROT_KERNEL, Scope: unix.RT_SCOPE_LINK, Type: unix.RTN_UNICAST,
			},
		},
		{
			description: "positive: local host route /32 scope-host in local table",
			data:        rtmsgHdr(unix.AF_INET, 32, 0, unix.RT_TABLE_LOCAL, unix.RTPROT_KERNEL, unix.RT_SCOPE_HOST, unix.RTN_LOCAL, 0),
			want: RtMsg{
				Family: unix.AF_INET, DstLen: 32, Table: unix.RT_TABLE_LOCAL,
				Protocol: unix.RTPROT_KERNEL, Scope: unix.RT_SCOPE_HOST, Type: unix.RTN_LOCAL,
			},
		},
		{
			description: "boundary: all-zero header decodes to the zero value",
			data:        make([]byte, RtMsgSizeCst),
			want:        RtMsg{},
		},
		{
			description: "corner: one byte short -> ErrRtMsgSmall",
			data:        make([]byte, RtMsgSizeCst-1),
			wantErr:     ErrRtMsgSmall,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var manual, refl RtMsg
			_, errM := DeserializeRtMsg(tc.data, &manual)
			if !errors.Is(errM, tc.wantErr) {
				t.Fatalf("manual err = %v, want %v", errM, tc.wantErr)
			}
			if tc.wantErr != nil {
				return
			}
			if manual != tc.want {
				t.Errorf("manual = %+v, want %+v", manual, tc.want)
			}
			if _, errR := deserializeRtMsgReflection(tc.data[:RtMsgSizeCst], &refl); errR != nil {
				t.Fatalf("reflection err = %v", errR)
			}
			if refl != tc.want {
				t.Errorf("reflection = %+v, want %+v", refl, tc.want)
			}
		})
	}
}

// TestDeserializeIfInfomsg checks the 16-byte ifinfomsg header decoder.
//
// go test ./pkg/xtcpnl/ -run TestDeserializeIfInfomsg
func TestDeserializeIfInfomsg(t *testing.T) {
	tests := []struct {
		description string
		data        []byte
		want        IfInfomsg
		wantErr     error
	}{
		{
			description: "positive: ethernet link index 2, IFF_UP",
			data:        ifinfomsgHdr(unix.AF_UNSPEC, 1 /*ARPHRD_ETHER*/, 2, unix.IFF_UP),
			want:        IfInfomsg{Family: unix.AF_UNSPEC, Type: 1, Index: 2, Flags: unix.IFF_UP},
		},
		{
			description: "boundary: loopback index 1",
			data:        ifinfomsgHdr(unix.AF_UNSPEC, 772 /*ARPHRD_LOOPBACK*/, 1, unix.IFF_UP|unix.IFF_LOOPBACK),
			want:        IfInfomsg{Family: unix.AF_UNSPEC, Type: 772, Index: 1, Flags: unix.IFF_UP | unix.IFF_LOOPBACK},
		},
		{
			description: "corner: one byte short -> ErrIfInfomsgSmall",
			data:        make([]byte, IfInfomsgSizeCst-1),
			wantErr:     ErrIfInfomsgSmall,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var manual, refl IfInfomsg
			_, errM := DeserializeIfInfomsg(tc.data, &manual)
			if !errors.Is(errM, tc.wantErr) {
				t.Fatalf("manual err = %v, want %v", errM, tc.wantErr)
			}
			if tc.wantErr != nil {
				return
			}
			if manual != tc.want {
				t.Errorf("manual = %+v, want %+v", manual, tc.want)
			}
			if _, errR := deserializeIfInfomsgReflection(tc.data[:IfInfomsgSizeCst], &refl); errR != nil {
				t.Fatalf("reflection err = %v", errR)
			}
			if refl != tc.want {
				t.Errorf("reflection = %+v, want %+v", refl, tc.want)
			}
		})
	}
}

// ---- parser tests ------------------------------------------------------------

// TestParseNewAddr decodes RTM_NEWADDR bodies (ifaddrmsg + IFA_* attributes).
//
// go test ./pkg/xtcpnl/ -run TestParseNewAddr
func TestParseNewAddr(t *testing.T) {
	tests := []struct {
		description string
		body        []byte
		want        AddrInfo
		wantErr     bool
	}{
		{
			description: "positive: IPv4 with IFA_ADDRESS, IFA_LOCAL and IFA_LABEL",
			body: concat(
				ifaddrmsgHdr(unix.AF_INET, 24, 0, unix.RT_SCOPE_UNIVERSE, 2),
				rtattr(unix.IFA_ADDRESS, v4b(10, 0, 0, 5)),
				rtattr(unix.IFA_LOCAL, v4b(10, 0, 0, 5)),
				rtattr(unix.IFA_LABEL, append([]byte("eth0"), 0)),
			),
			want: AddrInfo{
				Family: unix.AF_INET, Prefixlen: 24, Scope: unix.RT_SCOPE_UNIVERSE, Index: 2,
				Address: v4b(10, 0, 0, 5), Local: v4b(10, 0, 0, 5), Label: "eth0",
			},
		},
		{
			// IPv6 replies carry no IFA_LOCAL — all 15 in the committed v6 dump
			// omit it — and iproute2 aliases the two attributes onto each other
			// when either is missing (ip/ipaddress.c:1531-1534). Without that,
			// every IPv6 address would decode with an empty Local and a
			// renderer reading Local would print nothing at all.
			description: "positive: IPv6 /64 with only IFA_ADDRESS aliases it into Local",
			body: concat(
				ifaddrmsgHdr(unix.AF_INET6, 64, 0, unix.RT_SCOPE_UNIVERSE, 2),
				rtattr(unix.IFA_ADDRESS, mustV6(t, "2001:db8::5")),
			),
			want: AddrInfo{
				Family: unix.AF_INET6, Prefixlen: 64, Scope: unix.RT_SCOPE_UNIVERSE, Index: 2,
				Address: mustV6(t, "2001:db8::5"), Local: mustV6(t, "2001:db8::5"),
			},
		},
		{
			// The alias runs the other way too, which matters less in practice
			// but is the same two lines of C.
			description: "boundary: only IFA_LOCAL aliases it into Address",
			body: concat(
				ifaddrmsgHdr(unix.AF_INET, 32, 0, unix.RT_SCOPE_HOST, 1),
				rtattr(unix.IFA_LOCAL, v4b(127, 0, 0, 1)),
			),
			want: AddrInfo{
				Family: unix.AF_INET, Prefixlen: 32, Scope: unix.RT_SCOPE_HOST, Index: 1,
				Address: v4b(127, 0, 0, 1), Local: v4b(127, 0, 0, 1),
			},
		},
		{
			description: "positive: point-to-point IPv4 where IFA_LOCAL differs from IFA_ADDRESS (peer)",
			body: concat(
				ifaddrmsgHdr(unix.AF_INET, 32, 0, unix.RT_SCOPE_UNIVERSE, 3),
				rtattr(unix.IFA_LOCAL, v4b(10, 8, 0, 1)),
				rtattr(unix.IFA_ADDRESS, v4b(10, 8, 0, 2)),
			),
			want: AddrInfo{
				Family: unix.AF_INET, Prefixlen: 32, Scope: unix.RT_SCOPE_UNIVERSE, Index: 3,
				Local: v4b(10, 8, 0, 1), Address: v4b(10, 8, 0, 2),
			},
		},
		{
			description: "boundary: header only, no attributes",
			body:        ifaddrmsgHdr(unix.AF_INET, 32, 0, unix.RT_SCOPE_HOST, 1),
			want:        AddrInfo{Family: unix.AF_INET, Prefixlen: 32, Scope: unix.RT_SCOPE_HOST, Index: 1},
		},
		{
			// IFA_ANYCAST is a real attribute this package does not decode, so
			// it stands in for "unknown" without pretending a decoded one is
			// unknown. (This row used to use IFA_FLAGS, which is now decoded.)
			description: "corner: undecoded attribute types are ignored",
			body: concat(
				ifaddrmsgHdr(unix.AF_INET, 24, 0, unix.RT_SCOPE_UNIVERSE, 2),
				rtattr(unix.IFA_ANYCAST, v4b(192, 168, 1, 255)),
				rtattr(unix.IFA_ADDRESS, v4b(192, 168, 1, 2)),
			),
			want: AddrInfo{
				Family: unix.AF_INET, Prefixlen: 24, Scope: unix.RT_SCOPE_UNIVERSE, Index: 2,
				Address: v4b(192, 168, 1, 2), Local: v4b(192, 168, 1, 2),
			},
		},
		{
			description: "corner: truncated ifaddrmsg header -> error",
			body:        make([]byte, IfAddrmsgSizeCst-1),
			wantErr:     true,
		},
		{
			description: "corner: attribute length below the 4-byte header -> error",
			body: concat(
				ifaddrmsgHdr(unix.AF_INET, 24, 0, unix.RT_SCOPE_UNIVERSE, 2),
				[]byte{0x02, 0x00, byte(unix.IFA_ADDRESS), 0x00}, // alen=2 (< RTAttrSizeCst)
			),
			wantErr: true,
		},

		// ---- IFA_FLAGS, IFA_CACHEINFO, IFA_BROADCAST, IFA_PROTO -----------
		//
		// The positives for all four are in TestParseNewAddrRealFixtures,
		// since every one appears in the committed dumps. Constructed here:
		// the flag combinations the capture host happens not to have, and the
		// malformed lengths a kernel never sends.
		{
			// **The row the 8-bit header field cannot express.** ifa_flags in
			// the header is a byte; IFA_F_MANAGETEMPADDR is 0x100 and
			// IFA_F_NOPREFIXROUTE is 0x200, so a decoder reading only the
			// header sees 0 here and renders none of it. ip_addr_n:16 is a real
			// address printing "dynamic mngtmpaddr noprefixroute".
			description: "positive: IFA_FLAGS carries mngtmpaddr|noprefixroute, invisible to the header byte",
			body: concat(
				ifaddrmsgHdr(unix.AF_INET6, 64, 0, unix.RT_SCOPE_UNIVERSE, 2),
				rtattr(unix.IFA_ADDRESS, mustV6(t, "2001:db8::1")),
				rtattr(unix.IFA_FLAGS, le32(unix.IFA_F_MANAGETEMPADDR|unix.IFA_F_NOPREFIXROUTE)),
			),
			want: AddrInfo{
				Family: unix.AF_INET6, Prefixlen: 64, Scope: unix.RT_SCOPE_UNIVERSE, Index: 2,
				Address: mustV6(t, "2001:db8::1"), Local: mustV6(t, "2001:db8::1"),
				Flags: unix.IFA_F_MANAGETEMPADDR | unix.IFA_F_NOPREFIXROUTE,
			},
		},
		{
			// IFA_FLAGS REPLACES the header byte, it does not extend it —
			// get_ifa_flags is a ternary, not an OR (ip/ipaddress.c:1371-1376).
			// So IFA_F_PERMANENT in the header is *lost* when the attribute
			// omits it, and reproducing that is the whole point: OR-ing would
			// make goip print "forever"-style permanence `ip` does not.
			description: "boundary: IFA_FLAGS replaces the header byte rather than OR-ing with it",
			body: concat(
				ifaddrmsgHdr(unix.AF_INET, 24, unix.IFA_F_PERMANENT, unix.RT_SCOPE_UNIVERSE, 2),
				rtattr(unix.IFA_ADDRESS, v4b(10, 0, 0, 1)),
				rtattr(unix.IFA_FLAGS, le32(unix.IFA_F_NOPREFIXROUTE)),
			),
			want: AddrInfo{
				Family: unix.AF_INET, Prefixlen: 24, Scope: unix.RT_SCOPE_UNIVERSE, Index: 2,
				Address: v4b(10, 0, 0, 1), Local: v4b(10, 0, 0, 1),
				Flags: unix.IFA_F_NOPREFIXROUTE,
			},
		},
		{
			// With no IFA_FLAGS the header byte is all there is, and it must
			// still reach Flags — otherwise an old kernel's replies lose every
			// flag they do carry.
			description: "boundary: no IFA_FLAGS leaves the header's ifa_flags byte in place",
			body: concat(
				ifaddrmsgHdr(unix.AF_INET, 8, unix.IFA_F_PERMANENT, unix.RT_SCOPE_HOST, 1),
				rtattr(unix.IFA_ADDRESS, v4b(127, 0, 0, 1)),
			),
			want: AddrInfo{
				Family: unix.AF_INET, Prefixlen: 8, Scope: unix.RT_SCOPE_HOST, Index: 1,
				Address: v4b(127, 0, 0, 1), Local: v4b(127, 0, 0, 1),
				Flags: unix.IFA_F_PERMANENT,
			},
		},
		{
			description: "negative: 3-byte IFA_FLAGS is ignored, leaving the header byte",
			body: concat(
				ifaddrmsgHdr(unix.AF_INET, 8, unix.IFA_F_PERMANENT, unix.RT_SCOPE_HOST, 1),
				rtattr(unix.IFA_FLAGS, []byte{0x00, 0x02, 0x00}),
			),
			want: AddrInfo{
				Family: unix.AF_INET, Prefixlen: 8, Scope: unix.RT_SCOPE_HOST, Index: 1,
				Flags: unix.IFA_F_PERMANENT,
			},
		},
		{
			description: "positive: IFA_CACHEINFO decodes all four u32 fields",
			body: concat(
				ifaddrmsgHdr(unix.AF_INET, 24, 0, unix.RT_SCOPE_UNIVERSE, 2),
				rtattr(unix.IFA_CACHEINFO, concat(le32(3600), le32(7200), le32(11), le32(22))),
			),
			want: AddrInfo{
				Family: unix.AF_INET, Prefixlen: 24, Scope: unix.RT_SCOPE_UNIVERSE, Index: 2,
				HasCacheInfo: true,
				CacheInfo: IfaCacheinfo{
					Preferred: 3600, Valid: 7200, Cstamp: 11, Tstamp: 22,
				},
			},
		},
		{
			// The plan left "IFA_CACHEINFO of 15 bytes" explicitly
			// unspecified. It is specified here: a short cacheinfo is dropped,
			// HasCacheInfo stays false, and the rest of the message still
			// decodes — the same tolerance a short IFA_FLAGS gets. Failing the
			// whole address over an unusable lifetime would lose the address.
			description: "boundary: 15-byte IFA_CACHEINFO is dropped, leaving HasCacheInfo false",
			body: concat(
				ifaddrmsgHdr(unix.AF_INET, 24, 0, unix.RT_SCOPE_UNIVERSE, 2),
				rtattr(unix.IFA_ADDRESS, v4b(10, 0, 0, 9)),
				rtattr(unix.IFA_CACHEINFO, make([]byte, IfaCacheinfoSizeCst-1)),
			),
			want: AddrInfo{
				Family: unix.AF_INET, Prefixlen: 24, Scope: unix.RT_SCOPE_UNIVERSE, Index: 2,
				Address: v4b(10, 0, 0, 9), Local: v4b(10, 0, 0, 9),
			},
		},
		{
			// A longer cacheinfo is accepted and the extra bytes ignored, which
			// is what a forward-compatible reader must do if the struct grows.
			description: "boundary: a 20-byte IFA_CACHEINFO decodes its first 16 bytes",
			body: concat(
				ifaddrmsgHdr(unix.AF_INET, 24, 0, unix.RT_SCOPE_UNIVERSE, 2),
				rtattr(unix.IFA_CACHEINFO, concat(le32(1), le32(2), le32(3), le32(4), le32(5))),
			),
			want: AddrInfo{
				Family: unix.AF_INET, Prefixlen: 24, Scope: unix.RT_SCOPE_UNIVERSE, Index: 2,
				HasCacheInfo: true,
				CacheInfo:    IfaCacheinfo{Preferred: 1, Valid: 2, Cstamp: 3, Tstamp: 4},
			},
		},
		{
			description: "positive: IFA_BROADCAST and IFA_PROTO both decode",
			body: concat(
				ifaddrmsgHdr(unix.AF_INET, 24, 0, unix.RT_SCOPE_UNIVERSE, 2),
				rtattr(unix.IFA_ADDRESS, v4b(172, 16, 50, 219)),
				rtattr(unix.IFA_BROADCAST, v4b(172, 16, 50, 255)),
				rtattr(IfaProto, []byte{IfaProtoKernelLL}),
			),
			want: AddrInfo{
				Family: unix.AF_INET, Prefixlen: 24, Scope: unix.RT_SCOPE_UNIVERSE, Index: 2,
				Address: v4b(172, 16, 50, 219), Local: v4b(172, 16, 50, 219),
				Broadcast: v4b(172, 16, 50, 255), Proto: IfaProtoKernelLL,
			},
		},
		{
			description: "corner: duplicate IFA_FLAGS takes the first, as parse_rtattr does",
			body: concat(
				ifaddrmsgHdr(unix.AF_INET, 24, 0, unix.RT_SCOPE_UNIVERSE, 2),
				rtattr(unix.IFA_FLAGS, le32(unix.IFA_F_TEMPORARY)),
				rtattr(unix.IFA_FLAGS, le32(unix.IFA_F_DEPRECATED)),
			),
			want: AddrInfo{
				Family: unix.AF_INET, Prefixlen: 24, Scope: unix.RT_SCOPE_UNIVERSE, Index: 2,
				Flags: unix.IFA_F_TEMPORARY,
			},
		},
		{
			// ifa_prefixlen is decoded verbatim; nothing here range-checks it
			// against the family, because the kernel is the authority on what it
			// sent and a renderer flagging "/33" is more useful than a parser
			// refusing the message.
			description: "corner: ifa_prefixlen of 33 on AF_INET decodes verbatim",
			body: concat(
				ifaddrmsgHdr(unix.AF_INET, 33, 0, unix.RT_SCOPE_UNIVERSE, 2),
				rtattr(unix.IFA_ADDRESS, v4b(10, 0, 0, 1)),
			),
			want: AddrInfo{
				Family: unix.AF_INET, Prefixlen: 33, Scope: unix.RT_SCOPE_UNIVERSE, Index: 2,
				Address: v4b(10, 0, 0, 1), Local: v4b(10, 0, 0, 1),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := ParseNewAddr(tc.body)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ParseNewAddr = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestParseNewRoute decodes RTM_NEWROUTE bodies (rtmsg + RTA_* attributes).
//
// go test ./pkg/xtcpnl/ -run TestParseNewRoute
func TestParseNewRoute(t *testing.T) {
	tests := []struct {
		description string
		body        []byte
		want        RouteInfo
		wantErr     bool
	}{
		{
			description: "positive: connected /24 (scope-link, no gateway) with PREFSRC and OIF",
			body: concat(
				rtmsgHdr(unix.AF_INET, 24, 0, unix.RT_TABLE_MAIN, unix.RTPROT_KERNEL, unix.RT_SCOPE_LINK, unix.RTN_UNICAST, 0),
				rtattr(unix.RTA_DST, v4b(10, 0, 0, 0)),
				rtattr(unix.RTA_PREFSRC, v4b(10, 0, 0, 5)),
				rtattr(unix.RTA_OIF, le32(2)),
			),
			want: RouteInfo{
				Family: unix.AF_INET, DstLen: 24, Table: unix.RT_TABLE_MAIN,
				Scope: unix.RT_SCOPE_LINK, Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_KERNEL,
				Dst: v4b(10, 0, 0, 0), PrefSrc: v4b(10, 0, 0, 5), Oif: 2,
			},
		},
		{
			description: "positive: default route via gateway with RTA_PRIORITY",
			body: concat(
				rtmsgHdr(unix.AF_INET, 0, 0, unix.RT_TABLE_MAIN, unix.RTPROT_BOOT, unix.RT_SCOPE_UNIVERSE, unix.RTN_UNICAST, 0),
				rtattr(unix.RTA_GATEWAY, v4b(10, 0, 0, 1)),
				rtattr(unix.RTA_OIF, le32(2)),
				rtattr(unix.RTA_PRIORITY, le32(100)),
			),
			want: RouteInfo{
				Family: unix.AF_INET, DstLen: 0, Table: unix.RT_TABLE_MAIN,
				Scope: unix.RT_SCOPE_UNIVERSE, Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_BOOT,
				Gateway: v4b(10, 0, 0, 1), Oif: 2, Priority: 100,
			},
		},
		{
			description: "positive: RTA_TABLE upgrades the 8-bit header table id",
			body: concat(
				rtmsgHdr(unix.AF_INET, 32, 0, 0 /*RT_TABLE_UNSPEC in header*/, unix.RTPROT_KERNEL, unix.RT_SCOPE_HOST, unix.RTN_LOCAL, 0),
				rtattr(unix.RTA_DST, v4b(172, 16, 0, 1)),
				rtattr(unix.RTA_TABLE, le32(unix.RT_TABLE_LOCAL)),
			),
			want: RouteInfo{
				Family: unix.AF_INET, DstLen: 32, Table: unix.RT_TABLE_LOCAL,
				Scope: unix.RT_SCOPE_HOST, Type: unix.RTN_LOCAL, Protocol: unix.RTPROT_KERNEL,
				Dst: v4b(172, 16, 0, 1),
			},
		},
		{
			description: "positive: IPv6 connected /64",
			body: concat(
				rtmsgHdr(unix.AF_INET6, 64, 0, unix.RT_TABLE_MAIN, unix.RTPROT_KERNEL, unix.RT_SCOPE_LINK, unix.RTN_UNICAST, 0),
				rtattr(unix.RTA_DST, mustV6(t, "2001:db8::")),
				rtattr(unix.RTA_OIF, le32(2)),
			),
			want: RouteInfo{
				Family: unix.AF_INET6, DstLen: 64, Table: unix.RT_TABLE_MAIN,
				Scope: unix.RT_SCOPE_LINK, Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_KERNEL,
				Dst: mustV6(t, "2001:db8::"), Oif: 2,
			},
		},
		{
			description: "boundary: rtmsg header with no attributes",
			body:        rtmsgHdr(unix.AF_INET, 0, 0, unix.RT_TABLE_MAIN, 0, 0, unix.RTN_UNICAST, 0),
			want:        RouteInfo{Family: unix.AF_INET, Table: unix.RT_TABLE_MAIN, Type: unix.RTN_UNICAST},
		},
		{
			description: "corner: short RTA_OIF (2 bytes) is ignored, leaving Oif zero",
			body: concat(
				rtmsgHdr(unix.AF_INET, 24, 0, unix.RT_TABLE_MAIN, 0, unix.RT_SCOPE_LINK, unix.RTN_UNICAST, 0),
				rtattr(unix.RTA_DST, v4b(10, 0, 0, 0)),
				rtattr(unix.RTA_OIF, []byte{0x02, 0x00}),
			),
			want: RouteInfo{
				Family: unix.AF_INET, DstLen: 24, Table: unix.RT_TABLE_MAIN,
				Scope: unix.RT_SCOPE_LINK, Type: unix.RTN_UNICAST, Dst: v4b(10, 0, 0, 0),
			},
		},
		{
			// An ECMP route carries its per-path gateways/interfaces inside
			// RTA_MULTIPATH and has no top-level RTA_GATEWAY/RTA_OIF. Only the
			// presence is recorded; the nested nexthops are not parsed.
			description: "positive: ECMP route flags HasMultipath (RTA_MULTIPATH present, no RTA_GATEWAY)",
			body: concat(
				rtmsgHdr(unix.AF_INET, 16, 0, unix.RT_TABLE_MAIN, unix.RTPROT_BOOT, unix.RT_SCOPE_UNIVERSE, unix.RTN_UNICAST, 0),
				rtattr(unix.RTA_DST, v4b(10, 20, 0, 0)),
				rtattr(unix.RTA_MULTIPATH, make([]byte, 16)), // two opaque rtnexthop blobs
			),
			want: RouteInfo{
				Family: unix.AF_INET, DstLen: 16, Table: unix.RT_TABLE_MAIN,
				Scope: unix.RT_SCOPE_UNIVERSE, Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_BOOT,
				Dst: v4b(10, 20, 0, 0), HasMultipath: true,
			},
		},
		{
			description: "positive: IPv4 route via an IPv6 gateway flags HasVia (RTA_VIA, no RTA_GATEWAY)",
			body: concat(
				rtmsgHdr(unix.AF_INET, 16, 0, unix.RT_TABLE_MAIN, unix.RTPROT_BOOT, unix.RT_SCOPE_UNIVERSE, unix.RTN_UNICAST, 0),
				rtattr(unix.RTA_DST, v4b(10, 30, 0, 0)),
				rtattr(unix.RTA_VIA, append([]byte{byte(unix.AF_INET6), 0}, mustV6(t, "fe80::1")...)), // struct rtvia{family, addr}
				rtattr(unix.RTA_OIF, le32(2)),
			),
			want: RouteInfo{
				Family: unix.AF_INET, DstLen: 16, Table: unix.RT_TABLE_MAIN,
				Scope: unix.RT_SCOPE_UNIVERSE, Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_BOOT,
				Dst: v4b(10, 30, 0, 0), Oif: 2, HasVia: true,
			},
		},
		{
			description: "positive: route pointing at a nexthop object carries NhID (RTA_NH_ID, no RTA_GATEWAY/RTA_OIF)",
			body: concat(
				rtmsgHdr(unix.AF_INET, 16, 0, unix.RT_TABLE_MAIN, unix.RTPROT_BOOT, unix.RT_SCOPE_UNIVERSE, unix.RTN_UNICAST, 0),
				rtattr(unix.RTA_DST, v4b(10, 40, 0, 0)),
				rtattr(RtaNhID, le32(5)),
			),
			want: RouteInfo{
				Family: unix.AF_INET, DstLen: 16, Table: unix.RT_TABLE_MAIN,
				Scope: unix.RT_SCOPE_UNIVERSE, Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_BOOT,
				Dst: v4b(10, 40, 0, 0), NhID: 5,
			},
		},
		{
			description: "corner: short RTA_NH_ID (2 bytes) is ignored, leaving NhID zero",
			body: concat(
				rtmsgHdr(unix.AF_INET, 16, 0, unix.RT_TABLE_MAIN, 0, unix.RT_SCOPE_UNIVERSE, unix.RTN_UNICAST, 0),
				rtattr(unix.RTA_DST, v4b(10, 40, 0, 0)),
				rtattr(RtaNhID, []byte{0x05, 0x00}),
			),
			want: RouteInfo{
				Family: unix.AF_INET, DstLen: 16, Table: unix.RT_TABLE_MAIN,
				Scope: unix.RT_SCOPE_UNIVERSE, Type: unix.RTN_UNICAST, Dst: v4b(10, 40, 0, 0),
			},
		},
		{
			description: "corner: truncated rtmsg header -> error",
			body:        make([]byte, RtMsgSizeCst-1),
			wantErr:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := ParseNewRoute(tc.body)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ParseNewRoute = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestParseNewLink decodes RTM_NEWLINK bodies (ifinfomsg + IFLA_IFNAME).
//
// go test ./pkg/xtcpnl/ -run TestParseNewLink
func TestParseNewLink(t *testing.T) {
	tests := []struct {
		description string
		body        []byte
		want        LinkInfo
		wantErr     bool
	}{
		{
			description: "positive: eth0 with IFLA_IFNAME",
			body: concat(
				ifinfomsgHdr(unix.AF_UNSPEC, 1, 2, unix.IFF_UP),
				rtattr(unix.IFLA_IFNAME, append([]byte("eth0"), 0)),
			),
			want: LinkInfo{Index: 2, Flags: unix.IFF_UP, Name: "eth0", Type: 1},
		},
		{
			description: "boundary: loopback with no IFLA_IFNAME",
			body:        ifinfomsgHdr(unix.AF_UNSPEC, 772, 1, unix.IFF_UP|unix.IFF_LOOPBACK),
			want:        LinkInfo{Index: 1, Flags: unix.IFF_UP | unix.IFF_LOOPBACK, Type: 772},
		},
		{
			description: "corner: unknown IFLA attributes ignored, name and MTU still extracted",
			body: concat(
				ifinfomsgHdr(unix.AF_UNSPEC, 1, 5, unix.IFF_UP),
				rtattr(unix.IFLA_MTU, le32(1500)),
				rtattr(unix.IFLA_IFNAME, append([]byte("wg0"), 0)),
			),
			want: LinkInfo{Index: 5, Flags: unix.IFF_UP, Name: "wg0", Type: 1, MTU: 1500},
		},
		{
			description: "corner: truncated ifinfomsg header -> error",
			body:        make([]byte, IfInfomsgSizeCst-1),
			wantErr:     true,
		},

		// ---- the attributes `ip link show` renders ------------------------
		//
		// The real-fixture table (TestParseNewLinkRealFixture) owns the
		// positive assertions for these, since every one of them appears in
		// the committed 7.1.8 dump. What is constructed here is the shapes a
		// real kernel does not produce: wrong lengths, duplicates, malformed
		// nests.
		{
			// `ip` does not assume 6 bytes. InfiniBand hardware addresses are
			// 20, and ll_addr_n2a's generic path prints all of them — so a
			// decoder that copied into a [6]byte, or sliced val[:6], would
			// silently drop 14 bytes of a real address. No fixture: the capture
			// host has no IB device.
			description: "boundary: 20-byte IFLA_ADDRESS (InfiniBand) is retained whole",
			body: concat(
				ifinfomsgHdr(unix.AF_UNSPEC, unix.ARPHRD_INFINIBAND, 6, unix.IFF_UP),
				rtattr(unix.IFLA_ADDRESS, []byte{
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09,
					0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13,
				}),
			),
			want: LinkInfo{
				Index: 6, Flags: unix.IFF_UP, Type: unix.ARPHRD_INFINIBAND,
				Address: []byte{
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09,
					0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13,
				},
			},
		},
		{
			// A zero-length attribute is well-formed: rta_len == 4, no payload.
			// CopyBytes maps it to nil, which is the same value an absent
			// IFLA_ADDRESS produces — so "present but empty" and "absent" are
			// deliberately indistinguishable, because `ip` renders both as
			// nothing after "link/".
			description: "boundary: zero-length IFLA_ADDRESS decodes to nil, like an absent one",
			body: concat(
				ifinfomsgHdr(unix.AF_UNSPEC, unix.ARPHRD_NETLINK, 7, unix.IFF_UP),
				rtattr(unix.IFLA_ADDRESS, nil),
			),
			want: LinkInfo{Index: 7, Flags: unix.IFF_UP, Type: unix.ARPHRD_NETLINK},
		},
		{
			// The kernel NUL-terminates IFLA_IFNAME, but nothing in the wire
			// format requires it, and TrimRight on a payload with no NUL must
			// leave the name intact rather than losing its last byte.
			description: "boundary: IFLA_IFNAME with no trailing NUL keeps the whole payload",
			body: concat(
				ifinfomsgHdr(unix.AF_UNSPEC, unix.ARPHRD_ETHER, 8, unix.IFF_UP),
				rtattr(unix.IFLA_IFNAME, []byte("eth9")),
			),
			want: LinkInfo{
				Index: 8, Flags: unix.IFF_UP, Type: unix.ARPHRD_ETHER, Name: "eth9",
			},
		},
		{
			// -1 is a value the kernel really sends: "the peer is in a netns I
			// cannot name", which `ip` prints as "link-netnsid unknown". That is
			// a different line from printing nothing, which is why the struct
			// carries HasLinkNetnsID instead of overloading -1 or 0.
			description: "boundary: IFLA_LINK_NETNSID of -1 sets the flag and keeps the value",
			body: concat(
				ifinfomsgHdr(unix.AF_UNSPEC, unix.ARPHRD_ETHER, 9, unix.IFF_UP),
				rtattr(unix.IFLA_LINK, le32(3)),
				rtattr(unix.IFLA_LINK_NETNSID, le32(0xffffffff)),
			),
			want: LinkInfo{
				Index: 9, Flags: unix.IFF_UP, Type: unix.ARPHRD_ETHER,
				Link: 3, LinkNetnsID: -1, HasLinkNetnsID: true,
			},
		},
		{
			// The honest limit of the struct: IFLA_TXQLEN carrying 0 and no
			// IFLA_TXQLEN at all both decode to TxQLen 0. `ip` cannot tell them
			// apart either — it omits the "qlen" token when the value is 0 — so
			// nothing is lost, but the row exists so the ambiguity is recorded
			// rather than discovered.
			description: "boundary: IFLA_TXQLEN of 0 is indistinguishable from an absent one",
			body: concat(
				ifinfomsgHdr(unix.AF_UNSPEC, unix.ARPHRD_ETHER, 10, unix.IFF_UP),
				rtattr(unix.IFLA_TXQLEN, le32(0)),
			),
			want: LinkInfo{Index: 10, Flags: unix.IFF_UP, Type: unix.ARPHRD_ETHER},
		},
		{
			description: "negative: 2-byte IFLA_LINK is ignored, leaving Link zero",
			body: concat(
				ifinfomsgHdr(unix.AF_UNSPEC, unix.ARPHRD_ETHER, 11, unix.IFF_UP),
				rtattr(unix.IFLA_LINK, []byte{0x03, 0x00}),
			),
			want: LinkInfo{Index: 11, Flags: unix.IFF_UP, Type: unix.ARPHRD_ETHER},
		},
		{
			description: "negative: 2-byte IFLA_MASTER is ignored, leaving Master zero",
			body: concat(
				ifinfomsgHdr(unix.AF_UNSPEC, unix.ARPHRD_ETHER, 12, unix.IFF_UP),
				rtattr(unix.IFLA_MASTER, []byte{0x09, 0x00}),
			),
			want: LinkInfo{Index: 12, Flags: unix.IFF_UP, Type: unix.ARPHRD_ETHER},
		},
		{
			// iproute2's parse_rtattr keeps the FIRST occurrence
			// (lib/libnetlink.c:1554) where the kernel's own __nla_parse keeps
			// the last. pkg/xtcpnl follows iproute2, so 1500 wins over 9000 —
			// reverse that and goip renders an MTU `ip` never would.
			description: "corner: duplicate IFLA_MTU takes the first, as parse_rtattr does",
			body: concat(
				ifinfomsgHdr(unix.AF_UNSPEC, unix.ARPHRD_ETHER, 13, unix.IFF_UP),
				rtattr(unix.IFLA_MTU, le32(1500)),
				rtattr(unix.IFLA_MTU, le32(9000)),
			),
			want: LinkInfo{
				Index: 13, Flags: unix.IFF_UP, Type: unix.ARPHRD_ETHER, MTU: 1500,
			},
		},
		{
			description: "corner: duplicate IFLA_IFNAME takes the first",
			body: concat(
				ifinfomsgHdr(unix.AF_UNSPEC, unix.ARPHRD_ETHER, 14, unix.IFF_UP),
				rtattr(unix.IFLA_IFNAME, append([]byte("first"), 0)),
				rtattr(unix.IFLA_IFNAME, append([]byte("second"), 0)),
			),
			want: LinkInfo{
				Index: 14, Flags: unix.IFF_UP, Type: unix.ARPHRD_ETHER, Name: "first",
			},
		},
		{
			// The nest is the first production use of WalkRTAttrsNested.
			description: "positive: IFLA_LINKINFO nest yields IFLA_INFO_KIND",
			body: concat(
				ifinfomsgHdr(unix.AF_UNSPEC, unix.ARPHRD_ETHER, 15, unix.IFF_UP),
				rtattr(unix.IFLA_LINKINFO, rtattr(unix.IFLA_INFO_KIND, append([]byte("vxlan"), 0))),
			),
			want: LinkInfo{
				Index: 15, Flags: unix.IFF_UP, Type: unix.ARPHRD_ETHER, Kind: "vxlan",
			},
		},
		{
			// The kernel ORs NLA_F_NESTED into rta_type for a real nest, and
			// WalkRTAttrs masks it off — so the outer switch matches whether or
			// not the flag is set. Without the mask this row decodes to Kind ""
			// and the failure looks like missing data rather than a bug.
			description: "corner: IFLA_LINKINFO with NLA_F_NESTED set still descends",
			body: concat(
				ifinfomsgHdr(unix.AF_UNSPEC, unix.ARPHRD_ETHER, 16, unix.IFF_UP),
				rtattr(uint16(unix.IFLA_LINKINFO)|uint16(unix.NLA_F_NESTED),
					rtattr(unix.IFLA_INFO_KIND, append([]byte("bridge"), 0))),
			),
			want: LinkInfo{
				Index: 16, Flags: unix.IFF_UP, Type: unix.ARPHRD_ETHER, Kind: "bridge",
			},
		},
		{
			// A nest carrying only IFLA_INFO_DATA — which is what `ip` hands to
			// one of its forty per-kind print_opt bodies, and which this package
			// deliberately does not decode.
			description: "corner: IFLA_LINKINFO with no IFLA_INFO_KIND leaves Kind empty",
			body: concat(
				ifinfomsgHdr(unix.AF_UNSPEC, unix.ARPHRD_ETHER, 17, unix.IFF_UP),
				rtattr(unix.IFLA_LINKINFO, rtattr(unix.IFLA_INFO_DATA, []byte{0x01, 0x02, 0x03, 0x04})),
			),
			want: LinkInfo{Index: 17, Flags: unix.IFF_UP, Type: unix.ARPHRD_ETHER},
		},
		{
			// A malformed nest must not fail the whole link. linkInfoKind
			// swallows the walk error, so the message still decodes and only the
			// kind is lost — dropping an otherwise good link because a nest it
			// did not need was truncated is worse for a renderer.
			description: "corner: IFLA_LINKINFO whose inner rta_len overruns loses only Kind",
			body: concat(
				ifinfomsgHdr(unix.AF_UNSPEC, unix.ARPHRD_ETHER, 18, unix.IFF_UP),
				rtattr(unix.IFLA_IFNAME, append([]byte("brok0"), 0)),
				// Inner rta_len 0x40 with only 4 payload bytes behind it.
				rtattr(unix.IFLA_LINKINFO, []byte{0x40, 0x00, 0x01, 0x00, 0xaa, 0xaa, 0xaa, 0xaa}),
			),
			want: LinkInfo{
				Index: 18, Flags: unix.IFF_UP, Type: unix.ARPHRD_ETHER, Name: "brok0",
			},
		},
		{
			// WalkRTAttrs tolerates a trailing remainder shorter than an rtattr
			// header, because that is what the kernel's NLA_ALIGN walk does.
			description: "corner: 3 trailing bytes after the last attribute are tolerated",
			body: concat(
				ifinfomsgHdr(unix.AF_UNSPEC, unix.ARPHRD_ETHER, 19, unix.IFF_UP),
				rtattr(unix.IFLA_IFNAME, append([]byte("tail0"), 0)),
				[]byte{0x01, 0x02, 0x03},
			),
			want: LinkInfo{
				Index: 19, Flags: unix.IFF_UP, Type: unix.ARPHRD_ETHER, Name: "tail0",
			},
		},
		{
			// But a full header claiming more bytes than remain is an error, not
			// a tail: ErrRTAttrSmall. The pair of rows is the boundary between
			// "truncated stream" and "lying length".
			description: "negative: attribute whose rta_len exceeds the remaining bytes -> error",
			body: concat(
				ifinfomsgHdr(unix.AF_UNSPEC, unix.ARPHRD_ETHER, 20, unix.IFF_UP),
				[]byte{0x40, 0x00, 0x03, 0x00, 0x65, 0x74, 0x68, 0x30},
			),
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := ParseNewLink(tc.body)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ParseNewLink = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// ---- request builder tests ---------------------------------------------------

// TestBuildDumpRequests verifies each DUMP request builder emits a well-formed
// nlmsghdr (correct length, type, REQUEST|DUMP flags, seq) followed by a family
// header of the right size carrying the requested family.
//
// go test ./pkg/xtcpnl/ -run TestBuildDumpRequests
func TestBuildDumpRequests(t *testing.T) {
	const wantFlags = uint16(unix.NLM_F_REQUEST | unix.NLM_F_DUMP)

	tests := []struct {
		description string
		req         []byte
		wantType    uint16
		wantSeq     uint32
		hdrSize     int
		wantFamily  uint8
	}{
		{
			description: "RTM_GETLINK: ifinfomsg, AF_UNSPEC",
			req:         BuildDumpLinkRequest(1),
			wantType:    uint16(unix.RTM_GETLINK),
			wantSeq:     1,
			hdrSize:     IfInfomsgSizeCst,
			wantFamily:  unix.AF_UNSPEC,
		},
		{
			description: "RTM_GETADDR: ifaddrmsg, AF_INET",
			req:         BuildDumpAddrRequest(unix.AF_INET, 2),
			wantType:    uint16(unix.RTM_GETADDR),
			wantSeq:     2,
			hdrSize:     IfAddrmsgSizeCst,
			wantFamily:  unix.AF_INET,
		},
		{
			description: "RTM_GETROUTE: rtmsg, AF_INET6",
			req:         BuildDumpRouteRequest(unix.AF_INET6, 3),
			wantType:    uint16(unix.RTM_GETROUTE),
			wantSeq:     3,
			hdrSize:     RtMsgSizeCst,
			wantFamily:  unix.AF_INET6,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			wantLen := NlMsgHdrSizeCst + tc.hdrSize
			if len(tc.req) != wantLen {
				t.Fatalf("len(req) = %d, want %d", len(tc.req), wantLen)
			}
			var h NlMsgHdr
			if _, err := DeserializeNlMsgHdr(tc.req, &h); err != nil {
				t.Fatalf("DeserializeNlMsgHdr: %v", err)
			}
			if int(h.Len) != wantLen {
				t.Errorf("nlmsg_len = %d, want %d", h.Len, wantLen)
			}
			if h.Type != tc.wantType {
				t.Errorf("nlmsg_type = %d, want %d", h.Type, tc.wantType)
			}
			if h.Flags != wantFlags {
				t.Errorf("nlmsg_flags = %#x, want %#x", h.Flags, wantFlags)
			}
			if h.Seq != tc.wantSeq {
				t.Errorf("nlmsg_seq = %d, want %d", h.Seq, tc.wantSeq)
			}
			if fam := tc.req[NlMsgHdrSizeCst]; fam != tc.wantFamily {
				t.Errorf("family byte = %d, want %d", fam, tc.wantFamily)
			}
		})
	}
}

// ---- WalkRTAttrs / netlinkErr tests ------------------------------------------

// TestWalkRTAttrs covers the TLV walker's positive iteration and its
// truncation/short-length rejection.
//
// go test ./pkg/xtcpnl/ -run TestWalkRTAttrs
func TestWalkRTAttrs(t *testing.T) {
	tests := []struct {
		description string
		data        []byte
		wantTypes   []uint16
		wantErr     bool
	}{
		{
			description: "positive: two attributes with padding walked in order",
			data:        concat(rtattr(1, []byte{0xaa}), rtattr(2, []byte{0xbb, 0xcc, 0xdd, 0xee})),
			wantTypes:   []uint16{1, 2},
		},
		{
			description: "boundary: empty input yields no callbacks",
			data:        nil,
			wantTypes:   nil,
		},
		{
			description: "boundary: a lone 4-byte (empty-value) attribute",
			data:        rtattr(7, nil),
			wantTypes:   []uint16{7},
		},
		{
			description: "corner: sub-header trailing bytes are tolerated (kernel NLA_ALIGN walk)",
			data:        append(rtattr(1, []byte{0x01}), 0x00, 0x00),
			wantTypes:   []uint16{1},
		},
		{
			description: "corner: attribute length below header -> error",
			data:        []byte{0x02, 0x00, 0x01, 0x00},
			wantErr:     true,
		},
		{
			description: "corner: attribute length beyond buffer -> error",
			data:        []byte{0xff, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00},
			wantErr:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var got []uint16
			err := WalkRTAttrs(tc.data, func(atype uint16, _ []byte) {
				got = append(got, atype)
			})
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got types %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.wantTypes) {
				t.Errorf("types = %v, want %v", got, tc.wantTypes)
			}
		})
	}
}

// TestNetlinkErr covers the NLMSG_ERROR body decoder: a zero errno is an ACK
// (nil), a negative errno maps to a syscall.Errno, and a too-short body errors.
//
// go test ./pkg/xtcpnl/ -run TestNetlinkErr
func TestNetlinkErr(t *testing.T) {
	tests := []struct {
		description string
		body        []byte
		wantErr     error
		wantErrno   syscall.Errno
	}{
		{
			description: "positive: zero errno is an ACK -> nil",
			body:        le32(0),
			wantErr:     nil,
		},
		{
			description: "positive: -EPERM maps to EPERM",
			body:        negErrno(syscall.EPERM),
			wantErrno:   syscall.EPERM,
		},
		{
			description: "positive: -ENODEV maps to ENODEV",
			body:        negErrno(syscall.ENODEV),
			wantErrno:   syscall.ENODEV,
		},
		{
			description: "corner: body shorter than 4 bytes -> ErrNetlinkError",
			body:        []byte{0x00, 0x00},
			wantErr:     ErrNetlinkError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			err := netlinkErr(tc.body)
			switch {
			case tc.wantErrno != 0:
				if !errors.Is(err, tc.wantErrno) {
					t.Errorf("err = %v, want errno %v", err, tc.wantErrno)
				}
			case tc.wantErr != nil:
				if !errors.Is(err, tc.wantErr) {
					t.Errorf("err = %v, want %v", err, tc.wantErr)
				}
			default:
				if err != nil {
					t.Errorf("err = %v, want nil", err)
				}
			}
		})
	}
}

// ---- DumpRtnetlink integration test ------------------------------------------

// TestDumpRtnetlinkLive is an integration test against the running kernel's
// NETLINK_ROUTE: it dumps the current namespace's links and asserts a loopback
// interface is present. It is skipped when a route socket cannot be opened
// (restricted sandbox / CI without netlink), so it never blocks the build.
//
// go test ./pkg/xtcpnl/ -run TestDumpRtnetlinkLive
func TestDumpRtnetlinkLive(t *testing.T) {
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC, unix.NETLINK_ROUTE)
	if err != nil {
		t.Skipf("netlink route socket unavailable: %v", err)
	}
	defer func() {
		if cerr := unix.Close(fd); cerr != nil {
			t.Logf("close fd: %v", cerr)
		}
	}()

	sa := &unix.SockaddrNetlink{Family: unix.AF_NETLINK}
	if berr := unix.Bind(fd, sa); berr != nil {
		t.Skipf("bind netlink socket: %v", berr)
	}
	tv := unix.Timeval{Sec: 2}
	if serr := unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &tv); serr != nil {
		t.Skipf("set recv timeout: %v", serr)
	}

	var names []string
	err = DumpRtnetlink(fd, BuildDumpLinkRequest(1), sa, func(mt uint16, body []byte) error {
		if mt != uint16(unix.RTM_NEWLINK) {
			return nil
		}
		li, perr := ParseNewLink(body)
		if perr != nil {
			return perr
		}
		names = append(names, li.Name)
		return nil
	})
	if err != nil {
		t.Fatalf("DumpRtnetlink(GETLINK): %v", err)
	}

	if len(names) == 0 {
		t.Fatal("no links returned from RTM_GETLINK dump")
	}
	hasLo := false
	for _, n := range names {
		if n == "lo" {
			hasLo = true
		}
	}
	if !hasLo {
		t.Errorf("loopback 'lo' not found among links: %v", names)
	}

	// The address dump should decode cleanly and include the loopback address.
	var sawLoopback bool
	err = DumpRtnetlink(fd, BuildDumpAddrRequest(unix.AF_UNSPEC, 2), sa, func(mt uint16, body []byte) error {
		if mt != uint16(unix.RTM_NEWADDR) {
			return nil
		}
		ai, perr := ParseNewAddr(body)
		if perr != nil {
			return perr
		}
		raw := ai.Local
		if len(raw) == 0 {
			raw = ai.Address
		}
		if len(raw) == 4 && raw[0] == 127 {
			sawLoopback = true
		}
		if len(raw) == 16 && raw[15] == 1 && allZero(raw[:15]) {
			sawLoopback = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("DumpRtnetlink(GETADDR): %v", err)
	}
	if !sawLoopback {
		t.Log("note: no loopback address seen in RTM_GETADDR dump (unusual but not fatal)")
	}

	// The route dump should decode cleanly.
	var routeCount int
	err = DumpRtnetlink(fd, BuildDumpRouteRequest(unix.AF_UNSPEC, 3), sa, func(mt uint16, body []byte) error {
		if mt != uint16(unix.RTM_NEWROUTE) {
			return nil
		}
		if _, perr := ParseNewRoute(body); perr != nil {
			return perr
		}
		routeCount++
		return nil
	})
	if err != nil {
		t.Fatalf("DumpRtnetlink(GETROUTE): %v", err)
	}
	t.Logf("live dump: %d links, %d routes", len(names), routeCount)
}

// ---- fuzz --------------------------------------------------------------------

// FuzzParseNewAddr ensures ParseNewAddr never panics on arbitrary bytes.
func FuzzParseNewAddr(f *testing.F) {
	f.Add(ifaddrmsgHdr(unix.AF_INET, 24, 0, unix.RT_SCOPE_UNIVERSE, 2))
	f.Add(concat(ifaddrmsgHdr(unix.AF_INET, 24, 0, 0, 2), rtattr(unix.IFA_ADDRESS, v4b(10, 0, 0, 5))))
	f.Add([]byte{})
	f.Fuzz(func(_ *testing.T, body []byte) {
		_, _ = ParseNewAddr(body)
	})
}

// FuzzParseNewRoute ensures ParseNewRoute never panics on arbitrary bytes.
func FuzzParseNewRoute(f *testing.F) {
	f.Add(rtmsgHdr(unix.AF_INET, 24, 0, unix.RT_TABLE_MAIN, 0, unix.RT_SCOPE_LINK, unix.RTN_UNICAST, 0))
	f.Add(concat(rtmsgHdr(unix.AF_INET, 0, 0, unix.RT_TABLE_MAIN, 0, 0, unix.RTN_UNICAST, 0), rtattr(unix.RTA_GATEWAY, v4b(10, 0, 0, 1))))
	f.Add([]byte{})
	f.Fuzz(func(_ *testing.T, body []byte) {
		_, _ = ParseNewRoute(body)
	})
}

// FuzzParseNewLink ensures ParseNewLink never panics on arbitrary bytes.
func FuzzParseNewLink(f *testing.F) {
	f.Add(concat(ifinfomsgHdr(unix.AF_UNSPEC, 1, 2, unix.IFF_UP), rtattr(unix.IFLA_IFNAME, append([]byte("eth0"), 0))))
	f.Add([]byte{})
	f.Fuzz(func(_ *testing.T, body []byte) {
		_, _ = ParseNewLink(body)
	})
}

// ---- small helpers -----------------------------------------------------------

func concat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func le32(v uint32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, v)
	return b
}

// negErrno encodes the kernel's NLMSG_ERROR payload for errno e: a negative
// int32 in little-endian. Computed at runtime to avoid constant overflow.
func negErrno(e syscall.Errno) []byte {
	n := -int32(e)
	return le32(uint32(n))
}

func mustV6(t *testing.T, s string) []byte {
	t.Helper()
	a, err := netip.ParseAddr(s)
	if err != nil || !a.Is6() {
		t.Fatalf("bad IPv6 %q: %v", s, err)
	}
	b := a.As16()
	return b[:]
}

func allZero(b []byte) bool {
	for _, x := range b {
		if x != 0 {
			return false
		}
	}
	return true
}
