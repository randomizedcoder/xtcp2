package goip

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/randomizedcoder/xtcp2/internal/goip/model"
	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// `ip nexthop add`, `replace` and `delete` are ipnh_modify
// (ip/ipnexthop.c:1054-1175): the argument loop over a struct nhmsg and its
// attributes, then one RTM_NEWNEXTHOP or RTM_DELNEXTHOP talked to the kernel
// with NLM_F_ACK. `dev` is ll_name_to_index, which with an empty cache sends
// ll_link_get's by-name RTM_GETLINK, so a row with a device sends that get
// ahead of the write (link-write/rtnl/nexthop_add_single). goip writes the
// request to GOIP_WRITE_OUT.
//
// `ip nexthop flush` is ipnh_flush (:188-235): with no arguments, a dump of
// the groups alone and then a dump of everything, each reply's NHA_ID deleted
// in its own RTM_DELNEXTHOP, NLM_F_REQUEST with rtnl_talk's NLM_F_ACK, on a
// second socket, rth_del, whose seq rtnl_open seeds from the clock. Groups go
// first so no member is deleted under one. goip writes the deletes to
// GOIP_WRITE_OUT after both dumps, and since nothing it writes is applied,
// its second dump still holds the groups the first one copied, so it skips an
// id it has copied, as the route and neigh flushes skip a copied route.

const (
	// nexthopModifyBufCst is ipnh_modify's `char buf[1024]` after the header.
	nexthopModifyBufCst = 1024
	// nexthopWeightMaxCst is the largest group weight add_nh_group_attr takes.
	nexthopWeightMaxCst = 65536
	// nexthopTimerMaxCst is UINT32_MAX / 100: a resilient group's timers are
	// seconds, sent as clock_t hundredths.
	nexthopTimerMaxCst = math.MaxUint32 / 100
	nexthopTimerHzCst  = 100
	nexthopAddVerbCst  = "add"
	nexthopDelVerbCst  = "delete"
	nexthopIDKeyCst    = "id"
	nexthopViaKeyCst   = "via"
	// nexthopGrpTypeMpathCst and nexthopGrpTypeResCst are read_nh_group_type's
	// names.
	nexthopGrpTypeMpathCst = "mpath"
	nexthopGrpTypeResCst   = "resilient"
)

// ErrNexthopFlushTerminated is a dump failing inside `nexthop flush`:
// ipnh_flush returns -2, which do_cmd negates, so 2.
var ErrNexthopFlushTerminated = errors.New("nexthop flush: dump terminated")

// nexthopWriteVerb is one of do_ipnh's three ipnh_modify calls: the verb
// matches() takes, the type and the flags ORed into NLM_F_REQUEST.
type nexthopWriteVerb struct {
	name    string
	msgType uint16
	flags   uint16
}

// nexthopWriteVerbs is do_ipnh's order (:1456-1463), which decides an
// abbreviation: `a` is add, `r` replace, `d` delete.
var nexthopWriteVerbs = []nexthopWriteVerb{
	{nexthopAddVerbCst, unix.RTM_NEWNEXTHOP, unix.NLM_F_CREATE | unix.NLM_F_EXCL},
	{addrVerbReplaceCst, unix.RTM_NEWNEXTHOP, unix.NLM_F_CREATE | unix.NLM_F_REPLACE},
	{nexthopDelVerbCst, unix.RTM_DELNEXTHOP, 0},
}

// lookupNexthopWriteVerb is the verb arg names, or nil.
func lookupNexthopWriteVerb(arg string) *nexthopWriteVerb {
	for i := range nexthopWriteVerbs {
		if matchesPrefix(arg, nexthopWriteVerbs[i].name) {
			return &nexthopWriteVerbs[i]
		}
	}
	return nil
}

// nexthopWrite is ipnh_modify's request: the nhmsg fields and the attributes
// in the order the loop adds them.
type nexthopWrite struct {
	family   uint8
	protocol uint8
	flags    uint32
	attrs    xtcpnl.AttrBuilder
	resolve  linkResolver
}

// nhm is struct nhmsg: family, scope, protocol, a reserved byte and flags.
func (w *nexthopWrite) nhm() []byte {
	b := make([]byte, xtcpnl.NhMsgSizeCst)
	b[0], b[2] = w.family, w.protocol
	binary.LittleEndian.PutUint32(b[4:8], w.flags)
	return b
}

