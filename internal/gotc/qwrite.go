//go:build gotc_write

// gotc_write builds gotc's qdisc and class writes. Without the tag gotc
// refuses every write (qwrite_off.go), as it did before increment 17.

package gotc

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// tc/tc_qdisc.c tc_qdisc_modify and tc/tc_class.c tc_class_modify at
// iproute2 v7.2.0, with m_estimator.c parse_estimator, tc_stab.c
// parse_size_table and the kinds' parse_qopt and parse_copt (qw_*.go).
//
// gotc never applies a write (gotc-status.md, increment 17: dry-run, as
// goip's decision 5). A replay source compares the request with tc's,
// byte for byte, and answers with the kernel's recorded ack; on the kernel
// socket the request goes to the file GOTC_WRITE_OUT names, and without it
// gotc refuses (ErrWriteNotApplied).

// ErrWriteNotApplied is a write with no replay and no GOTC_WRITE_OUT.
var ErrWriteNotApplied = errors.New("gotc never applies a write; set " + writeOutEnvCst +
	" to receive the request it would send")

// writeOutEnvCst names the file the requests of a run are written to, one
// after another.
const writeOutEnvCst = "GOTC_WRITE_OUT"

// errKindParse is a kind's parse_qopt or parse_copt returning non-zero:
// tc_qdisc_modify and tc_class_modify return 1 for it (W4).
var errKindParse = errors.New("gotc: the kind's parser failed")

// kindParser is a qdisc_util's parse_qopt or parse_copt: args are the
// words after the kind and dev is the device (`d`, "" for none). It
// returns what it would addattr, run later in tc's attribute order, or
// errKindParse (or an exitCode, for invarg and the like).
type kindParser func(r *runner, args []string, dev string) (func(*xtcpnl.AttrBuilder) error, error)

// qoptParsers are the parse_qopts gotc clones: increment 9's kinds. A nil
// parser is a qdisc_util with no parse_qopt. A kind in qdiscUtils outside
// the map is refused by name until its increment (18, 19).
var qoptParsers = map[string]kindParser{
	xtcpnl.TcKindPfifo:         fifoParser(xtcpnl.TcKindPfifo),
	xtcpnl.TcKindBfifo:         fifoParser(xtcpnl.TcKindBfifo),
	xtcpnl.TcKindPfifoHeadDrop: fifoParser(xtcpnl.TcKindPfifoHeadDrop),
	xtcpnl.TcKindPrio:          parsePrioQopt,
	xtcpnl.TcKindPfifoFast:     nil, // q_fifo.c's pfifo_fast_qdisc_util has no parse_qopt (W8)
	xtcpnl.TcKindFqCodel:       parseFqCodelQopt,
	xtcpnl.TcKindFq:            parseFqQopt,
	xtcpnl.TcKindHtb:           parseHtbQopt,
	xtcpnl.TcKindTbf:           parseTbfQopt,
	xtcpnl.TcKindSfq:           parseSfqQopt,
	xtcpnl.TcKindNetem:         parseNetemQopt,
	xtcpnl.TcKindIngress:       parseIngressQopt,
	xtcpnl.TcKindClsact:        parseClsactQopt,
}

// coptParsers are the parse_copts gotc clones. A kind whose qdisc_util has
// a parse_copt (printCopt stands for it) but is not here is refused.
var coptParsers = map[string]kindParser{
	xtcpnl.TcKindHtb: parseHtbCopt,
}

// Words the kind parsers take more than twice.
const (
	wordBuckets   = "buckets"
	wordDivisor   = "divisor"
	wordDuplicate = "duplicate"
	wordEstimator = "estimator"
	wordLatency   = "latency"
	wordLinklayer = "linklayer"
	wordMpu       = "mpu"
	wordMtu       = "mtu"
	wordOverhead  = "overhead"
	wordPeakrate  = "peakrate"
	wordPrio      = "prio"
	wordRate      = "rate"
)

// More words the kind parsers share.
const (
	wordBands    = "bands"
	wordBuffer   = "buffer"
	wordMaxburst = "maxburst"
)

