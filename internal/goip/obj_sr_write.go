//go:build goip_write

package goip

import (
	"fmt"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

const (
	// srReqBufCst is SEG6_REQUEST's buffer, and srReqHdrCst the nlmsghdr
	// and genlmsghdr ahead of the attributes in it.
	srReqBufCst = 1024
	srReqHdrCst = xtcpnl.NlMsgHdrSizeCst + 4
)

// srWriteAttrs is a write's attributes. SETHMAC's SECRETLEN is the
// secret's length as a u8, so 256 bytes is 0, and a secret past
// SEG6_REQUEST's 1024-byte buffer is left out with addattr_l's message,
// which ip ignores.
func srWriteAttrs(c *runCtx, r *srReq) ([]byte, error) {
	var raw [srReqBufCst - srReqHdrCst]byte
	b := xtcpnl.NewAttrBuilder(raw[:])
	if r.cmd == xtcpnl.Seg6CmdSetTunsrc {
		if err := b.PutBytes(xtcpnl.Seg6AttrDst, r.addr[:]); err != nil {
			return nil, err
		}
		return b.Bytes(), nil
	}
	if err := b.PutU32(xtcpnl.Seg6AttrHmacKeyID, r.keyID); err != nil {
		return nil, err
	}
	if err := b.PutU8(xtcpnl.Seg6AttrSecretLen, uint8(len(r.pass))); err != nil {
		return nil, err
	}
	if err := b.PutU8(xtcpnl.Seg6AttrAlgID, r.algID); err != nil {
		return nil, err
	}
	if len(r.pass) != 0 {
		if err := b.PutBytes(xtcpnl.Seg6AttrSecret, r.pass); err != nil {
			if _, werr := fmt.Fprintf(c.errOut, "addattr_l ERROR: message exceeded bound of %d\n", srReqBufCst); werr != nil {
				return nil, werr
			}
		}
	}
	return b.Bytes(), nil
}

// srWrite is seg6_do_cmd's set: the request, NLM_F_ACK, to GOIP_WRITE_OUT.
func srWrite(c *runCtx, id uint16, r *srReq) error {
	attrs, err := srWriteAttrs(c, r)
	if err != nil {
		return err
	}
	req, err := xtcpnl.BuildGenlWriteRequest(xtcpnl.Seg6GenlName, id, r.cmd, xtcpnl.Seg6GenlVersion,
		unix.NLM_F_ACK, c.nextSeq(), attrs)
	if err != nil {
		return err
	}
	return writeAddrOut(req)
}
