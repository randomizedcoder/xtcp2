//
//  Generated code. Do not modify.
//  source: xtcp_flat_record/v1/xtcp_flat_record.proto
//
// @dart = 2.12

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names, library_prefixes
// ignore_for_file: non_constant_identifier_names, prefer_final_fields
// ignore_for_file: unnecessary_import, unnecessary_this, unused_import

import 'dart:core' as $core;

import 'package:fixnum/fixnum.dart' as $fixnum;
import 'package:protobuf/protobuf.dart' as $pb;

import 'xtcp_flat_record.pbenum.dart';

export 'xtcp_flat_record.pbenum.dart';

class FlatRecordsRequest extends $pb.GeneratedMessage {
  factory FlatRecordsRequest() => create();
  FlatRecordsRequest._() : super();
  factory FlatRecordsRequest.fromBuffer($core.List<$core.int> i, [$pb.ExtensionRegistry r = $pb.ExtensionRegistry.EMPTY]) => create()..mergeFromBuffer(i, r);
  factory FlatRecordsRequest.fromJson($core.String i, [$pb.ExtensionRegistry r = $pb.ExtensionRegistry.EMPTY]) => create()..mergeFromJson(i, r);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(_omitMessageNames ? '' : 'FlatRecordsRequest', package: const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_flat_record.v1'), createEmptyInstance: create)
    ..hasRequiredFields = false
  ;

  @$core.Deprecated(
  'Using this can add significant overhead to your binary. '
  'Use [GeneratedMessageGenericExtensions.deepCopy] instead. '
  'Will be removed in next major version')
  FlatRecordsRequest clone() => FlatRecordsRequest()..mergeFromMessage(this);
  @$core.Deprecated(
  'Using this can add significant overhead to your binary. '
  'Use [GeneratedMessageGenericExtensions.rebuild] instead. '
  'Will be removed in next major version')
  FlatRecordsRequest copyWith(void Function(FlatRecordsRequest) updates) => super.copyWith((message) => updates(message as FlatRecordsRequest)) as FlatRecordsRequest;

  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static FlatRecordsRequest create() => FlatRecordsRequest._();
  FlatRecordsRequest createEmptyInstance() => create();
  static $pb.PbList<FlatRecordsRequest> createRepeated() => $pb.PbList<FlatRecordsRequest>();
  @$core.pragma('dart2js:noInline')
  static FlatRecordsRequest getDefault() => _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<FlatRecordsRequest>(create);
  static FlatRecordsRequest? _defaultInstance;
}

class FlatRecordsResponse extends $pb.GeneratedMessage {
  factory FlatRecordsResponse({
    XtcpFlatRecord? xtcpFlatRecord,
  }) {
    final $result = create();
    if (xtcpFlatRecord != null) {
      $result.xtcpFlatRecord = xtcpFlatRecord;
    }
    return $result;
  }
  FlatRecordsResponse._() : super();
  factory FlatRecordsResponse.fromBuffer($core.List<$core.int> i, [$pb.ExtensionRegistry r = $pb.ExtensionRegistry.EMPTY]) => create()..mergeFromBuffer(i, r);
  factory FlatRecordsResponse.fromJson($core.String i, [$pb.ExtensionRegistry r = $pb.ExtensionRegistry.EMPTY]) => create()..mergeFromJson(i, r);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(_omitMessageNames ? '' : 'FlatRecordsResponse', package: const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_flat_record.v1'), createEmptyInstance: create)
    ..aOM<XtcpFlatRecord>(1, _omitFieldNames ? '' : 'xtcpFlatRecord', subBuilder: XtcpFlatRecord.create)
    ..hasRequiredFields = false
  ;

  @$core.Deprecated(
  'Using this can add significant overhead to your binary. '
  'Use [GeneratedMessageGenericExtensions.deepCopy] instead. '
  'Will be removed in next major version')
  FlatRecordsResponse clone() => FlatRecordsResponse()..mergeFromMessage(this);
  @$core.Deprecated(
  'Using this can add significant overhead to your binary. '
  'Use [GeneratedMessageGenericExtensions.rebuild] instead. '
  'Will be removed in next major version')
  FlatRecordsResponse copyWith(void Function(FlatRecordsResponse) updates) => super.copyWith((message) => updates(message as FlatRecordsResponse)) as FlatRecordsResponse;

  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static FlatRecordsResponse create() => FlatRecordsResponse._();
  FlatRecordsResponse createEmptyInstance() => create();
  static $pb.PbList<FlatRecordsResponse> createRepeated() => $pb.PbList<FlatRecordsResponse>();
  @$core.pragma('dart2js:noInline')
  static FlatRecordsResponse getDefault() => _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<FlatRecordsResponse>(create);
  static FlatRecordsResponse? _defaultInstance;

  @$pb.TagNumber(1)
  XtcpFlatRecord get xtcpFlatRecord => $_getN(0);
  @$pb.TagNumber(1)
  set xtcpFlatRecord(XtcpFlatRecord v) { setField(1, v); }
  @$pb.TagNumber(1)
  $core.bool hasXtcpFlatRecord() => $_has(0);
  @$pb.TagNumber(1)
  void clearXtcpFlatRecord() => clearField(1);
  @$pb.TagNumber(1)
  XtcpFlatRecord ensureXtcpFlatRecord() => $_ensure(0);
}

class PollFlatRecordsRequest extends $pb.GeneratedMessage {
  factory PollFlatRecordsRequest() => create();
  PollFlatRecordsRequest._() : super();
  factory PollFlatRecordsRequest.fromBuffer($core.List<$core.int> i, [$pb.ExtensionRegistry r = $pb.ExtensionRegistry.EMPTY]) => create()..mergeFromBuffer(i, r);
  factory PollFlatRecordsRequest.fromJson($core.String i, [$pb.ExtensionRegistry r = $pb.ExtensionRegistry.EMPTY]) => create()..mergeFromJson(i, r);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(_omitMessageNames ? '' : 'PollFlatRecordsRequest', package: const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_flat_record.v1'), createEmptyInstance: create)
    ..hasRequiredFields = false
  ;

  @$core.Deprecated(
  'Using this can add significant overhead to your binary. '
  'Use [GeneratedMessageGenericExtensions.deepCopy] instead. '
  'Will be removed in next major version')
  PollFlatRecordsRequest clone() => PollFlatRecordsRequest()..mergeFromMessage(this);
  @$core.Deprecated(
  'Using this can add significant overhead to your binary. '
  'Use [GeneratedMessageGenericExtensions.rebuild] instead. '
  'Will be removed in next major version')
  PollFlatRecordsRequest copyWith(void Function(PollFlatRecordsRequest) updates) => super.copyWith((message) => updates(message as PollFlatRecordsRequest)) as PollFlatRecordsRequest;

  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static PollFlatRecordsRequest create() => PollFlatRecordsRequest._();
  PollFlatRecordsRequest createEmptyInstance() => create();
  static $pb.PbList<PollFlatRecordsRequest> createRepeated() => $pb.PbList<PollFlatRecordsRequest>();
  @$core.pragma('dart2js:noInline')
  static PollFlatRecordsRequest getDefault() => _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<PollFlatRecordsRequest>(create);
  static PollFlatRecordsRequest? _defaultInstance;
}

/// https://clickhouse.com/docs/en/interfaces/formats#protobuflist
class Envelope extends $pb.GeneratedMessage {
  factory Envelope({
    $core.Iterable<XtcpFlatRecord>? row,
  }) {
    final $result = create();
    if (row != null) {
      $result.row.addAll(row);
    }
    return $result;
  }
  Envelope._() : super();
  factory Envelope.fromBuffer($core.List<$core.int> i, [$pb.ExtensionRegistry r = $pb.ExtensionRegistry.EMPTY]) => create()..mergeFromBuffer(i, r);
  factory Envelope.fromJson($core.String i, [$pb.ExtensionRegistry r = $pb.ExtensionRegistry.EMPTY]) => create()..mergeFromJson(i, r);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(_omitMessageNames ? '' : 'Envelope', package: const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_flat_record.v1'), createEmptyInstance: create)
    ..pc<XtcpFlatRecord>(1, _omitFieldNames ? '' : 'row', $pb.PbFieldType.PM, subBuilder: XtcpFlatRecord.create)
    ..hasRequiredFields = false
  ;

  @$core.Deprecated(
  'Using this can add significant overhead to your binary. '
  'Use [GeneratedMessageGenericExtensions.deepCopy] instead. '
  'Will be removed in next major version')
  Envelope clone() => Envelope()..mergeFromMessage(this);
  @$core.Deprecated(
  'Using this can add significant overhead to your binary. '
  'Use [GeneratedMessageGenericExtensions.rebuild] instead. '
  'Will be removed in next major version')
  Envelope copyWith(void Function(Envelope) updates) => super.copyWith((message) => updates(message as Envelope)) as Envelope;

  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static Envelope create() => Envelope._();
  Envelope createEmptyInstance() => create();
  static $pb.PbList<Envelope> createRepeated() => $pb.PbList<Envelope>();
  @$core.pragma('dart2js:noInline')
  static Envelope getDefault() => _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<Envelope>(create);
  static Envelope? _defaultInstance;

  @$pb.TagNumber(1)
  $core.List<XtcpFlatRecord> get row => $_getList(0);
}

/// xtcp_flat_record is the record type exported by xtcp with ALL the inet_diag information
class XtcpFlatRecord extends $pb.GeneratedMessage {
  factory XtcpFlatRecord({
    $fixnum.Int64? sec,
    $fixnum.Int64? nsec,
    $core.String? hostname,
    $core.String? netns,
    $core.int? nsid,
    $core.String? label,
    $core.String? tag,
    $fixnum.Int64? recordCounter,
    $fixnum.Int64? socketFd,
    $fixnum.Int64? netlinkerId,
    $core.int? inetDiagMsgFamily,
    $core.int? inetDiagMsgState,
    $core.int? inetDiagMsgTimer,
    $core.int? inetDiagMsgRetrans,
    $core.int? inetDiagMsgSocketSourcePort,
    $core.int? inetDiagMsgSocketDestinationPort,
    $core.List<$core.int>? inetDiagMsgSocketSource,
    $core.List<$core.int>? inetDiagMsgSocketDestination,
    $core.int? inetDiagMsgSocketInterface,
    $fixnum.Int64? inetDiagMsgSocketCookie,
    $fixnum.Int64? inetDiagMsgSocketDestAsn,
    $fixnum.Int64? inetDiagMsgSocketNextHopAsn,
    $core.int? inetDiagMsgExpires,
    $core.int? inetDiagMsgRqueue,
    $core.int? inetDiagMsgWqueue,
    $core.int? inetDiagMsgUid,
    $core.int? inetDiagMsgInode,
    $core.int? memInfoRmem,
    $core.int? memInfoWmem,
    $core.int? memInfoFmem,
    $core.int? memInfoTmem,
    $core.int? tcpInfoState,
    $core.int? tcpInfoCaState,
    $core.int? tcpInfoRetransmits,
    $core.int? tcpInfoProbes,
    $core.int? tcpInfoBackoff,
    $core.int? tcpInfoOptions,
    $core.int? tcpInfoSendScale,
    $core.int? tcpInfoRcvScale,
    $core.int? tcpInfoDeliveryRateAppLimited,
    $core.int? tcpInfoFastOpenClientFailed,
    $core.int? tcpInfoRto,
    $core.int? tcpInfoAto,
    $core.int? tcpInfoSndMss,
    $core.int? tcpInfoRcvMss,
    $core.int? tcpInfoUnacked,
    $core.int? tcpInfoSacked,
    $core.int? tcpInfoLost,
    $core.int? tcpInfoRetrans,
    $core.int? tcpInfoFackets,
    $core.int? tcpInfoLastDataSent,
    $core.int? tcpInfoLastAckSent,
    $core.int? tcpInfoLastDataRecv,
    $core.int? tcpInfoLastAckRecv,
    $core.int? tcpInfoPmtu,
    $core.int? tcpInfoRcvSsthresh,
    $core.int? tcpInfoRtt,
    $core.int? tcpInfoRttVar,
    $core.int? tcpInfoSndSsthresh,
    $core.int? tcpInfoSndCwnd,
    $core.int? tcpInfoAdvMss,
    $core.int? tcpInfoReordering,
    $core.int? tcpInfoRcvRtt,
    $core.int? tcpInfoRcvSpace,
    $core.int? tcpInfoTotalRetrans,
    $fixnum.Int64? tcpInfoPacingRate,
    $fixnum.Int64? tcpInfoMaxPacingRate,
    $fixnum.Int64? tcpInfoBytesAcked,
    $fixnum.Int64? tcpInfoBytesReceived,
    $core.int? tcpInfoSegsOut,
    $core.int? tcpInfoSegsIn,
    $core.int? tcpInfoNotSentBytes,
    $core.int? tcpInfoMinRtt,
    $core.int? tcpInfoDataSegsIn,
    $core.int? tcpInfoDataSegsOut,
    $fixnum.Int64? tcpInfoDeliveryRate,
    $fixnum.Int64? tcpInfoBusyTime,
    $fixnum.Int64? tcpInfoRwndLimited,
    $fixnum.Int64? tcpInfoSndbufLimited,
    $core.int? tcpInfoDelivered,
    $core.int? tcpInfoDeliveredCe,
    $fixnum.Int64? tcpInfoBytesSent,
    $fixnum.Int64? tcpInfoBytesRetrans,
    $core.int? tcpInfoDsackDups,
    $core.int? tcpInfoReordSeen,
    $core.int? tcpInfoRcvOoopack,
    $core.int? tcpInfoSndWnd,
    $core.int? tcpInfoRcvWnd,
    $core.int? tcpInfoRehash,
    $core.int? tcpInfoTotalRto,
    $core.int? tcpInfoTotalRtoRecoveries,
    $core.int? tcpInfoTotalRtoTime,
    $core.String? congestionAlgorithmString,
    XtcpFlatRecord_CongestionAlgorithm? congestionAlgorithmEnum,
    $core.int? typeOfService,
    $core.int? trafficClass,
    $core.int? skMemInfoRmemAlloc,
    $core.int? skMemInfoRcvBuf,
    $core.int? skMemInfoWmemAlloc,
    $core.int? skMemInfoSndBuf,
    $core.int? skMemInfoFwdAlloc,
    $core.int? skMemInfoWmemQueued,
    $core.int? skMemInfoOptmem,
    $core.int? skMemInfoBacklog,
    $core.int? skMemInfoDrops,
    $core.int? shutdownState,
    $core.int? vegasInfoEnabled,
    $core.int? vegasInfoRttCnt,
    $core.int? vegasInfoRtt,
    $core.int? vegasInfoMinRtt,
    $core.int? dctcpInfoEnabled,
    $core.int? dctcpInfoCeState,
    $core.int? dctcpInfoAlpha,
    $core.int? dctcpInfoAbEcn,
    $core.int? dctcpInfoAbTot,
    $core.int? bbrInfoBwLo,
    $core.int? bbrInfoBwHi,
    $core.int? bbrInfoMinRtt,
    $core.int? bbrInfoPacingGain,
    $core.int? bbrInfoCwndGain,
    $core.int? classId,
    $core.int? sockOpt,
    $fixnum.Int64? cGroup,
  }) {
    final $result = create();
    if (sec != null) {
      $result.sec = sec;
    }
    if (nsec != null) {
      $result.nsec = nsec;
    }
    if (hostname != null) {
      $result.hostname = hostname;
    }
    if (netns != null) {
      $result.netns = netns;
    }
    if (nsid != null) {
      $result.nsid = nsid;
    }
    if (label != null) {
      $result.label = label;
    }
    if (tag != null) {
      $result.tag = tag;
    }
    if (recordCounter != null) {
      $result.recordCounter = recordCounter;
    }
    if (socketFd != null) {
      $result.socketFd = socketFd;
    }
    if (netlinkerId != null) {
      $result.netlinkerId = netlinkerId;
    }
    if (inetDiagMsgFamily != null) {
      $result.inetDiagMsgFamily = inetDiagMsgFamily;
    }
    if (inetDiagMsgState != null) {
      $result.inetDiagMsgState = inetDiagMsgState;
    }
    if (inetDiagMsgTimer != null) {
      $result.inetDiagMsgTimer = inetDiagMsgTimer;
    }
    if (inetDiagMsgRetrans != null) {
      $result.inetDiagMsgRetrans = inetDiagMsgRetrans;
    }
    if (inetDiagMsgSocketSourcePort != null) {
      $result.inetDiagMsgSocketSourcePort = inetDiagMsgSocketSourcePort;
    }
    if (inetDiagMsgSocketDestinationPort != null) {
      $result.inetDiagMsgSocketDestinationPort = inetDiagMsgSocketDestinationPort;
    }
    if (inetDiagMsgSocketSource != null) {
      $result.inetDiagMsgSocketSource = inetDiagMsgSocketSource;
    }
    if (inetDiagMsgSocketDestination != null) {
      $result.inetDiagMsgSocketDestination = inetDiagMsgSocketDestination;
    }
    if (inetDiagMsgSocketInterface != null) {
      $result.inetDiagMsgSocketInterface = inetDiagMsgSocketInterface;
    }
    if (inetDiagMsgSocketCookie != null) {
      $result.inetDiagMsgSocketCookie = inetDiagMsgSocketCookie;
    }
    if (inetDiagMsgSocketDestAsn != null) {
      $result.inetDiagMsgSocketDestAsn = inetDiagMsgSocketDestAsn;
    }
    if (inetDiagMsgSocketNextHopAsn != null) {
      $result.inetDiagMsgSocketNextHopAsn = inetDiagMsgSocketNextHopAsn;
    }
    if (inetDiagMsgExpires != null) {
      $result.inetDiagMsgExpires = inetDiagMsgExpires;
    }
    if (inetDiagMsgRqueue != null) {
      $result.inetDiagMsgRqueue = inetDiagMsgRqueue;
    }
    if (inetDiagMsgWqueue != null) {
      $result.inetDiagMsgWqueue = inetDiagMsgWqueue;
    }
    if (inetDiagMsgUid != null) {
      $result.inetDiagMsgUid = inetDiagMsgUid;
    }
    if (inetDiagMsgInode != null) {
      $result.inetDiagMsgInode = inetDiagMsgInode;
    }
    if (memInfoRmem != null) {
      $result.memInfoRmem = memInfoRmem;
    }
    if (memInfoWmem != null) {
      $result.memInfoWmem = memInfoWmem;
    }
    if (memInfoFmem != null) {
      $result.memInfoFmem = memInfoFmem;
    }
    if (memInfoTmem != null) {
      $result.memInfoTmem = memInfoTmem;
    }
    if (tcpInfoState != null) {
      $result.tcpInfoState = tcpInfoState;
    }
    if (tcpInfoCaState != null) {
      $result.tcpInfoCaState = tcpInfoCaState;
    }
    if (tcpInfoRetransmits != null) {
      $result.tcpInfoRetransmits = tcpInfoRetransmits;
    }
    if (tcpInfoProbes != null) {
      $result.tcpInfoProbes = tcpInfoProbes;
    }
    if (tcpInfoBackoff != null) {
      $result.tcpInfoBackoff = tcpInfoBackoff;
    }
    if (tcpInfoOptions != null) {
      $result.tcpInfoOptions = tcpInfoOptions;
    }
    if (tcpInfoSendScale != null) {
      $result.tcpInfoSendScale = tcpInfoSendScale;
    }
    if (tcpInfoRcvScale != null) {
      $result.tcpInfoRcvScale = tcpInfoRcvScale;
    }
    if (tcpInfoDeliveryRateAppLimited != null) {
      $result.tcpInfoDeliveryRateAppLimited = tcpInfoDeliveryRateAppLimited;
    }
    if (tcpInfoFastOpenClientFailed != null) {
      $result.tcpInfoFastOpenClientFailed = tcpInfoFastOpenClientFailed;
    }
    if (tcpInfoRto != null) {
      $result.tcpInfoRto = tcpInfoRto;
    }
    if (tcpInfoAto != null) {
      $result.tcpInfoAto = tcpInfoAto;
    }
    if (tcpInfoSndMss != null) {
      $result.tcpInfoSndMss = tcpInfoSndMss;
    }
    if (tcpInfoRcvMss != null) {
      $result.tcpInfoRcvMss = tcpInfoRcvMss;
    }
    if (tcpInfoUnacked != null) {
      $result.tcpInfoUnacked = tcpInfoUnacked;
    }
    if (tcpInfoSacked != null) {
      $result.tcpInfoSacked = tcpInfoSacked;
    }
    if (tcpInfoLost != null) {
      $result.tcpInfoLost = tcpInfoLost;
    }
    if (tcpInfoRetrans != null) {
      $result.tcpInfoRetrans = tcpInfoRetrans;
    }
    if (tcpInfoFackets != null) {
      $result.tcpInfoFackets = tcpInfoFackets;
    }
    if (tcpInfoLastDataSent != null) {
      $result.tcpInfoLastDataSent = tcpInfoLastDataSent;
    }
    if (tcpInfoLastAckSent != null) {
      $result.tcpInfoLastAckSent = tcpInfoLastAckSent;
    }
    if (tcpInfoLastDataRecv != null) {
      $result.tcpInfoLastDataRecv = tcpInfoLastDataRecv;
    }
    if (tcpInfoLastAckRecv != null) {
      $result.tcpInfoLastAckRecv = tcpInfoLastAckRecv;
    }
    if (tcpInfoPmtu != null) {
      $result.tcpInfoPmtu = tcpInfoPmtu;
    }
    if (tcpInfoRcvSsthresh != null) {
      $result.tcpInfoRcvSsthresh = tcpInfoRcvSsthresh;
    }
    if (tcpInfoRtt != null) {
      $result.tcpInfoRtt = tcpInfoRtt;
    }
    if (tcpInfoRttVar != null) {
      $result.tcpInfoRttVar = tcpInfoRttVar;
    }
    if (tcpInfoSndSsthresh != null) {
      $result.tcpInfoSndSsthresh = tcpInfoSndSsthresh;
    }
    if (tcpInfoSndCwnd != null) {
      $result.tcpInfoSndCwnd = tcpInfoSndCwnd;
    }
    if (tcpInfoAdvMss != null) {
      $result.tcpInfoAdvMss = tcpInfoAdvMss;
    }
    if (tcpInfoReordering != null) {
      $result.tcpInfoReordering = tcpInfoReordering;
    }
    if (tcpInfoRcvRtt != null) {
      $result.tcpInfoRcvRtt = tcpInfoRcvRtt;
    }
    if (tcpInfoRcvSpace != null) {
      $result.tcpInfoRcvSpace = tcpInfoRcvSpace;
    }
    if (tcpInfoTotalRetrans != null) {
      $result.tcpInfoTotalRetrans = tcpInfoTotalRetrans;
    }
    if (tcpInfoPacingRate != null) {
      $result.tcpInfoPacingRate = tcpInfoPacingRate;
    }
    if (tcpInfoMaxPacingRate != null) {
      $result.tcpInfoMaxPacingRate = tcpInfoMaxPacingRate;
    }
    if (tcpInfoBytesAcked != null) {
      $result.tcpInfoBytesAcked = tcpInfoBytesAcked;
    }
    if (tcpInfoBytesReceived != null) {
      $result.tcpInfoBytesReceived = tcpInfoBytesReceived;
    }
    if (tcpInfoSegsOut != null) {
      $result.tcpInfoSegsOut = tcpInfoSegsOut;
    }
    if (tcpInfoSegsIn != null) {
      $result.tcpInfoSegsIn = tcpInfoSegsIn;
    }
    if (tcpInfoNotSentBytes != null) {
      $result.tcpInfoNotSentBytes = tcpInfoNotSentBytes;
    }
    if (tcpInfoMinRtt != null) {
      $result.tcpInfoMinRtt = tcpInfoMinRtt;
    }
    if (tcpInfoDataSegsIn != null) {
      $result.tcpInfoDataSegsIn = tcpInfoDataSegsIn;
    }
    if (tcpInfoDataSegsOut != null) {
      $result.tcpInfoDataSegsOut = tcpInfoDataSegsOut;
    }
    if (tcpInfoDeliveryRate != null) {
      $result.tcpInfoDeliveryRate = tcpInfoDeliveryRate;
    }
    if (tcpInfoBusyTime != null) {
      $result.tcpInfoBusyTime = tcpInfoBusyTime;
    }
    if (tcpInfoRwndLimited != null) {
      $result.tcpInfoRwndLimited = tcpInfoRwndLimited;
    }
    if (tcpInfoSndbufLimited != null) {
      $result.tcpInfoSndbufLimited = tcpInfoSndbufLimited;
    }
    if (tcpInfoDelivered != null) {
      $result.tcpInfoDelivered = tcpInfoDelivered;
    }
    if (tcpInfoDeliveredCe != null) {
      $result.tcpInfoDeliveredCe = tcpInfoDeliveredCe;
    }
    if (tcpInfoBytesSent != null) {
      $result.tcpInfoBytesSent = tcpInfoBytesSent;
    }
    if (tcpInfoBytesRetrans != null) {
      $result.tcpInfoBytesRetrans = tcpInfoBytesRetrans;
    }
    if (tcpInfoDsackDups != null) {
      $result.tcpInfoDsackDups = tcpInfoDsackDups;
    }
    if (tcpInfoReordSeen != null) {
      $result.tcpInfoReordSeen = tcpInfoReordSeen;
    }
    if (tcpInfoRcvOoopack != null) {
      $result.tcpInfoRcvOoopack = tcpInfoRcvOoopack;
    }
    if (tcpInfoSndWnd != null) {
      $result.tcpInfoSndWnd = tcpInfoSndWnd;
    }
    if (tcpInfoRcvWnd != null) {
      $result.tcpInfoRcvWnd = tcpInfoRcvWnd;
    }
    if (tcpInfoRehash != null) {
      $result.tcpInfoRehash = tcpInfoRehash;
    }
    if (tcpInfoTotalRto != null) {
      $result.tcpInfoTotalRto = tcpInfoTotalRto;
    }
    if (tcpInfoTotalRtoRecoveries != null) {
      $result.tcpInfoTotalRtoRecoveries = tcpInfoTotalRtoRecoveries;
    }
    if (tcpInfoTotalRtoTime != null) {
      $result.tcpInfoTotalRtoTime = tcpInfoTotalRtoTime;
    }
    if (congestionAlgorithmString != null) {
      $result.congestionAlgorithmString = congestionAlgorithmString;
    }
    if (congestionAlgorithmEnum != null) {
      $result.congestionAlgorithmEnum = congestionAlgorithmEnum;
    }
    if (typeOfService != null) {
      $result.typeOfService = typeOfService;
    }
    if (trafficClass != null) {
      $result.trafficClass = trafficClass;
    }
    if (skMemInfoRmemAlloc != null) {
      $result.skMemInfoRmemAlloc = skMemInfoRmemAlloc;
    }
    if (skMemInfoRcvBuf != null) {
      $result.skMemInfoRcvBuf = skMemInfoRcvBuf;
    }
    if (skMemInfoWmemAlloc != null) {
      $result.skMemInfoWmemAlloc = skMemInfoWmemAlloc;
    }
    if (skMemInfoSndBuf != null) {
      $result.skMemInfoSndBuf = skMemInfoSndBuf;
    }
    if (skMemInfoFwdAlloc != null) {
      $result.skMemInfoFwdAlloc = skMemInfoFwdAlloc;
    }
    if (skMemInfoWmemQueued != null) {
      $result.skMemInfoWmemQueued = skMemInfoWmemQueued;
    }
    if (skMemInfoOptmem != null) {
      $result.skMemInfoOptmem = skMemInfoOptmem;
    }
    if (skMemInfoBacklog != null) {
      $result.skMemInfoBacklog = skMemInfoBacklog;
    }
    if (skMemInfoDrops != null) {
      $result.skMemInfoDrops = skMemInfoDrops;
    }
    if (shutdownState != null) {
      $result.shutdownState = shutdownState;
    }
    if (vegasInfoEnabled != null) {
      $result.vegasInfoEnabled = vegasInfoEnabled;
    }
    if (vegasInfoRttCnt != null) {
      $result.vegasInfoRttCnt = vegasInfoRttCnt;
    }
    if (vegasInfoRtt != null) {
      $result.vegasInfoRtt = vegasInfoRtt;
    }
    if (vegasInfoMinRtt != null) {
      $result.vegasInfoMinRtt = vegasInfoMinRtt;
    }
    if (dctcpInfoEnabled != null) {
      $result.dctcpInfoEnabled = dctcpInfoEnabled;
    }
    if (dctcpInfoCeState != null) {
      $result.dctcpInfoCeState = dctcpInfoCeState;
    }
    if (dctcpInfoAlpha != null) {
      $result.dctcpInfoAlpha = dctcpInfoAlpha;
    }
    if (dctcpInfoAbEcn != null) {
      $result.dctcpInfoAbEcn = dctcpInfoAbEcn;
    }
    if (dctcpInfoAbTot != null) {
      $result.dctcpInfoAbTot = dctcpInfoAbTot;
    }
    if (bbrInfoBwLo != null) {
      $result.bbrInfoBwLo = bbrInfoBwLo;
    }
    if (bbrInfoBwHi != null) {
      $result.bbrInfoBwHi = bbrInfoBwHi;
    }
    if (bbrInfoMinRtt != null) {
      $result.bbrInfoMinRtt = bbrInfoMinRtt;
    }
    if (bbrInfoPacingGain != null) {
      $result.bbrInfoPacingGain = bbrInfoPacingGain;
    }
    if (bbrInfoCwndGain != null) {
      $result.bbrInfoCwndGain = bbrInfoCwndGain;
    }
    if (classId != null) {
      $result.classId = classId;
    }
    if (sockOpt != null) {
      $result.sockOpt = sockOpt;
    }
    if (cGroup != null) {
      $result.cGroup = cGroup;
    }
    return $result;
  }
  XtcpFlatRecord._() : super();
  factory XtcpFlatRecord.fromBuffer($core.List<$core.int> i, [$pb.ExtensionRegistry r = $pb.ExtensionRegistry.EMPTY]) => create()..mergeFromBuffer(i, r);
  factory XtcpFlatRecord.fromJson($core.String i, [$pb.ExtensionRegistry r = $pb.ExtensionRegistry.EMPTY]) => create()..mergeFromJson(i, r);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(_omitMessageNames ? '' : 'XtcpFlatRecord', package: const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_flat_record.v1'), createEmptyInstance: create)
    ..a<$fixnum.Int64>(1, _omitFieldNames ? '' : 'sec', $pb.PbFieldType.OU6, defaultOrMaker: $fixnum.Int64.ZERO)
    ..a<$fixnum.Int64>(2, _omitFieldNames ? '' : 'nsec', $pb.PbFieldType.OU6, defaultOrMaker: $fixnum.Int64.ZERO)
    ..aOS(3, _omitFieldNames ? '' : 'hostname')
    ..aOS(4, _omitFieldNames ? '' : 'netns')
    ..a<$core.int>(5, _omitFieldNames ? '' : 'nsid', $pb.PbFieldType.OU3)
    ..aOS(6, _omitFieldNames ? '' : 'label')
    ..aOS(7, _omitFieldNames ? '' : 'tag')
    ..a<$fixnum.Int64>(8, _omitFieldNames ? '' : 'recordCounter', $pb.PbFieldType.OU6, defaultOrMaker: $fixnum.Int64.ZERO)
    ..a<$fixnum.Int64>(9, _omitFieldNames ? '' : 'socketFd', $pb.PbFieldType.OU6, defaultOrMaker: $fixnum.Int64.ZERO)
    ..a<$fixnum.Int64>(10, _omitFieldNames ? '' : 'netlinkerId', $pb.PbFieldType.OU6, defaultOrMaker: $fixnum.Int64.ZERO)
    ..a<$core.int>(101, _omitFieldNames ? '' : 'inetDiagMsgFamily', $pb.PbFieldType.OU3)
    ..a<$core.int>(102, _omitFieldNames ? '' : 'inetDiagMsgState', $pb.PbFieldType.OU3)
    ..a<$core.int>(103, _omitFieldNames ? '' : 'inetDiagMsgTimer', $pb.PbFieldType.OU3)
    ..a<$core.int>(104, _omitFieldNames ? '' : 'inetDiagMsgRetrans', $pb.PbFieldType.OU3)
    ..a<$core.int>(105, _omitFieldNames ? '' : 'inetDiagMsgSocketSourcePort', $pb.PbFieldType.OU3)
    ..a<$core.int>(106, _omitFieldNames ? '' : 'inetDiagMsgSocketDestinationPort', $pb.PbFieldType.OU3)
    ..a<$core.List<$core.int>>(107, _omitFieldNames ? '' : 'inetDiagMsgSocketSource', $pb.PbFieldType.OY)
    ..a<$core.List<$core.int>>(108, _omitFieldNames ? '' : 'inetDiagMsgSocketDestination', $pb.PbFieldType.OY)
    ..a<$core.int>(109, _omitFieldNames ? '' : 'inetDiagMsgSocketInterface', $pb.PbFieldType.OU3)
    ..a<$fixnum.Int64>(110, _omitFieldNames ? '' : 'inetDiagMsgSocketCookie', $pb.PbFieldType.OU6, defaultOrMaker: $fixnum.Int64.ZERO)
    ..a<$fixnum.Int64>(111, _omitFieldNames ? '' : 'inetDiagMsgSocketDestAsn', $pb.PbFieldType.OU6, defaultOrMaker: $fixnum.Int64.ZERO)
    ..a<$fixnum.Int64>(112, _omitFieldNames ? '' : 'inetDiagMsgSocketNextHopAsn', $pb.PbFieldType.OU6, defaultOrMaker: $fixnum.Int64.ZERO)
    ..a<$core.int>(113, _omitFieldNames ? '' : 'inetDiagMsgExpires', $pb.PbFieldType.OU3)
    ..a<$core.int>(114, _omitFieldNames ? '' : 'inetDiagMsgRqueue', $pb.PbFieldType.OU3)
    ..a<$core.int>(115, _omitFieldNames ? '' : 'inetDiagMsgWqueue', $pb.PbFieldType.OU3)
    ..a<$core.int>(116, _omitFieldNames ? '' : 'inetDiagMsgUid', $pb.PbFieldType.OU3)
    ..a<$core.int>(117, _omitFieldNames ? '' : 'inetDiagMsgInode', $pb.PbFieldType.OU3)
    ..a<$core.int>(201, _omitFieldNames ? '' : 'memInfoRmem', $pb.PbFieldType.OU3)
    ..a<$core.int>(202, _omitFieldNames ? '' : 'memInfoWmem', $pb.PbFieldType.OU3)
    ..a<$core.int>(203, _omitFieldNames ? '' : 'memInfoFmem', $pb.PbFieldType.OU3)
    ..a<$core.int>(204, _omitFieldNames ? '' : 'memInfoTmem', $pb.PbFieldType.OU3)
    ..a<$core.int>(301, _omitFieldNames ? '' : 'tcpInfoState', $pb.PbFieldType.OU3)
    ..a<$core.int>(302, _omitFieldNames ? '' : 'tcpInfoCaState', $pb.PbFieldType.OU3)
    ..a<$core.int>(303, _omitFieldNames ? '' : 'tcpInfoRetransmits', $pb.PbFieldType.OU3)
    ..a<$core.int>(304, _omitFieldNames ? '' : 'tcpInfoProbes', $pb.PbFieldType.OU3)
    ..a<$core.int>(305, _omitFieldNames ? '' : 'tcpInfoBackoff', $pb.PbFieldType.OU3)
    ..a<$core.int>(306, _omitFieldNames ? '' : 'tcpInfoOptions', $pb.PbFieldType.OU3)
    ..a<$core.int>(307, _omitFieldNames ? '' : 'tcpInfoSendScale', $pb.PbFieldType.OU3)
    ..a<$core.int>(308, _omitFieldNames ? '' : 'tcpInfoRcvScale', $pb.PbFieldType.OU3)
    ..a<$core.int>(309, _omitFieldNames ? '' : 'tcpInfoDeliveryRateAppLimited', $pb.PbFieldType.OU3)
    ..a<$core.int>(310, _omitFieldNames ? '' : 'tcpInfoFastOpenClientFailed', $pb.PbFieldType.OU3)
    ..a<$core.int>(315, _omitFieldNames ? '' : 'tcpInfoRto', $pb.PbFieldType.OU3)
    ..a<$core.int>(316, _omitFieldNames ? '' : 'tcpInfoAto', $pb.PbFieldType.OU3)
    ..a<$core.int>(317, _omitFieldNames ? '' : 'tcpInfoSndMss', $pb.PbFieldType.OU3)
    ..a<$core.int>(318, _omitFieldNames ? '' : 'tcpInfoRcvMss', $pb.PbFieldType.OU3)
    ..a<$core.int>(319, _omitFieldNames ? '' : 'tcpInfoUnacked', $pb.PbFieldType.OU3)
    ..a<$core.int>(320, _omitFieldNames ? '' : 'tcpInfoSacked', $pb.PbFieldType.OU3)
    ..a<$core.int>(321, _omitFieldNames ? '' : 'tcpInfoLost', $pb.PbFieldType.OU3)
    ..a<$core.int>(322, _omitFieldNames ? '' : 'tcpInfoRetrans', $pb.PbFieldType.OU3)
    ..a<$core.int>(323, _omitFieldNames ? '' : 'tcpInfoFackets', $pb.PbFieldType.OU3)
    ..a<$core.int>(324, _omitFieldNames ? '' : 'tcpInfoLastDataSent', $pb.PbFieldType.OU3)
    ..a<$core.int>(325, _omitFieldNames ? '' : 'tcpInfoLastAckSent', $pb.PbFieldType.OU3)
    ..a<$core.int>(326, _omitFieldNames ? '' : 'tcpInfoLastDataRecv', $pb.PbFieldType.OU3)
    ..a<$core.int>(327, _omitFieldNames ? '' : 'tcpInfoLastAckRecv', $pb.PbFieldType.OU3)
    ..a<$core.int>(328, _omitFieldNames ? '' : 'tcpInfoPmtu', $pb.PbFieldType.OU3)
    ..a<$core.int>(329, _omitFieldNames ? '' : 'tcpInfoRcvSsthresh', $pb.PbFieldType.OU3)
    ..a<$core.int>(330, _omitFieldNames ? '' : 'tcpInfoRtt', $pb.PbFieldType.OU3)
    ..a<$core.int>(331, _omitFieldNames ? '' : 'tcpInfoRttVar', $pb.PbFieldType.OU3)
    ..a<$core.int>(332, _omitFieldNames ? '' : 'tcpInfoSndSsthresh', $pb.PbFieldType.OU3)
    ..a<$core.int>(333, _omitFieldNames ? '' : 'tcpInfoSndCwnd', $pb.PbFieldType.OU3)
    ..a<$core.int>(334, _omitFieldNames ? '' : 'tcpInfoAdvMss', $pb.PbFieldType.OU3)
    ..a<$core.int>(335, _omitFieldNames ? '' : 'tcpInfoReordering', $pb.PbFieldType.OU3)
    ..a<$core.int>(336, _omitFieldNames ? '' : 'tcpInfoRcvRtt', $pb.PbFieldType.OU3)
    ..a<$core.int>(337, _omitFieldNames ? '' : 'tcpInfoRcvSpace', $pb.PbFieldType.OU3)
    ..a<$core.int>(338, _omitFieldNames ? '' : 'tcpInfoTotalRetrans', $pb.PbFieldType.OU3)
    ..a<$fixnum.Int64>(339, _omitFieldNames ? '' : 'tcpInfoPacingRate', $pb.PbFieldType.OU6, defaultOrMaker: $fixnum.Int64.ZERO)
    ..a<$fixnum.Int64>(340, _omitFieldNames ? '' : 'tcpInfoMaxPacingRate', $pb.PbFieldType.OU6, defaultOrMaker: $fixnum.Int64.ZERO)
    ..a<$fixnum.Int64>(341, _omitFieldNames ? '' : 'tcpInfoBytesAcked', $pb.PbFieldType.OU6, defaultOrMaker: $fixnum.Int64.ZERO)
    ..a<$fixnum.Int64>(342, _omitFieldNames ? '' : 'tcpInfoBytesReceived', $pb.PbFieldType.OU6, defaultOrMaker: $fixnum.Int64.ZERO)
    ..a<$core.int>(343, _omitFieldNames ? '' : 'tcpInfoSegsOut', $pb.PbFieldType.OU3)
    ..a<$core.int>(344, _omitFieldNames ? '' : 'tcpInfoSegsIn', $pb.PbFieldType.OU3)
    ..a<$core.int>(345, _omitFieldNames ? '' : 'tcpInfoNotSentBytes', $pb.PbFieldType.OU3)
    ..a<$core.int>(346, _omitFieldNames ? '' : 'tcpInfoMinRtt', $pb.PbFieldType.OU3)
    ..a<$core.int>(347, _omitFieldNames ? '' : 'tcpInfoDataSegsIn', $pb.PbFieldType.OU3)
    ..a<$core.int>(348, _omitFieldNames ? '' : 'tcpInfoDataSegsOut', $pb.PbFieldType.OU3)
    ..a<$fixnum.Int64>(349, _omitFieldNames ? '' : 'tcpInfoDeliveryRate', $pb.PbFieldType.OU6, defaultOrMaker: $fixnum.Int64.ZERO)
    ..a<$fixnum.Int64>(350, _omitFieldNames ? '' : 'tcpInfoBusyTime', $pb.PbFieldType.OU6, defaultOrMaker: $fixnum.Int64.ZERO)
    ..a<$fixnum.Int64>(351, _omitFieldNames ? '' : 'tcpInfoRwndLimited', $pb.PbFieldType.OU6, defaultOrMaker: $fixnum.Int64.ZERO)
    ..a<$fixnum.Int64>(352, _omitFieldNames ? '' : 'tcpInfoSndbufLimited', $pb.PbFieldType.OU6, defaultOrMaker: $fixnum.Int64.ZERO)
    ..a<$core.int>(353, _omitFieldNames ? '' : 'tcpInfoDelivered', $pb.PbFieldType.OU3)
    ..a<$core.int>(354, _omitFieldNames ? '' : 'tcpInfoDeliveredCe', $pb.PbFieldType.OU3)
    ..a<$fixnum.Int64>(355, _omitFieldNames ? '' : 'tcpInfoBytesSent', $pb.PbFieldType.OU6, defaultOrMaker: $fixnum.Int64.ZERO)
    ..a<$fixnum.Int64>(356, _omitFieldNames ? '' : 'tcpInfoBytesRetrans', $pb.PbFieldType.OU6, defaultOrMaker: $fixnum.Int64.ZERO)
    ..a<$core.int>(357, _omitFieldNames ? '' : 'tcpInfoDsackDups', $pb.PbFieldType.OU3)
    ..a<$core.int>(358, _omitFieldNames ? '' : 'tcpInfoReordSeen', $pb.PbFieldType.OU3)
    ..a<$core.int>(359, _omitFieldNames ? '' : 'tcpInfoRcvOoopack', $pb.PbFieldType.OU3)
    ..a<$core.int>(360, _omitFieldNames ? '' : 'tcpInfoSndWnd', $pb.PbFieldType.OU3)
    ..a<$core.int>(361, _omitFieldNames ? '' : 'tcpInfoRcvWnd', $pb.PbFieldType.OU3)
    ..a<$core.int>(362, _omitFieldNames ? '' : 'tcpInfoRehash', $pb.PbFieldType.OU3)
    ..a<$core.int>(363, _omitFieldNames ? '' : 'tcpInfoTotalRto', $pb.PbFieldType.OU3)
    ..a<$core.int>(364, _omitFieldNames ? '' : 'tcpInfoTotalRtoRecoveries', $pb.PbFieldType.OU3)
    ..a<$core.int>(365, _omitFieldNames ? '' : 'tcpInfoTotalRtoTime', $pb.PbFieldType.OU3)
    ..aOS(400, _omitFieldNames ? '' : 'congestionAlgorithmString')
    ..e<XtcpFlatRecord_CongestionAlgorithm>(401, _omitFieldNames ? '' : 'congestionAlgorithmEnum', $pb.PbFieldType.OE, defaultOrMaker: XtcpFlatRecord_CongestionAlgorithm.CONGESTION_ALGORITHM_UNSPECIFIED, valueOf: XtcpFlatRecord_CongestionAlgorithm.valueOf, enumValues: XtcpFlatRecord_CongestionAlgorithm.values)
    ..a<$core.int>(501, _omitFieldNames ? '' : 'typeOfService', $pb.PbFieldType.OU3)
    ..a<$core.int>(502, _omitFieldNames ? '' : 'trafficClass', $pb.PbFieldType.OU3)
    ..a<$core.int>(601, _omitFieldNames ? '' : 'skMemInfoRmemAlloc', $pb.PbFieldType.OU3)
    ..a<$core.int>(602, _omitFieldNames ? '' : 'skMemInfoRcvBuf', $pb.PbFieldType.OU3)
    ..a<$core.int>(603, _omitFieldNames ? '' : 'skMemInfoWmemAlloc', $pb.PbFieldType.OU3)
    ..a<$core.int>(604, _omitFieldNames ? '' : 'skMemInfoSndBuf', $pb.PbFieldType.OU3)
    ..a<$core.int>(605, _omitFieldNames ? '' : 'skMemInfoFwdAlloc', $pb.PbFieldType.OU3)
    ..a<$core.int>(606, _omitFieldNames ? '' : 'skMemInfoWmemQueued', $pb.PbFieldType.OU3)
    ..a<$core.int>(607, _omitFieldNames ? '' : 'skMemInfoOptmem', $pb.PbFieldType.OU3)
    ..a<$core.int>(608, _omitFieldNames ? '' : 'skMemInfoBacklog', $pb.PbFieldType.OU3)
    ..a<$core.int>(609, _omitFieldNames ? '' : 'skMemInfoDrops', $pb.PbFieldType.OU3)
    ..a<$core.int>(700, _omitFieldNames ? '' : 'shutdownState', $pb.PbFieldType.OU3)
    ..a<$core.int>(801, _omitFieldNames ? '' : 'vegasInfoEnabled', $pb.PbFieldType.OU3)
    ..a<$core.int>(802, _omitFieldNames ? '' : 'vegasInfoRttCnt', $pb.PbFieldType.OU3)
    ..a<$core.int>(803, _omitFieldNames ? '' : 'vegasInfoRtt', $pb.PbFieldType.OU3)
    ..a<$core.int>(804, _omitFieldNames ? '' : 'vegasInfoMinRtt', $pb.PbFieldType.OU3)
    ..a<$core.int>(901, _omitFieldNames ? '' : 'dctcpInfoEnabled', $pb.PbFieldType.OU3)
    ..a<$core.int>(902, _omitFieldNames ? '' : 'dctcpInfoCeState', $pb.PbFieldType.OU3)
    ..a<$core.int>(903, _omitFieldNames ? '' : 'dctcpInfoAlpha', $pb.PbFieldType.OU3)
    ..a<$core.int>(904, _omitFieldNames ? '' : 'dctcpInfoAbEcn', $pb.PbFieldType.OU3)
    ..a<$core.int>(905, _omitFieldNames ? '' : 'dctcpInfoAbTot', $pb.PbFieldType.OU3)
    ..a<$core.int>(1001, _omitFieldNames ? '' : 'bbrInfoBwLo', $pb.PbFieldType.OU3)
    ..a<$core.int>(1002, _omitFieldNames ? '' : 'bbrInfoBwHi', $pb.PbFieldType.OU3)
    ..a<$core.int>(1003, _omitFieldNames ? '' : 'bbrInfoMinRtt', $pb.PbFieldType.OU3)
    ..a<$core.int>(1004, _omitFieldNames ? '' : 'bbrInfoPacingGain', $pb.PbFieldType.OU3)
    ..a<$core.int>(1005, _omitFieldNames ? '' : 'bbrInfoCwndGain', $pb.PbFieldType.OU3)
    ..a<$core.int>(1101, _omitFieldNames ? '' : 'classId', $pb.PbFieldType.OU3)
    ..a<$core.int>(1102, _omitFieldNames ? '' : 'sockOpt', $pb.PbFieldType.OU3)
    ..a<$fixnum.Int64>(1203, _omitFieldNames ? '' : 'cGroup', $pb.PbFieldType.OU6, defaultOrMaker: $fixnum.Int64.ZERO)
    ..hasRequiredFields = false
  ;

  @$core.Deprecated(
  'Using this can add significant overhead to your binary. '
  'Use [GeneratedMessageGenericExtensions.deepCopy] instead. '
  'Will be removed in next major version')
  XtcpFlatRecord clone() => XtcpFlatRecord()..mergeFromMessage(this);
  @$core.Deprecated(
  'Using this can add significant overhead to your binary. '
  'Use [GeneratedMessageGenericExtensions.rebuild] instead. '
  'Will be removed in next major version')
  XtcpFlatRecord copyWith(void Function(XtcpFlatRecord) updates) => super.copyWith((message) => updates(message as XtcpFlatRecord)) as XtcpFlatRecord;

  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static XtcpFlatRecord create() => XtcpFlatRecord._();
  XtcpFlatRecord createEmptyInstance() => create();
  static $pb.PbList<XtcpFlatRecord> createRepeated() => $pb.PbList<XtcpFlatRecord>();
  @$core.pragma('dart2js:noInline')
  static XtcpFlatRecord getDefault() => _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<XtcpFlatRecord>(create);
  static XtcpFlatRecord? _defaultInstance;

  /// message xtcp_flat_record {
  @$pb.TagNumber(1)
  $fixnum.Int64 get sec => $_getI64(0);
  @$pb.TagNumber(1)
  set sec($fixnum.Int64 v) { $_setInt64(0, v); }
  @$pb.TagNumber(1)
  $core.bool hasSec() => $_has(0);
  @$pb.TagNumber(1)
  void clearSec() => clearField(1);

  @$pb.TagNumber(2)
  $fixnum.Int64 get nsec => $_getI64(1);
  @$pb.TagNumber(2)
  set nsec($fixnum.Int64 v) { $_setInt64(1, v); }
  @$pb.TagNumber(2)
  $core.bool hasNsec() => $_has(1);
  @$pb.TagNumber(2)
  void clearNsec() => clearField(2);

  @$pb.TagNumber(3)
  $core.String get hostname => $_getSZ(2);
  @$pb.TagNumber(3)
  set hostname($core.String v) { $_setString(2, v); }
  @$pb.TagNumber(3)
  $core.bool hasHostname() => $_has(2);
  @$pb.TagNumber(3)
  void clearHostname() => clearField(3);

  /// network namespace
  @$pb.TagNumber(4)
  $core.String get netns => $_getSZ(3);
  @$pb.TagNumber(4)
  set netns($core.String v) { $_setString(3, v); }
  @$pb.TagNumber(4)
  $core.bool hasNetns() => $_has(3);
  @$pb.TagNumber(4)
  void clearNetns() => clearField(4);

  /// network namespace id
  /// TODO xtcp does not currently get the id
  @$pb.TagNumber(5)
  $core.int get nsid => $_getIZ(4);
  @$pb.TagNumber(5)
  set nsid($core.int v) { $_setUnsignedInt32(4, v); }
  @$pb.TagNumber(5)
  $core.bool hasNsid() => $_has(4);
  @$pb.TagNumber(5)
  void clearNsid() => clearField(5);

  /// free form string
  @$pb.TagNumber(6)
  $core.String get label => $_getSZ(5);
  @$pb.TagNumber(6)
  set label($core.String v) { $_setString(5, v); }
  @$pb.TagNumber(6)
  $core.bool hasLabel() => $_has(5);
  @$pb.TagNumber(6)
  void clearLabel() => clearField(6);

  /// free form string
  @$pb.TagNumber(7)
  $core.String get tag => $_getSZ(6);
  @$pb.TagNumber(7)
  set tag($core.String v) { $_setString(6, v); }
  @$pb.TagNumber(7)
  $core.bool hasTag() => $_has(6);
  @$pb.TagNumber(7)
  void clearTag() => clearField(7);

  @$pb.TagNumber(8)
  $fixnum.Int64 get recordCounter => $_getI64(7);
  @$pb.TagNumber(8)
  set recordCounter($fixnum.Int64 v) { $_setInt64(7, v); }
  @$pb.TagNumber(8)
  $core.bool hasRecordCounter() => $_has(7);
  @$pb.TagNumber(8)
  void clearRecordCounter() => clearField(8);

  @$pb.TagNumber(9)
  $fixnum.Int64 get socketFd => $_getI64(8);
  @$pb.TagNumber(9)
  set socketFd($fixnum.Int64 v) { $_setInt64(8, v); }
  @$pb.TagNumber(9)
  $core.bool hasSocketFd() => $_has(8);
  @$pb.TagNumber(9)
  void clearSocketFd() => clearField(9);

  @$pb.TagNumber(10)
  $fixnum.Int64 get netlinkerId => $_getI64(9);
  @$pb.TagNumber(10)
  set netlinkerId($fixnum.Int64 v) { $_setInt64(9, v); }
  @$pb.TagNumber(10)
  $core.bool hasNetlinkerId() => $_has(9);
  @$pb.TagNumber(10)
  void clearNetlinkerId() => clearField(10);

  @$pb.TagNumber(101)
  $core.int get inetDiagMsgFamily => $_getIZ(10);
  @$pb.TagNumber(101)
  set inetDiagMsgFamily($core.int v) { $_setUnsignedInt32(10, v); }
  @$pb.TagNumber(101)
  $core.bool hasInetDiagMsgFamily() => $_has(10);
  @$pb.TagNumber(101)
  void clearInetDiagMsgFamily() => clearField(101);

  @$pb.TagNumber(102)
  $core.int get inetDiagMsgState => $_getIZ(11);
  @$pb.TagNumber(102)
  set inetDiagMsgState($core.int v) { $_setUnsignedInt32(11, v); }
  @$pb.TagNumber(102)
  $core.bool hasInetDiagMsgState() => $_has(11);
  @$pb.TagNumber(102)
  void clearInetDiagMsgState() => clearField(102);

  @$pb.TagNumber(103)
  $core.int get inetDiagMsgTimer => $_getIZ(12);
  @$pb.TagNumber(103)
  set inetDiagMsgTimer($core.int v) { $_setUnsignedInt32(12, v); }
  @$pb.TagNumber(103)
  $core.bool hasInetDiagMsgTimer() => $_has(12);
  @$pb.TagNumber(103)
  void clearInetDiagMsgTimer() => clearField(103);

  @$pb.TagNumber(104)
  $core.int get inetDiagMsgRetrans => $_getIZ(13);
  @$pb.TagNumber(104)
  set inetDiagMsgRetrans($core.int v) { $_setUnsignedInt32(13, v); }
  @$pb.TagNumber(104)
  $core.bool hasInetDiagMsgRetrans() => $_has(13);
  @$pb.TagNumber(104)
  void clearInetDiagMsgRetrans() => clearField(104);

  @$pb.TagNumber(105)
  $core.int get inetDiagMsgSocketSourcePort => $_getIZ(14);
  @$pb.TagNumber(105)
  set inetDiagMsgSocketSourcePort($core.int v) { $_setUnsignedInt32(14, v); }
  @$pb.TagNumber(105)
  $core.bool hasInetDiagMsgSocketSourcePort() => $_has(14);
  @$pb.TagNumber(105)
  void clearInetDiagMsgSocketSourcePort() => clearField(105);

  @$pb.TagNumber(106)
  $core.int get inetDiagMsgSocketDestinationPort => $_getIZ(15);
  @$pb.TagNumber(106)
  set inetDiagMsgSocketDestinationPort($core.int v) { $_setUnsignedInt32(15, v); }
  @$pb.TagNumber(106)
  $core.bool hasInetDiagMsgSocketDestinationPort() => $_has(15);
  @$pb.TagNumber(106)
  void clearInetDiagMsgSocketDestinationPort() => clearField(106);

  @$pb.TagNumber(107)
  $core.List<$core.int> get inetDiagMsgSocketSource => $_getN(16);
  @$pb.TagNumber(107)
  set inetDiagMsgSocketSource($core.List<$core.int> v) { $_setBytes(16, v); }
  @$pb.TagNumber(107)
  $core.bool hasInetDiagMsgSocketSource() => $_has(16);
  @$pb.TagNumber(107)
  void clearInetDiagMsgSocketSource() => clearField(107);

  @$pb.TagNumber(108)
  $core.List<$core.int> get inetDiagMsgSocketDestination => $_getN(17);
  @$pb.TagNumber(108)
  set inetDiagMsgSocketDestination($core.List<$core.int> v) { $_setBytes(17, v); }
  @$pb.TagNumber(108)
  $core.bool hasInetDiagMsgSocketDestination() => $_has(17);
  @$pb.TagNumber(108)
  void clearInetDiagMsgSocketDestination() => clearField(108);

  @$pb.TagNumber(109)
  $core.int get inetDiagMsgSocketInterface => $_getIZ(18);
  @$pb.TagNumber(109)
  set inetDiagMsgSocketInterface($core.int v) { $_setUnsignedInt32(18, v); }
  @$pb.TagNumber(109)
  $core.bool hasInetDiagMsgSocketInterface() => $_has(18);
  @$pb.TagNumber(109)
  void clearInetDiagMsgSocketInterface() => clearField(109);

  @$pb.TagNumber(110)
  $fixnum.Int64 get inetDiagMsgSocketCookie => $_getI64(19);
  @$pb.TagNumber(110)
  set inetDiagMsgSocketCookie($fixnum.Int64 v) { $_setInt64(19, v); }
  @$pb.TagNumber(110)
  $core.bool hasInetDiagMsgSocketCookie() => $_has(19);
  @$pb.TagNumber(110)
  void clearInetDiagMsgSocketCookie() => clearField(110);

  @$pb.TagNumber(111)
  $fixnum.Int64 get inetDiagMsgSocketDestAsn => $_getI64(20);
  @$pb.TagNumber(111)
  set inetDiagMsgSocketDestAsn($fixnum.Int64 v) { $_setInt64(20, v); }
  @$pb.TagNumber(111)
  $core.bool hasInetDiagMsgSocketDestAsn() => $_has(20);
  @$pb.TagNumber(111)
  void clearInetDiagMsgSocketDestAsn() => clearField(111);

  @$pb.TagNumber(112)
  $fixnum.Int64 get inetDiagMsgSocketNextHopAsn => $_getI64(21);
  @$pb.TagNumber(112)
  set inetDiagMsgSocketNextHopAsn($fixnum.Int64 v) { $_setInt64(21, v); }
  @$pb.TagNumber(112)
  $core.bool hasInetDiagMsgSocketNextHopAsn() => $_has(21);
  @$pb.TagNumber(112)
  void clearInetDiagMsgSocketNextHopAsn() => clearField(112);

  @$pb.TagNumber(113)
  $core.int get inetDiagMsgExpires => $_getIZ(22);
  @$pb.TagNumber(113)
  set inetDiagMsgExpires($core.int v) { $_setUnsignedInt32(22, v); }
  @$pb.TagNumber(113)
  $core.bool hasInetDiagMsgExpires() => $_has(22);
  @$pb.TagNumber(113)
  void clearInetDiagMsgExpires() => clearField(113);

  @$pb.TagNumber(114)
  $core.int get inetDiagMsgRqueue => $_getIZ(23);
  @$pb.TagNumber(114)
  set inetDiagMsgRqueue($core.int v) { $_setUnsignedInt32(23, v); }
  @$pb.TagNumber(114)
  $core.bool hasInetDiagMsgRqueue() => $_has(23);
  @$pb.TagNumber(114)
  void clearInetDiagMsgRqueue() => clearField(114);

  @$pb.TagNumber(115)
  $core.int get inetDiagMsgWqueue => $_getIZ(24);
  @$pb.TagNumber(115)
  set inetDiagMsgWqueue($core.int v) { $_setUnsignedInt32(24, v); }
  @$pb.TagNumber(115)
  $core.bool hasInetDiagMsgWqueue() => $_has(24);
  @$pb.TagNumber(115)
  void clearInetDiagMsgWqueue() => clearField(115);

  @$pb.TagNumber(116)
  $core.int get inetDiagMsgUid => $_getIZ(25);
  @$pb.TagNumber(116)
  set inetDiagMsgUid($core.int v) { $_setUnsignedInt32(25, v); }
  @$pb.TagNumber(116)
  $core.bool hasInetDiagMsgUid() => $_has(25);
  @$pb.TagNumber(116)
  void clearInetDiagMsgUid() => clearField(116);

  @$pb.TagNumber(117)
  $core.int get inetDiagMsgInode => $_getIZ(26);
  @$pb.TagNumber(117)
  set inetDiagMsgInode($core.int v) { $_setUnsignedInt32(26, v); }
  @$pb.TagNumber(117)
  $core.bool hasInetDiagMsgInode() => $_has(26);
  @$pb.TagNumber(117)
  void clearInetDiagMsgInode() => clearField(117);

  @$pb.TagNumber(201)
  $core.int get memInfoRmem => $_getIZ(27);
  @$pb.TagNumber(201)
  set memInfoRmem($core.int v) { $_setUnsignedInt32(27, v); }
  @$pb.TagNumber(201)
  $core.bool hasMemInfoRmem() => $_has(27);
  @$pb.TagNumber(201)
  void clearMemInfoRmem() => clearField(201);

  @$pb.TagNumber(202)
  $core.int get memInfoWmem => $_getIZ(28);
  @$pb.TagNumber(202)
  set memInfoWmem($core.int v) { $_setUnsignedInt32(28, v); }
  @$pb.TagNumber(202)
  $core.bool hasMemInfoWmem() => $_has(28);
  @$pb.TagNumber(202)
  void clearMemInfoWmem() => clearField(202);

  @$pb.TagNumber(203)
  $core.int get memInfoFmem => $_getIZ(29);
  @$pb.TagNumber(203)
  set memInfoFmem($core.int v) { $_setUnsignedInt32(29, v); }
  @$pb.TagNumber(203)
  $core.bool hasMemInfoFmem() => $_has(29);
  @$pb.TagNumber(203)
  void clearMemInfoFmem() => clearField(203);

  @$pb.TagNumber(204)
  $core.int get memInfoTmem => $_getIZ(30);
  @$pb.TagNumber(204)
  set memInfoTmem($core.int v) { $_setUnsignedInt32(30, v); }
  @$pb.TagNumber(204)
  $core.bool hasMemInfoTmem() => $_has(30);
  @$pb.TagNumber(204)
  void clearMemInfoTmem() => clearField(204);

  @$pb.TagNumber(301)
  $core.int get tcpInfoState => $_getIZ(31);
  @$pb.TagNumber(301)
  set tcpInfoState($core.int v) { $_setUnsignedInt32(31, v); }
  @$pb.TagNumber(301)
  $core.bool hasTcpInfoState() => $_has(31);
  @$pb.TagNumber(301)
  void clearTcpInfoState() => clearField(301);

  @$pb.TagNumber(302)
  $core.int get tcpInfoCaState => $_getIZ(32);
  @$pb.TagNumber(302)
  set tcpInfoCaState($core.int v) { $_setUnsignedInt32(32, v); }
  @$pb.TagNumber(302)
  $core.bool hasTcpInfoCaState() => $_has(32);
  @$pb.TagNumber(302)
  void clearTcpInfoCaState() => clearField(302);

  @$pb.TagNumber(303)
  $core.int get tcpInfoRetransmits => $_getIZ(33);
  @$pb.TagNumber(303)
  set tcpInfoRetransmits($core.int v) { $_setUnsignedInt32(33, v); }
  @$pb.TagNumber(303)
  $core.bool hasTcpInfoRetransmits() => $_has(33);
  @$pb.TagNumber(303)
  void clearTcpInfoRetransmits() => clearField(303);

  @$pb.TagNumber(304)
  $core.int get tcpInfoProbes => $_getIZ(34);
  @$pb.TagNumber(304)
  set tcpInfoProbes($core.int v) { $_setUnsignedInt32(34, v); }
  @$pb.TagNumber(304)
  $core.bool hasTcpInfoProbes() => $_has(34);
  @$pb.TagNumber(304)
  void clearTcpInfoProbes() => clearField(304);

  @$pb.TagNumber(305)
  $core.int get tcpInfoBackoff => $_getIZ(35);
  @$pb.TagNumber(305)
  set tcpInfoBackoff($core.int v) { $_setUnsignedInt32(35, v); }
  @$pb.TagNumber(305)
  $core.bool hasTcpInfoBackoff() => $_has(35);
  @$pb.TagNumber(305)
  void clearTcpInfoBackoff() => clearField(305);

  @$pb.TagNumber(306)
  $core.int get tcpInfoOptions => $_getIZ(36);
  @$pb.TagNumber(306)
  set tcpInfoOptions($core.int v) { $_setUnsignedInt32(36, v); }
  @$pb.TagNumber(306)
  $core.bool hasTcpInfoOptions() => $_has(36);
  @$pb.TagNumber(306)
  void clearTcpInfoOptions() => clearField(306);

  /// 	__u8	_snd_wscale : 4, _rcv_wscale : 4;
  /// 	__u8	_delivery_rate_app_limited:1, _fastopen_client_fail:2;
  @$pb.TagNumber(307)
  $core.int get tcpInfoSendScale => $_getIZ(37);
  @$pb.TagNumber(307)
  set tcpInfoSendScale($core.int v) { $_setUnsignedInt32(37, v); }
  @$pb.TagNumber(307)
  $core.bool hasTcpInfoSendScale() => $_has(37);
  @$pb.TagNumber(307)
  void clearTcpInfoSendScale() => clearField(307);

  @$pb.TagNumber(308)
  $core.int get tcpInfoRcvScale => $_getIZ(38);
  @$pb.TagNumber(308)
  set tcpInfoRcvScale($core.int v) { $_setUnsignedInt32(38, v); }
  @$pb.TagNumber(308)
  $core.bool hasTcpInfoRcvScale() => $_has(38);
  @$pb.TagNumber(308)
  void clearTcpInfoRcvScale() => clearField(308);

  @$pb.TagNumber(309)
  $core.int get tcpInfoDeliveryRateAppLimited => $_getIZ(39);
  @$pb.TagNumber(309)
  set tcpInfoDeliveryRateAppLimited($core.int v) { $_setUnsignedInt32(39, v); }
  @$pb.TagNumber(309)
  $core.bool hasTcpInfoDeliveryRateAppLimited() => $_has(39);
  @$pb.TagNumber(309)
  void clearTcpInfoDeliveryRateAppLimited() => clearField(309);

  @$pb.TagNumber(310)
  $core.int get tcpInfoFastOpenClientFailed => $_getIZ(40);
  @$pb.TagNumber(310)
  set tcpInfoFastOpenClientFailed($core.int v) { $_setUnsignedInt32(40, v); }
  @$pb.TagNumber(310)
  $core.bool hasTcpInfoFastOpenClientFailed() => $_has(40);
  @$pb.TagNumber(310)
  void clearTcpInfoFastOpenClientFailed() => clearField(310);

  @$pb.TagNumber(315)
  $core.int get tcpInfoRto => $_getIZ(41);
  @$pb.TagNumber(315)
  set tcpInfoRto($core.int v) { $_setUnsignedInt32(41, v); }
  @$pb.TagNumber(315)
  $core.bool hasTcpInfoRto() => $_has(41);
  @$pb.TagNumber(315)
  void clearTcpInfoRto() => clearField(315);

  @$pb.TagNumber(316)
  $core.int get tcpInfoAto => $_getIZ(42);
  @$pb.TagNumber(316)
  set tcpInfoAto($core.int v) { $_setUnsignedInt32(42, v); }
  @$pb.TagNumber(316)
  $core.bool hasTcpInfoAto() => $_has(42);
  @$pb.TagNumber(316)
  void clearTcpInfoAto() => clearField(316);

  @$pb.TagNumber(317)
  $core.int get tcpInfoSndMss => $_getIZ(43);
  @$pb.TagNumber(317)
  set tcpInfoSndMss($core.int v) { $_setUnsignedInt32(43, v); }
  @$pb.TagNumber(317)
  $core.bool hasTcpInfoSndMss() => $_has(43);
  @$pb.TagNumber(317)
  void clearTcpInfoSndMss() => clearField(317);

  @$pb.TagNumber(318)
  $core.int get tcpInfoRcvMss => $_getIZ(44);
  @$pb.TagNumber(318)
  set tcpInfoRcvMss($core.int v) { $_setUnsignedInt32(44, v); }
  @$pb.TagNumber(318)
  $core.bool hasTcpInfoRcvMss() => $_has(44);
  @$pb.TagNumber(318)
  void clearTcpInfoRcvMss() => clearField(318);

  @$pb.TagNumber(319)
  $core.int get tcpInfoUnacked => $_getIZ(45);
  @$pb.TagNumber(319)
  set tcpInfoUnacked($core.int v) { $_setUnsignedInt32(45, v); }
  @$pb.TagNumber(319)
  $core.bool hasTcpInfoUnacked() => $_has(45);
  @$pb.TagNumber(319)
  void clearTcpInfoUnacked() => clearField(319);

  @$pb.TagNumber(320)
  $core.int get tcpInfoSacked => $_getIZ(46);
  @$pb.TagNumber(320)
  set tcpInfoSacked($core.int v) { $_setUnsignedInt32(46, v); }
  @$pb.TagNumber(320)
  $core.bool hasTcpInfoSacked() => $_has(46);
  @$pb.TagNumber(320)
  void clearTcpInfoSacked() => clearField(320);

  @$pb.TagNumber(321)
  $core.int get tcpInfoLost => $_getIZ(47);
  @$pb.TagNumber(321)
  set tcpInfoLost($core.int v) { $_setUnsignedInt32(47, v); }
  @$pb.TagNumber(321)
  $core.bool hasTcpInfoLost() => $_has(47);
  @$pb.TagNumber(321)
  void clearTcpInfoLost() => clearField(321);

  @$pb.TagNumber(322)
  $core.int get tcpInfoRetrans => $_getIZ(48);
  @$pb.TagNumber(322)
  set tcpInfoRetrans($core.int v) { $_setUnsignedInt32(48, v); }
  @$pb.TagNumber(322)
  $core.bool hasTcpInfoRetrans() => $_has(48);
  @$pb.TagNumber(322)
  void clearTcpInfoRetrans() => clearField(322);

  @$pb.TagNumber(323)
  $core.int get tcpInfoFackets => $_getIZ(49);
  @$pb.TagNumber(323)
  set tcpInfoFackets($core.int v) { $_setUnsignedInt32(49, v); }
  @$pb.TagNumber(323)
  $core.bool hasTcpInfoFackets() => $_has(49);
  @$pb.TagNumber(323)
  void clearTcpInfoFackets() => clearField(323);

  /// Times
  @$pb.TagNumber(324)
  $core.int get tcpInfoLastDataSent => $_getIZ(50);
  @$pb.TagNumber(324)
  set tcpInfoLastDataSent($core.int v) { $_setUnsignedInt32(50, v); }
  @$pb.TagNumber(324)
  $core.bool hasTcpInfoLastDataSent() => $_has(50);
  @$pb.TagNumber(324)
  void clearTcpInfoLastDataSent() => clearField(324);

  @$pb.TagNumber(325)
  $core.int get tcpInfoLastAckSent => $_getIZ(51);
  @$pb.TagNumber(325)
  set tcpInfoLastAckSent($core.int v) { $_setUnsignedInt32(51, v); }
  @$pb.TagNumber(325)
  $core.bool hasTcpInfoLastAckSent() => $_has(51);
  @$pb.TagNumber(325)
  void clearTcpInfoLastAckSent() => clearField(325);

  @$pb.TagNumber(326)
  $core.int get tcpInfoLastDataRecv => $_getIZ(52);
  @$pb.TagNumber(326)
  set tcpInfoLastDataRecv($core.int v) { $_setUnsignedInt32(52, v); }
  @$pb.TagNumber(326)
  $core.bool hasTcpInfoLastDataRecv() => $_has(52);
  @$pb.TagNumber(326)
  void clearTcpInfoLastDataRecv() => clearField(326);

  @$pb.TagNumber(327)
  $core.int get tcpInfoLastAckRecv => $_getIZ(53);
  @$pb.TagNumber(327)
  set tcpInfoLastAckRecv($core.int v) { $_setUnsignedInt32(53, v); }
  @$pb.TagNumber(327)
  $core.bool hasTcpInfoLastAckRecv() => $_has(53);
  @$pb.TagNumber(327)
  void clearTcpInfoLastAckRecv() => clearField(327);

  /// Metrics
  @$pb.TagNumber(328)
  $core.int get tcpInfoPmtu => $_getIZ(54);
  @$pb.TagNumber(328)
  set tcpInfoPmtu($core.int v) { $_setUnsignedInt32(54, v); }
  @$pb.TagNumber(328)
  $core.bool hasTcpInfoPmtu() => $_has(54);
  @$pb.TagNumber(328)
  void clearTcpInfoPmtu() => clearField(328);

  @$pb.TagNumber(329)
  $core.int get tcpInfoRcvSsthresh => $_getIZ(55);
  @$pb.TagNumber(329)
  set tcpInfoRcvSsthresh($core.int v) { $_setUnsignedInt32(55, v); }
  @$pb.TagNumber(329)
  $core.bool hasTcpInfoRcvSsthresh() => $_has(55);
  @$pb.TagNumber(329)
  void clearTcpInfoRcvSsthresh() => clearField(329);

  @$pb.TagNumber(330)
  $core.int get tcpInfoRtt => $_getIZ(56);
  @$pb.TagNumber(330)
  set tcpInfoRtt($core.int v) { $_setUnsignedInt32(56, v); }
  @$pb.TagNumber(330)
  $core.bool hasTcpInfoRtt() => $_has(56);
  @$pb.TagNumber(330)
  void clearTcpInfoRtt() => clearField(330);

  @$pb.TagNumber(331)
  $core.int get tcpInfoRttVar => $_getIZ(57);
  @$pb.TagNumber(331)
  set tcpInfoRttVar($core.int v) { $_setUnsignedInt32(57, v); }
  @$pb.TagNumber(331)
  $core.bool hasTcpInfoRttVar() => $_has(57);
  @$pb.TagNumber(331)
  void clearTcpInfoRttVar() => clearField(331);

  @$pb.TagNumber(332)
  $core.int get tcpInfoSndSsthresh => $_getIZ(58);
  @$pb.TagNumber(332)
  set tcpInfoSndSsthresh($core.int v) { $_setUnsignedInt32(58, v); }
  @$pb.TagNumber(332)
  $core.bool hasTcpInfoSndSsthresh() => $_has(58);
  @$pb.TagNumber(332)
  void clearTcpInfoSndSsthresh() => clearField(332);

  @$pb.TagNumber(333)
  $core.int get tcpInfoSndCwnd => $_getIZ(59);
  @$pb.TagNumber(333)
  set tcpInfoSndCwnd($core.int v) { $_setUnsignedInt32(59, v); }
  @$pb.TagNumber(333)
  $core.bool hasTcpInfoSndCwnd() => $_has(59);
  @$pb.TagNumber(333)
  void clearTcpInfoSndCwnd() => clearField(333);

  @$pb.TagNumber(334)
  $core.int get tcpInfoAdvMss => $_getIZ(60);
  @$pb.TagNumber(334)
  set tcpInfoAdvMss($core.int v) { $_setUnsignedInt32(60, v); }
  @$pb.TagNumber(334)
  $core.bool hasTcpInfoAdvMss() => $_has(60);
  @$pb.TagNumber(334)
  void clearTcpInfoAdvMss() => clearField(334);

  @$pb.TagNumber(335)
  $core.int get tcpInfoReordering => $_getIZ(61);
  @$pb.TagNumber(335)
  set tcpInfoReordering($core.int v) { $_setUnsignedInt32(61, v); }
  @$pb.TagNumber(335)
  $core.bool hasTcpInfoReordering() => $_has(61);
  @$pb.TagNumber(335)
  void clearTcpInfoReordering() => clearField(335);

  @$pb.TagNumber(336)
  $core.int get tcpInfoRcvRtt => $_getIZ(62);
  @$pb.TagNumber(336)
  set tcpInfoRcvRtt($core.int v) { $_setUnsignedInt32(62, v); }
  @$pb.TagNumber(336)
  $core.bool hasTcpInfoRcvRtt() => $_has(62);
  @$pb.TagNumber(336)
  void clearTcpInfoRcvRtt() => clearField(336);

  @$pb.TagNumber(337)
  $core.int get tcpInfoRcvSpace => $_getIZ(63);
  @$pb.TagNumber(337)
  set tcpInfoRcvSpace($core.int v) { $_setUnsignedInt32(63, v); }
  @$pb.TagNumber(337)
  $core.bool hasTcpInfoRcvSpace() => $_has(63);
  @$pb.TagNumber(337)
  void clearTcpInfoRcvSpace() => clearField(337);

  @$pb.TagNumber(338)
  $core.int get tcpInfoTotalRetrans => $_getIZ(64);
  @$pb.TagNumber(338)
  set tcpInfoTotalRetrans($core.int v) { $_setUnsignedInt32(64, v); }
  @$pb.TagNumber(338)
  $core.bool hasTcpInfoTotalRetrans() => $_has(64);
  @$pb.TagNumber(338)
  void clearTcpInfoTotalRetrans() => clearField(338);

  @$pb.TagNumber(339)
  $fixnum.Int64 get tcpInfoPacingRate => $_getI64(65);
  @$pb.TagNumber(339)
  set tcpInfoPacingRate($fixnum.Int64 v) { $_setInt64(65, v); }
  @$pb.TagNumber(339)
  $core.bool hasTcpInfoPacingRate() => $_has(65);
  @$pb.TagNumber(339)
  void clearTcpInfoPacingRate() => clearField(339);

  @$pb.TagNumber(340)
  $fixnum.Int64 get tcpInfoMaxPacingRate => $_getI64(66);
  @$pb.TagNumber(340)
  set tcpInfoMaxPacingRate($fixnum.Int64 v) { $_setInt64(66, v); }
  @$pb.TagNumber(340)
  $core.bool hasTcpInfoMaxPacingRate() => $_has(66);
  @$pb.TagNumber(340)
  void clearTcpInfoMaxPacingRate() => clearField(340);

  @$pb.TagNumber(341)
  $fixnum.Int64 get tcpInfoBytesAcked => $_getI64(67);
  @$pb.TagNumber(341)
  set tcpInfoBytesAcked($fixnum.Int64 v) { $_setInt64(67, v); }
  @$pb.TagNumber(341)
  $core.bool hasTcpInfoBytesAcked() => $_has(67);
  @$pb.TagNumber(341)
  void clearTcpInfoBytesAcked() => clearField(341);

  @$pb.TagNumber(342)
  $fixnum.Int64 get tcpInfoBytesReceived => $_getI64(68);
  @$pb.TagNumber(342)
  set tcpInfoBytesReceived($fixnum.Int64 v) { $_setInt64(68, v); }
  @$pb.TagNumber(342)
  $core.bool hasTcpInfoBytesReceived() => $_has(68);
  @$pb.TagNumber(342)
  void clearTcpInfoBytesReceived() => clearField(342);

  @$pb.TagNumber(343)
  $core.int get tcpInfoSegsOut => $_getIZ(69);
  @$pb.TagNumber(343)
  set tcpInfoSegsOut($core.int v) { $_setUnsignedInt32(69, v); }
  @$pb.TagNumber(343)
  $core.bool hasTcpInfoSegsOut() => $_has(69);
  @$pb.TagNumber(343)
  void clearTcpInfoSegsOut() => clearField(343);

  @$pb.TagNumber(344)
  $core.int get tcpInfoSegsIn => $_getIZ(70);
  @$pb.TagNumber(344)
  set tcpInfoSegsIn($core.int v) { $_setUnsignedInt32(70, v); }
  @$pb.TagNumber(344)
  $core.bool hasTcpInfoSegsIn() => $_has(70);
  @$pb.TagNumber(344)
  void clearTcpInfoSegsIn() => clearField(344);

  @$pb.TagNumber(345)
  $core.int get tcpInfoNotSentBytes => $_getIZ(71);
  @$pb.TagNumber(345)
  set tcpInfoNotSentBytes($core.int v) { $_setUnsignedInt32(71, v); }
  @$pb.TagNumber(345)
  $core.bool hasTcpInfoNotSentBytes() => $_has(71);
  @$pb.TagNumber(345)
  void clearTcpInfoNotSentBytes() => clearField(345);

  @$pb.TagNumber(346)
  $core.int get tcpInfoMinRtt => $_getIZ(72);
  @$pb.TagNumber(346)
  set tcpInfoMinRtt($core.int v) { $_setUnsignedInt32(72, v); }
  @$pb.TagNumber(346)
  $core.bool hasTcpInfoMinRtt() => $_has(72);
  @$pb.TagNumber(346)
  void clearTcpInfoMinRtt() => clearField(346);

  @$pb.TagNumber(347)
  $core.int get tcpInfoDataSegsIn => $_getIZ(73);
  @$pb.TagNumber(347)
  set tcpInfoDataSegsIn($core.int v) { $_setUnsignedInt32(73, v); }
  @$pb.TagNumber(347)
  $core.bool hasTcpInfoDataSegsIn() => $_has(73);
  @$pb.TagNumber(347)
  void clearTcpInfoDataSegsIn() => clearField(347);

  @$pb.TagNumber(348)
  $core.int get tcpInfoDataSegsOut => $_getIZ(74);
  @$pb.TagNumber(348)
  set tcpInfoDataSegsOut($core.int v) { $_setUnsignedInt32(74, v); }
  @$pb.TagNumber(348)
  $core.bool hasTcpInfoDataSegsOut() => $_has(74);
  @$pb.TagNumber(348)
  void clearTcpInfoDataSegsOut() => clearField(348);

  @$pb.TagNumber(349)
  $fixnum.Int64 get tcpInfoDeliveryRate => $_getI64(75);
  @$pb.TagNumber(349)
  set tcpInfoDeliveryRate($fixnum.Int64 v) { $_setInt64(75, v); }
  @$pb.TagNumber(349)
  $core.bool hasTcpInfoDeliveryRate() => $_has(75);
  @$pb.TagNumber(349)
  void clearTcpInfoDeliveryRate() => clearField(349);

  @$pb.TagNumber(350)
  $fixnum.Int64 get tcpInfoBusyTime => $_getI64(76);
  @$pb.TagNumber(350)
  set tcpInfoBusyTime($fixnum.Int64 v) { $_setInt64(76, v); }
  @$pb.TagNumber(350)
  $core.bool hasTcpInfoBusyTime() => $_has(76);
  @$pb.TagNumber(350)
  void clearTcpInfoBusyTime() => clearField(350);

  @$pb.TagNumber(351)
  $fixnum.Int64 get tcpInfoRwndLimited => $_getI64(77);
  @$pb.TagNumber(351)
  set tcpInfoRwndLimited($fixnum.Int64 v) { $_setInt64(77, v); }
  @$pb.TagNumber(351)
  $core.bool hasTcpInfoRwndLimited() => $_has(77);
  @$pb.TagNumber(351)
  void clearTcpInfoRwndLimited() => clearField(351);

  @$pb.TagNumber(352)
  $fixnum.Int64 get tcpInfoSndbufLimited => $_getI64(78);
  @$pb.TagNumber(352)
  set tcpInfoSndbufLimited($fixnum.Int64 v) { $_setInt64(78, v); }
  @$pb.TagNumber(352)
  $core.bool hasTcpInfoSndbufLimited() => $_has(78);
  @$pb.TagNumber(352)
  void clearTcpInfoSndbufLimited() => clearField(352);

  @$pb.TagNumber(353)
  $core.int get tcpInfoDelivered => $_getIZ(79);
  @$pb.TagNumber(353)
  set tcpInfoDelivered($core.int v) { $_setUnsignedInt32(79, v); }
  @$pb.TagNumber(353)
  $core.bool hasTcpInfoDelivered() => $_has(79);
  @$pb.TagNumber(353)
  void clearTcpInfoDelivered() => clearField(353);

  @$pb.TagNumber(354)
  $core.int get tcpInfoDeliveredCe => $_getIZ(80);
  @$pb.TagNumber(354)
  set tcpInfoDeliveredCe($core.int v) { $_setUnsignedInt32(80, v); }
  @$pb.TagNumber(354)
  $core.bool hasTcpInfoDeliveredCe() => $_has(80);
  @$pb.TagNumber(354)
  void clearTcpInfoDeliveredCe() => clearField(354);

  /// https://tools.ietf.org/html/rfc4898 TCP Extended Statistics MIB
  @$pb.TagNumber(355)
  $fixnum.Int64 get tcpInfoBytesSent => $_getI64(81);
  @$pb.TagNumber(355)
  set tcpInfoBytesSent($fixnum.Int64 v) { $_setInt64(81, v); }
  @$pb.TagNumber(355)
  $core.bool hasTcpInfoBytesSent() => $_has(81);
  @$pb.TagNumber(355)
  void clearTcpInfoBytesSent() => clearField(355);

  @$pb.TagNumber(356)
  $fixnum.Int64 get tcpInfoBytesRetrans => $_getI64(82);
  @$pb.TagNumber(356)
  set tcpInfoBytesRetrans($fixnum.Int64 v) { $_setInt64(82, v); }
  @$pb.TagNumber(356)
  $core.bool hasTcpInfoBytesRetrans() => $_has(82);
  @$pb.TagNumber(356)
  void clearTcpInfoBytesRetrans() => clearField(356);

  @$pb.TagNumber(357)
  $core.int get tcpInfoDsackDups => $_getIZ(83);
  @$pb.TagNumber(357)
  set tcpInfoDsackDups($core.int v) { $_setUnsignedInt32(83, v); }
  @$pb.TagNumber(357)
  $core.bool hasTcpInfoDsackDups() => $_has(83);
  @$pb.TagNumber(357)
  void clearTcpInfoDsackDups() => clearField(357);

  @$pb.TagNumber(358)
  $core.int get tcpInfoReordSeen => $_getIZ(84);
  @$pb.TagNumber(358)
  set tcpInfoReordSeen($core.int v) { $_setUnsignedInt32(84, v); }
  @$pb.TagNumber(358)
  $core.bool hasTcpInfoReordSeen() => $_has(84);
  @$pb.TagNumber(358)
  void clearTcpInfoReordSeen() => clearField(358);

  @$pb.TagNumber(359)
  $core.int get tcpInfoRcvOoopack => $_getIZ(85);
  @$pb.TagNumber(359)
  set tcpInfoRcvOoopack($core.int v) { $_setUnsignedInt32(85, v); }
  @$pb.TagNumber(359)
  $core.bool hasTcpInfoRcvOoopack() => $_has(85);
  @$pb.TagNumber(359)
  void clearTcpInfoRcvOoopack() => clearField(359);

  @$pb.TagNumber(360)
  $core.int get tcpInfoSndWnd => $_getIZ(86);
  @$pb.TagNumber(360)
  set tcpInfoSndWnd($core.int v) { $_setUnsignedInt32(86, v); }
  @$pb.TagNumber(360)
  $core.bool hasTcpInfoSndWnd() => $_has(86);
  @$pb.TagNumber(360)
  void clearTcpInfoSndWnd() => clearField(360);

  @$pb.TagNumber(361)
  $core.int get tcpInfoRcvWnd => $_getIZ(87);
  @$pb.TagNumber(361)
  set tcpInfoRcvWnd($core.int v) { $_setUnsignedInt32(87, v); }
  @$pb.TagNumber(361)
  $core.bool hasTcpInfoRcvWnd() => $_has(87);
  @$pb.TagNumber(361)
  void clearTcpInfoRcvWnd() => clearField(361);

  @$pb.TagNumber(362)
  $core.int get tcpInfoRehash => $_getIZ(88);
  @$pb.TagNumber(362)
  set tcpInfoRehash($core.int v) { $_setUnsignedInt32(88, v); }
  @$pb.TagNumber(362)
  $core.bool hasTcpInfoRehash() => $_has(88);
  @$pb.TagNumber(362)
  void clearTcpInfoRehash() => clearField(362);

  @$pb.TagNumber(363)
  $core.int get tcpInfoTotalRto => $_getIZ(89);
  @$pb.TagNumber(363)
  set tcpInfoTotalRto($core.int v) { $_setUnsignedInt32(89, v); }
  @$pb.TagNumber(363)
  $core.bool hasTcpInfoTotalRto() => $_has(89);
  @$pb.TagNumber(363)
  void clearTcpInfoTotalRto() => clearField(363);

  @$pb.TagNumber(364)
  $core.int get tcpInfoTotalRtoRecoveries => $_getIZ(90);
  @$pb.TagNumber(364)
  set tcpInfoTotalRtoRecoveries($core.int v) { $_setUnsignedInt32(90, v); }
  @$pb.TagNumber(364)
  $core.bool hasTcpInfoTotalRtoRecoveries() => $_has(90);
  @$pb.TagNumber(364)
  void clearTcpInfoTotalRtoRecoveries() => clearField(364);

  @$pb.TagNumber(365)
  $core.int get tcpInfoTotalRtoTime => $_getIZ(91);
  @$pb.TagNumber(365)
  set tcpInfoTotalRtoTime($core.int v) { $_setUnsignedInt32(91, v); }
  @$pb.TagNumber(365)
  $core.bool hasTcpInfoTotalRtoTime() => $_has(91);
  @$pb.TagNumber(365)
  void clearTcpInfoTotalRtoTime() => clearField(365);

  /// Please note it's recommended to use the enum for efficency, but keeping the string
  /// just in case we need to quickly put a different algorithm in without updating the enum.
  /// Obviously it's optional, so it low cost.
  @$pb.TagNumber(400)
  $core.String get congestionAlgorithmString => $_getSZ(92);
  @$pb.TagNumber(400)
  set congestionAlgorithmString($core.String v) { $_setString(92, v); }
  @$pb.TagNumber(400)
  $core.bool hasCongestionAlgorithmString() => $_has(92);
  @$pb.TagNumber(400)
  void clearCongestionAlgorithmString() => clearField(400);

  @$pb.TagNumber(401)
  XtcpFlatRecord_CongestionAlgorithm get congestionAlgorithmEnum => $_getN(93);
  @$pb.TagNumber(401)
  set congestionAlgorithmEnum(XtcpFlatRecord_CongestionAlgorithm v) { setField(401, v); }
  @$pb.TagNumber(401)
  $core.bool hasCongestionAlgorithmEnum() => $_has(93);
  @$pb.TagNumber(401)
  void clearCongestionAlgorithmEnum() => clearField(401);

  @$pb.TagNumber(501)
  $core.int get typeOfService => $_getIZ(94);
  @$pb.TagNumber(501)
  set typeOfService($core.int v) { $_setUnsignedInt32(94, v); }
  @$pb.TagNumber(501)
  $core.bool hasTypeOfService() => $_has(94);
  @$pb.TagNumber(501)
  void clearTypeOfService() => clearField(501);

  @$pb.TagNumber(502)
  $core.int get trafficClass => $_getIZ(95);
  @$pb.TagNumber(502)
  set trafficClass($core.int v) { $_setUnsignedInt32(95, v); }
  @$pb.TagNumber(502)
  $core.bool hasTrafficClass() => $_has(95);
  @$pb.TagNumber(502)
  void clearTrafficClass() => clearField(502);

  @$pb.TagNumber(601)
  $core.int get skMemInfoRmemAlloc => $_getIZ(96);
  @$pb.TagNumber(601)
  set skMemInfoRmemAlloc($core.int v) { $_setUnsignedInt32(96, v); }
  @$pb.TagNumber(601)
  $core.bool hasSkMemInfoRmemAlloc() => $_has(96);
  @$pb.TagNumber(601)
  void clearSkMemInfoRmemAlloc() => clearField(601);

  @$pb.TagNumber(602)
  $core.int get skMemInfoRcvBuf => $_getIZ(97);
  @$pb.TagNumber(602)
  set skMemInfoRcvBuf($core.int v) { $_setUnsignedInt32(97, v); }
  @$pb.TagNumber(602)
  $core.bool hasSkMemInfoRcvBuf() => $_has(97);
  @$pb.TagNumber(602)
  void clearSkMemInfoRcvBuf() => clearField(602);

  @$pb.TagNumber(603)
  $core.int get skMemInfoWmemAlloc => $_getIZ(98);
  @$pb.TagNumber(603)
  set skMemInfoWmemAlloc($core.int v) { $_setUnsignedInt32(98, v); }
  @$pb.TagNumber(603)
  $core.bool hasSkMemInfoWmemAlloc() => $_has(98);
  @$pb.TagNumber(603)
  void clearSkMemInfoWmemAlloc() => clearField(603);

  @$pb.TagNumber(604)
  $core.int get skMemInfoSndBuf => $_getIZ(99);
  @$pb.TagNumber(604)
  set skMemInfoSndBuf($core.int v) { $_setUnsignedInt32(99, v); }
  @$pb.TagNumber(604)
  $core.bool hasSkMemInfoSndBuf() => $_has(99);
  @$pb.TagNumber(604)
  void clearSkMemInfoSndBuf() => clearField(604);

  @$pb.TagNumber(605)
  $core.int get skMemInfoFwdAlloc => $_getIZ(100);
  @$pb.TagNumber(605)
  set skMemInfoFwdAlloc($core.int v) { $_setUnsignedInt32(100, v); }
  @$pb.TagNumber(605)
  $core.bool hasSkMemInfoFwdAlloc() => $_has(100);
  @$pb.TagNumber(605)
  void clearSkMemInfoFwdAlloc() => clearField(605);

  @$pb.TagNumber(606)
  $core.int get skMemInfoWmemQueued => $_getIZ(101);
  @$pb.TagNumber(606)
  set skMemInfoWmemQueued($core.int v) { $_setUnsignedInt32(101, v); }
  @$pb.TagNumber(606)
  $core.bool hasSkMemInfoWmemQueued() => $_has(101);
  @$pb.TagNumber(606)
  void clearSkMemInfoWmemQueued() => clearField(606);

  @$pb.TagNumber(607)
  $core.int get skMemInfoOptmem => $_getIZ(102);
  @$pb.TagNumber(607)
  set skMemInfoOptmem($core.int v) { $_setUnsignedInt32(102, v); }
  @$pb.TagNumber(607)
  $core.bool hasSkMemInfoOptmem() => $_has(102);
  @$pb.TagNumber(607)
  void clearSkMemInfoOptmem() => clearField(607);

  @$pb.TagNumber(608)
  $core.int get skMemInfoBacklog => $_getIZ(103);
  @$pb.TagNumber(608)
  set skMemInfoBacklog($core.int v) { $_setUnsignedInt32(103, v); }
  @$pb.TagNumber(608)
  $core.bool hasSkMemInfoBacklog() => $_has(103);
  @$pb.TagNumber(608)
  void clearSkMemInfoBacklog() => clearField(608);

  @$pb.TagNumber(609)
  $core.int get skMemInfoDrops => $_getIZ(104);
  @$pb.TagNumber(609)
  set skMemInfoDrops($core.int v) { $_setUnsignedInt32(104, v); }
  @$pb.TagNumber(609)
  $core.bool hasSkMemInfoDrops() => $_has(104);
  @$pb.TagNumber(609)
  void clearSkMemInfoDrops() => clearField(609);

  @$pb.TagNumber(700)
  $core.int get shutdownState => $_getIZ(105);
  @$pb.TagNumber(700)
  set shutdownState($core.int v) { $_setUnsignedInt32(105, v); }
  @$pb.TagNumber(700)
  $core.bool hasShutdownState() => $_has(105);
  @$pb.TagNumber(700)
  void clearShutdownState() => clearField(700);

  @$pb.TagNumber(801)
  $core.int get vegasInfoEnabled => $_getIZ(106);
  @$pb.TagNumber(801)
  set vegasInfoEnabled($core.int v) { $_setUnsignedInt32(106, v); }
  @$pb.TagNumber(801)
  $core.bool hasVegasInfoEnabled() => $_has(106);
  @$pb.TagNumber(801)
  void clearVegasInfoEnabled() => clearField(801);

  @$pb.TagNumber(802)
  $core.int get vegasInfoRttCnt => $_getIZ(107);
  @$pb.TagNumber(802)
  set vegasInfoRttCnt($core.int v) { $_setUnsignedInt32(107, v); }
  @$pb.TagNumber(802)
  $core.bool hasVegasInfoRttCnt() => $_has(107);
  @$pb.TagNumber(802)
  void clearVegasInfoRttCnt() => clearField(802);

  @$pb.TagNumber(803)
  $core.int get vegasInfoRtt => $_getIZ(108);
  @$pb.TagNumber(803)
  set vegasInfoRtt($core.int v) { $_setUnsignedInt32(108, v); }
  @$pb.TagNumber(803)
  $core.bool hasVegasInfoRtt() => $_has(108);
  @$pb.TagNumber(803)
  void clearVegasInfoRtt() => clearField(803);

  @$pb.TagNumber(804)
  $core.int get vegasInfoMinRtt => $_getIZ(109);
  @$pb.TagNumber(804)
  set vegasInfoMinRtt($core.int v) { $_setUnsignedInt32(109, v); }
  @$pb.TagNumber(804)
  $core.bool hasVegasInfoMinRtt() => $_has(109);
  @$pb.TagNumber(804)
  void clearVegasInfoMinRtt() => clearField(804);

  @$pb.TagNumber(901)
  $core.int get dctcpInfoEnabled => $_getIZ(110);
  @$pb.TagNumber(901)
  set dctcpInfoEnabled($core.int v) { $_setUnsignedInt32(110, v); }
  @$pb.TagNumber(901)
  $core.bool hasDctcpInfoEnabled() => $_has(110);
  @$pb.TagNumber(901)
  void clearDctcpInfoEnabled() => clearField(901);

  @$pb.TagNumber(902)
  $core.int get dctcpInfoCeState => $_getIZ(111);
  @$pb.TagNumber(902)
  set dctcpInfoCeState($core.int v) { $_setUnsignedInt32(111, v); }
  @$pb.TagNumber(902)
  $core.bool hasDctcpInfoCeState() => $_has(111);
  @$pb.TagNumber(902)
  void clearDctcpInfoCeState() => clearField(902);

  @$pb.TagNumber(903)
  $core.int get dctcpInfoAlpha => $_getIZ(112);
  @$pb.TagNumber(903)
  set dctcpInfoAlpha($core.int v) { $_setUnsignedInt32(112, v); }
  @$pb.TagNumber(903)
  $core.bool hasDctcpInfoAlpha() => $_has(112);
  @$pb.TagNumber(903)
  void clearDctcpInfoAlpha() => clearField(903);

  @$pb.TagNumber(904)
  $core.int get dctcpInfoAbEcn => $_getIZ(113);
  @$pb.TagNumber(904)
  set dctcpInfoAbEcn($core.int v) { $_setUnsignedInt32(113, v); }
  @$pb.TagNumber(904)
  $core.bool hasDctcpInfoAbEcn() => $_has(113);
  @$pb.TagNumber(904)
  void clearDctcpInfoAbEcn() => clearField(904);

  @$pb.TagNumber(905)
  $core.int get dctcpInfoAbTot => $_getIZ(114);
  @$pb.TagNumber(905)
  set dctcpInfoAbTot($core.int v) { $_setUnsignedInt32(114, v); }
  @$pb.TagNumber(905)
  $core.bool hasDctcpInfoAbTot() => $_has(114);
  @$pb.TagNumber(905)
  void clearDctcpInfoAbTot() => clearField(905);

  @$pb.TagNumber(1001)
  $core.int get bbrInfoBwLo => $_getIZ(115);
  @$pb.TagNumber(1001)
  set bbrInfoBwLo($core.int v) { $_setUnsignedInt32(115, v); }
  @$pb.TagNumber(1001)
  $core.bool hasBbrInfoBwLo() => $_has(115);
  @$pb.TagNumber(1001)
  void clearBbrInfoBwLo() => clearField(1001);

  @$pb.TagNumber(1002)
  $core.int get bbrInfoBwHi => $_getIZ(116);
  @$pb.TagNumber(1002)
  set bbrInfoBwHi($core.int v) { $_setUnsignedInt32(116, v); }
  @$pb.TagNumber(1002)
  $core.bool hasBbrInfoBwHi() => $_has(116);
  @$pb.TagNumber(1002)
  void clearBbrInfoBwHi() => clearField(1002);

  @$pb.TagNumber(1003)
  $core.int get bbrInfoMinRtt => $_getIZ(117);
  @$pb.TagNumber(1003)
  set bbrInfoMinRtt($core.int v) { $_setUnsignedInt32(117, v); }
  @$pb.TagNumber(1003)
  $core.bool hasBbrInfoMinRtt() => $_has(117);
  @$pb.TagNumber(1003)
  void clearBbrInfoMinRtt() => clearField(1003);

  @$pb.TagNumber(1004)
  $core.int get bbrInfoPacingGain => $_getIZ(118);
  @$pb.TagNumber(1004)
  set bbrInfoPacingGain($core.int v) { $_setUnsignedInt32(118, v); }
  @$pb.TagNumber(1004)
  $core.bool hasBbrInfoPacingGain() => $_has(118);
  @$pb.TagNumber(1004)
  void clearBbrInfoPacingGain() => clearField(1004);

  @$pb.TagNumber(1005)
  $core.int get bbrInfoCwndGain => $_getIZ(119);
  @$pb.TagNumber(1005)
  set bbrInfoCwndGain($core.int v) { $_setUnsignedInt32(119, v); }
  @$pb.TagNumber(1005)
  $core.bool hasBbrInfoCwndGain() => $_has(119);
  @$pb.TagNumber(1005)
  void clearBbrInfoCwndGain() => clearField(1005);

  @$pb.TagNumber(1101)
  $core.int get classId => $_getIZ(120);
  @$pb.TagNumber(1101)
  set classId($core.int v) { $_setUnsignedInt32(120, v); }
  @$pb.TagNumber(1101)
  $core.bool hasClassId() => $_has(120);
  @$pb.TagNumber(1101)
  void clearClassId() => clearField(1101);

  @$pb.TagNumber(1102)
  $core.int get sockOpt => $_getIZ(121);
  @$pb.TagNumber(1102)
  set sockOpt($core.int v) { $_setUnsignedInt32(121, v); }
  @$pb.TagNumber(1102)
  $core.bool hasSockOpt() => $_has(121);
  @$pb.TagNumber(1102)
  void clearSockOpt() => clearField(1102);

  @$pb.TagNumber(1203)
  $fixnum.Int64 get cGroup => $_getI64(122);
  @$pb.TagNumber(1203)
  set cGroup($fixnum.Int64 v) { $_setInt64(122, v); }
  @$pb.TagNumber(1203)
  $core.bool hasCGroup() => $_has(122);
  @$pb.TagNumber(1203)
  void clearCGroup() => clearField(1203);
}


const _omitFieldNames = $core.bool.fromEnvironment('protobuf.omit_field_names');
const _omitMessageNames = $core.bool.fromEnvironment('protobuf.omit_message_names');
