package goip

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// This file is the argv half of `ip link add|set|change|replace|delete`:
// do_iplink's verb table, iplink_parse, iplink_modify's kind dispatch and
// veth_parse_opt (ip/iplink.c, ip/link_veth.c, iproute2 7.1.0). The bytes
// are xtcpnl.BuildLinkWriteRequest's. See docs/netlink/link-write-path.md.
//
// # goip never applies a write
//
// The request is built, then written to the file GOIP_WRITE_OUT names, and
// never sent. With GOIP_WRITE_OUT unset the verb fails with
// ErrWriteNotApplied, after the argv is parsed and the names are resolved,
// so an argv `ip` refuses is refused here with the same message whether or
// not the hook is set. Name resolution is a read and does go to the kernel:
// `ip` sends those gets before its write, so they are part of what the
// parity harness compares (link-write-path.md, decision 5).

// ErrWriteNotApplied is a write verb run without GOIP_WRITE_OUT.
var ErrWriteNotApplied = errors.New("goip never applies a write; set " + writeOutEnvCst +
	" to receive the request it would send")

// writeOutEnvCst names the file a write verb writes its framed request to.
const writeOutEnvCst = "GOIP_WRITE_OUT"

// linkWriteVerb is one arm of do_iplink that reaches iplink_modify.
type linkWriteVerb struct {
	name    string
	msgType uint16
	// flags are the NLM_F_* bits do_iplink passes; iplink_modify ORs in
	// NLM_F_REQUEST and rtnl_talk NLM_F_ACK.
	flags uint16
}

// linkWriteVerbs is do_iplink's chain up to the show verbs, in its order
// (ip/iplink.c:1515-1529). Order matters because every test is matches():
// `s` is `set`, tried before `show`, and `d` is `delete`.
var linkWriteVerbs = []linkWriteVerb{
	{name: addrVerbAddCst, msgType: unix.RTM_NEWLINK, flags: unix.NLM_F_CREATE | unix.NLM_F_EXCL},
	{name: setVerbCst, msgType: unix.RTM_NEWLINK},
	{name: addrVerbChangeCst, msgType: unix.RTM_NEWLINK},
	{name: addrVerbReplaceCst, msgType: unix.RTM_NEWLINK, flags: unix.NLM_F_CREATE | unix.NLM_F_REPLACE},
	{name: addrVerbDeleteCst, msgType: unix.RTM_DELLINK},
}

func lookupLinkWriteVerb(arg string) *linkWriteVerb {
	for i := range linkWriteVerbs {
		if matchesPrefix(arg, linkWriteVerbs[i].name) {
			return &linkWriteVerbs[i]
		}
	}
	return nil
}

func (v *linkWriteVerb) create() bool { return v.flags&unix.NLM_F_CREATE != 0 }

// linkResolver is ll_name_to_index: the ifindex for a name, 0 for none.
type linkResolver func(name string) (int32, error)

// linkArm is what an iplink_parse keyword does.
type linkArm uint8

const (
	linkArmUp linkArm = iota + 1
	linkArmDown
	linkArmName
	linkArmAddress
	linkArmTxqlen
	linkArmMTU
	linkArmFlag // an on/off ifi_flags bit
	linkArmMaster
	linkArmNomaster
	linkArmType
	linkArmAlias
	linkArmUnsupported // a keyword `ip` knows and goip does not implement
)

// onKeywordCst and offKeywordCst are the two words of every on/off
// argument: link set's flags, the bond options and stats set's suites.
const (
	onKeywordCst  = "on"
	offKeywordCst = "off"
)

// linkKeyword is one test in iplink_parse's chain.
type linkKeyword struct {
	keyword string
	prefix  bool // matches(); otherwise strcmp
	arm     linkArm
	// flag and onSets describe a linkArmFlag: the IFF_ bit, and whether
	// "on" sets it (trailers and arp are inverted).
	flag   uint32
	onSets bool
}

