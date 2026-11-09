package xtcpnl

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// The ss rows' oracles (coverage-status.md, "W5c"). Each row's namespace
// holds the sockets tools/nlcapgen/matrix_ss.go ssHolder opens (wsSsKnown).
// O3a holds each SOCK_DIAG_BY_FAMILY request to the row's facts: family
// order, protocol, states, ext, the protocol attribute and whether there is
// bytecode, which must pass the kernel's audit. O3b runs the bytecode, as
// the kernel would, over probe sockets and compares each verdict with the
// filter the user meant (the pred fact, evaluated here on its own). O4
// holds the replies to the same filter: each lists a known socket that
// passes it, and every known socket that passes it is listed.

// sockDiagByFamily is SOCK_DIAG_BY_FAMILY (include/uapi/linux/sock_diag.h).
const sockDiagByFamily = 20

// inetDiagMarkAttr is INET_DIAG_MARK, sent to a CAP_NET_ADMIN reader
// (inet_diag.c:128).
const inetDiagMarkAttr = 15

// ssPred is a filter as the user meant it: an and, or or not of its kids,
// or one atom.
type ssPred struct {
	op   string // "and", "or", "not" or "atom"
	kids []*ssPred
	key  string // sport, dport, src, dst, auto, mark, dev or cgroup
	cmp  string // a port's =, !=, <, >, <= or >=
	num  int64
	// An address atom's prefixes (localhost is two) and port, -1 for any.
	prefixes   []netip.Prefix
	port       int64
	mark, mask uint32
}

var ssCmps = []string{">=", "<=", "!=", "=", ">", "<"}

// ssParsePred parses the pred fact; "" is no filter (nil).
func ssParsePred(s string) (*ssPred, error) {
	if s == "" {
		return nil, nil
	}
	for _, op := range []string{"and", "or", "not"} {
		inner, ok := strings.CutPrefix(s, op+"(")
		if !ok {
			continue
		}
		inner, ok = strings.CutSuffix(inner, ")")
		if !ok {
			return nil, fmt.Errorf("pred %q: unclosed %s", s, op)
		}
		p := &ssPred{op: op}
		for _, arg := range ssSplitArgs(inner) {
			k, err := ssParsePred(arg)
			if err != nil {
				return nil, err
			}
			if k == nil {
				return nil, fmt.Errorf("pred %q: empty operand", s)
			}
			p.kids = append(p.kids, k)
		}
		if want := map[string]int{"and": 2, "or": 2, "not": 1}[op]; len(p.kids) != want {
			return nil, fmt.Errorf("pred %q: %s takes %d operands", s, op, want)
		}
		return p, nil
	}
	return ssParseAtom(s)
}

