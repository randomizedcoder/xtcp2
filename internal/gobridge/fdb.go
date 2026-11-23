package gobridge

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// bridge/fdb.c: `bridge fdb show` and `bridge fdb get` (increment 7). The
// writes (add, append, replace, del, flush) are refused.

const (
	ndaMaxCst  = 17 // NDA_MAX in 7.2.0's uapi copy (NDA_NDM_FLAGS_MASK)
	nfeaMaxCst = 2  // NFEA_MAX (NFEA_DONT_REFRESH)
	userHZ     = 100

	// The ARPHRD_* types ll_addr_n2a prints as an address.
	arphrdTunnel  = 768
	arphrdTunnel6 = 769
	arphrdIPGRE   = 778
	arphrdSIT     = 776
	arphrdIP6GRE  = 823

	fdbNotifyBit         = 1 // FDB_NOTIFY_BIT
	fdbNotifyInactiveBit = 2 // FDB_NOTIFY_INACTIVE_BIT
)

// doFdb is do_fdb: ll_init_map first, then the sub-command by prefix,
// except flush (strcmp). timestamp is reset, so -t prints nothing.
func (r *runner) doFdb(args []string) error {
	if err := r.initMap(); err != nil {
		return err
	}
	if len(args) == 0 {
		return r.fdbShow(nil)
	}
	switch w := args[0]; {
	case matches(w, "add"), matches(w, "append"), matches(w, "replace"), matches(w, "delete"):
		return r.refuse("fdb " + w)(nil)
	case matches(w, "get"):
		return r.fdbGet(args[1:])
	case showWords(w, false):
		return r.fdbShow(args[1:])
	case w == "flush":
		return r.refuse("fdb flush")(nil)
	case matches(w, helpWord):
		return r.fdbUsage()
	}
	r.errf("Command \"%s\" is unknown, try \"bridge fdb help\".\n", args[0])
	return exitCode(255)
}

// fdbUsage is fdb.c usage: exit(-1).
func (r *runner) fdbUsage() error {
	r.errf("%s", "Usage: bridge fdb { add | append | del | replace } ADDR dev DEV\n"+
		"              [ self ] [ master ] [ use ] [ router ] [ extern_learn ]\n"+
		"              [ sticky ] [ local | static | dynamic ] [ vlan VID ]\n"+
		"              { [ dst IPADDR ] [ port PORT] [ vni VNI ] | [ nhid NHID ] }\n"+
		"	       [ via DEV ] [ src_vni VNI ] [ activity_notify ]\n"+
		"	       [ inactive ] [ norefresh ]\n"+
		"       bridge fdb [ show [ br BRDEV ] [ brport DEV ] [ vlan VID ]\n"+
		"              [ state STATE ] [ dynamic ] ]\n"+
		"       bridge fdb get [ to ] LLADDR [ br BRDEV ] { brport | dev } DEV\n"+
		"              [ vlan VID ] [ vni VNI ] [ self ] [ master ] [ dynamic ]\n"+
		"       bridge fdb flush dev DEV [ brport DEV ] [ vlan VID ] [ src_vni VNI ]\n"+
		"              [ nhid NHID ] [ vni VNI ] [ port PORT ] [ dst IPADDR ] [ self ]\n"+
		"	       [ master ] [ [no]permanent | [no]static | [no]dynamic ]\n"+
		"              [ [no]added_by_user ] [ [no]extern_learn ] [ [no]sticky ]\n"+
		"              [ [no]offloaded ] [ [no]router ]\n")
	return exitCode(255)
}

// invarg is lib/utils.c invarg: exit(-1).
func (r *runner) invarg(msg, arg string) error {
	r.errf("Error: argument \"%s\" is wrong: %s\n", arg, msg)
	return exitCode(255)
}

// duparg2 is lib/utils.c duparg2: exit(-1).
func (r *runner) duparg2(key, arg string) error {
	r.errf("Error: either \"%s\" is duplicate, or \"%s\" is garbage.\n", key, arg)
	return exitCode(255)
}

