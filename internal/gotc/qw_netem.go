//go:build gotc_write

package gotc

import (
	"encoding/binary"
	"errors"
	"math"
	"os"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// tc/q_netem.c netem_parse_opt at iproute2 v7.2.0, with get_distribution.
//
// TCA_OPTIONS is tc_netem_qopt followed by the netem attributes: tc adds
// it with addattr_l and then stretches its rta_len over everything after
// it (tail->rta_len), with no inner nest header (unlike
// addattr_nest_compat). Every addattr_l is bounded by maxlen 1024 bar the
// two distribution tables (MAX_DIST*2).

// netemMaxDist is MAX_DIST: a distribution table's entries.
const netemMaxDist = 16 * 1024

// netemMaxlen is the maxlen netem passes to addattr_l.
const netemMaxlen = 1024

// errNetemBound is an addattr_l over its maxlen: tc prints "addattr_l
// ERROR" and parse_qopt fails; gotc refuses the request instead.
var errNetemBound = errors.New("gotc: netem attribute over addattr_l's maxlen")

// tcLibDir is get_tc_lib: TC_LIB_DIR, else LIBDIR "/tc/".
func tcLibDir() string {
	if d, ok := os.LookupEnv("TC_LIB_DIR"); ok {
		return d
	}
	return "/usr/lib/tc/"
}

// netemExplain is q_netem.c explain.
func (r *runner) netemExplain() {
	r.errf("%s", "Usage: ... netem [ limit PACKETS ]\n"+
		"                 [ delay TIME [ JITTER [ CORRELATION ] ] ]\n"+
		"                 [ distribution {uniform|normal|pareto|paretonormal} ]\n"+
		"                 [ corrupt PERCENT [ CORRELATION ] ]\n"+
		"                 [ duplicate PERCENT [ CORRELATION ] ]\n"+
		"                 [ loss random PERCENT [ CORRELATION ] ]\n"+
		"                 [ loss state P13 [ P31 [ P32 [ P23 [ P14 ] ] ] ] ]\n"+
		"                 [ loss gemodel PERCENT [ R [ 1-H [ 1-K ] ] ] ]\n"+
		"                 [ seed SEED ]\n"+
		"                 [ ecn ]\n"+
		"                 [ reorder PERCENT [ CORRELATION ] [ gap DISTANCE ] ]\n"+
		"                 [ rate RATE [ PACKETOVERHEAD ] [ CELLSIZE ] [ CELLOVERHEAD ] ]\n"+
		"                 [ slot MIN_DELAY [ MAX_DELAY ] [ packets MAX_PACKETS ] [ bytes MAX_BYTES ] ]\n"+
		"                 [ slot distribution {uniform|normal|pareto|paretonormal|custom}\n"+
		"                   DELAY JITTER [ packets MAX_PACKETS ] [ bytes MAX_BYTES ] ]\n")
}

// illegal is explain1 and return -1.
func (r *runner) illegal(arg string) error {
	r.errf("Illegal \"%s\"\n", arg)
	return errKindParse
}

// setPercent is set_percent: rint(per * UINT32_MAX) into a __u32.
func setPercent(per float64) uint32 {
	return cDoubleToU32(math.RoundToEven(per * math.MaxUint32))
}

// getPercent is q_netem.c get_percent.
func getPercent(s string) (uint32, bool) {
	per, rc := parsePercent(s)
	if rc != 0 {
		return 0, false
	}
	return setPercent(per), true
}

// getU64 is lib/utils.c get_u64: strtoull, all of it, no ERANGE.
func getU64(s string, base uint64) (uint64, bool) {
	if s == "" {
		return 0, false
	}
	v, rest, ok, over := strtoulOver(s, base)
	if !ok || rest != "" || over {
		return 0, false
	}
	return v, true
}

// getS32 is lib/utils.c get_s32: strtol, all of it, within an int32.
func getS32(s string, _ uint64) (int32, bool) {
	if s == "" {
		return 0, false
	}
	v, rest, ok, over := strtolOver(s)
	if !ok || rest != "" || over || v > math.MaxInt32 || v < math.MinInt32 {
		return 0, false
	}
	return cint.LongToInt(v), true
}

// isDigitByte is isdigit in the C locale.
func isDigitByte(c byte) bool { return c >= '0' && c <= '9' }

// nextIsNumber is NEXT_IS_NUMBER: a next word starting with a digit.
func nextIsNumber(a []string) bool { return len(a) > 1 && a[1] != "" && isDigitByte(a[1][0]) }

// nextIsSigned is NEXT_IS_SIGNED_NUMBER: a digit or '-'.
func nextIsSigned(a []string) bool {
	return len(a) > 1 && a[1] != "" && (isDigitByte(a[1][0]) || a[1][0] == '-')
}

// getDistribution is q_netem.c get_distribution: the numbers of
// <lib>/<type>.dist, each strtol'd and kept as an __s16; lines starting
// with '\n' or '#' are skipped. n <= 0 is a failure (an empty table
// silently).
func (r *runner) getDistribution(typ string) ([]int16, int) {
	name := tcLibDir() + "/" + typ + ".dist"
	if len(name) > 127 { // snprintf(name, 128)
		name = name[:127]
	}
	body, err := r.sys.readFile(name)
	if err != nil {
		r.errf("No distribution data for %s (%s: %s)\n", typ, name, strerror(errnoOf(err)))
		return nil, -1
	}
	var data []int16
	for line := range strings.SplitAfterSeq(body, "\n") {
		if line == "" || line[0] == '\n' || line[0] == '#' {
			continue
		}
		for p := line; ; {
			x, rest, ok, _ := strtolOver(p)
			if !ok {
				break
			}
			if len(data) >= netemMaxDist {
				r.errf("%s: too much data\n", name)
				return nil, -1
			}
			data = append(data, cint.Short(uint16(cint.U64Bits(x)&math.MaxUint16)))
			p = rest
		}
	}
	return data, len(data)
}

// netemArgs is what netem_parse_opt gathers.
type netemArgs struct {
	opt       xtcpnl.TcNetemQopt
	cor       xtcpnl.TcNetemCorr
	reorder   xtcpnl.TcNetemProbCorr
	corrupt   xtcpnl.TcNetemProbCorr
	gi        xtcpnl.TcNetemGimodel
	ge        xtcpnl.TcNetemGemodel
	rate      xtcpnl.TcNetemRate
	slot      xtcpnl.TcNetemSlot
	dist      []int16
	slotDist  []int16
	lossType  uint16
	present   [xtcpnl.TcaNetemMaxKnown + 1]int
	latency64 int64
	jitter64  int64
	rate64    uint64
	seed      uint64
}

// parseNetemQopt is netem_parse_opt.
func parseNetemQopt(r *runner, args []string, dev string) (func(*xtcpnl.AttrBuilder) error, error) {
	n := &netemArgs{opt: xtcpnl.TcNetemQopt{Limit: 1000}}
	for a := args; len(a) > 0; a = a[1:] {
		var err error
		w := a[0]
		switch {
		case matches(w, wordLimit):
			if a, err = r.nextArg(a); err != nil {
				return nil, err
			}
			v, ok := getSize(a[0])
			if !ok {
				return nil, r.illegal(wordLimit)
			}
			n.opt.Limit = v
		case matches(w, wordLatency), matches(w, "delay"):
			a, err = r.netemDelay(n, a)
		case matches(w, "loss"), matches(w, "drop"):
			a, err = r.netemLossArg(n, a)
		case matches(w, "seed"):
			if a, err = r.nextArg(a); err != nil {
				return nil, err
			}
			n.present[xtcpnl.TcaNetemPrngSeed] = 1
			v, ok := getU64(a[0], 10)
			if !ok {
				return nil, r.illegal("seed")
			}
			n.seed = v
		case matches(w, wordEcn):
			n.present[xtcpnl.TcaNetemEcn] = 1
		case matches(w, "reorder"):
			n.present[xtcpnl.TcaNetemReorder] = 1
			a, err = r.netemProbCorr(n, a, &n.reorder, "reorder")
		case matches(w, "corrupt"):
			n.present[xtcpnl.TcaNetemCorrupt] = 1
			a, err = r.netemProbCorr(n, a, &n.corrupt, "corrupt")
		case matches(w, "gap"):
			if a, err = r.nextArg(a); err != nil {
				return nil, err
			}
			v, ok := getU32(a[0], 0)
			if !ok {
				return nil, r.illegal("gap")
			}
			n.opt.Gap = v
		case matches(w, wordDuplicate):
			a, err = r.netemDuplicate(n, a)
		case matches(w, "distribution"):
			if a, err = r.nextArg(a); err != nil {
				return nil, err
			}
			d, sz := r.getDistribution(a[0])
			if sz <= 0 {
				return nil, errKindParse
			}
			n.dist = d
		case matches(w, wordRate):
			a, err = r.netemRate(n, a, dev)
		case matches(w, "slot"):
			a, err = r.netemSlot(n, a)
		default:
			if w != helpWord {
				r.errf("What is \"%s\"?\n", w)
			}
			r.netemExplain()
			return nil, errKindParse
		}
		if err != nil {
			return nil, err
		}
	}
	if err := r.netemCheck(n); err != nil {
		return nil, err
	}
	return n.put, nil
}

// nextArg is NEXT_ARG: the next word, or incomplete_command's exit.
func (r *runner) nextArg(a []string) ([]string, error) {
	if len(a) < 2 {
		return nil, r.incomplete()
	}
	return a[1:], nil
}

// netemDelay is the latency/delay arm: TIME [JITTER [CORRELATION]].
func (r *runner) netemDelay(n *netemArgs, a []string) ([]string, error) {
	a, err := r.nextArg(a)
	if err != nil {
		return nil, err
	}
	n.present[xtcpnl.TcaNetemLatency64] = 1
	v, ok := getTime64(a[0])
	if !ok {
		return nil, r.illegal(wordLatency)
	}
	n.latency64 = v
	if !nextIsNumber(a) {
		return a, nil
	}
	a = a[1:]
	n.present[xtcpnl.TcaNetemJitter64] = 1
	if n.jitter64, ok = getTime64(a[0]); !ok {
		return nil, r.illegal(wordLatency)
	}
	if !nextIsNumber(a) {
		return a, nil
	}
	a = a[1:]
	n.present[xtcpnl.TcaNetemCorr]++
	if n.cor.DelayCorr, ok = getPercent(a[0]); !ok {
		return nil, r.illegal(wordLatency)
	}
	return a, nil
}

// netemLossArg is the loss/drop arm: random (or a bare percent), state or
// gemodel. A second loss is refused with explain1's text and its stray
// newline.
func (r *runner) netemLossArg(n *netemArgs, a []string) ([]string, error) {
	if n.opt.Loss > 0 || n.lossType != 0 {
		return nil, r.illegal("duplicate loss argument\n")
	}
	a, err := r.nextArg(a)
	if err != nil {
		return nil, err
	}
	switch {
	case a[0] != "" && isDigitByte(a[0][0]):
		return r.netemRandomLoss(n, a)
	case a[0] == "random":
		if a, err = r.nextArg(a); err != nil {
			return nil, err
		}
		return r.netemRandomLoss(n, a)
	case a[0] == "state":
		return r.netemLossState(n, a)
	case a[0] == "gemodel":
		return r.netemLossGemodel(n, a)
	}
	r.errf("Unknown loss parameter: %s\n", a[0])
	return nil, errKindParse
}

func (r *runner) netemRandomLoss(n *netemArgs, a []string) ([]string, error) {
	var ok bool
	if n.opt.Loss, ok = getPercent(a[0]); !ok {
		return nil, r.illegal("loss percent")
	}
	if nextIsNumber(a) {
		a = a[1:]
		n.present[xtcpnl.TcaNetemCorr]++
		if n.cor.LossCorr, ok = getPercent(a[0]); !ok {
			return nil, r.illegal("loss correlation")
		}
	}
	return a, nil
}

// netemOptional reads up to len(dst) more percents while the next word is
// a number, each failure naming its own label.
func (r *runner) netemOptional(a []string, dst []*uint32, labels []string) ([]string, error) {
	for i := range dst {
		if !nextIsNumber(a) {
			return a, nil
		}
		a = a[1:]
		v, ok := getPercent(a[0])
		if !ok {
			return nil, r.illegal(labels[i])
		}
		*dst[i] = v
	}
	return a, nil
}

func (r *runner) netemLossState(n *netemArgs, a []string) ([]string, error) {
	a, err := r.nextArg(a)
	if err != nil {
		return nil, err
	}
	p13, rc := parsePercent(a[0])
	if rc != 0 {
		return nil, r.illegal("loss p13")
	}
	g := &n.gi
	g.P13, g.P31, g.P32, g.P23, g.P14 = setPercent(p13), setPercent(1.-p13), setPercent(0), setPercent(1.), setPercent(0)
	n.lossType = xtcpnl.NetemLossGI
	return r.netemOptional(a, []*uint32{&g.P31, &g.P32, &g.P23, &g.P14},
		[]string{"loss p31", "loss p32", "loss p23", "loss p14"})
}

func (r *runner) netemLossGemodel(n *netemArgs, a []string) ([]string, error) {
	a, err := r.nextArg(a)
	if err != nil {
		return nil, err
	}
	p, rc := parsePercent(a[0])
	if rc != 0 {
		return nil, r.illegal("loss gemodel p")
	}
	g := &n.ge
	g.P, g.R, g.H, g.K1 = setPercent(p), setPercent(1.-p), setPercent(0), setPercent(0)
	n.lossType = xtcpnl.NetemLossGE
	if a, err = r.netemOptional(a, []*uint32{&g.R}, []string{"loss gemodel r"}); err != nil || !nextIsNumber(a) {
		return a, err
	}
	a = a[1:]
	var ok bool
	if g.H, ok = getPercent(a[0]); !ok {
		return nil, r.illegal("loss gemodel h")
	}
	g.H = math.MaxUint32 - g.H // netem's option is 1-h; the kernel takes h
	return r.netemOptional(a, []*uint32{&g.K1}, []string{"loss gemodel k"})
}

func (r *runner) netemProbCorr(n *netemArgs, a []string, pc *xtcpnl.TcNetemProbCorr, label string) ([]string, error) {
	a, err := r.nextArg(a)
	if err != nil {
		return nil, err
	}
	var ok bool
	if pc.Probability, ok = getPercent(a[0]); !ok {
		return nil, r.illegal(label)
	}
	if nextIsNumber(a) {
		a = a[1:]
		n.present[xtcpnl.TcaNetemCorr]++
		if pc.Correlation, ok = getPercent(a[0]); !ok {
			return nil, r.illegal(label)
		}
	}
	return a, nil
}

// netemDuplicate is the duplicate arm. Its correlation does not count
// toward present[TCA_NETEM_CORR], so alone it is never sent (W lead).
func (r *runner) netemDuplicate(n *netemArgs, a []string) ([]string, error) {
	a, err := r.nextArg(a)
	if err != nil {
		return nil, err
	}
	var ok bool
	if n.opt.Duplicate, ok = getPercent(a[0]); !ok {
		return nil, r.illegal(wordDuplicate)
	}
	if nextIsNumber(a) {
		a = a[1:]
		if n.cor.DupCorr, ok = getPercent(a[0]); !ok {
			return nil, r.illegal(wordDuplicate)
		}
	}
	return a, nil
}

func (r *runner) netemRate(n *netemArgs, a []string, dev string) ([]string, error) {
	n.present[xtcpnl.TcaNetemRate]++
	a, err := r.nextArg(a)
	if err != nil {
		return nil, err
	}
	var ok bool
	if strings.Contains(a[0], "%") {
		n.rate64, ok = r.getPercentRate64(a[0], dev)
	} else {
		n.rate64, ok = getRate64(a[0])
	}
	if !ok {
		return nil, r.illegal(wordRate)
	}
	if nextIsSigned(a) {
		a = a[1:]
		if n.rate.PacketOverhead, ok = getS32(a[0], 0); !ok {
			return nil, r.illegal(wordRate)
		}
	}
	if nextIsNumber(a) {
		a = a[1:]
		if n.rate.CellSize, ok = getU32(a[0], 0); !ok {
			return nil, r.illegal(wordRate)
		}
	}
	if nextIsSigned(a) {
		a = a[1:]
		if n.rate.CellOverhead, ok = getS32(a[0], 0); !ok {
			return nil, r.illegal(wordRate)
		}
	}
	return a, nil
}

func (r *runner) netemSlot(n *netemArgs, a []string) ([]string, error) {
	var err error
	var ok bool
	s := &n.slot
	if nextIsNumber(a) {
		a = a[1:]
		n.present[xtcpnl.TcaNetemSlot] = 1
		if s.MinDelay, ok = getTime64(a[0]); !ok {
			return nil, r.illegal("slot min_delay")
		}
		if nextIsNumber(a) {
			a = a[1:]
			if s.MaxDelay, ok = getTime64(a[0]); !ok || s.MaxDelay < s.MinDelay {
				return nil, r.illegal("slot max_delay")
			}
		} else {
			s.MaxDelay = s.MinDelay
		}
	} else {
		if a, err = r.nextArg(a); err != nil {
			return nil, err
		}
		if a[0] != "distribution" {
			r.errf("Unknown slot parameter: %s\n", a[0])
			return nil, errKindParse
		}
		if a, err = r.netemSlotDist(n, a); err != nil {
			return nil, err
		}
	}
	if len(a) > 1 && matches(a[1], "packets") {
		a = a[1:]
		if len(a) < 2 {
			return nil, r.illegal("slot packets")
		}
		if s.MaxPackets, ok = getS32(a[1], 0); !ok {
			return nil, r.illegal("slot packets")
		}
		a = a[1:]
	}
	if len(a) > 1 && matches(a[1], "bytes") {
		a = a[1:]
		if len(a) < 2 {
			return nil, r.illegal("slot bytes")
		}
		v, ok := getSize(a[1])
		if !ok {
			return nil, r.illegal("slot bytes")
		}
		s.MaxBytes = cint.Int(v)
		a = a[1:]
	}
	return a, nil
}

// netemSlotDist is `slot distribution TYPE DELAY JITTER`.
func (r *runner) netemSlotDist(n *netemArgs, a []string) ([]string, error) {
	n.present[xtcpnl.TcaNetemSlot] = 1
	a, err := r.nextArg(a)
	if err != nil {
		return nil, err
	}
	d, sz := r.getDistribution(a[0])
	if sz <= 0 {
		return nil, errKindParse
	}
	n.slotDist = d
	if a, err = r.nextArg(a); err != nil {
		return nil, err
	}
	var ok bool
	if n.slot.DistDelay, ok = getTime64(a[0]); !ok {
		return nil, r.illegal("slot delay")
	}
	if a, err = r.nextArg(a); err != nil {
		return nil, err
	}
	if n.slot.DistJitter, ok = getTime64(a[0]); !ok {
		return nil, r.illegal("slot jitter")
	}
	if n.slot.DistJitter <= 0 {
		r.errf("Non-positive jitter\n")
		return nil, errKindParse
	}
	return a, nil
}

// netemCheck is the checks after the loop.
func (r *runner) netemCheck(n *netemArgs) error {
	fail := func(msg string) error {
		r.errf("%s", msg)
		r.netemExplain()
		return errKindParse
	}
	if n.reorder.Probability != 0 {
		if n.latency64 == 0 {
			return fail("reordering not possible without specifying some delay\n")
		}
		if n.opt.Gap == 0 {
			n.opt.Gap = 1
		}
	} else if n.opt.Gap > 0 {
		return fail("gap specified without reorder probability\n")
	}
	if n.present[xtcpnl.TcaNetemEcn] != 0 && n.opt.Loss == 0 && n.lossType == 0 {
		return fail("ecn requested without loss model\n")
	}
	if n.dist != nil && (n.latency64 == 0 || n.jitter64 == 0) {
		return fail("distribution specified but no latency and jitter values\n")
	}
	return nil
}

// netemAttrs lays out TCA_OPTIONS's payload, checking each addattr_l
// against its maxlen as tc does, from base (the TCA_OPTIONS offset).
type netemAttrs struct {
	b    []byte
	base int
}

func rtaAlign(n int) int { return (n + 3) &^ 3 }

// add is addattr_l(n, maxlen, typ, data).
func (p *netemAttrs) add(maxlen int, typ uint16, data []byte) error {
	if rtaAlign(4+len(data)) > xtcpnl.TcMaxlenRoom(maxlen, p.base+4+len(p.b)) {
		return errNetemBound
	}
	l, _ := cint.U16(4 + len(data))
	p.b = binary.LittleEndian.AppendUint16(p.b, l)
	p.b = binary.LittleEndian.AppendUint16(p.b, typ)
	p.b = append(p.b, data...)
	for len(p.b)%4 != 0 {
		p.b = append(p.b, 0)
	}
	return nil
}

func netemLE64(v uint64) []byte { return binary.LittleEndian.AppendUint64(nil, v) }

func s16s(d []int16) []byte {
	b := make([]byte, 0, 2*len(d))
	for _, v := range d {
		b = binary.LittleEndian.AppendUint16(b, cint.ShortBits(v))
	}
	return b
}

// put writes TCA_OPTIONS in tc's order.
func (n *netemArgs) put(ab *xtcpnl.AttrBuilder) error {
	opt := n.opt.Append(nil)
	if rtaAlign(4+len(opt)) > xtcpnl.TcMaxlenRoom(netemMaxlen, ab.Len()) {
		return errNetemBound
	}
	p := &netemAttrs{b: opt, base: ab.Len()}
	var err error
	add := func(maxlen int, on bool, typ uint16, data func() []byte) {
		if err == nil && on {
			err = p.add(maxlen, typ, data())
		}
	}
	pr := n.present
	add(netemMaxlen, pr[xtcpnl.TcaNetemLatency64] != 0, xtcpnl.TcaNetemLatency64,
		func() []byte { return netemLE64(cint.U64Bits(n.latency64)) })
	add(netemMaxlen, pr[xtcpnl.TcaNetemJitter64] != 0, xtcpnl.TcaNetemJitter64,
		func() []byte { return netemLE64(cint.U64Bits(n.jitter64)) })
	add(netemMaxlen, pr[xtcpnl.TcaNetemCorr] != 0, xtcpnl.TcaNetemCorr, func() []byte { return n.cor.Append(nil) })
	add(netemMaxlen, pr[xtcpnl.TcaNetemReorder] != 0, xtcpnl.TcaNetemReorder,
		func() []byte { return n.reorder.Append(nil) })
	add(netemMaxlen, pr[xtcpnl.TcaNetemEcn] != 0, xtcpnl.TcaNetemEcn, func() []byte {
		u, _ := cint.U32(pr[xtcpnl.TcaNetemEcn])
		return binary.LittleEndian.AppendUint32(nil, u)
	})
	add(netemMaxlen, pr[xtcpnl.TcaNetemCorrupt] != 0, xtcpnl.TcaNetemCorrupt,
		func() []byte { return n.corrupt.Append(nil) })
	add(netemMaxlen, pr[xtcpnl.TcaNetemSlot] != 0, xtcpnl.TcaNetemSlot, func() []byte { return n.slot.Append(nil) })
	if err == nil && n.lossType != 0 {
		err = n.putLoss(p)
	}
	if pr[xtcpnl.TcaNetemRate] != 0 {
		if n.rate64 >= 1<<32 {
			add(netemMaxlen, true, xtcpnl.TcaNetemRate64, func() []byte { return netemLE64(n.rate64) })
			n.rate.Rate = math.MaxUint32
		} else {
			n.rate.Rate = cint.Low32(n.rate64)
		}
		add(netemMaxlen, true, xtcpnl.TcaNetemRate, func() []byte { return n.rate.Append(nil) })
	}
	add(netemMaxlen, pr[xtcpnl.TcaNetemPrngSeed] != 0, xtcpnl.TcaNetemPrngSeed, func() []byte { return netemLE64(n.seed) })
	add(netemMaxDist*2, n.dist != nil, xtcpnl.TcaNetemDelayDist, func() []byte { return s16s(n.dist) })
	add(netemMaxDist*2, n.slotDist != nil, xtcpnl.TcaNetemSlotDist, func() []byte { return s16s(n.slotDist) })
	if err != nil {
		return err
	}
	return ab.PutBytes(xtcpnl.TcaOptions, p.b)
}

// putLoss is TCA_NETEM_LOSS|NLA_F_NESTED holding the GI or GE model.
func (n *netemArgs) putLoss(p *netemAttrs) error {
	if err := p.add(netemMaxlen, xtcpnl.TcaNetemLoss|unix.NLA_F_NESTED, nil); err != nil {
		return err
	}
	start := len(p.b) - 4
	var err error
	if n.lossType == xtcpnl.NetemLossGI {
		err = p.add(netemMaxlen, xtcpnl.NetemLossGI, n.gi.Append(nil))
	} else {
		err = p.add(netemMaxlen, xtcpnl.NetemLossGE, n.ge.Append(nil))
	}
	if err != nil {
		return err
	}
	l, _ := cint.U16(len(p.b) - start)
	binary.LittleEndian.PutUint16(p.b[start:], l)
	return nil
}
