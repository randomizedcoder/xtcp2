package goip

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"syscall"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/cint"
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

// setOp is a kind parser's set_op: an RTM_NEWLINK without NLM_F_CREATE.
func (v *linkWriteVerb) setOp() bool { return v.msgType == unix.RTM_NEWLINK && !v.create() }

// linkResolver is ll_name_to_index: the ifindex for a name, 0 for none.
type linkResolver func(name string) (int32, error)

// vrfChecker is name_is_vrf: whether the device called name is a vrf.
type vrfChecker func(name string) (bool, error)

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
	linkArmInet
	linkArmLink // the lower device, resolved on a create
	linkArmVrf  // a vrf by name, sent as IFLA_MASTER
	linkArmTxQueues
	linkArmRxQueues
	linkArmGroup
	linkArmU32Max      // get_unsigned in base 0, at most maxVal, as a u32 attribute
	linkArmAddrGenMode // IFLA_AF_SPEC{AF_INET6{IFLA_INET6_ADDR_GEN_MODE}}
	linkArmProtodown   // parse_on_off, IFLA_PROTO_DOWN u8
	linkArmProtodownReason
	linkArmBroadcast   // ll_addr_a2n, IFLA_BROADCAST
	linkArmCarrier     // parse_on_off, IFLA_CARRIER u8
	linkArmLinkMode    // get_link_mode, IFLA_LINKMODE u8
	linkArmOperstate   // get_operstate, IFLA_OPERSTATE u8
	linkArmXdp         // xdp_parse's off/none: IFLA_XDP{FD -1, FLAGS}
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
	// attr and maxVal describe a linkArmU32Max: the IFLA_* it sends, and
	// its bound.
	attr   uint16
	maxVal uint64
	// xdpMode is a linkArmXdp's XDP_FLAGS_*_MODE bit; plain xdp has none.
	xdpMode uint32
}

// linkProtodownKeywordCst is protodown, whose u8 shares carrier's arm.
const linkProtodownKeywordCst = "protodown"

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
	{keyword: "link", prefix: true, arm: linkArmLink},
	{keyword: "address", prefix: true, arm: linkArmAddress},
	{keyword: "broadcast", prefix: true, arm: linkArmBroadcast},
	{keyword: "brd", arm: linkArmBroadcast},
	{keyword: "txqueuelen", prefix: true, arm: linkArmTxqlen},
	{keyword: "qlen", arm: linkArmTxqlen},
	{keyword: "txqlen", prefix: true, arm: linkArmTxqlen},
	{keyword: mtuKeywordCst, arm: linkArmMTU},
	{keyword: inetKeywordCst, arm: linkArmInet},
	{keyword: "xdpgeneric", arm: linkArmXdp, xdpMode: unix.XDP_FLAGS_SKB_MODE},
	{keyword: "xdpdrv", arm: linkArmXdp, xdpMode: unix.XDP_FLAGS_DRV_MODE},
	{keyword: "xdpoffload", arm: linkArmXdp, xdpMode: unix.XDP_FLAGS_HW_MODE},
	{keyword: "xdp", arm: linkArmXdp},
	{keyword: "netns", arm: linkArmUnsupported},
	{keyword: "multicast", arm: linkArmFlag, flag: unix.IFF_MULTICAST, onSets: true},
	{keyword: "allmulticast", arm: linkArmFlag, flag: unix.IFF_ALLMULTI, onSets: true},
	{keyword: "promisc", arm: linkArmFlag, flag: unix.IFF_PROMISC, onSets: true},
	{keyword: "trailers", arm: linkArmFlag, flag: unix.IFF_NOTRAILERS},
	{keyword: "arp", arm: linkArmFlag, flag: unix.IFF_NOARP},
	{keyword: "carrier", arm: linkArmCarrier},
	{keyword: "vf", arm: linkArmUnsupported},
	{keyword: "master", prefix: true, arm: linkArmMaster},
	{keyword: vrfKeywordCst, arm: linkArmVrf},
	{keyword: "nomaster", prefix: true, arm: linkArmNomaster},
	{keyword: "dynamic", prefix: true, arm: linkArmFlag, flag: unix.IFF_DYNAMIC, onSets: true},
	{keyword: linkTypeKeywordCst, prefix: true, arm: linkArmType},
	{keyword: "alias", prefix: true, arm: linkArmAlias},
	{keyword: linkGroupKeywordCst, arm: linkArmGroup},
	{keyword: "mode", arm: linkArmLinkMode},
	{keyword: "state", arm: linkArmOperstate},
	{keyword: numTxQueuesKeywordCst, prefix: true, arm: linkArmTxQueues},
	{keyword: numRxQueuesKeywordCst, prefix: true, arm: linkArmRxQueues},
	{keyword: "addrgenmode", prefix: true, arm: linkArmAddrGenMode},
	{keyword: "link-netns", prefix: true, arm: linkArmUnsupported},
	{keyword: "link-netnsid", prefix: true, arm: linkArmUnsupported},
	{keyword: linkProtodownKeywordCst, arm: linkArmProtodown},
	{keyword: "protodown_reason", arm: linkArmProtodownReason},
	{keyword: "gso_max_size", arm: linkArmU32Max, attr: unix.IFLA_GSO_MAX_SIZE, maxVal: math.MaxUint32},
	{keyword: "gso_max_segs", arm: linkArmU32Max, attr: unix.IFLA_GSO_MAX_SEGS, maxVal: linkGsoMaxSegsCst},
	{keyword: "gro_max_size", arm: linkArmU32Max, attr: unix.IFLA_GRO_MAX_SIZE, maxVal: math.MaxUint32},
	{keyword: "gso_ipv4_max_size", arm: linkArmU32Max, attr: unix.IFLA_GSO_IPV4_MAX_SIZE, maxVal: math.MaxUint32},
	{keyword: "gro_ipv4_max_size", arm: linkArmU32Max, attr: unix.IFLA_GRO_IPV4_MAX_SIZE, maxVal: math.MaxUint32},
	{keyword: "parentdev", arm: linkArmUnsupported},
}