// wordHandle is the qdisc and class handle keyword.
const wordHandle = "handle"

// truncKind is `k`, FILTER_NAMESZ: the kind's first 15 bytes.
func truncKind(s string) string { return cutName(s, filterNameSize-1) }

// qdiscWrite is do_qdisc's write verbs.
func (r *runner) qdiscWrite(verb string, args []string) error {
	switch {
	case matches(verb, "add"):
		return r.qdiscModify(unix.RTM_NEWQDISC, unix.NLM_F_EXCL|unix.NLM_F_CREATE, args)
	case matches(verb, "change"):
		return r.qdiscModify(unix.RTM_NEWQDISC, 0, args)
	case matches(verb, "replace"):
		return r.qdiscModify(unix.RTM_NEWQDISC, unix.NLM_F_CREATE|unix.NLM_F_REPLACE, args)
	case matches(verb, "link"):
		return r.qdiscModify(unix.RTM_NEWQDISC, unix.NLM_F_REPLACE, args)
	}
	return r.qdiscModify(unix.RTM_DELQDISC, 0, args)
}

// classWrite is do_class's write verbs: replace is CREATE alone.
func (r *runner) classWrite(verb string, args []string) error {
	switch {
	case matches(verb, "add"):
		return r.classModify(unix.RTM_NEWTCLASS, unix.NLM_F_EXCL|unix.NLM_F_CREATE, args)
	case matches(verb, "change"):
		return r.classModify(unix.RTM_NEWTCLASS, 0, args)
	case matches(verb, "replace"):
		return r.classModify(unix.RTM_NEWTCLASS, unix.NLM_F_CREATE, args)
	}
	return r.classModify(unix.RTM_DELTCLASS, 0, args)
}

// qmod is what the two modify loops gather.
type qmod struct {
	w    xtcpnl.TcWrite
	dev  string
	kind string
	q    *qdiscUtil // get_qdisc_kind's; nil when no kind was named
	rest []string   // the words after the kind
	stab sizeSpec
}

// qdiscModify is tc_qdisc_modify.
func (r *runner) qdiscModify(cmd uint16, flags int, args []string) error {
	m := qmod{w: xtcpnl.TcWrite{MsgType: cmd, Flags: uint16(flags | unix.NLM_F_ACK)}}
	if err := r.qdiscModifyArgs(&m, args); err != nil {
		return err
	}
	if err := r.modifyKind(&m, true); err != nil {
		return err
	}
	if m.stab.linklayer >= linklayerEthernet || m.stab.mpu != 0 || m.stab.overhead != 0 {
		data, ok := calcSizeTable(&m.stab)
		if !ok {
			return fmt.Errorf("%w: stab tsize %d with mtu %d (tc_calc_size_table never returns, W13)",
				errNotImplemented, m.stab.tsize, m.stab.mtu)
		}
		m.w.HasStab, m.w.StabData = true, data
		m.w.Stab = xtcpnl.TcSizespec{CellLog: m.stab.cellLog, SizeLog: m.stab.sizeLog, CellAlign: m.stab.cellAlign,
			Overhead: m.stab.overhead, Linklayer: m.stab.linklayer, Mpu: m.stab.mpu, Mtu: m.stab.mtu,
			Tsize: m.stab.tsize}
	}
	if m.dev != "" {
		idx, err := r.nameToIndex(m.dev)
		if err != nil {
			return err
		}
		if idx == 0 {
			return r.nodev(m.dev)
		}
		m.w.Ifindex = cint.Int(idx)
	}
	return r.sendWrite(&m.w)
}

