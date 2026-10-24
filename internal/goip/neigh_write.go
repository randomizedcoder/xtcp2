package goip

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// `ip neigh add`, `change`, `replace` and `delete` are ipneigh_modify
// (ip/ipneigh.c:101-216): the argument loop, ll_init_map's link dump, `dev`
// resolved out of it, and one RTM_NEWNEIGH or RTM_DELNEIGH talked to the
// kernel with NLM_F_ACK at the link dump's seq plus one
// (link-write/rtnl/neigh_*). goip sends the link dump and writes the request
// to GOIP_WRITE_OUT, checked after the argv is parsed and `dev` resolved, as
// the address and route verbs check it.

// ndaProtocolCst is NDA_PROTOCOL, which x/sys/unix does not export: the enum
// runs NDA_SRC_VNI 11, NDA_PROTOCOL 12 (the kernel's uapi neighbor header).
const ndaProtocolCst = 12

// neighModifyBufCst is ipneigh_modify's `char buf[256]` after the ndmsg.
const neighModifyBufCst = 256

// neighWriteVerb is one of do_ipneigh's write arms (ip/ipneigh.c:747-755):
// the message type and the flags beyond NLM_F_REQUEST.
type neighWriteVerb struct {
	name    string
	msgType uint16
	flags   uint16
}

var (
	neighAddVerb     = neighWriteVerb{addrVerbAddCst, unix.RTM_NEWNEIGH, unix.NLM_F_CREATE | unix.NLM_F_EXCL}
	neighChangeVerb  = neighWriteVerb{addrVerbChangeCst, unix.RTM_NEWNEIGH, unix.NLM_F_REPLACE}
	neighReplaceVerb = neighWriteVerb{addrVerbReplaceCst, unix.RTM_NEWNEIGH, unix.NLM_F_CREATE | unix.NLM_F_REPLACE}
	neighDeleteVerb  = neighWriteVerb{addrVerbDeleteCst, unix.RTM_DELNEIGH, 0}
)

// lookupNeighWriteVerb is do_ipneigh's write arms in their order, each
// matches() but `chg`, which is strcmp.
func lookupNeighWriteVerb(arg string) *neighWriteVerb {
	switch {
	case matchesPrefix(arg, neighAddVerb.name):
		return &neighAddVerb
	case matchesPrefix(arg, neighChangeVerb.name), arg == addrVerbChgCst:
		return &neighChangeVerb
	case matchesPrefix(arg, neighReplaceVerb.name):
		return &neighReplaceVerb
	case matchesPrefix(arg, neighDeleteVerb.name):
		return &neighDeleteVerb
	}
	return nil
}

// neighWrite is ipneigh_modify's request and locals: the ndmsg, the
// attributes in the order the loop and the code after it add them, and the
// device to resolve.
type neighWrite struct {
	family  uint8
	ifindex int32
	state   uint16
	flags   uint8
	extFlg  uint32
	proto   []byte // NDA_PROTOCOL, added inside the loop, so ahead of NDA_DST
	dst     inetPrefix
	dstOK   bool
	devOK   bool
	dev     string
	devSet  bool
	lladdr  string
	llSet   bool
}

// nudStateA2N is nud_state_a2n (ip/ipneigh.c:61-88): permanent, reachable and
// failed by matches(), the rest by strcmp, then get_unsigned base 0, which
// must be under 0x100 and zero or a single bit.
func nudStateA2N(arg string) (uint16, bool) {
	switch {
	case matchesPrefix(arg, "permanent"):
		return unix.NUD_PERMANENT, true
	case matchesPrefix(arg, "reachable"):
		return unix.NUD_REACHABLE, true
	case arg == "noarp":
		return unix.NUD_NOARP, true
	case arg == noneKeywordCst:
		return unix.NUD_NONE, true
	case arg == "stale":
		return unix.NUD_STALE, true
	case arg == "incomplete":
		return unix.NUD_INCOMPLETE, true
	case arg == "delay":
		return unix.NUD_DELAY, true
	case arg == "probe":
		return unix.NUD_PROBE, true
	case matchesPrefix(arg, "failed"):
		return unix.NUD_FAILED, true
	}
	v, ok := cGetUnsigned(arg, 0, math.MaxUint32)
	if !ok || v >= 0x100 || v&(v-1) != 0 {
		return 0, false
	}
	return uint16(v), true
}

