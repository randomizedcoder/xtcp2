package xtcpnl

import (
	"encoding/binary"
	"testing"

	"golang.org/x/sys/unix"
)

func testAttr(typ uint16, value []byte) []byte {
	n := 4 + len(value)
	b := make([]byte, n+FourByteAlignPadding(n))
	binary.LittleEndian.PutUint16(b[0:2], uint16(n))
	binary.LittleEndian.PutUint16(b[2:4], typ)
	copy(b[4:], value)
	return b
}

func TestParseNewRoutePreservesSourceIdentity(t *testing.T) {
	b := make([]byte, RtMsgSizeCst)
	b[0], b[1], b[2], b[3] = unix.AF_INET, 24, 16, 7
	b = append(b, testAttr(uint16(unix.RTA_SRC), []byte{192, 0, 2, 0})...)
	r, err := ParseNewRoute(b)
	if err != nil {
		t.Fatal(err)
	}
	if r.SrcLen != 16 || r.Tos != 7 || len(r.Src) != 4 {
		t.Fatalf("lost route identity: %#v", r)
	}
}

func TestOptionalZeroAttributesRetainPresence(t *testing.T) {
	b := make([]byte, IfInfomsgSizeCst)
	b = append(b, testAttr(uint16(unix.IFLA_OPERSTATE), []byte{0})...)
	b = append(b, testAttr(uint16(unix.IFLA_CARRIER), []byte{0})...)
	b = append(b, testAttr(uint16(unix.IFLA_MTU), []byte{0, 0, 0, 0})...)
	b = append(b, testAttr(uint16(unix.IFLA_LINKMODE), []byte{0})...)
	li, err := ParseNewLink(b)
	if err != nil {
		t.Fatal(err)
	}
	if !li.HasOperState || !li.HasCarrier || !li.HasMTU || !li.HasLinkMode {
		t.Fatalf("presence flags not retained: %#v", li)
	}

	a := make([]byte, IfAddrmsgSizeCst)
	a = append(a, testAttr(IfaProto, []byte{0})...)
	ai, err := ParseNewAddr(a)
	if err != nil {
		t.Fatal(err)
	}
	if !ai.HasProto {
		t.Fatalf("IFA_PROTO zero lost: %#v", ai)
	}
}
