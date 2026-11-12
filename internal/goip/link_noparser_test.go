package goip

// The kinds `ip` has no parser for, and iplink_parse's `vrf` and queue
// keywords (docs/netlink/goip-link-kinds.md, "KG5-T01 The kinds with no
// parser, and the vrf and queue keywords"), against KC6's rows.

// kindsDirCst is KC6's corpus root; a kind's rows are in its subdirectory.
const kindsDirCst = "../../pkg/xtcpnl/testdata/7_3_0/link-write/kinds/"

// noParserKindDir is a kind's corpus directory.
func noParserKindDir(kind string) string { return kindsDirCst + kind + "/" }
