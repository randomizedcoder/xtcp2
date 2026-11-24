package gotc

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// tc/m_ematch.c print_ematch and the print_eopt of em_cmp.c, em_u32.c,
// em_nbyte.c, em_canid.c and em_meta.c. Every one writes with fprintf to
// stdout, so under -j the text lands bare inside the JSON (F1): here
// through out.raw, which bypasses the JSON writer the same way.

// ematchMapName is the ematch_map file get_ematch_kind_num reads, from
// CONF_ETC_DIR and, when that has none, CONF_USR_DIR.
const ematchMapName = "ematch_map"

// ematchMaxMatches bounds an ematch list's table; tc mallocs nmatches+1
// pointers whatever nmatches is.
const ematchMaxMatches = 1024

// ematchMaxDepth bounds container recursion; tc recurses until its stack
// runs out on a container that refers to itself.
const ematchMaxDepth = 64

// emPrinter is a struct ematch_util's print_eopt: -1 is its failure.
type emPrinter func(r *runner, data []byte) int

// ematchUtils are the ematches tc links (tc/Makefile) by kind name.
var ematchUtils = map[string]emPrinter{
	"cmp":   printEmCmp,
	"nbyte": printEmNbyte,
	"u32":   printEmU32,
	"meta":  printEmMeta,
	"canid": printEmCanid,
}

// printEmatch is print_ematch. Its return is ignored by every filter
// printer; gotc returns only its refusals.
func (r *runner) printEmatch(nest []byte) error {
	tb := r.nested(nest, xtcpnl.TcaEmatchTreeMaxKnown)
	switch {
	case !tb.Has(xtcpnl.TcaEmatchTreeHdr):
		r.errf("Missing ematch tree header\n")
		return nil
	case !tb.Has(xtcpnl.TcaEmatchTreeList):
		r.errf("Missing ematch tree list\n")
		return nil
	}
	hdr := tb.Bytes(xtcpnl.TcaEmatchTreeHdr)
	if len(hdr) < xtcpnl.TcfEmatchTreeHdrSizeCst {
		r.errf("Ematch tree header size mismatch\n")
		return nil
	}
	n := binary.LittleEndian.Uint16(hdr[0:2])
	if n == 0 {
		return nil
	}
	if n > ematchMaxMatches {
		return fmt.Errorf("%w: an ematch list of %d matches", errNotImplemented, n)
	}
	list := r.nested(tb.Bytes(xtcpnl.TcaEmatchTreeList), n)
	r.o.raw("\n  ")
	_, err := r.ematchSeq(list, n, 1, 1)
	return err
}

// ematchSeq is print_ematch_seq. tc reads tb[nmatches + 1] when the last
// match still carries AND or OR (F2): one past its malloc'd table, whose
// garbage is unreplayable; here that slot is NULL, so -1.
func (r *runner) ematchSeq(tb xtcpnl.AttrTable, n uint16, start uint64, prefix int) (int, error) {
	if prefix > ematchMaxDepth {
		return -1, fmt.Errorf("%w: ematch containers nested past %d", errNotImplemented, ematchMaxDepth)
	}
	raw := r.o.raw
	for i := start; ; i++ {
		if i > uint64(n) {
			return -1, nil
		}
		idx, _ := cint.U16From64(i)
		if !tb.Has(idx) {
			return -1, nil
		}
		hdr, data, err := xtcpnl.DecodeTcfEmatch(tb.Bytes(idx))
		if err != nil {
			return -1, nil // dlen < 0
		}
		if hdr.Flags&xtcpnl.TcfEmInvert != 0 {
			raw("NOT ")
		}
		if hdr.Kind == xtcpnl.TcfEmKindContainer {
			if len(data) < 4 {
				return -1, nil
			}
			ref := binary.LittleEndian.Uint32(data)
			raw("(\n")
			raw(strings.Repeat("  ", prefix+1))
			if rc, err := r.ematchSeq(tb, n, uint64(ref)+1, prefix+1); rc < 0 || err != nil {
				return -1, err
			}
			raw(strings.Repeat("  ", prefix))
			raw(") ")
		} else {
			name, p, err := r.ematchKind(hdr.Kind)
			if err != nil {
				return -1, err
			}
			if p == nil {
				raw("[unknown ematch " + strconv.Itoa(int(hdr.Kind)) + "]\n")
			} else {
				raw(name + "(")
				if p(r, data) < 0 {
					return -1, nil
				}
				raw(")\n")
			}
			if hdr.Flags&xtcpnl.TcfEmRelMask != 0 {
				raw(strings.Repeat("  ", prefix))
			}
		}
		switch hdr.Flags & xtcpnl.TcfEmRelMask {
		case xtcpnl.TcfEmRelAnd:
			raw("AND ")
		case xtcpnl.TcfEmRelOr:
			raw("OR ")
		default:
			return 0, nil
		}
	}
}