// nexthopModify runs `ip nexthop add`, `replace` or `delete`: the argument
// loop, which sends the device lookups, then the request to GOIP_WRITE_OUT.
func nexthopModify(c *runCtx, verb *nexthopWriteVerb, args []string) error {
	svc := service.New(c.src, c.nextSeq)
	w, err := parseNexthopWriteArgs(c.family, newLinkNameResolver(svc), args)
	if err != nil {
		return err
	}
	b, err := xtcpnl.BuildWriteRequest(verb.msgType, verb.flags|unix.NLM_F_ACK, c.nextSeq(), w.nhm(), w.attrs.Bytes())
	if err != nil {
		return fmt.Errorf("goip: build nexthop request: %w", err)
	}
	return writeAddrOut(b)
}

// parseNexthopWriteArgs is ipnh_modify's argument loop. family is
// preferred_family, which `dev`, `blackhole` and `via` set when it is unset.
// No argument at all is a request with none, as `ip` sends it.
func parseNexthopWriteArgs(family uint8, resolve linkResolver, args []string) (*nexthopWrite, error) {
	var raw [nexthopModifyBufCst]byte
	w := &nexthopWrite{family: family, attrs: xtcpnl.NewAttrBuilder(raw[:]), resolve: resolve}
	for i := 0; i < len(args); i++ {
		var err error
		if i, err = w.parseArg(args, i); err != nil {
			return nil, err
		}
	}
	return w, nil
}

// parseArg is one turn of the loop, in its order; only `protocol` is matched
// with matches(), the rest with strcmp. It returns the index of the last
// argument it used.
func (w *nexthopWrite) parseArg(args []string, i int) (int, error) {
	switch a := args[i]; {
	case a == nexthopIDKeyCst:
		return w.u32Arg(args, i, xtcpnl.NhaID, "invalid id value")
	case a == devKeywordCst:
		return w.dev(args, i)
	case a == nexthopViaKeyCst:
		return w.via(args, i)
	case a == "encap", a == "dst_port", a == "hw_stats":
		return i, fmt.Errorf("nexthop %s: no capture shows it: %w", a, ErrNotImplemented)
	case a == "blackhole":
		w.familyIfUnset(unix.AF_INET)
		return i, w.attrs.PutBytes(xtcpnl.NhaBlackhole, nil)
	case a == "fdb":
		return i, w.attrs.PutBytes(xtcpnl.NhaFdb, nil)
	case a == "onlink":
		w.flags |= unix.RTNH_F_ONLINK
		return i, nil
	case a == groupKeywordCst:
		return w.group(args, i)
	case a == linkXstatsTypeCst:
		return w.groupType(args, i)
	case matchesPrefix(a, "protocol"):
		return w.protocolArg(args, i)
	case a == "help":
		return i, fmt.Errorf("nexthop help: %w", ErrNotImplemented)
	}
	return i, invargErr("", args[i])
}

func (w *nexthopWrite) familyIfUnset(f uint8) {
	if w.family == unix.AF_UNSPEC {
		w.family = f
	}
}

// u32Arg is get_unsigned base 0 into a u32 attribute, ipnh_parse_id's
// refusal its message.
func (w *nexthopWrite) u32Arg(args []string, i int, typ uint16, msg string) (int, error) {
	i, err := nextArg(args, i)
	if err != nil {
		return i, err
	}
	v, ok := getU32(args[i], 0)
	if !ok {
		return i, invargErr(msg, args[i])
	}
	return i, w.attrs.PutU32(typ, v)
}

// dev is ll_name_to_index into NHA_OIF, which also makes an unset family
// AF_INET.
func (w *nexthopWrite) dev(args []string, i int) (int, error) {
	i, err := nextArg(args, i)
	if err != nil {
		return i, err
	}
	idx, err := w.resolve(args[i])
	if err != nil {
		return i, err
	}
	if idx == 0 {
		return i, invargErr("Device does not exist\n", args[i])
	}
	if err = w.attrs.PutU32(xtcpnl.NhaOIF, uint32(idx)); err != nil {
		return i, err
	}
	w.familyIfUnset(unix.AF_INET)
	return i, nil
}

