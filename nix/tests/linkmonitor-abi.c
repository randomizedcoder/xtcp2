/*
 * nix/tests/linkmonitor-abi.c
 *
 * Prints "<package> <Go constant> <value>" for every kernel size, offset,
 * ioctl number and event value the go-native RDMA code uses. TestABILayout in
 * each package compares these lines with its Go constants. A line whose name
 * starts with '!' is a value the package must never use (IB_USER_MAD_REGISTER_AGENT
 * has an ABI-dependent layout; REGISTER_AGENT2 replaces it).
 *
 * Built by test-linkmonitor-rdma-abi against the flake's linuxHeaders and
 * rdma-core headers.
 */
#include <stddef.h>
#include <stdio.h>
#include <sys/ioctl.h>
#include <infiniband/verbs.h>
#include <rdma/ib_user_mad.h>
#include <rdma/ib_user_verbs.h>
#include <rdma/rdma_user_ioctl.h>
#include <linux/if.h>
#include <linux/if_addr.h>
#include <linux/rtnetlink.h>

#define P(pkg, name, value) printf("%s %s %llu\n", pkg, name, (unsigned long long)(value))
#define X(pkg, name, value) printf("%s %s %#llx\n", pkg, name, (unsigned long long)(value))
#define OFF(pkg, name, type, field) P(pkg, name, offsetof(struct type, field))