// linkParseKeywords is iplink_parse's if/else chain in its order
// (ip/iplink.c:578-951). Every keyword is listed, the unimplemented ones
// too, because the chain decides what an argument IS: an argument that
// matches no keyword is the device name, so `ip link set l up` is `link`
// and not a device called l, and `d` is `dynamic`. A keyword left out would
// turn into a device name here and diverge silently.
var linkParseKeywords = []linkKeyword{
	{keyword: "up", arm: linkArmUp},
	{keyword: "down", arm: linkArmDown},
	{keyword: "name", arm: linkArmName},
	{keyword: "index", arm: linkArmUnsupported},
	{keyword: "link", prefix: true, arm: linkArmUnsupported},
	{keyword: "address", prefix: true, arm: linkArmAddress},
	{keyword: "broadcast", prefix: true, arm: linkArmUnsupported},
	{keyword: "brd", arm: linkArmUnsupported},
	{keyword: "txqueuelen", prefix: true, arm: linkArmTxqlen},
	{keyword: "qlen", arm: linkArmTxqlen},
	{keyword: "txqlen", prefix: true, arm: linkArmTxqlen},
	{keyword: mtuKeywordCst, arm: linkArmMTU},
	{keyword: "xdpgeneric", arm: linkArmUnsupported},
	{keyword: "xdpdrv", arm: linkArmUnsupported},
	{keyword: "xdpoffload", arm: linkArmUnsupported},
	{keyword: "xdp", arm: linkArmUnsupported},
	{keyword: "netns", arm: linkArmUnsupported},
	{keyword: "multicast", arm: linkArmFlag, flag: unix.IFF_MULTICAST, onSets: true},
	{keyword: "allmulticast", arm: linkArmFlag, flag: unix.IFF_ALLMULTI, onSets: true},
	{keyword: "promisc", arm: linkArmFlag, flag: unix.IFF_PROMISC, onSets: true},
	{keyword: "trailers", arm: linkArmFlag, flag: unix.IFF_NOTRAILERS},
	{keyword: "arp", arm: linkArmFlag, flag: unix.IFF_NOARP},
	{keyword: "carrier", arm: linkArmUnsupported},
	{keyword: "vf", arm: linkArmUnsupported},
	{keyword: "master", prefix: true, arm: linkArmMaster},
	{keyword: vrfKeywordCst, arm: linkArmUnsupported},
	{keyword: "nomaster", prefix: true, arm: linkArmNomaster},
	{keyword: "dynamic", prefix: true, arm: linkArmFlag, flag: unix.IFF_DYNAMIC, onSets: true},
	{keyword: linkTypeKeywordCst, prefix: true, arm: linkArmType},
	{keyword: "alias", prefix: true, arm: linkArmAlias},
	{keyword: "group", arm: linkArmUnsupported},
	{keyword: "mode", arm: linkArmUnsupported},
	{keyword: "state", arm: linkArmUnsupported},
	{keyword: "numtxqueues", prefix: true, arm: linkArmUnsupported},
	{keyword: "numrxqueues", prefix: true, arm: linkArmUnsupported},
	{keyword: "addrgenmode", prefix: true, arm: linkArmUnsupported},
	{keyword: "link-netns", prefix: true, arm: linkArmUnsupported},
	{keyword: "link-netnsid", prefix: true, arm: linkArmUnsupported},
	{keyword: "protodown", arm: linkArmUnsupported},
	{keyword: "protodown_reason", arm: linkArmUnsupported},
	{keyword: "gso_max_size", arm: linkArmUnsupported},
	{keyword: "gso_max_segs", arm: linkArmUnsupported},
	{keyword: "gro_max_size", arm: linkArmUnsupported},
	{keyword: "gso_ipv4_max_size", arm: linkArmUnsupported},
	{keyword: "gro_ipv4_max_size", arm: linkArmUnsupported},
	{keyword: "parentdev", arm: linkArmUnsupported},
}

func lookupLinkKeyword(arg string) *linkKeyword {
	for i := range linkParseKeywords {
		k := &linkParseKeywords[i]
		if (k.prefix && matchesPrefix(arg, k.keyword)) || (!k.prefix && arg == k.keyword) {
			return k
		}
	}
	return nil
}

// Limits from include/uapi/linux/if.h: IFNAMSIZ and ALTIFNAMSIZ include
// the NUL, and IFALIASZ is the alias buffer.
const (
	ifNameSizeCst    = 16
	altIfNameSizeCst = 128
	ifAliasSizeCst   = 256
	// linkLLAddrBufCst is iplink_parse's char abuf[32].
	linkLLAddrBufCst = 32
)

// checkIfname is lib/utils.c's check_ifname, which mirrors the kernel's
// dev_valid_name.
func checkIfname(name string) bool {
	if name == "" || len(name) >= ifNameSizeCst || name == "." || name == ".." {
		return false
	}
	for i := 0; i < len(name); i++ {
		if name[i] == '/' || cIsSpace(name[i]) {
			return false
		}
	}
	return true
}

