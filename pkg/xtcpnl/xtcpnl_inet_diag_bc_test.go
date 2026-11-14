package xtcpnl

import (
	"encoding/binary"
	"errors"
	"math"
	"net/netip"
	"reflect"
	"testing"
)

// Bytecode builders that lay ops out the way iproute2's
// ssfilter_bytecompile does (misc/ss.c:1914).

func bcU8(t *testing.T, n int) uint8 {
	t.Helper()
	if n < 0 || n > math.MaxUint8 {
		t.Fatalf("%d does not fit a u8", n)
	}
	return uint8(n)
}

func bcU16(t *testing.T, n int) uint16 {
	t.Helper()
	if n < 0 || n > math.MaxUint16 {
		t.Fatalf("%d does not fit a u16", n)
	}
	return uint16(n)
}

func bcOpB(code, yes uint8, no uint16) []byte {
	b := []byte{code, yes, 0, 0}
	binary.LittleEndian.PutUint16(b[2:], no)
	return b
}

func bcPortB(code uint8, port uint16) []byte {
	return append(bcOpB(code, 8, 12), bcOpB(0, 0, port)...)
}

func bcHostB(t *testing.T, code, family, plen uint8, port uint32, addr []byte) []byte {
	t.Helper()
	oplen := InetDiagBcOpSizeCst + InetDiagHostcondSizeCst + len(addr)
	b := bcOpB(code, bcU8(t, oplen), bcU16(t, oplen+4))
	b = append(b, family, plen, 0, 0, 0, 0, 0, 0)
	binary.LittleEndian.PutUint32(b[8:], port)
	return append(b, addr...)
}

func bcU32OpB(t *testing.T, code uint8, vals ...uint32) []byte {
	t.Helper()
	n := InetDiagBcOpSizeCst + 4*len(vals)
	b := bcOpB(code, bcU8(t, n), bcU16(t, n+4))
	for _, v := range vals {
		b = binary.LittleEndian.AppendUint32(b, v)
	}
	return b
}

func bcCgroupB(id uint64) []byte {
	return binary.LittleEndian.AppendUint64(bcOpB(InetDiagBcCgroupCond, 12, 16), id)
}

func bcOrB(t *testing.T, a, b []byte) []byte {
	t.Helper()
	out := append(append([]byte(nil), a...), bcOpB(InetDiagBcJmp, 4, bcU16(t, len(b)+4))...)
	return append(out, b...)
}

func bcNotB(a []byte) []byte {
	return append(append([]byte(nil), a...), bcOpB(InetDiagBcJmp, 4, 8)...)
}

// bcAndB is ssfilter_patch: a's jumps to "reject" (no == left+4) move past b.
func bcAndB(t *testing.T, a, b []byte) []byte {
	t.Helper()
	out := append([]byte(nil), a...)
	for off := 0; off < len(a); off += int(out[off+1]) {
		left := len(a) - off
		if int(binary.LittleEndian.Uint16(out[off+2:])) == left+4 {
			binary.LittleEndian.PutUint16(out[off+2:], bcU16(t, left+4+len(b)))
		}
	}
	return append(out, b...)
}

func bcAddr(s string) []byte {
	return netip.MustParseAddr(s).AsSlice()
}

func bcEntry(saddr string, sport uint16, daddr string, dport uint16) *InetDiagEntry {
	e := &InetDiagEntry{Saddr: bcAddr(saddr), Daddr: bcAddr(daddr), Sport: sport, Dport: dport, Family: afInet}
	if len(e.Saddr) == 16 {
		e.Family = afInet6
	}
	return e
}

