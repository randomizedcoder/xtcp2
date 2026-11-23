package gobridge

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// bridge/mdb.c: `bridge mdb show` and `bridge mdb get` (increment 7). The
// writes (add, replace, del, flush) are refused.

const (
	mdbaMaxCst        = 2  // MDBA_MAX (MDBA_ROUTER)
	mdbEattrMaxCst    = 10 // MDBA_MDB_EATTR_MAX (MDBA_MDB_EATTR_SRC_VNI)
	mdbSrcattrMaxCst  = 2  // MDBA_MDB_SRCATTR_MAX (MDBA_MDB_SRCATTR_TIMER)
	mdbPermanent      = 1  // MDB_PERMANENT
	mcastInclude      = 1  // MCAST_INCLUDE
	ethPIP            = 0x0800
	mdbFlagsOffload   = 1 << 0
	mdbFlagsFastLeave = 1 << 1
	mdbFlagsStarExcl  = 1 << 2
	mdbFlagsBlocked   = 1 << 3
	mdbFlagsOffFailed = 1 << 4
)

// errAbort is bridge dying of SIGABRT: jsonw_destroy's assert(depth == 0)
// after mdb_show's failed dump under -j (M4). run exits 134, stdout lost.
var errAbort = errors.New("gobridge: bridge aborts here (jsonw_destroy assert)")

// rtprotNames is rtnl_rtprot_n2a's table: the built-in names and those of
// the rt_protos file iproute2 ships (ovn, openr).
var rtprotNames = map[uint8]string{
	0: "unspec", 1: "redirect", 2: "kernel", 3: "boot", 4: "static",
	8: "gated", 9: "ra", 10: "mrt", 11: "zebra", 12: "bird", 13: "dnrouted",
	14: "xorp", 15: "ntk", 16: "dhcp", 18: "keepalived", 42: "babel",
	84: "ovn", 99: "openr", 186: "bgp", 187: "isis", 188: "ospf",
	189: "rip", 192: "eigrp",
}

// doMdb is do_mdb: ll_init_map, then the sub-command. get, replace and
// flush are strcmp'd, so `mdb g` is unknown where `fdb g` is get (M2).
func (r *runner) doMdb(args []string) error {
	if err := r.initMap(); err != nil {
		return err
	}
	if len(args) == 0 {
		return r.mdbShow(nil)
	}
	switch w := args[0]; {
	case matches(w, "add"), w == "replace", matches(w, "delete"):
		return r.refuse("mdb " + w)(nil)
	case showWords(w, false):
		return r.mdbShow(args[1:])
	case w == "get":
		return r.mdbGet(args[1:])
	case w == "flush":
		return r.refuse("mdb flush")(nil)
	case matches(w, helpWord):
		return r.mdbUsage()
	}
	r.errf("Command \"%s\" is unknown, try \"bridge mdb help\".\n", args[0])
	return exitCode(255)
}

// mdbUsage is mdb.c usage: exit(-1).
func (r *runner) mdbUsage() error {
	r.errf("%s", "Usage: bridge mdb { add | del | replace } dev DEV port PORT grp GROUP [src SOURCE] [permanent | temp] [vid VID]\n"+
		"              [ filter_mode { include | exclude } ] [ source_list SOURCE_LIST ] [ proto PROTO ] [ dst IPADDR ]\n"+
		"              [ dst_port DST_PORT ] [ vni VNI ] [ src_vni SRC_VNI ] [ via DEV ]\n"+
		"       bridge mdb {show} [ dev DEV ] [ vid VID ]\n"+
		"       bridge mdb get dev DEV grp GROUP [ src SOURCE ] [ vid VID ] [ src_vni SRC_VNI ]\n"+
		"       bridge mdb flush dev DEV [ port PORT ] [ vid VID ] [ src_vni SRC_VNI ] [ proto PROTO ]\n"+
		"              [ [no]permanent ] [ dst IPADDR ] [ dst_port DST_PORT ] [ vni VNI ]\n")
	return exitCode(255)
}

