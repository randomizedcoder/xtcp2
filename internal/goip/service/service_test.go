package service

import (
	"encoding/binary"
	"errors"
	"reflect"
	"testing"

	"github.com/randomizedcoder/xtcp2/internal/goip/model"
	"github.com/randomizedcoder/xtcp2/internal/goip/req"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// These tests drive the read layer through a fake Source rather than a socket,
// so what they assert is the layer's own contract: which dumps it sends and in
// what order, whether it takes the single-get path when the source offers one,
// and which orderings it imposes on the decoded results. Byte-level request
// equality is req's job and decoding is pkg/xtcpnl's; neither is retested here.

var errSource = errors.New("source failed")

type fakeSource struct {
	types  []uint16
	bodies map[uint16][][]byte
	err    error

	// dumpReqs keeps every request handed to Dump, for the same reason
	// fakeTalkSource.reqs keeps its own: a dump filter such as
	// `neigh show dev NAME`'s NDA_IFINDEX changes the request and nothing
	// else, so a replay source that answers every request with the same
	// bodies makes it invisible in the return value.
	dumpReqs [][]byte
}

func (f *fakeSource) Dump(request []byte, typ uint16) ([][]byte, error) {
	f.types = append(f.types, typ)
	f.dumpReqs = append(f.dumpReqs, request)
	if f.err != nil {
		return nil, f.err
	}
	return f.bodies[typ], nil
}

// fakeTalkSource also satisfies TalkSource, which is what makes the service
// layer choose a kernel single-get over a dump-and-filter.
type fakeTalkSource struct {
	fakeSource
	talks int
	body  []byte
	err   error

	// reqs keeps every request handed to Talk. `ip link show dev NAME` sends
	// two single-gets that differ only in ifi_family and attribute order, so
	// which one a service method built is not visible in its return value —
	// only in the bytes it put on the socket.
	reqs [][]byte
}

func (f *fakeTalkSource) Talk(request []byte, _ uint16) ([]byte, error) {
	f.talks++
	f.reqs = append(f.reqs, request)
	if f.err != nil {
		return nil, f.err
	}
	return f.body, nil
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

func altNamedLinkBody(index int32, name, altName string) []byte {
	b := namedLinkBody(index, name)
	inner := testServiceAttr(uint16(unix.IFLA_ALT_IFNAME), append([]byte(altName), 0))
	b = append(b, testServiceAttr(uint16(unix.IFLA_PROP_LIST)|unix.NLA_F_NESTED, inner)...)
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

// routeBody builds a minimal rtmsg. Table is the tag the ordering assertions
// read, because it is the field model.SortRoutes ranks first — so a route list
// tagged through Table is the sharpest witness of whether a sort ran.
func routeBody(family, table uint8) []byte {
	b := make([]byte, xtcpnl.RtMsgSizeCst)
	b[0] = family
	b[4] = table
	return b
}

func neighBody(ifindex int32, family uint8) []byte {
	b := make([]byte, xtcpnl.NdMsgSizeCst)
	b[0] = family
	binary.LittleEndian.PutUint32(b[4:8], uint32(ifindex))
	return b
}

func newService(src Source) *Service {
	seq := uint32(0)
	return New(src, func() uint32 { seq++; return seq })
}

func TestAddressSnapshot(t *testing.T) {
	tests := []struct {
		description string
		family      uint8
		bodies      map[uint16][][]byte
		srcErr      error
		wantTypes   []uint16
		wantLinks   int
		wantAddrs   int
		wantErr     bool
	}{
		{
			// The ordering that matters: iproute2 fills its name cache from a
			// link dump before asking for addresses, and a renderer that has
			// not seen the links cannot label an address with a device.
			description: "positive: the link dump precedes the address dump",
			family:      unix.AF_INET,
			bodies: map[uint16][][]byte{
				uint16(unix.RTM_NEWLINK): {linkBody(2)},
				uint16(unix.RTM_NEWADDR): {addrBody(2)},
			},
			wantTypes: []uint16{uint16(unix.RTM_NEWLINK), uint16(unix.RTM_NEWADDR)},
			wantLinks: 1,
			wantAddrs: 1,
		},
		{
			// AF_PACKET is `ip link show` reached through the address path, and
			// it asks for no addresses at all — one transaction, not two. A
			// second dump here would be a transaction-count divergence against
			// `ip`.
			description: "boundary: AF_PACKET sends the link dump only",
			family:      unix.AF_PACKET,
			bodies: map[uint16][][]byte{
				uint16(unix.RTM_NEWLINK): {linkBody(2)},
				uint16(unix.RTM_NEWADDR): {addrBody(2)},
			},
			wantTypes: []uint16{uint16(unix.RTM_NEWLINK)},
			wantLinks: 1,
			wantAddrs: 0,
		},
		{
			description: "boundary: a host with no links returns two empty collections, not an error",
			family:      unix.AF_INET,
			bodies:      map[uint16][][]byte{},
			wantTypes:   []uint16{uint16(unix.RTM_NEWLINK), uint16(unix.RTM_NEWADDR)},
			wantLinks:   0,
			wantAddrs:   0,
		},
		{
			description: "negative: a source error surfaces and stops after the first dump",
			family:      unix.AF_INET,
			srcErr:      errSource,
			wantTypes:   []uint16{uint16(unix.RTM_NEWLINK)},
			wantErr:     true,
		},
		{
			// A body too short for its family header is a decode failure, and
			// it must fail the call rather than yield a zero-valued link that
			// later renders as interface 0.
			description: "corner: an undersized link body fails the snapshot",
			family:      unix.AF_INET,
			bodies: map[uint16][][]byte{
				uint16(unix.RTM_NEWLINK): {make([]byte, 3)},
			},
			wantTypes: []uint16{uint16(unix.RTM_NEWLINK)},
			wantErr:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			f := &fakeSource{bodies: tc.bodies, err: tc.srcErr}
			links, addrs, err := newService(f).AddressSnapshot(tc.family)
			if !reflect.DeepEqual(f.types, tc.wantTypes) {
				t.Errorf("transaction order = %v, want %v", f.types, tc.wantTypes)
			}
			if tc.wantErr {
				if err == nil {
					t.Fatalf("AddressSnapshot = %d links, %d addrs, want error", len(links), len(addrs))
				}
				return
			}
			if err != nil {
				t.Fatalf("AddressSnapshot: %v", err)
			}
			if len(links) != tc.wantLinks || len(addrs) != tc.wantAddrs {
				t.Errorf("got %d links, %d addresses; want %d, %d", len(links), len(addrs), tc.wantLinks, tc.wantAddrs)
			}
		})
	}
}

func TestLinks(t *testing.T) {
	tests := []struct {
		description string
		bodies      [][]byte
		srcErr      error
		wantIndexes []int32
		wantErr     bool
	}{
		{
			description: "positive: links come back ordered by ifindex",
			bodies:      [][]byte{linkBody(7), linkBody(2)},
			wantIndexes: []int32{2, 7},
		},
		{
			// A kernel dump is already index-ordered, so this is the shape the
			// sort is a no-op on — and the row that would still pass if the
			// sort were removed, which is why the row above exists too.
			description: "negative: an already-ordered dump is returned unchanged",
			bodies:      [][]byte{linkBody(1), linkBody(2), linkBody(3)},
			wantIndexes: []int32{1, 2, 3},
		},
		{
			description: "boundary: an empty dump returns an empty slice",
			bodies:      nil,
			wantIndexes: []int32{},
		},
		{
			description: "negative: a source error is returned, not swallowed into an empty list",
			srcErr:      errSource,
			wantErr:     true,
		},
		{
			description: "corner: an undersized body fails the call",
			bodies:      [][]byte{linkBody(1), make([]byte, 3)},
			wantErr:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			f := &fakeSource{
				bodies: map[uint16][][]byte{uint16(unix.RTM_NEWLINK): tc.bodies},
				err:    tc.srcErr,
			}
			links, err := newService(f).Links(req.ExtMaskShow)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("Links = %#v, want error", links)
				}
				return
			}
			if err != nil {
				t.Fatalf("Links: %v", err)
			}
			got := make([]int32, 0, len(links))
			for i := range links {
				got = append(got, links[i].Index)
			}
			if !reflect.DeepEqual(got, tc.wantIndexes) {
				t.Errorf("indexes = %v, want %v", got, tc.wantIndexes)
			}
		})
	}
}