// qdiscModifyArgs is tc_qdisc_modify's argument loop.
func (r *runner) qdiscModifyArgs(m *qmod, args []string) error {
	for len(args) > 0 {
		a := args[0]
		switch {
		case a == wordDev:
			if len(args) < 2 {
				return r.incomplete()
			}
			args = args[1:]
			if m.dev != "" {
				return r.duparg(wordDev, args[0])
			}
			m.dev = truncName(args[0])
		case a == wordHandle:
			if m.w.Handle != 0 {
				return r.duparg(wordHandle, a) // W3: duparg names the word "handle" itself
			}
			if len(args) < 2 {
				return r.incomplete()
			}
			args = args[1:]
			h, ok := qdiscHandle(args[0])
			if !ok {
				return r.invarg("invalid qdisc ID", args[0])
			}
			m.w.Handle = h
		case a == wordRoot:
			if m.w.Parent != 0 {
				r.errf("Error: \"root\" is duplicate parent ID\n")
				return retCode(-1)
			}
			m.w.Parent = tcHRoot
		case a == "clsact", a == wordIngress:
			return r.qdiscIngress(m, args)
		case a == wordParent:
			if len(args) < 2 {
				return r.incomplete()
			}
			args = args[1:]
			if m.w.Parent != 0 {
				return r.duparg(wordParent, args[0])
			}
			h, ok := tcClassid(args[0])
			if !ok {
				return r.invarg("invalid parent ID", args[0])
			}
			m.w.Parent = h
		case matches(a, wordEstimator):
			var err error
			if args, err = r.parseEstimator(args, &m.w.Est); err != nil {
				return err
			}
		case matches(a, "stab"):
			var err error
			if args, err = r.parseSizeTable(args, &m.stab); err != nil {
				return err
			}
			continue
		case matches(a, "ingress_block"), matches(a, "egress_block"):
			var err error
			if args, err = r.qdiscBlock(m, args); err != nil {
				return err
			}
		case matches(a, helpWord):
			r.qdiscUsage() // W1: and the loop goes on
		default:
			m.kind = truncKind(a)
			m.q = qdiscKindAny(m.kind)
			m.rest = args[1:]
			return nil
		}
		args = args[1:]
	}
	return nil
}

// qdiscIngress is the ingress and clsact arms: TC_H_CLSACT is
// TC_H_INGRESS; the handle an earlier `handle` set is overwritten, and
// NEXT_ARG_FWD; break hands the rest to the kind's parser (W2).
func (r *runner) qdiscIngress(m *qmod, args []string) error {
	a := args[0]
	if m.w.Parent != 0 {
		r.errf("Error: \"%s\" is a duplicate parent ID\n", a)
		return retCode(-1)
	}
	m.w.Parent = tcHIngress
	m.kind = a
	m.q = qdiscKindAny(a)
	m.w.Handle = tcHIngress & 0xffff0000 // TC_H_MAKE(TC_H_INGRESS, 0)
	m.rest = args[1:]
	return nil
}

// qdiscBlock is the ingress_block and egress_block arms: get_u32 base 0,
// and 0 is invalid. args is returned at the value.
func (r *runner) qdiscBlock(m *qmod, args []string) ([]string, error) {
	ingress := matches(args[0], "ingress_block")
	if len(args) < 2 {
		return nil, r.incomplete()
	}
	args = args[1:]
	v, ok := getU32(args[0], 0)
	if !ok || v == 0 {
		if ingress {
			return nil, r.invarg("invalid ingress block index value", args[0])
		}
		return nil, r.invarg("invalid egress block index value", args[0])
	}
	if ingress {
		m.w.IngressBlock = v
	} else {
		m.w.EgressBlock = v
	}
	return args, nil
}

// classModify is tc_class_modify.
func (r *runner) classModify(cmd uint16, flags int, args []string) error {
	m := qmod{w: xtcpnl.TcWrite{MsgType: cmd, Flags: uint16(flags | unix.NLM_F_ACK)}}
	if err := r.classModifyArgs(&m, args); err != nil {
		return err
	}
	if err := r.modifyKind(&m, false); err != nil {
		return err
	}
	if m.dev != "" {
		idx, err := r.nameToIndex(m.dev)
		if err != nil {
			return err
		}
		if idx == 0 {
			return r.nodev(m.dev)
		}
		m.w.Ifindex = cint.Int(idx)
	} else if err := r.initMap(); err != nil {
		return err
	}
	return r.sendWrite(&m.w)
}

