package xtcpnl

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// The bridge mdb rows' oracles (coverage-status.md, "W5b"). O3 rebuilds
// the request from the row's axes alone (tools/nlcapgen/matrix_mdb.go),
// following bridge/mdb.c mdb_modify, and checks the nested attributes in
// the order it adds them. O4 looks for the entry in the device's MDB dump
// read back after it, or for its absence after a delete.

// mdbWant is the request a row's axes call for. The indexes (br_port_msg's,
// the entry's and MDBE_ATTR_IFINDEX) are left 0: they are dev's, bport's
// and via's, as the kernel numbered them.
type mdbWant struct {
	typ, flags      uint16
	dev, bport, via string
	req             MdbRequest
	nest            []uint16 // the MDBE_ATTR_* sent, in mdb_modify's order; nil for no nest
}

// mdbVerbs is each verb's message type and the flags do_mdb passes.
var mdbVerbs = map[string]struct{ typ, flags uint16 }{
	"add":     {unix.RTM_NEWMDB, unix.NLM_F_CREATE | unix.NLM_F_EXCL},
	"replace": {unix.RTM_NEWMDB, unix.NLM_F_CREATE | unix.NLM_F_REPLACE},
	"del":     {unix.RTM_DELMDB, 0},
}

// mdbAddr is an address axis's bytes: 4 for IPv4, 16 for IPv6.
func mdbAddr(s string) ([]byte, error) {
	a, err := netip.ParseAddr(s)
	if err != nil {
		return nil, err
	}
	if a.Is4() {
		b := a.As4()
		return b[:], nil
	}
	b := a.As16()
	return b[:], nil
}

// mdbGroup is mdb_parse_grp: an IPv4 or IPv6 address, else a MAC.
func mdbGroup(s string) (proto uint16, g []byte, err error) {
	if a, err := netip.ParseAddr(s); err == nil {
		g, _ := mdbAddr(s)
		if a.Is4() {
			return MdbProtoIP, g, nil
		}
		return MdbProtoIPv6, g, nil
	}
	mac, err := net.ParseMAC(s)
	if err != nil || len(mac) != 6 {
		return 0, nil, fmt.Errorf("group %q is no address", s)
	}
	return 0, []byte(mac), nil
}

// mdbNumAxis reads an optional number axis no wider than bits.
func mdbNumAxis(ax map[string]string, k string, bits int) (uint64, bool, error) {
	s, ok := ax[k]
	if !ok {
		return 0, false, nil
	}
	n, err := strconv.ParseUint(s, 10, bits)
	if err != nil {
		return 0, false, fmt.Errorf("axis %s=%q: %w", k, s, err)
	}
	return n, true, nil
}

// mdbWantFromAxes models mdb_modify. permanent reaches only an add's state;
// the entry's VLAN is always sent, 0 when not given; the nest is sent when
// any of its keywords is, with its attributes in the order mdb_modify adds
// them.
func mdbWantFromAxes(ax map[string]string) (mdbWant, error) {
	verb, ok := mdbVerbs[ax["verb"]]
	if !ok {
		return mdbWant{}, fmt.Errorf("axis verb=%q", ax["verb"])
	}
	w := mdbWant{typ: verb.typ, flags: verb.flags, dev: ax["dev"], bport: ax["bport"], via: ax["via"]}
	if w.dev == "" || w.bport == "" {
		return w, errors.New("axis dev or bport missing")
	}
	proto, g, err := mdbGroup(ax["grp"])
	state, _, err2 := mdbNumAxis(ax, "state", 8)
	vid, _, err3 := mdbNumAxis(ax, "vid", 16)
	if err := errors.Join(err, err2, err3); err != nil {
		return w, err
	}
	if verb.typ == unix.RTM_DELMDB {
		state = 0
	}
	w.req = MdbRequest{Family: unix.AF_BRIDGE, HasEntry: true,
		Entry: MdbEntry{State: uint8(state), Vid: uint16(vid), Proto: proto, Group: g}}
	if err := mdbWantAttrs(ax, &w); err != nil {
		return w, err
	}
	return w, nil
}