// parseNeighWriteArgs is ipneigh_modify's argument loop and the check after
// it. family is preferred_family. `lladdr`, `proxy`, `extern_learn` and
// `protocol` are matches(), and `proxy` is tested first, so `pro` is proxy;
// the rest are strcmp. `dev` has no duparg and the last one wins.
func parseNeighWriteArgs(family uint8, args []string) (neighWrite, error) {
	w := neighWrite{family: family, state: unix.NUD_PERMANENT}
	for i := 0; i < len(args); i++ {
		var err error
		a := args[i]
		switch {
		case matchesPrefix(a, "lladdr"):
			if i, err = nextArg(args, i); err != nil {
				return neighWrite{}, err
			}
			if w.llSet {
				return neighWrite{}, dupargErr("lladdr", args[i])
			}
			w.lladdr, w.llSet = args[i], true
		case a == "nud":
			if i, err = nextArg(args, i); err != nil {
				return neighWrite{}, err
			}
			s, ok := nudStateA2N(args[i])
			if !ok {
				return neighWrite{}, invargErr("nud state is bad", args[i])
			}
			w.state = s
		case matchesPrefix(a, proxyKeywordCst):
			if i, err = nextArg(args, i); err != nil {
				return neighWrite{}, err
			}
			if matchesPrefix(args[i], routeHelpKeywordCst) {
				return neighWrite{}, fmt.Errorf("%w: ip neigh help is not implemented by goip", ErrNotImplemented)
			}
			if w.dstOK {
				return neighWrite{}, dupargErr("address", args[i])
			}
			if err = w.address(args[i], family); err != nil {
				return neighWrite{}, err
			}
			w.devOK = true
			w.flags |= unix.NTF_PROXY
		case a == "router":
			w.flags |= unix.NTF_ROUTER
		case a == "use":
			w.flags |= unix.NTF_USE
		case a == "managed":
			w.extFlg |= xtcpnl.NtfExtManaged
			w.state = unix.NUD_NONE
		case matchesPrefix(a, "extern_learn"):
			w.flags |= unix.NTF_EXT_LEARNED
		case a == "extern_valid":
			w.extFlg |= xtcpnl.NtfExtExtValidated
		case a == devKeywordCst:
			if i, err = nextArg(args, i); err != nil {
				return neighWrite{}, err
			}
			w.dev, w.devSet, w.devOK = args[i], true, true
		case matchesPrefix(a, "protocol"):
			if i, err = nextArg(args, i); err != nil {
				return neighWrite{}, err
			}
			p, ok := rtnlNameA2N(routeProtoNames, args[i], math.MaxUint32)
			if !ok {
				return neighWrite{}, invargErr(`"protocol" value is invalid`, args[i])
			}
			// addattr8 of the u32: its low byte (ip/ipneigh.c:164).
			w.proto = binary.LittleEndian.AppendUint32(nil, p)[:1]
		default:
			if i, err = w.destination(args, i, family); err != nil {
				return neighWrite{}, err
			}
		}
	}
	if !w.devOK || !w.dstOK || w.dst.family == unix.AF_UNSPEC {
		// iproute2 ends the line with a period; Go error strings do not.
		return neighWrite{}, fmt.Errorf("%w: Device and destination are required arguments", ErrInvalidArgument)
	}
	w.family = w.dst.family
	return w, nil
}