func TestLinkByName(t *testing.T) {
	tests := []struct {
		description string
		talkBody    []byte   // non-nil selects a TalkSource
		talkErr     error    //
		dumpBodies  [][]byte // used when talkBody is nil
		name        string
		wantIndex   int32
		wantTalks   int
		wantDumps   int
		wantErr     bool
	}{
		{
			// A source that can Talk must be asked once and never dumped: the
			// dump would be a whole extra transaction `ip` does not send.
			description: "positive: a Talk-capable source answers with one single-get and no dump",
			talkBody:    namedLinkBody(3, "eth0"),
			name:        "eth0",
			wantIndex:   3,
			wantTalks:   1,
			wantDumps:   0,
		},
		{
			// IFLA_PROP_LIST altnames are addressable by `ip link show dev`, so
			// a match on one is a match.
			description: "positive: an altname matches as well as the primary name",
			talkBody:    altNamedLinkBody(4, "eth1", "enp3s0"),
			name:        "enp3s0",
			wantIndex:   4,
			wantTalks:   1,
		},
		{
			// The kernel answers a by-name get by name, so a reply for a
			// different link means the request and the reply were mispaired —
			// worth an error rather than returning the wrong interface.
			description: "negative: a reply naming a different link is rejected",
			talkBody:    namedLinkBody(3, "eth0"),
			name:        "eth9",
			wantTalks:   1,
			wantErr:     true,
		},
		{
			description: "negative: a Talk error surfaces",
			talkBody:    namedLinkBody(3, "eth0"),
			talkErr:     errSource,
			name:        "eth0",
			wantTalks:   1,
			wantErr:     true,
		},
		{
			// A replay source has no Talk, so the layer falls back to one dump
			// plus exact filtering. Divergent from `ip` on the wire and
			// deliberately so: a capture cannot answer a request it never
			// recorded.
			description: "boundary: a dump-only source falls back to one dump and filters exactly",
			dumpBodies:  [][]byte{namedLinkBody(1, "lo"), namedLinkBody(3, "eth0")},
			name:        "eth0",
			wantIndex:   3,
			wantDumps:   1,
		},
		{
			description: "corner: a name absent from the fallback dump is an error, not a zero link",
			dumpBodies:  [][]byte{namedLinkBody(1, "lo")},
			name:        "eth0",
			wantDumps:   1,
			wantErr:     true,
		},
		{
			// Substring matching would make `ip link show dev eth` resolve
			// eth0; the comparison is exact.
			description: "corner: a name that is a prefix of a real link does not match it",
			dumpBodies:  [][]byte{namedLinkBody(3, "eth0")},
			name:        "eth",
			wantDumps:   1,
			wantErr:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var (
				index int32
				err   error
				talks int
				dumps int
			)
			if tc.talkBody != nil {
				f := &fakeTalkSource{body: tc.talkBody, err: tc.talkErr}
				l, e := newService(f).LinkByName(tc.name)
				index, err, talks, dumps = l.Index, e, f.talks, len(f.types)
			} else {
				f := &fakeSource{bodies: map[uint16][][]byte{uint16(unix.RTM_NEWLINK): tc.dumpBodies}}
				l, e := newService(f).LinkByName(tc.name)
				index, err, dumps = l.Index, e, len(f.types)
			}
			if talks != tc.wantTalks {
				t.Errorf("Talk calls = %d, want %d", talks, tc.wantTalks)
			}
			if dumps != tc.wantDumps {
				t.Errorf("Dump calls = %d, want %d", dumps, tc.wantDumps)
			}
			if tc.wantErr {
				if err == nil {
					t.Fatalf("LinkByName = index %d, want error", index)
				}
				return
			}
			if err != nil {
				t.Fatalf("LinkByName: %v", err)
			}
			if index != tc.wantIndex {
				t.Errorf("index = %d, want %d", index, tc.wantIndex)
			}
		})
	}
}