// mdbWantAttrs fills w's nest from the axes, in mdb_modify's order: src,
// filter_mode, source_list, proto, dst, dst_port, vni, src_vni, via.
func mdbWantAttrs(ax map[string]string, w *mdbWant) error {
	a := &w.req.Attrs
	var errs []error
	add := func(attr uint16, set func() error) {
		if err := set(); err != nil {
			errs = append(errs, err)
			return
		}
		w.nest = append(w.nest, attr)
	}
	num := func(k string, bits int, set func(uint64)) func() error {
		return func() error {
			n, _, err := mdbNumAxis(ax, k, bits)
			set(n)
			return err
		}
	}
	addr := func(k string, dst *[]byte) func() error {
		return func() (err error) { *dst, err = mdbAddr(ax[k]); return err }
	}
	for _, s := range []struct {
		key  string
		attr uint16
		set  func() error
	}{
		{"src", MdbeAttrSource, addr("src", &a.Source)},
		{"mode", MdbeAttrGroupMode, num("mode", 8, func(n uint64) { a.GroupMode, a.HasGroupMode = uint8(n), true })},
		{"srclist", MdbeAttrSrcList, func() error { return mdbWantSrcList(ax["srclist"], a) }},
		{"proto", MdbeAttrRtProt, num("proto", 8, func(n uint64) { a.RtProt, a.HasRtProt = uint8(n), true })},
		{"dst", MdbeAttrDst, addr("dst", &a.Dst)},
		{"dport", MdbeAttrDstPort, num("dport", 16, func(n uint64) { a.DstPort, a.HasDstPort = uint16(n), true })},
		{"vni", MdbeAttrVni, num("vni", 32, func(n uint64) { a.Vni, a.HasVni = uint32(n), true })},
		{"src_vni", MdbeAttrSrcVni, num("src_vni", 32, func(n uint64) { a.SrcVni, a.HasSrcVni = uint32(n), true })},
		{"via", MdbeAttrIfindex, func() error { a.HasIfindex = true; return nil }},
	} {
		if _, ok := ax[s.key]; ok {
			add(s.attr, s.set)
		}
	}
	w.req.HasAttrs = w.nest != nil
	return errors.Join(errs...)
}

// mdbWantSrcList is source_list's comma-separated addresses.
func mdbWantSrcList(s string, a *MdbAttrs) error {
	a.HasSrcList = true
	for _, e := range strings.Split(s, ",") {
		b, err := mdbAddr(e)
		if err != nil {
			return err
		}
		a.SrcList = append(a.SrcList, b)
	}
	return nil
}

// mdbRequestProblem is O3 for one request: its type and flags, its
// attribute order (MDBA_SET_ENTRY, then the nest, whose attributes are in
// w.nest's order), and its decoded values. dev, bport and via are the
// indexes the kernel gave those devices.
func mdbRequestProblem(w mdbWant, typ, flags uint16, body []byte, dev, bport, via uint32) error {
	var errs []error
	if typ != w.typ {
		errs = append(errs, fmt.Errorf("type %d, want %d", typ, w.typ))
	}
	if got := flags &^ (unix.NLM_F_REQUEST | unix.NLM_F_ACK); got != w.flags {
		errs = append(errs, fmt.Errorf("nlmsg flags %#x, want %#x", got, w.flags))
	}
	if len(body) < BrPortMsgSizeCst {
		return errors.Join(append(errs, ErrBrPortMsgSmall)...)
	}
	var top, nest []uint16
	err := WalkRTAttrs(body[BrPortMsgSizeCst:], func(atype uint16, val []byte) {
		top = append(top, atype)
		if atype == MdbaSetEntryAttrs {
			errs = append(errs, WalkRTAttrsNested(val, func(atype uint16, _ []byte) { nest = append(nest, atype) }))
		}
	})
	wantTop := []uint16{MdbaSetEntry}
	if w.nest != nil {
		wantTop = append(wantTop, MdbaSetEntryAttrs)
	}
	if !slices.Equal(top, wantTop) || !slices.Equal(nest, w.nest) {
		errs = append(errs, fmt.Errorf("attributes %v %v, want %v %v", top, nest, wantTop, w.nest))
	}
	got, perr := ParseMdbRequest(body)
	want := w.req
	want.Ifindex, want.Entry.Ifindex = dev, bport
	if want.Attrs.HasIfindex {
		want.Attrs.Ifindex = via
	}
	if perr == nil && !reflect.DeepEqual(got, want) {
		errs = append(errs, fmt.Errorf("mdb request %+v\n  want %+v", got, want))
	}
	return errors.Join(append(errs, err, perr)...)
}

