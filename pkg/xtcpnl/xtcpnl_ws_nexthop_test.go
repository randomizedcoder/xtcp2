package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// The ip nexthop rows' oracles (coverage-status.md, "Grammar matrix (W2)").
// O3 rebuilds the request a row's axes call for (tools/nlcapgen/
// matrix_nexthop.go), following ip/ipnexthop.c ipnh_modify, and checks the
// nhmsg, the attribute set and widths, and each value. NHA_GROUP is decoded
// raw: a request has no NHA_OP_FLAGS, so ParseNewNexthop would drop
// weight_high. O4 looks for the nexthop in the dump read back after it.

// NHA_HW_STATS_ENABLE and NHA_DST_PORT (include/uapi/linux/nexthop.h),
// which the parser does not model.
const (
	wsNhaHwStatsEnable uint16 = 16
	wsNhaDstPort       uint16 = 18
)

// nhWantMember is one NHA_GROUP entry; a weight runs 1 to 65536.
type nhWantMember struct {
	id     uint32
	weight uint32
}

// nhWant is the request a row's axes call for.
type nhWant struct {
	typ, flags uint16
	hdr        Nhmsg
	attrs      []uint16 // every NHA_* the request carries, sorted
	id         uint32
	gw         []byte
	group      []nhWantMember
	grpType    uint16
	resAttrs   []uint16 // every NHA_RES_GROUP_* in the nest, sorted
	res        ResGroup
	hwStats    uint32
}

// nhAttrWidths is the payload length each fixed-width NHA_* must have.
var nhAttrWidths = map[uint16]int{
	NhaID: 4, NhaOIF: 4, NhaBlackhole: 0, NhaFdb: 0, NhaGroupType: 2,
	wsNhaHwStatsEnable: 4, wsNhaDstPort: 2,
}

// nhResWidths is the payload length of each NHA_RES_GROUP_* iproute2 sends.
var nhResWidths = map[uint16]int{NhaResGroupBuckets: 2, NhaResGroupIdleTimer: 4, NhaResGroupUnbalancedTimer: 4}

// axGroup reads an axis "id/weight;..." member list.
func axGroup(s string) ([]nhWantMember, error) {
	var out []nhWantMember
	for e := range strings.SplitSeq(s, ";") {
		a, b, cut := strings.Cut(e, "/")
		id, err1 := strconv.ParseUint(a, 10, 32)
		w, err2 := strconv.ParseUint(b, 10, 32)
		if !cut || err1 != nil || err2 != nil || w == 0 || w > 65536 {
			return nil, fmt.Errorf("axis group entry %q is not id/weight", e)
		}
		out = append(out, nhWantMember{id: uint32(id), weight: uint32(w)})
	}
	return out, nil
}

// nhWantHeader sets the message type and flags (do_ipnh) and the nhmsg:
// the family -4/-6 named (groups name none), the protocol and onlink.
func nhWantHeader(ax map[string]string, w *nhWant) error {
	switch ax["verb"] {
	case "add":
		w.typ, w.flags = unix.RTM_NEWNEXTHOP, unix.NLM_F_CREATE|unix.NLM_F_EXCL
	case "replace":
		w.typ, w.flags = unix.RTM_NEWNEXTHOP, unix.NLM_F_CREATE|unix.NLM_F_REPLACE
	case "del":
		w.typ = unix.RTM_DELNEXTHOP
	default:
		return fmt.Errorf("axis verb=%q", ax["verb"])
	}
	switch ax["family"] {
	case "4":
		w.hdr.Family = unix.AF_INET
	case "6":
		w.hdr.Family = unix.AF_INET6
	case "":
	default:
		return fmt.Errorf("axis family=%q", ax["family"])
	}
	p, _, err := axU8(ax, "protocol")
	w.hdr.Protocol = p
	if ax["onlink"] == "1" {
		w.hdr.Flags = unix.RTNH_F_ONLINK
	}
	return err
}

