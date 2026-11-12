// Command goip is a Go reimplementation of a subset of ip(8).
//
// It exists as a coverage test for pkg/xtcpnl, not as an `ip` replacement.
// Cloning `ip` forces the library to exercise every message and attribute a
// real tool needs, and comparing the netlink traffic the two produce turns
// "did we cover this?" into a diff that can fail a build. See
// docs/netlink/coverage-expansion.md.
//
// goip never applies a write. Its write verbs (add, set, delete, flush,
// restore, …) build ip's request as a dry run, written to GOIP_WRITE_OUT,
// and compile only with -tags goip_write, which the Nix goip build sets.
// Without the tag they are ErrWriteNotCompiled, as they always are in the
// daemons that serve /ip; see docs/build-flavors.md.
package main

import (
	"os"

	"github.com/randomizedcoder/xtcp2/internal/goip"
)

func main() {
	os.Exit(goip.Run(os.Args[1:], os.Stdout, os.Stderr))
}
