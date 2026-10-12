// Command gordma-parity is the comparator half of gordma's live parity run
// (nix/microvms/scripts/gordma-parity.exp): it prints the command table the
// guest's driver iterates, and compares the rdma → gordma → rdma strace
// triples the driver leaves behind. See docs/netlink/coverage-status.md.
//
// It is a separate binary from cmd/gordma because it must never open a
// socket: it runs in the guest beside the captures, and internal/gordma's
// live source would put that within reach of a refactor.
// internal/gordmaparity's import guard holds the line.
package main

import (
	"os"

	"github.com/randomizedcoder/xtcp2/internal/gordmaparity"
)

func main() {
	os.Exit(gordmaparity.Run(os.Args[1:], os.Stdout, os.Stderr))
}
