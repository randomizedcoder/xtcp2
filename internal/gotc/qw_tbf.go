//go:build gotc_write

package gotc

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// tc/q_tbf.c tbf_parse_opt and tc/q_sfq.c sfq_parse_opt at iproute2
// v7.2.0, with tc/tc_red.c tc_red_eval_ewma and tc_red_eval_P.

func (r *runner) tbfExplain() {
	r.errf("%s", "Usage: ... tbf limit BYTES burst BYTES[/BYTES] rate KBPS [ mtu BYTES[/BYTES] ]\n"+
		"\t[ peakrate KBPS ] [ latency TIME ] "+
		"[ overhead BYTES ] [ linklayer TYPE ]\n")
}

func (r *runner) tbfExplain1(arg, val string) {
	r.errf("tbf: illegal value for \"%s\": \"%s\"\n", arg, val)
}

// tbfArgs is tbf_parse_opt's locals.
type tbfArgs struct {
	limit, mtu, mpu, latency uint32
	rcell, pcell             int
	overhead                 uint16
	linklayer                uint32
	rate64, prate64, buf64   uint64
}

// tbfRate is the rate and peakrate arms: a '%' is get_percent_rate64.
func (r *runner) tbfRate(name, arg, dev string, v *uint64) bool {
	var ok bool
	if strings.Contains(arg, "%") {
		*v, ok = r.getPercentRate64(arg, dev)
	} else {
		*v, ok = getRate64(arg)
	}
	if !ok {
		r.tbfExplain1(name, arg)
	}
	return ok
}

// tbfWord is one turn of tbf_parse_opt's loop; args[0] is the word, and
// the value (NEXT_ARG) is args[1]. It returns false for tc's return -1.
func (r *runner) tbfWord(t *tbfArgs, a, v, dev string) bool {
	dup := func(what string) bool { r.errf("tbf: duplicate \"%s\" specification\n", what); return false }
	ok := true
	switch {
	case matches(a, "limit"):
		if t.limit != 0 {
			return dup("limit")
		}
		if t.latency != 0 {
			r.errf("tbf: specifying both \"latency\" and \"limit\" is not allowed\n")
			return false
		}
		if t.limit, ok = getSize(v); !ok {
			r.tbfExplain1("limit", v)
		}
	case matches(a, "latency"):
		if t.latency != 0 {
			return dup("latency")
		}
		if t.limit != 0 {
			r.errf("tbf: specifying both \"limit\" and \"/latency\" is not allowed\n")
			return false
		}
		if t.latency, ok = getTime(v); !ok {
			r.tbfExplain1("latency", v)
		}
	case matches(a, "burst"), a == "buffer", a == "maxburst":
		if t.buf64 != 0 {
			return dup("buffer/burst/maxburst")
		}
		if t.buf64, ok = getSize64AndCell(v, &t.rcell); !ok {
			r.tbfExplain1(a, v)
		}
	case a == "mtu", a == "minburst":
		if t.mtu != 0 {
			return dup("mtu/minburst")
		}
		if t.mtu, ok = getSizeAndCell(v, &t.pcell); !ok {
			r.tbfExplain1(a, v)
		}
	case a == "mpu":
		if t.mpu != 0 {
			return dup("mpu")
		}
		if t.mpu, ok = getSize(v); !ok {
			r.tbfExplain1("mpu", v)
		}
	case a == "rate":
		if t.rate64 != 0 {
			return dup("rate")
		}
		ok = r.tbfRate("rate", v, dev, &t.rate64)
	case matches(a, "peakrate"):
		if t.prate64 != 0 {
			return dup("peakrate")
		}
		ok = r.tbfRate("peakrate", v, dev, &t.prate64)
	case matches(a, "overhead"):
		if t.overhead != 0 {
			return dup("overhead")
		}
		u, good := getU32(v, 10)
		if !good || u > math.MaxUint16 {
			r.tbfExplain1("overhead", v)
			return false
		}
		t.overhead = uint16(u)
	default: // linklayer
		if t.linklayer, ok = getLinklayer(v); !ok {
			r.tbfExplain1("linklayer", v)
		}
	}
	return ok
}

// tbfTakesValue is whether the word is one of tbf's options (all take a
// value) or "help"/unknown.
func tbfTakesValue(a string) bool {
	return matches(a, "limit") || matches(a, "latency") || matches(a, "burst") || a == "buffer" ||
		a == "maxburst" || a == "mtu" || a == "minburst" || a == "mpu" || a == "rate" ||
		matches(a, "peakrate") || matches(a, "overhead") || matches(a, "linklayer")
}

