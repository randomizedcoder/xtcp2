package goip

import (
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// wsMptcpDir is the workbench's mptcp space (genl-clones 4C): `ip mptcp`'s
// endpoint and limits shows, captured on generic+route, so each pcap holds
// the mptcp_pm exchanges and the RTM_GETLINK dump ip's ll_init_map made.
const wsMptcpDir = "../../pkg/xtcpnl/testdata/7_3_1/ws/mptcp"

// TestWsMptcpCorpus replays every row of the mptcp ws space through goip
// (genl-clones 4R): a show prints ip's stdout byte for byte, the -j
// limits' text included, and nothing on stderr; every row exits as ip
// did; goip makes ip's genl exchanges, every one of them and no write, and
// ip's route requests, the ll_init_map link dump, byte for byte with seq
// and pid zeroed. A refusal is goip's own text, ip's message inside it
// (without its final full stop), but for the kernel's extack, which goip
// does not print: it names the errno.
//
// go test ./internal/goip/ -run TestWsMptcpCorpus
func TestWsMptcpCorpus(t *testing.T) {
	useLLMap720(t) // the rows ran ip 7.2.0, whose link dump asks for names alone
	tests := []wsGenlCase{
		{description: "boundary: no endpoints, NLMSG_DONE alone and no link dump", row: "ec-001"},
		{description: "boundary: -j no endpoints, an empty array", row: "pw-002"},
		{description: "positive: one endpoint, the link dump on its print", row: "pw-001"},
		{description: "positive: -j one endpoint", row: "ec-002"},
		{description: "positive: four endpoints, dev, port and flags in table order", row: "ec-003"},
		{description: "positive: -j four endpoints", row: "pw-003"},
		{description: "positive: one endpoint by id, the link dump first", row: "ec-004"},
		{description: "positive: -j one endpoint by id, an array of one", row: "pw-004"},
		{description: "corner: bare endpoint is the dump", row: "ec-005"},
		{description: "corner: -j bare endpoint", row: "pw-005"},
		{description: "boundary: the namespace's default limits", row: "ec-006"},
		{description: "corner: -j default limits print text", row: "pw-006"},
		{description: "positive: limits set in pre, read back", row: "ec-007"},
		{description: "corner: -j limits set in pre, text", row: "pw-007"},
		{description: "corner: bare limits is show", row: "ec-008"},
		{description: "corner: -j bare limits, text", row: "pw-008"},
		{description: "corner: limits show with a set's attribute, the same limits", row: "ec-009"},
		{description: "corner: -j limits show with a set's attribute, text", row: "pw-009"},
		{description: "negative: help, usage before the lookup", row: "ng-001", expected: "Usage:\tip mptcp endpoint add ADDRESS"},
		{description: "negative: an unknown object after the lookup", row: "ng-002",
			expected: `Command "badobj" is unknown, try "ip mptcp help"`},
		{description: "negative: an unknown endpoint sub-command", row: "ng-003",
			expected: `Command "junk" is unknown, try "ip mptcp help"`},
		{description: "negative: show's word is no address, get_addr's exit 1", row: "ng-004",
			expected: `Error: any valid address is expected rather than "junk"`},
		{description: "boundary: id 0 is only for delete", row: "ng-005", expected: `Error: argument "ID" is wrong: cannot be 0`},
		{description: "negative: the kernel has no endpoint 9, EINVAL (ip prints its extack)", row: "ng-006",
			expected: "invalid argument"},
		{description: "negative: limits show's word is no limit", row: "ng-007",
			expected: `Error: argument "junk" is wrong: unknown limit`},
		{description: "corner: limits has no unknown branch of its own", row: "ng-008",
			expected: `Command "junk" is unknown, try "ip mptcp help"`},
	}
	wsGenlCheck(t, wsMptcpDir, "mptcp", xtcpnl.MPTCPPMGenlName, runMptcp, tests)
}
