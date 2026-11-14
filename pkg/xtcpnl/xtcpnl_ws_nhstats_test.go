package xtcpnl

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// The nhstats rows' O3 (goip-parity-next.md, NC8-T05). Each row adds one
// nexthop (tools/nlcapgen/matrix_nhstats.go), checked against its axes,
// and reads it back seven ways; what NC8-T04 is about is each readback's
// request. ip sends NHA_OP_FLAGS in a dump only under -s:
// NHA_OP_FLAG_DUMP_STATS (1), plus NHA_OP_FLAG_DUMP_HW_STATS (2) under
// -s -s; a get, `nexthop get` or `show id`, always sends it with NHA_ID
// (ip/ipnexthop.c, ipnh_get_op_flags).

// nhstatsOpFlags is each readback's NHA_OP_FLAGS, -1 for none sent, and
// whether it is a get.
var nhstatsOpFlags = map[string]struct {
	flags int
	get   bool
}{
	"dump": {-1, false}, "dump_s": {1, false}, "dump_ss": {3, false},
	"dump_js": {1, false}, "dump_jss": {3, false},
	"get_s": {1, true}, "show_id_s": {1, true},
}

// nhstatsAddProblem checks an RTM_NEWNEXTHOP body against the row's axes.
func nhstatsAddProblem(ax map[string]string, flags uint16, body []byte) error {
	if len(body) < unix.SizeofNhmsg {
		return fmt.Errorf("body %d bytes", len(body))
	}
	attrs, err := kwAttrs(body[unix.SizeofNhmsg:])
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(ax))
	for k := range ax {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		if err := nhstatsAxis(k, ax, flags, attrs); err != nil {
			return fmt.Errorf("%s=%s: %w", k, ax[k], err)
		}
	}
	return nil
}

func nhstatsAxis(k string, ax map[string]string, flags uint16, attrs map[uint16][]byte) error {
	val := ax[k]
	switch k {
	case "verb":
		return kwWantVerb(flags, val)
	case "id":
		return kwWantU32(attrs, NhaID, val)
	case "group":
		return nhstatsWantGroup(attrs[NhaGroup], val)
	case "type":
		if val != "resilient" {
			return fmt.Errorf("group type %q", val)
		}
		b, err := kwFixed(attrs, NhaGroupType, 2)
		if err != nil {
			return err
		}
		if got := binary.NativeEndian.Uint16(b); got != NexthopGrpTypeRes {
			return fmt.Errorf("group type %d", got)
		}
		return nil
	case "buckets":
		res, err := kwAttrs(attrs[NhaResGroup])
		if err != nil {
			return err
		}
		b, err := kwFixed(res, NhaResGroupBuckets, 2)
		if err != nil {
			return err
		}
		if got := strconv.Itoa(int(binary.NativeEndian.Uint16(b))); got != val {
			return fmt.Errorf("buckets %s", got)
		}
		return nil
	case "hw_stats":
		return kwWantU32(attrs, NhaHwStatsEnable, val)
	case "via":
		b, err := kwFixed(attrs, NhaGateway, 4)
		if err != nil {
			return err
		}
		if got := netip.AddrFrom4([4]byte(b)).String(); got != val {
			return fmt.Errorf("gateway %s", got)
		}
		return nil
	case "encap":
		e, err := lwtFind(attrs, NhaEncap, NhaEncapType)
		if err != nil {
			return err
		}
		if e.typ != unix.LWTUNNEL_ENCAP_SEG6_LOCAL || val != "seg6local" {
			return fmt.Errorf("encap type %d", e.typ)
		}
		return nil
	case "action", "count":
		e, err := lwtFind(attrs, NhaEncap, NhaEncapType)
		if err != nil {
			return err
		}
		return lwtxLocalAxis(k, val, "", "", e)
	}
	return fmt.Errorf("an nhstats axis %q", k)
}

// nhstatsWantGroup checks NHA_GROUP's struct nexthop_grp entries (id u32,
// weight-1 u8, weight_high u8, resvd u16) against "id[,weight]/...".
func nhstatsWantGroup(b []byte, val string) error {
	members := strings.Split(val, "/")
	if len(b) != 8*len(members) {
		return fmt.Errorf("NHA_GROUP %d bytes, want %d entries", len(b), len(members))
	}
	for i, m := range members {
		id, w, _ := strings.Cut(m, ",")
		if w == "" {
			w = "1"
		}
		e := b[8*i:]
		gotW := int(e[4]) + 1 + int(e[5])<<8
		if strconv.Itoa(int(binary.NativeEndian.Uint32(e))) != id || strconv.Itoa(gotW) != w {
			return fmt.Errorf("entry %d is id %d weight %d", i, binary.NativeEndian.Uint32(e), gotW)
		}
	}
	return nil
}