// nhWantGroup sets NHA_GROUP, NHA_GROUP_TYPE and the resilient nest, which
// parse_nh_group_type sends for any resilient group, empty or not.
func nhWantGroup(ax map[string]string, w *nhWant) error {
	if s, ok := ax["group"]; ok {
		g, err := axGroup(s)
		if err != nil {
			return err
		}
		w.group = g
		w.attrs = append(w.attrs, NhaGroup)
	}
	t, ok, err := axU32(ax, "grp_type")
	if err != nil || !ok {
		return err
	}
	if t > 0xffff {
		return fmt.Errorf("axis grp_type=%d is past u16", t)
	}
	w.grpType = uint16(t)
	w.attrs = append(w.attrs, NhaGroupType)
	if w.grpType != NexthopGrpTypeRes {
		return nil
	}
	w.attrs = append(w.attrs, NhaResGroup)
	b, okB, err1 := axU32(ax, "buckets")
	idle, okI, err2 := axU32(ax, "idle_timer")
	unbal, okU, err3 := axU32(ax, "unbalanced_timer")
	if err := errors.Join(err1, err2, err3); err != nil {
		return err
	}
	if b > 0xffff {
		return fmt.Errorf("axis buckets=%d is past u16", b)
	}
	w.res = ResGroup{Buckets: uint16(b), IdleTimer: idle, UnbalancedTimer: unbal}
	for _, r := range []struct {
		ok bool
		a  uint16
	}{{okB, NhaResGroupBuckets}, {okI, NhaResGroupIdleTimer}, {okU, NhaResGroupUnbalancedTimer}} {
		if r.ok {
			w.resAttrs = append(w.resAttrs, r.a)
		}
	}
	return nil
}

// nhWantFromAxes models ipnh_modify. A delete's axes hold only its verb,
// id and family, so it carries NHA_ID alone.
func nhWantFromAxes(ax map[string]string) (nhWant, error) {
	var w nhWant
	if err := nhWantHeader(ax, &w); err != nil {
		return w, err
	}
	id, ok, err := axU32(ax, "id")
	if err != nil {
		return w, err
	}
	if ok {
		w.id = id
		w.attrs = append(w.attrs, NhaID)
	}
	if _, ok := ax["oif"]; ok {
		w.attrs = append(w.attrs, NhaOIF)
	}
	if w.gw, err = axAddr(ax, "gw"); err != nil {
		return w, err
	} else if w.gw != nil {
		w.attrs = append(w.attrs, NhaGateway)
	}
	for _, fl := range []struct {
		axis string
		attr uint16
	}{{"blackhole", NhaBlackhole}, {"fdb", NhaFdb}} {
		if ax[fl.axis] == "1" {
			w.attrs = append(w.attrs, fl.attr)
		}
	}
	if hw, ok, err := axU32(ax, "hw_stats"); err != nil {
		return w, err
	} else if ok {
		w.hwStats = hw
		w.attrs = append(w.attrs, wsNhaHwStatsEnable)
	}
	if err := nhWantGroup(ax, &w); err != nil {
		return w, err
	}
	slices.Sort(w.attrs)
	return w, nil
}

// nhWantForCmd drops the resilient nest when "type resilient" ends the
// line: parse_nh_group_type_res opens it only if NEXT_ARG_OK, whatever the
// next argument is (so "type resilient fdb" sends an empty one).
func nhWantForCmd(w nhWant, cmd string) nhWant {
	if strings.HasSuffix(strings.TrimSpace(cmd), "type resilient") {
		w.attrs = slices.DeleteFunc(slices.Clone(w.attrs), func(a uint16) bool { return a == NhaResGroup })
	}
	return w
}

// nhRawGroup decodes a request's NHA_GROUP as nhgrp_set_weight wrote it:
// weight - 1 split over weight and weight_high.
func nhRawGroup(val []byte) ([]nhWantMember, error) {
	if len(val) == 0 || len(val)%NexthopGrpSizeCst != 0 {
		return nil, ErrNhGroupBad
	}
	out := make([]nhWantMember, 0, len(val)/NexthopGrpSizeCst)
	for off := 0; off < len(val); off += NexthopGrpSizeCst {
		w := uint32(val[off+5])<<8 | uint32(val[off+4])
		out = append(out, nhWantMember{id: binary.LittleEndian.Uint32(val[off : off+4]), weight: w + 1})
	}
	return out, nil
}