// fdbShow is fdb_show. Its words are strcmp'd; an unknown word that is a
// prefix of help prints the usage, any other is ignored. dev and br may
// repeat, the last winning (F1); vlan's duplicate test reads the static
// (F2). The statics outlive the line (C16).
func (r *runner) fdbShow(args []string) error {
	var dev, br string
	for i := 0; i < len(args); i++ {
		w := args[i]
		next := func() bool { i++; return i < len(args) }
		switch {
		case w == "brport" || w == wordDev:
			if !next() {
				return r.incomplete()
			}
			dev = args[i]
		case w == "br":
			if !next() {
				return r.incomplete()
			}
			br = args[i]
		case w == "vlan":
			if !next() {
				return r.incomplete()
			}
			if r.fdbVlan != 0 {
				return r.duparg("vlan", args[i])
			}
			r.fdbVlan = cint.Bits(atoi(args[i]))
		case w == "state":
			if !next() {
				return r.incomplete()
			}
			s, ok := stateA2N(args[i])
			if !ok {
				return r.invarg("invalid state", args[i])
			}
			r.fdbState |= s
		case w == "dynamic":
			r.fdbDynamic = true
		case matches(w, helpWord):
			return r.fdbUsage()
		}
	}
	if br != "" {
		i, err := r.nameToIndex(br)
		if err != nil {
			return err
		}
		if i == 0 {
			r.errf("Cannot find bridge device \"%s\"\n", br)
			return retCode(-1)
		}
		r.fdbMaster = i
	}
	// fdb.c keeps filter_dev "for older kernels": the index goes in the
	// request and is tested again in print_fdb.
	if dev != "" {
		i, err := r.nameToIndex(dev)
		if err != nil {
			return err
		}
		r.fdbIndex = i
		if i == 0 {
			return r.nodev(dev)
		}
	}
	req, berr := xtcpnl.BuildFdbDumpRequest(r.fdbIndex, r.fdbMaster, r.nextSeq())
	o := r.o
	o.newJSONObj()
	var perr error
	rc, err := r.dump(req, berr, func(m Msg) int {
		v, e := r.printFdb(m)
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
		// exit(1) with the JSON array left open (F12).
		r.errf("Dump terminated\n")
		return exitCode(1)
	}
	o.deleteJSONObj()
	return nil
}

// stateA2N is state_a2n: names by prefix (static before stale, so "s"
// is static, F5), "all" exactly, or an unsigned number (get_unsigned,
// base 0).
func stateA2N(arg string) (uint32, bool) {
	switch {
	case matches(arg, wordPermanent):
		return unix.NUD_PERMANENT, true
	case matches(arg, "static"), matches(arg, "temp"):
		return unix.NUD_NOARP, true
	case matches(arg, "stale"):
		return unix.NUD_STALE, true
	case matches(arg, "reachable"), matches(arg, "dynamic"):
		return unix.NUD_REACHABLE, true
	case arg == "all":
		return ^uint32(0), true
	}
	v, rest, ok := strtoul(arg, 0)
	if !ok || rest != "" || len(rest) == len(arg) || v > 0xffffffff {
		return 0, false
	}
	u, _ := cint.U32From64(v)
	return u, true
}

// strtoul is C strtoul on a 64-bit long: leading space, a sign, base 0's
// 0x and 0 prefixes, then digits. It returns the value (negated modulo
// 2^64 for '-', ULONG_MAX on overflow), the unparsed rest, and whether
// any digit was read; with none, rest is the whole input, as endptr is.
func strtoul(s string, base uint64) (uint64, string, bool) {
	orig := s
	s = strings.TrimLeft(s, " \t\n\v\f\r")
	neg := false
	if s != "" && (s[0] == '+' || s[0] == '-') {
		neg = s[0] == '-'
		s = s[1:]
	}
	if _, hex := hexDigit(s, 2); (base == 0 || base == 16) && len(s) > 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') && hex {
		s, base = s[2:], 16
	} else if base == 0 && s != "" && s[0] == '0' {
		base = 8
	} else if base == 0 {
		base = 10
	}
	var v uint64
	n, over := 0, false
	for n < len(s) {
		d, ok := hexDigit(s, n)
		if !ok || d >= base {
			break
		}
		if !over {
			if v > (^uint64(0)-d)/base {
				over = true
			} else {
				v = v*base + d
			}
		}
		n++
	}
	if n == 0 {
		return 0, orig, false
	}
	switch {
	case over:
		v = ^uint64(0)
	case neg:
		v = -v
	}
	return v, s[n:], true
}