// parseTbfQopt is tbf_parse_opt.
func parseTbfQopt(r *runner, args []string, dev string) (func(*xtcpnl.AttrBuilder) error, error) {
	t := tbfArgs{rcell: -1, pcell: -1, linklayer: linklayerEthernet}
	for ; len(args) > 0; args = args[1:] {
		a := args[0]
		if !tbfTakesValue(a) {
			if a != helpWord {
				r.errf("tbf: unknown parameter \"%s\"\n", a)
			}
			r.tbfExplain()
			return nil, errKindParse
		}
		if len(args) < 2 {
			return nil, r.incomplete()
		}
		args = args[1:]
		if !r.tbfWord(&t, a, args[0], dev) {
			return nil, errKindParse
		}
	}
	bad := false
	if t.rate64 == 0 {
		r.errf("tbf: the \"rate\" parameter is mandatory.\n")
		bad = true
	}
	if t.buf64 == 0 {
		r.errf("tbf: the \"burst\" parameter is mandatory.\n")
		bad = true
	}
	if t.prate64 != 0 && t.mtu == 0 {
		r.errf("tbf: when \"peakrate\" is specified, \"mtu\" must also be specified.\n")
		bad = true
	}
	if t.limit == 0 && t.latency == 0 {
		r.errf("tbf: either \"limit\" or \"latency\" is required.\n")
		bad = true
	}
	if bad {
		r.tbfExplain()
		return nil, errKindParse
	}
	return r.tbfBuild(&t)
}

// sat32 is `v >= 1ULL<<32 ? ~0U : v`.
func sat32(v uint64) uint32 {
	if v > math.MaxUint32 {
		return math.MaxUint32
	}
	return cint.Low32(v)
}

// tbfBuild is tbf_parse_opt after the checks: the limit from latency, the
// tables, and the attributes.
func (r *runner) tbfBuild(t *tbfArgs) (func(*xtcpnl.AttrBuilder) error, error) {
	var opt xtcpnl.TcTbfQopt
	opt.Limit = t.limit
	if opt.Limit == 0 {
		lim := float64(t.rate64)*float64(t.latency)/timeUnitsPerSec + float64(t.buf64)
		if t.prate64 != 0 {
			lim2 := float64(t.prate64)*float64(t.latency)/timeUnitsPerSec + float64(t.mtu)
			if lim2 < lim {
				lim = lim2
			}
		}
		opt.Limit = cDoubleToU32(lim)
	}
	var rtab, ptab [256]uint32
	rs := rateSpec{rate: sat32(t.rate64), mpu: uint16(t.mpu & math.MaxUint16), overhead: t.overhead}
	r.core.calcRtable(&rs, &rtab, t.rcell, t.mtu, t.linklayer, uint64(rs.rate))
	opt.Buffer = r.core.calcXmittime(uint64(rs.rate), t.buf64)
	if opt.Buffer == math.MaxUint32 {
		r.errf("tbf: burst out of range\n")
		return nil, errKindParse
	}
	ps := rateSpec{rate: sat32(t.prate64)}
	if ps.rate != 0 {
		ps.mpu, ps.overhead = rs.mpu, t.overhead
		r.core.calcRtable(&ps, &ptab, t.pcell, t.mtu, t.linklayer, uint64(ps.rate))
		opt.Mtu = r.core.calcXmittime(uint64(ps.rate), uint64(t.mtu))
	}
	opt.Rate, opt.Peakrate = rs.spec(), ps.spec()
	return func(ab *xtcpnl.AttrBuilder) error {
		nest, err := ab.BeginNest(xtcpnl.TcaOptions)
		if err != nil {
			return err
		}
		var pb [xtcpnl.TcTbfQoptSizeCst]byte
		if err := ab.PutBytes(xtcpnl.TcaTbfParms, opt.Append(pb[:0])); err != nil {
			return err
		}
		if t.buf64 <= math.MaxUint32 {
			if err := ab.PutU32(xtcpnl.TcaTbfBurst, cint.Low32(t.buf64)); err != nil {
				return err
			}
		}
		if t.rate64 > math.MaxUint32 {
			if err := putU64(ab, xtcpnl.TcaTbfRate64, t.rate64); err != nil {
				return err
			}
		}
		if err := putTable(ab, xtcpnl.TcaTbfRtab, &rtab); err != nil {
			return err
		}
		if ps.rate != 0 {
			if t.prate64 > math.MaxUint32 {
				if err := putU64(ab, xtcpnl.TcaTbfPrate64, t.prate64); err != nil {
					return err
				}
			}
			if err := ab.PutU32(xtcpnl.TcaTbfPburst, t.mtu); err != nil {
				return err
			}
			if err := putTable(ab, xtcpnl.TcaTbfPtab, &ptab); err != nil {
				return err
			}
		}
		return ab.EndNest(nest)
	}, nil
}