// mdbWantVids is the VLANs O4 expects the entry in: the one given, or,
// for a bridge entry with none, each VLAN br0 and d0 carry (1 and 10),
// as br_mdb_add installs it on all of them. A VXLAN entry has none.
func mdbWantVids(w mdbWant) []uint16 {
	switch {
	case w.dev != "br0":
		return []uint16{0}
	case w.req.Entry.Vid == 0:
		return []uint16{1, 10}
	}
	return []uint16{w.req.Entry.Vid}
}

// mdbDumpHas is whether a dump entry is w's in VLAN vid. bport is the
// entry port's index and via MDBE_ATTR_IFINDEX's. A bridge dumps its
// filter mode and source list only under IGMPv3 or MLDv2, which br0 does
// not run, and a host group's state is the kernel's, so neither is
// checked; a VXLAN leaves out a port that is its own (4789) or 0, a VNI
// that is its own (100), and a source VNI without external.
func mdbDumpHas(w mdbWant, d MdbDumpEntry, vid uint16, bport, via uint32) bool {
	in, a := w.req.Entry, w.req.Attrs
	if d.Entry.Ifindex != bport || d.Entry.Vid != vid || d.Entry.Proto != in.Proto || !bytes.Equal(d.Entry.Group, in.Group) ||
		!bytes.Equal(d.Attrs.Source, a.Source) {
		return false
	}
	if w.dev == "br0" {
		return (w.bport == "br0" || d.Entry.State == in.State) && (!a.HasRtProt || d.Attrs.RtProt == a.RtProt)
	}
	mode := a.GroupMode
	if a.Source != nil {
		mode = 1 // an (S, G) entry is MCAST_INCLUDE
	}
	switch {
	case !bytes.Equal(d.Attrs.Dst, a.Dst), d.Attrs.GroupMode != mode,
		a.HasRtProt && d.Attrs.RtProt != a.RtProt,
		!slices.EqualFunc(d.Attrs.SrcList, a.SrcList, bytes.Equal),
		a.HasDstPort && a.DstPort != 0 && a.DstPort != 4789 && d.Attrs.DstPort != a.DstPort,
		a.HasVni && a.Vni != 100 && (!d.Attrs.HasVni || d.Attrs.Vni != a.Vni),
		a.HasIfindex && d.Attrs.Ifindex != via:
		return false
	}
	return true
}

// mdbRoundTripProblem is O4's verdict: after an accepted add or replace
// the dump holds the entry in each of mdbWantVids, and after an accepted
// delete in none.
func mdbRoundTripProblem(w mdbWant, dump []MdbDumpEntry, bport, via uint32) error {
	var errs []error
	for _, vid := range mdbWantVids(w) {
		has := slices.ContainsFunc(dump, func(d MdbDumpEntry) bool { return mdbDumpHas(w, d, vid, bport, via) })
		switch {
		case w.typ == unix.RTM_DELMDB && has:
			errs = append(errs, fmt.Errorf("after del the dump still holds the entry in VLAN %d", vid))
		case w.typ == unix.RTM_NEWMDB && !has:
			errs = append(errs, fmt.Errorf("the dump lacks the entry in VLAN %d", vid))
		}
	}
	return errors.Join(errs...)
}

// TestWsMdbCorpus runs O3 and O4 over every generated mdb row, and fails
// if O3 checked nothing.
func TestWsMdbCorpus(t *testing.T) {
	var checked, roundTrips int
	for _, dir := range wsRows(t) {
		row := wsLoadRow(t, dir)
		if !strings.HasPrefix(row.ID, "mdbbr/") && !strings.HasPrefix(row.ID, "mdbvx/") {
			continue
		}
		t.Run(row.Env.Kernel+"/"+row.ID, func(t *testing.T) {
			if row.Result.Status != "OK" {
				return // TestWsCorpusRows fails it
			}
			c, rt := wsMdbRow(t, dir, row)
			checked += c
			roundTrips += rt
		})
	}
	t.Logf("O3 checked %d mdb requests, O4 %d round trips", checked, roundTrips)
	if checked == 0 || roundTrips == 0 {
		t.Error("O3 or O4 checked nothing: are there mdb rows under testdata/*/ws?")
	}
}