// destination is the loop's else arm: an optional `to`, then a `help` that
// matches() is skipped with NEXT_ARG and the argument after it taken as the
// address (ip/ipneigh.c:171-180), then duparg2 on a second address.
func (w *neighWrite) destination(args []string, i int, family uint8) (int, error) {
	var err error
	if args[i] == routeToKeywordCst {
		if i, err = nextArg(args, i); err != nil {
			return i, err
		}
	}
	if matchesPrefix(args[i], routeHelpKeywordCst) {
		if i, err = nextArg(args, i); err != nil {
			return i, err
		}
	}
	if w.dstOK {
		return i, duparg2Err(routeToKeywordCst, args[i])
	}
	return i, w.address(args[i], family)
}

func (w *neighWrite) address(v string, family uint8) error {
	a, err := routeGetAddr(v, family)
	if err != nil {
		return err
	}
	w.dst, w.dstOK = a, true
	return nil
}

// attrs is the attributes after the loop, in ipneigh_modify's order:
// NDA_PROTOCOL (added in the loop), NDA_DST, NDA_FLAGS_EXT when any extended
// flag is set, and NDA_LLADDR unless it is `null`.
func (w *neighWrite) attrs() ([]byte, error) {
	var raw [neighModifyBufCst]byte
	ab := xtcpnl.NewAttrBuilder(raw[:])
	if w.proto != nil {
		if err := ab.PutBytes(ndaProtocolCst, w.proto); err != nil {
			return nil, err
		}
	}
	if err := ab.PutBytes(unix.NDA_DST, w.dst.data); err != nil {
		return nil, err
	}
	if w.extFlg != 0 {
		if err := ab.PutU32(xtcpnl.NdaFlagsExt, w.extFlg); err != nil {
			return nil, err
		}
	}
	if w.llSet && w.lladdr != "null" {
		// ll_addr_a2n into `char llabuf[20]`.
		ll, ok := llAddrA2N(w.lladdr, 20)
		if !ok {
			return nil, fmt.Errorf("%w: %q is invalid lladdr", ErrInvalidArgument, w.lladdr)
		}
		if err := ab.PutBytes(unix.NDA_LLADDR, ll); err != nil {
			return nil, err
		}
	}
	return append([]byte(nil), ab.Bytes()...), nil
}

// ndmsg is struct ndmsg: family, two pad bytes, ifindex, state, flags and a
// zero ndm_type.
func (w *neighWrite) ndmsg() []byte {
	b := make([]byte, xtcpnl.NdMsgSizeCst)
	b[0] = w.family
	binary.LittleEndian.PutUint32(b[4:8], uint32(w.ifindex))
	binary.LittleEndian.PutUint16(b[8:10], w.state)
	b[xtcpnl.NdMsgFlagsOffCst] = w.flags
	return b
}

// neighModify runs a neigh write verb: parse, the attributes, ll_init_map,
// `dev`, then the request to GOIP_WRITE_OUT. ipneigh_modify builds the
// attributes before the link dump, so a bad lladdr sends nothing; a failed
// link dump is ll_init_map's Dump terminated, exit 1.
func neighModify(c *runCtx, verb *neighWriteVerb, args []string) error {
	w, err := parseNeighWriteArgs(c.family, args)
	if err != nil {
		return err
	}
	attrs, err := w.attrs()
	if err != nil {
		return err
	}

	svc := service.New(c.src, c.nextSeq)
	links, err := svc.NeighborLinks()
	if err != nil {
		return fmt.Errorf("%w: Dump terminated: %w", ErrDumpTerminated, err)
	}
	if w.devSet {
		decoded := make([]xtcpnl.LinkInfo, len(links))
		for i := range links {
			decoded[i] = xtcpnl.LinkInfo(links[i])
		}
		c.lltab.Fill(decoded)
		// ll_name_to_index out of the cache, as neighShow resolves it.
		idx := c.lltab.NameToIndex(w.dev)
		if idx == 0 {
			return noSuchDevice(nodevErr(w.dev))
		}
		w.ifindex = idx
	}

	b, err := xtcpnl.BuildWriteRequest(verb.msgType, verb.flags|unix.NLM_F_ACK, c.nextSeq(), w.ndmsg(), attrs)
	if err != nil {
		return fmt.Errorf("goip: build neigh %s: %w", verb.name, err)
	}
	return writeAddrOut(b)
}