// hexDigit is s[i]'s value as a digit up to base 16, false for none or
// past the end.
func hexDigit(s string, i int) (uint64, bool) {
	if i >= len(s) {
		return 0, false
	}
	switch c := s[i]; {
	case c >= '0' && c <= '9':
		return uint64(c - '0'), true
	case c >= 'a' && c <= 'f':
		return uint64(c-'a') + 10, true
	case c >= 'A' && c <= 'F':
		return uint64(c-'A') + 10, true
	}
	return 0, false
}

// stateN2A is state_n2a: the first of permanent, static, stale; "" for
// REACHABLE (F7); otherwise "%#x", with "state=" in text.
func (r *runner) stateN2A(s uint16) string {
	switch {
	case s&unix.NUD_PERMANENT != 0:
		return wordPermanent
	case s&unix.NUD_NOARP != 0:
		return "static"
	case s&unix.NUD_STALE != 0:
		return "stale"
	case s&unix.NUD_REACHABLE != 0:
		return ""
	}
	h := cHashX(uint64(s))
	if r.o.isJSON() {
		return h
	}
	return "state=" + h
}

// printFdb is print_fdb, for a dump's message or a get's answer.
func (r *runner) printFdb(m Msg) (int, error) {
	if m.Type != unix.RTM_NEWNEIGH && m.Type != unix.RTM_DELNEIGH {
		r.errf("Not RTM_NEWNEIGH: %08x %08x %08x\n", len(m.Body)+nlHeaderLen, m.Type, m.Flags)
		return 0, nil
	}
	if len(m.Body) < xtcpnl.NdMsgSizeCst {
		r.errf("BUG: wrong nlmsg len %d\n", len(m.Body)-xtcpnl.NdMsgSizeCst)
		return -1, nil
	}
	family := m.Body[0]
	ifindex := binary.LittleEndian.Uint32(m.Body[4:8])
	state := binary.LittleEndian.Uint16(m.Body[8:10])
	flags := m.Body[10]
	if family != unix.AF_BRIDGE {
		return 0, nil
	}
	if r.fdbIndex != 0 && r.fdbIndex != ifindex {
		return 0, nil
	}
	if r.fdbState != 0 && uint32(state)&r.fdbState == 0 {
		return 0, nil
	}
	tb, err := xtcpnl.ParseAttrTableFlags(m.Body[xtcpnl.NdMsgSizeCst:], ndaMaxCst, unix.NLA_F_NESTED)
	if err != nil {
		return -1, err
	}
	extFlags, vid, err := fdbExtVlan(tb)
	if err != nil {
		return -1, err
	}
	if r.fdbVlan != 0 && r.fdbVlan != uint32(vid) {
		return 0, nil
	}
	if r.fdbDynamic && state&unix.NUD_PERMANENT != 0 {
		return 0, nil
	}
	o := r.o
	o.openObject("")
	if m.Type == unix.RTM_DELNEIGH {
		o.boolean(printAny, "deleted", "Deleted ", true)
	}
	if err := r.fdbHead(tb, ifindex); err != nil {
		return -1, err
	}
	if err := r.fdbRemote(tb, vid); err != nil {
		return -1, err
	}
	if r.details && tb.Has(xtcpnl.NdaFdbExtAttrs) {
		if err := r.fdbExtAttrs(tb.Bytes(xtcpnl.NdaFdbExtAttrs)); err != nil {
			return -1, err
		}
	}
	if err := r.fdbCacheInfo(tb); err != nil {
		return -1, err
	}
	r.fdbFlags(flags, extFlags)
	if err := r.fdbMasterName(tb); err != nil {
		return -1, err
	}
	// A hard newline: -o is not honored (F11).
	st := r.stateN2A(state)
	o.str(printAny, colorNone, "state", st+"\n", st)
	o.closeObject()
	return 0, nil
}

