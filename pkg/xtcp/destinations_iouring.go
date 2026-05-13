package xtcp

import (
	"context"
	"encoding/binary"
	"errors"
	"log"

	xio "github.com/randomizedcoder/xtcp2/pkg/io_uring"
)

// io_uring destinations: queue send SQEs against the per-Netlinker ring
// that called us (looked up from ctx). Submit happens inside
// netlinkerIoUring after the Deserialize loop returns, so a whole dump
// cycle of N records turns into one io_uring_enter for all N sends —
// the headline "lighter on the system" win for the write path.
//
// Buffer ownership: the marshalled *[]byte passed in is pinned by the
// ring's in-flight map until the kernel signals the send is done. The
// CQE drainer (netlinker_iouring.handleSendCQE) records the outcome to
// Prometheus and lets GC reclaim the buffer. The destination function
// returns (1, nil) optimistically — mirrors the destKafka async
// callback contract (destinations.go:117-123).

// errNoRingInCtx is returned when an io_uring destination function is
// called without a Ring stashed in the context. Indicates a misconfig
// at init time — production should never see it.
var errNoRingInCtx = errors.New("io_uring destination: no ring in context (config.IoUring=true but netlinker variant disagrees?)")

func (x *XTCP) destUDPIoUring(ctx context.Context, b *[]byte) (int, error) {
	ring := ringFromContext(ctx)
	if ring == nil {
		x.pC.WithLabelValues("destUDPIoUring", "noRing", "error").Inc()
		return 0, errNoRingInCtx
	}
	if _, err := ring.EnqueueSend(x.udpFD, b, xio.OpSendUDP); err != nil {
		x.pC.WithLabelValues("destUDPIoUring", "EnqueueSend", "error").Inc()
		if x.debugLevel > 100 {
			log.Printf("destUDPIoUring EnqueueSend err:%v", err)
		}
		return 0, err
	}
	return 1, nil
}

func (x *XTCP) destUnixIoUring(ctx context.Context, b *[]byte) (int, error) {
	ring := ringFromContext(ctx)
	if ring == nil {
		x.pC.WithLabelValues("destUnixIoUring", "noRing", "error").Inc()
		return 0, errNoRingInCtx
	}
	// Same varint-length framing as destUnix on the syscall path
	// (destinations.go:283-302), but delivered atomically as a single
	// writev SQE so the daemon's receiver sees one frame per record
	// with no chance of partial-write interleaving.
	var hdr [binary.MaxVarintLen64]byte
	hdrLen := binary.PutUvarint(hdr[:], uint64(len(*b)))
	if _, err := ring.EnqueueWritevUnix(x.unixFD, hdr[:hdrLen], b); err != nil {
		x.pC.WithLabelValues("destUnixIoUring", "EnqueueWritev", "error").Inc()
		if x.debugLevel > 100 {
			log.Printf("destUnixIoUring EnqueueWritev err:%v", err)
		}
		return 0, err
	}
	return 1, nil
}

func (x *XTCP) destUnixGramIoUring(ctx context.Context, b *[]byte) (int, error) {
	ring := ringFromContext(ctx)
	if ring == nil {
		x.pC.WithLabelValues("destUnixGramIoUring", "noRing", "error").Inc()
		return 0, errNoRingInCtx
	}
	if _, err := ring.EnqueueSend(x.unixGramFD, b, xio.OpSendUnixGram); err != nil {
		x.pC.WithLabelValues("destUnixGramIoUring", "EnqueueSend", "error").Inc()
		if x.debugLevel > 100 {
			log.Printf("destUnixGramIoUring EnqueueSend err:%v", err)
		}
		return 0, err
	}
	return 1, nil
}
