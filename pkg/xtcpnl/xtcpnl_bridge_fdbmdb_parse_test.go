package xtcpnl

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// mdbInfo is an MDBA_MDB_ENTRY_INFO: a br_mdb_entry and its attributes.
func mdbInfo(e []byte, attrs ...[]byte) []byte {
	return rtattr(MdbaMdbEntryInfo, concat(append([][]byte{e}, attrs...)...))
}

// mdbReply is a dump or get reply body: br_port_msg, then nests.
func mdbReply(bridge uint32, nests ...[]byte) []byte {
	return concat(append([][]byte{brPortMsg(bridge)}, nests...)...)
}

// mdbNest is MDBA_MDB holding one MDBA_MDB_ENTRY of the given infos.
func mdbNest(infos ...[]byte) []byte {
	return rtattr(MdbaMdb|unix.NLA_F_NESTED, rtattr(MdbaMdbEntry|unix.NLA_F_NESTED, concat(infos...)))
}

// mdbSummary is a dump's entries and router ports in one line.
func mdbSummary(d MdbDump) string {
	var b strings.Builder
	fmt.Fprintf(&b, "br %d:", d.Bridge)
	for i := range d.Entries {
		e := &d.Entries[i]
		fmt.Fprintf(&b, " e(%d vid %d grp %v src %v t %v)", e.Entry.Ifindex, e.Entry.Vid, e.Entry.Group, e.Attrs.SrcList, e.Attrs.SrcTimers)
	}
	fmt.Fprintf(&b, " router %t", d.HasRouter)
	for _, p := range d.Routers {
		fmt.Fprintf(&b, " r(%d)", p.Ifindex)
	}
	return b.String()
}

// TestParseMdbDumpRouters: a dump reply's entries and router ports (7P).
//
// go test ./pkg/xtcpnl/ -run TestParseMdbDumpRouters
func TestParseMdbDumpRouters(t *testing.T) {
	g := []byte{239, 1, 1, 1}
	e5 := mdbEntryBytes(5, 1, 0, 10, MdbProtoIP, g)
	srcs := rtattr(MdbEattrSrcList|unix.NLA_F_NESTED, concat(
		rtattr(MdbSrcListEntry, concat(rtattr(MdbSrcAttrAddress, []byte{10, 0, 0, 1}), rtattr(MdbSrcAttrTimer, le32(0)))),
		rtattr(MdbSrcListEntry, concat(rtattr(MdbSrcAttrAddress, []byte{10, 0, 0, 2}), rtattr(MdbSrcAttrTimer, le32(26000)))),
	))
	tests := []struct {
		description string
		body        []byte
		expected    string // summary, or the error
	}{
		{"positive: entries then router ports", mdbReply(4, mdbNest(mdbInfo(e5)),
			rtattr(MdbaRouter|unix.NLA_F_NESTED, concat(rport(5, rta(MdbaRouterPattrType, 2)), rport(6)))),
			"br 4: e(5 vid 10 grp [239 1 1 1] src [] t []) router true r(5) r(6)"},
		{"positive: a source list keeps each source's timer", mdbReply(4, mdbNest(mdbInfo(e5, srcs))),
			"br 4: e(5 vid 10 grp [239 1 1 1] src [[10 0 0 1] [10 0 0 2]] t [0 26000]) router false"},
		{"positive: router ports only", mdbReply(4, rtattr(MdbaRouter, rport(7))), "br 4: router true r(7)"},
		{"negative: a router port shorter than its ifindex", mdbReply(4, rtattr(MdbaRouter, rtattr(MdbaRouterPort, []byte{1, 2}))),
			ErrMdbRouterPortSmall.Error()},
		{"negative: an entry info one byte short of br_mdb_entry", mdbReply(4, mdbNest(rtattr(MdbaMdbEntryInfo, e5[:27]))),
			ErrMdbEntrySize.Error()},
		{"negative: a header shorter than br_port_msg", []byte{unix.AF_BRIDGE, 0, 0}, ErrBrPortMsgSmall.Error()},
		{"boundary: an empty MDBA_ROUTER is present, with no ports", mdbReply(4, rtattr(MdbaRouter, nil)), "br 4: router true"},
		{"boundary: an empty MDBA_MDB has no entries", mdbReply(4, rtattr(MdbaMdb, nil)), "br 4: router false"},
		{"boundary: a bare br_port_msg", mdbReply(9), "br 9: router false"},
		{"corner: two MDBA_ROUTER nests append in order", mdbReply(4, rtattr(MdbaRouter, rport(2)), rtattr(MdbaRouter, rport(3))),
			"br 4: router true r(2) r(3)"},
		{"corner: a source entry with no timer reads 0", mdbReply(4, mdbNest(mdbInfo(e5, rtattr(MdbEattrSrcList,
			rtattr(MdbSrcListEntry, rtattr(MdbSrcAttrAddress, []byte{10, 0, 0, 9})))))),
			"br 4: e(5 vid 10 grp [239 1 1 1] src [[10 0 0 9]] t [0]) router false"},
		{"corner: a second address in one source entry is ignored (parse_rtattr's first wins)", mdbReply(4, mdbNest(mdbInfo(e5,
			rtattr(MdbEattrSrcList, rtattr(MdbSrcListEntry, concat(rtattr(MdbSrcAttrAddress, []byte{10, 0, 0, 1}),
				rtattr(MdbSrcAttrAddress, []byte{10, 0, 0, 2}), rtattr(MdbSrcAttrTimer, le32(5)))))))),
			"br 4: e(5 vid 10 grp [239 1 1 1] src [[10 0 0 1]] t [5]) router false"},
		{"corner: an unknown top-level type is skipped", mdbReply(4, rtattr(3, le32(1)), rtattr(MdbaRouter, rport(2))),
			"br 4: router true r(2)"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			d, err := ParseMdbDumpRouters(tc.body)
			got := mdbSummary(d)
			if err != nil {
				got = err.Error()
			}
			if got != tc.expected {
				t.Errorf("got %q\nwant %q", got, tc.expected)
			}
		})
	}
}