// fdbExtVlan reads NDA_FLAGS_EXT and NDA_VLAN, 0 when absent; print_fdb
// reads both at their widths with no length check (F10).
func fdbExtVlan(tb xtcpnl.AttrTable) (uint32, uint16, error) {
	var ext uint32
	if tb.Has(xtcpnl.NdaFlagsExt) {
		v, ok := tb.U32(xtcpnl.NdaFlagsExt)
		if !ok {
			return 0, 0, errOverread("NDA_FLAGS_EXT", "F10")
		}
		ext = v
	}
	var vid uint16
	if tb.Has(unix.NDA_VLAN) {
		v, ok := tb.U16(unix.NDA_VLAN)
		if !ok {
			return 0, 0, errOverread("NDA_VLAN", "F10")
		}
		vid = v
	}
	return ext, vid, nil
}

// fdbMasterName is print_fdb's "master NAME".
func (r *runner) fdbMasterName(tb xtcpnl.AttrTable) error {
	if !tb.Has(unix.NDA_MASTER) {
		return nil
	}
	v, ok := tb.U32(unix.NDA_MASTER)
	if !ok {
		return errOverread("NDA_MASTER", "F10")
	}
	name, err := r.indexToName(v)
	if err != nil {
		return err
	}
	r.o.str(printAny, colorNone, "master", "master "+name+" ", name)
	return nil
}

// fdbHead is print_fdb's mac, then the dev unless a dev filter is set.
func (r *runner) fdbHead(tb xtcpnl.AttrTable, ifindex uint32) error {
	o := r.o
	if tb.Has(unix.NDA_LLADDR) {
		s, ok := r.llAddrN2A(tb.Bytes(unix.NDA_LLADDR), ifindex)
		if !ok {
			return errOverread("NDA_LLADDR", "F10")
		}
		o.str(printAny, colorMAC, "mac", s+" ", s)
	}
	if r.fdbIndex == 0 && ifindex != 0 {
		name, err := r.indexToName(ifindex)
		if err != nil {
			return err
		}
		o.str(printFP, colorNone, "", "dev ", "")
		o.str(printAny, colorIfname, "ifname", name+" ", name)
	}
	return nil
}

// fdbRemote is print_fdb from dst to link-netnsid: a VXLAN entry's
// remote, the vlan between dst and port.
func (r *runner) fdbRemote(tb xtcpnl.AttrTable, vid uint16) error {
	o := r.o
	if tb.Has(unix.NDA_DST) {
		a := tb.Bytes(unix.NDA_DST)
		alen, c := inetAddrLen, colorInet
		if len(a) == inet6AddrLen {
			alen, c = inet6AddrLen, colorInet6
		}
		dst, ok := formatHost(a, alen)
		if !ok {
			return errOverread("NDA_DST", "F10")
		}
		o.str(printFP, colorNone, "", "dst ", "")
		o.str(printAny, c, "dst", dst+" ", dst)
	}
	if vid != 0 {
		o.uint(printAny, "vlan", "vlan "+strconv.FormatUint(uint64(vid), 10)+" ", uint32(vid))
	}
	if tb.Has(unix.NDA_PORT) {
		b := tb.Bytes(unix.NDA_PORT)
		if len(b) < 2 {
			return errOverread("NDA_PORT", "F10")
		}
		v := uint32(binary.BigEndian.Uint16(b))
		o.uint(printAny, "port", "port "+strconv.FormatUint(uint64(v), 10)+" ", v)
	}
	for _, a := range []struct {
		t   uint16
		key string
	}{{unix.NDA_VNI, wordVni}, {unix.NDA_SRC_VNI, wordSrcVni}} {
		if !tb.Has(a.t) {
			continue
		}
		v, ok := tb.U32(a.t)
		if !ok {
			return errOverread(a.key, "F10")
		}
		o.uint(printAny, a.key, a.key+" "+strconv.FormatUint(uint64(v), 10)+" ", v)
	}
	if tb.Has(unix.NDA_IFINDEX) {
		v, ok := tb.U32(unix.NDA_IFINDEX)
		if !ok {
			return errOverread("NDA_IFINDEX", "F10")
		}
		if tb.Has(unix.NDA_LINK_NETNSID) {
			o.uint(printAny, "viaIfIndex", "via ifindex "+strconv.FormatUint(uint64(v), 10)+" ", v)
		} else {
			name, err := r.indexToName(v)
			if err != nil {
				return err
			}
			o.str(printAny, colorNone, "viaIf", "via "+name+" ", name)
		}
	}
	if tb.Has(xtcpnl.NdaNhID) {
		v, ok := tb.U32(xtcpnl.NdaNhID)
		if !ok {
			return errOverread("NDA_NH_ID", "F10")
		}
		o.uint(printAny, "nhid", "nhid "+strconv.FormatUint(uint64(v), 10)+" ", v)
	}
	if tb.Has(unix.NDA_LINK_NETNSID) {
		v, ok := tb.U32(unix.NDA_LINK_NETNSID)
		if !ok {
			return errOverread("NDA_LINK_NETNSID", "F10")
		}
		// print_uint with "%d": the text is signed, the JSON unsigned (F8).
		o.uint(printAny, "linkNetNsId", "link-netnsid "+strconv.Itoa(int(cint.Int(v)))+" ", v)
	}
	return nil
}