// mdbShow is mdb_show: two dumps, the entries into "mdb" and the router
// ports into "router". Unknown words, help included, are ignored (M1);
// the statics outlive the line (M3). A failed dump returns -1 (rc 255)
// in text, and under -j dies in delete_json_obj's assert (M4).
func (r *runner) mdbShow(args []string) error {
	dev, err := r.showArgs(args, &r.mdbVlan)
	if err != nil {
		return err
	}
	if err := r.resolveFilter(dev, &r.mdbIndex); err != nil {
		return err
	}
	o := r.o
	o.newJSONObj()
	o.openObject("")
	o.openArray(printJSON, "mdb")
	if err := r.mdbDump(r.printMdbs); err != nil {
		return err
	}
	o.closeArray(printJSON, "")
	o.openObject("router")
	if err := r.mdbDump(r.printRtrs); err != nil {
		return err
	}
	o.closeObject()
	o.closeObject()
	o.deleteJSONObj()
	return nil
}

// mdbDump is one of mdb_show's dumps.
func (r *runner) mdbDump(filter func(Msg) (int, error)) error {
	req, berr := xtcpnl.BuildMdbDumpRequest(r.nextSeq())
	var perr error
	rc, err := r.dump(req, berr, func(m Msg) int {
		v, e := filter(m)
		if e != nil {
			perr = e
			return -1
		}
		return v
	})
	switch {
	case err != nil:
		return err
	case perr != nil:
		return perr
	case rc < 0:
		r.errf("Dump terminated\n")
		if r.o.isJSON() {
			return errAbort
		}
		return retCode(-1)
	}
	return nil
}

// parseMdbNlmsg is __parse_mdb_nlmsg: the type check, the length, the
// dev filter, then parse_rtattr (no flags masked). ok false is a skip,
// rc -1 an end.
func (r *runner) parseMdbNlmsg(m Msg) (xtcpnl.AttrTable, uint32, int, bool) {
	if m.Type != unix.RTM_GETMDB && m.Type != unix.RTM_NEWMDB && m.Type != unix.RTM_DELMDB {
		r.errf("Not RTM_GETMDB, RTM_NEWMDB or RTM_DELMDB: %08x %08x %08x\n", len(m.Body)+nlHeaderLen, m.Type, m.Flags)
		return xtcpnl.AttrTable{}, 0, 0, false
	}
	if len(m.Body) < xtcpnl.BrPortMsgSizeCst {
		r.errf("BUG: wrong nlmsg len %d\n", len(m.Body)-xtcpnl.BrPortMsgSizeCst)
		return xtcpnl.AttrTable{}, 0, -1, false
	}
	ifindex := binary.LittleEndian.Uint32(m.Body[4:8])
	if r.mdbIndex != 0 && r.mdbIndex != ifindex {
		return xtcpnl.AttrTable{}, 0, 0, false
	}
	tb, err := xtcpnl.ParseAttrTableFlags(m.Body[xtcpnl.BrPortMsgSizeCst:], mdbaMaxCst, 0)
	if err != nil {
		return xtcpnl.AttrTable{}, 0, 0, false
	}
	return tb, ifindex, 1, true
}

// printMdbs is print_mdbs: every MDBA_MDB_ENTRY_INFO of every entry, the
// nest types unchecked.
func (r *runner) printMdbs(m Msg) (int, error) {
	tb, ifindex, rc, ok := r.parseMdbNlmsg(m)
	if !ok {
		return rc, nil
	}
	if !tb.Has(xtcpnl.MdbaMdb) {
		return 0, nil
	}
	var perr error
	err := xtcpnl.WalkRawAttrs(tb.Bytes(xtcpnl.MdbaMdb), func(_ uint16, ent []byte) {
		if perr != nil {
			return
		}
		perr = xtcpnl.WalkRawAttrs(ent, func(_ uint16, info []byte) {
			if perr == nil {
				perr = r.printMdbEntry(ifindex, info)
			}
		})
	})
	if perr != nil {
		return -1, perr
	}
	return 0, err
}