// ssSplitArgs splits at the commas outside parentheses.
func ssSplitArgs(s string) []string {
	var out []string
	depth, start := 0, 0
	for i, c := range s {
		switch c {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

func ssParseAtom(s string) (*ssPred, error) {
	if s == "auto" {
		return &ssPred{op: "atom", key: "auto"}, nil
	}
	p := &ssPred{op: "atom", port: -1}
	var val string
	for _, c := range ssCmps {
		if k, v, ok := strings.Cut(s, c); ok {
			p.key, p.cmp, val = k, c, v
			break
		}
	}
	var err error
	switch p.key {
	case "sport", "dport":
		p.num, err = strconv.ParseInt(val, 10, 64)
	case "src", "dst":
		err = ssParseHost(p, val)
	case "mark":
		err = ssParseMark(p, val)
	case "dev", "cgroup":
		if p.cmp != "=" || val == "" {
			err = errors.New("want =NAME")
		}
	default:
		err = errors.New("unknown atom")
	}
	if err == nil && p.key != "sport" && p.key != "dport" && p.cmp != "=" {
		err = errors.New("only ports compare with " + p.cmp)
	}
	if err != nil {
		return nil, fmt.Errorf("pred atom %q: %w", s, err)
	}
	return p, nil
}

// ssParseHost reads PREFIX[:PORT] or localhost.
func ssParseHost(p *ssPred, val string) error {
	if val == "localhost" {
		p.prefixes = []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32"), netip.MustParsePrefix("::1/128")}
		return nil
	}
	addr, rest, ok := strings.Cut(val, "/")
	if !ok {
		return errors.New("want PREFIX/LEN[:PORT]")
	}
	if plen, port, ok := strings.Cut(rest, ":"); ok {
		n, err := strconv.ParseInt(port, 10, 64)
		if err != nil {
			return err
		}
		p.port, rest = n, plen
	}
	pfx, err := netip.ParsePrefix(addr + "/" + rest)
	if err != nil {
		return err
	}
	p.prefixes = []netip.Prefix{pfx}
	return nil
}

func ssParseMark(p *ssPred, val string) error {
	m, k, ok := strings.Cut(val, "/")
	if !ok {
		return errors.New("want MARK/MASK")
	}
	mark, err := strconv.ParseUint(m, 10, 32)
	mask, err2 := strconv.ParseUint(k, 10, 32)
	if err := errors.Join(err, err2); err != nil {
		return err
	}
	p.mark, p.mask = uint32(mark), uint32(mask)
	return nil
}

// passes is whether e passes the filter. cgroup is the id cgroup=/ names
// (the test takes it from the bytecode, having no way to resolve it).
func (p *ssPred) passes(e *InetDiagEntry, cgroup uint64) bool {
	if p == nil {
		return true
	}
	switch p.op {
	case "and":
		return p.kids[0].passes(e, cgroup) && p.kids[1].passes(e, cgroup)
	case "or":
		return p.kids[0].passes(e, cgroup) || p.kids[1].passes(e, cgroup)
	case "not":
		return !p.kids[0].passes(e, cgroup)
	}
	switch p.key {
	case "sport":
		return ssCompare(int64(e.Sport), p.cmp, p.num)
	case "dport":
		return ssCompare(int64(e.Dport), p.cmp, p.num)
	case "src":
		return ssHostMatch(p, e.Saddr, int64(e.Sport), e.Family)
	case "dst":
		return ssHostMatch(p, e.Daddr, int64(e.Dport), e.Family)
	case "auto":
		return e.Userlocks&sockBindportLock == 0
	case "mark":
		return e.Mark&p.mask == p.mark
	case "dev":
		return e.Ifindex == 1 // lo, in a fresh namespace
	default: // cgroup
		return e.CgroupID == cgroup
	}
}

func ssCompare(a int64, cmp string, b int64) bool {
	switch cmp {
	case "=":
		return a == b
	case "!=":
		return a != b
	case "<":
		return a < b
	case ">":
		return a > b
	case "<=":
		return a <= b
	default: // ">="
		return a >= b
	}
}

// ssHostMatch: an IPv4 prefix takes an IPv4 socket or a v4-mapped IPv6
// one, an IPv6 prefix only an IPv6 socket, as the kernel compares families
// (inet_diag.c inet_diag_bc_run).
func ssHostMatch(p *ssPred, addr []byte, port int64, family uint16) bool {
	if p.port != -1 && p.port != port {
		return false
	}
	a, ok := netip.AddrFromSlice(addr)
	if !ok {
		return false
	}
	for _, pfx := range p.prefixes {
		switch {
		case pfx.Addr().Is4() && family == afInet6 && a.Is4In6():
			if pfx.Contains(a.Unmap()) {
				return true
			}
		case pfx.Addr().Is4() == (family == afInet) && pfx.Contains(a):
			return true
		}
	}
	return false
}

// wsSsSock is one socket ssHolder opens.
type wsSsSock struct {
	proto uint32
	state uint8
	e     InetDiagEntry
}

func wsSsEntry(src string, sport uint16, dst string, dport uint16, userlocks uint16, mark uint32) InetDiagEntry {
	e := *bcEntry(src, sport, dst, dport)
	e.Userlocks, e.Mark = userlocks, mark
	return e
}

// The kernel's TCP_* states and the userlocks a bind sets
// (SOCK_BINDADDR_LOCK 4 for an address other than any, SOCK_BINDPORT_LOCK
// 8). An accepted socket keeps its listener's but the port lock
// (sk_clone_lock, net/core/sock.c:2570), so autobound takes it.
const (
	tcpEstablished = 1
	tcpClose       = 7
	tcpListen      = 10
	lockAddr       = 4
	lockAddrPort   = 12
	lockPort       = 8
)

// wsSsKnown is ssHolder's sockets; change both together.
var wsSsKnown = []wsSsSock{
	{6, tcpListen, wsSsEntry("127.0.0.1", 22, "0.0.0.0", 0, lockAddrPort, 0)},
	{6, tcpListen, wsSsEntry("::1", 80, "::", 0, lockAddrPort, 0)},
	{6, tcpListen, wsSsEntry("127.0.0.1", 8080, "0.0.0.0", 0, lockAddrPort, 0)},
	{6, tcpEstablished, wsSsEntry("127.0.0.1", 45000, "127.0.0.1", 8080, 0, 0)},
	{6, tcpEstablished, wsSsEntry("127.0.0.1", 8080, "127.0.0.1", 45000, lockAddr, 0)},
	{6, tcpListen, wsSsEntry("::", 443, "::", 0, lockPort, 0)},
	{6, tcpEstablished, wsSsEntry("127.0.0.1", 45000, "127.0.0.1", 443, 0, 0)},
	{6, tcpEstablished, wsSsEntry("::ffff:127.0.0.1", 443, "::ffff:127.0.0.1", 45000, 0, 0)},
	{6, tcpListen, wsSsEntry("127.0.0.1", 9000, "0.0.0.0", 0, lockAddrPort, 16)},
	{17, tcpClose, wsSsEntry("127.0.0.1", 53, "0.0.0.0", 0, lockAddrPort, 0)},
	{17, tcpClose, wsSsEntry("::1", 5353, "::", 0, lockAddrPort, 0)},
	{17, tcpEstablished, wsSsEntry("127.0.0.1", 45000, "127.0.0.1", 53, 0, 0)},
	{255, tcpClose, wsSsEntry("0.0.0.0", 1, "0.0.0.0", 0, 0, 0)},
}

// wsSsProbes are the sockets O3b runs the bytecode over: the known ones,
// then a grid of ports, addresses, locks, marks, devices and cgroups
// around the rows' boundaries.
func wsSsProbes(cgroup uint64) []InetDiagEntry {
	probes := make([]InetDiagEntry, 0, len(wsSsKnown)+400)
	for i := range wsSsKnown {
		probes = append(probes, wsSsKnown[i].e)
	}
	addrs := []string{"127.0.0.1", "127.255.255.255", "10.0.0.1", "0.0.0.0",
		"::1", "::", "2001:db8::1", "2001:db9::1", "::ffff:127.0.0.1", "::ffff:10.0.0.1"}
	ports := []uint16{0, 1, 22, 80, 443, 1023, 1024, 1025, 8079, 8080, 8081, 45000, 65535}
	for i, a := range addrs {
		for j, p := range ports {
			e := wsSsEntry(a, p, addrs[(i+3)%len(addrs)], ports[(j+5)%len(ports)], 0, 0)
			e.Daddr = ssSameFamily(e.Family, addrs[(i+3)%len(addrs)])
			e.Userlocks = []uint16{0, lockPort, lockAddrPort}[(i+j)%3]
			e.Mark = []uint32{0, 0x10, 0x1f, 0x20}[(i*j)%4]
			e.Ifindex = uint32((i + j) % 3)
			e.CgroupID = cgroup + uint64(j%2)
			probes = append(probes, e)
		}
	}
	return probes
}

// ssSameFamily is addr in family: an IPv4 address mapped into IPv6, or an
// IPv6 one's last 4 bytes for IPv4.
func ssSameFamily(family uint16, addr string) []byte {
	a := netip.MustParseAddr(addr)
	if family == afInet6 {
		b := a.As16()
		return b[:]
	}
	if a.Is4() || a.Is4In6() {
		b := a.Unmap().As4()
		return b[:]
	}
	b := a.As16()
	return b[12:]
}

// ssWant is the requests a row's facts call for: one per family, in order.
type ssWant struct {
	proto      uint32
	families   []uint8
	states     uint32
	ext        uint8
	pred       *ssPred
	bytecode   bool   // a filter ss compiles; false for none, or one it applies itself
	bug        string // an iproute2 bug the row shows
	predSource string
	probes     int // cgroup support probes sent before the dumps
}

func ssWantFromAxes(ax map[string]string) (ssWant, error) {
	var w ssWant
	proto, err := strconv.ParseUint(ax["proto"], 10, 32)
	states, err2 := strconv.ParseUint(ax["states"], 10, 32)
	ext, err3 := strconv.ParseUint(ax["ext"], 10, 8)
	if err := errors.Join(err, err2, err3); err != nil {
		return w, err
	}
	w.proto, w.states, w.ext = uint32(proto), uint32(states), uint8(ext)
	for _, fam := range strings.Split(ax["families"], ",") {
		n, err := strconv.ParseUint(fam, 10, 8)
		if err != nil {
			return w, fmt.Errorf("axis families=%q: %w", ax["families"], err)
		}
		w.families = append(w.families, uint8(n))
	}
	w.predSource, w.bug = ax["pred"], ax["bug"]
	if p, ok := ax["probes"]; ok {
		n, err := strconv.Atoi(p)
		if err != nil {
			return w, fmt.Errorf("axis probes=%q: %w", p, err)
		}
		w.probes = n
	}
	if w.pred, err = ssParsePred(w.predSource); err != nil {
		return w, err
	}
	w.bytecode = w.pred != nil && ax["bc"] != "none"
	return w, nil
}

// ssRequestProblem is O3a for one request.
func ssRequestProblem(w ssWant, family uint8, r *InetDiagRequest) error {
	var errs []error
	if r.Family != family {
		errs = append(errs, fmt.Errorf("family %d, want %d", r.Family, family))
	}
	if p := r.EffectiveProtocol(); p != w.proto {
		errs = append(errs, fmt.Errorf("protocol %d, want %d", p, w.proto))
	}
	// sockdiag_send stores the protocol in the u8 too, truncated.
	if r.Protocol != uint8(w.proto&0xff) {
		errs = append(errs, fmt.Errorf("sdiag_protocol %d, want %d", r.Protocol, w.proto&0xff))
	}
	if r.HasProtocolAttr != (w.proto > 255) {
		errs = append(errs, fmt.Errorf("INET_DIAG_REQ_PROTOCOL present %v, want %v", r.HasProtocolAttr, w.proto > 255))
	}
	if r.States != w.states {
		errs = append(errs, fmt.Errorf("states %#x, want %#x", r.States, w.states))
	}
	if r.Ext != w.ext {
		errs = append(errs, fmt.Errorf("ext %#x, want %#x", r.Ext, w.ext))
	}
	if r.HasBytecode != w.bytecode {
		errs = append(errs, fmt.Errorf("bytecode present %v, want %v", r.HasBytecode, w.bytecode))
	}
	if r.HasBytecode {
		if err := InetDiagBcAudit(r.Bytecode, true); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// ssCgroupProbe is the request ssfilter_check.c cgroup_filter_check sends
// to learn whether the kernel takes INET_DIAG_BC_CGROUP_COND: AF_INET,
// TCP, no states, and a cgroup 0 condition.
func ssCgroupProbeProblem(r *InetDiagRequest) error {
	want := binary.LittleEndian.AppendUint64([]byte{InetDiagBcCgroupCond, 12, 16, 0}, 0)
	if r.Family != afInet || r.Protocol != unix.IPPROTO_TCP || r.States != 0 || r.Ext != 0 ||
		string(r.Bytecode) != string(want) {
		return fmt.Errorf("not the cgroup probe: %+v", *r)
	}
	return nil
}

// ssBcCgroup is the id of the bytecode's first CGROUP_COND, or 0.
func ssBcCgroup(bc []byte) uint64 {
	for off := 0; off+12 <= len(bc) && bc[off+1] >= InetDiagBcOpSizeCst; off += int(bc[off+1]) {
		if bc[off] == InetDiagBcCgroupCond {
			return binary.LittleEndian.Uint64(bc[off+4:])
		}
	}
	return 0
}

// ssBcMismatches is O3b: the probes on which the bytecode and the pred
// disagree.
func ssBcMismatches(w ssWant, bc []byte) []InetDiagEntry {
	cg := ssBcCgroup(bc)
	var out []InetDiagEntry
	probes := wsSsProbes(cg)
	for i := range probes {
		if InetDiagBcRun(bc, &probes[i]) != w.pred.passes(&probes[i], cg) {
			out = append(out, probes[i])
		}
	}
	return out
}

// ssReply is one reply's socket, as an entry the filter can be run on.
func ssReply(body []byte) (InetDiagEntry, uint8, error) {
	var m InetDiagMsg
	var id InetDiagSockID
	if _, err := DeserializeInetDiagMsg(body, &m, &id); err != nil {
		return InetDiagEntry{}, 0, err
	}
	e := InetDiagEntry{Family: uint16(m.Family), Sport: id.SPort, Dport: id.DPort, Ifindex: id.Interface}
	n := 16
	if m.Family == afInet {
		n = 4
	}
	e.Saddr, e.Daddr = append([]byte(nil), id.SrcIP[:n]...), append([]byte(nil), id.DstIP[:n]...)
	err := WalkRTAttrs(body[InetDiagMsgSizeCst:], func(atype uint16, val []byte) {
		if atype == inetDiagMarkAttr && len(val) == 4 {
			e.Mark = binary.LittleEndian.Uint32(val)
		}
	})
	return e, m.State, err
}

// ssKnownOf is the known socket e is, by protocol, family, ports and
// addresses; nil when ssHolder opened no such socket.
func ssKnownOf(proto uint32, e *InetDiagEntry) *wsSsSock {
	for i := range wsSsKnown {
		k := &wsSsKnown[i]
		if k.proto == proto && k.e.Family == e.Family && k.e.Sport == e.Sport && k.e.Dport == e.Dport &&
			string(k.e.Saddr) == string(e.Saddr) && string(k.e.Daddr) == string(e.Daddr) {
			return k
		}
	}
	return nil
}

// ssRoundTripProblem is O4 for one request's replies. passes is the
// filter the kernel should have applied: the pred, or for a bug row the
// bytecode as sent, or nothing when ss sent none.
func ssRoundTripProblem(proto uint32, family uint8, states uint32, replies []capturedNlMsg,
	passes func(*InetDiagEntry) bool) error {
	var errs []error
	seen := map[*wsSsSock]bool{}
	for _, m := range replies {
		e, state, err := ssReply(m.body)
		if err != nil {
			return err
		}
		k := ssKnownOf(proto, &e)
		if k == nil {
			errs = append(errs, fmt.Errorf("reply lists a socket the holder never opened: %+v", e))
			continue
		}
		seen[k] = true
		e.Userlocks, e.CgroupID = k.e.Userlocks, k.e.CgroupID
		if e.Mark != k.e.Mark {
			errs = append(errs, fmt.Errorf("%d:%d mark %#x, want %#x", e.Sport, e.Dport, e.Mark, k.e.Mark))
		}
		if state != k.state || states&(1<<state) == 0 {
			errs = append(errs, fmt.Errorf("%d:%d state %d, holder's %d, mask %#x", e.Sport, e.Dport, state, k.state, states))
		}
		if !passes(&e) {
			errs = append(errs, fmt.Errorf("%d:%d listed although the filter refuses it", e.Sport, e.Dport))
		}
	}
	for i := range wsSsKnown {
		k := &wsSsKnown[i]
		if k.proto != proto || k.e.Family != uint16(family) || states&(1<<k.state) == 0 || seen[k] {
			continue
		}
		if e := k.e; passes(&e) {
			errs = append(errs, fmt.Errorf("%d:%d (family %d) passes the filter but is not listed", e.Sport, e.Dport, e.Family))
		}
	}
	return errors.Join(errs...)
}

// wsSsExchanges splits a capture into its sock_diag requests, each with
// the replies that follow it.
func wsSsExchanges(msgs []capturedNlMsg) (reqs []capturedNlMsg, replies [][]capturedNlMsg) {
	for _, m := range msgs {
		if m.proto != unix.NETLINK_SOCK_DIAG || m.h.Type != sockDiagByFamily {
			continue
		}
		if m.h.Flags&unix.NLM_F_REQUEST != 0 {
			reqs = append(reqs, m)
			replies = append(replies, nil)
			continue
		}
		if len(replies) > 0 {
			replies[len(replies)-1] = append(replies[len(replies)-1], m)
		}
	}
	return reqs, replies
}

func wsSsRow(t *testing.T, dir string, row wsRow) (checked, roundTrips int) {
	t.Helper()
	msgs := pcapNlMsgs(t, filepath.Join(dir, "ip.pcap"))
	raw, replies := wsSsExchanges(msgs)
	if row.Expect.RefusedBy == wsRefusedIproute2 {
		if len(raw) != 0 {
			t.Errorf("O3: ss refused the row but sent %d requests\n  cmd: %s", len(raw), row.Cmd)
		}
		return 0, 0
	}
	w, err := ssWantFromAxes(row.Axes)
	if err != nil {
		t.Fatalf("O3 model: %v", err)
	}
	reqs := make([]InetDiagRequest, len(raw))
	for i := range raw {
		if reqs[i], err = ParseInetDiagRequest(raw[i].body); err != nil {
			t.Fatal(err)
		}
	}
	errs, checked, roundTrips := ssExchangeProblems(w, reqs, replies)
	for _, err := range errs {
		t.Errorf("%v\n  cmd: %s", err, row.Cmd)
	}
	return checked, roundTrips
}

// ssExchangeProblems holds a row's requests (cgroup probes first) and each
// one's replies to the row's facts: O3a, O3b and O4.
func ssExchangeProblems(w ssWant, reqs []InetDiagRequest, replies [][]capturedNlMsg) (errs []error, checked, roundTrips int) {
	if len(reqs) != w.probes+len(w.families) {
		return []error{fmt.Errorf("O3: %d requests, want %d cgroup probes and one per family %v", len(reqs), w.probes, w.families)}, 0, 0
	}
	for i := range w.probes {
		if err := ssCgroupProbeProblem(&reqs[i]); err != nil {
			errs = append(errs, fmt.Errorf("O3a: request %d: %w", i, err))
		}
	}
	reqs, replies = reqs[w.probes:], replies[w.probes:]
	for i := range reqs {
		r := &reqs[i]
		if err := ssRequestProblem(w, w.families[i], r); err != nil {
			errs = append(errs, fmt.Errorf("O3a: request %d: %w", i, err))
			continue
		}
		checked++
		passes := func(*InetDiagEntry) bool { return true }
		if r.HasBytecode {
			miss := ssBcMismatches(w, r.Bytecode)
			switch {
			case w.bug == "" && len(miss) > 0:
				errs = append(errs, fmt.Errorf("O3b: request %d: bytecode and %q disagree on %d probes, first %+v\n  bc: % x",
					i, w.predSource, len(miss), miss[0], r.Bytecode))
			case w.bug != "" && len(miss) == 0:
				errs = append(errs, fmt.Errorf("O3b: request %d: iproute2 bug %s is gone; drop the row's bug fact", i, w.bug))
			}
			bc := r.Bytecode
			passes = func(e *InetDiagEntry) bool { return InetDiagBcRun(bc, e) }
			if w.bug == "" {
				cg := ssBcCgroup(bc)
				passes = func(e *InetDiagEntry) bool { return w.pred.passes(e, cg) }
			}
		}
		if strings.Contains(w.predSource, "cgroup") {
			continue // the holder's cgroup id is unknown to the model
		}
		// raw_diag's dump ignores idiag_states (net/ipv4/raw_diag.c
		// raw_diag_dump); ss drops the states it did not ask for itself
		// (ss.c:4330).
		mask := w.states
		if w.proto == unix.IPPROTO_RAW {
			mask = ^uint32(0)
		}
		if i >= len(replies) {
			errs = append(errs, fmt.Errorf("O4: request %d has no replies slot", i))
			continue
		}
		if err := ssRoundTripProblem(w.proto, w.families[i], mask, replies[i], passes); err != nil {
			errs = append(errs, fmt.Errorf("O4: request %d (family %d): %w", i, w.families[i], err))
			continue
		}
		roundTrips++
	}
	return errs, checked, roundTrips
}

func TestWsSsCorpus(t *testing.T) {
	var checked, roundTrips int
	for _, dir := range wsRows(t) {
		row := wsLoadRow(t, dir)
		if !strings.HasPrefix(row.ID, "ss/") {
			continue
		}
		t.Run(row.Env.Kernel+"/"+row.ID, func(t *testing.T) {
			if row.Result.Status != "OK" {
				return // TestWsCorpusRows fails it
			}
			c, rt := wsSsRow(t, dir, row)
			checked += c
			roundTrips += rt
		})
	}
	t.Logf("O3 checked %d ss requests, O4 %d round trips", checked, roundTrips)
	if checked == 0 || roundTrips == 0 {
		t.Error("O3 or O4 checked nothing: are there ss rows under testdata/*/ws?")
	}
}

func TestSsParsePred(t *testing.T) {
	tests := []struct {
		description string
		pred        string
		expected    string // "" for a nil pred, "error" for a refusal, else the root's op/key
	}{
		{description: "positive: an empty pred is no filter", pred: "", expected: ""},
		{description: "positive: a port atom", pred: "sport>=1024", expected: "atom/sport"},
		{description: "positive: a host with a port", pred: "src=127.0.0.1/32:22", expected: "atom/src"},
		{description: "positive: nested and/or/not", pred: "and(or(sport=22,sport=443),not(dst=127.0.0.1/32))", expected: "and/"},
		{description: "positive: localhost", pred: "src=localhost", expected: "atom/src"},
		{description: "positive: a mark", pred: "mark=16/240", expected: "atom/mark"},
		{description: "positive: auto", pred: "auto", expected: "atom/auto"},
		{description: "negative: an unknown atom", pred: "inode=5", expected: "error"},
		{description: "negative: an address compared with >", pred: "src>127.0.0.1/32", expected: "error"},
		{description: "negative: a host without a prefix length", pred: "src=127.0.0.1", expected: "error"},
		{description: "negative: not with two operands", pred: "not(auto,auto)", expected: "error"},
		{description: "negative: an unclosed and", pred: "and(auto,auto", expected: "error"},
		{description: "negative: a mark without a mask", pred: "mark=16", expected: "error"},
		{description: "boundary: an empty operand", pred: "or(auto,)", expected: "error"},
		{description: "corner: a port past 65535 parses", pred: "sport=65536", expected: "atom/sport"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			p, err := ssParsePred(tc.pred)
			got := "error"
			switch {
			case err != nil:
			case p == nil:
				got = ""
			default:
				got = p.op + "/" + p.key
			}
			if got != tc.expected {
				t.Errorf("got %q (err %v), want %q", got, err, tc.expected)
			}
		})
	}
}

func TestSsPredPasses(t *testing.T) {
	lo22 := wsSsEntry("127.0.0.1", 22, "0.0.0.0", 0, lockAddrPort, 0)
	mapped := wsSsEntry("::ffff:127.0.0.1", 443, "::ffff:127.0.0.1", 45000, 0, 0x10)
	v6 := wsSsEntry("::1", 80, "::", 0, lockAddrPort, 0)
	tests := []struct {
		description string
		pred        string
		entry       InetDiagEntry
		expected    bool
	}{
		{description: "positive: no filter passes", pred: "", entry: lo22, expected: true},
		{description: "positive: sport=22", pred: "sport=22", entry: lo22, expected: true},
		{description: "negative: sport!=22", pred: "sport!=22", entry: lo22},
		{description: "boundary: sport<=22", pred: "sport<=22", entry: lo22, expected: true},
		{description: "boundary: sport<22", pred: "sport<22", entry: lo22},
		{description: "boundary: sport>21", pred: "sport>21", entry: lo22, expected: true},
		{description: "boundary: dport>=0", pred: "dport>=0", entry: lo22, expected: true},
		{description: "corner: sport=65536 passes nothing", pred: "sport=65536", entry: wsSsEntry("127.0.0.1", 0, "0.0.0.0", 0, 0, 0)},
		{description: "positive: a v4 prefix takes a v4-mapped socket", pred: "src=127.0.0.0/8", entry: mapped, expected: true},
		{description: "negative: a v6 prefix refuses a v4 socket", pred: "src=::/0", entry: lo22},
		{description: "negative: a v4 /0 refuses a plain v6 socket", pred: "src=0.0.0.0/0", entry: v6},
		{description: "positive: localhost takes ::1", pred: "src=localhost", entry: v6, expected: true},
		{description: "negative: a host port that differs", pred: "src=127.0.0.1/32:80", entry: lo22},
		{description: "positive: auto on an unlocked socket", pred: "auto", entry: mapped, expected: true},
		{description: "negative: auto on a bound socket", pred: "auto", entry: lo22},
		{description: "positive: mark 16/240 on 0x10", pred: "mark=16/240", entry: mapped, expected: true},
		{description: "negative: mark 16/4294967295 on 0", pred: "mark=16/4294967295", entry: lo22},
		{description: "positive: or, second", pred: "or(sport=80,sport=22)", entry: lo22, expected: true},
		{description: "negative: and, second fails", pred: "and(sport=22,dport=1)", entry: lo22},
		{description: "positive: not", pred: "not(sport=80)", entry: lo22, expected: true},
		{description: "corner: dev=lo is ifindex 1", pred: "dev=lo", entry: InetDiagEntry{Ifindex: 1}, expected: true},
		{description: "corner: cgroup compares the given id", pred: "cgroup=/sys/fs/cgroup", entry: InetDiagEntry{CgroupID: 7}, expected: true},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			p, err := ssParsePred(tc.pred)
			if err != nil {
				t.Fatal(err)
			}
			if got := p.passes(&tc.entry, 7); got != tc.expected {
				t.Errorf("got %v, want %v", got, tc.expected)
			}
		})
	}
}

func TestSsWantFromAxes(t *testing.T) {
	base := func(kv ...string) map[string]string {
		ax := map[string]string{"proto": "6", "families": "2,10", "states": "15159", "ext": "0", "pred": "sport=22"}
		for i := 0; i+1 < len(kv); i += 2 {
			ax[kv[i]] = kv[i+1]
		}
		return ax
	}
	tests := []struct {
		description string
		axes        map[string]string
		expected    string // "families bytecode probes", or "error"
	}{
		{description: "positive: tcp, both families, a filter", axes: base(), expected: "[2 10] true 0"},
		{description: "positive: -4", axes: base("families", "2"), expected: "[2] true 0"},
		{description: "positive: no filter, no bytecode", axes: base("pred", ""), expected: "[2 10] false 0"},
		{description: "positive: a filter ss applies itself", axes: base("pred", "dev=lo", "bc", "none"), expected: "[2 10] false 0"},
		{description: "positive: two cgroup probes", axes: base("probes", "2"), expected: "[2 10] true 2"},
		{description: "negative: no proto", axes: base("proto", ""), expected: "error"},
		{description: "negative: a bad family", axes: base("families", "2,x"), expected: "error"},
		{description: "negative: a bad pred", axes: base("pred", "nosuch=1"), expected: "error"},
		{description: "boundary: ext over 255", axes: base("ext", "256"), expected: "error"},
		{description: "corner: bad probes", axes: base("probes", "two"), expected: "error"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			w, err := ssWantFromAxes(tc.axes)
			got := "error"
			if err == nil {
				got = fmt.Sprintf("%v %v %d", w.families, w.bytecode, w.probes)
			}
			if got != tc.expected {
				t.Errorf("got %q (err %v), want %q", got, err, tc.expected)
			}
		})
	}
}

func TestSsRequestProblem(t *testing.T) {
	sport22 := bcHostB(t, InetDiagBcSCond, afUnspec, 0, 22, make([]byte, 4))
	want := ssWant{proto: 6, families: []uint8{afInet}, states: 0x3b37, bytecode: true}
	good := InetDiagRequest{Family: afInet, Protocol: 6, States: 0x3b37, Bytecode: sport22, HasBytecode: true}
	with := func(f func(r *InetDiagRequest)) InetDiagRequest {
		r := good
		f(&r)
		return r
	}
	mptcp := ssWant{proto: 262, families: []uint8{afInet}, states: 0x3b37}
	tests := []struct {
		description string
		w           ssWant
		r           InetDiagRequest
		expected    string // a substring of the problem; "" for none
	}{
		{description: "positive: as wanted", w: want, r: good},
		{description: "positive: MPTCP's 262 truncated to 6 plus the attribute", w: mptcp,
			r: InetDiagRequest{Family: afInet, Protocol: 6, States: 0x3b37, ProtocolAttr: 262, HasProtocolAttr: true}},
		{description: "negative: MPTCP without the attribute", w: mptcp,
			r: InetDiagRequest{Family: afInet, Protocol: 6, States: 0x3b37}, expected: "protocol 6, want 262"},
		{description: "negative: the wrong family", w: want, r: with(func(r *InetDiagRequest) { r.Family = afInet6 }), expected: "family 10"},
		{description: "negative: states byte-swapped", w: want, r: with(func(r *InetDiagRequest) { r.States = 0x373b0000 }), expected: "states"},
		{description: "negative: ext bits", w: want, r: with(func(r *InetDiagRequest) { r.Ext = 1 }), expected: "ext"},
		{description: "negative: bytecode missing", w: want, r: with(func(r *InetDiagRequest) { r.HasBytecode, r.Bytecode = false, nil }), expected: "bytecode present false"},
		{description: "negative: bytecode the audit refuses", w: want, r: with(func(r *InetDiagRequest) { r.Bytecode = bcOpB(14, 4, 8) }), expected: "unknown code"},
		{description: "corner: a stray protocol attribute", w: want, r: with(func(r *InetDiagRequest) { r.HasProtocolAttr, r.ProtocolAttr = true, 6 }), expected: "INET_DIAG_REQ_PROTOCOL"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			err := ssRequestProblem(tc.w, tc.w.families[0], &tc.r)
			if tc.expected == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.expected) {
				t.Fatalf("err %v, want it to contain %q", err, tc.expected)
			}
		})
	}
}