// fdbCacheInfo is print_fdb's -s "used/updated", in user HZ.
func (r *runner) fdbCacheInfo(tb xtcpnl.AttrTable) error {
	if !r.stats || !tb.Has(unix.NDA_CACHEINFO) {
		return nil
	}
	o := r.o
	ci := tb.Bytes(unix.NDA_CACHEINFO)
	if len(ci) < 12 {
		return errOverread("NDA_CACHEINFO", "F9")
	}
	used := binary.LittleEndian.Uint32(ci[4:8]) / userHZ
	updated := binary.LittleEndian.Uint32(ci[8:12]) / userHZ
	if o.isJSON() {
		o.uint(printJSON, "used", "", used)
		o.uint(printJSON, "updated", "", updated)
	} else {
		// "used %d/%d " of unsigned quotients (F9).
		o.printf(fmt.Sprintf("used %d/%d ", cint.Int(used), cint.Int(updated)))
	}
	return nil
}

// fdbFlags is fdb_print_flags: the "flags" array is always there in JSON.
func (r *runner) fdbFlags(flags uint8, ext uint32) {
	o := r.o
	o.openArray(printJSON, "flags")
	for _, f := range []struct {
		set  bool
		name string
	}{
		{flags&unix.NTF_SELF != 0, "self"},
		{flags&unix.NTF_ROUTER != 0, "router"},
		{flags&unix.NTF_EXT_LEARNED != 0, "extern_learn"},
		{flags&unix.NTF_OFFLOADED != 0, "offload"},
		{flags&unix.NTF_MASTER != 0, "master"},
		{flags&xtcpnl.NtfSticky != 0, "sticky"},
		{ext&xtcpnl.NtfExtLocked != 0, "locked"},
	} {
		if f.set {
			o.str(printAny, colorNone, "", f.name+" ", f.name)
		}
	}
	o.closeArray(printJSON, "")
}

// fdbExtAttrs is fdb_print_ext_attrs, under -d.
func (r *runner) fdbExtAttrs(nest []byte) error {
	t, err := xtcpnl.ParseAttrTableFlags(nest, nfeaMaxCst, unix.NLA_F_NESTED)
	if err != nil {
		return err
	}
	if !t.Has(xtcpnl.NfeaActivityNotify) {
		return nil
	}
	n, ok := t.U8(xtcpnl.NfeaActivityNotify)
	if !ok {
		return errOverread("NFEA_ACTIVITY_NOTIFY", "F10")
	}
	if n&fdbNotifyBit != 0 {
		r.o.boolean(printAny, "activity_notify", "activity_notify ", true)
	}
	if n&fdbNotifyInactiveBit != 0 {
		r.o.boolean(printAny, "inactive", "inactive ", true)
	}
	return nil
}