// The queue keywords, matches() in iplink_parse.
const (
	numTxQueuesKeywordCst = "numtxqueues"
	numRxQueuesKeywordCst = "numrxqueues"
)

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

// ErrLinkUsage is a refusal iplink_parse ends with exit(-1), 255: invarg,
// duparg, duparg2 and incomplete_command. Its returned refusals (a bad
// lladdr, an on/off word, nodev) exit 1, as ErrInvalidArgument does.
var ErrLinkUsage = errors.New("link")

// linkUsage marks err as one of ErrLinkUsage's refusals.
func linkUsage(err error) error {
	return fmt.Errorf("%w: %w", ErrLinkUsage, err)
}

func incompleteErr() error {
	return fmt.Errorf("%w: Command line is not complete. Try option \"help\"", ErrInvalidArgument)
}

func nodevErr(dev string) error {
	return fmt.Errorf("%w: Cannot find device %q", ErrInvalidArgument, dev)
}

// nodevExitErr is a kind's exit(nodev(dev)): nodev's -1 as the status, 255.
func nodevExitErr(dev string) error {
	return linkUsage(nodevErr(dev))
}

// kindUnknownErr is a kind's parse_opt on a word it doesn't know, which it
// prints before the usage and returns -1 from.
func kindUnknownErr(kind, word string) error {
	return fmt.Errorf("%w: %s: unknown command %q?", ErrInvalidArgument, kind, word)
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
	isVRF   vrfChecker
	// family and fetch are preferred_family and the get by index that
	// nl_get_ll_addr_len sends for a set's address.
	family uint8
	fetch  linkDataFetcher
	// devIndex is dev's index once a lookup found it: ll_name_to_index
	// caches a hit, so finish doesn't look it up again. A miss isn't kept.
	devIndex int32
	// groupSet is iplink_parse's group != -1.
	groupSet bool
	out      linkParsed
	name     string
	dev      string
	// link is the `link` argument, the last one given.
	link    string
	devSt   devState
	mtu     int
	qlen    int
	txq     int
	rxq     int
	addrLen int
}

