package goip

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
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

// wsCorpusDir is the workbench's corpus for the guest kernel the rows ran on.
const wsCorpusDir = "../../pkg/xtcpnl/testdata/7_3_0/ws"

// wsCorpusDir731 is the corpus of the rows run on kernel 7.3.1
// (goip-parity-next.md, Tier C), at iproute2 7.2.0.
const wsCorpusDir731 = "../../pkg/xtcpnl/testdata/7_3_1/ws"

// wsCorpusDir731IP730 is the corpus of the rows run on kernel 7.3.1 at
// iproute2 7.3.0 (goip-parity-next.md, NC3-T02).
const wsCorpusDir731IP730 = "../../pkg/xtcpnl/testdata/7_3_1/ws-ip730"

// wsO5IP730Prefix marks a space O5 reads from wsCorpusDir731IP730, as
// iproute2 7.3.0.
const wsO5IP730Prefix = "ip730/"

// wsO5SpacePath is the directory of a space's rows: 7.3.1's for those
// captured there, at 7.3.0 for an ip730/ space, else 7.3.0's kernel.
func wsO5SpacePath(sp string) string {
	if name, ok := strings.CutPrefix(sp, wsO5IP730Prefix); ok {
		return filepath.Join(wsCorpusDir731IP730, name)
	}
	if sp == "linkns" || sp == "mpls" || strings.HasPrefix(sp, "v730") {
		return filepath.Join(wsCorpusDir731, sp)
	}
	return filepath.Join(wsCorpusDir, sp)
}

// useWsO5Release sets the knobs to the release a space's rows ran: 7.3.0
// for an ip730/ space, else 7.2.0.
func useWsO5Release(sp string) {
	ip730 := strings.HasPrefix(sp, wsO5IP730Prefix)
	LLAddrStrict = ip730
	Keywords730 = ip730
	render.RenderBondLacpStrict = ip730
	render.RenderNhDstPort = ip730
	render.RenderOvpnMode = ip730
}

// wsO5Verbs are the verbs whose request O5 compares, as ip spells them in
// full. A mined row's get, show, flush or bucket is a dump or a lookup, not
// a write, so O5 does not apply to it; such rows are counted. `ip link
// property` adds or deletes an altname.
var wsO5Verbs = []string{"add", "change", "replace", "prepend", "append", "del", "delete", "set", "property"}

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
		case "-j":
			// A readback's, never a write's: O5 counts it as not a write.
		case "-f":
			if len(f) < 2 || readFamily(f[1]) == unix.AF_UNSPEC {
				return 0, "", nil, fmt.Errorf("bad -f in %q", cmd)
			}
			family = readFamily(f[1])
			f = f[1:]
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
	"route":    runRouteCtx,
	"rule":     runRuleCtx,
	"nexthop":  runNexthopCtx,
	"address":  runAddrWith,
	"neighbor": runNeighCtx,
	objLinkCst: runLinkCtx,
}

// runLinkCtx runs `ip link` args on c, as runRuleCtx runs `ip rule`.
func runLinkCtx(c *runCtx, args []string) (int, string, string) {
	var out, errOut bytes.Buffer
	if c.out == nil {
		c.out = &out
	}
	c.errOut = &errOut
	if c.lltab == nil {
		c.lltab = NewLLTab()
	}
	if err := runLink(c, args); err != nil {
		errOut.WriteString("goip: " + err.Error() + "\n")
		return exitCodeOf(err), out.String(), errOut.String()
	}
	return ExitOK, out.String(), errOut.String()
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
	case unix.RTM_NEWADDR, unix.RTM_DELADDR:
		return xtcpnl.IfAddrmsgSizeCst
	case unix.RTM_NEWNEIGH, unix.RTM_DELNEIGH:
		return xtcpnl.NdMsgSizeCst
	case unix.RTM_NEWLINK, unix.RTM_DELLINK, unix.RTM_NEWLINKPROP, unix.RTM_DELLINKPROP:
		return xtcpnl.IfInfomsgSizeCst
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
			zeroed = wsO5ZeroSubPad(out, off+4, off+l) || zeroed
		}
		off += (l + 3) &^ 3
	}
	return out, zeroed
}

