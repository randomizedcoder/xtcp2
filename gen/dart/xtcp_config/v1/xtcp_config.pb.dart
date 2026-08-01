// This is a generated file - do not edit.
//
// Generated from xtcp_config/v1/xtcp_config.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, prefer_relative_imports

import 'dart:core' as $core;

import 'package:fixnum/fixnum.dart' as $fixnum;
import 'package:protobuf/protobuf.dart' as $pb;
import 'package:protobuf/well_known_types/google/protobuf/duration.pb.dart'
    as $1;

export 'package:protobuf/protobuf.dart' show GeneratedMessageGenericExtensions;

class GetRequest extends $pb.GeneratedMessage {
  factory GetRequest() => create();

  GetRequest._();

  factory GetRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory GetRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'GetRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_config.v1'),
      createEmptyInstance: create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetRequest copyWith(void Function(GetRequest) updates) =>
      super.copyWith((message) => updates(message as GetRequest)) as GetRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static GetRequest create() => GetRequest._();
  @$core.override
  GetRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static GetRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<GetRequest>(create);
  static GetRequest? _defaultInstance;
}

class GetResponse extends $pb.GeneratedMessage {
  factory GetResponse({
    XtcpConfig? config,
  }) {
    final result = create();
    if (config != null) result.config = config;
    return result;
  }

  GetResponse._();

  factory GetResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory GetResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'GetResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_config.v1'),
      createEmptyInstance: create)
    ..aOM<XtcpConfig>(1, _omitFieldNames ? '' : 'config',
        subBuilder: XtcpConfig.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetResponse copyWith(void Function(GetResponse) updates) =>
      super.copyWith((message) => updates(message as GetResponse))
          as GetResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static GetResponse create() => GetResponse._();
  @$core.override
  GetResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static GetResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<GetResponse>(create);
  static GetResponse? _defaultInstance;

  @$pb.TagNumber(1)
  XtcpConfig get config => $_getN(0);
  @$pb.TagNumber(1)
  set config(XtcpConfig value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasConfig() => $_has(0);
  @$pb.TagNumber(1)
  void clearConfig() => $_clearField(1);
  @$pb.TagNumber(1)
  XtcpConfig ensureConfig() => $_ensure(0);
}

class SetRequest extends $pb.GeneratedMessage {
  factory SetRequest({
    XtcpConfig? config,
  }) {
    final result = create();
    if (config != null) result.config = config;
    return result;
  }

  SetRequest._();

  factory SetRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory SetRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'SetRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_config.v1'),
      createEmptyInstance: create)
    ..aOM<XtcpConfig>(1, _omitFieldNames ? '' : 'config',
        subBuilder: XtcpConfig.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SetRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SetRequest copyWith(void Function(SetRequest) updates) =>
      super.copyWith((message) => updates(message as SetRequest)) as SetRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static SetRequest create() => SetRequest._();
  @$core.override
  SetRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static SetRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<SetRequest>(create);
  static SetRequest? _defaultInstance;

  @$pb.TagNumber(1)
  XtcpConfig get config => $_getN(0);
  @$pb.TagNumber(1)
  set config(XtcpConfig value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasConfig() => $_has(0);
  @$pb.TagNumber(1)
  void clearConfig() => $_clearField(1);
  @$pb.TagNumber(1)
  XtcpConfig ensureConfig() => $_ensure(0);
}

class SetResponse extends $pb.GeneratedMessage {
  factory SetResponse({
    XtcpConfig? config,
  }) {
    final result = create();
    if (config != null) result.config = config;
    return result;
  }

  SetResponse._();

  factory SetResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory SetResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'SetResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_config.v1'),
      createEmptyInstance: create)
    ..aOM<XtcpConfig>(1, _omitFieldNames ? '' : 'config',
        subBuilder: XtcpConfig.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SetResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SetResponse copyWith(void Function(SetResponse) updates) =>
      super.copyWith((message) => updates(message as SetResponse))
          as SetResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static SetResponse create() => SetResponse._();
  @$core.override
  SetResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static SetResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<SetResponse>(create);
  static SetResponse? _defaultInstance;

  @$pb.TagNumber(1)
  XtcpConfig get config => $_getN(0);
  @$pb.TagNumber(1)
  set config(XtcpConfig value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasConfig() => $_has(0);
  @$pb.TagNumber(1)
  void clearConfig() => $_clearField(1);
  @$pb.TagNumber(1)
  XtcpConfig ensureConfig() => $_ensure(0);
}

class SetPollFrequencyRequest extends $pb.GeneratedMessage {
  factory SetPollFrequencyRequest({
    $1.Duration? pollFrequency,
    $1.Duration? pollTimeout,
  }) {
    final result = create();
    if (pollFrequency != null) result.pollFrequency = pollFrequency;
    if (pollTimeout != null) result.pollTimeout = pollTimeout;
    return result;
  }

  SetPollFrequencyRequest._();

  factory SetPollFrequencyRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory SetPollFrequencyRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'SetPollFrequencyRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_config.v1'),
      createEmptyInstance: create)
    ..aOM<$1.Duration>(20, _omitFieldNames ? '' : 'pollFrequency',
        subBuilder: $1.Duration.create)
    ..aOM<$1.Duration>(30, _omitFieldNames ? '' : 'pollTimeout',
        subBuilder: $1.Duration.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SetPollFrequencyRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SetPollFrequencyRequest copyWith(
          void Function(SetPollFrequencyRequest) updates) =>
      super.copyWith((message) => updates(message as SetPollFrequencyRequest))
          as SetPollFrequencyRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static SetPollFrequencyRequest create() => SetPollFrequencyRequest._();
  @$core.override
  SetPollFrequencyRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static SetPollFrequencyRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<SetPollFrequencyRequest>(create);
  static SetPollFrequencyRequest? _defaultInstance;

  /// Poll frequency
  /// This is how often xtcp sends the netlink dump request
  /// Recommend not too frequently, so maybe 30s or 60s
  /// https://pkg.go.dev/google.golang.org/protobuf/types/known/durationpb
  @$pb.TagNumber(20)
  $1.Duration get pollFrequency => $_getN(0);
  @$pb.TagNumber(20)
  set pollFrequency($1.Duration value) => $_setField(20, value);
  @$pb.TagNumber(20)
  $core.bool hasPollFrequency() => $_has(0);
  @$pb.TagNumber(20)
  void clearPollFrequency() => $_clearField(20);
  @$pb.TagNumber(20)
  $1.Duration ensurePollFrequency() => $_ensure(0);

  /// Poll timeout per name space
  /// Must be less than the poll frequency
  @$pb.TagNumber(30)
  $1.Duration get pollTimeout => $_getN(1);
  @$pb.TagNumber(30)
  set pollTimeout($1.Duration value) => $_setField(30, value);
  @$pb.TagNumber(30)
  $core.bool hasPollTimeout() => $_has(1);
  @$pb.TagNumber(30)
  void clearPollTimeout() => $_clearField(30);
  @$pb.TagNumber(30)
  $1.Duration ensurePollTimeout() => $_ensure(1);
}

class SetPollFrequencyResponse extends $pb.GeneratedMessage {
  factory SetPollFrequencyResponse({
    XtcpConfig? config,
  }) {
    final result = create();
    if (config != null) result.config = config;
    return result;
  }

  SetPollFrequencyResponse._();

  factory SetPollFrequencyResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory SetPollFrequencyResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'SetPollFrequencyResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_config.v1'),
      createEmptyInstance: create)
    ..aOM<XtcpConfig>(1, _omitFieldNames ? '' : 'config',
        subBuilder: XtcpConfig.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SetPollFrequencyResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SetPollFrequencyResponse copyWith(
          void Function(SetPollFrequencyResponse) updates) =>
      super.copyWith((message) => updates(message as SetPollFrequencyResponse))
          as SetPollFrequencyResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static SetPollFrequencyResponse create() => SetPollFrequencyResponse._();
  @$core.override
  SetPollFrequencyResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static SetPollFrequencyResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<SetPollFrequencyResponse>(create);
  static SetPollFrequencyResponse? _defaultInstance;

  @$pb.TagNumber(1)
  XtcpConfig get config => $_getN(0);
  @$pb.TagNumber(1)
  set config(XtcpConfig value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasConfig() => $_has(0);
  @$pb.TagNumber(1)
  void clearConfig() => $_clearField(1);
  @$pb.TagNumber(1)
  XtcpConfig ensureConfig() => $_ensure(0);
}

class TriggerPollRequest extends $pb.GeneratedMessage {
  factory TriggerPollRequest() => create();

  TriggerPollRequest._();

  factory TriggerPollRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory TriggerPollRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'TriggerPollRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_config.v1'),
      createEmptyInstance: create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  TriggerPollRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  TriggerPollRequest copyWith(void Function(TriggerPollRequest) updates) =>
      super.copyWith((message) => updates(message as TriggerPollRequest))
          as TriggerPollRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static TriggerPollRequest create() => TriggerPollRequest._();
  @$core.override
  TriggerPollRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static TriggerPollRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<TriggerPollRequest>(create);
  static TriggerPollRequest? _defaultInstance;
}

