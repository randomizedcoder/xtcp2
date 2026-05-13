package xtcp

import (
	"context"
	"log"
	"strings"
	"sync"
)

// InitDests chooses the configured destination scheme (parsed from
// x.config.Dest's prefix) and stores the built Destination on x.dest.
//
// Destinations are registered into a package-level registry from per-scheme
// init() funcs (see destinations_*.go). Missing entries are reported with
// distinct error messages for "unknown scheme" vs "scheme known but not
// compiled into this binary" — the latter tells the operator which build
// tag to add (e.g. `-tags dest_kafka`).
//
// Errors abort startup via x.fatalf so tests can intercept rather than
// taking the process down with log.Fatalf.
func (x *XTCP) InitDests(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	scheme, _, _ := strings.Cut(x.config.Dest, ":")

	factory, status := lookupDestinationFactory(scheme)
	if status != destLookupFound {
		x.fatalf("%v", destinationLookupError(scheme, status))
		return
	}

	dest, err := factory(ctx, x)
	if err != nil {
		x.fatalf("InitDests factory(%s): %v", scheme, err)
		return
	}
	x.dest = dest

	if x.debugLevel > 10 {
		log.Printf("InitDests scheme:%s compiled-in:%v", scheme, CompiledInSchemes())
	}

	x.DestinationReady <- struct{}{}
}
