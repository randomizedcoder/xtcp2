// Command gotc is a read-only Go clone of tc(8), iproute2 7.2.0's, for the
// qdisc and class shows captured in pkg/xtcpnl's workbench corpus.
//
// Like gobridge, it is a coverage test for pkg/xtcpnl rather than a
// replacement: replaying tc's nlmon (ws) pcap corpus through it must
// reproduce tc's stdout, stderr and exit code byte for byte. See
// internal/gotc. Without GOTC_REPLAY it asks the kernel.
//
// gotc never writes: its requests are qdisc, class and link dumps and the
// link lookups tc makes.
package main

import (
	"os"

	"github.com/randomizedcoder/xtcp2/internal/gotc"
)

func main() {
	os.Exit(gotc.Run(os.Args[1:], os.Stdout, os.Stderr))
}
