package goip

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// O5, the goip differential (docs/netlink/coverage-status.md, "Grammar
// matrix (W2)"): every workbench row of ip route, rule and nexthop is run
// through goip over the row's own capture, and goip's requests are compared
// with ip's. The lookups are answered from the capture, so a dev resolves to
// the ifindex the kernel gave ip, and the write is taken from
// GOIP_WRITE_OUT, never sent.
//
// Every row gets one outcome for its write, and every outcome but "same" is
// listed in testdata/ws-differential.txt, with a count of the rows whose
// lookups differ and how. A new divergence and a fixed one both fail until
// the file is regenerated:
//
//	go test ./internal/goip/ -run TestWsGoipDifferential -update-ws-o5

var updateWsO5 = flag.Bool("update-ws-o5", false, "rewrite testdata/ws-differential.txt")

// wsCorpusDir is the workbench's corpus for the guest kernel the rows ran on.
const wsCorpusDir = "../../pkg/xtcpnl/testdata/7_3_0/ws"

// wsO5Golden lists every row whose outcome is not "same".
const wsO5Golden = "testdata/ws-differential.txt"

// wsO5Spaces are the row directories O5 reads, in report order: the
// matrix's, then those mined from the fib selftests.
var wsO5Spaces = []string{"route4", "route6", "rule4", "rule6", "nh4", "nh6", "nhgrp", "fibt", "fibnh", "fibrule"}

// wsO5Verbs are the verbs whose request O5 compares, as ip spells them in
// full. A mined row's get, show, flush or bucket is a dump or a lookup, not
// a write, so O5 does not apply to it; such rows are counted.
var wsO5Verbs = []string{"add", "change", "replace", "prepend", "append", "del", "delete"}

// The outcomes, as the report spells them.
const (
	o5Same    = "same"    // the same lookups and the same write, or the same refusal after them
	o5Refuses = "refuses" // goip refuses a command ip sent, or one it does not implement
	o5Sends   = "sends"   // goip writes a request ip refused to send
	o5Differs = "differs" // both wrote, and the writes differ
)

// wsRowFile is the part of a row's cmd.json O5 reads.
type wsRowFile struct {
	ID     string `json:"id"`
	Cmd    string `json:"cmd"`
	Result struct {
		Status string `json:"status"`
		RC     int    `json:"rc"`
	} `json:"result"`
}

// wsO5Argv splits a row's command into goip's family, object and args,
// the object as goip resolves an abbreviation of it. The rows are written
// without quoting, so a split on spaces is exact.
func wsO5Argv(cmd string) (family uint8, object string, args []string, err error) {
	f := strings.Fields(cmd)
	if len(f) < 2 || f[0] != "ip" {
		return 0, "", nil, fmt.Errorf("not an ip command: %q", cmd)
	}
	f = f[1:]
	for len(f) > 0 && strings.HasPrefix(f[0], "-") {
		switch f[0] {
		case "-4":
			family = unix.AF_INET
		case "-6":
			family = unix.AF_INET6
		default:
			return 0, "", nil, fmt.Errorf("option %s in %q", f[0], cmd)
		}
		f = f[1:]
	}
	if len(f) == 0 {
		return 0, "", nil, fmt.Errorf("no object in %q", cmd)
	}
	i := objectIndex(f[0])
	if i < 0 {
		return 0, "", nil, fmt.Errorf("unknown object %s in %q", f[0], cmd)
	}
	return family, objects[i].name, f[1:], nil
}

// wsO5Applies says whether O5 compares a row's args: a write verb, spelled
// in full as the mined rows and the matrix's spell them.
func wsO5Applies(args []string) bool {
	return len(args) > 0 && slices.Contains(wsO5Verbs, args[0])
}

// wsO5Runners runs one object's args, as runRouteCtx does for route.
var wsO5Runners = map[string]func(*runCtx, []string) (int, string, string){
	"route":   runRouteCtx,
	"rule":    runRuleCtx,
	"nexthop": runNexthopCtx,
}