// wsO5ZeroPad zeroes out[from:to], and says whether any of it wasn't zero.
func wsO5ZeroPad(out []byte, from, to int) bool {
	zeroed := false
	for i := from; i < to; i++ {
		zeroed = zeroed || out[i] != 0
		out[i] = 0
	}
	return zeroed
}

// wsO5ZeroSubPad zeroes the alignment padding after each attribute in
// out[start:end], and inside each NLA_F_NESTED one, stopping at one that
// runs past end. rta_nest sets the flag on every nest lwt_parse_encap opens,
// down to a geneve option's.
func wsO5ZeroSubPad(out []byte, start, end int) bool {
	zeroed := false
	for sub := start; sub+4 <= end; {
		sl := int(binary.LittleEndian.Uint16(out[sub:]))
		if sl < 4 || sub+sl > end {
			break
		}
		if binary.LittleEndian.Uint16(out[sub+2:])&unix.NLA_F_NESTED != 0 {
			zeroed = wsO5ZeroSubPad(out, sub+4, sub+sl) || zeroed
		}
		zeroed = wsO5ZeroPad(out, sub+sl, min(sub+(sl+3)&^3, end)) || zeroed
		sub += (sl + 3) &^ 3
	}
	return zeroed
}

// wsO5ZeroEncapPad is a nexthop or route write with the alignment padding
// inside each encap and after each encap type zeroed, and whether any of
// it wasn't zero: NHA_ENCAP and NHA_ENCAP_TYPE on a nexthop, RTA_ENCAP and
// RTA_ENCAP_TYPE on a route and in each hop of its RTA_MULTIPATH.
// lwt_parse_encap builds them in an uninitialized stack buffer
// (ip/ipnexthop.c:1096, ip/iproute.c:1518, parse_nexthops' at :1103), and
// rta_addattr_l doesn't clear the padding, so a ttl's three pad bytes and
// the encap type's two can be stack bytes. goip writes zeros.
func wsO5ZeroEncapPad(msg []byte) ([]byte, bool) {
	if len(msg) < xtcpnl.NlMsgHdrSizeCst {
		return msg, false
	}
	hdr, encap, typ := 0, uint16(0), uint16(0)
	switch binary.LittleEndian.Uint16(msg[4:6]) {
	case unix.RTM_NEWNEXTHOP:
		hdr, encap, typ = xtcpnl.NhMsgSizeCst, xtcpnl.NhaEncap, xtcpnl.NhaEncapType
	case unix.RTM_NEWROUTE, unix.RTM_DELROUTE:
		hdr, encap, typ = xtcpnl.RtMsgSizeCst, unix.RTA_ENCAP, unix.RTA_ENCAP_TYPE
	default:
		return msg, false
	}
	if len(msg) < xtcpnl.NlMsgHdrSizeCst+hdr {
		return msg, false
	}
	out := slices.Clone(msg)
	return out, wsO5ZeroEncapAttrs(out, xtcpnl.NlMsgHdrSizeCst+hdr, len(out), encap, typ)
}

// wsO5ZeroEncapAttrs is wsO5ZeroEncapPad over the attributes in
// out[start:end], a message's or a hop's. A route's RTA_MULTIPATH is
// walked hop by hop, each hop's attributes after its struct rtnexthop.
func wsO5ZeroEncapAttrs(out []byte, start, end int, encap, typ uint16) bool {
	zeroed := false
	for off := start; off+4 <= end; {
		l := int(binary.LittleEndian.Uint16(out[off:]))
		if l < 4 || off+l > end {
			break
		}
		switch binary.LittleEndian.Uint16(out[off+2:]) &^ (unix.NLA_F_NESTED | unix.NLA_F_NET_BYTEORDER) {
		case encap:
			zeroed = wsO5ZeroSubPad(out, off+4, off+l) || zeroed
		case typ:
			zeroed = wsO5ZeroPad(out, off+l, min(off+(l+3)&^3, end)) || zeroed
		case unix.RTA_MULTIPATH:
			if encap == unix.RTA_ENCAP {
				zeroed = wsO5ZeroHops(out, off+4, off+l) || zeroed
			}
		}
		off += (l + 3) &^ 3
	}
	return zeroed
}