// via is the gateway: an optional family word, then get_addr in that family
// or the request's. The address's family becomes the request's if it is
// unset, and must equal it if not.
func (w *nexthopWrite) via(args []string, i int) (int, error) {
	i, err := nextArg(args, i)
	if err != nil {
		return i, err
	}
	family := readFamily(args[i])
	if family == unix.AF_UNSPEC {
		family = w.family
	} else if i, err = nextArg(args, i); err != nil {
		return i, err
	}
	addr, err := routeGetAddr(args[i], family)
	if err != nil {
		return i, err
	}
	if w.family == unix.AF_UNSPEC {
		w.family = addr.family
	} else if w.family != addr.family {
		return i, invargErr("address family mismatch\n", args[i])
	}
	return i, w.attrs.PutBytes(xtcpnl.NhaGateway, addr.data)
}

// group is add_nh_group_attr: ID[,WEIGHT] entries split on '/', each a
// struct nexthop_grp of the id, the weight less one in its low and high
// bytes, and two reserved bytes.
func (w *nexthopWrite) group(args []string, i int) (int, error) {
	i, err := nextArg(args, i)
	if err != nil {
		return i, err
	}
	bad := invargErr("\"group\" value is invalid\n", args[i])
	if args[i] == "" {
		return i, bad
	}
	parts := strings.Split(args[i], "/")
	grps := make([]byte, 0, len(parts)*xtcpnl.NexthopGrpSizeCst)
	for _, p := range parts {
		idArg, weightArg, weighted := strings.Cut(p, ",")
		id, ok := getU32(idArg, 0)
		if !ok {
			return i, bad
		}
		var weight uint32
		if weighted {
			wt, wok := getU32(weightArg, 0)
			if !wok || wt == 0 || wt > nexthopWeightMaxCst {
				return i, invargErr("\"weight\" is invalid\n", weightArg)
			}
			weight = wt - 1
		}
		grps = binary.LittleEndian.AppendUint32(grps, id)
		grps = append(grps, byte(weight), byte(weight>>8), 0, 0)
	}
	return i, w.attrs.PutBytes(xtcpnl.NhaGroup, grps)
}

// groupType is parse_nh_group_type: the type name, a resilient group's
// arguments, then NHA_GROUP_TYPE, after the nest they fill.
func (w *nexthopWrite) groupType(args []string, i int) (int, error) {
	i, err := nextArg(args, i)
	if err != nil {
		return i, err
	}
	var typ uint16
	switch args[i] {
	case nexthopGrpTypeMpathCst:
		typ = xtcpnl.NexthopGrpTypeMpath
	case nexthopGrpTypeResCst:
		typ = xtcpnl.NexthopGrpTypeRes
		if i, err = w.resilient(args, i); err != nil {
			return i, err
		}
	default:
		return i, invargErr("\"type\" value is invalid\n", args[i])
	}
	return i, w.attrs.PutU16(xtcpnl.NhaGroupType, typ)
}

// resilient is parse_nh_group_type_res. With nothing after `resilient` it
// adds nothing; otherwise it opens the NHA_RES_GROUP nest, takes the
// buckets and timers it knows and closes the nest at the first argument it
// does not, which the loop then reads, so that argument may leave the nest
// empty.
func (w *nexthopWrite) resilient(args []string, i int) (int, error) {
	if i+1 >= len(args) {
		return i, nil
	}
	nest, err := w.attrs.BeginNest(xtcpnl.NhaResGroup | unix.NLA_F_NESTED)
	if err != nil {
		return i, err
	}
	for i+1 < len(args) {
		var used bool
		if i, used, err = w.resilientArg(args, i+1); err != nil {
			return i, err
		}
		if !used {
			break
		}
	}
	return i, w.attrs.EndNest(nest)
}

