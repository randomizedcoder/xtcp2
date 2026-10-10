package xtcpnl

// Shared constants for the xtcpnl test suite.
//
// goconst would otherwise flag the testdata paths and t.Run sub-test names
// that recur across this package's _test.go files. Centralize them here so
// adding a new test for an existing fixture reuses the same name and any
// path change lands in exactly one place.

// Sub-test names passed to t.Run.
const (
	tnAttrInfo          = "attribute_info"
	tnAttrBbrinfo       = "attribute_bbrinfo"
	tnAttrVegasinfo     = "attribute_vegasinfo"
	tnAttrCgroupID      = "attribute_cgroup_id"
	tnAttrShutdown      = "attribute_shutdown"
	tnAttrSkmeminfo2    = "attribute_skmeminfo2"
	tnAttrSockopt       = "attribute_sockopt"
	tnAttrTcclass       = "attribute_tcclass"
	tnPort4018          = "port4018"
	tnVerifyRequest     = "verify_request"
	tnDeserializePcap   = "DeserializePcapTest"
	tnPadSlow           = "pad slow"
	tnPadFastBranchless = "pad fast/brachless"
	tnMeminfo4_19_319   = "4_19_319_attribute_meminfo"
	tnSport26546V4      = "7_0_3 sport26546 dport443"
	tnSport19000V6      = "7_0_3 sport19000 dport10156 v6"

	// 7.1.8 rtnetlink dump fixtures.
	tnGetLinkDump   = "7_1_8 getlink dump"
	tnGetAddrV4Dump = "7_1_8 getaddr v4 dump"
	tnGetAddrV6Dump = "7_1_8 getaddr v6 dump"
	tnGetRouteDump  = "7_1_8 getroute dump"

	// 7.1.4 rtnetlink event fixtures.
	tnLinkEvents  = "7_1_4 link events"
	tnAddrEvents  = "7_1_4 addr events"
	tnRouteEvents = "7_1_4 route events"
	tnNeighEvents = "7_1_4 neigh events"
)