// printMdbEntry is br_print_mdb_entry's parse and print_mdb_entry.
func (r *runner) printMdbEntry(ifindex uint32, info []byte) error {
	if len(info) < xtcpnl.MdbEntrySizeCst {
		return errOverread("br_mdb_entry", "M6")
	}
	e, err := xtcpnl.ParseMdbEntry(info[:xtcpnl.MdbEntrySizeCst])
	if err != nil {
		return err
	}
	tb, err := xtcpnl.ParseAttrTableFlags(info[xtcpnl.MdbEntrySizeCst:], mdbEattrMaxCst, unix.NLA_F_NESTED)
	if err != nil {
		return err
	}
	if r.mdbVlan != 0 && uint32(e.Vid) != cint.Bits(r.mdbVlan) {
		return nil
	}
	o := r.o
	af := unix.AF_INET6
	var grp string
	switch e.Proto {
	case 0:
		af = unix.AF_PACKET
		grp = macText(e.Group)
	case ethPIP:
		af = unix.AF_INET
		grp, _ = formatHost(e.Group, inetAddrLen)
	default:
		grp, _ = formatHost(info[8:24], inet6AddrLen)
	}
	c := familyColor(af)
	dev, err := r.indexToName(ifindex)
	if err != nil {
		return err
	}
	port, err := r.indexToName(e.Ifindex)
	if err != nil {
		return err
	}
	o.openObject("")
	o.int(printJSON, "index", "", cint.Int(ifindex))
	o.str(printAny, colorIfname, wordDev, "dev "+dev, dev)
	o.str(printAny, colorNone, "port", " port "+port, port)
	o.str(printAny, c, "grp", " grp "+grp, grp)
	if tb.Has(xtcpnl.MdbEattrSource) {
		src, ok := inetNtop(af, tb.Bytes(xtcpnl.MdbEattrSource))
		switch {
		case ok:
			o.str(printAny, c, "src", " src "+src, src)
		case o.jsonCtx(printAny):
			// inet_ntop(AF_PACKET) is NULL: print_color_string prints
			// the key alone (M5).
			o.jw.name("src")
		default:
			o.colorText(c, " src (null)")
		}
	}
	state := "temp"
	if e.State&mdbPermanent != 0 {
		state = wordPermanent
	}
	o.str(printAny, colorNone, "state", " "+state, state)
	if r.details {
		if err := r.mdbDetails(tb, af); err != nil {
			return err
		}
	}
	o.openArray(printJSON, "flags")
	for _, f := range []struct {
		bit  uint8
		name string
	}{
		{mdbFlagsOffload, "offload"}, {mdbFlagsFastLeave, "fast_leave"}, {mdbFlagsStarExcl, "added_by_star_ex"},
		{mdbFlagsBlocked, "blocked"}, {mdbFlagsOffFailed, "offload_failed"},
	} {
		if e.Flags&f.bit != 0 {
			o.str(printAny, colorNone, "", " "+f.name, f.name)
		}
	}
	o.closeArray(printJSON, "")
	if e.Vid != 0 {
		o.uint(printAny, "vid", " vid "+strconv.FormatUint(uint64(e.Vid), 10), uint32(e.Vid))
	}
	if err := r.mdbRemote(tb); err != nil {
		return err
	}
	o.nl()
	o.closeObject()
	return nil
}

// mdbRemote is print_mdb_entry from dst to the -s timer: a VXLAN
// entry's remote, then the timer.
func (r *runner) mdbRemote(tb xtcpnl.AttrTable) error {
	o := r.o
	if tb.Has(xtcpnl.MdbEattrDst) {
		a := tb.Bytes(xtcpnl.MdbEattrDst)
		daf := unix.AF_INET
		if len(a) == inet6AddrLen {
			daf = unix.AF_INET6
		}
		dst, ok := inetNtop(daf, a)
		if !ok {
			return errOverread("MDBA_MDB_EATTR_DST", "M6")
		}
		o.str(printAny, familyColor(daf), "dst", " dst "+dst, dst)
	}
	if tb.Has(xtcpnl.MdbEattrDstPort) {
		v, ok := tb.U16(xtcpnl.MdbEattrDstPort)
		if !ok {
			return errOverread("MDBA_MDB_EATTR_DST_PORT", "M6")
		}
		o.uint(printAny, "dst_port", " dst_port "+strconv.FormatUint(uint64(v), 10), uint32(v))
	}
	for _, a := range []struct {
		t   uint16
		key string
	}{{xtcpnl.MdbEattrVni, wordVni}, {xtcpnl.MdbEattrSrcVni, wordSrcVni}} {
		if !tb.Has(a.t) {
			continue
		}
		v, ok := tb.U32(a.t)
		if !ok {
			return errOverread(a.key, "M6")
		}
		o.uint(printAny, a.key, " "+a.key+" "+strconv.FormatUint(uint64(v), 10), v)
	}
	if tb.Has(xtcpnl.MdbEattrIfindex) {
		v, ok := tb.U32(xtcpnl.MdbEattrIfindex)
		if !ok {
			return errOverread("MDBA_MDB_EATTR_IFINDEX", "M6")
		}
		via, err := r.indexToName(v)
		if err != nil {
			return err
		}
		o.str(printAny, colorNone, "via", " via "+via, via)
	}
	if r.stats && tb.Has(xtcpnl.MdbEattrTimer) {
		v, ok := tb.U32(xtcpnl.MdbEattrTimer)
		if !ok {
			return errOverread("MDBA_MDB_EATTR_TIMER", "M6")
		}
		s := formatTimer(v)
		o.str(printAny, colorNone, "timer", " "+s, s)
	}
	return nil
}