// classModifyArgs is tc_class_modify's argument loop.
func (r *runner) classModifyArgs(m *qmod, args []string) error {
	for len(args) > 0 {
		a := args[0]
		switch {
		case a == wordDev:
			if len(args) < 2 {
				return r.incomplete()
			}
			args = args[1:]
			if m.dev != "" {
				return r.duparg(wordDev, args[0])
			}
			m.dev = truncName(args[0])
		case a == wordClassid:
			if len(args) < 2 {
				return r.incomplete()
			}
			args = args[1:]
			if m.w.Handle != 0 {
				return r.duparg(wordClassid, args[0])
			}
			h, ok := tcClassid(args[0])
			if !ok {
				return r.invarg("invalid class ID", args[0])
			}
			m.w.Handle = h
		case a == wordHandle:
			r.errf("Error: try \"classid\" instead of \"handle\"\n")
			return retCode(-1)
		case a == wordRoot:
			if m.w.Parent != 0 {
				r.errf("Error: \"root\" is duplicate parent ID.\n")
				return retCode(-1)
			}
			m.w.Parent = tcHRoot
		case a == wordParent:
			if len(args) < 2 {
				return r.incomplete()
			}
			args = args[1:]
			if m.w.Parent != 0 {
				return r.duparg(wordParent, args[0])
			}
			h, ok := tcClassid(args[0])
			if !ok {
				return r.invarg("invalid parent ID", args[0])
			}
			m.w.Parent = h
		case matches(a, wordEstimator):
			var err error
			if args, err = r.parseEstimator(args, &m.w.Est); err != nil {
				return err
			}
		case matches(a, helpWord):
			r.classUsage()
		default:
			m.kind = truncKind(a)
			m.q = qdiscKindAny(m.kind)
			m.rest = args[1:]
			return nil
		}
		args = args[1:]
	}
	return nil
}

// modifyKind is TCA_KIND and the kind's parser: parse_qopt for a qdisc,
// parse_copt for a class. get_qdisc_kind never returns NULL for a name (an
// unknown one gets parse_noqopt and no parse_copt), so the "Garbage
// instead of arguments" arm is reached only with no kind (W5).
func (r *runner) modifyKind(m *qmod, qdisc bool) error {
	m.w.Kind = m.kind
	if m.q == nil {
		if len(m.rest) > 0 {
			return r.garbage(m.rest[0], qdisc)
		}
		return nil
	}
	p, err := r.kindParserFor(m, qdisc)
	if err != nil || p == nil {
		return err
	}
	opts, err := p(r, m.rest, m.dev)
	if errors.Is(err, errKindParse) {
		return retCode(1)
	}
	if err != nil {
		return err
	}
	m.w.Opts = opts
	return nil
}

// kindParserFor finds the kind's parser, or says what tc does without one.
// A nil parser and nil error is parse_noqopt's empty success.
func (r *runner) kindParserFor(m *qmod, qdisc bool) (kindParser, error) {
	known := m.q.id != ""
	if qdisc {
		if p, ok := qoptParsers[m.kind]; ok {
			if p == nil && len(m.rest) > 0 {
				r.errf("qdisc '%s' does not support option parsing\n", m.kind)
				return nil, retCode(-1)
			}
			return p, nil
		}
		if known {
			return nil, fmt.Errorf("%w: tc qdisc write of kind %s", errNotImplemented, m.kind)
		}
		if len(m.rest) > 0 { // parse_noqopt
			r.errf("Unknown qdisc \"%s\", hence option \"%s\" is unparsable\n", m.kind, m.rest[0])
			return nil, retCode(1)
		}
		return nil, nil
	}
	if p, ok := coptParsers[m.kind]; ok {
		return p, nil
	}
	if known && m.q.printCopt != nil {
		return nil, fmt.Errorf("%w: tc class write of kind %s", errNotImplemented, m.kind)
	}
	r.errf("Error: Qdisc \"%s\" is classless.\n", m.kind)
	return nil, retCode(1)
}

