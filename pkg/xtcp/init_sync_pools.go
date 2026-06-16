package xtcp

import (
	"sync"
	"syscall"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

const (
	destBytesMaxSizeCst = 10000
)

func (x *XTCP) InitSyncPools(wg *sync.WaitGroup) {

	defer wg.Done()

	// we want to read in large blocks, ideally 32kB.  defaults to 32kB
	if x.config.PacketSizeMply == 0 {
		x.config.PacketSizeMply = 8
	}

	var packetBufferSize int
	// ** is not double pointer.  it is multiply by pointer.
	if x.config.PacketSize == 0 {
		packetBufferSize = syscall.Getpagesize() * int(x.config.PacketSizeMply)
	} else {
		packetBufferSize = int(x.config.PacketSize * uint64(x.config.PacketSizeMply))
	}

	x.packetBufferPool.Init(func() *[]byte {
		b := make([]byte, packetBufferSize)
		return &b
	})

	x.xtcpEnvelopePool.Init(func() *xtcp_flat_record.Envelope {
		return new(xtcp_flat_record.Envelope)
	})

	x.xtcpRecordPool.Init(func() *xtcp_flat_record.XtcpFlatRecord {
		return new(xtcp_flat_record.XtcpFlatRecord)
		// return new(xtcp_flat_record.Envelope_XtcpFlatRecord)
	})

	x.nlhPool.Init(func() *xtcpnl.NlMsgHdr {
		return new(xtcpnl.NlMsgHdr)
	})

	x.rtaPool.Init(func() *xtcpnl.RTAttr {
		return new(xtcpnl.RTAttr)
	})

	x.destBytesPool.Init(func() *[]byte {
		b := make([]byte, 0, destBytesMaxSizeCst)
		return &b
	})
}
