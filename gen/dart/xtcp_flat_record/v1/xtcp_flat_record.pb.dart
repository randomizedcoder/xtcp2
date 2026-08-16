// This is a generated file - do not edit.
//
// Generated from xtcp_flat_record/v1/xtcp_flat_record.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, prefer_relative_imports

import 'dart:core' as $core;

import 'package:fixnum/fixnum.dart' as $fixnum;
import 'package:protobuf/protobuf.dart' as $pb;

import 'xtcp_flat_record.pbenum.dart';

export 'package:protobuf/protobuf.dart' show GeneratedMessageGenericExtensions;

export 'xtcp_flat_record.pbenum.dart';

/// Envelope is the protobufList wrapper to allow for batch inserts into Clickhouse
/// https://clickhouse.com/docs/en/interfaces/formats#protobuflist
class Envelope extends $pb.GeneratedMessage {
  factory Envelope({
    $core.Iterable<XtcpFlatRecord>? row,
  }) {
    final result = create();
    if (row != null) result.row.addAll(row);
    return result;
  }

  Envelope._();

  factory Envelope.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory Envelope.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'Envelope',
      package:
          const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_flat_record.v1'),
      createEmptyInstance: create)
    ..pPM<XtcpFlatRecord>(10, _omitFieldNames ? '' : 'row',
        subBuilder: XtcpFlatRecord.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Envelope clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Envelope copyWith(void Function(Envelope) updates) =>
      super.copyWith((message) => updates(message as Envelope)) as Envelope;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static Envelope create() => Envelope._();
  @$core.override
  Envelope createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static Envelope getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<Envelope>(create);
  static Envelope? _defaultInstance;

  @$pb.TagNumber(10)
  $pb.PbList<XtcpFlatRecord> get row => $_getList(0);
}

