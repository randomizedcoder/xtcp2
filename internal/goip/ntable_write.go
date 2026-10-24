package goip

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// `ip ntable change` is ipntable_modify (ip/ipntable.c:51-306) after
// do_ipntable's ll_init_map (:685), which dumps the links before the verb is
// even read: the argument loop, then one RTM_SETNEIGHTBL with
// NLM_F_REPLACE|NLM_F_ACK. `name`, the thresholds and `gc_int` are top-level
// attributes in argument order; `dev` and the per-device parameters fill an
// NDTA_PARMS, not flagged nested, added last. `dev` is ll_name_to_index out
// of the dump's cache, so it sends nothing. goip writes the request to
// GOIP_WRITE_OUT.

const (
	ntableChangeVerbCst = "change"
	ntableChgVerbCst    = "chg"
	ntableNameKeyCst    = "name"
	ntableModifyBufCst  = 1024
	// ntableNoChangeCst is ipntable_modify's refusal of a request that
	// changes nothing.
	ntableNoChangeCst = "Not enough information: changeable attributes required"
)

// ntableArgKind is how ipntable_modify's loop reads a keyword's value.
type ntableArgKind uint8

const (
	ntableTopU32  ntableArgKind = iota // get_u32 into a top-level attribute
	ntableTopU64                       // get_u64 into a top-level attribute
	ntableParmU32                      // get_u32 into NDTA_PARMS
	ntableParmU64                      // get_u64 into NDTA_PARMS
)

// ntableArg is one keyword of the loop: its attribute and how it is read.
type ntableArg struct {
	attr uint16
	kind ntableArgKind
}

// ntableArgs is the loop's keywords but name and dev, each matched with
// strcmp.
var ntableArgs = map[string]ntableArg{
	"thresh1":        {xtcpnl.NdtaThresh1, ntableTopU32},
	"thresh2":        {xtcpnl.NdtaThresh2, ntableTopU32},
	"thresh3":        {xtcpnl.NdtaThresh3, ntableTopU32},
	"gc_int":         {xtcpnl.NdtaGcInterval, ntableTopU64},
	"base_reachable": {xtcpnl.NdtpaBaseReachableTime, ntableParmU64},
	"retrans":        {xtcpnl.NdtpaRetransTime, ntableParmU64},
	"gc_stale":       {xtcpnl.NdtpaGcStaletime, ntableParmU64},
	"delay_probe":    {xtcpnl.NdtpaDelayProbeTime, ntableParmU64},
	"queue":          {xtcpnl.NdtpaQueueLen, ntableParmU32},
	"app_probes":     {xtcpnl.NdtpaAppProbes, ntableParmU32},
	"ucast_probes":   {xtcpnl.NdtpaUcastProbes, ntableParmU32},
	"mcast_probes":   {xtcpnl.NdtpaMcastProbes, ntableParmU32},
	"mcast_reprobes": {xtcpnl.NdtpaMcastReprobes, ntableParmU32},
	"anycast_delay":  {xtcpnl.NdtpaAnycastDelay, ntableParmU64},
	"proxy_delay":    {xtcpnl.NdtpaProxyDelay, ntableParmU64},
	"proxy_queue":    {xtcpnl.NdtpaProxyQlen, ntableParmU32},
	"locktime":       {xtcpnl.NdtpaLocktime, ntableParmU64},
}

// ntableWrite is ipntable_modify's request as the loop builds it.
type ntableWrite struct {
	family  uint8
	named   bool
	changed bool
	top     xtcpnl.AttrBuilder
	parms   xtcpnl.AttrBuilder
}

