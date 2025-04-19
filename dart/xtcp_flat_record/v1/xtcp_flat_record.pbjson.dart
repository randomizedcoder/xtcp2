//
//  Generated code. Do not modify.
//  source: xtcp_flat_record/v1/xtcp_flat_record.proto
//
// @dart = 2.12

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names, library_prefixes
// ignore_for_file: non_constant_identifier_names, prefer_final_fields
// ignore_for_file: unnecessary_import, unnecessary_this, unused_import

import 'dart:convert' as $convert;
import 'dart:core' as $core;
import 'dart:typed_data' as $typed_data;

@$core.Deprecated('Use envelopeDescriptor instead')
const Envelope$json = {
  '1': 'Envelope',
  '2': [
    {'1': 'row', '3': 10, '4': 3, '5': 11, '6': '.xtcp_flat_record.v1.Envelope.XtcpFlatRecord', '10': 'row'},
  ],
  '3': [Envelope_XtcpFlatRecord$json],
};

@$core.Deprecated('Use envelopeDescriptor instead')
const Envelope_XtcpFlatRecord$json = {
  '1': 'XtcpFlatRecord',
  '2': [
    {'1': 'timestamp_ns', '3': 10, '4': 1, '5': 1, '10': 'timestampNs'},
    {'1': 'hostname', '3': 20, '4': 1, '5': 9, '10': 'hostname'},
    {'1': 'netns', '3': 30, '4': 1, '5': 9, '10': 'netns'},
    {'1': 'nsid', '3': 40, '4': 1, '5': 13, '10': 'nsid'},
    {'1': 'label', '3': 50, '4': 1, '5': 9, '10': 'label'},
    {'1': 'tag', '3': 60, '4': 1, '5': 9, '10': 'tag'},
    {'1': 'record_counter', '3': 70, '4': 1, '5': 4, '10': 'recordCounter'},
    {'1': 'socket_fd', '3': 80, '4': 1, '5': 4, '10': 'socketFd'},
    {'1': 'netlinker_id', '3': 90, '4': 1, '5': 4, '10': 'netlinkerId'},
    {'1': 'inet_diag_msg_family', '3': 101, '4': 1, '5': 13, '10': 'inetDiagMsgFamily'},
    {'1': 'inet_diag_msg_state', '3': 102, '4': 1, '5': 13, '10': 'inetDiagMsgState'},
    {'1': 'inet_diag_msg_timer', '3': 103, '4': 1, '5': 13, '10': 'inetDiagMsgTimer'},
    {'1': 'inet_diag_msg_retrans', '3': 104, '4': 1, '5': 13, '10': 'inetDiagMsgRetrans'},
    {'1': 'inet_diag_msg_socket_source_port', '3': 105, '4': 1, '5': 13, '10': 'inetDiagMsgSocketSourcePort'},
    {'1': 'inet_diag_msg_socket_destination_port', '3': 106, '4': 1, '5': 13, '10': 'inetDiagMsgSocketDestinationPort'},
    {'1': 'inet_diag_msg_socket_source', '3': 107, '4': 1, '5': 12, '10': 'inetDiagMsgSocketSource'},
    {'1': 'inet_diag_msg_socket_destination', '3': 108, '4': 1, '5': 12, '10': 'inetDiagMsgSocketDestination'},
    {'1': 'inet_diag_msg_socket_interface', '3': 109, '4': 1, '5': 13, '10': 'inetDiagMsgSocketInterface'},
    {'1': 'inet_diag_msg_socket_cookie', '3': 110, '4': 1, '5': 4, '10': 'inetDiagMsgSocketCookie'},
    {'1': 'inet_diag_msg_socket_dest_asn', '3': 111, '4': 1, '5': 4, '10': 'inetDiagMsgSocketDestAsn'},
    {'1': 'inet_diag_msg_socket_next_hop_asn', '3': 112, '4': 1, '5': 4, '10': 'inetDiagMsgSocketNextHopAsn'},
    {'1': 'inet_diag_msg_expires', '3': 113, '4': 1, '5': 13, '10': 'inetDiagMsgExpires'},
    {'1': 'inet_diag_msg_rqueue', '3': 114, '4': 1, '5': 13, '10': 'inetDiagMsgRqueue'},
    {'1': 'inet_diag_msg_wqueue', '3': 115, '4': 1, '5': 13, '10': 'inetDiagMsgWqueue'},
    {'1': 'inet_diag_msg_uid', '3': 116, '4': 1, '5': 13, '10': 'inetDiagMsgUid'},
    {'1': 'inet_diag_msg_inode', '3': 117, '4': 1, '5': 13, '10': 'inetDiagMsgInode'},
    {'1': 'mem_info_rmem', '3': 201, '4': 1, '5': 13, '10': 'memInfoRmem'},
    {'1': 'mem_info_wmem', '3': 202, '4': 1, '5': 13, '10': 'memInfoWmem'},
    {'1': 'mem_info_fmem', '3': 203, '4': 1, '5': 13, '10': 'memInfoFmem'},
    {'1': 'mem_info_tmem', '3': 204, '4': 1, '5': 13, '10': 'memInfoTmem'},
    {'1': 'tcp_info_state', '3': 301, '4': 1, '5': 13, '10': 'tcpInfoState'},
    {'1': 'tcp_info_ca_state', '3': 302, '4': 1, '5': 13, '10': 'tcpInfoCaState'},
    {'1': 'tcp_info_retransmits', '3': 303, '4': 1, '5': 13, '10': 'tcpInfoRetransmits'},
    {'1': 'tcp_info_probes', '3': 304, '4': 1, '5': 13, '10': 'tcpInfoProbes'},
    {'1': 'tcp_info_backoff', '3': 305, '4': 1, '5': 13, '10': 'tcpInfoBackoff'},
    {'1': 'tcp_info_options', '3': 306, '4': 1, '5': 13, '10': 'tcpInfoOptions'},
    {'1': 'tcp_info_send_scale', '3': 307, '4': 1, '5': 13, '10': 'tcpInfoSendScale'},
    {'1': 'tcp_info_rcv_scale', '3': 308, '4': 1, '5': 13, '10': 'tcpInfoRcvScale'},
    {'1': 'tcp_info_delivery_rate_app_limited', '3': 309, '4': 1, '5': 13, '10': 'tcpInfoDeliveryRateAppLimited'},
    {'1': 'tcp_info_fast_open_client_failed', '3': 310, '4': 1, '5': 13, '10': 'tcpInfoFastOpenClientFailed'},
    {'1': 'tcp_info_rto', '3': 315, '4': 1, '5': 13, '10': 'tcpInfoRto'},
    {'1': 'tcp_info_ato', '3': 316, '4': 1, '5': 13, '10': 'tcpInfoAto'},
    {'1': 'tcp_info_snd_mss', '3': 317, '4': 1, '5': 13, '10': 'tcpInfoSndMss'},
    {'1': 'tcp_info_rcv_mss', '3': 318, '4': 1, '5': 13, '10': 'tcpInfoRcvMss'},
    {'1': 'tcp_info_unacked', '3': 319, '4': 1, '5': 13, '10': 'tcpInfoUnacked'},
    {'1': 'tcp_info_sacked', '3': 320, '4': 1, '5': 13, '10': 'tcpInfoSacked'},
    {'1': 'tcp_info_lost', '3': 321, '4': 1, '5': 13, '10': 'tcpInfoLost'},
    {'1': 'tcp_info_retrans', '3': 322, '4': 1, '5': 13, '10': 'tcpInfoRetrans'},
    {'1': 'tcp_info_fackets', '3': 323, '4': 1, '5': 13, '10': 'tcpInfoFackets'},
    {'1': 'tcp_info_last_data_sent', '3': 324, '4': 1, '5': 13, '10': 'tcpInfoLastDataSent'},
    {'1': 'tcp_info_last_ack_sent', '3': 325, '4': 1, '5': 13, '10': 'tcpInfoLastAckSent'},
    {'1': 'tcp_info_last_data_recv', '3': 326, '4': 1, '5': 13, '10': 'tcpInfoLastDataRecv'},
    {'1': 'tcp_info_last_ack_recv', '3': 327, '4': 1, '5': 13, '10': 'tcpInfoLastAckRecv'},
    {'1': 'tcp_info_pmtu', '3': 328, '4': 1, '5': 13, '10': 'tcpInfoPmtu'},
    {'1': 'tcp_info_rcv_ssthresh', '3': 329, '4': 1, '5': 13, '10': 'tcpInfoRcvSsthresh'},
    {'1': 'tcp_info_rtt', '3': 330, '4': 1, '5': 13, '10': 'tcpInfoRtt'},
    {'1': 'tcp_info_rtt_var', '3': 331, '4': 1, '5': 13, '10': 'tcpInfoRttVar'},
    {'1': 'tcp_info_snd_ssthresh', '3': 332, '4': 1, '5': 13, '10': 'tcpInfoSndSsthresh'},
    {'1': 'tcp_info_snd_cwnd', '3': 333, '4': 1, '5': 13, '10': 'tcpInfoSndCwnd'},
    {'1': 'tcp_info_adv_mss', '3': 334, '4': 1, '5': 13, '10': 'tcpInfoAdvMss'},
    {'1': 'tcp_info_reordering', '3': 335, '4': 1, '5': 13, '10': 'tcpInfoReordering'},
    {'1': 'tcp_info_rcv_rtt', '3': 336, '4': 1, '5': 13, '10': 'tcpInfoRcvRtt'},
    {'1': 'tcp_info_rcv_space', '3': 337, '4': 1, '5': 13, '10': 'tcpInfoRcvSpace'},
    {'1': 'tcp_info_total_retrans', '3': 338, '4': 1, '5': 13, '10': 'tcpInfoTotalRetrans'},
    {'1': 'tcp_info_pacing_rate', '3': 339, '4': 1, '5': 4, '10': 'tcpInfoPacingRate'},
    {'1': 'tcp_info_max_pacing_rate', '3': 340, '4': 1, '5': 4, '10': 'tcpInfoMaxPacingRate'},
    {'1': 'tcp_info_bytes_acked', '3': 341, '4': 1, '5': 4, '10': 'tcpInfoBytesAcked'},
    {'1': 'tcp_info_bytes_received', '3': 342, '4': 1, '5': 4, '10': 'tcpInfoBytesReceived'},
    {'1': 'tcp_info_segs_out', '3': 343, '4': 1, '5': 13, '10': 'tcpInfoSegsOut'},
    {'1': 'tcp_info_segs_in', '3': 344, '4': 1, '5': 13, '10': 'tcpInfoSegsIn'},
    {'1': 'tcp_info_not_sent_bytes', '3': 345, '4': 1, '5': 13, '10': 'tcpInfoNotSentBytes'},
    {'1': 'tcp_info_min_rtt', '3': 346, '4': 1, '5': 13, '10': 'tcpInfoMinRtt'},
    {'1': 'tcp_info_data_segs_in', '3': 347, '4': 1, '5': 13, '10': 'tcpInfoDataSegsIn'},
    {'1': 'tcp_info_data_segs_out', '3': 348, '4': 1, '5': 13, '10': 'tcpInfoDataSegsOut'},
    {'1': 'tcp_info_delivery_rate', '3': 349, '4': 1, '5': 4, '10': 'tcpInfoDeliveryRate'},
    {'1': 'tcp_info_busy_time', '3': 350, '4': 1, '5': 4, '10': 'tcpInfoBusyTime'},
    {'1': 'tcp_info_rwnd_limited', '3': 351, '4': 1, '5': 4, '10': 'tcpInfoRwndLimited'},
    {'1': 'tcp_info_sndbuf_limited', '3': 352, '4': 1, '5': 4, '10': 'tcpInfoSndbufLimited'},
    {'1': 'tcp_info_delivered', '3': 353, '4': 1, '5': 13, '10': 'tcpInfoDelivered'},
    {'1': 'tcp_info_delivered_ce', '3': 354, '4': 1, '5': 13, '10': 'tcpInfoDeliveredCe'},
    {'1': 'tcp_info_bytes_sent', '3': 355, '4': 1, '5': 4, '10': 'tcpInfoBytesSent'},
    {'1': 'tcp_info_bytes_retrans', '3': 356, '4': 1, '5': 4, '10': 'tcpInfoBytesRetrans'},
    {'1': 'tcp_info_dsack_dups', '3': 357, '4': 1, '5': 13, '10': 'tcpInfoDsackDups'},
    {'1': 'tcp_info_reord_seen', '3': 358, '4': 1, '5': 13, '10': 'tcpInfoReordSeen'},
    {'1': 'tcp_info_rcv_ooopack', '3': 359, '4': 1, '5': 13, '10': 'tcpInfoRcvOoopack'},
    {'1': 'tcp_info_snd_wnd', '3': 360, '4': 1, '5': 13, '10': 'tcpInfoSndWnd'},
    {'1': 'tcp_info_rcv_wnd', '3': 361, '4': 1, '5': 13, '10': 'tcpInfoRcvWnd'},
    {'1': 'tcp_info_rehash', '3': 362, '4': 1, '5': 13, '10': 'tcpInfoRehash'},
    {'1': 'tcp_info_total_rto', '3': 363, '4': 1, '5': 13, '10': 'tcpInfoTotalRto'},
    {'1': 'tcp_info_total_rto_recoveries', '3': 364, '4': 1, '5': 13, '10': 'tcpInfoTotalRtoRecoveries'},
    {'1': 'tcp_info_total_rto_time', '3': 365, '4': 1, '5': 13, '10': 'tcpInfoTotalRtoTime'},
    {'1': 'congestion_algorithm_string', '3': 400, '4': 1, '5': 9, '10': 'congestionAlgorithmString'},
    {'1': 'congestion_algorithm_enum', '3': 401, '4': 1, '5': 14, '6': '.xtcp_flat_record.v1.Envelope.XtcpFlatRecord.CongestionAlgorithm', '10': 'congestionAlgorithmEnum'},
    {'1': 'type_of_service', '3': 501, '4': 1, '5': 13, '10': 'typeOfService'},
    {'1': 'traffic_class', '3': 502, '4': 1, '5': 13, '10': 'trafficClass'},
    {'1': 'sk_mem_info_rmem_alloc', '3': 601, '4': 1, '5': 13, '10': 'skMemInfoRmemAlloc'},
    {'1': 'sk_mem_info_rcv_buf', '3': 602, '4': 1, '5': 13, '10': 'skMemInfoRcvBuf'},
    {'1': 'sk_mem_info_wmem_alloc', '3': 603, '4': 1, '5': 13, '10': 'skMemInfoWmemAlloc'},
    {'1': 'sk_mem_info_snd_buf', '3': 604, '4': 1, '5': 13, '10': 'skMemInfoSndBuf'},
    {'1': 'sk_mem_info_fwd_alloc', '3': 605, '4': 1, '5': 13, '10': 'skMemInfoFwdAlloc'},
    {'1': 'sk_mem_info_wmem_queued', '3': 606, '4': 1, '5': 13, '10': 'skMemInfoWmemQueued'},
    {'1': 'sk_mem_info_optmem', '3': 607, '4': 1, '5': 13, '10': 'skMemInfoOptmem'},
    {'1': 'sk_mem_info_backlog', '3': 608, '4': 1, '5': 13, '10': 'skMemInfoBacklog'},
    {'1': 'sk_mem_info_drops', '3': 609, '4': 1, '5': 13, '10': 'skMemInfoDrops'},
    {'1': 'shutdown_state', '3': 700, '4': 1, '5': 13, '10': 'shutdownState'},
    {'1': 'vegas_info_enabled', '3': 801, '4': 1, '5': 13, '10': 'vegasInfoEnabled'},
    {'1': 'vegas_info_rtt_cnt', '3': 802, '4': 1, '5': 13, '10': 'vegasInfoRttCnt'},
    {'1': 'vegas_info_rtt', '3': 803, '4': 1, '5': 13, '10': 'vegasInfoRtt'},
    {'1': 'vegas_info_min_rtt', '3': 804, '4': 1, '5': 13, '10': 'vegasInfoMinRtt'},
    {'1': 'dctcp_info_enabled', '3': 901, '4': 1, '5': 13, '10': 'dctcpInfoEnabled'},
    {'1': 'dctcp_info_ce_state', '3': 902, '4': 1, '5': 13, '10': 'dctcpInfoCeState'},
    {'1': 'dctcp_info_alpha', '3': 903, '4': 1, '5': 13, '10': 'dctcpInfoAlpha'},
    {'1': 'dctcp_info_ab_ecn', '3': 904, '4': 1, '5': 13, '10': 'dctcpInfoAbEcn'},
    {'1': 'dctcp_info_ab_tot', '3': 905, '4': 1, '5': 13, '10': 'dctcpInfoAbTot'},
    {'1': 'bbr_info_bw_lo', '3': 1001, '4': 1, '5': 13, '10': 'bbrInfoBwLo'},
    {'1': 'bbr_info_bw_hi', '3': 1002, '4': 1, '5': 13, '10': 'bbrInfoBwHi'},
    {'1': 'bbr_info_min_rtt', '3': 1003, '4': 1, '5': 13, '10': 'bbrInfoMinRtt'},
    {'1': 'bbr_info_pacing_gain', '3': 1004, '4': 1, '5': 13, '10': 'bbrInfoPacingGain'},
    {'1': 'bbr_info_cwnd_gain', '3': 1005, '4': 1, '5': 13, '10': 'bbrInfoCwndGain'},
    {'1': 'class_id', '3': 1101, '4': 1, '5': 13, '10': 'classId'},
    {'1': 'sock_opt', '3': 1102, '4': 1, '5': 13, '10': 'sockOpt'},
    {'1': 'c_group', '3': 1203, '4': 1, '5': 4, '10': 'cGroup'},
  ],
  '4': [Envelope_XtcpFlatRecord_CongestionAlgorithm$json],
};

