package goip

import (
	"encoding/json"
	"fmt"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// neighGetVerbCst is do_ipneigh's `get`, matched with matches() after the
// write verbs and before the listing (ip/ipneigh.c:756-757), so `g` is get.
const neighGetVerbCst = "get"

// neighGetState is ipneigh_get's locals (ip/ipneigh.c:675-679): the device,
// the destination and the flag `proxy` sets.
type neighGetState struct {
	dev    string
	devSet bool // d != NULL: `dev` was given, so it is resolved
	devOK  bool // dev_ok: `dev` or `proxy` was given
	dst    inetPrefix
	dstOK  bool
	ndmFlg uint8
}

// parseNeighGetArgs is ipneigh_get's argument loop and the check after it
// (ip/ipneigh.c:681-713). family is preferred_family.
//
// `dev` is strcmp and `proxy` matches(), so `p ADDR` is proxy and `d0` is a
// destination, as in `ip`. `dev` has no duparg and the last one wins. `proxy`
// takes the address and counts as the device too (:694), so `ip neigh get
// proxy ADDR` needs no `dev`; a second address, by either arm, is duparg.
func parseNeighGetArgs(family uint8, args []string) (neighGetState, error) {
	var st neighGetState
	for i := 0; i < len(args); i++ {
		var err error
		switch {
		case args[i] == devKeywordCst:
			if i, err = nextArg(args, i); err != nil {
				return neighGetState{}, err
			}
			st.dev, st.devSet, st.devOK = args[i], true, true
		case matchesPrefix(args[i], proxyKeywordCst):
			if i, err = nextArg(args, i); err != nil {
				return neighGetState{}, err
			}
			if err = st.address(args[i], family, func(v string) error { return dupargErr("address", v) }); err != nil {
				return neighGetState{}, err
			}
			st.devOK = true
			st.ndmFlg |= unix.NTF_PROXY
		default:
			if args[i] == routeToKeywordCst {
				if i, err = nextArg(args, i); err != nil {
					return neighGetState{}, err
				}
			}
			if err = st.address(args[i], family, func(v string) error { return duparg2Err(routeToKeywordCst, v) }); err != nil {
				return neighGetState{}, err
			}
		}
	}
	if !st.devOK || !st.dstOK || st.dst.family == unix.AF_UNSPEC {
		return neighGetState{}, fmt.Errorf("%w: Device and address are required arguments", ErrInvalidArgument)
	}
	return st, nil
}

// address is the destination half of the proxy arm and the final else: help,
// the duplicate test, then get_addr in preferred_family.
func (st *neighGetState) address(v string, family uint8, dup func(string) error) error {
	if matchesPrefix(v, routeHelpKeywordCst) {
		return fmt.Errorf("%w: ip neigh help is not implemented by goip", ErrNotImplemented)
	}
	if st.dstOK {
		return dup(v)
	}
	a, err := routeGetAddr(v, family)
	if err != nil {
		return err
	}
	st.dst, st.dstOK = a, true
	return nil
}

// neighGet is `ip neigh get` (ipneigh_get, ip/ipneigh.c:663-743): the device
// resolved by name, one RTM_GETNEIGH single get, and print_neigh over its
// reply.
//
// The traffic is the get by name and the get (link-write/rtnl/neigh_get).
// The by-name lookup caches the device, so print_neigh names it with no third
// request; a `proxy` get with no `dev` names the reply's device with a get by
// index. A missing entry is the kernel's ENOENT, a failed talk: exit 2 and
// nothing on stdout (neigh_get_missing).
//
// print_neigh runs after ipneigh_reset_filter(0), whose filter.state is ~0
// and whose index is 0 (:478-483), so the reply is printed whatever its state,
// with its `dev` token.
func neighGet(c *runCtx, args []string) error {
	st, err := parseNeighGetArgs(c.family, args)
	if err != nil {
		return err
	}
	g := xtcpnl.NeighGet{Family: st.dst.family, Flags: st.ndmFlg, Dst: st.dst.data}

	svc := service.New(c.src, c.nextSeq)
	if st.devSet {
		resolve := newLinkNameResolverRemember(svc, func(l xtcpnl.LinkInfo) {
			c.lltab.Fill([]xtcpnl.LinkInfo{l})
		})
		idx, rerr := resolve(st.dev)
		if rerr != nil {
			return rerr
		}
		if idx == 0 {
			return noSuchDevice(nodevErr(st.dev))
		}
		g.Ifindex = idx
	}

	n, err := svc.NeighGet(g)
	if err != nil {
		return err
	}
	// print_neigh's `dev` token is ll_index_to_name, a get by index on a
	// cache miss; a `dev` lookup has already cached the usual answer.
	resolveIndexName(c, svc, n.Ifindex)
	view := render.NeighViewOf(xtcpnl.NeighInfo(n), c.lltab, render.NeighShowFilter{Stats: c.stats()})
	if c.json {
		return json.NewEncoder(c.out).Encode([]render.NeighView{view})
	}
	_, werr := fmt.Fprint(c.out, view.Text())
	return werr
}