// parseLinkArgs is iplink_parse for the keywords goip implements. create
// is NLM_F_CREATE in the request, which decides how the device is
// addressed: by name on add and replace, by ifindex otherwise.
func parseLinkArgs(args []string, create bool, family uint8, resolve linkResolver, isVRF vrfChecker,
	fetch linkDataFetcher) (linkParsed, error) {
	st := linkParseState{create: create, resolve: resolve, isVRF: isVRF, family: family, fetch: fetch,
		mtu: -1, qlen: -1, txq: -1, rxq: -1}
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
		if kw.arm == linkArmInet {
			var err error
			if i, err = st.inet(args, i); err != nil {
				return linkParsed{}, err
			}
			continue
		}
		if kw.arm == linkArmXdp {
			var err error
			if i, err = st.xdp(kw, args, i); err != nil {
				return linkParsed{}, err
			}
			continue
		}
		if kw.arm == linkArmProtodownReason {
			var err error
			if i, err = st.protodownReason(args, i); err != nil {
				return linkParsed{}, err
			}
			continue
		}
		// Every remaining arm is NEXT_ARG.
		if i+1 >= len(args) {
			return linkParsed{}, linkUsage(incompleteErr())
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
			return i, linkUsage(incompleteErr())
		}
		i++
	}
	arg := args[i]
	if st.devSt == devExplicit {
		return i, linkUsage(fmt.Errorf("%w: Error: either \"dev\" is duplicate, or %q is garbage", ErrInvalidArgument, arg))
	}
	if !checkAltIfname(arg) {
		return i, linkUsage(invargErr("\"dev\" not a valid ifname", arg))
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
			return linkUsage(dupargErr("name", v))
		}
		if !checkIfname(v) {
			return linkUsage(invargErr("\"name\" not a valid ifname", v))
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
	case linkArmTxQueues:
		return st.intAttr(&st.txq, numTxQueuesKeywordCst, unix.IFLA_NUM_TX_QUEUES, v)
	case linkArmRxQueues:
		return st.intAttr(&st.rxq, numRxQueuesKeywordCst, unix.IFLA_NUM_RX_QUEUES, v)
	case linkArmVrf:
		return st.vrf(v)
	case linkArmGroup:
		if st.groupSet {
			return linkUsage(dupargErr(linkGroupKeywordCst, v))
		}
		g, ok := rtnlGroupA2N(v)
		if !ok {
			return linkUsage(invargErr("Invalid \"group\" value\n", v))
		}
		st.groupSet = true
		st.out.attrs = append(st.out.attrs, xtcpnl.LinkAttrU32(unix.IFLA_GROUP, g))
	case linkArmU32Max:
		return st.u32Max(kw, v)
	case linkArmAddrGenMode:
		return st.addrGenMode(v)
	case linkArmProtodown, linkArmCarrier:
		return st.onOffU8(kw.keyword, v)
	case linkArmBroadcast, linkArmLinkMode, linkArmOperstate:
		return st.linkState(kw.arm, v)
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
			return linkUsage(invargErr("Device does not exist\n", v))
		}
		st.out.attrs = append(st.out.attrs, xtcpnl.LinkAttrU32(unix.IFLA_MASTER, cint.Bits(idx)))
	case linkArmLink:
		st.link = v
	case linkArmAlias:
		if len(v) >= ifAliasSizeCst {
			return linkUsage(invargErr("alias too long\n", v))
		}
		st.out.attrs = append(st.out.attrs, xtcpnl.LinkAttrBytes(unix.IFLA_IFALIAS, []byte(v)))
	default:
		return fmt.Errorf("goip: link keyword %q has no value reader", kw.keyword)
	}
	return nil
}

// linkGroupKeywordCst is iplink_parse's `group`, compared with strcmp.
const linkGroupKeywordCst = "group"

// linkGsoMaxSegsCst is ip/iplink.c's GSO_MAX_SEGS, gso_max_segs' bound.
const linkGsoMaxSegsCst = 65535

// rtnlGroupA2N is rtnl_group_a2n (lib/rt_names.c:713): the stock group
// file's one name, default, then strtol in base 0 over the whole argument
// into an int, refused when negative. The int keeps the long's low 32 bits,
// so 2147483648 is refused and 4294967296 is 0.
func rtnlGroupA2N(arg string) (uint32, bool) {
	if arg == addrNameDefaultCst {
		return 0, true
	}
	l, end := cStrtol(arg, 0)
	if end == 0 || end != len(arg) {
		return 0, false
	}
	i := cint.LongToInt(l)
	if i < 0 {
		return 0, false
	}
	return cint.Bits(i), true
}

// vrf is `vrf NAME`: ll_name_to_index, then name_is_vrf, either failing
// as invarg; it sends IFLA_MASTER, as `master` does.
func (st *linkParseState) vrf(v string) error {
	idx, err := st.resolve(v)
	if err != nil {
		return err
	}
	if idx == 0 {
		return linkUsage(invargErr("Not a valid VRF name\n", v))
	}
	ok, err := st.isVRF(v)
	if err != nil {
		return err
	}
	if !ok {
		return linkUsage(invargErr("Not a valid VRF name\n", v))
	}
	st.out.attrs = append(st.out.attrs, xtcpnl.LinkAttrU32(unix.IFLA_MASTER, cint.Bits(idx)))
	return nil
}

