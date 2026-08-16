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
    'c2dJbm9kZRIjCg1tZW1faW5mb19ybWVtGM0IIAEoDVILbWVtSW5mb1JtZW0SIwoNbWVtX2luZm'
    '9fd21lbRjOCCABKA1SC21lbUluZm9XbWVtEiMKDW1lbV9pbmZvX2ZtZW0YzwggASgNUgttZW1J'
    'bmZvRm1lbRIjCg1tZW1faW5mb190bWVtGNAIIAEoDVILbWVtSW5mb1RtZW0SJQoOdGNwX2luZm'
    '9fc3RhdGUYsQkgASgNUgx0Y3BJbmZvU3RhdGUSKgoRdGNwX2luZm9fY2Ffc3RhdGUYsgkgASgN'
    'Ug50Y3BJbmZvQ2FTdGF0ZRIxChR0Y3BfaW5mb19yZXRyYW5zbWl0cxizCSABKA1SEnRjcEluZm'
    '9SZXRyYW5zbWl0cxInCg90Y3BfaW5mb19wcm9iZXMYtAkgASgNUg10Y3BJbmZvUHJvYmVzEikK'
    'EHRjcF9pbmZvX2JhY2tvZmYYtQkgASgNUg50Y3BJbmZvQmFja29mZhIpChB0Y3BfaW5mb19vcH'
    'Rpb25zGLYJIAEoDVIOdGNwSW5mb09wdGlvbnMSLgoTdGNwX2luZm9fc2VuZF9zY2FsZRi3CSAB'
    'KA1SEHRjcEluZm9TZW5kU2NhbGUSLAoSdGNwX2luZm9fcmN2X3NjYWxlGLgJIAEoDVIPdGNwSW'
    '5mb1JjdlNjYWxlEkoKInRjcF9pbmZvX2RlbGl2ZXJ5X3JhdGVfYXBwX2xpbWl0ZWQYuQkgASgN'
    'Uh10Y3BJbmZvRGVsaXZlcnlSYXRlQXBwTGltaXRlZBJGCiB0Y3BfaW5mb19mYXN0X29wZW5fY2'
    'xpZW50X2ZhaWxlZBi6CSABKA1SG3RjcEluZm9GYXN0T3BlbkNsaWVudEZhaWxlZBIhCgx0Y3Bf'
    'aW5mb19ydG8YvwkgASgNUgp0Y3BJbmZvUnRvEiEKDHRjcF9pbmZvX2F0bxjACSABKA1SCnRjcE'
    'luZm9BdG8SKAoQdGNwX2luZm9fc25kX21zcxjBCSABKA1SDXRjcEluZm9TbmRNc3MSKAoQdGNw'
    'X2luZm9fcmN2X21zcxjCCSABKA1SDXRjcEluZm9SY3ZNc3MSKQoQdGNwX2luZm9fdW5hY2tlZB'
    'jDCSABKA1SDnRjcEluZm9VbmFja2VkEicKD3RjcF9pbmZvX3NhY2tlZBjECSABKA1SDXRjcElu'
    'Zm9TYWNrZWQSIwoNdGNwX2luZm9fbG9zdBjFCSABKA1SC3RjcEluZm9Mb3N0EikKEHRjcF9pbm'
    'ZvX3JldHJhbnMYxgkgASgNUg50Y3BJbmZvUmV0cmFucxIpChB0Y3BfaW5mb19mYWNrZXRzGMcJ'
    'IAEoDVIOdGNwSW5mb0ZhY2tldHMSNQoXdGNwX2luZm9fbGFzdF9kYXRhX3NlbnQYyAkgASgNUh'
    'N0Y3BJbmZvTGFzdERhdGFTZW50EjMKFnRjcF9pbmZvX2xhc3RfYWNrX3NlbnQYyQkgASgNUhJ0'
    'Y3BJbmZvTGFzdEFja1NlbnQSNQoXdGNwX2luZm9fbGFzdF9kYXRhX3JlY3YYygkgASgNUhN0Y3'
    'BJbmZvTGFzdERhdGFSZWN2EjMKFnRjcF9pbmZvX2xhc3RfYWNrX3JlY3YYywkgASgNUhJ0Y3BJ'
    'bmZvTGFzdEFja1JlY3YSIwoNdGNwX2luZm9fcG10dRjMCSABKA1SC3RjcEluZm9QbXR1EjIKFX'
    'RjcF9pbmZvX3Jjdl9zc3RocmVzaBjNCSABKA1SEnRjcEluZm9SY3ZTc3RocmVzaBIhCgx0Y3Bf'
    'aW5mb19ydHQYzgkgASgNUgp0Y3BJbmZvUnR0EigKEHRjcF9pbmZvX3J0dF92YXIYzwkgASgNUg'
    '10Y3BJbmZvUnR0VmFyEjIKFXRjcF9pbmZvX3NuZF9zc3RocmVzaBjQCSABKA1SEnRjcEluZm9T'
    'bmRTc3RocmVzaBIqChF0Y3BfaW5mb19zbmRfY3duZBjRCSABKA1SDnRjcEluZm9TbmRDd25kEi'
    'gKEHRjcF9pbmZvX2Fkdl9tc3MY0gkgASgNUg10Y3BJbmZvQWR2TXNzEi8KE3RjcF9pbmZvX3Jl'
    'b3JkZXJpbmcY0wkgASgNUhF0Y3BJbmZvUmVvcmRlcmluZxIoChB0Y3BfaW5mb19yY3ZfcnR0GN'
    'QJIAEoDVINdGNwSW5mb1JjdlJ0dBIsChJ0Y3BfaW5mb19yY3Zfc3BhY2UY1QkgASgNUg90Y3BJ'
    'bmZvUmN2U3BhY2USNAoWdGNwX2luZm9fdG90YWxfcmV0cmFucxjWCSABKA1SE3RjcEluZm9Ub3'
    'RhbFJldHJhbnMSMAoUdGNwX2luZm9fcGFjaW5nX3JhdGUY1wkgASgEUhF0Y3BJbmZvUGFjaW5n'
    'UmF0ZRI3Chh0Y3BfaW5mb19tYXhfcGFjaW5nX3JhdGUY2AkgASgEUhR0Y3BJbmZvTWF4UGFjaW'
    '5nUmF0ZRIwChR0Y3BfaW5mb19ieXRlc19hY2tlZBjZCSABKARSEXRjcEluZm9CeXRlc0Fja2Vk'
    'EjYKF3RjcF9pbmZvX2J5dGVzX3JlY2VpdmVkGNoJIAEoBFIUdGNwSW5mb0J5dGVzUmVjZWl2ZW'
    'QSKgoRdGNwX2luZm9fc2Vnc19vdXQY2wkgASgNUg50Y3BJbmZvU2Vnc091dBIoChB0Y3BfaW5m'
    'b19zZWdzX2luGNwJIAEoDVINdGNwSW5mb1NlZ3NJbhI1Chd0Y3BfaW5mb19ub3Rfc2VudF9ieX'
    'RlcxjdCSABKA1SE3RjcEluZm9Ob3RTZW50Qnl0ZXMSKAoQdGNwX2luZm9fbWluX3J0dBjeCSAB'
    'KA1SDXRjcEluZm9NaW5SdHQSMQoVdGNwX2luZm9fZGF0YV9zZWdzX2luGN8JIAEoDVIRdGNwSW'
    '5mb0RhdGFTZWdzSW4SMwoWdGNwX2luZm9fZGF0YV9zZWdzX291dBjgCSABKA1SEnRjcEluZm9E'
    'YXRhU2Vnc091dBI0ChZ0Y3BfaW5mb19kZWxpdmVyeV9yYXRlGOEJIAEoBFITdGNwSW5mb0RlbG'
    'l2ZXJ5UmF0ZRIsChJ0Y3BfaW5mb19idXN5X3RpbWUY4gkgASgEUg90Y3BJbmZvQnVzeVRpbWUS'
    'MgoVdGNwX2luZm9fcnduZF9saW1pdGVkGOMJIAEoBFISdGNwSW5mb1J3bmRMaW1pdGVkEjYKF3'
    'RjcF9pbmZvX3NuZGJ1Zl9saW1pdGVkGOQJIAEoBFIUdGNwSW5mb1NuZGJ1ZkxpbWl0ZWQSLQoS'
    'dGNwX2luZm9fZGVsaXZlcmVkGOUJIAEoDVIQdGNwSW5mb0RlbGl2ZXJlZBIyChV0Y3BfaW5mb1'
    '9kZWxpdmVyZWRfY2UY5gkgASgNUhJ0Y3BJbmZvRGVsaXZlcmVkQ2USLgoTdGNwX2luZm9fYnl0'
    'ZXNfc2VudBjnCSABKARSEHRjcEluZm9CeXRlc1NlbnQSNAoWdGNwX2luZm9fYnl0ZXNfcmV0cm'
    'FucxjoCSABKARSE3RjcEluZm9CeXRlc1JldHJhbnMSLgoTdGNwX2luZm9fZHNhY2tfZHVwcxjp'
    'CSABKA1SEHRjcEluZm9Ec2Fja0R1cHMSLgoTdGNwX2luZm9fcmVvcmRfc2VlbhjqCSABKA1SEH'
    'RjcEluZm9SZW9yZFNlZW4SMAoUdGNwX2luZm9fcmN2X29vb3BhY2sY6wkgASgNUhF0Y3BJbmZv'
    'UmN2T29vcGFjaxIoChB0Y3BfaW5mb19zbmRfd25kGOwJIAEoDVINdGNwSW5mb1NuZFduZBIoCh'
    'B0Y3BfaW5mb19yY3Zfd25kGO0JIAEoDVINdGNwSW5mb1JjdlduZBInCg90Y3BfaW5mb19yZWhh'
    'c2gY7gkgASgNUg10Y3BJbmZvUmVoYXNoEiwKEnRjcF9pbmZvX3RvdGFsX3J0bxjvCSABKA1SD3'
    'RjcEluZm9Ub3RhbFJ0bxJBCh10Y3BfaW5mb190b3RhbF9ydG9fcmVjb3ZlcmllcxjwCSABKA1S'
    'GXRjcEluZm9Ub3RhbFJ0b1JlY292ZXJpZXMSNQoXdGNwX2luZm9fdG90YWxfcnRvX3RpbWUY8Q'
    'kgASgNUhN0Y3BJbmZvVG90YWxSdG9UaW1lEj8KG2Nvbmdlc3Rpb25fYWxnb3JpdGhtX3N0cmlu'
    'ZxiUCiABKAlSGWNvbmdlc3Rpb25BbGdvcml0aG1TdHJpbmcSdAoZY29uZ2VzdGlvbl9hbGdvcm'
    'l0aG1fZW51bRiVCiABKA4yNy54dGNwX2ZsYXRfcmVjb3JkLnYxLlh0Y3BGbGF0UmVjb3JkLkNv'
    'bmdlc3Rpb25BbGdvcml0aG1SF2Nvbmdlc3Rpb25BbGdvcml0aG1FbnVtEicKD3R5cGVfb2Zfc2'
    'VydmljZRj5CiABKA1SDXR5cGVPZlNlcnZpY2USJAoNdHJhZmZpY19jbGFzcxj6CiABKA1SDHRy'
    'YWZmaWNDbGFzcxIzChZza19tZW1faW5mb19ybWVtX2FsbG9jGN0LIAEoDVISc2tNZW1JbmZvUm'
    '1lbUFsbG9jEi0KE3NrX21lbV9pbmZvX3Jjdl9idWYY3gsgASgNUg9za01lbUluZm9SY3ZCdWYS'
    'MwoWc2tfbWVtX2luZm9fd21lbV9hbGxvYxjfCyABKA1SEnNrTWVtSW5mb1dtZW1BbGxvYxItCh'
    'Nza19tZW1faW5mb19zbmRfYnVmGOALIAEoDVIPc2tNZW1JbmZvU25kQnVmEjEKFXNrX21lbV9p'
    'bmZvX2Z3ZF9hbGxvYxjhCyABKA1SEXNrTWVtSW5mb0Z3ZEFsbG9jEjUKF3NrX21lbV9pbmZvX3'
    'dtZW1fcXVldWVkGOILIAEoDVITc2tNZW1JbmZvV21lbVF1ZXVlZBIsChJza19tZW1faW5mb19v'
    'cHRtZW0Y4wsgASgNUg9za01lbUluZm9PcHRtZW0SLgoTc2tfbWVtX2luZm9fYmFja2xvZxjkCy'
    'ABKA1SEHNrTWVtSW5mb0JhY2tsb2cSKgoRc2tfbWVtX2luZm9fZHJvcHMY5QsgASgNUg5za01l'
    'bUluZm9Ecm9wcxImCg5zaHV0ZG93bl9zdGF0ZRjADCABKA1SDXNodXRkb3duU3RhdGUSLQoSdm'
    'VnYXNfaW5mb19lbmFibGVkGKUNIAEoDVIQdmVnYXNJbmZvRW5hYmxlZBIsChJ2ZWdhc19pbmZv'
    'X3J0dF9jbnQYpg0gASgNUg92ZWdhc0luZm9SdHRDbnQSJQoOdmVnYXNfaW5mb19ydHQYpw0gAS'
    'gNUgx2ZWdhc0luZm9SdHQSLAoSdmVnYXNfaW5mb19taW5fcnR0GKgNIAEoDVIPdmVnYXNJbmZv'
    'TWluUnR0Ei0KEmRjdGNwX2luZm9fZW5hYmxlZBiJDiABKA1SEGRjdGNwSW5mb0VuYWJsZWQSLg'
    'oTZGN0Y3BfaW5mb19jZV9zdGF0ZRiKDiABKA1SEGRjdGNwSW5mb0NlU3RhdGUSKQoQZGN0Y3Bf'
    'aW5mb19hbHBoYRiLDiABKA1SDmRjdGNwSW5mb0FscGhhEioKEWRjdGNwX2luZm9fYWJfZWNuGI'
    'wOIAEoDVIOZGN0Y3BJbmZvQWJFY24SKgoRZGN0Y3BfaW5mb19hYl90b3QYjQ4gASgNUg5kY3Rj'
    'cEluZm9BYlRvdBIkCg5iYnJfaW5mb19id19sbxjtDiABKA1SC2JickluZm9Cd0xvEiQKDmJicl'
    '9pbmZvX2J3X2hpGO4OIAEoDVILYmJySW5mb0J3SGkSKAoQYmJyX2luZm9fbWluX3J0dBjvDiAB'
    'KA1SDWJickluZm9NaW5SdHQSMAoUYmJyX2luZm9fcGFjaW5nX2dhaW4Y8A4gASgNUhFiYnJJbm'
    'ZvUGFjaW5nR2FpbhIsChJiYnJfaW5mb19jd25kX2dhaW4Y8Q4gASgNUg9iYnJJbmZvQ3duZEdh'
    'aW4SGgoIY2xhc3NfaWQY0Q8gASgNUgdjbGFzc0lkEhoKCHNvY2tfb3B0GNIPIAEoDVIHc29ja0'
    '9wdBIYCgdjX2dyb3VwGLcQIAEoBFIGY0dyb3VwIpkCChNDb25nZXN0aW9uQWxnb3JpdGhtEiQK'
    'IENPTkdFU1RJT05fQUxHT1JJVEhNX1VOU1BFQ0lGSUVEEAASHgoaQ09OR0VTVElPTl9BTEdPUk'
    'lUSE1fQ1VCSUMQARIeChpDT05HRVNUSU9OX0FMR09SSVRITV9EQ1RDUBACEh4KGkNPTkdFU1RJ'
    'T05fQUxHT1JJVEhNX1ZFR0FTEAMSHwobQ09OR0VTVElPTl9BTEdPUklUSE1fUFJBR1VFEAQSHQ'
    'oZQ09OR0VTVElPTl9BTEdPUklUSE1fQkJSMRAFEh0KGUNPTkdFU1RJT05fQUxHT1JJVEhNX0JC'
    'UjIQBhIdChlDT05HRVNUSU9OX0FMR09SSVRITV9CQlIzEAc=');

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