// nhstatsReadbackProblem checks a readback's RTM_GETNEXTHOP: a dump or a
// get, its NHA_OP_FLAGS, and the NHA_ID a get carries.
func nhstatsReadbackProblem(name, id string, msgs []capturedNlMsg) error {
	want, ok := nhstatsOpFlags[name]
	if !ok {
		return fmt.Errorf("readback %q", name)
	}
	reqs := wsRouteRequests(msgs, unix.RTM_GETNEXTHOP)
	if len(reqs) != 1 {
		return fmt.Errorf("%d RTM_GETNEXTHOP requests", len(reqs))
	}
	r := reqs[0]
	if dump := r.h.Flags&unix.NLM_F_DUMP == unix.NLM_F_DUMP; dump == want.get {
		return fmt.Errorf("flags %#x, get %v", r.h.Flags, want.get)
	}
	if len(r.body) < unix.SizeofNhmsg {
		return fmt.Errorf("body %d bytes", len(r.body))
	}
	attrs, err := kwAttrs(r.body[unix.SizeofNhmsg:])
	if err != nil {
		return err
	}
	if want.flags < 0 {
		if _, ok := attrs[NhaOpFlags]; ok || len(attrs) != 0 {
			return fmt.Errorf("attributes %v, want none", slices.Sorted(func(yield func(uint16) bool) {
				for a := range attrs {
					if !yield(a) {
						return
					}
				}
			}))
		}
		return nil
	}
	if err := kwWantU32(attrs, NhaOpFlags, strconv.Itoa(want.flags)); err != nil {
		return err
	}
	if want.get {
		return kwWantU32(attrs, NhaID, id)
	}
	if len(attrs) != 1 {
		return fmt.Errorf("%d attributes in the dump, want NHA_OP_FLAGS alone", len(attrs))
	}
	return nil
}

// nhstatsGetMsg is a captured RTM_GETNEXTHOP request with the given
// flags and attributes.
func nhstatsGetMsg(t *testing.T, flags uint16, put func(ab *AttrBuilder) error) []capturedNlMsg {
	t.Helper()
	return []capturedNlMsg{{h: NlMsgHdr{Type: unix.RTM_GETNEXTHOP, Flags: flags},
		body: kwBody(t, make([]byte, unix.SizeofNhmsg), put)}}
}

// TestNhstatsProblems holds both checks to hand-built requests.
//
// go test ./pkg/xtcpnl/ -run TestNhstatsProblems
func TestNhstatsProblems(t *testing.T) {
	const add = unix.NLM_F_REQUEST | unix.NLM_F_ACK | unix.NLM_F_CREATE | unix.NLM_F_EXCL
	const dump = unix.NLM_F_REQUEST | unix.NLM_F_DUMP
	nh := make([]byte, unix.SizeofNhmsg)
	group := func(entries ...[2]uint32) func(ab *AttrBuilder) error {
		return func(ab *AttrBuilder) error {
			b := make([]byte, 8*len(entries))
			for i, e := range entries {
				binary.NativeEndian.PutUint32(b[8*i:], e[0])
				b[8*i+4] = byte(e[1] - 1)
			}
			if err := ab.PutU32(NhaID, 10); err != nil {
				return err
			}
			return ab.PutBytes(NhaGroup, b)
		}
	}
	op := func(flags uint32, id uint32) func(ab *AttrBuilder) error {
		return func(ab *AttrBuilder) error {
			if id != 0 {
				if err := ab.PutU32(NhaID, id); err != nil {
					return err
				}
			}
			return ab.PutU32(NhaOpFlags, flags)
		}
	}
	facts := lwtTestFacts
	addTests := []struct {
		description string
		ax          map[string]string
		body        []byte
		expected    string // "" passes; else in the error
	}{
		{"positive: a 2-way group", facts("verb", "add", "id", "10", "group", "1/2"), kwBody(t, nh, group([2]uint32{1, 1}, [2]uint32{2, 1})), ""},
		{"positive: weights", facts("id", "10", "group", "1,3/2,7"), kwBody(t, nh, group([2]uint32{1, 3}, [2]uint32{2, 7})), ""},
		{"negative: a weight off by one", facts("group", "1,3/2,7"), kwBody(t, nh, group([2]uint32{1, 4}, [2]uint32{2, 7})), "weight 4"},
		{"negative: a member missing", facts("group", "1/2"), kwBody(t, nh, group([2]uint32{1, 1})), "want 2 entries"},
		{"negative: an axis with no check", facts("color", "red"), kwBody(t, nh, group([2]uint32{1, 1})), `axis "color"`},
		{"boundary: a group of one", facts("group", "1"), kwBody(t, nh, group([2]uint32{1, 1})), ""},
		{"corner: hw_stats absent", facts("hw_stats", "1"), kwBody(t, nh, group([2]uint32{1, 1})), "no attribute 16"},
	}
	for i := range addTests {
		tt := &addTests[i]
		t.Run(tt.description, func(t *testing.T) {
			err := nhstatsAddProblem(tt.ax, add, tt.body)
			switch {
			case tt.expected == "" && err != nil:
				t.Fatalf("err = %v, want none", err)
			case tt.expected != "" && (err == nil || !strings.Contains(err.Error(), tt.expected)):
				t.Fatalf("err = %v, want one with %q", err, tt.expected)
			}
		})
	}
	rbTests := []struct {
		description string
		name        string
		msgs        []capturedNlMsg
		expected    string
	}{
		{"positive: the plain dump sends nothing", "dump", nhstatsGetMsg(t, dump, nil), ""},
		{"positive: -s sends 1", "dump_s", nhstatsGetMsg(t, dump, op(1, 0)), ""},
		{"positive: -s -s sends 3", "dump_jss", nhstatsGetMsg(t, dump, op(3, 0)), ""},
		{"positive: a get sends 1 and the id", "get_s", nhstatsGetMsg(t, unix.NLM_F_REQUEST, op(1, 10)), ""},
		{"negative: -s -s sending 1", "dump_ss", nhstatsGetMsg(t, dump, op(1, 0)), "is 1"},
		{"negative: the plain dump sending flags", "dump", nhstatsGetMsg(t, dump, op(0, 0)), "want none"},
		{"negative: a get of another id", "show_id_s", nhstatsGetMsg(t, unix.NLM_F_REQUEST, op(1, 11)), "is 11"},
		{"negative: a get sent as a dump", "get_s", nhstatsGetMsg(t, dump, op(1, 10)), "get true"},
		{"negative: the dump with an id too", "dump_s", nhstatsGetMsg(t, dump, op(1, 10)), "want NHA_OP_FLAGS alone"},
		{"boundary: no request", "dump_s", nil, "0 RTM_GETNEXTHOP"},
		{"corner: an unknown readback", "dump_x", nil, `readback "dump_x"`},
	}
	for i := range rbTests {
		tt := &rbTests[i]
		t.Run(tt.description, func(t *testing.T) {
			err := nhstatsReadbackProblem(tt.name, "10", tt.msgs)
			switch {
			case tt.expected == "" && err != nil:
				t.Fatalf("err = %v, want none", err)
			case tt.expected != "" && (err == nil || !strings.Contains(err.Error(), tt.expected)):
				t.Fatalf("err = %v, want one with %q", err, tt.expected)
			}
		})
	}
}