// TestParseMdbGetReply: an mdb get answer is exactly one entry (7P).
//
// go test ./pkg/xtcpnl/ -run TestParseMdbGetReply
func TestParseMdbGetReply(t *testing.T) {
	g4 := []byte{232, 1, 1, 1}
	e := mdbEntryBytes(5, 1, 0, 0, MdbProtoIP, g4)
	l2 := mdbEntryBytes(5, 1, 0, 0, 0, []byte{1, 2, 3, 4, 5, 6})
	tests := []struct {
		description string
		body        []byte
		expected    string // ifindex grp source proto, or the error
	}{
		{"positive: an S,G entry", mdbReply(4, mdbNest(mdbInfo(e, rtattr(MdbEattrSource, []byte{10, 0, 0, 1})))),
			"5 [232 1 1 1] [10 0 0 1] 0x0800"},
		{"negative: no entry", mdbReply(4, rtattr(MdbaMdb, nil)), ErrMdbGetReply.Error()},
		{"negative: two entries", mdbReply(4, mdbNest(mdbInfo(e), mdbInfo(e))), ErrMdbGetReply.Error()},
		{"negative: router ports beside the entry", mdbReply(4, mdbNest(mdbInfo(e)), rtattr(MdbaRouter, nil)), ErrMdbGetReply.Error()},
		{"negative: a 27-byte entry info", mdbReply(4, mdbNest(rtattr(MdbaMdbEntryInfo, e[:27]))), ErrMdbEntrySize.Error()},
		{"negative: an attribute longer than the body", append(mdbReply(4), 8, 0, 1, 0), ErrRTAttrSmall.Error()},
		{"boundary: a 3-byte tail is no attribute: nothing found", append(mdbReply(4), 8, 0, 1), ErrMdbGetReply.Error()},
		{"boundary: an L2 group, proto 0", mdbReply(4, mdbNest(mdbInfo(l2))), "5 [1 2 3 4 5 6] [] 0x0000"},
		{"corner: two MDBA_MDB nests of one entry each are two entries", mdbReply(4, mdbNest(mdbInfo(e)), mdbNest(mdbInfo(e))),
			ErrMdbGetReply.Error()},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			d, err := ParseMdbGetReply(tc.body)
			got := fmt.Sprintf("%d %v %v 0x%04x", d.Entry.Ifindex, d.Entry.Group, d.Attrs.Source, d.Entry.Proto)
			if err != nil {
				got = err.Error()
			}
			if got != tc.expected {
				t.Errorf("got %q\nwant %q", got, tc.expected)
			}
		})
	}
}

// fdbMdbReadbackDirs is every 7_3_0 fdb*/mdb* row with a readback capture.
func fdbMdbReadbackDirs(t testing.TB) []string {
	t.Helper()
	bases, err := filepath.Glob("testdata/7_3_0/ws/[fm]db*")
	if err != nil {
		t.Fatal(err)
	}
	var dirs []string
	for _, base := range bases {
		ents, err := os.ReadDir(base)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range ents {
			d := filepath.Join(base, e.Name())
			if _, err := os.Stat(filepath.Join(d, "netlink_route_dump.pcap")); err == nil {
				dirs = append(dirs, d)
			}
		}
	}
	return dirs
}