class TriggerPollResponse extends $pb.GeneratedMessage {
  factory TriggerPollResponse() => create();

  TriggerPollResponse._();

  factory TriggerPollResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory TriggerPollResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'TriggerPollResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_config.v1'),
      createEmptyInstance: create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  TriggerPollResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  TriggerPollResponse copyWith(void Function(TriggerPollResponse) updates) =>
      super.copyWith((message) => updates(message as TriggerPollResponse))
          as TriggerPollResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static TriggerPollResponse create() => TriggerPollResponse._();
  @$core.override
  TriggerPollResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static TriggerPollResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<TriggerPollResponse>(create);
  static TriggerPollResponse? _defaultInstance;
}

class TriggerPollBurstRequest extends $pb.GeneratedMessage {
  factory TriggerPollBurstRequest({
    $core.int? count,
    $1.Duration? interval,
  }) {
    final result = create();
    if (count != null) result.count = count;
    if (interval != null) result.interval = interval;
    return result;
  }

  TriggerPollBurstRequest._();

  factory TriggerPollBurstRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory TriggerPollBurstRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'TriggerPollBurstRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_config.v1'),
      createEmptyInstance: create)
    ..aI(10, _omitFieldNames ? '' : 'count', fieldType: $pb.PbFieldType.OU3)
    ..aOM<$1.Duration>(20, _omitFieldNames ? '' : 'interval',
        subBuilder: $1.Duration.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  TriggerPollBurstRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  TriggerPollBurstRequest copyWith(
          void Function(TriggerPollBurstRequest) updates) =>
      super.copyWith((message) => updates(message as TriggerPollBurstRequest))
          as TriggerPollBurstRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static TriggerPollBurstRequest create() => TriggerPollBurstRequest._();
  @$core.override
  TriggerPollBurstRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static TriggerPollBurstRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<TriggerPollBurstRequest>(create);
  static TriggerPollBurstRequest? _defaultInstance;

  /// Number of polls to fire in the burst.
  @$pb.TagNumber(10)
  $core.int get count => $_getIZ(0);
  @$pb.TagNumber(10)
  set count($core.int value) => $_setUnsignedInt32(0, value);
  @$pb.TagNumber(10)
  $core.bool hasCount() => $_has(0);
  @$pb.TagNumber(10)
  void clearCount() => $_clearField(10);

  /// Spacing between consecutive polls. Must exceed the running
  /// poll_timeout so each poll completes before the next fires; the
  /// handler additionally rejects interval <= poll_timeout at runtime.
  @$pb.TagNumber(20)
  $1.Duration get interval => $_getN(1);
  @$pb.TagNumber(20)
  set interval($1.Duration value) => $_setField(20, value);
  @$pb.TagNumber(20)
  $core.bool hasInterval() => $_has(1);
  @$pb.TagNumber(20)
  void clearInterval() => $_clearField(20);
  @$pb.TagNumber(20)
  $1.Duration ensureInterval() => $_ensure(1);
}

class TriggerPollBurstResponse extends $pb.GeneratedMessage {
  factory TriggerPollBurstResponse({
    $core.int? count,
    $1.Duration? interval,
  }) {
    final result = create();
    if (count != null) result.count = count;
    if (interval != null) result.interval = interval;
    return result;
  }

  TriggerPollBurstResponse._();

  factory TriggerPollBurstResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory TriggerPollBurstResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'TriggerPollBurstResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_config.v1'),
      createEmptyInstance: create)
    ..aI(10, _omitFieldNames ? '' : 'count', fieldType: $pb.PbFieldType.OU3)
    ..aOM<$1.Duration>(20, _omitFieldNames ? '' : 'interval',
        subBuilder: $1.Duration.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  TriggerPollBurstResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  TriggerPollBurstResponse copyWith(
          void Function(TriggerPollBurstResponse) updates) =>
      super.copyWith((message) => updates(message as TriggerPollBurstResponse))
          as TriggerPollBurstResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static TriggerPollBurstResponse create() => TriggerPollBurstResponse._();
  @$core.override
  TriggerPollBurstResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static TriggerPollBurstResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<TriggerPollBurstResponse>(create);
  static TriggerPollBurstResponse? _defaultInstance;

  /// Echo of the accepted burst parameters (operator confirmation).
  @$pb.TagNumber(10)
  $core.int get count => $_getIZ(0);
  @$pb.TagNumber(10)
  set count($core.int value) => $_setUnsignedInt32(0, value);
  @$pb.TagNumber(10)
  $core.bool hasCount() => $_has(0);
  @$pb.TagNumber(10)
  void clearCount() => $_clearField(10);

  @$pb.TagNumber(20)
  $1.Duration get interval => $_getN(1);
  @$pb.TagNumber(20)
  set interval($1.Duration value) => $_setField(20, value);
  @$pb.TagNumber(20)
  $core.bool hasInterval() => $_has(1);
  @$pb.TagNumber(20)
  void clearInterval() => $_clearField(20);
  @$pb.TagNumber(20)
  $1.Duration ensureInterval() => $_ensure(1);
}

class SetS3UploadRequest extends $pb.GeneratedMessage {
  factory SetS3UploadRequest({
    $1.Duration? s3FlushInterval,
    $core.int? s3ParquetFlushThresholdBytes,
  }) {
    final result = create();
    if (s3FlushInterval != null) result.s3FlushInterval = s3FlushInterval;
    if (s3ParquetFlushThresholdBytes != null)
      result.s3ParquetFlushThresholdBytes = s3ParquetFlushThresholdBytes;
    return result;
  }

  SetS3UploadRequest._();

  factory SetS3UploadRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory SetS3UploadRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'SetS3UploadRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_config.v1'),
      createEmptyInstance: create)
    ..aOM<$1.Duration>(10, _omitFieldNames ? '' : 's3FlushInterval',
        subBuilder: $1.Duration.create)
    ..aI(20, _omitFieldNames ? '' : 's3ParquetFlushThresholdBytes',
        fieldType: $pb.PbFieldType.OU3)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SetS3UploadRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SetS3UploadRequest copyWith(void Function(SetS3UploadRequest) updates) =>
      super.copyWith((message) => updates(message as SetS3UploadRequest))
          as SetS3UploadRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static SetS3UploadRequest create() => SetS3UploadRequest._();
  @$core.override
  SetS3UploadRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static SetS3UploadRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<SetS3UploadRequest>(create);
  static SetS3UploadRequest? _defaultInstance;

  /// New s3parquet staleness-flush timer. Optional; omit (or leave unset)
  /// to leave the timer unchanged. 0 = derive as max(poll_frequency, 30m).
  @$pb.TagNumber(10)
  $1.Duration get s3FlushInterval => $_getN(0);
  @$pb.TagNumber(10)
  set s3FlushInterval($1.Duration value) => $_setField(10, value);
  @$pb.TagNumber(10)
  $core.bool hasS3FlushInterval() => $_has(0);
  @$pb.TagNumber(10)
  void clearS3FlushInterval() => $_clearField(10);
  @$pb.TagNumber(10)
  $1.Duration ensureS3FlushInterval() => $_ensure(0);

  /// New s3parquet byte-cap threshold before the worker finalizes and
  /// uploads an object. Optional; omit to leave unchanged. Takes effect on
  /// the next parquet object. 0 = default (63 MiB).
  @$pb.TagNumber(20)
  $core.int get s3ParquetFlushThresholdBytes => $_getIZ(1);
  @$pb.TagNumber(20)
  set s3ParquetFlushThresholdBytes($core.int value) =>
      $_setUnsignedInt32(1, value);
  @$pb.TagNumber(20)
  $core.bool hasS3ParquetFlushThresholdBytes() => $_has(1);
  @$pb.TagNumber(20)
  void clearS3ParquetFlushThresholdBytes() => $_clearField(20);
}

class SetS3UploadResponse extends $pb.GeneratedMessage {
  factory SetS3UploadResponse({
    XtcpConfig? config,
  }) {
    final result = create();
    if (config != null) result.config = config;
    return result;
  }