// wsO5HdrLen is each write's family header, the attributes following it.
func wsO5HdrLen(msgType uint16) int {
	switch msgType {
	case unix.RTM_NEWROUTE, unix.RTM_DELROUTE:
		return xtcpnl.RtMsgSizeCst
	case unix.RTM_NEWRULE, unix.RTM_DELRULE:
		return xtcpnl.FibRuleHdrSizeCst
	case unix.RTM_NEWNEXTHOP, unix.RTM_DELNEXTHOP:
		return xtcpnl.NhMsgSizeCst
	default:
		return -1
	}
}

// wsO5ZeroMetricsPad is a route write with the alignment padding after each
// RTA_METRICS sub-attribute zeroed, and whether any of it wasn't zero.
// iproute2 builds the metrics in an uninitialized stack buffer (mxbuf,
// ip/iproute.c:1172), and rta_addattr_l doesn't clear the padding
// (lib/libnetlink.c:1500-1505), so a congctl name of a length not a
// multiple of 4 is followed by stack bytes. goip writes zeros.
func wsO5ZeroMetricsPad(msg []byte) ([]byte, bool) {
	if len(msg) < xtcpnl.NlMsgHdrSizeCst+xtcpnl.RtMsgSizeCst {
		return msg, false
	}
	if t := binary.LittleEndian.Uint16(msg[4:6]); t != unix.RTM_NEWROUTE && t != unix.RTM_DELROUTE {
		return msg, false
	}
	out := slices.Clone(msg)
	zeroed := false
	for off := xtcpnl.NlMsgHdrSizeCst + xtcpnl.RtMsgSizeCst; off+4 <= len(out); {
		l := int(binary.LittleEndian.Uint16(out[off:]))
		if l < 4 || off+l > len(out) {
			break
		}
		if binary.LittleEndian.Uint16(out[off+2:]) == unix.RTA_METRICS {
			for sub := off + 4; sub+4 <= off+l; {
				sl := int(binary.LittleEndian.Uint16(out[sub:]))
				if sl < 4 || sub+sl > off+l {
					break
				}
				for i := sub + sl; i < min(sub+(sl+3)&^3, off+l); i++ {
					zeroed = zeroed || out[i] != 0
					out[i] = 0
				}
				sub += (sl + 3) &^ 3
			}
		}
		off += (l + 3) &^ 3
	}
	return out, zeroed
}

// wsO5Attrs is a write's top-level attribute values by type.
func wsO5Attrs(msg []byte) (hdr []byte, attrs map[uint16][]byte) {
	attrs = map[uint16][]byte{}
	if len(msg) < xtcpnl.NlMsgHdrSizeCst {
		return nil, attrs
	}
	body := msg[xtcpnl.NlMsgHdrSizeCst:]
	n := wsO5HdrLen(binary.LittleEndian.Uint16(msg[4:6]))
	if n < 0 || len(body) < n {
		return body, attrs
	}
	if err := xtcpnl.WalkRTAttrs(body[n:], func(a uint16, v []byte) { attrs[a] = v }); err != nil {
		attrs[0xffff] = []byte(err.Error())
	}
	return body[:n], attrs
}

// wsO5WriteDiff says how goip's write differs from ip's: the netlink
// header's type or flags, the family header, then the attributes only one
// sends and those both send with different values.
func wsO5WriteDiff(ip, goip []byte) string {
	if len(ip) < xtcpnl.NlMsgHdrSizeCst || len(goip) < xtcpnl.NlMsgHdrSizeCst {
		return fmt.Sprintf("lengths %d and %d", len(ip), len(goip))
	}
	var parts []string
	if !bytes.Equal(ip[4:8], goip[4:8]) {
		parts = append(parts, fmt.Sprintf("nlmsg type/flags %x, goip %x", ip[4:8], goip[4:8]))
	}
	ih, ia := wsO5Attrs(ip)
	gh, ga := wsO5Attrs(goip)
	if !bytes.Equal(ih, gh) {
		parts = append(parts, fmt.Sprintf("header %x, goip %x", ih, gh))
	}
	var only, gonly, vals []uint16
	for a, v := range ia {
		g, ok := ga[a]
		switch {
		case !ok:
			only = append(only, a)
		case !bytes.Equal(v, g):
			vals = append(vals, a)
		}
	}
	for a := range ga {
		if _, ok := ia[a]; !ok {
			gonly = append(gonly, a)
		}
	}
	for _, s := range []struct {
		name string
		as   []uint16
	}{{"attrs only ip sends", only}, {"attrs only goip sends", gonly}, {"attr values differ", vals}} {
		if len(s.as) > 0 {
			slices.Sort(s.as)
			parts = append(parts, fmt.Sprintf("%s %v", s.name, s.as))
		}
	}
	if len(parts) == 0 {
		return "the same attributes, in another order or padding"
	}
	return strings.Join(parts, "; ")
}