// ntableChange runs `ip ntable change`: ll_init_map's link dump, the
// argument loop, then the request to GOIP_WRITE_OUT.
func ntableChange(c *runCtx, args []string) error {
	svc := service.New(c.src, c.nextSeq)
	links, err := svc.NeighTableLinks()
	if err != nil {
		return fmt.Errorf("%w: Dump terminated: %w", ErrDumpTerminated, err)
	}
	decoded := make([]xtcpnl.LinkInfo, len(links))
	for i := range links {
		decoded[i] = xtcpnl.LinkInfo(links[i])
	}
	c.lltab.Fill(decoded)
	w, err := parseNTableWriteArgs(c.family, c.lltab.NameToIndex, args)
	if err != nil {
		return err
	}
	attrs, err := w.attrs()
	if err != nil {
		return err
	}
	b, err := xtcpnl.BuildWriteRequest(unix.RTM_SETNEIGHTBL, unix.NLM_F_REPLACE|unix.NLM_F_ACK, c.nextSeq(),
		w.ndtmsg(), attrs)
	if err != nil {
		return fmt.Errorf("goip: build ntable request: %w", err)
	}
	return writeAddrOut(b)
}

// parseNTableWriteArgs is ipntable_modify's loop and the checks after it.
// index is ll_name_to_index over the link dump's cache.
func parseNTableWriteArgs(family uint8, index func(string) int32, args []string) (*ntableWrite, error) {
	var top, parms [ntableModifyBufCst]byte
	w := &ntableWrite{family: family, top: xtcpnl.NewAttrBuilder(top[:]), parms: xtcpnl.NewAttrBuilder(parms[:])}
	for i := 0; i < len(args); i++ {
		var err error
		if i, err = w.parseArg(args, i, index); err != nil {
			return nil, err
		}
	}
	if !w.named {
		return nil, fmt.Errorf("%w: Error: argument \"NAME\" is required", ErrInvalidArgument)
	}
	if !w.changed {
		return nil, fmt.Errorf("%w: %s", ErrInvalidArgument, ntableNoChangeCst)
	}
	return w, nil
}

// parseArg is one turn of the loop. It returns the index of the last
// argument it used.
func (w *ntableWrite) parseArg(args []string, i int, index func(string) int32) (int, error) {
	key := args[i]
	if key != ntableNameKeyCst && key != devKeywordCst {
		if _, ok := ntableArgs[key]; !ok {
			return i, invargErr("unknown", key)
		}
	}
	i, err := nextArg(args, i)
	if err != nil {
		return i, err
	}
	v := args[i]
	switch key {
	case ntableNameKeyCst:
		if w.named {
			return i, dupargErr("NAME", v)
		}
		w.named = true
		return i, w.top.PutString(xtcpnl.NdtaName, v)
	case devKeywordCst:
		idx := index(v)
		if idx == 0 {
			return i, noSuchDevice(nodevErr(v))
		}
		return i, w.parms.PutU32(xtcpnl.NdtpaIfindex, uint32(idx))
	}
	a := ntableArgs[key]
	refused := invargErr(fmt.Sprintf("%q value is invalid", key), v)
	w.changed = true
	switch a.kind {
	case ntableTopU32, ntableParmU32:
		n, ok := getU32(v, 0)
		if !ok {
			return i, refused
		}
		return i, w.builder(a.kind).PutU32(a.attr, n)
	default:
		n, ok := cGetUnsigned(v, 0, math.MaxUint64)
		if !ok {
			return i, refused
		}
		return i, w.builder(a.kind).PutBytes(a.attr, binary.NativeEndian.AppendUint64(nil, n))
	}
}

// builder is where a keyword's attribute goes: the request, or NDTA_PARMS.
func (w *ntableWrite) builder(k ntableArgKind) *xtcpnl.AttrBuilder {
	if k == ntableTopU32 || k == ntableTopU64 {
		return &w.top
	}
	return &w.parms
}

// ndtmsg is struct ndtmsg: the family and three pad bytes.
func (w *ntableWrite) ndtmsg() []byte {
	b := make([]byte, xtcpnl.NdtMsgSizeCst)
	b[0] = w.family
	return b
}

// attrs is the top-level attributes, then NDTA_PARMS when it holds any:
// addattr_l of the parms buffer's payload, so the type has no NLA_F_NESTED.
func (w *ntableWrite) attrs() ([]byte, error) {
	if w.parms.Len() == 0 {
		return w.top.Bytes(), nil
	}
	if err := w.top.PutBytes(xtcpnl.NdtaParms, w.parms.Bytes()); err != nil {
		return nil, err
	}
	return w.top.Bytes(), nil
}