// TestLinkShowDev covers the second of the two by-name single-gets, and the
// one whose reply is printed.
//
// Its answering and fallback behavior is LinkByName's — same match rule, same
// dump-and-filter when the source cannot Talk — so the rows here concentrate
// on the part that is NOT shared: the request it builds. A method that simply
// delegated to LinkByName would satisfy every other assertion in this file
// while sending iproute2's first request twice and its second one never.
//
// go test ./internal/goip/service/ -run TestLinkShowDev
func TestLinkShowDev(t *testing.T) {
	tests := []struct {
		description string
		talkBody    []byte   // non-nil selects a TalkSource
		talkErr     error    //
		dumpBodies  [][]byte // used when talkBody is nil
		name        string
		wantIndex   int32
		wantTalks   int
		wantDumps   int
		wantErr     bool
	}{
		{
			description: "positive: a Talk-capable source answers with one single-get and no dump",
			talkBody:    namedLinkBody(3, "eth0"),
			name:        "eth0",
			wantIndex:   3,
			wantTalks:   1,
			wantDumps:   0,
		},
		{
			// print_linkinfo renders this reply, and it renders altnames, so an
			// altname selector has to resolve here as well as in LinkByName.
			description: "positive: an altname matches as well as the primary name",
			talkBody:    altNamedLinkBody(4, "eth1", "enp3s0"),
			name:        "enp3s0",
			wantIndex:   4,
			wantTalks:   1,
		},
		{
			description: "negative: a reply naming a different link is rejected",
			talkBody:    namedLinkBody(3, "eth0"),
			name:        "eth9",
			wantTalks:   1,
			wantErr:     true,
		},
		{
			description: "negative: a Talk error surfaces",
			talkBody:    namedLinkBody(3, "eth0"),
			talkErr:     errSource,
			name:        "eth0",
			wantTalks:   1,
			wantErr:     true,
		},
		{
			// A capture cannot answer a request it never recorded, so replay
			// sources take the same fallback LinkByName takes. Divergent from
			// `ip` on the wire, and deliberate.
			description: "boundary: a dump-only source falls back to one dump and filters exactly",
			dumpBodies:  [][]byte{namedLinkBody(1, "lo"), namedLinkBody(3, "eth0")},
			name:        "eth0",
			wantIndex:   3,
			wantDumps:   1,
		},
		{
			description: "corner: a name absent from the fallback dump is an error, not a zero link",
			dumpBodies:  [][]byte{namedLinkBody(1, "lo")},
			name:        "eth0",
			wantDumps:   1,
			wantErr:     true,
		},
		{
			description: "negative: an unbuildable name never reaches the source",
			talkBody:    namedLinkBody(3, "eth0"),
			name:        "this-name-is-too-long",
			wantTalks:   0,
			wantErr:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var (
				index int32
				err   error
				talks int
				dumps int
				reqs  [][]byte
			)
			if tc.talkBody != nil {
				f := &fakeTalkSource{body: tc.talkBody, err: tc.talkErr}
				l, e := newService(f).LinkShowDev(tc.name, req.ExtMaskShow)
				index, err, talks, dumps, reqs = l.Index, e, f.talks, len(f.types), f.reqs
			} else {
				f := &fakeSource{bodies: map[uint16][][]byte{uint16(unix.RTM_NEWLINK): tc.dumpBodies}}
				l, e := newService(f).LinkShowDev(tc.name, req.ExtMaskShow)
				index, err, dumps = l.Index, e, len(f.types)
			}
			if talks != tc.wantTalks {
				t.Errorf("Talk calls = %d, want %d", talks, tc.wantTalks)
			}
			if dumps != tc.wantDumps {
				t.Errorf("Dump calls = %d, want %d", dumps, tc.wantDumps)
			}
			// Whatever else the row asserts, any request that was sent must be
			// iplink_get's and not ll_link_get's.
			for i := range reqs {
				assertIplinkGetShape(t, reqs[i])
			}
			if tc.wantErr {
				if err == nil {
					t.Fatalf("LinkShowDev = index %d, want error", index)
				}
				return
			}
			if err != nil {
				t.Fatalf("LinkShowDev: %v", err)
			}
			if index != tc.wantIndex {
				t.Errorf("index = %d, want %d", index, tc.wantIndex)
			}
		})
	}
}

