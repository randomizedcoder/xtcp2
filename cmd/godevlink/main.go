// Command godevlink is a read-only Go clone of devlink(8), iproute2 7.2.0's,
// for the dump/show subcommands captured in pkg/xtcpnl's workbench corpus.
//
// Like goip and gordma, it is a coverage test for pkg/xtcpnl rather than a
// replacement: replaying devlink's nlmon (ws) pcap corpus through it must
// reproduce devlink's stdout, stderr and exit code byte for byte. See
// internal/godevlink.
//
// godevlink never writes: its requests are the read-only DEVLINK_CMD_*_GET
// dumps and the dpipe-table gets `devlink resource show` issues.
package main

import (
	"os"

	"github.com/randomizedcoder/xtcp2/internal/godevlink"
)

func main() {
	os.Exit(godevlink.Run(os.Args[1:], os.Stdout, os.Stderr))
}
