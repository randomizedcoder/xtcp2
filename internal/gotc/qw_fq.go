//go:build gotc_write

package gotc

import (
	"encoding/binary"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// tc/q_fq_codel.c fq_codel_parse_opt and tc/q_fq.c fq_parse_opt at
// iproute2 v7.2.0. Each parses every word first, then writes TCA_OPTIONS
// with its attributes in a fixed order, whatever the argv order.

// getUnsigned is lib/utils.c get_unsigned: strtoul in base, nothing left
// over, no ERANGE, within an unsigned int (get_u32's rules).
func getUnsigned(s string, base uint64) (uint32, bool) { return getU32(s, base) }

// getU8 is lib/utils.c get_u8.
func getU8(s string, base uint64) (uint8, bool) {
	v, ok := getU32(s, base)
	if !ok || v > 0xff {
		return 0, false
	}
	return uint8(v), true
}

// u32Attr is one addattr_l of a __u32 (or int) option.
type u32Attr struct {
	t uint16
	v uint32
}

// putU32s writes the set attributes in order.
func putU32s(ab *xtcpnl.AttrBuilder, attrs []u32Attr) error {
	for _, a := range attrs {
		if err := ab.PutU32(a.t, a.v); err != nil {
			return err
		}
	}
	return nil
}

func fqCodelExplain(r *runner) {
	r.errf("%s", "Usage: ... fq_codel	[ limit PACKETS ] [ flows NUMBER ]\n"+
		"[ memory_limit BYTES ]\n"+
		"[ target TIME ] [ interval TIME ]\n"+
		"[ quantum BYTES ] [ [no]ecn ]\n"+
		"[ ce_threshold TIME ]\n"+
		"[ ce_threshold_selector VALUE/MASK ]\n"+
		"[ drop_batch SIZE ]\n")
}

// illegal is a parser's `fprintf(stderr, "Illegal \"%s\"\n"); return -1`.
func illegal(r *runner, what string) error {
	r.errf("Illegal \"%s\"\n", what)
	return errKindParse
}

// parseFqCodelQopt is fq_codel_parse_opt.
func parseFqCodelQopt(r *runner, args []string, _ string) (func(*xtcpnl.AttrBuilder) error, error) {
	var dropBatch, limit, flows, target, interval, quantum uint32
	ceThreshold, memory := ^uint32(0), ^uint32(0)
	var ceMask uint8
	ceSel := uint8(0xff)
	ecn := int32(-1)
	for ; len(args) > 0; args = args[1:] {
		a := args[0]
		var dst *uint32
		get := getTime
		switch a {
		case wordLimit:
			dst, get = &limit, func(s string) (uint32, bool) { return getUnsigned(s, 0) }
		case wordFlows:
			dst, get = &flows, func(s string) (uint32, bool) { return getUnsigned(s, 0) }
		case wordQuantum:
			dst, get = &quantum, func(s string) (uint32, bool) { return getUnsigned(s, 0) }
		case "drop_batch":
			dst, get = &dropBatch, func(s string) (uint32, bool) { return getUnsigned(s, 0) }
		case wordTarget:
			dst = &target
		case wordCeThreshold:
			dst = &ceThreshold
		case "memory_limit":
			dst, get = &memory, getSize
		case "interval":
			dst = &interval
		case "ce_threshold_selector":
			var err error
			if args, ceSel, ceMask, err = fqCodelSelector(r, args); err != nil {
				return nil, err
			}
			continue
		case wordEcn:
			ecn = 1
			continue
		case "noecn":
			ecn = 0
			continue
		case helpWord:
			fqCodelExplain(r)
			return nil, errKindParse
		default:
			r.errf("What is \"%s\"?\n", a)
			fqCodelExplain(r)
			return nil, errKindParse
		}
		if len(args) < 2 {
			return nil, r.incomplete()
		}
		args = args[1:]
		v, ok := get(args[0])
		if !ok {
			return nil, illegal(r, a)
		}
		*dst = v
	}
	return fqCodelAttrs(fqCodelVals{limit, flows, quantum, interval, target, ceThreshold, memory, dropBatch, ecn, ceMask, ceSel}), nil
}

// fqCodelSelector is the ce_threshold_selector arm: VALUE/MASK, both
// get_u8; args starts at the word and is returned at its value.
func fqCodelSelector(r *runner, args []string) ([]string, uint8, uint8, error) {
	if len(args) < 2 {
		return nil, 0, 0, r.incomplete()
	}
	args = args[1:]
	val, mask, ok := strings.Cut(args[0], "/")
	if !ok {
		r.errf("Missing mask for \"ce_threshold_selector\"\n")
		return nil, 0, 0, errKindParse
	}
	m, ok := getU8(mask, 0)
	if !ok {
		r.errf("Illegal mask for \"ce_threshold_selector\"\n")
		return nil, 0, 0, errKindParse
	}
	v, ok := getU8(val, 0)
	if !ok {
		return nil, 0, 0, illegal(r, "ce_threshold_selector")
	}
	return args, v, m, nil
}

// fqCodelVals is what fq_codel_parse_opt gathered.
type fqCodelVals struct {
	limit, flows, quantum, interval, target, ceThreshold, memory, dropBatch uint32
	ecn                                                                     int32
	ceMask, ceSel                                                           uint8
}

// fqCodelAttrs is fq_codel_parse_opt's addattrs, in its order.
func fqCodelAttrs(v fqCodelVals) func(*xtcpnl.AttrBuilder) error {
	limit, flows, quantum, interval, target, ceThreshold, memory, dropBatch, ecn, ceMask, ceSel :=
		v.limit, v.flows, v.quantum, v.interval, v.target, v.ceThreshold, v.memory, v.dropBatch, v.ecn, v.ceMask, v.ceSel
	var attrs []u32Attr
	add := func(cond bool, t uint16, v uint32) {
		if cond {
			attrs = append(attrs, u32Attr{t, v})
		}
	}
	add(limit != 0, xtcpnl.TcaFqCodelLimit, limit)
	add(flows != 0, xtcpnl.TcaFqCodelFlows, flows)
	add(quantum != 0, xtcpnl.TcaFqCodelQuantum, quantum)
	add(interval != 0, xtcpnl.TcaFqCodelInterval, interval)
	add(target != 0, xtcpnl.TcaFqCodelTarget, target)
	add(ecn != -1, xtcpnl.TcaFqCodelEcn, cint.Bits(ecn))
	add(ceThreshold != ^uint32(0), xtcpnl.TcaFqCodelCeThreshold, ceThreshold)
	tailAttrs := []u32Attr{}
	addT := func(cond bool, t uint16, v uint32) {
		if cond {
			tailAttrs = append(tailAttrs, u32Attr{t, v})
		}
	}
	addT(memory != ^uint32(0), xtcpnl.TcaFqCodelMemoryLimit, memory)
	addT(dropBatch != 0, xtcpnl.TcaFqCodelDropBatchSize, dropBatch)
	return func(ab *xtcpnl.AttrBuilder) error {
		m, err := ab.BeginNest(xtcpnl.TcaOptions)
		if err != nil {
			return err
		}
		if err := putU32s(ab, attrs); err != nil {
			return err
		}
		if ceSel != 0xff {
			if err := ab.PutU8(xtcpnl.TcaFqCodelCeThresholdMask, ceMask); err != nil {
				return err
			}
			if err := ab.PutU8(xtcpnl.TcaFqCodelCeThresholdSelector, ceSel); err != nil {
				return err
			}
		}
		if err := putU32s(ab, tailAttrs); err != nil {
			return err
		}
		return ab.EndNest(m)
	}
}

func fqExplain(r *runner) {
	r.errf("%s", "Usage: ... fq	[ limit PACKETS ] [ flow_limit PACKETS ]\n"+
		"		[ quantum BYTES ] [ initial_quantum BYTES ]\n"+
		"		[ maxrate RATE ] [ buckets NUMBER ]\n"+
		"		[ [no]pacing ] [ refill_delay TIME ]\n"+
		"		[ bands 3 priomap P0 P1 ... P14 P15 ]\n"+
		"		[ weights W1 W2 W3 ]\n"+
		"		[ low_rate_threshold RATE ]\n"+
		"		[ orphan_mask MASK]\n"+
		"		[ timer_slack TIME]\n"+
		"		[ ce_threshold TIME ]\n"+
		"		[ horizon TIME ]\n"+
		"		[ horizon_{cap|drop} ]\n"+
		"		[ offload_horizon TIME ]\n")
}

// fqIlog2 is q_fq.c ilog2: bits of val-1.
func fqIlog2(val uint32) uint32 {
	res := uint32(0)
	for val--; val != 0; val >>= 1 {
		res++
	}
	return res
}

// fqBands is FQ_BANDS; fqMinWeight is FQ_MIN_WEIGHT.
const (
	fqBands     = 3
	fqMinWeight = 16384
)

// fqOpt is one of fq's set_* options: its value and whether it was given.
type fqOpt struct {
	v   uint32
	set bool
}

// fqState is fq_parse_opt's locals.
type fqState struct {
	plimit, flowPlimit, quantum, initQuantum, maxrate, lowRate, defrate fqOpt
	refill, orphan, ce, slack, horizon, offload                         fqOpt

	buckets     uint32
	horizonDrop uint8
	pacing      int32
	prio        xtcpnl.TcPrioQopt
	setPriomap  bool
	setWeights  bool
	weights     [fqBands]int32
}

// parseFqQopt is fq_parse_opt.
func parseFqQopt(r *runner, args []string, dev string) (func(*xtcpnl.AttrBuilder) error, error) {
	st := &fqState{horizonDrop: 255, pacing: -1}
	for ; len(args) > 0; args = args[1:] {
		a := args[0]
		if dst, get := st.valueOpt(r, a, dev); dst != nil {
			if len(args) < 2 {
				return nil, r.incomplete()
			}
			args = args[1:]
			v, ok := get(args[0])
			if !ok {
				return nil, illegal(r, a)
			}
			*dst = fqOpt{v, true}
			continue
		}
		var err error
		if args, err = st.special(r, args); err != nil {
			return nil, err
		}
	}
	return st.opts(), nil
}

// valueOpt is the options that are a word and one value: where it goes
// and how it is read; nil for the others.
func (st *fqState) valueOpt(r *runner, a, dev string) (*fqOpt, func(string) (uint32, bool)) {
	unsigned0 := func(s string) (uint32, bool) { return getUnsigned(s, 0) }
	rate := func(s string) (uint32, bool) {
		if strings.Contains(s, "%") {
			return r.getPercentRate(s, dev)
		}
		return getRate(s)
	}
	switch a {
	case wordLimit:
		return &st.plimit, unsigned0
	case "flow_limit":
		return &st.flowPlimit, unsigned0
	case "maxrate":
		return &st.maxrate, rate
	case "low_rate_threshold":
		return &st.lowRate, getRate
	case wordCeThreshold:
		return &st.ce, getTime
	case "horizon":
		return &st.horizon, getTime
	case "offload_horizon":
		return &st.offload, getTime
	case "defrate":
		return &st.defrate, rate
	case wordQuantum:
		return &st.quantum, unsigned0
	case "initial_quantum":
		return &st.initQuantum, unsigned0
	case "refill_delay":
		return &st.refill, getTime
	}
	return nil, nil
}

// special is the rest of fq_parse_opt's loop: args starts at the word and
// is returned at the last word it took.
func (st *fqState) special(r *runner, args []string) ([]string, error) {
	a := args[0]
	switch a {
	case wordBuckets, "orphan_mask":
		if len(args) < 2 {
			return nil, r.incomplete()
		}
		args = args[1:]
		v, ok := getUnsigned(args[0], 0)
		switch {
		case !ok && a == wordBuckets:
			return nil, illegal(r, a)
		case !ok:
			return nil, illegal(r, "initial_quantum") // tc's message names the wrong option
		case a == wordBuckets:
			st.buckets = v
		default:
			st.orphan = fqOpt{v, true}
		}
	case "timer_slack":
		return st.timerSlack(r, args)
	case "horizon_drop":
		st.horizonDrop = 1
	case "horizon_cap":
		st.horizonDrop = 0
	case "pacing":
		st.pacing = 1
	case "nopacing":
		st.pacing = 0
	case wordBands:
		var err error
		if args, err = fqPriomap(r, args, &st.prio, st.setPriomap); err != nil {
			return nil, err
		}
		st.setPriomap = true
	case "weights":
		return st.parseWeights(r, args)
	case helpWord:
		fqExplain(r)
		return nil, errKindParse
	default:
		r.errf("What is \"%s\"?\n", a)
		fqExplain(r)
		return nil, errKindParse
	}
	return args, nil
}

func (st *fqState) timerSlack(r *runner, args []string) ([]string, error) {
	if len(args) < 2 {
		return nil, r.incomplete()
	}
	args = args[1:]
	t64, ok := getTime64(args[0])
	if !ok {
		return nil, illegal(r, "timer_slack")
	}
	if t64 < 0 || t64 > 0xffffffff { // timer_slack != t64
		r.errf("Illegal (out of range) \"timer_slack\"\n")
		return nil, errKindParse
	}
	st.slack = fqOpt{uint32(t64), true}
	return args, nil
}

func (st *fqState) parseWeights(r *runner, args []string) ([]string, error) {
	if st.setWeights {
		r.errf("Duplicate \"weights\"\n")
		return nil, errKindParse
	}
	for i := range fqBands {
		if len(args) < 2 {
			r.errf("Not enough elements in weights\n")
			return nil, errKindParse
		}
		args = args[1:]
		v, ok := getInteger(args[0], 10)
		if !ok {
			r.errf("Illegal \"weights\" element, positive number expected\n")
			return nil, errKindParse
		}
		if v < fqMinWeight {
			r.errf("\"weight\" element %d too small\n", v)
			return nil, errKindParse
		}
		st.weights[i] = v
	}
	st.setWeights = true
	return args, nil
}

// opts is fq_parse_opt's addattrs, in its order.
func (st *fqState) opts() func(*xtcpnl.AttrBuilder) error {
	var head []u32Attr
	add := func(o fqOpt, t uint16) {
		if o.set {
			head = append(head, u32Attr{t, o.v})
		}
	}
	if st.buckets != 0 {
		head = append(head, u32Attr{xtcpnl.TcaFqBucketsLog, fqIlog2(st.buckets)})
	}
	add(st.plimit, xtcpnl.TcaFqPlimit)
	add(st.flowPlimit, xtcpnl.TcaFqFlowPlimit)
	add(st.quantum, xtcpnl.TcaFqQuantum)
	add(st.initQuantum, xtcpnl.TcaFqInitialQuantum)
	add(fqOpt{cint.Bits(st.pacing), st.pacing != -1}, xtcpnl.TcaFqRateEnable)
	add(st.maxrate, xtcpnl.TcaFqFlowMaxRate)
	add(st.lowRate, xtcpnl.TcaFqLowRateThreshold)
	add(st.defrate, xtcpnl.TcaFqFlowDefaultRate)
	add(st.refill, xtcpnl.TcaFqFlowRefillDelay)
	add(st.orphan, xtcpnl.TcaFqOrphanMask)
	add(st.ce, xtcpnl.TcaFqCeThreshold)
	add(st.slack, xtcpnl.TcaFqTimerSlack)
	add(st.horizon, xtcpnl.TcaFqHorizon)
	return func(ab *xtcpnl.AttrBuilder) error {
		m, err := ab.BeginNest(xtcpnl.TcaOptions)
		if err != nil {
			return err
		}
		if err := putU32s(ab, head); err != nil {
			return err
		}
		if err := st.putTail(ab); err != nil {
			return err
		}
		return ab.EndNest(m)
	}
}

// putTail is what follows the u32s: horizon_drop, the priomap, the
// weights and offload_horizon.
func (st *fqState) putTail(ab *xtcpnl.AttrBuilder) error {
	if st.horizonDrop != 255 {
		if err := ab.PutU8(xtcpnl.TcaFqHorizonDrop, st.horizonDrop); err != nil {
			return err
		}
	}
	if st.setPriomap {
		var b [xtcpnl.TcPrioQoptSizeCst]byte
		if err := ab.PutBytes(xtcpnl.TcaFqPriomap, st.prio.Append(b[:0])); err != nil {
			return err
		}
	}
	if st.setWeights {
		var b [4 * fqBands]byte
		w := b[:0]
		for _, v := range st.weights {
			w = binary.LittleEndian.AppendUint32(w, cint.Bits(v))
		}
		if err := ab.PutBytes(xtcpnl.TcaFqWeights, w); err != nil {
			return err
		}
	}
	if st.offload.set {
		return ab.PutU32(xtcpnl.TcaFqOffloadHorizon, st.offload.v)
	}
	return nil
}

// fqPriomap is fq_parse_opt's "bands 3 priomap P0 ... P15": args starts
// at "bands" and is returned at the last element.
func fqPriomap(r *runner, args []string, prio *xtcpnl.TcPrioQopt, dup bool) ([]string, error) {
	if dup {
		r.errf("Duplicate \"bands\"\n")
		return nil, errKindParse
	}
	*prio = xtcpnl.TcPrioQopt{}
	if len(args) < 2 {
		return nil, r.incomplete()
	}
	args = args[1:]
	bands, ok := getInteger(args[0], 10)
	if !ok {
		return nil, illegal(r, wordBands)
	}
	prio.Bands = bands
	if bands != fqBands {
		r.errf("\"bands\" must be 3\n")
		return nil, errKindParse
	}
	if len(args) < 2 {
		return nil, r.incomplete()
	}
	args = args[1:]
	if args[0] != "priomap" {
		r.errf("\"priomap\" expected\n")
		return nil, errKindParse
	}
	for i := range prio.Priomap {
		if len(args) < 2 { // NEXT_ARG_OK
			r.errf("Not enough elements in priomap\n")
			return nil, errKindParse
		}
		args = args[1:]
		band, ok := getUnsigned(args[0], 10)
		if !ok {
			r.errf("Illegal \"priomap\" element, number in [0..%d] expected\n", bands-1)
			return nil, errKindParse
		}
		if band >= cint.Bits(bands) {
			r.errf("\"priomap\" element %d too big\n", band)
			return nil, errKindParse
		}
		prio.Priomap[i] = uint8(band & 0xff)
	}
	return args, nil
}