func TestParseInetDiagRequest(t *testing.T) {
	hdr := func(family, proto, ext uint8, states uint32) []byte {
		b := make([]byte, InetDiagReqV2SizeCst)
		b[0], b[1], b[2] = family, proto, ext
		binary.LittleEndian.PutUint32(b[4:], states)
		binary.BigEndian.PutUint16(b[8:], 22)
		binary.LittleEndian.PutUint32(b[44:], 3)
		return b
	}
	attr := func(atype uint16, val []byte) []byte {
		b := make([]byte, 4, 4+len(val)+3)
		binary.LittleEndian.PutUint16(b, bcU16(t, 4+len(val)))
		binary.LittleEndian.PutUint16(b[2:], atype)
		b = append(b, val...)
		for len(b)%4 != 0 {
			b = append(b, 0)
		}
		return b
	}
	bc := bcPortB(InetDiagBcSEq, 22)
	tests := []struct {
		description string
		body        []byte
		expected    InetDiagRequest
		wantErr     error
		wantProto   uint32
	}{
		{
			description: "positive: ss -tna, no attributes; states read host order",
			body:        hdr(afInet, 6, 0, 0x3fff),
			expected:    InetDiagRequest{Family: afInet, Protocol: 6, States: 0x3fff, ID: InetDiagSockID{SPort: 22, Interface: 3}},
			wantProto:   6,
		},
		{
			description: "positive: a bytecode attribute is copied",
			body:        append(hdr(afInet6, 6, 0x19, 0xfff), attr(InetDiagReqBytecode, bc)...),
			expected: InetDiagRequest{Family: afInet6, Protocol: 6, Ext: 0x19, States: 0xfff,
				ID: InetDiagSockID{SPort: 22, Interface: 3}, Bytecode: bc, HasBytecode: true},
			wantProto: 6,
		},
		{
			description: "positive: MPTCP's 262 rides in INET_DIAG_REQ_PROTOCOL, the u8 truncated to 6",
			body:        append(hdr(afInet, 6, 0, 0x3fff), attr(InetDiagReqProtocol, []byte{6, 1, 0, 0})...),
			expected: InetDiagRequest{Family: afInet, Protocol: 6, States: 0x3fff,
				ID: InetDiagSockID{SPort: 22, Interface: 3}, ProtocolAttr: 262, HasProtocolAttr: true},
			wantProto: 262,
		},
		{
			description: "corner: SK_BPF_STORAGES is noted, not decoded",
			body:        append(hdr(afInet, 6, 0, 1), attr(InetDiagReqSkBpfStorages, []byte{1, 2, 3, 4})...),
			expected: InetDiagRequest{Family: afInet, Protocol: 6, States: 1,
				ID: InetDiagSockID{SPort: 22, Interface: 3}, HasBpfStorages: true},
			wantProto: 6,
		},
		{
			description: "boundary: exactly 56 bytes",
			body:        hdr(afInet, 17, 0, 0),
			expected:    InetDiagRequest{Family: afInet, Protocol: 17, ID: InetDiagSockID{SPort: 22, Interface: 3}},
			wantProto:   17,
		},
		{
			description: "negative: 55 bytes",
			body:        hdr(afInet, 6, 0, 0)[:55],
			wantErr:     ErrInetDiagRequestSmall,
		},
		{
			description: "negative: a 2-byte PROTOCOL attribute (inet_diag.c:190)",
			body:        append(hdr(afInet, 6, 0, 0), attr(InetDiagReqProtocol, []byte{6, 1})...),
			wantErr:     ErrInetDiagProtocolLen,
		},
		{
			description: "boundary: an 8-byte PROTOCOL attribute is refused too",
			body:        append(hdr(afInet, 6, 0, 0), attr(InetDiagReqProtocol, []byte{6, 1, 0, 0, 0, 0, 0, 0})...),
			wantErr:     ErrInetDiagProtocolLen,
		},
		{
			description: "negative: an attribute longer than the body",
			body:        append(hdr(afInet, 6, 0, 0), 0x20, 0, 1, 0),
			wantErr:     ErrRTAttrSmall,
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := ParseInetDiagRequest(tc.body)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if string(got.Bytecode) != string(tc.expected.Bytecode) {
				t.Errorf("bytecode % x, want % x", got.Bytecode, tc.expected.Bytecode)
			}
			got.Bytecode, tc.expected.Bytecode = nil, nil
			if !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("got %+v\nwant %+v", got, tc.expected)
			}
			if p := got.EffectiveProtocol(); p != tc.wantProto {
				t.Errorf("effective protocol %d, want %d", p, tc.wantProto)
			}
		})
	}
}

