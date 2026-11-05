package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// The ip rule rows' oracles (coverage-status.md, "Grammar matrix (W2)").
// O3 rebuilds the RuleInfo the request should parse to from the row's
// axes alone (tools/nlcapgen/matrix_rule.go), following ip/iprule.c
// iprule_modify, and checks the request's attribute set and widths. O4
// looks for the rule in the dump read back after it.

// ruleWant is the request a row's axes call for.
type ruleWant struct {
	typ, flags uint16
	info       RuleInfo
	attrs      []uint16 // every FRA_* the request carries, sorted
}

// ruleAttrWidths is the payload length each fixed-width FRA_* must have.
var ruleAttrWidths = map[uint16]int{
	unix.FRA_PRIORITY: 4, unix.FRA_FWMARK: 4, unix.FRA_FWMASK: 4, unix.FRA_TABLE: 4,
	unix.FRA_SUPPRESS_PREFIXLEN: 4, unix.FRA_SUPPRESS_IFGROUP: 4, unix.FRA_FLOW: 4, unix.FRA_GOTO: 4,
	unix.FRA_L3MDEV: 1, unix.FRA_IP_PROTO: 1, unix.FRA_PROTOCOL: 1, FraDscp: 1, FraDscpMask: 1,
	unix.FRA_SPORT_RANGE: FibRulePortRangeSizeCst, unix.FRA_DPORT_RANGE: FibRulePortRangeSizeCst,
	FraSportMask: 2, FraDportMask: 2, unix.FRA_UID_RANGE: FibRuleUidRangeSizeCst, unix.FRA_TUN_ID: 8,
	FraFlowlabel: 4, FraFlowlabelMask: 4,
}

// axRange reads an axis "start-end" pair.
func axRange(ax map[string]string, k string, bits int) (lo, hi uint64, ok bool, err error) {
	s, ok := ax[k]
	if !ok {
		return 0, 0, false, nil
	}
	a, b, cut := strings.Cut(s, "-")
	lo, err1 := strconv.ParseUint(a, 10, bits)
	hi, err2 := strconv.ParseUint(b, 10, bits)
	if !cut || err1 != nil || err2 != nil {
		return 0, 0, false, fmt.Errorf("axis %s=%q is not a %d-bit range", k, s, bits)
	}
	return lo, hi, true, nil
}

// ruleWantAddrs sets the selector prefixes. "all" sends an empty FRA_SRC
// or FRA_DST: __get_addr_1 gives all and any a bytelen of 0, where it
// gives default the family's.
func ruleWantAddrs(ax map[string]string, w *ruleWant) error {
	alen := 4
	if w.info.Family == unix.AF_INET6 {
		alen = 16
	}
	for _, k := range []string{"src", "dst"} {
		s, ok := ax[k]
		if !ok {
			continue
		}
		var b []byte
		switch s {
		case "default":
			b = make([]byte, alen)
		case "all":
		default:
			var err error
			if b, err = axAddr(ax, k); err != nil {
				return err
			}
		}
		n, _, err := axU8(ax, k+"_len")
		if err != nil {
			return err
		}
		if k == "src" {
			w.info.Src, w.info.SrcLen = b, n
			w.attrs = append(w.attrs, unix.FRA_SRC)
		} else {
			w.info.Dst, w.info.DstLen = b, n
			w.attrs = append(w.attrs, unix.FRA_DST)
		}
	}
	return nil
}

// ruleU32s are the u32 axes and where each lands.
var ruleU32s = []struct {
	axis string
	attr uint16
	set  func(ri *RuleInfo, v uint32)
}{
	{"priority", unix.FRA_PRIORITY, func(ri *RuleInfo, v uint32) { ri.Priority, ri.HasPriority = v, true }},
	{"fwmark", unix.FRA_FWMARK, func(ri *RuleInfo, v uint32) { ri.Fwmark, ri.HasFwmark = v, true }},
	{"fwmask", unix.FRA_FWMASK, func(ri *RuleInfo, v uint32) { ri.Fwmask, ri.HasFwmask = v, true }},
	{"sup_pl", unix.FRA_SUPPRESS_PREFIXLEN, func(ri *RuleInfo, v uint32) { ri.SuppressPrefixlen, ri.HasSuppressPrefixlen = v, true }},
	{"sup_group", unix.FRA_SUPPRESS_IFGROUP, func(ri *RuleInfo, v uint32) { ri.SuppressIfgroup, ri.HasSuppressIfgroup = v, true }},
	{"flow", unix.FRA_FLOW, func(ri *RuleInfo, v uint32) { ri.Flow, ri.HasFlow = v, true }},
	{"goto", unix.FRA_GOTO, func(ri *RuleInfo, v uint32) { ri.Goto, ri.HasGoto = v, true }},
	{"flowlabel", FraFlowlabel, func(ri *RuleInfo, v uint32) { ri.Flowlabel, ri.HasFlowlabel = v, true }},
	{"flowlabel_mask", FraFlowlabelMask, func(ri *RuleInfo, v uint32) { ri.FlowlabelMask, ri.HasFlowlabelMask = v, true }},
}