// intAttr is the mtu, txqueuelen and queue-count arms: get_integer base 0, a duplicate
// test against the -1 sentinel (so `mtu -1 mtu 5` is not a duplicate, as
// in `ip`), and the C int written as four bytes.
func (st *linkParseState) intAttr(seen *int, key string, atype uint16, v string) error {
	if *seen != -1 {
		return linkUsage(dupargErr(key, v))
	}
	n, ok := getInteger(v, 0)
	if !ok {
		return linkUsage(invargErr("Invalid \""+key+"\" value\n", v))
	}
	*seen = n
	st.out.attrs = append(st.out.attrs, xtcpnl.LinkAttrU32(atype, cIntBits(n)))
	return nil
}

// inetKeywordCst is iplink_parse's `inet`, compared with strcmp.
const inetKeywordCst = "inet"

// inet is iplink_parse_inet (ip/iplink.c:572-860, iproute2 7.2.0): the
// devconf params after `inet`, each compared with strcmp, up to the first
// word that is none, which PREV_ARG hands back to iplink_parse. It returns
// the index of the last argument it consumed, and appends IFLA_AF_SPEC
// where `inet` fell. There is no repeat check: a param twice sends two
// attributes, and `inet` twice two IFLA_AF_SPECs.
func (st *linkParseState) inet(args []string, i int) (int, error) {
	var settings []xtcpnl.InetDevconfSetting
	for i+1 < len(args) {
		p := lookupInetParam(args[i+1])
		if p == nil {
			break
		}
		i += 2
		if i >= len(args) {
			return i, linkUsage(incompleteErr())
		}
		v, err := inetValue(p, args[i])
		if err != nil {
			return i, err
		}
		settings = append(settings, xtcpnl.InetDevconfSetting{ID: p.ID, Value: v})
	}
	if len(settings) == 0 {
		return i, linkUsage(incompleteErr())
	}
	a, err := xtcpnl.LinkAttrInetDevconf(settings)
	if err != nil {
		return i, err
	}
	st.out.attrs = append(st.out.attrs, a)
	return i, nil
}

// u32Max is a linkArmU32Max: get_unsigned in base 0, at most kw.maxVal.
func (st *linkParseState) u32Max(kw *linkKeyword, v string) error {
	n, ok := cGetUnsigned(v, 0, kw.maxVal)
	if !ok {
		return linkUsage(invargErr("Invalid \""+kw.keyword+"\" value\n", v))
	}
	u, ok := cint.U32From64(n)
	if !ok {
		return linkUsage(invargErr("Invalid \""+kw.keyword+"\" value\n", v))
	}
	st.out.attrs = append(st.out.attrs, xtcpnl.LinkAttrU32(kw.attr, u))
	return nil
}

// addrGenMode is the addrgenmode arm: the name, then the two nests.
func (st *linkParseState) addrGenMode(v string) error {
	mode, ok := addrGenModeA2N(v)
	if !ok {
		return linkUsage(invargErr("Invalid address generation mode\n", v))
	}
	a, err := xtcpnl.LinkAttrAddrGenMode(mode)
	if err != nil {
		return err
	}
	st.out.attrs = append(st.out.attrs, a)
	return nil
}

// onOffU8 is the protodown and carrier arms: parse_on_off, then
// IFLA_PROTO_DOWN or IFLA_CARRIER as a u8.
func (st *linkParseState) onOffU8(key, v string) error {
	on, err := parseOnOff(key, v)
	if err != nil {
		return err
	}
	atype := uint16(unix.IFLA_CARRIER)
	if key == linkProtodownKeywordCst {
		atype = unix.IFLA_PROTO_DOWN
	}
	var b uint8
	if on {
		b = 1
	}
	st.out.attrs = append(st.out.attrs, xtcpnl.LinkAttrU8(atype, b))
	return nil
}

