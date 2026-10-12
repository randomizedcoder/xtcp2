// Command gordma is a read-only Go clone of rdma(8), iproute2 7.1.0's.
//
// Like goip, it is a coverage test for pkg/xtcpnl rather than a
// replacement: replaying rdma's strace corpus through it must reproduce
// rdma's requests and its output byte for byte. See internal/gordma.
//
// gordma never writes: its requests come from xtcpnl's builders, which
// admit only the read-only nldev commands.
package main

import (
	"os"

	"github.com/randomizedcoder/xtcp2/internal/gordma"
)

func main() {
	os.Exit(gordma.Run(os.Args[1:], os.Stdout, os.Stderr))
}
