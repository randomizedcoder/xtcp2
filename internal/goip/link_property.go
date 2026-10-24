package goip

import (
	"encoding/binary"
	"fmt"

	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// `ip link property add|del` is iplink_prop and iplink_prop_mod
// (ip/iplink.c:1724-1796): the verb, then the argument loop, which opens an
// IFLA_PROP_LIST nest, flagged NLA_F_NESTED, and adds one IFLA_ALT_IFNAME per
// `altname` in argument order; then ll_name_to_index on `dev`, whose by-name
// RTM_GETLINK goes ahead of the write, and one RTM_NEWLINKPROP or
// RTM_DELLINKPROP under an ifinfomsg naming the ifindex. add carries
// NLM_F_EXCL|NLM_F_CREATE|NLM_F_APPEND, both rtnl_talk's NLM_F_ACK. goip
// writes the request to GOIP_WRITE_OUT.

const (
	linkPropertyVerbCst = "property"
	linkPropAddVerbCst  = "add"
	linkPropDelVerbCst  = "del"
	linkAltnameKeyCst   = "altname"
	// linkPropBufCst is struct iplink_req's buffer after the ifinfomsg.
	linkPropBufCst = 1024
	// linkPropNoDevCst is iplink_prop_mod's refusal of a request with no
	// device, "Not enough of" as ip spells it.
	linkPropNoDevCst = `Not enough of information: "dev" argument is required`
)

// linkPropWrite is iplink_prop_mod's request as the loop builds it.
type linkPropWrite struct {
	dev   string
	attrs xtcpnl.AttrBuilder
}

// linkProperty runs `ip link property`: the verb, the loop, the device
// lookup, then the request to GOIP_WRITE_OUT.
func linkProperty(c *runCtx, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("link property usage: %w", ErrNotImplemented)
	}
	var msgType, flags uint16
	switch verb := args[0]; {
	case matchesPrefix(verb, linkPropAddVerbCst):
		msgType = unix.RTM_NEWLINKPROP
		flags = unix.NLM_F_EXCL | unix.NLM_F_CREATE | unix.NLM_F_APPEND
	case matchesPrefix(verb, linkPropDelVerbCst):
		msgType = unix.RTM_DELLINKPROP
	case matchesPrefix(verb, linkHelpKeywordCst):
		return fmt.Errorf("link property help: %w", ErrNotImplemented)
	default:
		return fmt.Errorf("%w: Operator required", ErrInvalidArgument)
	}
	w, err := parseLinkPropArgs(args[1:])
	if err != nil {
		return err
	}
	idx, err := newLinkNameResolver(service.New(c.src, c.nextSeq))(w.dev)
	if err != nil {
		return err
	}
	if idx == 0 {
		return noSuchDevice(nodevErr(w.dev))
	}
	b, err := xtcpnl.BuildWriteRequest(msgType, flags|unix.NLM_F_ACK, c.nextSeq(),
		linkPropHeader(c.family, idx), w.attrs.Bytes())
	if err != nil {
		return fmt.Errorf("goip: build link property request: %w", err)
	}
	return writeAddrOut(b)
}

// parseLinkPropArgs is iplink_prop_mod's loop and its check for a device.
// `altname` and `help` are matched with matches(), in that order, so `a` is
// `altname`; any other word is the device, after a `dev` that strcmp
// matches, and is checked with check_altifname, not check_ifname.
func parseLinkPropArgs(args []string) (*linkPropWrite, error) {
	var raw [linkPropBufCst]byte
	w := &linkPropWrite{attrs: xtcpnl.NewAttrBuilder(raw[:])}
	nest, err := w.attrs.BeginNest(unix.IFLA_PROP_LIST | unix.NLA_F_NESTED)
	if err != nil {
		return nil, err
	}
	for i := 0; i < len(args); i++ {
		if i, err = w.parseArg(args, i); err != nil {
			return nil, err
		}
	}
	if err = w.attrs.EndNest(nest); err != nil {
		return nil, err
	}
	if w.dev == "" {
		return nil, fmt.Errorf("%w: %s", ErrInvalidArgument, linkPropNoDevCst)
	}
	return w, nil
}

// parseArg is one turn of the loop. It returns the index of the last
// argument it used.
func (w *linkPropWrite) parseArg(args []string, i int) (int, error) {
	switch a := args[i]; {
	case matchesPrefix(a, linkAltnameKeyCst):
		i, err := nextArg(args, i)
		if err != nil {
			return i, err
		}
		if !checkAltIfname(args[i]) {
			return i, invargErr("not a valid altname", args[i])
		}
		return i, w.attrs.PutString(unix.IFLA_ALT_IFNAME, args[i])
	case matchesPrefix(a, linkHelpKeywordCst):
		return i, fmt.Errorf("link property help: %w", ErrNotImplemented)
	case a == devKeywordCst:
		var err error
		if i, err = nextArg(args, i); err != nil {
			return i, err
		}
	}
	v := args[i]
	if w.dev != "" {
		return i, duparg2Err(devKeywordCst, v)
	}
	if !checkAltIfname(v) {
		return i, invargErr(`"dev" not a valid ifname`, v)
	}
	w.dev = v
	return i, nil
}

// linkPropHeader is the struct ifinfomsg: preferred_family and the
// ifindex, the rest zero.
func linkPropHeader(family uint8, idx int32) []byte {
	b := make([]byte, xtcpnl.IfInfomsgSizeCst)
	b[0] = family
	binary.LittleEndian.PutUint32(b[4:8], uint32(idx))
	return b
}