// wsO5Lookups compares goip's lookups with ip's: the RTM_GETLINK requests
// ip sent before anything else. It returns "" when they match, and
// otherwise how they differ; a difference only in IFLA_EXT_MASK says which
// masks, since iproute2 7.2.0 added RTEXT_FILTER_NAME_ONLY to ll_link_get
// (lib/ll_map.c:276) after goip modeled 7.1's.
func wsO5Lookups(ipLook, gets [][]byte) string {
	if len(ipLook) != len(gets) {
		return fmt.Sprintf("goip sent %d lookups, ip %d", len(gets), len(ipLook))
	}
	masks := map[string]bool{}
	for i := range gets {
		if bytes.Equal(ipLook[i], gets[i]) {
			continue
		}
		ih, ia := wsO5LinkAttrs(ipLook[i])
		gh, ga := wsO5LinkAttrs(gets[i])
		im, gm := ia[unix.IFLA_EXT_MASK], ga[unix.IFLA_EXT_MASK]
		delete(ia, unix.IFLA_EXT_MASK)
		delete(ga, unix.IFLA_EXT_MASK)
		if !bytes.Equal(ih, gh) || len(im) != 4 || len(gm) != 4 || !maps.EqualFunc(ia, ga, bytes.Equal) {
			return fmt.Sprintf("lookup %d differs", i+1)
		}
		masks[fmt.Sprintf("IFLA_EXT_MASK %#x, goip %#x", binary.LittleEndian.Uint32(im), binary.LittleEndian.Uint32(gm))] = true
	}
	return strings.Join(slices.Sorted(maps.Keys(masks)), "; ")
}

// wsO5LinkAttrs is an RTM_GETLINK's ifinfomsg and attributes.
func wsO5LinkAttrs(msg []byte) (hdr []byte, attrs map[uint16][]byte) {
	attrs = map[uint16][]byte{}
	body := msg[xtcpnl.NlMsgHdrSizeCst:]
	if len(body) < unix.SizeofIfInfomsg {
		return body, attrs
	}
	if err := xtcpnl.WalkRTAttrs(body[unix.SizeofIfInfomsg:], func(a uint16, v []byte) { attrs[a] = v }); err != nil {
		attrs[0xffff] = []byte(err.Error())
	}
	return body[:unix.SizeofIfInfomsg], attrs
}

// wsO5Compare classifies one row's write from ip's requests after its
// lookups, goip's writes, and goip's error text ("" when it exited 0).
func wsO5Compare(ipWrites, writes [][]byte, goipErr string) (outcome, detail string) {
	switch {
	case len(ipWrites) == 0 && len(writes) == 0 && strings.Contains(goipErr, "not implemented"):
		// Both refuse, but goip never reached the check ip failed.
		return o5Refuses, goipErr
	case len(ipWrites) == 0 && len(writes) == 0:
		return o5Same, ""
	case len(writes) == 0:
		return o5Refuses, goipErr
	case len(ipWrites) == 0:
		return o5Sends, "ip sent nothing after its lookups"
	case len(ipWrites) != len(writes):
		return o5Differs, fmt.Sprintf("ip wrote %d messages, goip %d", len(ipWrites), len(writes))
	}
	for i := range writes {
		if !bytes.Equal(ipWrites[i], writes[i]) {
			return o5Differs, wsO5WriteDiff(ipWrites[i], writes[i])
		}
	}
	return o5Same, ""
}

// wsO5Split splits ip's requests into its leading RTM_GETLINK lookups and
// the rest.
func wsO5Split(ipReqs [][]byte) (look, rest [][]byte) {
	n := 0
	for n < len(ipReqs) && len(ipReqs[n]) >= xtcpnl.NlMsgHdrSizeCst &&
		binary.LittleEndian.Uint16(ipReqs[n][4:6]) == unix.RTM_GETLINK {
		n++
	}
	return ipReqs[:n], ipReqs[n:]
}