// nhResProblem checks the NHA_RES_GROUP nest's attributes, widths and values.
func nhResProblem(w nhWant, val []byte) error {
	var errs []error
	var got []uint16
	var res ResGroup
	err := WalkRTAttrsNested(val, func(a uint16, v []byte) {
		got = append(got, a)
		if n, ok := nhResWidths[a]; !ok || len(v) != n {
			errs = append(errs, fmt.Errorf("NHA_RES_GROUP attribute %d is %d bytes, want %d", a, len(v), n))
			return
		}
		switch a {
		case NhaResGroupBuckets:
			res.Buckets = binary.LittleEndian.Uint16(v)
		case NhaResGroupIdleTimer:
			res.IdleTimer = binary.LittleEndian.Uint32(v)
		case NhaResGroupUnbalancedTimer:
			res.UnbalancedTimer = binary.LittleEndian.Uint32(v)
		}
	})
	if err != nil {
		return err
	}
	slices.Sort(got)
	if !slices.Equal(got, w.resAttrs) {
		errs = append(errs, fmt.Errorf("NHA_RES_GROUP attributes %v, want %v", got, w.resAttrs))
	}
	if res != w.res {
		errs = append(errs, fmt.Errorf("resilient group %+v, want %+v", res, w.res))
	}
	return errors.Join(errs...)
}

// nhAttrProblem checks one request attribute's width and value.
func nhAttrProblem(w nhWant, a uint16, val []byte) error {
	if n, ok := nhAttrWidths[a]; ok && len(val) != n {
		return fmt.Errorf("attribute %d is %d bytes, want %d", a, len(val), n)
	}
	switch a {
	case NhaID:
		if got := binary.LittleEndian.Uint32(val); got != w.id {
			return fmt.Errorf("NHA_ID %d, want %d", got, w.id)
		}
	case NhaGateway:
		if !bytes.Equal(val, w.gw) {
			return fmt.Errorf("NHA_GATEWAY %v, want %v", val, w.gw)
		}
	case NhaGroup:
		g, err := nhRawGroup(val)
		if err != nil {
			return err
		}
		if !slices.Equal(g, w.group) {
			return fmt.Errorf("NHA_GROUP %v, want %v", g, w.group)
		}
	case NhaGroupType:
		if got := binary.LittleEndian.Uint16(val); got != w.grpType {
			return fmt.Errorf("NHA_GROUP_TYPE %d, want %d", got, w.grpType)
		}
	case NhaResGroup:
		return nhResProblem(w, val)
	case wsNhaHwStatsEnable:
		if got := binary.LittleEndian.Uint32(val); got != w.hwStats {
			return fmt.Errorf("NHA_HW_STATS_ENABLE %d, want %d", got, w.hwStats)
		}
	}
	return nil
}

// nhRequestProblem is O3: how a captured request differs from the model's.
// body is the message after its nlmsghdr.
func nhRequestProblem(w nhWant, flags uint16, body []byte) error {
	var errs []error
	if got := flags &^ (unix.NLM_F_REQUEST | unix.NLM_F_ACK); got != w.flags {
		errs = append(errs, fmt.Errorf("nlmsg flags %#x, want %#x", got, w.flags))
	}
	var h Nhmsg
	if _, err := DeserializeNhmsg(body, &h); err != nil {
		return err
	}
	if h != w.hdr {
		errs = append(errs, fmt.Errorf("nhmsg %+v, want %+v", h, w.hdr))
	}
	var attrs []uint16
	err := WalkRTAttrs(body[NhMsgSizeCst:], func(a uint16, val []byte) {
		attrs = append(attrs, a)
		if err := nhAttrProblem(w, a, val); err != nil {
			errs = append(errs, err)
		}
	})
	if err != nil {
		return err
	}
	slices.Sort(attrs)
	if !slices.Equal(attrs, w.attrs) {
		errs = append(errs, fmt.Errorf("attributes %v, want %v", attrs, w.attrs))
	}
	return errors.Join(errs...)
}