// wsMdbSent is every mdb write in msgs.
func wsMdbSent(msgs []capturedNlMsg) []capturedNlMsg {
	return append(wsRouteRequests(msgs, unix.RTM_NEWMDB), wsRouteRequests(msgs, unix.RTM_DELMDB)...)
}

// wsMdbDump is every dump reply's entries. The bridge answers a dump with
// RTM_GETMDB (net/bridge/br_mdb.c:391), the VXLAN with RTM_NEWMDB
// (drivers/net/vxlan/vxlan_mdb.c:292).
func wsMdbDump(t *testing.T, msgs []capturedNlMsg) []MdbDumpEntry {
	t.Helper()
	var out []MdbDumpEntry
	for _, m := range msgs {
		if m.proto != unix.NETLINK_ROUTE || m.h.Flags&unix.NLM_F_REQUEST != 0 ||
			(m.h.Type != unix.RTM_NEWMDB && m.h.Type != unix.RTM_GETMDB) {
			continue
		}
		d, err := ParseMdbDump(m.body)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, d...)
	}
	return out
}

// wsMdbIndexes is dev's, bport's and via's indexes from the links bridge
// read; via is 0 when the row names none.
func wsMdbIndexes(t *testing.T, msgs []capturedNlMsg, w mdbWant, cmd string) (dev, bport, via uint32) {
	t.Helper()
	idx := func(name string) uint32 {
		i := wsLinkIndexOf(msgs, name)
		if i <= 0 {
			t.Fatalf("O3: bridge read no RTM_NEWLINK for %s\n  cmd: %s", name, cmd)
		}
		return uint32(i)
	}
	dev, bport = idx(w.dev), idx(w.bport)
	if w.via != "" {
		via = idx(w.via)
	}
	return dev, bport, via
}

func wsMdbRow(t *testing.T, dir string, row wsRow) (checked, roundTrips int) {
	t.Helper()
	msgs := pcapNlMsgs(t, filepath.Join(dir, "ip.pcap"))
	sent := wsMdbSent(msgs)
	if err := fdbAckFailProblem(row.Expect.RefusedBy, len(sent)); err != nil {
		t.Fatalf("O3: %v\n  cmd: %s", err, row.Cmd)
	}
	w, err := mdbWantFromAxes(row.Axes)
	if err != nil {
		// A value bridge refuses to parse has no facts to model.
		if row.Expect.RefusedBy != wsRefusedNone && len(sent) == 0 {
			return 0, 0
		}
		t.Fatalf("O3 model: %v", err)
	}
	rc := wsReadRC(t, dir)
	if len(sent) == 0 {
		if rc == 0 {
			t.Errorf("O3: rc 0 but no mdb request\n  cmd: %s", row.Cmd)
		}
		return 0, 0
	}
	if len(sent) != 1 {
		t.Fatalf("O3: %d mdb requests, want 1\n  cmd: %s", len(sent), row.Cmd)
	}
	dev, bport, via := wsMdbIndexes(t, msgs, w, row.Cmd)
	if err := mdbRequestProblem(w, sent[0].h.Type, sent[0].h.Flags, sent[0].body, dev, bport, via); err != nil {
		t.Errorf("O3: %v\n  cmd: %s", err, row.Cmd)
	}
	if rc != 0 {
		return 1, 0
	}
	dump := wsMdbDump(t, pcapNlMsgs(t, filepath.Join(dir, "netlink_route_dump.pcap")))
	if err := mdbRoundTripProblem(w, dump, bport, via); err != nil {
		t.Errorf("O4: %v: %+v\n  cmd: %s", err, dump, row.Cmd)
	}
	return 1, 1
}