// TestFdbMdbReadbackReplies decodes every reply in the 7_3_0 fdb and mdb
// readbacks: each AF_BRIDGE RTM_NEWNEIGH with ParseNeigh (bridge, lladdr
// and master or a vxlan's dst), and each mdb dump reply with
// ParseMdbDumpRouters, whose entries are ParseMdbDump's. The reply types
// are K5's: the bridge's are RTM_GETMDB, a VXLAN's RTM_NEWMDB.
//
// go test ./pkg/xtcpnl/ -run TestFdbMdbReadbackReplies
func TestFdbMdbReadbackReplies(t *testing.T) {
	dirs := fdbMdbReadbackDirs(t)
	if len(dirs) != 376 {
		t.Errorf("%d readbacks, want 376", len(dirs))
	}
	counts := map[string]int{}
	for _, dir := range dirs {
		kind := filepath.Base(filepath.Dir(dir))
		for _, m := range pcapNlMsgs(t, filepath.Join(dir, "netlink_route_dump.pcap")) {
			if m.h.Flags&unix.NLM_F_REQUEST != 0 {
				continue
			}
			switch m.h.Type {
			case unix.RTM_NEWNEIGH:
				ni, err := ParseNeigh(m.body)
				if err != nil {
					t.Fatalf("%s: %v", dir, err)
				}
				if ni.Family != unix.AF_BRIDGE || len(ni.LLAddr) != 6 {
					t.Fatalf("%s: family %d lladdr %v", dir, ni.Family, ni.LLAddr)
				}
				counts[kind+" neigh"]++
				if ni.HasLinkNetnsid {
					counts[kind+" netnsid"]++
				}
			case unix.RTM_GETMDB, unix.RTM_NEWMDB:
				d, err := ParseMdbDumpRouters(m.body)
				if err != nil {
					t.Fatalf("%s: %v", dir, err)
				}
				es, err := ParseMdbDump(m.body)
				if err != nil || len(es) != len(d.Entries) {
					t.Fatalf("%s: ParseMdbDump %d entries (%v), ParseMdbDumpRouters %d", dir, len(es), err, len(d.Entries))
				}
				counts[fmt.Sprintf("%s mdb type %d", kind, m.h.Type)]++
				counts[kind+" entries"] += len(d.Entries)
				for i := range d.Entries {
					if e := &d.Entries[i]; len(e.Attrs.SrcTimers) != len(e.Attrs.SrcList) {
						t.Fatalf("%s: %d sources, %d timers", dir, len(e.Attrs.SrcList), len(e.Attrs.SrcTimers))
					}
				}
			}
		}
	}
	for _, k := range []string{"fdbbr neigh", "fdbvx neigh", "mdbbr entries", "mdbvx entries"} {
		if counts[k] == 0 {
			t.Errorf("%s: none decoded", k)
		}
	}
	if counts[fmt.Sprintf("mdbbr mdb type %d", unix.RTM_NEWMDB)] != 0 || counts[fmt.Sprintf("mdbvx mdb type %d", unix.RTM_GETMDB)] != 0 {
		t.Errorf("K5: reply types %v", counts)
	}
	t.Logf("%v", counts)
}

// FuzzParseMdbDumpRouters: no panic, and an error leaves no partial dump.
//
// go test ./pkg/xtcpnl/ -run '^$' -fuzz FuzzParseMdbDumpRouters -fuzztime 30s
func FuzzParseMdbDumpRouters(f *testing.F) {
	for _, dir := range fdbMdbReadbackDirs(f)[:8] {
		for _, m := range pcapNlMsgs(f, filepath.Join(dir, "netlink_route_dump.pcap")) {
			if m.h.Type == unix.RTM_GETMDB || m.h.Type == unix.RTM_NEWMDB {
				f.Add(m.body)
			}
		}
	}
	f.Add(mdbReply(4, mdbNest(mdbInfo(mdbEntryBytes(5, 1, 0, 10, MdbProtoIP, []byte{239, 1, 1, 1}))), rtattr(MdbaRouter, rport(5))))
	f.Fuzz(func(t *testing.T, b []byte) {
		d, err := ParseMdbDumpRouters(b)
		if err != nil && (d.Entries != nil || d.Routers != nil) {
			t.Fatal("a partial dump with an error")
		}
		g, gerr := ParseMdbGetReply(b)
		if gerr == nil && (err != nil || len(d.Entries) != 1 || d.HasRouter) {
			t.Fatalf("get reply %+v from a dump of %d entries (%v)", g, len(d.Entries), err)
		}
		if gerr != nil && !errors.Is(gerr, ErrMdbGetReply) && err == nil {
			t.Fatalf("get error %v from a good dump", gerr)
		}
	})
}

// BenchmarkParseMdbDumpRouters decodes a two-port, one-entry reply.
//
// go test ./pkg/xtcpnl/ -run '^$' -bench BenchmarkParseMdbDumpRouters -benchmem
func BenchmarkParseMdbDumpRouters(b *testing.B) {
	body := mdbReply(4, mdbNest(mdbInfo(mdbEntryBytes(5, 1, 0, 10, MdbProtoIP, []byte{239, 1, 1, 1}))),
		rtattr(MdbaRouter, concat(rport(5, rta(MdbaRouterPattrType, 2)), rport(6))))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ParseMdbDumpRouters(body); err != nil {
			b.Fatal(err)
		}
	}
}
