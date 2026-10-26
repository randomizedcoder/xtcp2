// This is a generated file - do not edit.
//
// Generated from xtcp_treatment/v1/operator.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, prefer_relative_imports

import 'dart:core' as $core;

import 'package:fixnum/fixnum.dart' as $fixnum;
import 'package:protobuf/protobuf.dart' as $pb;

import 'agent.pb.dart' as $2;
import 'operator.pbenum.dart';
import 'treatment.pb.dart' as $1;

export 'package:protobuf/protobuf.dart' show GeneratedMessageGenericExtensions;

export 'operator.pbenum.dart';

class ProposeRequest extends $pb.GeneratedMessage {
  factory ProposeRequest({
    $1.Experiment? experiment,
    $core.String? requestId,
  }) {
    final result = ProposeRequest._();
    if (experiment != null) result.experiment = experiment;
    if (requestId != null) result.requestId = requestId;
    return result;
  }

  ProposeRequest._();

  factory ProposeRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ProposeRequest()..mergeFromBuffer(data, registry);
  factory ProposeRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ProposeRequest()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ProposeRequest',
      package:
          const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_treatment.v1'),
      createEmptyInstance: ProposeRequest.$_createMessage)
    ..aOM<$1.Experiment>(1, _omitFieldNames ? '' : 'experiment',
        subBuilder: $1.Experiment.$_createMessage)
    ..aOS(2, _omitFieldNames ? '' : 'requestId')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ProposeRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ProposeRequest copyWith(void Function(ProposeRequest) updates) =>
      super.copyWith((message) => updates(message as ProposeRequest))
          as ProposeRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated('Use ProposeRequest() / ProposeRequest.new instead')
  static ProposeRequest create() => ProposeRequest._();
  static $pb.GeneratedMessage $_createMessage() => ProposeRequest._();
  @$core.override
  ProposeRequest createEmptyInstance() => ProposeRequest._();
  @$core.pragma('dart2js:noInline')
  static ProposeRequest getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<ProposeRequest>(
          ProposeRequest.$_createMessage);
  static ProposeRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $1.Experiment get experiment => $_getN(0);
  @$pb.TagNumber(1)
  set experiment($1.Experiment value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasExperiment() => $_has(0);
  @$pb.TagNumber(1)
  void clearExperiment() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.Experiment ensureExperiment() => $_ensure(0);

  @$pb.TagNumber(2)
  $core.String get requestId => $_getSZ(1);
  @$pb.TagNumber(2)
  set requestId($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasRequestId() => $_has(1);
  @$pb.TagNumber(2)
  void clearRequestId() => $_clearField(2);
}

class ProposeResponse extends $pb.GeneratedMessage {
  factory ProposeResponse({
    $1.Experiment? experiment,
  }) {
    final result = ProposeResponse._();
    if (experiment != null) result.experiment = experiment;
    return result;
  }

  ProposeResponse._();

  factory ProposeResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ProposeResponse()..mergeFromBuffer(data, registry);
  factory ProposeResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ProposeResponse()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ProposeResponse',
      package:
          const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_treatment.v1'),
      createEmptyInstance: ProposeResponse.$_createMessage)
    ..aOM<$1.Experiment>(1, _omitFieldNames ? '' : 'experiment',
        subBuilder: $1.Experiment.$_createMessage)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ProposeResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ProposeResponse copyWith(void Function(ProposeResponse) updates) =>
      super.copyWith((message) => updates(message as ProposeResponse))
          as ProposeResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated('Use ProposeResponse() / ProposeResponse.new instead')
  static ProposeResponse create() => ProposeResponse._();
  static $pb.GeneratedMessage $_createMessage() => ProposeResponse._();
  @$core.override
  ProposeResponse createEmptyInstance() => ProposeResponse._();
  @$core.pragma('dart2js:noInline')
  static ProposeResponse getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<ProposeResponse>(
          ProposeResponse.$_createMessage);
  static ProposeResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $1.Experiment get experiment => $_getN(0);
  @$pb.TagNumber(1)
  set experiment($1.Experiment value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasExperiment() => $_has(0);
  @$pb.TagNumber(1)
  void clearExperiment() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.Experiment ensureExperiment() => $_ensure(0);
}

class AnalyzeRequest extends $pb.GeneratedMessage {
  factory AnalyzeRequest({
    $core.String? experimentId,
    $fixnum.Int64? expectedRevision,
  }) {
    final result = AnalyzeRequest._();
    if (experimentId != null) result.experimentId = experimentId;
    if (expectedRevision != null) result.expectedRevision = expectedRevision;
    return result;
  }

  AnalyzeRequest._();

  factory AnalyzeRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      AnalyzeRequest()..mergeFromBuffer(data, registry);
  factory AnalyzeRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      AnalyzeRequest()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'AnalyzeRequest',
      package:
          const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_treatment.v1'),
      createEmptyInstance: AnalyzeRequest.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'experimentId')
    ..a<$fixnum.Int64>(
        2, _omitFieldNames ? '' : 'expectedRevision', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  AnalyzeRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  AnalyzeRequest copyWith(void Function(AnalyzeRequest) updates) =>
      super.copyWith((message) => updates(message as AnalyzeRequest))
          as AnalyzeRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated('Use AnalyzeRequest() / AnalyzeRequest.new instead')
  static AnalyzeRequest create() => AnalyzeRequest._();
  static $pb.GeneratedMessage $_createMessage() => AnalyzeRequest._();
  @$core.override
  AnalyzeRequest createEmptyInstance() => AnalyzeRequest._();
  @$core.pragma('dart2js:noInline')
  static AnalyzeRequest getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<AnalyzeRequest>(
          AnalyzeRequest.$_createMessage);
  static AnalyzeRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get experimentId => $_getSZ(0);
  @$pb.TagNumber(1)
  set experimentId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasExperimentId() => $_has(0);
  @$pb.TagNumber(1)
  void clearExperimentId() => $_clearField(1);

  @$pb.TagNumber(2)
  $fixnum.Int64 get expectedRevision => $_getI64(1);
  @$pb.TagNumber(2)
  set expectedRevision($fixnum.Int64 value) => $_setInt64(1, value);
  @$pb.TagNumber(2)
  $core.bool hasExpectedRevision() => $_has(1);
  @$pb.TagNumber(2)
  void clearExpectedRevision() => $_clearField(2);
}

class AnalyzeResponse extends $pb.GeneratedMessage {
  factory AnalyzeResponse({
    $1.Evidence? evidence,
    $1.Outcome? outcome,
  }) {
    final result = AnalyzeResponse._();
    if (evidence != null) result.evidence = evidence;
    if (outcome != null) result.outcome = outcome;
    return result;
  }

  AnalyzeResponse._();

  factory AnalyzeResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      AnalyzeResponse()..mergeFromBuffer(data, registry);
  factory AnalyzeResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      AnalyzeResponse()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'AnalyzeResponse',
      package:
          const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_treatment.v1'),
      createEmptyInstance: AnalyzeResponse.$_createMessage)
    ..aOM<$1.Evidence>(1, _omitFieldNames ? '' : 'evidence',
        subBuilder: $1.Evidence.$_createMessage)
    ..aE<$1.Outcome>(2, _omitFieldNames ? '' : 'outcome',
        enumValues: $1.Outcome.values)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  AnalyzeResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  AnalyzeResponse copyWith(void Function(AnalyzeResponse) updates) =>
      super.copyWith((message) => updates(message as AnalyzeResponse))
          as AnalyzeResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated('Use AnalyzeResponse() / AnalyzeResponse.new instead')
  static AnalyzeResponse create() => AnalyzeResponse._();
  static $pb.GeneratedMessage $_createMessage() => AnalyzeResponse._();
  @$core.override
  AnalyzeResponse createEmptyInstance() => AnalyzeResponse._();
  @$core.pragma('dart2js:noInline')
  static AnalyzeResponse getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<AnalyzeResponse>(
          AnalyzeResponse.$_createMessage);
  static AnalyzeResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $1.Evidence get evidence => $_getN(0);
  @$pb.TagNumber(1)
  set evidence($1.Evidence value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasEvidence() => $_has(0);
  @$pb.TagNumber(1)
  void clearEvidence() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.Evidence ensureEvidence() => $_ensure(0);

  @$pb.TagNumber(2)
  $1.Outcome get outcome => $_getN(1);
  @$pb.TagNumber(2)
  set outcome($1.Outcome value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasOutcome() => $_has(1);
  @$pb.TagNumber(2)
  void clearOutcome() => $_clearField(2);
}

class ReviewRequest extends $pb.GeneratedMessage {
  factory ReviewRequest({
    $core.String? experimentId,
    $fixnum.Int64? expectedRevision,
    $core.bool? approve,
    $core.String? reason,
    $core.String? requestId,
    $1.Evidence? evidence,
  }) {
    final result = ReviewRequest._();
    if (experimentId != null) result.experimentId = experimentId;
    if (expectedRevision != null) result.expectedRevision = expectedRevision;
    if (approve != null) result.approve = approve;
    if (reason != null) result.reason = reason;
    if (requestId != null) result.requestId = requestId;
    if (evidence != null) result.evidence = evidence;
    return result;
  }

  ReviewRequest._();

  factory ReviewRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ReviewRequest()..mergeFromBuffer(data, registry);
  factory ReviewRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ReviewRequest()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ReviewRequest',
      package:
          const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_treatment.v1'),
      createEmptyInstance: ReviewRequest.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'experimentId')
    ..a<$fixnum.Int64>(
        2, _omitFieldNames ? '' : 'expectedRevision', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..aOB(3, _omitFieldNames ? '' : 'approve')
    ..aOS(4, _omitFieldNames ? '' : 'reason')
    ..aOS(5, _omitFieldNames ? '' : 'requestId')
    ..aOM<$1.Evidence>(6, _omitFieldNames ? '' : 'evidence',
        subBuilder: $1.Evidence.$_createMessage)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ReviewRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ReviewRequest copyWith(void Function(ReviewRequest) updates) =>
      super.copyWith((message) => updates(message as ReviewRequest))
          as ReviewRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated('Use ReviewRequest() / ReviewRequest.new instead')
  static ReviewRequest create() => ReviewRequest._();
  static $pb.GeneratedMessage $_createMessage() => ReviewRequest._();
  @$core.override
  ReviewRequest createEmptyInstance() => ReviewRequest._();
  @$core.pragma('dart2js:noInline')
  static ReviewRequest getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<ReviewRequest>(
          ReviewRequest.$_createMessage);
  static ReviewRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get experimentId => $_getSZ(0);
  @$pb.TagNumber(1)
  set experimentId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasExperimentId() => $_has(0);
  @$pb.TagNumber(1)
  void clearExperimentId() => $_clearField(1);

  @$pb.TagNumber(2)
  $fixnum.Int64 get expectedRevision => $_getI64(1);
  @$pb.TagNumber(2)
  set expectedRevision($fixnum.Int64 value) => $_setInt64(1, value);
  @$pb.TagNumber(2)
  $core.bool hasExpectedRevision() => $_has(1);
  @$pb.TagNumber(2)
  void clearExpectedRevision() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.bool get approve => $_getBF(2);
  @$pb.TagNumber(3)
  set approve($core.bool value) => $_setBool(2, value);
  @$pb.TagNumber(3)
  $core.bool hasApprove() => $_has(2);
  @$pb.TagNumber(3)
  void clearApprove() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.String get reason => $_getSZ(3);
  @$pb.TagNumber(4)
  set reason($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasReason() => $_has(3);
  @$pb.TagNumber(4)
  void clearReason() => $_clearField(4);

  @$pb.TagNumber(5)
  $core.String get requestId => $_getSZ(4);
  @$pb.TagNumber(5)
  set requestId($core.String value) => $_setString(4, value);
  @$pb.TagNumber(5)
  $core.bool hasRequestId() => $_has(4);
  @$pb.TagNumber(5)
  void clearRequestId() => $_clearField(5);

  /// Initial review uses baseline; subsequent reviews require fresh evidence.
  @$pb.TagNumber(6)
  $1.Evidence get evidence => $_getN(5);
  @$pb.TagNumber(6)
  set evidence($1.Evidence value) => $_setField(6, value);
  @$pb.TagNumber(6)
  $core.bool hasEvidence() => $_has(5);
  @$pb.TagNumber(6)
  void clearEvidence() => $_clearField(6);
  @$pb.TagNumber(6)
  $1.Evidence ensureEvidence() => $_ensure(5);
}

class ReviewResponse extends $pb.GeneratedMessage {
  factory ReviewResponse({
    $1.Experiment? experiment,
  }) {
    final result = ReviewResponse._();
    if (experiment != null) result.experiment = experiment;
    return result;
  }

  ReviewResponse._();

  factory ReviewResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ReviewResponse()..mergeFromBuffer(data, registry);
  factory ReviewResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ReviewResponse()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ReviewResponse',
      package:
          const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_treatment.v1'),
      createEmptyInstance: ReviewResponse.$_createMessage)
    ..aOM<$1.Experiment>(1, _omitFieldNames ? '' : 'experiment',
        subBuilder: $1.Experiment.$_createMessage)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ReviewResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ReviewResponse copyWith(void Function(ReviewResponse) updates) =>
      super.copyWith((message) => updates(message as ReviewResponse))
          as ReviewResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated('Use ReviewResponse() / ReviewResponse.new instead')
  static ReviewResponse create() => ReviewResponse._();
  static $pb.GeneratedMessage $_createMessage() => ReviewResponse._();
  @$core.override
  ReviewResponse createEmptyInstance() => ReviewResponse._();
  @$core.pragma('dart2js:noInline')
  static ReviewResponse getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<ReviewResponse>(
          ReviewResponse.$_createMessage);
  static ReviewResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $1.Experiment get experiment => $_getN(0);
  @$pb.TagNumber(1)
  set experiment($1.Experiment value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasExperiment() => $_has(0);
  @$pb.TagNumber(1)
  void clearExperiment() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.Experiment ensureExperiment() => $_ensure(0);
}

class ControlRequest extends $pb.GeneratedMessage {
  factory ControlRequest({
    $core.String? experimentId,
    $core.String? agentId,
    $core.String? location,
    $core.bool? fleet,
    ControlAction? action,
    $core.String? reason,
    $core.String? requestId,
  }) {
    final result = ControlRequest._();
    if (experimentId != null) result.experimentId = experimentId;
    if (agentId != null) result.agentId = agentId;
    if (location != null) result.location = location;
    if (fleet != null) result.fleet = fleet;
    if (action != null) result.action = action;
    if (reason != null) result.reason = reason;
    if (requestId != null) result.requestId = requestId;
    return result;
  }

  ControlRequest._();

  factory ControlRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ControlRequest()..mergeFromBuffer(data, registry);
  factory ControlRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ControlRequest()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ControlRequest',
      package:
          const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_treatment.v1'),
      createEmptyInstance: ControlRequest.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'experimentId')
    ..aOS(2, _omitFieldNames ? '' : 'agentId')
    ..aOS(3, _omitFieldNames ? '' : 'location')
    ..aOB(4, _omitFieldNames ? '' : 'fleet')
    ..aE<ControlAction>(5, _omitFieldNames ? '' : 'action',
        enumValues: ControlAction.values)
    ..aOS(6, _omitFieldNames ? '' : 'reason')
    ..aOS(7, _omitFieldNames ? '' : 'requestId')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ControlRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ControlRequest copyWith(void Function(ControlRequest) updates) =>
      super.copyWith((message) => updates(message as ControlRequest))
          as ControlRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated('Use ControlRequest() / ControlRequest.new instead')
  static ControlRequest create() => ControlRequest._();
  static $pb.GeneratedMessage $_createMessage() => ControlRequest._();
  @$core.override
  ControlRequest createEmptyInstance() => ControlRequest._();
  @$core.pragma('dart2js:noInline')
  static ControlRequest getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<ControlRequest>(
          ControlRequest.$_createMessage);
  static ControlRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get experimentId => $_getSZ(0);
  @$pb.TagNumber(1)
  set experimentId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasExperimentId() => $_has(0);
  @$pb.TagNumber(1)
  void clearExperimentId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get agentId => $_getSZ(1);
  @$pb.TagNumber(2)
  set agentId($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasAgentId() => $_has(1);
  @$pb.TagNumber(2)
  void clearAgentId() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get location => $_getSZ(2);
  @$pb.TagNumber(3)
  set location($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasLocation() => $_has(2);
  @$pb.TagNumber(3)
  void clearLocation() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.bool get fleet => $_getBF(3);
  @$pb.TagNumber(4)
  set fleet($core.bool value) => $_setBool(3, value);
  @$pb.TagNumber(4)
  $core.bool hasFleet() => $_has(3);
  @$pb.TagNumber(4)
  void clearFleet() => $_clearField(4);

  @$pb.TagNumber(5)
  ControlAction get action => $_getN(4);
  @$pb.TagNumber(5)
  set action(ControlAction value) => $_setField(5, value);
  @$pb.TagNumber(5)
  $core.bool hasAction() => $_has(4);
  @$pb.TagNumber(5)
  void clearAction() => $_clearField(5);

  @$pb.TagNumber(6)
  $core.String get reason => $_getSZ(5);
  @$pb.TagNumber(6)
  set reason($core.String value) => $_setString(5, value);
  @$pb.TagNumber(6)
  $core.bool hasReason() => $_has(5);
  @$pb.TagNumber(6)
  void clearReason() => $_clearField(6);

  @$pb.TagNumber(7)
  $core.String get requestId => $_getSZ(6);
  @$pb.TagNumber(7)
  set requestId($core.String value) => $_setString(6, value);
  @$pb.TagNumber(7)
  $core.bool hasRequestId() => $_has(6);
  @$pb.TagNumber(7)
  void clearRequestId() => $_clearField(7);
}

class ControlResponse extends $pb.GeneratedMessage {
  factory ControlResponse({
    $core.int? affectedAgents,
  }) {
    final result = ControlResponse._();
    if (affectedAgents != null) result.affectedAgents = affectedAgents;
    return result;
  }

  ControlResponse._();

  factory ControlResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ControlResponse()..mergeFromBuffer(data, registry);
  factory ControlResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ControlResponse()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ControlResponse',
      package:
          const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_treatment.v1'),
      createEmptyInstance: ControlResponse.$_createMessage)
    ..aI(1, _omitFieldNames ? '' : 'affectedAgents',
        fieldType: $pb.PbFieldType.OU3)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ControlResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ControlResponse copyWith(void Function(ControlResponse) updates) =>
      super.copyWith((message) => updates(message as ControlResponse))
          as ControlResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated('Use ControlResponse() / ControlResponse.new instead')
  static ControlResponse create() => ControlResponse._();
  static $pb.GeneratedMessage $_createMessage() => ControlResponse._();
  @$core.override
  ControlResponse createEmptyInstance() => ControlResponse._();
  @$core.pragma('dart2js:noInline')
  static ControlResponse getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<ControlResponse>(
          ControlResponse.$_createMessage);
  static ControlResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $core.int get affectedAgents => $_getIZ(0);
  @$pb.TagNumber(1)
  set affectedAgents($core.int value) => $_setUnsignedInt32(0, value);
  @$pb.TagNumber(1)
  $core.bool hasAffectedAgents() => $_has(0);
  @$pb.TagNumber(1)
  void clearAffectedAgents() => $_clearField(1);
}

class InspectRequest extends $pb.GeneratedMessage {
  factory InspectRequest({
    $core.String? experimentId,
    $core.String? agentId,
  }) {
    final result = InspectRequest._();
    if (experimentId != null) result.experimentId = experimentId;
    if (agentId != null) result.agentId = agentId;
    return result;
  }

  InspectRequest._();

  factory InspectRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      InspectRequest()..mergeFromBuffer(data, registry);
  factory InspectRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      InspectRequest()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'InspectRequest',
      package:
          const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_treatment.v1'),
      createEmptyInstance: InspectRequest.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'experimentId')
    ..aOS(2, _omitFieldNames ? '' : 'agentId')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  InspectRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  InspectRequest copyWith(void Function(InspectRequest) updates) =>
      super.copyWith((message) => updates(message as InspectRequest))
          as InspectRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated('Use InspectRequest() / InspectRequest.new instead')
  static InspectRequest create() => InspectRequest._();
  static $pb.GeneratedMessage $_createMessage() => InspectRequest._();
  @$core.override
  InspectRequest createEmptyInstance() => InspectRequest._();
  @$core.pragma('dart2js:noInline')
  static InspectRequest getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<InspectRequest>(
          InspectRequest.$_createMessage);
  static InspectRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get experimentId => $_getSZ(0);
  @$pb.TagNumber(1)
  set experimentId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasExperimentId() => $_has(0);
  @$pb.TagNumber(1)
  void clearExperimentId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get agentId => $_getSZ(1);
  @$pb.TagNumber(2)
  set agentId($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasAgentId() => $_has(1);
  @$pb.TagNumber(2)
  void clearAgentId() => $_clearField(2);
}

class InspectResponse extends $pb.GeneratedMessage {
  factory InspectResponse({
    $core.Iterable<$1.Experiment>? experiments,
    $core.Iterable<$1.DesiredState>? desired,
    $core.Iterable<$2.CheckInRequest>? observed,
  }) {
    final result = InspectResponse._();
    if (experiments != null) result.experiments.addAll(experiments);
    if (desired != null) result.desired.addAll(desired);
    if (observed != null) result.observed.addAll(observed);
    return result;
  }

  InspectResponse._();

  factory InspectResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      InspectResponse()..mergeFromBuffer(data, registry);
  factory InspectResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      InspectResponse()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'InspectResponse',
      package:
          const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_treatment.v1'),
      createEmptyInstance: InspectResponse.$_createMessage)
    ..pPM<$1.Experiment>(1, _omitFieldNames ? '' : 'experiments',
        subBuilder: $1.Experiment.$_createMessage)
    ..pPM<$1.DesiredState>(2, _omitFieldNames ? '' : 'desired',
        subBuilder: $1.DesiredState.$_createMessage)
    ..pPM<$2.CheckInRequest>(3, _omitFieldNames ? '' : 'observed',
        subBuilder: $2.CheckInRequest.$_createMessage)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  InspectResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  InspectResponse copyWith(void Function(InspectResponse) updates) =>
      super.copyWith((message) => updates(message as InspectResponse))
          as InspectResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated('Use InspectResponse() / InspectResponse.new instead')
  static InspectResponse create() => InspectResponse._();
  static $pb.GeneratedMessage $_createMessage() => InspectResponse._();
  @$core.override
  InspectResponse createEmptyInstance() => InspectResponse._();
  @$core.pragma('dart2js:noInline')
  static InspectResponse getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<InspectResponse>(
          InspectResponse.$_createMessage);
  static InspectResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $pb.PbList<$1.Experiment> get experiments => $_getList(0);

  @$pb.TagNumber(2)
  $pb.PbList<$1.DesiredState> get desired => $_getList(1);

  @$pb.TagNumber(3)
  $pb.PbList<$2.CheckInRequest> get observed => $_getList(2);
}

const $core.bool _omitFieldNames =
    $core.bool.fromEnvironment('protobuf.omit_field_names');
const $core.bool _omitMessageNames =
    $core.bool.fromEnvironment('protobuf.omit_message_names');
