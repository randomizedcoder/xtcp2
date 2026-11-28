//go:build gotc_write

package gotc

import (
	"encoding/binary"
	"math"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// tc/q_htb.c htb_parse_opt and htb_parse_class_opt at iproute2 v7.2.0.

// htbExplain is q_htb.c explain.
func (r *runner) htbExplain() {
	r.errf("%s", "Usage: ... qdisc add ... htb [default N] [r2q N]\n"+
		"                      [direct_qlen P] [offload]\n"+
		" default  minor id of class to which unclassified packets are sent {0}\n"+
		" r2q      DRR quantums are computed as rate in Bps/r2q {10}\n"+
		" debug    string of 16 numbers each 0-3 {0}\n\n"+
		" direct_qlen  Limit of the direct queue {in packets}\n"+
		" offload  enable hardware offload\n"+
		"... class add ... htb rate R1 [burst B1] [mpu B] [overhead O]\n"+
		"                      [prio P] [slot S] [pslot PS]\n"+
		"                      [ceil R2] [cburst B2] [mtu MTU] [quantum Q]\n"+
		" rate     rate allocated to this class (class can still borrow)\n"+
		" burst    max bytes burst which can be accumulated during idle period {computed}\n"+
		" mpu      minimum packet size used in rate computations\n"+
		" overhead per-packet size overhead used in rate computations\n"+
		" linklay  adapting to a linklayer e.g. atm\n"+
		" ceil     definite upper class rate (no borrows) {rate}\n"+
		" cburst   burst but for ceil {computed}\n"+
		" mtu      max packet size we create rate map for {1600}\n"+
		" prio     priority of leaf; lower are served first {0}\n"+
		" quantum  how much bytes to serve from leaf at once {use r2q}\n"+
		"\nTC HTB version 3.3\n")
}

// htbExplain1 is explain1: "Illegal" and the usage; the parser returns -1.
func (r *runner) htbExplain1(arg string) error {
	r.errf("Illegal \"%s\"\n", arg)
	r.htbExplain()
	return errKindParse
}

// htbWhat is the unknown-word arm.
func (r *runner) htbWhat(arg string) error {
	r.errf("What is \"%s\"?\n", arg)
	r.htbExplain()
	return errKindParse
}

// parseHtbQopt is htb_parse_opt.
func parseHtbQopt(r *runner, args []string, _ string) (func(*xtcpnl.AttrBuilder) error, error) {
	directQlen := uint32(math.MaxUint32)
	opt := xtcpnl.TcHtbGlob{Rate2Quantum: 10, Version: 3}
	offload := false
	for ; len(args) > 0; args = args[1:] {
		a := args[0]
		var ok bool
		switch {
		case matches(a, "r2q"):
			if len(args) < 2 {
				return nil, r.incomplete()
			}
			args = args[1:]
			if opt.Rate2Quantum, ok = getU32(args[0], 10); !ok {
				return nil, r.htbExplain1("r2q")
			}
		case matches(a, "default"):
			if len(args) < 2 {
				return nil, r.incomplete()
			}
			args = args[1:]
			if opt.Defcls, ok = getU32(args[0], 16); !ok {
				return nil, r.htbExplain1("default")
			}
		case matches(a, "debug"):
			if len(args) < 2 {
				return nil, r.incomplete()
			}
			args = args[1:]
			p := args[0]
			for i := 0; i < 16 && i < len(p); i++ {
				if p[i] < '0' || p[i] > '3' {
					break
				}
				opt.Debug |= uint32(p[i]-'0') << (2 * i)
			}
		case matches(a, "direct_qlen"):
			if len(args) < 2 {
				return nil, r.incomplete()
			}
			args = args[1:]
			v, ok := getU32(args[0], 10)
			if !ok {
				return nil, r.htbExplain1("direct_qlen")
			}
			directQlen = v
		case matches(a, "offload"):
			offload = true
		default:
			return nil, r.htbWhat(a)
		}
	}
	return func(ab *xtcpnl.AttrBuilder) error {
		m, err := ab.BeginNest(xtcpnl.TcaOptions)
		if err != nil {
			return err
		}
		var g [xtcpnl.TcHtbGlobSizeCst]byte
		if err := ab.PutBytes(xtcpnl.TcaHtbInit, opt.Append(g[:0])); err != nil {
			return err
		}
		if directQlen != math.MaxUint32 {
			if err := ab.PutU32(xtcpnl.TcaHtbDirectQlen, directQlen); err != nil {
				return err
			}
		}
		if offload {
			if err := ab.PutBytes(xtcpnl.TcaHtbOffload, nil); err != nil {
				return err
			}
		}
		return ab.EndNest(m)
	}, nil
}

// getU16 is lib/utils.c get_u16: get_u32's strtoul, capped at 0xffff.
func getU16(s string, base uint64) (uint16, bool) {
	v, ok := getU32(s, base)
	if !ok || v > math.MaxUint16 {
		return 0, false
	}
	return uint16(v), true
}

// hzDefault is utils.h HZ, get_hz's fallback.
const hzDefault = 100

// getHz is lib/utils.c __get_hz without its HZ, PROC_NET_PSCHED and
// PROC_ROOT overrides: the psched file's fourth word when the third is
// 1000000, else HZ.
func (r *runner) getHz() uint64 {
	content, err := r.sys.readFile(pschedPathCst)
	if err != nil {
		return hzDefault
	}
	var v [4]uint32
	rest := content
	for i := range v {
		n, rr, ok := scanHex(rest, 8)
		if !ok {
			return hzDefault
		}
		v[i], rest = n, rr
	}
	if v[2] == 1000000 && v[3] != 0 {
		return uint64(v[3])
	}
	return hzDefault
}

// htbRate is the rate and ceil arms: a percent through the device's speed.
func (r *runner) htbRate(s, dev string) (uint64, bool) {
	if strings.Contains(s, "%") {
		return r.getPercentRate64(s, dev)
	}
	return getRate64(s)
}

// htbCopt is htb_parse_class_opt's locals.
type htbCopt struct {
	opt                xtcpnl.TcHtbOpt
	cbuffer            uint32
	cellLog, ccellLog  int
	mtu                uint32
	mpu, overhead      uint16
	linklayer          uint32
	ceil, rate, buffer uint64
}

// parseHtbCopt is htb_parse_class_opt.
func parseHtbCopt(r *runner, args []string, dev string) (func(*xtcpnl.AttrBuilder) error, error) {
	c := htbCopt{cellLog: -1, ccellLog: -1, mtu: 1600, linklayer: linklayerEthernet}
	for ; len(args) > 0; args = args[1:] {
		if err := r.htbClassArg(&c, &args, dev); err != nil {
			return nil, err
		}
	}
	if c.rate == 0 {
		r.errf("\"rate\" is required.\n")
		return nil, errKindParse
	}
	if c.ceil == 0 {
		c.ceil = c.rate
	}
	o := &c.opt
	o.Rate.Rate = clampU32(c.rate)
	o.Ceil.Rate = clampU32(c.ceil)
	hz := r.getHz()
	if c.buffer == 0 {
		c.buffer = c.rate/hz + uint64(c.mtu)
	}
	if c.cbuffer == 0 {
		c.cbuffer = uint32((c.ceil/hz + uint64(c.mtu)) & math.MaxUint32)
	}
	var rtab, ctab [256]uint32
	rs := rateSpec{overhead: c.overhead, mpu: c.mpu, rate: o.Rate.Rate}
	// tc_calc_rtable takes the spec's own rate: the clamped 32 bits, so a
	// rate of 2^32 bytes/s or more gets a table for ~4.3 GB/s.
	r.core.calcRtable(&rs, &rtab, c.cellLog, c.mtu, c.linklayer, uint64(rs.rate))
	o.Rate = toTcRateSpec(rs)
	o.Buffer = r.core.calcXmittime(c.rate, c.buffer)
	if o.Buffer == math.MaxUint32 {
		r.errf("htb: burst out of range\n")
		return nil, errKindParse
	}
	cs := rateSpec{overhead: c.overhead, mpu: c.mpu, rate: o.Ceil.Rate}
	r.core.calcRtable(&cs, &ctab, c.ccellLog, c.mtu, c.linklayer, uint64(cs.rate))
	o.Ceil = toTcRateSpec(cs)
	o.Cbuffer = r.core.calcXmittime(c.ceil, uint64(c.cbuffer))
	return func(ab *xtcpnl.AttrBuilder) error {
		m, err := ab.BeginNest(xtcpnl.TcaOptions)
		if err != nil {
			return err
		}
		var b8 [8]byte
		if c.rate >= 1<<32 {
			if err := ab.PutBytes(xtcpnl.TcaHtbRate64, binary.LittleEndian.AppendUint64(b8[:0], c.rate)); err != nil {
				return err
			}
		}
		if c.ceil >= 1<<32 {
			if err := ab.PutBytes(xtcpnl.TcaHtbCeil64, binary.LittleEndian.AppendUint64(b8[:0], c.ceil)); err != nil {
				return err
			}
		}
		var p [xtcpnl.TcHtbOptSizeCst]byte
		if err := ab.PutBytes(xtcpnl.TcaHtbParms, o.Append(p[:0])); err != nil {
			return err
		}
		var tb [1024]byte
		if err := ab.PutBytes(xtcpnl.TcaHtbRtab, appendTab(tb[:0], &rtab)); err != nil {
			return err
		}
		if err := ab.PutBytes(xtcpnl.TcaHtbCtab, appendTab(tb[:0], &ctab)); err != nil {
			return err
		}
		return ab.EndNest(m)
	}, nil
}

// htbClassArg is one turn of htb_parse_class_opt's loop; args is left at
// the word the loop's step passes.
func (r *runner) htbClassArg(c *htbCopt, args *[]string, dev string) error {
	a := (*args)[0]
	next := func() (string, error) {
		if len(*args) < 2 {
			return "", r.incomplete()
		}
		*args = (*args)[1:]
		return (*args)[0], nil
	}
	var ok bool
	switch {
	case matches(a, wordPrio), matches(a, wordMtu), matches(a, wordQuantum):
		key := wordPrio
		dst := &c.opt.Prio
		switch {
		case matches(a, wordPrio):
		case matches(a, wordMtu):
			key, dst = wordMtu, &c.mtu
		default:
			key, dst = wordQuantum, &c.opt.Quantum
		}
		w, err := next()
		if err != nil {
			return err
		}
		if *dst, ok = getU32(w, 10); !ok {
			return r.htbExplain1(key)
		}
	case matches(a, wordMpu), matches(a, wordOverhead):
		key, dst := wordMpu, &c.mpu
		if !matches(a, wordMpu) {
			key, dst = wordOverhead, &c.overhead
		}
		w, err := next()
		if err != nil {
			return err
		}
		if *dst, ok = getU16(w, 10); !ok {
			return r.htbExplain1(key)
		}
	case matches(a, wordLinklayer):
		w, err := next()
		if err != nil {
			return err
		}
		if c.linklayer, ok = getLinklayer(w); !ok {
			return r.htbExplain1(wordLinklayer)
		}
	case matches(a, wordBurst), a == wordBuffer, a == wordMaxburst:
		w, err := next()
		if err != nil {
			return err
		}
		if c.buffer, ok = getSize64AndCell(w, &c.cellLog); !ok {
			return r.htbExplain1(a)
		}
	case matches(a, "cburst"), a == "cbuffer", a == "cmaxburst":
		w, err := next()
		if err != nil {
			return err
		}
		if c.cbuffer, ok = getSizeAndCell(w, &c.ccellLog); !ok {
			return r.htbExplain1(a)
		}
	case a == "ceil", a == wordRate:
		w, err := next()
		if err != nil {
			return err
		}
		dst := &c.rate
		if a == "ceil" {
			dst = &c.ceil
		}
		if *dst != 0 {
			r.errf("Double \"%s\" spec\n", a)
			return errKindParse
		}
		if *dst, ok = r.htbRate(w, dev); !ok {
			return r.htbExplain1(a)
		}
	case a == helpWord:
		r.htbExplain()
		return errKindParse
	default:
		return r.htbWhat(a)
	}
	return nil
}

// clampU32 is `(v >= 1ULL << 32) ? ~0U : v`.
func clampU32(v uint64) uint32 {
	if v > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(v)
}

// toTcRateSpec is the struct tc_ratespec tc_calc_rtable filled.
func toTcRateSpec(s rateSpec) xtcpnl.TcRateSpec {
	return xtcpnl.TcRateSpec{CellLog: s.cellLog, Linklayer: s.linklayer, Overhead: s.overhead,
		CellAlign: s.cellAlign, Mpu: s.mpu, Rate: s.rate}
}

// appendTab is a rate table's 256 __u32s.
func appendTab(b []byte, t *[256]uint32) []byte {
	for _, v := range t {
		b = binary.LittleEndian.AppendUint32(b, v)
	}
	return b
}
