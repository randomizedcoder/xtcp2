package main

import (
	"fmt"

	"github.com/randomizedcoder/xtcp2/internal/goip/goiphttp"
)

// withIPPaths mounts the read-only `ip … show` paths (/ip/OBJECT?query,
// /ip/OBJECT/{interface} and the `ip` command lines /ip/[OPTIONS/]OBJECT/VERB/…,
// each under /i as well) on the main listener, served[0]. They are live
// netlink reads, independent of the monitor's snapshot, so on-demand refresh
// never applies to them and they never take a metrics route's slots.
func withIPPaths(served []endpoint, o goiphttp.Options) ([]endpoint, error) {
	h, err := goiphttp.New(o)
	if err != nil {
		return nil, fmt.Errorf("ip paths: %w", err)
	}
	served[0].routes = append(served[0].routes, route{path: goiphttp.Prefix, mount: h.Register})
	return served, nil
}
