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
	"reflect"
	"testing"

	"golang.org/x/sys/unix"
)

// ---- byte builders -----------------------------------------------------------
//
// These complement the rtnetlink builders in xtcpnl_rtnetlink_test.go (rtattr,
// concat, le32, v4b are reused from there — same package, same test binary).

// ndmsgHdr encodes a 12-byte ndmsg family header.
func ndmsgHdr(family uint8, ifindex int32, state uint16, flags, ntype uint8) []byte {
	b := make([]byte, NdMsgSizeCst)
	b[0] = family
	// b[1] ndm_pad1 and b[2:4] ndm_pad2 stay zero, as the kernel sends them.
	binary.LittleEndian.PutUint32(b[4:8], uint32(ifindex))
	binary.LittleEndian.PutUint16(b[8:10], state)
	b[10] = flags
	b[11] = ntype
	return b
}

// ndaCacheInfoBytes encodes a 16-byte struct nda_cacheinfo value.
func ndaCacheInfoBytes(confirmed, used, updated, refcnt uint32) []byte {
	return concat(le32(confirmed), le32(used), le32(updated), le32(refcnt))
}

// macb is the link-layer address counterpart of v4b.
func macb(a, b, c, d, e, f byte) []byte { return []byte{a, b, c, d, e, f} }

// ---- deserializer tests ------------------------------------------------------

// TestDeserializeNdMsg checks the 12-byte ndmsg header decoder against both the
// manual and reflection implementations.
//
// go test ./pkg/xtcpnl/ -run TestDeserializeNdMsg
func TestDeserializeNdMsg(t *testing.T) {
	tests := []struct {
		description string
		data        []byte
		want        NdMsg
		wantErr     error
	}{
		{
			description: "positive: IPv4 reachable neighbor on ifindex 2",
			data:        ndmsgHdr(unix.AF_INET, 2, unix.NUD_REACHABLE, 0, unix.RTN_UNICAST),
			want:        NdMsg{Family: unix.AF_INET, Ifindex: 2, State: unix.NUD_REACHABLE, Type: unix.RTN_UNICAST},
		},
		{
			description: "positive: IPv6 permanent neighbor with NTF_ROUTER flag",
			data:        ndmsgHdr(unix.AF_INET6, 3, unix.NUD_PERMANENT, unix.NTF_ROUTER, unix.RTN_UNICAST),
			want: NdMsg{
				Family: unix.AF_INET6, Ifindex: 3, State: unix.NUD_PERMANENT,
				Flags: unix.NTF_ROUTER, Type: unix.RTN_UNICAST,
			},
		},
		{
			description: "boundary: all-zero header decodes to the zero value",
			data:        make([]byte, NdMsgSizeCst),
			want:        NdMsg{},
		},
		{
			description: "boundary: exactly NdMsgSizeCst bytes is sufficient",
			data:        ndmsgHdr(unix.AF_INET, 1, unix.NUD_NOARP, 0, 0),
			want:        NdMsg{Family: unix.AF_INET, Ifindex: 1, State: unix.NUD_NOARP},
		},
		{
			description: "boundary: negative ifindex round-trips as a signed __s32",
			data:        ndmsgHdr(unix.AF_INET, -1, unix.NUD_FAILED, 0, 0),
			want:        NdMsg{Family: unix.AF_INET, Ifindex: -1, State: unix.NUD_FAILED},
		},
		{
			description: "boundary: trailing attribute bytes are ignored by the header decoder",
			data:        append(ndmsgHdr(unix.AF_INET, 7, unix.NUD_STALE, 0, 0), 0xde, 0xad, 0xbe, 0xef),
			want:        NdMsg{Family: unix.AF_INET, Ifindex: 7, State: unix.NUD_STALE},
		},
		{
			description: "corner: one byte short -> ErrNdMsgSmall",
			data:        make([]byte, NdMsgSizeCst-1),
			wantErr:     ErrNdMsgSmall,
		},
		{
			description: "corner: empty input -> ErrNdMsgSmall",
			data:        nil,
			wantErr:     ErrNdMsgSmall,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var manual, refl NdMsg
			_, errM := DeserializeNdMsg(tc.data, &manual)
			if !errors.Is(errM, tc.wantErr) {
				t.Fatalf("manual err = %v, want %v", errM, tc.wantErr)
			}
			if tc.wantErr != nil {
				// The reflection decoder reports short input as binary.Read's
				// own io error rather than ErrNdMsgSmall, so only its
				// non-nil-ness is comparable — but it must agree that the input
				// is unusable.
				if _, errR := deserializeNdMsgReflection(tc.data, &refl); errR == nil {
					t.Error("reflection accepted input the manual decoder rejected")
				}
				return
			}
			if !reflect.DeepEqual(manual, tc.want) {
				t.Errorf("DeserializeNdMsg = %+v, want %+v", manual, tc.want)
			}
			if _, errR := deserializeNdMsgReflection(tc.data, &refl); errR != nil {
				t.Fatalf("reflection err = %v, want nil", errR)
			}
			if !reflect.DeepEqual(refl, manual) {
				t.Errorf("reflection = %+v, manual = %+v", refl, manual)
			}
		})
	}
}