@$core.Deprecated('Use envelopeDescriptor instead')
const Envelope_XtcpFlatRecord_CongestionAlgorithm$json = {
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

/// Descriptor for `Envelope`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List envelopeDescriptor = $convert.base64Decode(
    'CghFbnZlbG9wZRI+CgNyb3cYCiADKAsyLC54dGNwX2ZsYXRfcmVjb3JkLnYxLkVudmVsb3BlLl'
    'h0Y3BGbGF0UmVjb3JkUgNyb3carC8KDlh0Y3BGbGF0UmVjb3JkEiEKDHRpbWVzdGFtcF9ucxgK'
    'IAEoAVILdGltZXN0YW1wTnMSGgoIaG9zdG5hbWUYFCABKAlSCGhvc3RuYW1lEhQKBW5ldG5zGB'
    '4gASgJUgVuZXRucxISCgRuc2lkGCggASgNUgRuc2lkEhQKBWxhYmVsGDIgASgJUgVsYWJlbBIQ'
    'CgN0YWcYPCABKAlSA3RhZxIlCg5yZWNvcmRfY291bnRlchhGIAEoBFINcmVjb3JkQ291bnRlch'
    'IbCglzb2NrZXRfZmQYUCABKARSCHNvY2tldEZkEiEKDG5ldGxpbmtlcl9pZBhaIAEoBFILbmV0'
    'bGlua2VySWQSLwoUaW5ldF9kaWFnX21zZ19mYW1pbHkYZSABKA1SEWluZXREaWFnTXNnRmFtaW'
    'x5Ei0KE2luZXRfZGlhZ19tc2dfc3RhdGUYZiABKA1SEGluZXREaWFnTXNnU3RhdGUSLQoTaW5l'
    'dF9kaWFnX21zZ190aW1lchhnIAEoDVIQaW5ldERpYWdNc2dUaW1lchIxChVpbmV0X2RpYWdfbX'
    'NnX3JldHJhbnMYaCABKA1SEmluZXREaWFnTXNnUmV0cmFucxJFCiBpbmV0X2RpYWdfbXNnX3Nv'
    'Y2tldF9zb3VyY2VfcG9ydBhpIAEoDVIbaW5ldERpYWdNc2dTb2NrZXRTb3VyY2VQb3J0Ek8KJW'
    'luZXRfZGlhZ19tc2dfc29ja2V0X2Rlc3RpbmF0aW9uX3BvcnQYaiABKA1SIGluZXREaWFnTXNn'
    'U29ja2V0RGVzdGluYXRpb25Qb3J0EjwKG2luZXRfZGlhZ19tc2dfc29ja2V0X3NvdXJjZRhrIA'
    'EoDFIXaW5ldERpYWdNc2dTb2NrZXRTb3VyY2USRgogaW5ldF9kaWFnX21zZ19zb2NrZXRfZGVz'
    'dGluYXRpb24YbCABKAxSHGluZXREaWFnTXNnU29ja2V0RGVzdGluYXRpb24SQgoeaW5ldF9kaW'
    'FnX21zZ19zb2NrZXRfaW50ZXJmYWNlGG0gASgNUhppbmV0RGlhZ01zZ1NvY2tldEludGVyZmFj'
    'ZRI8ChtpbmV0X2RpYWdfbXNnX3NvY2tldF9jb29raWUYbiABKARSF2luZXREaWFnTXNnU29ja2'
    'V0Q29va2llEj8KHWluZXRfZGlhZ19tc2dfc29ja2V0X2Rlc3RfYXNuGG8gASgEUhhpbmV0RGlh'
    'Z01zZ1NvY2tldERlc3RBc24SRgohaW5ldF9kaWFnX21zZ19zb2NrZXRfbmV4dF9ob3BfYXNuGH'
    'AgASgEUhtpbmV0RGlhZ01zZ1NvY2tldE5leHRIb3BBc24SMQoVaW5ldF9kaWFnX21zZ19leHBp'
    'cmVzGHEgASgNUhJpbmV0RGlhZ01zZ0V4cGlyZXMSLwoUaW5ldF9kaWFnX21zZ19ycXVldWUYci'
    'ABKA1SEWluZXREaWFnTXNnUnF1ZXVlEi8KFGluZXRfZGlhZ19tc2dfd3F1ZXVlGHMgASgNUhFp'
    'bmV0RGlhZ01zZ1dxdWV1ZRIpChFpbmV0X2RpYWdfbXNnX3VpZBh0IAEoDVIOaW5ldERpYWdNc2'
    'dVaWQSLQoTaW5ldF9kaWFnX21zZ19pbm9kZRh1IAEoDVIQaW5ldERpYWdNc2dJbm9kZRIjCg1t'
    'ZW1faW5mb19ybWVtGMkBIAEoDVILbWVtSW5mb1JtZW0SIwoNbWVtX2luZm9fd21lbRjKASABKA'
    '1SC21lbUluZm9XbWVtEiMKDW1lbV9pbmZvX2ZtZW0YywEgASgNUgttZW1JbmZvRm1lbRIjCg1t'
    'ZW1faW5mb190bWVtGMwBIAEoDVILbWVtSW5mb1RtZW0SJQoOdGNwX2luZm9fc3RhdGUYrQIgAS'
    'gNUgx0Y3BJbmZvU3RhdGUSKgoRdGNwX2luZm9fY2Ffc3RhdGUYrgIgASgNUg50Y3BJbmZvQ2FT'
    'dGF0ZRIxChR0Y3BfaW5mb19yZXRyYW5zbWl0cxivAiABKA1SEnRjcEluZm9SZXRyYW5zbWl0cx'
    'InCg90Y3BfaW5mb19wcm9iZXMYsAIgASgNUg10Y3BJbmZvUHJvYmVzEikKEHRjcF9pbmZvX2Jh'
    'Y2tvZmYYsQIgASgNUg50Y3BJbmZvQmFja29mZhIpChB0Y3BfaW5mb19vcHRpb25zGLICIAEoDV'
    'IOdGNwSW5mb09wdGlvbnMSLgoTdGNwX2luZm9fc2VuZF9zY2FsZRizAiABKA1SEHRjcEluZm9T'
    'ZW5kU2NhbGUSLAoSdGNwX2luZm9fcmN2X3NjYWxlGLQCIAEoDVIPdGNwSW5mb1JjdlNjYWxlEk'
    'oKInRjcF9pbmZvX2RlbGl2ZXJ5X3JhdGVfYXBwX2xpbWl0ZWQYtQIgASgNUh10Y3BJbmZvRGVs'
    'aXZlcnlSYXRlQXBwTGltaXRlZBJGCiB0Y3BfaW5mb19mYXN0X29wZW5fY2xpZW50X2ZhaWxlZB'
    'i2AiABKA1SG3RjcEluZm9GYXN0T3BlbkNsaWVudEZhaWxlZBIhCgx0Y3BfaW5mb19ydG8YuwIg'
    'ASgNUgp0Y3BJbmZvUnRvEiEKDHRjcF9pbmZvX2F0bxi8AiABKA1SCnRjcEluZm9BdG8SKAoQdG'
    'NwX2luZm9fc25kX21zcxi9AiABKA1SDXRjcEluZm9TbmRNc3MSKAoQdGNwX2luZm9fcmN2X21z'
    'cxi+AiABKA1SDXRjcEluZm9SY3ZNc3MSKQoQdGNwX2luZm9fdW5hY2tlZBi/AiABKA1SDnRjcE'
    'luZm9VbmFja2VkEicKD3RjcF9pbmZvX3NhY2tlZBjAAiABKA1SDXRjcEluZm9TYWNrZWQSIwoN'
    'dGNwX2luZm9fbG9zdBjBAiABKA1SC3RjcEluZm9Mb3N0EikKEHRjcF9pbmZvX3JldHJhbnMYwg'
    'IgASgNUg50Y3BJbmZvUmV0cmFucxIpChB0Y3BfaW5mb19mYWNrZXRzGMMCIAEoDVIOdGNwSW5m'
    'b0ZhY2tldHMSNQoXdGNwX2luZm9fbGFzdF9kYXRhX3NlbnQYxAIgASgNUhN0Y3BJbmZvTGFzdE'
    'RhdGFTZW50EjMKFnRjcF9pbmZvX2xhc3RfYWNrX3NlbnQYxQIgASgNUhJ0Y3BJbmZvTGFzdEFj'
    'a1NlbnQSNQoXdGNwX2luZm9fbGFzdF9kYXRhX3JlY3YYxgIgASgNUhN0Y3BJbmZvTGFzdERhdG'
    'FSZWN2EjMKFnRjcF9pbmZvX2xhc3RfYWNrX3JlY3YYxwIgASgNUhJ0Y3BJbmZvTGFzdEFja1Jl'
    'Y3YSIwoNdGNwX2luZm9fcG10dRjIAiABKA1SC3RjcEluZm9QbXR1EjIKFXRjcF9pbmZvX3Jjdl'
    '9zc3RocmVzaBjJAiABKA1SEnRjcEluZm9SY3ZTc3RocmVzaBIhCgx0Y3BfaW5mb19ydHQYygIg'
    'ASgNUgp0Y3BJbmZvUnR0EigKEHRjcF9pbmZvX3J0dF92YXIYywIgASgNUg10Y3BJbmZvUnR0Vm'
    'FyEjIKFXRjcF9pbmZvX3NuZF9zc3RocmVzaBjMAiABKA1SEnRjcEluZm9TbmRTc3RocmVzaBIq'
    'ChF0Y3BfaW5mb19zbmRfY3duZBjNAiABKA1SDnRjcEluZm9TbmRDd25kEigKEHRjcF9pbmZvX2'
    'Fkdl9tc3MYzgIgASgNUg10Y3BJbmZvQWR2TXNzEi8KE3RjcF9pbmZvX3Jlb3JkZXJpbmcYzwIg'
    'ASgNUhF0Y3BJbmZvUmVvcmRlcmluZxIoChB0Y3BfaW5mb19yY3ZfcnR0GNACIAEoDVINdGNwSW'
    '5mb1JjdlJ0dBIsChJ0Y3BfaW5mb19yY3Zfc3BhY2UY0QIgASgNUg90Y3BJbmZvUmN2U3BhY2US'
    'NAoWdGNwX2luZm9fdG90YWxfcmV0cmFucxjSAiABKA1SE3RjcEluZm9Ub3RhbFJldHJhbnMSMA'
    'oUdGNwX2luZm9fcGFjaW5nX3JhdGUY0wIgASgEUhF0Y3BJbmZvUGFjaW5nUmF0ZRI3Chh0Y3Bf'
    'aW5mb19tYXhfcGFjaW5nX3JhdGUY1AIgASgEUhR0Y3BJbmZvTWF4UGFjaW5nUmF0ZRIwChR0Y3'
    'BfaW5mb19ieXRlc19hY2tlZBjVAiABKARSEXRjcEluZm9CeXRlc0Fja2VkEjYKF3RjcF9pbmZv'
    'X2J5dGVzX3JlY2VpdmVkGNYCIAEoBFIUdGNwSW5mb0J5dGVzUmVjZWl2ZWQSKgoRdGNwX2luZm'
    '9fc2Vnc19vdXQY1wIgASgNUg50Y3BJbmZvU2Vnc091dBIoChB0Y3BfaW5mb19zZWdzX2luGNgC'
    'IAEoDVINdGNwSW5mb1NlZ3NJbhI1Chd0Y3BfaW5mb19ub3Rfc2VudF9ieXRlcxjZAiABKA1SE3'
    'RjcEluZm9Ob3RTZW50Qnl0ZXMSKAoQdGNwX2luZm9fbWluX3J0dBjaAiABKA1SDXRjcEluZm9N'
    'aW5SdHQSMQoVdGNwX2luZm9fZGF0YV9zZWdzX2luGNsCIAEoDVIRdGNwSW5mb0RhdGFTZWdzSW'
    '4SMwoWdGNwX2luZm9fZGF0YV9zZWdzX291dBjcAiABKA1SEnRjcEluZm9EYXRhU2Vnc091dBI0'
    'ChZ0Y3BfaW5mb19kZWxpdmVyeV9yYXRlGN0CIAEoBFITdGNwSW5mb0RlbGl2ZXJ5UmF0ZRIsCh'
    'J0Y3BfaW5mb19idXN5X3RpbWUY3gIgASgEUg90Y3BJbmZvQnVzeVRpbWUSMgoVdGNwX2luZm9f'
    'cnduZF9saW1pdGVkGN8CIAEoBFISdGNwSW5mb1J3bmRMaW1pdGVkEjYKF3RjcF9pbmZvX3NuZG'
    'J1Zl9saW1pdGVkGOACIAEoBFIUdGNwSW5mb1NuZGJ1ZkxpbWl0ZWQSLQoSdGNwX2luZm9fZGVs'
    'aXZlcmVkGOECIAEoDVIQdGNwSW5mb0RlbGl2ZXJlZBIyChV0Y3BfaW5mb19kZWxpdmVyZWRfY2'
    'UY4gIgASgNUhJ0Y3BJbmZvRGVsaXZlcmVkQ2USLgoTdGNwX2luZm9fYnl0ZXNfc2VudBjjAiAB'
    'KARSEHRjcEluZm9CeXRlc1NlbnQSNAoWdGNwX2luZm9fYnl0ZXNfcmV0cmFucxjkAiABKARSE3'
    'RjcEluZm9CeXRlc1JldHJhbnMSLgoTdGNwX2luZm9fZHNhY2tfZHVwcxjlAiABKA1SEHRjcElu'
    'Zm9Ec2Fja0R1cHMSLgoTdGNwX2luZm9fcmVvcmRfc2VlbhjmAiABKA1SEHRjcEluZm9SZW9yZF'
    'NlZW4SMAoUdGNwX2luZm9fcmN2X29vb3BhY2sY5wIgASgNUhF0Y3BJbmZvUmN2T29vcGFjaxIo'
    'ChB0Y3BfaW5mb19zbmRfd25kGOgCIAEoDVINdGNwSW5mb1NuZFduZBIoChB0Y3BfaW5mb19yY3'
    'Zfd25kGOkCIAEoDVINdGNwSW5mb1JjdlduZBInCg90Y3BfaW5mb19yZWhhc2gY6gIgASgNUg10'
    'Y3BJbmZvUmVoYXNoEiwKEnRjcF9pbmZvX3RvdGFsX3J0bxjrAiABKA1SD3RjcEluZm9Ub3RhbF'
    'J0bxJBCh10Y3BfaW5mb190b3RhbF9ydG9fcmVjb3ZlcmllcxjsAiABKA1SGXRjcEluZm9Ub3Rh'
    'bFJ0b1JlY292ZXJpZXMSNQoXdGNwX2luZm9fdG90YWxfcnRvX3RpbWUY7QIgASgNUhN0Y3BJbm'
    'ZvVG90YWxSdG9UaW1lEj8KG2Nvbmdlc3Rpb25fYWxnb3JpdGhtX3N0cmluZxiQAyABKAlSGWNv'
    'bmdlc3Rpb25BbGdvcml0aG1TdHJpbmcSfQoZY29uZ2VzdGlvbl9hbGdvcml0aG1fZW51bRiRAy'
    'ABKA4yQC54dGNwX2ZsYXRfcmVjb3JkLnYxLkVudmVsb3BlLlh0Y3BGbGF0UmVjb3JkLkNvbmdl'
    'c3Rpb25BbGdvcml0aG1SF2Nvbmdlc3Rpb25BbGdvcml0aG1FbnVtEicKD3R5cGVfb2Zfc2Vydm'
    'ljZRj1AyABKA1SDXR5cGVPZlNlcnZpY2USJAoNdHJhZmZpY19jbGFzcxj2AyABKA1SDHRyYWZm'
    'aWNDbGFzcxIzChZza19tZW1faW5mb19ybWVtX2FsbG9jGNkEIAEoDVISc2tNZW1JbmZvUm1lbU'
    'FsbG9jEi0KE3NrX21lbV9pbmZvX3Jjdl9idWYY2gQgASgNUg9za01lbUluZm9SY3ZCdWYSMwoW'
    'c2tfbWVtX2luZm9fd21lbV9hbGxvYxjbBCABKA1SEnNrTWVtSW5mb1dtZW1BbGxvYxItChNza1'
    '9tZW1faW5mb19zbmRfYnVmGNwEIAEoDVIPc2tNZW1JbmZvU25kQnVmEjEKFXNrX21lbV9pbmZv'
    'X2Z3ZF9hbGxvYxjdBCABKA1SEXNrTWVtSW5mb0Z3ZEFsbG9jEjUKF3NrX21lbV9pbmZvX3dtZW'
    '1fcXVldWVkGN4EIAEoDVITc2tNZW1JbmZvV21lbVF1ZXVlZBIsChJza19tZW1faW5mb19vcHRt'
    'ZW0Y3wQgASgNUg9za01lbUluZm9PcHRtZW0SLgoTc2tfbWVtX2luZm9fYmFja2xvZxjgBCABKA'
    '1SEHNrTWVtSW5mb0JhY2tsb2cSKgoRc2tfbWVtX2luZm9fZHJvcHMY4QQgASgNUg5za01lbUlu'
    'Zm9Ecm9wcxImCg5zaHV0ZG93bl9zdGF0ZRi8BSABKA1SDXNodXRkb3duU3RhdGUSLQoSdmVnYX'
    'NfaW5mb19lbmFibGVkGKEGIAEoDVIQdmVnYXNJbmZvRW5hYmxlZBIsChJ2ZWdhc19pbmZvX3J0'
    'dF9jbnQYogYgASgNUg92ZWdhc0luZm9SdHRDbnQSJQoOdmVnYXNfaW5mb19ydHQYowYgASgNUg'
    'x2ZWdhc0luZm9SdHQSLAoSdmVnYXNfaW5mb19taW5fcnR0GKQGIAEoDVIPdmVnYXNJbmZvTWlu'
    'UnR0Ei0KEmRjdGNwX2luZm9fZW5hYmxlZBiFByABKA1SEGRjdGNwSW5mb0VuYWJsZWQSLgoTZG'
    'N0Y3BfaW5mb19jZV9zdGF0ZRiGByABKA1SEGRjdGNwSW5mb0NlU3RhdGUSKQoQZGN0Y3BfaW5m'
    'b19hbHBoYRiHByABKA1SDmRjdGNwSW5mb0FscGhhEioKEWRjdGNwX2luZm9fYWJfZWNuGIgHIA'
    'EoDVIOZGN0Y3BJbmZvQWJFY24SKgoRZGN0Y3BfaW5mb19hYl90b3QYiQcgASgNUg5kY3RjcElu'
    'Zm9BYlRvdBIkCg5iYnJfaW5mb19id19sbxjpByABKA1SC2JickluZm9Cd0xvEiQKDmJicl9pbm'
    'ZvX2J3X2hpGOoHIAEoDVILYmJySW5mb0J3SGkSKAoQYmJyX2luZm9fbWluX3J0dBjrByABKA1S'
    'DWJickluZm9NaW5SdHQSMAoUYmJyX2luZm9fcGFjaW5nX2dhaW4Y7AcgASgNUhFiYnJJbmZvUG'
    'FjaW5nR2FpbhIsChJiYnJfaW5mb19jd25kX2dhaW4Y7QcgASgNUg9iYnJJbmZvQ3duZEdhaW4S'
    'GgoIY2xhc3NfaWQYzQggASgNUgdjbGFzc0lkEhoKCHNvY2tfb3B0GM4IIAEoDVIHc29ja09wdB'
    'IYCgdjX2dyb3VwGLMJIAEoBFIGY0dyb3VwIpkCChNDb25nZXN0aW9uQWxnb3JpdGhtEiQKIENP'
    'TkdFU1RJT05fQUxHT1JJVEhNX1VOU1BFQ0lGSUVEEAASHgoaQ09OR0VTVElPTl9BTEdPUklUSE'
    '1fQ1VCSUMQARIeChpDT05HRVNUSU9OX0FMR09SSVRITV9EQ1RDUBACEh4KGkNPTkdFU1RJT05f'
    'QUxHT1JJVEhNX1ZFR0FTEAMSHwobQ09OR0VTVElPTl9BTEdPUklUSE1fUFJBR1VFEAQSHQoZQ0'
    '9OR0VTVElPTl9BTEdPUklUSE1fQkJSMRAFEh0KGUNPTkdFU1RJT05fQUxHT1JJVEhNX0JCUjIQ'
    'BhIdChlDT05HRVNUSU9OX0FMR09SSVRITV9CQlIzEAc=');

