package xtcp

import (
	"sync"
	"syscall"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"github.com/twmb/franz-go/pkg/kgo"
)

func (x *XTCP) InitSyncPools(wg *sync.WaitGroup) {

	defer wg.Done()

	// we want to read in large blocks, ideally 32kB.  defaults to 32kB
	if x.config.PacketSizeMply == 0 {
		x.config.PacketSizeMply = 8
	}

	var packetBufferSize int
	//** is not double pointer.  it is multiply by pointer.
	if x.config.PacketSize == 0 {
		packetBufferSize = syscall.Getpagesize() * int(x.config.PacketSizeMply)
	} else {
		packetBufferSize = int(x.config.PacketSize * uint64(x.config.PacketSizeMply))
	}

	x.packetBufferPool = sync.Pool{
		New: func() interface{} {
			b := make([]byte, packetBufferSize)
			return &b
		},
	}

	x.xtcpEnvelopePool = sync.Pool{
		New: func() interface{} {
			return new(xtcp_flat_record.Envelope)
		},
	}

	x.xtcpRecordPool = sync.Pool{
		New: func() interface{} {
			return new(xtcp_flat_record.XtcpFlatRecord)
		},
	}

	x.nlhPool = sync.Pool{
		New: func() interface{} {
			return new(xtcpnl.NlMsgHdr)
		},
	}

	x.rtaPool = sync.Pool{
		New: func() interface{} {
			return new(xtcpnl.RTAttr)
		},
	}

	x.kgoRecordPool = sync.Pool{
		New: func() interface{} {
			return new(kgo.Record)
		},
	}
}