// TestDeserializeNdaCacheInfo checks the 16-byte nda_cacheinfo decoder against
// both implementations.
//
// go test ./pkg/xtcpnl/ -run TestDeserializeNdaCacheInfo
func TestDeserializeNdaCacheInfo(t *testing.T) {
	tests := []struct {
		description string
		data        []byte
		want        NdaCacheInfo
		wantErr     error
	}{
		{
			description: "positive: four distinct counters in field order",
			data:        ndaCacheInfoBytes(1, 2, 3, 4),
			want:        NdaCacheInfo{Confirmed: 1, Used: 2, Updated: 3, Refcnt: 4},
		},
		{
			description: "boundary: all-zero counters (a freshly created entry)",
			data:        ndaCacheInfoBytes(0, 0, 0, 0),
			want:        NdaCacheInfo{},
		},
		{
			description: "boundary: max uint32 counters do not overflow",
			data:        ndaCacheInfoBytes(0xffffffff, 0xffffffff, 0xffffffff, 0xffffffff),
			want: NdaCacheInfo{
				Confirmed: 0xffffffff, Used: 0xffffffff,
				Updated: 0xffffffff, Refcnt: 0xffffffff,
			},
		},
		{
			description: "boundary: extra trailing bytes are ignored",
			data:        append(ndaCacheInfoBytes(9, 8, 7, 6), 0xff, 0xff),
			want:        NdaCacheInfo{Confirmed: 9, Used: 8, Updated: 7, Refcnt: 6},
		},
		{
			description: "corner: one byte short -> ErrNdaCacheInfoSmall",
			data:        make([]byte, NdaCacheInfoSizeCst-1),
			wantErr:     ErrNdaCacheInfoSmall,
		},
		{
			description: "corner: empty input -> ErrNdaCacheInfoSmall",
			data:        nil,
			wantErr:     ErrNdaCacheInfoSmall,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var manual, refl NdaCacheInfo
			_, errM := DeserializeNdaCacheInfo(tc.data, &manual)
			if !errors.Is(errM, tc.wantErr) {
				t.Fatalf("manual err = %v, want %v", errM, tc.wantErr)
			}
			if tc.wantErr != nil {
				if _, errR := deserializeNdaCacheInfoReflection(tc.data, &refl); errR == nil {
					t.Error("reflection accepted input the manual decoder rejected")
				}
				return
			}
			if !reflect.DeepEqual(manual, tc.want) {
				t.Errorf("DeserializeNdaCacheInfo = %+v, want %+v", manual, tc.want)
			}
			if _, errR := deserializeNdaCacheInfoReflection(tc.data, &refl); errR != nil {
				t.Fatalf("reflection err = %v, want nil", errR)
			}
			if !reflect.DeepEqual(refl, manual) {
				t.Errorf("reflection = %+v, manual = %+v", refl, manual)
			}
		})
	}
}

// ---- ParseNeigh --------------------------------------------------------------

