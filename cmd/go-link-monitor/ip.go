package main

import (
	"fmt"

	"github.com/randomizedcoder/xtcp2/internal/goip/goiphttp"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/linkmonitorhttp"
)

// withIPPaths mounts the read-only `ip … show` paths (/ip/OBJECT?query,
// /ip/OBJECT/{interface} and the `ip` command lines /ip/[OPTIONS/]OBJECT/VERB/…,
// each under /i as well) on the main listener, served[0]. They are live
// netlink reads, independent of the monitor's snapshot, so on-demand refresh
// never applies to them and they never take a metrics route's slots.
func withIPPaths(served []endpoint, o goiphttp.Options) ([]endpoint, error) {
	r, err := linkmonitorhttp.IPRoute(o)
	if err != nil {
		return nil, fmt.Errorf("ip paths: %w", err)
	}
	served[0].routes = append(served[0].routes, r)
	return served, nil
}