// assertIplinkGetShape checks the two fields that distinguish iplink_get's
// request from ll_link_get's: ifi_family AF_PACKET rather than AF_UNSPEC
// (ip/iplink.c:1502 with preferred_family forced at ip/ipaddress.c:2416), and
// IFLA_IFNAME ahead of IFLA_EXT_MASK rather than behind it (:1513-1514).
func assertIplinkGetShape(t *testing.T, request []byte) {
	t.Helper()
	const attrsAtCst = xtcpnl.NlMsgHdrSizeCst + xtcpnl.IfInfomsgSizeCst
	if len(request) < attrsAtCst+4 {
		t.Fatalf("request is %d bytes, too short to carry an attribute", len(request))
	}
	if fam := request[xtcpnl.NlMsgHdrSizeCst]; fam != unix.AF_PACKET {
		t.Errorf("ifi_family = %d, want AF_PACKET — this is ll_link_get's request, not iplink_get's", fam)
	}
	if at := binary.LittleEndian.Uint16(request[attrsAtCst+2 : attrsAtCst+4]); at != uint16(unix.IFLA_IFNAME) {
		t.Errorf("first attribute = %d, want IFLA_IFNAME — the attributes are in ll_link_get's order", at)
	}
}

// TestLinkByNameSendsLlLinkGetShape is assertIplinkGetShape's counterpart, and
// exists so the pair of tests cannot both pass on one request shape.
//
// Without it, a LinkByName rewritten to call req.LinkShowDev would break
// nothing here: the returned link would be identical and every row in
// TestLinkByName would still pass. The command would then send iproute2's
// second request twice, which stdout cannot see and the parity harness can.
//
// go test ./internal/goip/service/ -run TestLinkByNameSendsLlLinkGetShape
func TestLinkByNameSendsLlLinkGetShape(t *testing.T) {
	tests := []struct {
		description   string
		call          func(*Service) (model.Link, error)
		wantFamily    uint8
		wantFirstAttr uint16
	}{
		{
			description:   "positive: LinkByName sends ll_link_get — AF_UNSPEC, ext-mask first",
			call:          func(s *Service) (model.Link, error) { return s.LinkByName("eth0") },
			wantFamily:    unix.AF_UNSPEC,
			wantFirstAttr: uint16(unix.IFLA_EXT_MASK),
		},
		{
			description:   "positive: LinkShowDev sends iplink_get — AF_PACKET, name first",
			call:          func(s *Service) (model.Link, error) { return s.LinkShowDev("eth0", req.ExtMaskShow) },
			wantFamily:    unix.AF_PACKET,
			wantFirstAttr: uint16(unix.IFLA_IFNAME),
		},
	}

	const attrsAtCst = xtcpnl.NlMsgHdrSizeCst + xtcpnl.IfInfomsgSizeCst

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			f := &fakeTalkSource{body: namedLinkBody(3, "eth0")}
			if _, err := tc.call(newService(f)); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(f.reqs) != 1 {
				t.Fatalf("Talk calls = %d, want 1", len(f.reqs))
			}
			r := f.reqs[0]
			if fam := r[xtcpnl.NlMsgHdrSizeCst]; fam != tc.wantFamily {
				t.Errorf("ifi_family = %d, want %d", fam, tc.wantFamily)
			}
			if at := binary.LittleEndian.Uint16(r[attrsAtCst+2 : attrsAtCst+4]); at != tc.wantFirstAttr {
				t.Errorf("first attribute = %d, want %d", at, tc.wantFirstAttr)
			}
		})
	}
}

