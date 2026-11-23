package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// The 7C corpus, ws/bridgefdb and ws/bridgemdb (gobridge increment 7):
// O3 rebuilds every fdb and mdb request bridge sent with the 7S builders,
// and O4 checks each -j row's objects against the decoded replies.

// fdbMdbWsDirs is every row directory of the two spaces, by path.
func fdbMdbWsDirs(t testing.TB, space string) []string {
	t.Helper()
	dirs, err := filepath.Glob(filepath.Join("testdata", "7_3_1", "ws", space, "*"))
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(dirs)
	return dirs
}

// fdbMdbRebuild decodes one captured fdb or mdb request and builds it
// again: the kind it is, and the built bytes. ok is false for a request
// of another type (the link dump, ll_link_get).
func fdbMdbRebuild(m capturedNlMsg) (kind string, built []byte, ok bool, err error) {
	dump := m.h.Flags&unix.NLM_F_DUMP == unix.NLM_F_DUMP
	switch {
	case m.h.Type == unix.RTM_GETNEIGH && dump:
		if len(m.body) < NdMsgSizeCst {
			return "", nil, true, ErrRTAttrSmall
		}
		ni, err := ParseNeigh(m.body)
		if err != nil {
			return "", nil, true, err
		}
		b, err := BuildFdbDumpRequest(binary.LittleEndian.Uint32(m.body[4:8]), ni.Master, 0)
		return "fdb dump", b, true, err
	case m.h.Type == unix.RTM_GETNEIGH:
		ni, err := ParseNeigh(m.body)
		if err != nil {
			return "", nil, true, err
		}
		g := FdbGet{Ifindex: binary.LittleEndian.Uint32(m.body[4:8]), Flags: ni.Flags, Lladdr: ni.LLAddr,
			Vlan: ni.Vlan, HasVlan: ni.HasVlan, Vni: ni.Vni, HasVni: ni.HasVni, Master: ni.Master}
		b, err := BuildFdbGetRequest(g, 0)
		return "fdb get", b, true, err
	case m.h.Type == unix.RTM_GETMDB && dump:
		b, err := BuildMdbDumpRequest(0)
		return "mdb dump", b, true, err
	case m.h.Type == unix.RTM_GETMDB:
		g, err := fdbMdbParseGet(m.body)
		if err != nil {
			return "", nil, true, err
		}
		b, err := BuildMdbGetRequest(g, 0)
		return "mdb get", b, true, err
	}
	return "", nil, false, nil
}

// fdbMdbParseGet decodes mdb_get's request into the builder's input.
func fdbMdbParseGet(body []byte) (MdbGet, error) {
	if len(body) < BrPortMsgSizeCst {
		return MdbGet{}, ErrBrPortMsgSmall
	}
	g := MdbGet{Ifindex: binary.LittleEndian.Uint32(body[4:8])}
	var perr error
	err := WalkRTAttrs(body[BrPortMsgSizeCst:], func(atype uint16, val []byte) {
		switch atype {
		case MdbaGetEntry:
			e, err := ParseMdbEntry(val)
			if err != nil {
				perr = errors.Join(perr, err)
				return
			}
			g.Vid, g.Group = e.Vid, e.Group
		case MdbaGetEntryAttrs:
			var a MdbAttrs
			perr = errors.Join(perr, parseMdbAttrs(val, mdbeIDs, &a))
			g.Source, g.SrcVni, g.HasSrcVni = a.Source, a.SrcVni, a.HasSrcVni
		}
	})
	return g, errors.Join(err, perr)
}

// TestWsBridgeFdbMdbRequests (O3) rebuilds every fdb and mdb request in
// the two spaces' captures byte for byte, seq and pid aside: the strict
// fdb dump with its dev index and NDA_MASTER, fdb_get's NDA_* in source
// order, mdb_show's two dumps and mdb_get's br_mdb_entry and nest.
//
// go test ./pkg/xtcpnl/ -run TestWsBridgeFdbMdbRequests
func TestWsBridgeFdbMdbRequests(t *testing.T) {
	want := map[string]int{"fdb dump": 69, "fdb get": 22, "mdb dump": 94, "mdb get": 22}
	got := map[string]int{}
	rows := 0
	for _, space := range []string{"bridgefdb", "bridgemdb"} {
		for _, dir := range fdbMdbWsDirs(t, space) {
			rows++
			for _, m := range pcapNlMsgs(t, filepath.Join(dir, "ip.pcap")) {
				if m.h.Flags&unix.NLM_F_REQUEST == 0 {
					continue
				}
				kind, built, ok, err := fdbMdbRebuild(m)
				if !ok {
					continue
				}
				if err != nil {
					t.Fatalf("%s: %s: %v", dir, kind, err)
				}
				full := fmNlmsgRaw(m.h, m.body)
				clear(full[8:16])
				if !bytes.Equal(full, built) {
					t.Errorf("%s: %s\n captured %x\n built    %x", dir, kind, full, built)
				}
				got[kind]++
			}
		}
	}
	if rows != 94+74 {
		t.Errorf("%d rows, want %d", rows, 94+74)
	}
	for k, n := range want {
		if got[k] != n {
			t.Errorf("%s: %d requests, want %d", k, got[k], n)
		}
	}
}