// nhDumpMatches reports whether a dumped nexthop is the one the request
// made: its id when it named one (0 asks the kernel for one), and what the
// kernel keeps of the rest. A group dumps as AF_UNSPEC; its weights are
// whole only when the dump says weight_high is filled in (or none passes
// 256), and its resilient timers only when the request named them.
func nhDumpMatches(w nhWant, d NexthopInfo) bool {
	if w.id != 0 && d.ID != w.id {
		return false
	}
	same := d.Family == w.hdr.Family && d.Protocol == w.hdr.Protocol &&
		d.Flags&unix.RTNH_F_ONLINK == w.hdr.Flags&unix.RTNH_F_ONLINK &&
		bytes.Equal(d.Gateway, w.gw) && (d.OIF != 0) == slices.Contains(w.attrs, NhaOIF) &&
		d.Blackhole == slices.Contains(w.attrs, NhaBlackhole) && d.Fdb == slices.Contains(w.attrs, NhaFdb) &&
		d.HasGroup == (w.group != nil) && len(d.Group) == len(w.group)
	if !same || w.group == nil {
		return same
	}
	whole := d.RespOpFlags&NhaOpFlagRespGrpResvd0 != 0
	for i := range w.group {
		g := w.group[i]
		if d.Group[i].ID != g.id || ((whole || g.weight <= 256) && uint32(d.Group[i].Weight) != g.weight) {
			return false
		}
	}
	return d.GroupType == w.grpType && d.HasResGroup == (w.grpType == NexthopGrpTypeRes) && nhResMatches(w, d.ResGroup)
}

// nhResMatches holds a dumped resilient group to the arguments named.
func nhResMatches(w nhWant, d ResGroup) bool {
	for _, a := range w.resAttrs {
		switch {
		case a == NhaResGroupBuckets && d.Buckets != w.res.Buckets,
			a == NhaResGroupIdleTimer && d.IdleTimer != w.res.IdleTimer,
			a == NhaResGroupUnbalancedTimer && d.UnbalancedTimer != w.res.UnbalancedTimer:
			return false
		}
	}
	return true
}

// wsNexthopDump returns the nexthops a readback's dump replied with.
func wsNexthopDump(t *testing.T, msgs []capturedNlMsg) []NexthopInfo {
	t.Helper()
	var out []NexthopInfo
	for _, m := range msgs {
		if m.proto != unix.NETLINK_ROUTE || m.h.Type != unix.RTM_NEWNEXTHOP || m.h.Flags&unix.NLM_F_REQUEST != 0 {
			continue
		}
		nh, err := ParseNewNexthop(m.body)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, nh)
	}
	return out
}

// TestWsNexthopCorpus runs O3 and O4 over every generated nexthop row, and
// fails if O3 checked nothing.
func TestWsNexthopCorpus(t *testing.T) {
	var checked, roundTrips int
	for _, dir := range wsRows(t) {
		row := wsLoadRow(t, dir)
		if !strings.HasPrefix(row.ID, "nh4/") && !strings.HasPrefix(row.ID, "nh6/") && !strings.HasPrefix(row.ID, "nhgrp/") {
			continue
		}
		t.Run(row.Env.Kernel+"/"+row.ID, func(t *testing.T) {
			if row.Result.Status != "OK" {
				return // TestWsCorpusRows fails it
			}
			c, rt := wsNexthopRow(t, dir, row)
			checked += c
			roundTrips += rt
		})
	}
	t.Logf("O3 checked %d nexthop requests, O4 %d round trips", checked, roundTrips)
	if checked == 0 {
		t.Error("O3 checked no nexthop request: are there nexthop rows under testdata/*/ws?")
	}
}

