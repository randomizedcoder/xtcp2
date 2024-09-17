package xtcp

import (
	"sync"
	"syscall"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"github.com/randomizedcoder/xtcp2/pkg/xtcppb"
	"github.com/twmb/franz-go/pkg/kgo"
)

func (x *XTCP) InitSyncPools() {

	// we want to read in large blocks, ideally 32kB.  defaults to 32kB
	if *x.config.PacketSizeMply == 0 {
		*x.config.PacketSizeMply = 8
	}

	var packetBufferSize int
	//** is not double pointer.  it is multiply by pointer.
	if *x.config.PacketSize == 0 {
		packetBufferSize = syscall.Getpagesize() * *x.config.PacketSizeMply
	} else {
		packetBufferSize = *x.config.PacketSize * *x.config.PacketSizeMply
	}

	x.packetBufferPool = sync.Pool{
		New: func() interface{} {
			b := make([]byte, packetBufferSize)
			return &b
		},
	}

	x.xtcpRecordPool = sync.Pool{
		New: func() interface{} {
			return new(xtcppb.FlatXtcpRecord)
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