// llAddrN2A is ll_addr_n2a with ll_index_to_type: a tunnel's 4 or 16
// bytes as an address, otherwise colon hex. An empty address is read
// past (addr[0] is printed whatever alen says).
func (r *runner) llAddrN2A(a []byte, ifindex uint32) (string, bool) {
	typ := -1
	if e, ok := r.cache.byIndex[ifindex]; ok && ifindex != 0 {
		typ = int(e.typ)
	}
	switch {
	case len(a) == inetAddrLen && (typ == arphrdTunnel || typ == arphrdSIT || typ == arphrdIPGRE):
		return formatHost(a, inetAddrLen)
	case len(a) == inet6AddrLen && (typ == arphrdTunnel6 || typ == arphrdIP6GRE):
		return formatHost(a, inet6AddrLen)
	case len(a) == 0:
		return "", false
	}
	var b strings.Builder
	for i, c := range a {
		if i > 0 {
			b.WriteByte(':')
		}
		fmt.Fprintf(&b, "%02x", c)
	}
	return b.String(), true
}

// fdbGetArgs is fdb_get's arguments as its loop leaves them.
type fdbGetArgs struct {
	d, br, addr string
	g           xtcpnl.FdbGet
	vni         uint64 // ^0 when unset
	// vlan is fdb_get's short: unset is -1, and atoi's int is cut to 16
	// bits, so 65535 is -1 again and 70000 is 4464 (F13).
	vlan    uint16
	vlanSet bool
}

// fdbGetParse is fdb_get's argument loop.
func (r *runner) fdbGetParse(args []string) (fdbGetArgs, error) {
	a := fdbGetArgs{vni: ^uint64(0)}
	for i := 0; i < len(args); i++ {
		w := args[i]
		next := func() bool { i++; return i < len(args) }
		switch {
		case w == "brport" || w == wordDev:
			if !next() {
				return a, r.incomplete()
			}
			a.d = args[i]
		case w == "br":
			if !next() {
				return a, r.incomplete()
			}
			a.br = args[i]
		case w == wordVni:
			if !next() {
				return a, r.incomplete()
			}
			v, rest, _ := strtoul(args[i], 0)
			if rest != "" || v>>24 != 0 || v == ^uint64(0) {
				// The message ends in its own newline, then invarg's (F13).
				return a, r.invarg("invalid VNI\n", args[i])
			}
			a.vni = v
		case w == "self":
			a.g.Flags |= unix.NTF_SELF
		case matches(w, "master"):
			a.g.Flags |= unix.NTF_MASTER
		case matches(w, "vlan"):
			// The duplicate test comes before NEXT_ARG, so it names "vlan".
			if a.vlanSet {
				return a, r.duparg2("vlan", w)
			}
			if !next() {
				return a, r.incomplete()
			}
			a.vlan, _ = cint.U16From64(uint64(cint.Bits(atoi(args[i])) & 0xffff))
			a.vlanSet = a.vlan&0x8000 == 0
		case matches(w, "dynamic"):
			r.fdbDynamic = true
		default:
			if w == "to" {
				if !next() {
					return a, r.incomplete()
				}
				w = args[i]
			}
			if matches(w, helpWord) {
				return a, r.fdbUsage()
			}
			if a.addr != "" {
				return a, r.duparg2("to", w)
			}
			a.addr = w
		}
	}
	return a, nil
}