func wsNexthopRow(t *testing.T, dir string, row wsRow) (checked, roundTrips int) {
	t.Helper()
	w, err := nhWantFromAxes(row.Axes)
	if err != nil {
		t.Fatalf("O3 model: %v", err)
	}
	w = nhWantForCmd(w, row.Cmd)
	reqs := wsRouteRequests(pcapNlMsgs(t, filepath.Join(dir, "ip.pcap")), w.typ)
	rc := wsReadRC(t, dir)
	if len(reqs) == 0 {
		if rc == 0 {
			t.Errorf("O3: rc 0 but no request of type %d\n  cmd: %s", w.typ, row.Cmd)
		}
		return 0, 0
	}
	if len(reqs) != 1 {
		t.Fatalf("O3: %d requests of type %d, want 1\n  cmd: %s", len(reqs), w.typ, row.Cmd)
	}
	if err := nhRequestProblem(w, reqs[0].h.Flags, reqs[0].body); err != nil {
		t.Errorf("O3: %v\n  cmd: %s", err, row.Cmd)
	}
	if rc != 0 {
		return 1, 0
	}
	dump := wsNexthopDump(t, pcapNlMsgs(t, filepath.Join(dir, "netlink_route_dump.pcap")))
	if w.typ == unix.RTM_DELNEXTHOP {
		if slices.ContainsFunc(dump, func(d NexthopInfo) bool { return d.ID == w.id }) {
			t.Errorf("O4: after del the dump's %d nexthops still hold id %d\n  cmd: %s", len(dump), w.id, row.Cmd)
		}
		return 1, 1
	}
	if !slices.ContainsFunc(dump, func(d NexthopInfo) bool { return nhDumpMatches(w, d) }) {
		t.Errorf("O4: the dump's %d nexthops lack it: %+v\n  cmd: %s", len(dump), dump, row.Cmd)
	}
	return 1, 1
}

// nhmsgBody is an nhmsg built from h, then attrs.
func nhmsgBody(h Nhmsg, attrs ...[]byte) []byte {
	b := []byte{h.Family, h.Scope, h.Protocol, h.Resvd}
	return cat(append([][]byte{b, u32le(h.Flags)}, attrs...)...)
}

// nhGrp is one struct nexthop_grp as nhgrp_set_weight fills it.
func nhGrp(id uint32, weight uint32) []byte {
	w := weight - 1
	return cat(u32le(id), []byte{byte(w), byte(w >> 8), 0, 0})
}

func u16le(v uint16) []byte { return binary.LittleEndian.AppendUint16(nil, v) }