// mdbDetails is print_mdb_entry's -d part: filter mode, source list
// (unaligned timers), protocol.
func (r *runner) mdbDetails(tb xtcpnl.AttrTable, af int) error {
	o := r.o
	if tb.Has(xtcpnl.MdbEattrGroupMode) {
		v, ok := tb.U8(xtcpnl.MdbEattrGroupMode)
		if !ok {
			return errOverread("MDBA_MDB_EATTR_GROUP_MODE", "M6")
		}
		mode := "exclude"
		if v == mcastInclude {
			mode = "include"
		}
		o.str(printAny, colorNone, "filter_mode", " filter_mode "+mode, mode)
	}
	if tb.Has(xtcpnl.MdbEattrSrcList) {
		name := " source_list"
		if o.isJSON() {
			name = "source_list"
		}
		o.openArray(printAny, name)
		sep := " "
		var perr error
		err := xtcpnl.WalkRawAttrs(tb.Bytes(xtcpnl.MdbEattrSrcList), func(_ uint16, ent []byte) {
			if perr == nil {
				perr = r.printSrcEntry(ent, af, sep)
				sep = ","
			}
		})
		if err = errors.Join(err, perr); err != nil {
			return err
		}
		o.closeArray(printJSON, "")
	}
	if tb.Has(xtcpnl.MdbEattrRtProt) {
		v, ok := tb.U8(xtcpnl.MdbEattrRtProt)
		if !ok {
			return errOverread("MDBA_MDB_EATTR_RTPROT", "M6")
		}
		p, known := rtprotNames[v]
		if !known {
			p = strconv.FormatUint(uint64(v), 10)
		}
		o.str(printAny, colorNone, "protocol", " proto "+p, p)
	}
	return nil
}

// printSrcEntry is print_src_entry: nothing without both the address and
// the timer, or when inet_ntop fails (an L2 group's sources).
func (r *runner) printSrcEntry(ent []byte, af int, sep string) error {
	t, err := xtcpnl.ParseAttrTableFlags(ent, mdbSrcattrMaxCst, unix.NLA_F_NESTED)
	if err != nil {
		return err
	}
	if !t.Has(xtcpnl.MdbSrcAttrAddress) || !t.Has(xtcpnl.MdbSrcAttrTimer) {
		return nil
	}
	addr, ok := inetNtop(af, t.Bytes(xtcpnl.MdbSrcAttrAddress))
	if !ok {
		if af == unix.AF_PACKET {
			return nil
		}
		return errOverread("MDBA_MDB_SRCATTR_ADDRESS", "M6")
	}
	v, ok := t.U32(xtcpnl.MdbSrcAttrTimer)
	if !ok {
		return errOverread("MDBA_MDB_SRCATTR_TIMER", "M6")
	}
	o := r.o
	o.openObject("")
	o.str(printFP, colorNone, "", sep, "")
	o.str(printAny, familyColor(af), "address", addr, addr)
	tm := fmt.Sprintf("%d.%02d", v/100, v%100)
	o.str(printAny, colorNone, "timer", "/"+tm, tm)
	o.closeObject()
	return nil
}

// printRtrs is print_rtrs and print_router_entries: a bridge's
// (RTM_GETMDB) router ports only under -d; a VXLAN's first port as a line.
func (r *runner) printRtrs(m Msg) (int, error) {
	tb, ifindex, rc, ok := r.parseMdbNlmsg(m)
	if !ok {
		return rc, nil
	}
	if !tb.Has(xtcpnl.MdbaRouter) {
		return 0, nil
	}
	br, err := r.indexToName(ifindex)
	if err != nil {
		return -1, err
	}
	router := tb.Bytes(xtcpnl.MdbaRouter)
	o := r.o
	if m.Type == unix.RTM_GETMDB {
		if r.details {
			if err := r.routerPorts(router, br); err != nil {
				return -1, err
			}
		}
		return 0, nil
	}
	if len(router) < 8 {
		return -1, errOverread("MDBA_ROUTER_PORT", "M6")
	}
	name, err := r.indexToName(binary.LittleEndian.Uint32(router[4:8]))
	if err != nil {
		return -1, err
	}
	if o.isJSON() {
		o.openArray(printJSON, br)
		o.openObject("")
		o.str(printJSON, colorNone, "port", "", name)
		o.closeObject()
		o.closeArray(printJSON, "")
	} else {
		o.printf("router port dev " + name + " master " + br + "\n")
	}
	return 0, nil
}

