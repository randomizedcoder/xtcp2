package goip

import (
	"encoding/binary"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// The xfrm words and bounds the state listing shares with its writes.
const (
	// xfrmCtxBufCst is CTX_BUF_SIZE, ctx's buffer, which ip overruns.
	xfrmCtxBufCst = 256
	xfrmDirCst    = "dir"
	xfrmDirOutCst = "out"
)

// xfrmSecCtxBytes is xfrm_sctx_parse's struct and the context.
func xfrmSecCtxBytes(s string) []byte {
	slen := len(s) + 1
	b := binary.LittleEndian.AppendUint16(nil, uint16(xtcpnl.XfrmUserSecCtxSizeCst+slen))
	b = binary.LittleEndian.AppendUint16(b, xtcpnl.XfrmAttrSecCtx)
	b = append(b, 1, 1)
	b = binary.LittleEndian.AppendUint16(b, uint16(slen))
	b = append(b, s...)
	return append(b, 0)
}