// linkState is the broadcast, mode and state arms. broadcast is address's
// ll_addr_a2n without the length check; mode and state are an index into
// print's name tables by strcasecmp, refused by invarg.
func (st *linkParseState) linkState(arm linkArm, v string) error {
	switch arm {
	case linkArmBroadcast:
		b, ok := llAddrA2N(v, linkLLAddrBufCst)
		if !ok {
			return fmt.Errorf("%w: %q is invalid lladdr", ErrInvalidArgument, v)
		}
		st.out.attrs = append(st.out.attrs, xtcpnl.LinkAttrBytes(unix.IFLA_BROADCAST, b))
	case linkArmLinkMode:
		m, ok := nameIndex(render.LinkModeNames(), v)
		if !ok {
			return linkUsage(invargErr("Invalid link mode\n", v))
		}
		st.out.attrs = append(st.out.attrs, xtcpnl.LinkAttrU8(unix.IFLA_LINKMODE, m))
	default: // linkArmOperstate
		o, ok := nameIndex(render.OperStateNames(), v)
		if !ok {
			return linkUsage(invargErr("Invalid operstate\n", v))
		}
		st.out.attrs = append(st.out.attrs, xtcpnl.LinkAttrU8(unix.IFLA_OPERSTATE, o))
	}
	return nil
}

// nameIndex is get_operstate's loop: the index of the first name equal to v
// by strcasecmp.
func nameIndex(names []string, v string) (uint8, bool) {
	for i, n := range names {
		if cStrcaseEq(v, n) {
			return cint.U8(i)
		}
	}
	return 0, false
}

// xdp is xdp_parse (ip/iplink_xdp.c:47) for the one form goip builds: off
// or none as argv's last word, xdp_delete's fd -1. Anything else is a
// program to load, which goip doesn't do. xdpoffload looks dev up first and
// a miss is incomplete_command. -force is refused by goip's options, so
// XDP_FLAGS_UPDATE_IF_NOEXIST is always set. It returns the index of the
// off word.
func (st *linkParseState) xdp(kw *linkKeyword, args []string, i int) (int, error) {
	if i+1 >= len(args) {
		return i, linkUsage(incompleteErr())
	}
	i++
	if kw.xdpMode == unix.XDP_FLAGS_HW_MODE {
		idx := int32(0)
		if st.dev != "" {
			var err error
			if idx, err = st.resolve(st.dev); err != nil {
				return i, err
			}
		}
		if idx == 0 {
			return i, linkUsage(incompleteErr())
		}
		st.devIndex = idx
	}
	if i != len(args)-1 || (args[i] != noneKeywordCst && args[i] != offKeywordCst) {
		return i, fmt.Errorf("link keyword %q: an XDP program: %w", kw.keyword, ErrNotImplemented)
	}
	a, err := xtcpnl.LinkAttrXdp(-1, unix.XDP_FLAGS_UPDATE_IF_NOEXIST|kw.xdpMode)
	if err != nil {
		return i, err
	}
	st.out.attrs = append(st.out.attrs, a)
	return i, nil
}

// protodownReasonBitsCst is PROTODOWN_REASON_NUM_BITS: a reason is a bit
// of the u32 mask.
const protodownReasonBitsCst = 32

// protodownReason is the protodown_reason arm: two NEXT_ARGs, the reason
// between them, so a bad reason is refused before a missing on/off. The
// reason is protodown_reason_a2n's strtoul in base 0; goip reads no
// protodown_reasons.d, and iproute2 ships none. It returns the index of the
// on/off word.
func (st *linkParseState) protodownReason(args []string, i int) (int, error) {
	if i+1 >= len(args) {
		return i, linkUsage(incompleteErr())
	}
	i++
	r := cStrtoul(args[i], 0)
	if r.end == 0 || r.end != len(args[i]) || r.overflow || r.val >= protodownReasonBitsCst {
		return i, linkUsage(invargErr("invalid protodown reason\n", args[i]))
	}
	if i+1 >= len(args) {
		return i, linkUsage(incompleteErr())
	}
	i++
	mask := uint32(1) << r.val
	var value uint32
	// matches(): the word is a prefix of on or off, and "" is a prefix of
	// both, so it is on.
	switch v := args[i]; {
	case strings.HasPrefix(onKeywordCst, v):
		value = mask
	case strings.HasPrefix(offKeywordCst, v):
	default:
		return i, fmt.Errorf("%w: Error: argument of \"protodown_reason\" must be \"on\" or \"off\", not %q",
			ErrInvalidArgument, v)
	}
	a, err := xtcpnl.LinkAttrProtoDownReason(mask, value)
	if err != nil {
		return i, err
	}
	st.out.attrs = append(st.out.attrs, a)
	return i, nil
}

