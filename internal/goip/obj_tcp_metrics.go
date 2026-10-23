package goip

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// `ip tcp_metrics` is do_tcp_metrics and tcpm_do_cmd (ip/tcp_metrics.c): the
// argument loop first, with nothing sent, then genl_init_handle's family
// lookup, then one of four requests of the tcp_metrics family:
//
//   - show, with no exact address: a GET dump, filtered by process_msg on
//     the family, the address prefix and the source prefix;
//   - show with an exact address: one GET carrying it (and the source), and
//     its one reply printed through the same filter;
//   - delete, or flush of an exact address: one DEL with NLM_F_ACK;
//   - flush of everything (no address, no source, no -4 or -6): one DEL with
//     no attributes and NLM_F_ACK;
//   - any other flush: rounds of the dump whose kept replies process_msg
//     copies into DELs, NLM_F_REQUEST alone, at the next seq each, in as many
//     3584-byte datagrams as they need, until a dump keeps nothing.
//
// goip sends the lookup and the GETs and writes each DEL to GOIP_WRITE_OUT.
// A flush's round 2 skips every entry round 1 copied, as route and neigh
// flush do, so it ends where `ip`'s, whose kernel deleted them, keeps
// nothing.

const (
	tcpmCmdList  = 1 << iota // list, lst, show
	tcpmCmdDel               // delete, remove
	tcpmCmdFlush             // flush

	tcpmSrcCst     = "src"
	tcpmSourceCst  = "source"
	tcpmAddressCst = "address"
	tcpmHelpCst    = "help"
	// tcpmBufCst is TCPM_REQUEST's buffer for the GET and DEL, ample for
	// two addresses.
	tcpmBufCst = 128
)

// tcpmCmds is cmds[], matched with matches() in its order.
var tcpmCmds = []struct {
	name string
	code int
}{
	{listVerbCst, tcpmCmdList},
	{lstVerbCst, tcpmCmdList},
	{showVerbCst, tcpmCmdList},
	{"delete", tcpmCmdDel},
	{"remove", tcpmCmdDel},
	{routeFlushVerbCst, tcpmCmdFlush},
}

var (
	// ErrTCPMetricsDumpTerminated is a dump or a reply process_msg refuses
	// inside `ip tcp_metrics`: "Dump terminated" or "Flush terminated", exit 1.
	ErrTCPMetricsDumpTerminated = errors.New("dump terminated")
	// ErrTCPMetricsUsage is a refusal tcp_metrics.c ends with exit(-1), 255:
	// an unknown verb (do_tcp_metrics), and in the loop duparg2,
	// incomplete_command and, after it, missarg.
	ErrTCPMetricsUsage = errors.New("tcp_metrics")
)

// tcpmUsage marks err as one of ErrTCPMetricsUsage's refusals.
func tcpmUsage(err error) error {
	return fmt.Errorf("%w: %w", ErrTCPMetricsUsage, err)
}

// runTCPMetrics is do_tcp_metrics: no verb is the listing.
func runTCPMetrics(c *runCtx, args []string) error {
	if len(args) == 0 {
		return tcpmDoCmd(c, tcpmCmdList, nil)
	}
	for _, cmd := range tcpmCmds {
		if matchesPrefix(args[0], cmd.name) {
			return tcpmDoCmd(c, cmd.code, args[1:])
		}
	}
	if matchesPrefix(args[0], tcpmHelpCst) {
		return fmt.Errorf("tcp_metrics help: %w", ErrNotImplemented)
	}
	return tcpmUsage(fmt.Errorf("%w: Command %q is unknown, try \"ip tcp_metrics help\"", ErrInvalidArgument, args[0]))
}

// tcpmFilter is tcpm_do_cmd's f: the address and source prefixes, bitlen -1
// when not given, and the attribute types of the exact ones, -1 otherwise.
type tcpmFilter struct {
	daddr, saddr inetPrefix
	atype, stype int
}

