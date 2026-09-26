// This is a generated file - do not edit.
//
// Generated from xtcp_flat_record/v1/xtcp_flat_record.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, prefer_relative_imports
// ignore_for_file: unused_import

import 'dart:convert' as $convert;
import 'dart:core' as $core;
import 'dart:typed_data' as $typed_data;

@$core.Deprecated('Use envelopeDescriptor instead')
const Envelope$json = {
  '1': 'Envelope',
  '2': [
    {
      '1': 'row',
      '3': 10,
      '4': 3,
      '5': 11,
      '6': '.xtcp_flat_record.v1.XtcpFlatRecord',
      '10': 'row'
    },
  ],
};

/// Descriptor for `Envelope`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List envelopeDescriptor = $convert.base64Decode(
    'CghFbnZlbG9wZRI1CgNyb3cYCiADKAsyIy54dGNwX2ZsYXRfcmVjb3JkLnYxLlh0Y3BGbGF0Um'
    'Vjb3JkUgNyb3c=');

@$core.Deprecated('Use xtcpFlatRecordDescriptor instead')
const XtcpFlatRecord$json = {
  '1': 'XtcpFlatRecord',
  '2': [
    {'1': 'schema_version', '3': 1, '4': 1, '5': 13, '10': 'schemaVersion'},
    {'1': 'daemon_version', '3': 2, '4': 1, '5': 9, '10': 'daemonVersion'},
    {'1': 'timestamp_ns', '3': 10, '4': 1, '5': 3, '10': 'timestampNs'},
    {'1': 'hostname', '3': 20, '4': 1, '5': 9, '10': 'hostname'},
    {'1': 'location', '3': 21, '4': 1, '5': 9, '10': 'location'},
    {'1': 'netns', '3': 30, '4': 1, '5': 9, '10': 'netns'},
    {'1': 'netns_inode', '3': 31, '4': 1, '5': 4, '10': 'netnsInode'},
    {'1': 'nsid', '3': 32, '4': 1, '5': 13, '10': 'nsid'},
    {'1': 'container_id', '3': 40, '4': 1, '5': 9, '10': 'containerId'},
    {
      '1': 'container_runtime',
      '3': 41,
      '4': 1,
      '5': 9,
      '10': 'containerRuntime'
    },
    {'1': 'container_name', '3': 42, '4': 1, '5': 9, '10': 'containerName'},
    {'1': 'container_image', '3': 43, '4': 1, '5': 9, '10': 'containerImage'},
    {'1': 'label', '3': 50, '4': 1, '5': 9, '10': 'label'},
    {'1': 'tag', '3': 51, '4': 1, '5': 9, '10': 'tag'},
    {'1': 'record_counter', '3': 60, '4': 1, '5': 4, '10': 'recordCounter'},
    {'1': 'socket_fd', '3': 61, '4': 1, '5': 4, '10': 'socketFd'},
    {'1': 'netlinker_id', '3': 62, '4': 1, '5': 4, '10': 'netlinkerId'},
    {'1': 'uplink1_ifname', '3': 100, '4': 1, '5': 9, '10': 'uplink1Ifname'},
    {
      '1': 'uplink1_nic_driver',
      '3': 101,
      '4': 1,
      '5': 9,
      '10': 'uplink1NicDriver'
    },
    {
      '1': 'uplink1_nic_model',
      '3': 102,
      '4': 1,
      '5': 9,
      '10': 'uplink1NicModel'
    },
    {
      '1': 'uplink1_nic_pci_vendor',
      '3': 103,
      '4': 1,
      '5': 13,
      '10': 'uplink1NicPciVendor'
    },
    {
      '1': 'uplink1_nic_pci_device',
      '3': 104,
      '4': 1,
      '5': 13,
      '10': 'uplink1NicPciDevice'
    },
    {
      '1': 'uplink1_nic_bus_info',
      '3': 105,
      '4': 1,
      '5': 9,
      '10': 'uplink1NicBusInfo'
    },
    {
      '1': 'uplink1_nic_speed_mbps',
      '3': 106,
      '4': 1,
      '5': 13,
      '10': 'uplink1NicSpeedMbps'
    },
    {
      '1': 'uplink1_nic_fw_version',
      '3': 107,
      '4': 1,
      '5': 9,
      '10': 'uplink1NicFwVersion'
    },
    {
      '1': 'uplink1_lldp_chassis_name',
      '3': 120,
      '4': 1,
      '5': 9,
      '10': 'uplink1LldpChassisName'
    },
    {
      '1': 'uplink1_lldp_chassis_id',
      '3': 121,
      '4': 1,
      '5': 9,
      '10': 'uplink1LldpChassisId'
    },
    {
      '1': 'uplink1_lldp_mgmt_ip',
      '3': 122,
      '4': 1,
      '5': 9,
      '10': 'uplink1LldpMgmtIp'
    },
    {
      '1': 'uplink1_lldp_port_id',
      '3': 123,
      '4': 1,
      '5': 9,
      '10': 'uplink1LldpPortId'
    },
    {
      '1': 'uplink1_lldp_port_descr',
      '3': 124,
      '4': 1,
      '5': 9,
      '10': 'uplink1LldpPortDescr'
    },
    {'1': 'uplink2_ifname', '3': 200, '4': 1, '5': 9, '10': 'uplink2Ifname'},
    {
      '1': 'uplink2_nic_driver',
      '3': 201,
      '4': 1,
      '5': 9,
      '10': 'uplink2NicDriver'
    },
    {
      '1': 'uplink2_nic_model',
      '3': 202,
      '4': 1,
      '5': 9,
      '10': 'uplink2NicModel'
    },
    {
      '1': 'uplink2_nic_pci_vendor',
      '3': 203,
      '4': 1,
      '5': 13,
      '10': 'uplink2NicPciVendor'
    },
    {
      '1': 'uplink2_nic_pci_device',
      '3': 204,
      '4': 1,
      '5': 13,
      '10': 'uplink2NicPciDevice'
    },
    {
      '1': 'uplink2_nic_bus_info',
      '3': 205,
      '4': 1,
      '5': 9,
      '10': 'uplink2NicBusInfo'
    },
    {
      '1': 'uplink2_nic_speed_mbps',
      '3': 206,
      '4': 1,
      '5': 13,
      '10': 'uplink2NicSpeedMbps'
    },
    {
      '1': 'uplink2_nic_fw_version',
      '3': 207,
      '4': 1,
      '5': 9,
      '10': 'uplink2NicFwVersion'
    },
    {
      '1': 'uplink2_lldp_chassis_name',
      '3': 220,
      '4': 1,
      '5': 9,
      '10': 'uplink2LldpChassisName'
    },
    {
      '1': 'uplink2_lldp_chassis_id',
      '3': 221,
      '4': 1,
      '5': 9,
      '10': 'uplink2LldpChassisId'
    },
    {
      '1': 'uplink2_lldp_mgmt_ip',
      '3': 222,
      '4': 1,
      '5': 9,
      '10': 'uplink2LldpMgmtIp'
    },
    {
      '1': 'uplink2_lldp_port_id',
      '3': 223,
      '4': 1,
      '5': 9,
      '10': 'uplink2LldpPortId'
    },
    {
      '1': 'uplink2_lldp_port_descr',
      '3': 224,
      '4': 1,
      '5': 9,
      '10': 'uplink2LldpPortDescr'
    },
    {
      '1': 'enrich_socket_interface_name',
      '3': 300,
      '4': 1,
      '5': 9,
      '10': 'enrichSocketInterfaceName'
    },
    {
      '1': 'enrich_socket_dest_locality',
      '3': 310,
      '4': 1,
      '5': 14,
      '6': '.xtcp_flat_record.v1.XtcpFlatRecord.Locality',
      '10': 'enrichSocketDestLocality'
    },
    {
      '1': 'enrich_socket_dest_egress_ifindex',
      '3': 311,
      '4': 1,
      '5': 13,
      '10': 'enrichSocketDestEgressIfindex'
    },
    {
      '1': 'enrich_socket_dest_egress_ifname',
      '3': 312,
      '4': 1,
      '5': 9,
      '10': 'enrichSocketDestEgressIfname'
    },
    {
      '1': 'enrich_socket_dest_asn',
      '3': 320,
      '4': 1,
      '5': 4,
      '10': 'enrichSocketDestAsn'
    },
    {
      '1': 'enrich_socket_dest_next_hop_asn',
      '3': 321,
      '4': 1,
      '5': 4,
      '10': 'enrichSocketDestNextHopAsn'
    },
    {
      '1': 'enrich_socket_dest_network_owner',
      '3': 322,
      '4': 1,
      '5': 9,
      '10': 'enrichSocketDestNetworkOwner'
    },
    {
      '1': 'inet_diag_msg_family',
      '3': 1001,
      '4': 1,
      '5': 13,
      '10': 'inetDiagMsgFamily'
    },
    {
      '1': 'inet_diag_msg_state',
      '3': 1002,
      '4': 1,
      '5': 13,
      '10': 'inetDiagMsgState'
    },
    {
      '1': 'inet_diag_msg_timer',
      '3': 1003,
      '4': 1,
      '5': 13,
      '10': 'inetDiagMsgTimer'
    },
    {
      '1': 'inet_diag_msg_retrans',
      '3': 1004,
      '4': 1,
      '5': 13,
      '10': 'inetDiagMsgRetrans'
    },
    {
      '1': 'inet_diag_msg_socket_source_port',
      '3': 1005,
      '4': 1,
      '5': 13,
      '10': 'inetDiagMsgSocketSourcePort'
    },
    {
      '1': 'inet_diag_msg_socket_destination_port',
      '3': 1006,
      '4': 1,
      '5': 13,
      '10': 'inetDiagMsgSocketDestinationPort'
    },
    {
      '1': 'inet_diag_msg_socket_source',
      '3': 1007,
      '4': 1,
      '5': 12,
      '10': 'inetDiagMsgSocketSource'
    },
    {
      '1': 'inet_diag_msg_socket_destination',
      '3': 1008,
      '4': 1,
      '5': 12,
      '10': 'inetDiagMsgSocketDestination'
    },
    {
      '1': 'inet_diag_msg_socket_interface',
      '3': 1009,
      '4': 1,
      '5': 13,
      '10': 'inetDiagMsgSocketInterface'
    },
    {
      '1': 'inet_diag_msg_socket_cookie',
      '3': 1010,
      '4': 1,
      '5': 4,
      '10': 'inetDiagMsgSocketCookie'
    },
    {
      '1': 'inet_diag_msg_expires',
      '3': 1013,
      '4': 1,
      '5': 13,
      '10': 'inetDiagMsgExpires'
    },
    {
      '1': 'inet_diag_msg_rqueue',
      '3': 1014,
      '4': 1,
      '5': 13,
      '10': 'inetDiagMsgRqueue'
    },
    {
      '1': 'inet_diag_msg_wqueue',
      '3': 1015,
      '4': 1,
      '5': 13,
      '10': 'inetDiagMsgWqueue'
    },
    {
      '1': 'inet_diag_msg_uid',
      '3': 1016,
      '4': 1,
      '5': 13,
      '10': 'inetDiagMsgUid'
    },
    {
      '1': 'inet_diag_msg_inode',
      '3': 1017,
      '4': 1,
      '5': 13,
      '10': 'inetDiagMsgInode'
    },
    {'1': 'mem_info_rmem', '3': 1101, '4': 1, '5': 13, '10': 'memInfoRmem'},
    {'1': 'mem_info_wmem', '3': 1102, '4': 1, '5': 13, '10': 'memInfoWmem'},
    {'1': 'mem_info_fmem', '3': 1103, '4': 1, '5': 13, '10': 'memInfoFmem'},
    {'1': 'mem_info_tmem', '3': 1104, '4': 1, '5': 13, '10': 'memInfoTmem'},
    {'1': 'tcp_info_state', '3': 1201, '4': 1, '5': 13, '10': 'tcpInfoState'},
    {
      '1': 'tcp_info_ca_state',
      '3': 1202,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoCaState'
    },
    {
      '1': 'tcp_info_retransmits',
      '3': 1203,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoRetransmits'
    },
    {'1': 'tcp_info_probes', '3': 1204, '4': 1, '5': 13, '10': 'tcpInfoProbes'},
    {
      '1': 'tcp_info_backoff',
      '3': 1205,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoBackoff'
    },
    {
      '1': 'tcp_info_options',
      '3': 1206,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoOptions'
    },
    {
      '1': 'tcp_info_snd_wscale',
      '3': 1207,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoSndWscale'
    },
    {
      '1': 'tcp_info_rcv_wscale',
      '3': 1208,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoRcvWscale'
    },
    {
      '1': 'tcp_info_delivery_rate_app_limited',
      '3': 1209,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoDeliveryRateAppLimited'
    },
    {
      '1': 'tcp_info_fastopen_client_fail',
      '3': 1210,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoFastopenClientFail'
    },
    {'1': 'tcp_info_rto', '3': 1215, '4': 1, '5': 13, '10': 'tcpInfoRto'},
    {'1': 'tcp_info_ato', '3': 1216, '4': 1, '5': 13, '10': 'tcpInfoAto'},
    {
      '1': 'tcp_info_snd_mss',
      '3': 1217,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoSndMss'
    },
    {
      '1': 'tcp_info_rcv_mss',
      '3': 1218,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoRcvMss'
    },
    {
      '1': 'tcp_info_unacked',
      '3': 1219,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoUnacked'
    },
    {'1': 'tcp_info_sacked', '3': 1220, '4': 1, '5': 13, '10': 'tcpInfoSacked'},
    {'1': 'tcp_info_lost', '3': 1221, '4': 1, '5': 13, '10': 'tcpInfoLost'},
    {
      '1': 'tcp_info_retrans',
      '3': 1222,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoRetrans'
    },
    {
      '1': 'tcp_info_fackets',
      '3': 1223,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoFackets'
    },
    {
      '1': 'tcp_info_last_data_sent',
      '3': 1224,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoLastDataSent'
    },
    {
      '1': 'tcp_info_last_ack_sent',
      '3': 1225,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoLastAckSent'
    },
    {
      '1': 'tcp_info_last_data_recv',
      '3': 1226,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoLastDataRecv'
    },
    {
      '1': 'tcp_info_last_ack_recv',
      '3': 1227,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoLastAckRecv'
    },
    {'1': 'tcp_info_pmtu', '3': 1228, '4': 1, '5': 13, '10': 'tcpInfoPmtu'},
    {
      '1': 'tcp_info_rcv_ssthresh',
      '3': 1229,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoRcvSsthresh'
    },
    {'1': 'tcp_info_rtt', '3': 1230, '4': 1, '5': 13, '10': 'tcpInfoRtt'},
    {'1': 'tcp_info_rttvar', '3': 1231, '4': 1, '5': 13, '10': 'tcpInfoRttvar'},
    {
      '1': 'tcp_info_snd_ssthresh',
      '3': 1232,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoSndSsthresh'
    },
    {
      '1': 'tcp_info_snd_cwnd',
      '3': 1233,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoSndCwnd'
    },
    {'1': 'tcp_info_advmss', '3': 1234, '4': 1, '5': 13, '10': 'tcpInfoAdvmss'},
    {
      '1': 'tcp_info_reordering',
      '3': 1235,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoReordering'
    },
    {
      '1': 'tcp_info_rcv_rtt',
      '3': 1236,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoRcvRtt'
    },
    {
      '1': 'tcp_info_rcv_space',
      '3': 1237,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoRcvSpace'
    },
    {
      '1': 'tcp_info_total_retrans',
      '3': 1238,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoTotalRetrans'
    },
    {
      '1': 'tcp_info_pacing_rate',
      '3': 1239,
      '4': 1,
      '5': 4,
      '10': 'tcpInfoPacingRate'
    },
    {
      '1': 'tcp_info_max_pacing_rate',
      '3': 1240,
      '4': 1,
      '5': 4,
      '10': 'tcpInfoMaxPacingRate'
    },
    {
      '1': 'tcp_info_bytes_acked',
      '3': 1241,
      '4': 1,
      '5': 4,
      '10': 'tcpInfoBytesAcked'
    },
    {
      '1': 'tcp_info_bytes_received',
      '3': 1242,
      '4': 1,
      '5': 4,
      '10': 'tcpInfoBytesReceived'
    },
    {
      '1': 'tcp_info_segs_out',
      '3': 1243,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoSegsOut'
    },
    {
      '1': 'tcp_info_segs_in',
      '3': 1244,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoSegsIn'
    },
    {
      '1': 'tcp_info_notsent_bytes',
      '3': 1245,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoNotsentBytes'
    },
    {
      '1': 'tcp_info_min_rtt',
      '3': 1246,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoMinRtt'
    },
    {
      '1': 'tcp_info_data_segs_in',
      '3': 1247,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoDataSegsIn'
    },
    {
      '1': 'tcp_info_data_segs_out',
      '3': 1248,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoDataSegsOut'
    },
    {
      '1': 'tcp_info_delivery_rate',
      '3': 1249,
      '4': 1,
      '5': 4,
      '10': 'tcpInfoDeliveryRate'
    },
    {
      '1': 'tcp_info_busy_time',
      '3': 1250,
      '4': 1,
      '5': 4,
      '10': 'tcpInfoBusyTime'
    },
    {
      '1': 'tcp_info_rwnd_limited',
      '3': 1251,
      '4': 1,
      '5': 4,
      '10': 'tcpInfoRwndLimited'
    },
    {
      '1': 'tcp_info_sndbuf_limited',
      '3': 1252,
      '4': 1,
      '5': 4,
      '10': 'tcpInfoSndbufLimited'
    },
    {
      '1': 'tcp_info_delivered',
      '3': 1253,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoDelivered'
    },
    {
      '1': 'tcp_info_delivered_ce',
      '3': 1254,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoDeliveredCe'
    },
    {
      '1': 'tcp_info_bytes_sent',
      '3': 1255,
      '4': 1,
      '5': 4,
      '10': 'tcpInfoBytesSent'
    },
    {
      '1': 'tcp_info_bytes_retrans',
      '3': 1256,
      '4': 1,
      '5': 4,
      '10': 'tcpInfoBytesRetrans'
    },
    {
      '1': 'tcp_info_dsack_dups',
      '3': 1257,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoDsackDups'
    },
    {
      '1': 'tcp_info_reord_seen',
      '3': 1258,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoReordSeen'
    },
    {
      '1': 'tcp_info_rcv_ooopack',
      '3': 1259,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoRcvOoopack'
    },
    {
      '1': 'tcp_info_snd_wnd',
      '3': 1260,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoSndWnd'
    },
    {
      '1': 'tcp_info_rcv_wnd',
      '3': 1261,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoRcvWnd'
    },
    {'1': 'tcp_info_rehash', '3': 1262, '4': 1, '5': 13, '10': 'tcpInfoRehash'},
    {
      '1': 'tcp_info_total_rto',
      '3': 1263,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoTotalRto'
    },
    {
      '1': 'tcp_info_total_rto_recoveries',
      '3': 1264,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoTotalRtoRecoveries'
    },
    {
      '1': 'tcp_info_total_rto_time',
      '3': 1265,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoTotalRtoTime'
    },
    {
      '1': 'tcp_info_received_ce',
      '3': 1266,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoReceivedCe'
    },
    {
      '1': 'tcp_info_delivered_e1_bytes',
      '3': 1267,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoDeliveredE1Bytes'
    },
    {
      '1': 'tcp_info_delivered_e0_bytes',
      '3': 1268,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoDeliveredE0Bytes'
    },
    {
      '1': 'tcp_info_delivered_ce_bytes',
      '3': 1269,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoDeliveredCeBytes'
    },
    {
      '1': 'tcp_info_received_e1_bytes',
      '3': 1270,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoReceivedE1Bytes'
    },
    {
      '1': 'tcp_info_received_e0_bytes',
      '3': 1271,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoReceivedE0Bytes'
    },
    {
      '1': 'tcp_info_received_ce_bytes',
      '3': 1272,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoReceivedCeBytes'
    },
    {
      '1': 'tcp_info_ecn_mode',
      '3': 1273,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoEcnMode'
    },
    {
      '1': 'tcp_info_accecn_opt_seen',
      '3': 1274,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoAccecnOptSeen'
    },
    {
      '1': 'tcp_info_accecn_fail_mode',
      '3': 1275,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoAccecnFailMode'
    },
    {
      '1': 'tcp_info_options2',
      '3': 1276,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoOptions2'
    },
    {'1': 'inet_diag_cong', '3': 1300, '4': 1, '5': 9, '10': 'inetDiagCong'},
    {
      '1': 'inet_diag_cong_enum',
      '3': 1301,
      '4': 1,
      '5': 14,
      '6': '.xtcp_flat_record.v1.XtcpFlatRecord.CongestionAlgorithm',
      '10': 'inetDiagCongEnum'
    },
    {'1': 'inet_diag_tos', '3': 1401, '4': 1, '5': 13, '10': 'inetDiagTos'},
    {
      '1': 'inet_diag_tclass',
      '3': 1402,
      '4': 1,
      '5': 13,
      '10': 'inetDiagTclass'
    },
    {
      '1': 'sk_mem_info_rmem_alloc',
      '3': 1501,
      '4': 1,
      '5': 13,
      '10': 'skMemInfoRmemAlloc'
    },
    {
      '1': 'sk_mem_info_rcvbuf',
      '3': 1502,
      '4': 1,
      '5': 13,
      '10': 'skMemInfoRcvbuf'
    },
    {
      '1': 'sk_mem_info_wmem_alloc',
      '3': 1503,
      '4': 1,
      '5': 13,
      '10': 'skMemInfoWmemAlloc'
    },
    {
      '1': 'sk_mem_info_sndbuf',
      '3': 1504,
      '4': 1,
      '5': 13,
      '10': 'skMemInfoSndbuf'
    },
    {
      '1': 'sk_mem_info_fwd_alloc',
      '3': 1505,
      '4': 1,
      '5': 13,
      '10': 'skMemInfoFwdAlloc'
    },
    {
      '1': 'sk_mem_info_wmem_queued',
      '3': 1506,
      '4': 1,
      '5': 13,
      '10': 'skMemInfoWmemQueued'
    },
    {
      '1': 'sk_mem_info_optmem',
      '3': 1507,
      '4': 1,
      '5': 13,
      '10': 'skMemInfoOptmem'
    },
    {
      '1': 'sk_mem_info_backlog',
      '3': 1508,
      '4': 1,
      '5': 13,
      '10': 'skMemInfoBacklog'
    },
    {
      '1': 'sk_mem_info_drops',
      '3': 1509,
      '4': 1,
      '5': 13,
      '10': 'skMemInfoDrops'
    },
    {
      '1': 'inet_diag_shutdown',
      '3': 1600,
      '4': 1,
      '5': 13,
      '10': 'inetDiagShutdown'
    },
    {
      '1': 'vegas_info_enabled',
      '3': 1701,
      '4': 1,
      '5': 13,
      '10': 'vegasInfoEnabled'
    },
    {
      '1': 'vegas_info_rttcnt',
      '3': 1702,
      '4': 1,
      '5': 13,
      '10': 'vegasInfoRttcnt'
    },
    {'1': 'vegas_info_rtt', '3': 1703, '4': 1, '5': 13, '10': 'vegasInfoRtt'},
    {
      '1': 'vegas_info_minrtt',
      '3': 1704,
      '4': 1,
      '5': 13,
      '10': 'vegasInfoMinrtt'
    },
    {
      '1': 'dctcp_info_enabled',
      '3': 1801,
      '4': 1,
      '5': 13,
      '10': 'dctcpInfoEnabled'
    },
    {
      '1': 'dctcp_info_ce_state',
      '3': 1802,
      '4': 1,
      '5': 13,
      '10': 'dctcpInfoCeState'
    },
    {
      '1': 'dctcp_info_alpha',
      '3': 1803,
      '4': 1,
      '5': 13,
      '10': 'dctcpInfoAlpha'
    },
    {
      '1': 'dctcp_info_ab_ecn',
      '3': 1804,
      '4': 1,
      '5': 13,
      '10': 'dctcpInfoAbEcn'
    },
    {
      '1': 'dctcp_info_ab_tot',
      '3': 1805,
      '4': 1,
      '5': 13,
      '10': 'dctcpInfoAbTot'
    },
    {'1': 'bbr_info_bw_lo', '3': 1901, '4': 1, '5': 13, '10': 'bbrInfoBwLo'},
    {'1': 'bbr_info_bw_hi', '3': 1902, '4': 1, '5': 13, '10': 'bbrInfoBwHi'},
    {
      '1': 'bbr_info_min_rtt',
      '3': 1903,
      '4': 1,
      '5': 13,
      '10': 'bbrInfoMinRtt'
    },
    {
      '1': 'bbr_info_pacing_gain',
      '3': 1904,
      '4': 1,
      '5': 13,
      '10': 'bbrInfoPacingGain'
    },
    {
      '1': 'bbr_info_cwnd_gain',
      '3': 1905,
      '4': 1,
      '5': 13,
      '10': 'bbrInfoCwndGain'
    },
    {
      '1': 'inet_diag_class_id',
      '3': 2001,
      '4': 1,
      '5': 13,
      '10': 'inetDiagClassId'
    },
    {
      '1': 'inet_diag_sockopt',
      '3': 2002,
      '4': 1,
      '5': 13,
      '10': 'inetDiagSockopt'
    },
    {
      '1': 'inet_diag_cgroup_id',
      '3': 2003,
      '4': 1,
      '5': 4,
      '10': 'inetDiagCgroupId'
    },
  ],
  '4': [XtcpFlatRecord_Locality$json, XtcpFlatRecord_CongestionAlgorithm$json],
  '9': [
    {'1': 301, '2': 302},
    {'1': 302, '2': 303},
    {'1': 1011, '2': 1012},
    {'1': 1012, '2': 1013},
    {'1': 1018, '2': 1019},
    {'1': 1019, '2': 1020},
    {'1': 2103, '2': 2104},
  ],
  '10': [
    'inet_diag_msg_socket_dest_asn',
    'inet_diag_msg_socket_next_hop_asn',
    'inet_diag_msg_socket_dest_network_owner',
    'inet_diag_msg_socket_dest_locality',
    'enrich_socket_next_hop_asn',
    'tcp_info_send_scale',
    'tcp_info_rcv_scale',
    'tcp_info_fast_open_client_failed',
    'tcp_info_rtt_var',
    'tcp_info_adv_mss',
    'tcp_info_not_sent_bytes',
    'sk_mem_info_rcv_buf',
    'sk_mem_info_snd_buf',
    'vegas_info_rtt_cnt',
    'vegas_info_min_rtt',
    'congestion_algorithm_string',
    'congestion_algorithm_enum',
    'type_of_service',
    'traffic_class',
    'shutdown_state',
    'class_id',
    'sock_opt',
    'c_group'
  ],
};

