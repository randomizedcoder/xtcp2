package nlparity

// Shared fixture paths and byte builders for the nlparity test suite.
//
// # Why these paths reach into a sibling package
//
// The parity fixtures this package will eventually own (ip-vs-goip capture
// pairs) are versioned by BOTH kernel and iproute2 version, because the request
// bytes depend on the latter, so they will live under
// pkg/nlparity/testdata/<kernel>/iproute2-<ver>/ and not in the decoder corpus.
//
// The captures below are different: they are the *existing* decoder corpus,
// recorded by `nix run .#capture-netlink-fixtures` for pkg/xtcpnl, and they
// already contain real `ip link show` / `ip route show` request bytes. Copying
// them here would fork a 100 KiB fixture so two packages could disagree about
// it later. Referencing them keeps one copy and one recording provenance.
const (
	tdXtcpnl_7_1_8 = "../xtcpnl/testdata/7_1_8"

	// Bulk nlmon captures: our RTM_GET* dump plus whatever else the host was
	// doing on NETLINK_ROUTE at the time. These are the only fixtures in the
	// repo that contain iproute2's REQUEST bytes — the extracted *_dump.pcap
	// files below keep the replies and throw the request away.
	tdBulkGetLink  = tdXtcpnl_7_1_8 + "/netlink_route_getlink.pcap"
	tdBulkGetAddr  = tdXtcpnl_7_1_8 + "/netlink_route_getaddr.pcap"
	tdBulkGetRoute = tdXtcpnl_7_1_8 + "/netlink_route_getroute.pcap"

	// The extracted, single-datagram reply streams.
	tdGetLinkDump = tdXtcpnl_7_1_8 + "/netlink_route_getlink_dump.pcap"

	// A NETLINK_SOCK_DIAG capture, used to prove the family filter counts what
	// it excludes rather than silently dropping it.
	tdSockDiagReply = "../xtcpnl/testdata/6_6_44/netlink_sock_diag_reply_single_packet_port4001.pcap"
)

// Sub-test names, so a new case for an existing fixture reuses the same name.
const (
	tnLinkShowRequest = "ip_link_show_request"
	tnRouteShowOverse = "ip_route_show_oversend"
	tnMultipartDump   = "multipart_link_dump"
)
