// This is a generated file - do not edit.
//
// Generated from xtcp_treatment/v1/agent.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, prefer_relative_imports

import 'dart:core' as $core;

import 'package:fixnum/fixnum.dart' as $fixnum;
import 'package:protobuf/protobuf.dart' as $pb;

import 'treatment.pb.dart' as $1;

export 'package:protobuf/protobuf.dart' show GeneratedMessageGenericExtensions;

class CheckInRequest extends $pb.GeneratedMessage {
  factory CheckInRequest({
    $core.String? agentId,
    $core.String? location,
    $1.Capabilities? capabilities,
    $fixnum.Int64? desiredRevision,
    $core.Iterable<$1.ApplicationRecord>? records,
    $core.bool? locallySuspended,
  }) {
    final result = CheckInRequest._();
    if (agentId != null) result.agentId = agentId;
    if (location != null) result.location = location;
    if (capabilities != null) result.capabilities = capabilities;
    if (desiredRevision != null) result.desiredRevision = desiredRevision;
    if (records != null) result.records.addAll(records);
    if (locallySuspended != null) result.locallySuspended = locallySuspended;
    return result;
  }

  CheckInRequest._();

  factory CheckInRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      CheckInRequest()..mergeFromBuffer(data, registry);
  factory CheckInRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      CheckInRequest()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'CheckInRequest',
      package:
          const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_treatment.v1'),
      createEmptyInstance: CheckInRequest.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'agentId')
    ..aOS(2, _omitFieldNames ? '' : 'location')
    ..aOM<$1.Capabilities>(3, _omitFieldNames ? '' : 'capabilities',
        subBuilder: $1.Capabilities.$_createMessage)
    ..a<$fixnum.Int64>(
        4, _omitFieldNames ? '' : 'desiredRevision', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..pPM<$1.ApplicationRecord>(5, _omitFieldNames ? '' : 'records',
        subBuilder: $1.ApplicationRecord.$_createMessage)
    ..aOB(6, _omitFieldNames ? '' : 'locallySuspended')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  CheckInRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  CheckInRequest copyWith(void Function(CheckInRequest) updates) =>
      super.copyWith((message) => updates(message as CheckInRequest))
          as CheckInRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated('Use CheckInRequest() / CheckInRequest.new instead')
  static CheckInRequest create() => CheckInRequest._();
  static $pb.GeneratedMessage $_createMessage() => CheckInRequest._();
  @$core.override
  CheckInRequest createEmptyInstance() => CheckInRequest._();
  @$core.pragma('dart2js:noInline')
  static CheckInRequest getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<CheckInRequest>(
          CheckInRequest.$_createMessage);
  static CheckInRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get agentId => $_getSZ(0);
  @$pb.TagNumber(1)
  set agentId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasAgentId() => $_has(0);
  @$pb.TagNumber(1)
  void clearAgentId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get location => $_getSZ(1);
  @$pb.TagNumber(2)
  set location($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasLocation() => $_has(1);
  @$pb.TagNumber(2)
  void clearLocation() => $_clearField(2);

  @$pb.TagNumber(3)
  $1.Capabilities get capabilities => $_getN(2);
  @$pb.TagNumber(3)
  set capabilities($1.Capabilities value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasCapabilities() => $_has(2);
  @$pb.TagNumber(3)
  void clearCapabilities() => $_clearField(3);
  @$pb.TagNumber(3)
  $1.Capabilities ensureCapabilities() => $_ensure(2);

  @$pb.TagNumber(4)
  $fixnum.Int64 get desiredRevision => $_getI64(3);
  @$pb.TagNumber(4)
  set desiredRevision($fixnum.Int64 value) => $_setInt64(3, value);
  @$pb.TagNumber(4)
  $core.bool hasDesiredRevision() => $_has(3);
  @$pb.TagNumber(4)
  void clearDesiredRevision() => $_clearField(4);

  @$pb.TagNumber(5)
  $pb.PbList<$1.ApplicationRecord> get records => $_getList(4);

  @$pb.TagNumber(6)
  $core.bool get locallySuspended => $_getBF(5);
  @$pb.TagNumber(6)
  set locallySuspended($core.bool value) => $_setBool(5, value);
  @$pb.TagNumber(6)
  $core.bool hasLocallySuspended() => $_has(5);
  @$pb.TagNumber(6)
  void clearLocallySuspended() => $_clearField(6);
}

class CheckInResponse extends $pb.GeneratedMessage {
  factory CheckInResponse({
    $1.DesiredState? desired,
  }) {
    final result = CheckInResponse._();
    if (desired != null) result.desired = desired;
    return result;
  }

  CheckInResponse._();

  factory CheckInResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      CheckInResponse()..mergeFromBuffer(data, registry);
  factory CheckInResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      CheckInResponse()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'CheckInResponse',
      package:
          const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_treatment.v1'),
      createEmptyInstance: CheckInResponse.$_createMessage)
    ..aOM<$1.DesiredState>(1, _omitFieldNames ? '' : 'desired',
        subBuilder: $1.DesiredState.$_createMessage)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  CheckInResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  CheckInResponse copyWith(void Function(CheckInResponse) updates) =>
      super.copyWith((message) => updates(message as CheckInResponse))
          as CheckInResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated('Use CheckInResponse() / CheckInResponse.new instead')
  static CheckInResponse create() => CheckInResponse._();
  static $pb.GeneratedMessage $_createMessage() => CheckInResponse._();
  @$core.override
  CheckInResponse createEmptyInstance() => CheckInResponse._();
  @$core.pragma('dart2js:noInline')
  static CheckInResponse getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<CheckInResponse>(
          CheckInResponse.$_createMessage);
  static CheckInResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $1.DesiredState get desired => $_getN(0);
  @$pb.TagNumber(1)
  set desired($1.DesiredState value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasDesired() => $_has(0);
  @$pb.TagNumber(1)
  void clearDesired() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.DesiredState ensureDesired() => $_ensure(0);
}

class GetDesiredStateRequest extends $pb.GeneratedMessage {
  factory GetDesiredStateRequest({
    $core.String? agentId,
  }) {
    final result = GetDesiredStateRequest._();
    if (agentId != null) result.agentId = agentId;
    return result;
  }

  GetDesiredStateRequest._();

  factory GetDesiredStateRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      GetDesiredStateRequest()..mergeFromBuffer(data, registry);
  factory GetDesiredStateRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      GetDesiredStateRequest()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'GetDesiredStateRequest',
      package:
          const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_treatment.v1'),
      createEmptyInstance: GetDesiredStateRequest.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'agentId')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetDesiredStateRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetDesiredStateRequest copyWith(
          void Function(GetDesiredStateRequest) updates) =>
      super.copyWith((message) => updates(message as GetDesiredStateRequest))
          as GetDesiredStateRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated(
      'Use GetDesiredStateRequest() / GetDesiredStateRequest.new instead')
  static GetDesiredStateRequest create() => GetDesiredStateRequest._();
  static $pb.GeneratedMessage $_createMessage() => GetDesiredStateRequest._();
  @$core.override
  GetDesiredStateRequest createEmptyInstance() => GetDesiredStateRequest._();
  @$core.pragma('dart2js:noInline')
  static GetDesiredStateRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<GetDesiredStateRequest>(
          GetDesiredStateRequest.$_createMessage);
  static GetDesiredStateRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get agentId => $_getSZ(0);
  @$pb.TagNumber(1)
  set agentId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasAgentId() => $_has(0);
  @$pb.TagNumber(1)
  void clearAgentId() => $_clearField(1);
}

class GetDesiredStateResponse extends $pb.GeneratedMessage {
  factory GetDesiredStateResponse({
    $1.DesiredState? desired,
  }) {
    final result = GetDesiredStateResponse._();
    if (desired != null) result.desired = desired;
    return result;
  }

  GetDesiredStateResponse._();

  factory GetDesiredStateResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      GetDesiredStateResponse()..mergeFromBuffer(data, registry);
  factory GetDesiredStateResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      GetDesiredStateResponse()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'GetDesiredStateResponse',
      package:
          const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_treatment.v1'),
      createEmptyInstance: GetDesiredStateResponse.$_createMessage)
    ..aOM<$1.DesiredState>(1, _omitFieldNames ? '' : 'desired',
        subBuilder: $1.DesiredState.$_createMessage)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetDesiredStateResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetDesiredStateResponse copyWith(
          void Function(GetDesiredStateResponse) updates) =>
      super.copyWith((message) => updates(message as GetDesiredStateResponse))
          as GetDesiredStateResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated(
      'Use GetDesiredStateResponse() / GetDesiredStateResponse.new instead')
  static GetDesiredStateResponse create() => GetDesiredStateResponse._();
  static $pb.GeneratedMessage $_createMessage() => GetDesiredStateResponse._();
  @$core.override
  GetDesiredStateResponse createEmptyInstance() => GetDesiredStateResponse._();
  @$core.pragma('dart2js:noInline')
  static GetDesiredStateResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<GetDesiredStateResponse>(
          GetDesiredStateResponse.$_createMessage);
  static GetDesiredStateResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $1.DesiredState get desired => $_getN(0);
  @$pb.TagNumber(1)
  set desired($1.DesiredState value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasDesired() => $_has(0);
  @$pb.TagNumber(1)
  void clearDesired() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.DesiredState ensureDesired() => $_ensure(0);
}

class WatchDesiredStateRequest extends $pb.GeneratedMessage {
  factory WatchDesiredStateRequest({
    $core.String? agentId,
    $fixnum.Int64? afterRevision,
  }) {
    final result = WatchDesiredStateRequest._();
    if (agentId != null) result.agentId = agentId;
    if (afterRevision != null) result.afterRevision = afterRevision;
    return result;
  }

  WatchDesiredStateRequest._();

  factory WatchDesiredStateRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      WatchDesiredStateRequest()..mergeFromBuffer(data, registry);
  factory WatchDesiredStateRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      WatchDesiredStateRequest()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'WatchDesiredStateRequest',
      package:
          const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_treatment.v1'),
      createEmptyInstance: WatchDesiredStateRequest.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'agentId')
    ..a<$fixnum.Int64>(
        2, _omitFieldNames ? '' : 'afterRevision', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  WatchDesiredStateRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  WatchDesiredStateRequest copyWith(
          void Function(WatchDesiredStateRequest) updates) =>
      super.copyWith((message) => updates(message as WatchDesiredStateRequest))
          as WatchDesiredStateRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated(
      'Use WatchDesiredStateRequest() / WatchDesiredStateRequest.new instead')
  static WatchDesiredStateRequest create() => WatchDesiredStateRequest._();
  static $pb.GeneratedMessage $_createMessage() => WatchDesiredStateRequest._();
  @$core.override
  WatchDesiredStateRequest createEmptyInstance() =>
      WatchDesiredStateRequest._();
  @$core.pragma('dart2js:noInline')
  static WatchDesiredStateRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<WatchDesiredStateRequest>(
          WatchDesiredStateRequest.$_createMessage);
  static WatchDesiredStateRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get agentId => $_getSZ(0);
  @$pb.TagNumber(1)
  set agentId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasAgentId() => $_has(0);
  @$pb.TagNumber(1)
  void clearAgentId() => $_clearField(1);

  @$pb.TagNumber(2)
  $fixnum.Int64 get afterRevision => $_getI64(1);
  @$pb.TagNumber(2)
  set afterRevision($fixnum.Int64 value) => $_setInt64(1, value);
  @$pb.TagNumber(2)
  $core.bool hasAfterRevision() => $_has(1);
  @$pb.TagNumber(2)
  void clearAfterRevision() => $_clearField(2);
}

class WatchDesiredStateResponse extends $pb.GeneratedMessage {
  factory WatchDesiredStateResponse({
    $fixnum.Int64? revision,
  }) {
    final result = WatchDesiredStateResponse._();
    if (revision != null) result.revision = revision;
    return result;
  }

  WatchDesiredStateResponse._();

  factory WatchDesiredStateResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      WatchDesiredStateResponse()..mergeFromBuffer(data, registry);
  factory WatchDesiredStateResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      WatchDesiredStateResponse()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'WatchDesiredStateResponse',
      package:
          const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_treatment.v1'),
      createEmptyInstance: WatchDesiredStateResponse.$_createMessage)
    ..a<$fixnum.Int64>(
        1, _omitFieldNames ? '' : 'revision', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  WatchDesiredStateResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  WatchDesiredStateResponse copyWith(
          void Function(WatchDesiredStateResponse) updates) =>
      super.copyWith((message) => updates(message as WatchDesiredStateResponse))
          as WatchDesiredStateResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated(
      'Use WatchDesiredStateResponse() / WatchDesiredStateResponse.new instead')
  static WatchDesiredStateResponse create() => WatchDesiredStateResponse._();
  static $pb.GeneratedMessage $_createMessage() =>
      WatchDesiredStateResponse._();
  @$core.override
  WatchDesiredStateResponse createEmptyInstance() =>
      WatchDesiredStateResponse._();
  @$core.pragma('dart2js:noInline')
  static WatchDesiredStateResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<WatchDesiredStateResponse>(
          WatchDesiredStateResponse.$_createMessage);
  static WatchDesiredStateResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $fixnum.Int64 get revision => $_getI64(0);
  @$pb.TagNumber(1)
  set revision($fixnum.Int64 value) => $_setInt64(0, value);
  @$pb.TagNumber(1)
  $core.bool hasRevision() => $_has(0);
  @$pb.TagNumber(1)
  void clearRevision() => $_clearField(1);
}

const $core.bool _omitFieldNames =
    $core.bool.fromEnvironment('protobuf.omit_field_names');
const $core.bool _omitMessageNames =
    $core.bool.fromEnvironment('protobuf.omit_message_names');
