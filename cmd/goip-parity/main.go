// Command goip-parity is the comparator half of the netlink parity harness.
//
// It reads the `ip` → goip → `ip` capture triples a run produced and reports
// whether goip asked the kernel the same questions `ip` did: transaction
// counts, request bytes, reply object keys and attributes, plus a structural
// comparison of what each tool printed. See docs/netlink/coverage-status.md.
//
// # Why this is a separate binary from cmd/goip
//
// Because it must never open a socket. The comparator runs while captures are
// being taken, and a comparator that could talk netlink would put its own
// traffic inside a capture window and have it attributed to goip — a failure
// that would look like a goip defect and would be very hard to read back out
// of a pcap. Keeping the two binaries apart means no refactor inside
// internal/goip can give the comparator a socket by accident, and
// internal/goipparity's import guard fails the build if its closure ever
// grows one.
//
// It does not capture, either. The guest's xtcp2-nlcap does that, and the
// measurements behind it — libpcap's --immediate-mode, the `ss -x` stop
// sentinel, the per-command datagram floors — are not worth rediscovering in
// a Go reimplementation.
package main

import (
	"os"

	"github.com/randomizedcoder/xtcp2/internal/goipparity"
)

func main() {
	os.Exit(goipparity.Run(os.Args[1:], os.Stdout, os.Stderr))
}
