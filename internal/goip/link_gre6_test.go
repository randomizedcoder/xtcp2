package goip

// The `type ip6gre|ip6gretap|ip6erspan` tests (docs/netlink/goip-link-kinds.md,
// "KG4-T03 The ip6gre, ip6gretap and ip6erspan words"), against KC5-T02's
// rows of the 7.3.0 kind corpus.

const (
	ip6greKindDir    = "../../pkg/xtcpnl/testdata/7_3_0/link-write/kinds/ip6gre/"
	ip6gretapKindDir = "../../pkg/xtcpnl/testdata/7_3_0/link-write/kinds/ip6gretap/"
	ip6erspanKindDir = "../../pkg/xtcpnl/testdata/7_3_0/link-write/kinds/ip6erspan/"
)