func TestMdbWantFromAxes(t *testing.T) {
	g4 := []byte{239, 1, 1, 1}
	s4 := []byte{192, 0, 2, 1}
	tests := []struct {
		description string
		axes        map[string]string
		expected    mdbWant
		err         bool
	}{
		{
			description: "positive: a bare add has the entry and no nest",
			axes:        map[string]string{"verb": "add", "dev": "br0", "bport": "d0", "grp": "239.1.1.1"},
			expected: mdbWant{typ: unix.RTM_NEWMDB, flags: unix.NLM_F_CREATE | unix.NLM_F_EXCL, dev: "br0", bport: "d0",
				req: MdbRequest{Family: unix.AF_BRIDGE, HasEntry: true, Entry: MdbEntry{Proto: MdbProtoIP, Group: g4}}},
		},
		{
			description: "positive: the nest follows mdb_modify's order, not the axes'",
			axes: map[string]string{"verb": "replace", "dev": "vx0", "bport": "vx0", "grp": "239.1.1.1", "state": "1",
				"via": "d1", "dst": "192.0.2.1", "src": "192.0.2.1", "dport": "4790"},
			expected: mdbWant{typ: unix.RTM_NEWMDB, flags: unix.NLM_F_CREATE | unix.NLM_F_REPLACE, dev: "vx0", bport: "vx0", via: "d1",
				req: MdbRequest{Family: unix.AF_BRIDGE, HasEntry: true, HasAttrs: true,
					Entry: MdbEntry{State: 1, Proto: MdbProtoIP, Group: g4},
					Attrs: MdbAttrs{Source: s4, Dst: s4, DstPort: 4790, HasDstPort: true, HasIfindex: true}},
				nest: []uint16{MdbeAttrSource, MdbeAttrDst, MdbeAttrDstPort, MdbeAttrIfindex}},
		},
		{
			description: "negative: an unknown verb",
			axes:        map[string]string{"verb": "flush", "dev": "br0", "bport": "d0", "grp": "239.1.1.1"},
			err:         true,
		},
		{
			description: "negative: a group that is neither an address nor a MAC",
			axes:        map[string]string{"verb": "add", "dev": "br0", "bport": "d0", "grp": "nosuch"},
			err:         true,
		},
		{
			description: "boundary: vid 65535 fits the entry's u16",
			axes:        map[string]string{"verb": "add", "dev": "br0", "bport": "d0", "grp": "01:00:5e:01:01:01", "vid": "65535"},
			expected: mdbWant{typ: unix.RTM_NEWMDB, flags: unix.NLM_F_CREATE | unix.NLM_F_EXCL, dev: "br0", bport: "d0",
				req: MdbRequest{Family: unix.AF_BRIDGE, HasEntry: true,
					Entry: MdbEntry{Vid: 65535, Group: []byte{1, 0, 0x5e, 1, 1, 1}}}},
		},
		{
			description: "boundary: vid 65536 does not",
			axes:        map[string]string{"verb": "add", "dev": "br0", "bport": "d0", "grp": "239.1.1.1", "vid": "65536"},
			err:         true,
		},
		{
			description: "corner: a del drops permanent, as mdb_modify sends it only on NEWMDB",
			axes:        map[string]string{"verb": "del", "dev": "br0", "bport": "d0", "grp": "239.1.1.1", "state": "1"},
			expected: mdbWant{typ: unix.RTM_DELMDB, dev: "br0", bport: "d0",
				req: MdbRequest{Family: unix.AF_BRIDGE, HasEntry: true, Entry: MdbEntry{Proto: MdbProtoIP, Group: g4}}},
		},
		{
			description: "corner: a source list keeps its order",
			axes: map[string]string{"verb": "add", "dev": "br0", "bport": "d0", "grp": "239.1.1.1",
				"mode": "0", "srclist": "192.0.2.2,192.0.2.1"},
			expected: mdbWant{typ: unix.RTM_NEWMDB, flags: unix.NLM_F_CREATE | unix.NLM_F_EXCL, dev: "br0", bport: "d0",
				req: MdbRequest{Family: unix.AF_BRIDGE, HasEntry: true, HasAttrs: true,
					Entry: MdbEntry{Proto: MdbProtoIP, Group: g4},
					Attrs: MdbAttrs{HasGroupMode: true, HasSrcList: true, SrcList: [][]byte{{192, 0, 2, 2}, s4}}},
				nest: []uint16{MdbeAttrGroupMode, MdbeAttrSrcList}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := mdbWantFromAxes(tc.axes)
			if (err != nil) != tc.err {
				t.Fatalf("err %v, want error %v", err, tc.err)
			}
			if !tc.err && !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("got %+v\nwant %+v", got, tc.expected)
			}
		})
	}
}