@$core.Deprecated('Use xtcpFlatRecordDescriptor instead')
const XtcpFlatRecord_Locality$json = {
  '1': 'Locality',
  '2': [
    {'1': 'LOCALITY_UNSPECIFIED', '2': 0},
    {'1': 'LOCALITY_SELF', '2': 1},
    {'1': 'LOCALITY_LOCAL_SUBNET', '2': 2},
    {'1': 'LOCALITY_REMOTE', '2': 3},
  ],
};

@$core.Deprecated('Use xtcpFlatRecordDescriptor instead')
const XtcpFlatRecord_CongestionAlgorithm$json = {
  '1': 'CongestionAlgorithm',
  '2': [
    {'1': 'CONGESTION_ALGORITHM_UNSPECIFIED', '2': 0},
    {'1': 'CONGESTION_ALGORITHM_CUBIC', '2': 1},
    {'1': 'CONGESTION_ALGORITHM_DCTCP', '2': 2},
    {'1': 'CONGESTION_ALGORITHM_VEGAS', '2': 3},
    {'1': 'CONGESTION_ALGORITHM_PRAGUE', '2': 4},
    {'1': 'CONGESTION_ALGORITHM_BBR1', '2': 5},
    {'1': 'CONGESTION_ALGORITHM_BBR2', '2': 6},
    {'1': 'CONGESTION_ALGORITHM_BBR3', '2': 7},
  ],
};

