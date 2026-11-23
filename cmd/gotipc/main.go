// Command gotipc is a read-only Go clone of tipc(8), iproute2 7.2.0's, for
// the read-only commands captured in pkg/xtcpnl's workbench corpus.
//
// Like godevlink and govdpa, it is a coverage test for pkg/xtcpnl rather
// than a replacement: replaying tipc's nlmon (ws) pcap corpus through it
// must reproduce tipc's stdout, stderr and exit code byte for byte. See
// internal/gotipc.
//
// gotipc never writes: its requests are the TIPC *_GET dumps and gets and
// the family lookups tipc makes.
package main

import (
	"os"

	"github.com/randomizedcoder/xtcp2/internal/gotipc"
)

func main() {
	os.Exit(gotipc.Run(os.Args[1:], os.Stdout, os.Stderr))
}