// wsO5ZeroHops walks RTA_MULTIPATH's struct rtnexthops in out[start:end].
func wsO5ZeroHops(out []byte, start, end int) bool {
	const rtnhLen = 8
	zeroed := false
	for hop := start; hop+rtnhLen <= end; {
		l := int(binary.LittleEndian.Uint16(out[hop:]))
		if l < rtnhLen || hop+l > end {
			break
		}
		zeroed = wsO5ZeroEncapAttrs(out, hop+rtnhLen, hop+l, unix.RTA_ENCAP, unix.RTA_ENCAP_TYPE) || zeroed
		hop += (l + 3) &^ 3
	}
	return zeroed
}

func TestWsO5HdrLen(t *testing.T) {
	cases := []struct {
		description string
		typ         uint16
		expected    int
	}{
		{"positive: a route's rtmsg", unix.RTM_NEWROUTE, xtcpnl.RtMsgSizeCst},
		{"positive: a rule's fib_rule_hdr", unix.RTM_DELRULE, xtcpnl.FibRuleHdrSizeCst},
		{"positive: a nexthop's nhmsg", unix.RTM_NEWNEXTHOP, xtcpnl.NhMsgSizeCst},
		{"positive: an address's ifaddrmsg", unix.RTM_NEWADDR, xtcpnl.IfAddrmsgSizeCst},
		{"positive: a deleted address's ifaddrmsg", unix.RTM_DELADDR, xtcpnl.IfAddrmsgSizeCst},
		{"positive: a neighbor's ndmsg", unix.RTM_NEWNEIGH, xtcpnl.NdMsgSizeCst},
		{"positive: a deleted neighbor's ndmsg", unix.RTM_DELNEIGH, xtcpnl.NdMsgSizeCst},
		{"positive: a link's ifinfomsg", unix.RTM_NEWLINK, xtcpnl.IfInfomsgSizeCst},
		{"positive: an altname add's ifinfomsg", unix.RTM_NEWLINKPROP, xtcpnl.IfInfomsgSizeCst},
		{"positive: an altname delete's ifinfomsg", unix.RTM_DELLINKPROP, xtcpnl.IfInfomsgSizeCst},
		{"negative: a link property get is not a write", unix.RTM_GETLINKPROP, -1},
		{"negative: a type O5 does not compare", unix.RTM_GETLINK, -1},
		{"boundary: type 0", 0, -1},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			if got := wsO5HdrLen(tc.typ); got != tc.expected {
				t.Errorf("got %d, want %d", got, tc.expected)
			}
		})
	}
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

// wsO5Lookups compares goip's RTM_GETLINK requests of one kind (noun:
// "lookup" or "dump") with those ip sent before anything else. It returns
// "" when they match, and otherwise how they differ; a difference only in
// IFLA_EXT_MASK says which masks, since iproute2 7.2.0 added
// RTEXT_FILTER_NAME_ONLY to ll_link_get and ll_init_map's dump
// (lib/ll_map.c:276 and :397, commit de91e928) after goip modeled 7.1's.
func wsO5Lookups(noun string, ipLook, gets [][]byte) string {
	if len(ipLook) != len(gets) {
		return fmt.Sprintf("goip sent %d %ss, ip %d", len(gets), noun, len(ipLook))
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
			return fmt.Sprintf("%s %d differs", noun, i+1)
		}
		masks[fmt.Sprintf("%s IFLA_EXT_MASK %#x, goip %#x", noun, binary.LittleEndian.Uint32(im), binary.LittleEndian.Uint32(gm))] = true
	}
	return strings.Join(slices.Sorted(maps.Keys(masks)), "; ")
}