func TestNhWantFromAxes(t *testing.T) {
	base := map[string]string{"verb": "add", "family": "4", "id": "10", "oif": "d0"}
	grp := map[string]string{"verb": "add", "id": "100", "group": "21/1;22/20"}
	with := func(m map[string]string, kv ...string) map[string]string {
		m = maps.Clone(m)
		for i := 0; i+1 < len(kv); i += 2 {
			if kv[i+1] == "" {
				delete(m, kv[i])
			} else {
				m[kv[i]] = kv[i+1]
			}
		}
		return m
	}
	cases := []struct {
		description string
		axes        map[string]string
		expected    func(w nhWant) bool
		wantErr     bool
	}{
		{"positive: a device nexthop", base, func(w nhWant) bool {
			return w.typ == unix.RTM_NEWNEXTHOP && w.flags == unix.NLM_F_CREATE|unix.NLM_F_EXCL &&
				w.hdr.Family == unix.AF_INET && w.id == 10 && slices.Equal(w.attrs, []uint16{NhaID, NhaOIF})
		}, false},
		{"positive: a replace asks CREATE|REPLACE", with(base, "verb", "replace"), func(w nhWant) bool {
			return w.flags == unix.NLM_F_CREATE|unix.NLM_F_REPLACE
		}, false},
		{"positive: a delete carries the id alone", map[string]string{"verb": "del", "family": "6", "id": "10"}, func(w nhWant) bool {
			return w.typ == unix.RTM_DELNEXTHOP && w.flags == 0 && w.hdr.Family == unix.AF_INET6 && slices.Equal(w.attrs, []uint16{NhaID})
		}, false},
		{"positive: onlink is a header flag", with(base, "gw", "203.0.113.1", "onlink", "1", "protocol", "4"), func(w nhWant) bool {
			return w.hdr.Flags == unix.RTNH_F_ONLINK && w.hdr.Protocol == 4 && len(w.gw) == 4 && slices.Contains(w.attrs, NhaGateway)
		}, false},
		{"positive: blackhole and fdb are flags", with(base, "oif", "", "blackhole", "1", "fdb", "1"), func(w nhWant) bool {
			return slices.Equal(w.attrs, []uint16{NhaID, NhaBlackhole, NhaFdb})
		}, false},
		{"corner: id 0 is still sent", with(base, "id", "0"), func(w nhWant) bool {
			return w.id == 0 && slices.Contains(w.attrs, NhaID)
		}, false},
		{"positive: a group names no family", grp, func(w nhWant) bool {
			return w.hdr.Family == unix.AF_UNSPEC && slices.Equal(w.group, []nhWantMember{{21, 1}, {22, 20}}) &&
				slices.Equal(w.attrs, []uint16{NhaID, NhaGroup})
		}, false},
		{"boundary: weight 65536, which the kernel refuses but the model takes", with(grp, "group", "21/65536"), func(w nhWant) bool {
			return w.group[0].weight == 65536
		}, false},
		{"positive: mpath sends a type and no nest", with(grp, "grp_type", "0"), func(w nhWant) bool {
			return w.grpType == NexthopGrpTypeMpath && !slices.Contains(w.attrs, NhaResGroup)
		}, false},
		{"positive: resilient sends the nest of what was named", with(grp, "grp_type", "1", "buckets", "8", "idle_timer", "1000", "unbalanced_timer", "2000"),
			func(w nhWant) bool {
				return slices.Contains(w.attrs, NhaResGroup) && w.res == ResGroup{Buckets: 8, IdleTimer: 1000, UnbalancedTimer: 2000} &&
					slices.Equal(w.resAttrs, []uint16{NhaResGroupBuckets, NhaResGroupIdleTimer, NhaResGroupUnbalancedTimer})
			}, false},
		{"positive: hw_stats off is still sent", with(grp, "hw_stats", "0"), func(w nhWant) bool {
			return w.hwStats == 0 && slices.Contains(w.attrs, wsNhaHwStatsEnable)
		}, false},
		{"negative: an unknown verb", with(base, "verb", "flush"), nil, true},
		{"negative: an unknown family", with(base, "family", "5"), nil, true},
		{"negative: a protocol past u8", with(base, "protocol", "256"), nil, true},
		{"negative: an id past u32", with(base, "id", "4294967296"), nil, true},
		{"negative: a member without a weight", with(grp, "group", "21"), nil, true},
		{"negative: a weight of 0", with(grp, "group", "21/0"), nil, true},
		{"negative: a weight past 65536", with(grp, "group", "21/65537"), nil, true},
		{"negative: buckets past u16", with(grp, "grp_type", "1", "buckets", "65536"), nil, true},
		{"negative: a group type past u16", with(grp, "grp_type", "65536"), nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			w, err := nhWantFromAxes(tc.axes)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err %v, want error %v", err, tc.wantErr)
			}
			if !tc.wantErr && !tc.expected(w) {
				t.Errorf("want %+v", w)
			}
		})
	}
}

func TestNhWantForCmd(t *testing.T) {
	w := nhWant{attrs: []uint16{NhaID, NhaGroup, NhaGroupType, NhaResGroup}}
	cases := []struct {
		description string
		cmd         string
		expected    bool // the nest is still wanted
	}{
		{"positive: type resilient ends the line", "ip nexthop add id 100 group 21 type resilient", false},
		{"positive: another argument follows", "ip nexthop add id 100 group 21 type resilient fdb", true},
		{"positive: resilient arguments follow", "ip nexthop add id 100 group 21 type resilient buckets 8", true},
		{"corner: trailing blanks", "ip nexthop add id 100 group 21 type resilient  ", false},
		{"boundary: an empty command", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			got := nhWantForCmd(w, tc.cmd)
			if slices.Contains(got.attrs, NhaResGroup) != tc.expected {
				t.Errorf("attrs %v, want the nest %v", got.attrs, tc.expected)
			}
			if !slices.Contains(w.attrs, NhaResGroup) {
				t.Error("the caller's attrs were changed")
			}
		})
	}
}

