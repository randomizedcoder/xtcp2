//
//  Generated code. Do not modify.
//  source: xtcp_config/v1/xtcp_config.proto
//
// @dart = 2.12

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names, library_prefixes
// ignore_for_file: non_constant_identifier_names, prefer_final_fields
// ignore_for_file: unnecessary_import, unnecessary_this, unused_import

import 'dart:core' as $core;

import 'package:fixnum/fixnum.dart' as $fixnum;
import 'package:protobuf/protobuf.dart' as $pb;

import '../../google/protobuf/duration.pb.dart' as $2;

class GetRequest extends $pb.GeneratedMessage {
  factory GetRequest() => create();
  GetRequest._() : super();
  factory GetRequest.fromBuffer($core.List<$core.int> i, [$pb.ExtensionRegistry r = $pb.ExtensionRegistry.EMPTY]) => create()..mergeFromBuffer(i, r);
  factory GetRequest.fromJson($core.String i, [$pb.ExtensionRegistry r = $pb.ExtensionRegistry.EMPTY]) => create()..mergeFromJson(i, r);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(_omitMessageNames ? '' : 'GetRequest', package: const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_config.v1'), createEmptyInstance: create)
    ..hasRequiredFields = false
  ;

  @$core.Deprecated(
  'Using this can add significant overhead to your binary. '
  'Use [GeneratedMessageGenericExtensions.deepCopy] instead. '
  'Will be removed in next major version')
  GetRequest clone() => GetRequest()..mergeFromMessage(this);
  @$core.Deprecated(
  'Using this can add significant overhead to your binary. '
  'Use [GeneratedMessageGenericExtensions.rebuild] instead. '
  'Will be removed in next major version')
  GetRequest copyWith(void Function(GetRequest) updates) => super.copyWith((message) => updates(message as GetRequest)) as GetRequest;

  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static GetRequest create() => GetRequest._();
  GetRequest createEmptyInstance() => create();
  static $pb.PbList<GetRequest> createRepeated() => $pb.PbList<GetRequest>();
  @$core.pragma('dart2js:noInline')
  static GetRequest getDefault() => _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<GetRequest>(create);
  static GetRequest? _defaultInstance;
}

class GetResponse extends $pb.GeneratedMessage {
  factory GetResponse({
    XtcpConfig? config,
  }) {
    final $result = create();
    if (config != null) {
      $result.config = config;
    }
    return $result;
  }
  GetResponse._() : super();
  factory GetResponse.fromBuffer($core.List<$core.int> i, [$pb.ExtensionRegistry r = $pb.ExtensionRegistry.EMPTY]) => create()..mergeFromBuffer(i, r);
  factory GetResponse.fromJson($core.String i, [$pb.ExtensionRegistry r = $pb.ExtensionRegistry.EMPTY]) => create()..mergeFromJson(i, r);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(_omitMessageNames ? '' : 'GetResponse', package: const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_config.v1'), createEmptyInstance: create)
    ..aOM<XtcpConfig>(1, _omitFieldNames ? '' : 'config', subBuilder: XtcpConfig.create)
    ..hasRequiredFields = false
  ;

  @$core.Deprecated(
  'Using this can add significant overhead to your binary. '
  'Use [GeneratedMessageGenericExtensions.deepCopy] instead. '
  'Will be removed in next major version')
  GetResponse clone() => GetResponse()..mergeFromMessage(this);
  @$core.Deprecated(
  'Using this can add significant overhead to your binary. '
  'Use [GeneratedMessageGenericExtensions.rebuild] instead. '
  'Will be removed in next major version')
  GetResponse copyWith(void Function(GetResponse) updates) => super.copyWith((message) => updates(message as GetResponse)) as GetResponse;

  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static GetResponse create() => GetResponse._();
  GetResponse createEmptyInstance() => create();
  static $pb.PbList<GetResponse> createRepeated() => $pb.PbList<GetResponse>();
  @$core.pragma('dart2js:noInline')
  static GetResponse getDefault() => _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<GetResponse>(create);
  static GetResponse? _defaultInstance;

  @$pb.TagNumber(1)
  XtcpConfig get config => $_getN(0);
  @$pb.TagNumber(1)
  set config(XtcpConfig v) { setField(1, v); }
  @$pb.TagNumber(1)
  $core.bool hasConfig() => $_has(0);
  @$pb.TagNumber(1)
  void clearConfig() => clearField(1);
  @$pb.TagNumber(1)
  XtcpConfig ensureConfig() => $_ensure(0);
}

class SetRequest extends $pb.GeneratedMessage {
  factory SetRequest({
    XtcpConfig? config,
  }) {
    final $result = create();
    if (config != null) {
      $result.config = config;
    }
    return $result;
  }
  SetRequest._() : super();
  factory SetRequest.fromBuffer($core.List<$core.int> i, [$pb.ExtensionRegistry r = $pb.ExtensionRegistry.EMPTY]) => create()..mergeFromBuffer(i, r);
  factory SetRequest.fromJson($core.String i, [$pb.ExtensionRegistry r = $pb.ExtensionRegistry.EMPTY]) => create()..mergeFromJson(i, r);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(_omitMessageNames ? '' : 'SetRequest', package: const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_config.v1'), createEmptyInstance: create)
    ..aOM<XtcpConfig>(1, _omitFieldNames ? '' : 'config', subBuilder: XtcpConfig.create)
    ..hasRequiredFields = false
  ;

  @$core.Deprecated(
  'Using this can add significant overhead to your binary. '
  'Use [GeneratedMessageGenericExtensions.deepCopy] instead. '
  'Will be removed in next major version')
  SetRequest clone() => SetRequest()..mergeFromMessage(this);
  @$core.Deprecated(
  'Using this can add significant overhead to your binary. '
  'Use [GeneratedMessageGenericExtensions.rebuild] instead. '
  'Will be removed in next major version')
  SetRequest copyWith(void Function(SetRequest) updates) => super.copyWith((message) => updates(message as SetRequest)) as SetRequest;

  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static SetRequest create() => SetRequest._();
  SetRequest createEmptyInstance() => create();
  static $pb.PbList<SetRequest> createRepeated() => $pb.PbList<SetRequest>();
  @$core.pragma('dart2js:noInline')
  static SetRequest getDefault() => _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<SetRequest>(create);
  static SetRequest? _defaultInstance;

  @$pb.TagNumber(1)
  XtcpConfig get config => $_getN(0);
  @$pb.TagNumber(1)
  set config(XtcpConfig v) { setField(1, v); }
  @$pb.TagNumber(1)
  $core.bool hasConfig() => $_has(0);
  @$pb.TagNumber(1)
  void clearConfig() => clearField(1);
  @$pb.TagNumber(1)
  XtcpConfig ensureConfig() => $_ensure(0);
}

class SetResponse extends $pb.GeneratedMessage {
  factory SetResponse({
    XtcpConfig? config,
  }) {
    final $result = create();
    if (config != null) {
      $result.config = config;
    }
    return $result;
  }
  SetResponse._() : super();
  factory SetResponse.fromBuffer($core.List<$core.int> i, [$pb.ExtensionRegistry r = $pb.ExtensionRegistry.EMPTY]) => create()..mergeFromBuffer(i, r);
  factory SetResponse.fromJson($core.String i, [$pb.ExtensionRegistry r = $pb.ExtensionRegistry.EMPTY]) => create()..mergeFromJson(i, r);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(_omitMessageNames ? '' : 'SetResponse', package: const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_config.v1'), createEmptyInstance: create)
    ..aOM<XtcpConfig>(1, _omitFieldNames ? '' : 'config', subBuilder: XtcpConfig.create)
    ..hasRequiredFields = false
  ;

  @$core.Deprecated(
  'Using this can add significant overhead to your binary. '
  'Use [GeneratedMessageGenericExtensions.deepCopy] instead. '
  'Will be removed in next major version')
  SetResponse clone() => SetResponse()..mergeFromMessage(this);
  @$core.Deprecated(
  'Using this can add significant overhead to your binary. '
  'Use [GeneratedMessageGenericExtensions.rebuild] instead. '
  'Will be removed in next major version')
  SetResponse copyWith(void Function(SetResponse) updates) => super.copyWith((message) => updates(message as SetResponse)) as SetResponse;

  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static SetResponse create() => SetResponse._();
  SetResponse createEmptyInstance() => create();
  static $pb.PbList<SetResponse> createRepeated() => $pb.PbList<SetResponse>();
  @$core.pragma('dart2js:noInline')
  static SetResponse getDefault() => _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<SetResponse>(create);
  static SetResponse? _defaultInstance;

  @$pb.TagNumber(1)
  XtcpConfig get config => $_getN(0);
  @$pb.TagNumber(1)
  set config(XtcpConfig v) { setField(1, v); }
  @$pb.TagNumber(1)
  $core.bool hasConfig() => $_has(0);
  @$pb.TagNumber(1)
  void clearConfig() => clearField(1);
  @$pb.TagNumber(1)
  XtcpConfig ensureConfig() => $_ensure(0);
}

class SetPollFrequencyRequest extends $pb.GeneratedMessage {
  factory SetPollFrequencyRequest({
    $2.Duration? pollFrequency,
    $2.Duration? pollTimeout,
  }) {
    final $result = create();
    if (pollFrequency != null) {
      $result.pollFrequency = pollFrequency;
    }
    if (pollTimeout != null) {
      $result.pollTimeout = pollTimeout;
    }
    return $result;
  }
  SetPollFrequencyRequest._() : super();
  factory SetPollFrequencyRequest.fromBuffer($core.List<$core.int> i, [$pb.ExtensionRegistry r = $pb.ExtensionRegistry.EMPTY]) => create()..mergeFromBuffer(i, r);
  factory SetPollFrequencyRequest.fromJson($core.String i, [$pb.ExtensionRegistry r = $pb.ExtensionRegistry.EMPTY]) => create()..mergeFromJson(i, r);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(_omitMessageNames ? '' : 'SetPollFrequencyRequest', package: const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_config.v1'), createEmptyInstance: create)
    ..aOM<$2.Duration>(20, _omitFieldNames ? '' : 'pollFrequency', subBuilder: $2.Duration.create)
    ..aOM<$2.Duration>(30, _omitFieldNames ? '' : 'pollTimeout', subBuilder: $2.Duration.create)
    ..hasRequiredFields = false
  ;

  @$core.Deprecated(
  'Using this can add significant overhead to your binary. '
  'Use [GeneratedMessageGenericExtensions.deepCopy] instead. '
  'Will be removed in next major version')
  SetPollFrequencyRequest clone() => SetPollFrequencyRequest()..mergeFromMessage(this);
  @$core.Deprecated(
  'Using this can add significant overhead to your binary. '
  'Use [GeneratedMessageGenericExtensions.rebuild] instead. '
  'Will be removed in next major version')
  SetPollFrequencyRequest copyWith(void Function(SetPollFrequencyRequest) updates) => super.copyWith((message) => updates(message as SetPollFrequencyRequest)) as SetPollFrequencyRequest;

  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static SetPollFrequencyRequest create() => SetPollFrequencyRequest._();
  SetPollFrequencyRequest createEmptyInstance() => create();
  static $pb.PbList<SetPollFrequencyRequest> createRepeated() => $pb.PbList<SetPollFrequencyRequest>();
  @$core.pragma('dart2js:noInline')
  static SetPollFrequencyRequest getDefault() => _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<SetPollFrequencyRequest>(create);
  static SetPollFrequencyRequest? _defaultInstance;

  /// Poll frequency
  /// This is how often xtcp sends the netlink dump request
  /// Recommend not too frequently, so maybe 30s or 60s
  /// https://pkg.go.dev/google.golang.org/protobuf/types/known/durationpb
  @$pb.TagNumber(20)
  $2.Duration get pollFrequency => $_getN(0);
  @$pb.TagNumber(20)
  set pollFrequency($2.Duration v) { setField(20, v); }
  @$pb.TagNumber(20)
  $core.bool hasPollFrequency() => $_has(0);
  @$pb.TagNumber(20)
  void clearPollFrequency() => clearField(20);
  @$pb.TagNumber(20)
  $2.Duration ensurePollFrequency() => $_ensure(0);

  /// Poll timeout per name space
  /// Must be less than the poll frequency
  @$pb.TagNumber(30)
  $2.Duration get pollTimeout => $_getN(1);
  @$pb.TagNumber(30)
  set pollTimeout($2.Duration v) { setField(30, v); }
  @$pb.TagNumber(30)
  $core.bool hasPollTimeout() => $_has(1);
  @$pb.TagNumber(30)
  void clearPollTimeout() => clearField(30);
  @$pb.TagNumber(30)
  $2.Duration ensurePollTimeout() => $_ensure(1);
}

class SetPollFrequencyResponse extends $pb.GeneratedMessage {
  factory SetPollFrequencyResponse({
    XtcpConfig? config,
  }) {
    final $result = create();
    if (config != null) {
      $result.config = config;
    }
    return $result;
  }
  SetPollFrequencyResponse._() : super();
  factory SetPollFrequencyResponse.fromBuffer($core.List<$core.int> i, [$pb.ExtensionRegistry r = $pb.ExtensionRegistry.EMPTY]) => create()..mergeFromBuffer(i, r);
  factory SetPollFrequencyResponse.fromJson($core.String i, [$pb.ExtensionRegistry r = $pb.ExtensionRegistry.EMPTY]) => create()..mergeFromJson(i, r);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(_omitMessageNames ? '' : 'SetPollFrequencyResponse', package: const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_config.v1'), createEmptyInstance: create)
    ..aOM<XtcpConfig>(1, _omitFieldNames ? '' : 'config', subBuilder: XtcpConfig.create)
    ..hasRequiredFields = false
  ;

  @$core.Deprecated(
  'Using this can add significant overhead to your binary. '
  'Use [GeneratedMessageGenericExtensions.deepCopy] instead. '
  'Will be removed in next major version')
  SetPollFrequencyResponse clone() => SetPollFrequencyResponse()..mergeFromMessage(this);
  @$core.Deprecated(
  'Using this can add significant overhead to your binary. '
  'Use [GeneratedMessageGenericExtensions.rebuild] instead. '
  'Will be removed in next major version')
  SetPollFrequencyResponse copyWith(void Function(SetPollFrequencyResponse) updates) => super.copyWith((message) => updates(message as SetPollFrequencyResponse)) as SetPollFrequencyResponse;

  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static SetPollFrequencyResponse create() => SetPollFrequencyResponse._();
  SetPollFrequencyResponse createEmptyInstance() => create();
  static $pb.PbList<SetPollFrequencyResponse> createRepeated() => $pb.PbList<SetPollFrequencyResponse>();
  @$core.pragma('dart2js:noInline')
  static SetPollFrequencyResponse getDefault() => _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<SetPollFrequencyResponse>(create);
  static SetPollFrequencyResponse? _defaultInstance;

  @$pb.TagNumber(1)
  XtcpConfig get config => $_getN(0);
  @$pb.TagNumber(1)
  set config(XtcpConfig v) { setField(1, v); }
  @$pb.TagNumber(1)
  $core.bool hasConfig() => $_has(0);
  @$pb.TagNumber(1)
  void clearConfig() => clearField(1);
  @$pb.TagNumber(1)
  XtcpConfig ensureConfig() => $_ensure(0);
}

/// xtcp configuration
class XtcpConfig extends $pb.GeneratedMessage {
  factory XtcpConfig({
    $fixnum.Int64? nlTimeoutMilliseconds,
    $2.Duration? pollFrequency,
    $2.Duration? pollTimeout,
    $fixnum.Int64? maxLoops,
    $core.int? netlinkers,
    $core.int? netlinkersDoneChanSize,
    $core.int? nlmsgSeq,
    $fixnum.Int64? packetSize,
    $core.int? packetSizeMply,
    $core.int? writeFiles,
    $core.String? capturePath,
    $fixnum.Int64? modulus,
    $core.String? marshalTo,
    $core.int? envelopeFlushThresholdBytes,
    $core.int? envelopeFlushThresholdRows,
    $core.String? kafkaCompression,
    $core.String? s3Endpoint,
    $core.String? s3Bucket,
    $core.String? s3Prefix,
    $core.String? s3AccessKey,
    $core.String? s3SecretKey,
    $core.String? dest,
    $core.int? s3ParquetFlushThresholdBytes,
    $core.String? s3Region,
    $core.int? destWriteFiles,
    $core.String? topic,
    $core.String? xtcpProtoFile,
    $core.String? kafkaSchemaUrl,
    $2.Duration? kafkaProduceTimeout,
    $core.int? debugLevel,
    $core.String? label,
    $core.String? tag,
    $core.int? grpcPort,
    EnabledDeserializers? enabledDeserializers,
    $core.bool? ioUring,
    $core.int? ioUringRecvBatchSize,
    $core.int? ioUringCqeBatchSize,
  }) {
    final $result = create();
    if (nlTimeoutMilliseconds != null) {
      $result.nlTimeoutMilliseconds = nlTimeoutMilliseconds;
    }
    if (pollFrequency != null) {
      $result.pollFrequency = pollFrequency;
    }
    if (pollTimeout != null) {
      $result.pollTimeout = pollTimeout;
    }
    if (maxLoops != null) {
      $result.maxLoops = maxLoops;
    }
    if (netlinkers != null) {
      $result.netlinkers = netlinkers;
    }
    if (netlinkersDoneChanSize != null) {
      $result.netlinkersDoneChanSize = netlinkersDoneChanSize;
    }
    if (nlmsgSeq != null) {
      $result.nlmsgSeq = nlmsgSeq;
    }
    if (packetSize != null) {
      $result.packetSize = packetSize;
    }
    if (packetSizeMply != null) {
      $result.packetSizeMply = packetSizeMply;
    }
    if (writeFiles != null) {
      $result.writeFiles = writeFiles;
    }
    if (capturePath != null) {
      $result.capturePath = capturePath;
    }
    if (modulus != null) {
      $result.modulus = modulus;
    }
    if (marshalTo != null) {
      $result.marshalTo = marshalTo;
    }
    if (envelopeFlushThresholdBytes != null) {
      $result.envelopeFlushThresholdBytes = envelopeFlushThresholdBytes;
    }
    if (envelopeFlushThresholdRows != null) {
      $result.envelopeFlushThresholdRows = envelopeFlushThresholdRows;
    }
    if (kafkaCompression != null) {
      $result.kafkaCompression = kafkaCompression;
    }
    if (s3Endpoint != null) {
      $result.s3Endpoint = s3Endpoint;
    }
    if (s3Bucket != null) {
      $result.s3Bucket = s3Bucket;
    }
    if (s3Prefix != null) {
      $result.s3Prefix = s3Prefix;
    }
    if (s3AccessKey != null) {
      $result.s3AccessKey = s3AccessKey;
    }
    if (s3SecretKey != null) {
      $result.s3SecretKey = s3SecretKey;
    }
    if (dest != null) {
      $result.dest = dest;
    }
    if (s3ParquetFlushThresholdBytes != null) {
      $result.s3ParquetFlushThresholdBytes = s3ParquetFlushThresholdBytes;
    }
    if (s3Region != null) {
      $result.s3Region = s3Region;
    }
    if (destWriteFiles != null) {
      $result.destWriteFiles = destWriteFiles;
    }
    if (topic != null) {
      $result.topic = topic;
    }
    if (xtcpProtoFile != null) {
      $result.xtcpProtoFile = xtcpProtoFile;
    }
    if (kafkaSchemaUrl != null) {
      $result.kafkaSchemaUrl = kafkaSchemaUrl;
    }
    if (kafkaProduceTimeout != null) {
      $result.kafkaProduceTimeout = kafkaProduceTimeout;
    }
    if (debugLevel != null) {
      $result.debugLevel = debugLevel;
    }
    if (label != null) {
      $result.label = label;
    }
    if (tag != null) {
      $result.tag = tag;
    }
    if (grpcPort != null) {
      $result.grpcPort = grpcPort;
    }
    if (enabledDeserializers != null) {
      $result.enabledDeserializers = enabledDeserializers;
    }
    if (ioUring != null) {
      $result.ioUring = ioUring;
    }
    if (ioUringRecvBatchSize != null) {
      $result.ioUringRecvBatchSize = ioUringRecvBatchSize;
    }
    if (ioUringCqeBatchSize != null) {
      $result.ioUringCqeBatchSize = ioUringCqeBatchSize;
    }
    return $result;
  }
  XtcpConfig._() : super();
  factory XtcpConfig.fromBuffer($core.List<$core.int> i, [$pb.ExtensionRegistry r = $pb.ExtensionRegistry.EMPTY]) => create()..mergeFromBuffer(i, r);
  factory XtcpConfig.fromJson($core.String i, [$pb.ExtensionRegistry r = $pb.ExtensionRegistry.EMPTY]) => create()..mergeFromJson(i, r);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(_omitMessageNames ? '' : 'XtcpConfig', package: const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_config.v1'), createEmptyInstance: create)
    ..a<$fixnum.Int64>(10, _omitFieldNames ? '' : 'nlTimeoutMilliseconds', $pb.PbFieldType.OU6, defaultOrMaker: $fixnum.Int64.ZERO)
    ..aOM<$2.Duration>(20, _omitFieldNames ? '' : 'pollFrequency', subBuilder: $2.Duration.create)
    ..aOM<$2.Duration>(30, _omitFieldNames ? '' : 'pollTimeout', subBuilder: $2.Duration.create)
    ..a<$fixnum.Int64>(40, _omitFieldNames ? '' : 'maxLoops', $pb.PbFieldType.OU6, defaultOrMaker: $fixnum.Int64.ZERO)
    ..a<$core.int>(50, _omitFieldNames ? '' : 'netlinkers', $pb.PbFieldType.OU3)
    ..a<$core.int>(51, _omitFieldNames ? '' : 'netlinkersDoneChanSize', $pb.PbFieldType.OU3)
    ..a<$core.int>(60, _omitFieldNames ? '' : 'nlmsgSeq', $pb.PbFieldType.OU3)
    ..a<$fixnum.Int64>(70, _omitFieldNames ? '' : 'packetSize', $pb.PbFieldType.OU6, defaultOrMaker: $fixnum.Int64.ZERO)
    ..a<$core.int>(80, _omitFieldNames ? '' : 'packetSizeMply', $pb.PbFieldType.OU3)
    ..a<$core.int>(90, _omitFieldNames ? '' : 'writeFiles', $pb.PbFieldType.OU3)
    ..aOS(100, _omitFieldNames ? '' : 'capturePath')
    ..a<$fixnum.Int64>(110, _omitFieldNames ? '' : 'modulus', $pb.PbFieldType.OU6, defaultOrMaker: $fixnum.Int64.ZERO)
    ..aOS(120, _omitFieldNames ? '' : 'marshalTo')
    ..a<$core.int>(122, _omitFieldNames ? '' : 'envelopeFlushThresholdBytes', $pb.PbFieldType.OU3)
    ..a<$core.int>(123, _omitFieldNames ? '' : 'envelopeFlushThresholdRows', $pb.PbFieldType.OU3)
    ..aOS(124, _omitFieldNames ? '' : 'kafkaCompression')
    ..aOS(125, _omitFieldNames ? '' : 's3Endpoint')
    ..aOS(126, _omitFieldNames ? '' : 's3Bucket')
    ..aOS(127, _omitFieldNames ? '' : 's3Prefix')
    ..aOS(128, _omitFieldNames ? '' : 's3AccessKey')
    ..aOS(129, _omitFieldNames ? '' : 's3SecretKey')
    ..aOS(130, _omitFieldNames ? '' : 'dest')
    ..a<$core.int>(132, _omitFieldNames ? '' : 's3ParquetFlushThresholdBytes', $pb.PbFieldType.OU3)
    ..aOS(133, _omitFieldNames ? '' : 's3Region')
    ..a<$core.int>(135, _omitFieldNames ? '' : 'destWriteFiles', $pb.PbFieldType.OU3)
    ..aOS(140, _omitFieldNames ? '' : 'topic')
    ..aOS(143, _omitFieldNames ? '' : 'xtcpProtoFile')
    ..aOS(145, _omitFieldNames ? '' : 'kafkaSchemaUrl')
    ..aOM<$2.Duration>(150, _omitFieldNames ? '' : 'kafkaProduceTimeout', subBuilder: $2.Duration.create)
    ..a<$core.int>(160, _omitFieldNames ? '' : 'debugLevel', $pb.PbFieldType.OU3)
    ..aOS(170, _omitFieldNames ? '' : 'label')
    ..aOS(180, _omitFieldNames ? '' : 'tag')
    ..a<$core.int>(190, _omitFieldNames ? '' : 'grpcPort', $pb.PbFieldType.OU3)
    ..aOM<EnabledDeserializers>(200, _omitFieldNames ? '' : 'enabledDeserializers', subBuilder: EnabledDeserializers.create)
    ..aOB(210, _omitFieldNames ? '' : 'ioUring')
    ..a<$core.int>(211, _omitFieldNames ? '' : 'ioUringRecvBatchSize', $pb.PbFieldType.OU3)
    ..a<$core.int>(212, _omitFieldNames ? '' : 'ioUringCqeBatchSize', $pb.PbFieldType.OU3)
    ..hasRequiredFields = false
  ;

  @$core.Deprecated(
  'Using this can add significant overhead to your binary. '
  'Use [GeneratedMessageGenericExtensions.deepCopy] instead. '
  'Will be removed in next major version')
  XtcpConfig clone() => XtcpConfig()..mergeFromMessage(this);
  @$core.Deprecated(
  'Using this can add significant overhead to your binary. '
  'Use [GeneratedMessageGenericExtensions.rebuild] instead. '
  'Will be removed in next major version')
  XtcpConfig copyWith(void Function(XtcpConfig) updates) => super.copyWith((message) => updates(message as XtcpConfig)) as XtcpConfig;

  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static XtcpConfig create() => XtcpConfig._();
  XtcpConfig createEmptyInstance() => create();
  static $pb.PbList<XtcpConfig> createRepeated() => $pb.PbList<XtcpConfig>();
  @$core.pragma('dart2js:noInline')
  static XtcpConfig getDefault() => _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<XtcpConfig>(create);
  static XtcpConfig? _defaultInstance;

  /// Netlink socket timeout in milliseconds
  /// Recommend 5000
  @$pb.TagNumber(10)
  $fixnum.Int64 get nlTimeoutMilliseconds => $_getI64(0);
  @$pb.TagNumber(10)
  set nlTimeoutMilliseconds($fixnum.Int64 v) { $_setInt64(0, v); }
  @$pb.TagNumber(10)
  $core.bool hasNlTimeoutMilliseconds() => $_has(0);
  @$pb.TagNumber(10)
  void clearNlTimeoutMilliseconds() => clearField(10);

  /// Poll frequency
  /// This is how often xtcp sends the netlink dump request
  /// Recommend not too frequently, so maybe 30s or 60s
  /// https://pkg.go.dev/google.golang.org/protobuf/types/known/durationpb
  @$pb.TagNumber(20)
  $2.Duration get pollFrequency => $_getN(1);
  @$pb.TagNumber(20)
  set pollFrequency($2.Duration v) { setField(20, v); }
  @$pb.TagNumber(20)
  $core.bool hasPollFrequency() => $_has(1);
  @$pb.TagNumber(20)
  void clearPollFrequency() => clearField(20);
  @$pb.TagNumber(20)
  $2.Duration ensurePollFrequency() => $_ensure(1);

  /// Poll timeout per name space
  /// Must be less than the poll frequency
  @$pb.TagNumber(30)
  $2.Duration get pollTimeout => $_getN(2);
  @$pb.TagNumber(30)
  set pollTimeout($2.Duration v) { setField(30, v); }
  @$pb.TagNumber(30)
  $core.bool hasPollTimeout() => $_has(2);
  @$pb.TagNumber(30)
  void clearPollTimeout() => clearField(30);
  @$pb.TagNumber(30)
  $2.Duration ensurePollTimeout() => $_ensure(2);

  /// Maximum number of loops, or zero (0) for forever
  @$pb.TagNumber(40)
  $fixnum.Int64 get maxLoops => $_getI64(3);
  @$pb.TagNumber(40)
  set maxLoops($fixnum.Int64 v) { $_setInt64(3, v); }
  @$pb.TagNumber(40)
  $core.bool hasMaxLoops() => $_has(3);
  @$pb.TagNumber(40)
  void clearMaxLoops() => clearField(40);

  /// Netlinker goroutines per netlink socket ( recommend 1,2,4 range )
  /// Netlinkers read the tcp-diag response messages from the netlink socket
  /// If you have a large number of
  @$pb.TagNumber(50)
  $core.int get netlinkers => $_getIZ(4);
  @$pb.TagNumber(50)
  set netlinkers($core.int v) { $_setUnsignedInt32(4, v); }
  @$pb.TagNumber(50)
  $core.bool hasNetlinkers() => $_has(4);
  @$pb.TagNumber(50)
  void clearNetlinkers() => clearField(50);

  /// netlinkerDoneCh channel size
  /// This channel is used between the netlinkers and the poller
  /// Check the prom counter to see if the channel is too small
  /// d.pC.WithLabelValues("Deserialize", "netlinkerDoneCh", "error").Inc()
  @$pb.TagNumber(51)
  $core.int get netlinkersDoneChanSize => $_getIZ(5);
  @$pb.TagNumber(51)
  set netlinkersDoneChanSize($core.int v) { $_setUnsignedInt32(5, v); }
  @$pb.TagNumber(51)
  $core.bool hasNetlinkersDoneChanSize() => $_has(5);
  @$pb.TagNumber(51)
  void clearNetlinkersDoneChanSize() => clearField(51);

  /// nlmsg_seq sequence number (start). This gets incremented.
  @$pb.TagNumber(60)
  $core.int get nlmsgSeq => $_getIZ(6);
  @$pb.TagNumber(60)
  set nlmsgSeq($core.int v) { $_setUnsignedInt32(6, v); }
  @$pb.TagNumber(60)
  $core.bool hasNlmsgSeq() => $_has(6);
  @$pb.TagNumber(60)
  void clearNlmsgSeq() => clearField(60);

  /// netlinker packetSize.  buffer size = packetSize * packetSizeMply. Use zero (0) for syscall.Getpagesize()
  /// recommend using 0
  @$pb.TagNumber(70)
  $fixnum.Int64 get packetSize => $_getI64(7);
  @$pb.TagNumber(70)
  set packetSize($fixnum.Int64 v) { $_setInt64(7, v); }
  @$pb.TagNumber(70)
  $core.bool hasPacketSize() => $_has(7);
  @$pb.TagNumber(70)
  void clearPacketSize() => clearField(70);

  /// netlinker packetSize multiplier.  buffer size = packetSize * packetSizeMply
  @$pb.TagNumber(80)
  $core.int get packetSizeMply => $_getIZ(8);
  @$pb.TagNumber(80)
  set packetSizeMply($core.int v) { $_setUnsignedInt32(8, v); }
  @$pb.TagNumber(80)
  $core.bool hasPacketSizeMply() => $_has(8);
  @$pb.TagNumber(80)
  void clearPacketSizeMply() => clearField(80);

  /// Write netlink packets to writeFiles number of files ( to generate test data ) per netlinker
  /// xtcp will capture this many Netlink response packets when it starts
  /// This is PER netlinker
  @$pb.TagNumber(90)
  $core.int get writeFiles => $_getIZ(9);
  @$pb.TagNumber(90)
  set writeFiles($core.int v) { $_setUnsignedInt32(9, v); }
  @$pb.TagNumber(90)
  $core.bool hasWriteFiles() => $_has(9);
  @$pb.TagNumber(90)
  void clearWriteFiles() => clearField(90);

  /// Write files path
  @$pb.TagNumber(100)
  $core.String get capturePath => $_getSZ(10);
  @$pb.TagNumber(100)
  set capturePath($core.String v) { $_setString(10, v); }
  @$pb.TagNumber(100)
  $core.bool hasCapturePath() => $_has(10);
  @$pb.TagNumber(100)
  void clearCapturePath() => clearField(100);

  /// modulus. Report every X socket diag messages to output
  @$pb.TagNumber(110)
  $fixnum.Int64 get modulus => $_getI64(11);
  @$pb.TagNumber(110)
  set modulus($fixnum.Int64 v) { $_setInt64(11, v); }
  @$pb.TagNumber(110)
  $core.bool hasModulus() => $_has(11);
  @$pb.TagNumber(110)
  void clearModulus() => clearField(110);

  /// Marshalling of the exported data (protobufList,json,prototext)
  @$pb.TagNumber(120)
  $core.String get marshalTo => $_getSZ(12);
  @$pb.TagNumber(120)
  set marshalTo($core.String v) { $_setString(12, v); }
  @$pb.TagNumber(120)
  $core.bool hasMarshalTo() => $_has(12);
  @$pb.TagNumber(120)
  void clearMarshalTo() => clearField(120);

  ///  Soft cap on the in-flight envelope's marshalled size, in bytes.
  ///  Measured via proto.Size — i.e. the UNCOMPRESSED serialized size.
  ///  franz-go applies ZSTD/LZ4/Snappy compression after handoff, so the
  ///  actual on-wire Kafka message is typically 3-8x smaller than the
  ///  proto.Size we measure here. Treat this as a conservative upper
  ///  bound, not the wire size.
  ///
  ///  0 = use the daemon's compile-time default (768 KiB; see
  ///  EnvelopeFlushThresholdBytesCst in pkg/xtcp/marshallers.go).
  ///  Useful primarily as a safety net against records with huge
  ///  `bytes` fields. For everyday batch sizing, prefer the row-count
  ///  cap (envelope_flush_threshold_rows) below.
  @$pb.TagNumber(122)
  $core.int get envelopeFlushThresholdBytes => $_getIZ(13);
  @$pb.TagNumber(122)
  set envelopeFlushThresholdBytes($core.int v) { $_setUnsignedInt32(13, v); }
  @$pb.TagNumber(122)
  $core.bool hasEnvelopeFlushThresholdBytes() => $_has(13);
  @$pb.TagNumber(122)
  void clearEnvelopeFlushThresholdBytes() => clearField(122);

  ///  Soft cap on the in-flight envelope's row count. When the envelope
  ///  reaches this many rows, deserialize.go triggers an early mid-poll
  ///  flush. Cheaper than the byte cap (no proto.Size walk on the hot
  ///  path) and more predictable for operators reasoning about batch
  ///  size directly.
  ///
  ///  0 = use the daemon's compile-time default
  ///  (EnvelopeFlushThresholdRowsCst, currently 10000 — chosen to align
  ///  with the ClickHouse kafka_max_rows_per_message setting so a
  ///  produced envelope never forces the consumer to split it).
  @$pb.TagNumber(123)
  $core.int get envelopeFlushThresholdRows => $_getIZ(14);
  @$pb.TagNumber(123)
  set envelopeFlushThresholdRows($core.int v) { $_setUnsignedInt32(14, v); }
  @$pb.TagNumber(123)
  $core.bool hasEnvelopeFlushThresholdRows() => $_has(14);
  @$pb.TagNumber(123)
  void clearEnvelopeFlushThresholdRows() => clearField(123);

  ///  Kafka producer-batch compression codec. franz-go picks one codec
  ///  from the supplied preference list that the broker advertises.
  ///  Both Redpanda and ClickHouse (via librdkafka on its Kafka engine)
  ///  decompress all standard codecs transparently — no consumer-side
  ///  config is needed regardless of which codec is chosen here.
  ///
  ///  Valid values:
  ///    "" or "auto" → preference list [zstd, lz4, snappy, none] —
  ///                   modern brokers (Redpanda, Kafka 2.1+) end up
  ///                   on zstd; older brokers fall back through the list
  ///    "zstd"       → force ZStandard (best ratio, modern default)
  ///    "lz4"        → force LZ4 (fast, low CPU)
  ///    "snappy"     → force Snappy (legacy, broad compat)
  ///    "gzip"       → force Gzip (highest CPU; legacy clients)
  ///    "none"       → no compression on the wire
  ///
  ///  Pick "lz4" if xtcp2 is CPU-bound on the producer side; pick
  ///  "zstd" (the default) if Kafka throughput / disk usage matters more.
  @$pb.TagNumber(124)
  $core.String get kafkaCompression => $_getSZ(15);
  @$pb.TagNumber(124)
  set kafkaCompression($core.String v) { $_setString(15, v); }
  @$pb.TagNumber(124)
  $core.bool hasKafkaCompression() => $_has(15);
  @$pb.TagNumber(124)
  void clearKafkaCompression() => clearField(124);

  /// S3 endpoint URL, e.g. "http://127.0.0.1:9000" (MinIO) or
  /// "https://s3.amazonaws.com" (AWS). May be empty if -dest carries
  /// it via the s3parquet:<endpoint> form.
  @$pb.TagNumber(125)
  $core.String get s3Endpoint => $_getSZ(16);
  @$pb.TagNumber(125)
  set s3Endpoint($core.String v) { $_setString(16, v); }
  @$pb.TagNumber(125)
  $core.bool hasS3Endpoint() => $_has(16);
  @$pb.TagNumber(125)
  void clearS3Endpoint() => clearField(125);

  /// Required when -dest s3parquet. Bucket must already exist on the
  /// endpoint; the daemon does not auto-create.
  @$pb.TagNumber(126)
  $core.String get s3Bucket => $_getSZ(17);
  @$pb.TagNumber(126)
  set s3Bucket($core.String v) { $_setString(17, v); }
  @$pb.TagNumber(126)
  $core.bool hasS3Bucket() => $_has(17);
  @$pb.TagNumber(126)
  void clearS3Bucket() => clearField(126);

  /// Optional key-prefix WITHIN the bucket. Joined with the Hive-style
  /// partition segments (host=…/date=…/hour=…/<file>.parquet). Empty
  /// = files land at the bucket root level.
  @$pb.TagNumber(127)
  $core.String get s3Prefix => $_getSZ(18);
  @$pb.TagNumber(127)
  set s3Prefix($core.String v) { $_setString(18, v); }
  @$pb.TagNumber(127)
  $core.bool hasS3Prefix() => $_has(18);
  @$pb.TagNumber(127)
  void clearS3Prefix() => clearField(127);

  /// Required when -dest s3parquet. Picked up from AWS_ACCESS_KEY_ID
  /// env if blank.
  @$pb.TagNumber(128)
  $core.String get s3AccessKey => $_getSZ(19);
  @$pb.TagNumber(128)
  set s3AccessKey($core.String v) { $_setString(19, v); }
  @$pb.TagNumber(128)
  $core.bool hasS3AccessKey() => $_has(19);
  @$pb.TagNumber(128)
  void clearS3AccessKey() => clearField(128);

  /// Required when -dest s3parquet. Picked up from AWS_SECRET_ACCESS_KEY
  /// env if blank. Never logged.
  @$pb.TagNumber(129)
  $core.String get s3SecretKey => $_getSZ(20);
  @$pb.TagNumber(129)
  set s3SecretKey($core.String v) { $_setString(20, v); }
  @$pb.TagNumber(129)
  $core.bool hasS3SecretKey() => $_has(20);
  @$pb.TagNumber(129)
  void clearS3SecretKey() => clearField(129);

  /// kafka:127.0.0.1:9092, udp:127.0.0.1:13000, nsq:127.0.0.1:4150,
  /// nats:nats://127.0.0.1:4222, valkey:127.0.0.1:6379, null:,
  /// unix:/path/to/sock (SOCK_STREAM, length-prefixed via varint), or
  /// unixgram:/path/to/sock (SOCK_DGRAM, one record per datagram).
  /// max_len 128 leaves room for unixgram: (9 bytes) + Linux sun_path (108 bytes).
  @$pb.TagNumber(130)
  $core.String get dest => $_getSZ(21);
  @$pb.TagNumber(130)
  set dest($core.String v) { $_setString(21, v); }
  @$pb.TagNumber(130)
  $core.bool hasDest() => $_has(21);
  @$pb.TagNumber(130)
  void clearDest() => clearField(130);

  /// Soft cap on the in-memory Parquet builder's accumulated
  /// uncompressed row bytes before the worker finalizes the file and
  /// uploads. Default 0 → 63 MiB (S3ParquetFlushThresholdBytesCst).
  /// Operators tune down for faster file rotation (more S3 PUTs,
  /// smaller per-file query latency) or up for fewer larger files
  /// (better compression ratio, more memory).
  @$pb.TagNumber(132)
  $core.int get s3ParquetFlushThresholdBytes => $_getIZ(22);
  @$pb.TagNumber(132)
  set s3ParquetFlushThresholdBytes($core.int v) { $_setUnsignedInt32(22, v); }
  @$pb.TagNumber(132)
  $core.bool hasS3ParquetFlushThresholdBytes() => $_has(22);
  @$pb.TagNumber(132)
  void clearS3ParquetFlushThresholdBytes() => clearField(132);

  /// S3 region. Required by some S3 implementations even when talking
  /// to a single-region MinIO. Default "us-east-1" when blank.
  @$pb.TagNumber(133)
  $core.String get s3Region => $_getSZ(23);
  @$pb.TagNumber(133)
  set s3Region($core.String v) { $_setString(23, v); }
  @$pb.TagNumber(133)
  $core.bool hasS3Region() => $_has(23);
  @$pb.TagNumber(133)
  void clearS3Region() => clearField(133);

  /// Write marhselled data to writeFiles number of files ( to allow debugging of the serialization )
  /// xtcp will capture this many examples of the marshalled data
  /// This is PER poller
  @$pb.TagNumber(135)
  $core.int get destWriteFiles => $_getIZ(24);
  @$pb.TagNumber(135)
  set destWriteFiles($core.int v) { $_setUnsignedInt32(24, v); }
  @$pb.TagNumber(135)
  $core.bool hasDestWriteFiles() => $_has(24);
  @$pb.TagNumber(135)
  void clearDestWriteFiles() => clearField(135);

  /// Kafka or NSQ topic
  @$pb.TagNumber(140)
  $core.String get topic => $_getSZ(25);
  @$pb.TagNumber(140)
  set topic($core.String v) { $_setString(25, v); }
  @$pb.TagNumber(140)
  $core.bool hasTopic() => $_has(25);
  @$pb.TagNumber(140)
  void clearTopic() => clearField(140);

  /// XtcpProtoFile
  @$pb.TagNumber(143)
  $core.String get xtcpProtoFile => $_getSZ(26);
  @$pb.TagNumber(143)
  set xtcpProtoFile($core.String v) { $_setString(26, v); }
  @$pb.TagNumber(143)
  $core.bool hasXtcpProtoFile() => $_has(26);
  @$pb.TagNumber(143)
  void clearXtcpProtoFile() => clearField(143);

  /// Kafka schema registry url
  @$pb.TagNumber(145)
  $core.String get kafkaSchemaUrl => $_getSZ(27);
  @$pb.TagNumber(145)
  set kafkaSchemaUrl($core.String v) { $_setString(27, v); }
  @$pb.TagNumber(145)
  $core.bool hasKafkaSchemaUrl() => $_has(27);
  @$pb.TagNumber(145)
  void clearKafkaSchemaUrl() => clearField(145);

  /// Kafka Produce context timeout.  Use 0 for no context timeout
  /// Recommend a small timeout, like 1-2 seconds
  /// kgo seems to have a bug, because the timeout is always expired
  @$pb.TagNumber(150)
  $2.Duration get kafkaProduceTimeout => $_getN(28);
  @$pb.TagNumber(150)
  set kafkaProduceTimeout($2.Duration v) { setField(150, v); }
  @$pb.TagNumber(150)
  $core.bool hasKafkaProduceTimeout() => $_has(28);
  @$pb.TagNumber(150)
  void clearKafkaProduceTimeout() => clearField(150);
  @$pb.TagNumber(150)
  $2.Duration ensureKafkaProduceTimeout() => $_ensure(28);

  /// DebugLevel
  @$pb.TagNumber(160)
  $core.int get debugLevel => $_getIZ(29);
  @$pb.TagNumber(160)
  set debugLevel($core.int v) { $_setUnsignedInt32(29, v); }
  @$pb.TagNumber(160)
  $core.bool hasDebugLevel() => $_has(29);
  @$pb.TagNumber(160)
  void clearDebugLevel() => clearField(160);

  /// Label applied to the protobuf
  @$pb.TagNumber(170)
  $core.String get label => $_getSZ(30);
  @$pb.TagNumber(170)
  set label($core.String v) { $_setString(30, v); }
  @$pb.TagNumber(170)
  $core.bool hasLabel() => $_has(30);
  @$pb.TagNumber(170)
  void clearLabel() => clearField(170);

  /// Tag applied to the protobuf
  @$pb.TagNumber(180)
  $core.String get tag => $_getSZ(31);
  @$pb.TagNumber(180)
  set tag($core.String v) { $_setString(31, v); }
  @$pb.TagNumber(180)
  $core.bool hasTag() => $_has(31);
  @$pb.TagNumber(180)
  void clearTag() => clearField(180);

  /// GRPC listening port
  @$pb.TagNumber(190)
  $core.int get grpcPort => $_getIZ(32);
  @$pb.TagNumber(190)
  set grpcPort($core.int v) { $_setUnsignedInt32(32, v); }
  @$pb.TagNumber(190)
  $core.bool hasGrpcPort() => $_has(32);
  @$pb.TagNumber(190)
  void clearGrpcPort() => clearField(190);

  @$pb.TagNumber(200)
  EnabledDeserializers get enabledDeserializers => $_getN(33);
  @$pb.TagNumber(200)
  set enabledDeserializers(EnabledDeserializers v) { setField(200, v); }
  @$pb.TagNumber(200)
  $core.bool hasEnabledDeserializers() => $_has(33);
  @$pb.TagNumber(200)
  void clearEnabledDeserializers() => clearField(200);
  @$pb.TagNumber(200)
  EnabledDeserializers ensureEnabledDeserializers() => $_ensure(33);

  /// When true, route netlink reads and raw-socket destination writes
  /// through an io_uring ring per Netlinker. Requires Linux 6.1+.
  /// Library-backed destinations (kafka, nsq, nats, valkey) ignore this
  /// flag — they continue to use their own client sockets unchanged.
  @$pb.TagNumber(210)
  $core.bool get ioUring => $_getBF(34);
  @$pb.TagNumber(210)
  set ioUring($core.bool v) { $_setBool(34, v); }
  @$pb.TagNumber(210)
  $core.bool hasIoUring() => $_has(34);
  @$pb.TagNumber(210)
  void clearIoUring() => clearField(210);

  /// Number of recvmsg SQEs kept in flight per Netlinker ring. Higher
  /// values reduce io_uring_enter syscalls per dump cycle on hosts with
  /// many sockets, at the cost of more pinned buffers from packet pool.
  /// Ignored unless io_uring=true. Default 64.
  @$pb.TagNumber(211)
  $core.int get ioUringRecvBatchSize => $_getIZ(35);
  @$pb.TagNumber(211)
  set ioUringRecvBatchSize($core.int v) { $_setUnsignedInt32(35, v); }
  @$pb.TagNumber(211)
  $core.bool hasIoUringRecvBatchSize() => $_has(35);
  @$pb.TagNumber(211)
  void clearIoUringRecvBatchSize() => clearField(211);

  /// Maximum CQEs reaped per PeekBatchCQE call. Larger batches amortise
  /// userland loop overhead but increase scheduling latency for the
  /// netlinker goroutine. Ignored unless io_uring=true. Default 128.
  @$pb.TagNumber(212)
  $core.int get ioUringCqeBatchSize => $_getIZ(36);
  @$pb.TagNumber(212)
  set ioUringCqeBatchSize($core.int v) { $_setUnsignedInt32(36, v); }
  @$pb.TagNumber(212)
  $core.bool hasIoUringCqeBatchSize() => $_has(36);
  @$pb.TagNumber(212)
  void clearIoUringCqeBatchSize() => clearField(212);
}

class EnabledDeserializers extends $pb.GeneratedMessage {
  factory EnabledDeserializers({
    $core.Map<$core.String, $core.bool>? enabled,
  }) {
    final $result = create();
    if (enabled != null) {
      $result.enabled.addAll(enabled);
    }
    return $result;
  }
  EnabledDeserializers._() : super();
  factory EnabledDeserializers.fromBuffer($core.List<$core.int> i, [$pb.ExtensionRegistry r = $pb.ExtensionRegistry.EMPTY]) => create()..mergeFromBuffer(i, r);
  factory EnabledDeserializers.fromJson($core.String i, [$pb.ExtensionRegistry r = $pb.ExtensionRegistry.EMPTY]) => create()..mergeFromJson(i, r);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(_omitMessageNames ? '' : 'EnabledDeserializers', package: const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_config.v1'), createEmptyInstance: create)
    ..m<$core.String, $core.bool>(1, _omitFieldNames ? '' : 'enabled', entryClassName: 'EnabledDeserializers.EnabledEntry', keyFieldType: $pb.PbFieldType.OS, valueFieldType: $pb.PbFieldType.OB, packageName: const $pb.PackageName('xtcp_config.v1'))
    ..hasRequiredFields = false
  ;

  @$core.Deprecated(
  'Using this can add significant overhead to your binary. '
  'Use [GeneratedMessageGenericExtensions.deepCopy] instead. '
  'Will be removed in next major version')
  EnabledDeserializers clone() => EnabledDeserializers()..mergeFromMessage(this);
  @$core.Deprecated(
  'Using this can add significant overhead to your binary. '
  'Use [GeneratedMessageGenericExtensions.rebuild] instead. '
  'Will be removed in next major version')
  EnabledDeserializers copyWith(void Function(EnabledDeserializers) updates) => super.copyWith((message) => updates(message as EnabledDeserializers)) as EnabledDeserializers;

  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static EnabledDeserializers create() => EnabledDeserializers._();
  EnabledDeserializers createEmptyInstance() => create();
  static $pb.PbList<EnabledDeserializers> createRepeated() => $pb.PbList<EnabledDeserializers>();
  @$core.pragma('dart2js:noInline')
  static EnabledDeserializers getDefault() => _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<EnabledDeserializers>(create);
  static EnabledDeserializers? _defaultInstance;

  @$pb.TagNumber(1)
  $core.Map<$core.String, $core.bool> get enabled => $_getMap(0);
}


const _omitFieldNames = $core.bool.fromEnvironment('protobuf.omit_field_names');
const _omitMessageNames = $core.bool.fromEnvironment('protobuf.omit_message_names');
