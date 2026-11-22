package goip

import (
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// wsFouDir is the workbench's fou space (genl-clones 3C): `ip fou show`
// over five fixtures, plain and -j, and three refusals, captured on
// generic netlink alone.
const wsFouDir = "../../pkg/xtcpnl/testdata/7_3_1/ws/fou"

// TestWsFouCorpus replays every row of the fou ws space through goip,
// the corpus gofou was built on before goip's obj_fou.go took its place:
// a show prints ip's stdout byte for byte and nothing on stderr; every row
// exits as ip did; goip makes ip's genl exchanges, every one of them and
// no write, and no route request, as ip made none. A refusal is goip's own
// text, ip's message inside it without its final full stop.
//
// go test ./internal/goip/ -run TestWsFouCorpus
func TestWsFouCorpus(t *testing.T) {
	tests := []wsGenlCase{
		{description: "boundary: no ports, NLMSG_DONE alone", row: "ec-001"},
		{description: "boundary: -j no ports, an empty array", row: "pw-002"},
		{description: "positive: one direct port", row: "pw-001"},
		{description: "positive: -j one direct port", row: "ec-002"},
		{description: "positive: a gue port", row: "ec-003"},
		{description: "positive: -j a gue port", row: "pw-003"},
		{description: "positive: several ports in the kernel's order", row: "ec-004"},
		{description: "positive: -j several ports", row: "pw-004"},
		{description: "positive: an IPv6 port", row: "ec-005"},
		{description: "positive: -j an IPv6 port, family inet6", row: "pw-005"},
		{description: "negative: help, usage before the lookup, exit 255", row: "ng-001", expected: "Usage: ip fou add port PORT"},
		{description: "negative: show takes no argument, exit 1 after the lookup", row: "ng-002",
			expected: `"ip fou show" does not take any arguments`},
		{description: "negative: an unknown sub-command, exit 255 after the lookup", row: "ng-003",
			expected: `Command "badobj" is unknown, try "ip fou help"`},
	}
	wsGenlCheck(t, wsFouDir, "fou", xtcpnl.FOUGenlName, runFou, tests)
}