int main(void)
{
	P("rdmacaps", "umadHdrLen", sizeof(struct ib_user_mad_hdr));
	OFF("rdmacaps", "umadHdrOffID", ib_user_mad_hdr, id);
	OFF("rdmacaps", "umadHdrOffStatus", ib_user_mad_hdr, status);
	OFF("rdmacaps", "umadHdrOffTimeout", ib_user_mad_hdr, timeout_ms);
	OFF("rdmacaps", "umadHdrOffRetries", ib_user_mad_hdr, retries);
	OFF("rdmacaps", "umadHdrOffLength", ib_user_mad_hdr, length);
	OFF("rdmacaps", "umadHdrOffQPN", ib_user_mad_hdr, qpn);
	OFF("rdmacaps", "umadHdrOffQKey", ib_user_mad_hdr, qkey);
	OFF("rdmacaps", "umadHdrOffLID", ib_user_mad_hdr, lid);
	OFF("rdmacaps", "umadHdrOffSL", ib_user_mad_hdr, sl);
	OFF("rdmacaps", "umadHdrOffPathBits", ib_user_mad_hdr, path_bits);
	OFF("rdmacaps", "umadHdrOffGRHPresent", ib_user_mad_hdr, grh_present);
	OFF("rdmacaps", "umadHdrOffGIDIndex", ib_user_mad_hdr, gid_index);
	OFF("rdmacaps", "umadHdrOffHopLimit", ib_user_mad_hdr, hop_limit);
	OFF("rdmacaps", "umadHdrOffTrafficClass", ib_user_mad_hdr, traffic_class);
	OFF("rdmacaps", "umadHdrOffGID", ib_user_mad_hdr, gid);
	OFF("rdmacaps", "umadHdrOffFlowLabel", ib_user_mad_hdr, flow_label);
	OFF("rdmacaps", "umadHdrOffPKeyIndex", ib_user_mad_hdr, pkey_index);

	P("rdmacaps", "umadRegReq2Len", sizeof(struct ib_user_mad_reg_req2));
	OFF("rdmacaps", "umadRegReq2OffID", ib_user_mad_reg_req2, id);
	OFF("rdmacaps", "umadRegReq2OffQPN", ib_user_mad_reg_req2, qpn);
	OFF("rdmacaps", "umadRegReq2OffMgmtClass", ib_user_mad_reg_req2, mgmt_class);
	OFF("rdmacaps", "umadRegReq2OffVersion", ib_user_mad_reg_req2, mgmt_class_version);
	OFF("rdmacaps", "umadRegReq2OffRes", ib_user_mad_reg_req2, res);
	OFF("rdmacaps", "umadRegReq2OffFlags", ib_user_mad_reg_req2, flags);
	OFF("rdmacaps", "umadRegReq2OffMethodMask", ib_user_mad_reg_req2, method_mask);
	OFF("rdmacaps", "umadRegReq2OffOUI", ib_user_mad_reg_req2, oui);
	OFF("rdmacaps", "umadRegReq2OffRMPP", ib_user_mad_reg_req2, rmpp_version);

	X("rdmacaps", "umadRegisterAgent2", IB_USER_MAD_REGISTER_AGENT2);
	X("rdmacaps", "umadUnregisterAgent", IB_USER_MAD_UNREGISTER_AGENT);
	X("rdmacaps", "!umadRegisterAgent", IB_USER_MAD_REGISTER_AGENT);

	P("rdmanative", "asyncEventDescLen", sizeof(struct ib_uverbs_async_event_desc));
	OFF("rdmanative", "asyncEventDescOffElement", ib_uverbs_async_event_desc, element);
	OFF("rdmanative", "asyncEventDescOffType", ib_uverbs_async_event_desc, event_type);

	P("rdmanative", "cmdHdrLen", sizeof(struct ib_uverbs_cmd_hdr));
	OFF("rdmanative", "cmdHdrOffCommand", ib_uverbs_cmd_hdr, command);
	OFF("rdmanative", "cmdHdrOffInWords", ib_uverbs_cmd_hdr, in_words);
	OFF("rdmanative", "cmdHdrOffOutWords", ib_uverbs_cmd_hdr, out_words);
	P("rdmanative", "cmdGetContext", IB_USER_VERBS_CMD_GET_CONTEXT);

	P("rdmanative", "getContextLen", sizeof(struct ib_uverbs_get_context));
	OFF("rdmanative", "getContextOffResponse", ib_uverbs_get_context, response);
	P("rdmanative", "getContextRespLen", sizeof(struct ib_uverbs_get_context_resp));
	OFF("rdmanative", "getContextRespOffAsyncFD", ib_uverbs_get_context_resp, async_fd);
	OFF("rdmanative", "getContextRespOffNumCompVectors", ib_uverbs_get_context_resp, num_comp_vectors);

	P("rdmanative", "eventDeviceFatal", IBV_EVENT_DEVICE_FATAL);
	P("rdmanative", "eventPortActive", IBV_EVENT_PORT_ACTIVE);
	P("rdmanative", "eventPortErr", IBV_EVENT_PORT_ERR);
	P("rdmanative", "eventLIDChange", IBV_EVENT_LID_CHANGE);
	P("rdmanative", "eventPKeyChange", IBV_EVENT_PKEY_CHANGE);
	P("rdmanative", "eventSMChange", IBV_EVENT_SM_CHANGE);
	P("rdmanative", "eventClientReregister", IBV_EVENT_CLIENT_REREGISTER);
	P("rdmanative", "eventGIDChange", IBV_EVENT_GID_CHANGE);
	P("rdmanative", "eventDeviceSpeedChange", IBV_EVENT_DEVICE_SPEED_CHANGE);

	P("rdmanative", "ifinfomsgLen", sizeof(struct ifinfomsg));
	OFF("rdmanative", "ifinfomsgOffIndex", ifinfomsg, ifi_index);
	OFF("rdmanative", "ifinfomsgOffFlags", ifinfomsg, ifi_flags);
	P("rdmanative", "ifaddrmsgLen", sizeof(struct ifaddrmsg));
	OFF("rdmanative", "ifaddrmsgOffIndex", ifaddrmsg, ifa_index);
	P("rdmanative", "rtmNewLink", RTM_NEWLINK);
	P("rdmanative", "rtmDelLink", RTM_DELLINK);
	P("rdmanative", "rtmNewAddr", RTM_NEWADDR);
	P("rdmanative", "rtmDelAddr", RTM_DELADDR);
	P("rdmanative", "iffRunning", IFF_RUNNING);
	P("rdmanative", "iffLowerUp", IFF_LOWER_UP);
	P("rdmanative", "rtnlGroupLink", RTNLGRP_LINK);
	P("rdmanative", "rtnlGroupIPv4Addr", RTNLGRP_IPV4_IFADDR);
	P("rdmanative", "rtnlGroupIPv6Addr", RTNLGRP_IPV6_IFADDR);
	return 0;
}