// ematchKind is get_ematch_kind_num: the kind's name from ematch_map, then
// its ematch_util. A name tc links but gotc does not print (ipset, ipt)
// is refused; a name it does not link is dlsym's NULL.
func (r *runner) ematchKind(kind uint16) (string, emPrinter, error) {
	name, err := r.lookupEmatchMap(kind, confEtcDirCst+"/"+ematchMapName)
	if errors.Is(err, errMapNoent) {
		name, err = r.lookupEmatchMap(kind, r.sys.usrDir+"/"+ematchMapName)
	}
	if err != nil {
		return "", nil, nil
	}
	if name == "ipset" || name == "ipt" {
		return "", nil, fmt.Errorf("%w: ematch %q", errNotImplemented, name)
	}
	return name, ematchUtils[name], nil
}

// The lookup_map failures: the file is not there (fopen's ENOENT, or no
// line for the kind, which is -ENOENT too), or a line is corrupt.
var (
	errMapNoent   = errors.New("ematch map: no entry")
	errMapCorrupt = errors.New("ematch map: corrupted")
)

// lookupEmatchMap is lookup_map: fgets lines, blanks and comments skipped,
// each "%d %s"; the first whose id is kind names it.
func (r *runner) lookupEmatchMap(kind uint16, path string) (string, error) {
	content, err := r.sys.readFile(path)
	if err != nil {
		return "", errMapNoent
	}
	for _, line := range fgets(content) {
		p := strings.TrimLeft(cstr(line), " \t")
		if p == "" || p[0] == '#' || p[0] == '\n' {
			continue
		}
		id, rest, ok := scanDec(p)
		var name string
		if ok {
			name, _, ok = scanStr(rest)
		}
		if !ok {
			r.errf("ematch map %s corrupted at %s\n", path, p)
			return "", errMapCorrupt
		}
		if id == int32(kind) {
			return name, nil
		}
	}
	return "", errMapNoent
}

// printEmCmp is cmp_print_eopt: the value through %d (F10).
func printEmCmp(r *runner, data []byte) int {
	c, err := xtcpnl.DecodeTcfEmCmp(data)
	if err != nil {
		r.errf("CMP header size mismatch\n")
		return -1
	}
	var b strings.Builder
	switch c.Align {
	case xtcpnl.TcfEmAlignU8:
		b.WriteString("u8 ")
	case xtcpnl.TcfEmAlignU16:
		b.WriteString("u16 ")
	case xtcpnl.TcfEmAlignU32:
		b.WriteString("u32 ")
	}
	fmt.Fprintf(&b, "at %d layer %d ", c.Off, c.Layer)
	if c.Mask != 0 {
		fmt.Fprintf(&b, "mask 0x%x ", c.Mask)
	}
	if c.Flags&xtcpnl.TcfEmCmpTrans != 0 {
		b.WriteString("trans ")
	}
	b.WriteString(emOpnd(c.Opnd, "", " "))
	b.WriteString(i32s(cint.Int(c.Val)))
	r.o.raw(b.String())
	return 0
}

// emOpnd is the eq/lt/gt word, nothing for another operand.
func emOpnd(op uint8, pre, post string) string {
	switch op {
	case xtcpnl.TcfEmOpndEq:
		return pre + "eq" + post
	case xtcpnl.TcfEmOpndLt:
		return pre + "lt" + post
	case xtcpnl.TcfEmOpndGt:
		return pre + "gt" + post
	}
	return ""
}

// printEmU32 is u32_print_eopt.
func printEmU32(r *runner, data []byte) int {
	k, err := xtcpnl.DecodeTcU32Key(data)
	if err != nil {
		r.errf("U32 header size mismatch\n")
		return -1
	}
	nh := ""
	if k.Offmask != 0 {
		nh = "nexthdr+"
	}
	r.o.raw(fmt.Sprintf("%08x/%08x at %s%d", k.Val, k.Mask, nh, k.Off))
	return 0
}

// cIsPrint is isprint in the C locale.
func cIsPrint(c byte) bool { return c >= 0x20 && c < 0x7f }

// printableOrDot is the "%c" of isprint(c) ? c : '.' for each byte.
func printableOrDot(b []byte) string {
	out := make([]byte, len(b))
	for i, c := range b {
		out[i] = '.'
		if cIsPrint(c) {
			out[i] = c
		}
	}
	return string(out)
}

// hexBytes is "%02x " of each byte.
func hexBytes(b []byte) string {
	var s strings.Builder
	for _, c := range b {
		fmt.Fprintf(&s, "%02x ", c)
	}
	return s.String()
}

// printEmNbyte is nbyte_print_eopt.
func printEmNbyte(r *runner, data []byte) int {
	if len(data) < xtcpnl.TcfEmNbyteSize {
		r.errf("NByte header size mismatch\n")
		return -1
	}
	nb, err := xtcpnl.DecodeTcfEmNbyte(data)
	if err != nil {
		r.errf("NByte payload size mismatch\n")
		return -1
	}
	r.o.raw(hexBytes(nb.Needle) + "\"" + printableOrDot(nb.Needle) +
		fmt.Sprintf("\" at %d layer %d", nb.Off, nb.Layer))
	return 0
}