// garbage is the "Garbage instead of arguments" arm, with the class one's
// missing newline (W5).
func (r *runner) garbage(word string, qdisc bool) error {
	if matches(word, helpWord) {
		if qdisc {
			r.qdiscUsage()
		} else {
			r.classUsage()
		}
	}
	if qdisc {
		r.errf("Garbage instead of arguments \"%s ...\". Try \"tc qdisc help\".\n", word)
	} else {
		r.errf("Garbage instead of arguments \"%s ...\". Try \"tc class help\".", word)
	}
	return retCode(-1)
}

// qdiscKindAny is get_qdisc_kind: the kind's qdisc_util, or for a name tc
// does not link, one with no id (parse_noqopt, no parse_copt).
func qdiscKindAny(kind string) *qdiscUtil {
	if q, ok := qdiscUtils[kind]; ok {
		return q
	}
	return &qdiscUtil{}
}

// parseEstimator is m_estimator.c parse_estimator: args starts at
// "estimator"; it returns args at the time constant, which the caller's
// step passes (W7).
func (r *runner) parseEstimator(args []string, est *xtcpnl.TcEstimator) ([]string, error) {
	if len(args) < 2 {
		return nil, r.incomplete()
	}
	args = args[1:]
	if est.EwmaLog != 0 {
		return nil, r.duparg(wordEstimator, args[0])
	}
	if matches(args[0], helpWord) {
		r.estHelp()
	}
	a, ok := getTime(args[0])
	if !ok {
		return nil, r.invarg(wordEstimator, "invalid estimator interval")
	}
	if len(args) < 2 {
		return nil, r.incomplete()
	}
	args = args[1:]
	if matches(args[0], helpWord) {
		r.estHelp()
	}
	tconst, ok := getTime(args[0])
	if !ok {
		return nil, r.invarg(wordEstimator, "invalid estimator time constant")
	}
	var e estimator
	if !setupEstimator(a, tconst, &e) {
		r.errf("Error: estimator parameters are out of range.\n")
		return nil, retCode(-1)
	}
	est.Interval, est.EwmaLog = e.interval, e.ewmaLog
	if r.raw != 0 {
		r.errf("[estimator i=%d e=%d]\n", e.interval, e.ewmaLog)
	}
	return args, nil
}

func (r *runner) estHelp() {
	r.errf("%s", "Usage: ... estimator INTERVAL TIME-CONST\n"+
		"  INTERVAL is interval between measurements\n"+
		"  TIME-CONST is averaging time constant\n"+
		"Example: ... est 1sec 8sec\n")
}

// parseSizeTable is tc_stab.c parse_size_table: args starts at "stab"; it
// returns args at the first word it does not take. `stab help` and a stab
// of only mtu or tsize (check_size_table_opts false) return -1, the second
// silently (W14).
func (r *runner) parseSizeTable(args []string, sp *sizeSpec) ([]string, error) {
	if len(args) < 2 {
		return nil, r.incomplete()
	}
	args = args[1:]
	if matches(args[0], helpWord) {
		r.stabHelp()
		return nil, retCode(-1)
	}
	var s sizeSpec
	for len(args) > 0 {
		a := args[0]
		var dup bool
		var err error
		switch {
		case matches(a, wordMtu):
			dup, err = r.stabU32(&args, &s.mtu, wordMtu, "invalid mtu")
		case matches(a, wordMpu):
			dup, err = r.stabU32(&args, &s.mpu, wordMpu, "invalid mpu")
		case matches(a, wordOverhead):
			if len(args) < 2 {
				return nil, r.incomplete()
			}
			args = args[1:]
			if s.overhead != 0 {
				return nil, r.duparg(wordOverhead, args[0])
			}
			v, ok := getInteger(args[0], 10)
			if !ok {
				return nil, r.invarg(wordOverhead, "invalid overhead")
			}
			s.overhead = v
		case matches(a, "tsize"):
			dup, err = r.stabU32(&args, &s.tsize, "tsize", "invalid table size")
		case matches(a, wordLinklayer):
			if len(args) < 2 {
				return nil, r.incomplete()
			}
			args = args[1:]
			if s.linklayer != 0 {
				return nil, r.duparg(wordLinklayer, args[0])
			}
			v, ok := getLinklayer(args[0])
			if !ok {
				return nil, r.invarg(wordLinklayer, "invalid linklayer")
			}
			s.linklayer = v
		default:
			if s.linklayer < linklayerEthernet && s.mpu == 0 && s.overhead == 0 {
				return nil, retCode(-1)
			}
			*sp = s
			return args, nil
		}
		if dup || err != nil {
			return nil, err
		}
		args = args[1:]
	}
	if s.linklayer < linklayerEthernet && s.mpu == 0 && s.overhead == 0 {
		return nil, retCode(-1)
	}
	*sp = s
	return args, nil
}