// spec is the rateSpec as the wire struct.
func (s rateSpec) spec() xtcpnl.TcRateSpec {
	return xtcpnl.TcRateSpec{CellLog: s.cellLog, Linklayer: s.linklayer, Overhead: s.overhead,
		CellAlign: s.cellAlign, Mpu: s.mpu, Rate: s.rate}
}

// putU64 is addattr_l of a __u64.
func putU64(ab *xtcpnl.AttrBuilder, t uint16, v uint64) error {
	var b [8]byte
	return ab.PutBytes(t, binary.LittleEndian.AppendUint64(b[:0], v))
}

// putTable is addattr_l of a __u32 rtab[256].
func putTable(ab *xtcpnl.AttrBuilder, t uint16, tab *[256]uint32) error {
	var b [1024]byte
	o := b[:0]
	for _, v := range tab {
		o = binary.LittleEndian.AppendUint32(o, v)
	}
	return ab.PutBytes(t, o)
}

func (r *runner) sfqExplain() {
	r.errf("%s", "Usage: ... sfq\t[ limit NUMBER ] [ perturb SECS ] [ quantum BYTES ]\n"+
		"\t\t[ divisor NUMBER ] [ flows NUMBER] [ depth NUMBER ]\n"+
		"\t\t[ headdrop ]\n"+
		"\t\t[ redflowlimit BYTES ] [ min BYTES ] [ max BYTES ]\n"+
		"\t\t[ avpkt BYTES ] [ burst PACKETS ] [ probability P ]\n"+
		"\t\t[ ecn ] [ harddrop ]\n")
}

// sfqArgs is sfq_parse_opt's locals.
type sfqArgs struct {
	opt         xtcpnl.TcSfqQoptV1
	ok, red     int
	burst       uint32
	avpkt       uint32
	probability float64
}

// sfqU32 are sfq's get_u32 (and get_unsigned) words: the field and the
// word its message names ("depth" says "flows", tc's own slip).
func (s *sfqArgs) sfqU32(a string) (*uint32, string, *int) {
	switch a {
	case "limit":
		return &s.opt.V0.Limit, "limit", &s.ok
	case "divisor":
		return &s.opt.V0.Divisor, "divisor", &s.ok
	case "flows":
		return &s.opt.V0.Flows, "flows", &s.ok
	case "depth":
		return &s.opt.Depth, "flows", &s.ok
	case "redflowlimit":
		return &s.opt.Limit, "redflowlimit", &s.red
	case "min":
		return &s.opt.QthMin, "min", &s.red
	case "max":
		return &s.opt.QthMax, "max", &s.red
	case "burst":
		return &s.burst, "burst", &s.red
	}
	return nil, "", nil
}

// sfqWord is one valued word of sfq_parse_opt's loop.
func (r *runner) sfqWord(s *sfqArgs, a, v string) bool {
	if p, name, cnt := s.sfqU32(a); p != nil {
		u, ok := getU32(v, 0)
		if !ok {
			r.errf("Illegal \"%s\"\n", name)
			return false
		}
		*p = u
		if a == "limit" && u < 2 {
			r.errf("Illegal \"limit\", must be > 1\n")
			return false
		}
		*cnt++
		return true
	}
	switch a {
	case "quantum":
		u, ok := getSize(v)
		if !ok {
			r.errf("Illegal \"limit\"\n") // tc's message for quantum
			return false
		}
		s.opt.V0.Quantum = u
		s.ok++
	case "perturb":
		n, ok := getInteger(v, 0)
		if !ok {
			r.errf("Illegal \"perturb\"\n")
			return false
		}
		s.opt.V0.PerturbPeriod = n
		s.ok++
	case "avpkt":
		u, ok := getSize(v)
		if !ok {
			r.errf("Illegal \"avpkt\"\n")
			return false
		}
		s.avpkt = u
		s.red++
	default: // probability: sscanf %lg
		p, rest, _ := cStrtod(v)
		if rest == v {
			r.errf("Illegal \"probability\"\n")
			return false
		}
		s.probability = p
		s.red++
	}
	return true
}

