package service

import (
	"encoding/binary"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

type fakeSource struct {
	types  []uint16
	bodies map[uint16][][]byte
}

type fakeTalkSource struct {
	fakeSource
	talks int
	body  []byte
}

func (f *fakeTalkSource) Talk(_ []byte, _ uint16) ([]byte, error) {
	f.talks++
	return f.body, nil
}

func (f *fakeSource) Dump(_ []byte, typ uint16) ([][]byte, error) {
	f.types = append(f.types, typ)
	return f.bodies[typ], nil
}

func linkBody(index int32) []byte {
	b := make([]byte, xtcpnl.IfInfomsgSizeCst)
	b[0] = unix.AF_UNSPEC
	binary.LittleEndian.PutUint32(b[4:8], uint32(index))
	return b
}

func namedLinkBody(index int32, name string) []byte {
	b := linkBody(index)
	b = append(b, testServiceAttr(uint16(unix.IFLA_IFNAME), append([]byte(name), 0))...)
	return b
}

func testServiceAttr(typ uint16, value []byte) []byte {
	n := 4 + len(value)
	b := make([]byte, n+xtcpnl.FourByteAlignPadding(n))
	binary.LittleEndian.PutUint16(b[0:2], uint16(n))
	binary.LittleEndian.PutUint16(b[2:4], typ)
	copy(b[4:], value)
	return b
}

func addrBody(index uint32) []byte {
	b := make([]byte, xtcpnl.IfAddrmsgSizeCst)
	b[0], b[1] = unix.AF_INET, 24
	binary.LittleEndian.PutUint32(b[4:8], index)
	return b
}

func TestAddressSnapshotPlansLinkBeforeAddress(t *testing.T) {
	f := &fakeSource{bodies: map[uint16][][]byte{
		uint16(unix.RTM_NEWLINK): {linkBody(2)},
		uint16(unix.RTM_NEWADDR): {addrBody(2)},
	}}
	seq := uint32(0)
	s := New(f, func() uint32 { seq++; return seq })
	links, addrs, err := s.AddressSnapshot(unix.AF_INET)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || len(addrs) != 1 {
		t.Fatalf("got %d links, %d addresses", len(links), len(addrs))
	}
	if len(f.types) != 2 || f.types[0] != unix.RTM_NEWLINK || f.types[1] != unix.RTM_NEWADDR {
		t.Fatalf("transaction order = %v", f.types)
	}
}

func TestLinksAreSortedByIfindex(t *testing.T) {
	f := &fakeSource{bodies: map[uint16][][]byte{uint16(unix.RTM_NEWLINK): {linkBody(7), linkBody(2)}}}
	s := New(f, func() uint32 { return 1 })
	links, err := s.Links()
	if err != nil {
		t.Fatal(err)
	}
	if links[0].Index != 2 || links[1].Index != 7 {
		t.Fatalf("order = %#v", links)
	}
}

func TestLinkByNameUsesSingleReplyTransaction(t *testing.T) {
	f := &fakeTalkSource{body: namedLinkBody(3, "eth0")}
	s := New(f, func() uint32 { return 9 })
	link, err := s.LinkByName("eth0")
	if err != nil {
		t.Fatal(err)
	}
	if link.Index != 3 || link.Name != "eth0" {
		t.Fatalf("link = %#v", link)
	}
	if f.talks != 1 || len(f.types) != 0 {
		t.Fatalf("talks=%d dump types=%v", f.talks, f.types)
	}
}