// ruleU8s are the u8 axes and where each lands.
var ruleU8s = []struct {
	axis string
	attr uint16
	set  func(ri *RuleInfo, v uint8)
}{
	{"l3mdev", unix.FRA_L3MDEV, func(ri *RuleInfo, v uint8) { ri.L3mdev, ri.HasL3mdev = v, true }},
	{"ipproto", unix.FRA_IP_PROTO, func(ri *RuleInfo, v uint8) { ri.IPProto, ri.HasIPProto = v, true }},
	{"protocol", unix.FRA_PROTOCOL, func(ri *RuleInfo, v uint8) { ri.Protocol, ri.HasProtocol = v, true }},
	{"dscp", FraDscp, func(ri *RuleInfo, v uint8) { ri.Dscp, ri.HasDscp = v, true }},
	{"dscp_mask", FraDscpMask, func(ri *RuleInfo, v uint8) { ri.DscpMask, ri.HasDscpMask = v, true }},
}

func ruleWantScalars(ax map[string]string, w *ruleWant) error {
	for _, s := range ruleU32s {
		n, ok, err := axU32(ax, s.axis)
		if err != nil {
			return err
		}
		if ok {
			s.set(&w.info, n)
			w.attrs = append(w.attrs, s.attr)
		}
	}
	for _, s := range ruleU8s {
		n, ok, err := axU8(ax, s.axis)
		if err != nil {
			return err
		}
		if ok {
			s.set(&w.info, n)
			w.attrs = append(w.attrs, s.attr)
		}
	}
	if s, ok := ax["tun_id"]; ok {
		n, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return fmt.Errorf("axis tun_id=%q: %w", s, err)
		}
		w.info.TunID, w.info.HasTunID = n, true
		w.attrs = append(w.attrs, unix.FRA_TUN_ID)
	}
	return nil
}

// ruleWantRanges sets the port and uid ranges and the port masks.
func ruleWantRanges(ax map[string]string, w *ruleWant) error {
	for _, p := range []struct {
		axis       string
		attr, mask uint16
		r          *FibRulePortRange
		has        *bool
		m          *uint16
		hasM       *bool
	}{
		{"sport", unix.FRA_SPORT_RANGE, FraSportMask, &w.info.SportRange, &w.info.HasSportRange, &w.info.SportMask, &w.info.HasSportMask},
		{"dport", unix.FRA_DPORT_RANGE, FraDportMask, &w.info.DportRange, &w.info.HasDportRange, &w.info.DportMask, &w.info.HasDportMask},
	} {
		lo, hi, ok, err := axRange(ax, p.axis, 16)
		if err != nil || !ok {
			if err != nil {
				return err
			}
			continue
		}
		*p.r, *p.has = FibRulePortRange{Start: uint16(lo), End: uint16(hi)}, true
		w.attrs = append(w.attrs, p.attr)
		if m, ok, err := axU32(ax, p.axis+"_mask"); err != nil || ok {
			if err != nil || m > 0xffff {
				return fmt.Errorf("axis %s_mask=%d: %w", p.axis, m, err)
			}
			*p.m, *p.hasM = uint16(m), true
			w.attrs = append(w.attrs, p.mask)
		}
	}
	lo, hi, ok, err := axRange(ax, "uidrange", 32)
	if ok {
		w.info.UidRange, w.info.HasUidRange = FibRuleUidRange{Start: uint32(lo), End: uint32(hi)}, true
		w.attrs = append(w.attrs, unix.FRA_UID_RANGE)
	}
	return err
}