// fdbGet is fdb_get.
func (r *runner) fdbGet(args []string) error {
	a, err := r.fdbGetParse(args)
	if err != nil {
		return err
	}
	d, br, addr, g, vni, vlan, vlanSet := a.d, a.br, a.addr, a.g, a.vni, a.vlan, a.vlanSet
	if (d == "" && br == "") || addr == "" {
		r.errf("Device or master and address are required arguments.\n")
		return retCode(-1)
	}
	mac, ok := sscanfMAC(addr)
	if !ok {
		r.errf("Invalid mac address %s\n", addr)
		return retCode(-1)
	}
	g.Lladdr = mac[:]
	if vlanSet {
		g.Vlan, g.HasVlan = vlan, true
	}
	if vni != ^uint64(0) {
		g.Vni, g.HasVni = cint.Low32(vni), true
	}
	if d != "" {
		i, err := r.nameToIndex(d)
		if err != nil {
			return err
		}
		if i == 0 {
			r.errf("Cannot find device \"%s\"\n", d)
			return retCode(-1)
		}
		g.Ifindex = i
	}
	if br != "" {
		i, err := r.nameToIndex(br)
		if err != nil {
			return err
		}
		if i == 0 {
			r.errf("Cannot find bridge device \"%s\"\n", br)
			return retCode(-1)
		}
		g.Master = i
	}
	req, berr := xtcpnl.BuildFdbGetRequest(g, r.nextSeq())
	answer, ok, err := r.talkAnswer(req, berr)
	if err != nil {
		return err
	}
	if !ok {
		return retCode(-2)
	}
	o := r.o
	o.newJSONObj()
	var ret error
	v, perr := r.printFdb(answer)
	if perr != nil {
		return perr
	}
	if v < 0 {
		r.errf("An error :-)\n")
		ret = retCode(-1)
	}
	o.deleteJSONObj()
	return ret
}

// sscanfMAC is sscanf(addr, "%hhx:%hhx:%hhx:%hhx:%hhx:%hhx") == 6: each
// %hhx skips space, takes a sign and an optional 0x, and keeps the low
// byte; anything after the sixth is ignored.
func sscanfMAC(s string) ([6]byte, bool) {
	var mac [6]byte
	for i := range mac {
		if i > 0 {
			if s == "" || s[0] != ':' {
				return mac, false
			}
			s = s[1:]
		}
		v, rest, ok := strtoul(s, 16)
		if !ok {
			return mac, false
		}
		mac[i], _ = cint.U8From64(v & 0xff) // %hhx keeps the low byte
		s = rest
	}
	return mac, true
}

// talkAnswer is rtnl_talk with an answer: the reply's first message, or,
// for an NLMSG_ERROR, its report and false (a non-zero error), or the
// ack itself as the answer.
func (r *runner) talkAnswer(req []byte, berr error) (Msg, bool, error) {
	rep, err := r.talk(req, berr)
	if err != nil {
		return Msg{}, false, err
	}
	if len(rep.Msgs) == 0 {
		return Msg{}, false, fmt.Errorf("%w: a request's reply has no message", ErrReplay)
	}
	m := rep.Msgs[0]
	if m.Type != unix.NLMSG_ERROR {
		return m, true, nil
	}
	if len(m.Body) < 4+nlHeaderLen {
		r.errf("ERROR truncated\n")
		return Msg{}, false, nil
	}
	e := cint.Int(binary.LittleEndian.Uint32(m.Body[0:4]))
	if e == 0 {
		r.dumpExtAck(m, false)
		return m, true, nil
	}
	if !r.dumpExtAck(m, true) {
		r.errf("RTNETLINK answers: %s\n", strerror(unix.Errno(-e)))
	}
	return Msg{}, false, nil
}

// dumpExtAck is nl_dump_ext_ack with no errfn: only with NLM_F_ACK_TLVS,
// the TLVs past struct nlmsgerr (and past the echoed request unless
// NLM_F_CAPPED). It reports whether an error message was printed.
func (r *runner) dumpExtAck(m Msg, isErr bool) bool {
	if m.Flags&unix.NLM_F_ACK_TLVS == 0 {
		return false
	}
	hlen := 4 + nlHeaderLen
	if m.Flags&unix.NLM_F_CAPPED == 0 {
		inner := int(binary.LittleEndian.Uint32(m.Body[4:8]))
		hlen += max(inner-nlHeaderLen, 0)
	}
	if hlen > len(m.Body) {
		return false
	}
	t, err := xtcpnl.ParseAttrTable(m.Body[hlen:], nlmsgerrAttrMaxCst)
	if err != nil {
		return false
	}
	if v, ok := t.U32(nlmsgerrAttrMissTypeCst); ok {
		r.errf("Missing required attribute type %d\n", v)
	}
	msg, _ := t.Str(unix.NLMSGERR_ATTR_MSG)
	if msg == "" {
		return false
	}
	r.extack(isErr, msg)
	return isErr
}
