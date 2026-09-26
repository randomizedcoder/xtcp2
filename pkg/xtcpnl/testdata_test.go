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

	// 6.10.3
	tdAttrInfo_6_10_3      = tdBase + "/6_10_3/attribute_info"
	tdAttrBbrinfo_6_10_3   = tdBase + "/6_10_3/attribute_bbrinfo"
	tdAttrSockopt_6_10_3   = tdBase + "/6_10_3/attribute_sockopt_4305"
	tdReplyPort4322_6_10_3 = tdBase + "/6_10_3/netlink_sock_diag_reply_single_packet_port4322.pcap"
	tdRespDumpDone_6_10_3  = tdBase + "/6_10_3/netlink_sock_diag_response_dump_done.pcap"

	// 4.19.319
	tdAttrMeminfo_4_19_319   = tdBase + "/4_19_319/attribute_meminfo_f4096"
	tdReplyPort4005_4_19_319 = tdBase + "/4_19_319/netlink_sock_diag_reply_single_packet_port4005.pcap"

	// 7.0.3
	tdResp26546_7_0_3   = tdBase + "/7_0_3/netlink_sock_diag_response_7_0_3_sport26546_dport443.pcap"
	tdResp19000V6_7_0_3 = tdBase + "/7_0_3/netlink_sock_diag_response_7_0_3_sport19000_dport10156_v6.pcap"

	// 7.1.8 rtnetlink captures (nlmon, NETLINK_ROUTE only).
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

	// Sidecars: the source of truth the event expectations are derived from.
	// ip_monitor_all is the event-side counterpart to ip_link_n — `ip monitor`
	// decoded the same notifications live as they were captured.
	tdEventsMonitor_7_1_4 = tdBase + "/7_1_4/ip_monitor_all"
	tdEventsIPLink_7_1_4  = tdBase + "/7_1_4/ip_link_n"

	// Bare testdata/ (no kernel subdir — placeholder fixtures)
	tdAttrPragueinfoFake = tdBase + "/attribute_pragueinfo_fake_fixme"
)