// checkAltIfname is check_altifname: the `dev` argument may be an altname.
func checkAltIfname(name string) bool {
	return name != "" && len(name) < altIfNameSizeCst
}

// The error text iplink_parse prints, from lib/utils.c.
func invargErr(msg, arg string) error {
	return fmt.Errorf("%w: Error: argument %q is wrong: %s", ErrInvalidArgument, arg, msg)
}

func dupargErr(key, arg string) error {
	return fmt.Errorf("%w: Error: duplicate %q: %q is the second value", ErrInvalidArgument, key, arg)
}

func incompleteErr() error {
	return fmt.Errorf("%w: Command line is not complete. Try option \"help\"", ErrInvalidArgument)
}

func nodevErr(dev string) error {
	return fmt.Errorf("%w: Cannot find device %q", ErrInvalidArgument, dev)
}

// devState tracks iplink_parse's `dev` pointer against its `name` pointer,
// which the C compares by address: `if (dev != name) duparg2(...)`.
type devState uint8

const (
	devUnset    devState = iota // dev == name == NULL
	devFromName                 // `name X` set dev = name
	devExplicit                 // a bare argument or `dev X`
)

// linkParsed is what iplink_parse leaves in the request: the ifinfomsg
// fields, the attributes in wire order, and the kind with the arguments
// after it.
type linkParsed struct {
	index    int32
	ifFlags  uint32
	ifChange uint32
	attrs    []xtcpnl.LinkAttr
	kind     string
	hasKind  bool
	rest     []string
	// consumed is iplink_parse's return value: the arguments it read,
	// `type KIND` included. veth_parse_opt indexes argv with it.
	consumed int
}

// linkParseState is iplink_parse's locals.
type linkParseState struct {
	create  bool
	resolve linkResolver
	out     linkParsed
	name    string
	dev     string
	devSt   devState
	mtu     int
	qlen    int
	addrLen int
}

// parseLinkArgs is iplink_parse for the keywords goip implements. create
// is NLM_F_CREATE in the request, which decides how the device is
// addressed: by name on add and replace, by ifindex otherwise.
func parseLinkArgs(args []string, create bool, resolve linkResolver) (linkParsed, error) {
	st := linkParseState{create: create, resolve: resolve, mtu: -1, qlen: -1}
	i := 0
loop:
	for ; i < len(args); i++ {
		arg := args[i]
		kw := lookupLinkKeyword(arg)
		if kw == nil {
			var err error
			if i, err = st.devArg(args, i); err != nil {
				return linkParsed{}, err
			}
			continue
		}
		if kw.arm == linkArmUp || kw.arm == linkArmDown || kw.arm == linkArmNomaster {
			st.noValue(kw.arm)
			continue
		}
		if kw.arm == linkArmUnsupported {
			return linkParsed{}, fmt.Errorf("link keyword %q: %w", kw.keyword, ErrNotImplemented)
		}
		// Every remaining arm is NEXT_ARG.
		if i+1 >= len(args) {
			return linkParsed{}, incompleteErr()
		}
		i++
		if kw.arm == linkArmType {
			st.out.kind = args[i]
			st.out.hasKind = true
			i++
			break loop
		}
		if err := st.value(kw, args[i]); err != nil {
			return linkParsed{}, err
		}
	}
	st.out.consumed = i
	st.out.rest = args[i:]
	if err := st.finish(); err != nil {
		return linkParsed{}, err
	}
	return st.out, nil
}

// devArg is the else arm: `help`, `dev X`, or a bare device name. It
// returns the index of the last argument it consumed.
func (st *linkParseState) devArg(args []string, i int) (int, error) {
	if matchesPrefix(args[i], bondHelpKeywordCst) {
		return i, fmt.Errorf("%w: ip link help is not implemented by goip", ErrNotImplemented)
	}
	if args[i] == devKeywordCst {
		if i+1 >= len(args) {
			return i, incompleteErr()
		}
		i++
	}
	arg := args[i]
	if st.devSt == devExplicit {
		return i, fmt.Errorf("%w: Error: either \"dev\" is duplicate, or %q is garbage", ErrInvalidArgument, arg)
	}
	if !checkAltIfname(arg) {
		return i, invargErr("\"dev\" not a valid ifname", arg)
	}
	st.dev = arg
	st.devSt = devExplicit
	return i, nil
}