// ruleWantFromAxes models iprule_modify: an add asks NLM_F_CREATE|EXCL; a
// table below 256 goes in the header, one above in FRA_TABLE.
func ruleWantFromAxes(ax map[string]string) (ruleWant, error) {
	var w ruleWant
	switch ax["verb"] {
	case "add":
		w.typ, w.flags = unix.RTM_NEWRULE, unix.NLM_F_CREATE|unix.NLM_F_EXCL
	case "del":
		w.typ = unix.RTM_DELRULE
	default:
		return w, fmt.Errorf("axis verb=%q", ax["verb"])
	}
	switch ax["family"] {
	case "4":
		w.info.Family = unix.AF_INET
	case "6":
		w.info.Family = unix.AF_INET6
	default:
		return w, fmt.Errorf("axis family=%q", ax["family"])
	}
	if ax["invert"] == "1" {
		w.info.Flags = unix.FIB_RULE_INVERT
	}
	tos, _, err := axU8(ax, "tos")
	action, _, err2 := axU8(ax, "action")
	table, _, err3 := axU32(ax, "table")
	if err := errors.Join(err, err2, err3); err != nil {
		return w, err
	}
	if _, typed := ax["action"]; !typed && w.typ == unix.RTM_NEWRULE {
		action = unix.FR_ACT_TO_TBL
	}
	w.info.Tos, w.info.Action, w.info.Table = tos, action, table
	if table < 256 {
		w.info.RawTable = uint8(table)
	} else {
		w.attrs = append(w.attrs, unix.FRA_TABLE)
	}
	for _, n := range []struct {
		axis string
		attr uint16
		name *string
		has  *bool
	}{
		{"iif", unix.FRA_IIFNAME, &w.info.IifName, &w.info.HasIifName},
		{"oif", unix.FRA_OIFNAME, &w.info.OifName, &w.info.HasOifName},
	} {
		if s, ok := ax[n.axis]; ok {
			*n.name, *n.has = s, true
			w.attrs = append(w.attrs, n.attr)
		}
	}
	for _, step := range []func(map[string]string, *ruleWant) error{ruleWantAddrs, ruleWantScalars, ruleWantRanges} {
		if err := step(ax, &w); err != nil {
			return w, err
		}
	}
	slices.Sort(w.attrs)
	return w, nil
}

// ruleAttrWidth is the length attribute a must have in w, or -1 if any.
func ruleAttrWidth(w ruleWant, a uint16) int {
	switch a {
	case unix.FRA_SRC:
		return len(w.info.Src)
	case unix.FRA_DST:
		return len(w.info.Dst)
	case unix.FRA_IIFNAME:
		return len(w.info.IifName) + 1
	case unix.FRA_OIFNAME:
		return len(w.info.OifName) + 1
	}
	if n, ok := ruleAttrWidths[a]; ok {
		return n
	}
	return -1
}

// ruleRequestProblem is O3: how a captured request differs from the
// model's. body is the message after its nlmsghdr.
func ruleRequestProblem(w ruleWant, flags uint16, body []byte) error {
	var errs []error
	if got := flags &^ (unix.NLM_F_REQUEST | unix.NLM_F_ACK); got != w.flags {
		errs = append(errs, fmt.Errorf("nlmsg flags %#x, want %#x", got, w.flags))
	}
	if len(body) < FibRuleHdrSizeCst {
		return ErrFibRuleHdrSmall
	}
	var attrs []uint16
	err := WalkRTAttrs(body[FibRuleHdrSizeCst:], func(a uint16, val []byte) {
		attrs = append(attrs, a)
		if n := ruleAttrWidth(w, a); n >= 0 && len(val) != n {
			errs = append(errs, fmt.Errorf("attribute %d is %d bytes, want %d", a, len(val), n))
		}
	})
	if err != nil {
		return err
	}
	slices.Sort(attrs)
	if !slices.Equal(attrs, w.attrs) {
		errs = append(errs, fmt.Errorf("attributes %v, want %v", attrs, w.attrs))
	}
	ri, err := ParseRule(body)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(ri, w.info) {
		errs = append(errs, fmt.Errorf("rule %+v\n  want %+v", ri, w.info))
	}
	return errors.Join(errs...)
}

