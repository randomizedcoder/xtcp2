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
    {'1': 'nsid_assigned', '3': 33, '4': 1, '5': 8, '10': 'nsidAssigned'},
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
    '5zX2lub2RlGB8gASgEUgpuZXRuc0lub2RlEhIKBG5zaWQYICABKA1SBG5zaWQSIwoNbnNpZF9h'
    'c3NpZ25lZBghIAEoCFIMbnNpZEFzc2lnbmVkEiEKDGNvbnRhaW5lcl9pZBgoIAEoCVILY29udG'
    'FpbmVySWQSKwoRY29udGFpbmVyX3J1bnRpbWUYKSABKAlSEGNvbnRhaW5lclJ1bnRpbWUSJQoO'
    'Y29udGFpbmVyX25hbWUYKiABKAlSDWNvbnRhaW5lck5hbWUSJwoPY29udGFpbmVyX2ltYWdlGC'
    'sgASgJUg5jb250YWluZXJJbWFnZRIUCgVsYWJlbBgyIAEoCVIFbGFiZWwSEAoDdGFnGDMgASgJ'
    'UgN0YWcSJQoOcmVjb3JkX2NvdW50ZXIYPCABKARSDXJlY29yZENvdW50ZXISGwoJc29ja2V0X2'
    'ZkGD0gASgEUghzb2NrZXRGZBIhCgxuZXRsaW5rZXJfaWQYPiABKARSC25ldGxpbmtlcklkEiUK'
    'DnVwbGluazFfaWZuYW1lGGQgASgJUg11cGxpbmsxSWZuYW1lEiwKEnVwbGluazFfbmljX2RyaX'
    'ZlchhlIAEoCVIQdXBsaW5rMU5pY0RyaXZlchIqChF1cGxpbmsxX25pY19tb2RlbBhmIAEoCVIP'
    'dXBsaW5rMU5pY01vZGVsEjMKFnVwbGluazFfbmljX3BjaV92ZW5kb3IYZyABKA1SE3VwbGluaz'
    'FOaWNQY2lWZW5kb3ISMwoWdXBsaW5rMV9uaWNfcGNpX2RldmljZRhoIAEoDVITdXBsaW5rMU5p'
    'Y1BjaURldmljZRIvChR1cGxpbmsxX25pY19idXNfaW5mbxhpIAEoCVIRdXBsaW5rMU5pY0J1c0'
    'luZm8SMwoWdXBsaW5rMV9uaWNfc3BlZWRfbWJwcxhqIAEoDVITdXBsaW5rMU5pY1NwZWVkTWJw'
    'cxIzChZ1cGxpbmsxX25pY19md192ZXJzaW9uGGsgASgJUhN1cGxpbmsxTmljRndWZXJzaW9uEj'
    'kKGXVwbGluazFfbGxkcF9jaGFzc2lzX25hbWUYeCABKAlSFnVwbGluazFMbGRwQ2hhc3Npc05h'
    'bWUSNQoXdXBsaW5rMV9sbGRwX2NoYXNzaXNfaWQYeSABKAlSFHVwbGluazFMbGRwQ2hhc3Npc0'
    'lkEi8KFHVwbGluazFfbGxkcF9tZ210X2lwGHogASgJUhF1cGxpbmsxTGxkcE1nbXRJcBIvChR1'
    'cGxpbmsxX2xsZHBfcG9ydF9pZBh7IAEoCVIRdXBsaW5rMUxsZHBQb3J0SWQSNQoXdXBsaW5rMV'
    '9sbGRwX3BvcnRfZGVzY3IYfCABKAlSFHVwbGluazFMbGRwUG9ydERlc2NyEiYKDnVwbGluazJf'
    'aWZuYW1lGMgBIAEoCVINdXBsaW5rMklmbmFtZRItChJ1cGxpbmsyX25pY19kcml2ZXIYyQEgAS'
    'gJUhB1cGxpbmsyTmljRHJpdmVyEisKEXVwbGluazJfbmljX21vZGVsGMoBIAEoCVIPdXBsaW5r'
    'Mk5pY01vZGVsEjQKFnVwbGluazJfbmljX3BjaV92ZW5kb3IYywEgASgNUhN1cGxpbmsyTmljUG'
    'NpVmVuZG9yEjQKFnVwbGluazJfbmljX3BjaV9kZXZpY2UYzAEgASgNUhN1cGxpbmsyTmljUGNp'
    'RGV2aWNlEjAKFHVwbGluazJfbmljX2J1c19pbmZvGM0BIAEoCVIRdXBsaW5rMk5pY0J1c0luZm'
    '8SNAoWdXBsaW5rMl9uaWNfc3BlZWRfbWJwcxjOASABKA1SE3VwbGluazJOaWNTcGVlZE1icHMS'
    'NAoWdXBsaW5rMl9uaWNfZndfdmVyc2lvbhjPASABKAlSE3VwbGluazJOaWNGd1ZlcnNpb24SOg'
    'oZdXBsaW5rMl9sbGRwX2NoYXNzaXNfbmFtZRjcASABKAlSFnVwbGluazJMbGRwQ2hhc3Npc05h'
    'bWUSNgoXdXBsaW5rMl9sbGRwX2NoYXNzaXNfaWQY3QEgASgJUhR1cGxpbmsyTGxkcENoYXNzaX'
    'NJZBIwChR1cGxpbmsyX2xsZHBfbWdtdF9pcBjeASABKAlSEXVwbGluazJMbGRwTWdtdElwEjAK'
    'FHVwbGluazJfbGxkcF9wb3J0X2lkGN8BIAEoCVIRdXBsaW5rMkxsZHBQb3J0SWQSNgoXdXBsaW'
    '5rMl9sbGRwX3BvcnRfZGVzY3IY4AEgASgJUhR1cGxpbmsyTGxkcFBvcnREZXNjchJAChxlbnJp'
    'Y2hfc29ja2V0X2ludGVyZmFjZV9uYW1lGKwCIAEoCVIZZW5yaWNoU29ja2V0SW50ZXJmYWNlTm'
    'FtZRJsChtlbnJpY2hfc29ja2V0X2Rlc3RfbG9jYWxpdHkYtgIgASgOMiwueHRjcF9mbGF0X3Jl'
    'Y29yZC52MS5YdGNwRmxhdFJlY29yZC5Mb2NhbGl0eVIYZW5yaWNoU29ja2V0RGVzdExvY2FsaX'
    'R5EkkKIWVucmljaF9zb2NrZXRfZGVzdF9lZ3Jlc3NfaWZpbmRleBi3AiABKA1SHWVucmljaFNv'
    'Y2tldERlc3RFZ3Jlc3NJZmluZGV4EkcKIGVucmljaF9zb2NrZXRfZGVzdF9lZ3Jlc3NfaWZuYW'
    '1lGLgCIAEoCVIcZW5yaWNoU29ja2V0RGVzdEVncmVzc0lmbmFtZRI0ChZlbnJpY2hfc29ja2V0'
    'X2Rlc3RfYXNuGMACIAEoBFITZW5yaWNoU29ja2V0RGVzdEFzbhJECh9lbnJpY2hfc29ja2V0X2'
    'Rlc3RfbmV4dF9ob3BfYXNuGMECIAEoBFIaZW5yaWNoU29ja2V0RGVzdE5leHRIb3BBc24SRwog'
    'ZW5yaWNoX3NvY2tldF9kZXN0X25ldHdvcmtfb3duZXIYwgIgASgJUhxlbnJpY2hTb2NrZXREZX'
    'N0TmV0d29ya093bmVyEjAKFGluZXRfZGlhZ19tc2dfZmFtaWx5GOkHIAEoDVIRaW5ldERpYWdN'
    'c2dGYW1pbHkSLgoTaW5ldF9kaWFnX21zZ19zdGF0ZRjqByABKA1SEGluZXREaWFnTXNnU3RhdG'
    'USLgoTaW5ldF9kaWFnX21zZ190aW1lchjrByABKA1SEGluZXREaWFnTXNnVGltZXISMgoVaW5l'
    'dF9kaWFnX21zZ19yZXRyYW5zGOwHIAEoDVISaW5ldERpYWdNc2dSZXRyYW5zEkYKIGluZXRfZG'
    'lhZ19tc2dfc29ja2V0X3NvdXJjZV9wb3J0GO0HIAEoDVIbaW5ldERpYWdNc2dTb2NrZXRTb3Vy'
    'Y2VQb3J0ElAKJWluZXRfZGlhZ19tc2dfc29ja2V0X2Rlc3RpbmF0aW9uX3BvcnQY7gcgASgNUi'
    'BpbmV0RGlhZ01zZ1NvY2tldERlc3RpbmF0aW9uUG9ydBI9ChtpbmV0X2RpYWdfbXNnX3NvY2tl'
    'dF9zb3VyY2UY7wcgASgMUhdpbmV0RGlhZ01zZ1NvY2tldFNvdXJjZRJHCiBpbmV0X2RpYWdfbX'
    'NnX3NvY2tldF9kZXN0aW5hdGlvbhjwByABKAxSHGluZXREaWFnTXNnU29ja2V0RGVzdGluYXRp'
    'b24SQwoeaW5ldF9kaWFnX21zZ19zb2NrZXRfaW50ZXJmYWNlGPEHIAEoDVIaaW5ldERpYWdNc2'
    'dTb2NrZXRJbnRlcmZhY2USPQobaW5ldF9kaWFnX21zZ19zb2NrZXRfY29va2llGPIHIAEoBFIX'
    'aW5ldERpYWdNc2dTb2NrZXRDb29raWUSMgoVaW5ldF9kaWFnX21zZ19leHBpcmVzGPUHIAEoDV'
    'ISaW5ldERpYWdNc2dFeHBpcmVzEjAKFGluZXRfZGlhZ19tc2dfcnF1ZXVlGPYHIAEoDVIRaW5l'
    'dERpYWdNc2dScXVldWUSMAoUaW5ldF9kaWFnX21zZ193cXVldWUY9wcgASgNUhFpbmV0RGlhZ0'
    '1zZ1dxdWV1ZRIqChFpbmV0X2RpYWdfbXNnX3VpZBj4ByABKA1SDmluZXREaWFnTXNnVWlkEi4K'
    'E2luZXRfZGlhZ19tc2dfaW5vZGUY+QcgASgNUhBpbmV0RGlhZ01zZ0lub2RlEiMKDW1lbV9pbm'
    'ZvX3JtZW0YzQggASgNUgttZW1JbmZvUm1lbRIjCg1tZW1faW5mb193bWVtGM4IIAEoDVILbWVt'
    'SW5mb1dtZW0SIwoNbWVtX2luZm9fZm1lbRjPCCABKA1SC21lbUluZm9GbWVtEiMKDW1lbV9pbm'
    'ZvX3RtZW0Y0AggASgNUgttZW1JbmZvVG1lbRIlCg50Y3BfaW5mb19zdGF0ZRixCSABKA1SDHRj'
    'cEluZm9TdGF0ZRIqChF0Y3BfaW5mb19jYV9zdGF0ZRiyCSABKA1SDnRjcEluZm9DYVN0YXRlEj'
    'EKFHRjcF9pbmZvX3JldHJhbnNtaXRzGLMJIAEoDVISdGNwSW5mb1JldHJhbnNtaXRzEicKD3Rj'
    'cF9pbmZvX3Byb2Jlcxi0CSABKA1SDXRjcEluZm9Qcm9iZXMSKQoQdGNwX2luZm9fYmFja29mZh'
    'i1CSABKA1SDnRjcEluZm9CYWNrb2ZmEikKEHRjcF9pbmZvX29wdGlvbnMYtgkgASgNUg50Y3BJ'
    'bmZvT3B0aW9ucxIuChN0Y3BfaW5mb19zbmRfd3NjYWxlGLcJIAEoDVIQdGNwSW5mb1NuZFdzY2'
    'FsZRIuChN0Y3BfaW5mb19yY3Zfd3NjYWxlGLgJIAEoDVIQdGNwSW5mb1JjdldzY2FsZRJKCiJ0'
    'Y3BfaW5mb19kZWxpdmVyeV9yYXRlX2FwcF9saW1pdGVkGLkJIAEoDVIddGNwSW5mb0RlbGl2ZX'
    'J5UmF0ZUFwcExpbWl0ZWQSQQoddGNwX2luZm9fZmFzdG9wZW5fY2xpZW50X2ZhaWwYugkgASgN'
    'Uhl0Y3BJbmZvRmFzdG9wZW5DbGllbnRGYWlsEiEKDHRjcF9pbmZvX3J0bxi/CSABKA1SCnRjcE'
    'luZm9SdG8SIQoMdGNwX2luZm9fYXRvGMAJIAEoDVIKdGNwSW5mb0F0bxIoChB0Y3BfaW5mb19z'
    'bmRfbXNzGMEJIAEoDVINdGNwSW5mb1NuZE1zcxIoChB0Y3BfaW5mb19yY3ZfbXNzGMIJIAEoDV'
    'INdGNwSW5mb1Jjdk1zcxIpChB0Y3BfaW5mb191bmFja2VkGMMJIAEoDVIOdGNwSW5mb1VuYWNr'
    'ZWQSJwoPdGNwX2luZm9fc2Fja2VkGMQJIAEoDVINdGNwSW5mb1NhY2tlZBIjCg10Y3BfaW5mb1'
    '9sb3N0GMUJIAEoDVILdGNwSW5mb0xvc3QSKQoQdGNwX2luZm9fcmV0cmFucxjGCSABKA1SDnRj'
    'cEluZm9SZXRyYW5zEikKEHRjcF9pbmZvX2ZhY2tldHMYxwkgASgNUg50Y3BJbmZvRmFja2V0cx'
    'I1Chd0Y3BfaW5mb19sYXN0X2RhdGFfc2VudBjICSABKA1SE3RjcEluZm9MYXN0RGF0YVNlbnQS'
    'MwoWdGNwX2luZm9fbGFzdF9hY2tfc2VudBjJCSABKA1SEnRjcEluZm9MYXN0QWNrU2VudBI1Ch'
    'd0Y3BfaW5mb19sYXN0X2RhdGFfcmVjdhjKCSABKA1SE3RjcEluZm9MYXN0RGF0YVJlY3YSMwoW'
    'dGNwX2luZm9fbGFzdF9hY2tfcmVjdhjLCSABKA1SEnRjcEluZm9MYXN0QWNrUmVjdhIjCg10Y3'
    'BfaW5mb19wbXR1GMwJIAEoDVILdGNwSW5mb1BtdHUSMgoVdGNwX2luZm9fcmN2X3NzdGhyZXNo'
    'GM0JIAEoDVISdGNwSW5mb1JjdlNzdGhyZXNoEiEKDHRjcF9pbmZvX3J0dBjOCSABKA1SCnRjcE'
    'luZm9SdHQSJwoPdGNwX2luZm9fcnR0dmFyGM8JIAEoDVINdGNwSW5mb1J0dHZhchIyChV0Y3Bf'
    'aW5mb19zbmRfc3N0aHJlc2gY0AkgASgNUhJ0Y3BJbmZvU25kU3N0aHJlc2gSKgoRdGNwX2luZm'
    '9fc25kX2N3bmQY0QkgASgNUg50Y3BJbmZvU25kQ3duZBInCg90Y3BfaW5mb19hZHZtc3MY0gkg'
    'ASgNUg10Y3BJbmZvQWR2bXNzEi8KE3RjcF9pbmZvX3Jlb3JkZXJpbmcY0wkgASgNUhF0Y3BJbm'
    'ZvUmVvcmRlcmluZxIoChB0Y3BfaW5mb19yY3ZfcnR0GNQJIAEoDVINdGNwSW5mb1JjdlJ0dBIs'
    'ChJ0Y3BfaW5mb19yY3Zfc3BhY2UY1QkgASgNUg90Y3BJbmZvUmN2U3BhY2USNAoWdGNwX2luZm'
    '9fdG90YWxfcmV0cmFucxjWCSABKA1SE3RjcEluZm9Ub3RhbFJldHJhbnMSMAoUdGNwX2luZm9f'
    'cGFjaW5nX3JhdGUY1wkgASgEUhF0Y3BJbmZvUGFjaW5nUmF0ZRI3Chh0Y3BfaW5mb19tYXhfcG'
    'FjaW5nX3JhdGUY2AkgASgEUhR0Y3BJbmZvTWF4UGFjaW5nUmF0ZRIwChR0Y3BfaW5mb19ieXRl'
    'c19hY2tlZBjZCSABKARSEXRjcEluZm9CeXRlc0Fja2VkEjYKF3RjcF9pbmZvX2J5dGVzX3JlY2'
    'VpdmVkGNoJIAEoBFIUdGNwSW5mb0J5dGVzUmVjZWl2ZWQSKgoRdGNwX2luZm9fc2Vnc19vdXQY'
    '2wkgASgNUg50Y3BJbmZvU2Vnc091dBIoChB0Y3BfaW5mb19zZWdzX2luGNwJIAEoDVINdGNwSW'
    '5mb1NlZ3NJbhI0ChZ0Y3BfaW5mb19ub3RzZW50X2J5dGVzGN0JIAEoDVITdGNwSW5mb05vdHNl'
    'bnRCeXRlcxIoChB0Y3BfaW5mb19taW5fcnR0GN4JIAEoDVINdGNwSW5mb01pblJ0dBIxChV0Y3'
    'BfaW5mb19kYXRhX3NlZ3NfaW4Y3wkgASgNUhF0Y3BJbmZvRGF0YVNlZ3NJbhIzChZ0Y3BfaW5m'
    'b19kYXRhX3NlZ3Nfb3V0GOAJIAEoDVISdGNwSW5mb0RhdGFTZWdzT3V0EjQKFnRjcF9pbmZvX2'
    'RlbGl2ZXJ5X3JhdGUY4QkgASgEUhN0Y3BJbmZvRGVsaXZlcnlSYXRlEiwKEnRjcF9pbmZvX2J1'
    'c3lfdGltZRjiCSABKARSD3RjcEluZm9CdXN5VGltZRIyChV0Y3BfaW5mb19yd25kX2xpbWl0ZW'
    'QY4wkgASgEUhJ0Y3BJbmZvUnduZExpbWl0ZWQSNgoXdGNwX2luZm9fc25kYnVmX2xpbWl0ZWQY'
    '5AkgASgEUhR0Y3BJbmZvU25kYnVmTGltaXRlZBItChJ0Y3BfaW5mb19kZWxpdmVyZWQY5QkgAS'
    'gNUhB0Y3BJbmZvRGVsaXZlcmVkEjIKFXRjcF9pbmZvX2RlbGl2ZXJlZF9jZRjmCSABKA1SEnRj'
    'cEluZm9EZWxpdmVyZWRDZRIuChN0Y3BfaW5mb19ieXRlc19zZW50GOcJIAEoBFIQdGNwSW5mb0'
    'J5dGVzU2VudBI0ChZ0Y3BfaW5mb19ieXRlc19yZXRyYW5zGOgJIAEoBFITdGNwSW5mb0J5dGVz'
    'UmV0cmFucxIuChN0Y3BfaW5mb19kc2Fja19kdXBzGOkJIAEoDVIQdGNwSW5mb0RzYWNrRHVwcx'
    'IuChN0Y3BfaW5mb19yZW9yZF9zZWVuGOoJIAEoDVIQdGNwSW5mb1Jlb3JkU2VlbhIwChR0Y3Bf'
    'aW5mb19yY3Zfb29vcGFjaxjrCSABKA1SEXRjcEluZm9SY3ZPb29wYWNrEigKEHRjcF9pbmZvX3'
    'NuZF93bmQY7AkgASgNUg10Y3BJbmZvU25kV25kEigKEHRjcF9pbmZvX3Jjdl93bmQY7QkgASgN'
    'Ug10Y3BJbmZvUmN2V25kEicKD3RjcF9pbmZvX3JlaGFzaBjuCSABKA1SDXRjcEluZm9SZWhhc2'
    'gSLAoSdGNwX2luZm9fdG90YWxfcnRvGO8JIAEoDVIPdGNwSW5mb1RvdGFsUnRvEkEKHXRjcF9p'
    'bmZvX3RvdGFsX3J0b19yZWNvdmVyaWVzGPAJIAEoDVIZdGNwSW5mb1RvdGFsUnRvUmVjb3Zlcm'
    'llcxI1Chd0Y3BfaW5mb190b3RhbF9ydG9fdGltZRjxCSABKA1SE3RjcEluZm9Ub3RhbFJ0b1Rp'
    'bWUSMAoUdGNwX2luZm9fcmVjZWl2ZWRfY2UY8gkgASgNUhF0Y3BJbmZvUmVjZWl2ZWRDZRI9Ch'
    't0Y3BfaW5mb19kZWxpdmVyZWRfZTFfYnl0ZXMY8wkgASgNUhd0Y3BJbmZvRGVsaXZlcmVkRTFC'
    'eXRlcxI9Cht0Y3BfaW5mb19kZWxpdmVyZWRfZTBfYnl0ZXMY9AkgASgNUhd0Y3BJbmZvRGVsaX'
    'ZlcmVkRTBCeXRlcxI9Cht0Y3BfaW5mb19kZWxpdmVyZWRfY2VfYnl0ZXMY9QkgASgNUhd0Y3BJ'
    'bmZvRGVsaXZlcmVkQ2VCeXRlcxI7Chp0Y3BfaW5mb19yZWNlaXZlZF9lMV9ieXRlcxj2CSABKA'
    '1SFnRjcEluZm9SZWNlaXZlZEUxQnl0ZXMSOwoadGNwX2luZm9fcmVjZWl2ZWRfZTBfYnl0ZXMY'
    '9wkgASgNUhZ0Y3BJbmZvUmVjZWl2ZWRFMEJ5dGVzEjsKGnRjcF9pbmZvX3JlY2VpdmVkX2NlX2'
    'J5dGVzGPgJIAEoDVIWdGNwSW5mb1JlY2VpdmVkQ2VCeXRlcxIqChF0Y3BfaW5mb19lY25fbW9k'
    'ZRj5CSABKA1SDnRjcEluZm9FY25Nb2RlEjcKGHRjcF9pbmZvX2FjY2Vjbl9vcHRfc2Vlbhj6CS'
    'ABKA1SFHRjcEluZm9BY2NlY25PcHRTZWVuEjkKGXRjcF9pbmZvX2FjY2Vjbl9mYWlsX21vZGUY'
    '+wkgASgNUhV0Y3BJbmZvQWNjZWNuRmFpbE1vZGUSKwoRdGNwX2luZm9fb3B0aW9uczIY/AkgAS'
    'gNUg90Y3BJbmZvT3B0aW9uczISJQoOaW5ldF9kaWFnX2NvbmcYlAogASgJUgxpbmV0RGlhZ0Nv'
    'bmcSZwoTaW5ldF9kaWFnX2NvbmdfZW51bRiVCiABKA4yNy54dGNwX2ZsYXRfcmVjb3JkLnYxLl'
    'h0Y3BGbGF0UmVjb3JkLkNvbmdlc3Rpb25BbGdvcml0aG1SEGluZXREaWFnQ29uZ0VudW0SIwoN'
    'aW5ldF9kaWFnX3Rvcxj5CiABKA1SC2luZXREaWFnVG9zEikKEGluZXRfZGlhZ190Y2xhc3MY+g'
    'ogASgNUg5pbmV0RGlhZ1RjbGFzcxIzChZza19tZW1faW5mb19ybWVtX2FsbG9jGN0LIAEoDVIS'
    'c2tNZW1JbmZvUm1lbUFsbG9jEiwKEnNrX21lbV9pbmZvX3JjdmJ1ZhjeCyABKA1SD3NrTWVtSW'
    '5mb1JjdmJ1ZhIzChZza19tZW1faW5mb193bWVtX2FsbG9jGN8LIAEoDVISc2tNZW1JbmZvV21l'
    'bUFsbG9jEiwKEnNrX21lbV9pbmZvX3NuZGJ1ZhjgCyABKA1SD3NrTWVtSW5mb1NuZGJ1ZhIxCh'
    'Vza19tZW1faW5mb19md2RfYWxsb2MY4QsgASgNUhFza01lbUluZm9Gd2RBbGxvYxI1Chdza19t'
    'ZW1faW5mb193bWVtX3F1ZXVlZBjiCyABKA1SE3NrTWVtSW5mb1dtZW1RdWV1ZWQSLAoSc2tfbW'
    'VtX2luZm9fb3B0bWVtGOMLIAEoDVIPc2tNZW1JbmZvT3B0bWVtEi4KE3NrX21lbV9pbmZvX2Jh'
    'Y2tsb2cY5AsgASgNUhBza01lbUluZm9CYWNrbG9nEioKEXNrX21lbV9pbmZvX2Ryb3BzGOULIA'
    'EoDVIOc2tNZW1JbmZvRHJvcHMSLQoSaW5ldF9kaWFnX3NodXRkb3duGMAMIAEoDVIQaW5ldERp'
    'YWdTaHV0ZG93bhItChJ2ZWdhc19pbmZvX2VuYWJsZWQYpQ0gASgNUhB2ZWdhc0luZm9FbmFibG'
    'VkEisKEXZlZ2FzX2luZm9fcnR0Y250GKYNIAEoDVIPdmVnYXNJbmZvUnR0Y250EiUKDnZlZ2Fz'
    'X2luZm9fcnR0GKcNIAEoDVIMdmVnYXNJbmZvUnR0EisKEXZlZ2FzX2luZm9fbWlucnR0GKgNIA'
    'EoDVIPdmVnYXNJbmZvTWlucnR0Ei0KEmRjdGNwX2luZm9fZW5hYmxlZBiJDiABKA1SEGRjdGNw'
    'SW5mb0VuYWJsZWQSLgoTZGN0Y3BfaW5mb19jZV9zdGF0ZRiKDiABKA1SEGRjdGNwSW5mb0NlU3'
    'RhdGUSKQoQZGN0Y3BfaW5mb19hbHBoYRiLDiABKA1SDmRjdGNwSW5mb0FscGhhEioKEWRjdGNw'
    'X2luZm9fYWJfZWNuGIwOIAEoDVIOZGN0Y3BJbmZvQWJFY24SKgoRZGN0Y3BfaW5mb19hYl90b3'
    'QYjQ4gASgNUg5kY3RjcEluZm9BYlRvdBIkCg5iYnJfaW5mb19id19sbxjtDiABKA1SC2Jicklu'
    'Zm9Cd0xvEiQKDmJicl9pbmZvX2J3X2hpGO4OIAEoDVILYmJySW5mb0J3SGkSKAoQYmJyX2luZm'
    '9fbWluX3J0dBjvDiABKA1SDWJickluZm9NaW5SdHQSMAoUYmJyX2luZm9fcGFjaW5nX2dhaW4Y'
    '8A4gASgNUhFiYnJJbmZvUGFjaW5nR2FpbhIsChJiYnJfaW5mb19jd25kX2dhaW4Y8Q4gASgNUg'
    '9iYnJJbmZvQ3duZEdhaW4SLAoSaW5ldF9kaWFnX2NsYXNzX2lkGNEPIAEoDVIPaW5ldERpYWdD'
    'bGFzc0lkEisKEWluZXRfZGlhZ19zb2Nrb3B0GNIPIAEoDVIPaW5ldERpYWdTb2Nrb3B0Ei4KE2'
    'luZXRfZGlhZ19jZ3JvdXBfaWQY0w8gASgEUhBpbmV0RGlhZ0Nncm91cElkImcKCExvY2FsaXR5'
    'EhgKFExPQ0FMSVRZX1VOU1BFQ0lGSUVEEAASEQoNTE9DQUxJVFlfU0VMRhABEhkKFUxPQ0FMSV'
    'RZX0xPQ0FMX1NVQk5FVBACEhMKD0xPQ0FMSVRZX1JFTU9URRADIpkCChNDb25nZXN0aW9uQWxn'
    'b3JpdGhtEiQKIENPTkdFU1RJT05fQUxHT1JJVEhNX1VOU1BFQ0lGSUVEEAASHgoaQ09OR0VTVE'
    'lPTl9BTEdPUklUSE1fQ1VCSUMQARIeChpDT05HRVNUSU9OX0FMR09SSVRITV9EQ1RDUBACEh4K'
    'GkNPTkdFU1RJT05fQUxHT1JJVEhNX1ZFR0FTEAMSHwobQ09OR0VTVElPTl9BTEdPUklUSE1fUF'
    'JBR1VFEAQSHQoZQ09OR0VTVElPTl9BTEdPUklUSE1fQkJSMRAFEh0KGUNPTkdFU1RJT05fQUxH'
    'T1JJVEhNX0JCUjIQBhIdChlDT05HRVNUSU9OX0FMR09SSVRITV9CQlIzEAdKBgitAhCuAkoGCK'
    '4CEK8CSgYI8wcQ9AdKBgj0BxD1B0oGCPoHEPsHSgYI+wcQ/AdKBgi3EBC4EFIdaW5ldF9kaWFn'
    'X21zZ19zb2NrZXRfZGVzdF9hc25SIWluZXRfZGlhZ19tc2dfc29ja2V0X25leHRfaG9wX2Fzbl'
    'InaW5ldF9kaWFnX21zZ19zb2NrZXRfZGVzdF9uZXR3b3JrX293bmVyUiJpbmV0X2RpYWdfbXNn'
    'X3NvY2tldF9kZXN0X2xvY2FsaXR5UhplbnJpY2hfc29ja2V0X25leHRfaG9wX2FzblITdGNwX2'
    'luZm9fc2VuZF9zY2FsZVISdGNwX2luZm9fcmN2X3NjYWxlUiB0Y3BfaW5mb19mYXN0X29wZW5f'
    'Y2xpZW50X2ZhaWxlZFIQdGNwX2luZm9fcnR0X3ZhclIQdGNwX2luZm9fYWR2X21zc1IXdGNwX2'
    'luZm9fbm90X3NlbnRfYnl0ZXNSE3NrX21lbV9pbmZvX3Jjdl9idWZSE3NrX21lbV9pbmZvX3Nu'
    'ZF9idWZSEnZlZ2FzX2luZm9fcnR0X2NudFISdmVnYXNfaW5mb19taW5fcnR0Uhtjb25nZXN0aW'
    '9uX2FsZ29yaXRobV9zdHJpbmdSGWNvbmdlc3Rpb25fYWxnb3JpdGhtX2VudW1SD3R5cGVfb2Zf'
    'c2VydmljZVINdHJhZmZpY19jbGFzc1IOc2h1dGRvd25fc3RhdGVSCGNsYXNzX2lkUghzb2NrX2'
    '9wdFIHY19ncm91cA==');

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