// fdbMdbGroupText is print_mdb_entry's grp: an address, or an L2
// group's colon hex.
func fdbMdbGroupText(g []byte) string {
	if a, ok := netip.AddrFromSlice(g); ok {
		return a.String()
	}
	parts := make([]string, len(g))
	for i, b := range g {
		parts[i] = fmt.Sprintf("%02x", b)
	}
	return strings.Join(parts, ":")
}

// fdbMdbDecoded is what a row's replies carry: the AF_BRIDGE neighbors'
// MACs, the mdb entries' groups, and the bridges with router ports.
type fdbMdbDecoded struct {
	macs    map[string]bool
	groups  map[string]bool
	routers map[uint32]bool
	entries int
}

func fdbMdbDecode(t *testing.T, dir string) fdbMdbDecoded {
	t.Helper()
	d := fdbMdbDecoded{macs: map[string]bool{}, groups: map[string]bool{}, routers: map[uint32]bool{}}
	for _, m := range pcapNlMsgs(t, filepath.Join(dir, "ip.pcap")) {
		if m.h.Flags&unix.NLM_F_REQUEST != 0 {
			continue
		}
		switch m.h.Type {
		case unix.RTM_NEWNEIGH:
			ni, err := ParseNeigh(m.body)
			if err != nil {
				t.Fatalf("%s: %v", dir, err)
			}
			if ni.Family == unix.AF_BRIDGE {
				d.macs[fdbMdbGroupText(ni.LLAddr)] = true
				d.entries++
			}
		case unix.RTM_GETMDB, unix.RTM_NEWMDB:
			md, err := ParseMdbDumpRouters(m.body)
			if err != nil {
				t.Fatalf("%s: %v", dir, err)
			}
			for i := range md.Entries {
				d.groups[fdbMdbGroupText(md.Entries[i].Entry.Group)] = true
			}
			d.entries += len(md.Entries)
			if md.HasRouter {
				d.routers[md.Bridge] = true
			}
		}
	}
	return d
}

// fdbMdbJSONObjs flattens a -j output to its entry objects and the
// router keys: fdb and mdb get print one array of entries, mdb show
// `[{"mdb":[...],"router":{...}}]`. A batch prints one value a command.
func fdbMdbJSONObjs(out []byte) (objs []map[string]any, routers []string, err error) {
	dec := json.NewDecoder(bytes.NewReader(out))
	for dec.More() {
		var v []map[string]any
		if err := dec.Decode(&v); err != nil {
			return nil, nil, err
		}
		for _, o := range v {
			mdb, isShow := o["mdb"].([]any)
			if !isShow {
				objs = append(objs, o)
				continue
			}
			for _, e := range mdb {
				if eo, ok := e.(map[string]any); ok {
					objs = append(objs, eo)
				}
			}
			if r, ok := o["router"].(map[string]any); ok {
				for k := range r {
					routers = append(routers, k)
				}
			}
		}
	}
	return objs, routers, nil
}

// TestWsBridgeFdbMdbJSON (O4): every object a -j row printed is an entry
// the kernel sent, by MAC (fdb) or group (mdb), never more objects than
// entries, and each router key is a bridge whose reply had MDBA_ROUTER.
//
// go test ./pkg/xtcpnl/ -run TestWsBridgeFdbMdbJSON
func TestWsBridgeFdbMdbJSON(t *testing.T) {
	want := map[string]int{"bridgefdb rows": 37, "bridgefdb objs": 643, "bridgemdb rows": 29, "bridgemdb objs": 228, "routers": 4}
	got := map[string]int{}
	for _, space := range []string{"bridgefdb", "bridgemdb"} {
		for _, dir := range fdbMdbWsDirs(t, space) {
			r := wsLoadRow(t, dir)
			if !strings.Contains(r.Cmd, " -j ") || wsReadRC(t, dir) != 0 {
				continue
			}
			out, err := os.ReadFile(filepath.Join(dir, "ip.out"))
			if err != nil {
				t.Fatal(err)
			}
			objs, routers, err := fdbMdbJSONObjs(out)
			if err != nil {
				t.Fatalf("%s: %v", dir, err)
			}
			d := fdbMdbDecode(t, dir)
			if len(objs) > d.entries {
				t.Errorf("%s: %d objects from %d entries", dir, len(objs), d.entries)
			}
			for _, o := range objs {
				key, set := "mac", d.macs
				if space == "bridgemdb" {
					key, set = "grp", d.groups
				}
				if s, _ := o[key].(string); !set[s] {
					t.Errorf("%s: %s %q is in no reply", dir, key, s)
				}
			}
			if len(routers) > len(d.routers) {
				t.Errorf("%s: router keys %v, %d bridges with MDBA_ROUTER", dir, routers, len(d.routers))
			}
			got[space+" rows"]++
			got[space+" objs"] += len(objs)
			got["routers"] += len(routers)
		}
	}
	for k, n := range want {
		if got[k] != n {
			t.Errorf("%s: %d, want %d", k, got[k], n)
		}
	}
}
