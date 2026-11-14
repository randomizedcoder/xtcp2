package goip

// The `type ipvlan` and `type ipvtap` tests (docs/netlink/goip-link-kinds.md,
// "KG2-T03 The ipvlan and ipvtap words"), against KC3-T03's rows of the
// 7.3.0 kind corpus.

// KC3-T03's rows, a directory per kind.
const (
	ipvlanKindDir = "../../pkg/xtcpnl/testdata/7_3_0/link-write/kinds/ipvlan/"
	ipvtapKindDir = "../../pkg/xtcpnl/testdata/7_3_0/link-write/kinds/ipvtap/"
)