// ruleDumpMatches reports whether a dumped rule holds every selector the
// request named. The kernel reports what it stored, so it leaves out the
// DS field (fib4 folds tos into dscp) and a zero fwmark, and gives an
// unnamed table and preference values of its own.
func ruleDumpMatches(w RuleInfo, d RuleInfo) bool {
	same := d.Family == w.Family && d.SrcLen == w.SrcLen && d.DstLen == w.DstLen &&
		d.Flags&unix.FIB_RULE_INVERT == w.Flags&unix.FIB_RULE_INVERT &&
		(w.SrcLen == 0 || slices.Equal(d.Src, w.Src)) && (w.DstLen == 0 || slices.Equal(d.Dst, w.Dst)) &&
		(!w.HasPriority || d.Priority == w.Priority) &&
		(w.Fwmark == 0 || d.Fwmark == w.Fwmark) &&
		d.IifName == w.IifName && d.OifName == w.OifName &&
		(w.Action == 0 || d.Action == w.Action) && (w.Table == 0 || d.Table == w.Table) &&
		d.Goto == w.Goto && d.L3mdev == w.L3mdev && d.IPProto == w.IPProto &&
		d.SportRange == w.SportRange && d.DportRange == w.DportRange &&
		d.TunID == w.TunID && d.Flowlabel == w.Flowlabel
	return same && (!w.HasUidRange || d.UidRange == w.UidRange) && (!w.HasProtocol || d.Protocol == w.Protocol)
}

// wsRuleDump returns the rules a readback's dump replied with.
func wsRuleDump(t *testing.T, msgs []capturedNlMsg) []RuleInfo {
	t.Helper()
	var out []RuleInfo
	for _, m := range msgs {
		if m.proto != unix.NETLINK_ROUTE || m.h.Type != unix.RTM_NEWRULE || m.h.Flags&unix.NLM_F_REQUEST != 0 {
			continue
		}
		ri, err := ParseRule(m.body)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, ri)
	}
	return out
}

// TestWsRuleCorpus runs O3 and O4 over every generated rule row, and
// fails if O3 checked nothing.
func TestWsRuleCorpus(t *testing.T) {
	var checked, roundTrips int
	for _, dir := range wsRows(t) {
		row := wsLoadRow(t, dir)
		if !strings.HasPrefix(row.ID, "rule4/") && !strings.HasPrefix(row.ID, "rule6/") {
			continue
		}
		t.Run(row.Env.Kernel+"/"+row.ID, func(t *testing.T) {
			if row.Result.Status != "OK" {
				return // TestWsCorpusRows fails it
			}
			c, rt := wsRuleRow(t, dir, row)
			checked += c
			roundTrips += rt
		})
	}
	t.Logf("O3 checked %d rule requests, O4 %d round trips", checked, roundTrips)
	if checked == 0 {
		t.Error("O3 checked no rule request: are there rule rows under testdata/*/ws?")
	}
}

func wsRuleRow(t *testing.T, dir string, row wsRow) (checked, roundTrips int) {
	t.Helper()
	w, err := ruleWantFromAxes(row.Axes)
	if err != nil {
		t.Fatalf("O3 model: %v", err)
	}
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
	if err := ruleRequestProblem(w, reqs[0].h.Flags, reqs[0].body); err != nil {
		t.Errorf("O3: %v\n  cmd: %s", err, row.Cmd)
	}
	if rc != 0 {
		return 1, 0
	}
	dump := wsRuleDump(t, pcapNlMsgs(t, filepath.Join(dir, "netlink_route_dump.pcap")))
	has := slices.ContainsFunc(dump, func(d RuleInfo) bool { return ruleDumpMatches(w.info, d) })
	if has == (w.typ == unix.RTM_DELRULE) {
		t.Errorf("O4: after %s the dump's %d rules hold it: %v\n  cmd: %s", row.Axes["verb"], len(dump), has, row.Cmd)
	}
	return 1, 1
}

// fibRuleBody is a fib_rule_hdr built from ri, then attrs.
func fibRuleBody(ri RuleInfo, attrs ...[]byte) []byte {
	h := []byte{ri.Family, ri.DstLen, ri.SrcLen, ri.Tos, ri.RawTable, 0, 0, ri.Action}
	return cat(append([][]byte{h, u32le(ri.Flags)}, attrs...)...)
}

// portRange is a fib_rule_port_range's bytes.
func portRange(lo, hi uint16) []byte {
	return binary.LittleEndian.AppendUint16(binary.LittleEndian.AppendUint16(nil, lo), hi)
}

