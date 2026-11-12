package goip

// The `type macvlan` and `type macvtap` tests (docs/netlink/goip-link-kinds.md,
// "KG2-T02 The macvlan and macvtap words"), against KC3-T02's rows of the
// 7.3.0 kind corpus.

// KC3-T02's rows, a directory per kind.
const (
	macvlanKindDir = "../../pkg/xtcpnl/testdata/7_3_0/link-write/kinds/macvlan/"
	macvtapKindDir = "../../pkg/xtcpnl/testdata/7_3_0/link-write/kinds/macvtap/"
)