/// Field-number layout (reorganised 2026-08 while the record had few consumers):
///   metadata  ...  1-999   (identity + per-uplink network topology)
///   payload   ... 1000+    (kernel inet_diag subsystems, one hundred-block each)
/// ClickHouse's Protobuf format maps columns by field NAME and Parquet uses its own
/// schema, so the wire-tag renumber does not break ingestion or historical Parquet.
class XtcpFlatRecord extends $pb.GeneratedMessage {
  factory XtcpFlatRecord({
    $core.int? schemaVersion,
    $core.String? daemonVersion,
    $fixnum.Int64? timestampNs,
    $core.String? hostname,
    $core.String? location,
    $core.String? netns,
    $fixnum.Int64? netnsInode,
    $core.int? nsid,
    $core.String? containerId,
    $core.String? containerRuntime,
    $core.String? containerName,
    $core.String? containerImage,
    $core.String? label,
    $core.String? tag,
    $fixnum.Int64? recordCounter,
    $fixnum.Int64? socketFd,
    $fixnum.Int64? netlinkerId,
    $core.String? uplink1Ifname,
    $core.String? uplink1NicDriver,
    $core.String? uplink1NicModel,
    $core.int? uplink1NicPciVendor,
    $core.int? uplink1NicPciDevice,
    $core.String? uplink1NicBusInfo,
    $core.int? uplink1NicSpeedMbps,
    $core.String? uplink1NicFwVersion,
    $core.String? uplink1LldpChassisName,
    $core.String? uplink1LldpChassisId,
    $core.String? uplink1LldpMgmtIp,
    $core.String? uplink1LldpPortId,
    $core.String? uplink1LldpPortDescr,
    $core.String? uplink2Ifname,
    $core.String? uplink2NicDriver,
    $core.String? uplink2NicModel,
    $core.int? uplink2NicPciVendor,
    $core.int? uplink2NicPciDevice,
    $core.String? uplink2NicBusInfo,
    $core.int? uplink2NicSpeedMbps,
    $core.String? uplink2NicFwVersion,
    $core.String? uplink2LldpChassisName,
    $core.String? uplink2LldpChassisId,
    $core.String? uplink2LldpMgmtIp,
    $core.String? uplink2LldpPortId,
    $core.String? uplink2LldpPortDescr,
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
    final result = create();
    if (schemaVersion != null) result.schemaVersion = schemaVersion;
    if (daemonVersion != null) result.daemonVersion = daemonVersion;
    if (timestampNs != null) result.timestampNs = timestampNs;
    if (hostname != null) result.hostname = hostname;
    if (location != null) result.location = location;
    if (netns != null) result.netns = netns;
    if (netnsInode != null) result.netnsInode = netnsInode;
    if (nsid != null) result.nsid = nsid;
    if (containerId != null) result.containerId = containerId;
    if (containerRuntime != null) result.containerRuntime = containerRuntime;
    if (containerName != null) result.containerName = containerName;
    if (containerImage != null) result.containerImage = containerImage;
    if (label != null) result.label = label;
    if (tag != null) result.tag = tag;
    if (recordCounter != null) result.recordCounter = recordCounter;
    if (socketFd != null) result.socketFd = socketFd;
    if (netlinkerId != null) result.netlinkerId = netlinkerId;
    if (uplink1Ifname != null) result.uplink1Ifname = uplink1Ifname;
    if (uplink1NicDriver != null) result.uplink1NicDriver = uplink1NicDriver;
    if (uplink1NicModel != null) result.uplink1NicModel = uplink1NicModel;
    if (uplink1NicPciVendor != null)
      result.uplink1NicPciVendor = uplink1NicPciVendor;
    if (uplink1NicPciDevice != null)
      result.uplink1NicPciDevice = uplink1NicPciDevice;
    if (uplink1NicBusInfo != null) result.uplink1NicBusInfo = uplink1NicBusInfo;
    if (uplink1NicSpeedMbps != null)
      result.uplink1NicSpeedMbps = uplink1NicSpeedMbps;
    if (uplink1NicFwVersion != null)
      result.uplink1NicFwVersion = uplink1NicFwVersion;
    if (uplink1LldpChassisName != null)
      result.uplink1LldpChassisName = uplink1LldpChassisName;
    if (uplink1LldpChassisId != null)
      result.uplink1LldpChassisId = uplink1LldpChassisId;
    if (uplink1LldpMgmtIp != null) result.uplink1LldpMgmtIp = uplink1LldpMgmtIp;
    if (uplink1LldpPortId != null) result.uplink1LldpPortId = uplink1LldpPortId;
    if (uplink1LldpPortDescr != null)
      result.uplink1LldpPortDescr = uplink1LldpPortDescr;
    if (uplink2Ifname != null) result.uplink2Ifname = uplink2Ifname;
    if (uplink2NicDriver != null) result.uplink2NicDriver = uplink2NicDriver;
    if (uplink2NicModel != null) result.uplink2NicModel = uplink2NicModel;
    if (uplink2NicPciVendor != null)
      result.uplink2NicPciVendor = uplink2NicPciVendor;
    if (uplink2NicPciDevice != null)
      result.uplink2NicPciDevice = uplink2NicPciDevice;
    if (uplink2NicBusInfo != null) result.uplink2NicBusInfo = uplink2NicBusInfo;
    if (uplink2NicSpeedMbps != null)
      result.uplink2NicSpeedMbps = uplink2NicSpeedMbps;
    if (uplink2NicFwVersion != null)
      result.uplink2NicFwVersion = uplink2NicFwVersion;
    if (uplink2LldpChassisName != null)
      result.uplink2LldpChassisName = uplink2LldpChassisName;
    if (uplink2LldpChassisId != null)
      result.uplink2LldpChassisId = uplink2LldpChassisId;
    if (uplink2LldpMgmtIp != null) result.uplink2LldpMgmtIp = uplink2LldpMgmtIp;
    if (uplink2LldpPortId != null) result.uplink2LldpPortId = uplink2LldpPortId;
    if (uplink2LldpPortDescr != null)
      result.uplink2LldpPortDescr = uplink2LldpPortDescr;
    if (inetDiagMsgFamily != null) result.inetDiagMsgFamily = inetDiagMsgFamily;
    if (inetDiagMsgState != null) result.inetDiagMsgState = inetDiagMsgState;
    if (inetDiagMsgTimer != null) result.inetDiagMsgTimer = inetDiagMsgTimer;
    if (inetDiagMsgRetrans != null)
      result.inetDiagMsgRetrans = inetDiagMsgRetrans;
    if (inetDiagMsgSocketSourcePort != null)
      result.inetDiagMsgSocketSourcePort = inetDiagMsgSocketSourcePort;
    if (inetDiagMsgSocketDestinationPort != null)
      result.inetDiagMsgSocketDestinationPort =
          inetDiagMsgSocketDestinationPort;
    if (inetDiagMsgSocketSource != null)
      result.inetDiagMsgSocketSource = inetDiagMsgSocketSource;
    if (inetDiagMsgSocketDestination != null)
      result.inetDiagMsgSocketDestination = inetDiagMsgSocketDestination;
    if (inetDiagMsgSocketInterface != null)
      result.inetDiagMsgSocketInterface = inetDiagMsgSocketInterface;
    if (inetDiagMsgSocketCookie != null)
      result.inetDiagMsgSocketCookie = inetDiagMsgSocketCookie;
    if (inetDiagMsgSocketDestAsn != null)
      result.inetDiagMsgSocketDestAsn = inetDiagMsgSocketDestAsn;
    if (inetDiagMsgSocketNextHopAsn != null)
      result.inetDiagMsgSocketNextHopAsn = inetDiagMsgSocketNextHopAsn;
    if (inetDiagMsgExpires != null)
      result.inetDiagMsgExpires = inetDiagMsgExpires;
    if (inetDiagMsgRqueue != null) result.inetDiagMsgRqueue = inetDiagMsgRqueue;
    if (inetDiagMsgWqueue != null) result.inetDiagMsgWqueue = inetDiagMsgWqueue;
    if (inetDiagMsgUid != null) result.inetDiagMsgUid = inetDiagMsgUid;
    if (inetDiagMsgInode != null) result.inetDiagMsgInode = inetDiagMsgInode;
    if (memInfoRmem != null) result.memInfoRmem = memInfoRmem;
    if (memInfoWmem != null) result.memInfoWmem = memInfoWmem;
    if (memInfoFmem != null) result.memInfoFmem = memInfoFmem;
    if (memInfoTmem != null) result.memInfoTmem = memInfoTmem;
    if (tcpInfoState != null) result.tcpInfoState = tcpInfoState;
    if (tcpInfoCaState != null) result.tcpInfoCaState = tcpInfoCaState;
    if (tcpInfoRetransmits != null)
      result.tcpInfoRetransmits = tcpInfoRetransmits;
    if (tcpInfoProbes != null) result.tcpInfoProbes = tcpInfoProbes;
    if (tcpInfoBackoff != null) result.tcpInfoBackoff = tcpInfoBackoff;
    if (tcpInfoOptions != null) result.tcpInfoOptions = tcpInfoOptions;
    if (tcpInfoSendScale != null) result.tcpInfoSendScale = tcpInfoSendScale;
    if (tcpInfoRcvScale != null) result.tcpInfoRcvScale = tcpInfoRcvScale;
    if (tcpInfoDeliveryRateAppLimited != null)
      result.tcpInfoDeliveryRateAppLimited = tcpInfoDeliveryRateAppLimited;
    if (tcpInfoFastOpenClientFailed != null)
      result.tcpInfoFastOpenClientFailed = tcpInfoFastOpenClientFailed;
    if (tcpInfoRto != null) result.tcpInfoRto = tcpInfoRto;
    if (tcpInfoAto != null) result.tcpInfoAto = tcpInfoAto;
    if (tcpInfoSndMss != null) result.tcpInfoSndMss = tcpInfoSndMss;
    if (tcpInfoRcvMss != null) result.tcpInfoRcvMss = tcpInfoRcvMss;
    if (tcpInfoUnacked != null) result.tcpInfoUnacked = tcpInfoUnacked;
    if (tcpInfoSacked != null) result.tcpInfoSacked = tcpInfoSacked;
    if (tcpInfoLost != null) result.tcpInfoLost = tcpInfoLost;
    if (tcpInfoRetrans != null) result.tcpInfoRetrans = tcpInfoRetrans;
    if (tcpInfoFackets != null) result.tcpInfoFackets = tcpInfoFackets;
    if (tcpInfoLastDataSent != null)
      result.tcpInfoLastDataSent = tcpInfoLastDataSent;
    if (tcpInfoLastAckSent != null)
      result.tcpInfoLastAckSent = tcpInfoLastAckSent;
    if (tcpInfoLastDataRecv != null)
      result.tcpInfoLastDataRecv = tcpInfoLastDataRecv;
    if (tcpInfoLastAckRecv != null)
      result.tcpInfoLastAckRecv = tcpInfoLastAckRecv;
    if (tcpInfoPmtu != null) result.tcpInfoPmtu = tcpInfoPmtu;
    if (tcpInfoRcvSsthresh != null)
      result.tcpInfoRcvSsthresh = tcpInfoRcvSsthresh;
    if (tcpInfoRtt != null) result.tcpInfoRtt = tcpInfoRtt;
    if (tcpInfoRttVar != null) result.tcpInfoRttVar = tcpInfoRttVar;
    if (tcpInfoSndSsthresh != null)
      result.tcpInfoSndSsthresh = tcpInfoSndSsthresh;
    if (tcpInfoSndCwnd != null) result.tcpInfoSndCwnd = tcpInfoSndCwnd;
    if (tcpInfoAdvMss != null) result.tcpInfoAdvMss = tcpInfoAdvMss;
    if (tcpInfoReordering != null) result.tcpInfoReordering = tcpInfoReordering;
    if (tcpInfoRcvRtt != null) result.tcpInfoRcvRtt = tcpInfoRcvRtt;
    if (tcpInfoRcvSpace != null) result.tcpInfoRcvSpace = tcpInfoRcvSpace;
    if (tcpInfoTotalRetrans != null)
      result.tcpInfoTotalRetrans = tcpInfoTotalRetrans;
    if (tcpInfoPacingRate != null) result.tcpInfoPacingRate = tcpInfoPacingRate;
    if (tcpInfoMaxPacingRate != null)
      result.tcpInfoMaxPacingRate = tcpInfoMaxPacingRate;
    if (tcpInfoBytesAcked != null) result.tcpInfoBytesAcked = tcpInfoBytesAcked;
    if (tcpInfoBytesReceived != null)
      result.tcpInfoBytesReceived = tcpInfoBytesReceived;
    if (tcpInfoSegsOut != null) result.tcpInfoSegsOut = tcpInfoSegsOut;
    if (tcpInfoSegsIn != null) result.tcpInfoSegsIn = tcpInfoSegsIn;
    if (tcpInfoNotSentBytes != null)
      result.tcpInfoNotSentBytes = tcpInfoNotSentBytes;
    if (tcpInfoMinRtt != null) result.tcpInfoMinRtt = tcpInfoMinRtt;
    if (tcpInfoDataSegsIn != null) result.tcpInfoDataSegsIn = tcpInfoDataSegsIn;
    if (tcpInfoDataSegsOut != null)
      result.tcpInfoDataSegsOut = tcpInfoDataSegsOut;
    if (tcpInfoDeliveryRate != null)
      result.tcpInfoDeliveryRate = tcpInfoDeliveryRate;
    if (tcpInfoBusyTime != null) result.tcpInfoBusyTime = tcpInfoBusyTime;
    if (tcpInfoRwndLimited != null)
      result.tcpInfoRwndLimited = tcpInfoRwndLimited;
    if (tcpInfoSndbufLimited != null)
      result.tcpInfoSndbufLimited = tcpInfoSndbufLimited;
    if (tcpInfoDelivered != null) result.tcpInfoDelivered = tcpInfoDelivered;
    if (tcpInfoDeliveredCe != null)
      result.tcpInfoDeliveredCe = tcpInfoDeliveredCe;
    if (tcpInfoBytesSent != null) result.tcpInfoBytesSent = tcpInfoBytesSent;
    if (tcpInfoBytesRetrans != null)
      result.tcpInfoBytesRetrans = tcpInfoBytesRetrans;
    if (tcpInfoDsackDups != null) result.tcpInfoDsackDups = tcpInfoDsackDups;
    if (tcpInfoReordSeen != null) result.tcpInfoReordSeen = tcpInfoReordSeen;
    if (tcpInfoRcvOoopack != null) result.tcpInfoRcvOoopack = tcpInfoRcvOoopack;
    if (tcpInfoSndWnd != null) result.tcpInfoSndWnd = tcpInfoSndWnd;
    if (tcpInfoRcvWnd != null) result.tcpInfoRcvWnd = tcpInfoRcvWnd;
    if (tcpInfoRehash != null) result.tcpInfoRehash = tcpInfoRehash;
    if (tcpInfoTotalRto != null) result.tcpInfoTotalRto = tcpInfoTotalRto;
    if (tcpInfoTotalRtoRecoveries != null)
      result.tcpInfoTotalRtoRecoveries = tcpInfoTotalRtoRecoveries;
    if (tcpInfoTotalRtoTime != null)
      result.tcpInfoTotalRtoTime = tcpInfoTotalRtoTime;
    if (congestionAlgorithmString != null)
      result.congestionAlgorithmString = congestionAlgorithmString;
    if (congestionAlgorithmEnum != null)
      result.congestionAlgorithmEnum = congestionAlgorithmEnum;
    if (typeOfService != null) result.typeOfService = typeOfService;
    if (trafficClass != null) result.trafficClass = trafficClass;
    if (skMemInfoRmemAlloc != null)
      result.skMemInfoRmemAlloc = skMemInfoRmemAlloc;
    if (skMemInfoRcvBuf != null) result.skMemInfoRcvBuf = skMemInfoRcvBuf;
    if (skMemInfoWmemAlloc != null)
      result.skMemInfoWmemAlloc = skMemInfoWmemAlloc;
    if (skMemInfoSndBuf != null) result.skMemInfoSndBuf = skMemInfoSndBuf;
    if (skMemInfoFwdAlloc != null) result.skMemInfoFwdAlloc = skMemInfoFwdAlloc;
    if (skMemInfoWmemQueued != null)
      result.skMemInfoWmemQueued = skMemInfoWmemQueued;
    if (skMemInfoOptmem != null) result.skMemInfoOptmem = skMemInfoOptmem;
    if (skMemInfoBacklog != null) result.skMemInfoBacklog = skMemInfoBacklog;
    if (skMemInfoDrops != null) result.skMemInfoDrops = skMemInfoDrops;
    if (shutdownState != null) result.shutdownState = shutdownState;
    if (vegasInfoEnabled != null) result.vegasInfoEnabled = vegasInfoEnabled;
    if (vegasInfoRttCnt != null) result.vegasInfoRttCnt = vegasInfoRttCnt;
    if (vegasInfoRtt != null) result.vegasInfoRtt = vegasInfoRtt;
    if (vegasInfoMinRtt != null) result.vegasInfoMinRtt = vegasInfoMinRtt;
    if (dctcpInfoEnabled != null) result.dctcpInfoEnabled = dctcpInfoEnabled;
    if (dctcpInfoCeState != null) result.dctcpInfoCeState = dctcpInfoCeState;
    if (dctcpInfoAlpha != null) result.dctcpInfoAlpha = dctcpInfoAlpha;
    if (dctcpInfoAbEcn != null) result.dctcpInfoAbEcn = dctcpInfoAbEcn;
    if (dctcpInfoAbTot != null) result.dctcpInfoAbTot = dctcpInfoAbTot;
    if (bbrInfoBwLo != null) result.bbrInfoBwLo = bbrInfoBwLo;
    if (bbrInfoBwHi != null) result.bbrInfoBwHi = bbrInfoBwHi;
    if (bbrInfoMinRtt != null) result.bbrInfoMinRtt = bbrInfoMinRtt;
    if (bbrInfoPacingGain != null) result.bbrInfoPacingGain = bbrInfoPacingGain;
    if (bbrInfoCwndGain != null) result.bbrInfoCwndGain = bbrInfoCwndGain;
    if (classId != null) result.classId = classId;
    if (sockOpt != null) result.sockOpt = sockOpt;
    if (cGroup != null) result.cGroup = cGroup;
    return result;
  }

  XtcpFlatRecord._();

  factory XtcpFlatRecord.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory XtcpFlatRecord.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'XtcpFlatRecord',
      package:
          const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_flat_record.v1'),
      createEmptyInstance: create)
    ..aI(1, _omitFieldNames ? '' : 'schemaVersion',
        fieldType: $pb.PbFieldType.OU3)
    ..aOS(2, _omitFieldNames ? '' : 'daemonVersion')
    ..aInt64(10, _omitFieldNames ? '' : 'timestampNs')
    ..aOS(20, _omitFieldNames ? '' : 'hostname')
    ..aOS(21, _omitFieldNames ? '' : 'location')
    ..aOS(30, _omitFieldNames ? '' : 'netns')
    ..a<$fixnum.Int64>(
        31, _omitFieldNames ? '' : 'netnsInode', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..aI(32, _omitFieldNames ? '' : 'nsid', fieldType: $pb.PbFieldType.OU3)
    ..aOS(40, _omitFieldNames ? '' : 'containerId')
    ..aOS(41, _omitFieldNames ? '' : 'containerRuntime')
    ..aOS(42, _omitFieldNames ? '' : 'containerName')
    ..aOS(43, _omitFieldNames ? '' : 'containerImage')
    ..aOS(50, _omitFieldNames ? '' : 'label')
    ..aOS(51, _omitFieldNames ? '' : 'tag')
    ..a<$fixnum.Int64>(
        60, _omitFieldNames ? '' : 'recordCounter', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..a<$fixnum.Int64>(
        61, _omitFieldNames ? '' : 'socketFd', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..a<$fixnum.Int64>(
        62, _omitFieldNames ? '' : 'netlinkerId', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..aOS(100, _omitFieldNames ? '' : 'uplink1Ifname')
    ..aOS(101, _omitFieldNames ? '' : 'uplink1NicDriver')
    ..aOS(102, _omitFieldNames ? '' : 'uplink1NicModel')
    ..aI(103, _omitFieldNames ? '' : 'uplink1NicPciVendor',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(104, _omitFieldNames ? '' : 'uplink1NicPciDevice',
        fieldType: $pb.PbFieldType.OU3)
    ..aOS(105, _omitFieldNames ? '' : 'uplink1NicBusInfo')
    ..aI(106, _omitFieldNames ? '' : 'uplink1NicSpeedMbps',
        fieldType: $pb.PbFieldType.OU3)
    ..aOS(107, _omitFieldNames ? '' : 'uplink1NicFwVersion')
    ..aOS(120, _omitFieldNames ? '' : 'uplink1LldpChassisName')
    ..aOS(121, _omitFieldNames ? '' : 'uplink1LldpChassisId')
    ..aOS(122, _omitFieldNames ? '' : 'uplink1LldpMgmtIp')
    ..aOS(123, _omitFieldNames ? '' : 'uplink1LldpPortId')
    ..aOS(124, _omitFieldNames ? '' : 'uplink1LldpPortDescr')
    ..aOS(200, _omitFieldNames ? '' : 'uplink2Ifname')
    ..aOS(201, _omitFieldNames ? '' : 'uplink2NicDriver')
    ..aOS(202, _omitFieldNames ? '' : 'uplink2NicModel')
    ..aI(203, _omitFieldNames ? '' : 'uplink2NicPciVendor',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(204, _omitFieldNames ? '' : 'uplink2NicPciDevice',
        fieldType: $pb.PbFieldType.OU3)
    ..aOS(205, _omitFieldNames ? '' : 'uplink2NicBusInfo')
    ..aI(206, _omitFieldNames ? '' : 'uplink2NicSpeedMbps',
        fieldType: $pb.PbFieldType.OU3)
    ..aOS(207, _omitFieldNames ? '' : 'uplink2NicFwVersion')
    ..aOS(220, _omitFieldNames ? '' : 'uplink2LldpChassisName')
    ..aOS(221, _omitFieldNames ? '' : 'uplink2LldpChassisId')
    ..aOS(222, _omitFieldNames ? '' : 'uplink2LldpMgmtIp')
    ..aOS(223, _omitFieldNames ? '' : 'uplink2LldpPortId')
    ..aOS(224, _omitFieldNames ? '' : 'uplink2LldpPortDescr')
    ..aI(1001, _omitFieldNames ? '' : 'inetDiagMsgFamily',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1002, _omitFieldNames ? '' : 'inetDiagMsgState',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1003, _omitFieldNames ? '' : 'inetDiagMsgTimer',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1004, _omitFieldNames ? '' : 'inetDiagMsgRetrans',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1005, _omitFieldNames ? '' : 'inetDiagMsgSocketSourcePort',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1006, _omitFieldNames ? '' : 'inetDiagMsgSocketDestinationPort',
        fieldType: $pb.PbFieldType.OU3)
    ..a<$core.List<$core.int>>(1007,
        _omitFieldNames ? '' : 'inetDiagMsgSocketSource', $pb.PbFieldType.OY)
    ..a<$core.List<$core.int>>(
        1008,
        _omitFieldNames ? '' : 'inetDiagMsgSocketDestination',
        $pb.PbFieldType.OY)
    ..aI(1009, _omitFieldNames ? '' : 'inetDiagMsgSocketInterface',
        fieldType: $pb.PbFieldType.OU3)
    ..a<$fixnum.Int64>(1010, _omitFieldNames ? '' : 'inetDiagMsgSocketCookie',
        $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..a<$fixnum.Int64>(1011, _omitFieldNames ? '' : 'inetDiagMsgSocketDestAsn',
        $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..a<$fixnum.Int64>(
        1012,
        _omitFieldNames ? '' : 'inetDiagMsgSocketNextHopAsn',
        $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..aI(1013, _omitFieldNames ? '' : 'inetDiagMsgExpires',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1014, _omitFieldNames ? '' : 'inetDiagMsgRqueue',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1015, _omitFieldNames ? '' : 'inetDiagMsgWqueue',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1016, _omitFieldNames ? '' : 'inetDiagMsgUid',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1017, _omitFieldNames ? '' : 'inetDiagMsgInode',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1101, _omitFieldNames ? '' : 'memInfoRmem',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1102, _omitFieldNames ? '' : 'memInfoWmem',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1103, _omitFieldNames ? '' : 'memInfoFmem',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1104, _omitFieldNames ? '' : 'memInfoTmem',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1201, _omitFieldNames ? '' : 'tcpInfoState',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1202, _omitFieldNames ? '' : 'tcpInfoCaState',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1203, _omitFieldNames ? '' : 'tcpInfoRetransmits',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1204, _omitFieldNames ? '' : 'tcpInfoProbes',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1205, _omitFieldNames ? '' : 'tcpInfoBackoff',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1206, _omitFieldNames ? '' : 'tcpInfoOptions',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1207, _omitFieldNames ? '' : 'tcpInfoSendScale',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1208, _omitFieldNames ? '' : 'tcpInfoRcvScale',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1209, _omitFieldNames ? '' : 'tcpInfoDeliveryRateAppLimited',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1210, _omitFieldNames ? '' : 'tcpInfoFastOpenClientFailed',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1215, _omitFieldNames ? '' : 'tcpInfoRto',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1216, _omitFieldNames ? '' : 'tcpInfoAto',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1217, _omitFieldNames ? '' : 'tcpInfoSndMss',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1218, _omitFieldNames ? '' : 'tcpInfoRcvMss',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1219, _omitFieldNames ? '' : 'tcpInfoUnacked',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1220, _omitFieldNames ? '' : 'tcpInfoSacked',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1221, _omitFieldNames ? '' : 'tcpInfoLost',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1222, _omitFieldNames ? '' : 'tcpInfoRetrans',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1223, _omitFieldNames ? '' : 'tcpInfoFackets',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1224, _omitFieldNames ? '' : 'tcpInfoLastDataSent',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1225, _omitFieldNames ? '' : 'tcpInfoLastAckSent',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1226, _omitFieldNames ? '' : 'tcpInfoLastDataRecv',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1227, _omitFieldNames ? '' : 'tcpInfoLastAckRecv',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1228, _omitFieldNames ? '' : 'tcpInfoPmtu',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1229, _omitFieldNames ? '' : 'tcpInfoRcvSsthresh',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1230, _omitFieldNames ? '' : 'tcpInfoRtt',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1231, _omitFieldNames ? '' : 'tcpInfoRttVar',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1232, _omitFieldNames ? '' : 'tcpInfoSndSsthresh',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1233, _omitFieldNames ? '' : 'tcpInfoSndCwnd',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1234, _omitFieldNames ? '' : 'tcpInfoAdvMss',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1235, _omitFieldNames ? '' : 'tcpInfoReordering',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1236, _omitFieldNames ? '' : 'tcpInfoRcvRtt',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1237, _omitFieldNames ? '' : 'tcpInfoRcvSpace',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1238, _omitFieldNames ? '' : 'tcpInfoTotalRetrans',
        fieldType: $pb.PbFieldType.OU3)
    ..a<$fixnum.Int64>(
        1239, _omitFieldNames ? '' : 'tcpInfoPacingRate', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..a<$fixnum.Int64>(1240, _omitFieldNames ? '' : 'tcpInfoMaxPacingRate',
        $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..a<$fixnum.Int64>(
        1241, _omitFieldNames ? '' : 'tcpInfoBytesAcked', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..a<$fixnum.Int64>(1242, _omitFieldNames ? '' : 'tcpInfoBytesReceived',
        $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..aI(1243, _omitFieldNames ? '' : 'tcpInfoSegsOut',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1244, _omitFieldNames ? '' : 'tcpInfoSegsIn',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1245, _omitFieldNames ? '' : 'tcpInfoNotSentBytes',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1246, _omitFieldNames ? '' : 'tcpInfoMinRtt',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1247, _omitFieldNames ? '' : 'tcpInfoDataSegsIn',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1248, _omitFieldNames ? '' : 'tcpInfoDataSegsOut',
        fieldType: $pb.PbFieldType.OU3)
    ..a<$fixnum.Int64>(
        1249, _omitFieldNames ? '' : 'tcpInfoDeliveryRate', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..a<$fixnum.Int64>(
        1250, _omitFieldNames ? '' : 'tcpInfoBusyTime', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..a<$fixnum.Int64>(
        1251, _omitFieldNames ? '' : 'tcpInfoRwndLimited', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..a<$fixnum.Int64>(1252, _omitFieldNames ? '' : 'tcpInfoSndbufLimited',
        $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..aI(1253, _omitFieldNames ? '' : 'tcpInfoDelivered',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1254, _omitFieldNames ? '' : 'tcpInfoDeliveredCe',
        fieldType: $pb.PbFieldType.OU3)
    ..a<$fixnum.Int64>(
        1255, _omitFieldNames ? '' : 'tcpInfoBytesSent', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..a<$fixnum.Int64>(
        1256, _omitFieldNames ? '' : 'tcpInfoBytesRetrans', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..aI(1257, _omitFieldNames ? '' : 'tcpInfoDsackDups',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1258, _omitFieldNames ? '' : 'tcpInfoReordSeen',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1259, _omitFieldNames ? '' : 'tcpInfoRcvOoopack',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1260, _omitFieldNames ? '' : 'tcpInfoSndWnd',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1261, _omitFieldNames ? '' : 'tcpInfoRcvWnd',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1262, _omitFieldNames ? '' : 'tcpInfoRehash',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1263, _omitFieldNames ? '' : 'tcpInfoTotalRto',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1264, _omitFieldNames ? '' : 'tcpInfoTotalRtoRecoveries',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1265, _omitFieldNames ? '' : 'tcpInfoTotalRtoTime',
        fieldType: $pb.PbFieldType.OU3)
    ..aOS(1300, _omitFieldNames ? '' : 'congestionAlgorithmString')
    ..aE<XtcpFlatRecord_CongestionAlgorithm>(
        1301, _omitFieldNames ? '' : 'congestionAlgorithmEnum',
        enumValues: XtcpFlatRecord_CongestionAlgorithm.values)
    ..aI(1401, _omitFieldNames ? '' : 'typeOfService',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1402, _omitFieldNames ? '' : 'trafficClass',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1501, _omitFieldNames ? '' : 'skMemInfoRmemAlloc',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1502, _omitFieldNames ? '' : 'skMemInfoRcvBuf',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1503, _omitFieldNames ? '' : 'skMemInfoWmemAlloc',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1504, _omitFieldNames ? '' : 'skMemInfoSndBuf',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1505, _omitFieldNames ? '' : 'skMemInfoFwdAlloc',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1506, _omitFieldNames ? '' : 'skMemInfoWmemQueued',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1507, _omitFieldNames ? '' : 'skMemInfoOptmem',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1508, _omitFieldNames ? '' : 'skMemInfoBacklog',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1509, _omitFieldNames ? '' : 'skMemInfoDrops',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1600, _omitFieldNames ? '' : 'shutdownState',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1701, _omitFieldNames ? '' : 'vegasInfoEnabled',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1702, _omitFieldNames ? '' : 'vegasInfoRttCnt',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1703, _omitFieldNames ? '' : 'vegasInfoRtt',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1704, _omitFieldNames ? '' : 'vegasInfoMinRtt',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1801, _omitFieldNames ? '' : 'dctcpInfoEnabled',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1802, _omitFieldNames ? '' : 'dctcpInfoCeState',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1803, _omitFieldNames ? '' : 'dctcpInfoAlpha',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1804, _omitFieldNames ? '' : 'dctcpInfoAbEcn',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1805, _omitFieldNames ? '' : 'dctcpInfoAbTot',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1901, _omitFieldNames ? '' : 'bbrInfoBwLo',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1902, _omitFieldNames ? '' : 'bbrInfoBwHi',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1903, _omitFieldNames ? '' : 'bbrInfoMinRtt',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1904, _omitFieldNames ? '' : 'bbrInfoPacingGain',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(1905, _omitFieldNames ? '' : 'bbrInfoCwndGain',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(2001, _omitFieldNames ? '' : 'classId', fieldType: $pb.PbFieldType.OU3)
    ..aI(2002, _omitFieldNames ? '' : 'sockOpt', fieldType: $pb.PbFieldType.OU3)
    ..a<$fixnum.Int64>(
        2103, _omitFieldNames ? '' : 'cGroup', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  XtcpFlatRecord clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  XtcpFlatRecord copyWith(void Function(XtcpFlatRecord) updates) =>
      super.copyWith((message) => updates(message as XtcpFlatRecord))
          as XtcpFlatRecord;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static XtcpFlatRecord create() => XtcpFlatRecord._();
  @$core.override
  XtcpFlatRecord createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static XtcpFlatRecord getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<XtcpFlatRecord>(create);
  static XtcpFlatRecord? _defaultInstance;

  /// ---- metadata: record format provenance (1-2) ----------------------------
  /// Record format epoch. Stamped unconditionally into every record so consumers
  /// can route records to per-version tables and migrate/aggregate across them.
  /// 0 = pre-versioning daemons (this field absent on the wire → proto3 zero
  /// default), which acts as the "legacy" bucket. Bump the daemon-side constant
  /// (XtcpFlatRecordSchemaVersion) whenever the format changes meaningfully.
  @$pb.TagNumber(1)
  $core.int get schemaVersion => $_getIZ(0);
  @$pb.TagNumber(1)
  set schemaVersion($core.int value) => $_setUnsignedInt32(0, value);
  @$pb.TagNumber(1)
  $core.bool hasSchemaVersion() => $_has(0);
  @$pb.TagNumber(1)
  void clearSchemaVersion() => $_clearField(1);

  /// Daemon build provenance (git commit / build date / version, from -ldflags).
  /// Informational only — for debugging which binary produced a row; NOT used for
  /// routing (that is schema_version). Empty when built without ldflags.
  @$pb.TagNumber(2)
  $core.String get daemonVersion => $_getSZ(1);
  @$pb.TagNumber(2)
  set daemonVersion($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasDaemonVersion() => $_has(1);
  @$pb.TagNumber(2)
  void clearDaemonVersion() => $_clearField(2);

  /// ---- metadata: time (10) -------------------------------------------------
  @$pb.TagNumber(10)
  $fixnum.Int64 get timestampNs => $_getI64(2);
  @$pb.TagNumber(10)
  set timestampNs($fixnum.Int64 value) => $_setInt64(2, value);
  @$pb.TagNumber(10)
  $core.bool hasTimestampNs() => $_has(2);
  @$pb.TagNumber(10)
  void clearTimestampNs() => $_clearField(10);

  /// ---- metadata: host identity (20s) ---------------------------------------
  @$pb.TagNumber(20)
  $core.String get hostname => $_getSZ(3);
  @$pb.TagNumber(20)
  set hostname($core.String value) => $_setString(3, value);
  @$pb.TagNumber(20)
  $core.bool hasHostname() => $_has(3);
  @$pb.TagNumber(20)
  void clearHostname() => $_clearField(20);

  /// Deployment grouping / facility this daemon runs in — generic across
  /// operators (data center, PoP, region, site, …). Free-form; set via
  /// -location flag or LOCATION env. Constant for every record from one
  /// daemon instance.
  @$pb.TagNumber(21)
  $core.String get location => $_getSZ(4);
  @$pb.TagNumber(21)
  set location($core.String value) => $_setString(4, value);
  @$pb.TagNumber(21)
  $core.bool hasLocation() => $_has(4);
  @$pb.TagNumber(21)
  void clearLocation() => $_clearField(21);

  /// ---- metadata: network namespace identity (30s) --------------------------
  /// network namespace — best-effort human name (bind-mount name, else a
  /// container-derived name, else "netns:[<inode>]"). Not a stable key; use
  /// netns_inode for identity.
  @$pb.TagNumber(30)
  $core.String get netns => $_getSZ(5);
  @$pb.TagNumber(30)
  set netns($core.String value) => $_setString(5, value);
  @$pb.TagNumber(30)
  $core.bool hasNetns() => $_has(5);
  @$pb.TagNumber(30)
  void clearNetns() => $_clearField(30);

  /// network-namespace inode (from /proc/<pid>/ns/net or the netns bind-mount).
  /// The stable identity of the namespace this socket lives in — unique per live
  /// namespace on the host for the daemon's lifetime, including the host/default
  /// namespace (its own self-inode). 0 only when the inode could not be
  /// determined. This is the canonical namespace key (prefer it over nsid).
  @$pb.TagNumber(31)
  $fixnum.Int64 get netnsInode => $_getI64(6);
  @$pb.TagNumber(31)
  set netnsInode($fixnum.Int64 value) => $_setInt64(6, value);
  @$pb.TagNumber(31)
  $core.bool hasNetnsInode() => $_has(6);
  @$pb.TagNumber(31)
  void clearNetnsInode() => $_clearField(31);

  /// Kernel NETNSA_NSID for the namespace (best-effort, via RTM_GETNSID). Usually
  /// 0/unset for Docker/containerd namespaces unless assigned (e.g. ip netns
  /// set-id). Populated only when -populateNsid is enabled; netns_inode is the
  /// real stable key.
  @$pb.TagNumber(32)
  $core.int get nsid => $_getIZ(7);
  @$pb.TagNumber(32)
  set nsid($core.int value) => $_setUnsignedInt32(7, value);
  @$pb.TagNumber(32)
  $core.bool hasNsid() => $_has(7);
  @$pb.TagNumber(32)
  void clearNsid() => $_clearField(32);

  /// ---- metadata: container identity (40s) ----------------------------------
  /// Container that owns the socket. Resolved by joining the socket's owning
  /// netns inode against the Docker Engine API index (best-effort, over
  /// /run/docker.sock), falling back to the per-socket cgroup v2 id when the
  /// Docker index misses. Empty for host-namespace sockets or when enrichment
  /// is disabled/unavailable.
  @$pb.TagNumber(40)
  $core.String get containerId => $_getSZ(8);
  @$pb.TagNumber(40)
  set containerId($core.String value) => $_setString(8, value);
  @$pb.TagNumber(40)
  $core.bool hasContainerId() => $_has(8);
  @$pb.TagNumber(40)
  void clearContainerId() => $_clearField(40);

  /// Container runtime, e.g. "docker", "containerd", "crio". Empty when
  /// container_id is empty.
  @$pb.TagNumber(41)
  $core.String get containerRuntime => $_getSZ(9);
  @$pb.TagNumber(41)
  set containerRuntime($core.String value) => $_setString(9, value);
  @$pb.TagNumber(41)
  $core.bool hasContainerRuntime() => $_has(9);
  @$pb.TagNumber(41)
  void clearContainerRuntime() => $_clearField(41);

  /// Container name (Docker Engine API, best-effort). Empty when unknown.
  @$pb.TagNumber(42)
  $core.String get containerName => $_getSZ(10);
  @$pb.TagNumber(42)
  set containerName($core.String value) => $_setString(10, value);
  @$pb.TagNumber(42)
  $core.bool hasContainerName() => $_has(10);
  @$pb.TagNumber(42)
  void clearContainerName() => $_clearField(42);

  /// Container image (Docker Engine API, best-effort). Empty when unknown.
  @$pb.TagNumber(43)
  $core.String get containerImage => $_getSZ(11);
  @$pb.TagNumber(43)
  set containerImage($core.String value) => $_setString(11, value);
  @$pb.TagNumber(43)
  $core.bool hasContainerImage() => $_has(11);
  @$pb.TagNumber(43)
  void clearContainerImage() => $_clearField(43);

  /// ---- metadata: free-form labels (50s) ------------------------------------
  @$pb.TagNumber(50)
  $core.String get label => $_getSZ(12);
  @$pb.TagNumber(50)
  set label($core.String value) => $_setString(12, value);
  @$pb.TagNumber(50)
  $core.bool hasLabel() => $_has(12);
  @$pb.TagNumber(50)
  void clearLabel() => $_clearField(50);

  @$pb.TagNumber(51)
  $core.String get tag => $_getSZ(13);
  @$pb.TagNumber(51)
  set tag($core.String value) => $_setString(13, value);
  @$pb.TagNumber(51)
  $core.bool hasTag() => $_has(13);
  @$pb.TagNumber(51)
  void clearTag() => $_clearField(51);

  /// ---- metadata: record bookkeeping (60s) ----------------------------------
  @$pb.TagNumber(60)
  $fixnum.Int64 get recordCounter => $_getI64(14);
  @$pb.TagNumber(60)
  set recordCounter($fixnum.Int64 value) => $_setInt64(14, value);
  @$pb.TagNumber(60)
  $core.bool hasRecordCounter() => $_has(14);
  @$pb.TagNumber(60)
  void clearRecordCounter() => $_clearField(60);

  @$pb.TagNumber(61)
  $fixnum.Int64 get socketFd => $_getI64(15);
  @$pb.TagNumber(61)
  set socketFd($fixnum.Int64 value) => $_setInt64(15, value);
  @$pb.TagNumber(61)
  $core.bool hasSocketFd() => $_has(15);
  @$pb.TagNumber(61)
  void clearSocketFd() => $_clearField(61);

  @$pb.TagNumber(62)
  $fixnum.Int64 get netlinkerId => $_getI64(16);
  @$pb.TagNumber(62)
  set netlinkerId($fixnum.Int64 value) => $_setInt64(16, value);
  @$pb.TagNumber(62)
  $core.bool hasNetlinkerId() => $_has(16);
  @$pb.TagNumber(62)
  void clearNetlinkerId() => $_clearField(62);

  /// ---- metadata: host network topology, uplink slot 1 (100s) ---------------
  /// Static per boot; captured once at startup (best-effort). Hosts are
  /// dual-homed, so there are two fixed uplink slots. All values repeat on every
  /// record and dictionary-compress to ~nothing. NIC info: sysfs + ethtool
  /// ioctl. LLDP: lldpd control socket (/run/lldpd.socket).
  @$pb.TagNumber(100)
  $core.String get uplink1Ifname => $_getSZ(17);
  @$pb.TagNumber(100)
  set uplink1Ifname($core.String value) => $_setString(17, value);
  @$pb.TagNumber(100)
  $core.bool hasUplink1Ifname() => $_has(17);
  @$pb.TagNumber(100)
  void clearUplink1Ifname() => $_clearField(100);

  @$pb.TagNumber(101)
  $core.String get uplink1NicDriver => $_getSZ(18);
  @$pb.TagNumber(101)
  set uplink1NicDriver($core.String value) => $_setString(18, value);
  @$pb.TagNumber(101)
  $core.bool hasUplink1NicDriver() => $_has(18);
  @$pb.TagNumber(101)
  void clearUplink1NicDriver() => $_clearField(101);

  @$pb.TagNumber(102)
  $core.String get uplink1NicModel => $_getSZ(19);
  @$pb.TagNumber(102)
  set uplink1NicModel($core.String value) => $_setString(19, value);
  @$pb.TagNumber(102)
  $core.bool hasUplink1NicModel() => $_has(19);
  @$pb.TagNumber(102)
  void clearUplink1NicModel() => $_clearField(102);

  @$pb.TagNumber(103)
  $core.int get uplink1NicPciVendor => $_getIZ(20);
  @$pb.TagNumber(103)
  set uplink1NicPciVendor($core.int value) => $_setUnsignedInt32(20, value);
  @$pb.TagNumber(103)
  $core.bool hasUplink1NicPciVendor() => $_has(20);
  @$pb.TagNumber(103)
  void clearUplink1NicPciVendor() => $_clearField(103);

  @$pb.TagNumber(104)
  $core.int get uplink1NicPciDevice => $_getIZ(21);
  @$pb.TagNumber(104)
  set uplink1NicPciDevice($core.int value) => $_setUnsignedInt32(21, value);
  @$pb.TagNumber(104)
  $core.bool hasUplink1NicPciDevice() => $_has(21);
  @$pb.TagNumber(104)
  void clearUplink1NicPciDevice() => $_clearField(104);

  @$pb.TagNumber(105)
  $core.String get uplink1NicBusInfo => $_getSZ(22);
  @$pb.TagNumber(105)
  set uplink1NicBusInfo($core.String value) => $_setString(22, value);
  @$pb.TagNumber(105)
  $core.bool hasUplink1NicBusInfo() => $_has(22);
  @$pb.TagNumber(105)
  void clearUplink1NicBusInfo() => $_clearField(105);

  @$pb.TagNumber(106)
  $core.int get uplink1NicSpeedMbps => $_getIZ(23);
  @$pb.TagNumber(106)
  set uplink1NicSpeedMbps($core.int value) => $_setUnsignedInt32(23, value);
  @$pb.TagNumber(106)
  $core.bool hasUplink1NicSpeedMbps() => $_has(23);
  @$pb.TagNumber(106)
  void clearUplink1NicSpeedMbps() => $_clearField(106);

  @$pb.TagNumber(107)
  $core.String get uplink1NicFwVersion => $_getSZ(24);
  @$pb.TagNumber(107)
  set uplink1NicFwVersion($core.String value) => $_setString(24, value);
  @$pb.TagNumber(107)
  $core.bool hasUplink1NicFwVersion() => $_has(24);
  @$pb.TagNumber(107)
  void clearUplink1NicFwVersion() => $_clearField(107);

  @$pb.TagNumber(120)
  $core.String get uplink1LldpChassisName => $_getSZ(25);
  @$pb.TagNumber(120)
  set uplink1LldpChassisName($core.String value) => $_setString(25, value);
  @$pb.TagNumber(120)
  $core.bool hasUplink1LldpChassisName() => $_has(25);
  @$pb.TagNumber(120)
  void clearUplink1LldpChassisName() => $_clearField(120);

  @$pb.TagNumber(121)
  $core.String get uplink1LldpChassisId => $_getSZ(26);
  @$pb.TagNumber(121)
  set uplink1LldpChassisId($core.String value) => $_setString(26, value);
  @$pb.TagNumber(121)
  $core.bool hasUplink1LldpChassisId() => $_has(26);
  @$pb.TagNumber(121)
  void clearUplink1LldpChassisId() => $_clearField(121);

  @$pb.TagNumber(122)
  $core.String get uplink1LldpMgmtIp => $_getSZ(27);
  @$pb.TagNumber(122)
  set uplink1LldpMgmtIp($core.String value) => $_setString(27, value);
  @$pb.TagNumber(122)
  $core.bool hasUplink1LldpMgmtIp() => $_has(27);
  @$pb.TagNumber(122)
  void clearUplink1LldpMgmtIp() => $_clearField(122);

  @$pb.TagNumber(123)
  $core.String get uplink1LldpPortId => $_getSZ(28);
  @$pb.TagNumber(123)
  set uplink1LldpPortId($core.String value) => $_setString(28, value);
  @$pb.TagNumber(123)
  $core.bool hasUplink1LldpPortId() => $_has(28);
  @$pb.TagNumber(123)
  void clearUplink1LldpPortId() => $_clearField(123);

  @$pb.TagNumber(124)
  $core.String get uplink1LldpPortDescr => $_getSZ(29);
  @$pb.TagNumber(124)
  set uplink1LldpPortDescr($core.String value) => $_setString(29, value);
  @$pb.TagNumber(124)
  $core.bool hasUplink1LldpPortDescr() => $_has(29);
  @$pb.TagNumber(124)
  void clearUplink1LldpPortDescr() => $_clearField(124);

  /// ---- metadata: host network topology, uplink slot 2 (200s) ---------------
  @$pb.TagNumber(200)
  $core.String get uplink2Ifname => $_getSZ(30);
  @$pb.TagNumber(200)
  set uplink2Ifname($core.String value) => $_setString(30, value);
  @$pb.TagNumber(200)
  $core.bool hasUplink2Ifname() => $_has(30);
  @$pb.TagNumber(200)
  void clearUplink2Ifname() => $_clearField(200);

  @$pb.TagNumber(201)
  $core.String get uplink2NicDriver => $_getSZ(31);
  @$pb.TagNumber(201)
  set uplink2NicDriver($core.String value) => $_setString(31, value);
  @$pb.TagNumber(201)
  $core.bool hasUplink2NicDriver() => $_has(31);
  @$pb.TagNumber(201)
  void clearUplink2NicDriver() => $_clearField(201);

  @$pb.TagNumber(202)
  $core.String get uplink2NicModel => $_getSZ(32);
  @$pb.TagNumber(202)
  set uplink2NicModel($core.String value) => $_setString(32, value);
  @$pb.TagNumber(202)
  $core.bool hasUplink2NicModel() => $_has(32);
  @$pb.TagNumber(202)
  void clearUplink2NicModel() => $_clearField(202);

  @$pb.TagNumber(203)
  $core.int get uplink2NicPciVendor => $_getIZ(33);
  @$pb.TagNumber(203)
  set uplink2NicPciVendor($core.int value) => $_setUnsignedInt32(33, value);
  @$pb.TagNumber(203)
  $core.bool hasUplink2NicPciVendor() => $_has(33);
  @$pb.TagNumber(203)
  void clearUplink2NicPciVendor() => $_clearField(203);

  @$pb.TagNumber(204)
  $core.int get uplink2NicPciDevice => $_getIZ(34);
  @$pb.TagNumber(204)
  set uplink2NicPciDevice($core.int value) => $_setUnsignedInt32(34, value);
  @$pb.TagNumber(204)
  $core.bool hasUplink2NicPciDevice() => $_has(34);
  @$pb.TagNumber(204)
  void clearUplink2NicPciDevice() => $_clearField(204);

  @$pb.TagNumber(205)
  $core.String get uplink2NicBusInfo => $_getSZ(35);
  @$pb.TagNumber(205)
  set uplink2NicBusInfo($core.String value) => $_setString(35, value);
  @$pb.TagNumber(205)
  $core.bool hasUplink2NicBusInfo() => $_has(35);
  @$pb.TagNumber(205)
  void clearUplink2NicBusInfo() => $_clearField(205);

  @$pb.TagNumber(206)
  $core.int get uplink2NicSpeedMbps => $_getIZ(36);
  @$pb.TagNumber(206)
  set uplink2NicSpeedMbps($core.int value) => $_setUnsignedInt32(36, value);
  @$pb.TagNumber(206)
  $core.bool hasUplink2NicSpeedMbps() => $_has(36);
  @$pb.TagNumber(206)
  void clearUplink2NicSpeedMbps() => $_clearField(206);

  @$pb.TagNumber(207)
  $core.String get uplink2NicFwVersion => $_getSZ(37);
  @$pb.TagNumber(207)
  set uplink2NicFwVersion($core.String value) => $_setString(37, value);
  @$pb.TagNumber(207)
  $core.bool hasUplink2NicFwVersion() => $_has(37);
  @$pb.TagNumber(207)
  void clearUplink2NicFwVersion() => $_clearField(207);

  @$pb.TagNumber(220)
  $core.String get uplink2LldpChassisName => $_getSZ(38);
  @$pb.TagNumber(220)
  set uplink2LldpChassisName($core.String value) => $_setString(38, value);
  @$pb.TagNumber(220)
  $core.bool hasUplink2LldpChassisName() => $_has(38);
  @$pb.TagNumber(220)
  void clearUplink2LldpChassisName() => $_clearField(220);

  @$pb.TagNumber(221)
  $core.String get uplink2LldpChassisId => $_getSZ(39);
  @$pb.TagNumber(221)
  set uplink2LldpChassisId($core.String value) => $_setString(39, value);
  @$pb.TagNumber(221)
  $core.bool hasUplink2LldpChassisId() => $_has(39);
  @$pb.TagNumber(221)
  void clearUplink2LldpChassisId() => $_clearField(221);

  @$pb.TagNumber(222)
  $core.String get uplink2LldpMgmtIp => $_getSZ(40);
  @$pb.TagNumber(222)
  set uplink2LldpMgmtIp($core.String value) => $_setString(40, value);
  @$pb.TagNumber(222)
  $core.bool hasUplink2LldpMgmtIp() => $_has(40);
  @$pb.TagNumber(222)
  void clearUplink2LldpMgmtIp() => $_clearField(222);

  @$pb.TagNumber(223)
  $core.String get uplink2LldpPortId => $_getSZ(41);
  @$pb.TagNumber(223)
  set uplink2LldpPortId($core.String value) => $_setString(41, value);
  @$pb.TagNumber(223)
  $core.bool hasUplink2LldpPortId() => $_has(41);
  @$pb.TagNumber(223)
  void clearUplink2LldpPortId() => $_clearField(223);

  @$pb.TagNumber(224)
  $core.String get uplink2LldpPortDescr => $_getSZ(42);
  @$pb.TagNumber(224)
  set uplink2LldpPortDescr($core.String value) => $_setString(42, value);
  @$pb.TagNumber(224)
  $core.bool hasUplink2LldpPortDescr() => $_has(42);
  @$pb.TagNumber(224)
  void clearUplink2LldpPortDescr() => $_clearField(224);

  @$pb.TagNumber(1001)
  $core.int get inetDiagMsgFamily => $_getIZ(43);
  @$pb.TagNumber(1001)
  set inetDiagMsgFamily($core.int value) => $_setUnsignedInt32(43, value);
  @$pb.TagNumber(1001)
  $core.bool hasInetDiagMsgFamily() => $_has(43);
  @$pb.TagNumber(1001)
  void clearInetDiagMsgFamily() => $_clearField(1001);

  @$pb.TagNumber(1002)
  $core.int get inetDiagMsgState => $_getIZ(44);
  @$pb.TagNumber(1002)
  set inetDiagMsgState($core.int value) => $_setUnsignedInt32(44, value);
  @$pb.TagNumber(1002)
  $core.bool hasInetDiagMsgState() => $_has(44);
  @$pb.TagNumber(1002)
  void clearInetDiagMsgState() => $_clearField(1002);

  @$pb.TagNumber(1003)
  $core.int get inetDiagMsgTimer => $_getIZ(45);
  @$pb.TagNumber(1003)
  set inetDiagMsgTimer($core.int value) => $_setUnsignedInt32(45, value);
  @$pb.TagNumber(1003)
  $core.bool hasInetDiagMsgTimer() => $_has(45);
  @$pb.TagNumber(1003)
  void clearInetDiagMsgTimer() => $_clearField(1003);

  @$pb.TagNumber(1004)
  $core.int get inetDiagMsgRetrans => $_getIZ(46);
  @$pb.TagNumber(1004)
  set inetDiagMsgRetrans($core.int value) => $_setUnsignedInt32(46, value);
  @$pb.TagNumber(1004)
  $core.bool hasInetDiagMsgRetrans() => $_has(46);
  @$pb.TagNumber(1004)
  void clearInetDiagMsgRetrans() => $_clearField(1004);

  @$pb.TagNumber(1005)
  $core.int get inetDiagMsgSocketSourcePort => $_getIZ(47);
  @$pb.TagNumber(1005)
  set inetDiagMsgSocketSourcePort($core.int value) =>
      $_setUnsignedInt32(47, value);
  @$pb.TagNumber(1005)
  $core.bool hasInetDiagMsgSocketSourcePort() => $_has(47);
  @$pb.TagNumber(1005)
  void clearInetDiagMsgSocketSourcePort() => $_clearField(1005);

  @$pb.TagNumber(1006)
  $core.int get inetDiagMsgSocketDestinationPort => $_getIZ(48);
  @$pb.TagNumber(1006)
  set inetDiagMsgSocketDestinationPort($core.int value) =>
      $_setUnsignedInt32(48, value);
  @$pb.TagNumber(1006)
  $core.bool hasInetDiagMsgSocketDestinationPort() => $_has(48);
  @$pb.TagNumber(1006)
  void clearInetDiagMsgSocketDestinationPort() => $_clearField(1006);

  @$pb.TagNumber(1007)
  $core.List<$core.int> get inetDiagMsgSocketSource => $_getN(49);
  @$pb.TagNumber(1007)
  set inetDiagMsgSocketSource($core.List<$core.int> value) =>
      $_setBytes(49, value);
  @$pb.TagNumber(1007)
  $core.bool hasInetDiagMsgSocketSource() => $_has(49);
  @$pb.TagNumber(1007)
  void clearInetDiagMsgSocketSource() => $_clearField(1007);

  @$pb.TagNumber(1008)
  $core.List<$core.int> get inetDiagMsgSocketDestination => $_getN(50);
  @$pb.TagNumber(1008)
  set inetDiagMsgSocketDestination($core.List<$core.int> value) =>
      $_setBytes(50, value);
  @$pb.TagNumber(1008)
  $core.bool hasInetDiagMsgSocketDestination() => $_has(50);
  @$pb.TagNumber(1008)
  void clearInetDiagMsgSocketDestination() => $_clearField(1008);

  @$pb.TagNumber(1009)
  $core.int get inetDiagMsgSocketInterface => $_getIZ(51);
  @$pb.TagNumber(1009)
  set inetDiagMsgSocketInterface($core.int value) =>
      $_setUnsignedInt32(51, value);
  @$pb.TagNumber(1009)
  $core.bool hasInetDiagMsgSocketInterface() => $_has(51);
  @$pb.TagNumber(1009)
  void clearInetDiagMsgSocketInterface() => $_clearField(1009);

  @$pb.TagNumber(1010)
  $fixnum.Int64 get inetDiagMsgSocketCookie => $_getI64(52);
  @$pb.TagNumber(1010)
  set inetDiagMsgSocketCookie($fixnum.Int64 value) => $_setInt64(52, value);
  @$pb.TagNumber(1010)
  $core.bool hasInetDiagMsgSocketCookie() => $_has(52);
  @$pb.TagNumber(1010)
  void clearInetDiagMsgSocketCookie() => $_clearField(1010);

  @$pb.TagNumber(1011)
  $fixnum.Int64 get inetDiagMsgSocketDestAsn => $_getI64(53);
  @$pb.TagNumber(1011)
  set inetDiagMsgSocketDestAsn($fixnum.Int64 value) => $_setInt64(53, value);
  @$pb.TagNumber(1011)
  $core.bool hasInetDiagMsgSocketDestAsn() => $_has(53);
  @$pb.TagNumber(1011)
  void clearInetDiagMsgSocketDestAsn() => $_clearField(1011);

  @$pb.TagNumber(1012)
  $fixnum.Int64 get inetDiagMsgSocketNextHopAsn => $_getI64(54);
  @$pb.TagNumber(1012)
  set inetDiagMsgSocketNextHopAsn($fixnum.Int64 value) => $_setInt64(54, value);
  @$pb.TagNumber(1012)
  $core.bool hasInetDiagMsgSocketNextHopAsn() => $_has(54);
  @$pb.TagNumber(1012)
  void clearInetDiagMsgSocketNextHopAsn() => $_clearField(1012);

  @$pb.TagNumber(1013)
  $core.int get inetDiagMsgExpires => $_getIZ(55);
  @$pb.TagNumber(1013)
  set inetDiagMsgExpires($core.int value) => $_setUnsignedInt32(55, value);
  @$pb.TagNumber(1013)
  $core.bool hasInetDiagMsgExpires() => $_has(55);
  @$pb.TagNumber(1013)
  void clearInetDiagMsgExpires() => $_clearField(1013);

  @$pb.TagNumber(1014)
  $core.int get inetDiagMsgRqueue => $_getIZ(56);
  @$pb.TagNumber(1014)
  set inetDiagMsgRqueue($core.int value) => $_setUnsignedInt32(56, value);
  @$pb.TagNumber(1014)
  $core.bool hasInetDiagMsgRqueue() => $_has(56);
  @$pb.TagNumber(1014)
  void clearInetDiagMsgRqueue() => $_clearField(1014);

  @$pb.TagNumber(1015)
  $core.int get inetDiagMsgWqueue => $_getIZ(57);
  @$pb.TagNumber(1015)
  set inetDiagMsgWqueue($core.int value) => $_setUnsignedInt32(57, value);
  @$pb.TagNumber(1015)
  $core.bool hasInetDiagMsgWqueue() => $_has(57);
  @$pb.TagNumber(1015)
  void clearInetDiagMsgWqueue() => $_clearField(1015);

  @$pb.TagNumber(1016)
  $core.int get inetDiagMsgUid => $_getIZ(58);
  @$pb.TagNumber(1016)
  set inetDiagMsgUid($core.int value) => $_setUnsignedInt32(58, value);
  @$pb.TagNumber(1016)
  $core.bool hasInetDiagMsgUid() => $_has(58);
  @$pb.TagNumber(1016)
  void clearInetDiagMsgUid() => $_clearField(1016);

  @$pb.TagNumber(1017)
  $core.int get inetDiagMsgInode => $_getIZ(59);
  @$pb.TagNumber(1017)
  set inetDiagMsgInode($core.int value) => $_setUnsignedInt32(59, value);
  @$pb.TagNumber(1017)
  $core.bool hasInetDiagMsgInode() => $_has(59);
  @$pb.TagNumber(1017)
  void clearInetDiagMsgInode() => $_clearField(1017);

  /// DEPRECATED: mem_info duplicates sk_mem_info value-for-value and is off by
  /// default (the daemon no longer requests INET_DIAG_MEMINFO from the kernel),
  /// so these ship as 0 on current records. The same values live in sk_mem_info:
  ///   mem_info_rmem == sk_mem_info_rmem_alloc  (1501)
  ///   mem_info_wmem == sk_mem_info_wmem_queued (1506)
  ///   mem_info_fmem == sk_mem_info_fwd_alloc   (1505)
  ///   mem_info_tmem == sk_mem_info_wmem_alloc  (1503)
  /// Field numbers retained (never reused); enable with `-deserializers all`.
  /// (Not marked `[deprecated = true]` so the still-supported opt-in decode path
  /// and tests don't trip staticcheck SA1019.)
  @$pb.TagNumber(1101)
  $core.int get memInfoRmem => $_getIZ(60);
  @$pb.TagNumber(1101)
  set memInfoRmem($core.int value) => $_setUnsignedInt32(60, value);
  @$pb.TagNumber(1101)
  $core.bool hasMemInfoRmem() => $_has(60);
  @$pb.TagNumber(1101)
  void clearMemInfoRmem() => $_clearField(1101);

  @$pb.TagNumber(1102)
  $core.int get memInfoWmem => $_getIZ(61);
  @$pb.TagNumber(1102)
  set memInfoWmem($core.int value) => $_setUnsignedInt32(61, value);
  @$pb.TagNumber(1102)
  $core.bool hasMemInfoWmem() => $_has(61);
  @$pb.TagNumber(1102)
  void clearMemInfoWmem() => $_clearField(1102);

  @$pb.TagNumber(1103)
  $core.int get memInfoFmem => $_getIZ(62);
  @$pb.TagNumber(1103)
  set memInfoFmem($core.int value) => $_setUnsignedInt32(62, value);
  @$pb.TagNumber(1103)
  $core.bool hasMemInfoFmem() => $_has(62);
  @$pb.TagNumber(1103)
  void clearMemInfoFmem() => $_clearField(1103);

  @$pb.TagNumber(1104)
  $core.int get memInfoTmem => $_getIZ(63);
  @$pb.TagNumber(1104)
  set memInfoTmem($core.int value) => $_setUnsignedInt32(63, value);
  @$pb.TagNumber(1104)
  $core.bool hasMemInfoTmem() => $_has(63);
  @$pb.TagNumber(1104)
  void clearMemInfoTmem() => $_clearField(1104);

  @$pb.TagNumber(1201)
  $core.int get tcpInfoState => $_getIZ(64);
  @$pb.TagNumber(1201)
  set tcpInfoState($core.int value) => $_setUnsignedInt32(64, value);
  @$pb.TagNumber(1201)
  $core.bool hasTcpInfoState() => $_has(64);
  @$pb.TagNumber(1201)
  void clearTcpInfoState() => $_clearField(1201);

  @$pb.TagNumber(1202)
  $core.int get tcpInfoCaState => $_getIZ(65);
  @$pb.TagNumber(1202)
  set tcpInfoCaState($core.int value) => $_setUnsignedInt32(65, value);
  @$pb.TagNumber(1202)
  $core.bool hasTcpInfoCaState() => $_has(65);
  @$pb.TagNumber(1202)
  void clearTcpInfoCaState() => $_clearField(1202);

  @$pb.TagNumber(1203)
  $core.int get tcpInfoRetransmits => $_getIZ(66);
  @$pb.TagNumber(1203)
  set tcpInfoRetransmits($core.int value) => $_setUnsignedInt32(66, value);
  @$pb.TagNumber(1203)
  $core.bool hasTcpInfoRetransmits() => $_has(66);
  @$pb.TagNumber(1203)
  void clearTcpInfoRetransmits() => $_clearField(1203);

  @$pb.TagNumber(1204)
  $core.int get tcpInfoProbes => $_getIZ(67);
  @$pb.TagNumber(1204)
  set tcpInfoProbes($core.int value) => $_setUnsignedInt32(67, value);
  @$pb.TagNumber(1204)
  $core.bool hasTcpInfoProbes() => $_has(67);
  @$pb.TagNumber(1204)
  void clearTcpInfoProbes() => $_clearField(1204);

  @$pb.TagNumber(1205)
  $core.int get tcpInfoBackoff => $_getIZ(68);
  @$pb.TagNumber(1205)
  set tcpInfoBackoff($core.int value) => $_setUnsignedInt32(68, value);
  @$pb.TagNumber(1205)
  $core.bool hasTcpInfoBackoff() => $_has(68);
  @$pb.TagNumber(1205)
  void clearTcpInfoBackoff() => $_clearField(1205);

  @$pb.TagNumber(1206)
  $core.int get tcpInfoOptions => $_getIZ(69);
  @$pb.TagNumber(1206)
  set tcpInfoOptions($core.int value) => $_setUnsignedInt32(69, value);
  @$pb.TagNumber(1206)
  $core.bool hasTcpInfoOptions() => $_has(69);
  @$pb.TagNumber(1206)
  void clearTcpInfoOptions() => $_clearField(1206);

  /// 	__u8	_snd_wscale : 4, _rcv_wscale : 4;
  /// 	__u8	_delivery_rate_app_limited:1, _fastopen_client_fail:2;
  @$pb.TagNumber(1207)
  $core.int get tcpInfoSendScale => $_getIZ(70);
  @$pb.TagNumber(1207)
  set tcpInfoSendScale($core.int value) => $_setUnsignedInt32(70, value);
  @$pb.TagNumber(1207)
  $core.bool hasTcpInfoSendScale() => $_has(70);
  @$pb.TagNumber(1207)
  void clearTcpInfoSendScale() => $_clearField(1207);

  @$pb.TagNumber(1208)
  $core.int get tcpInfoRcvScale => $_getIZ(71);
  @$pb.TagNumber(1208)
  set tcpInfoRcvScale($core.int value) => $_setUnsignedInt32(71, value);
  @$pb.TagNumber(1208)
  $core.bool hasTcpInfoRcvScale() => $_has(71);
  @$pb.TagNumber(1208)
  void clearTcpInfoRcvScale() => $_clearField(1208);

  @$pb.TagNumber(1209)
  $core.int get tcpInfoDeliveryRateAppLimited => $_getIZ(72);
  @$pb.TagNumber(1209)
  set tcpInfoDeliveryRateAppLimited($core.int value) =>
      $_setUnsignedInt32(72, value);
  @$pb.TagNumber(1209)
  $core.bool hasTcpInfoDeliveryRateAppLimited() => $_has(72);
  @$pb.TagNumber(1209)
  void clearTcpInfoDeliveryRateAppLimited() => $_clearField(1209);

  @$pb.TagNumber(1210)
  $core.int get tcpInfoFastOpenClientFailed => $_getIZ(73);
  @$pb.TagNumber(1210)
  set tcpInfoFastOpenClientFailed($core.int value) =>
      $_setUnsignedInt32(73, value);
  @$pb.TagNumber(1210)
  $core.bool hasTcpInfoFastOpenClientFailed() => $_has(73);
  @$pb.TagNumber(1210)
  void clearTcpInfoFastOpenClientFailed() => $_clearField(1210);

  @$pb.TagNumber(1215)
  $core.int get tcpInfoRto => $_getIZ(74);
  @$pb.TagNumber(1215)
  set tcpInfoRto($core.int value) => $_setUnsignedInt32(74, value);
  @$pb.TagNumber(1215)
  $core.bool hasTcpInfoRto() => $_has(74);
  @$pb.TagNumber(1215)
  void clearTcpInfoRto() => $_clearField(1215);

  @$pb.TagNumber(1216)
  $core.int get tcpInfoAto => $_getIZ(75);
  @$pb.TagNumber(1216)
  set tcpInfoAto($core.int value) => $_setUnsignedInt32(75, value);
  @$pb.TagNumber(1216)
  $core.bool hasTcpInfoAto() => $_has(75);
  @$pb.TagNumber(1216)
  void clearTcpInfoAto() => $_clearField(1216);

  @$pb.TagNumber(1217)
  $core.int get tcpInfoSndMss => $_getIZ(76);
  @$pb.TagNumber(1217)
  set tcpInfoSndMss($core.int value) => $_setUnsignedInt32(76, value);
  @$pb.TagNumber(1217)
  $core.bool hasTcpInfoSndMss() => $_has(76);
  @$pb.TagNumber(1217)
  void clearTcpInfoSndMss() => $_clearField(1217);

  @$pb.TagNumber(1218)
  $core.int get tcpInfoRcvMss => $_getIZ(77);
  @$pb.TagNumber(1218)
  set tcpInfoRcvMss($core.int value) => $_setUnsignedInt32(77, value);
  @$pb.TagNumber(1218)
  $core.bool hasTcpInfoRcvMss() => $_has(77);
  @$pb.TagNumber(1218)
  void clearTcpInfoRcvMss() => $_clearField(1218);

  @$pb.TagNumber(1219)
  $core.int get tcpInfoUnacked => $_getIZ(78);
  @$pb.TagNumber(1219)
  set tcpInfoUnacked($core.int value) => $_setUnsignedInt32(78, value);
  @$pb.TagNumber(1219)
  $core.bool hasTcpInfoUnacked() => $_has(78);
  @$pb.TagNumber(1219)
  void clearTcpInfoUnacked() => $_clearField(1219);

  @$pb.TagNumber(1220)
  $core.int get tcpInfoSacked => $_getIZ(79);
  @$pb.TagNumber(1220)
  set tcpInfoSacked($core.int value) => $_setUnsignedInt32(79, value);
  @$pb.TagNumber(1220)
  $core.bool hasTcpInfoSacked() => $_has(79);
  @$pb.TagNumber(1220)
  void clearTcpInfoSacked() => $_clearField(1220);

  @$pb.TagNumber(1221)
  $core.int get tcpInfoLost => $_getIZ(80);
  @$pb.TagNumber(1221)
  set tcpInfoLost($core.int value) => $_setUnsignedInt32(80, value);
  @$pb.TagNumber(1221)
  $core.bool hasTcpInfoLost() => $_has(80);
  @$pb.TagNumber(1221)
  void clearTcpInfoLost() => $_clearField(1221);

  @$pb.TagNumber(1222)
  $core.int get tcpInfoRetrans => $_getIZ(81);
  @$pb.TagNumber(1222)
  set tcpInfoRetrans($core.int value) => $_setUnsignedInt32(81, value);
  @$pb.TagNumber(1222)
  $core.bool hasTcpInfoRetrans() => $_has(81);
  @$pb.TagNumber(1222)
  void clearTcpInfoRetrans() => $_clearField(1222);

  @$pb.TagNumber(1223)
  $core.int get tcpInfoFackets => $_getIZ(82);
  @$pb.TagNumber(1223)
  set tcpInfoFackets($core.int value) => $_setUnsignedInt32(82, value);
  @$pb.TagNumber(1223)
  $core.bool hasTcpInfoFackets() => $_has(82);
  @$pb.TagNumber(1223)
  void clearTcpInfoFackets() => $_clearField(1223);

  /// Times
  @$pb.TagNumber(1224)
  $core.int get tcpInfoLastDataSent => $_getIZ(83);
  @$pb.TagNumber(1224)
  set tcpInfoLastDataSent($core.int value) => $_setUnsignedInt32(83, value);
  @$pb.TagNumber(1224)
  $core.bool hasTcpInfoLastDataSent() => $_has(83);
  @$pb.TagNumber(1224)
  void clearTcpInfoLastDataSent() => $_clearField(1224);

  @$pb.TagNumber(1225)
  $core.int get tcpInfoLastAckSent => $_getIZ(84);
  @$pb.TagNumber(1225)
  set tcpInfoLastAckSent($core.int value) => $_setUnsignedInt32(84, value);
  @$pb.TagNumber(1225)
  $core.bool hasTcpInfoLastAckSent() => $_has(84);
  @$pb.TagNumber(1225)
  void clearTcpInfoLastAckSent() => $_clearField(1225);

  @$pb.TagNumber(1226)
  $core.int get tcpInfoLastDataRecv => $_getIZ(85);
  @$pb.TagNumber(1226)
  set tcpInfoLastDataRecv($core.int value) => $_setUnsignedInt32(85, value);
  @$pb.TagNumber(1226)
  $core.bool hasTcpInfoLastDataRecv() => $_has(85);
  @$pb.TagNumber(1226)
  void clearTcpInfoLastDataRecv() => $_clearField(1226);

  @$pb.TagNumber(1227)
  $core.int get tcpInfoLastAckRecv => $_getIZ(86);
  @$pb.TagNumber(1227)
  set tcpInfoLastAckRecv($core.int value) => $_setUnsignedInt32(86, value);
  @$pb.TagNumber(1227)
  $core.bool hasTcpInfoLastAckRecv() => $_has(86);
  @$pb.TagNumber(1227)
  void clearTcpInfoLastAckRecv() => $_clearField(1227);

  /// Metrics
  @$pb.TagNumber(1228)
  $core.int get tcpInfoPmtu => $_getIZ(87);
  @$pb.TagNumber(1228)
  set tcpInfoPmtu($core.int value) => $_setUnsignedInt32(87, value);
  @$pb.TagNumber(1228)
  $core.bool hasTcpInfoPmtu() => $_has(87);
  @$pb.TagNumber(1228)
  void clearTcpInfoPmtu() => $_clearField(1228);

  @$pb.TagNumber(1229)
  $core.int get tcpInfoRcvSsthresh => $_getIZ(88);
  @$pb.TagNumber(1229)
  set tcpInfoRcvSsthresh($core.int value) => $_setUnsignedInt32(88, value);
  @$pb.TagNumber(1229)
  $core.bool hasTcpInfoRcvSsthresh() => $_has(88);
  @$pb.TagNumber(1229)
  void clearTcpInfoRcvSsthresh() => $_clearField(1229);

  @$pb.TagNumber(1230)
  $core.int get tcpInfoRtt => $_getIZ(89);
  @$pb.TagNumber(1230)
  set tcpInfoRtt($core.int value) => $_setUnsignedInt32(89, value);
  @$pb.TagNumber(1230)
  $core.bool hasTcpInfoRtt() => $_has(89);
  @$pb.TagNumber(1230)
  void clearTcpInfoRtt() => $_clearField(1230);

  @$pb.TagNumber(1231)
  $core.int get tcpInfoRttVar => $_getIZ(90);
  @$pb.TagNumber(1231)
  set tcpInfoRttVar($core.int value) => $_setUnsignedInt32(90, value);
  @$pb.TagNumber(1231)
  $core.bool hasTcpInfoRttVar() => $_has(90);
  @$pb.TagNumber(1231)
  void clearTcpInfoRttVar() => $_clearField(1231);

  @$pb.TagNumber(1232)
  $core.int get tcpInfoSndSsthresh => $_getIZ(91);
  @$pb.TagNumber(1232)
  set tcpInfoSndSsthresh($core.int value) => $_setUnsignedInt32(91, value);
  @$pb.TagNumber(1232)
  $core.bool hasTcpInfoSndSsthresh() => $_has(91);
  @$pb.TagNumber(1232)
  void clearTcpInfoSndSsthresh() => $_clearField(1232);

  @$pb.TagNumber(1233)
  $core.int get tcpInfoSndCwnd => $_getIZ(92);
  @$pb.TagNumber(1233)
  set tcpInfoSndCwnd($core.int value) => $_setUnsignedInt32(92, value);
  @$pb.TagNumber(1233)
  $core.bool hasTcpInfoSndCwnd() => $_has(92);
  @$pb.TagNumber(1233)
  void clearTcpInfoSndCwnd() => $_clearField(1233);

  @$pb.TagNumber(1234)
  $core.int get tcpInfoAdvMss => $_getIZ(93);
  @$pb.TagNumber(1234)
  set tcpInfoAdvMss($core.int value) => $_setUnsignedInt32(93, value);
  @$pb.TagNumber(1234)
  $core.bool hasTcpInfoAdvMss() => $_has(93);
  @$pb.TagNumber(1234)
  void clearTcpInfoAdvMss() => $_clearField(1234);

  @$pb.TagNumber(1235)
  $core.int get tcpInfoReordering => $_getIZ(94);
  @$pb.TagNumber(1235)
  set tcpInfoReordering($core.int value) => $_setUnsignedInt32(94, value);
  @$pb.TagNumber(1235)
  $core.bool hasTcpInfoReordering() => $_has(94);
  @$pb.TagNumber(1235)
  void clearTcpInfoReordering() => $_clearField(1235);

  @$pb.TagNumber(1236)
  $core.int get tcpInfoRcvRtt => $_getIZ(95);
  @$pb.TagNumber(1236)
  set tcpInfoRcvRtt($core.int value) => $_setUnsignedInt32(95, value);
  @$pb.TagNumber(1236)
  $core.bool hasTcpInfoRcvRtt() => $_has(95);
  @$pb.TagNumber(1236)
  void clearTcpInfoRcvRtt() => $_clearField(1236);

  @$pb.TagNumber(1237)
  $core.int get tcpInfoRcvSpace => $_getIZ(96);
  @$pb.TagNumber(1237)
  set tcpInfoRcvSpace($core.int value) => $_setUnsignedInt32(96, value);
  @$pb.TagNumber(1237)
  $core.bool hasTcpInfoRcvSpace() => $_has(96);
  @$pb.TagNumber(1237)
  void clearTcpInfoRcvSpace() => $_clearField(1237);

  @$pb.TagNumber(1238)
  $core.int get tcpInfoTotalRetrans => $_getIZ(97);
  @$pb.TagNumber(1238)
  set tcpInfoTotalRetrans($core.int value) => $_setUnsignedInt32(97, value);
  @$pb.TagNumber(1238)
  $core.bool hasTcpInfoTotalRetrans() => $_has(97);
  @$pb.TagNumber(1238)
  void clearTcpInfoTotalRetrans() => $_clearField(1238);

  @$pb.TagNumber(1239)
  $fixnum.Int64 get tcpInfoPacingRate => $_getI64(98);
  @$pb.TagNumber(1239)
  set tcpInfoPacingRate($fixnum.Int64 value) => $_setInt64(98, value);
  @$pb.TagNumber(1239)
  $core.bool hasTcpInfoPacingRate() => $_has(98);
  @$pb.TagNumber(1239)
  void clearTcpInfoPacingRate() => $_clearField(1239);

  @$pb.TagNumber(1240)
  $fixnum.Int64 get tcpInfoMaxPacingRate => $_getI64(99);
  @$pb.TagNumber(1240)
  set tcpInfoMaxPacingRate($fixnum.Int64 value) => $_setInt64(99, value);
  @$pb.TagNumber(1240)
  $core.bool hasTcpInfoMaxPacingRate() => $_has(99);
  @$pb.TagNumber(1240)
  void clearTcpInfoMaxPacingRate() => $_clearField(1240);

  @$pb.TagNumber(1241)
  $fixnum.Int64 get tcpInfoBytesAcked => $_getI64(100);
  @$pb.TagNumber(1241)
  set tcpInfoBytesAcked($fixnum.Int64 value) => $_setInt64(100, value);
  @$pb.TagNumber(1241)
  $core.bool hasTcpInfoBytesAcked() => $_has(100);
  @$pb.TagNumber(1241)
  void clearTcpInfoBytesAcked() => $_clearField(1241);

  @$pb.TagNumber(1242)
  $fixnum.Int64 get tcpInfoBytesReceived => $_getI64(101);
  @$pb.TagNumber(1242)
  set tcpInfoBytesReceived($fixnum.Int64 value) => $_setInt64(101, value);
  @$pb.TagNumber(1242)
  $core.bool hasTcpInfoBytesReceived() => $_has(101);
  @$pb.TagNumber(1242)
  void clearTcpInfoBytesReceived() => $_clearField(1242);

  @$pb.TagNumber(1243)
  $core.int get tcpInfoSegsOut => $_getIZ(102);
  @$pb.TagNumber(1243)
  set tcpInfoSegsOut($core.int value) => $_setUnsignedInt32(102, value);
  @$pb.TagNumber(1243)
  $core.bool hasTcpInfoSegsOut() => $_has(102);
  @$pb.TagNumber(1243)
  void clearTcpInfoSegsOut() => $_clearField(1243);

  @$pb.TagNumber(1244)
  $core.int get tcpInfoSegsIn => $_getIZ(103);
  @$pb.TagNumber(1244)
  set tcpInfoSegsIn($core.int value) => $_setUnsignedInt32(103, value);
  @$pb.TagNumber(1244)
  $core.bool hasTcpInfoSegsIn() => $_has(103);
  @$pb.TagNumber(1244)
  void clearTcpInfoSegsIn() => $_clearField(1244);

  @$pb.TagNumber(1245)
  $core.int get tcpInfoNotSentBytes => $_getIZ(104);
  @$pb.TagNumber(1245)
  set tcpInfoNotSentBytes($core.int value) => $_setUnsignedInt32(104, value);
  @$pb.TagNumber(1245)
  $core.bool hasTcpInfoNotSentBytes() => $_has(104);
  @$pb.TagNumber(1245)
  void clearTcpInfoNotSentBytes() => $_clearField(1245);

  @$pb.TagNumber(1246)
  $core.int get tcpInfoMinRtt => $_getIZ(105);
  @$pb.TagNumber(1246)
  set tcpInfoMinRtt($core.int value) => $_setUnsignedInt32(105, value);
  @$pb.TagNumber(1246)
  $core.bool hasTcpInfoMinRtt() => $_has(105);
  @$pb.TagNumber(1246)
  void clearTcpInfoMinRtt() => $_clearField(1246);

  @$pb.TagNumber(1247)
  $core.int get tcpInfoDataSegsIn => $_getIZ(106);
  @$pb.TagNumber(1247)
  set tcpInfoDataSegsIn($core.int value) => $_setUnsignedInt32(106, value);
  @$pb.TagNumber(1247)
  $core.bool hasTcpInfoDataSegsIn() => $_has(106);
  @$pb.TagNumber(1247)
  void clearTcpInfoDataSegsIn() => $_clearField(1247);

  @$pb.TagNumber(1248)
  $core.int get tcpInfoDataSegsOut => $_getIZ(107);
  @$pb.TagNumber(1248)
  set tcpInfoDataSegsOut($core.int value) => $_setUnsignedInt32(107, value);
  @$pb.TagNumber(1248)
  $core.bool hasTcpInfoDataSegsOut() => $_has(107);
  @$pb.TagNumber(1248)
  void clearTcpInfoDataSegsOut() => $_clearField(1248);

  @$pb.TagNumber(1249)
  $fixnum.Int64 get tcpInfoDeliveryRate => $_getI64(108);
  @$pb.TagNumber(1249)
  set tcpInfoDeliveryRate($fixnum.Int64 value) => $_setInt64(108, value);
  @$pb.TagNumber(1249)
  $core.bool hasTcpInfoDeliveryRate() => $_has(108);
  @$pb.TagNumber(1249)
  void clearTcpInfoDeliveryRate() => $_clearField(1249);

  @$pb.TagNumber(1250)
  $fixnum.Int64 get tcpInfoBusyTime => $_getI64(109);
  @$pb.TagNumber(1250)
  set tcpInfoBusyTime($fixnum.Int64 value) => $_setInt64(109, value);
  @$pb.TagNumber(1250)
  $core.bool hasTcpInfoBusyTime() => $_has(109);
  @$pb.TagNumber(1250)
  void clearTcpInfoBusyTime() => $_clearField(1250);

  @$pb.TagNumber(1251)
  $fixnum.Int64 get tcpInfoRwndLimited => $_getI64(110);
  @$pb.TagNumber(1251)
  set tcpInfoRwndLimited($fixnum.Int64 value) => $_setInt64(110, value);
  @$pb.TagNumber(1251)
  $core.bool hasTcpInfoRwndLimited() => $_has(110);
  @$pb.TagNumber(1251)
  void clearTcpInfoRwndLimited() => $_clearField(1251);

  @$pb.TagNumber(1252)
  $fixnum.Int64 get tcpInfoSndbufLimited => $_getI64(111);
  @$pb.TagNumber(1252)
  set tcpInfoSndbufLimited($fixnum.Int64 value) => $_setInt64(111, value);
  @$pb.TagNumber(1252)
  $core.bool hasTcpInfoSndbufLimited() => $_has(111);
  @$pb.TagNumber(1252)
  void clearTcpInfoSndbufLimited() => $_clearField(1252);

  @$pb.TagNumber(1253)
  $core.int get tcpInfoDelivered => $_getIZ(112);
  @$pb.TagNumber(1253)
  set tcpInfoDelivered($core.int value) => $_setUnsignedInt32(112, value);
  @$pb.TagNumber(1253)
  $core.bool hasTcpInfoDelivered() => $_has(112);
  @$pb.TagNumber(1253)
  void clearTcpInfoDelivered() => $_clearField(1253);

  @$pb.TagNumber(1254)
  $core.int get tcpInfoDeliveredCe => $_getIZ(113);
  @$pb.TagNumber(1254)
  set tcpInfoDeliveredCe($core.int value) => $_setUnsignedInt32(113, value);
  @$pb.TagNumber(1254)
  $core.bool hasTcpInfoDeliveredCe() => $_has(113);
  @$pb.TagNumber(1254)
  void clearTcpInfoDeliveredCe() => $_clearField(1254);

  /// https://tools.ietf.org/html/rfc4898 TCP Extended Statistics MIB
  @$pb.TagNumber(1255)
  $fixnum.Int64 get tcpInfoBytesSent => $_getI64(114);
  @$pb.TagNumber(1255)
  set tcpInfoBytesSent($fixnum.Int64 value) => $_setInt64(114, value);
  @$pb.TagNumber(1255)
  $core.bool hasTcpInfoBytesSent() => $_has(114);
  @$pb.TagNumber(1255)
  void clearTcpInfoBytesSent() => $_clearField(1255);

  @$pb.TagNumber(1256)
  $fixnum.Int64 get tcpInfoBytesRetrans => $_getI64(115);
  @$pb.TagNumber(1256)
  set tcpInfoBytesRetrans($fixnum.Int64 value) => $_setInt64(115, value);
  @$pb.TagNumber(1256)
  $core.bool hasTcpInfoBytesRetrans() => $_has(115);
  @$pb.TagNumber(1256)
  void clearTcpInfoBytesRetrans() => $_clearField(1256);

  @$pb.TagNumber(1257)
  $core.int get tcpInfoDsackDups => $_getIZ(116);
  @$pb.TagNumber(1257)
  set tcpInfoDsackDups($core.int value) => $_setUnsignedInt32(116, value);
  @$pb.TagNumber(1257)
  $core.bool hasTcpInfoDsackDups() => $_has(116);
  @$pb.TagNumber(1257)
  void clearTcpInfoDsackDups() => $_clearField(1257);

  @$pb.TagNumber(1258)
  $core.int get tcpInfoReordSeen => $_getIZ(117);
  @$pb.TagNumber(1258)
  set tcpInfoReordSeen($core.int value) => $_setUnsignedInt32(117, value);
  @$pb.TagNumber(1258)
  $core.bool hasTcpInfoReordSeen() => $_has(117);
  @$pb.TagNumber(1258)
  void clearTcpInfoReordSeen() => $_clearField(1258);

  @$pb.TagNumber(1259)
  $core.int get tcpInfoRcvOoopack => $_getIZ(118);
  @$pb.TagNumber(1259)
  set tcpInfoRcvOoopack($core.int value) => $_setUnsignedInt32(118, value);
  @$pb.TagNumber(1259)
  $core.bool hasTcpInfoRcvOoopack() => $_has(118);
  @$pb.TagNumber(1259)
  void clearTcpInfoRcvOoopack() => $_clearField(1259);

  @$pb.TagNumber(1260)
  $core.int get tcpInfoSndWnd => $_getIZ(119);
  @$pb.TagNumber(1260)
  set tcpInfoSndWnd($core.int value) => $_setUnsignedInt32(119, value);
  @$pb.TagNumber(1260)
  $core.bool hasTcpInfoSndWnd() => $_has(119);
  @$pb.TagNumber(1260)
  void clearTcpInfoSndWnd() => $_clearField(1260);

  @$pb.TagNumber(1261)
  $core.int get tcpInfoRcvWnd => $_getIZ(120);
  @$pb.TagNumber(1261)
  set tcpInfoRcvWnd($core.int value) => $_setUnsignedInt32(120, value);
  @$pb.TagNumber(1261)
  $core.bool hasTcpInfoRcvWnd() => $_has(120);
  @$pb.TagNumber(1261)
  void clearTcpInfoRcvWnd() => $_clearField(1261);

  @$pb.TagNumber(1262)
  $core.int get tcpInfoRehash => $_getIZ(121);
  @$pb.TagNumber(1262)
  set tcpInfoRehash($core.int value) => $_setUnsignedInt32(121, value);
  @$pb.TagNumber(1262)
  $core.bool hasTcpInfoRehash() => $_has(121);
  @$pb.TagNumber(1262)
  void clearTcpInfoRehash() => $_clearField(1262);

  @$pb.TagNumber(1263)
  $core.int get tcpInfoTotalRto => $_getIZ(122);
  @$pb.TagNumber(1263)
  set tcpInfoTotalRto($core.int value) => $_setUnsignedInt32(122, value);
  @$pb.TagNumber(1263)
  $core.bool hasTcpInfoTotalRto() => $_has(122);
  @$pb.TagNumber(1263)
  void clearTcpInfoTotalRto() => $_clearField(1263);

  @$pb.TagNumber(1264)
  $core.int get tcpInfoTotalRtoRecoveries => $_getIZ(123);
  @$pb.TagNumber(1264)
  set tcpInfoTotalRtoRecoveries($core.int value) =>
      $_setUnsignedInt32(123, value);
  @$pb.TagNumber(1264)
  $core.bool hasTcpInfoTotalRtoRecoveries() => $_has(123);
  @$pb.TagNumber(1264)
  void clearTcpInfoTotalRtoRecoveries() => $_clearField(1264);

  @$pb.TagNumber(1265)
  $core.int get tcpInfoTotalRtoTime => $_getIZ(124);
  @$pb.TagNumber(1265)
  set tcpInfoTotalRtoTime($core.int value) => $_setUnsignedInt32(124, value);
  @$pb.TagNumber(1265)
  $core.bool hasTcpInfoTotalRtoTime() => $_has(124);
  @$pb.TagNumber(1265)
  void clearTcpInfoTotalRtoTime() => $_clearField(1265);

  /// Please note it's recommended to use the enum for efficency, but keeping the string
  /// just in case we need to quickly put a different algorithm in without updating the enum.
  /// Obviously it's optional, so it low cost.
  @$pb.TagNumber(1300)
  $core.String get congestionAlgorithmString => $_getSZ(125);
  @$pb.TagNumber(1300)
  set congestionAlgorithmString($core.String value) => $_setString(125, value);
  @$pb.TagNumber(1300)
  $core.bool hasCongestionAlgorithmString() => $_has(125);
  @$pb.TagNumber(1300)
  void clearCongestionAlgorithmString() => $_clearField(1300);

  @$pb.TagNumber(1301)
  XtcpFlatRecord_CongestionAlgorithm get congestionAlgorithmEnum => $_getN(126);
  @$pb.TagNumber(1301)
  set congestionAlgorithmEnum(XtcpFlatRecord_CongestionAlgorithm value) =>
      $_setField(1301, value);
  @$pb.TagNumber(1301)
  $core.bool hasCongestionAlgorithmEnum() => $_has(126);
  @$pb.TagNumber(1301)
  void clearCongestionAlgorithmEnum() => $_clearField(1301);

  @$pb.TagNumber(1401)
  $core.int get typeOfService => $_getIZ(127);
  @$pb.TagNumber(1401)
  set typeOfService($core.int value) => $_setUnsignedInt32(127, value);
  @$pb.TagNumber(1401)
  $core.bool hasTypeOfService() => $_has(127);
  @$pb.TagNumber(1401)
  void clearTypeOfService() => $_clearField(1401);

  @$pb.TagNumber(1402)
  $core.int get trafficClass => $_getIZ(128);
  @$pb.TagNumber(1402)
  set trafficClass($core.int value) => $_setUnsignedInt32(128, value);
  @$pb.TagNumber(1402)
  $core.bool hasTrafficClass() => $_has(128);
  @$pb.TagNumber(1402)
  void clearTrafficClass() => $_clearField(1402);

  @$pb.TagNumber(1501)
  $core.int get skMemInfoRmemAlloc => $_getIZ(129);
  @$pb.TagNumber(1501)
  set skMemInfoRmemAlloc($core.int value) => $_setUnsignedInt32(129, value);
  @$pb.TagNumber(1501)
  $core.bool hasSkMemInfoRmemAlloc() => $_has(129);
  @$pb.TagNumber(1501)
  void clearSkMemInfoRmemAlloc() => $_clearField(1501);

  @$pb.TagNumber(1502)
  $core.int get skMemInfoRcvBuf => $_getIZ(130);
  @$pb.TagNumber(1502)
  set skMemInfoRcvBuf($core.int value) => $_setUnsignedInt32(130, value);
  @$pb.TagNumber(1502)
  $core.bool hasSkMemInfoRcvBuf() => $_has(130);
  @$pb.TagNumber(1502)
  void clearSkMemInfoRcvBuf() => $_clearField(1502);

  @$pb.TagNumber(1503)
  $core.int get skMemInfoWmemAlloc => $_getIZ(131);
  @$pb.TagNumber(1503)
  set skMemInfoWmemAlloc($core.int value) => $_setUnsignedInt32(131, value);
  @$pb.TagNumber(1503)
  $core.bool hasSkMemInfoWmemAlloc() => $_has(131);
  @$pb.TagNumber(1503)
  void clearSkMemInfoWmemAlloc() => $_clearField(1503);

  @$pb.TagNumber(1504)
  $core.int get skMemInfoSndBuf => $_getIZ(132);
  @$pb.TagNumber(1504)
  set skMemInfoSndBuf($core.int value) => $_setUnsignedInt32(132, value);
  @$pb.TagNumber(1504)
  $core.bool hasSkMemInfoSndBuf() => $_has(132);
  @$pb.TagNumber(1504)
  void clearSkMemInfoSndBuf() => $_clearField(1504);

  @$pb.TagNumber(1505)
  $core.int get skMemInfoFwdAlloc => $_getIZ(133);
  @$pb.TagNumber(1505)
  set skMemInfoFwdAlloc($core.int value) => $_setUnsignedInt32(133, value);
  @$pb.TagNumber(1505)
  $core.bool hasSkMemInfoFwdAlloc() => $_has(133);
  @$pb.TagNumber(1505)
  void clearSkMemInfoFwdAlloc() => $_clearField(1505);

  @$pb.TagNumber(1506)
  $core.int get skMemInfoWmemQueued => $_getIZ(134);
  @$pb.TagNumber(1506)
  set skMemInfoWmemQueued($core.int value) => $_setUnsignedInt32(134, value);
  @$pb.TagNumber(1506)
  $core.bool hasSkMemInfoWmemQueued() => $_has(134);
  @$pb.TagNumber(1506)
  void clearSkMemInfoWmemQueued() => $_clearField(1506);

  @$pb.TagNumber(1507)
  $core.int get skMemInfoOptmem => $_getIZ(135);
  @$pb.TagNumber(1507)
  set skMemInfoOptmem($core.int value) => $_setUnsignedInt32(135, value);
  @$pb.TagNumber(1507)
  $core.bool hasSkMemInfoOptmem() => $_has(135);
  @$pb.TagNumber(1507)
  void clearSkMemInfoOptmem() => $_clearField(1507);

  @$pb.TagNumber(1508)
  $core.int get skMemInfoBacklog => $_getIZ(136);
  @$pb.TagNumber(1508)
  set skMemInfoBacklog($core.int value) => $_setUnsignedInt32(136, value);
  @$pb.TagNumber(1508)
  $core.bool hasSkMemInfoBacklog() => $_has(136);
  @$pb.TagNumber(1508)
  void clearSkMemInfoBacklog() => $_clearField(1508);

  @$pb.TagNumber(1509)
  $core.int get skMemInfoDrops => $_getIZ(137);
  @$pb.TagNumber(1509)
  set skMemInfoDrops($core.int value) => $_setUnsignedInt32(137, value);
  @$pb.TagNumber(1509)
  $core.bool hasSkMemInfoDrops() => $_has(137);
  @$pb.TagNumber(1509)
  void clearSkMemInfoDrops() => $_clearField(1509);

  @$pb.TagNumber(1600)
  $core.int get shutdownState => $_getIZ(138);
  @$pb.TagNumber(1600)
  set shutdownState($core.int value) => $_setUnsignedInt32(138, value);
  @$pb.TagNumber(1600)
  $core.bool hasShutdownState() => $_has(138);
  @$pb.TagNumber(1600)
  void clearShutdownState() => $_clearField(1600);

  @$pb.TagNumber(1701)
  $core.int get vegasInfoEnabled => $_getIZ(139);
  @$pb.TagNumber(1701)
  set vegasInfoEnabled($core.int value) => $_setUnsignedInt32(139, value);
  @$pb.TagNumber(1701)
  $core.bool hasVegasInfoEnabled() => $_has(139);
  @$pb.TagNumber(1701)
  void clearVegasInfoEnabled() => $_clearField(1701);

  @$pb.TagNumber(1702)
  $core.int get vegasInfoRttCnt => $_getIZ(140);
  @$pb.TagNumber(1702)
  set vegasInfoRttCnt($core.int value) => $_setUnsignedInt32(140, value);
  @$pb.TagNumber(1702)
  $core.bool hasVegasInfoRttCnt() => $_has(140);
  @$pb.TagNumber(1702)
  void clearVegasInfoRttCnt() => $_clearField(1702);

  @$pb.TagNumber(1703)
  $core.int get vegasInfoRtt => $_getIZ(141);
  @$pb.TagNumber(1703)
  set vegasInfoRtt($core.int value) => $_setUnsignedInt32(141, value);
  @$pb.TagNumber(1703)
  $core.bool hasVegasInfoRtt() => $_has(141);
  @$pb.TagNumber(1703)
  void clearVegasInfoRtt() => $_clearField(1703);

  @$pb.TagNumber(1704)
  $core.int get vegasInfoMinRtt => $_getIZ(142);
  @$pb.TagNumber(1704)
  set vegasInfoMinRtt($core.int value) => $_setUnsignedInt32(142, value);
  @$pb.TagNumber(1704)
  $core.bool hasVegasInfoMinRtt() => $_has(142);
  @$pb.TagNumber(1704)
  void clearVegasInfoMinRtt() => $_clearField(1704);

  @$pb.TagNumber(1801)
  $core.int get dctcpInfoEnabled => $_getIZ(143);
  @$pb.TagNumber(1801)
  set dctcpInfoEnabled($core.int value) => $_setUnsignedInt32(143, value);
  @$pb.TagNumber(1801)
  $core.bool hasDctcpInfoEnabled() => $_has(143);
  @$pb.TagNumber(1801)
  void clearDctcpInfoEnabled() => $_clearField(1801);

  @$pb.TagNumber(1802)
  $core.int get dctcpInfoCeState => $_getIZ(144);
  @$pb.TagNumber(1802)
  set dctcpInfoCeState($core.int value) => $_setUnsignedInt32(144, value);
  @$pb.TagNumber(1802)
  $core.bool hasDctcpInfoCeState() => $_has(144);
  @$pb.TagNumber(1802)
  void clearDctcpInfoCeState() => $_clearField(1802);

  @$pb.TagNumber(1803)
  $core.int get dctcpInfoAlpha => $_getIZ(145);
  @$pb.TagNumber(1803)
  set dctcpInfoAlpha($core.int value) => $_setUnsignedInt32(145, value);
  @$pb.TagNumber(1803)
  $core.bool hasDctcpInfoAlpha() => $_has(145);
  @$pb.TagNumber(1803)
  void clearDctcpInfoAlpha() => $_clearField(1803);

  @$pb.TagNumber(1804)
  $core.int get dctcpInfoAbEcn => $_getIZ(146);
  @$pb.TagNumber(1804)
  set dctcpInfoAbEcn($core.int value) => $_setUnsignedInt32(146, value);
  @$pb.TagNumber(1804)
  $core.bool hasDctcpInfoAbEcn() => $_has(146);
  @$pb.TagNumber(1804)
  void clearDctcpInfoAbEcn() => $_clearField(1804);

  @$pb.TagNumber(1805)
  $core.int get dctcpInfoAbTot => $_getIZ(147);
  @$pb.TagNumber(1805)
  set dctcpInfoAbTot($core.int value) => $_setUnsignedInt32(147, value);
  @$pb.TagNumber(1805)
  $core.bool hasDctcpInfoAbTot() => $_has(147);
  @$pb.TagNumber(1805)
  void clearDctcpInfoAbTot() => $_clearField(1805);

  @$pb.TagNumber(1901)
  $core.int get bbrInfoBwLo => $_getIZ(148);
  @$pb.TagNumber(1901)
  set bbrInfoBwLo($core.int value) => $_setUnsignedInt32(148, value);
  @$pb.TagNumber(1901)
  $core.bool hasBbrInfoBwLo() => $_has(148);
  @$pb.TagNumber(1901)
  void clearBbrInfoBwLo() => $_clearField(1901);

  @$pb.TagNumber(1902)
  $core.int get bbrInfoBwHi => $_getIZ(149);
  @$pb.TagNumber(1902)
  set bbrInfoBwHi($core.int value) => $_setUnsignedInt32(149, value);
  @$pb.TagNumber(1902)
  $core.bool hasBbrInfoBwHi() => $_has(149);
  @$pb.TagNumber(1902)
  void clearBbrInfoBwHi() => $_clearField(1902);

  @$pb.TagNumber(1903)
  $core.int get bbrInfoMinRtt => $_getIZ(150);
  @$pb.TagNumber(1903)
  set bbrInfoMinRtt($core.int value) => $_setUnsignedInt32(150, value);
  @$pb.TagNumber(1903)
  $core.bool hasBbrInfoMinRtt() => $_has(150);
  @$pb.TagNumber(1903)
  void clearBbrInfoMinRtt() => $_clearField(1903);

  @$pb.TagNumber(1904)
  $core.int get bbrInfoPacingGain => $_getIZ(151);
  @$pb.TagNumber(1904)
  set bbrInfoPacingGain($core.int value) => $_setUnsignedInt32(151, value);
  @$pb.TagNumber(1904)
  $core.bool hasBbrInfoPacingGain() => $_has(151);
  @$pb.TagNumber(1904)
  void clearBbrInfoPacingGain() => $_clearField(1904);

  @$pb.TagNumber(1905)
  $core.int get bbrInfoCwndGain => $_getIZ(152);
  @$pb.TagNumber(1905)
  set bbrInfoCwndGain($core.int value) => $_setUnsignedInt32(152, value);
  @$pb.TagNumber(1905)
  $core.bool hasBbrInfoCwndGain() => $_has(152);
  @$pb.TagNumber(1905)
  void clearBbrInfoCwndGain() => $_clearField(1905);

  @$pb.TagNumber(2001)
  $core.int get classId => $_getIZ(153);
  @$pb.TagNumber(2001)
  set classId($core.int value) => $_setUnsignedInt32(153, value);
  @$pb.TagNumber(2001)
  $core.bool hasClassId() => $_has(153);
  @$pb.TagNumber(2001)
  void clearClassId() => $_clearField(2001);

  @$pb.TagNumber(2002)
  $core.int get sockOpt => $_getIZ(154);
  @$pb.TagNumber(2002)
  set sockOpt($core.int value) => $_setUnsignedInt32(154, value);
  @$pb.TagNumber(2002)
  $core.bool hasSockOpt() => $_has(154);
  @$pb.TagNumber(2002)
  void clearSockOpt() => $_clearField(2002);

  @$pb.TagNumber(2103)
  $fixnum.Int64 get cGroup => $_getI64(155);
  @$pb.TagNumber(2103)
  set cGroup($fixnum.Int64 value) => $_setInt64(155, value);
  @$pb.TagNumber(2103)
  $core.bool hasCGroup() => $_has(155);
  @$pb.TagNumber(2103)
  void clearCGroup() => $_clearField(2103);
}

class FlatRecordsRequest extends $pb.GeneratedMessage {
  factory FlatRecordsRequest() => create();

  FlatRecordsRequest._();

  factory FlatRecordsRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory FlatRecordsRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'FlatRecordsRequest',
      package:
          const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_flat_record.v1'),
      createEmptyInstance: create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  FlatRecordsRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  FlatRecordsRequest copyWith(void Function(FlatRecordsRequest) updates) =>
      super.copyWith((message) => updates(message as FlatRecordsRequest))
          as FlatRecordsRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static FlatRecordsRequest create() => FlatRecordsRequest._();
  @$core.override
  FlatRecordsRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static FlatRecordsRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<FlatRecordsRequest>(create);
  static FlatRecordsRequest? _defaultInstance;
}

class FlatRecordsResponse extends $pb.GeneratedMessage {
  factory FlatRecordsResponse({
    XtcpFlatRecord? xtcpFlatRecord,
  }) {
    final result = create();
    if (xtcpFlatRecord != null) result.xtcpFlatRecord = xtcpFlatRecord;
    return result;
  }

  FlatRecordsResponse._();

  factory FlatRecordsResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory FlatRecordsResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'FlatRecordsResponse',
      package:
          const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_flat_record.v1'),
      createEmptyInstance: create)
    ..aOM<XtcpFlatRecord>(1, _omitFieldNames ? '' : 'xtcpFlatRecord',
        subBuilder: XtcpFlatRecord.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  FlatRecordsResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  FlatRecordsResponse copyWith(void Function(FlatRecordsResponse) updates) =>
      super.copyWith((message) => updates(message as FlatRecordsResponse))
          as FlatRecordsResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static FlatRecordsResponse create() => FlatRecordsResponse._();
  @$core.override
  FlatRecordsResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static FlatRecordsResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<FlatRecordsResponse>(create);
  static FlatRecordsResponse? _defaultInstance;

  @$pb.TagNumber(1)
  XtcpFlatRecord get xtcpFlatRecord => $_getN(0);
  @$pb.TagNumber(1)
  set xtcpFlatRecord(XtcpFlatRecord value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasXtcpFlatRecord() => $_has(0);
  @$pb.TagNumber(1)
  void clearXtcpFlatRecord() => $_clearField(1);
  @$pb.TagNumber(1)
  XtcpFlatRecord ensureXtcpFlatRecord() => $_ensure(0);
}

class PollFlatRecordsRequest extends $pb.GeneratedMessage {
  factory PollFlatRecordsRequest() => create();

  PollFlatRecordsRequest._();

  factory PollFlatRecordsRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory PollFlatRecordsRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'PollFlatRecordsRequest',
      package:
          const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_flat_record.v1'),
      createEmptyInstance: create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  PollFlatRecordsRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  PollFlatRecordsRequest copyWith(
          void Function(PollFlatRecordsRequest) updates) =>
      super.copyWith((message) => updates(message as PollFlatRecordsRequest))
          as PollFlatRecordsRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static PollFlatRecordsRequest create() => PollFlatRecordsRequest._();
  @$core.override
  PollFlatRecordsRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static PollFlatRecordsRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<PollFlatRecordsRequest>(create);
  static PollFlatRecordsRequest? _defaultInstance;
}

class PollFlatRecordsResponse extends $pb.GeneratedMessage {
  factory PollFlatRecordsResponse({
    XtcpFlatRecord? xtcpFlatRecord,
  }) {
    final result = create();
    if (xtcpFlatRecord != null) result.xtcpFlatRecord = xtcpFlatRecord;
    return result;
  }

  PollFlatRecordsResponse._();

  factory PollFlatRecordsResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory PollFlatRecordsResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'PollFlatRecordsResponse',
      package:
          const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_flat_record.v1'),
      createEmptyInstance: create)
    ..aOM<XtcpFlatRecord>(1, _omitFieldNames ? '' : 'xtcpFlatRecord',
        subBuilder: XtcpFlatRecord.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  PollFlatRecordsResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  PollFlatRecordsResponse copyWith(
          void Function(PollFlatRecordsResponse) updates) =>
      super.copyWith((message) => updates(message as PollFlatRecordsResponse))
          as PollFlatRecordsResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static PollFlatRecordsResponse create() => PollFlatRecordsResponse._();
  @$core.override
  PollFlatRecordsResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static PollFlatRecordsResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<PollFlatRecordsResponse>(create);
  static PollFlatRecordsResponse? _defaultInstance;

  @$pb.TagNumber(1)
  XtcpFlatRecord get xtcpFlatRecord => $_getN(0);
  @$pb.TagNumber(1)
  set xtcpFlatRecord(XtcpFlatRecord value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasXtcpFlatRecord() => $_has(0);
  @$pb.TagNumber(1)
  void clearXtcpFlatRecord() => $_clearField(1);
  @$pb.TagNumber(1)
  XtcpFlatRecord ensureXtcpFlatRecord() => $_ensure(0);
}

const $core.bool _omitFieldNames =
    $core.bool.fromEnvironment('protobuf.omit_field_names');
const $core.bool _omitMessageNames =
    $core.bool.fromEnvironment('protobuf.omit_message_names');
