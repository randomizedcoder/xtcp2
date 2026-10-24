package goip

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// `ip addrlabel add` and `delete` are ipaddrlabel_modify
// (ip/ipaddrlabel.c:128-187): the argument loop, then one RTM_NEWADDRLABEL
// or RTM_DELADDRLABEL talked to the kernel with NLM_F_ACK, add also
// NLM_F_CREATE|NLM_F_EXCL. `dev` is ll_name_to_index, whose by-name
// RTM_GETLINK goes ahead of the write. goip writes the request to
// GOIP_WRITE_OUT.
//
// `ip addrlabel flush` is ipaddrlabel_flush and flush_addrlabel (:190-242):
// the listing's dump, then each reply carrying IFAL_ADDRESS sent back as an
// RTM_DELADDRLABEL, NLM_F_REQUEST with rtnl_talk's NLM_F_ACK, from a socket
// of its own. The kernel's default labels carry the attribute, so they go
// too.

const (
	addrLabelAddVerbCst    = "add"
	addrLabelDelVerbCst    = "delete"
	addrLabelPrefixKeyCst  = "prefix"
	addrLabelLabelKeyCst   = "label"
	addrLabelModifyBufCst  = 1024
	addrLabelNoLabelCst    = math.MaxUint32
	addrLabelFlushVerbCst  = "flush"
	addrLabelFlushExtraCst = `"ip addrlabel flush" does not allow extra arguments`
)

// ErrAddrLabelFlushTerminated is `Flush terminated`: a failed dump or a short
// reply, after which ipaddrlabel_flush returns -1, so 255.
var ErrAddrLabelFlushTerminated = errors.New("addrlabel flush: Flush terminated")

// addrLabelWrite is ipaddrlabel_modify's request: the ifaddrlblmsg fields
// and the attributes, IFAL_LABEL then IFAL_ADDRESS.
type addrLabelWrite struct {
	family    uint8
	prefixLen uint8
	index     uint32
	label     uint32
	prefix    *inetPrefix
	labelSet  bool
}

// hdr is struct ifaddrlblmsg: family, a reserved byte, the prefix length,
// flags, the ifindex and seq.
func (w *addrLabelWrite) hdr() []byte {
	b := make([]byte, xtcpnl.IfAddrlblmsgSizeCst)
	b[0], b[2] = w.family, w.prefixLen
	binary.LittleEndian.PutUint32(b[4:8], w.index)
	return b
}

// addrLabelModify runs `ip addrlabel add` or `delete`: the argument loop,
// which sends the device lookup, then the request to GOIP_WRITE_OUT.
func addrLabelModify(c *runCtx, msgType uint16, args []string) error {
	svc := service.New(c.src, c.nextSeq)
	w, err := parseAddrLabelWriteArgs(c.family, newLinkNameResolver(svc), args)
	if err != nil {
		return err
	}
	var raw [addrLabelModifyBufCst]byte
	ab := xtcpnl.NewAttrBuilder(raw[:])
	if err = ab.PutU32(unix.IFAL_LABEL, w.label); err != nil {
		return err
	}
	if err = ab.PutBytes(unix.IFAL_ADDRESS, w.prefix.data); err != nil {
		return err
	}
	flags := uint16(unix.NLM_F_ACK)
	if msgType == unix.RTM_NEWADDRLABEL {
		flags |= unix.NLM_F_CREATE | unix.NLM_F_EXCL
	}
	b, err := xtcpnl.BuildWriteRequest(msgType, flags, c.nextSeq(), w.hdr(), ab.Bytes())
	if err != nil {
		return fmt.Errorf("goip: build addrlabel request: %w", err)
	}
	return writeAddrOut(b)
}

// parseAddrLabelWriteArgs is ipaddrlabel_modify's loop and the checks after
// it. family is preferred_family: get_prefix reads `prefix` in it, and the
// header takes it, AF_INET6 if unset. The loop skips an argument it does not
// know, as `ip` does.
func parseAddrLabelWriteArgs(family uint8, resolve linkResolver, args []string) (*addrLabelWrite, error) {
	w := &addrLabelWrite{family: family, label: addrLabelNoLabelCst}
	for i := 0; i < len(args); i++ {
		var err error
		switch args[i] {
		case addrLabelPrefixKeyCst:
			i, err = w.prefixArg(args, i, family)
		case devKeywordCst:
			i, err = w.dev(args, i, resolve)
		case addrLabelLabelKeyCst:
			i, err = w.labelArg(args, i)
		}
		if err != nil {
			return nil, err
		}
	}
	if w.prefix == nil {
		return nil, fmt.Errorf("%w: Not enough information: \"prefix\" argument is required", ErrInvalidArgument)
	}
	if !w.labelSet {
		return nil, fmt.Errorf("%w: Not enough information: \"label\" argument is required", ErrInvalidArgument)
	}
	w.prefixLen = bitlenU8(w.prefix.bitlen)
	if w.family == unix.AF_UNSPEC {
		w.family = unix.AF_INET6
	}
	return w, nil
}