func (st *linkParseState) noValue(arm linkArm) {
	switch arm {
	case linkArmUp:
		st.out.ifChange |= unix.IFF_UP
		st.out.ifFlags |= unix.IFF_UP
	case linkArmDown:
		st.out.ifChange |= unix.IFF_UP
		st.out.ifFlags &^= unix.IFF_UP
	default: // linkArmNomaster
		st.out.attrs = append(st.out.attrs, xtcpnl.LinkAttrU32(unix.IFLA_MASTER, 0))
	}
}

// value runs the keyword arms that read one argument.
func (st *linkParseState) value(kw *linkKeyword, v string) error {
	switch kw.arm {
	case linkArmName:
		if st.name != "" {
			return dupargErr("name", v)
		}
		if !checkIfname(v) {
			return invargErr("\"name\" not a valid ifname", v)
		}
		st.name = v
		if st.devSt == devUnset {
			st.dev = v
			st.devSt = devFromName
		}
	case linkArmAddress:
		b, ok := llAddrA2N(v, linkLLAddrBufCst)
		if !ok {
			return fmt.Errorf("%w: %q is invalid lladdr", ErrInvalidArgument, v)
		}
		st.addrLen = len(b)
		st.out.attrs = append(st.out.attrs, xtcpnl.LinkAttrBytes(unix.IFLA_ADDRESS, b))
	case linkArmTxqlen:
		return st.intAttr(&st.qlen, "txqueuelen", unix.IFLA_TXQLEN, v)
	case linkArmMTU:
		return st.intAttr(&st.mtu, mtuKeywordCst, unix.IFLA_MTU, v)
	case linkArmFlag:
		st.out.ifChange |= kw.flag
		switch v {
		case onKeywordCst:
			st.setFlag(kw.flag, kw.onSets)
		case offKeywordCst:
			st.setFlag(kw.flag, !kw.onSets)
		default:
			return fmt.Errorf("%w: Error: argument of %q must be \"on\" or \"off\", not %q",
				ErrInvalidArgument, kw.keyword, v)
		}
	case linkArmMaster:
		idx, err := st.resolve(v)
		if err != nil {
			return err
		}
		if idx == 0 {
			return invargErr("Device does not exist\n", v)
		}
		st.out.attrs = append(st.out.attrs, xtcpnl.LinkAttrU32(unix.IFLA_MASTER, uint32(idx))) //nolint:gosec // an ifindex
	case linkArmAlias:
		if len(v) >= ifAliasSizeCst {
			return invargErr("alias too long\n", v)
		}
		st.out.attrs = append(st.out.attrs, xtcpnl.LinkAttrBytes(unix.IFLA_IFALIAS, []byte(v)))
	default:
		return fmt.Errorf("goip: link keyword %q has no value reader", kw.keyword)
	}
	return nil
}

// intAttr is the mtu and txqueuelen arms: get_integer base 0, a duplicate
// test against the -1 sentinel (so `mtu -1 mtu 5` is not a duplicate, as
// in `ip`), and the C int written as four bytes.
func (st *linkParseState) intAttr(seen *int, key string, atype uint16, v string) error {
	if *seen != -1 {
		return dupargErr(key, v)
	}
	n, ok := getInteger(v, 0)
	if !ok {
		return invargErr("Invalid \""+key+"\" value\n", v)
	}
	*seen = n
	st.out.attrs = append(st.out.attrs, xtcpnl.LinkAttrU32(atype, uint32(int32(n)))) //nolint:gosec // the C int, reinterpreted
	return nil
}

func (st *linkParseState) setFlag(flag uint32, set bool) {
	if set {
		st.out.ifFlags |= flag
		return
	}
	st.out.ifFlags &^= flag
}