func TestLinkByIndex(t *testing.T) {
	tests := []struct {
		description string
		talkBody    []byte
		dumpBodies  [][]byte
		index       int32
		wantName    string
		wantTalks   int
		wantDumps   int
		wantErr     bool
	}{
		{
			description: "positive: a Talk-capable source answers with one single-get and no dump",
			talkBody:    namedLinkBody(3, "eth0"),
			index:       3,
			wantName:    "eth0",
			wantTalks:   1,
		},
		{
			description: "negative: a reply for a different index is rejected",
			talkBody:    namedLinkBody(3, "eth0"),
			index:       9,
			wantTalks:   1,
			wantErr:     true,
		},
		{
			// This is the path `route show` takes under replay: ReplaySource has
			// no Talk, so a lazy name resolution becomes one dump whose replies
			// are the recorded single-gets.
			description: "boundary: a dump-only source falls back to one dump and filters by index",
			dumpBodies:  [][]byte{namedLinkBody(1, "lo"), namedLinkBody(3, "eth0")},
			index:       3,
			wantName:    "eth0",
			wantDumps:   1,
		},
		{
			description: "corner: an index absent from the fallback dump is an error",
			dumpBodies:  [][]byte{namedLinkBody(1, "lo")},
			index:       3,
			wantDumps:   1,
			wantErr:     true,
		},
		{
			// Index 0 is never a real interface — it is what lltab returns for
			// an unresolvable name — so it must not silently match anything.
			description: "corner: index 0 resolves to nothing",
			dumpBodies:  [][]byte{namedLinkBody(1, "lo")},
			index:       0,
			wantDumps:   1,
			wantErr:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var (
				name  string
				err   error
				talks int
				dumps int
			)
			if tc.talkBody != nil {
				f := &fakeTalkSource{body: tc.talkBody}
				l, e := newService(f).LinkByIndex(tc.index)
				name, err, talks, dumps = l.Name, e, f.talks, len(f.types)
			} else {
				f := &fakeSource{bodies: map[uint16][][]byte{uint16(unix.RTM_NEWLINK): tc.dumpBodies}}
				l, e := newService(f).LinkByIndex(tc.index)
				name, err, dumps = l.Name, e, len(f.types)
			}
			if talks != tc.wantTalks {
				t.Errorf("Talk calls = %d, want %d", talks, tc.wantTalks)
			}
			if dumps != tc.wantDumps {
				t.Errorf("Dump calls = %d, want %d", dumps, tc.wantDumps)
			}
			if tc.wantErr {
				if err == nil {
					t.Fatalf("LinkByIndex = %q, want error", name)
				}
				return
			}
			if err != nil {
				t.Fatalf("LinkByIndex: %v", err)
			}
			if name != tc.wantName {
				t.Errorf("name = %q, want %q", name, tc.wantName)
			}
		})
	}
}

