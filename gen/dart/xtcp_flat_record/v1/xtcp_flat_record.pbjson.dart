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
      '1': 'inet_diag_msg_socket_dest_asn',
      '3': 1011,
      '4': 1,
      '5': 4,
      '10': 'inetDiagMsgSocketDestAsn'
    },
    {
      '1': 'inet_diag_msg_socket_next_hop_asn',
      '3': 1012,
      '4': 1,
      '5': 4,
      '10': 'inetDiagMsgSocketNextHopAsn'
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
    {
      '1': 'inet_diag_msg_socket_dest_network_owner',
      '3': 1018,
      '4': 1,
      '5': 9,
      '10': 'inetDiagMsgSocketDestNetworkOwner'
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
      '1': 'tcp_info_send_scale',
      '3': 1207,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoSendScale'
    },
    {
      '1': 'tcp_info_rcv_scale',
      '3': 1208,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoRcvScale'
    },
    {
      '1': 'tcp_info_delivery_rate_app_limited',
      '3': 1209,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoDeliveryRateAppLimited'
    },
    {
      '1': 'tcp_info_fast_open_client_failed',
      '3': 1210,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoFastOpenClientFailed'
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
    {
      '1': 'tcp_info_rtt_var',
      '3': 1231,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoRttVar'
    },
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
    {
      '1': 'tcp_info_adv_mss',
      '3': 1234,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoAdvMss'
    },
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
      '1': 'tcp_info_not_sent_bytes',
      '3': 1245,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoNotSentBytes'
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
      '1': 'congestion_algorithm_string',
      '3': 1300,
      '4': 1,
      '5': 9,
      '10': 'congestionAlgorithmString'
    },
    {
      '1': 'congestion_algorithm_enum',
      '3': 1301,
      '4': 1,
      '5': 14,
      '6': '.xtcp_flat_record.v1.XtcpFlatRecord.CongestionAlgorithm',
      '10': 'congestionAlgorithmEnum'
    },
    {'1': 'type_of_service', '3': 1401, '4': 1, '5': 13, '10': 'typeOfService'},
    {'1': 'traffic_class', '3': 1402, '4': 1, '5': 13, '10': 'trafficClass'},
    {
      '1': 'sk_mem_info_rmem_alloc',
      '3': 1501,
      '4': 1,
      '5': 13,
      '10': 'skMemInfoRmemAlloc'
    },
    {
      '1': 'sk_mem_info_rcv_buf',
      '3': 1502,
      '4': 1,
      '5': 13,
      '10': 'skMemInfoRcvBuf'
    },
    {
      '1': 'sk_mem_info_wmem_alloc',
      '3': 1503,
      '4': 1,
      '5': 13,
      '10': 'skMemInfoWmemAlloc'
    },
    {
      '1': 'sk_mem_info_snd_buf',
      '3': 1504,
      '4': 1,
      '5': 13,
      '10': 'skMemInfoSndBuf'
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
    {'1': 'shutdown_state', '3': 1600, '4': 1, '5': 13, '10': 'shutdownState'},
    {
      '1': 'vegas_info_enabled',
      '3': 1701,
      '4': 1,
      '5': 13,
      '10': 'vegasInfoEnabled'
    },
    {
      '1': 'vegas_info_rtt_cnt',
      '3': 1702,
      '4': 1,
      '5': 13,
      '10': 'vegasInfoRttCnt'
    },
    {'1': 'vegas_info_rtt', '3': 1703, '4': 1, '5': 13, '10': 'vegasInfoRtt'},
    {
      '1': 'vegas_info_min_rtt',
      '3': 1704,
      '4': 1,
      '5': 13,
      '10': 'vegasInfoMinRtt'
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
    {'1': 'class_id', '3': 2001, '4': 1, '5': 13, '10': 'classId'},
    {'1': 'sock_opt', '3': 2002, '4': 1, '5': 13, '10': 'sockOpt'},
    {'1': 'c_group', '3': 2103, '4': 1, '5': 4, '10': 'cGroup'},
  ],
  '4': [XtcpFlatRecord_CongestionAlgorithm$json],
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
    'c2NyEjAKFGluZXRfZGlhZ19tc2dfZmFtaWx5GOkHIAEoDVIRaW5ldERpYWdNc2dGYW1pbHkSLg'
    'oTaW5ldF9kaWFnX21zZ19zdGF0ZRjqByABKA1SEGluZXREaWFnTXNnU3RhdGUSLgoTaW5ldF9k'
    'aWFnX21zZ190aW1lchjrByABKA1SEGluZXREaWFnTXNnVGltZXISMgoVaW5ldF9kaWFnX21zZ1'
    '9yZXRyYW5zGOwHIAEoDVISaW5ldERpYWdNc2dSZXRyYW5zEkYKIGluZXRfZGlhZ19tc2dfc29j'
    'a2V0X3NvdXJjZV9wb3J0GO0HIAEoDVIbaW5ldERpYWdNc2dTb2NrZXRTb3VyY2VQb3J0ElAKJW'
    'luZXRfZGlhZ19tc2dfc29ja2V0X2Rlc3RpbmF0aW9uX3BvcnQY7gcgASgNUiBpbmV0RGlhZ01z'
    'Z1NvY2tldERlc3RpbmF0aW9uUG9ydBI9ChtpbmV0X2RpYWdfbXNnX3NvY2tldF9zb3VyY2UY7w'
    'cgASgMUhdpbmV0RGlhZ01zZ1NvY2tldFNvdXJjZRJHCiBpbmV0X2RpYWdfbXNnX3NvY2tldF9k'
    'ZXN0aW5hdGlvbhjwByABKAxSHGluZXREaWFnTXNnU29ja2V0RGVzdGluYXRpb24SQwoeaW5ldF'
    '9kaWFnX21zZ19zb2NrZXRfaW50ZXJmYWNlGPEHIAEoDVIaaW5ldERpYWdNc2dTb2NrZXRJbnRl'
    'cmZhY2USPQobaW5ldF9kaWFnX21zZ19zb2NrZXRfY29va2llGPIHIAEoBFIXaW5ldERpYWdNc2'
    'dTb2NrZXRDb29raWUSQAodaW5ldF9kaWFnX21zZ19zb2NrZXRfZGVzdF9hc24Y8wcgASgEUhhp'
    'bmV0RGlhZ01zZ1NvY2tldERlc3RBc24SRwohaW5ldF9kaWFnX21zZ19zb2NrZXRfbmV4dF9ob3'
    'BfYXNuGPQHIAEoBFIbaW5ldERpYWdNc2dTb2NrZXROZXh0SG9wQXNuEjIKFWluZXRfZGlhZ19t'
    'c2dfZXhwaXJlcxj1ByABKA1SEmluZXREaWFnTXNnRXhwaXJlcxIwChRpbmV0X2RpYWdfbXNnX3'
    'JxdWV1ZRj2ByABKA1SEWluZXREaWFnTXNnUnF1ZXVlEjAKFGluZXRfZGlhZ19tc2dfd3F1ZXVl'
    'GPcHIAEoDVIRaW5ldERpYWdNc2dXcXVldWUSKgoRaW5ldF9kaWFnX21zZ191aWQY+AcgASgNUg'
    '5pbmV0RGlhZ01zZ1VpZBIuChNpbmV0X2RpYWdfbXNnX2lub2RlGPkHIAEoDVIQaW5ldERpYWdN'
    'c2dJbm9kZRJTCidpbmV0X2RpYWdfbXNnX3NvY2tldF9kZXN0X25ldHdvcmtfb3duZXIY+gcgAS'
    'gJUiFpbmV0RGlhZ01zZ1NvY2tldERlc3ROZXR3b3JrT3duZXISIwoNbWVtX2luZm9fcm1lbRjN'
    'CCABKA1SC21lbUluZm9SbWVtEiMKDW1lbV9pbmZvX3dtZW0YzgggASgNUgttZW1JbmZvV21lbR'
    'IjCg1tZW1faW5mb19mbWVtGM8IIAEoDVILbWVtSW5mb0ZtZW0SIwoNbWVtX2luZm9fdG1lbRjQ'
    'CCABKA1SC21lbUluZm9UbWVtEiUKDnRjcF9pbmZvX3N0YXRlGLEJIAEoDVIMdGNwSW5mb1N0YX'
    'RlEioKEXRjcF9pbmZvX2NhX3N0YXRlGLIJIAEoDVIOdGNwSW5mb0NhU3RhdGUSMQoUdGNwX2lu'
    'Zm9fcmV0cmFuc21pdHMYswkgASgNUhJ0Y3BJbmZvUmV0cmFuc21pdHMSJwoPdGNwX2luZm9fcH'
    'JvYmVzGLQJIAEoDVINdGNwSW5mb1Byb2JlcxIpChB0Y3BfaW5mb19iYWNrb2ZmGLUJIAEoDVIO'
    'dGNwSW5mb0JhY2tvZmYSKQoQdGNwX2luZm9fb3B0aW9ucxi2CSABKA1SDnRjcEluZm9PcHRpb2'
    '5zEi4KE3RjcF9pbmZvX3NlbmRfc2NhbGUYtwkgASgNUhB0Y3BJbmZvU2VuZFNjYWxlEiwKEnRj'
    'cF9pbmZvX3Jjdl9zY2FsZRi4CSABKA1SD3RjcEluZm9SY3ZTY2FsZRJKCiJ0Y3BfaW5mb19kZW'
    'xpdmVyeV9yYXRlX2FwcF9saW1pdGVkGLkJIAEoDVIddGNwSW5mb0RlbGl2ZXJ5UmF0ZUFwcExp'
    'bWl0ZWQSRgogdGNwX2luZm9fZmFzdF9vcGVuX2NsaWVudF9mYWlsZWQYugkgASgNUht0Y3BJbm'
    'ZvRmFzdE9wZW5DbGllbnRGYWlsZWQSIQoMdGNwX2luZm9fcnRvGL8JIAEoDVIKdGNwSW5mb1J0'
    'bxIhCgx0Y3BfaW5mb19hdG8YwAkgASgNUgp0Y3BJbmZvQXRvEigKEHRjcF9pbmZvX3NuZF9tc3'
    'MYwQkgASgNUg10Y3BJbmZvU25kTXNzEigKEHRjcF9pbmZvX3Jjdl9tc3MYwgkgASgNUg10Y3BJ'
    'bmZvUmN2TXNzEikKEHRjcF9pbmZvX3VuYWNrZWQYwwkgASgNUg50Y3BJbmZvVW5hY2tlZBInCg'
    '90Y3BfaW5mb19zYWNrZWQYxAkgASgNUg10Y3BJbmZvU2Fja2VkEiMKDXRjcF9pbmZvX2xvc3QY'
    'xQkgASgNUgt0Y3BJbmZvTG9zdBIpChB0Y3BfaW5mb19yZXRyYW5zGMYJIAEoDVIOdGNwSW5mb1'
    'JldHJhbnMSKQoQdGNwX2luZm9fZmFja2V0cxjHCSABKA1SDnRjcEluZm9GYWNrZXRzEjUKF3Rj'
    'cF9pbmZvX2xhc3RfZGF0YV9zZW50GMgJIAEoDVITdGNwSW5mb0xhc3REYXRhU2VudBIzChZ0Y3'
    'BfaW5mb19sYXN0X2Fja19zZW50GMkJIAEoDVISdGNwSW5mb0xhc3RBY2tTZW50EjUKF3RjcF9p'
    'bmZvX2xhc3RfZGF0YV9yZWN2GMoJIAEoDVITdGNwSW5mb0xhc3REYXRhUmVjdhIzChZ0Y3BfaW'
    '5mb19sYXN0X2Fja19yZWN2GMsJIAEoDVISdGNwSW5mb0xhc3RBY2tSZWN2EiMKDXRjcF9pbmZv'
    'X3BtdHUYzAkgASgNUgt0Y3BJbmZvUG10dRIyChV0Y3BfaW5mb19yY3Zfc3N0aHJlc2gYzQkgAS'
    'gNUhJ0Y3BJbmZvUmN2U3N0aHJlc2gSIQoMdGNwX2luZm9fcnR0GM4JIAEoDVIKdGNwSW5mb1J0'
    'dBIoChB0Y3BfaW5mb19ydHRfdmFyGM8JIAEoDVINdGNwSW5mb1J0dFZhchIyChV0Y3BfaW5mb1'
    '9zbmRfc3N0aHJlc2gY0AkgASgNUhJ0Y3BJbmZvU25kU3N0aHJlc2gSKgoRdGNwX2luZm9fc25k'
    'X2N3bmQY0QkgASgNUg50Y3BJbmZvU25kQ3duZBIoChB0Y3BfaW5mb19hZHZfbXNzGNIJIAEoDV'
    'INdGNwSW5mb0Fkdk1zcxIvChN0Y3BfaW5mb19yZW9yZGVyaW5nGNMJIAEoDVIRdGNwSW5mb1Jl'
    'b3JkZXJpbmcSKAoQdGNwX2luZm9fcmN2X3J0dBjUCSABKA1SDXRjcEluZm9SY3ZSdHQSLAoSdG'
    'NwX2luZm9fcmN2X3NwYWNlGNUJIAEoDVIPdGNwSW5mb1JjdlNwYWNlEjQKFnRjcF9pbmZvX3Rv'
    'dGFsX3JldHJhbnMY1gkgASgNUhN0Y3BJbmZvVG90YWxSZXRyYW5zEjAKFHRjcF9pbmZvX3BhY2'
    'luZ19yYXRlGNcJIAEoBFIRdGNwSW5mb1BhY2luZ1JhdGUSNwoYdGNwX2luZm9fbWF4X3BhY2lu'
    'Z19yYXRlGNgJIAEoBFIUdGNwSW5mb01heFBhY2luZ1JhdGUSMAoUdGNwX2luZm9fYnl0ZXNfYW'
    'NrZWQY2QkgASgEUhF0Y3BJbmZvQnl0ZXNBY2tlZBI2Chd0Y3BfaW5mb19ieXRlc19yZWNlaXZl'
    'ZBjaCSABKARSFHRjcEluZm9CeXRlc1JlY2VpdmVkEioKEXRjcF9pbmZvX3NlZ3Nfb3V0GNsJIA'
    'EoDVIOdGNwSW5mb1NlZ3NPdXQSKAoQdGNwX2luZm9fc2Vnc19pbhjcCSABKA1SDXRjcEluZm9T'
    'ZWdzSW4SNQoXdGNwX2luZm9fbm90X3NlbnRfYnl0ZXMY3QkgASgNUhN0Y3BJbmZvTm90U2VudE'
    'J5dGVzEigKEHRjcF9pbmZvX21pbl9ydHQY3gkgASgNUg10Y3BJbmZvTWluUnR0EjEKFXRjcF9p'
    'bmZvX2RhdGFfc2Vnc19pbhjfCSABKA1SEXRjcEluZm9EYXRhU2Vnc0luEjMKFnRjcF9pbmZvX2'
    'RhdGFfc2Vnc19vdXQY4AkgASgNUhJ0Y3BJbmZvRGF0YVNlZ3NPdXQSNAoWdGNwX2luZm9fZGVs'
    'aXZlcnlfcmF0ZRjhCSABKARSE3RjcEluZm9EZWxpdmVyeVJhdGUSLAoSdGNwX2luZm9fYnVzeV'
    '90aW1lGOIJIAEoBFIPdGNwSW5mb0J1c3lUaW1lEjIKFXRjcF9pbmZvX3J3bmRfbGltaXRlZBjj'
    'CSABKARSEnRjcEluZm9Sd25kTGltaXRlZBI2Chd0Y3BfaW5mb19zbmRidWZfbGltaXRlZBjkCS'
    'ABKARSFHRjcEluZm9TbmRidWZMaW1pdGVkEi0KEnRjcF9pbmZvX2RlbGl2ZXJlZBjlCSABKA1S'
    'EHRjcEluZm9EZWxpdmVyZWQSMgoVdGNwX2luZm9fZGVsaXZlcmVkX2NlGOYJIAEoDVISdGNwSW'
    '5mb0RlbGl2ZXJlZENlEi4KE3RjcF9pbmZvX2J5dGVzX3NlbnQY5wkgASgEUhB0Y3BJbmZvQnl0'
    'ZXNTZW50EjQKFnRjcF9pbmZvX2J5dGVzX3JldHJhbnMY6AkgASgEUhN0Y3BJbmZvQnl0ZXNSZX'
    'RyYW5zEi4KE3RjcF9pbmZvX2RzYWNrX2R1cHMY6QkgASgNUhB0Y3BJbmZvRHNhY2tEdXBzEi4K'
    'E3RjcF9pbmZvX3Jlb3JkX3NlZW4Y6gkgASgNUhB0Y3BJbmZvUmVvcmRTZWVuEjAKFHRjcF9pbm'
    'ZvX3Jjdl9vb29wYWNrGOsJIAEoDVIRdGNwSW5mb1Jjdk9vb3BhY2sSKAoQdGNwX2luZm9fc25k'
    'X3duZBjsCSABKA1SDXRjcEluZm9TbmRXbmQSKAoQdGNwX2luZm9fcmN2X3duZBjtCSABKA1SDX'
    'RjcEluZm9SY3ZXbmQSJwoPdGNwX2luZm9fcmVoYXNoGO4JIAEoDVINdGNwSW5mb1JlaGFzaBIs'
    'ChJ0Y3BfaW5mb190b3RhbF9ydG8Y7wkgASgNUg90Y3BJbmZvVG90YWxSdG8SQQoddGNwX2luZm'
    '9fdG90YWxfcnRvX3JlY292ZXJpZXMY8AkgASgNUhl0Y3BJbmZvVG90YWxSdG9SZWNvdmVyaWVz'
    'EjUKF3RjcF9pbmZvX3RvdGFsX3J0b190aW1lGPEJIAEoDVITdGNwSW5mb1RvdGFsUnRvVGltZR'
    'I/Chtjb25nZXN0aW9uX2FsZ29yaXRobV9zdHJpbmcYlAogASgJUhljb25nZXN0aW9uQWxnb3Jp'
    'dGhtU3RyaW5nEnQKGWNvbmdlc3Rpb25fYWxnb3JpdGhtX2VudW0YlQogASgOMjcueHRjcF9mbG'
    'F0X3JlY29yZC52MS5YdGNwRmxhdFJlY29yZC5Db25nZXN0aW9uQWxnb3JpdGhtUhdjb25nZXN0'
    'aW9uQWxnb3JpdGhtRW51bRInCg90eXBlX29mX3NlcnZpY2UY+QogASgNUg10eXBlT2ZTZXJ2aW'
    'NlEiQKDXRyYWZmaWNfY2xhc3MY+gogASgNUgx0cmFmZmljQ2xhc3MSMwoWc2tfbWVtX2luZm9f'
    'cm1lbV9hbGxvYxjdCyABKA1SEnNrTWVtSW5mb1JtZW1BbGxvYxItChNza19tZW1faW5mb19yY3'
    'ZfYnVmGN4LIAEoDVIPc2tNZW1JbmZvUmN2QnVmEjMKFnNrX21lbV9pbmZvX3dtZW1fYWxsb2MY'
    '3wsgASgNUhJza01lbUluZm9XbWVtQWxsb2MSLQoTc2tfbWVtX2luZm9fc25kX2J1ZhjgCyABKA'
    '1SD3NrTWVtSW5mb1NuZEJ1ZhIxChVza19tZW1faW5mb19md2RfYWxsb2MY4QsgASgNUhFza01l'
    'bUluZm9Gd2RBbGxvYxI1Chdza19tZW1faW5mb193bWVtX3F1ZXVlZBjiCyABKA1SE3NrTWVtSW'
    '5mb1dtZW1RdWV1ZWQSLAoSc2tfbWVtX2luZm9fb3B0bWVtGOMLIAEoDVIPc2tNZW1JbmZvT3B0'
    'bWVtEi4KE3NrX21lbV9pbmZvX2JhY2tsb2cY5AsgASgNUhBza01lbUluZm9CYWNrbG9nEioKEX'
    'NrX21lbV9pbmZvX2Ryb3BzGOULIAEoDVIOc2tNZW1JbmZvRHJvcHMSJgoOc2h1dGRvd25fc3Rh'
    'dGUYwAwgASgNUg1zaHV0ZG93blN0YXRlEi0KEnZlZ2FzX2luZm9fZW5hYmxlZBilDSABKA1SEH'
    'ZlZ2FzSW5mb0VuYWJsZWQSLAoSdmVnYXNfaW5mb19ydHRfY250GKYNIAEoDVIPdmVnYXNJbmZv'
    'UnR0Q250EiUKDnZlZ2FzX2luZm9fcnR0GKcNIAEoDVIMdmVnYXNJbmZvUnR0EiwKEnZlZ2FzX2'
    'luZm9fbWluX3J0dBioDSABKA1SD3ZlZ2FzSW5mb01pblJ0dBItChJkY3RjcF9pbmZvX2VuYWJs'
    'ZWQYiQ4gASgNUhBkY3RjcEluZm9FbmFibGVkEi4KE2RjdGNwX2luZm9fY2Vfc3RhdGUYig4gAS'
    'gNUhBkY3RjcEluZm9DZVN0YXRlEikKEGRjdGNwX2luZm9fYWxwaGEYiw4gASgNUg5kY3RjcElu'
    'Zm9BbHBoYRIqChFkY3RjcF9pbmZvX2FiX2VjbhiMDiABKA1SDmRjdGNwSW5mb0FiRWNuEioKEW'
    'RjdGNwX2luZm9fYWJfdG90GI0OIAEoDVIOZGN0Y3BJbmZvQWJUb3QSJAoOYmJyX2luZm9fYndf'
    'bG8Y7Q4gASgNUgtiYnJJbmZvQndMbxIkCg5iYnJfaW5mb19id19oaRjuDiABKA1SC2JickluZm'
    '9Cd0hpEigKEGJicl9pbmZvX21pbl9ydHQY7w4gASgNUg1iYnJJbmZvTWluUnR0EjAKFGJicl9p'
    'bmZvX3BhY2luZ19nYWluGPAOIAEoDVIRYmJySW5mb1BhY2luZ0dhaW4SLAoSYmJyX2luZm9fY3'
    'duZF9nYWluGPEOIAEoDVIPYmJySW5mb0N3bmRHYWluEhoKCGNsYXNzX2lkGNEPIAEoDVIHY2xh'
    'c3NJZBIaCghzb2NrX29wdBjSDyABKA1SB3NvY2tPcHQSGAoHY19ncm91cBi3ECABKARSBmNHcm'
    '91cCKZAgoTQ29uZ2VzdGlvbkFsZ29yaXRobRIkCiBDT05HRVNUSU9OX0FMR09SSVRITV9VTlNQ'
    'RUNJRklFRBAAEh4KGkNPTkdFU1RJT05fQUxHT1JJVEhNX0NVQklDEAESHgoaQ09OR0VTVElPTl'
    '9BTEdPUklUSE1fRENUQ1AQAhIeChpDT05HRVNUSU9OX0FMR09SSVRITV9WRUdBUxADEh8KG0NP'
    'TkdFU1RJT05fQUxHT1JJVEhNX1BSQUdVRRAEEh0KGUNPTkdFU1RJT05fQUxHT1JJVEhNX0JCUj'
    'EQBRIdChlDT05HRVNUSU9OX0FMR09SSVRITV9CQlIyEAYSHQoZQ09OR0VTVElPTl9BTEdPUklU'
    'SE1fQkJSMxAH');

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