func TestRuleWantFromAxes(t *testing.T) {
	base := map[string]string{"verb": "add", "family": "4", "src": "192.0.2.0", "src_len": "24", "table": "100"}
	with := func(kv ...string) map[string]string {
		m := maps.Clone(base)
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
		expected    func(w ruleWant) bool
		wantErr     bool
	}{
		{"positive: from a prefix lookup 100", base, func(w ruleWant) bool {
			return w.typ == unix.RTM_NEWRULE && w.flags == unix.NLM_F_CREATE|unix.NLM_F_EXCL && w.info.RawTable == 100 &&
				w.info.Action == unix.FR_ACT_TO_TBL && slices.Equal(w.attrs, []uint16{unix.FRA_SRC})
		}, false},
		{"boundary: table 255 fits the header", with("table", "255"), func(w ruleWant) bool {
			return w.info.RawTable == 255 && !slices.Contains(w.attrs, unix.FRA_TABLE)
		}, false},
		{"boundary: table 256 goes in FRA_TABLE, the header 0", with("table", "256"), func(w ruleWant) bool {
			return w.info.RawTable == 0 && w.info.Table == 256 && slices.Contains(w.attrs, unix.FRA_TABLE)
		}, false},
		{"corner: from all sends an empty FRA_SRC", with("src", "all", "src_len", "0"), func(w ruleWant) bool {
			return w.info.Src == nil && slices.Contains(w.attrs, unix.FRA_SRC) && ruleAttrWidth(w, unix.FRA_SRC) == 0
		}, false},
		{"corner: -6 from default sends sixteen zero bytes", with("family", "6", "src", "default", "src_len", "0"), func(w ruleWant) bool {
			return bytes.Equal(w.info.Src, make([]byte, 16))
		}, false},
		{"positive: a typed action is kept", with("table", "", "action", "6"), func(w ruleWant) bool {
			return w.info.Action == unix.FR_ACT_BLACKHOLE && w.info.Table == 0
		}, false},
		{"positive: a delete names no action and asks no flags", with("verb", "del"), func(w ruleWant) bool {
			return w.typ == unix.RTM_DELRULE && w.flags == 0 && w.info.Action == 0
		}, false},
		{"positive: a port range and its mask", with("sport", "80-80", "sport_mask", "65520"), func(w ruleWant) bool {
			return w.info.SportRange == FibRulePortRange{Start: 80, End: 80} && w.info.SportMask == 0xfff0 &&
				slices.Contains(w.attrs, unix.FRA_SPORT_RANGE) && slices.Contains(w.attrs, FraSportMask)
		}, false},
		{"positive: an iif name is sent with its NUL", with("iif", "d0"), func(w ruleWant) bool {
			return w.info.IifName == "d0" && ruleAttrWidth(w, unix.FRA_IIFNAME) == 3
		}, false},
		{"boundary: uid range 0-4294967295", with("uidrange", "0-4294967295"), func(w ruleWant) bool {
			return w.info.UidRange == FibRuleUidRange{Start: 0, End: 4294967295}
		}, false},
		{"positive: invert sets FIB_RULE_INVERT", with("invert", "1"), func(w ruleWant) bool {
			return w.info.Flags == unix.FIB_RULE_INVERT
		}, false},
		{"negative: an unknown verb", with("verb", "flush"), nil, true},
		{"negative: no family", with("family", ""), nil, true},
		{"negative: a port that is not a range", with("sport", "80"), nil, true},
		{"negative: a port mask past u16", with("sport", "80-80", "sport_mask", "65536"), nil, true},
		{"negative: a dscp past u8", with("dscp", "256"), nil, true},
		{"negative: a table past u32", with("table", "4294967296"), nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			w, err := ruleWantFromAxes(tc.axes)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err %v, want error %v", err, tc.wantErr)
			}
			if !tc.wantErr && !tc.expected(w) {
				t.Errorf("want %+v", w)
			}
		})
	}
}