func TestSsCgroupProbeProblem(t *testing.T) {
	probe := binary.LittleEndian.AppendUint64([]byte{InetDiagBcCgroupCond, 12, 16, 0}, 0)
	tests := []struct {
		description string
		r           InetDiagRequest
		expected    bool // a problem
	}{
		{description: "positive: the probe", r: InetDiagRequest{Family: afInet, Protocol: 6, Bytecode: probe, HasBytecode: true}},
		{description: "negative: a real dump's states", r: InetDiagRequest{Family: afInet, Protocol: 6, States: 0x3b37, Bytecode: probe}, expected: true},
		{description: "negative: AF_INET6", r: InetDiagRequest{Family: afInet6, Protocol: 6, Bytecode: probe}, expected: true},
		{description: "boundary: cgroup id 1, not 0", r: InetDiagRequest{Family: afInet, Protocol: 6, Bytecode: bcCgroupB(1)}, expected: true},
		{description: "corner: no bytecode", r: InetDiagRequest{Family: afInet, Protocol: 6}, expected: true},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if err := ssCgroupProbeProblem(&tc.r); (err != nil) != tc.expected {
				t.Errorf("err %v, want a problem %v", err, tc.expected)
			}
		})
	}
}

// ssReplyMsg builds a SOCK_DIAG_BY_FAMILY reply for k, with a mark
// attribute when mark is not 0.
func ssReplyMsg(k *wsSsSock, state uint8, mark uint32) capturedNlMsg {
	b := make([]byte, InetDiagMsgSizeCst)
	b[0], b[1] = uint8(k.e.Family), state
	binary.BigEndian.PutUint16(b[4:], k.e.Sport)
	binary.BigEndian.PutUint16(b[6:], k.e.Dport)
	copy(b[8:24], k.e.Saddr)
	copy(b[24:40], k.e.Daddr)
	if mark != 0 {
		b = append(b, 8, 0, inetDiagMarkAttr, 0)
		b = binary.LittleEndian.AppendUint32(b, mark)
	}
	return capturedNlMsg{proto: unix.NETLINK_SOCK_DIAG, h: NlMsgHdr{Type: sockDiagByFamily}, body: b}
}