// addrGenModeA2N is get_addr_gen_mode: the four names by strcasecmp.
func addrGenModeA2N(v string) (uint8, bool) {
	switch {
	case cStrcaseEq(v, "eui64"):
		return xtcpnl.In6AddrGenModeEUI64, true
	case cStrcaseEq(v, noneKeywordCst):
		return xtcpnl.In6AddrGenModeNone, true
	case cStrcaseEq(v, "stable_secret"):
		return xtcpnl.In6AddrGenModeStablePrivacy, true
	case cStrcaseEq(v, "random"):
		return xtcpnl.In6AddrGenModeRandom, true
	}
	return 0, false
}

// lookupInetParam is iplink_parse_inet's strcmp chain.
func lookupInetParam(arg string) *xtcpnl.InetDevconfParam {
	for i := range xtcpnl.InetDevconfParams {
		if xtcpnl.InetDevconfParams[i].Name == arg {
			return &xtcpnl.InetDevconfParams[i]
		}
	}
	return nil
}

// inetValue reads a param's value: parse_on_off for an on/off param, whose
// error is returned (exit 1), and get_integer base 0 for a numeric one,
// whose error is invarg (exit 255). addattr32 sends the C int's 32 bits, so
// -1 is 0xffffffff.
func inetValue(p *xtcpnl.InetDevconfParam, v string) (uint32, error) {
	if p.OnOff {
		on, err := parseOnOff(p.Name, v)
		if on {
			return 1, err
		}
		return 0, err
	}
	n, ok := getInteger(v, 0)
	if !ok {
		return 0, linkUsage(invargErr("Invalid \""+p.Name+"\" value\n", v))
	}
	b, err := binary.Append(nil, binary.LittleEndian, int64(n))
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(b), nil
}

// parseOnOff is lib/utils.c's parse_on_off: off or on by strcmp, and its
// returned error otherwise (exit 1).
func parseOnOff(key, v string) (bool, error) {
	switch v {
	case offKeywordCst:
		return false, nil
	case onKeywordCst:
		return true, nil
	}
	return false, fmt.Errorf("%w: Error: argument of %q must be one of \"off\", \"on\", not %q",
		ErrInvalidArgument, key, v)
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
		if err := st.checkAddrLen(); err != nil {
			return err
		}
	}

	if st.groupSet && !haveDev {
		return st.groupWide()
	}

	if !st.create {
		if !haveDev {
			return linkUsage(fmt.Errorf("%w: Not enough information: \"dev\" argument is required", ErrInvalidArgument))
		}
		idx := st.devIndex
		if idx == 0 {
			var err error
			if idx, err = st.resolve(st.dev); err != nil {
				return err
			}
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
		return linkUsage(fmt.Errorf("%w: both \"name\" and \"dev\" cannot be used when creating devices", ErrInvalidArgument))
	}
	// The lower device, looked up as ll_name_to_index does; `link-netnsid`
	// is not implemented, so it's never looked up in another namespace.
	if st.link != "" {
		idx, err := st.resolve(st.link)
		if err != nil {
			return err
		}
		if idx <= 0 {
			return nodevErr(st.link)
		}
		st.out.attrs = append(st.out.attrs, xtcpnl.LinkAttrU32(unix.IFLA_LINK, uint32(idx)))
	}
	// `index N` is not implemented, so ifi_index stays 0.
	if haveDev {
		st.out.attrs = append(st.out.attrs, xtcpnl.LinkAttrString(unix.IFLA_IFNAME, st.dev))
	}
	return nil
}

// groupWide is iplink_parse's `group` with no dev (ip/iplink.c:1299): a set
// of every device in the group, sent with ifi_index 0 and IFLA_GROUP. Words
// after a type are refused, and so is a create; a type with none is dropped.
// Both refusals are exit(-1).
func (st *linkParseState) groupWide() error {
	if len(st.out.rest) > 0 {
		return linkUsage(fmt.Errorf("%w: Garbage instead of arguments \"%s ...\". Try \"ip link help\"",
			ErrInvalidArgument, st.out.rest[0]))
	}
	if st.create {
		return linkUsage(fmt.Errorf("%w: group cannot be used when creating devices", ErrInvalidArgument))
	}
	st.out.kind, st.out.hasKind = "", false
	return nil
}

// checkAddrLen is iplink_parse's address-length check on a set
// (ip/iplink.c:1279), through nl_get_ll_addr_len: the dev's index, the get
// by index, and the reply's IFLA_ADDRESS length. No index, a refused get or
// a reply with no address is -1 there, which skips the check.
func (st *linkParseState) checkAddrLen() error {
	idx, err := st.resolve(st.dev)
	if err != nil || idx == 0 {
		return err
	}
	st.devIndex = idx
	d, err := st.fetch(st.family, idx)
	var errno syscall.Errno
	switch {
	case errors.As(err, &errno):
		return nil
	case err != nil:
		return err
	case !d.HasAddress || d.AddrLen == st.addrLen:
		return nil
	}
	return fmt.Errorf("%w: Invalid address length %d - must be %d bytes", ErrInvalidArgument, st.addrLen, d.AddrLen)
}