func TestInetDiagBcAudit(t *testing.T) {
	v4 := bcAddr("127.0.0.1")
	v6 := bcAddr("::1")
	sport := bcPortB(InetDiagBcSEq, 22)
	host4 := bcHostB(t, InetDiagBcSCond, afInet, 32, anyPortCst, v4)
	withNo := func(b []byte, no uint16) []byte {
		out := append([]byte(nil), b...)
		binary.LittleEndian.PutUint16(out[2:], no)
		return out
	}
	withYes := func(b []byte, yes uint8) []byte {
		out := append([]byte(nil), b...)
		out[1] = yes
		return out
	}
	tests := []struct {
		description string
		bc          []byte
		netAdmin    bool
		expected    error
	}{
		{description: "positive: sport = :22", bc: sport},
		{description: "positive: src 127.0.0.1/32", bc: host4},
		{description: "positive: src [::1]/128 port 80", bc: bcHostB(t, InetDiagBcSCond, afInet6, 128, 80, v6)},
		{description: "positive: an AF_UNSPEC hostcond, prefix 0, no address", bc: bcHostB(t, InetDiagBcDCond, afUnspec, 0, 80, nil)},
		{description: "positive: or", bc: bcOrB(t, sport, bcPortB(InetDiagBcDGe, 1024))},
		{description: "positive: not", bc: bcNotB(sport)},
		{description: "positive: and of an or", bc: bcAndB(t, bcOrB(t, sport, host4), bcNotB(bcOpB(InetDiagBcAuto, 4, 8)))},
		{description: "positive: autobound", bc: bcOpB(InetDiagBcAuto, 4, 8)},
		{description: "positive: dev cond", bc: bcU32OpB(t, InetDiagBcDevCond, 1)},
		{description: "positive: cgroup cond", bc: bcCgroupB(1)},
		{description: "positive: fwmark with CAP_NET_ADMIN", bc: bcU32OpB(t, InetDiagBcMarkCond, 0x10, 0xffffffff), netAdmin: true},
		{description: "negative: fwmark without CAP_NET_ADMIN is EPERM", bc: bcU32OpB(t, InetDiagBcMarkCond, 0x10, 0xffffffff), expected: ErrInetDiagBcPerm},
		{description: "negative: empty", bc: nil, expected: ErrInetDiagBcInvalid},
		{description: "negative: three bytes", bc: []byte{6, 4, 8}, expected: ErrInetDiagBcInvalid},
		{description: "negative: unknown code 14", bc: bcOpB(14, 4, 8), expected: ErrInetDiagBcInvalid},
		{description: "negative: hostcond family 3", bc: bcHostB(t, InetDiagBcSCond, 3, 0, anyPortCst, v4), expected: ErrInetDiagBcInvalid},
		{description: "boundary: v4 prefix 33", bc: bcHostB(t, InetDiagBcSCond, afInet, 33, anyPortCst, v4), expected: ErrInetDiagBcInvalid},
		{description: "boundary: v6 prefix 129", bc: bcHostB(t, InetDiagBcSCond, afInet6, 129, anyPortCst, v6), expected: ErrInetDiagBcInvalid},
		{description: "negative: AF_UNSPEC with prefix 1", bc: bcHostB(t, InetDiagBcSCond, afUnspec, 1, anyPortCst, nil), expected: ErrInetDiagBcInvalid},
		{description: "negative: a v6 hostcond cut to a v4 address", bc: withYes(bcHostB(t, InetDiagBcSCond, afInet6, 0, anyPortCst, v4), 16), expected: ErrInetDiagBcInvalid},
		{description: "negative: a port op with no port operand", bc: bcOpB(InetDiagBcSEq, 4, 8), expected: ErrInetDiagBcInvalid},
		{description: "negative: yes shorter than the operand", bc: append(withYes(sport, 4), 0, 0, 0, 0), expected: ErrInetDiagBcInvalid},
		{description: "negative: no not a multiple of 4", bc: withNo(sport, 10), expected: ErrInetDiagBcInvalid},
		{description: "negative: no shorter than the op", bc: withNo(sport, 4), expected: ErrInetDiagBcInvalid},
		{description: "boundary: no = left+4 (reject) passes", bc: withNo(sport, 12)},
		{description: "boundary: no = left+8 overshoots", bc: withNo(sport, 16), expected: ErrInetDiagBcInvalid},
		{description: "positive: no = 8 lands on the next op", bc: append(withNo(sport, 8), bcPortB(InetDiagBcDEq, 1)...)},
		{description: "negative: no lands on a port operand, not an op", bc: append(withNo(bcOpB(InetDiagBcAuto, 4, 8), 8), bcPortB(InetDiagBcDEq, 1)...),
			expected: ErrInetDiagBcInvalid},
		{description: "negative: the last yes runs past the end", bc: withYes(bcOpB(InetDiagBcAuto, 4, 8), 8), expected: ErrInetDiagBcInvalid},
		{description: "corner: NOP's no is not checked", bc: bcOpB(InetDiagBcNop, 4, 3)},
		{description: "corner: a trailing 2 bytes", bc: append(bcOpB(InetDiagBcAuto, 4, 8), 0, 0), expected: ErrInetDiagBcInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			err := InetDiagBcAudit(tc.bc, tc.netAdmin)
			if tc.expected == nil {
				if err != nil {
					t.Fatalf("% x: %v", tc.bc, err)
				}
				return
			}
			if !errors.Is(err, tc.expected) {
				t.Fatalf("% x: err %v, want %v", tc.bc, err, tc.expected)
			}
		})
	}
}

