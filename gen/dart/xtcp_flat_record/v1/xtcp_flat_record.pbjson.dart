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
    {'1': 'agent_id', '3': 22, '4': 1, '5': 9, '10': 'agentId'},
    {'1': 'host_boot_id', '3': 23, '4': 1, '5': 9, '10': 'hostBootId'},
    {'1': 'collection_id', '3': 24, '4': 1, '5': 9, '10': 'collectionId'},
    {
      '1': 'treatment_state_revision',
      '3': 25,
      '4': 1,
      '5': 4,
      '10': 'treatmentStateRevision'
    },
    {
      '1': 'treatment_assignment_id',
      '3': 26,
      '4': 1,
      '5': 9,
      '10': 'treatmentAssignmentId'
    },
    {
      '1': 'treatment_generation',
      '3': 27,
      '4': 1,
      '5': 4,
      '10': 'treatmentGeneration'
    },
    {
      '1': 'treatment_applied_timestamp_ns',
      '3': 28,
      '4': 1,
      '5': 3,
      '10': 'treatmentAppliedTimestampNs'
    },
    {
      '1': 'treatment_algorithm_exposed',
      '3': 29,
      '4': 1,
      '5': 8,
      '10': 'treatmentAlgorithmExposed'
    },
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
    'lvbhgVIAEoCVIIbG9jYXRpb24SGQoIYWdlbnRfaWQYFiABKAlSB2FnZW50SWQSIAoMaG9zdF9i'
    'b290X2lkGBcgASgJUgpob3N0Qm9vdElkEiMKDWNvbGxlY3Rpb25faWQYGCABKAlSDGNvbGxlY3'
    'Rpb25JZBI4Chh0cmVhdG1lbnRfc3RhdGVfcmV2aXNpb24YGSABKARSFnRyZWF0bWVudFN0YXRl'
    'UmV2aXNpb24SNgoXdHJlYXRtZW50X2Fzc2lnbm1lbnRfaWQYGiABKAlSFXRyZWF0bWVudEFzc2'
    'lnbm1lbnRJZBIxChR0cmVhdG1lbnRfZ2VuZXJhdGlvbhgbIAEoBFITdHJlYXRtZW50R2VuZXJh'
    'dGlvbhJDCh50cmVhdG1lbnRfYXBwbGllZF90aW1lc3RhbXBfbnMYHCABKANSG3RyZWF0bWVudE'
    'FwcGxpZWRUaW1lc3RhbXBOcxI+Cht0cmVhdG1lbnRfYWxnb3JpdGhtX2V4cG9zZWQYHSABKAhS'
    'GXRyZWF0bWVudEFsZ29yaXRobUV4cG9zZWQSFAoFbmV0bnMYHiABKAlSBW5ldG5zEh8KC25ldG'
    '5zX2lub2RlGB8gASgEUgpuZXRuc0lub2RlEhIKBG5zaWQYICABKA1SBG5zaWQSIQoMY29udGFp'
    'bmVyX2lkGCggASgJUgtjb250YWluZXJJZBIrChFjb250YWluZXJfcnVudGltZRgpIAEoCVIQY2'
    '9udGFpbmVyUnVudGltZRIlCg5jb250YWluZXJfbmFtZRgqIAEoCVINY29udGFpbmVyTmFtZRIn'
    'Cg9jb250YWluZXJfaW1hZ2UYKyABKAlSDmNvbnRhaW5lckltYWdlEhQKBWxhYmVsGDIgASgJUg'
    'VsYWJlbBIQCgN0YWcYMyABKAlSA3RhZxIlCg5yZWNvcmRfY291bnRlchg8IAEoBFINcmVjb3Jk'
    'Q291bnRlchIbCglzb2NrZXRfZmQYPSABKARSCHNvY2tldEZkEiEKDG5ldGxpbmtlcl9pZBg+IA'
    'EoBFILbmV0bGlua2VySWQSJQoOdXBsaW5rMV9pZm5hbWUYZCABKAlSDXVwbGluazFJZm5hbWUS'
    'LAoSdXBsaW5rMV9uaWNfZHJpdmVyGGUgASgJUhB1cGxpbmsxTmljRHJpdmVyEioKEXVwbGluaz'
    'FfbmljX21vZGVsGGYgASgJUg91cGxpbmsxTmljTW9kZWwSMwoWdXBsaW5rMV9uaWNfcGNpX3Zl'
    'bmRvchhnIAEoDVITdXBsaW5rMU5pY1BjaVZlbmRvchIzChZ1cGxpbmsxX25pY19wY2lfZGV2aW'
    'NlGGggASgNUhN1cGxpbmsxTmljUGNpRGV2aWNlEi8KFHVwbGluazFfbmljX2J1c19pbmZvGGkg'
    'ASgJUhF1cGxpbmsxTmljQnVzSW5mbxIzChZ1cGxpbmsxX25pY19zcGVlZF9tYnBzGGogASgNUh'
    'N1cGxpbmsxTmljU3BlZWRNYnBzEjMKFnVwbGluazFfbmljX2Z3X3ZlcnNpb24YayABKAlSE3Vw'
    'bGluazFOaWNGd1ZlcnNpb24SOQoZdXBsaW5rMV9sbGRwX2NoYXNzaXNfbmFtZRh4IAEoCVIWdX'
    'BsaW5rMUxsZHBDaGFzc2lzTmFtZRI1Chd1cGxpbmsxX2xsZHBfY2hhc3Npc19pZBh5IAEoCVIU'
    'dXBsaW5rMUxsZHBDaGFzc2lzSWQSLwoUdXBsaW5rMV9sbGRwX21nbXRfaXAYeiABKAlSEXVwbG'
    'luazFMbGRwTWdtdElwEi8KFHVwbGluazFfbGxkcF9wb3J0X2lkGHsgASgJUhF1cGxpbmsxTGxk'
    'cFBvcnRJZBI1Chd1cGxpbmsxX2xsZHBfcG9ydF9kZXNjchh8IAEoCVIUdXBsaW5rMUxsZHBQb3'
    'J0RGVzY3ISJgoOdXBsaW5rMl9pZm5hbWUYyAEgASgJUg11cGxpbmsySWZuYW1lEi0KEnVwbGlu'
    'azJfbmljX2RyaXZlchjJASABKAlSEHVwbGluazJOaWNEcml2ZXISKwoRdXBsaW5rMl9uaWNfbW'
    '9kZWwYygEgASgJUg91cGxpbmsyTmljTW9kZWwSNAoWdXBsaW5rMl9uaWNfcGNpX3ZlbmRvchjL'
    'ASABKA1SE3VwbGluazJOaWNQY2lWZW5kb3ISNAoWdXBsaW5rMl9uaWNfcGNpX2RldmljZRjMAS'
    'ABKA1SE3VwbGluazJOaWNQY2lEZXZpY2USMAoUdXBsaW5rMl9uaWNfYnVzX2luZm8YzQEgASgJ'
    'UhF1cGxpbmsyTmljQnVzSW5mbxI0ChZ1cGxpbmsyX25pY19zcGVlZF9tYnBzGM4BIAEoDVITdX'
    'BsaW5rMk5pY1NwZWVkTWJwcxI0ChZ1cGxpbmsyX25pY19md192ZXJzaW9uGM8BIAEoCVITdXBs'
    'aW5rMk5pY0Z3VmVyc2lvbhI6Chl1cGxpbmsyX2xsZHBfY2hhc3Npc19uYW1lGNwBIAEoCVIWdX'
    'BsaW5rMkxsZHBDaGFzc2lzTmFtZRI2Chd1cGxpbmsyX2xsZHBfY2hhc3Npc19pZBjdASABKAlS'
    'FHVwbGluazJMbGRwQ2hhc3Npc0lkEjAKFHVwbGluazJfbGxkcF9tZ210X2lwGN4BIAEoCVIRdX'
    'BsaW5rMkxsZHBNZ210SXASMAoUdXBsaW5rMl9sbGRwX3BvcnRfaWQY3wEgASgJUhF1cGxpbmsy'
    'TGxkcFBvcnRJZBI2Chd1cGxpbmsyX2xsZHBfcG9ydF9kZXNjchjgASABKAlSFHVwbGluazJMbG'
    'RwUG9ydERlc2NyEkAKHGVucmljaF9zb2NrZXRfaW50ZXJmYWNlX25hbWUYrAIgASgJUhllbnJp'
    'Y2hTb2NrZXRJbnRlcmZhY2VOYW1lEmwKG2VucmljaF9zb2NrZXRfZGVzdF9sb2NhbGl0eRi2Ai'
    'ABKA4yLC54dGNwX2ZsYXRfcmVjb3JkLnYxLlh0Y3BGbGF0UmVjb3JkLkxvY2FsaXR5UhhlbnJp'
    'Y2hTb2NrZXREZXN0TG9jYWxpdHkSSQohZW5yaWNoX3NvY2tldF9kZXN0X2VncmVzc19pZmluZG'
    'V4GLcCIAEoDVIdZW5yaWNoU29ja2V0RGVzdEVncmVzc0lmaW5kZXgSRwogZW5yaWNoX3NvY2tl'
    'dF9kZXN0X2VncmVzc19pZm5hbWUYuAIgASgJUhxlbnJpY2hTb2NrZXREZXN0RWdyZXNzSWZuYW'
    '1lEjQKFmVucmljaF9zb2NrZXRfZGVzdF9hc24YwAIgASgEUhNlbnJpY2hTb2NrZXREZXN0QXNu'
    'EkQKH2VucmljaF9zb2NrZXRfZGVzdF9uZXh0X2hvcF9hc24YwQIgASgEUhplbnJpY2hTb2NrZX'
    'REZXN0TmV4dEhvcEFzbhJHCiBlbnJpY2hfc29ja2V0X2Rlc3RfbmV0d29ya19vd25lchjCAiAB'
    'KAlSHGVucmljaFNvY2tldERlc3ROZXR3b3JrT3duZXISMAoUaW5ldF9kaWFnX21zZ19mYW1pbH'
    'kY6QcgASgNUhFpbmV0RGlhZ01zZ0ZhbWlseRIuChNpbmV0X2RpYWdfbXNnX3N0YXRlGOoHIAEo'
    'DVIQaW5ldERpYWdNc2dTdGF0ZRIuChNpbmV0X2RpYWdfbXNnX3RpbWVyGOsHIAEoDVIQaW5ldE'
    'RpYWdNc2dUaW1lchIyChVpbmV0X2RpYWdfbXNnX3JldHJhbnMY7AcgASgNUhJpbmV0RGlhZ01z'
    'Z1JldHJhbnMSRgogaW5ldF9kaWFnX21zZ19zb2NrZXRfc291cmNlX3BvcnQY7QcgASgNUhtpbm'
    'V0RGlhZ01zZ1NvY2tldFNvdXJjZVBvcnQSUAolaW5ldF9kaWFnX21zZ19zb2NrZXRfZGVzdGlu'
    'YXRpb25fcG9ydBjuByABKA1SIGluZXREaWFnTXNnU29ja2V0RGVzdGluYXRpb25Qb3J0Ej0KG2'
    'luZXRfZGlhZ19tc2dfc29ja2V0X3NvdXJjZRjvByABKAxSF2luZXREaWFnTXNnU29ja2V0U291'
    'cmNlEkcKIGluZXRfZGlhZ19tc2dfc29ja2V0X2Rlc3RpbmF0aW9uGPAHIAEoDFIcaW5ldERpYW'
    'dNc2dTb2NrZXREZXN0aW5hdGlvbhJDCh5pbmV0X2RpYWdfbXNnX3NvY2tldF9pbnRlcmZhY2UY'
    '8QcgASgNUhppbmV0RGlhZ01zZ1NvY2tldEludGVyZmFjZRI9ChtpbmV0X2RpYWdfbXNnX3NvY2'
    'tldF9jb29raWUY8gcgASgEUhdpbmV0RGlhZ01zZ1NvY2tldENvb2tpZRIyChVpbmV0X2RpYWdf'
    'bXNnX2V4cGlyZXMY9QcgASgNUhJpbmV0RGlhZ01zZ0V4cGlyZXMSMAoUaW5ldF9kaWFnX21zZ1'
    '9ycXVldWUY9gcgASgNUhFpbmV0RGlhZ01zZ1JxdWV1ZRIwChRpbmV0X2RpYWdfbXNnX3dxdWV1'
    'ZRj3ByABKA1SEWluZXREaWFnTXNnV3F1ZXVlEioKEWluZXRfZGlhZ19tc2dfdWlkGPgHIAEoDV'
    'IOaW5ldERpYWdNc2dVaWQSLgoTaW5ldF9kaWFnX21zZ19pbm9kZRj5ByABKA1SEGluZXREaWFn'
    'TXNnSW5vZGUSIwoNbWVtX2luZm9fcm1lbRjNCCABKA1SC21lbUluZm9SbWVtEiMKDW1lbV9pbm'
    'ZvX3dtZW0YzgggASgNUgttZW1JbmZvV21lbRIjCg1tZW1faW5mb19mbWVtGM8IIAEoDVILbWVt'
    'SW5mb0ZtZW0SIwoNbWVtX2luZm9fdG1lbRjQCCABKA1SC21lbUluZm9UbWVtEiUKDnRjcF9pbm'
    'ZvX3N0YXRlGLEJIAEoDVIMdGNwSW5mb1N0YXRlEioKEXRjcF9pbmZvX2NhX3N0YXRlGLIJIAEo'
    'DVIOdGNwSW5mb0NhU3RhdGUSMQoUdGNwX2luZm9fcmV0cmFuc21pdHMYswkgASgNUhJ0Y3BJbm'
    'ZvUmV0cmFuc21pdHMSJwoPdGNwX2luZm9fcHJvYmVzGLQJIAEoDVINdGNwSW5mb1Byb2JlcxIp'
    'ChB0Y3BfaW5mb19iYWNrb2ZmGLUJIAEoDVIOdGNwSW5mb0JhY2tvZmYSKQoQdGNwX2luZm9fb3'
    'B0aW9ucxi2CSABKA1SDnRjcEluZm9PcHRpb25zEi4KE3RjcF9pbmZvX3NuZF93c2NhbGUYtwkg'
    'ASgNUhB0Y3BJbmZvU25kV3NjYWxlEi4KE3RjcF9pbmZvX3Jjdl93c2NhbGUYuAkgASgNUhB0Y3'
    'BJbmZvUmN2V3NjYWxlEkoKInRjcF9pbmZvX2RlbGl2ZXJ5X3JhdGVfYXBwX2xpbWl0ZWQYuQkg'
    'ASgNUh10Y3BJbmZvRGVsaXZlcnlSYXRlQXBwTGltaXRlZBJBCh10Y3BfaW5mb19mYXN0b3Blbl'
    '9jbGllbnRfZmFpbBi6CSABKA1SGXRjcEluZm9GYXN0b3BlbkNsaWVudEZhaWwSIQoMdGNwX2lu'
    'Zm9fcnRvGL8JIAEoDVIKdGNwSW5mb1J0bxIhCgx0Y3BfaW5mb19hdG8YwAkgASgNUgp0Y3BJbm'
    'ZvQXRvEigKEHRjcF9pbmZvX3NuZF9tc3MYwQkgASgNUg10Y3BJbmZvU25kTXNzEigKEHRjcF9p'
    'bmZvX3Jjdl9tc3MYwgkgASgNUg10Y3BJbmZvUmN2TXNzEikKEHRjcF9pbmZvX3VuYWNrZWQYww'
    'kgASgNUg50Y3BJbmZvVW5hY2tlZBInCg90Y3BfaW5mb19zYWNrZWQYxAkgASgNUg10Y3BJbmZv'
    'U2Fja2VkEiMKDXRjcF9pbmZvX2xvc3QYxQkgASgNUgt0Y3BJbmZvTG9zdBIpChB0Y3BfaW5mb1'
    '9yZXRyYW5zGMYJIAEoDVIOdGNwSW5mb1JldHJhbnMSKQoQdGNwX2luZm9fZmFja2V0cxjHCSAB'
    'KA1SDnRjcEluZm9GYWNrZXRzEjUKF3RjcF9pbmZvX2xhc3RfZGF0YV9zZW50GMgJIAEoDVITdG'
    'NwSW5mb0xhc3REYXRhU2VudBIzChZ0Y3BfaW5mb19sYXN0X2Fja19zZW50GMkJIAEoDVISdGNw'
    'SW5mb0xhc3RBY2tTZW50EjUKF3RjcF9pbmZvX2xhc3RfZGF0YV9yZWN2GMoJIAEoDVITdGNwSW'
    '5mb0xhc3REYXRhUmVjdhIzChZ0Y3BfaW5mb19sYXN0X2Fja19yZWN2GMsJIAEoDVISdGNwSW5m'
    'b0xhc3RBY2tSZWN2EiMKDXRjcF9pbmZvX3BtdHUYzAkgASgNUgt0Y3BJbmZvUG10dRIyChV0Y3'
    'BfaW5mb19yY3Zfc3N0aHJlc2gYzQkgASgNUhJ0Y3BJbmZvUmN2U3N0aHJlc2gSIQoMdGNwX2lu'
    'Zm9fcnR0GM4JIAEoDVIKdGNwSW5mb1J0dBInCg90Y3BfaW5mb19ydHR2YXIYzwkgASgNUg10Y3'
    'BJbmZvUnR0dmFyEjIKFXRjcF9pbmZvX3NuZF9zc3RocmVzaBjQCSABKA1SEnRjcEluZm9TbmRT'
    'c3RocmVzaBIqChF0Y3BfaW5mb19zbmRfY3duZBjRCSABKA1SDnRjcEluZm9TbmRDd25kEicKD3'
    'RjcF9pbmZvX2Fkdm1zcxjSCSABKA1SDXRjcEluZm9BZHZtc3MSLwoTdGNwX2luZm9fcmVvcmRl'
    'cmluZxjTCSABKA1SEXRjcEluZm9SZW9yZGVyaW5nEigKEHRjcF9pbmZvX3Jjdl9ydHQY1AkgAS'
    'gNUg10Y3BJbmZvUmN2UnR0EiwKEnRjcF9pbmZvX3Jjdl9zcGFjZRjVCSABKA1SD3RjcEluZm9S'
    'Y3ZTcGFjZRI0ChZ0Y3BfaW5mb190b3RhbF9yZXRyYW5zGNYJIAEoDVITdGNwSW5mb1RvdGFsUm'
    'V0cmFucxIwChR0Y3BfaW5mb19wYWNpbmdfcmF0ZRjXCSABKARSEXRjcEluZm9QYWNpbmdSYXRl'
    'EjcKGHRjcF9pbmZvX21heF9wYWNpbmdfcmF0ZRjYCSABKARSFHRjcEluZm9NYXhQYWNpbmdSYX'
    'RlEjAKFHRjcF9pbmZvX2J5dGVzX2Fja2VkGNkJIAEoBFIRdGNwSW5mb0J5dGVzQWNrZWQSNgoX'
    'dGNwX2luZm9fYnl0ZXNfcmVjZWl2ZWQY2gkgASgEUhR0Y3BJbmZvQnl0ZXNSZWNlaXZlZBIqCh'
    'F0Y3BfaW5mb19zZWdzX291dBjbCSABKA1SDnRjcEluZm9TZWdzT3V0EigKEHRjcF9pbmZvX3Nl'
    'Z3NfaW4Y3AkgASgNUg10Y3BJbmZvU2Vnc0luEjQKFnRjcF9pbmZvX25vdHNlbnRfYnl0ZXMY3Q'
    'kgASgNUhN0Y3BJbmZvTm90c2VudEJ5dGVzEigKEHRjcF9pbmZvX21pbl9ydHQY3gkgASgNUg10'
    'Y3BJbmZvTWluUnR0EjEKFXRjcF9pbmZvX2RhdGFfc2Vnc19pbhjfCSABKA1SEXRjcEluZm9EYX'
    'RhU2Vnc0luEjMKFnRjcF9pbmZvX2RhdGFfc2Vnc19vdXQY4AkgASgNUhJ0Y3BJbmZvRGF0YVNl'
    'Z3NPdXQSNAoWdGNwX2luZm9fZGVsaXZlcnlfcmF0ZRjhCSABKARSE3RjcEluZm9EZWxpdmVyeV'
    'JhdGUSLAoSdGNwX2luZm9fYnVzeV90aW1lGOIJIAEoBFIPdGNwSW5mb0J1c3lUaW1lEjIKFXRj'
    'cF9pbmZvX3J3bmRfbGltaXRlZBjjCSABKARSEnRjcEluZm9Sd25kTGltaXRlZBI2Chd0Y3BfaW'
    '5mb19zbmRidWZfbGltaXRlZBjkCSABKARSFHRjcEluZm9TbmRidWZMaW1pdGVkEi0KEnRjcF9p'
    'bmZvX2RlbGl2ZXJlZBjlCSABKA1SEHRjcEluZm9EZWxpdmVyZWQSMgoVdGNwX2luZm9fZGVsaX'
    'ZlcmVkX2NlGOYJIAEoDVISdGNwSW5mb0RlbGl2ZXJlZENlEi4KE3RjcF9pbmZvX2J5dGVzX3Nl'
    'bnQY5wkgASgEUhB0Y3BJbmZvQnl0ZXNTZW50EjQKFnRjcF9pbmZvX2J5dGVzX3JldHJhbnMY6A'
    'kgASgEUhN0Y3BJbmZvQnl0ZXNSZXRyYW5zEi4KE3RjcF9pbmZvX2RzYWNrX2R1cHMY6QkgASgN'
    'UhB0Y3BJbmZvRHNhY2tEdXBzEi4KE3RjcF9pbmZvX3Jlb3JkX3NlZW4Y6gkgASgNUhB0Y3BJbm'
    'ZvUmVvcmRTZWVuEjAKFHRjcF9pbmZvX3Jjdl9vb29wYWNrGOsJIAEoDVIRdGNwSW5mb1Jjdk9v'
    'b3BhY2sSKAoQdGNwX2luZm9fc25kX3duZBjsCSABKA1SDXRjcEluZm9TbmRXbmQSKAoQdGNwX2'
    'luZm9fcmN2X3duZBjtCSABKA1SDXRjcEluZm9SY3ZXbmQSJwoPdGNwX2luZm9fcmVoYXNoGO4J'
    'IAEoDVINdGNwSW5mb1JlaGFzaBIsChJ0Y3BfaW5mb190b3RhbF9ydG8Y7wkgASgNUg90Y3BJbm'
    'ZvVG90YWxSdG8SQQoddGNwX2luZm9fdG90YWxfcnRvX3JlY292ZXJpZXMY8AkgASgNUhl0Y3BJ'
    'bmZvVG90YWxSdG9SZWNvdmVyaWVzEjUKF3RjcF9pbmZvX3RvdGFsX3J0b190aW1lGPEJIAEoDV'
    'ITdGNwSW5mb1RvdGFsUnRvVGltZRIwChR0Y3BfaW5mb19yZWNlaXZlZF9jZRjyCSABKA1SEXRj'
    'cEluZm9SZWNlaXZlZENlEj0KG3RjcF9pbmZvX2RlbGl2ZXJlZF9lMV9ieXRlcxjzCSABKA1SF3'
    'RjcEluZm9EZWxpdmVyZWRFMUJ5dGVzEj0KG3RjcF9pbmZvX2RlbGl2ZXJlZF9lMF9ieXRlcxj0'
    'CSABKA1SF3RjcEluZm9EZWxpdmVyZWRFMEJ5dGVzEj0KG3RjcF9pbmZvX2RlbGl2ZXJlZF9jZV'
    '9ieXRlcxj1CSABKA1SF3RjcEluZm9EZWxpdmVyZWRDZUJ5dGVzEjsKGnRjcF9pbmZvX3JlY2Vp'
    'dmVkX2UxX2J5dGVzGPYJIAEoDVIWdGNwSW5mb1JlY2VpdmVkRTFCeXRlcxI7Chp0Y3BfaW5mb1'
    '9yZWNlaXZlZF9lMF9ieXRlcxj3CSABKA1SFnRjcEluZm9SZWNlaXZlZEUwQnl0ZXMSOwoadGNw'
    'X2luZm9fcmVjZWl2ZWRfY2VfYnl0ZXMY+AkgASgNUhZ0Y3BJbmZvUmVjZWl2ZWRDZUJ5dGVzEi'
    'oKEXRjcF9pbmZvX2Vjbl9tb2RlGPkJIAEoDVIOdGNwSW5mb0Vjbk1vZGUSNwoYdGNwX2luZm9f'
    'YWNjZWNuX29wdF9zZWVuGPoJIAEoDVIUdGNwSW5mb0FjY2Vjbk9wdFNlZW4SOQoZdGNwX2luZm'
    '9fYWNjZWNuX2ZhaWxfbW9kZRj7CSABKA1SFXRjcEluZm9BY2NlY25GYWlsTW9kZRIrChF0Y3Bf'
    'aW5mb19vcHRpb25zMhj8CSABKA1SD3RjcEluZm9PcHRpb25zMhIlCg5pbmV0X2RpYWdfY29uZx'
    'iUCiABKAlSDGluZXREaWFnQ29uZxJnChNpbmV0X2RpYWdfY29uZ19lbnVtGJUKIAEoDjI3Lnh0'
    'Y3BfZmxhdF9yZWNvcmQudjEuWHRjcEZsYXRSZWNvcmQuQ29uZ2VzdGlvbkFsZ29yaXRobVIQaW'
    '5ldERpYWdDb25nRW51bRIjCg1pbmV0X2RpYWdfdG9zGPkKIAEoDVILaW5ldERpYWdUb3MSKQoQ'
    'aW5ldF9kaWFnX3RjbGFzcxj6CiABKA1SDmluZXREaWFnVGNsYXNzEjMKFnNrX21lbV9pbmZvX3'
    'JtZW1fYWxsb2MY3QsgASgNUhJza01lbUluZm9SbWVtQWxsb2MSLAoSc2tfbWVtX2luZm9fcmN2'
    'YnVmGN4LIAEoDVIPc2tNZW1JbmZvUmN2YnVmEjMKFnNrX21lbV9pbmZvX3dtZW1fYWxsb2MY3w'
    'sgASgNUhJza01lbUluZm9XbWVtQWxsb2MSLAoSc2tfbWVtX2luZm9fc25kYnVmGOALIAEoDVIP'
    'c2tNZW1JbmZvU25kYnVmEjEKFXNrX21lbV9pbmZvX2Z3ZF9hbGxvYxjhCyABKA1SEXNrTWVtSW'
    '5mb0Z3ZEFsbG9jEjUKF3NrX21lbV9pbmZvX3dtZW1fcXVldWVkGOILIAEoDVITc2tNZW1JbmZv'
    'V21lbVF1ZXVlZBIsChJza19tZW1faW5mb19vcHRtZW0Y4wsgASgNUg9za01lbUluZm9PcHRtZW'
    '0SLgoTc2tfbWVtX2luZm9fYmFja2xvZxjkCyABKA1SEHNrTWVtSW5mb0JhY2tsb2cSKgoRc2tf'
    'bWVtX2luZm9fZHJvcHMY5QsgASgNUg5za01lbUluZm9Ecm9wcxItChJpbmV0X2RpYWdfc2h1dG'
    'Rvd24YwAwgASgNUhBpbmV0RGlhZ1NodXRkb3duEi0KEnZlZ2FzX2luZm9fZW5hYmxlZBilDSAB'
    'KA1SEHZlZ2FzSW5mb0VuYWJsZWQSKwoRdmVnYXNfaW5mb19ydHRjbnQYpg0gASgNUg92ZWdhc0'
    'luZm9SdHRjbnQSJQoOdmVnYXNfaW5mb19ydHQYpw0gASgNUgx2ZWdhc0luZm9SdHQSKwoRdmVn'
    'YXNfaW5mb19taW5ydHQYqA0gASgNUg92ZWdhc0luZm9NaW5ydHQSLQoSZGN0Y3BfaW5mb19lbm'
    'FibGVkGIkOIAEoDVIQZGN0Y3BJbmZvRW5hYmxlZBIuChNkY3RjcF9pbmZvX2NlX3N0YXRlGIoO'
    'IAEoDVIQZGN0Y3BJbmZvQ2VTdGF0ZRIpChBkY3RjcF9pbmZvX2FscGhhGIsOIAEoDVIOZGN0Y3'
    'BJbmZvQWxwaGESKgoRZGN0Y3BfaW5mb19hYl9lY24YjA4gASgNUg5kY3RjcEluZm9BYkVjbhIq'
    'ChFkY3RjcF9pbmZvX2FiX3RvdBiNDiABKA1SDmRjdGNwSW5mb0FiVG90EiQKDmJicl9pbmZvX2'
    'J3X2xvGO0OIAEoDVILYmJySW5mb0J3TG8SJAoOYmJyX2luZm9fYndfaGkY7g4gASgNUgtiYnJJ'
    'bmZvQndIaRIoChBiYnJfaW5mb19taW5fcnR0GO8OIAEoDVINYmJySW5mb01pblJ0dBIwChRiYn'
    'JfaW5mb19wYWNpbmdfZ2FpbhjwDiABKA1SEWJickluZm9QYWNpbmdHYWluEiwKEmJicl9pbmZv'
    'X2N3bmRfZ2FpbhjxDiABKA1SD2JickluZm9Dd25kR2FpbhIsChJpbmV0X2RpYWdfY2xhc3NfaW'
    'QY0Q8gASgNUg9pbmV0RGlhZ0NsYXNzSWQSKwoRaW5ldF9kaWFnX3NvY2tvcHQY0g8gASgNUg9p'
    'bmV0RGlhZ1NvY2tvcHQSLgoTaW5ldF9kaWFnX2Nncm91cF9pZBjTDyABKARSEGluZXREaWFnQ2'
    'dyb3VwSWQiZwoITG9jYWxpdHkSGAoUTE9DQUxJVFlfVU5TUEVDSUZJRUQQABIRCg1MT0NBTElU'
    'WV9TRUxGEAESGQoVTE9DQUxJVFlfTE9DQUxfU1VCTkVUEAISEwoPTE9DQUxJVFlfUkVNT1RFEA'
    'MimQIKE0Nvbmdlc3Rpb25BbGdvcml0aG0SJAogQ09OR0VTVElPTl9BTEdPUklUSE1fVU5TUEVD'
    'SUZJRUQQABIeChpDT05HRVNUSU9OX0FMR09SSVRITV9DVUJJQxABEh4KGkNPTkdFU1RJT05fQU'
    'xHT1JJVEhNX0RDVENQEAISHgoaQ09OR0VTVElPTl9BTEdPUklUSE1fVkVHQVMQAxIfChtDT05H'
    'RVNUSU9OX0FMR09SSVRITV9QUkFHVUUQBBIdChlDT05HRVNUSU9OX0FMR09SSVRITV9CQlIxEA'
    'USHQoZQ09OR0VTVElPTl9BTEdPUklUSE1fQkJSMhAGEh0KGUNPTkdFU1RJT05fQUxHT1JJVEhN'
    'X0JCUjMQB0oGCK0CEK4CSgYIrgIQrwJKBgjzBxD0B0oGCPQHEPUHSgYI+gcQ+wdKBgj7BxD8B0'
    'oGCLcQELgQUh1pbmV0X2RpYWdfbXNnX3NvY2tldF9kZXN0X2FzblIhaW5ldF9kaWFnX21zZ19z'
    'b2NrZXRfbmV4dF9ob3BfYXNuUidpbmV0X2RpYWdfbXNnX3NvY2tldF9kZXN0X25ldHdvcmtfb3'
    'duZXJSImluZXRfZGlhZ19tc2dfc29ja2V0X2Rlc3RfbG9jYWxpdHlSGmVucmljaF9zb2NrZXRf'
    'bmV4dF9ob3BfYXNuUhN0Y3BfaW5mb19zZW5kX3NjYWxlUhJ0Y3BfaW5mb19yY3Zfc2NhbGVSIH'
    'RjcF9pbmZvX2Zhc3Rfb3Blbl9jbGllbnRfZmFpbGVkUhB0Y3BfaW5mb19ydHRfdmFyUhB0Y3Bf'
    'aW5mb19hZHZfbXNzUhd0Y3BfaW5mb19ub3Rfc2VudF9ieXRlc1ITc2tfbWVtX2luZm9fcmN2X2'
    'J1ZlITc2tfbWVtX2luZm9fc25kX2J1ZlISdmVnYXNfaW5mb19ydHRfY250UhJ2ZWdhc19pbmZv'
    'X21pbl9ydHRSG2Nvbmdlc3Rpb25fYWxnb3JpdGhtX3N0cmluZ1IZY29uZ2VzdGlvbl9hbGdvcm'
    'l0aG1fZW51bVIPdHlwZV9vZl9zZXJ2aWNlUg10cmFmZmljX2NsYXNzUg5zaHV0ZG93bl9zdGF0'
    'ZVIIY2xhc3NfaWRSCHNvY2tfb3B0UgdjX2dyb3Vw');

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
