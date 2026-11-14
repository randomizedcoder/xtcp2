// Command gofou is a read-only Go clone of `ip fou` (foo-over-UDP),
// iproute2 7.2.0's, for the `fou show` dump captured in pkg/xtcpnl's
// workbench corpus.
//
// Like goip, gordma, godevlink and govdpa, it is a coverage test for
// pkg/xtcpnl rather than a replacement: replaying `ip fou`'s nlmon (ws) pcap
// corpus through it must reproduce ip's stdout, stderr and exit code byte for
// byte. See internal/gofou.
//
// gofou never writes: its only request is the read-only FOU_CMD_GET dump.
package main

import (
	"os"

	"github.com/randomizedcoder/xtcp2/internal/gofou"
)

func main() {
	os.Exit(gofou.Run(os.Args[1:], os.Stdout, os.Stderr))
}