func TestMdbRequestProblem(t *testing.T) {
	g4 := []byte{239, 1, 1, 1}
	w := mdbWant{typ: unix.RTM_NEWMDB, flags: unix.NLM_F_CREATE | unix.NLM_F_EXCL, dev: "br0", bport: "d0",
		req: MdbRequest{Family: unix.AF_BRIDGE, HasEntry: true, HasAttrs: true,
			Entry: MdbEntry{Proto: MdbProtoIP, Group: g4, Vid: 10},
			Attrs: MdbAttrs{RtProt: 186, HasRtProt: true, HasIfindex: true}},
		nest: []uint16{MdbeAttrRtProt, MdbeAttrIfindex}}
	entry := rtattr(MdbaSetEntry, mdbEntryBytes(4, 0, 0, 10, MdbProtoIP, g4))
	nest := func(attrs ...[]byte) []byte { return rtattr(MdbaSetEntryAttrs|unix.NLA_F_NESTED, concat(attrs...)) }
	good := concat(brPortMsg(3), entry, nest(rtattr(MdbeAttrRtProt, []byte{186}), rtattr(MdbeAttrIfindex, le32(7))))
	add := unix.NLM_F_REQUEST | unix.NLM_F_ACK | unix.NLM_F_CREATE | unix.NLM_F_EXCL
	tests := []struct {
		description string
		typ, flags  uint16
		body        []byte
		expected    string // a substring of the problem; "" for none
	}{
		{"positive: the request the axes call for", unix.RTM_NEWMDB, uint16(add), good, ""},
		{"negative: a delete where an add was due", unix.RTM_DELMDB, uint16(add), good, "type 85"},
		{"negative: replace's flags", unix.RTM_NEWMDB, unix.NLM_F_REQUEST | unix.NLM_F_CREATE | unix.NLM_F_REPLACE, good, "nlmsg flags"},
		{"negative: the nest's attributes swapped", unix.RTM_NEWMDB, uint16(add),
			concat(brPortMsg(3), entry, nest(rtattr(MdbeAttrIfindex, le32(7)), rtattr(MdbeAttrRtProt, []byte{186}))), "attributes"},
		{"negative: another port's index", unix.RTM_NEWMDB, uint16(add),
			concat(brPortMsg(3), rtattr(MdbaSetEntry, mdbEntryBytes(5, 0, 0, 10, MdbProtoIP, g4)),
				nest(rtattr(MdbeAttrRtProt, []byte{186}), rtattr(MdbeAttrIfindex, le32(7)))), "mdb request"},
		{"boundary: a body of br_port_msg less one byte", unix.RTM_NEWMDB, uint16(add), brPortMsg(3)[:7], "br_port_msg"},
		{"corner: the nest left out", unix.RTM_NEWMDB, uint16(add), concat(brPortMsg(3), entry), "attributes"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			err := mdbRequestProblem(w, tc.typ, tc.flags, tc.body, 3, 4, 7)
			switch {
			case tc.expected == "" && err != nil:
				t.Errorf("unexpected problem: %v", err)
			case tc.expected != "" && (err == nil || !strings.Contains(err.Error(), tc.expected)):
				t.Errorf("problem %v, want one naming %q", err, tc.expected)
			}
		})
	}
}