func TestRuleRequestProblem(t *testing.T) {
	want := func(ax map[string]string) ruleWant {
		w, err := ruleWantFromAxes(ax)
		if err != nil {
			t.Fatal(err)
		}
		return w
	}
	w := want(map[string]string{"verb": "add", "family": "4", "src": "192.0.2.0", "src_len": "24",
		"table": "256", "priority": "100", "iif": "d0", "sport": "1000-2000"})
	all := want(map[string]string{"verb": "add", "family": "4", "src": "all", "src_len": "0", "table": "254"})
	src := rtattr(unix.FRA_SRC, []byte{192, 0, 2, 0})
	iif := rtattr(unix.FRA_IIFNAME, []byte("d0\x00"))
	prio := rtattr(unix.FRA_PRIORITY, u32le(100))
	table := rtattr(unix.FRA_TABLE, u32le(256))
	sport := rtattr(unix.FRA_SPORT_RANGE, portRange(1000, 2000))
	flags := uint16(unix.NLM_F_REQUEST | unix.NLM_F_ACK | unix.NLM_F_CREATE | unix.NLM_F_EXCL)
	hdrTable := w.info
	hdrTable.RawTable = 1
	cases := []struct {
		description string
		w           ruleWant
		flags       uint16
		body        []byte
		expected    string // in the error; "" for none
	}{
		{"positive: the request the axes call for", w, flags, fibRuleBody(w.info, src, iif, prio, table, sport), ""},
		{"positive: attribute order does not matter", w, flags, fibRuleBody(w.info, sport, table, prio, iif, src), ""},
		{"corner: from all is an empty FRA_SRC", all, flags, fibRuleBody(all.info, rtattr(unix.FRA_SRC, nil)), ""},
		{"negative: from all sent four bytes", all, flags, fibRuleBody(all.info, rtattr(unix.FRA_SRC, make([]byte, 4))), "is 4 bytes, want 0"},
		{"negative: FRA_TABLE two bytes wide", w, flags, fibRuleBody(w.info, src, iif, prio, rtattr(unix.FRA_TABLE, []byte{0, 1}), sport), "is 2 bytes, want 4"},
		{"negative: a large table also in the header", w, flags, fibRuleBody(hdrTable, src, iif, prio, table, sport), "rule"},
		{"negative: an iif name without its NUL", w, flags, fibRuleBody(w.info, src, rtattr(unix.FRA_IIFNAME, []byte("d0")), prio, table, sport), "is 2 bytes, want 3"},
		{"negative: the priority missing", w, flags, fibRuleBody(w.info, src, iif, table, sport), "attributes"},
		{"negative: NLM_F_REPLACE for an add", w, flags | unix.NLM_F_REPLACE, fibRuleBody(w.info, src, iif, prio, table, sport), "nlmsg flags"},
		{"negative: the port range reversed", w, flags, fibRuleBody(w.info, src, iif, prio, table, rtattr(unix.FRA_SPORT_RANGE, portRange(2000, 1000))), "rule"},
		{"boundary: a bare header is short of what the axes name", w, flags, fibRuleBody(w.info), "attributes"},
		{"boundary: a body shorter than fib_rule_hdr", w, flags, []byte{2, 0, 24}, ErrFibRuleHdrSmall.Error()},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			err := ruleRequestProblem(tc.w, tc.flags, tc.body)
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

func TestRuleDumpMatches(t *testing.T) {
	w := RuleInfo{Family: unix.AF_INET, Priority: 100, HasPriority: true, Fwmark: 16, Action: unix.FR_ACT_TO_TBL, Table: 100,
		UidRange: FibRuleUidRange{Start: 1000, End: 2000}, HasUidRange: true}
	d := w
	with := func(f func(r *RuleInfo)) RuleInfo { r := d; f(&r); return r }
	cases := []struct {
		description string
		w, d        RuleInfo
		expected    bool
	}{
		{"positive: the rule as stored", w, d, true},
		{"corner: a pref the kernel chose when none was named", with(func(r *RuleInfo) { r.HasPriority, r.Priority = false, 0 }), with(func(r *RuleInfo) { r.Priority = 32765 }), true},
		{"corner: tos is not compared (fib4 folds it into dscp)", with(func(r *RuleInfo) { r.Tos = 0x10 }), d, true},
		{"corner: a zero fwmark is not dumped", with(func(r *RuleInfo) { r.Fwmark = 0 }), with(func(r *RuleInfo) { r.Fwmark = 0 }), true},
		{"negative: another table", w, with(func(r *RuleInfo) { r.Table = 254 }), false},
		{"negative: invert differs", w, with(func(r *RuleInfo) { r.Flags = unix.FIB_RULE_INVERT }), false},
		{"negative: another uid range", w, with(func(r *RuleInfo) { r.UidRange.End = 2001 }), false},
		{"negative: another family", w, with(func(r *RuleInfo) { r.Family = unix.AF_INET6 }), false},
		{"boundary: a dumped rule with nothing set", w, RuleInfo{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			if got := ruleDumpMatches(tc.w, tc.d); got != tc.expected {
				t.Errorf("got %v, want %v", got, tc.expected)
			}
		})
	}
}
