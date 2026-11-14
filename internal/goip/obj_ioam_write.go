//go:build goip_write

package goip

import (
	"errors"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

const (
	// ioamReqBufCst is IOAM6_REQUEST's buffer, and ioamReqHdrCst the
	// nlmsghdr and genlmsghdr ahead of the attributes in it.
	ioamReqBufCst = 1056
	ioamReqHdrCst = xtcpnl.NlMsgHdrSizeCst + 4
)

// ioamWriteAttrs is a command's attributes, in ioam6_do_cmd's order. The
// wide data is addattr64's, eight bytes in host order with no pad
// attribute.
func ioamWriteAttrs(r *ioamReq) ([]byte, error) {
	var raw [ioamReqBufCst - ioamReqHdrCst]byte
	b := xtcpnl.NewAttrBuilder(raw[:])
	var errs []error
	switch r.cmd {
	case xtcpnl.IOAM6CmdAddNamespace:
		errs = append(errs, b.PutU16(xtcpnl.IOAM6AttrNsID, r.nsID))
		if r.hasData {
			errs = append(errs, b.PutU32(xtcpnl.IOAM6AttrNsData, r.nsData))
		}
		if r.hasWide {
			var w [8]byte
			xtcpnl.NativeEndian().PutUint64(w[:], r.nsWide)
			errs = append(errs, b.PutBytes(xtcpnl.IOAM6AttrNsDataWide, w[:]))
		}
	case xtcpnl.IOAM6CmdDelNamespace:
		errs = append(errs, b.PutU16(xtcpnl.IOAM6AttrNsID, r.nsID))
	case xtcpnl.IOAM6CmdAddSchema:
		errs = append(errs, b.PutU32(xtcpnl.IOAM6AttrScID, r.scID),
			b.PutBytes(xtcpnl.IOAM6AttrScData, []byte(r.scData)))
	case xtcpnl.IOAM6CmdDelSchema:
		errs = append(errs, b.PutU32(xtcpnl.IOAM6AttrScID, r.scID))
	case xtcpnl.IOAM6CmdNsSetSchema:
		errs = append(errs, b.PutU16(xtcpnl.IOAM6AttrNsID, r.nsID))
		if r.scNone {
			errs = append(errs, b.PutBytes(xtcpnl.IOAM6AttrScNone, nil))
		} else {
			errs = append(errs, b.PutU32(xtcpnl.IOAM6AttrScID, r.scID))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// ioamWrite is ioam6_do_cmd's command: the request, NLM_F_ACK, to
// GOIP_WRITE_OUT.
func ioamWrite(c *runCtx, id uint16, r *ioamReq) error {
	attrs, err := ioamWriteAttrs(r)
	if err != nil {
		return err
	}
	req, err := xtcpnl.BuildGenlWriteRequest(xtcpnl.IOAM6GenlName, id, r.cmd, xtcpnl.IOAM6GenlVersion,
		unix.NLM_F_ACK, c.nextSeq(), attrs)
	if err != nil {
		return err
	}
	return writeAddrOut(req)
}
