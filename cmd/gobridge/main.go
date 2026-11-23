// Command gobridge is a read-only Go clone of bridge(8), iproute2 7.2.0's,
// for the link, vlan, vni and mst shows captured in pkg/xtcpnl's workbench
// corpus.
//
// Like gotipc and govdpa, it is a coverage test for pkg/xtcpnl rather than
// a replacement: replaying bridge's nlmon (ws) pcap corpus through it must
// reproduce bridge's stdout, stderr and exit code byte for byte. See
// internal/gobridge. Without GOBRIDGE_REPLAY it asks the kernel.
//
// gobridge never writes: its requests are link, vlan, stats and tunnel
// dumps and the link lookups bridge makes.
package main

import (
	"os"

	"github.com/randomizedcoder/xtcp2/internal/gobridge"
)

func main() {
	os.Exit(gobridge.Run(os.Args[1:], os.Stdout, os.Stderr))
}