/// Descriptor for `XtcpFlatRecord`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List xtcpFlatRecordDescriptor = $convert.base64Decode(
    'Cg5YdGNwRmxhdFJlY29yZBIlCg5zY2hlbWFfdmVyc2lvbhgBIAEoDVINc2NoZW1hVmVyc2lvbh'
    'IlCg5kYWVtb25fdmVyc2lvbhgCIAEoCVINZGFlbW9uVmVyc2lvbhIhCgx0aW1lc3RhbXBfbnMY'
    'CiABKANSC3RpbWVzdGFtcE5zEhoKCGhvc3RuYW1lGBQgASgJUghob3N0bmFtZRIaCghsb2NhdG'
    'lvbhgVIAEoCVIIbG9jYXRpb24SFAoFbmV0bnMYHiABKAlSBW5ldG5zEh8KC25ldG5zX2lub2Rl'
    'GB8gASgEUgpuZXRuc0lub2RlEhIKBG5zaWQYICABKA1SBG5zaWQSIQoMY29udGFpbmVyX2lkGC'
    'ggASgJUgtjb250YWluZXJJZBIrChFjb250YWluZXJfcnVudGltZRgpIAEoCVIQY29udGFpbmVy'
    'UnVudGltZRIlCg5jb250YWluZXJfbmFtZRgqIAEoCVINY29udGFpbmVyTmFtZRInCg9jb250YW'
    'luZXJfaW1hZ2UYKyABKAlSDmNvbnRhaW5lckltYWdlEhQKBWxhYmVsGDIgASgJUgVsYWJlbBIQ'
    'CgN0YWcYMyABKAlSA3RhZxIlCg5yZWNvcmRfY291bnRlchg8IAEoBFINcmVjb3JkQ291bnRlch'
    'IbCglzb2NrZXRfZmQYPSABKARSCHNvY2tldEZkEiEKDG5ldGxpbmtlcl9pZBg+IAEoBFILbmV0'
    'bGlua2VySWQSJQoOdXBsaW5rMV9pZm5hbWUYZCABKAlSDXVwbGluazFJZm5hbWUSLAoSdXBsaW'
    '5rMV9uaWNfZHJpdmVyGGUgASgJUhB1cGxpbmsxTmljRHJpdmVyEioKEXVwbGluazFfbmljX21v'
    'ZGVsGGYgASgJUg91cGxpbmsxTmljTW9kZWwSMwoWdXBsaW5rMV9uaWNfcGNpX3ZlbmRvchhnIA'
    'EoDVITdXBsaW5rMU5pY1BjaVZlbmRvchIzChZ1cGxpbmsxX25pY19wY2lfZGV2aWNlGGggASgN'
    'UhN1cGxpbmsxTmljUGNpRGV2aWNlEi8KFHVwbGluazFfbmljX2J1c19pbmZvGGkgASgJUhF1cG'
    'xpbmsxTmljQnVzSW5mbxIzChZ1cGxpbmsxX25pY19zcGVlZF9tYnBzGGogASgNUhN1cGxpbmsx'
    'TmljU3BlZWRNYnBzEjMKFnVwbGluazFfbmljX2Z3X3ZlcnNpb24YayABKAlSE3VwbGluazFOaW'
    'NGd1ZlcnNpb24SOQoZdXBsaW5rMV9sbGRwX2NoYXNzaXNfbmFtZRh4IAEoCVIWdXBsaW5rMUxs'
    'ZHBDaGFzc2lzTmFtZRI1Chd1cGxpbmsxX2xsZHBfY2hhc3Npc19pZBh5IAEoCVIUdXBsaW5rMU'
    'xsZHBDaGFzc2lzSWQSLwoUdXBsaW5rMV9sbGRwX21nbXRfaXAYeiABKAlSEXVwbGluazFMbGRw'
    'TWdtdElwEi8KFHVwbGluazFfbGxkcF9wb3J0X2lkGHsgASgJUhF1cGxpbmsxTGxkcFBvcnRJZB'
    'I1Chd1cGxpbmsxX2xsZHBfcG9ydF9kZXNjchh8IAEoCVIUdXBsaW5rMUxsZHBQb3J0RGVzY3IS'
    'JgoOdXBsaW5rMl9pZm5hbWUYyAEgASgJUg11cGxpbmsySWZuYW1lEi0KEnVwbGluazJfbmljX2'
    'RyaXZlchjJASABKAlSEHVwbGluazJOaWNEcml2ZXISKwoRdXBsaW5rMl9uaWNfbW9kZWwYygEg'
    'ASgJUg91cGxpbmsyTmljTW9kZWwSNAoWdXBsaW5rMl9uaWNfcGNpX3ZlbmRvchjLASABKA1SE3'
    'VwbGluazJOaWNQY2lWZW5kb3ISNAoWdXBsaW5rMl9uaWNfcGNpX2RldmljZRjMASABKA1SE3Vw'
    'bGluazJOaWNQY2lEZXZpY2USMAoUdXBsaW5rMl9uaWNfYnVzX2luZm8YzQEgASgJUhF1cGxpbm'
    'syTmljQnVzSW5mbxI0ChZ1cGxpbmsyX25pY19zcGVlZF9tYnBzGM4BIAEoDVITdXBsaW5rMk5p'
    'Y1NwZWVkTWJwcxI0ChZ1cGxpbmsyX25pY19md192ZXJzaW9uGM8BIAEoCVITdXBsaW5rMk5pY0'
    'Z3VmVyc2lvbhI6Chl1cGxpbmsyX2xsZHBfY2hhc3Npc19uYW1lGNwBIAEoCVIWdXBsaW5rMkxs'
    'ZHBDaGFzc2lzTmFtZRI2Chd1cGxpbmsyX2xsZHBfY2hhc3Npc19pZBjdASABKAlSFHVwbGluaz'
    'JMbGRwQ2hhc3Npc0lkEjAKFHVwbGluazJfbGxkcF9tZ210X2lwGN4BIAEoCVIRdXBsaW5rMkxs'
    'ZHBNZ210SXASMAoUdXBsaW5rMl9sbGRwX3BvcnRfaWQY3wEgASgJUhF1cGxpbmsyTGxkcFBvcn'
    'RJZBI2Chd1cGxpbmsyX2xsZHBfcG9ydF9kZXNjchjgASABKAlSFHVwbGluazJMbGRwUG9ydERl'
    'c2NyEkAKHGVucmljaF9zb2NrZXRfaW50ZXJmYWNlX25hbWUYrAIgASgJUhllbnJpY2hTb2NrZX'
    'RJbnRlcmZhY2VOYW1lEmwKG2VucmljaF9zb2NrZXRfZGVzdF9sb2NhbGl0eRi2AiABKA4yLC54'
    'dGNwX2ZsYXRfcmVjb3JkLnYxLlh0Y3BGbGF0UmVjb3JkLkxvY2FsaXR5UhhlbnJpY2hTb2NrZX'
    'REZXN0TG9jYWxpdHkSSQohZW5yaWNoX3NvY2tldF9kZXN0X2VncmVzc19pZmluZGV4GLcCIAEo'
    'DVIdZW5yaWNoU29ja2V0RGVzdEVncmVzc0lmaW5kZXgSRwogZW5yaWNoX3NvY2tldF9kZXN0X2'
    'VncmVzc19pZm5hbWUYuAIgASgJUhxlbnJpY2hTb2NrZXREZXN0RWdyZXNzSWZuYW1lEjQKFmVu'
    'cmljaF9zb2NrZXRfZGVzdF9hc24YwAIgASgEUhNlbnJpY2hTb2NrZXREZXN0QXNuEkQKH2Vucm'
    'ljaF9zb2NrZXRfZGVzdF9uZXh0X2hvcF9hc24YwQIgASgEUhplbnJpY2hTb2NrZXREZXN0TmV4'
    'dEhvcEFzbhJHCiBlbnJpY2hfc29ja2V0X2Rlc3RfbmV0d29ya19vd25lchjCAiABKAlSHGVucm'
    'ljaFNvY2tldERlc3ROZXR3b3JrT3duZXISMAoUaW5ldF9kaWFnX21zZ19mYW1pbHkY6QcgASgN'
    'UhFpbmV0RGlhZ01zZ0ZhbWlseRIuChNpbmV0X2RpYWdfbXNnX3N0YXRlGOoHIAEoDVIQaW5ldE'
    'RpYWdNc2dTdGF0ZRIuChNpbmV0X2RpYWdfbXNnX3RpbWVyGOsHIAEoDVIQaW5ldERpYWdNc2dU'
    'aW1lchIyChVpbmV0X2RpYWdfbXNnX3JldHJhbnMY7AcgASgNUhJpbmV0RGlhZ01zZ1JldHJhbn'
    'MSRgogaW5ldF9kaWFnX21zZ19zb2NrZXRfc291cmNlX3BvcnQY7QcgASgNUhtpbmV0RGlhZ01z'
    'Z1NvY2tldFNvdXJjZVBvcnQSUAolaW5ldF9kaWFnX21zZ19zb2NrZXRfZGVzdGluYXRpb25fcG'
    '9ydBjuByABKA1SIGluZXREaWFnTXNnU29ja2V0RGVzdGluYXRpb25Qb3J0Ej0KG2luZXRfZGlh'
    'Z19tc2dfc29ja2V0X3NvdXJjZRjvByABKAxSF2luZXREaWFnTXNnU29ja2V0U291cmNlEkcKIG'
    'luZXRfZGlhZ19tc2dfc29ja2V0X2Rlc3RpbmF0aW9uGPAHIAEoDFIcaW5ldERpYWdNc2dTb2Nr'
    'ZXREZXN0aW5hdGlvbhJDCh5pbmV0X2RpYWdfbXNnX3NvY2tldF9pbnRlcmZhY2UY8QcgASgNUh'
    'ppbmV0RGlhZ01zZ1NvY2tldEludGVyZmFjZRI9ChtpbmV0X2RpYWdfbXNnX3NvY2tldF9jb29r'
    'aWUY8gcgASgEUhdpbmV0RGlhZ01zZ1NvY2tldENvb2tpZRIyChVpbmV0X2RpYWdfbXNnX2V4cG'
    'lyZXMY9QcgASgNUhJpbmV0RGlhZ01zZ0V4cGlyZXMSMAoUaW5ldF9kaWFnX21zZ19ycXVldWUY'
    '9gcgASgNUhFpbmV0RGlhZ01zZ1JxdWV1ZRIwChRpbmV0X2RpYWdfbXNnX3dxdWV1ZRj3ByABKA'
    '1SEWluZXREaWFnTXNnV3F1ZXVlEioKEWluZXRfZGlhZ19tc2dfdWlkGPgHIAEoDVIOaW5ldERp'
    'YWdNc2dVaWQSLgoTaW5ldF9kaWFnX21zZ19pbm9kZRj5ByABKA1SEGluZXREaWFnTXNnSW5vZG'
    'USIwoNbWVtX2luZm9fcm1lbRjNCCABKA1SC21lbUluZm9SbWVtEiMKDW1lbV9pbmZvX3dtZW0Y'
    'zgggASgNUgttZW1JbmZvV21lbRIjCg1tZW1faW5mb19mbWVtGM8IIAEoDVILbWVtSW5mb0ZtZW'
    '0SIwoNbWVtX2luZm9fdG1lbRjQCCABKA1SC21lbUluZm9UbWVtEiUKDnRjcF9pbmZvX3N0YXRl'
    'GLEJIAEoDVIMdGNwSW5mb1N0YXRlEioKEXRjcF9pbmZvX2NhX3N0YXRlGLIJIAEoDVIOdGNwSW'
    '5mb0NhU3RhdGUSMQoUdGNwX2luZm9fcmV0cmFuc21pdHMYswkgASgNUhJ0Y3BJbmZvUmV0cmFu'
    'c21pdHMSJwoPdGNwX2luZm9fcHJvYmVzGLQJIAEoDVINdGNwSW5mb1Byb2JlcxIpChB0Y3BfaW'
    '5mb19iYWNrb2ZmGLUJIAEoDVIOdGNwSW5mb0JhY2tvZmYSKQoQdGNwX2luZm9fb3B0aW9ucxi2'
    'CSABKA1SDnRjcEluZm9PcHRpb25zEi4KE3RjcF9pbmZvX3NuZF93c2NhbGUYtwkgASgNUhB0Y3'
    'BJbmZvU25kV3NjYWxlEi4KE3RjcF9pbmZvX3Jjdl93c2NhbGUYuAkgASgNUhB0Y3BJbmZvUmN2'
    'V3NjYWxlEkoKInRjcF9pbmZvX2RlbGl2ZXJ5X3JhdGVfYXBwX2xpbWl0ZWQYuQkgASgNUh10Y3'
    'BJbmZvRGVsaXZlcnlSYXRlQXBwTGltaXRlZBJBCh10Y3BfaW5mb19mYXN0b3Blbl9jbGllbnRf'
    'ZmFpbBi6CSABKA1SGXRjcEluZm9GYXN0b3BlbkNsaWVudEZhaWwSIQoMdGNwX2luZm9fcnRvGL'
    '8JIAEoDVIKdGNwSW5mb1J0bxIhCgx0Y3BfaW5mb19hdG8YwAkgASgNUgp0Y3BJbmZvQXRvEigK'
    'EHRjcF9pbmZvX3NuZF9tc3MYwQkgASgNUg10Y3BJbmZvU25kTXNzEigKEHRjcF9pbmZvX3Jjdl'
    '9tc3MYwgkgASgNUg10Y3BJbmZvUmN2TXNzEikKEHRjcF9pbmZvX3VuYWNrZWQYwwkgASgNUg50'
    'Y3BJbmZvVW5hY2tlZBInCg90Y3BfaW5mb19zYWNrZWQYxAkgASgNUg10Y3BJbmZvU2Fja2VkEi'
    'MKDXRjcF9pbmZvX2xvc3QYxQkgASgNUgt0Y3BJbmZvTG9zdBIpChB0Y3BfaW5mb19yZXRyYW5z'
    'GMYJIAEoDVIOdGNwSW5mb1JldHJhbnMSKQoQdGNwX2luZm9fZmFja2V0cxjHCSABKA1SDnRjcE'
    'luZm9GYWNrZXRzEjUKF3RjcF9pbmZvX2xhc3RfZGF0YV9zZW50GMgJIAEoDVITdGNwSW5mb0xh'
    'c3REYXRhU2VudBIzChZ0Y3BfaW5mb19sYXN0X2Fja19zZW50GMkJIAEoDVISdGNwSW5mb0xhc3'
    'RBY2tTZW50EjUKF3RjcF9pbmZvX2xhc3RfZGF0YV9yZWN2GMoJIAEoDVITdGNwSW5mb0xhc3RE'
    'YXRhUmVjdhIzChZ0Y3BfaW5mb19sYXN0X2Fja19yZWN2GMsJIAEoDVISdGNwSW5mb0xhc3RBY2'
    'tSZWN2EiMKDXRjcF9pbmZvX3BtdHUYzAkgASgNUgt0Y3BJbmZvUG10dRIyChV0Y3BfaW5mb19y'
    'Y3Zfc3N0aHJlc2gYzQkgASgNUhJ0Y3BJbmZvUmN2U3N0aHJlc2gSIQoMdGNwX2luZm9fcnR0GM'
    '4JIAEoDVIKdGNwSW5mb1J0dBInCg90Y3BfaW5mb19ydHR2YXIYzwkgASgNUg10Y3BJbmZvUnR0'
    'dmFyEjIKFXRjcF9pbmZvX3NuZF9zc3RocmVzaBjQCSABKA1SEnRjcEluZm9TbmRTc3RocmVzaB'
    'IqChF0Y3BfaW5mb19zbmRfY3duZBjRCSABKA1SDnRjcEluZm9TbmRDd25kEicKD3RjcF9pbmZv'
    'X2Fkdm1zcxjSCSABKA1SDXRjcEluZm9BZHZtc3MSLwoTdGNwX2luZm9fcmVvcmRlcmluZxjTCS'
    'ABKA1SEXRjcEluZm9SZW9yZGVyaW5nEigKEHRjcF9pbmZvX3Jjdl9ydHQY1AkgASgNUg10Y3BJ'
    'bmZvUmN2UnR0EiwKEnRjcF9pbmZvX3Jjdl9zcGFjZRjVCSABKA1SD3RjcEluZm9SY3ZTcGFjZR'
    'I0ChZ0Y3BfaW5mb190b3RhbF9yZXRyYW5zGNYJIAEoDVITdGNwSW5mb1RvdGFsUmV0cmFucxIw'
    'ChR0Y3BfaW5mb19wYWNpbmdfcmF0ZRjXCSABKARSEXRjcEluZm9QYWNpbmdSYXRlEjcKGHRjcF'
    '9pbmZvX21heF9wYWNpbmdfcmF0ZRjYCSABKARSFHRjcEluZm9NYXhQYWNpbmdSYXRlEjAKFHRj'
    'cF9pbmZvX2J5dGVzX2Fja2VkGNkJIAEoBFIRdGNwSW5mb0J5dGVzQWNrZWQSNgoXdGNwX2luZm'
    '9fYnl0ZXNfcmVjZWl2ZWQY2gkgASgEUhR0Y3BJbmZvQnl0ZXNSZWNlaXZlZBIqChF0Y3BfaW5m'
    'b19zZWdzX291dBjbCSABKA1SDnRjcEluZm9TZWdzT3V0EigKEHRjcF9pbmZvX3NlZ3NfaW4Y3A'
    'kgASgNUg10Y3BJbmZvU2Vnc0luEjQKFnRjcF9pbmZvX25vdHNlbnRfYnl0ZXMY3QkgASgNUhN0'
    'Y3BJbmZvTm90c2VudEJ5dGVzEigKEHRjcF9pbmZvX21pbl9ydHQY3gkgASgNUg10Y3BJbmZvTW'
    'luUnR0EjEKFXRjcF9pbmZvX2RhdGFfc2Vnc19pbhjfCSABKA1SEXRjcEluZm9EYXRhU2Vnc0lu'
    'EjMKFnRjcF9pbmZvX2RhdGFfc2Vnc19vdXQY4AkgASgNUhJ0Y3BJbmZvRGF0YVNlZ3NPdXQSNA'
    'oWdGNwX2luZm9fZGVsaXZlcnlfcmF0ZRjhCSABKARSE3RjcEluZm9EZWxpdmVyeVJhdGUSLAoS'
    'dGNwX2luZm9fYnVzeV90aW1lGOIJIAEoBFIPdGNwSW5mb0J1c3lUaW1lEjIKFXRjcF9pbmZvX3'
    'J3bmRfbGltaXRlZBjjCSABKARSEnRjcEluZm9Sd25kTGltaXRlZBI2Chd0Y3BfaW5mb19zbmRi'
    'dWZfbGltaXRlZBjkCSABKARSFHRjcEluZm9TbmRidWZMaW1pdGVkEi0KEnRjcF9pbmZvX2RlbG'
    'l2ZXJlZBjlCSABKA1SEHRjcEluZm9EZWxpdmVyZWQSMgoVdGNwX2luZm9fZGVsaXZlcmVkX2Nl'
    'GOYJIAEoDVISdGNwSW5mb0RlbGl2ZXJlZENlEi4KE3RjcF9pbmZvX2J5dGVzX3NlbnQY5wkgAS'
    'gEUhB0Y3BJbmZvQnl0ZXNTZW50EjQKFnRjcF9pbmZvX2J5dGVzX3JldHJhbnMY6AkgASgEUhN0'
    'Y3BJbmZvQnl0ZXNSZXRyYW5zEi4KE3RjcF9pbmZvX2RzYWNrX2R1cHMY6QkgASgNUhB0Y3BJbm'
    'ZvRHNhY2tEdXBzEi4KE3RjcF9pbmZvX3Jlb3JkX3NlZW4Y6gkgASgNUhB0Y3BJbmZvUmVvcmRT'
    'ZWVuEjAKFHRjcF9pbmZvX3Jjdl9vb29wYWNrGOsJIAEoDVIRdGNwSW5mb1Jjdk9vb3BhY2sSKA'
    'oQdGNwX2luZm9fc25kX3duZBjsCSABKA1SDXRjcEluZm9TbmRXbmQSKAoQdGNwX2luZm9fcmN2'
    'X3duZBjtCSABKA1SDXRjcEluZm9SY3ZXbmQSJwoPdGNwX2luZm9fcmVoYXNoGO4JIAEoDVINdG'
    'NwSW5mb1JlaGFzaBIsChJ0Y3BfaW5mb190b3RhbF9ydG8Y7wkgASgNUg90Y3BJbmZvVG90YWxS'
    'dG8SQQoddGNwX2luZm9fdG90YWxfcnRvX3JlY292ZXJpZXMY8AkgASgNUhl0Y3BJbmZvVG90YW'
    'xSdG9SZWNvdmVyaWVzEjUKF3RjcF9pbmZvX3RvdGFsX3J0b190aW1lGPEJIAEoDVITdGNwSW5m'
    'b1RvdGFsUnRvVGltZRIwChR0Y3BfaW5mb19yZWNlaXZlZF9jZRjyCSABKA1SEXRjcEluZm9SZW'
    'NlaXZlZENlEj0KG3RjcF9pbmZvX2RlbGl2ZXJlZF9lMV9ieXRlcxjzCSABKA1SF3RjcEluZm9E'
    'ZWxpdmVyZWRFMUJ5dGVzEj0KG3RjcF9pbmZvX2RlbGl2ZXJlZF9lMF9ieXRlcxj0CSABKA1SF3'
    'RjcEluZm9EZWxpdmVyZWRFMEJ5dGVzEj0KG3RjcF9pbmZvX2RlbGl2ZXJlZF9jZV9ieXRlcxj1'
    'CSABKA1SF3RjcEluZm9EZWxpdmVyZWRDZUJ5dGVzEjsKGnRjcF9pbmZvX3JlY2VpdmVkX2UxX2'
    'J5dGVzGPYJIAEoDVIWdGNwSW5mb1JlY2VpdmVkRTFCeXRlcxI7Chp0Y3BfaW5mb19yZWNlaXZl'
    'ZF9lMF9ieXRlcxj3CSABKA1SFnRjcEluZm9SZWNlaXZlZEUwQnl0ZXMSOwoadGNwX2luZm9fcm'
    'VjZWl2ZWRfY2VfYnl0ZXMY+AkgASgNUhZ0Y3BJbmZvUmVjZWl2ZWRDZUJ5dGVzEioKEXRjcF9p'
    'bmZvX2Vjbl9tb2RlGPkJIAEoDVIOdGNwSW5mb0Vjbk1vZGUSNwoYdGNwX2luZm9fYWNjZWNuX2'
    '9wdF9zZWVuGPoJIAEoDVIUdGNwSW5mb0FjY2Vjbk9wdFNlZW4SOQoZdGNwX2luZm9fYWNjZWNu'
    'X2ZhaWxfbW9kZRj7CSABKA1SFXRjcEluZm9BY2NlY25GYWlsTW9kZRIrChF0Y3BfaW5mb19vcH'
    'Rpb25zMhj8CSABKA1SD3RjcEluZm9PcHRpb25zMhIlCg5pbmV0X2RpYWdfY29uZxiUCiABKAlS'
    'DGluZXREaWFnQ29uZxJnChNpbmV0X2RpYWdfY29uZ19lbnVtGJUKIAEoDjI3Lnh0Y3BfZmxhdF'
    '9yZWNvcmQudjEuWHRjcEZsYXRSZWNvcmQuQ29uZ2VzdGlvbkFsZ29yaXRobVIQaW5ldERpYWdD'
    'b25nRW51bRIjCg1pbmV0X2RpYWdfdG9zGPkKIAEoDVILaW5ldERpYWdUb3MSKQoQaW5ldF9kaW'
    'FnX3RjbGFzcxj6CiABKA1SDmluZXREaWFnVGNsYXNzEjMKFnNrX21lbV9pbmZvX3JtZW1fYWxs'
    'b2MY3QsgASgNUhJza01lbUluZm9SbWVtQWxsb2MSLAoSc2tfbWVtX2luZm9fcmN2YnVmGN4LIA'
    'EoDVIPc2tNZW1JbmZvUmN2YnVmEjMKFnNrX21lbV9pbmZvX3dtZW1fYWxsb2MY3wsgASgNUhJz'
    'a01lbUluZm9XbWVtQWxsb2MSLAoSc2tfbWVtX2luZm9fc25kYnVmGOALIAEoDVIPc2tNZW1Jbm'
    'ZvU25kYnVmEjEKFXNrX21lbV9pbmZvX2Z3ZF9hbGxvYxjhCyABKA1SEXNrTWVtSW5mb0Z3ZEFs'
    'bG9jEjUKF3NrX21lbV9pbmZvX3dtZW1fcXVldWVkGOILIAEoDVITc2tNZW1JbmZvV21lbVF1ZX'
    'VlZBIsChJza19tZW1faW5mb19vcHRtZW0Y4wsgASgNUg9za01lbUluZm9PcHRtZW0SLgoTc2tf'
    'bWVtX2luZm9fYmFja2xvZxjkCyABKA1SEHNrTWVtSW5mb0JhY2tsb2cSKgoRc2tfbWVtX2luZm'
    '9fZHJvcHMY5QsgASgNUg5za01lbUluZm9Ecm9wcxItChJpbmV0X2RpYWdfc2h1dGRvd24YwAwg'
    'ASgNUhBpbmV0RGlhZ1NodXRkb3duEi0KEnZlZ2FzX2luZm9fZW5hYmxlZBilDSABKA1SEHZlZ2'
    'FzSW5mb0VuYWJsZWQSKwoRdmVnYXNfaW5mb19ydHRjbnQYpg0gASgNUg92ZWdhc0luZm9SdHRj'
    'bnQSJQoOdmVnYXNfaW5mb19ydHQYpw0gASgNUgx2ZWdhc0luZm9SdHQSKwoRdmVnYXNfaW5mb1'
    '9taW5ydHQYqA0gASgNUg92ZWdhc0luZm9NaW5ydHQSLQoSZGN0Y3BfaW5mb19lbmFibGVkGIkO'
    'IAEoDVIQZGN0Y3BJbmZvRW5hYmxlZBIuChNkY3RjcF9pbmZvX2NlX3N0YXRlGIoOIAEoDVIQZG'
    'N0Y3BJbmZvQ2VTdGF0ZRIpChBkY3RjcF9pbmZvX2FscGhhGIsOIAEoDVIOZGN0Y3BJbmZvQWxw'
    'aGESKgoRZGN0Y3BfaW5mb19hYl9lY24YjA4gASgNUg5kY3RjcEluZm9BYkVjbhIqChFkY3RjcF'
    '9pbmZvX2FiX3RvdBiNDiABKA1SDmRjdGNwSW5mb0FiVG90EiQKDmJicl9pbmZvX2J3X2xvGO0O'
    'IAEoDVILYmJySW5mb0J3TG8SJAoOYmJyX2luZm9fYndfaGkY7g4gASgNUgtiYnJJbmZvQndIaR'
    'IoChBiYnJfaW5mb19taW5fcnR0GO8OIAEoDVINYmJySW5mb01pblJ0dBIwChRiYnJfaW5mb19w'
    'YWNpbmdfZ2FpbhjwDiABKA1SEWJickluZm9QYWNpbmdHYWluEiwKEmJicl9pbmZvX2N3bmRfZ2'
    'FpbhjxDiABKA1SD2JickluZm9Dd25kR2FpbhIsChJpbmV0X2RpYWdfY2xhc3NfaWQY0Q8gASgN'
    'Ug9pbmV0RGlhZ0NsYXNzSWQSKwoRaW5ldF9kaWFnX3NvY2tvcHQY0g8gASgNUg9pbmV0RGlhZ1'
    'NvY2tvcHQSLgoTaW5ldF9kaWFnX2Nncm91cF9pZBjTDyABKARSEGluZXREaWFnQ2dyb3VwSWQi'
    'ZwoITG9jYWxpdHkSGAoUTE9DQUxJVFlfVU5TUEVDSUZJRUQQABIRCg1MT0NBTElUWV9TRUxGEA'
    'ESGQoVTE9DQUxJVFlfTE9DQUxfU1VCTkVUEAISEwoPTE9DQUxJVFlfUkVNT1RFEAMimQIKE0Nv'
    'bmdlc3Rpb25BbGdvcml0aG0SJAogQ09OR0VTVElPTl9BTEdPUklUSE1fVU5TUEVDSUZJRUQQAB'
    'IeChpDT05HRVNUSU9OX0FMR09SSVRITV9DVUJJQxABEh4KGkNPTkdFU1RJT05fQUxHT1JJVEhN'
    'X0RDVENQEAISHgoaQ09OR0VTVElPTl9BTEdPUklUSE1fVkVHQVMQAxIfChtDT05HRVNUSU9OX0'
    'FMR09SSVRITV9QUkFHVUUQBBIdChlDT05HRVNUSU9OX0FMR09SSVRITV9CQlIxEAUSHQoZQ09O'
    'R0VTVElPTl9BTEdPUklUSE1fQkJSMhAGEh0KGUNPTkdFU1RJT05fQUxHT1JJVEhNX0JCUjMQB0'
    'oGCK0CEK4CSgYIrgIQrwJKBgjzBxD0B0oGCPQHEPUHSgYI+gcQ+wdKBgj7BxD8B0oGCLcQELgQ'
    'Uh1pbmV0X2RpYWdfbXNnX3NvY2tldF9kZXN0X2FzblIhaW5ldF9kaWFnX21zZ19zb2NrZXRfbm'
    'V4dF9ob3BfYXNuUidpbmV0X2RpYWdfbXNnX3NvY2tldF9kZXN0X25ldHdvcmtfb3duZXJSImlu'
    'ZXRfZGlhZ19tc2dfc29ja2V0X2Rlc3RfbG9jYWxpdHlSGmVucmljaF9zb2NrZXRfbmV4dF9ob3'
    'BfYXNuUhN0Y3BfaW5mb19zZW5kX3NjYWxlUhJ0Y3BfaW5mb19yY3Zfc2NhbGVSIHRjcF9pbmZv'
    'X2Zhc3Rfb3Blbl9jbGllbnRfZmFpbGVkUhB0Y3BfaW5mb19ydHRfdmFyUhB0Y3BfaW5mb19hZH'
    'ZfbXNzUhd0Y3BfaW5mb19ub3Rfc2VudF9ieXRlc1ITc2tfbWVtX2luZm9fcmN2X2J1ZlITc2tf'
    'bWVtX2luZm9fc25kX2J1ZlISdmVnYXNfaW5mb19ydHRfY250UhJ2ZWdhc19pbmZvX21pbl9ydH'
    'RSG2Nvbmdlc3Rpb25fYWxnb3JpdGhtX3N0cmluZ1IZY29uZ2VzdGlvbl9hbGdvcml0aG1fZW51'
    'bVIPdHlwZV9vZl9zZXJ2aWNlUg10cmFmZmljX2NsYXNzUg5zaHV0ZG93bl9zdGF0ZVIIY2xhc3'
    'NfaWRSCHNvY2tfb3B0UgdjX2dyb3Vw');

