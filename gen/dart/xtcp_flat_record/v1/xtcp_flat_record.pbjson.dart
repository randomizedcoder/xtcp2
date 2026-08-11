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
    {'1': 'timestamp_ns', '3': 10, '4': 1, '5': 3, '10': 'timestampNs'},
    {'1': 'hostname', '3': 20, '4': 1, '5': 9, '10': 'hostname'},
    {'1': 'location', '3': 21, '4': 1, '5': 9, '10': 'location'},
    {'1': 'netns', '3': 30, '4': 1, '5': 9, '10': 'netns'},
    {'1': 'netns_inode', '3': 33, '4': 1, '5': 4, '10': 'netnsInode'},
    {'1': 'container_id', '3': 31, '4': 1, '5': 9, '10': 'containerId'},
    {
      '1': 'container_runtime',
      '3': 32,
      '4': 1,
      '5': 9,
      '10': 'containerRuntime'
    },
    {'1': 'nsid', '3': 40, '4': 1, '5': 13, '10': 'nsid'},
    {'1': 'label', '3': 50, '4': 1, '5': 9, '10': 'label'},
    {'1': 'tag', '3': 60, '4': 1, '5': 9, '10': 'tag'},
    {'1': 'record_counter', '3': 70, '4': 1, '5': 4, '10': 'recordCounter'},
    {'1': 'socket_fd', '3': 80, '4': 1, '5': 4, '10': 'socketFd'},
    {'1': 'netlinker_id', '3': 90, '4': 1, '5': 4, '10': 'netlinkerId'},
    {
      '1': 'inet_diag_msg_family',
      '3': 101,
      '4': 1,
      '5': 13,
      '10': 'inetDiagMsgFamily'
    },
    {
      '1': 'inet_diag_msg_state',
      '3': 102,
      '4': 1,
      '5': 13,
      '10': 'inetDiagMsgState'
    },
    {
      '1': 'inet_diag_msg_timer',
      '3': 103,
      '4': 1,
      '5': 13,
      '10': 'inetDiagMsgTimer'
    },
    {
      '1': 'inet_diag_msg_retrans',
      '3': 104,
      '4': 1,
      '5': 13,
      '10': 'inetDiagMsgRetrans'
    },
    {
      '1': 'inet_diag_msg_socket_source_port',
      '3': 105,
      '4': 1,
      '5': 13,
      '10': 'inetDiagMsgSocketSourcePort'
    },
    {
      '1': 'inet_diag_msg_socket_destination_port',
      '3': 106,
      '4': 1,
      '5': 13,
      '10': 'inetDiagMsgSocketDestinationPort'
    },
    {
      '1': 'inet_diag_msg_socket_source',
      '3': 107,
      '4': 1,
      '5': 12,
      '10': 'inetDiagMsgSocketSource'
    },
    {
      '1': 'inet_diag_msg_socket_destination',
      '3': 108,
      '4': 1,
      '5': 12,
      '10': 'inetDiagMsgSocketDestination'
    },
    {
      '1': 'inet_diag_msg_socket_interface',
      '3': 109,
      '4': 1,
      '5': 13,
      '10': 'inetDiagMsgSocketInterface'
    },
    {
      '1': 'inet_diag_msg_socket_cookie',
      '3': 110,
      '4': 1,
      '5': 4,
      '10': 'inetDiagMsgSocketCookie'
    },
    {
      '1': 'inet_diag_msg_socket_dest_asn',
      '3': 111,
      '4': 1,
      '5': 4,
      '10': 'inetDiagMsgSocketDestAsn'
    },
    {
      '1': 'inet_diag_msg_socket_next_hop_asn',
      '3': 112,
      '4': 1,
      '5': 4,
      '10': 'inetDiagMsgSocketNextHopAsn'
    },
    {
      '1': 'inet_diag_msg_expires',
      '3': 113,
      '4': 1,
      '5': 13,
      '10': 'inetDiagMsgExpires'
    },
    {
      '1': 'inet_diag_msg_rqueue',
      '3': 114,
      '4': 1,
      '5': 13,
      '10': 'inetDiagMsgRqueue'
    },
    {
      '1': 'inet_diag_msg_wqueue',
      '3': 115,
      '4': 1,
      '5': 13,
      '10': 'inetDiagMsgWqueue'
    },
    {
      '1': 'inet_diag_msg_uid',
      '3': 116,
      '4': 1,
      '5': 13,
      '10': 'inetDiagMsgUid'
    },
    {
      '1': 'inet_diag_msg_inode',
      '3': 117,
      '4': 1,
      '5': 13,
      '10': 'inetDiagMsgInode'
    },
    {'1': 'mem_info_rmem', '3': 201, '4': 1, '5': 13, '10': 'memInfoRmem'},
    {'1': 'mem_info_wmem', '3': 202, '4': 1, '5': 13, '10': 'memInfoWmem'},
    {'1': 'mem_info_fmem', '3': 203, '4': 1, '5': 13, '10': 'memInfoFmem'},
    {'1': 'mem_info_tmem', '3': 204, '4': 1, '5': 13, '10': 'memInfoTmem'},
    {'1': 'tcp_info_state', '3': 301, '4': 1, '5': 13, '10': 'tcpInfoState'},
    {
      '1': 'tcp_info_ca_state',
      '3': 302,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoCaState'
    },
    {
      '1': 'tcp_info_retransmits',
      '3': 303,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoRetransmits'
    },
    {'1': 'tcp_info_probes', '3': 304, '4': 1, '5': 13, '10': 'tcpInfoProbes'},
    {
      '1': 'tcp_info_backoff',
      '3': 305,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoBackoff'
    },
    {
      '1': 'tcp_info_options',
      '3': 306,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoOptions'
    },
    {
      '1': 'tcp_info_send_scale',
      '3': 307,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoSendScale'
    },
    {
      '1': 'tcp_info_rcv_scale',
      '3': 308,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoRcvScale'
    },
    {
      '1': 'tcp_info_delivery_rate_app_limited',
      '3': 309,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoDeliveryRateAppLimited'
    },
    {
      '1': 'tcp_info_fast_open_client_failed',
      '3': 310,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoFastOpenClientFailed'
    },
    {'1': 'tcp_info_rto', '3': 315, '4': 1, '5': 13, '10': 'tcpInfoRto'},
    {'1': 'tcp_info_ato', '3': 316, '4': 1, '5': 13, '10': 'tcpInfoAto'},
    {'1': 'tcp_info_snd_mss', '3': 317, '4': 1, '5': 13, '10': 'tcpInfoSndMss'},
    {'1': 'tcp_info_rcv_mss', '3': 318, '4': 1, '5': 13, '10': 'tcpInfoRcvMss'},
    {
      '1': 'tcp_info_unacked',
      '3': 319,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoUnacked'
    },
    {'1': 'tcp_info_sacked', '3': 320, '4': 1, '5': 13, '10': 'tcpInfoSacked'},
    {'1': 'tcp_info_lost', '3': 321, '4': 1, '5': 13, '10': 'tcpInfoLost'},
    {
      '1': 'tcp_info_retrans',
      '3': 322,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoRetrans'
    },
    {
      '1': 'tcp_info_fackets',
      '3': 323,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoFackets'
    },
    {
      '1': 'tcp_info_last_data_sent',
      '3': 324,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoLastDataSent'
    },
    {
      '1': 'tcp_info_last_ack_sent',
      '3': 325,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoLastAckSent'
    },
    {
      '1': 'tcp_info_last_data_recv',
      '3': 326,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoLastDataRecv'
    },
    {
      '1': 'tcp_info_last_ack_recv',
      '3': 327,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoLastAckRecv'
    },
    {'1': 'tcp_info_pmtu', '3': 328, '4': 1, '5': 13, '10': 'tcpInfoPmtu'},
    {
      '1': 'tcp_info_rcv_ssthresh',
      '3': 329,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoRcvSsthresh'
    },
    {'1': 'tcp_info_rtt', '3': 330, '4': 1, '5': 13, '10': 'tcpInfoRtt'},
    {'1': 'tcp_info_rtt_var', '3': 331, '4': 1, '5': 13, '10': 'tcpInfoRttVar'},
    {
      '1': 'tcp_info_snd_ssthresh',
      '3': 332,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoSndSsthresh'
    },
    {
      '1': 'tcp_info_snd_cwnd',
      '3': 333,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoSndCwnd'
    },
    {'1': 'tcp_info_adv_mss', '3': 334, '4': 1, '5': 13, '10': 'tcpInfoAdvMss'},
    {
      '1': 'tcp_info_reordering',
      '3': 335,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoReordering'
    },
    {'1': 'tcp_info_rcv_rtt', '3': 336, '4': 1, '5': 13, '10': 'tcpInfoRcvRtt'},
    {
      '1': 'tcp_info_rcv_space',
      '3': 337,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoRcvSpace'
    },
    {
      '1': 'tcp_info_total_retrans',
      '3': 338,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoTotalRetrans'
    },
    {
      '1': 'tcp_info_pacing_rate',
      '3': 339,
      '4': 1,
      '5': 4,
      '10': 'tcpInfoPacingRate'
    },
    {
      '1': 'tcp_info_max_pacing_rate',
      '3': 340,
      '4': 1,
      '5': 4,
      '10': 'tcpInfoMaxPacingRate'
    },
    {
      '1': 'tcp_info_bytes_acked',
      '3': 341,
      '4': 1,
      '5': 4,
      '10': 'tcpInfoBytesAcked'
    },
    {
      '1': 'tcp_info_bytes_received',
      '3': 342,
      '4': 1,
      '5': 4,
      '10': 'tcpInfoBytesReceived'
    },
    {
      '1': 'tcp_info_segs_out',
      '3': 343,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoSegsOut'
    },
    {'1': 'tcp_info_segs_in', '3': 344, '4': 1, '5': 13, '10': 'tcpInfoSegsIn'},
    {
      '1': 'tcp_info_not_sent_bytes',
      '3': 345,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoNotSentBytes'
    },
    {'1': 'tcp_info_min_rtt', '3': 346, '4': 1, '5': 13, '10': 'tcpInfoMinRtt'},
    {
      '1': 'tcp_info_data_segs_in',
      '3': 347,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoDataSegsIn'
    },
    {
      '1': 'tcp_info_data_segs_out',
      '3': 348,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoDataSegsOut'
    },
    {
      '1': 'tcp_info_delivery_rate',
      '3': 349,
      '4': 1,
      '5': 4,
      '10': 'tcpInfoDeliveryRate'
    },
    {
      '1': 'tcp_info_busy_time',
      '3': 350,
      '4': 1,
      '5': 4,
      '10': 'tcpInfoBusyTime'
    },
    {
      '1': 'tcp_info_rwnd_limited',
      '3': 351,
      '4': 1,
      '5': 4,
      '10': 'tcpInfoRwndLimited'
    },
    {
      '1': 'tcp_info_sndbuf_limited',
      '3': 352,
      '4': 1,
      '5': 4,
      '10': 'tcpInfoSndbufLimited'
    },
    {
      '1': 'tcp_info_delivered',
      '3': 353,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoDelivered'
    },
    {
      '1': 'tcp_info_delivered_ce',
      '3': 354,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoDeliveredCe'
    },
    {
      '1': 'tcp_info_bytes_sent',
      '3': 355,
      '4': 1,
      '5': 4,
      '10': 'tcpInfoBytesSent'
    },
    {
      '1': 'tcp_info_bytes_retrans',
      '3': 356,
      '4': 1,
      '5': 4,
      '10': 'tcpInfoBytesRetrans'
    },
    {
      '1': 'tcp_info_dsack_dups',
      '3': 357,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoDsackDups'
    },
    {
      '1': 'tcp_info_reord_seen',
      '3': 358,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoReordSeen'
    },
    {
      '1': 'tcp_info_rcv_ooopack',
      '3': 359,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoRcvOoopack'
    },
    {'1': 'tcp_info_snd_wnd', '3': 360, '4': 1, '5': 13, '10': 'tcpInfoSndWnd'},
    {'1': 'tcp_info_rcv_wnd', '3': 361, '4': 1, '5': 13, '10': 'tcpInfoRcvWnd'},
    {'1': 'tcp_info_rehash', '3': 362, '4': 1, '5': 13, '10': 'tcpInfoRehash'},
    {
      '1': 'tcp_info_total_rto',
      '3': 363,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoTotalRto'
    },
    {
      '1': 'tcp_info_total_rto_recoveries',
      '3': 364,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoTotalRtoRecoveries'
    },
    {
      '1': 'tcp_info_total_rto_time',
      '3': 365,
      '4': 1,
      '5': 13,
      '10': 'tcpInfoTotalRtoTime'
    },
    {
      '1': 'congestion_algorithm_string',
      '3': 400,
      '4': 1,
      '5': 9,
      '10': 'congestionAlgorithmString'
    },
    {
      '1': 'congestion_algorithm_enum',
      '3': 401,
      '4': 1,
      '5': 14,
      '6': '.xtcp_flat_record.v1.XtcpFlatRecord.CongestionAlgorithm',
      '10': 'congestionAlgorithmEnum'
    },
    {'1': 'type_of_service', '3': 501, '4': 1, '5': 13, '10': 'typeOfService'},
    {'1': 'traffic_class', '3': 502, '4': 1, '5': 13, '10': 'trafficClass'},
    {
      '1': 'sk_mem_info_rmem_alloc',
      '3': 601,
      '4': 1,
      '5': 13,
      '10': 'skMemInfoRmemAlloc'
    },
    {
      '1': 'sk_mem_info_rcv_buf',
      '3': 602,
      '4': 1,
      '5': 13,
      '10': 'skMemInfoRcvBuf'
    },
    {
      '1': 'sk_mem_info_wmem_alloc',
      '3': 603,
      '4': 1,
      '5': 13,
      '10': 'skMemInfoWmemAlloc'
    },
    {
      '1': 'sk_mem_info_snd_buf',
      '3': 604,
      '4': 1,
      '5': 13,
      '10': 'skMemInfoSndBuf'
    },
    {
      '1': 'sk_mem_info_fwd_alloc',
      '3': 605,
      '4': 1,
      '5': 13,
      '10': 'skMemInfoFwdAlloc'
    },
    {
      '1': 'sk_mem_info_wmem_queued',
      '3': 606,
      '4': 1,
      '5': 13,
      '10': 'skMemInfoWmemQueued'
    },
    {
      '1': 'sk_mem_info_optmem',
      '3': 607,
      '4': 1,
      '5': 13,
      '10': 'skMemInfoOptmem'
    },
    {
      '1': 'sk_mem_info_backlog',
      '3': 608,
      '4': 1,
      '5': 13,
      '10': 'skMemInfoBacklog'
    },
    {
      '1': 'sk_mem_info_drops',
      '3': 609,
      '4': 1,
      '5': 13,
      '10': 'skMemInfoDrops'
    },
    {'1': 'shutdown_state', '3': 700, '4': 1, '5': 13, '10': 'shutdownState'},
    {
      '1': 'vegas_info_enabled',
      '3': 801,
      '4': 1,
      '5': 13,
      '10': 'vegasInfoEnabled'
    },
    {
      '1': 'vegas_info_rtt_cnt',
      '3': 802,
      '4': 1,
      '5': 13,
      '10': 'vegasInfoRttCnt'
    },
    {'1': 'vegas_info_rtt', '3': 803, '4': 1, '5': 13, '10': 'vegasInfoRtt'},
    {
      '1': 'vegas_info_min_rtt',
      '3': 804,
      '4': 1,
      '5': 13,
      '10': 'vegasInfoMinRtt'
    },
    {
      '1': 'dctcp_info_enabled',
      '3': 901,
      '4': 1,
      '5': 13,
      '10': 'dctcpInfoEnabled'
    },
    {
      '1': 'dctcp_info_ce_state',
      '3': 902,
      '4': 1,
      '5': 13,
      '10': 'dctcpInfoCeState'
    },
    {
      '1': 'dctcp_info_alpha',
      '3': 903,
      '4': 1,
      '5': 13,
      '10': 'dctcpInfoAlpha'
    },
    {
      '1': 'dctcp_info_ab_ecn',
      '3': 904,
      '4': 1,
      '5': 13,
      '10': 'dctcpInfoAbEcn'
    },
    {
      '1': 'dctcp_info_ab_tot',
      '3': 905,
      '4': 1,
      '5': 13,
      '10': 'dctcpInfoAbTot'
    },
    {'1': 'bbr_info_bw_lo', '3': 1001, '4': 1, '5': 13, '10': 'bbrInfoBwLo'},
    {'1': 'bbr_info_bw_hi', '3': 1002, '4': 1, '5': 13, '10': 'bbrInfoBwHi'},
    {
      '1': 'bbr_info_min_rtt',
      '3': 1003,
      '4': 1,
      '5': 13,
      '10': 'bbrInfoMinRtt'
    },
    {
      '1': 'bbr_info_pacing_gain',
      '3': 1004,
      '4': 1,
      '5': 13,
      '10': 'bbrInfoPacingGain'
    },
    {
      '1': 'bbr_info_cwnd_gain',
      '3': 1005,
      '4': 1,
      '5': 13,
      '10': 'bbrInfoCwndGain'
    },
    {'1': 'class_id', '3': 1101, '4': 1, '5': 13, '10': 'classId'},
    {'1': 'sock_opt', '3': 1102, '4': 1, '5': 13, '10': 'sockOpt'},
    {'1': 'c_group', '3': 1203, '4': 1, '5': 4, '10': 'cGroup'},
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
    'Cg5YdGNwRmxhdFJlY29yZBIhCgx0aW1lc3RhbXBfbnMYCiABKANSC3RpbWVzdGFtcE5zEhoKCG'
    'hvc3RuYW1lGBQgASgJUghob3N0bmFtZRIaCghsb2NhdGlvbhgVIAEoCVIIbG9jYXRpb24SFAoF'
    'bmV0bnMYHiABKAlSBW5ldG5zEh8KC25ldG5zX2lub2RlGCEgASgEUgpuZXRuc0lub2RlEiEKDG'
    'NvbnRhaW5lcl9pZBgfIAEoCVILY29udGFpbmVySWQSKwoRY29udGFpbmVyX3J1bnRpbWUYICAB'
    'KAlSEGNvbnRhaW5lclJ1bnRpbWUSEgoEbnNpZBgoIAEoDVIEbnNpZBIUCgVsYWJlbBgyIAEoCV'
    'IFbGFiZWwSEAoDdGFnGDwgASgJUgN0YWcSJQoOcmVjb3JkX2NvdW50ZXIYRiABKARSDXJlY29y'
    'ZENvdW50ZXISGwoJc29ja2V0X2ZkGFAgASgEUghzb2NrZXRGZBIhCgxuZXRsaW5rZXJfaWQYWi'
    'ABKARSC25ldGxpbmtlcklkEi8KFGluZXRfZGlhZ19tc2dfZmFtaWx5GGUgASgNUhFpbmV0RGlh'
    'Z01zZ0ZhbWlseRItChNpbmV0X2RpYWdfbXNnX3N0YXRlGGYgASgNUhBpbmV0RGlhZ01zZ1N0YX'
    'RlEi0KE2luZXRfZGlhZ19tc2dfdGltZXIYZyABKA1SEGluZXREaWFnTXNnVGltZXISMQoVaW5l'
    'dF9kaWFnX21zZ19yZXRyYW5zGGggASgNUhJpbmV0RGlhZ01zZ1JldHJhbnMSRQogaW5ldF9kaW'
    'FnX21zZ19zb2NrZXRfc291cmNlX3BvcnQYaSABKA1SG2luZXREaWFnTXNnU29ja2V0U291cmNl'
    'UG9ydBJPCiVpbmV0X2RpYWdfbXNnX3NvY2tldF9kZXN0aW5hdGlvbl9wb3J0GGogASgNUiBpbm'
    'V0RGlhZ01zZ1NvY2tldERlc3RpbmF0aW9uUG9ydBI8ChtpbmV0X2RpYWdfbXNnX3NvY2tldF9z'
    'b3VyY2UYayABKAxSF2luZXREaWFnTXNnU29ja2V0U291cmNlEkYKIGluZXRfZGlhZ19tc2dfc2'
    '9ja2V0X2Rlc3RpbmF0aW9uGGwgASgMUhxpbmV0RGlhZ01zZ1NvY2tldERlc3RpbmF0aW9uEkIK'
    'HmluZXRfZGlhZ19tc2dfc29ja2V0X2ludGVyZmFjZRhtIAEoDVIaaW5ldERpYWdNc2dTb2NrZX'
    'RJbnRlcmZhY2USPAobaW5ldF9kaWFnX21zZ19zb2NrZXRfY29va2llGG4gASgEUhdpbmV0RGlh'
    'Z01zZ1NvY2tldENvb2tpZRI/Ch1pbmV0X2RpYWdfbXNnX3NvY2tldF9kZXN0X2FzbhhvIAEoBF'
    'IYaW5ldERpYWdNc2dTb2NrZXREZXN0QXNuEkYKIWluZXRfZGlhZ19tc2dfc29ja2V0X25leHRf'
    'aG9wX2FzbhhwIAEoBFIbaW5ldERpYWdNc2dTb2NrZXROZXh0SG9wQXNuEjEKFWluZXRfZGlhZ1'
    '9tc2dfZXhwaXJlcxhxIAEoDVISaW5ldERpYWdNc2dFeHBpcmVzEi8KFGluZXRfZGlhZ19tc2df'
    'cnF1ZXVlGHIgASgNUhFpbmV0RGlhZ01zZ1JxdWV1ZRIvChRpbmV0X2RpYWdfbXNnX3dxdWV1ZR'
    'hzIAEoDVIRaW5ldERpYWdNc2dXcXVldWUSKQoRaW5ldF9kaWFnX21zZ191aWQYdCABKA1SDmlu'
    'ZXREaWFnTXNnVWlkEi0KE2luZXRfZGlhZ19tc2dfaW5vZGUYdSABKA1SEGluZXREaWFnTXNnSW'
    '5vZGUSIwoNbWVtX2luZm9fcm1lbRjJASABKA1SC21lbUluZm9SbWVtEiMKDW1lbV9pbmZvX3dt'
    'ZW0YygEgASgNUgttZW1JbmZvV21lbRIjCg1tZW1faW5mb19mbWVtGMsBIAEoDVILbWVtSW5mb0'
    'ZtZW0SIwoNbWVtX2luZm9fdG1lbRjMASABKA1SC21lbUluZm9UbWVtEiUKDnRjcF9pbmZvX3N0'
    'YXRlGK0CIAEoDVIMdGNwSW5mb1N0YXRlEioKEXRjcF9pbmZvX2NhX3N0YXRlGK4CIAEoDVIOdG'
    'NwSW5mb0NhU3RhdGUSMQoUdGNwX2luZm9fcmV0cmFuc21pdHMYrwIgASgNUhJ0Y3BJbmZvUmV0'
    'cmFuc21pdHMSJwoPdGNwX2luZm9fcHJvYmVzGLACIAEoDVINdGNwSW5mb1Byb2JlcxIpChB0Y3'
    'BfaW5mb19iYWNrb2ZmGLECIAEoDVIOdGNwSW5mb0JhY2tvZmYSKQoQdGNwX2luZm9fb3B0aW9u'
    'cxiyAiABKA1SDnRjcEluZm9PcHRpb25zEi4KE3RjcF9pbmZvX3NlbmRfc2NhbGUYswIgASgNUh'
    'B0Y3BJbmZvU2VuZFNjYWxlEiwKEnRjcF9pbmZvX3Jjdl9zY2FsZRi0AiABKA1SD3RjcEluZm9S'
    'Y3ZTY2FsZRJKCiJ0Y3BfaW5mb19kZWxpdmVyeV9yYXRlX2FwcF9saW1pdGVkGLUCIAEoDVIddG'
    'NwSW5mb0RlbGl2ZXJ5UmF0ZUFwcExpbWl0ZWQSRgogdGNwX2luZm9fZmFzdF9vcGVuX2NsaWVu'
    'dF9mYWlsZWQYtgIgASgNUht0Y3BJbmZvRmFzdE9wZW5DbGllbnRGYWlsZWQSIQoMdGNwX2luZm'
    '9fcnRvGLsCIAEoDVIKdGNwSW5mb1J0bxIhCgx0Y3BfaW5mb19hdG8YvAIgASgNUgp0Y3BJbmZv'
    'QXRvEigKEHRjcF9pbmZvX3NuZF9tc3MYvQIgASgNUg10Y3BJbmZvU25kTXNzEigKEHRjcF9pbm'
    'ZvX3Jjdl9tc3MYvgIgASgNUg10Y3BJbmZvUmN2TXNzEikKEHRjcF9pbmZvX3VuYWNrZWQYvwIg'
    'ASgNUg50Y3BJbmZvVW5hY2tlZBInCg90Y3BfaW5mb19zYWNrZWQYwAIgASgNUg10Y3BJbmZvU2'
    'Fja2VkEiMKDXRjcF9pbmZvX2xvc3QYwQIgASgNUgt0Y3BJbmZvTG9zdBIpChB0Y3BfaW5mb19y'
    'ZXRyYW5zGMICIAEoDVIOdGNwSW5mb1JldHJhbnMSKQoQdGNwX2luZm9fZmFja2V0cxjDAiABKA'
    '1SDnRjcEluZm9GYWNrZXRzEjUKF3RjcF9pbmZvX2xhc3RfZGF0YV9zZW50GMQCIAEoDVITdGNw'
    'SW5mb0xhc3REYXRhU2VudBIzChZ0Y3BfaW5mb19sYXN0X2Fja19zZW50GMUCIAEoDVISdGNwSW'
    '5mb0xhc3RBY2tTZW50EjUKF3RjcF9pbmZvX2xhc3RfZGF0YV9yZWN2GMYCIAEoDVITdGNwSW5m'
    'b0xhc3REYXRhUmVjdhIzChZ0Y3BfaW5mb19sYXN0X2Fja19yZWN2GMcCIAEoDVISdGNwSW5mb0'
    'xhc3RBY2tSZWN2EiMKDXRjcF9pbmZvX3BtdHUYyAIgASgNUgt0Y3BJbmZvUG10dRIyChV0Y3Bf'
    'aW5mb19yY3Zfc3N0aHJlc2gYyQIgASgNUhJ0Y3BJbmZvUmN2U3N0aHJlc2gSIQoMdGNwX2luZm'
    '9fcnR0GMoCIAEoDVIKdGNwSW5mb1J0dBIoChB0Y3BfaW5mb19ydHRfdmFyGMsCIAEoDVINdGNw'
    'SW5mb1J0dFZhchIyChV0Y3BfaW5mb19zbmRfc3N0aHJlc2gYzAIgASgNUhJ0Y3BJbmZvU25kU3'
    'N0aHJlc2gSKgoRdGNwX2luZm9fc25kX2N3bmQYzQIgASgNUg50Y3BJbmZvU25kQ3duZBIoChB0'
    'Y3BfaW5mb19hZHZfbXNzGM4CIAEoDVINdGNwSW5mb0Fkdk1zcxIvChN0Y3BfaW5mb19yZW9yZG'
    'VyaW5nGM8CIAEoDVIRdGNwSW5mb1Jlb3JkZXJpbmcSKAoQdGNwX2luZm9fcmN2X3J0dBjQAiAB'
    'KA1SDXRjcEluZm9SY3ZSdHQSLAoSdGNwX2luZm9fcmN2X3NwYWNlGNECIAEoDVIPdGNwSW5mb1'
    'JjdlNwYWNlEjQKFnRjcF9pbmZvX3RvdGFsX3JldHJhbnMY0gIgASgNUhN0Y3BJbmZvVG90YWxS'
    'ZXRyYW5zEjAKFHRjcF9pbmZvX3BhY2luZ19yYXRlGNMCIAEoBFIRdGNwSW5mb1BhY2luZ1JhdG'
    'USNwoYdGNwX2luZm9fbWF4X3BhY2luZ19yYXRlGNQCIAEoBFIUdGNwSW5mb01heFBhY2luZ1Jh'
    'dGUSMAoUdGNwX2luZm9fYnl0ZXNfYWNrZWQY1QIgASgEUhF0Y3BJbmZvQnl0ZXNBY2tlZBI2Ch'
    'd0Y3BfaW5mb19ieXRlc19yZWNlaXZlZBjWAiABKARSFHRjcEluZm9CeXRlc1JlY2VpdmVkEioK'
    'EXRjcF9pbmZvX3NlZ3Nfb3V0GNcCIAEoDVIOdGNwSW5mb1NlZ3NPdXQSKAoQdGNwX2luZm9fc2'
    'Vnc19pbhjYAiABKA1SDXRjcEluZm9TZWdzSW4SNQoXdGNwX2luZm9fbm90X3NlbnRfYnl0ZXMY'
    '2QIgASgNUhN0Y3BJbmZvTm90U2VudEJ5dGVzEigKEHRjcF9pbmZvX21pbl9ydHQY2gIgASgNUg'
    '10Y3BJbmZvTWluUnR0EjEKFXRjcF9pbmZvX2RhdGFfc2Vnc19pbhjbAiABKA1SEXRjcEluZm9E'
    'YXRhU2Vnc0luEjMKFnRjcF9pbmZvX2RhdGFfc2Vnc19vdXQY3AIgASgNUhJ0Y3BJbmZvRGF0YV'
    'NlZ3NPdXQSNAoWdGNwX2luZm9fZGVsaXZlcnlfcmF0ZRjdAiABKARSE3RjcEluZm9EZWxpdmVy'
    'eVJhdGUSLAoSdGNwX2luZm9fYnVzeV90aW1lGN4CIAEoBFIPdGNwSW5mb0J1c3lUaW1lEjIKFX'
    'RjcF9pbmZvX3J3bmRfbGltaXRlZBjfAiABKARSEnRjcEluZm9Sd25kTGltaXRlZBI2Chd0Y3Bf'
    'aW5mb19zbmRidWZfbGltaXRlZBjgAiABKARSFHRjcEluZm9TbmRidWZMaW1pdGVkEi0KEnRjcF'
    '9pbmZvX2RlbGl2ZXJlZBjhAiABKA1SEHRjcEluZm9EZWxpdmVyZWQSMgoVdGNwX2luZm9fZGVs'
    'aXZlcmVkX2NlGOICIAEoDVISdGNwSW5mb0RlbGl2ZXJlZENlEi4KE3RjcF9pbmZvX2J5dGVzX3'
    'NlbnQY4wIgASgEUhB0Y3BJbmZvQnl0ZXNTZW50EjQKFnRjcF9pbmZvX2J5dGVzX3JldHJhbnMY'
    '5AIgASgEUhN0Y3BJbmZvQnl0ZXNSZXRyYW5zEi4KE3RjcF9pbmZvX2RzYWNrX2R1cHMY5QIgAS'
    'gNUhB0Y3BJbmZvRHNhY2tEdXBzEi4KE3RjcF9pbmZvX3Jlb3JkX3NlZW4Y5gIgASgNUhB0Y3BJ'
    'bmZvUmVvcmRTZWVuEjAKFHRjcF9pbmZvX3Jjdl9vb29wYWNrGOcCIAEoDVIRdGNwSW5mb1Jjdk'
    '9vb3BhY2sSKAoQdGNwX2luZm9fc25kX3duZBjoAiABKA1SDXRjcEluZm9TbmRXbmQSKAoQdGNw'
    'X2luZm9fcmN2X3duZBjpAiABKA1SDXRjcEluZm9SY3ZXbmQSJwoPdGNwX2luZm9fcmVoYXNoGO'
    'oCIAEoDVINdGNwSW5mb1JlaGFzaBIsChJ0Y3BfaW5mb190b3RhbF9ydG8Y6wIgASgNUg90Y3BJ'
    'bmZvVG90YWxSdG8SQQoddGNwX2luZm9fdG90YWxfcnRvX3JlY292ZXJpZXMY7AIgASgNUhl0Y3'
    'BJbmZvVG90YWxSdG9SZWNvdmVyaWVzEjUKF3RjcF9pbmZvX3RvdGFsX3J0b190aW1lGO0CIAEo'
    'DVITdGNwSW5mb1RvdGFsUnRvVGltZRI/Chtjb25nZXN0aW9uX2FsZ29yaXRobV9zdHJpbmcYkA'
    'MgASgJUhljb25nZXN0aW9uQWxnb3JpdGhtU3RyaW5nEnQKGWNvbmdlc3Rpb25fYWxnb3JpdGht'
    'X2VudW0YkQMgASgOMjcueHRjcF9mbGF0X3JlY29yZC52MS5YdGNwRmxhdFJlY29yZC5Db25nZX'
    'N0aW9uQWxnb3JpdGhtUhdjb25nZXN0aW9uQWxnb3JpdGhtRW51bRInCg90eXBlX29mX3NlcnZp'
    'Y2UY9QMgASgNUg10eXBlT2ZTZXJ2aWNlEiQKDXRyYWZmaWNfY2xhc3MY9gMgASgNUgx0cmFmZm'
    'ljQ2xhc3MSMwoWc2tfbWVtX2luZm9fcm1lbV9hbGxvYxjZBCABKA1SEnNrTWVtSW5mb1JtZW1B'
    'bGxvYxItChNza19tZW1faW5mb19yY3ZfYnVmGNoEIAEoDVIPc2tNZW1JbmZvUmN2QnVmEjMKFn'
    'NrX21lbV9pbmZvX3dtZW1fYWxsb2MY2wQgASgNUhJza01lbUluZm9XbWVtQWxsb2MSLQoTc2tf'
    'bWVtX2luZm9fc25kX2J1ZhjcBCABKA1SD3NrTWVtSW5mb1NuZEJ1ZhIxChVza19tZW1faW5mb1'
    '9md2RfYWxsb2MY3QQgASgNUhFza01lbUluZm9Gd2RBbGxvYxI1Chdza19tZW1faW5mb193bWVt'
    'X3F1ZXVlZBjeBCABKA1SE3NrTWVtSW5mb1dtZW1RdWV1ZWQSLAoSc2tfbWVtX2luZm9fb3B0bW'
    'VtGN8EIAEoDVIPc2tNZW1JbmZvT3B0bWVtEi4KE3NrX21lbV9pbmZvX2JhY2tsb2cY4AQgASgN'
    'UhBza01lbUluZm9CYWNrbG9nEioKEXNrX21lbV9pbmZvX2Ryb3BzGOEEIAEoDVIOc2tNZW1Jbm'
    'ZvRHJvcHMSJgoOc2h1dGRvd25fc3RhdGUYvAUgASgNUg1zaHV0ZG93blN0YXRlEi0KEnZlZ2Fz'
    'X2luZm9fZW5hYmxlZBihBiABKA1SEHZlZ2FzSW5mb0VuYWJsZWQSLAoSdmVnYXNfaW5mb19ydH'
    'RfY250GKIGIAEoDVIPdmVnYXNJbmZvUnR0Q250EiUKDnZlZ2FzX2luZm9fcnR0GKMGIAEoDVIM'
    'dmVnYXNJbmZvUnR0EiwKEnZlZ2FzX2luZm9fbWluX3J0dBikBiABKA1SD3ZlZ2FzSW5mb01pbl'
    'J0dBItChJkY3RjcF9pbmZvX2VuYWJsZWQYhQcgASgNUhBkY3RjcEluZm9FbmFibGVkEi4KE2Rj'
    'dGNwX2luZm9fY2Vfc3RhdGUYhgcgASgNUhBkY3RjcEluZm9DZVN0YXRlEikKEGRjdGNwX2luZm'
    '9fYWxwaGEYhwcgASgNUg5kY3RjcEluZm9BbHBoYRIqChFkY3RjcF9pbmZvX2FiX2VjbhiIByAB'
    'KA1SDmRjdGNwSW5mb0FiRWNuEioKEWRjdGNwX2luZm9fYWJfdG90GIkHIAEoDVIOZGN0Y3BJbm'
    'ZvQWJUb3QSJAoOYmJyX2luZm9fYndfbG8Y6QcgASgNUgtiYnJJbmZvQndMbxIkCg5iYnJfaW5m'
    'b19id19oaRjqByABKA1SC2JickluZm9Cd0hpEigKEGJicl9pbmZvX21pbl9ydHQY6wcgASgNUg'
    '1iYnJJbmZvTWluUnR0EjAKFGJicl9pbmZvX3BhY2luZ19nYWluGOwHIAEoDVIRYmJySW5mb1Bh'
    'Y2luZ0dhaW4SLAoSYmJyX2luZm9fY3duZF9nYWluGO0HIAEoDVIPYmJySW5mb0N3bmRHYWluEh'
    'oKCGNsYXNzX2lkGM0IIAEoDVIHY2xhc3NJZBIaCghzb2NrX29wdBjOCCABKA1SB3NvY2tPcHQS'
    'GAoHY19ncm91cBizCSABKARSBmNHcm91cCKZAgoTQ29uZ2VzdGlvbkFsZ29yaXRobRIkCiBDT0'
    '5HRVNUSU9OX0FMR09SSVRITV9VTlNQRUNJRklFRBAAEh4KGkNPTkdFU1RJT05fQUxHT1JJVEhN'
    'X0NVQklDEAESHgoaQ09OR0VTVElPTl9BTEdPUklUSE1fRENUQ1AQAhIeChpDT05HRVNUSU9OX0'
    'FMR09SSVRITV9WRUdBUxADEh8KG0NPTkdFU1RJT05fQUxHT1JJVEhNX1BSQUdVRRAEEh0KGUNP'
    'TkdFU1RJT05fQUxHT1JJVEhNX0JCUjEQBRIdChlDT05HRVNUSU9OX0FMR09SSVRITV9CQlIyEA'
    'YSHQoZQ09OR0VTVElPTl9BTEdPUklUSE1fQkJSMxAH');

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