// Kinds iplink_modify treats specially.
const (
	kindBondCst      = "bond"
	kindBondSlaveCst = "bond_slave"
	kindVethCst      = "veth"
	kindVxcanCst     = "vxcan"
	kindDummyCst     = "dummy"
	kindIfbCst       = "ifb"
	kindNlmonCst     = "nlmon"
	kindVcanCst      = "vcan"
	kindTeamCst      = "team"
	kindTeamSlaveCst = "team_slave"
	kindVrfSlaveCst  = "vrf_slave"
	vethPeerCst      = "peer"
)

// buildLinkWrite is iplink_modify up to the send: parse, then the kind and
// its options. fetch is the tunnel parsers' prefetch.
func buildLinkWrite(errOut io.Writer, verb *linkWriteVerb, family uint8, args []string, resolve linkResolver,
	isVRF vrfChecker, fetch linkDataFetcher,
) (xtcpnl.LinkWrite, error) {
	p, err := parseLinkArgs(args, verb.create(), family, resolve, isVRF, fetch)
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
	env := &kindEnv{errOut: errOut, verb: verb, family: family, index: p.index, resolve: resolve, isVRF: isVRF, fetch: fetch}
	w.Data, err = kindData(env, p.kind, p.rest)
	if err != nil {
		return xtcpnl.LinkWrite{}, err
	}
	w.HasData = true
	return w, nil
}