// TestParseNeigh decodes RTM_NEWNEIGH / RTM_DELNEIGH bodies: the ndmsg header
// followed by NDA_* attributes.
//
// go test ./pkg/xtcpnl/ -run TestParseNeigh
func TestParseNeigh(t *testing.T) {
	tests := []struct {
		description string
		body        []byte
		want        NeighInfo
		wantErr     error
	}{
		{
			description: "positive: IPv4 permanent ARP entry with NDA_DST and NDA_LLADDR",
			body: concat(
				ndmsgHdr(unix.AF_INET, 2, unix.NUD_PERMANENT, 0, unix.RTN_UNICAST),
				rtattr(unix.NDA_DST, v4b(192, 168, 1, 1)),
				rtattr(unix.NDA_LLADDR, macb(0x52, 0x54, 0x00, 0x12, 0x34, 0x56)),
			),
			want: NeighInfo{
				Family: unix.AF_INET, Ifindex: 2, State: unix.NUD_PERMANENT,
				Type:   unix.RTN_UNICAST,
				Dst:    v4b(192, 168, 1, 1),
				LLAddr: macb(0x52, 0x54, 0x00, 0x12, 0x34, 0x56),
			},
		},
		{
			description: "positive: IPv6 reachable NDISC entry with NDA_CACHEINFO",
			body: concat(
				ndmsgHdr(unix.AF_INET6, 3, unix.NUD_REACHABLE, unix.NTF_ROUTER, unix.RTN_UNICAST),
				rtattr(unix.NDA_DST, mustV6(t, "fe80::1")),
				rtattr(unix.NDA_LLADDR, macb(0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff)),
				rtattr(unix.NDA_CACHEINFO, ndaCacheInfoBytes(10, 20, 30, 1)),
			),
			want: NeighInfo{
				Family: unix.AF_INET6, Ifindex: 3, State: unix.NUD_REACHABLE,
				Flags:        unix.NTF_ROUTER,
				Type:         unix.RTN_UNICAST,
				Dst:          mustV6(t, "fe80::1"),
				LLAddr:       macb(0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff),
				HasCacheInfo: true,
				CacheInfo:    NdaCacheInfo{Confirmed: 10, Used: 20, Updated: 30, Refcnt: 1},
			},
		},
		{
			description: "positive: NUD_FAILED entry keeps NDA_DST but has no lladdr",
			body: concat(
				ndmsgHdr(unix.AF_INET, 2, unix.NUD_FAILED, 0, unix.RTN_UNICAST),
				rtattr(unix.NDA_DST, v4b(10, 0, 0, 99)),
			),
			want: NeighInfo{
				Family: unix.AF_INET, Ifindex: 2, State: unix.NUD_FAILED,
				Type: unix.RTN_UNICAST,
				Dst:  v4b(10, 0, 0, 99),
			},
		},
		{
			description: "boundary: header only, no attributes",
			body:        ndmsgHdr(unix.AF_INET, 1, unix.NUD_NOARP, 0, 0),
			want:        NeighInfo{Family: unix.AF_INET, Ifindex: 1, State: unix.NUD_NOARP},
		},
		{
			description: "boundary: a single attribute exactly consuming the buffer",
			body: concat(
				ndmsgHdr(unix.AF_INET, 2, unix.NUD_STALE, 0, 0),
				rtattr(unix.NDA_DST, v4b(172, 16, 0, 1)),
			),
			want: NeighInfo{
				Family: unix.AF_INET, Ifindex: 2, State: unix.NUD_STALE,
				Dst: v4b(172, 16, 0, 1),
			},
		},
		{
			// copyBytes collapses an empty value to nil, so a present-but-empty
			// NDA_LLADDR is indistinguishable from an absent one. That is the
			// intended behavior: either way there is no usable address.
			description: "corner: zero-length NDA_LLADDR (a link with no address) yields a nil LLAddr",
			body: concat(
				ndmsgHdr(unix.AF_INET, 4, unix.NUD_NOARP, 0, 0),
				rtattr(unix.NDA_DST, v4b(10, 1, 1, 1)),
				rtattr(unix.NDA_LLADDR, nil),
			),
			want: NeighInfo{
				Family: unix.AF_INET, Ifindex: 4, State: unix.NUD_NOARP,
				Dst:    v4b(10, 1, 1, 1),
				LLAddr: nil,
			},
		},
		{
			description: "corner: short NDA_CACHEINFO is tolerated, HasCacheInfo stays false",
			body: concat(
				ndmsgHdr(unix.AF_INET, 2, unix.NUD_REACHABLE, 0, 0),
				rtattr(unix.NDA_DST, v4b(10, 0, 0, 1)),
				rtattr(unix.NDA_CACHEINFO, make([]byte, NdaCacheInfoSizeCst-4)),
			),
			want: NeighInfo{
				Family: unix.AF_INET, Ifindex: 2, State: unix.NUD_REACHABLE,
				Dst: v4b(10, 0, 0, 1),
			},
		},
		{
			description: "corner: unknown NDA attribute types are skipped, known ones still extracted",
			body: concat(
				ndmsgHdr(unix.AF_INET, 2, unix.NUD_REACHABLE, 0, 0),
				rtattr(unix.NDA_PROBES, le32(3)),
				rtattr(unix.NDA_VLAN, []byte{0x64, 0x00}),
				rtattr(unix.NDA_DST, v4b(10, 0, 0, 2)),
			),
			want: NeighInfo{
				Family: unix.AF_INET, Ifindex: 2, State: unix.NUD_REACHABLE,
				Dst: v4b(10, 0, 0, 2),
			},
		},
		{
			description: "corner: nested-flagged NDA_DST is still matched (NLA_F_NESTED masked off)",
			body: concat(
				ndmsgHdr(unix.AF_INET, 2, unix.NUD_REACHABLE, 0, 0),
				rtattr(unix.NDA_DST|unix.NLA_F_NESTED, v4b(10, 0, 0, 3)),
			),
			want: NeighInfo{
				Family: unix.AF_INET, Ifindex: 2, State: unix.NUD_REACHABLE,
				Dst: v4b(10, 0, 0, 3),
			},
		},
		{
			description: "corner: one byte short of the ndmsg header -> ErrNdMsgSmall",
			body:        make([]byte, NdMsgSizeCst-1),
			wantErr:     ErrNdMsgSmall,
		},
		{
			description: "corner: attribute length below the RTAttr header -> walk error",
			body: concat(
				ndmsgHdr(unix.AF_INET, 2, unix.NUD_REACHABLE, 0, 0),
				[]byte{0x02, 0x00, byte(unix.NDA_DST), 0x00},
			),
			wantErr: ErrRTAttrSmall,
		},
		{
			description: "corner: attribute length beyond the buffer -> walk error",
			body: concat(
				ndmsgHdr(unix.AF_INET, 2, unix.NUD_REACHABLE, 0, 0),
				[]byte{0xff, 0x00, byte(unix.NDA_DST), 0x00, 0x00, 0x00, 0x00, 0x00},
			),
			wantErr: ErrRTAttrSmall,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := ParseNeigh(tc.body)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ParseNeigh = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// ---- NUD state helpers -------------------------------------------------------

// TestNudStateString covers the NUD_* bitmask renderer: single bits, combined
// bits, the zero state, and unknown bits that must not be silently dropped.
//
// go test ./pkg/xtcpnl/ -run TestNudStateString
func TestNudStateString(t *testing.T) {
	tests := []struct {
		description string
		state       uint16
		want        string
	}{
		{
			description: "positive: NUD_REACHABLE alone",
			state:       unix.NUD_REACHABLE,
			want:        "NUD_REACHABLE",
		},
		{
			description: "positive: NUD_PERMANENT alone",
			state:       unix.NUD_PERMANENT,
			want:        "NUD_PERMANENT",
		},
		{
			description: "positive: NUD_FAILED alone",
			state:       unix.NUD_FAILED,
			want:        "NUD_FAILED",
		},
		{
			description: "positive: two bits render in kernel bit order, not argument order",
			state:       unix.NUD_PERMANENT | unix.NUD_STALE,
			want:        "NUD_STALE|NUD_PERMANENT",
		},
		{
			description: "boundary: zero state is NUD_NONE, not the empty string",
			state:       0,
			want:        "NUD_NONE",
		},
		{
			description: "boundary: every known bit set",
			state: unix.NUD_INCOMPLETE | unix.NUD_REACHABLE | unix.NUD_STALE |
				unix.NUD_DELAY | unix.NUD_PROBE | unix.NUD_FAILED |
				unix.NUD_NOARP | unix.NUD_PERMANENT,
			want: "NUD_INCOMPLETE|NUD_REACHABLE|NUD_STALE|NUD_DELAY|NUD_PROBE|NUD_FAILED|NUD_NOARP|NUD_PERMANENT",
		},
		{
			description: "corner: an unknown bit is reported as a hex remainder",
			state:       0x8000,
			want:        "0x8000",
		},
		{
			description: "corner: a known bit plus an unknown bit keeps both",
			state:       unix.NUD_REACHABLE | 0x8000,
			want:        "NUD_REACHABLE|0x8000",
		},
		{
			description: "corner: all bits set renders every name plus the unknown remainder",
			state:       0xffff,
			want:        "NUD_INCOMPLETE|NUD_REACHABLE|NUD_STALE|NUD_DELAY|NUD_PROBE|NUD_FAILED|NUD_NOARP|NUD_PERMANENT|0xff00",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := NudStateString(tc.state); got != tc.want {
				t.Errorf("NudStateString(%#x) = %q, want %q", tc.state, got, tc.want)
			}
		})
	}
}

// TestNeighInfoIsReachable covers the usability predicate over ndm_state.
//
// go test ./pkg/xtcpnl/ -run TestNeighInfoIsReachable
func TestNeighInfoIsReachable(t *testing.T) {
	tests := []struct {
		description string
		state       uint16
		want        bool
	}{
		{
			description: "positive: NUD_REACHABLE is usable",
			state:       unix.NUD_REACHABLE,
			want:        true,
		},
		{
			description: "positive: NUD_PERMANENT is usable",
			state:       unix.NUD_PERMANENT,
			want:        true,
		},
		{
			description: "positive: NUD_NOARP is usable",
			state:       unix.NUD_NOARP,
			want:        true,
		},
		{
			description: "negative: NUD_FAILED is not usable",
			state:       unix.NUD_FAILED,
			want:        false,
		},
		{
			description: "negative: NUD_INCOMPLETE is not usable",
			state:       unix.NUD_INCOMPLETE,
			want:        false,
		},
		{
			description: "negative: NUD_STALE is deliberately excluded (kernel will revalidate)",
			state:       unix.NUD_STALE,
			want:        false,
		},
		{
			description: "boundary: zero state is not usable",
			state:       0,
			want:        false,
		},
		{
			description: "corner: NUD_STALE|NUD_PERMANENT is usable on the strength of PERMANENT",
			state:       unix.NUD_STALE | unix.NUD_PERMANENT,
			want:        true,
		},
		{
			description: "corner: NUD_DELAY|NUD_PROBE, both transient, is not usable",
			state:       unix.NUD_DELAY | unix.NUD_PROBE,
			want:        false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			ni := NeighInfo{State: tc.state}
			if got := ni.IsReachable(); got != tc.want {
				t.Errorf("IsReachable(%s) = %v, want %v", NudStateString(tc.state), got, tc.want)
			}
		})
	}
}

// FuzzParseNeigh checks ParseNeigh never panics on arbitrary bodies.
//
// go test ./pkg/xtcpnl/ -run FuzzParseNeigh -fuzz FuzzParseNeigh
func FuzzParseNeigh(f *testing.F) {
	f.Add(ndmsgHdr(unix.AF_INET, 2, unix.NUD_REACHABLE, 0, 0))
	f.Add(concat(
		ndmsgHdr(unix.AF_INET, 2, unix.NUD_PERMANENT, 0, 0),
		rtattr(unix.NDA_DST, v4b(10, 0, 0, 1)),
		rtattr(unix.NDA_LLADDR, macb(1, 2, 3, 4, 5, 6)),
	))
	f.Fuzz(func(_ *testing.T, body []byte) {
		_, _ = ParseNeigh(body)
	})
}