func TestSsRoundTripProblem(t *testing.T) {
	var tcp4 []*wsSsSock
	for i := range wsSsKnown {
		if k := &wsSsKnown[i]; k.proto == 6 && k.e.Family == afInet {
			tcp4 = append(tcp4, k)
		}
	}
	all := func(*InetDiagEntry) bool { return true }
	sport22 := func(e *InetDiagEntry) bool { return e.Sport == 22 }
	listen := uint32(1 << tcpListen)
	replies := func(ks ...*wsSsSock) []capturedNlMsg {
		out := make([]capturedNlMsg, 0, len(ks))
		for _, k := range ks {
			out = append(out, ssReplyMsg(k, k.state, k.e.Mark))
		}
		return out
	}
	var listeners []*wsSsSock
	for _, k := range tcp4 {
		if k.state == tcpListen {
			listeners = append(listeners, k)
		}
	}
	stranger := &wsSsSock{proto: 6, state: tcpListen, e: wsSsEntry("127.0.0.1", 7, "0.0.0.0", 0, 0, 0)}
	tests := []struct {
		description string
		msgs        []capturedNlMsg
		states      uint32
		passes      func(*InetDiagEntry) bool
		expected    string // a substring of the problem; "" for none
	}{
		{description: "positive: every v4 listener", msgs: replies(listeners...), states: listen, passes: all},
		{description: "positive: sport 22 only", msgs: replies(tcp4[0]), states: listen, passes: sport22},
		{description: "negative: a listener missing", msgs: replies(listeners[1:]...), states: listen, passes: all, expected: "not listed"},
		{description: "negative: a socket the filter refuses", msgs: replies(listeners...), states: listen, passes: sport22, expected: "refuses"},
		{description: "negative: a socket the holder never opened", msgs: replies(append(listeners, stranger)...), states: listen, passes: all, expected: "never opened"},
		{description: "negative: a state outside the mask", msgs: replies(listeners...), states: 1 << tcpEstablished, passes: all, expected: "state 10"},
		{description: "boundary: no replies, nothing passes", msgs: nil, states: listen, passes: func(*InetDiagEntry) bool { return false }},
		{description: "corner: the marked listener without its mark", msgs: append(replies(listeners[:len(listeners)-1]...),
			ssReplyMsg(listeners[len(listeners)-1], tcpListen, 0)), states: listen, passes: all, expected: "mark 0x0, want 0x10"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			err := ssRoundTripProblem(6, afInet, tc.states, tc.msgs, tc.passes)
			if tc.expected == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.expected) {
				t.Fatalf("err %v, want it to contain %q", err, tc.expected)
			}
		})
	}
}