// Testdata file paths. Grouped by kernel-version subdirectory so a new
// kernel's fixtures fall in next to its peers.
const (
	tdBase = "./testdata"

	// 6.6.44 attributes
	tdAttrInfo_6_6_44       = tdBase + "/6_6_44/attribute_info"
	tdAttrBbrinfo_6_6_44    = tdBase + "/6_6_44/attribute_bbrinfo"
	tdAttrVegasinfo_6_6_44  = tdBase + "/6_6_44/attribute_vegasinfo"
	tdAttrClassID_6_6_44    = tdBase + "/6_6_44/attribute_class_id"
	tdAttrCgroupID_6_6_44   = tdBase + "/6_6_44/attribute_cgroup_id"
	tdAttrDctcpinfo_6_6_44  = tdBase + "/6_6_44/attribute_dctcpinfo_4033"
	tdAttrShutdown_6_6_44   = tdBase + "/6_6_44/attribute_shutdown"
	tdAttrSkmeminfo2_6_6_44 = tdBase + "/6_6_44/attribute_skmeminfo2"
	tdAttrTcclass_6_6_44    = tdBase + "/6_6_44/attribute_tcclass"
	tdAttrTos_6_6_44        = tdBase + "/6_6_44/attribute_tos"
	tdAttrTos2_6_6_44       = tdBase + "/6_6_44/attribute_tos2"

	// 6.6.44 request bytes / single-packet captures
	tdReqBytes_6_6_44       = tdBase + "/6_6_44/netlink_sock_diag_request_bytes"
	tdReqBytes2_6_6_44      = tdBase + "/6_6_44/netlink_sock_diag_request_bytes_example2"
	tdReqBytes3_6_6_44      = tdBase + "/6_6_44/netlink_sock_diag_request_bytes_example3"
	tdReqSinglePktV6_6_6_44 = tdBase + "/6_6_44/netlink_sock_diag_request_single_packet_v6.pcap"
	tdReplyPort4001_6_6_44  = tdBase + "/6_6_44/netlink_sock_diag_reply_single_packet_port4001.pcap"
	tdReplyPort4018_6_6_44  = tdBase + "/6_6_44/netlink_sock_diag_reply_single_packet_port4018.pcap"
	tdReplyPort443V4_6_6_44 = tdBase + "/6_6_44/netlink_sock_diag_reply_single_packet_port443v4.pcap"
	tdReplyPort443V6_6_6_44 = tdBase + "/6_6_44/netlink_sock_diag_reply_single_packet_port443v6.pcap"
	// The second port443v6 capture, and a raw (non-pcap) protocol export of a
	// 448-byte netlink message. Both were previously spelled as literals in
	// the benchmarks, which is what this file exists to avoid.
	tdReplyPort443V6b_6_6_44     = tdBase + "/6_6_44/netlink_sock_diag_reply_single_packet_port443v6_2.pcap"
	tdLargeSockDiagExport_6_6_44 = tdBase + "/6_6_44/large_netlink_sock_diag_protocol_export"

	// 6.10.3
	tdAttrInfo_6_10_3      = tdBase + "/6_10_3/attribute_info"
	tdAttrBbrinfo_6_10_3   = tdBase + "/6_10_3/attribute_bbrinfo"
	tdAttrSockopt_6_10_3   = tdBase + "/6_10_3/attribute_sockopt_4305"
	tdReplyPort4322_6_10_3 = tdBase + "/6_10_3/netlink_sock_diag_reply_single_packet_port4322.pcap"
	tdRespDumpDone_6_10_3  = tdBase + "/6_10_3/netlink_sock_diag_response_dump_done.pcap"

	// 4.19.319
	tdAttrInfo_4_19_319      = tdBase + "/4_19_319/attribute_info"
	tdAttrMeminfo_4_19_319   = tdBase + "/4_19_319/attribute_meminfo_f4096"
	tdReplyPort4005_4_19_319 = tdBase + "/4_19_319/netlink_sock_diag_reply_single_packet_port4005.pcap"

	// 7.0.3
	tdResp26546_7_0_3   = tdBase + "/7_0_3/netlink_sock_diag_response_7_0_3_sport26546_dport443.pcap"
	tdResp19000V6_7_0_3 = tdBase + "/7_0_3/netlink_sock_diag_response_7_0_3_sport19000_dport10156_v6.pcap"

	// The INET_DIAG_INFO attributes extracted from the three 7.0.3 captures:
	// 284 bytes each — a 4-byte nla header plus the 280-byte tcp_info that
	// carries the AccECN trailer (TCPInfo7_0_3). These are the only fixtures
	// in the corpus long enough to exercise deserializeTCPInfoTail7_0.
	tdAttrInfo26546_7_0_3   = tdBase + "/7_0_3/netlink_sock_diag_response_7_0_3_sport26546_dport443_info"
	tdAttrInfo19000V6_7_0_3 = tdBase + "/7_0_3/netlink_sock_diag_response_7_0_3_sport19000_dport10156_v6_info"
	tdAttrInfoRcvRtt_7_0_3  = tdBase + "/7_0_3/netlink_sock_diag_response_7_0_3_sport63282_dport443_rcvrtt_info"

	// 7.1.8 rtnetlink captures (nlmon, NETLINK_ROUTE only).
	//
	// # Why there are two rtnetlink dump corpora, and what belongs in each
	//
	// The 7_1_4/dumps set below is captured in a pinned microVM against a
	// scripted three-device namespace, which makes it the better corpus for
	// almost everything. It did NOT replace this one. Moving the citations
	// here over to it would have deleted three kinds of coverage that only a
	// messy, shared host can provide:
	//
	//   * POLLUTION. netlink_route_getaddr.pcap holds 251 messages across six
	//     distinct portids and 17 sequence numbers, because nlmon mirrors the
	//     whole namespace and other processes were talking to NETLINK_ROUTE at
	//     the time. pkg/nlparity's attribution logic exists to survive exactly
	//     that, and this file is the only real input that tests it. The
	//     namespace captures are clean by construction, so they cannot.
	//   * BREADTH. Eleven links including bonds and bridges, real vendor MACs,
	//     an InfiniBand-length address, truncated IFNAMEs with intact
	//     altnames, SLAAC addresses with finite lifetimes, and a deprecated
	//     temporary address with preferred_lft 0. A namespace built by a
	//     script has whatever the script created and nothing else.
	//   * CROSS-NETNS RELATIONS. The veth here has its peer in another
	//     namespace, so it carries IFLA_LINK_NETNSID and its IFLA_LINK
	//     indexes a device absent from the dump. The mesh pair below is
	//     entirely local. Both cases are real and they render differently.
	//
	// What the 7_1_4/dumps set is for, correspondingly: the attributes this
	// host simply does not have (RTA_MULTIPATH, RTA_VIA, RTA_METRICS,
	// RTNH_F_LINKDOWN), the RTM_GETNEIGH dump the corpus had none of, and the
	// request bytes — because it commits each request next to the replies it
	// provoked, which this set cannot, having thrown the requests away during
	// extraction.
	//
	// So: keep a citation here when the row is about pollution, attribution,
	// device breadth, or a peer in another namespace. Cite 7_1_4/dumps when
	// the row is about a nested route attribute, a neighbor, a request, or a
	// relationship between two local devices.
	//
	// The three *bulk* pcaps are raw per-type nlmon captures produced by
	// `nix run .#capture-netlink-fixtures`; they contain our RTM_GET* dump plus
	// whatever other NETLINK_ROUTE traffic the namespace was doing. The
	// generator (xtcpnl_extract_7_1_8_fixtures_test.go) isolates our dump by
	// (nlmsg_seq, nlmsg_pid) and writes the clean single-record *_dump.pcap
	// fixtures the deserialize tests read.
	tdRouteBulkGetLink_7_1_8  = tdBase + "/7_1_8/netlink_route_getlink.pcap"
	tdRouteBulkGetAddr_7_1_8  = tdBase + "/7_1_8/netlink_route_getaddr.pcap"
	tdRouteBulkGetRoute_7_1_8 = tdBase + "/7_1_8/netlink_route_getroute.pcap"

	tdRouteGetLinkDump_7_1_8   = tdBase + "/7_1_8/netlink_route_getlink_dump.pcap"
	tdRouteGetAddrV4Dump_7_1_8 = tdBase + "/7_1_8/netlink_route_getaddr_v4_dump.pcap"
	tdRouteGetAddrV6Dump_7_1_8 = tdBase + "/7_1_8/netlink_route_getaddr_v6_dump.pcap"
	tdRouteGetRouteDump_7_1_8  = tdBase + "/7_1_8/netlink_route_getroute_dump.pcap"

	// 7.1.4 rtnetlink EVENT captures (nlmon, NETLINK_ROUTE only), produced by
	// `nix run .#microvm-x86_64-nlmon-capture`. 7_1_4 is the microVM's guest
	// kernel, not this host's — the fixture is versioned by the kernel that
	// actually emitted the bytes.
	//
	// The bulk capture is the whole session and is deliberately mixed: `ip`
	// issues an RTM_GET* dump before most subcommands, so solicited replies sit
	// alongside the unsolicited notifications. The generator
	// (xtcpnl_extract_event_fixtures_test.go) keeps only the unsolicited
	// notifications — selected by IsRtnetlinkNotification on nlmsg_flags, not
	// by nlmsg_pid/nlmsg_seq — and splits them per family into the
	// *_events_<family>.pcap fixtures the event tests read.
	tdEventsBulk_7_1_4  = tdBase + "/7_1_4/netlink_route_events.pcap"
	tdEventsLink_7_1_4  = tdBase + "/7_1_4/netlink_route_events_link.pcap"
	tdEventsAddr_7_1_4  = tdBase + "/7_1_4/netlink_route_events_addr.pcap"
	tdEventsRoute_7_1_4 = tdBase + "/7_1_4/netlink_route_events_route.pcap"
	tdEventsNeigh_7_1_4 = tdBase + "/7_1_4/netlink_route_events_neigh.pcap"

	// 7.1.4 rtnetlink DUMP captures, produced in the microVM by
	// `nix run .#microvm-x86_64-netlink-dump-capture` (nix/microvms/
	// netlink-capture.nix plus scripts/capture-netlink-dumps.exp).
	//
	// These live in a `dumps/` subdirectory rather than beside the 7_1_4 event
	// fixtures because five sidecar names collide — ip_addr_n, ip_link_n,
	// ip_neigh_n, ip_route_table_all_n and uname all exist in both sets and
	// describe DIFFERENT topologies. Both sets are cited by line number, so
	// merging them would silently repoint every citation.
	//
	// Unlike the 7_1_8 host captures, each pcap here holds exactly one
	// transaction: the capture ran in a dedicated network namespace whose only
	// interfaces are lo, the nlmon device and one dummy, so there is no other
	// process and no other portid on NETLINK_ROUTE. The topology is recorded
	// step by step in the `topology` sidecar, which is what makes an
	// expectation here reproducible rather than a description of whatever
	// machine happened to run the capture.
	tdDumps_7_1_4 = tdBase + "/7_1_4/dumps"

	tdDumpGetLink_7_1_4     = tdDumps_7_1_4 + "/netlink_route_getlink.pcap"
	tdDumpGetLinkDev_7_1_4  = tdDumps_7_1_4 + "/netlink_route_getlink_dev.pcap"
	tdDumpGetAddr_7_1_4     = tdDumps_7_1_4 + "/netlink_route_getaddr.pcap"
	tdDumpGetAddrV4_7_1_4   = tdDumps_7_1_4 + "/netlink_route_getaddr_v4.pcap"
	tdDumpGetAddrV6_7_1_4   = tdDumps_7_1_4 + "/netlink_route_getaddr_v6.pcap"
	tdDumpGetRoute_7_1_4    = tdDumps_7_1_4 + "/netlink_route_getroute.pcap"
	tdDumpGetRoute6_7_1_4   = tdDumps_7_1_4 + "/netlink_route_getroute6.pcap"
	tdDumpGetRouteAll_7_1_4 = tdDumps_7_1_4 + "/netlink_route_getroute_table_all.pcap"
	tdDumpGetNeigh_7_1_4    = tdDumps_7_1_4 + "/netlink_route_getneigh.pcap"

	// `ip -s link show`. The same 40-byte request as tdDumpGetLink_7_1_4 with
	// one byte changed: IFLA_EXT_MASK is 0x01 rather than 0x09, because -s
	// clears RTEXT_FILTER_SKIP_STATS (ip/ipaddress.c:2017-2026). It is the
	// only capture in the corpus that reaches IFLA_STATS/IFLA_STATS64 through
	// a request that ASKED for them, rather than through a dump that simply
	// carried no mask — which makes it the fixture that pins the request
	// delta, and the two getlink pcaps together are the assertion.
	tdDumpGetLinkStats_7_1_4 = tdDumps_7_1_4 + "/netlink_route_getlink_stats.pcap"

	// `ip rule show` and `ip -6 rule show`. The smallest captures in the
	// corpus — two datagrams each, one request and one multipart reply — and
	// smallest for a structural reason rather than an incidental one:
	// iprule_list_flush_or_save calls no ll_init_map, because FRA_IIFNAME and
	// FRA_OIFNAME travel as strings and there is no index to resolve. Every
	// other dump in this set pays for at least one side transaction.
	//
	// The request is 28 bytes with ZERO attributes: nlmsghdr plus a bare
	// fib_rule_hdr (lib/libnetlink.c:407-421). That is not a stylistic choice
	// by iproute2 — under strict checking the kernel REFUSES a rule dump that
	// carries any attribute at all (net/core/fib_rules.c:1278-1281), so these
	// two pcaps pin the one dump shape in the corpus where an extra attribute
	// is an error rather than an addition.
	//
	// There is deliberately no `-4` pcap. iprule_list_flush_or_save
	// substitutes AF_INET for AF_UNSPEC before building the request
	// (ip/iprule.c:748-752), so `ip rule show` and `ip -4 rule show` emit
	// identical bytes; the committed ip_rule and ip_rule_v4 sidecars are
	// byte-identical for the same reason. A third pcap would assert nothing.
	tdDumpGetRule_7_1_4  = tdDumps_7_1_4 + "/netlink_route_getrule.pcap"
	tdDumpGetRule6_7_1_4 = tdDumps_7_1_4 + "/netlink_route_getrule6.pcap"

	// `ip vrf show`. A filtered link dump: RTM_GETLINK with ifi_family AF_UNSPEC
	// carrying one IFLA_LINKINFO nest holding IFLA_INFO_KIND = "vrf" and NO
	// IFLA_EXT_MASK (ip/ipvrf.c:482-499, ipvrf_filter_req). The kind payload is
	// three bytes with no NUL (addattr_l with strlen), the one shape in the
	// corpus whose request filters a link dump by linkinfo kind. The kernel does
	// not honor the filter, so the reply is a full link dump.
	tdDumpGetVrf_7_1_4 = tdDumps_7_1_4 + "/netlink_route_getvrf.pcap"

	// Sidecars for the dump set: the source of truth its expectations cite.
	tdDumpIPLink_7_1_4   = tdDumps_7_1_4 + "/ip_link_n"
	tdDumpIPAddr_7_1_4   = tdDumps_7_1_4 + "/ip_addr_n"
	tdDumpIPRoute_7_1_4  = tdDumps_7_1_4 + "/ip_route_main_n"
	tdDumpIPRoute6_7_1_4 = tdDumps_7_1_4 + "/ip_route6_n"
	tdDumpIPNeigh_7_1_4  = tdDumps_7_1_4 + "/ip_neigh_n"
	tdDumpIPRule_7_1_4   = tdDumps_7_1_4 + "/ip_rule_n"

	// The three provenance sidecars, and the only files in the corpus that
	// describe the CAPTURE rather than an answer to a command.
	//
	// topology is the transcript of every `ip` command the driver ran to build
	// the namespace, one line each, tagged with the namespace it ran in. uname
	// and ip_version are the kernel and the `ip` that produced everything
	// beside them. All three are claims the rest of the corpus rests on —
	// which kernel answered, which iproute2 rendered, and what was configured
	// — and TestCaptureProvenance is what turns them into assertions.
	tdDumpTopology_7_1_4  = tdDumps_7_1_4 + "/topology"
	tdDumpUname_7_1_4     = tdDumps_7_1_4 + "/uname"
	tdDumpIPVersion_7_1_4 = tdDumps_7_1_4 + "/ip_version"

	// The mesh half of the same capture run: a bridge with a veth member whose
	// peer is left down. It is the only source in the repo of IFLA_MASTER,
	// IFLA_LINK between a real pair, IFLA_INFO_KIND of bridge/veth, `M-DOWN`
	// and RTNH_F_LINKDOWN — states that exist only when devices are related to
	// each other, which a single dummy cannot express.
	//
	// It is advisory, never gated: a bridge and a veth pair generate side
	// transactions (see tdDumpMeshGetLinkDev_7_1_4 below), which is exactly why
	// the clean set exists. Expectations that must be reproducible cite the
	// clean set; expectations about relationships cite this one.
	tdDumpsMesh_7_1_4 = tdDumps_7_1_4 + "/mesh"

	tdDumpMeshGetLink_7_1_4    = tdDumpsMesh_7_1_4 + "/netlink_route_getlink.pcap"
	tdDumpMeshGetLinkDev_7_1_4 = tdDumpsMesh_7_1_4 + "/netlink_route_getlink_dev.pcap"
	tdDumpMeshGetAddr_7_1_4    = tdDumpsMesh_7_1_4 + "/netlink_route_getaddr.pcap"
	tdDumpMeshGetRoute_7_1_4   = tdDumpsMesh_7_1_4 + "/netlink_route_getroute.pcap"
	tdDumpMeshGetNeigh_7_1_4   = tdDumpsMesh_7_1_4 + "/netlink_route_getneigh.pcap"
	tdDumpMeshGetRule_7_1_4    = tdDumpsMesh_7_1_4 + "/netlink_route_getrule.pcap"

	tdDumpMeshIPLink_7_1_4  = tdDumpsMesh_7_1_4 + "/ip_link_n"
	tdDumpMeshIPAddr_7_1_4  = tdDumpsMesh_7_1_4 + "/ip_addr_n"
	tdDumpMeshIPNeigh_7_1_4 = tdDumpsMesh_7_1_4 + "/ip_neigh_n"

	tdDumpMeshTopology_7_1_4 = tdDumpsMesh_7_1_4 + "/topology"

	// The tunnel half of the same capture run: five configured tunnel devices
	// — ipip, sit, gre, ip6tnl, ip6gre — plus the fallback device each of
	// those five modules creates from pernet_operations when it loads.
	//
	// It is the only source in the repo of ll_addr_n2a's SPECIAL cases
	// (lib/ll_addr.c:32-38), where a 4- or 16-byte link-layer address renders
	// as an IP rather than as colon-hex, and the only one of IFLA_LINK
	// present with value 0 — `ip`'s "@NONE" suffix, which every device here
	// carries because a tunnel sits on no underlying interface.
	//
	// # Two things this set cannot promise
	//
	//  1. **The v6 permaddrs are random per boot.** ip6_tunnel and ip6_gre
	//     call eth_random_addr(dev->perm_addr) in their setup
	//     (net/ipv6/ip6_tunnel.c:1913, net/ipv6/ip6_gre.c:1443), so the four
	//     " permaddr …" tokens in tunnel/ip_link change on every capture.
	//     Assert their shape, never their bytes; the bytes are pinned in
	//     internal/goip/render's table instead.
	//  2. **Interface indexes depend on module load order.** The modules are
	//     loaded by the capture driver AFTER the clean and mesh sets are
	//     recorded, precisely so their fallback devices cannot renumber those
	//     namespaces. Cite devices here by name, not by position.
	tdDumpsTunnel_7_1_4 = tdDumps_7_1_4 + "/tunnel"

	tdDumpTunnelGetLink_7_1_4  = tdDumpsTunnel_7_1_4 + "/netlink_route_getlink.pcap"
	tdDumpTunnelGetNeigh_7_1_4 = tdDumpsTunnel_7_1_4 + "/netlink_route_getneigh.pcap"

	// The tunnel namespace's four route dumps. They were predicted to be the
	// one place in the corpus where a non-zero rta_expires could appear — a
	// tunnel route is the kind that can carry a lifetime — and measurement
	// says otherwise: every RTA_CACHEINFO in all four is 32 zero bytes.
	// TestParseNewRouteCacheinfo carries them as the rows that say so.
	tdDumpTunnelGetRoute_7_1_4    = tdDumpsTunnel_7_1_4 + "/netlink_route_getroute.pcap"
	tdDumpTunnelGetRoute6_7_1_4   = tdDumpsTunnel_7_1_4 + "/netlink_route_getroute6.pcap"
	tdDumpTunnelGetRouteDev_7_1_4 = tdDumpsTunnel_7_1_4 + "/netlink_route_getroute_dev.pcap"
	tdDumpTunnelGetRouteAll_7_1_4 = tdDumpsTunnel_7_1_4 + "/netlink_route_getroute_table_all.pcap"
	tdDumpTunnelIPLink_7_1_4      = tdDumpsTunnel_7_1_4 + "/ip_link_n"
	tdDumpTunnelIPNeigh_7_1_4     = tdDumpsTunnel_7_1_4 + "/ip_neigh"
	tdDumpTunnelTopology_7_1_4    = tdDumpsTunnel_7_1_4 + "/topology"

	// Sidecars: the source of truth the event expectations are derived from.
	// ip_monitor_all is the event-side counterpart to ip_link_n — `ip monitor`
	// decoded the same notifications live as they were captured.
	tdEventsMonitor_7_1_4 = tdBase + "/7_1_4/ip_monitor_all"
	tdEventsIPLink_7_1_4  = tdBase + "/7_1_4/ip_link_n"

	// Bare testdata/ (no kernel subdir — placeholder fixtures)
	tdAttrPragueinfoFake = tdBase + "/attribute_pragueinfo_fake_fixme"
)