// parseSfqQopt is sfq_parse_opt.
func parseSfqQopt(r *runner, args []string, _ string) (func(*xtcpnl.AttrBuilder) error, error) {
	s := sfqArgs{avpkt: 1000, probability: 0.02}
	for ; len(args) > 0; args = args[1:] {
		a := args[0]
		switch a {
		case "headdrop":
			s.opt.Headdrop = 1
			s.ok++
			continue
		case "ecn":
			s.opt.Flags |= xtcpnl.TcRedEcn
			s.red++
			continue
		case "harddrop":
			s.opt.Flags |= xtcpnl.TcRedHarddrop
			s.red++
			continue
		case "quantum", "perturb", "limit", "divisor", "flows", "depth", "redflowlimit", "min", "max",
			"burst", "avpkt", "probability":
		default:
			if a != helpWord {
				r.errf("What is \"%s\"?\n", a)
			}
			r.sfqExplain()
			return nil, errKindParse
		}
		if len(args) < 2 {
			return nil, r.incomplete()
		}
		args = args[1:]
		if !r.sfqWord(&s, a, args[0]) {
			return nil, errKindParse
		}
	}
	if s.red != 0 && s.opt.Limit != 0 && s.avpkt == 0 {
		return nil, fmt.Errorf("%w: sfq red with avpkt 0 (tc divides by it, SIGFPE)", errNotImplemented)
	}
	if s.red != 0 && !r.sfqRedParse(&s) {
		return nil, errKindParse
	}
	if s.ok == 0 && s.red == 0 {
		return nil, nil
	}
	opt := s.opt
	return func(ab *xtcpnl.AttrBuilder) error {
		var b [xtcpnl.TcSfqQoptV1SizeCst]byte
		return ab.PutBytes(xtcpnl.TcaOptions, opt.Append(b[:0]))
	}, nil
}

// sfqRedParse is sfq_parse_opt's red block.
func (r *runner) sfqRedParse(s *sfqArgs) bool {
	o := &s.opt
	if o.Limit == 0 {
		r.errf("Required parameter (redflowlimit) is missing\n")
		return false
	}
	if o.QthMax == 0 {
		o.QthMax = o.Limit / 4
	}
	if o.QthMin == 0 {
		o.QthMin = o.QthMax / 3
	}
	if s.burst == 0 {
		s.burst = (2*o.QthMin + o.QthMax) / (3 * s.avpkt) // unsigned, wraps as C's; avpkt 0 is SIGFPE in tc
	}
	if o.QthMax > o.Limit {
		r.errf("\"max\" is larger than \"limit\"\n")
		return false
	}
	if o.QthMin >= o.QthMax {
		r.errf("\"min\" is not smaller than \"max\"\n")
		return false
	}
	wlog := r.redEvalEwma(o.QthMin, s.burst, s.avpkt)
	if wlog < 0 {
		r.errf("SFQ: failed to calculate EWMA constant.\n")
		return false
	}
	if wlog >= 10 {
		r.errf("SFQ: WARNING. Burst %d seems to be too large.\n", s.burst)
	}
	o.Wlog = uint8(wlog)
	plog := redEvalP(o.QthMin, o.QthMax, s.probability)
	if plog < 0 {
		r.errf("SFQ: failed to calculate probability.\n")
		return false
	}
	o.Plog = uint8(plog)
	o.MaxP = cDoubleToU32(s.probability * math.Pow(2, 32))
	return true
}

// redEvalP is tc_red_eval_P.
func redEvalP(qmin, qmax uint32, prob float64) int {
	i := cint.Int(qmax - qmin)
	if i == 0 {
		return 0
	}
	if i < 0 {
		return -1
	}
	prob /= float64(i)
	n := 0
	for ; n < 32; n++ {
		if prob > 1.0 {
			break
		}
		prob *= 2
	}
	if n >= 32 {
		return -1
	}
	return n
}

// redEvalEwma is tc_red_eval_ewma.
func (r *runner) redEvalEwma(qmin, burst, avpkt uint32) int {
	w := 0.5
	a := float64(burst) + 1 - float64(qmin)/float64(avpkt)
	if a < 1.0 {
		r.errf("tc_red_eval_ewma() burst %d is too small ? Try burst %d\n", burst, 1+qmin/avpkt)
		return -1
	}
	for wlog := 1; wlog < 32; wlog++ {
		if a <= (1-math.Pow(1-w, float64(burst)))/w {
			return wlog
		}
		w /= 2
	}
	return -1
}