// printEmCanid is canid_print_eopt.
func printEmCanid(r *runner, data []byte) int {
	rules := xtcpnl.DecodeCanFilters(data)
	var b strings.Builder
	for i, f := range rules {
		word, mask := "sff", uint32(xtcpnl.CanSffMask)
		if f.ID&xtcpnl.CanEffFlag != 0 {
			word, mask = "eff", xtcpnl.CanEffMask
		}
		if f.Mask == xtcpnl.CanEffFlag|mask {
			fmt.Fprintf(&b, "%s 0x%X", word, f.ID&mask)
		} else {
			fmt.Fprintf(&b, "%s 0x%X:0x%X", word, f.ID&mask, f.Mask&mask)
		}
		if i+1 < len(rules) {
			b.WriteString(" ")
		}
	}
	r.o.raw(b.String())
	return 0
}

// metaNames is em_meta.c meta_table's ids and names in table order:
// lookup_meta_entry_byid takes the first entry with the id, so nf_mark
// shadows fwmark.
var metaNames = []struct {
	id   uint16
	name string
}{
	{0, "Generic"}, {1, "random"}, {2, "loadavg_1"}, {3, "loadavg_5"}, {4, "loadavg_15"},
	{0, "Interfaces"}, {5, "dev"}, {0, "Packet attributes"}, {6, "priority"}, {7, "protocol"},
	{8, "pkt_type"}, {9, "pkt_len"}, {10, "data_len"}, {11, "mac_len"}, {0, "Netfilter"},
	{12, "nf_mark"}, {12, "fwmark"}, {0, "Traffic Control"}, {13, "tc_index"}, {0, "Routing"},
	{14, "rt_classid"}, {15, "rt_iif"}, {48, "vlan"}, {0, "Sockets"}, {16, "sk_family"},
	{17, "sk_state"}, {18, "sk_reuse"}, {19, "sk_bind_if"}, {20, "sk_refcnt"}, {21, "sk_shutdown"},
	{22, "sk_proto"}, {23, "sk_type"}, {24, "sk_rcvbuf"}, {25, "sk_rmem"}, {26, "sk_wmem"},
	{27, "sk_omem"}, {28, "sk_wmem_queue"}, {30, "sk_snd_queue"}, {29, "sk_rcv_queue"},
	{31, "sk_err_queue"}, {32, "sk_fwd_alloc"}, {33, "sk_sndbuf"},
}

// printEmMeta is meta_print_eopt: parse_rtattr with no flag masked.
func printEmMeta(r *runner, data []byte) int {
	tb := r.attrTable(data, xtcpnl.TcaEmMetaMaxKnown, 0)
	m, err := xtcpnl.ParseTcEmMeta(data)
	switch {
	case err != nil || !tb.Has(xtcpnl.TcaEmMetaHdr):
		r.errf("Missing meta header\n")
		return -1
	case m.HdrShort:
		r.errf("Meta header size mismatch\n")
		return -1
	}
	if r.metaObject(m.Left, tb.Bytes(xtcpnl.TcaEmMetaLvalue), tb.Has(xtcpnl.TcaEmMetaLvalue)) < 0 {
		return -1
	}
	r.o.raw(emOpnd(m.Left.Op, " ", " "))
	return r.metaObject(m.Right, tb.Bytes(xtcpnl.TcaEmMetaRvalue), tb.Has(xtcpnl.TcaEmMetaRvalue))
}

// metaObject is em_meta.c print_object, with print_value for a value: an
// int through %d (F10).
func (r *runner) metaObject(v xtcpnl.TcfMetaVal, val []byte, has bool) int {
	raw := r.o.raw
	if v.ID() == xtcpnl.TcfMetaIDValue {
		if !has {
			r.errf("Missing value TLV\n")
			return -1
		}
		switch v.Type() {
		case xtcpnl.TcfMetaTypeInt:
			if len(val) < 4 {
				r.errf("meta int type value TLV size mismatch.\n")
				return -1
			}
			raw(i32s(cint.Int(binary.LittleEndian.Uint32(val))))
		case xtcpnl.TcfMetaTypeVar:
			raw(metaBinary(val))
		}
		return 0
	}
	name := "[unknown meta id " + strconv.Itoa(int(v.ID())) + "]"
	for _, e := range metaNames {
		if e.id == v.ID() {
			name = e.name
			break
		}
	}
	raw(name)
	if v.Shift != 0 {
		raw(" shift " + strconv.Itoa(int(v.Shift)))
	}
	if v.Type() == xtcpnl.TcfMetaTypeInt && has {
		if len(val) < 4 {
			r.errf("meta int type mask TLV size mismatch\n")
			return -1
		}
		if m := binary.LittleEndian.Uint32(val); m != 0 {
			raw(fmt.Sprintf(" mask 0x%08x", m))
		}
	}
	return 0
}

// metaBinary is em_meta.c print_binary: the bytes as they are when all
// print, else hex and the quoted printable form.
func metaBinary(b []byte) string {
	for _, c := range b {
		if !cIsPrint(c) {
			return hexBytes(b) + "\"" + printableOrDot(b) + "\""
		}
	}
	return string(b)
}