// tcpmParse is tcpm_do_cmd's loop. Each pass steps past its word twice, once
// in the body and once in the for, so after an address or a source and its
// value the next word is skipped: `address A src B` reads B as a second
// address, a duplicate. matches(*argv, "help") is usage.
func tcpmParse(cmd int, family uint8, args []string) (tcpmFilter, error) {
	f := tcpmFilter{
		daddr: inetPrefix{family: family, bitlen: -1},
		saddr: inetPrefix{family: family, bitlen: -1},
		atype: -1, stype: -1,
	}
	for i := 0; i < len(args); i += 2 {
		var err error
		if args[i] == tcpmSrcCst || args[i] == tcpmSourceCst {
			who := args[i]
			if i, err = nextArg(args, i); err != nil {
				return f, tcpmUsage(err)
			}
			if err = tcpmPrefix(&f.saddr, &f.stype, who, args[i], family, true); err != nil {
				return f, err
			}
			continue
		}
		who := tcpmAddressCst
		if args[i] == "addr" || args[i] == tcpmAddressCst {
			who = args[i]
			if i, err = nextArg(args, i); err != nil {
				return f, tcpmUsage(err)
			}
		}
		if err = tcpmPrefix(&f.daddr, &f.atype, who, args[i], family, cmd&tcpmCmdDel != 0); err != nil {
			return f, err
		}
	}
	return f, nil
}

// tcpmPrefix reads one prefix into p: help is usage, a second one duparg2,
// then get_prefix, and an exact address sets its attribute type. exact says
// whether anything else is refused: always for a source, and for an address
// under delete.
func tcpmPrefix(p *inetPrefix, typ *int, who, arg string, family uint8, exact bool) error {
	if matchesPrefix(arg, tcpmHelpCst) {
		return fmt.Errorf("tcp_metrics help: %w", ErrNotImplemented)
	}
	if p.bitlen >= 0 {
		return tcpmUsage(duparg2Err(who, arg))
	}
	got, err := routeGetPrefix(arg, family)
	if err != nil {
		return err
	}
	*p = got
	if n := len(p.data); n != 0 && n*8 == p.bitlen {
		source := who == tcpmSrcCst || who == tcpmSourceCst
		switch {
		case p.family == unix.AF_INET && source:
			*typ = int(xtcpnl.TCPMetricsAttrSaddrIPv4)
		case p.family == unix.AF_INET6 && source:
			*typ = int(xtcpnl.TCPMetricsAttrSaddrIPv6)
		case p.family == unix.AF_INET:
			*typ = int(xtcpnl.TCPMetricsAttrAddrIPv4)
		case p.family == unix.AF_INET6:
			*typ = int(xtcpnl.TCPMetricsAttrAddrIPv6)
		}
	}
	if exact && *typ < 0 {
		return fmt.Errorf("%w: Error: a specific IP address is expected rather than %q", ErrInvalidArgument, arg)
	}
	return nil
}

// tcpmDoCmd is tcpm_do_cmd after the family check: the loop, the choice of
// request, the lookup, then the request.
func tcpmDoCmd(c *runCtx, cmd int, args []string) error {
	switch c.family {
	case unix.AF_UNSPEC, unix.AF_INET, unix.AF_INET6:
	default:
		return fmt.Errorf("%w: Unsupported protocol family: %d", ErrInvalidArgument, c.family)
	}
	f, err := tcpmParse(cmd, c.family, args)
	if err != nil {
		return err
	}
	if cmd == tcpmCmdDel && f.atype < 0 {
		return tcpmUsage(fmt.Errorf("%w: Error: argument \"address\" is required", ErrInvalidArgument))
	}
	if cmd == tcpmCmdFlush && f.atype >= 0 {
		cmd = tcpmCmdDel
	}
	if cmd == tcpmCmdFlush && f.daddr.bitlen <= 0 && f.saddr.bitlen <= 0 && c.family == unix.AF_UNSPEC {
		cmd = tcpmCmdDel
	}

	id, err := c.genlFamily(xtcpnl.TCPMetricsGenlName)
	if err != nil {
		return err
	}
	switch {
	case cmd == tcpmCmdDel:
		return tcpmDelete(c, id, &f)
	case cmd == tcpmCmdFlush:
		return tcpmFlush(c, id, &f)
	case f.atype >= 0:
		return tcpmGet(c, id, &f)
	}
	return tcpmShow(c, id, &f)
}

