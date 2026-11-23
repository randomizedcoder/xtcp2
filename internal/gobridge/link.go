package gobridge

import (
	"strconv"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// bridge/link.c: `bridge link show`.

// doLink is do_link: ll_init_map, then show (by prefix) or a refusal.
func (r *runner) doLink(args []string) error {
	if err := r.initMap(); err != nil {
		return err
	}
	if len(args) > 0 {
		switch {
		case matches(args[0], "set"), matches(args[0], "change"):
			return r.refuse("link set")(nil)
		case showWords(args[0], false):
			return r.linkShow(args[1:])
		case matches(args[0], helpWord):
			return r.linkUsage()
		}
		return r.unknownCommand("link", args[0])
	}
	return r.linkShow(nil)
}

// linkShow is brlink_show. Its loop tests "master" after "dev" with no
// else (B1): a dev value of "master" is read again as the keyword.
func (r *runner) linkShow(args []string) error {
	var dev, master string
	for i := 0; i < len(args); i++ {
		if args[i] == wordDev {
			if i+1 >= len(args) {
				return r.incomplete()
			}
			i++
			if dev != "" {
				return r.duparg(wordDev, args[i])
			}
			dev = args[i]
		}
		if args[i] == "master" {
			if i+1 >= len(args) {
				return r.incomplete()
			}
			i++
			if master != "" {
				return r.duparg("master", args[i])
			}
			master = args[i]
		}
	}
	if err := r.resolveFilter(dev, &r.linkDev); err != nil {
		return err
	}
	if err := r.resolveFilter(master, &r.linkMaster); err != nil {
		return err
	}
	req, berr := xtcpnl.BuildDumpLinkRequestExt(unix.AF_BRIDGE, 0, r.nextSeq())
	r.o.newJSONObj()
	var perr error
	rc, err := r.dump(req, berr, func(m Msg) int {
		v, e := r.printLinkinfo(m)
		if e != nil && perr == nil {
			perr = e
		}
		return v
	})
	if err != nil {
		return err
	}
	if perr != nil {
		return perr
	}
	if rc < 0 {
		r.errf("Dump terminated\n")
		return exitCode(1)
	}
	r.o.deleteJSONObj()
	return nil
}

// linkUsage is link.c usage.
func (r *runner) linkUsage() error {
	r.errf("%s", "Usage: bridge link set dev DEV [ cost COST ] [ priority PRIO ] [ state STATE ]\n"+
		"                               [ guard {on | off} ]\n"+
		"                               [ hairpin {on | off} ]\n"+
		"                               [ fastleave {on | off} ]\n"+
		"                               [ root_block {on | off} ]\n"+
		"                               [ learning {on | off} ]\n"+
		"                               [ learning_sync {on | off} ]\n"+
		"                               [ flood {on | off} ]\n"+
		"                               [ mcast_router MULTICAST_ROUTER ]\n"+
		"                               [ mcast_flood {on | off} ]\n"+
		"                               [ bcast_flood {on | off} ]\n"+
		"                               [ mcast_to_unicast {on | off} ]\n"+
		"                               [ mcast_max_groups MAX_GROUPS ]\n"+
		"                               [ neigh_suppress {on | off} ]\n"+
		"                               [ neigh_vlan_suppress {on | off} ]\n"+
		"                               [ neigh_forward_grat {on | off} ]\n"+
		"                               [ vlan_tunnel {on | off} ]\n"+
		"                               [ isolated {on | off} ]\n"+
		"                               [ locked {on | off} ]\n"+
		"                               [ mab {on | off} ]\n"+
		"                               [ hwmode {vepa | veb} ]\n"+
		"                               [ backup_port DEVICE ] [ nobackup_port ]\n"+
		"                               [ backup_nhid NHID ]\n"+
		"                               [ self ] [ master ]\n"+
		"       bridge link show [dev DEV] [master DEVICE]\n")
	return exitCode(255)
}

// stpStates is link.c stp_states[].
var stpStates = [...]string{"disabled", "listening", "learning", "forwarding", "blocking"}

// printSTPState is print_stp_state.
func (r *runner) printSTPState(state uint8) {
	if int(state) < len(stpStates) {
		r.o.str(printAny, colorNone, "state", "state "+stpStates[state]+" ", stpStates[state])
		return
	}
	r.o.uint(printAny, "state", "state ("+strconv.Itoa(int(state))+") ", uint32(state))
}

// printLinkinfo is print_linkinfo. It returns the callback's int (-1 ends
// the dump) and any replay or output error.
func (r *runner) printLinkinfo(m Msg) (int, error) {
	if len(m.Body) < xtcpnl.IfInfomsgSizeCst {
		r.errf("Message too short!\n")
		return -1, nil
	}
	l, err := xtcpnl.ParseBridgeLink(m.Body)
	if err != nil {
		return -1, err
	}
	if l.Family != unix.AF_BRIDGE && l.Family != unix.AF_UNSPEC {
		return 0, nil
	}
	index := cint.Bits(l.Index) // the unsigned filter compares ifi_index so
	if r.linkDev != 0 && r.linkDev != index {
		return 0, nil
	}
	if mst, ok := l.Attrs.U32(unix.IFLA_MASTER); r.linkMaster != 0 && ok && r.linkMaster != mst {
		return 0, nil // a link with no IFLA_MASTER passes (B7)
	}
	name, ok := l.Attrs.Str(unix.IFLA_IFNAME)
	if !l.Attrs.Has(unix.IFLA_IFNAME) {
		r.errf("BUG: device with ifindex %d has nil ifname\n", l.Index)
		name, ok = "if"+strconv.FormatUint(uint64(index), 10), true
	}
	if !ok || !validIfName(name) {
		return -1, nil
	}
	o := r.o
	o.openObject("")
	if m.Type == unix.RTM_DELLINK {
		o.boolean(printAny, "deleted", "Deleted ", true)
	}
	o.int(printAny, "ifindex", strconv.Itoa(int(l.Index))+": ", l.Index)
	mdown, err := r.printNameAndLink(name, l.Attrs)
	if err != nil {
		return -1, err
	}
	r.printLinkFlags(l.Flags, mdown)
	if mtu, ok := l.Attrs.U32(unix.IFLA_MTU); ok {
		// print_int with "%u": unsigned text, signed JSON (B12).
		o.int(printAny, "mtu", "mtu "+strconv.FormatUint(uint64(mtu), 10)+" ", cint.Int(mtu))
	}
	if mst, ok := l.Attrs.U32(unix.IFLA_MASTER); ok {
		n, err := r.indexToName(mst)
		if err != nil {
			return -1, err
		}
		o.str(printAny, colorNone, "master", "master "+n+" ", n)
	}
	if l.Attrs.Has(xtcpnl.IflaProtinfoCst) {
		r.printProtinfo(l)
	}
	if l.Attrs.Has(xtcpnl.IflaAfSpecCst) {
		r.printAFSpec(l)
	}
	o.str(printFP, colorNone, "", "\n", "\n")
	o.closeObject()
	return 0, nil
}

// validIfName is check_ifname == 0: 1-15 bytes, no '/', ':' or space.
func validIfName(name string) bool {
	if name == "" || len(name) >= ifNameSize {
		return false
	}
	for _, c := range []byte(name) {
		if c == '/' || c == ':' || c == ' ' || c == '\t' || c == '\n' || c == '\v' || c == '\f' || c == '\r' {
			return false
		}
	}
	return true
}

// printNameAndLink is print_name_and_link with "%s: ": the name, with
// "@peer" for an IFLA_LINK. It returns the M-DOWN flag.
func (r *runner) printNameAndLink(name string, tb xtcpnl.AttrTable) (bool, error) {
	o := r.o
	mdown := false
	if tb.Has(unix.IFLA_LINK) {
		v, _ := tb.U32(unix.IFLA_LINK)
		link := ""
		switch {
		case v == 0 && o.isJSON():
			o.emit(printJSON, colorNone, "link", "null", "")
		case v == 0:
			link = "NONE"
		case tb.Has(unix.IFLA_LINK_NETNSID) && o.isJSON():
			o.int(printJSON, "link_index", "", cint.Int(v))
		case tb.Has(unix.IFLA_LINK_NETNSID):
			link = "if" + strconv.FormatUint(uint64(v), 10)
		default:
			n, err := r.indexToName(v)
			if err != nil {
				return false, err
			}
			if o.isJSON() {
				o.str(printJSON, colorNone, "link", "", n)
			} else {
				link = n
			}
			mdown = r.cache.byIndex[v].flags&unix.IFF_UP == 0
		}
		if link != "" {
			name += "@" + link
		}
	}
	o.str(printAny, colorIfname, "ifname", name+": ", name)
	return mdown, nil
}

// linkFlagNames is print_link_flags' _PF order.
var linkFlagNames = []struct {
	bit  uint32
	name string
}{
	{unix.IFF_LOOPBACK, "LOOPBACK"}, {unix.IFF_BROADCAST, "BROADCAST"}, {unix.IFF_POINTOPOINT, "POINTOPOINT"},
	{unix.IFF_MULTICAST, "MULTICAST"}, {unix.IFF_NOARP, "NOARP"}, {unix.IFF_ALLMULTI, "ALLMULTI"},
	{unix.IFF_PROMISC, "PROMISC"}, {unix.IFF_MASTER, "MASTER"}, {unix.IFF_SLAVE, "SLAVE"},
	{unix.IFF_DEBUG, "DEBUG"}, {unix.IFF_DYNAMIC, "DYNAMIC"}, {unix.IFF_AUTOMEDIA, "AUTOMEDIA"},
	{unix.IFF_PORTSEL, "PORTSEL"}, {unix.IFF_NOTRAILERS, "NOTRAILERS"}, {unix.IFF_UP, "UP"},
	{unix.IFF_LOWER_UP, "LOWER_UP"}, {unix.IFF_DORMANT, "DORMANT"}, {unix.IFF_ECHO, "ECHO"},
}

// printLinkFlags is link.c print_link_flags.
func (r *runner) printLinkFlags(flags uint32, mdown bool) {
	o := r.o
	open := "<"
	if o.isJSON() {
		open = "flags"
	}
	o.openArray(printAny, open)
	item := func(name string) {
		text := name
		if flags != 0 {
			text += ","
		}
		o.str(printAny, colorNone, "", text, name)
	}
	if flags&unix.IFF_UP != 0 && flags&unix.IFF_RUNNING == 0 {
		item("NO-CARRIER")
	}
	flags &^= unix.IFF_RUNNING
	for _, f := range linkFlagNames {
		if flags&f.bit != 0 {
			flags &^= f.bit
			item(f.name)
		}
	}
	if flags != 0 {
		o.hex(printAny, "", strconv.FormatUint(uint64(flags), 16), flags)
	}
	if mdown {
		o.str(printAny, colorNone, "", ",M-DOWN", "M-DOWN")
	}
	o.closeArray(printAny, "> ")
}

// brportDetail is print_protinfo's -d list after cost: each IFLA_BRPORT_*
// as print_on_off, or as print_uint where uint.
var brportDetail = []struct {
	id   uint16
	key  string
	uint bool
}{
	{unix.IFLA_BRPORT_MODE, "hairpin", false},
	{unix.IFLA_BRPORT_GUARD, "guard", false},
	{unix.IFLA_BRPORT_PROTECT, "root_block", false},
	{unix.IFLA_BRPORT_FAST_LEAVE, "fastleave", false},
	{unix.IFLA_BRPORT_LEARNING, "learning", false},
	{unix.IFLA_BRPORT_LEARNING_SYNC, "learning_sync", false},
	{unix.IFLA_BRPORT_UNICAST_FLOOD, "flood", false},
	{unix.IFLA_BRPORT_MCAST_FLOOD, "mcast_flood", false},
	{unix.IFLA_BRPORT_BCAST_FLOOD, "bcast_flood", false},
	{unix.IFLA_BRPORT_MULTICAST_ROUTER, "mcast_router", true},
	{unix.IFLA_BRPORT_MCAST_TO_UCAST, "mcast_to_unicast", false},
	{unix.IFLA_BRPORT_NEIGH_SUPPRESS, "neigh_suppress", false},
	{unix.IFLA_BRPORT_NEIGH_VLAN_SUPPRESS, "neigh_vlan_suppress", false},
	{xtcpnl.IflaBrportNeighForwardGrat, "neigh_forward_grat", false},
	{unix.IFLA_BRPORT_VLAN_TUNNEL, "vlan_tunnel", false},
}

// brportTail is the list after vlan_tunnel and backup_port/backup_nhid.
var brportTail = []struct {
	id  uint16
	key string
	u32 bool
}{
	{unix.IFLA_BRPORT_ISOLATED, "isolated", false},
	{unix.IFLA_BRPORT_LOCKED, "locked", false},
	{unix.IFLA_BRPORT_MAB, "mab", false},
	{unix.IFLA_BRPORT_MCAST_N_GROUPS, "mcast_n_groups", true},
	{unix.IFLA_BRPORT_MCAST_MAX_GROUPS, "mcast_max_groups", true},
}

// printProtinfo is print_protinfo: the nest's state, priority and cost,
// then under -d the rest; a PROTINFO without NLA_F_NESTED is a bare state.
func (r *runner) printProtinfo(l xtcpnl.BridgeLink) {
	o := r.o
	if !l.ProtInfoNested {
		s, _ := l.Attrs.U8(xtcpnl.IflaProtinfoCst)
		r.printSTPState(s)
		return
	}
	p := l.Port
	if s, ok := p.U8(unix.IFLA_BRPORT_STATE); ok || p.Has(unix.IFLA_BRPORT_STATE) {
		r.printSTPState(s)
	}
	if v, ok := p.U16(unix.IFLA_BRPORT_PRIORITY); ok || p.Has(unix.IFLA_BRPORT_PRIORITY) {
		o.uint(printAny, "priority", "priority "+strconv.Itoa(int(v))+" ", uint32(v))
	}
	if v, ok := p.U32(unix.IFLA_BRPORT_COST); ok || p.Has(unix.IFLA_BRPORT_COST) {
		o.uint(printAny, "cost", "cost "+strconv.FormatUint(uint64(v), 10)+" ", v)
	}
	if !r.details {
		return
	}
	if !o.isJSON() {
		o.raw(o.sl + "    ")
	}
	for _, d := range brportDetail {
		if !p.Has(d.id) {
			continue
		}
		v, _ := p.U8(d.id)
		if d.uint {
			o.uint(printAny, d.key, d.key+" "+strconv.Itoa(int(v))+" ", uint32(v))
			continue
		}
		o.onOff(printAny, d.key, d.key+" %s ", v != 0)
	}
	if p.Has(unix.IFLA_BRPORT_BACKUP_PORT) {
		v, _ := p.U32(unix.IFLA_BRPORT_BACKUP_PORT)
		n, err := r.indexToName(v)
		if err != nil && r.errOut == nil {
			r.errOut = err
		}
		o.str(printAny, colorNone, "backup_port", "backup_port "+n+" ", n)
	}
	if p.Has(unix.IFLA_BRPORT_BACKUP_NHID) {
		v, _ := p.U32(unix.IFLA_BRPORT_BACKUP_NHID)
		o.uint(printAny, "backup_nhid", "backup_nhid "+strconv.FormatUint(uint64(v), 10)+" ", v)
	}
	for _, d := range brportTail {
		if !p.Has(d.id) {
			continue
		}
		if d.u32 {
			v, _ := p.U32(d.id)
			o.uint(printAny, d.key, d.key+" "+strconv.FormatUint(uint64(v), 10)+" ", v)
			continue
		}
		v, _ := p.U8(d.id)
		o.onOff(printAny, d.key, d.key+" %s ", v != 0)
	}
}

// hwModes is link.c hw_mode[].
var hwModes = [...]string{"VEB", "VEPA"}

// printAFSpec is print_af_spec: IFLA_BRIDGE_MODE as hwmode.
func (r *runner) printAFSpec(l xtcpnl.BridgeLink) {
	if !l.AFSpec.Has(xtcpnl.IflaBridgeMode) {
		return
	}
	mode, _ := l.AFSpec.U16(xtcpnl.IflaBridgeMode)
	if int(mode) >= len(hwModes) {
		r.o.hex0x(printAny, "hwmode", "hwmode "+cHashX(uint64(mode))+" ", uint64(mode))
		return
	}
	r.o.str(printAny, colorNone, "hwmode", "hwmode "+hwModes[mode]+" ", hwModes[mode])
}