// routerPorts is br_print_router_ports: keyed by the bridge's name in
// JSON (M7); under -s one hard-newlined line a port (F11).
func (r *runner) routerPorts(router []byte, br string) error {
	o := r.o
	if o.isJSON() {
		o.openArray(printJSON, br)
	} else if !r.stats {
		o.printf("router ports on " + br + ": ")
	}
	var perr error
	err := xtcpnl.WalkRawAttrs(router, func(_ uint16, val []byte) {
		if perr != nil {
			return
		}
		if len(val) < 4 {
			perr = errOverread("MDBA_ROUTER_PORT", "B9")
			return
		}
		name, e := r.indexToName(binary.LittleEndian.Uint32(val[0:4]))
		if e != nil {
			perr = e
			return
		}
		switch {
		case o.isJSON():
			o.openObject("")
			o.str(printJSON, colorNone, "port", "", name)
			if r.stats {
				perr = r.routerPortStats(val)
			}
			o.closeObject()
		case r.stats:
			o.printf("router ports on " + br + ": " + name)
			perr = r.routerPortStats(val)
			o.printf("\n")
		default:
			o.printf(name + " ")
		}
	})
	if err = errors.Join(err, perr); err != nil {
		return err
	}
	if !r.stats {
		o.nl()
	}
	o.closeArray(printJSON, "")
	return nil
}

// familyColor is ifa_family_color.
func familyColor(af int) colorAttr {
	switch af {
	case unix.AF_INET:
		return colorInet
	case unix.AF_INET6:
		return colorInet6
	}
	return colorNone
}

// inetNtop is inet_ntop for AF_INET and AF_INET6 (false for a short
// buffer, which bridge would read past, or for any other family, where
// inet_ntop returns NULL).
func inetNtop(af int, b []byte) (string, bool) {
	switch af {
	case unix.AF_INET:
		return formatHost(b, inetAddrLen)
	case unix.AF_INET6:
		return formatHost(b, inet6AddrLen)
	}
	return "", false
}

// macText is ll_addr_n2a's colon hex.
func macText(b []byte) string {
	var s strings.Builder
	for i, c := range b {
		if i > 0 {
			s.WriteByte(':')
		}
		fmt.Fprintf(&s, "%02x", c)
	}
	return s.String()
}

// mdbGetArgs is mdb_get's arguments as its loop leaves them.
type mdbGetArgs struct {
	d, grp, src, srcVni string
	vid                 uint16
	getAttrs            bool
}

// mdbGetParse is mdb_get's argument loop: strcmp'd words, an exact help,
// anything else ignored; vid is atoi into a short (M9).
func (r *runner) mdbGetParse(args []string) (mdbGetArgs, error) {
	var a mdbGetArgs
	for i := 0; i < len(args); i++ {
		w := args[i]
		val := func(dst *string) error {
			i++
			if i >= len(args) {
				return r.incomplete()
			}
			*dst = args[i]
			return nil
		}
		var err error
		switch w {
		case wordDev:
			err = val(&a.d)
		case "grp":
			err = val(&a.grp)
		case "vid":
			var s string
			if err = val(&s); err == nil {
				a.vid, _ = cint.U16From64(uint64(cint.Bits(atoi(s)) & 0xffff))
			}
		case "src":
			err, a.getAttrs = val(&a.src), true
		case wordSrcVni:
			err, a.getAttrs = val(&a.srcVni), true
		case helpWord:
			return a, r.mdbUsage()
		}
		if err != nil {
			return a, err
		}
	}
	return a, nil
}