func TestWsSsExchanges(t *testing.T) {
	req := capturedNlMsg{proto: unix.NETLINK_SOCK_DIAG, h: NlMsgHdr{Type: sockDiagByFamily, Flags: unix.NLM_F_REQUEST}}
	rep := capturedNlMsg{proto: unix.NETLINK_SOCK_DIAG, h: NlMsgHdr{Type: sockDiagByFamily}}
	done := capturedNlMsg{proto: unix.NETLINK_SOCK_DIAG, h: NlMsgHdr{Type: unix.NLMSG_DONE}}
	route := capturedNlMsg{proto: unix.NETLINK_ROUTE, h: NlMsgHdr{Type: sockDiagByFamily, Flags: unix.NLM_F_REQUEST}}
	tests := []struct {
		description string
		msgs        []capturedNlMsg
		expected    []int // replies per request
	}{
		{description: "positive: two requests, their replies", msgs: []capturedNlMsg{req, rep, rep, done, req, rep, done}, expected: []int{2, 1}},
		{description: "boundary: a request with no replies", msgs: []capturedNlMsg{req, done}, expected: []int{0}},
		{description: "negative: route messages are not sock_diag", msgs: []capturedNlMsg{route, rep}, expected: nil},
		{description: "corner: a reply before any request is dropped", msgs: []capturedNlMsg{rep, req, rep}, expected: []int{1}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			reqs, replies := wsSsExchanges(tc.msgs)
			got := make([]int, 0, len(reqs))
			for i := range reqs {
				got = append(got, len(replies[i]))
			}
			if fmt.Sprint(got) != fmt.Sprint(tc.expected) {
				t.Errorf("got %v, want %v", got, tc.expected)
			}
		})
	}
}