func TestRoutes(t *testing.T) {
	tests := []struct {
		description string
		bodies      [][]byte
		srcErr      error
		wantTables  []uint32
		wantErr     bool
	}{
		{
			// The regression guard for the un-sort. `ip route show table all`
			// prints v4-main, v4-local, v6-main, v6-local, and the input below
			// is that order. model.SortRoutes ranks Table above Family, so if
			// Routes sorted, this would come back 254, 254, 255, 255 and every
			// table-all golden would fail on line 9.
			description: "positive: the kernel's dump order is preserved, not canonically sorted",
			bodies: [][]byte{
				routeBody(unix.AF_INET, unix.RT_TABLE_MAIN),
				routeBody(unix.AF_INET, unix.RT_TABLE_LOCAL),
				routeBody(unix.AF_INET6, unix.RT_TABLE_MAIN),
				routeBody(unix.AF_INET6, unix.RT_TABLE_LOCAL),
			},
			wantTables: []uint32{unix.RT_TABLE_MAIN, unix.RT_TABLE_LOCAL, unix.RT_TABLE_MAIN, unix.RT_TABLE_LOCAL},
		},
		{
			description: "negative: a single-table dump is likewise returned as it arrived",
			bodies: [][]byte{
				routeBody(unix.AF_INET, unix.RT_TABLE_MAIN),
				routeBody(unix.AF_INET, unix.RT_TABLE_MAIN),
			},
			wantTables: []uint32{unix.RT_TABLE_MAIN, unix.RT_TABLE_MAIN},
		},
		{
			description: "boundary: an empty routing table returns an empty slice and no error",
			bodies:      nil,
			wantTables:  []uint32{},
		},
		{
			description: "negative: a source error is returned",
			srcErr:      errSource,
			wantErr:     true,
		},
		{
			description: "corner: a body shorter than the rtmsg header fails the call",
			bodies:      [][]byte{make([]byte, xtcpnl.RtMsgSizeCst-1)},
			wantErr:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			f := &fakeSource{
				bodies: map[uint16][][]byte{uint16(unix.RTM_NEWROUTE): tc.bodies},
				err:    tc.srcErr,
			}
			routes, err := newService(f).Routes(unix.AF_UNSPEC, unix.RT_TABLE_UNSPEC, 0)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("Routes = %#v, want error", routes)
				}
				return
			}
			if err != nil {
				t.Fatalf("Routes: %v", err)
			}
			got := make([]uint32, 0, len(routes))
			for i := range routes {
				got = append(got, routes[i].Table)
			}
			if !reflect.DeepEqual(got, tc.wantTables) {
				t.Errorf("tables = %v, want %v", got, tc.wantTables)
			}
		})
	}
}