// finish is iplink_parse after its loop: reconcile name and dev, resolve
// the device on a non-create, and append IFLA_IFNAME.
func (st *linkParseState) finish() error {
	// `if (!name) name = dev; ... else if (!strcmp(name, dev)) name = dev;`
	// leaves name == dev, by address, exactly when this holds.
	sameName := st.name == "" || st.name == st.dev
	haveDev := st.devSt != devUnset

	if haveDev && st.addrLen > 0 && !st.create {
		// nl_get_ll_addr_len(dev): one more read, for a length check.
		return fmt.Errorf("link set address: the address-length lookup is %w", ErrNotImplemented)
	}

	if !st.create {
		if !haveDev {
			return fmt.Errorf("%w: Not enough information: \"dev\" argument is required", ErrInvalidArgument)
		}
		idx, err := st.resolve(st.dev)
		if err != nil {
			return err
		}
		if idx == 0 {
			return nodevErr(st.dev)
		}
		st.out.index = idx
		if !sameName {
			st.out.attrs = append(st.out.attrs, xtcpnl.LinkAttrString(unix.IFLA_IFNAME, st.name))
		}
		return nil
	}

	if !sameName {
		return fmt.Errorf("%w: both \"name\" and \"dev\" cannot be used when creating devices", ErrInvalidArgument)
	}
	// `index N` is not implemented, so ifi_index stays 0.
	if haveDev {
		st.out.attrs = append(st.out.attrs, xtcpnl.LinkAttrString(unix.IFLA_IFNAME, st.dev))
	}
	return nil
}

// Kinds iplink_modify treats specially.
const (
	kindBondCst      = "bond"
	kindBondSlaveCst = "bond_slave"
	kindVethCst      = "veth"
	kindDummyCst     = "dummy"
	vethPeerCst      = "peer"
)

// buildLinkWrite is iplink_modify up to the send: parse, then the kind and
// its options.
func buildLinkWrite(verb *linkWriteVerb, family uint8, args []string, resolve linkResolver) (xtcpnl.LinkWrite, error) {
	p, err := parseLinkArgs(args, verb.create(), resolve)
	if err != nil {
		return xtcpnl.LinkWrite{}, err
	}
	w := xtcpnl.LinkWrite{
		MsgType:  verb.msgType,
		Flags:    verb.flags | unix.NLM_F_ACK,
		Family:   family,
		Index:    p.index,
		IfFlags:  p.ifFlags,
		IfChange: p.ifChange,
		Attrs:    p.attrs,
	}
	if !p.hasKind {
		if verb.create() {
			return xtcpnl.LinkWrite{}, fmt.Errorf("%w: Not enough information: \"type\" argument is required",
				ErrInvalidArgument)
		}
		return w, nil
	}
	if p.kind == "" {
		// `type ""` is IFLA_INFO_KIND "\0" in `ip`, which LinkWrite cannot
		// express: an empty Kind means no IFLA_LINKINFO.
		return xtcpnl.LinkWrite{}, fmt.Errorf("link type \"\": %w", ErrNotImplemented)
	}
	w.Kind = p.kind
	if len(p.rest) == 0 {
		return w, nil
	}
	w.Data, err = kindData(verb, p.kind, p.rest, resolve)
	if err != nil {
		return xtcpnl.LinkWrite{}, err
	}
	w.HasData = true
	return w, nil
}

// kindData is lu->parse_opt for the kinds goip implements, run only when
// arguments follow the kind.
func kindData(verb *linkWriteVerb, kind string, rest []string, resolve linkResolver) ([]byte, error) {
	switch kind {
	case kindBondCst:
		settings, err := parseBondArgs(rest, func(name string) (uint32, error) {
			idx, err := resolve(name)
			return uint32(idx), err //nolint:gosec // an ifindex
		})
		if err != nil {
			return nil, err
		}
		return xtcpnl.EncodeBondInfoData(settings)
	case kindBondSlaveCst:
		settings, err := parseBondSlaveArgs(rest)
		if err != nil {
			return nil, err
		}
		return xtcpnl.EncodeBondSlaveData(settings)
	case kindVethCst:
		return vethData(verb, rest, resolve)
	case kindDummyCst:
		// dummy has no parse_opt (ip/iplink_dummy.c), so iplink_modify
		// takes its else arm.
		if matchesPrefix(rest[0], bondHelpKeywordCst) {
			return nil, fmt.Errorf("%w: ip link help is not implemented by goip", ErrNotImplemented)
		}
		return nil, fmt.Errorf("%w: Garbage instead of arguments \"%s ...\". Try \"ip link help\"",
			ErrInvalidArgument, rest[0])
	default:
		return nil, fmt.Errorf("link type %q options: %w", kind, ErrNotImplemented)
	}
}