func TestInetDiagBcRun(t *testing.T) {
	v4 := bcAddr("127.0.0.1")
	net4 := bcAddr("10.1.2.0")
	sport22 := bcPortB(InetDiagBcSEq, 22)
	sport80 := bcPortB(InetDiagBcSEq, 80)
	mark := bcU32OpB(t, InetDiagBcMarkCond, 0x10, 0xf0)
	lo22 := bcEntry("127.0.0.1", 22, "0.0.0.0", 0)
	lo80 := bcEntry("127.0.0.1", 80, "0.0.0.0", 0)
	mapped := bcEntry("::ffff:127.0.0.1", 22, "::", 0)
	v6lo := bcEntry("::1", 22, "::", 0)
	tests := []struct {
		description string
		bc          []byte
		entry       *InetDiagEntry
		expected    bool
	}{
		{description: "positive: sport = 22 on :22", bc: sport22, entry: lo22, expected: true},
		{description: "negative: sport = 22 on :80", bc: sport22, entry: lo80},
		{description: "boundary: sport >= 80 on :80", bc: bcPortB(InetDiagBcSGe, 80), entry: lo80, expected: true},
		{description: "boundary: sport <= 79 on :80", bc: bcPortB(InetDiagBcSLe, 79), entry: lo80},
		{description: "boundary: dport >= 0 on dport 0", bc: bcPortB(InetDiagBcDGe, 0), entry: lo22, expected: true},
		{description: "boundary: dport <= 65535", bc: bcPortB(InetDiagBcDLe, 65535), entry: bcEntry("127.0.0.1", 1, "127.0.0.1", 65535), expected: true},
		{description: "negative: dport = 1 on dport 0", bc: bcPortB(InetDiagBcDEq, 1), entry: lo22},
		{description: "positive: or, second branch", bc: bcOrB(t, sport22, sport80), entry: lo80, expected: true},
		{description: "negative: or, neither branch", bc: bcOrB(t, sport22, sport80), entry: bcEntry("127.0.0.1", 443, "0.0.0.0", 0)},
		{description: "positive: not sport 22 on :80", bc: bcNotB(sport22), entry: lo80, expected: true},
		{description: "negative: not sport 22 on :22", bc: bcNotB(sport22), entry: lo22},
		{description: "negative: and, first fails", bc: bcAndB(t, sport80, bcNotB(sport22)), entry: lo22},
		{description: "positive: and of an or and a not", bc: bcAndB(t, bcOrB(t, sport22, sport80), bcNotB(sport22)), entry: lo80, expected: true},
		{description: "positive: src 10.1.2.0/24 on 10.1.2.9", bc: bcHostB(t, InetDiagBcSCond, afInet, 24, anyPortCst, net4), entry: bcEntry("10.1.2.9", 1, "0.0.0.0", 0), expected: true},
		{description: "negative: src 10.1.2.0/24 on 10.1.3.9", bc: bcHostB(t, InetDiagBcSCond, afInet, 24, anyPortCst, net4), entry: bcEntry("10.1.3.9", 1, "0.0.0.0", 0)},
		{description: "boundary: /23 takes 10.1.3.9 by a partial byte", bc: bcHostB(t, InetDiagBcSCond, afInet, 23, anyPortCst, net4), entry: bcEntry("10.1.3.9", 1, "0.0.0.0", 0), expected: true},
		{description: "negative: hostcond port 80 on sport 22", bc: bcHostB(t, InetDiagBcSCond, afInet, 32, 80, v4), entry: lo22},
		{description: "positive: hostcond port 22 on sport 22", bc: bcHostB(t, InetDiagBcSCond, afInet, 32, 22, v4), entry: lo22, expected: true},
		{description: "positive: dst hostcond reads the destination", bc: bcHostB(t, InetDiagBcDCond, afInet, 8, anyPortCst, bcAddr("127.0.0.0")), entry: bcEntry("10.0.0.1", 1, "127.9.9.9", 2), expected: true},
		{description: "positive: AF_UNSPEC prefix 0 matches any family", bc: bcHostB(t, InetDiagBcSCond, afUnspec, 0, anyPortCst, nil), entry: v6lo, expected: true},
		{description: "corner: a v4 cond matches a v4-mapped v6 socket", bc: bcHostB(t, InetDiagBcSCond, afInet, 32, anyPortCst, v4), entry: mapped, expected: true},
		{description: "negative: a v4 cond on ::1", bc: bcHostB(t, InetDiagBcSCond, afInet, 0, anyPortCst, v4), entry: v6lo},
		{description: "negative: a v6 cond on a v4 socket", bc: bcHostB(t, InetDiagBcSCond, afInet6, 0, anyPortCst, bcAddr("::")), entry: lo22},
		{description: "positive: [::1]/128", bc: bcHostB(t, InetDiagBcSCond, afInet6, 128, anyPortCst, bcAddr("::1")), entry: v6lo, expected: true},
		{description: "positive: autobound with no bind lock", bc: bcOpB(InetDiagBcAuto, 4, 8), entry: lo22, expected: true},
		{description: "negative: autobound with SOCK_BINDPORT_LOCK", bc: bcOpB(InetDiagBcAuto, 4, 8), entry: &InetDiagEntry{Userlocks: sockBindportLock}},
		{description: "positive: dev 3", bc: bcU32OpB(t, InetDiagBcDevCond, 3), entry: &InetDiagEntry{Ifindex: 3}, expected: true},
		{description: "negative: dev 3 on 4", bc: bcU32OpB(t, InetDiagBcDevCond, 3), entry: &InetDiagEntry{Ifindex: 4}},
		{description: "positive: fwmark 0x10/0xf0 on 0x1f", bc: mark, entry: &InetDiagEntry{Mark: 0x1f}, expected: true},
		{description: "negative: fwmark 0x10/0xf0 on 0x20", bc: mark, entry: &InetDiagEntry{Mark: 0x20}},
		{description: "positive: cgroup 7", bc: bcCgroupB(7), entry: &InetDiagEntry{CgroupID: 7}, expected: true},
		{description: "negative: cgroup 7 on 8", bc: bcCgroupB(7), entry: &InetDiagEntry{CgroupID: 8}},
		{description: "corner: a zero step ends as a miss", bc: bcOpB(InetDiagBcJmp, 4, 0), entry: lo22},
		{description: "corner: a truncated op is a miss", bc: []byte{6, 4}, entry: lo22},
		{description: "corner: a port op cut short is a miss", bc: bcOpB(InetDiagBcSEq, 4, 8), entry: lo22},
		{description: "boundary: empty bytecode passes everything", bc: nil, entry: lo22, expected: true},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := InetDiagBcRun(tc.bc, tc.entry); got != tc.expected {
				t.Errorf("% x on %+v: got %v, want %v", tc.bc, *tc.entry, got, tc.expected)
			}
		})
	}
}