// wsO5Row runs one row through goip and classifies it.
func wsO5Row(t *testing.T, dir string, row wsRowFile) (outcome, detail, lookups string, pad bool) {
	t.Helper()
	family, object, args, err := wsO5Argv(row.Cmd)
	if err != nil {
		t.Fatal(err)
	}
	run, ok := wsO5Runners[object]
	if !ok {
		t.Fatalf("%s: no goip runner for %q", row.ID, object)
	}
	pcap := filepath.Join(dir, "ip.pcap")
	ipReqs := captureRequests(t, pcap)
	out := filepath.Join(t.TempDir(), "write.bin")
	t.Setenv(writeOutEnvCst, out)
	src := &ruleReplay{restoreReplay: &restoreReplay{linkReplay: newLinkReplay(t, pcap)}}
	rc, _, stderr := run(&runCtx{src: src, family: family}, args)
	var writes [][]byte
	if _, err := os.Stat(out); err == nil {
		for _, m := range restoreWrites(t, out) {
			writes = append(writes, wireBytesOf(m))
		}
	}
	goipErr := ""
	if rc != 0 {
		goipErr = strings.TrimSpace(strings.TrimPrefix(strings.SplitN(stderr, "\n", 2)[0], "goip: "))
	}
	look, ipWrites := wsO5Split(ipReqs)
	for i := range ipWrites {
		var z bool
		ipWrites[i], z = wsO5ZeroMetricsPad(ipWrites[i])
		pad = pad || z
	}
	lookups = wsO5Lookups(look, src.gets)
	if len(src.dumpReqs) != 0 {
		lookups = strings.TrimPrefix(lookups+fmt.Sprintf("; goip sent %d dumps", len(src.dumpReqs)), "; ")
	}
	outcome, detail = wsO5Compare(ipWrites, writes, goipErr)
	if outcome == o5Refuses {
		// goip stopped at an argument it does not take, and may not have
		// reached the device whose lookup ip sent.
		lookups = ""
	}
	return outcome, detail, lookups, pad
}