func TestNhRequestProblem(t *testing.T) {
	want := func(ax map[string]string) nhWant {
		w, err := nhWantFromAxes(ax)
		if err != nil {
			t.Fatal(err)
		}
		return w
	}
	g := want(map[string]string{"verb": "add", "id": "100", "group": "21/1;22/65535", "grp_type": "1",
		"buckets": "8", "idle_timer": "1000", "hw_stats": "0"})
	v := want(map[string]string{"verb": "add", "family": "4", "id": "10", "oif": "d0", "gw": "203.0.113.1", "onlink": "1"})
	id := rtattr(NhaID, u32le(100))
	members := rtattr(NhaGroup, cat(nhGrp(21, 1), nhGrp(22, 65535)))
	typ := rtattr(NhaGroupType, u16le(NexthopGrpTypeRes))
	nest := func(attrs ...[]byte) []byte { return rtattr(NhaResGroup|unix.NLA_F_NESTED, cat(attrs...)) }
	res := nest(rtattr(NhaResGroupBuckets, u16le(8)), rtattr(NhaResGroupIdleTimer, u32le(1000)))
	hw := rtattr(wsNhaHwStatsEnable, u32le(0))
	vBody := func(gw []byte) []byte {
		return nhmsgBody(v.hdr, rtattr(NhaID, u32le(10)), rtattr(NhaOIF, u32le(2)), rtattr(NhaGateway, gw))
	}
	flags := uint16(unix.NLM_F_REQUEST | unix.NLM_F_ACK | unix.NLM_F_CREATE | unix.NLM_F_EXCL)
	inet := g.hdr
	inet.Family = unix.AF_INET
	cases := []struct {
		description string
		w           nhWant
		flags       uint16
		body        []byte
		expected    string // in the error; "" for none
	}{
		{"positive: the resilient group the axes call for", g, flags, nhmsgBody(g.hdr, id, members, typ, res, hw), ""},
		{"positive: attribute order does not matter", g, flags, nhmsgBody(g.hdr, hw, res, typ, members, id), ""},
		{"positive: an onlink gateway", v, flags, vBody([]byte{203, 0, 113, 1}), ""},
		{"negative: the gateway's last byte wrong", v, flags, vBody([]byte{203, 0, 113, 2}), "NHA_GATEWAY"},
		{"negative: onlink left out of the header", v, flags, nhmsgBody(g.hdr, rtattr(NhaID, u32le(10))), "nhmsg"},
		{"negative: weight_high dropped", g, flags, nhmsgBody(g.hdr, id, rtattr(NhaGroup, cat(nhGrp(21, 1), nhGrp(22, 255))), typ, res, hw), "NHA_GROUP"},
		{"negative: buckets sent as a u32", g, flags, nhmsgBody(g.hdr, id, members, typ,
			nest(rtattr(NhaResGroupBuckets, u32le(8)), rtattr(NhaResGroupIdleTimer, u32le(1000))), hw), "is 4 bytes, want 2"},
		{"negative: the idle timer missing from the nest", g, flags, nhmsgBody(g.hdr, id, members, typ,
			nest(rtattr(NhaResGroupBuckets, u16le(8))), hw), "NHA_RES_GROUP attributes"},
		{"negative: the idle timer in seconds, not clock_t", g, flags, nhmsgBody(g.hdr, id, members, typ,
			nest(rtattr(NhaResGroupBuckets, u16le(8)), rtattr(NhaResGroupIdleTimer, u32le(10))), hw), "resilient group"},
		{"negative: a group with a family", g, flags, nhmsgBody(inet, id, members, typ, res, hw), "nhmsg"},
		{"negative: NHA_GROUP_TYPE as a u32", g, flags, nhmsgBody(g.hdr, id, members, rtattr(NhaGroupType, u32le(1)), res, hw), "is 4 bytes, want 2"},
		{"negative: hw_stats on", g, flags, nhmsgBody(g.hdr, id, members, typ, res, rtattr(wsNhaHwStatsEnable, u32le(1))), "NHA_HW_STATS_ENABLE 1"},
		{"negative: another id", g, flags, nhmsgBody(g.hdr, rtattr(NhaID, u32le(101)), members, typ, res, hw), "NHA_ID 101"},
		{"negative: NLM_F_REPLACE for an add", g, flags | unix.NLM_F_REPLACE, nhmsgBody(g.hdr, id, members, typ, res, hw), "nlmsg flags"},
		{"negative: an attribute the axes don't name", g, flags, nhmsgBody(g.hdr, id, members, typ, res, hw, rtattr(NhaFdb, nil)), "attributes"},
		{"boundary: a group entry one byte short", g, flags, nhmsgBody(g.hdr, id, rtattr(NhaGroup, make([]byte, 7)), typ, res, hw), ErrNhGroupBad.Error()},
		{"boundary: a bare nhmsg is short of what the axes name", g, flags, nhmsgBody(g.hdr), "attributes"},
		{"boundary: a body shorter than nhmsg", g, flags, []byte{0, 0, 0}, ErrNhmsgSmall.Error()},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			err := nhRequestProblem(tc.w, tc.flags, tc.body)
			if tc.expected == "" {
				if err != nil {
					t.Errorf("err %v, want none", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.expected) {
				t.Errorf("err %v, want it to contain %q", err, tc.expected)
			}
		})
	}
}