// tcpmAttrs is the request's attributes: the exact address, then the
// exact source, each that was given.
func tcpmAttrs(f *tcpmFilter) ([]byte, error) {
	var raw [tcpmBufCst]byte
	b := xtcpnl.NewAttrBuilder(raw[:])
	if f.atype >= 0 {
		if err := b.PutBytes(uint16(f.atype), f.daddr.data); err != nil {
			return nil, err
		}
	}
	if f.stype >= 0 {
		if err := b.PutBytes(uint16(f.stype), f.saddr.data); err != nil {
			return nil, err
		}
	}
	return b.Bytes(), nil
}

// tcpmDelete is the ACKed DEL, written to GOIP_WRITE_OUT: an exact address
// and source, or none at all for flush everything.
func tcpmDelete(c *runCtx, id uint16, f *tcpmFilter) error {
	attrs, err := tcpmAttrs(f)
	if err != nil {
		return err
	}
	req, err := xtcpnl.BuildGenlWriteRequest(xtcpnl.TCPMetricsGenlName, id, xtcpnl.TCPMetricsCmdDel,
		xtcpnl.TCPMetricsGenlVersion, unix.NLM_F_ACK, c.nextSeq(), attrs)
	if err != nil {
		return err
	}
	return writeAddrOut(req)
}

// tcpmGet is the GET of one exact address, rtnl_talk with an answer: a
// refusal is the kernel's errno, exit 2, and the answer is printed through
// process_msg. new_json_obj is never called on this path, so -j prints the
// line.
func tcpmGet(c *runCtx, id uint16, f *tcpmFilter) error {
	gs, err := genlSource(c)
	if err != nil {
		return err
	}
	attrs, err := tcpmAttrs(f)
	if err != nil {
		return err
	}
	req, err := xtcpnl.BuildGenlRequest(xtcpnl.TCPMetricsGenlName, id, xtcpnl.TCPMetricsCmdGet,
		xtcpnl.TCPMetricsGenlVersion, 0, c.nextSeq(), attrs)
	if err != nil {
		return err
	}
	typ, body, err := gs.GenlTalk(req)
	if err != nil {
		return fmt.Errorf("RTNETLINK answers: %w", err)
	}
	if typ != id {
		return fmt.Errorf("%w: a reply of type %d", ErrTCPMetricsDumpTerminated, typ)
	}
	m, keep, err := tcpmKeep(body, f)
	if err != nil || !keep {
		return err
	}
	v := render.TCPMetricsViewOf(&m)
	_, err = fmt.Fprint(c.out, v.Text())
	return err
}

// tcpmDump sends one GET dump and returns its replies' bodies.
func tcpmDump(c *runCtx, id uint16) ([][]byte, error) {
	gs, err := genlSource(c)
	if err != nil {
		return nil, err
	}
	req, err := xtcpnl.BuildGenlRequest(xtcpnl.TCPMetricsGenlName, id, xtcpnl.TCPMetricsCmdGet,
		xtcpnl.TCPMetricsGenlVersion, unix.NLM_F_DUMP, c.nextSeq(), nil)
	if err != nil {
		return nil, err
	}
	return gs.GenlDump(req)
}

// tcpmShow is the dump path: every kept entry's line, or under -j one
// array of their objects.
func tcpmShow(c *runCtx, id uint16, f *tcpmFilter) error {
	bodies, err := tcpmDump(c, id)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrTCPMetricsDumpTerminated, err)
	}
	views := make([]render.TCPMetricsView, 0, len(bodies))
	for _, body := range bodies {
		m, keep, err := tcpmKeep(body, f)
		if err != nil {
			return err
		}
		if keep {
			views = append(views, render.TCPMetricsViewOf(&m))
		}
	}
	if c.json {
		return json.NewEncoder(c.out).Encode(views)
	}
	for i := range views {
		if _, err := fmt.Fprint(c.out, views[i].Text()); err != nil {
			return err
		}
	}
	return nil
}

// tcpmKeep is process_msg's decode and filter. A reply of another command
// is skipped, and so is one without an address, or one outside the address
// or source prefix, or of another family than -4, -6 or a prefix names. A
// reply that does not decode ends the dump where process_msg would print
// what it could: goip does not render half an entry.
func tcpmKeep(body []byte, f *tcpmFilter) (xtcpnl.TCPMetrics, bool, error) {
	if len(body) >= 1 && body[0] != xtcpnl.TCPMetricsCmdGet {
		return xtcpnl.TCPMetrics{}, false, nil
	}
	m, err := xtcpnl.ParseTCPMetrics(body)
	if err != nil {
		return m, false, fmt.Errorf("%w: %w", ErrTCPMetricsDumpTerminated, err)
	}
	if !m.Addr.IsValid() || !tcpmMatch(&f.daddr, m.Addr) {
		return m, false, nil
	}
	if m.Saddr.IsValid() && !tcpmMatch(&f.saddr, m.Saddr) {
		return m, false, nil
	}
	return m, true, nil
}