// resilientArg is one turn of parse_nh_group_type_res's loop at args[i]. It
// returns used false, and i less one, for an argument that is not its own.
func (w *nexthopWrite) resilientArg(args []string, i int) (int, bool, error) {
	var typ uint16
	var msg string
	switch args[i] {
	case "buckets":
		i, err := nextArg(args, i)
		if err != nil {
			return i, true, err
		}
		b, ok := getU16(args[i], 0)
		if !ok {
			return i, true, invargErr("invalid buckets value", args[i])
		}
		return i, true, w.attrs.PutU16(xtcpnl.NhaResGroupBuckets, b)
	case "idle_timer":
		typ, msg = xtcpnl.NhaResGroupIdleTimer, "invalid idle timer value"
	case "unbalanced_timer":
		typ, msg = xtcpnl.NhaResGroupUnbalancedTimer, "invalid unbalanced timer value"
	default:
		return i - 1, false, nil
	}
	i, err := nextArg(args, i)
	if err != nil {
		return i, true, err
	}
	v, ok := getU32(args[i], 0)
	if !ok || v >= nexthopTimerMaxCst {
		return i, true, invargErr(msg, args[i])
	}
	return i, true, w.attrs.PutU32(typ, v*nexthopTimerHzCst)
}

// protocolArg is rtnl_rtprot_a2n into nh_protocol, its low byte.
func (w *nexthopWrite) protocolArg(args []string, i int) (int, error) {
	i, err := nextArg(args, i)
	if err != nil {
		return i, err
	}
	p, ok := rtnlNameA2N(routeProtoNames, args[i], math.MaxUint32)
	if !ok {
		return i, invargErr(`"protocol" value is invalid`+"\n", args[i])
	}
	w.protocol = binary.LittleEndian.AppendUint32(nil, p)[0]
	return i, nil
}

// nexthopFlush is ipnh_flush with no arguments, then `Flushed N nexthops` or
// `Nothing to flush`, which `ip` prints even when a dump fails. goip counts
// the deletes it writes, each of which `ip` would see acked.
//
// The selectors, `id` among them, and -s, which adds NHA_OP_FLAGS to the
// dumps, are refused: no capture shows them.
func nexthopFlush(c *runCtx, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("nexthop flush %q: %w", args[0], ErrNotImplemented)
	}
	if c.showStats > 0 {
		return fmt.Errorf("-s nexthop flush: %w", ErrNotImplemented)
	}
	svc := service.New(c.src, c.nextSeq)
	copied := map[uint32]bool{}
	var ids []uint32
	keep := func(nhs []model.Nexthop, err error) error {
		for _, nh := range nhs {
			if nh.ID != 0 && !copied[nh.ID] {
				copied[nh.ID] = true
				ids = append(ids, nh.ID)
			}
		}
		return err
	}
	dumpErr := keep(svc.NexthopsFiltered(c.family, xtcpnl.NexthopDumpFilter{Groups: true}))
	if dumpErr == nil {
		dumpErr = keep(svc.Nexthops(c.family))
	}
	var out []byte
	for _, id := range ids {
		del, err := nexthopDelRequest(c, id)
		if err != nil {
			return err
		}
		out = append(out, del...)
	}
	if len(out) > 0 {
		if err := writeAddrOut(out); err != nil {
			return err
		}
	}
	if err := nexthopFlushReport(c, len(ids)); err != nil {
		return err
	}
	if dumpErr != nil {
		return fmt.Errorf("%w: Dump terminated. Failed to flush nexthops: %w", ErrNexthopFlushTerminated, dumpErr)
	}
	return nil
}

// nexthopDelRequest is delete_nexthop: an AF_UNSPEC nhmsg and NHA_ID.
func nexthopDelRequest(c *runCtx, id uint32) ([]byte, error) {
	var raw [8]byte
	ab := xtcpnl.NewAttrBuilder(raw[:])
	if err := ab.PutU32(xtcpnl.NhaID, id); err != nil {
		return nil, err
	}
	b, err := xtcpnl.BuildWriteRequest(unix.RTM_DELNEXTHOP, unix.NLM_F_ACK, c.nextSeq(),
		make([]byte, xtcpnl.NhMsgSizeCst), ab.Bytes())
	if err != nil {
		return nil, fmt.Errorf("goip: build nexthop delete: %w", err)
	}
	return b, nil
}

// nexthopFlushReport is ipnh_flush's last line.
func nexthopFlushReport(c *runCtx, n int) error {
	var err error
	if n == 0 {
		_, err = fmt.Fprintln(c.out, "Nothing to flush")
	} else {
		_, err = fmt.Fprintf(c.out, "Flushed %d nexthops\n", n)
	}
	return err
}