// kindData is lu->parse_opt for the kinds goip implements, run only when
// arguments follow the kind. env.errOut takes the warnings a parser prints
// and goes on from; env.family is preferred_family, ip's -4 or -6.
func kindData(env *kindEnv, kind string, rest []string) ([]byte, error) {
	errOut, verb, resolve := env.errOut, env.verb, env.resolve
	switch kind {
	case kindVxlanCst, kindGeneveCst, kindBareudpCst, kindAmtCst, kindGtpCst:
		return overlayData(errOut, verb.setOp(), env.family, kind, rest, resolve)
	case kindGreCst, kindGretapCst, kindErspanCst:
		return greData(env, kind, rest)
	case kindIP6GreCst, kindIP6GretapCst, kindIP6ErspanCst:
		return gre6Data(env, kind, rest)
	case kindIpipCst, kindSitCst:
		return iptnlData(env, kind, rest)
	case kindIp6tnlCst:
		return ip6tnlData(env, rest)
	case kindVtiCst, kindVti6Cst:
		return vtiData(env, kind, rest)
	case kindXfrmCst:
		return xfrmData(env, rest)
	case kindBridgeCst:
		settings, err := parseBridgeArgs(errOut, rest)
		if err != nil {
			return nil, err
		}
		return xtcpnl.EncodeBridgeInfoData(settings)
	case kindBridgeSlaveCst:
		settings, err := parseBridgeSlaveArgs(rest, resolve)
		if err != nil {
			return nil, err
		}
		return xtcpnl.EncodeBridgeSlaveData(settings)
	case kindVlanCst:
		settings, err := parseVlanArgs(rest)
		if err != nil {
			return nil, err
		}
		return xtcpnl.EncodeVlanInfoData(settings)
	case kindMacvlanCst, kindMacvtapCst:
		return macvlanData(errOut, kind, rest)
	case kindIpvlanCst, kindIpvtapCst:
		settings, err := parseIpvlanArgs(kind, rest)
		if err != nil {
			return nil, err
		}
		return xtcpnl.EncodeIpvlanInfoData(settings)
	case kindBondCst:
		settings, err := parseBondArgs(rest, func(name string) (uint32, error) {
			idx, err := resolve(name)
			return cint.Bits(idx), err
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
	case kindHsrCst:
		return hsrData(env, rest)
	case kindNetkitCst:
		return netkitData(env, rest)
	case kindVethCst, kindVxcanCst:
		return vethData(env, kind, rest)
	case kindVrfCst:
		settings, err := parseVrfArgs(rest)
		if err != nil {
			return nil, err
		}
		return xtcpnl.EncodeVrfInfoData(settings)
	case kindBatadvCst:
		settings, err := parseBatadvArgs(rest)
		if err != nil {
			return nil, err
		}
		return xtcpnl.EncodeBatadvInfoData(settings)
	case kindDummyCst, kindIfbCst, kindNlmonCst, kindVcanCst, kindTeamCst, kindTeamSlaveCst, kindVrfSlaveCst:
		// These kinds have no parse_opt (ip/iplink_dummy.c, ifb.c and
		// so on), so iplink_modify takes its else arm.
		if matchesPrefix(rest[0], bondHelpKeywordCst) {
			return nil, fmt.Errorf("%w: ip link help is not implemented by goip", ErrNotImplemented)
		}
		return nil, fmt.Errorf("%w: Garbage instead of arguments \"%s ...\". Try \"ip link help\"",
			ErrInvalidArgument, rest[0])
	default:
		return nil, fmt.Errorf("link type %q options: %w", kind, ErrNotImplemented)
	}
}

// overlayData is the UDP overlays' parse_opts (KG3). set is set_op.
func overlayData(errOut io.Writer, set bool, family uint8, kind string, rest []string,
	resolve linkResolver,
) ([]byte, error) {
	switch kind {
	case kindGeneveCst:
		settings, err := parseGeneveArgs(set, rest)
		if err != nil {
			return nil, err
		}
		return xtcpnl.EncodeGeneveInfoData(settings, set)
	case kindBareudpCst:
		settings, err := parseBareudpArgs(rest)
		if err != nil {
			return nil, err
		}
		return xtcpnl.EncodeBareudpInfoData(settings)
	case kindAmtCst:
		settings, err := parseAmtArgs(rest, resolve)
		if err != nil {
			return nil, err
		}
		return xtcpnl.EncodeAmtInfoData(settings)
	case kindGtpCst:
		settings, err := parseGtpArgs(rest)
		if err != nil {
			return nil, err
		}
		return xtcpnl.EncodeGtpInfoData(settings)
	default:
		settings, err := parseVxlanArgs(errOut, family, set, rest, resolve)
		if err != nil {
			return nil, err
		}
		return xtcpnl.EncodeVxlanInfoData(settings, set)
	}
}

// vethData is veth_parse_opt, and vxcan_parse_opt, which is veth's with
// VXCAN_INFO_PEER and its own usage: `peer` and then a second
// iplink_parse, whose header fields go in the peer's own ifinfomsg while
// the outer ones are kept. Parsing into a fresh linkParsed is the save,
// zero and restore the C does around the shared request.
func vethData(env *kindEnv, kind string, rest []string) ([]byte, error) {
	if rest[0] != vethPeerCst {
		if kind == kindVxcanCst {
			// vxcan's print_usage writes to stdout; goip's refusals go
			// to stderr.
			return nil, tunnelUsageErr(vxcanUsageLine)
		}
		return nil, fmt.Errorf("%w: veth: expected \"peer\", not %q", ErrInvalidArgument, rest[0])
	}
	p, err := parseLinkArgs(rest[1:], env.verb.create(), env.family, env.resolve, env.isVRF, env.fetch)
	if err != nil {
		return nil, err
	}
	if p.hasKind {
		// duparg("type", argv[err]): veth's argv starts at "peer", so
		// index consumed is the peer's kind value.
		return nil, linkUsage(dupargErr(linkTypeKeywordCst, p.kind))
	}
	peer := xtcpnl.VethPeer{
		Index:    p.index,
		IfFlags:  p.ifFlags,
		IfChange: p.ifChange,
		Attrs:    p.attrs,
	}
	if kind == kindVxcanCst {
		return xtcpnl.EncodeVxcanInfoData(&peer)
	}
	return xtcpnl.EncodeVethPeer(peer)
}

// vxcanUsageLine is vxcan's print_usage's first line.
const vxcanUsageLine = "Usage: ip link <options> type vxcan [peer <options>]"

// linkWrite runs a write verb: resolve, build, and hand the request to
// GOIP_WRITE_OUT instead of the kernel.
func linkWrite(c *runCtx, verb *linkWriteVerb, args []string) error {
	svc := service.New(c.src, c.nextSeq)
	isVRF := func(name string) (bool, error) { return svc.IsVRF(c.family, name) }
	w, err := buildLinkWrite(c.errOut, verb, c.family, args, newLinkNameResolver(svc), isVRF, svc.LinkInfoDataByIndex)
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
			return cint.Int(llIdxA2N(name)), nil
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
	return cint.Low32(r.val)
}