// TestWsGoipDifferential is O5 over every route, rule and nexthop row the
// workbench recorded. It fails when any row's outcome differs from the
// golden list, or when a space holds no rows.
//
// go test ./internal/goip/ -run TestWsGoipDifferential
func TestWsGoipDifferential(t *testing.T) {
	var report []string
	counts := map[string]map[string]int{}
	lookupNotes := map[string]int{}
	padRows := 0
	for _, sp := range wsO5Spaces {
		ents, err := os.ReadDir(filepath.Join(wsCorpusDir, sp))
		if err != nil {
			t.Fatal(err)
		}
		counts[sp] = map[string]int{}
		for _, e := range ents {
			dir := filepath.Join(wsCorpusDir, sp, e.Name())
			b, err := os.ReadFile(filepath.Join(dir, "cmd.json"))
			if err != nil {
				t.Fatal(err)
			}
			var row wsRowFile
			if err := json.Unmarshal(b, &row); err != nil {
				t.Fatalf("%s: %v", dir, err)
			}
			if row.Result.Status != "OK" {
				t.Fatalf("%s: status %s, which O1 already fails", row.ID, row.Result.Status)
			}
			if _, _, args, err := wsO5Argv(row.Cmd); err == nil && !wsO5Applies(args) {
				counts[sp]["n/a"]++
				continue
			}
			outcome, detail, lookups, pad := wsO5Row(t, dir, row)
			if pad {
				padRows++
			}
			counts[sp][outcome]++
			if lookups != "" {
				counts[sp]["lookups"]++
				lookupNotes[lookups]++
			}
			if outcome != o5Same {
				report = append(report, fmt.Sprintf("%s %s rc=%d %s | %s", row.ID, outcome, row.Result.RC, detail, row.Cmd))
			}
			if lookups != "" && !strings.HasPrefix(lookups, "IFLA_EXT_MASK") {
				report = append(report, fmt.Sprintf("%s lookups rc=%d %s | %s", row.ID, row.Result.RC, lookups, row.Cmd))
			}
		}
		if len(ents) == 0 {
			t.Errorf("%s holds no rows", sp)
		}
	}
	head := make([]string, 0, len(wsO5Spaces)+len(lookupNotes)+len(report))
	for _, sp := range wsO5Spaces {
		c := counts[sp]
		head = append(head, fmt.Sprintf("# %s: %d same, %d refuses, %d sends, %d differs; lookups differ in %d; not a write %d",
			sp, c[o5Same], c[o5Refuses], c[o5Sends], c[o5Differs], c["lookups"], c["n/a"]))
	}
	head = append(head, fmt.Sprintf("# metrics padding: %d rows: ip sends stack bytes after an RTA_METRICS sub-attribute; zeroed before comparing", padRows))
	for _, n := range slices.Sorted(maps.Keys(lookupNotes)) {
		head = append(head, fmt.Sprintf("# lookups: %d rows: %s", lookupNotes[n], n))
	}
	got := strings.Join(append(head, report...), "\n") + "\n"
	if *updateWsO5 {
		if err := os.MkdirAll(filepath.Dir(wsO5Golden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(wsO5Golden, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(wsO5Golden)
	if err != nil {
		t.Fatalf("%v (run with -update-ws-o5 to write it)", err)
	}
	for _, l := range wsO5GoldenDiff(string(want), got) {
		t.Error(l)
	}
}

// wsO5GoldenDiff lists the report's lines the golden file lacks ("new: ")
// and the golden lines the report lacks ("gone: "), or says the lines are
// reordered or repeated.
func wsO5GoldenDiff(want, got string) []string {
	if want == got {
		return nil
	}
	var out []string
	wl, gl := strings.Split(want, "\n"), strings.Split(got, "\n")
	for _, l := range gl {
		if !slices.Contains(wl, l) {
			out = append(out, "new: "+l)
		}
	}
	for _, l := range wl {
		if !slices.Contains(gl, l) {
			out = append(out, "gone: "+l)
		}
	}
	if len(out) == 0 {
		out = append(out, "the same lines in another order or count")
	}
	return out
}

// TestWsO5GoldenDiff is how a changed report fails against the golden file.
func TestWsO5GoldenDiff(t *testing.T) {
	tests := []struct {
		description string
		want, got   string
		expected    string // the lines, joined by " | "
	}{
		{"positive: the same report", "a\nb\n", "a\nb\n", ""},
		{"negative: a new divergence", "a\n", "a\nb\n", "new: b"},
		{"negative: a fixed divergence", "a\nb\n", "a\n", "gone: b"},
		{"negative: a changed count", "# x: 1 same\nr\n", "# x: 2 same\nr\n", "new: # x: 2 same | gone: # x: 1 same"},
		{"boundary: an empty golden file", "", "a\n", "new: a"},
		{"corner: the same lines in another order", "a\nb\n", "b\na\n", "the same lines in another order or count"},
		{"corner: a line repeated", "a\n", "a\na\n", "the same lines in another order or count"},
	}
	for i := range tests {
		tc := &tests[i]
		t.Run(tc.description, func(t *testing.T) {
			if got := strings.Join(wsO5GoldenDiff(tc.want, tc.got), " | "); got != tc.expected {
				t.Fatalf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

// o5Msg is a netlink message with seq and pid zeroed, as wireBytesOf
// rebuilds one.
func o5Msg(typ, flags uint16, hdr []byte, attrs ...[]byte) []byte {
	body := cat(append([][]byte{hdr}, attrs...)...)
	b := make([]byte, xtcpnl.NlMsgHdrSizeCst, xtcpnl.NlMsgHdrSizeCst+len(body))
	binary.LittleEndian.PutUint32(b[0:4], uint32(xtcpnl.NlMsgHdrSizeCst+len(body)))
	binary.LittleEndian.PutUint16(b[4:6], typ)
	binary.LittleEndian.PutUint16(b[6:8], flags)
	return append(b, body...)
}

// TestWsO5Compare is O5's outcome for one row on hand-built writes, each of
// which the workbench rows have not yet produced.
func TestWsO5Compare(t *testing.T) {
	ruleHdr := func(table byte) []byte { return []byte{unix.AF_INET, 0, 0, 0, table, 0, 0, 0, 1, 0, 0, 0} }
	prio := fraOf(unix.FRA_PRIORITY, 100, 0, 0, 0)
	prio2 := fraOf(unix.FRA_PRIORITY, 101, 0, 0, 0)
	table := fraOf(unix.FRA_TABLE, 100, 0, 0, 0)
	rule := o5Msg(unix.RTM_NEWRULE, 0x605, ruleHdr(100), prio, table)
	tests := []struct {
		description     string
		ipWrites        [][]byte
		writes          [][]byte
		goipErr         string
		expected        string // the outcome
		expectedDetails string
	}{
		{"positive: the same write", [][]byte{rule}, [][]byte{rule}, "", o5Same, ""},
		{"positive: both refuse, goip at a check it implements", nil, nil,
			`"protocol" value is invalid`, o5Same, ""},
		{"negative: goip refuses a command ip sent", [][]byte{rule}, nil,
			`route "pref": object recognized but not implemented by goip`, o5Refuses,
			`route "pref": object recognized but not implemented by goip`},
		{"negative: both refuse, but goip never reached ip's check", nil, nil,
			`route "nexthop": object recognized but not implemented by goip`, o5Refuses,
			`route "nexthop": object recognized but not implemented by goip`},
		{"negative: goip writes what ip refused to send", nil, [][]byte{rule}, "", o5Sends,
			"ip sent nothing after its lookups"},
		{"negative: goip writes twice", [][]byte{rule}, [][]byte{rule, rule}, "", o5Differs,
			"ip wrote 1 messages, goip 2"},
		{"negative: other flags", [][]byte{rule}, [][]byte{o5Msg(unix.RTM_NEWRULE, 0x405, ruleHdr(100), prio, table)},
			"", o5Differs, "nlmsg type/flags 20000506, goip 20000504"},
		{"negative: another family header", [][]byte{rule}, [][]byte{o5Msg(unix.RTM_NEWRULE, 0x605, ruleHdr(200), prio, table)},
			"", o5Differs, "header 020000006400000001000000, goip 02000000c800000001000000"},
		{"negative: an attribute only ip sends", [][]byte{rule}, [][]byte{o5Msg(unix.RTM_NEWRULE, 0x605, ruleHdr(100), prio)},
			"", o5Differs, "attrs only ip sends [15]"},
		{"negative: an attribute only goip sends", [][]byte{o5Msg(unix.RTM_NEWRULE, 0x605, ruleHdr(100), prio)}, [][]byte{rule},
			"", o5Differs, "attrs only goip sends [15]"},
		{"negative: an attribute value", [][]byte{rule}, [][]byte{o5Msg(unix.RTM_NEWRULE, 0x605, ruleHdr(100), prio2, table)},
			"", o5Differs, "attr values differ [6]"},
		{"corner: the same attributes in another order", [][]byte{rule},
			[][]byte{o5Msg(unix.RTM_NEWRULE, 0x605, ruleHdr(100), table, prio)},
			"", o5Differs, "the same attributes, in another order or padding"},
		{"boundary: a write shorter than its netlink header", [][]byte{rule}, [][]byte{{1, 2, 3, 4}},
			"", o5Differs, "lengths 44 and 4"},
	}
	for i := range tests {
		tc := &tests[i]
		t.Run(tc.description, func(t *testing.T) {
			outcome, detail := wsO5Compare(tc.ipWrites, tc.writes, tc.goipErr)
			if outcome != tc.expected || detail != tc.expectedDetails {
				t.Fatalf("got %s %q, want %s %q", outcome, detail, tc.expected, tc.expectedDetails)
			}
		})
	}
}

// TestWsO5Lookups compares hand-built RTM_GETLINK lookups.
func TestWsO5Lookups(t *testing.T) {
	ifi := func(index byte) []byte { return []byte{0, 0, 0, 0, index, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0} }
	name := fraOf(unix.IFLA_IFNAME, 'd', '0', 0)
	mask := func(m byte, n byte) []byte { return fraOf(unix.IFLA_EXT_MASK, m, n, 0, 0) }
	get := func(hdr []byte, attrs ...[]byte) []byte { return o5Msg(unix.RTM_GETLINK, 1, hdr, attrs...) }
	ip := get(ifi(0), name, mask(0x09, 0x01))
	goip := get(ifi(0), name, mask(0x09, 0))
	tests := []struct {
		description string
		ipLook      [][]byte
		gets        [][]byte
		expected    string
	}{
		{"positive: the same lookup", [][]byte{goip}, [][]byte{goip}, ""},
		{"positive: only the 7.2.0 ext mask differs", [][]byte{ip, ip}, [][]byte{goip, goip},
			"IFLA_EXT_MASK 0x109, goip 0x9"},
		{"negative: goip skips a lookup", [][]byte{ip}, nil, "goip sent 0 lookups, ip 1"},
		{"negative: another name", [][]byte{ip}, [][]byte{get(ifi(0), fraOf(unix.IFLA_IFNAME, 'd', '1', 0), mask(0x09, 0))},
			"lookup 1 differs"},
		{"negative: another ifinfomsg", [][]byte{goip, ip}, [][]byte{goip, get(ifi(2), name, mask(0x09, 0))},
			"lookup 2 differs"},
		{"corner: an ext mask only goip sends", [][]byte{get(ifi(0), name)}, [][]byte{goip}, "lookup 1 differs"},
		{"boundary: no lookups on either side", nil, nil, ""},
	}
	for i := range tests {
		tc := &tests[i]
		t.Run(tc.description, func(t *testing.T) {
			if got := wsO5Lookups(tc.ipLook, tc.gets); got != tc.expected {
				t.Fatalf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

// TestWsO5Split splits ip's requests at its first one that is not a lookup.
func TestWsO5Split(t *testing.T) {
	get := o5Msg(unix.RTM_GETLINK, 1, make([]byte, unix.SizeofIfInfomsg))
	add := o5Msg(unix.RTM_NEWROUTE, 0x605, make([]byte, xtcpnl.RtMsgSizeCst))
	tests := []struct {
		description string
		reqs        [][]byte
		expected    [2]int // lookups, the rest
	}{
		{"positive: two lookups, then the write", [][]byte{get, get, add}, [2]int{2, 1}},
		{"boundary: no lookups", [][]byte{add}, [2]int{0, 1}},
		{"boundary: no requests", nil, [2]int{0, 0}},
		{"corner: a lookup after the write stays with it", [][]byte{add, get}, [2]int{0, 2}},
		{"corner: a request shorter than a netlink header ends the lookups", [][]byte{get, {1, 2}}, [2]int{1, 1}},
	}
	for i := range tests {
		tc := &tests[i]
		t.Run(tc.description, func(t *testing.T) {
			look, rest := wsO5Split(tc.reqs)
			if got := [2]int{len(look), len(rest)}; got != tc.expected {
				t.Fatalf("got %v, want %v", got, tc.expected)
			}
		})
	}
}

// TestWsO5Argv splits a row's command for goip.
func TestWsO5ZeroMetricsPad(t *testing.T) {
	// route is an RTM_NEWROUTE with an rtmsg and the attributes after it.
	route := func(attrs ...byte) []byte {
		m := make([]byte, xtcpnl.NlMsgHdrSizeCst+xtcpnl.RtMsgSizeCst, 64)
		binary.LittleEndian.PutUint16(m[4:6], unix.RTM_NEWROUTE)
		return append(m, attrs...)
	}
	// metrics nests RTAX_CC_ALGO "foo" and its pad byte p, then RTAX_LOCK.
	metrics := func(p byte) []byte {
		return []byte{20, 0, 8, 0, 7, 0, 16, 0, 'f', 'o', 'o', p, 8, 0, 1, 0, 0, 0, 1, 0}
	}
	other := route(metrics(0xc4)...)
	binary.LittleEndian.PutUint16(other[4:6], unix.RTM_NEWRULE)
	tests := []struct {
		description string
		msg         []byte
		want        []byte
		wantZeroed  bool
	}{
		{"positive: a stack byte after congctl foo is zeroed", route(metrics(0xc4)...), route(metrics(0)...), true},
		{"positive: a zero pad is left and not reported", route(metrics(0)...), route(metrics(0)...), false},
		{"negative: padding outside RTA_METRICS is left", route(8, 0, 15, 0, 1, 2, 3, 0xff), route(8, 0, 15, 0, 1, 2, 3, 0xff), false},
		{"negative: a rule write is left", other, other, false},
		{"corner: metrics after another attribute", route(append([]byte{8, 0, 1, 0, 10, 0, 0, 0}, metrics(0xe9)...)...),
			route(append([]byte{8, 0, 1, 0, 10, 0, 0, 0}, metrics(0)...)...), true},
		{"corner: a sub-attribute longer than its nest stops the walk", route(8, 0, 8, 0, 9, 0, 16, 0), route(8, 0, 8, 0, 9, 0, 16, 0), false},
		{"boundary: shorter than an rtmsg", []byte{1, 2, 3}, []byte{1, 2, 3}, false},
	}
	for i := range tests {
		tc := &tests[i]
		t.Run(tc.description, func(t *testing.T) {
			in := slices.Clone(tc.msg)
			got, zeroed := wsO5ZeroMetricsPad(tc.msg)
			if !bytes.Equal(got, tc.want) || zeroed != tc.wantZeroed {
				t.Fatalf("got % x %v, want % x %v", got, zeroed, tc.want, tc.wantZeroed)
			}
			if !bytes.Equal(tc.msg, in) {
				t.Fatal("the input was modified")
			}
		})
	}
}

func TestWsO5Applies(t *testing.T) {
	tests := []struct {
		description string
		args        []string
		expected    bool
	}{
		{"positive: add", []string{"add", "10.0.0.0/24"}, true},
		{"positive: delete spelled in full", []string{"delete", "id", "1"}, true},
		{"positive: del", []string{"del", "id", "1"}, true},
		{"negative: a get is a lookup", []string{"get", "10.0.0.1"}, false},
		{"negative: a flush is a dump", []string{"flush", "table", "main"}, false},
		{"negative: a show", []string{"show"}, false},
		{"corner: an abbreviated add is not compared", []string{"a", "10.0.0.0/24"}, false},
		{"boundary: no verb", nil, false},
	}
	for i := range tests {
		tc := &tests[i]
		t.Run(tc.description, func(t *testing.T) {
			if got := wsO5Applies(tc.args); got != tc.expected {
				t.Fatalf("got %v, want %v", got, tc.expected)
			}
		})
	}
}

func TestWsO5Argv(t *testing.T) {
	tests := []struct {
		description string
		cmd         string
		expected    string // family object [args], or the error
	}{
		{"positive: -4 route add", "ip -4 route add 192.0.2.0/24 dev d0", "2 route [add 192.0.2.0/24 dev d0]"},
		{"positive: -6 nexthop", "ip -6 nexthop add id 10 dev d0", "10 nexthop [add id 10 dev d0]"},
		{"positive: no family", "ip rule add pref 100", "0 rule [add pref 100]"},
		{"boundary: an object with no args", "ip rule", "0 rule []"},
		{"negative: not ip", "tc qdisc add dev d0 root fq", `not an ip command: "tc qdisc add dev d0 root fq"`},
		{"negative: an option goip's rows do not use", "ip -j route", `option -j in "ip -j route"`},
		{"negative: a family and no object", "ip -4", `no object in "ip -4"`},
		{"corner: a bare ip", "ip", `not an ip command: "ip"`},
		{"positive: ro is route", "ip ro add 10.0.0.0/24 dev lo", "0 route [add 10.0.0.0/24 dev lo]"},
		{"positive: nex is nexthop", "ip nex del id 1", "0 nexthop [del id 1]"},
		{"positive: ru is rule", "ip -6 ru add pref 5", "10 rule [add pref 5]"},
		{"negative: an object ip does not have", "ip zz add", `unknown object zz in "ip zz add"`},
	}
	for i := range tests {
		tc := &tests[i]
		t.Run(tc.description, func(t *testing.T) {
			family, object, args, err := wsO5Argv(tc.cmd)
			got := fmt.Sprintf("%d %s %v", family, object, args)
			if err != nil {
				got = err.Error()
			}
			if got != tc.expected {
				t.Fatalf("got %q, want %q", got, tc.expected)
			}
		})
	}
}