// TestNeighborDumps drives the two calls `ip neigh show` makes, in the order
// it makes them, through the split that `dev NAME` forced: NeighborLinks is
// ll_init_map (ip/ipneigh.c:597) and Neighbors is the dump after it.
//
// The split is the subject as much as the results are. The name is resolved
// BETWEEN the two calls, out of the first one's replies, which is why there is
// no combined snapshot method any more and why `dev NAME` adds no transaction.
func TestNeighborDumps(t *testing.T) {
	tests := []struct {
		description string
		bodies      map[uint16][][]byte
		srcErr      error
		// devIndex is the resolved `dev NAME` index, 0 for the bare command.
		devIndex      uint32
		wantTypes     []uint16
		wantIfindexes []int32
		// wantNeighReqLen is the length of the RTM_GETNEIGH request, which is
		// the only place the device filter is visible: 28 bytes bare, 36 with
		// NDA_IFINDEX. Zero skips the check.
		wantNeighReqLen int
		wantErr         bool
	}{
		{
			// Same ordering contract as AddressSnapshot, and the same reason:
			// a neighbor without a resolved device name renders `dev if3`.
			description: "positive: the link dump precedes the neighbor dump",
			bodies: map[uint16][][]byte{
				uint16(unix.RTM_NEWLINK):  {linkBody(2)},
				uint16(unix.RTM_NEWNEIGH): {neighBody(2, unix.AF_INET)},
			},
			wantTypes:       []uint16{uint16(unix.RTM_NEWLINK), uint16(unix.RTM_NEWNEIGH)},
			wantIfindexes:   []int32{2},
			wantNeighReqLen: xtcpnl.NlMsgHdrSizeCst + xtcpnl.NdMsgSizeCst,
		},
		{
			// The whole of `neigh show dev NAME` on the wire: the same two
			// transactions in the same order, and eight more bytes on the
			// second. Contrast route, where the selector added a transaction
			// at the front and removed the lazy ones at the back.
			description: "positive: a device filter adds 8 bytes to the dump request and no transaction",
			bodies: map[uint16][][]byte{
				uint16(unix.RTM_NEWLINK):  {linkBody(2)},
				uint16(unix.RTM_NEWNEIGH): {neighBody(2, unix.AF_INET)},
			},
			devIndex:      2,
			wantTypes:     []uint16{uint16(unix.RTM_NEWLINK), uint16(unix.RTM_NEWNEIGH)},
			wantIfindexes: []int32{2},
			// 8 = the NDA_IFINDEX attribute: 4 bytes of rtattr header and a
			// u32 payload, already aligned.
			wantNeighReqLen: xtcpnl.NlMsgHdrSizeCst + xtcpnl.NdMsgSizeCst + 8,
		},
		{
			// Neighbors ARE sorted, unlike routes: the neighbor cache has no
			// dump order worth preserving — it is hash-table order, which
			// varies between two dumps of an unchanged cache.
			description: "positive: neighbors come back ordered by ifindex",
			bodies: map[uint16][][]byte{
				uint16(unix.RTM_NEWLINK):  {linkBody(2)},
				uint16(unix.RTM_NEWNEIGH): {neighBody(7, unix.AF_INET), neighBody(2, unix.AF_INET)},
			},
			wantTypes:     []uint16{uint16(unix.RTM_NEWLINK), uint16(unix.RTM_NEWNEIGH)},
			wantIfindexes: []int32{2, 7},
		},
		{
			description:   "boundary: an empty neighbor cache returns an empty slice",
			bodies:        map[uint16][][]byte{uint16(unix.RTM_NEWLINK): {linkBody(2)}},
			wantTypes:     []uint16{uint16(unix.RTM_NEWLINK), uint16(unix.RTM_NEWNEIGH)},
			wantIfindexes: []int32{},
		},
		{
			description: "negative: a source error stops after the link dump",
			srcErr:      errSource,
			wantTypes:   []uint16{uint16(unix.RTM_NEWLINK)},
			wantErr:     true,
		},
		{
			description: "corner: an undersized neighbor body fails the snapshot",
			bodies: map[uint16][][]byte{
				uint16(unix.RTM_NEWLINK):  {linkBody(2)},
				uint16(unix.RTM_NEWNEIGH): {make([]byte, 3)},
			},
			wantTypes: []uint16{uint16(unix.RTM_NEWLINK), uint16(unix.RTM_NEWNEIGH)},
			wantErr:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			f := &fakeSource{bodies: tc.bodies, err: tc.srcErr}
			svc := newService(f)

			var neighbors []model.Neighbor
			_, err := svc.NeighborLinks()
			if err == nil {
				// Only reached when the link dump succeeded, which is what
				// makes the "stops after the link dump" row assert a real
				// ordering rather than an accident of the fake.
				neighbors, err = svc.Neighbors(unix.AF_INET, 0, tc.devIndex)
			}

			if !reflect.DeepEqual(f.types, tc.wantTypes) {
				t.Errorf("transaction order = %v, want %v", f.types, tc.wantTypes)
			}
			if tc.wantErr {
				if err == nil {
					t.Fatalf("neighbor dumps = %#v, want error", neighbors)
				}
				return
			}
			if err != nil {
				t.Fatalf("neighbor dumps: %v", err)
			}
			if tc.wantNeighReqLen != 0 {
				got := len(f.dumpReqs[len(f.dumpReqs)-1])
				if got != tc.wantNeighReqLen {
					t.Errorf("RTM_GETNEIGH request = %d bytes, want %d", got, tc.wantNeighReqLen)
				}
			}
			got := make([]int32, 0, len(neighbors))
			for i := range neighbors {
				got = append(got, neighbors[i].Ifindex)
			}
			if !reflect.DeepEqual(got, tc.wantIfindexes) {
				t.Errorf("ifindexes = %v, want %v", got, tc.wantIfindexes)
			}
		})
	}
}
