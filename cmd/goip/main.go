// Command goip is a read-only Go reimplementation of a subset of ip(8).
//
// It exists as a coverage test for pkg/xtcpnl, not as an `ip` replacement.
// Cloning `ip` forces the library to exercise every message and attribute a
// real tool needs, and comparing the netlink traffic the two produce turns
// "did we cover this?" into a diff that can fail a build. See
// docs/netlink/coverage-expansion.md.
//
// goip never creates, deletes or sets anything: pkg/xtcpnl's request encoder
// rejects every message type outside RTM_GET* and the NLMSG_* control types,
// so the read-only property is enforced in code rather than by convention.
package main

import (
	"os"

	"github.com/randomizedcoder/xtcp2/internal/goip"
)

func main() {
	os.Exit(goip.Run(os.Args[1:], os.Stdout, os.Stderr))
}