func TestMdbRoundTripProblem(t *testing.T) {
	g4 := []byte{239, 1, 1, 1}
	d1 := []byte{192, 0, 2, 1}
	br := mdbWant{typ: unix.RTM_NEWMDB, dev: "br0", bport: "d0",
		req: MdbRequest{Entry: MdbEntry{State: 1, Proto: MdbProtoIP, Group: g4}}}
	brDel := br
	brDel.typ = unix.RTM_DELMDB
	brVid := br
	brVid.req.Entry.Vid = 10
	vx := mdbWant{typ: unix.RTM_NEWMDB, dev: "vx0", bport: "vx0",
		req: MdbRequest{Entry: MdbEntry{State: 1, Proto: MdbProtoIP, Group: g4},
			Attrs: MdbAttrs{Dst: d1, DstPort: 4789, HasDstPort: true, Vni: 100, HasVni: true}}}
	vxVni := vx
	vxVni.req.Attrs.Vni = 200
	vxList := vx
	vxList.req.Attrs.SrcList, vxList.req.Attrs.HasSrcList = [][]byte{{192, 0, 2, 5}}, true
	vxSG := vx
	vxSG.req.Attrs.Source = d1
	at := func(vid uint16, state uint8) MdbDumpEntry {
		return MdbDumpEntry{Bridge: 3, Entry: MdbEntry{Ifindex: 4, State: state, Vid: vid, Proto: MdbProtoIP, Group: g4}}
	}
	vxEntry := MdbDumpEntry{Bridge: 9, Entry: MdbEntry{Ifindex: 9, State: 1, Proto: MdbProtoIP, Group: g4}, Attrs: MdbAttrs{Dst: d1}}
	vxSGEntry := vxEntry
	vxSGEntry.Attrs.Source, vxSGEntry.Attrs.GroupMode = d1, 1
	tests := []struct {
		description string
		w           mdbWant
		dump        []MdbDumpEntry
		bport       uint32
		expected    string // a substring of the problem; "" for none
	}{
		{"positive: an add with no VLAN lands in VLANs 1 and 10", br, []MdbDumpEntry{at(1, 1), at(10, 1)}, 4, ""},
		{"positive: a VXLAN dump leaves out its own port and VNI", vx, []MdbDumpEntry{vxEntry}, 9, ""},
		{"negative: VLAN 10's copy is missing", br, []MdbDumpEntry{at(1, 1)}, 4, "lacks the entry in VLAN 10"},
		{"negative: a temp entry where permanent was asked", brVid, []MdbDumpEntry{at(10, 0)}, 4, "VLAN 10"},
		{"negative: a deleted entry still dumped", brDel, []MdbDumpEntry{at(10, 1)}, 4, "still holds"},
		{"negative: the VXLAN dumps no VNI though 200 was sent", vxVni, []MdbDumpEntry{vxEntry}, 9, "lacks"},
		{"negative: the VXLAN dumps no source list though one was sent", vxList, []MdbDumpEntry{vxEntry}, 9, "lacks"},
		{"corner: a VXLAN (S, G) entry dumps as INCLUDE, though no mode was sent", vxSG, []MdbDumpEntry{vxSGEntry}, 9, ""},
		{"boundary: an empty dump after a delete", brDel, nil, 4, ""},
		{"corner: the same entry on another port", brVid, []MdbDumpEntry{at(10, 1)}, 5, "lacks"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			err := mdbRoundTripProblem(tc.w, tc.dump, tc.bport, 0)
			switch {
			case tc.expected == "" && err != nil:
				t.Errorf("unexpected problem: %v", err)
			case tc.expected != "" && (err == nil || !strings.Contains(err.Error(), tc.expected)):
				t.Errorf("problem %v, want one naming %q", err, tc.expected)
			}
		})
	}
}

func TestWsMdbDump(t *testing.T) {
	g4 := []byte{239, 1, 1, 1}
	body := concat(brPortMsg(3), rtattr(MdbaMdb, rtattr(MdbaMdbEntry,
		rtattr(MdbaMdbEntryInfo, mdbEntryBytes(4, 0, 0, 1, MdbProtoIP, g4)))))
	one := []MdbDumpEntry{{Bridge: 3, Entry: MdbEntry{Ifindex: 4, Vid: 1, Proto: MdbProtoIP, Group: g4}}}
	msg := func(typ, flags uint16) capturedNlMsg {
		return capturedNlMsg{proto: unix.NETLINK_ROUTE, h: NlMsgHdr{Type: typ, Flags: flags}, body: body}
	}
	tests := []struct {
		description string
		msgs        []capturedNlMsg
		expected    []MdbDumpEntry
	}{
		{"positive: a bridge's reply is RTM_GETMDB", []capturedNlMsg{msg(unix.RTM_GETMDB, unix.NLM_F_MULTI)}, one},
		{"positive: a VXLAN's reply is RTM_NEWMDB", []capturedNlMsg{msg(unix.RTM_NEWMDB, unix.NLM_F_MULTI)}, one},
		{"negative: the dump request is no reply", []capturedNlMsg{msg(unix.RTM_GETMDB, unix.NLM_F_REQUEST|unix.NLM_F_DUMP)}, nil},
		{"boundary: no messages", nil, nil},
		{"corner: an RTM_DELMDB notification is no dump", []capturedNlMsg{msg(unix.RTM_DELMDB, 0)}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := wsMdbDump(t, tc.msgs); !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("got %+v\nwant %+v", got, tc.expected)
			}
		})
	}
}