// tcpmMatch is process_msg's test of one address: the filter's family, if
// any, then its prefix, if one was given.
func tcpmMatch(p *inetPrefix, a netip.Addr) bool {
	fam := uint8(unix.AF_INET6)
	if a.Is4() {
		fam = unix.AF_INET
	}
	if p.family != unix.AF_UNSPEC && p.family != fam {
		return false
	}
	return p.bitlen < 0 || inetPrefixMatch(p, a.AsSlice())
}

// tcpmFlush is the flush rounds. Round 1 copies each kept entry into a DEL;
// round 2 skips every entry already copied, so it keeps none and ends the
// flush. Nothing kept in round 1 is "Nothing to flush." on stderr, -s or
// not, and nothing written.
func tcpmFlush(c *runCtx, id uint16, f *tcpmFilter) error {
	copied := map[string]bool{}
	var buf flushBuf
	for round := 0; ; {
		bodies, err := tcpmDump(c, id)
		if err != nil {
			return fmt.Errorf("%w: Flush terminated: %w", ErrTCPMetricsDumpTerminated, err)
		}
		flushed := 0
		for _, body := range bodies {
			m, keep, err := tcpmKeep(body, f)
			if err != nil {
				return fmt.Errorf("%w: Flush terminated: %w", ErrTCPMetricsDumpTerminated, err)
			}
			key := m.Addr.String() + "/" + m.Saddr.String()
			if !keep || copied[key] {
				continue
			}
			if round == 0 && flushed == 0 {
				if path, ok := os.LookupEnv(writeOutEnvCst); !ok || path == "" {
					return ErrWriteNotApplied
				}
			}
			del, err := tcpmFlushDel(c, id, &m)
			if err != nil {
				return err
			}
			buf.add(del)
			copied[key] = true
			flushed++
		}
		if flushed == 0 {
			return tcpmFlushDone(c, round, buf.datagrams)
		}
		round++
		buf.end()
		if c.stats() {
			if _, err := fmt.Fprintf(c.out, "\n*** Round %d, deleting %d entries ***\n", round, flushed); err != nil {
				return err
			}
		}
	}
}

// tcpmFlushDel is process_msg's copy: a DEL of the entry's address and
// source, the attribute types the reply carried them in.
func tcpmFlushDel(c *runCtx, id uint16, m *xtcpnl.TCPMetrics) ([]byte, error) {
	var raw [tcpmBufCst]byte
	b := xtcpnl.NewAttrBuilder(raw[:])
	atype, stype := xtcpnl.TCPMetricsAttrAddrIPv6, xtcpnl.TCPMetricsAttrSaddrIPv6
	if m.Addr.Is4() {
		atype = xtcpnl.TCPMetricsAttrAddrIPv4
	}
	if m.Saddr.Is4() {
		stype = xtcpnl.TCPMetricsAttrSaddrIPv4
	}
	if err := b.PutBytes(atype, m.Addr.AsSlice()); err != nil {
		return nil, err
	}
	if m.Saddr.IsValid() {
		if err := b.PutBytes(stype, m.Saddr.AsSlice()); err != nil {
			return nil, err
		}
	}
	return xtcpnl.BuildGenlWriteRequest(xtcpnl.TCPMetricsGenlName, id, xtcpnl.TCPMetricsCmdDel,
		xtcpnl.TCPMetricsGenlVersion, 0, c.nextSeq(), b.Bytes())
}

// tcpmFlushDone ends the flush: nothing to flush on stderr in round 0, or
// the copies written and, under -s, the summary.
func tcpmFlushDone(c *runCtx, round int, datagrams [][]byte) error {
	if round == 0 {
		_, err := fmt.Fprint(c.errOut, "Nothing to flush.\n")
		return err
	}
	if err := writeAddrOut(joinDatagrams(datagrams)); err != nil {
		return err
	}
	if !c.stats() {
		return nil
	}
	return routeFlushDone(c.out, true, round)
}