@$core.Deprecated('Use xtcpFlatRecordDescriptor instead')
const XtcpFlatRecord$json = {
  '1': 'XtcpFlatRecord',
  '2': [
    {'1': 'timestamp_ns', '3': 10, '4': 1, '5': 1, '10': 'timestampNs'},
    {'1': 'hostname', '3': 20, '4': 1, '5': 9, '10': 'hostname'},
    {'1': 'netns', '3': 30, '4': 1, '5': 9, '10': 'netns'},
    {'1': 'nsid', '3': 40, '4': 1, '5': 13, '10': 'nsid'},
    {'1': 'label', '3': 50, '4': 1, '5': 9, '10': 'label'},
    {'1': 'tag', '3': 60, '4': 1, '5': 9, '10': 'tag'},
    {'1': 'record_counter', '3': 70, '4': 1, '5': 4, '10': 'recordCounter'},
    {'1': 'socket_fd', '3': 80, '4': 1, '5': 4, '10': 'socketFd'},
    {'1': 'netlinker_id', '3': 90, '4': 1, '5': 4, '10': 'netlinkerId'},
    {'1': 'inet_diag_msg_family', '3': 101, '4': 1, '5': 13, '10': 'inetDiagMsgFamily'},
    {'1': 'inet_diag_msg_state', '3': 102, '4': 1, '5': 13, '10': 'inetDiagMsgState'},
    {'1': 'inet_diag_msg_timer', '3': 103, '4': 1, '5': 13, '10': 'inetDiagMsgTimer'},
    {'1': 'inet_diag_msg_retrans', '3': 104, '4': 1, '5': 13, '10': 'inetDiagMsgRetrans'},
    {'1': 'inet_diag_msg_socket_source_port', '3': 105, '4': 1, '5': 13, '10': 'inetDiagMsgSocketSourcePort'},
    {'1': 'inet_diag_msg_socket_destination_port', '3': 106, '4': 1, '5': 13, '10': 'inetDiagMsgSocketDestinationPort'},
    {'1': 'inet_diag_msg_socket_source', '3': 107, '4': 1, '5': 12, '10': 'inetDiagMsgSocketSource'},
    {'1': 'inet_diag_msg_socket_destination', '3': 108, '4': 1, '5': 12, '10': 'inetDiagMsgSocketDestination'},
    {'1': 'inet_diag_msg_socket_interface', '3': 109, '4': 1, '5': 13, '10': 'inetDiagMsgSocketInterface'},
    {'1': 'inet_diag_msg_socket_cookie', '3': 110, '4': 1, '5': 4, '10': 'inetDiagMsgSocketCookie'},
    {'1': 'inet_diag_msg_socket_dest_asn', '3': 111, '4': 1, '5': 4, '10': 'inetDiagMsgSocketDestAsn'},
    {'1': 'inet_diag_msg_socket_next_hop_asn', '3': 112, '4': 1, '5': 4, '10': 'inetDiagMsgSocketNextHopAsn'},
    {'1': 'inet_diag_msg_expires', '3': 113, '4': 1, '5': 13, '10': 'inetDiagMsgExpires'},
    {'1': 'inet_diag_msg_rqueue', '3': 114, '4': 1, '5': 13, '10': 'inetDiagMsgRqueue'},
    {'1': 'inet_diag_msg_wqueue', '3': 115, '4': 1, '5': 13, '10': 'inetDiagMsgWqueue'},
    {'1': 'inet_diag_msg_uid', '3': 116, '4': 1, '5': 13, '10': 'inetDiagMsgUid'},
    {'1': 'inet_diag_msg_inode', '3': 117, '4': 1, '5': 13, '10': 'inetDiagMsgInode'},
    {'1': 'mem_info_rmem', '3': 201, '4': 1, '5': 13, '10': 'memInfoRmem'},
    {'1': 'mem_info_wmem', '3': 202, '4': 1, '5': 13, '10': 'memInfoWmem'},
    {'1': 'mem_info_fmem', '3': 203, '4': 1, '5': 13, '10': 'memInfoFmem'},
    {'1': 'mem_info_tmem', '3': 204, '4': 1, '5': 13, '10': 'memInfoTmem'},
    {'1': 'tcp_info_state', '3': 301, '4': 1, '5': 13, '10': 'tcpInfoState'},
    {'1': 'tcp_info_ca_state', '3': 302, '4': 1, '5': 13, '10': 'tcpInfoCaState'},
    {'1': 'tcp_info_retransmits', '3': 303, '4': 1, '5': 13, '10': 'tcpInfoRetransmits'},
    {'1': 'tcp_info_probes', '3': 304, '4': 1, '5': 13, '10': 'tcpInfoProbes'},
    {'1': 'tcp_info_backoff', '3': 305, '4': 1, '5': 13, '10': 'tcpInfoBackoff'},
    {'1': 'tcp_info_options', '3': 306, '4': 1, '5': 13, '10': 'tcpInfoOptions'},
    {'1': 'tcp_info_send_scale', '3': 307, '4': 1, '5': 13, '10': 'tcpInfoSendScale'},
    {'1': 'tcp_info_rcv_scale', '3': 308, '4': 1, '5': 13, '10': 'tcpInfoRcvScale'},
    {'1': 'tcp_info_delivery_rate_app_limited', '3': 309, '4': 1, '5': 13, '10': 'tcpInfoDeliveryRateAppLimited'},
    {'1': 'tcp_info_fast_open_client_failed', '3': 310, '4': 1, '5': 13, '10': 'tcpInfoFastOpenClientFailed'},
    {'1': 'tcp_info_rto', '3': 315, '4': 1, '5': 13, '10': 'tcpInfoRto'},
    {'1': 'tcp_info_ato', '3': 316, '4': 1, '5': 13, '10': 'tcpInfoAto'},
    {'1': 'tcp_info_snd_mss', '3': 317, '4': 1, '5': 13, '10': 'tcpInfoSndMss'},
    {'1': 'tcp_info_rcv_mss', '3': 318, '4': 1, '5': 13, '10': 'tcpInfoRcvMss'},
    {'1': 'tcp_info_unacked', '3': 319, '4': 1, '5': 13, '10': 'tcpInfoUnacked'},
    {'1': 'tcp_info_sacked', '3': 320, '4': 1, '5': 13, '10': 'tcpInfoSacked'},
    {'1': 'tcp_info_lost', '3': 321, '4': 1, '5': 13, '10': 'tcpInfoLost'},
    {'1': 'tcp_info_retrans', '3': 322, '4': 1, '5': 13, '10': 'tcpInfoRetrans'},
    {'1': 'tcp_info_fackets', '3': 323, '4': 1, '5': 13, '10': 'tcpInfoFackets'},
    {'1': 'tcp_info_last_data_sent', '3': 324, '4': 1, '5': 13, '10': 'tcpInfoLastDataSent'},
    {'1': 'tcp_info_last_ack_sent', '3': 325, '4': 1, '5': 13, '10': 'tcpInfoLastAckSent'},
    {'1': 'tcp_info_last_data_recv', '3': 326, '4': 1, '5': 13, '10': 'tcpInfoLastDataRecv'},
    {'1': 'tcp_info_last_ack_recv', '3': 327, '4': 1, '5': 13, '10': 'tcpInfoLastAckRecv'},
    {'1': 'tcp_info_pmtu', '3': 328, '4': 1, '5': 13, '10': 'tcpInfoPmtu'},
    {'1': 'tcp_info_rcv_ssthresh', '3': 329, '4': 1, '5': 13, '10': 'tcpInfoRcvSsthresh'},
    {'1': 'tcp_info_rtt', '3': 330, '4': 1, '5': 13, '10': 'tcpInfoRtt'},
    {'1': 'tcp_info_rtt_var', '3': 331, '4': 1, '5': 13, '10': 'tcpInfoRttVar'},
    {'1': 'tcp_info_snd_ssthresh', '3': 332, '4': 1, '5': 13, '10': 'tcpInfoSndSsthresh'},
    {'1': 'tcp_info_snd_cwnd', '3': 333, '4': 1, '5': 13, '10': 'tcpInfoSndCwnd'},
    {'1': 'tcp_info_adv_mss', '3': 334, '4': 1, '5': 13, '10': 'tcpInfoAdvMss'},
    {'1': 'tcp_info_reordering', '3': 335, '4': 1, '5': 13, '10': 'tcpInfoReordering'},
    {'1': 'tcp_info_rcv_rtt', '3': 336, '4': 1, '5': 13, '10': 'tcpInfoRcvRtt'},
    {'1': 'tcp_info_rcv_space', '3': 337, '4': 1, '5': 13, '10': 'tcpInfoRcvSpace'},
    {'1': 'tcp_info_total_retrans', '3': 338, '4': 1, '5': 13, '10': 'tcpInfoTotalRetrans'},
    {'1': 'tcp_info_pacing_rate', '3': 339, '4': 1, '5': 4, '10': 'tcpInfoPacingRate'},
    {'1': 'tcp_info_max_pacing_rate', '3': 340, '4': 1, '5': 4, '10': 'tcpInfoMaxPacingRate'},
    {'1': 'tcp_info_bytes_acked', '3': 341, '4': 1, '5': 4, '10': 'tcpInfoBytesAcked'},
    {'1': 'tcp_info_bytes_received', '3': 342, '4': 1, '5': 4, '10': 'tcpInfoBytesReceived'},
    {'1': 'tcp_info_segs_out', '3': 343, '4': 1, '5': 13, '10': 'tcpInfoSegsOut'},
    {'1': 'tcp_info_segs_in', '3': 344, '4': 1, '5': 13, '10': 'tcpInfoSegsIn'},
    {'1': 'tcp_info_not_sent_bytes', '3': 345, '4': 1, '5': 13, '10': 'tcpInfoNotSentBytes'},
    {'1': 'tcp_info_min_rtt', '3': 346, '4': 1, '5': 13, '10': 'tcpInfoMinRtt'},
    {'1': 'tcp_info_data_segs_in', '3': 347, '4': 1, '5': 13, '10': 'tcpInfoDataSegsIn'},
    {'1': 'tcp_info_data_segs_out', '3': 348, '4': 1, '5': 13, '10': 'tcpInfoDataSegsOut'},
    {'1': 'tcp_info_delivery_rate', '3': 349, '4': 1, '5': 4, '10': 'tcpInfoDeliveryRate'},
    {'1': 'tcp_info_busy_time', '3': 350, '4': 1, '5': 4, '10': 'tcpInfoBusyTime'},
    {'1': 'tcp_info_rwnd_limited', '3': 351, '4': 1, '5': 4, '10': 'tcpInfoRwndLimited'},
    {'1': 'tcp_info_sndbuf_limited', '3': 352, '4': 1, '5': 4, '10': 'tcpInfoSndbufLimited'},
    {'1': 'tcp_info_delivered', '3': 353, '4': 1, '5': 13, '10': 'tcpInfoDelivered'},
    {'1': 'tcp_info_delivered_ce', '3': 354, '4': 1, '5': 13, '10': 'tcpInfoDeliveredCe'},
    {'1': 'tcp_info_bytes_sent', '3': 355, '4': 1, '5': 4, '10': 'tcpInfoBytesSent'},
    {'1': 'tcp_info_bytes_retrans', '3': 356, '4': 1, '5': 4, '10': 'tcpInfoBytesRetrans'},
    {'1': 'tcp_info_dsack_dups', '3': 357, '4': 1, '5': 13, '10': 'tcpInfoDsackDups'},
    {'1': 'tcp_info_reord_seen', '3': 358, '4': 1, '5': 13, '10': 'tcpInfoReordSeen'},
    {'1': 'tcp_info_rcv_ooopack', '3': 359, '4': 1, '5': 13, '10': 'tcpInfoRcvOoopack'},
    {'1': 'tcp_info_snd_wnd', '3': 360, '4': 1, '5': 13, '10': 'tcpInfoSndWnd'},
    {'1': 'tcp_info_rcv_wnd', '3': 361, '4': 1, '5': 13, '10': 'tcpInfoRcvWnd'},
    {'1': 'tcp_info_rehash', '3': 362, '4': 1, '5': 13, '10': 'tcpInfoRehash'},
    {'1': 'tcp_info_total_rto', '3': 363, '4': 1, '5': 13, '10': 'tcpInfoTotalRto'},
    {'1': 'tcp_info_total_rto_recoveries', '3': 364, '4': 1, '5': 13, '10': 'tcpInfoTotalRtoRecoveries'},
    {'1': 'tcp_info_total_rto_time', '3': 365, '4': 1, '5': 13, '10': 'tcpInfoTotalRtoTime'},
    {'1': 'congestion_algorithm_string', '3': 400, '4': 1, '5': 9, '10': 'congestionAlgorithmString'},
    {'1': 'congestion_algorithm_enum', '3': 401, '4': 1, '5': 14, '6': '.xtcp_flat_record.v1.XtcpFlatRecord.CongestionAlgorithm', '10': 'congestionAlgorithmEnum'},
    {'1': 'type_of_service', '3': 501, '4': 1, '5': 13, '10': 'typeOfService'},
    {'1': 'traffic_class', '3': 502, '4': 1, '5': 13, '10': 'trafficClass'},
    {'1': 'sk_mem_info_rmem_alloc', '3': 601, '4': 1, '5': 13, '10': 'skMemInfoRmemAlloc'},
    {'1': 'sk_mem_info_rcv_buf', '3': 602, '4': 1, '5': 13, '10': 'skMemInfoRcvBuf'},
    {'1': 'sk_mem_info_wmem_alloc', '3': 603, '4': 1, '5': 13, '10': 'skMemInfoWmemAlloc'},
    {'1': 'sk_mem_info_snd_buf', '3': 604, '4': 1, '5': 13, '10': 'skMemInfoSndBuf'},
    {'1': 'sk_mem_info_fwd_alloc', '3': 605, '4': 1, '5': 13, '10': 'skMemInfoFwdAlloc'},
    {'1': 'sk_mem_info_wmem_queued', '3': 606, '4': 1, '5': 13, '10': 'skMemInfoWmemQueued'},
    {'1': 'sk_mem_info_optmem', '3': 607, '4': 1, '5': 13, '10': 'skMemInfoOptmem'},
    {'1': 'sk_mem_info_backlog', '3': 608, '4': 1, '5': 13, '10': 'skMemInfoBacklog'},
    {'1': 'sk_mem_info_drops', '3': 609, '4': 1, '5': 13, '10': 'skMemInfoDrops'},
    {'1': 'shutdown_state', '3': 700, '4': 1, '5': 13, '10': 'shutdownState'},
    {'1': 'vegas_info_enabled', '3': 801, '4': 1, '5': 13, '10': 'vegasInfoEnabled'},
    {'1': 'vegas_info_rtt_cnt', '3': 802, '4': 1, '5': 13, '10': 'vegasInfoRttCnt'},
    {'1': 'vegas_info_rtt', '3': 803, '4': 1, '5': 13, '10': 'vegasInfoRtt'},
    {'1': 'vegas_info_min_rtt', '3': 804, '4': 1, '5': 13, '10': 'vegasInfoMinRtt'},
    {'1': 'dctcp_info_enabled', '3': 901, '4': 1, '5': 13, '10': 'dctcpInfoEnabled'},
    {'1': 'dctcp_info_ce_state', '3': 902, '4': 1, '5': 13, '10': 'dctcpInfoCeState'},
    {'1': 'dctcp_info_alpha', '3': 903, '4': 1, '5': 13, '10': 'dctcpInfoAlpha'},
    {'1': 'dctcp_info_ab_ecn', '3': 904, '4': 1, '5': 13, '10': 'dctcpInfoAbEcn'},
    {'1': 'dctcp_info_ab_tot', '3': 905, '4': 1, '5': 13, '10': 'dctcpInfoAbTot'},
    {'1': 'bbr_info_bw_lo', '3': 1001, '4': 1, '5': 13, '10': 'bbrInfoBwLo'},
    {'1': 'bbr_info_bw_hi', '3': 1002, '4': 1, '5': 13, '10': 'bbrInfoBwHi'},
    {'1': 'bbr_info_min_rtt', '3': 1003, '4': 1, '5': 13, '10': 'bbrInfoMinRtt'},
    {'1': 'bbr_info_pacing_gain', '3': 1004, '4': 1, '5': 13, '10': 'bbrInfoPacingGain'},
    {'1': 'bbr_info_cwnd_gain', '3': 1005, '4': 1, '5': 13, '10': 'bbrInfoCwndGain'},
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
    'Cg5YdGNwRmxhdFJlY29yZBIhCgx0aW1lc3RhbXBfbnMYCiABKAFSC3RpbWVzdGFtcE5zEhoKCG'
    'hvc3RuYW1lGBQgASgJUghob3N0bmFtZRIUCgVuZXRucxgeIAEoCVIFbmV0bnMSEgoEbnNpZBgo'
    'IAEoDVIEbnNpZBIUCgVsYWJlbBgyIAEoCVIFbGFiZWwSEAoDdGFnGDwgASgJUgN0YWcSJQoOcm'
    'Vjb3JkX2NvdW50ZXIYRiABKARSDXJlY29yZENvdW50ZXISGwoJc29ja2V0X2ZkGFAgASgEUghz'
    'b2NrZXRGZBIhCgxuZXRsaW5rZXJfaWQYWiABKARSC25ldGxpbmtlcklkEi8KFGluZXRfZGlhZ1'
    '9tc2dfZmFtaWx5GGUgASgNUhFpbmV0RGlhZ01zZ0ZhbWlseRItChNpbmV0X2RpYWdfbXNnX3N0'
    'YXRlGGYgASgNUhBpbmV0RGlhZ01zZ1N0YXRlEi0KE2luZXRfZGlhZ19tc2dfdGltZXIYZyABKA'
    '1SEGluZXREaWFnTXNnVGltZXISMQoVaW5ldF9kaWFnX21zZ19yZXRyYW5zGGggASgNUhJpbmV0'
    'RGlhZ01zZ1JldHJhbnMSRQogaW5ldF9kaWFnX21zZ19zb2NrZXRfc291cmNlX3BvcnQYaSABKA'
    '1SG2luZXREaWFnTXNnU29ja2V0U291cmNlUG9ydBJPCiVpbmV0X2RpYWdfbXNnX3NvY2tldF9k'
    'ZXN0aW5hdGlvbl9wb3J0GGogASgNUiBpbmV0RGlhZ01zZ1NvY2tldERlc3RpbmF0aW9uUG9ydB'
    'I8ChtpbmV0X2RpYWdfbXNnX3NvY2tldF9zb3VyY2UYayABKAxSF2luZXREaWFnTXNnU29ja2V0'
    'U291cmNlEkYKIGluZXRfZGlhZ19tc2dfc29ja2V0X2Rlc3RpbmF0aW9uGGwgASgMUhxpbmV0RG'
    'lhZ01zZ1NvY2tldERlc3RpbmF0aW9uEkIKHmluZXRfZGlhZ19tc2dfc29ja2V0X2ludGVyZmFj'
    'ZRhtIAEoDVIaaW5ldERpYWdNc2dTb2NrZXRJbnRlcmZhY2USPAobaW5ldF9kaWFnX21zZ19zb2'
    'NrZXRfY29va2llGG4gASgEUhdpbmV0RGlhZ01zZ1NvY2tldENvb2tpZRI/Ch1pbmV0X2RpYWdf'
    'bXNnX3NvY2tldF9kZXN0X2FzbhhvIAEoBFIYaW5ldERpYWdNc2dTb2NrZXREZXN0QXNuEkYKIW'
    'luZXRfZGlhZ19tc2dfc29ja2V0X25leHRfaG9wX2FzbhhwIAEoBFIbaW5ldERpYWdNc2dTb2Nr'
    'ZXROZXh0SG9wQXNuEjEKFWluZXRfZGlhZ19tc2dfZXhwaXJlcxhxIAEoDVISaW5ldERpYWdNc2'
    'dFeHBpcmVzEi8KFGluZXRfZGlhZ19tc2dfcnF1ZXVlGHIgASgNUhFpbmV0RGlhZ01zZ1JxdWV1'
    'ZRIvChRpbmV0X2RpYWdfbXNnX3dxdWV1ZRhzIAEoDVIRaW5ldERpYWdNc2dXcXVldWUSKQoRaW'
    '5ldF9kaWFnX21zZ191aWQYdCABKA1SDmluZXREaWFnTXNnVWlkEi0KE2luZXRfZGlhZ19tc2df'
    'aW5vZGUYdSABKA1SEGluZXREaWFnTXNnSW5vZGUSIwoNbWVtX2luZm9fcm1lbRjJASABKA1SC2'
    '1lbUluZm9SbWVtEiMKDW1lbV9pbmZvX3dtZW0YygEgASgNUgttZW1JbmZvV21lbRIjCg1tZW1f'
    'aW5mb19mbWVtGMsBIAEoDVILbWVtSW5mb0ZtZW0SIwoNbWVtX2luZm9fdG1lbRjMASABKA1SC2'
    '1lbUluZm9UbWVtEiUKDnRjcF9pbmZvX3N0YXRlGK0CIAEoDVIMdGNwSW5mb1N0YXRlEioKEXRj'
    'cF9pbmZvX2NhX3N0YXRlGK4CIAEoDVIOdGNwSW5mb0NhU3RhdGUSMQoUdGNwX2luZm9fcmV0cm'
    'Fuc21pdHMYrwIgASgNUhJ0Y3BJbmZvUmV0cmFuc21pdHMSJwoPdGNwX2luZm9fcHJvYmVzGLAC'
    'IAEoDVINdGNwSW5mb1Byb2JlcxIpChB0Y3BfaW5mb19iYWNrb2ZmGLECIAEoDVIOdGNwSW5mb0'
    'JhY2tvZmYSKQoQdGNwX2luZm9fb3B0aW9ucxiyAiABKA1SDnRjcEluZm9PcHRpb25zEi4KE3Rj'
    'cF9pbmZvX3NlbmRfc2NhbGUYswIgASgNUhB0Y3BJbmZvU2VuZFNjYWxlEiwKEnRjcF9pbmZvX3'
    'Jjdl9zY2FsZRi0AiABKA1SD3RjcEluZm9SY3ZTY2FsZRJKCiJ0Y3BfaW5mb19kZWxpdmVyeV9y'
    'YXRlX2FwcF9saW1pdGVkGLUCIAEoDVIddGNwSW5mb0RlbGl2ZXJ5UmF0ZUFwcExpbWl0ZWQSRg'
    'ogdGNwX2luZm9fZmFzdF9vcGVuX2NsaWVudF9mYWlsZWQYtgIgASgNUht0Y3BJbmZvRmFzdE9w'
    'ZW5DbGllbnRGYWlsZWQSIQoMdGNwX2luZm9fcnRvGLsCIAEoDVIKdGNwSW5mb1J0bxIhCgx0Y3'
    'BfaW5mb19hdG8YvAIgASgNUgp0Y3BJbmZvQXRvEigKEHRjcF9pbmZvX3NuZF9tc3MYvQIgASgN'
    'Ug10Y3BJbmZvU25kTXNzEigKEHRjcF9pbmZvX3Jjdl9tc3MYvgIgASgNUg10Y3BJbmZvUmN2TX'
    'NzEikKEHRjcF9pbmZvX3VuYWNrZWQYvwIgASgNUg50Y3BJbmZvVW5hY2tlZBInCg90Y3BfaW5m'
    'b19zYWNrZWQYwAIgASgNUg10Y3BJbmZvU2Fja2VkEiMKDXRjcF9pbmZvX2xvc3QYwQIgASgNUg'
    't0Y3BJbmZvTG9zdBIpChB0Y3BfaW5mb19yZXRyYW5zGMICIAEoDVIOdGNwSW5mb1JldHJhbnMS'
    'KQoQdGNwX2luZm9fZmFja2V0cxjDAiABKA1SDnRjcEluZm9GYWNrZXRzEjUKF3RjcF9pbmZvX2'
    'xhc3RfZGF0YV9zZW50GMQCIAEoDVITdGNwSW5mb0xhc3REYXRhU2VudBIzChZ0Y3BfaW5mb19s'
    'YXN0X2Fja19zZW50GMUCIAEoDVISdGNwSW5mb0xhc3RBY2tTZW50EjUKF3RjcF9pbmZvX2xhc3'
    'RfZGF0YV9yZWN2GMYCIAEoDVITdGNwSW5mb0xhc3REYXRhUmVjdhIzChZ0Y3BfaW5mb19sYXN0'
    'X2Fja19yZWN2GMcCIAEoDVISdGNwSW5mb0xhc3RBY2tSZWN2EiMKDXRjcF9pbmZvX3BtdHUYyA'
    'IgASgNUgt0Y3BJbmZvUG10dRIyChV0Y3BfaW5mb19yY3Zfc3N0aHJlc2gYyQIgASgNUhJ0Y3BJ'
    'bmZvUmN2U3N0aHJlc2gSIQoMdGNwX2luZm9fcnR0GMoCIAEoDVIKdGNwSW5mb1J0dBIoChB0Y3'
    'BfaW5mb19ydHRfdmFyGMsCIAEoDVINdGNwSW5mb1J0dFZhchIyChV0Y3BfaW5mb19zbmRfc3N0'
    'aHJlc2gYzAIgASgNUhJ0Y3BJbmZvU25kU3N0aHJlc2gSKgoRdGNwX2luZm9fc25kX2N3bmQYzQ'
    'IgASgNUg50Y3BJbmZvU25kQ3duZBIoChB0Y3BfaW5mb19hZHZfbXNzGM4CIAEoDVINdGNwSW5m'
    'b0Fkdk1zcxIvChN0Y3BfaW5mb19yZW9yZGVyaW5nGM8CIAEoDVIRdGNwSW5mb1Jlb3JkZXJpbm'
    'cSKAoQdGNwX2luZm9fcmN2X3J0dBjQAiABKA1SDXRjcEluZm9SY3ZSdHQSLAoSdGNwX2luZm9f'
    'cmN2X3NwYWNlGNECIAEoDVIPdGNwSW5mb1JjdlNwYWNlEjQKFnRjcF9pbmZvX3RvdGFsX3JldH'
    'JhbnMY0gIgASgNUhN0Y3BJbmZvVG90YWxSZXRyYW5zEjAKFHRjcF9pbmZvX3BhY2luZ19yYXRl'
    'GNMCIAEoBFIRdGNwSW5mb1BhY2luZ1JhdGUSNwoYdGNwX2luZm9fbWF4X3BhY2luZ19yYXRlGN'
    'QCIAEoBFIUdGNwSW5mb01heFBhY2luZ1JhdGUSMAoUdGNwX2luZm9fYnl0ZXNfYWNrZWQY1QIg'
    'ASgEUhF0Y3BJbmZvQnl0ZXNBY2tlZBI2Chd0Y3BfaW5mb19ieXRlc19yZWNlaXZlZBjWAiABKA'
    'RSFHRjcEluZm9CeXRlc1JlY2VpdmVkEioKEXRjcF9pbmZvX3NlZ3Nfb3V0GNcCIAEoDVIOdGNw'
    'SW5mb1NlZ3NPdXQSKAoQdGNwX2luZm9fc2Vnc19pbhjYAiABKA1SDXRjcEluZm9TZWdzSW4SNQ'
    'oXdGNwX2luZm9fbm90X3NlbnRfYnl0ZXMY2QIgASgNUhN0Y3BJbmZvTm90U2VudEJ5dGVzEigK'
    'EHRjcF9pbmZvX21pbl9ydHQY2gIgASgNUg10Y3BJbmZvTWluUnR0EjEKFXRjcF9pbmZvX2RhdG'
    'Ffc2Vnc19pbhjbAiABKA1SEXRjcEluZm9EYXRhU2Vnc0luEjMKFnRjcF9pbmZvX2RhdGFfc2Vn'
    'c19vdXQY3AIgASgNUhJ0Y3BJbmZvRGF0YVNlZ3NPdXQSNAoWdGNwX2luZm9fZGVsaXZlcnlfcm'
    'F0ZRjdAiABKARSE3RjcEluZm9EZWxpdmVyeVJhdGUSLAoSdGNwX2luZm9fYnVzeV90aW1lGN4C'
    'IAEoBFIPdGNwSW5mb0J1c3lUaW1lEjIKFXRjcF9pbmZvX3J3bmRfbGltaXRlZBjfAiABKARSEn'
    'RjcEluZm9Sd25kTGltaXRlZBI2Chd0Y3BfaW5mb19zbmRidWZfbGltaXRlZBjgAiABKARSFHRj'
    'cEluZm9TbmRidWZMaW1pdGVkEi0KEnRjcF9pbmZvX2RlbGl2ZXJlZBjhAiABKA1SEHRjcEluZm'
    '9EZWxpdmVyZWQSMgoVdGNwX2luZm9fZGVsaXZlcmVkX2NlGOICIAEoDVISdGNwSW5mb0RlbGl2'
    'ZXJlZENlEi4KE3RjcF9pbmZvX2J5dGVzX3NlbnQY4wIgASgEUhB0Y3BJbmZvQnl0ZXNTZW50Ej'
    'QKFnRjcF9pbmZvX2J5dGVzX3JldHJhbnMY5AIgASgEUhN0Y3BJbmZvQnl0ZXNSZXRyYW5zEi4K'
    'E3RjcF9pbmZvX2RzYWNrX2R1cHMY5QIgASgNUhB0Y3BJbmZvRHNhY2tEdXBzEi4KE3RjcF9pbm'
    'ZvX3Jlb3JkX3NlZW4Y5gIgASgNUhB0Y3BJbmZvUmVvcmRTZWVuEjAKFHRjcF9pbmZvX3Jjdl9v'
    'b29wYWNrGOcCIAEoDVIRdGNwSW5mb1Jjdk9vb3BhY2sSKAoQdGNwX2luZm9fc25kX3duZBjoAi'
    'ABKA1SDXRjcEluZm9TbmRXbmQSKAoQdGNwX2luZm9fcmN2X3duZBjpAiABKA1SDXRjcEluZm9S'
    'Y3ZXbmQSJwoPdGNwX2luZm9fcmVoYXNoGOoCIAEoDVINdGNwSW5mb1JlaGFzaBIsChJ0Y3BfaW'
    '5mb190b3RhbF9ydG8Y6wIgASgNUg90Y3BJbmZvVG90YWxSdG8SQQoddGNwX2luZm9fdG90YWxf'
    'cnRvX3JlY292ZXJpZXMY7AIgASgNUhl0Y3BJbmZvVG90YWxSdG9SZWNvdmVyaWVzEjUKF3RjcF'
    '9pbmZvX3RvdGFsX3J0b190aW1lGO0CIAEoDVITdGNwSW5mb1RvdGFsUnRvVGltZRI/Chtjb25n'
    'ZXN0aW9uX2FsZ29yaXRobV9zdHJpbmcYkAMgASgJUhljb25nZXN0aW9uQWxnb3JpdGhtU3RyaW'
    '5nEnQKGWNvbmdlc3Rpb25fYWxnb3JpdGhtX2VudW0YkQMgASgOMjcueHRjcF9mbGF0X3JlY29y'
    'ZC52MS5YdGNwRmxhdFJlY29yZC5Db25nZXN0aW9uQWxnb3JpdGhtUhdjb25nZXN0aW9uQWxnb3'
    'JpdGhtRW51bRInCg90eXBlX29mX3NlcnZpY2UY9QMgASgNUg10eXBlT2ZTZXJ2aWNlEiQKDXRy'
    'YWZmaWNfY2xhc3MY9gMgASgNUgx0cmFmZmljQ2xhc3MSMwoWc2tfbWVtX2luZm9fcm1lbV9hbG'
    'xvYxjZBCABKA1SEnNrTWVtSW5mb1JtZW1BbGxvYxItChNza19tZW1faW5mb19yY3ZfYnVmGNoE'
    'IAEoDVIPc2tNZW1JbmZvUmN2QnVmEjMKFnNrX21lbV9pbmZvX3dtZW1fYWxsb2MY2wQgASgNUh'
    'Jza01lbUluZm9XbWVtQWxsb2MSLQoTc2tfbWVtX2luZm9fc25kX2J1ZhjcBCABKA1SD3NrTWVt'
    'SW5mb1NuZEJ1ZhIxChVza19tZW1faW5mb19md2RfYWxsb2MY3QQgASgNUhFza01lbUluZm9Gd2'
    'RBbGxvYxI1Chdza19tZW1faW5mb193bWVtX3F1ZXVlZBjeBCABKA1SE3NrTWVtSW5mb1dtZW1R'
    'dWV1ZWQSLAoSc2tfbWVtX2luZm9fb3B0bWVtGN8EIAEoDVIPc2tNZW1JbmZvT3B0bWVtEi4KE3'
    'NrX21lbV9pbmZvX2JhY2tsb2cY4AQgASgNUhBza01lbUluZm9CYWNrbG9nEioKEXNrX21lbV9p'
    'bmZvX2Ryb3BzGOEEIAEoDVIOc2tNZW1JbmZvRHJvcHMSJgoOc2h1dGRvd25fc3RhdGUYvAUgAS'
    'gNUg1zaHV0ZG93blN0YXRlEi0KEnZlZ2FzX2luZm9fZW5hYmxlZBihBiABKA1SEHZlZ2FzSW5m'
    'b0VuYWJsZWQSLAoSdmVnYXNfaW5mb19ydHRfY250GKIGIAEoDVIPdmVnYXNJbmZvUnR0Q250Ei'
    'UKDnZlZ2FzX2luZm9fcnR0GKMGIAEoDVIMdmVnYXNJbmZvUnR0EiwKEnZlZ2FzX2luZm9fbWlu'
    'X3J0dBikBiABKA1SD3ZlZ2FzSW5mb01pblJ0dBItChJkY3RjcF9pbmZvX2VuYWJsZWQYhQcgAS'
    'gNUhBkY3RjcEluZm9FbmFibGVkEi4KE2RjdGNwX2luZm9fY2Vfc3RhdGUYhgcgASgNUhBkY3Rj'
    'cEluZm9DZVN0YXRlEikKEGRjdGNwX2luZm9fYWxwaGEYhwcgASgNUg5kY3RjcEluZm9BbHBoYR'
    'IqChFkY3RjcF9pbmZvX2FiX2VjbhiIByABKA1SDmRjdGNwSW5mb0FiRWNuEioKEWRjdGNwX2lu'
    'Zm9fYWJfdG90GIkHIAEoDVIOZGN0Y3BJbmZvQWJUb3QSJAoOYmJyX2luZm9fYndfbG8Y6QcgAS'
    'gNUgtiYnJJbmZvQndMbxIkCg5iYnJfaW5mb19id19oaRjqByABKA1SC2JickluZm9Cd0hpEigK'
    'EGJicl9pbmZvX21pbl9ydHQY6wcgASgNUg1iYnJJbmZvTWluUnR0EjAKFGJicl9pbmZvX3BhY2'
    'luZ19nYWluGOwHIAEoDVIRYmJySW5mb1BhY2luZ0dhaW4SLAoSYmJyX2luZm9fY3duZF9nYWlu'
    'GO0HIAEoDVIPYmJySW5mb0N3bmRHYWluEhoKCGNsYXNzX2lkGM0IIAEoDVIHY2xhc3NJZBIaCg'
    'hzb2NrX29wdBjOCCABKA1SB3NvY2tPcHQSGAoHY19ncm91cBizCSABKARSBmNHcm91cCKZAgoT'
    'Q29uZ2VzdGlvbkFsZ29yaXRobRIkCiBDT05HRVNUSU9OX0FMR09SSVRITV9VTlNQRUNJRklFRB'
    'AAEh4KGkNPTkdFU1RJT05fQUxHT1JJVEhNX0NVQklDEAESHgoaQ09OR0VTVElPTl9BTEdPUklU'
    'SE1fRENUQ1AQAhIeChpDT05HRVNUSU9OX0FMR09SSVRITV9WRUdBUxADEh8KG0NPTkdFU1RJT0'
    '5fQUxHT1JJVEhNX1BSQUdVRRAEEh0KGUNPTkdFU1RJT05fQUxHT1JJVEhNX0JCUjEQBRIdChlD'
    'T05HRVNUSU9OX0FMR09SSVRITV9CQlIyEAYSHQoZQ09OR0VTVElPTl9BTEdPUklUSE1fQkJSMx'
    'AH');

@$core.Deprecated('Use flatRecordsRequestDescriptor instead')
const FlatRecordsRequest$json = {
  '1': 'FlatRecordsRequest',
};

/// Descriptor for `FlatRecordsRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List flatRecordsRequestDescriptor = $convert.base64Decode(
    'ChJGbGF0UmVjb3Jkc1JlcXVlc3Q=');