// wsO5MaskOnly reports whether every part of a lookups note is a
// difference in IFLA_EXT_MASK alone, which the report counts in its header
// rather than listing row by row.
func wsO5MaskOnly(lookups string) bool {
	for part := range strings.SplitSeq(lookups, "; ") {
		if _, rest, ok := strings.Cut(part, " "); !ok || !strings.HasPrefix(rest, "IFLA_EXT_MASK ") {
			return false
		}
	}
	return lookups != ""
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

// wsO5Split splits ip's requests into its leading RTM_GETLINK requests,
// lookups and dumps apart (ll_init_map dumps every link, as ip neigh does
// before its lookup), and the rest.
func wsO5Split(ipReqs [][]byte) (look, dumps, rest [][]byte) {
	n := 0
	for ; n < len(ipReqs) && len(ipReqs[n]) >= xtcpnl.NlMsgHdrSizeCst &&
		binary.LittleEndian.Uint16(ipReqs[n][4:6]) == unix.RTM_GETLINK; n++ {
		if binary.LittleEndian.Uint16(ipReqs[n][6:8])&unix.NLM_F_DUMP == unix.NLM_F_DUMP {
			dumps = append(dumps, ipReqs[n])
		} else {
			look = append(look, ipReqs[n])
		}
	}
	return look, dumps, ipReqs[n:]
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
	dump := o5Msg(unix.RTM_GETLINK, unix.NLM_F_REQUEST|unix.NLM_F_DUMP, ifi(0), mask(0x01, 0))
	tests := []struct {
		description string
		noun        string
		ipLook      [][]byte
		gets        [][]byte
		expected    string
	}{
		{"positive: the same lookup", "lookup", [][]byte{goip}, [][]byte{goip}, ""},
		{"positive: only the 7.2.0 ext mask differs", "lookup", [][]byte{ip, ip}, [][]byte{goip, goip},
			"lookup IFLA_EXT_MASK 0x109, goip 0x9"},
		{"negative: goip skips a lookup", "lookup", [][]byte{ip}, nil, "goip sent 0 lookups, ip 1"},
		{"negative: another name", "lookup", [][]byte{ip}, [][]byte{get(ifi(0), fraOf(unix.IFLA_IFNAME, 'd', '1', 0), mask(0x09, 0))},
			"lookup 1 differs"},
		{"negative: another ifinfomsg", "lookup", [][]byte{goip, ip}, [][]byte{goip, get(ifi(2), name, mask(0x09, 0))},
			"lookup 2 differs"},
		{"corner: an ext mask only goip sends", "lookup", [][]byte{get(ifi(0), name)}, [][]byte{goip}, "lookup 1 differs"},
		{"boundary: no lookups on either side", "lookup", nil, nil, ""},
		{"positive: the same link dump", "dump", [][]byte{dump}, [][]byte{dump}, ""},
		{"negative: goip skips ip's link dump", "dump", [][]byte{dump}, nil, "goip sent 0 dumps, ip 1"},
		{"positive: only the 7.2.0 ext mask of the dump differs", "dump",
			[][]byte{o5Msg(unix.RTM_GETLINK, unix.NLM_F_REQUEST|unix.NLM_F_DUMP, ifi(0), mask(0x09, 0x01))}, [][]byte{dump},
			"dump IFLA_EXT_MASK 0x109, goip 0x1"},
		{"negative: goip dumps where ip did not", "dump", nil, [][]byte{dump}, "goip sent 1 dumps, ip 0"},
	}
	for i := range tests {
		tc := &tests[i]
		t.Run(tc.description, func(t *testing.T) {
			if got := wsO5Lookups(tc.noun, tc.ipLook, tc.gets); got != tc.expected {
				t.Fatalf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

func TestWsO5MaskOnly(t *testing.T) {
	tests := []struct {
		description string
		lookups     string
		expected    bool
	}{
		{"positive: a lookup's mask", "lookup IFLA_EXT_MASK 0x109, goip 0x9", true},
		{"positive: a dump's mask", "dump IFLA_EXT_MASK 0x109, goip 0x1", true},
		{"positive: both", "lookup IFLA_EXT_MASK 0x109, goip 0x9; dump IFLA_EXT_MASK 0x109, goip 0x1", true},
		{"negative: a lookup that differs otherwise", "lookup 1 differs", false},
		{"negative: a mask and a count", "lookup IFLA_EXT_MASK 0x109, goip 0x9; goip sent 0 dumps, ip 1", false},
		{"boundary: no note", "", false},
		{"corner: the mask without its noun", "IFLA_EXT_MASK 0x109, goip 0x9", false},
	}
	for i := range tests {
		tc := &tests[i]
		t.Run(tc.description, func(t *testing.T) {
			if got := wsO5MaskOnly(tc.lookups); got != tc.expected {
				t.Fatalf("got %v, want %v", got, tc.expected)
			}
		})
	}
}

// TestWsO5Split splits ip's requests at its first one that is not a
// lookup or a link dump.
func TestWsO5Split(t *testing.T) {
	get := o5Msg(unix.RTM_GETLINK, 1, make([]byte, unix.SizeofIfInfomsg))
	dump := o5Msg(unix.RTM_GETLINK, unix.NLM_F_REQUEST|unix.NLM_F_DUMP, make([]byte, unix.SizeofIfInfomsg))
	add := o5Msg(unix.RTM_NEWROUTE, 0x605, make([]byte, xtcpnl.RtMsgSizeCst))
	tests := []struct {
		description string
		reqs        [][]byte
		expected    [3]int // lookups, dumps, the rest
	}{
		{"positive: two lookups, then the write", [][]byte{get, get, add}, [3]int{2, 0, 1}},
		{"positive: ip neigh's link dump, then its lookup, then the write", [][]byte{dump, get, add}, [3]int{1, 1, 1}},
		{"boundary: no lookups", [][]byte{add}, [3]int{0, 0, 1}},
		{"boundary: no requests", nil, [3]int{0, 0, 0}},
		{"corner: a lookup after the write stays with it", [][]byte{add, get}, [3]int{0, 0, 2}},
		{"corner: a request shorter than a netlink header ends the lookups", [][]byte{get, {1, 2}}, [3]int{1, 0, 1}},
		{"corner: NLM_F_ROOT alone is not a dump", [][]byte{o5Msg(unix.RTM_GETLINK, unix.NLM_F_REQUEST|unix.NLM_F_ROOT, make([]byte, unix.SizeofIfInfomsg)), add},
			[3]int{1, 0, 1}},
	}
	for i := range tests {
		tc := &tests[i]
		t.Run(tc.description, func(t *testing.T) {
			look, dumps, rest := wsO5Split(tc.reqs)
			if got := [3]int{len(look), len(dumps), len(rest)}; got != tc.expected {
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

func TestWsO5ZeroEncapPad(t *testing.T) {
	// nh is an RTM_NEWNEXTHOP with an nhmsg and the attributes after it.
	nh := func(attrs ...byte) []byte {
		m := make([]byte, xtcpnl.NlMsgHdrSizeCst+xtcpnl.NhMsgSizeCst, 64)
		binary.LittleEndian.PutUint16(m[4:6], unix.RTM_NEWNEXTHOP)
		return append(m, attrs...)
	}
	// encap nests MPLS_IPTUNNEL_DST label 101 and MPLS_IPTUNNEL_TTL 10 with
	// pad bytes p, then NHA_ENCAP_TYPE mpls with pad bytes q.
	encap := func(p, q byte) []byte {
		return []byte{20, 0, 8, 0x80, 8, 0, 1, 0, 0, 6, 0x51, 0, 5, 0, 2, 0, 10, p, p, p, 6, 0, 7, 0, 1, 0, q, q}
	}
	other := nh(encap(0x7f, 0x9b)...)
	binary.LittleEndian.PutUint16(other[4:6], unix.RTM_NEWLINK)
	// route is an RTM_NEWROUTE with an rtmsg and the attributes after it.
	route := func(attrs ...byte) []byte {
		m := make([]byte, xtcpnl.NlMsgHdrSizeCst+xtcpnl.RtMsgSizeCst, 96)
		binary.LittleEndian.PutUint16(m[4:6], unix.RTM_NEWROUTE)
		return append(m, attrs...)
	}
	// ttl is RTA_ENCAP holding LWTUNNEL_IP_TTL 64 with pad bytes p, then
	// RTA_ENCAP_TYPE ip with pad bytes q.
	ttl := func(p, q byte) []byte {
		return []byte{12, 0, 0x16, 0x80, 5, 0, 4, 0, 64, p, p, p, 6, 0, 21, 0, 2, 0, q, q}
	}
	// geneve is RTA_ENCAP holding LWTUNNEL_IP_OPTS, a GENEVE option, and
	// its CLASS and TYPE with pad bytes p, two nests below the encap.
	geneve := func(p byte) []byte {
		return []byte{28, 0, 0x16, 0x80, 24, 0, 8, 0x80, 20, 0, 1, 0x80,
			6, 0, 1, 0, 1, 2, p, p, 5, 0, 2, 0, 0x80, p, p, p, 6, 0, 21, 0, 2, 0, 0, 0}
	}
	// flavors is RTA_ENCAP holding seg6local's ACTION End and its FLAVORS
	// nest, OPERATION next-csid and LCBLOCK_BITS 32 with pad bytes p, then
	// RTA_ENCAP_TYPE seg6local (LX3-T03).
	flavors := func(p byte) []byte {
		return []byte{32, 0, 0x16, 0x80, 8, 0, 1, 0, 1, 0, 0, 0, 20, 0, 0x0b, 0x80,
			8, 0, 1, 0, 0x10, 0, 0, 0, 5, 0, 2, 0, 32, p, p, p, 6, 0, 21, 0, 7, 0, 0, 0}
	}
	// hop is RTA_MULTIPATH with one rtnexthop: ttl(p, q), then a gateway.
	hop := func(p, q byte) []byte {
		return append([]byte{40, 0, 9, 0, 36, 0, 0, 0, 2, 0, 0, 0}, append(ttl(p, q), 8, 0, 5, 0, 192, 0, 2, 1)...)
	}
	tests := []struct {
		description string
		msg         []byte
		want        []byte
		wantZeroed  bool
	}{
		{"positive: stack bytes after the ttl and the encap type are zeroed", nh(encap(0x7f, 0x9b)...), nh(encap(0, 0)...), true},
		{"positive: only the encap type's pad", nh(encap(0, 0x17)...), nh(encap(0, 0)...), true},
		{"positive: a zero pad is left and not reported", nh(encap(0, 0)...), nh(encap(0, 0)...), false},
		{"negative: padding after another attribute is left", nh(5, 0, 1, 0, 9, 0xff, 0xff, 0xff), nh(5, 0, 1, 0, 9, 0xff, 0xff, 0xff), false},
		{"negative: another message type is left", other, other, false},
		{"positive: a route's encap and its type", route(ttl(0x7f, 0x9b)...), route(ttl(0, 0)...), true},
		{"positive: a geneve option's padding, two nests down", route(geneve(0x7f)...), route(geneve(0)...), true},
		{"positive: a hop's padding zeroed, its other bytes kept", route(hop(0x7f, 0x9b)...), route(hop(0, 0)...), true},
		{"positive: a seg6local flavors u8's padding, a nest down", route(flavors(0x7f)...), route(flavors(0)...), true},
		{"negative: padding after a route's other attribute is left", route(5, 0, 20, 0, 1, 0xff, 0xff, 0xff),
			route(5, 0, 20, 0, 1, 0xff, 0xff, 0xff), false},
		{"corner: a hop longer than RTA_MULTIPATH stops the walk", route(12, 0, 9, 0, 9, 0, 0, 0, 2, 0, 0, 0),
			route(12, 0, 9, 0, 9, 0, 0, 0, 2, 0, 0, 0), false},
		{"corner: a sub-attribute longer than its nest stops the walk", nh(8, 0, 8, 0x80, 9, 0, 1, 0), nh(8, 0, 8, 0x80, 9, 0, 1, 0), false},
		{"boundary: shorter than an nhmsg", []byte{1, 2, 3}, []byte{1, 2, 3}, false},
	}
	for i := range tests {
		tc := &tests[i]
		t.Run(tc.description, func(t *testing.T) {
			in := slices.Clone(tc.msg)
			got, zeroed := wsO5ZeroEncapPad(tc.msg)
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
		{"positive: a link set", []string{"set", "dev", "d0", "up"}, true},
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
		{"negative: an option goip's rows do not use", "ip -d route", `option -d in "ip -d route"`},
		{"positive: -f mpls is AF_MPLS", "ip -f mpls route add 100 dev d0", "28 route [add 100 dev d0]"},
		{"corner: -j is a readback's, and changes nothing", "ip -j -f mpls route show", "28 route [show]"},
		{"negative: a -f word read_family does not know", "ip -f ipx route", `bad -f in "ip -f ipx route"`},
		{"boundary: -f as the last word", "ip -f", `bad -f in "ip -f"`},
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