  SetS3UploadResponse._();

  factory SetS3UploadResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory SetS3UploadResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'SetS3UploadResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_config.v1'),
      createEmptyInstance: create)
    ..aOM<XtcpConfig>(1, _omitFieldNames ? '' : 'config',
        subBuilder: XtcpConfig.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SetS3UploadResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SetS3UploadResponse copyWith(void Function(SetS3UploadResponse) updates) =>
      super.copyWith((message) => updates(message as SetS3UploadResponse))
          as SetS3UploadResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static SetS3UploadResponse create() => SetS3UploadResponse._();
  @$core.override
  SetS3UploadResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static SetS3UploadResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<SetS3UploadResponse>(create);
  static SetS3UploadResponse? _defaultInstance;

  @$pb.TagNumber(1)
  XtcpConfig get config => $_getN(0);
  @$pb.TagNumber(1)
  set config(XtcpConfig value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasConfig() => $_has(0);
  @$pb.TagNumber(1)
  void clearConfig() => $_clearField(1);
  @$pb.TagNumber(1)
  XtcpConfig ensureConfig() => $_ensure(0);
}

/// xtcp configuration
class XtcpConfig extends $pb.GeneratedMessage {
  factory XtcpConfig({
    $fixnum.Int64? nlTimeoutMilliseconds,
    $1.Duration? pollFrequency,
    $1.Duration? pollTimeout,
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
    $core.bool? s3SkipBucketProbe,
    $core.int? destWriteFiles,
    $core.String? pyroscopeUrl,
    $core.String? pyroscopeAppName,
    $core.int? pyroscopeSampleHz,
    $core.int? pyroscopeUploadIntervalSec,
    $core.String? topic,
    $core.String? xtcpProtoFile,
    $core.String? kafkaSchemaUrl,
    $1.Duration? kafkaProduceTimeout,
    $core.int? debugLevel,
    $core.String? label,
    $core.String? tag,
    $core.String? location,
    $core.String? hostname,
    $core.bool? resolveContainerId,
    $core.int? ipv4Ttl,
    $core.int? ipv6HopLimit,
    $core.int? grpcPort,
    EnabledDeserializers? enabledDeserializers,
    $core.bool? ioUring,
    $core.int? ioUringRecvBatchSize,
    $core.int? ioUringCqeBatchSize,
    $core.String? csvColumns,
    $core.int? pollJitterPct,
    $1.Duration? s3FlushInterval,
    $core.int? s3FlushJitterPct,
    $core.int? s3FlushThresholdJitterPct,
    $core.int? s3UploadMaxAttempts,
    $1.Duration? s3UploadBackoffCap,
    $1.Duration? reconcileFrequency,
    $core.bool? reconcileBeforePoll,
  }) {
    final result = create();
    if (nlTimeoutMilliseconds != null)
      result.nlTimeoutMilliseconds = nlTimeoutMilliseconds;
    if (pollFrequency != null) result.pollFrequency = pollFrequency;
    if (pollTimeout != null) result.pollTimeout = pollTimeout;
    if (maxLoops != null) result.maxLoops = maxLoops;
    if (netlinkers != null) result.netlinkers = netlinkers;
    if (netlinkersDoneChanSize != null)
      result.netlinkersDoneChanSize = netlinkersDoneChanSize;
    if (nlmsgSeq != null) result.nlmsgSeq = nlmsgSeq;
    if (packetSize != null) result.packetSize = packetSize;
    if (packetSizeMply != null) result.packetSizeMply = packetSizeMply;
    if (writeFiles != null) result.writeFiles = writeFiles;
    if (capturePath != null) result.capturePath = capturePath;
    if (modulus != null) result.modulus = modulus;
    if (marshalTo != null) result.marshalTo = marshalTo;
    if (envelopeFlushThresholdBytes != null)
      result.envelopeFlushThresholdBytes = envelopeFlushThresholdBytes;
    if (envelopeFlushThresholdRows != null)
      result.envelopeFlushThresholdRows = envelopeFlushThresholdRows;
    if (kafkaCompression != null) result.kafkaCompression = kafkaCompression;
    if (s3Endpoint != null) result.s3Endpoint = s3Endpoint;
    if (s3Bucket != null) result.s3Bucket = s3Bucket;
    if (s3Prefix != null) result.s3Prefix = s3Prefix;
    if (s3AccessKey != null) result.s3AccessKey = s3AccessKey;
    if (s3SecretKey != null) result.s3SecretKey = s3SecretKey;
    if (dest != null) result.dest = dest;
    if (s3ParquetFlushThresholdBytes != null)
      result.s3ParquetFlushThresholdBytes = s3ParquetFlushThresholdBytes;
    if (s3Region != null) result.s3Region = s3Region;
    if (s3SkipBucketProbe != null) result.s3SkipBucketProbe = s3SkipBucketProbe;
    if (destWriteFiles != null) result.destWriteFiles = destWriteFiles;
    if (pyroscopeUrl != null) result.pyroscopeUrl = pyroscopeUrl;
    if (pyroscopeAppName != null) result.pyroscopeAppName = pyroscopeAppName;
    if (pyroscopeSampleHz != null) result.pyroscopeSampleHz = pyroscopeSampleHz;
    if (pyroscopeUploadIntervalSec != null)
      result.pyroscopeUploadIntervalSec = pyroscopeUploadIntervalSec;
    if (topic != null) result.topic = topic;
    if (xtcpProtoFile != null) result.xtcpProtoFile = xtcpProtoFile;
    if (kafkaSchemaUrl != null) result.kafkaSchemaUrl = kafkaSchemaUrl;
    if (kafkaProduceTimeout != null)
      result.kafkaProduceTimeout = kafkaProduceTimeout;
    if (debugLevel != null) result.debugLevel = debugLevel;
    if (label != null) result.label = label;
    if (tag != null) result.tag = tag;
    if (location != null) result.location = location;
    if (hostname != null) result.hostname = hostname;
    if (resolveContainerId != null)
      result.resolveContainerId = resolveContainerId;
    if (ipv4Ttl != null) result.ipv4Ttl = ipv4Ttl;
    if (ipv6HopLimit != null) result.ipv6HopLimit = ipv6HopLimit;
    if (grpcPort != null) result.grpcPort = grpcPort;
    if (enabledDeserializers != null)
      result.enabledDeserializers = enabledDeserializers;
    if (ioUring != null) result.ioUring = ioUring;
    if (ioUringRecvBatchSize != null)
      result.ioUringRecvBatchSize = ioUringRecvBatchSize;
    if (ioUringCqeBatchSize != null)
      result.ioUringCqeBatchSize = ioUringCqeBatchSize;
    if (csvColumns != null) result.csvColumns = csvColumns;
    if (pollJitterPct != null) result.pollJitterPct = pollJitterPct;
    if (s3FlushInterval != null) result.s3FlushInterval = s3FlushInterval;
    if (s3FlushJitterPct != null) result.s3FlushJitterPct = s3FlushJitterPct;
    if (s3FlushThresholdJitterPct != null)
      result.s3FlushThresholdJitterPct = s3FlushThresholdJitterPct;
    if (s3UploadMaxAttempts != null)
      result.s3UploadMaxAttempts = s3UploadMaxAttempts;
    if (s3UploadBackoffCap != null)
      result.s3UploadBackoffCap = s3UploadBackoffCap;
    if (reconcileFrequency != null)
      result.reconcileFrequency = reconcileFrequency;
    if (reconcileBeforePoll != null)
      result.reconcileBeforePoll = reconcileBeforePoll;
    return result;
  }

  XtcpConfig._();

  factory XtcpConfig.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory XtcpConfig.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'XtcpConfig',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_config.v1'),
      createEmptyInstance: create)
    ..a<$fixnum.Int64>(
        10, _omitFieldNames ? '' : 'nlTimeoutMilliseconds', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..aOM<$1.Duration>(20, _omitFieldNames ? '' : 'pollFrequency',
        subBuilder: $1.Duration.create)
    ..aOM<$1.Duration>(30, _omitFieldNames ? '' : 'pollTimeout',
        subBuilder: $1.Duration.create)
    ..a<$fixnum.Int64>(
        40, _omitFieldNames ? '' : 'maxLoops', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..aI(50, _omitFieldNames ? '' : 'netlinkers',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(51, _omitFieldNames ? '' : 'netlinkersDoneChanSize',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(60, _omitFieldNames ? '' : 'nlmsgSeq', fieldType: $pb.PbFieldType.OU3)
    ..a<$fixnum.Int64>(
        70, _omitFieldNames ? '' : 'packetSize', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..aI(80, _omitFieldNames ? '' : 'packetSizeMply',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(90, _omitFieldNames ? '' : 'writeFiles',
        fieldType: $pb.PbFieldType.OU3)
    ..aOS(100, _omitFieldNames ? '' : 'capturePath')
    ..a<$fixnum.Int64>(
        110, _omitFieldNames ? '' : 'modulus', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..aOS(120, _omitFieldNames ? '' : 'marshalTo')
    ..aI(122, _omitFieldNames ? '' : 'envelopeFlushThresholdBytes',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(123, _omitFieldNames ? '' : 'envelopeFlushThresholdRows',
        fieldType: $pb.PbFieldType.OU3)
    ..aOS(124, _omitFieldNames ? '' : 'kafkaCompression')
    ..aOS(125, _omitFieldNames ? '' : 's3Endpoint')
    ..aOS(126, _omitFieldNames ? '' : 's3Bucket')
    ..aOS(127, _omitFieldNames ? '' : 's3Prefix')
    ..aOS(128, _omitFieldNames ? '' : 's3AccessKey')
    ..aOS(129, _omitFieldNames ? '' : 's3SecretKey')
    ..aOS(130, _omitFieldNames ? '' : 'dest')
    ..aI(132, _omitFieldNames ? '' : 's3ParquetFlushThresholdBytes',
        fieldType: $pb.PbFieldType.OU3)
    ..aOS(133, _omitFieldNames ? '' : 's3Region')
    ..aOB(134, _omitFieldNames ? '' : 's3SkipBucketProbe')
    ..aI(135, _omitFieldNames ? '' : 'destWriteFiles',
        fieldType: $pb.PbFieldType.OU3)
    ..aOS(136, _omitFieldNames ? '' : 'pyroscopeUrl')
    ..aOS(137, _omitFieldNames ? '' : 'pyroscopeAppName')
    ..aI(138, _omitFieldNames ? '' : 'pyroscopeSampleHz',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(139, _omitFieldNames ? '' : 'pyroscopeUploadIntervalSec',
        fieldType: $pb.PbFieldType.OU3)
    ..aOS(140, _omitFieldNames ? '' : 'topic')
    ..aOS(143, _omitFieldNames ? '' : 'xtcpProtoFile')
    ..aOS(145, _omitFieldNames ? '' : 'kafkaSchemaUrl')
    ..aOM<$1.Duration>(150, _omitFieldNames ? '' : 'kafkaProduceTimeout',
        subBuilder: $1.Duration.create)
    ..aI(160, _omitFieldNames ? '' : 'debugLevel',
        fieldType: $pb.PbFieldType.OU3)
    ..aOS(170, _omitFieldNames ? '' : 'label')
    ..aOS(180, _omitFieldNames ? '' : 'tag')
    ..aOS(181, _omitFieldNames ? '' : 'location')
    ..aOS(182, _omitFieldNames ? '' : 'hostname')
    ..aOB(183, _omitFieldNames ? '' : 'resolveContainerId')
    ..aI(184, _omitFieldNames ? '' : 'ipv4Ttl', fieldType: $pb.PbFieldType.OU3)
    ..aI(185, _omitFieldNames ? '' : 'ipv6HopLimit',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(190, _omitFieldNames ? '' : 'grpcPort', fieldType: $pb.PbFieldType.OU3)
    ..aOM<EnabledDeserializers>(
        200, _omitFieldNames ? '' : 'enabledDeserializers',
        subBuilder: EnabledDeserializers.create)
    ..aOB(210, _omitFieldNames ? '' : 'ioUring')
    ..aI(211, _omitFieldNames ? '' : 'ioUringRecvBatchSize',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(212, _omitFieldNames ? '' : 'ioUringCqeBatchSize',
        fieldType: $pb.PbFieldType.OU3)
    ..aOS(220, _omitFieldNames ? '' : 'csvColumns')
    ..aI(221, _omitFieldNames ? '' : 'pollJitterPct',
        fieldType: $pb.PbFieldType.OU3)
    ..aOM<$1.Duration>(222, _omitFieldNames ? '' : 's3FlushInterval',
        subBuilder: $1.Duration.create)
    ..aI(223, _omitFieldNames ? '' : 's3FlushJitterPct',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(224, _omitFieldNames ? '' : 's3FlushThresholdJitterPct',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(225, _omitFieldNames ? '' : 's3UploadMaxAttempts',
        fieldType: $pb.PbFieldType.OU3)
    ..aOM<$1.Duration>(226, _omitFieldNames ? '' : 's3UploadBackoffCap',
        subBuilder: $1.Duration.create)
    ..aOM<$1.Duration>(227, _omitFieldNames ? '' : 'reconcileFrequency',
        subBuilder: $1.Duration.create)
    ..aOB(228, _omitFieldNames ? '' : 'reconcileBeforePoll')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  XtcpConfig clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  XtcpConfig copyWith(void Function(XtcpConfig) updates) =>
      super.copyWith((message) => updates(message as XtcpConfig)) as XtcpConfig;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static XtcpConfig create() => XtcpConfig._();
  @$core.override
  XtcpConfig createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static XtcpConfig getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<XtcpConfig>(create);
  static XtcpConfig? _defaultInstance;

  /// Netlink socket timeout in milliseconds
  /// Recommend 5000
  @$pb.TagNumber(10)
  $fixnum.Int64 get nlTimeoutMilliseconds => $_getI64(0);
  @$pb.TagNumber(10)
  set nlTimeoutMilliseconds($fixnum.Int64 value) => $_setInt64(0, value);
  @$pb.TagNumber(10)
  $core.bool hasNlTimeoutMilliseconds() => $_has(0);
  @$pb.TagNumber(10)
  void clearNlTimeoutMilliseconds() => $_clearField(10);

  /// Poll frequency
  /// This is how often xtcp sends the netlink dump request
  /// Recommend not too frequently, so maybe 30s or 60s
  /// https://pkg.go.dev/google.golang.org/protobuf/types/known/durationpb
  @$pb.TagNumber(20)
  $1.Duration get pollFrequency => $_getN(1);
  @$pb.TagNumber(20)
  set pollFrequency($1.Duration value) => $_setField(20, value);
  @$pb.TagNumber(20)
  $core.bool hasPollFrequency() => $_has(1);
  @$pb.TagNumber(20)
  void clearPollFrequency() => $_clearField(20);
  @$pb.TagNumber(20)
  $1.Duration ensurePollFrequency() => $_ensure(1);

  /// Poll timeout per name space
  /// Must be less than the poll frequency
  @$pb.TagNumber(30)
  $1.Duration get pollTimeout => $_getN(2);
  @$pb.TagNumber(30)
  set pollTimeout($1.Duration value) => $_setField(30, value);
  @$pb.TagNumber(30)
  $core.bool hasPollTimeout() => $_has(2);
  @$pb.TagNumber(30)
  void clearPollTimeout() => $_clearField(30);
  @$pb.TagNumber(30)
  $1.Duration ensurePollTimeout() => $_ensure(2);

  /// Maximum number of loops, or zero (0) for forever
  @$pb.TagNumber(40)
  $fixnum.Int64 get maxLoops => $_getI64(3);
  @$pb.TagNumber(40)
  set maxLoops($fixnum.Int64 value) => $_setInt64(3, value);
  @$pb.TagNumber(40)
  $core.bool hasMaxLoops() => $_has(3);
  @$pb.TagNumber(40)
  void clearMaxLoops() => $_clearField(40);

  /// Netlinker goroutines per netlink socket ( recommend 1,2,4 range )
  /// Netlinkers read the tcp-diag response messages from the netlink socket
  /// If you have a large number of
  @$pb.TagNumber(50)
  $core.int get netlinkers => $_getIZ(4);
  @$pb.TagNumber(50)
  set netlinkers($core.int value) => $_setUnsignedInt32(4, value);
  @$pb.TagNumber(50)
  $core.bool hasNetlinkers() => $_has(4);
  @$pb.TagNumber(50)
  void clearNetlinkers() => $_clearField(50);

  /// netlinkerDoneCh channel size
  /// This channel is used between the netlinkers and the poller
  /// Check the prom counter to see if the channel is too small
  /// d.pC.WithLabelValues("Deserialize", "netlinkerDoneCh", "error").Inc()
  @$pb.TagNumber(51)
  $core.int get netlinkersDoneChanSize => $_getIZ(5);
  @$pb.TagNumber(51)
  set netlinkersDoneChanSize($core.int value) => $_setUnsignedInt32(5, value);
  @$pb.TagNumber(51)
  $core.bool hasNetlinkersDoneChanSize() => $_has(5);
  @$pb.TagNumber(51)
  void clearNetlinkersDoneChanSize() => $_clearField(51);

  /// nlmsg_seq sequence number (start). This gets incremented.
  @$pb.TagNumber(60)
  $core.int get nlmsgSeq => $_getIZ(6);
  @$pb.TagNumber(60)
  set nlmsgSeq($core.int value) => $_setUnsignedInt32(6, value);
  @$pb.TagNumber(60)
  $core.bool hasNlmsgSeq() => $_has(6);
  @$pb.TagNumber(60)
  void clearNlmsgSeq() => $_clearField(60);

  /// netlinker packetSize.  buffer size = packetSize * packetSizeMply. Use zero (0) for syscall.Getpagesize()
  /// recommend using 0
  @$pb.TagNumber(70)
  $fixnum.Int64 get packetSize => $_getI64(7);
  @$pb.TagNumber(70)
  set packetSize($fixnum.Int64 value) => $_setInt64(7, value);
  @$pb.TagNumber(70)
  $core.bool hasPacketSize() => $_has(7);
  @$pb.TagNumber(70)
  void clearPacketSize() => $_clearField(70);

  /// netlinker packetSize multiplier.  buffer size = packetSize * packetSizeMply
  @$pb.TagNumber(80)
  $core.int get packetSizeMply => $_getIZ(8);
  @$pb.TagNumber(80)
  set packetSizeMply($core.int value) => $_setUnsignedInt32(8, value);
  @$pb.TagNumber(80)
  $core.bool hasPacketSizeMply() => $_has(8);
  @$pb.TagNumber(80)
  void clearPacketSizeMply() => $_clearField(80);

  /// Write netlink packets to writeFiles number of files ( to generate test data ) per netlinker
  /// xtcp will capture this many Netlink response packets when it starts
  /// This is PER netlinker
  @$pb.TagNumber(90)
  $core.int get writeFiles => $_getIZ(9);
  @$pb.TagNumber(90)
  set writeFiles($core.int value) => $_setUnsignedInt32(9, value);
  @$pb.TagNumber(90)
  $core.bool hasWriteFiles() => $_has(9);
  @$pb.TagNumber(90)
  void clearWriteFiles() => $_clearField(90);

  /// Write files path
  @$pb.TagNumber(100)
  $core.String get capturePath => $_getSZ(10);
  @$pb.TagNumber(100)
  set capturePath($core.String value) => $_setString(10, value);
  @$pb.TagNumber(100)
  $core.bool hasCapturePath() => $_has(10);
  @$pb.TagNumber(100)
  void clearCapturePath() => $_clearField(100);

  /// modulus. Report every X socket diag messages to output
  @$pb.TagNumber(110)
  $fixnum.Int64 get modulus => $_getI64(11);
  @$pb.TagNumber(110)
  set modulus($fixnum.Int64 value) => $_setInt64(11, value);
  @$pb.TagNumber(110)
  $core.bool hasModulus() => $_has(11);
  @$pb.TagNumber(110)
  void clearModulus() => $_clearField(110);

  /// Marshalling of the exported data (protobufList,json,prototext)
  @$pb.TagNumber(120)
  $core.String get marshalTo => $_getSZ(12);
  @$pb.TagNumber(120)
  set marshalTo($core.String value) => $_setString(12, value);
  @$pb.TagNumber(120)
  $core.bool hasMarshalTo() => $_has(12);
  @$pb.TagNumber(120)
  void clearMarshalTo() => $_clearField(120);

  /// Soft cap on the in-flight envelope's marshalled size, in bytes.
  /// Measured via proto.Size — i.e. the UNCOMPRESSED serialized size.
  /// franz-go applies ZSTD/LZ4/Snappy compression after handoff, so the
  /// actual on-wire Kafka message is typically 3-8x smaller than the
  /// proto.Size we measure here. Treat this as a conservative upper
  /// bound, not the wire size.
  ///
  /// 0 = use the daemon's compile-time default (768 KiB; see
  /// EnvelopeFlushThresholdBytesCst in pkg/xtcp/marshallers.go).
  /// Useful primarily as a safety net against records with huge
  /// `bytes` fields. For everyday batch sizing, prefer the row-count
  /// cap (envelope_flush_threshold_rows) below.
  @$pb.TagNumber(122)
  $core.int get envelopeFlushThresholdBytes => $_getIZ(13);
  @$pb.TagNumber(122)
  set envelopeFlushThresholdBytes($core.int value) =>
      $_setUnsignedInt32(13, value);
  @$pb.TagNumber(122)
  $core.bool hasEnvelopeFlushThresholdBytes() => $_has(13);
  @$pb.TagNumber(122)
  void clearEnvelopeFlushThresholdBytes() => $_clearField(122);

  /// Soft cap on the in-flight envelope's row count. When the envelope
  /// reaches this many rows, deserialize.go triggers an early mid-poll
  /// flush. Cheaper than the byte cap (no proto.Size walk on the hot
  /// path) and more predictable for operators reasoning about batch
  /// size directly.
  ///
  /// 0 = use the daemon's compile-time default
  /// (EnvelopeFlushThresholdRowsCst, currently 10000 — chosen to align
  /// with the ClickHouse kafka_max_rows_per_message setting so a
  /// produced envelope never forces the consumer to split it).
  @$pb.TagNumber(123)
  $core.int get envelopeFlushThresholdRows => $_getIZ(14);
  @$pb.TagNumber(123)
  set envelopeFlushThresholdRows($core.int value) =>
      $_setUnsignedInt32(14, value);
  @$pb.TagNumber(123)
  $core.bool hasEnvelopeFlushThresholdRows() => $_has(14);
  @$pb.TagNumber(123)
  void clearEnvelopeFlushThresholdRows() => $_clearField(123);

  /// Kafka producer-batch compression codec. franz-go picks one codec
  /// from the supplied preference list that the broker advertises.
  /// Both Redpanda and ClickHouse (via librdkafka on its Kafka engine)
  /// decompress all standard codecs transparently — no consumer-side
  /// config is needed regardless of which codec is chosen here.
  ///
  /// Valid values:
  ///   "" or "auto" → preference list [zstd, lz4, snappy, none] —
  ///                  modern brokers (Redpanda, Kafka 2.1+) end up
  ///                  on zstd; older brokers fall back through the list
  ///   "zstd"       → force ZStandard (best ratio, modern default)
  ///   "lz4"        → force LZ4 (fast, low CPU)
  ///   "snappy"     → force Snappy (legacy, broad compat)
  ///   "gzip"       → force Gzip (highest CPU; legacy clients)
  ///   "none"       → no compression on the wire
  ///
  /// Pick "lz4" if xtcp2 is CPU-bound on the producer side; pick
  /// "zstd" (the default) if Kafka throughput / disk usage matters more.
  @$pb.TagNumber(124)
  $core.String get kafkaCompression => $_getSZ(15);
  @$pb.TagNumber(124)
  set kafkaCompression($core.String value) => $_setString(15, value);
  @$pb.TagNumber(124)
  $core.bool hasKafkaCompression() => $_has(15);
  @$pb.TagNumber(124)
  void clearKafkaCompression() => $_clearField(124);

  /// S3 endpoint URL, e.g. "http://127.0.0.1:9000" (MinIO) or
  /// "https://s3.amazonaws.com" (AWS). May be empty if -dest carries
  /// it via the s3parquet:<endpoint> form.
  @$pb.TagNumber(125)
  $core.String get s3Endpoint => $_getSZ(16);
  @$pb.TagNumber(125)
  set s3Endpoint($core.String value) => $_setString(16, value);
  @$pb.TagNumber(125)
  $core.bool hasS3Endpoint() => $_has(16);
  @$pb.TagNumber(125)
  void clearS3Endpoint() => $_clearField(125);

  /// Required when -dest s3parquet. Bucket must already exist on the
  /// endpoint; the daemon does not auto-create.
  @$pb.TagNumber(126)
  $core.String get s3Bucket => $_getSZ(17);
  @$pb.TagNumber(126)
  set s3Bucket($core.String value) => $_setString(17, value);
  @$pb.TagNumber(126)
  $core.bool hasS3Bucket() => $_has(17);
  @$pb.TagNumber(126)
  void clearS3Bucket() => $_clearField(126);

  /// Optional key-prefix WITHIN the bucket. Joined with the Hive-style
  /// partition segments (host=…/date=…/hour=…/<file>.parquet). Empty
  /// = files land at the bucket root level.
  @$pb.TagNumber(127)
  $core.String get s3Prefix => $_getSZ(18);
  @$pb.TagNumber(127)
  set s3Prefix($core.String value) => $_setString(18, value);
  @$pb.TagNumber(127)
  $core.bool hasS3Prefix() => $_has(18);
  @$pb.TagNumber(127)
  void clearS3Prefix() => $_clearField(127);

  /// Required when -dest s3parquet. Picked up from AWS_ACCESS_KEY_ID
  /// env if blank.
  @$pb.TagNumber(128)
  $core.String get s3AccessKey => $_getSZ(19);
  @$pb.TagNumber(128)
  set s3AccessKey($core.String value) => $_setString(19, value);
  @$pb.TagNumber(128)
  $core.bool hasS3AccessKey() => $_has(19);
  @$pb.TagNumber(128)
  void clearS3AccessKey() => $_clearField(128);

  /// Required when -dest s3parquet. Picked up from AWS_SECRET_ACCESS_KEY
  /// env if blank. Never logged.
  @$pb.TagNumber(129)
  $core.String get s3SecretKey => $_getSZ(20);
  @$pb.TagNumber(129)
  set s3SecretKey($core.String value) => $_setString(20, value);
  @$pb.TagNumber(129)
  $core.bool hasS3SecretKey() => $_has(20);
  @$pb.TagNumber(129)
  void clearS3SecretKey() => $_clearField(129);

  /// kafka:127.0.0.1:9092, udp:127.0.0.1:13000, nsq:127.0.0.1:4150,
  /// nats:nats://127.0.0.1:4222, valkey:127.0.0.1:6379, null:,
  /// unix:/path/to/sock (SOCK_STREAM, length-prefixed via varint), or
  /// unixgram:/path/to/sock (SOCK_DGRAM, one record per datagram).
  /// max_len 512: a unix sun_path needs ~117 bytes (unixgram: + 108), but the
  /// http(s) destination carries a full URL — for ClickHouse/Loki/Splunk/ES
  /// and S3 endpoints the INSERT query + FORMAT + format_schema + auth query
  /// params routinely run ~150+ chars, which the old 128 cap rejected.
  @$pb.TagNumber(130)
  $core.String get dest => $_getSZ(21);
  @$pb.TagNumber(130)
  set dest($core.String value) => $_setString(21, value);
  @$pb.TagNumber(130)
  $core.bool hasDest() => $_has(21);
  @$pb.TagNumber(130)
  void clearDest() => $_clearField(130);

  /// Soft cap on the in-memory Parquet builder's accumulated
  /// uncompressed row bytes before the worker finalizes the file and
  /// uploads. Default 0 → 63 MiB (S3ParquetFlushThresholdBytesCst).
  /// Operators tune down for faster file rotation (more S3 PUTs,
  /// smaller per-file query latency) or up for fewer larger files
  /// (better compression ratio, more memory).
  @$pb.TagNumber(132)
  $core.int get s3ParquetFlushThresholdBytes => $_getIZ(22);
  @$pb.TagNumber(132)
  set s3ParquetFlushThresholdBytes($core.int value) =>
      $_setUnsignedInt32(22, value);
  @$pb.TagNumber(132)
  $core.bool hasS3ParquetFlushThresholdBytes() => $_has(22);
  @$pb.TagNumber(132)
  void clearS3ParquetFlushThresholdBytes() => $_clearField(132);

  /// S3 region. Required by some S3 implementations even when talking
  /// to a single-region MinIO. Default "us-east-1" when blank.
  @$pb.TagNumber(133)
  $core.String get s3Region => $_getSZ(23);
  @$pb.TagNumber(133)
  set s3Region($core.String value) => $_setString(23, value);
  @$pb.TagNumber(133)
  $core.bool hasS3Region() => $_has(23);
  @$pb.TagNumber(133)
  void clearS3Region() => $_clearField(133);

  /// Skip the startup S3 BucketExists probe. The probe issues a
  /// HeadBucket, which requires the s3:ListBucket permission. Set true
  /// when the upload credential is deliberately scoped to s3:PutObject
  /// only (write-only key, e.g. a baked deployment credential) so the
  /// daemon can start without list permission. Default false keeps the
  /// fail-fast probe for normal deployments.
  @$pb.TagNumber(134)
  $core.bool get s3SkipBucketProbe => $_getBF(24);
  @$pb.TagNumber(134)
  set s3SkipBucketProbe($core.bool value) => $_setBool(24, value);
  @$pb.TagNumber(134)
  $core.bool hasS3SkipBucketProbe() => $_has(24);
  @$pb.TagNumber(134)
  void clearS3SkipBucketProbe() => $_clearField(134);

  /// Write marhselled data to writeFiles number of files ( to allow debugging of the serialization )
  /// xtcp will capture this many examples of the marshalled data
  /// This is PER poller
  @$pb.TagNumber(135)
  $core.int get destWriteFiles => $_getIZ(25);
  @$pb.TagNumber(135)
  set destWriteFiles($core.int value) => $_setUnsignedInt32(25, value);
  @$pb.TagNumber(135)
  $core.bool hasDestWriteFiles() => $_has(25);
  @$pb.TagNumber(135)
  void clearDestWriteFiles() => $_clearField(135);

  /// Pyroscope continuous-profiling server URL (e.g.
  /// http://127.0.0.1:4040). When set, the daemon streams CPU,
  /// memory, goroutine, mutex, and block profiles to that endpoint.
  /// Empty disables the agent — no overhead in production runs that
  /// don't need it. Operators bring up a Pyroscope OSS server (or
  /// Grafana Cloud Pyroscope) and point xtcp2 at it for live profile
  /// data without restarts.
  @$pb.TagNumber(136)
  $core.String get pyroscopeUrl => $_getSZ(26);
  @$pb.TagNumber(136)
  set pyroscopeUrl($core.String value) => $_setString(26, value);
  @$pb.TagNumber(136)
  $core.bool hasPyroscopeUrl() => $_has(26);
  @$pb.TagNumber(136)
  void clearPyroscopeUrl() => $_clearField(136);

  /// Application name registered with the Pyroscope server (the
  /// "application" facet in the Pyroscope UI). Empty → "xtcp2".
  /// Set per fleet/role for multi-host environments
  /// (e.g. "xtcp2.prod.iad", "xtcp2.staging.fra").
  @$pb.TagNumber(137)
  $core.String get pyroscopeAppName => $_getSZ(27);
  @$pb.TagNumber(137)
  set pyroscopeAppName($core.String value) => $_setString(27, value);
  @$pb.TagNumber(137)
  $core.bool hasPyroscopeAppName() => $_has(27);
  @$pb.TagNumber(137)
  void clearPyroscopeAppName() => $_clearField(137);

  /// CPU profile sampling rate in Hz. Default 100. The Pyroscope
  /// agent uses this to call runtime.SetCPUProfileRate at startup.
  @$pb.TagNumber(138)
  $core.int get pyroscopeSampleHz => $_getIZ(28);
  @$pb.TagNumber(138)
  set pyroscopeSampleHz($core.int value) => $_setUnsignedInt32(28, value);
  @$pb.TagNumber(138)
  $core.bool hasPyroscopeSampleHz() => $_has(28);
  @$pb.TagNumber(138)
  void clearPyroscopeSampleHz() => $_clearField(138);

  /// Profile upload interval (seconds between batched profile
  /// pushes). Default 15 s.
  @$pb.TagNumber(139)
  $core.int get pyroscopeUploadIntervalSec => $_getIZ(29);
  @$pb.TagNumber(139)
  set pyroscopeUploadIntervalSec($core.int value) =>
      $_setUnsignedInt32(29, value);
  @$pb.TagNumber(139)
  $core.bool hasPyroscopeUploadIntervalSec() => $_has(29);
  @$pb.TagNumber(139)
  void clearPyroscopeUploadIntervalSec() => $_clearField(139);

  /// Kafka or NSQ topic
  @$pb.TagNumber(140)
  $core.String get topic => $_getSZ(30);
  @$pb.TagNumber(140)
  set topic($core.String value) => $_setString(30, value);
  @$pb.TagNumber(140)
  $core.bool hasTopic() => $_has(30);
  @$pb.TagNumber(140)
  void clearTopic() => $_clearField(140);

  /// XtcpProtoFile
  @$pb.TagNumber(143)
  $core.String get xtcpProtoFile => $_getSZ(31);
  @$pb.TagNumber(143)
  set xtcpProtoFile($core.String value) => $_setString(31, value);
  @$pb.TagNumber(143)
  $core.bool hasXtcpProtoFile() => $_has(31);
  @$pb.TagNumber(143)
  void clearXtcpProtoFile() => $_clearField(143);

  /// Kafka schema registry url
  @$pb.TagNumber(145)
  $core.String get kafkaSchemaUrl => $_getSZ(32);
  @$pb.TagNumber(145)
  set kafkaSchemaUrl($core.String value) => $_setString(32, value);
  @$pb.TagNumber(145)
  $core.bool hasKafkaSchemaUrl() => $_has(32);
  @$pb.TagNumber(145)
  void clearKafkaSchemaUrl() => $_clearField(145);

  /// Kafka Produce context timeout.  Use 0 for no context timeout
  /// Recommend a small timeout, like 1-2 seconds
  /// kgo seems to have a bug, because the timeout is always expired
  @$pb.TagNumber(150)
  $1.Duration get kafkaProduceTimeout => $_getN(33);
  @$pb.TagNumber(150)
  set kafkaProduceTimeout($1.Duration value) => $_setField(150, value);
  @$pb.TagNumber(150)
  $core.bool hasKafkaProduceTimeout() => $_has(33);
  @$pb.TagNumber(150)
  void clearKafkaProduceTimeout() => $_clearField(150);
  @$pb.TagNumber(150)
  $1.Duration ensureKafkaProduceTimeout() => $_ensure(33);

  /// DebugLevel
  @$pb.TagNumber(160)
  $core.int get debugLevel => $_getIZ(34);
  @$pb.TagNumber(160)
  set debugLevel($core.int value) => $_setUnsignedInt32(34, value);
  @$pb.TagNumber(160)
  $core.bool hasDebugLevel() => $_has(34);
  @$pb.TagNumber(160)
  void clearDebugLevel() => $_clearField(160);

  /// Label applied to the protobuf
  @$pb.TagNumber(170)
  $core.String get label => $_getSZ(35);
  @$pb.TagNumber(170)
  set label($core.String value) => $_setString(35, value);
  @$pb.TagNumber(170)
  $core.bool hasLabel() => $_has(35);
  @$pb.TagNumber(170)
  void clearLabel() => $_clearField(170);

  /// Tag applied to the protobuf
  @$pb.TagNumber(180)
  $core.String get tag => $_getSZ(36);
  @$pb.TagNumber(180)
  set tag($core.String value) => $_setString(36, value);
  @$pb.TagNumber(180)
  $core.bool hasTag() => $_has(36);
  @$pb.TagNumber(180)
  void clearTag() => $_clearField(180);

  /// Deployment grouping / facility this daemon runs in (data center, PoP,
  /// region, site, …). Generic; stamped on every record's `location` field.
  /// Set via -location flag or LOCATION env.
  @$pb.TagNumber(181)
  $core.String get location => $_getSZ(37);
  @$pb.TagNumber(181)
  set location($core.String value) => $_setString(37, value);
  @$pb.TagNumber(181)
  $core.bool hasLocation() => $_has(37);
  @$pb.TagNumber(181)
  void clearLocation() => $_clearField(181);

  /// Hostname override. When empty the daemon uses os.Hostname(); set this to
  /// stamp an explicit hostname on records — required in containers, where
  /// os.Hostname() returns the container id, not the host. Set via -hostname
  /// flag or XTCP_HOSTNAME env (NOT HOSTNAME, which Docker sets to the
  /// container id).
  @$pb.TagNumber(182)
  $core.String get hostname => $_getSZ(38);
  @$pb.TagNumber(182)
  set hostname($core.String value) => $_setString(38, value);
  @$pb.TagNumber(182)
  $core.bool hasHostname() => $_has(38);
  @$pb.TagNumber(182)
  void clearHostname() => $_clearField(182);

  /// Resolve each socket's owning container id from its cgroup (sets the
  /// record's container_id / container_runtime). Set via -resolveContainerId
  /// flag or CONTAINER_ID_RESOLVE env. Needs /sys/fs/cgroup readable (mount it
  /// and run --cgroupns=host in a container).
  @$pb.TagNumber(183)
  $core.bool get resolveContainerId => $_getBF(39);
  @$pb.TagNumber(183)
  set resolveContainerId($core.bool value) => $_setBool(39, value);
  @$pb.TagNumber(183)
  $core.bool hasResolveContainerId() => $_has(39);
  @$pb.TagNumber(183)
  void clearResolveContainerId() => $_clearField(183);

  /// Outgoing IPv4 TTL for xtcp2's own TCP listeners (Prometheus + gRPC).
  /// 0 = kernel default. A low value (e.g. 3) keeps replies from travelling
  /// far if the host is unexpectedly internet-exposed — the per-listener
  /// analogue of the host nftables TTL clamp. Set via -ipv4Ttl / IPV4_TTL.
  /// (cf. prometheus/exporter-toolkit#396.)
  @$pb.TagNumber(184)
  $core.int get ipv4Ttl => $_getIZ(40);
  @$pb.TagNumber(184)
  set ipv4Ttl($core.int value) => $_setUnsignedInt32(40, value);
  @$pb.TagNumber(184)
  $core.bool hasIpv4Ttl() => $_has(40);
  @$pb.TagNumber(184)
  void clearIpv4Ttl() => $_clearField(184);

  /// Outgoing IPv6 unicast hop limit for xtcp2's own TCP listeners. 0 = kernel
  /// default. Same intent as ipv4_ttl. Set via -ipv6HopLimit / IPV6_HOP_LIMIT.
  @$pb.TagNumber(185)
  $core.int get ipv6HopLimit => $_getIZ(41);
  @$pb.TagNumber(185)
  set ipv6HopLimit($core.int value) => $_setUnsignedInt32(41, value);
  @$pb.TagNumber(185)
  $core.bool hasIpv6HopLimit() => $_has(41);
  @$pb.TagNumber(185)
  void clearIpv6HopLimit() => $_clearField(185);

  /// GRPC listening port
  @$pb.TagNumber(190)
  $core.int get grpcPort => $_getIZ(42);
  @$pb.TagNumber(190)
  set grpcPort($core.int value) => $_setUnsignedInt32(42, value);
  @$pb.TagNumber(190)
  $core.bool hasGrpcPort() => $_has(42);
  @$pb.TagNumber(190)
  void clearGrpcPort() => $_clearField(190);

  @$pb.TagNumber(200)
  EnabledDeserializers get enabledDeserializers => $_getN(43);
  @$pb.TagNumber(200)
  set enabledDeserializers(EnabledDeserializers value) =>
      $_setField(200, value);
  @$pb.TagNumber(200)
  $core.bool hasEnabledDeserializers() => $_has(43);
  @$pb.TagNumber(200)
  void clearEnabledDeserializers() => $_clearField(200);
  @$pb.TagNumber(200)
  EnabledDeserializers ensureEnabledDeserializers() => $_ensure(43);

  /// When true, route netlink reads and raw-socket destination writes
  /// through an io_uring ring per Netlinker. Requires Linux 6.1+.
  /// Library-backed destinations (kafka, nsq, nats, valkey) ignore this
  /// flag — they continue to use their own client sockets unchanged.
  @$pb.TagNumber(210)
  $core.bool get ioUring => $_getBF(44);
  @$pb.TagNumber(210)
  set ioUring($core.bool value) => $_setBool(44, value);
  @$pb.TagNumber(210)
  $core.bool hasIoUring() => $_has(44);
  @$pb.TagNumber(210)
  void clearIoUring() => $_clearField(210);

  /// Number of recvmsg SQEs kept in flight per Netlinker ring. Higher
  /// values reduce io_uring_enter syscalls per dump cycle on hosts with
  /// many sockets, at the cost of more pinned buffers from packet pool.
  /// Ignored unless io_uring=true. Default 64.
  @$pb.TagNumber(211)
  $core.int get ioUringRecvBatchSize => $_getIZ(45);
  @$pb.TagNumber(211)
  set ioUringRecvBatchSize($core.int value) => $_setUnsignedInt32(45, value);
  @$pb.TagNumber(211)
  $core.bool hasIoUringRecvBatchSize() => $_has(45);
  @$pb.TagNumber(211)
  void clearIoUringRecvBatchSize() => $_clearField(211);

  /// Maximum CQEs reaped per PeekBatchCQE call. Larger batches amortise
  /// userland loop overhead but increase scheduling latency for the
  /// netlinker goroutine. Ignored unless io_uring=true. Default 128.
  @$pb.TagNumber(212)
  $core.int get ioUringCqeBatchSize => $_getIZ(46);
  @$pb.TagNumber(212)
  set ioUringCqeBatchSize($core.int value) => $_setUnsignedInt32(46, value);
  @$pb.TagNumber(212)
  $core.bool hasIoUringCqeBatchSize() => $_has(46);
  @$pb.TagNumber(212)
  void clearIoUringCqeBatchSize() => $_clearField(212);

  /// Comma-separated subset of XtcpFlatRecord json field names selecting
  /// which columns the csv/tsv marshallers emit (e.g.
  /// "hostname,inetDiagMsgSocketSourcePort,inetDiagMsgState,tcpInfoRtt").
  /// Empty = all fields. Ignored by non-tabular marshallers.
  @$pb.TagNumber(220)
  $core.String get csvColumns => $_getSZ(47);
  @$pb.TagNumber(220)
  set csvColumns($core.String value) => $_setString(47, value);
  @$pb.TagNumber(220)
  $core.bool hasCsvColumns() => $_has(47);
  @$pb.TagNumber(220)
  void clearCsvColumns() => $_clearField(220);

  /// Maximum poll-schedule jitter as a percent of poll_frequency, applied to
  /// both the startup delay before the first poll and each subsequent tick.
  /// 0 disables (immediate first poll, fixed interval). Default 20.
  @$pb.TagNumber(221)
  $core.int get pollJitterPct => $_getIZ(48);
  @$pb.TagNumber(221)
  set pollJitterPct($core.int value) => $_setUnsignedInt32(48, value);
  @$pb.TagNumber(221)
  $core.bool hasPollJitterPct() => $_has(48);
  @$pb.TagNumber(221)
  void clearPollJitterPct() => $_clearField(221);

  /// s3parquet staleness ceiling: force-flush the in-memory Parquet object
  /// after this long even if it hasn't reached the byte cap, bounding upload
  /// latency for low-volume hosts. 0 = derive as max(poll_frequency, 30m).
  @$pb.TagNumber(222)
  $1.Duration get s3FlushInterval => $_getN(49);
  @$pb.TagNumber(222)
  set s3FlushInterval($1.Duration value) => $_setField(222, value);
  @$pb.TagNumber(222)
  $core.bool hasS3FlushInterval() => $_has(49);
  @$pb.TagNumber(222)
  void clearS3FlushInterval() => $_clearField(222);
  @$pb.TagNumber(222)
  $1.Duration ensureS3FlushInterval() => $_ensure(49);

  /// Maximum jitter as a percent of s3_flush_interval, applied to the first
  /// timed flush and each interval so the fleet doesn't ceiling-flush in
  /// lockstep. 0 disables. Default 20.
  @$pb.TagNumber(223)
  $core.int get s3FlushJitterPct => $_getIZ(50);
  @$pb.TagNumber(223)
  set s3FlushJitterPct($core.int value) => $_setUnsignedInt32(50, value);
  @$pb.TagNumber(223)
  $core.bool hasS3FlushJitterPct() => $_has(50);
  @$pb.TagNumber(223)
  void clearS3FlushJitterPct() => $_clearField(223);

  /// Per-object downward jitter as a percent of the s3parquet byte cap: each
  /// object finalizes at threshold*(1 - rand[0,pct/100]), de-syncing the
  /// size-cap upload path even under uniform load. Downward-only, so an
  /// object never exceeds the in-memory byte bound. 0 disables. Default 20.
  @$pb.TagNumber(224)
  $core.int get s3FlushThresholdJitterPct => $_getIZ(51);
  @$pb.TagNumber(224)
  set s3FlushThresholdJitterPct($core.int value) =>
      $_setUnsignedInt32(51, value);
  @$pb.TagNumber(224)
  $core.bool hasS3FlushThresholdJitterPct() => $_has(51);
  @$pb.TagNumber(224)
  void clearS3FlushThresholdJitterPct() => $_clearField(224);

  /// Maximum S3 upload attempts (original + retries) before dropping the
  /// object. Retries use full-jitter exponential backoff. Default 10.
  @$pb.TagNumber(225)
  $core.int get s3UploadMaxAttempts => $_getIZ(52);
  @$pb.TagNumber(225)
  set s3UploadMaxAttempts($core.int value) => $_setUnsignedInt32(52, value);
  @$pb.TagNumber(225)
  $core.bool hasS3UploadMaxAttempts() => $_has(52);
  @$pb.TagNumber(225)
  void clearS3UploadMaxAttempts() => $_clearField(225);

  /// Cap on a single upload retry's backoff window (full jitter draws in
  /// [0, window], window grows exponentially up to this cap). 0 = derive as
  /// clamp(poll_frequency/10, 1s, 1h).
  @$pb.TagNumber(226)
  $1.Duration get s3UploadBackoffCap => $_getN(53);
  @$pb.TagNumber(226)
  set s3UploadBackoffCap($1.Duration value) => $_setField(226, value);
  @$pb.TagNumber(226)
  $core.bool hasS3UploadBackoffCap() => $_has(53);
  @$pb.TagNumber(226)
  void clearS3UploadBackoffCap() => $_clearField(226);
  @$pb.TagNumber(226)
  $1.Duration ensureS3UploadBackoffCap() => $_ensure(53);

  /// Period of the background namespace-reconcile ticker (Method B /proc scan
  /// that converges the tracked namespace set). With reconcile_before_poll the
  /// Poller reconciles every cycle and is the real discovery mechanism, so this
  /// background pass is an occasional safety-net expected to find nothing
  /// (mapReconciler dels/stores stay 0) — the default is deliberately long (6h)
  /// so operators can confirm from the counters that it is redundant. It still
  /// matters when the poller is idle or disabled. 0 disables the background
  /// ticker entirely (the startup reconcile still runs once).
  @$pb.TagNumber(227)
  $1.Duration get reconcileFrequency => $_getN(54);
  @$pb.TagNumber(227)
  set reconcileFrequency($1.Duration value) => $_setField(227, value);
  @$pb.TagNumber(227)
  $core.bool hasReconcileFrequency() => $_has(54);
  @$pb.TagNumber(227)
  void clearReconcileFrequency() => $_clearField(227);
  @$pb.TagNumber(227)
  $1.Duration ensureReconcileFrequency() => $_ensure(54);

  /// Run a namespace reconcile immediately before each poll cycle, so a
  /// namespace that appeared since the last cycle is entered and gets a socket
  /// within ~1 poll interval instead of waiting for the background ticker. Ties
  /// discovery cadence to poll cadence; the /proc scan is zero-allocation and
  /// mutex-serialized with the background reconciler. Default true.
  @$pb.TagNumber(228)
  $core.bool get reconcileBeforePoll => $_getBF(55);
  @$pb.TagNumber(228)
  set reconcileBeforePoll($core.bool value) => $_setBool(55, value);
  @$pb.TagNumber(228)
  $core.bool hasReconcileBeforePoll() => $_has(55);
  @$pb.TagNumber(228)
  void clearReconcileBeforePoll() => $_clearField(228);
}

class EnabledDeserializers extends $pb.GeneratedMessage {
  factory EnabledDeserializers({
    $core.Iterable<$core.MapEntry<$core.String, $core.bool>>? enabled,
  }) {
    final result = create();
    if (enabled != null) result.enabled.addEntries(enabled);
    return result;
  }

  EnabledDeserializers._();

  factory EnabledDeserializers.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory EnabledDeserializers.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'EnabledDeserializers',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_config.v1'),
      createEmptyInstance: create)
    ..m<$core.String, $core.bool>(1, _omitFieldNames ? '' : 'enabled',
        entryClassName: 'EnabledDeserializers.EnabledEntry',
        keyFieldType: $pb.PbFieldType.OS,
        valueFieldType: $pb.PbFieldType.OB,
        packageName: const $pb.PackageName('xtcp_config.v1'))
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  EnabledDeserializers clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  EnabledDeserializers copyWith(void Function(EnabledDeserializers) updates) =>
      super.copyWith((message) => updates(message as EnabledDeserializers))
          as EnabledDeserializers;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static EnabledDeserializers create() => EnabledDeserializers._();
  @$core.override
  EnabledDeserializers createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static EnabledDeserializers getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<EnabledDeserializers>(create);
  static EnabledDeserializers? _defaultInstance;

  @$pb.TagNumber(1)
  $pb.PbMap<$core.String, $core.bool> get enabled => $_getMap(0);
}

const $core.bool _omitFieldNames =
    $core.bool.fromEnvironment('protobuf.omit_field_names');
const $core.bool _omitMessageNames =
    $core.bool.fromEnvironment('protobuf.omit_message_names');
