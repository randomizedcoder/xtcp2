package xtcp

import (
	"context"
	"log"
	"sync"
)

// netlinkerReadyChSize matches destinationReadyChSize — buffered so the
// poller side never blocks on the readiness signal.
const netlinkerReadyChSize = 1

// InitNetlinkers registers the syscall and io_uring netlinker variants
// into x.Netlinkers, then selects the active one based on config.IoUring
// and stores it in x.Netlinker. Mirrors the InitDests pattern at
// pkg/xtcp/init_destinations.go:65.
//
// Run during xtcp Init alongside InitDests.
func (x *XTCP) InitNetlinkers(ctx context.Context, wg *sync.WaitGroup) {

	defer wg.Done()

	x.Netlinkers.Store("syscall", NetlinkerFunc(func(ctx context.Context, wg *sync.WaitGroup, nsName *string, fd int, id uint32) {
		x.netlinkerSyscall(ctx, wg, nsName, fd, id)
	}))
	x.Netlinkers.Store("io_uring", NetlinkerFunc(func(ctx context.Context, wg *sync.WaitGroup, nsName *string, fd int, id uint32) {
		x.netlinkerIoUring(ctx, wg, nsName, fd, id)
	}))

	key := "syscall"
	if x.config.IoUring {
		key = "io_uring"
	}
	f, ok := x.Netlinkers.Load(key)
	if !ok {
		log.Fatalf("InitNetlinkers no variant registered for key:%s", key)
	}
	x.Netlinker = f.(NetlinkerFunc)

	if x.debugLevel > 10 {
		log.Printf("InitNetlinkers selected variant:%s", key)
	}

	if x.NetlinkerReady != nil {
		x.NetlinkerReady <- struct{}{}
	}
}
