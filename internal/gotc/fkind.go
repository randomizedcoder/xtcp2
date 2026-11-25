package gotc

import (
	"encoding/binary"
	"fmt"
	"net/netip"
	"strconv"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// tc.c get_filter_kind and the print_fopt of each slice kind: tc/f_u32.c,
// f_fw.c, f_route.c, f_basic.c, f_cgroup.c, f_flow.c and f_matchall.c.
// print_filter ignores what a printer returns, so one that "returns -1"
// here just stops.

// filterPrinter is a struct filter_util's print_fopt.
type filterPrinter func(r *runner, opt []byte, handle uint32) error

// filterUtils are the kinds gotc prints.
var filterUtils = map[string]filterPrinter{
	xtcpnl.TcFKindU32:      printU32Opt,
	xtcpnl.TcFKindFw:       printFwOpt,
	xtcpnl.TcFKindRoute:    printRouteOpt,
	xtcpnl.TcFKindBasic:    printBasicOpt,
	xtcpnl.TcFKindCgroup:   printCgroupOpt,
	xtcpnl.TcFKindFlow:     printFlowOpt,
	xtcpnl.TcFKindMatchall: printMatchallOpt,
	xtcpnl.TcFKindFlower:   printFlowerOpt,
}

// tcBuiltinFilters is every filter_util id the tc binary links (tc/Makefile
// at v7.2.0): one gotc does not print is refused rather than misprinted. A
// kind outside it is dlsym's miss, which tc prints with print_nofopt.
var tcBuiltinFilters = map[string]bool{
	"u32": true, "route": true, "fw": true, "basic": true, "bpf": true,
	"flow": true, "cgroup": true, "flower": true, "matchall": true,
}

// printFopt is get_filter_kind's print_fopt for kind.
func (r *runner) printFopt(kind string, opt []byte, handle uint32) error {
	if p, ok := filterUtils[kind]; ok {
		return p(r, opt, handle)
	}
	if tcBuiltinFilters[kind] {
		return fmt.Errorf("%w: filter kind %q", errNotImplemented, kind)
	}
	// print_nofopt: fprintf to stdout, so bare text inside -j's JSON (L46).
	switch {
	case len(opt) > 0:
		r.o.raw(fmt.Sprintf("fh %08x [Unknown filter, optlen=%d] ", handle, len(opt)))
	case handle != 0:
		r.o.raw(fmt.Sprintf("fh %08x ", handle))
	}
	return nil
}

// printFilterPolice is tc_print_police, a filter's TCA_*_POLICE: u32,
// basic and cgroup print a newline before it, fw, route and flow do not;
// its return is ignored.
func (r *runner) printFilterPolice(b []byte, nl bool) error {
	if nl {
		r.o.nl()
	}
	_, err := r.printPolice(b, true)
	return err
}

// sprintU32Handle is f_u32.c sprint_u32_handle, with -r's "[%08x]".
func (r *runner) sprintU32Handle(h uint32) string {
	if h == 0 {
		return wordNone
	}
	s := ""
	htid, hash, node := h&0xFFF00000, h>>12&0xFF, h&0xFFF
	if htid != 0 {
		s = strconv.FormatUint(uint64(htid>>20), 16) + ":"
	}
	if hash != 0 {
		s += strconv.FormatUint(uint64(hash), 16)
	}
	if node != 0 {
		s += ":" + strconv.FormatUint(uint64(node), 16)
	}
	if r.raw > 0 {
		s += fmt.Sprintf("[%08x]", h)
	}
	return s
}

// clsFlags is u32's TCA_U32_FLAGS bools; matchall prints the same with a
// "\n  " before each word (L47).
func (r *runner) clsFlags(flags uint32, pre, post string) {
	o := r.o
	if flags&xtcpnl.TcaClsFlagsSkipHw != 0 {
		o.boolean(printAny, "skip_hw", pre+"skip_hw"+post, true)
	}
	if flags&xtcpnl.TcaClsFlagsSkipSw != 0 {
		o.boolean(printAny, "skip_sw", pre+"skip_sw"+post, true)
	}
	if flags&xtcpnl.TcaClsFlagsInHw != 0 {
		o.boolean(printAny, "in_hw", pre+"in_hw"+post, true)
	} else if flags&xtcpnl.TcaClsFlagsNotInHw != 0 {
		o.boolean(printAny, "not_in_hw", pre+"not_in_hw"+post, true)
	}
}

func i32s(v int32) string { return strconv.FormatInt(int64(v), 10) }

// printU32Opt is u32_print_opt.
func printU32Opt(r *runner, opt []byte, handle uint32) error {
	o := r.o
	tb := r.nested(opt, xtcpnl.TcaU32MaxKnown)
	if handle != 0 {
		h := r.sprintU32Handle(handle)
		o.str(printAny, colorNone, "fh", "fh "+h+" ", h)
	}
	if node := handle & 0xFFF; node != 0 {
		n := cint.Int(node)
		o.int(printAny, "order", "order "+i32s(n)+" ", n)
	}
	var sel *xtcpnl.TcU32Sel
	if tb.Has(xtcpnl.TcaU32Sel) {
		s, err := xtcpnl.DecodeTcU32Sel(tb.Bytes(xtcpnl.TcaU32Sel))
		if err != nil {
			return nil // RTA_PAYLOAD < sizeof(*sel): return -1 (F17)
		}
		sel = &s
	}
	switch {
	case tb.Has(xtcpnl.TcaU32Divisor):
		d := cint.Int(attrU32(tb, xtcpnl.TcaU32Divisor))
		o.int(printAny, "ht_divisor", "ht divisor "+i32s(d)+" ", d)
	case tb.Has(xtcpnl.TcaU32Hash):
		htid := attrU32(tb, xtcpnl.TcaU32Hash)
		user, bkt := htid>>20&0xFFF, htid>>12&0xFF
		o.hex(printAny, "key_ht", "key ht "+u32hex(user)+" ", user)
		o.hex(printAny, "bkt", "bkt "+u32hex(bkt)+" ", bkt)
	default:
		r.errf("divisor and hash missing ")
	}
	terminal := sel != nil && sel.Flags&xtcpnl.TcU32Terminal != 0
	if tb.Has(xtcpnl.TcaU32Classid) {
		if !terminal {
			o.fp("*")
		}
		c := r.classid(attrU32(tb, xtcpnl.TcaU32Classid))
		o.str(printAny, colorNone, "flowid", "flowid "+c+" ", c)
	} else if terminal {
		o.fp("terminal flowid ")
	}
	if tb.Has(xtcpnl.TcaU32Link) {
		l := r.sprintU32Handle(attrU32(tb, xtcpnl.TcaU32Link))
		o.str(printAny, colorNone, "link", "link "+l+" ", l)
	}
	if tb.Has(xtcpnl.TcaU32Flags) {
		r.clsFlags(attrU32(tb, xtcpnl.TcaU32Flags), "", " ")
	}
	var pf *xtcpnl.TcU32Pcnt
	if tb.Has(xtcpnl.TcaU32Pcnt) {
		p, err := xtcpnl.DecodeTcU32Pcnt(tb.Bytes(xtcpnl.TcaU32Pcnt))
		if err != nil {
			r.errf("Broken perf counters\n")
			return nil
		}
		pf = &p
	}
	if sel != nil && r.stats > 0 && pf != nil {
		o.u64(printAny, "rule_hit", "(rule hit "+u64s(pf.Rcnt)+" ", pf.Rcnt)
		o.u64(printAny, "success", "success "+u64s(pf.Rhit)+")", pf.Rhit)
	}
	if tb.Has(xtcpnl.TcaU32Mark) {
		if m, err := xtcpnl.DecodeTcU32Mark(tb.Bytes(xtcpnl.TcaU32Mark)); err != nil {
			r.errf("Invalid mark (kernel&iproute2 mismatch)\n")
		} else {
			o.nl()
			o.hex0x(printAny, "fwmark_value", fmt.Sprintf("  mark 0x%04x ", m.Val), uint64(m.Val))
			o.hex0x(printAny, "fwmark_mask", fmt.Sprintf("0x%04x ", m.Mask), uint64(m.Mask))
			s := cint.Int(m.Success) // print_int (F11)
			o.int(printAny, "fwmark_success", "(success "+i32s(s)+")", s)
		}
	}
	if sel != nil {
		if err := r.u32Sel(sel, pf); err != nil {
			return err
		}
	}
	if tb.Has(xtcpnl.TcaU32Police) {
		if err := r.printFilterPolice(tb.Bytes(xtcpnl.TcaU32Police), true); err != nil {
			return err
		}
	}
	if tb.Has(xtcpnl.TcaU32Indev) {
		dev, _ := tb.Str(xtcpnl.TcaU32Indev)
		o.nl()
		o.str(printAny, colorNone, "input_dev", "  input dev "+dev, dev)
		o.nl()
	}
	if tb.Has(xtcpnl.TcaU32Act) {
		return r.printActions(tb.Bytes(xtcpnl.TcaU32Act))
	}
	return nil
}

// u32Sel is u32_print_opt's keys, offset, eat and hash mask.
func (r *runner) u32Sel(sel *xtcpnl.TcU32Sel, pf *xtcpnl.TcU32Pcnt) error {
	o := r.o
	for i := range int(sel.NKeys) {
		if i >= len(sel.Keys) {
			// tc reads keys[i] past the attribute (L5): whatever follows.
			return fmt.Errorf("%w: a u32 sel of %d keys holding %d (L5)", errNotImplemented, sel.NKeys, len(sel.Keys))
		}
		r.showKeys(sel.Keys[i])
		if r.stats > 0 && pf != nil {
			if i >= len(pf.Kcnts) {
				return fmt.Errorf("%w: a u32 pcnt of %d counts for %d keys (F12)", errNotImplemented, len(pf.Kcnts), sel.NKeys)
			}
			o.u64(printAny, "success", " (success "+u64s(pf.Kcnts[i])+" ) ", pf.Kcnts[i])
		}
	}
	if sel.Flags&(xtcpnl.TcU32VarOffset|xtcpnl.TcU32Offset) != 0 {
		o.nl()
		o.fp("    offset ")
		if sel.Flags&xtcpnl.TcU32VarOffset != 0 {
			m := uint32(sel.Offmask)
			o.hex(printAny, "offset_mask", fmt.Sprintf("%04x", m), m)
			sh := int32(sel.Offshift)
			o.int(printAny, "offset_shift", ">>"+i32s(sh)+" ", sh)
			off := int32(sel.Offoff)
			o.int(printAny, "offset_off", "at "+i32s(off)+" ", off)
		}
		if sel.Off != 0 {
			p := int32(sel.Off)
			o.int(printAny, "plus", "plus "+i32s(p)+" ", p)
		}
	}
	if sel.Flags&xtcpnl.TcU32Eat != 0 {
		o.str(printAny, colorNone, "", " eat ", " eat ") // no key inside the object (L1)
	}
	if sel.Hmask != 0 {
		o.nl()
		o.hex(printAny, "hash_mask", fmt.Sprintf("    hash mask %08x ", sel.Hmask), sel.Hmask)
		h := int32(sel.Hoff)
		o.int(printAny, "hash_off", "at "+i32s(h)+" ", h)
	}
	return nil
}

// The u32_pprinters protocols.
const (
	ethPIP   = 0x0800
	ethPIPv6 = 0x86DD
)

// showKeys is f_u32.c show_keys: print_raw, or under -p the printer of
// f_proto, the last filter's protocol (L4, F4, F13).
func (r *runner) showKeys(k xtcpnl.TcU32Key) {
	if r.o.pretty {
		switch u32Proto(r.fProto) {
		case ethPIP:
			r.printU32IP(k, false)
			return
		case ethPIPv6:
			r.printU32IP(k, true)
			return
		}
	}
	r.printU32Raw(k)
}

// u32Proto is ntohs(f_proto).
func u32Proto(p uint16) uint16 { return p>>8 | p<<8 }

// printU32Raw is print_raw.
func (r *runner) printU32Raw(k xtcpnl.TcU32Key) {
	o := r.o
	o.openObject("match")
	o.nl()
	o.hex(printAny, "value", fmt.Sprintf("  match %08x", k.Val), k.Val)
	o.hex(printAny, "mask", fmt.Sprintf("/%08x ", k.Mask), k.Mask)
	nh := ""
	if k.Offmask != 0 {
		nh = "nexthdr+"
	}
	o.str(printAny, colorNone, "offmask", "at "+nh, nh)
	o.int(printAny, "off", i32s(k.Off), k.Off)
	o.closeObject()
}

// mask2bits is lib/utils.c mask2bits of a host-order mask.
func mask2bits(mask uint32) int32 {
	host := ^mask
	if host&(host+1) != 0 {
		return -1
	}
	var b int32
	for ; mask != 0; mask <<= 1 {
		b++
	}
	return b
}

// printU32IP is print_ipv4, or print_ipv6 (v6): the same but for the port
// keys, and with IPv4 offsets and addresses in both (L4); IPv4's dport
// has no newline or indent (F13).
func (r *runner) printU32IP(k xtcpnl.TcU32Key, v6 bool) {
	o := r.o
	o.openObject("match")
	switch k.Off {
	case 0:
		switch k.Mask {
		case 0x0f000000:
			o.nl()
			v := k.Val >> 24
			o.uint(printAny, "ip_ihl", "  match IP ihl "+u32s(v), v)
		case 0x00ff0000:
			o.nl()
			v := uint64(k.Val >> 16)
			o.hex0x(printAny, "ip_dsfield", "  match IP dsfield "+cHashX(v), v)
		}
	case 8:
		if k.Mask == 0x00ff0000 {
			o.nl()
			v := cint.Int(k.Val >> 16)
			o.int(printAny, "ip_protocol", "  match IP protocol "+i32s(v), v)
		}
	case 12, 16:
		if bitsN := mask2bits(k.Mask); bitsN >= 0 {
			o.nl()
			if k.Off == 12 {
				o.fp("  match IP src ")
				o.openObject("src")
			} else {
				o.fp("  match IP dst ")
				o.openObject("dst")
			}
			var a [4]byte
			binary.BigEndian.PutUint32(a[:], k.Val)
			addr := netip.AddrFrom4(a).String()
			o.str(printAny, colorNone, "address", addr, addr)
			o.int(printAny, "prefixlen", "/"+i32s(bitsN), bitsN)
			o.closeObject()
		}
	case 20:
		lo, hi := k.Val&0xffff, k.Val>>16
		switch {
		case k.Mask == 0x0000ffff && !v6:
			o.uint(printAny, "dport", "match dport "+u32s(lo), lo)
		case k.Mask == 0x0000ffff:
			o.nl()
			o.uint(printAny, "sport", "  match sport "+u32s(lo), lo)
		case k.Mask == 0xffff0000 && !v6:
			o.nl()
			o.uint(printAny, "sport", "  match sport "+u32s(hi), hi)
		case k.Mask == 0xffff0000:
			o.uint(printAny, "dport", "match dport "+u32s(hi), hi)
		case k.Mask == 0xffffffff && !v6:
			o.nl()
			o.uint(printAny, "dport", "  match dport "+u32s(lo)+", ", lo)
			o.uint(printAny, "sport", "match sport "+u32s(hi), hi)
		case k.Mask == 0xffffffff:
			o.nl()
			o.uint(printAny, "sport", "  match sport "+u32s(lo)+", ", lo)
			o.uint(printAny, "dport", "match dport "+u32s(hi), hi)
		}
	}
	o.closeObject()
}

// printFwOpt is fw_print_opt.
func printFwOpt(r *runner, opt []byte, handle uint32) error {
	o := r.o
	tb := r.nested(opt, xtcpnl.TcaFwMaxKnown)
	if handle != 0 || tb.Has(xtcpnl.TcaFwMask) {
		o.openObject("fw")
		mark := handle
		if mask := attrU32(tb, xtcpnl.TcaFwMask); tb.Has(xtcpnl.TcaFwMask) && mask != 0xFFFFFFFF {
			o.hex0x(printAny, "mark", fmt.Sprintf("handle 0x%x", mark), uint64(mark))
			o.hex0x(printAny, "mask", fmt.Sprintf("/0x%x ", mask), uint64(mask))
		} else {
			o.hex0x(printAny, "mark", fmt.Sprintf("handle 0x%x ", mark), uint64(mark))
			o.hex0x(printJSON, "mask", "", 0xFFFFFFFF)
		}
		o.closeObject()
	}
	if tb.Has(xtcpnl.TcaFwClassid) {
		c := r.classid(attrU32(tb, xtcpnl.TcaFwClassid))
		o.str(printAny, colorNone, "classid", "classid "+c+" ", c)
	}
	if tb.Has(xtcpnl.TcaFwPolice) {
		if err := r.printFilterPolice(tb.Bytes(xtcpnl.TcaFwPolice), false); err != nil {
			return err
		}
	}
	if tb.Has(xtcpnl.TcaFwIndev) {
		dev, _ := tb.Str(xtcpnl.TcaFwIndev)
		o.str(printAny, colorNone, "indev", "input dev "+dev+" ", dev)
	}
	if tb.Has(xtcpnl.TcaFwAct) {
		o.nl()
		return r.printActions(tb.Bytes(xtcpnl.TcaFwAct))
	}
	return nil
}

// printRouteOpt is route_print_opt: `to` and `from` both under "name"
// (F3), and no space after `fromif` (F14).
func printRouteOpt(r *runner, opt []byte, handle uint32) error {
	o := r.o
	tb := r.nested(opt, xtcpnl.TcaRoute4MaxKnown)
	if handle != 0 {
		o.hex0x(printAny, "fh", fmt.Sprintf("fh 0x%08x ", handle), uint64(handle))
	}
	if handle&0x7F00 != 0 {
		ord := handle >> 8 & 0x7F
		o.uint(printAny, "order", "order "+u32s(ord)+" ", ord)
	}
	if tb.Has(xtcpnl.TcaRoute4Classid) {
		c := r.classid(attrU32(tb, xtcpnl.TcaRoute4Classid))
		o.str(printAny, colorNone, "flowid", "flowid "+c+" ", c)
	}
	if tb.Has(xtcpnl.TcaRoute4To) {
		n := r.rtrealmN2A(cint.Int(attrU32(tb, xtcpnl.TcaRoute4To)))
		o.str(printAny, colorNone, "name", "to "+n+" ", n)
	}
	if tb.Has(xtcpnl.TcaRoute4From) {
		n := r.rtrealmN2A(cint.Int(attrU32(tb, xtcpnl.TcaRoute4From)))
		o.str(printAny, colorNone, "name", "from "+n+" ", n)
	}
	if tb.Has(xtcpnl.TcaRoute4Iif) {
		name, err := r.indexToName(attrU32(tb, xtcpnl.TcaRoute4Iif))
		if err != nil {
			return err
		}
		o.str(printAny, colorIfname, "fromif", "fromif "+name, name)
	}
	if tb.Has(xtcpnl.TcaRoute4Police) {
		if err := r.printFilterPolice(tb.Bytes(xtcpnl.TcaRoute4Police), false); err != nil {
			return err
		}
	}
	if tb.Has(xtcpnl.TcaRoute4Act) {
		return r.printActions(tb.Bytes(xtcpnl.TcaRoute4Act))
	}
	return nil
}

// printBasicOpt is basic_print_opt: print_hex's handle, a JSON string of
// bare hex (F18).
func printBasicOpt(r *runner, opt []byte, handle uint32) error {
	o := r.o
	tb := r.nested(opt, xtcpnl.TcaBasicMaxKnown)
	if handle != 0 {
		o.hex(printAny, "handle", "handle 0x"+u32hex(handle)+" ", handle)
	}
	if tb.Has(xtcpnl.TcaBasicClassid) {
		c := r.classid(attrU32(tb, xtcpnl.TcaBasicClassid))
		o.str(printAny, colorNone, "flowid", "flowid "+c+" ", c)
	}
	if tb.Has(xtcpnl.TcaBasicEmatches) {
		if err := r.printEmatch(tb.Bytes(xtcpnl.TcaBasicEmatches)); err != nil {
			return err
		}
	}
	if tb.Has(xtcpnl.TcaBasicPolice) {
		if err := r.printFilterPolice(tb.Bytes(xtcpnl.TcaBasicPolice), true); err != nil {
			return err
		}
	}
	if tb.Has(xtcpnl.TcaBasicAct) {
		return r.printActions(tb.Bytes(xtcpnl.TcaBasicAct))
	}
	return nil
}

// printCgroupOpt is cgroup_print_opt: print_0xhex's handle, "0x1" in
// JSON (F18).
func printCgroupOpt(r *runner, opt []byte, handle uint32) error {
	o := r.o
	tb := r.nested(opt, xtcpnl.TcaCgroupMaxKnown)
	if handle != 0 {
		o.hex0x(printAny, "handle", "handle "+cHashX(uint64(handle))+" ", uint64(handle))
	}
	if tb.Has(xtcpnl.TcaCgroupEmatches) {
		if err := r.printEmatch(tb.Bytes(xtcpnl.TcaCgroupEmatches)); err != nil {
			return err
		}
	}
	if tb.Has(xtcpnl.TcaCgroupPolice) {
		if err := r.printFilterPolice(tb.Bytes(xtcpnl.TcaCgroupPolice), true); err != nil {
			return err
		}
	}
	if tb.Has(xtcpnl.TcaCgroupAct) {
		return r.printActions(tb.Bytes(xtcpnl.TcaCgroupAct))
	}
	return nil
}

// flowKeys is f_flow.c flow_keys.
var flowKeys = [xtcpnl.FlowKeyMaxCst + 1]string{
	"src", "dst", "proto", "proto-src", "proto-dst", "iif", "priority", "mark", "nfct",
	"nfct-src", "nfct-dst", "nfct-proto-src", "nfct-proto-dst", "rt-classid", "sk-uid",
	"sk-gid", "vlan-tag", "rxhash",
}

// printFlowOpt is flow_print_opt.
func printFlowOpt(r *runner, opt []byte, handle uint32) error {
	o := r.o
	tb := r.nested(opt, xtcpnl.TcaFlowMaxKnown)
	o.hex0x(printAny, "handle", "handle "+cHashX(uint64(handle))+" ", uint64(handle))
	if tb.Has(xtcpnl.TcaFlowMode) {
		m := "map"
		switch mode := attrU32(tb, xtcpnl.TcaFlowMode); mode {
		case xtcpnl.FlowModeMap:
		case xtcpnl.FlowModeHash:
			m = "hash"
		default:
			m = cHashX(uint64(mode))
		}
		o.str(printAny, colorNone, "mode", m+" ", m)
	}
	if tb.Has(xtcpnl.TcaFlowKeys) {
		keymask := attrU32(tb, xtcpnl.TcaFlowKeys)
		sep := " "
		o.openArray(printAny, "keys")
		for i, k := range flowKeys {
			if keymask&(1<<i) != 0 {
				o.fp(sep)
				o.str(printAny, colorNone, "", k, k)
				sep = ","
			}
		}
		o.closeArray(printAny, " ")
	}
	mask, val := uint32(0xFFFFFFFF), uint32(0)
	if tb.Has(xtcpnl.TcaFlowMask) {
		mask = attrU32(tb, xtcpnl.TcaFlowMask)
	}
	if tb.Has(xtcpnl.TcaFlowXor) {
		val = attrU32(tb, xtcpnl.TcaFlowXor)
	}
	if mask != 0xFFFFFFFF || val != 0 {
		or, xor := (mask&val)^val, mask&val
		if mask != 0xFFFFFFFF {
			o.hex0x(printAny, "and", fmt.Sprintf("and 0x%.8x ", mask), uint64(mask))
		}
		if xor != 0 {
			o.hex0x(printAny, "xor", fmt.Sprintf("xor 0x%.8x ", xor), uint64(xor))
		}
		if or != 0 {
			o.hex0x(printAny, "or", fmt.Sprintf("or 0x%.8x ", or), uint64(or))
		}
	}
	if tb.Has(xtcpnl.TcaFlowRshift) {
		v := attrU32(tb, xtcpnl.TcaFlowRshift)
		o.uint(printAny, "rshift", "rshift "+u32s(v)+" ", v)
	}
	if tb.Has(xtcpnl.TcaFlowAddend) {
		v := attrU32(tb, xtcpnl.TcaFlowAddend)
		o.hex0x(printAny, "addend", "addend 0x"+u32hex(v)+" ", uint64(v))
	}
	if tb.Has(xtcpnl.TcaFlowDivisor) {
		v := attrU32(tb, xtcpnl.TcaFlowDivisor)
		o.uint(printAny, "divisor", "divisor "+u32s(v)+" ", v)
	}
	if tb.Has(xtcpnl.TcaFlowBaseclass) {
		c := r.classid(attrU32(tb, xtcpnl.TcaFlowBaseclass))
		o.str(printAny, colorNone, "baseclass", "baseclass "+c+" ", c)
	}
	if tb.Has(xtcpnl.TcaFlowPerturb) {
		v := attrU32(tb, xtcpnl.TcaFlowPerturb)
		o.uint(printAny, "perturb", "perturb "+u32s(v)+"sec ", v)
	}
	if tb.Has(xtcpnl.TcaFlowEmatches) {
		if err := r.printEmatch(tb.Bytes(xtcpnl.TcaFlowEmatches)); err != nil {
			return err
		}
	}
	if tb.Has(xtcpnl.TcaFlowPolice) {
		if err := r.printFilterPolice(tb.Bytes(xtcpnl.TcaFlowPolice), false); err != nil {
			return err
		}
	}
	if tb.Has(xtcpnl.TcaFlowAct) {
		o.nl()
		return r.printActions(tb.Bytes(xtcpnl.TcaFlowAct))
	}
	return nil
}

// printMatchallOpt is matchall_print_opt: print_uint's handle, a JSON
// number (F18); each flag after a hard "\n  " (L47); and "Broken perf
// counters" on stdout.
func printMatchallOpt(r *runner, opt []byte, handle uint32) error {
	o := r.o
	tb := r.nested(opt, xtcpnl.TcaMatchallMaxKnown)
	if handle != 0 {
		o.uint(printAny, "handle", "handle 0x"+u32hex(handle)+" ", handle)
	}
	if tb.Has(xtcpnl.TcaMatchallClassid) {
		c := r.classid(attrU32(tb, xtcpnl.TcaMatchallClassid))
		o.str(printAny, colorNone, "flowid", "flowid "+c+" ", c)
	}
	if tb.Has(xtcpnl.TcaMatchallFlags) {
		r.clsFlags(attrU32(tb, xtcpnl.TcaMatchallFlags), "\n  ", "")
	}
	var rhit uint64
	hasPf := false
	if b := tb.Bytes(xtcpnl.TcaMatchallPcnt); b != nil {
		if len(b) < xtcpnl.TcMatchallPcntSizeCst {
			o.fp("Broken perf counters\n")
			return nil
		}
		rhit, hasPf = binary.LittleEndian.Uint64(b), true
	}
	if r.stats > 0 && hasPf {
		o.u64(printAny, "rule_hit", " (rule hit "+u64s(rhit)+")", rhit)
	}
	if tb.Has(xtcpnl.TcaMatchallAct) {
		return r.printActions(tb.Bytes(xtcpnl.TcaMatchallAct))
	}
	return nil
}
