package xtcpnl

// Shared constants for the xtcpnl test suite.
//
// goconst would otherwise flag the testdata paths and t.Run sub-test names
// that recur across this package's _test.go files. Centralise them here so
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
	tdReqBytes_6_6_44        = tdBase + "/6_6_44/netlink_sock_diag_request_bytes"
	tdReqBytes2_6_6_44       = tdBase + "/6_6_44/netlink_sock_diag_request_bytes_example2"
	tdReqBytes3_6_6_44       = tdBase + "/6_6_44/netlink_sock_diag_request_bytes_example3"
	tdReqSinglePktV6_6_6_44  = tdBase + "/6_6_44/netlink_sock_diag_request_single_packet_v6.pcap"
	tdReplyPort4001_6_6_44   = tdBase + "/6_6_44/netlink_sock_diag_reply_single_packet_port4001.pcap"
	tdReplyPort4018_6_6_44   = tdBase + "/6_6_44/netlink_sock_diag_reply_single_packet_port4018.pcap"
	tdReplyPort443V4_6_6_44  = tdBase + "/6_6_44/netlink_sock_diag_reply_single_packet_port443v4.pcap"
	tdReplyPort443V6_6_6_44  = tdBase + "/6_6_44/netlink_sock_diag_reply_single_packet_port443v6.pcap"

	// 6.10.3
	tdAttrInfo_6_10_3       = tdBase + "/6_10_3/attribute_info"
	tdAttrBbrinfo_6_10_3    = tdBase + "/6_10_3/attribute_bbrinfo"
	tdAttrSockopt_6_10_3    = tdBase + "/6_10_3/attribute_sockopt_4305"
	tdReplyPort4322_6_10_3  = tdBase + "/6_10_3/netlink_sock_diag_reply_single_packet_port4322.pcap"
	tdRespDumpDone_6_10_3   = tdBase + "/6_10_3/netlink_sock_diag_response_dump_done.pcap"

	// 4.19.319
	tdAttrMeminfo_4_19_319  = tdBase + "/4_19_319/attribute_meminfo_f4096"
	tdReplyPort4005_4_19_319 = tdBase + "/4_19_319/netlink_sock_diag_reply_single_packet_port4005.pcap"

	// 7.0.3
	tdResp26546_7_0_3 = tdBase + "/7_0_3/netlink_sock_diag_response_7_0_3_sport26546_dport443.pcap"
	tdResp19000V6_7_0_3 = tdBase + "/7_0_3/netlink_sock_diag_response_7_0_3_sport19000_dport10156_v6.pcap"

	// Bare testdata/ (no kernel subdir — placeholder fixtures)
	tdAttrPragueinfoFake = tdBase + "/attribute_pragueinfo_fake_fixme"
)