// vethData is veth_parse_opt: `peer` and then a second iplink_parse, whose
// header fields go in the peer's own ifinfomsg while the outer ones are
// kept. Parsing into a fresh linkParsed is the save, zero and restore the
// C does around the shared request.
func vethData(verb *linkWriteVerb, rest []string, resolve linkResolver) ([]byte, error) {
	if rest[0] != vethPeerCst {
		return nil, fmt.Errorf("%w: veth: expected \"peer\", not %q", ErrInvalidArgument, rest[0])
	}
	p, err := parseLinkArgs(rest[1:], verb.create(), resolve)
	if err != nil {
		return nil, err
	}
	if p.hasKind {
		// duparg("type", argv[err]): veth's argv starts at "peer", so
		// index consumed is the peer's kind value.
		return nil, dupargErr(linkTypeKeywordCst, p.kind)
	}
	return xtcpnl.EncodeVethPeer(xtcpnl.VethPeer{
		Index:    p.index,
		IfFlags:  p.ifFlags,
		IfChange: p.ifChange,
		Attrs:    p.attrs,
	})
}

// linkWrite runs a write verb: resolve, build, and hand the request to
// GOIP_WRITE_OUT instead of the kernel.
func linkWrite(c *runCtx, verb *linkWriteVerb, args []string) error {
	svc := service.New(c.src, c.nextSeq)
	w, err := buildLinkWrite(verb, c.family, args, newLinkNameResolver(svc))
	if err != nil {
		return err
	}
	b, err := xtcpnl.BuildLinkWriteRequest(w, c.nextSeq())
	if err != nil {
		return fmt.Errorf("goip: build link write: %w", err)
	}
	path, ok := os.LookupEnv(writeOutEnvCst)
	if !ok || path == "" {
		return ErrWriteNotApplied
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return fmt.Errorf("goip: %s: %w", writeOutEnvCst, err)
	}
	return nil
}

// newLinkNameResolver is ll_name_to_index over one command (lib/ll_map.c:
// 353-371): the cache, then ll_link_get, then the "ifN" spelling.
//
// The cache is why `ip link set la1 master bond0 type bond_slave …` costs
// one get per distinct name: ll_link_get remembers the reply under its name
// and its altnames, and a later lookup of either is answered from memory.
//
// Any errno from the get is a miss, because ll_link_get returns 0 for every
// failure and suppresses the message. if_nametoindex, tried next in `ip`,
// is an ioctl on the same namespace's device table, so after a refused get
// it is skipped here. A failure that is not a kernel answer (a broken
// socket, a replay with no matching request) is returned.
func newLinkNameResolver(svc *service.Service) linkResolver {
	return newLinkNameResolverRemember(svc, nil)
}

// newLinkNameResolverRemember is newLinkNameResolver that also hands each
// link it fetches to remember. ll_link_get stores its reply in the one ll_map
// that ll_index_to_name reads later (lib/ll_map.c:296), so a command that
// renders the names its lookups resolved, `ip addr show master bond0`, must
// not fetch them a second time by index. remember may be nil.
func newLinkNameResolverRemember(svc *service.Service, remember func(xtcpnl.LinkInfo)) linkResolver {
	cache := map[string]int32{}
	return func(name string) (int32, error) {
		if idx, ok := cache[name]; ok {
			return idx, nil
		}
		if !checkIfname(name) {
			// ll_link_get would send IFLA_ALT_IFNAME; req.LinkShowByName
			// has no captured example to build it from.
			return 0, fmt.Errorf("resolving %q by IFLA_ALT_IFNAME: %w", name, ErrNotImplemented)
		}
		l, err := svc.LinkByName(name)
		var errno syscall.Errno
		switch {
		case err == nil:
			if remember != nil {
				remember(xtcpnl.LinkInfo(l))
			}
			cache[l.Name] = l.Index
			for _, alt := range l.AltNames {
				cache[alt] = l.Index
			}
			return l.Index, nil
		case errors.As(err, &errno):
			return int32(llIdxA2N(name)), nil //nolint:gosec // the C unsigned, reinterpreted
		default:
			return 0, err
		}
	}
}

// llIdxA2N is ll_idx_a2n: sscanf(name, "if%u"), so "if3" is ifindex 3 and
// trailing characters after the digits are ignored.
func llIdxA2N(name string) uint32 {
	rest, ok := strings.CutPrefix(name, "if")
	if !ok {
		return 0
	}
	r := cStrtoul(rest, 10)
	if r.end == 0 {
		return 0
	}
	return uint32(r.val) //nolint:gosec // %u stores the low 32 bits
}