// stabU32 is one of parse_size_table's get_u32 options: NEXT_ARG, the
// duplicate check on a non-zero value, get_u32 base 10.
func (r *runner) stabU32(args *[]string, v *uint32, key, msg string) (bool, error) {
	if len(*args) < 2 {
		return true, r.incomplete()
	}
	*args = (*args)[1:]
	if *v != 0 {
		return true, r.duparg(key, (*args)[0])
	}
	u, ok := getU32((*args)[0], 10)
	if !ok {
		return true, r.invarg(key, msg)
	}
	*v = u
	return false, nil
}

func (r *runner) stabHelp() {
	r.errf("%s", "Usage: ... stab [ mtu BYTES ] [ tsize SLOTS ] [ mpu BYTES ]\n"+
		"                [ overhead BYTES ] [ linklayer TYPE ] ...\n"+
		"   mtu       : max packet size we create rate map for {2047}\n"+
		"   tsize     : how many slots should size table have {512}\n"+
		"   mpu       : minimum packet size used in rate computations\n"+
		"   overhead  : per-packet size overhead used in rate computations\n"+
		"   linklayer : adapting to a linklayer e.g. atm\n"+
		"Example: ... stab overhead 20 linklayer atm\n")
}

// readProp is lib/utils.c read_prop: /sys/class/net/DEV/PROP as a long.
func (r *runner) readProp(dev, prop string) (int64, bool) {
	fname := "/sys/class/net/" + dev + "/" + prop
	if len(fname) >= 128 {
		r.errf("could not build pathname for property\n")
		return 0, false
	}
	f, err := os.Open(r.sys.path(fname))
	if err != nil {
		r.errf("fopen %s: %s\n", fname, strerror(errnoOf(err)))
		return 0, false
	}
	buf := make([]byte, 79) // fgets(buf, 80)
	n, rerr := f.Read(buf)
	if cerr := f.Close(); cerr != nil && rerr == nil {
		rerr = cerr
	}
	if rerr != nil || n == 0 {
		r.errf("property \"%s\" in file %s is currently unknown\n", prop, fname)
		r.errf("Failed to parse %s\n", fname)
		return 0, false
	}
	line, _, _ := strings.Cut(string(buf[:n]), "\n")
	v, rest, ok, over := strtolOver(line)
	if !ok || rest != "" {
		r.errf("value \"%s\" in file %s is not a number\n", line, fname)
		r.errf("Failed to parse %s\n", fname)
		return 0, false
	}
	if over {
		r.errf("strtol %s: %s", fname, strerror(unix.ERANGE))
		r.errf("Failed to parse %s\n", fname)
		return 0, false
	}
	return v, true
}

// sendWrite builds the request and delivers it: to a replay, which
// matches it with tc's and answers with the kernel's recorded ack (rtnl_talk
// and return 2 on an error), or, on the kernel socket, to GOTC_WRITE_OUT.
func (r *runner) sendWrite(w *xtcpnl.TcWrite) error {
	req, err := xtcpnl.BuildTcWriteRequest(w, r.nextSeq())
	if err != nil {
		return fmt.Errorf("%w: a request tc would truncate or cannot be laid out: %w", errNotImplemented, err)
	}
	if _, live := r.src.(*LiveSource); live {
		return r.writeOut(req)
	}
	if _, ok, err := r.talkAnswer(req, nil); err != nil || !ok {
		if err != nil {
			return err
		}
		return retCode(2)
	}
	return nil
}

