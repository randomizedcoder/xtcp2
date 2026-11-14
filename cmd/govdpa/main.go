// Command govdpa is a read-only Go clone of vdpa(8), iproute2 7.2.0's, for
// the show subcommands captured in pkg/xtcpnl's workbench corpus.
//
// Like goip, gordma and godevlink, it is a coverage test for pkg/xtcpnl
// rather than a replacement: replaying vdpa's nlmon (ws) pcap corpus through
// it must reproduce vdpa's stdout, stderr and exit code byte for byte. See
// internal/govdpa.
//
// govdpa never writes: its requests are the read-only VDPA_CMD_*_GET
// dumps and gets.
package main

import (
	"os"

	"github.com/randomizedcoder/xtcp2/internal/govdpa"
)

func main() {
	os.Exit(govdpa.Run(os.Args[1:], os.Stdout, os.Stderr))
}