// mdbGet is mdb_get. Its words are strcmp'd; only an exact help prints
// the usage, other words are ignored. vid is atoi into a short (M9).
func (r *runner) mdbGet(args []string) error {
	a, err := r.mdbGetParse(args)
	if err != nil {
		return err
	}
	d, grp, src, srcVni, vid, getAttrs := a.d, a.grp, a.src, a.srcVni, a.vid, a.getAttrs
	if d == "" || grp == "" {
		r.errf("Device and group address are required arguments.\n")
		return retCode(-1)
	}
	idx, err := r.nameToIndex(d)
	if err != nil {
		return err
	}
	if idx == 0 {
		return r.nodev(d)
	}
	g := xtcpnl.MdbGet{Ifindex: idx, Vid: vid}
	group, ok := r.mdbParseGrp(grp)
	if !ok {
		r.errf("Invalid address \"%s\"\n", grp)
		return retCode(-1)
	}
	g.Group = group
	if getAttrs {
		if src != "" {
			a, err := netip.ParseAddr(src)
			if err != nil || a.Zone() != "" {
				r.errf("Invalid source address \"%s\"\n", src)
				return retCode(-1)
			}
			g.Source = a.AsSlice()
		}
		if srcVni != "" {
			v, rest, ok := strtoul(srcVni, 0)
			if !ok || rest != "" || v == ^uint64(0) {
				r.errf("Invalid source VNI \"%s\"\n", srcVni)
				return retCode(-1)
			}
			// addattr32 keeps the low 32 bits; there is no 24-bit check (M9).
			g.SrcVni, g.HasSrcVni = cint.Low32(v), true
		}
		if g.Source == nil && !g.HasSrcVni {
			// `src` or `src_vni` with an empty value: the nest is sent
			// empty, which the builder does not make.
			return fmt.Errorf("%w: mdb get with an empty src or src_vni", errNotImplemented)
		}
	}
	req, berr := xtcpnl.BuildMdbGetRequest(g, r.nextSeq())
	answer, ok, err := r.talkAnswer(req, berr)
	if err != nil {
		return err
	}
	if !ok {
		return retCode(-2)
	}
	o := r.o
	o.newJSONObj()
	rc, err := r.printMdbs(answer)
	if err != nil {
		return err
	}
	o.deleteJSONObj()
	if rc < 0 {
		return retCode(-1)
	}
	return nil
}

// mdbParseGrp is mdb_parse_grp: inet_pton v4, then v6, then
// ll_addr_a2n, which must return ETH_ALEN.
func (r *runner) mdbParseGrp(s string) ([]byte, bool) {
	if a, err := netip.ParseAddr(s); err == nil && a.Zone() == "" {
		return a.AsSlice(), true
	}
	n, mac := r.llAddrA2N(s, 6)
	return mac, n == 6
}

// llAddrA2N is ll_addr_a2n into a buffer of size n. With a '.', it is
// get_addr_1(AF_INET): 4 on success, so never a MAC. Otherwise each
// ':'-cut part is sscanf "%x" into an int, and the count is returned,
// size+1 when parts remain; a part that is not hex, or out of 0-255, is
// named in the message (the part, not the whole argument).
func (r *runner) llAddrA2N(s string, size int) (int, []byte) {
	if strings.Contains(s, ".") {
		if strings.Contains(s, ":") || !getAddrIPv4(s) {
			r.errf("\"%s\" is invalid lladdr.\n", s)
			return -1, nil
		}
		return 4, nil
	}
	mac := make([]byte, 0, size)
	rest := s
	for i := range size {
		part, after, more := strings.Cut(rest, ":")
		v, _, ok := strtoul(part, 16)
		temp := cint.Int(cint.Low32(v)) // %x stores an unsigned int into an int
		if !ok || temp < 0 || temp > 255 {
			r.errf("\"%s\" is invalid lladdr.\n", part)
			return -1, nil
		}
		b, _ := cint.U8From64(v & 0xff)
		mac = append(mac, b)
		if !more {
			return i + 1, mac
		}
		rest = after
	}
	return size + 1, nil
}

// getAddrIPv4 is get_addr_ipv4: up to four strtoul (base 0) parts of at
// most 255, '.'-separated, nothing after.
func getAddrIPv4(s string) bool {
	for i := range 4 {
		v, rest, ok := strtoul(s, 0)
		if !ok || v > 255 {
			return false
		}
		if rest == "" {
			return true
		}
		if i == 3 || rest[0] != '.' {
			return false
		}
		s = rest[1:]
	}
	return true
}