// writeOut appends req to GOTC_WRITE_OUT, which a run's first write
// truncates.
func (r *runner) writeOut(req []byte) error {
	path, ok := os.LookupEnv(writeOutEnvCst)
	if !ok || path == "" {
		return ErrWriteNotApplied
	}
	flag := os.O_WRONLY | os.O_CREATE | os.O_APPEND
	if !r.wroteOut {
		flag |= os.O_TRUNC
	}
	f, err := os.OpenFile(path, flag, 0o600)
	if err != nil {
		return fmt.Errorf("gotc: %s: %w", writeOutEnvCst, err)
	}
	_, werr := f.Write(req)
	if err := errors.Join(werr, f.Close()); err != nil {
		return fmt.Errorf("gotc: %s: %w", writeOutEnvCst, err)
	}
	r.wroteOut = true
	return nil
}

// strtolOver is strtol(s, &end, 0) for read_prop: ok is false when no
// digit was read; over is ERANGE, the result saturated.
func strtolOver(s string) (v int64, rest string, ok, over bool) {
	t := strings.TrimLeft(s, " \t\n\v\f\r")
	neg := false
	if t != "" && (t[0] == '+' || t[0] == '-') {
		neg, t = t[0] == '-', t[1:]
	}
	if t == "" || t[0] == '+' || t[0] == '-' || cIsSpace(t[0]) {
		return 0, s, false, false
	}
	mag, rest, ok, over := strtoulOver(t, 0)
	if !ok {
		return 0, s, false, false
	}
	if neg {
		if over || mag > 1<<63 {
			return math.MinInt64, rest, true, true
		}
		n, _ := cint.NegInt64(mag)
		return n, rest, true, false
	}
	if over || mag > math.MaxInt64 {
		return math.MaxInt64, rest, true, true
	}
	return cint.LongLong(mag), rest, true, false
}

// percentRate is tc_util.c parse_percent_rate: the device's speed (Mbit/s)
// times the percentage, formatted "%lf" into r_str[20], which get_rate or
// get_rate64 then parses. ok false is its -1, after its message.
func (r *runner) percentRate(str, dev string) (string, bool) {
	if dev == "" {
		r.errf("No device specified; specify device to rate limit by percentage\n")
		return "", false
	}
	mbit, ok := r.readProp(dev, "speed")
	if !ok {
		return "", false
	}
	n := 0 // sscanf %m[0-9.%]
	for n < len(str) && (str[n] >= '0' && str[n] <= '9' || str[n] == '.' || str[n] == '%') {
		n++
	}
	if n == 0 {
		r.errf("Specified rate value could not be read or is malformed\n")
		return "", false
	}
	perc, rc := parsePercent(str[:n])
	switch rc {
	case 1:
		r.errf("Invalid rate specified; should be between [0,100]%% but is %s\n", str)
		return "", false
	case -1:
		r.errf("Specified rate value could not be read or is malformed\n")
		return "", false
	}
	s := strconv.FormatFloat(perc*float64(mbit)*1000*1000, 'f', 6, 64)
	if len(s) >= 20 {
		r.errf("Unable to parse calculated rate\n")
		return "", false
	}
	return s, true
}

// getPercentRate is get_percent_rate.
func (r *runner) getPercentRate(str, dev string) (uint32, bool) {
	s, ok := r.percentRate(str, dev)
	if !ok {
		return 0, false
	}
	return getRate(s)
}

// getPercentRate64 is get_percent_rate64.
func (r *runner) getPercentRate64(str, dev string) (uint64, bool) {
	s, ok := r.percentRate(str, dev)
	if !ok {
		return 0, false
	}
	return getRate64(s)
}

// writesBuilt is whether this build lays out qdisc and class writes.
const writesBuilt = true