func TestSsExchangeProblems(t *testing.T) {
	sport22 := bcHostB(t, InetDiagBcSCond, afUnspec, 0, 22, make([]byte, 4))
	sport80 := bcHostB(t, InetDiagBcSCond, afUnspec, 0, 80, make([]byte, 4))
	probe := InetDiagRequest{Family: afInet, Protocol: 6, Bytecode: bcCgroupB(0), HasBytecode: true}
	req := func(family uint8, bc []byte) InetDiagRequest {
		return InetDiagRequest{Family: family, Protocol: 6, States: 1 << tcpListen, Bytecode: bc, HasBytecode: bc != nil}
	}
	pred, err := ssParsePred("sport=22")
	if err != nil {
		t.Fatal(err)
	}
	w := ssWant{proto: 6, families: []uint8{afInet, afInet6}, states: 1 << tcpListen, pred: pred, bytecode: true, predSource: "sport=22"}
	listener := ssReplyMsg(&wsSsKnown[0], tcpListen, 0) // 127.0.0.1:22
	good := [][]capturedNlMsg{{listener}, nil}
	withBug := w
	withBug.bug = "ss-test"
	cg, err := ssParsePred("cgroup=/sys/fs/cgroup")
	if err != nil {
		t.Fatal(err)
	}
	wcg := ssWant{proto: 6, families: []uint8{afInet}, states: 1 << tcpListen, pred: cg, bytecode: true,
		predSource: "cgroup=/sys/fs/cgroup", probes: 1}
	tests := []struct {
		description string
		w           ssWant
		reqs        []InetDiagRequest
		replies     [][]capturedNlMsg
		expected    string // a substring of the first problem; "" for none
		roundTrips  int
	}{
		{description: "positive: both families, the listener on 22", w: w,
			reqs: []InetDiagRequest{req(afInet, sport22), req(afInet6, sport22)}, replies: good, roundTrips: 2},
		{description: "negative: the families out of order", w: w,
			reqs: []InetDiagRequest{req(afInet6, sport22), req(afInet, sport22)}, replies: good, expected: "family 10, want 2"},
		{description: "negative: bytecode that tests another port", w: w,
			reqs: []InetDiagRequest{req(afInet, sport80), req(afInet6, sport80)}, replies: [][]capturedNlMsg{nil, nil}, expected: "disagree",
			roundTrips: 1}, // O4 still holds the replies to the pred: 22 is missing from AF_INET only
		{description: "negative: a bug row whose bytecode is now right", w: withBug,
			reqs: []InetDiagRequest{req(afInet, sport22), req(afInet6, sport22)}, replies: good, expected: "is gone", roundTrips: 2},
		{description: "negative: one request short", w: w,
			reqs: []InetDiagRequest{req(afInet, sport22)}, replies: good[:1], expected: "want 0 cgroup probes"},
		{description: "positive: a cgroup row's probe, then its dump; O4 skipped", w: wcg,
			reqs: []InetDiagRequest{probe, req(afInet, bcCgroupB(1))}, replies: [][]capturedNlMsg{nil, nil}},
		{description: "negative: a probe that is not the probe's shape", w: wcg,
			reqs: []InetDiagRequest{req(afInet, bcCgroupB(1)), req(afInet, bcCgroupB(1))}, replies: [][]capturedNlMsg{nil, nil},
			expected: "not the cgroup probe"},
		{description: "corner: a listed socket the filter refuses", w: w,
			reqs:    []InetDiagRequest{req(afInet, sport22), req(afInet6, sport22)},
			replies: [][]capturedNlMsg{{listener, ssReplyMsg(&wsSsKnown[2], tcpListen, 0)}, nil}, expected: "refuses", roundTrips: 1},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			errs, _, rt := ssExchangeProblems(tc.w, tc.reqs, tc.replies)
			switch {
			case tc.expected == "" && len(errs) > 0:
				t.Fatalf("problems: %v", errs)
			case tc.expected != "" && (len(errs) == 0 || !strings.Contains(errs[0].Error(), tc.expected)):
				t.Fatalf("problems %v, want the first to contain %q", errs, tc.expected)
			}
			if rt != tc.roundTrips {
				t.Errorf("%d round trips, want %d", rt, tc.roundTrips)
			}
		})
	}
}
