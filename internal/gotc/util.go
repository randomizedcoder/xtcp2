package gotc

import (
	"fmt"
	"math"
	"strconv"
)

// This is the part of tc/tc_util.c, tc/tc_core.c and lib/utils.c the shows
// print through: print_tc_classid, print_devname, sprint_time,
// sprint_time64, sprint_linklayer and tc_calc_xmitsize.

// The command words more than one parser tests.
const (
	wordDev     = "dev"
	wordRoot    = "root"
	wordNone    = "none"
	wordQdisc   = "qdisc"
	wordIngress = "ingress"
	wordEgress  = "egress"
	wordParent  = "parent"
)

// The tc handle values (pkt_sched.h).
const (
	tcHRoot    uint32 = 0xFFFFFFFF // TC_H_ROOT
	tcHIngress uint32 = 0xFFFFFFF1 // TC_H_INGRESS, also TC_H_CLSACT
	tcHUnspec  uint32 = 0          // TC_H_UNSPEC
	tcHMajMask uint32 = 0xFFFF0000 // TC_H_MAJ_MASK
	tcHMinMask uint32 = 0x0000FFFF // TC_H_MIN_MASK

	nsecPerSec  = 1e9
	nsecPerMsec = 1e6
	nsecPerUsec = 1e3
)

func tcHMaj(h uint32) uint32 { return h & tcHMajMask }
func tcHMin(h uint32) uint32 { return h & tcHMinMask }

// classid is print_tc_classid: "root", "none", ":min", "maj:" or
// "maj:min" in hex, prefixed "NAME#" under -nm when the names database
// knows h.
func (r *runner) classid(h uint32) string {
	var handle string
	switch {
	case h == tcHRoot:
		handle = wordRoot
	case h == tcHUnspec:
		handle = wordNone
	case tcHMaj(h) == 0:
		handle = ":" + strconv.FormatUint(uint64(tcHMin(h)), 16)
	case tcHMin(h) == 0:
		handle = strconv.FormatUint(uint64(tcHMaj(h)>>16), 16) + ":"
	default:
		handle = strconv.FormatUint(uint64(tcHMaj(h)>>16), 16) + ":" + strconv.FormatUint(uint64(tcHMin(h)), 16)
	}
	if r.useNames && r.names != nil {
		if name, ok := r.names.lookup(h); ok {
			return name + "#" + handle
		}
	}
	return handle
}

// devname is print_devname: "dev " outside JSON, then the colored name and
// a space; in JSON the "dev" key.
func (r *runner) devname(t outType, ifindex uint32) error {
	name, err := r.indexToName(ifindex)
	if err != nil {
		return err
	}
	if !r.o.isJSON() {
		r.o.raw("dev ")
	}
	r.o.str(t, colorIfname, wordDev, name+" ", name)
	return nil
}

// sprintTime is lib/utils.c print_time: microseconds as "%.3gs",
// "%.3gms" or "%uus".
func sprintTime(t uint32) string {
	tmp := float64(t)
	switch {
	case tmp >= timeUnitsPerSec:
		return cGPrec(tmp/timeUnitsPerSec, 3) + "s"
	case tmp >= timeUnitsPerSec/1000:
		return cGPrec(tmp/(timeUnitsPerSec/1000), 3) + "ms"
	}
	return strconv.FormatUint(uint64(t), 10) + "us"
}

// sprintTime64 is lib/utils.c print_time64: nanoseconds.
func sprintTime64(t int64) string {
	ns := float64(t)
	switch {
	case t >= nsecPerSec:
		return cGPrec(ns/nsecPerSec, 3) + "s"
	case t >= nsecPerMsec:
		return cGPrec(ns/nsecPerMsec, 3) + "ms"
	case t >= nsecPerUsec:
		return cGPrec(ns/nsecPerUsec, 3) + "us"
	}
	return strconv.FormatInt(t, 10) + "ns"
}

// cGPrec is C's "%.<prec>g".
func cGPrec(v float64, prec int) string {
	return strconv.FormatFloat(v, 'g', prec, 64)
}

// sprintLinklayer is sprint_linklayer.
func sprintLinklayer(ll uint8) string {
	switch ll {
	case 0:
		return "unspec"
	case 1:
		return "ethernet"
	case 2:
		return "atm"
	}
	return "unknown"
}

// xmitsize is tc_calc_xmitsize: ((double)rate * tc_core_tick2time(ticks))
// / TIME_UNITS_PER_SEC, truncated to a __u64 as C converts a double.
func (r *runner) xmitsize(rate uint64, ticks uint32) uint64 {
	v := float64(rate) * r.core.tick2time(float64(ticks)) / timeUnitsPerSec
	return cDoubleToU64(v)
}

// tick2time is tc_core_tick2time.
func (c tcCore) tick2time(tick float64) float64 { return tick / c.tickInUsec }

// cDoubleToU64 is C's (__u64) of a double on x86-64: truncation toward
// zero; out of range (NaN, negative, too big) gives what cvttsd2si does,
// 0x8000000000000000 for the big ones above 2^63.
func cDoubleToU64(v float64) uint64 {
	switch {
	case math.IsNaN(v) || v <= -1:
		return 0x8000000000000000
	case v < 0:
		return 0
	case v < 1<<63:
		return uint64(v)
	case v < 1<<64:
		return uint64(v-(1<<63)) + 1<<63
	}
	return 0x8000000000000000
}

// cDoubleToU32 is C's (unsigned int) of a double on x86-64: through the
// 64-bit signed conversion, then the low 32 bits.
func cDoubleToU32(v float64) uint32 {
	if math.IsNaN(v) || v >= 1<<63 || v < -(1<<63) {
		return 0
	}
	return uint32(uint64(int64(v)) & math.MaxUint32)
}

// errKind names an out-of-slice kind gotc refuses to print.
func errKind(kind string) error {
	return fmt.Errorf("%w: qdisc kind %q", errNotImplemented, kind)
}