func TestNhDumpMatches(t *testing.T) {
	want := func(ax map[string]string) nhWant {
		w, err := nhWantFromAxes(ax)
		if err != nil {
			t.Fatal(err)
		}
		return w
	}
	v := want(map[string]string{"verb": "add", "family": "4", "id": "10", "oif": "d0", "gw": "192.0.2.1", "protocol": "4"})
	auto := want(map[string]string{"verb": "add", "family": "4", "id": "0", "oif": "d0", "gw": "192.0.2.1", "protocol": "4"})
	g := want(map[string]string{"verb": "add", "id": "100", "group": "21/1;22/65535", "grp_type": "1", "buckets": "8", "idle_timer": "1000"})
	vd := NexthopInfo{Family: unix.AF_INET, Protocol: 4, ID: 10, OIF: 2, Gateway: []byte{192, 0, 2, 1}}
	gd := NexthopInfo{ID: 100, HasGroup: true, Group: []GroupMember{{ID: 21, Weight: 1}, {ID: 22, Weight: 65535}},
		RespOpFlags: NhaOpFlagRespGrpResvd0, GroupType: NexthopGrpTypeRes, HasResGroup: true,
		ResGroup: ResGroup{Buckets: 8, IdleTimer: 1000, UnbalancedTimer: 0}}
	withV := func(f func(d *NexthopInfo)) NexthopInfo { d := vd; f(&d); return d }
	withG := func(f func(d *NexthopInfo)) NexthopInfo {
		d := gd
		d.Group = slices.Clone(gd.Group)
		f(&d)
		return d
	}
	cases := []struct {
		description string
		w           nhWant
		d           NexthopInfo
		expected    bool
	}{
		{"positive: the gateway nexthop as stored", v, vd, true},
		{"corner: id 0 matches the id the kernel chose", auto, withV(func(d *NexthopInfo) { d.ID = 7 }), true},
		{"negative: another id", v, withV(func(d *NexthopInfo) { d.ID = 11 }), false},
		{"negative: no device in the dump", v, withV(func(d *NexthopInfo) { d.OIF = 0 }), false},
		{"negative: another protocol", v, withV(func(d *NexthopInfo) { d.Protocol = 3 }), false},
		{"negative: onlink set though not asked", v, withV(func(d *NexthopInfo) { d.Flags = unix.RTNH_F_ONLINK }), false},
		{"positive: the resilient group as stored", g, gd, true},
		{"corner: weights past 256 are not compared without weight_high", g, withG(func(d *NexthopInfo) {
			d.RespOpFlags, d.Group[1].Weight = 0, 255
		}), true},
		{"negative: weight 255 when weight_high is filled in", g, withG(func(d *NexthopInfo) { d.Group[1].Weight = 255 }), false},
		{"negative: another member", g, withG(func(d *NexthopInfo) { d.Group[1].ID = 23 }), false},
		{"negative: another bucket count", g, withG(func(d *NexthopInfo) { d.ResGroup.Buckets = 16 }), false},
		{"corner: an unbalanced timer not named is not compared", g, withG(func(d *NexthopInfo) { d.ResGroup.UnbalancedTimer = 500 }), true},
		{"negative: an mpath group", g, withG(func(d *NexthopInfo) { d.GroupType, d.HasResGroup = NexthopGrpTypeMpath, false }), false},
		{"boundary: a dumped nexthop with nothing set", v, NexthopInfo{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			if got := nhDumpMatches(tc.w, tc.d); got != tc.expected {
				t.Errorf("got %v, want %v", got, tc.expected)
			}
		})
	}
}