@$core.Deprecated('Use flatRecordsResponseDescriptor instead')
const FlatRecordsResponse$json = {
  '1': 'FlatRecordsResponse',
  '2': [
    {'1': 'xtcp_flat_record', '3': 1, '4': 1, '5': 11, '6': '.xtcp_flat_record.v1.XtcpFlatRecord', '10': 'xtcpFlatRecord'},
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
final $typed_data.Uint8List pollFlatRecordsRequestDescriptor = $convert.base64Decode(
    'ChZQb2xsRmxhdFJlY29yZHNSZXF1ZXN0');

@$core.Deprecated('Use pollFlatRecordsResponseDescriptor instead')
const PollFlatRecordsResponse$json = {
  '1': 'PollFlatRecordsResponse',
  '2': [
    {'1': 'xtcp_flat_record', '3': 1, '4': 1, '5': 11, '6': '.xtcp_flat_record.v1.XtcpFlatRecord', '10': 'xtcpFlatRecord'},
  ],
};

/// Descriptor for `PollFlatRecordsResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List pollFlatRecordsResponseDescriptor = $convert.base64Decode(
    'ChdQb2xsRmxhdFJlY29yZHNSZXNwb25zZRJNChB4dGNwX2ZsYXRfcmVjb3JkGAEgASgLMiMueH'
    'RjcF9mbGF0X3JlY29yZC52MS5YdGNwRmxhdFJlY29yZFIOeHRjcEZsYXRSZWNvcmQ=');