func TestBitstringMatch(t *testing.T) {
	tests := []struct {
		description string
		a, b        []byte
		bits        int
		expected    bool
	}{
		{description: "boundary: zero bits always match", a: nil, b: nil, bits: 0, expected: true},
		{description: "positive: one bit", a: []byte{0x80}, b: []byte{0xff}, bits: 1, expected: true},
		{description: "negative: one bit differs", a: []byte{0x00}, b: []byte{0x80}, bits: 1},
		{description: "boundary: 7 bits ignore the last", a: []byte{0xfe}, b: []byte{0xff}, bits: 7, expected: true},
		{description: "boundary: 8 bits see it", a: []byte{0xfe}, b: []byte{0xff}, bits: 8},
		{description: "positive: 9 bits", a: []byte{1, 0x80}, b: []byte{1, 0xc0}, bits: 9, expected: true},
		{description: "positive: 32 bits", a: []byte{1, 2, 3, 4}, b: []byte{1, 2, 3, 4}, bits: 32, expected: true},
		{description: "corner: a side too short is a miss", a: []byte{1}, b: []byte{1, 2}, bits: 9},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := bitstringMatch(tc.a, tc.b, tc.bits); got != tc.expected {
				t.Errorf("got %v, want %v", got, tc.expected)
			}
		})
	}
}