@$core.Deprecated('Use flatRecordsRequestDescriptor instead')
const FlatRecordsRequest$json = {
  '1': 'FlatRecordsRequest',
};

/// Descriptor for `FlatRecordsRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List flatRecordsRequestDescriptor =
    $convert.base64Decode('ChJGbGF0UmVjb3Jkc1JlcXVlc3Q=');

@$core.Deprecated('Use flatRecordsResponseDescriptor instead')
const FlatRecordsResponse$json = {
  '1': 'FlatRecordsResponse',
  '2': [
    {
      '1': 'xtcp_flat_record',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.xtcp_flat_record.v1.XtcpFlatRecord',
      '10': 'xtcpFlatRecord'
    },
  ],
};

/// Descriptor for `FlatRecordsResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List flatRecordsResponseDescriptor = $convert.base64Decode(
    'ChNGbGF0UmVjb3Jkc1Jlc3BvbnNlEk0KEHh0Y3BfZmxhdF9yZWNvcmQYASABKAsyIy54dGNwX2'
    'ZsYXRfcmVjb3JkLnYxLlh0Y3BGbGF0UmVjb3JkUg54dGNwRmxhdFJlY29yZA==');

@$core.Deprecated('Use pollFlatRecordsRequestDescriptor instead')
const PollFlatRecordsRequest$json = {
  '1': 'PollFlatRecordsRequest',
};

/// Descriptor for `PollFlatRecordsRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List pollFlatRecordsRequestDescriptor =
    $convert.base64Decode('ChZQb2xsRmxhdFJlY29yZHNSZXF1ZXN0');

@$core.Deprecated('Use pollFlatRecordsResponseDescriptor instead')
const PollFlatRecordsResponse$json = {
  '1': 'PollFlatRecordsResponse',
  '2': [
    {
      '1': 'xtcp_flat_record',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.xtcp_flat_record.v1.XtcpFlatRecord',
      '10': 'xtcpFlatRecord'
    },
  ],
};

/// Descriptor for `PollFlatRecordsResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List pollFlatRecordsResponseDescriptor =
    $convert.base64Decode(
        'ChdQb2xsRmxhdFJlY29yZHNSZXNwb25zZRJNChB4dGNwX2ZsYXRfcmVjb3JkGAEgASgLMiMueH'
        'RjcF9mbGF0X3JlY29yZC52MS5YdGNwRmxhdFJlY29yZFIOeHRjcEZsYXRSZWNvcmQ=');