// TestWsNhstatsCorpus runs both checks over every captured nhstats row:
// the add (none for the traffic row, whose command is ping) and all seven
// readbacks' requests.
//
// go test ./pkg/xtcpnl/ -run TestWsNhstatsCorpus
func TestWsNhstatsCorpus(t *testing.T) {
	var adds, readbacks int
	for _, dir := range wsRows(t) {
		row := wsLoadRow(t, dir)
		if !strings.HasPrefix(row.ID, "nhstats/") {
			continue
		}
		t.Run(row.Env.Kernel+"/"+row.ID, func(t *testing.T) {
			if row.Result.Status != "OK" {
				return // TestWsCorpusRows fails it
			}
			reqs := wsRouteRequests(pcapNlMsgs(t, filepath.Join(dir, "ip.pcap")), unix.RTM_NEWNEXTHOP)
			switch {
			case row.Axes["verb"] == "traffic":
				if len(reqs) != 0 {
					t.Errorf("O3: the ping row sent %d requests", len(reqs))
				}
			case len(reqs) != 1:
				t.Fatalf("O3: %d RTM_NEWNEXTHOP requests, want 1\n  cmd: %s", len(reqs), row.Cmd)
			default:
				adds++
				if err := nhstatsAddProblem(row.Axes, reqs[0].h.Flags, reqs[0].body); err != nil {
					t.Errorf("O3: %v\n  cmd: %s", err, row.Cmd)
				}
			}
			if len(row.Readback) != len(nhstatsOpFlags) {
				t.Errorf("%d readbacks, want %d", len(row.Readback), len(nhstatsOpFlags))
			}
			for _, rb := range row.Readback {
				readbacks++
				msgs := pcapNlMsgs(t, filepath.Join(dir, "netlink_route_"+rb.Name+".pcap"))
				if err := nhstatsReadbackProblem(rb.Name, row.Axes["id"], msgs); err != nil {
					t.Error(errors.New("O3 readback " + rb.Name + ": " + err.Error()))
				}
			}
		})
	}
	t.Logf("O3 checked %d nhstats adds and %d readbacks", adds, readbacks)
	if adds != 10 || readbacks != 77 {
		t.Errorf("O3 checked %d adds and %d readbacks, want 10 and 77", adds, readbacks)
	}
}
