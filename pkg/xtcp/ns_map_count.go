// Package misc are some small helper functions used throughout the xtcp code
//
// It is perhaps poor form to name a module "misc", could be renamed to "utils"
package xtcp

import (
	"context"
	"log"
	"sync"
	"time"
)

const (
	xtcpNSName = "xtcpNS"

	guageUpdateFrequency       = 1 * time.Minute
	reconcileFrequency         = 5 * time.Minute
	goRoutineReporterFrequency = 1 * time.Minute
)

// nsMapCountReporter regularly update the promethus gauge
// that tracks how many items are in the map
// the number of items in the map should match the number of network
// name spaces
func (x *XTCP) nsMapCountReporter(ctx context.Context, wg *sync.WaitGroup) {

	defer wg.Done()

	x.pC.WithLabelValues("mapCountReporter", "start", "counter").Inc()
	defer x.pC.WithLabelValues("mapCountReporter", "complete", "counter").Inc()

	t := time.NewTicker(guageUpdateFrequency)
	for {
		select {
		case <-t.C:
			mc := x.MapCount()
			x.pG.Set(float64(mc))
			x.pC.WithLabelValues("mapCountReporter", "tick", "count").Inc()

			if x.debugLevel > 100 {
				// debug code to check the counters work correctly
				log.Printf("add MapCount(): %d\n", mc)
				log.Printf("add LenSyncMap(): %d\n", x.LenSyncMap())
			}
		case <-ctx.Done():
			return
		}
	}
}

func (x *XTCP) MapCount() uint64 {
	store := x.storeCount.Load()
	delete := x.deleteCount.Load()
	return store - delete
}

// LenSyncMap wraps lenSyncMap
func (x *XTCP) LenSyncMap() int {
	return lenSyncMap(x.nsMap)
}

// lenSyncMap is a generic function for iterating
// over a map to count the items
// this function was used for verification only
// and is not used in production
func lenSyncMap(m *sync.Map) int {
	var i int
	m.Range(func(k, v interface{}) bool {
		i++
		return true
	})
	return i
}

// GetNetlinkSocketFDs is the accessor function to return
// the current active set of netlink file descriptors
func (x *XTCP) GetNetlinkSocketFDs() (fds []int) {
	x.pC.WithLabelValues("GetNetlinkSocketFDs", "start", "counter").Inc()
	return getNetlinkSocketFDs(x.nsMap)
}

// getNetlinkSocketFDs returns the current active set of
// netlink file descriptors
func getNetlinkSocketFDs(m *sync.Map) (fds []int) {
	m.Range(func(k, v interface{}) bool {
		fds = append(fds, v.(netNSitem).socketFD)
		return true
	})
	return fds
}