func (w *addrLabelWrite) prefixArg(args []string, i int, family uint8) (int, error) {
	i, err := nextArg(args, i)
	if err != nil {
		return i, err
	}
	p, err := routeGetPrefix(args[i], family)
	if err != nil {
		return i, err
	}
	w.prefix = &p
	return i, nil
}

func (w *addrLabelWrite) dev(args []string, i int, resolve linkResolver) (int, error) {
	i, err := nextArg(args, i)
	if err != nil {
		return i, err
	}
	idx, err := resolve(args[i])
	if err != nil {
		return i, err
	}
	if idx == 0 {
		return i, invargErr("dev is invalid\n", args[i])
	}
	w.index = uint32(idx)
	return i, nil
}

// labelArg is get_u32 base 0, which may not be 0xffffffff, ip's unset label.
func (w *addrLabelWrite) labelArg(args []string, i int) (int, error) {
	i, err := nextArg(args, i)
	if err != nil {
		return i, err
	}
	v, ok := getU32(args[i], 0)
	if !ok || v == addrLabelNoLabelCst {
		return i, invargErr("label is invalid\n", args[i])
	}
	w.label, w.labelSet = v, true
	return i, nil
}

// addrLabelFlush is ipaddrlabel_flush: no arguments, the listing's dump, and
// an RTM_DELADDRLABEL copy of each reply carrying IFAL_ADDRESS, written to
// GOIP_WRITE_OUT after the dump. The copy keeps the reply's seq and port id;
// `ip` sends it at its second socket's seq, which the comparator does not
// compare. ip prints nothing, and -j changes nothing.
func addrLabelFlush(c *runCtx, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("%w: %s", ErrInvalidArgument, addrLabelFlushExtraCst)
	}
	if _, ok := c.src.(MsgDumpSource); !ok {
		return fmt.Errorf("addrlabel flush on a source without whole replies: %w", ErrNotImplemented)
	}
	msgs, err := service.New(c.src, c.nextSeq).AddrLabelMsgs(addrLabelFamily(c))
	if err != nil {
		return fmt.Errorf("%w: %w", ErrAddrLabelFlushTerminated, err)
	}
	var out []byte
	for _, m := range msgs {
		del, ok, cerr := addrLabelFlushCopy(m)
		if cerr != nil {
			if len(out) > 0 {
				// The copies ahead of it were sent before flush_addrlabel
				// stopped the dump.
				if werr := writeAddrOut(out); werr != nil {
					return werr
				}
			}
			return cerr
		}
		if ok {
			out = append(out, del...)
		}
	}
	if len(out) > 0 {
		return writeAddrOut(out)
	}
	return nil
}

// addrLabelFlushCopy is flush_addrlabel on one reply: the message itself,
// its type and flags rewritten. ok is false for a reply without
// IFAL_ADDRESS. A reply shorter than its header is `len < 0`, which ends
// the dump.
func addrLabelFlushCopy(m []byte) (del []byte, ok bool, err error) {
	body := m[xtcpnl.NlMsgHdrSizeCst:]
	if len(body) < xtcpnl.IfAddrlblmsgSizeCst {
		return nil, false, fmt.Errorf("%w: a %d-byte addrlabel", ErrAddrLabelFlushTerminated, len(body))
	}
	var addr bool
	if werr := xtcpnl.WalkRTAttrs(body[xtcpnl.IfAddrlblmsgSizeCst:], func(atype uint16, _ []byte) {
		addr = addr || atype == unix.IFAL_ADDRESS
	}); werr != nil {
		return nil, false, fmt.Errorf("addrlabel flush of an undecodable reply: %w: %w", ErrNotImplemented, werr)
	}
	if !addr {
		return nil, false, nil
	}
	del = append([]byte(nil), m...)
	binary.NativeEndian.PutUint16(del[4:6], unix.RTM_DELADDRLABEL)
	binary.NativeEndian.PutUint16(del[6:8], unix.NLM_F_REQUEST|unix.NLM_F_ACK)
	return del, true, nil
}

// addrLabelFamily is the listing's and the flush's `af`: preferred_family,
// AF_INET6 if unset (ip/ipaddrlabel.c:101-104, :221-224).
func addrLabelFamily(c *runCtx) uint8 {
	if c.family == unix.AF_UNSPEC {
		return unix.AF_INET6
	}
	return c.family
}
