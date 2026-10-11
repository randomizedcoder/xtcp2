// This is a generated file - do not edit.
//
// Generated from linkmonitor/v1/linkmonitor.proto.

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
    as $2;
import 'package:protobuf/well_known_types/google/protobuf/timestamp.pb.dart'
    as $1;

import 'linkmonitor.pbenum.dart';

export 'package:protobuf/protobuf.dart' show GeneratedMessageGenericExtensions;

export 'linkmonitor.pbenum.dart';

/// Health mirrors linkmonitor.Health.
class Health extends $pb.GeneratedMessage {
  factory Health({
    $core.bool? running,
    $core.bool? ready,
    $core.bool? collectionHealthy,
    $core.bool? baselineReady,
  }) {
    final result = create();
    if (running != null) result.running = running;
    if (ready != null) result.ready = ready;
    if (collectionHealthy != null) result.collectionHealthy = collectionHealthy;
    if (baselineReady != null) result.baselineReady = baselineReady;
    return result;
  }

  Health._();

  factory Health.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory Health.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'Health',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'linkmonitor.v1'),
      createEmptyInstance: create)
    ..aOB(1, _omitFieldNames ? '' : 'running')
    ..aOB(2, _omitFieldNames ? '' : 'ready')
    ..aOB(3, _omitFieldNames ? '' : 'collectionHealthy')
    ..aOB(4, _omitFieldNames ? '' : 'baselineReady')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Health clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Health copyWith(void Function(Health) updates) =>
      super.copyWith((message) => updates(message as Health)) as Health;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static Health create() => Health._();
  @$core.override
  Health createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static Health getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<Health>(create);
  static Health? _defaultInstance;

  @$pb.TagNumber(1)
  $core.bool get running => $_getBF(0);
  @$pb.TagNumber(1)
  set running($core.bool value) => $_setBool(0, value);
  @$pb.TagNumber(1)
  $core.bool hasRunning() => $_has(0);
  @$pb.TagNumber(1)
  void clearRunning() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.bool get ready => $_getBF(1);
  @$pb.TagNumber(2)
  set ready($core.bool value) => $_setBool(1, value);
  @$pb.TagNumber(2)
  $core.bool hasReady() => $_has(1);
  @$pb.TagNumber(2)
  void clearReady() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.bool get collectionHealthy => $_getBF(2);
  @$pb.TagNumber(3)
  set collectionHealthy($core.bool value) => $_setBool(2, value);
  @$pb.TagNumber(3)
  $core.bool hasCollectionHealthy() => $_has(2);
  @$pb.TagNumber(3)
  void clearCollectionHealthy() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.bool get baselineReady => $_getBF(3);
  @$pb.TagNumber(4)
  set baselineReady($core.bool value) => $_setBool(3, value);
  @$pb.TagNumber(4)
  $core.bool hasBaselineReady() => $_has(3);
  @$pb.TagNumber(4)
  void clearBaselineReady() => $_clearField(4);
}

/// LinkCounts are exact counts from one publication. Unset means unknown.
class LinkCounts extends $pb.GeneratedMessage {
  factory LinkCounts({
    $fixnum.Int64? current,
    $fixnum.Int64? expected,
    $fixnum.Int64? delta,
  }) {
    final result = create();
    if (current != null) result.current = current;
    if (expected != null) result.expected = expected;
    if (delta != null) result.delta = delta;
    return result;
  }

  LinkCounts._();

  factory LinkCounts.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory LinkCounts.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'LinkCounts',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'linkmonitor.v1'),
      createEmptyInstance: create)
    ..a<$fixnum.Int64>(1, _omitFieldNames ? '' : 'current', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..a<$fixnum.Int64>(
        2, _omitFieldNames ? '' : 'expected', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..a<$fixnum.Int64>(3, _omitFieldNames ? '' : 'delta', $pb.PbFieldType.OS6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  LinkCounts clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  LinkCounts copyWith(void Function(LinkCounts) updates) =>
      super.copyWith((message) => updates(message as LinkCounts)) as LinkCounts;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static LinkCounts create() => LinkCounts._();
  @$core.override
  LinkCounts createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static LinkCounts getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<LinkCounts>(create);
  static LinkCounts? _defaultInstance;

  @$pb.TagNumber(1)
  $fixnum.Int64 get current => $_getI64(0);
  @$pb.TagNumber(1)
  set current($fixnum.Int64 value) => $_setInt64(0, value);
  @$pb.TagNumber(1)
  $core.bool hasCurrent() => $_has(0);
  @$pb.TagNumber(1)
  void clearCurrent() => $_clearField(1);

  @$pb.TagNumber(2)
  $fixnum.Int64 get expected => $_getI64(1);
  @$pb.TagNumber(2)
  set expected($fixnum.Int64 value) => $_setInt64(1, value);
  @$pb.TagNumber(2)
  $core.bool hasExpected() => $_has(1);
  @$pb.TagNumber(2)
  void clearExpected() => $_clearField(2);

  /// current - expected, set when both are known.
  @$pb.TagNumber(3)
  $fixnum.Int64 get delta => $_getI64(2);
  @$pb.TagNumber(3)
  set delta($fixnum.Int64 value) => $_setInt64(2, value);
  @$pb.TagNumber(3)
  $core.bool hasDelta() => $_has(2);
  @$pb.TagNumber(3)
  void clearDelta() => $_clearField(3);
}

/// ControlProgress counts passes of one control in this monitoring lifetime.
class ControlProgress extends $pb.GeneratedMessage {
  factory ControlProgress({
    $fixnum.Int64? requested,
    $fixnum.Int64? completed,
  }) {
    final result = create();
    if (requested != null) result.requested = requested;
    if (completed != null) result.completed = completed;
    return result;
  }

  ControlProgress._();

  factory ControlProgress.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ControlProgress.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ControlProgress',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'linkmonitor.v1'),
      createEmptyInstance: create)
    ..a<$fixnum.Int64>(
        1, _omitFieldNames ? '' : 'requested', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..a<$fixnum.Int64>(
        2, _omitFieldNames ? '' : 'completed', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ControlProgress clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ControlProgress copyWith(void Function(ControlProgress) updates) =>
      super.copyWith((message) => updates(message as ControlProgress))
          as ControlProgress;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ControlProgress create() => ControlProgress._();
  @$core.override
  ControlProgress createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static ControlProgress getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ControlProgress>(create);
  static ControlProgress? _defaultInstance;

  @$pb.TagNumber(1)
  $fixnum.Int64 get requested => $_getI64(0);
  @$pb.TagNumber(1)
  set requested($fixnum.Int64 value) => $_setInt64(0, value);
  @$pb.TagNumber(1)
  $core.bool hasRequested() => $_has(0);
  @$pb.TagNumber(1)
  void clearRequested() => $_clearField(1);

  @$pb.TagNumber(2)
  $fixnum.Int64 get completed => $_getI64(1);
  @$pb.TagNumber(2)
  set completed($fixnum.Int64 value) => $_setInt64(1, value);
  @$pb.TagNumber(2)
  $core.bool hasCompleted() => $_has(1);
  @$pb.TagNumber(2)
  void clearCompleted() => $_clearField(2);
}

class GetStatusRequest extends $pb.GeneratedMessage {
  factory GetStatusRequest() => create();

  GetStatusRequest._();

  factory GetStatusRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory GetStatusRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'GetStatusRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'linkmonitor.v1'),
      createEmptyInstance: create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetStatusRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetStatusRequest copyWith(void Function(GetStatusRequest) updates) =>
      super.copyWith((message) => updates(message as GetStatusRequest))
          as GetStatusRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static GetStatusRequest create() => GetStatusRequest._();
  @$core.override
  GetStatusRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static GetStatusRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<GetStatusRequest>(create);
  static GetStatusRequest? _defaultInstance;
}

class GetStatusResponse extends $pb.GeneratedMessage {
  factory GetStatusResponse({
    $fixnum.Int64? snapshotVersion,
    Health? health,
    LinkCounts? counts,
    $1.Timestamp? baselineRecordedAt,
    $fixnum.Int64? baselineWriteErrors,
    $1.Timestamp? lastSuccessfulResync,
    ControlProgress? resync,
    ControlProgress? rebaseline,
  }) {
    final result = create();
    if (snapshotVersion != null) result.snapshotVersion = snapshotVersion;
    if (health != null) result.health = health;
    if (counts != null) result.counts = counts;
    if (baselineRecordedAt != null)
      result.baselineRecordedAt = baselineRecordedAt;
    if (baselineWriteErrors != null)
      result.baselineWriteErrors = baselineWriteErrors;
    if (lastSuccessfulResync != null)
      result.lastSuccessfulResync = lastSuccessfulResync;
    if (resync != null) result.resync = resync;
    if (rebaseline != null) result.rebaseline = rebaseline;
    return result;
  }

  GetStatusResponse._();

  factory GetStatusResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory GetStatusResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'GetStatusResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'linkmonitor.v1'),
      createEmptyInstance: create)
    ..a<$fixnum.Int64>(
        1, _omitFieldNames ? '' : 'snapshotVersion', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..aOM<Health>(2, _omitFieldNames ? '' : 'health', subBuilder: Health.create)
    ..aOM<LinkCounts>(3, _omitFieldNames ? '' : 'counts',
        subBuilder: LinkCounts.create)
    ..aOM<$1.Timestamp>(4, _omitFieldNames ? '' : 'baselineRecordedAt',
        subBuilder: $1.Timestamp.create)
    ..a<$fixnum.Int64>(
        5, _omitFieldNames ? '' : 'baselineWriteErrors', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..aOM<$1.Timestamp>(6, _omitFieldNames ? '' : 'lastSuccessfulResync',
        subBuilder: $1.Timestamp.create)
    ..aOM<ControlProgress>(7, _omitFieldNames ? '' : 'resync',
        subBuilder: ControlProgress.create)
    ..aOM<ControlProgress>(8, _omitFieldNames ? '' : 'rebaseline',
        subBuilder: ControlProgress.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetStatusResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetStatusResponse copyWith(void Function(GetStatusResponse) updates) =>
      super.copyWith((message) => updates(message as GetStatusResponse))
          as GetStatusResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static GetStatusResponse create() => GetStatusResponse._();
  @$core.override
  GetStatusResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static GetStatusResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<GetStatusResponse>(create);
  static GetStatusResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $fixnum.Int64 get snapshotVersion => $_getI64(0);
  @$pb.TagNumber(1)
  set snapshotVersion($fixnum.Int64 value) => $_setInt64(0, value);
  @$pb.TagNumber(1)
  $core.bool hasSnapshotVersion() => $_has(0);
  @$pb.TagNumber(1)
  void clearSnapshotVersion() => $_clearField(1);

  @$pb.TagNumber(2)
  Health get health => $_getN(1);
  @$pb.TagNumber(2)
  set health(Health value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasHealth() => $_has(1);
  @$pb.TagNumber(2)
  void clearHealth() => $_clearField(2);
  @$pb.TagNumber(2)
  Health ensureHealth() => $_ensure(1);

  @$pb.TagNumber(3)
  LinkCounts get counts => $_getN(2);
  @$pb.TagNumber(3)
  set counts(LinkCounts value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasCounts() => $_has(2);
  @$pb.TagNumber(3)
  void clearCounts() => $_clearField(3);
  @$pb.TagNumber(3)
  LinkCounts ensureCounts() => $_ensure(2);

  /// When the expected count in effect was recorded; unset if unknown.
  @$pb.TagNumber(4)
  $1.Timestamp get baselineRecordedAt => $_getN(3);
  @$pb.TagNumber(4)
  set baselineRecordedAt($1.Timestamp value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasBaselineRecordedAt() => $_has(3);
  @$pb.TagNumber(4)
  void clearBaselineRecordedAt() => $_clearField(4);
  @$pb.TagNumber(4)
  $1.Timestamp ensureBaselineRecordedAt() => $_ensure(3);

  @$pb.TagNumber(5)
  $fixnum.Int64 get baselineWriteErrors => $_getI64(4);
  @$pb.TagNumber(5)
  set baselineWriteErrors($fixnum.Int64 value) => $_setInt64(4, value);
  @$pb.TagNumber(5)
  $core.bool hasBaselineWriteErrors() => $_has(4);
  @$pb.TagNumber(5)
  void clearBaselineWriteErrors() => $_clearField(5);

  @$pb.TagNumber(6)
  $1.Timestamp get lastSuccessfulResync => $_getN(5);
  @$pb.TagNumber(6)
  set lastSuccessfulResync($1.Timestamp value) => $_setField(6, value);
  @$pb.TagNumber(6)
  $core.bool hasLastSuccessfulResync() => $_has(5);
  @$pb.TagNumber(6)
  void clearLastSuccessfulResync() => $_clearField(6);
  @$pb.TagNumber(6)
  $1.Timestamp ensureLastSuccessfulResync() => $_ensure(5);

  @$pb.TagNumber(7)
  ControlProgress get resync => $_getN(6);
  @$pb.TagNumber(7)
  set resync(ControlProgress value) => $_setField(7, value);
  @$pb.TagNumber(7)
  $core.bool hasResync() => $_has(6);
  @$pb.TagNumber(7)
  void clearResync() => $_clearField(7);
  @$pb.TagNumber(7)
  ControlProgress ensureResync() => $_ensure(6);

  @$pb.TagNumber(8)
  ControlProgress get rebaseline => $_getN(7);
  @$pb.TagNumber(8)
  set rebaseline(ControlProgress value) => $_setField(8, value);
  @$pb.TagNumber(8)
  $core.bool hasRebaseline() => $_has(7);
  @$pb.TagNumber(8)
  void clearRebaseline() => $_clearField(8);
  @$pb.TagNumber(8)
  ControlProgress ensureRebaseline() => $_ensure(7);
}

class Interface extends $pb.GeneratedMessage {
  factory Interface({
    $core.String? name,
    $core.String? identity,
    $fixnum.Int64? generation,
    $core.bool? up,
    Eligibility? eligibility,
    CheckStatus? maximumSpeed,
    CheckStatus? maximumWidth,
    CheckStatus? fullDuplex,
    CheckStatus? rdmaReadiness,
    $core.bool? maximumSpeedException,
    $fixnum.Int64? upTransitions,
    $fixnum.Int64? downTransitions,
  }) {
    final result = create();
    if (name != null) result.name = name;
    if (identity != null) result.identity = identity;
    if (generation != null) result.generation = generation;
    if (up != null) result.up = up;
    if (eligibility != null) result.eligibility = eligibility;
    if (maximumSpeed != null) result.maximumSpeed = maximumSpeed;
    if (maximumWidth != null) result.maximumWidth = maximumWidth;
    if (fullDuplex != null) result.fullDuplex = fullDuplex;
    if (rdmaReadiness != null) result.rdmaReadiness = rdmaReadiness;
    if (maximumSpeedException != null)
      result.maximumSpeedException = maximumSpeedException;
    if (upTransitions != null) result.upTransitions = upTransitions;
    if (downTransitions != null) result.downTransitions = downTransitions;
    return result;
  }

  Interface._();

  factory Interface.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory Interface.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'Interface',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'linkmonitor.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'name')
    ..aOS(2, _omitFieldNames ? '' : 'identity')
    ..a<$fixnum.Int64>(
        3, _omitFieldNames ? '' : 'generation', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..aOB(4, _omitFieldNames ? '' : 'up')
    ..aE<Eligibility>(5, _omitFieldNames ? '' : 'eligibility',
        enumValues: Eligibility.values)
    ..aE<CheckStatus>(6, _omitFieldNames ? '' : 'maximumSpeed',
        enumValues: CheckStatus.values)
    ..aE<CheckStatus>(7, _omitFieldNames ? '' : 'maximumWidth',
        enumValues: CheckStatus.values)
    ..aE<CheckStatus>(8, _omitFieldNames ? '' : 'fullDuplex',
        enumValues: CheckStatus.values)
    ..aE<CheckStatus>(9, _omitFieldNames ? '' : 'rdmaReadiness',
        enumValues: CheckStatus.values)
    ..aOB(10, _omitFieldNames ? '' : 'maximumSpeedException')
    ..a<$fixnum.Int64>(
        11, _omitFieldNames ? '' : 'upTransitions', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..a<$fixnum.Int64>(
        12, _omitFieldNames ? '' : 'downTransitions', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Interface clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Interface copyWith(void Function(Interface) updates) =>
      super.copyWith((message) => updates(message as Interface)) as Interface;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static Interface create() => Interface._();
  @$core.override
  Interface createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static Interface getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<Interface>(create);
  static Interface? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get name => $_getSZ(0);
  @$pb.TagNumber(1)
  set name($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasName() => $_has(0);
  @$pb.TagNumber(1)
  void clearName() => $_clearField(1);

  /// Canonical device key within the monitored namespace.
  @$pb.TagNumber(2)
  $core.String get identity => $_getSZ(1);
  @$pb.TagNumber(2)
  set identity($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasIdentity() => $_has(1);
  @$pb.TagNumber(2)
  void clearIdentity() => $_clearField(2);

  /// Distinguishes removal and recreation, even when the kernel reuses IDs.
  @$pb.TagNumber(3)
  $fixnum.Int64 get generation => $_getI64(2);
  @$pb.TagNumber(3)
  set generation($fixnum.Int64 value) => $_setInt64(2, value);
  @$pb.TagNumber(3)
  $core.bool hasGeneration() => $_has(2);
  @$pb.TagNumber(3)
  void clearGeneration() => $_clearField(3);

  /// Unset when the operational state is unknown.
  @$pb.TagNumber(4)
  $core.bool get up => $_getBF(3);
  @$pb.TagNumber(4)
  set up($core.bool value) => $_setBool(3, value);
  @$pb.TagNumber(4)
  $core.bool hasUp() => $_has(3);
  @$pb.TagNumber(4)
  void clearUp() => $_clearField(4);

  @$pb.TagNumber(5)
  Eligibility get eligibility => $_getN(4);
  @$pb.TagNumber(5)
  set eligibility(Eligibility value) => $_setField(5, value);
  @$pb.TagNumber(5)
  $core.bool hasEligibility() => $_has(4);
  @$pb.TagNumber(5)
  void clearEligibility() => $_clearField(5);

  @$pb.TagNumber(6)
  CheckStatus get maximumSpeed => $_getN(5);
  @$pb.TagNumber(6)
  set maximumSpeed(CheckStatus value) => $_setField(6, value);
  @$pb.TagNumber(6)
  $core.bool hasMaximumSpeed() => $_has(5);
  @$pb.TagNumber(6)
  void clearMaximumSpeed() => $_clearField(6);

  @$pb.TagNumber(7)
  CheckStatus get maximumWidth => $_getN(6);
  @$pb.TagNumber(7)
  set maximumWidth(CheckStatus value) => $_setField(7, value);
  @$pb.TagNumber(7)
  $core.bool hasMaximumWidth() => $_has(6);
  @$pb.TagNumber(7)
  void clearMaximumWidth() => $_clearField(7);

  @$pb.TagNumber(8)
  CheckStatus get fullDuplex => $_getN(7);
  @$pb.TagNumber(8)
  set fullDuplex(CheckStatus value) => $_setField(8, value);
  @$pb.TagNumber(8)
  $core.bool hasFullDuplex() => $_has(7);
  @$pb.TagNumber(8)
  void clearFullDuplex() => $_clearField(8);

  @$pb.TagNumber(9)
  CheckStatus get rdmaReadiness => $_getN(8);
  @$pb.TagNumber(9)
  set rdmaReadiness(CheckStatus value) => $_setField(9, value);
  @$pb.TagNumber(9)
  $core.bool hasRdmaReadiness() => $_has(8);
  @$pb.TagNumber(9)
  void clearRdmaReadiness() => $_clearField(9);

  /// A configured maximum-speed exception covers this interface.
  @$pb.TagNumber(10)
  $core.bool get maximumSpeedException => $_getBF(9);
  @$pb.TagNumber(10)
  set maximumSpeedException($core.bool value) => $_setBool(9, value);
  @$pb.TagNumber(10)
  $core.bool hasMaximumSpeedException() => $_has(9);
  @$pb.TagNumber(10)
  void clearMaximumSpeedException() => $_clearField(10);

  @$pb.TagNumber(11)
  $fixnum.Int64 get upTransitions => $_getI64(10);
  @$pb.TagNumber(11)
  set upTransitions($fixnum.Int64 value) => $_setInt64(10, value);
  @$pb.TagNumber(11)
  $core.bool hasUpTransitions() => $_has(10);
  @$pb.TagNumber(11)
  void clearUpTransitions() => $_clearField(11);

  @$pb.TagNumber(12)
  $fixnum.Int64 get downTransitions => $_getI64(11);
  @$pb.TagNumber(12)
  set downTransitions($fixnum.Int64 value) => $_setInt64(11, value);
  @$pb.TagNumber(12)
  $core.bool hasDownTransitions() => $_has(11);
  @$pb.TagNumber(12)
  void clearDownTransitions() => $_clearField(12);
}

class ListInterfacesRequest extends $pb.GeneratedMessage {
  factory ListInterfacesRequest({
    $core.Iterable<$core.String>? names,
    $core.bool? eligibleOnly,
  }) {
    final result = create();
    if (names != null) result.names.addAll(names);
    if (eligibleOnly != null) result.eligibleOnly = eligibleOnly;
    return result;
  }

  ListInterfacesRequest._();

  factory ListInterfacesRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ListInterfacesRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListInterfacesRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'linkmonitor.v1'),
      createEmptyInstance: create)
    ..pPS(1, _omitFieldNames ? '' : 'names')
    ..aOB(2, _omitFieldNames ? '' : 'eligibleOnly')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListInterfacesRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListInterfacesRequest copyWith(
          void Function(ListInterfacesRequest) updates) =>
      super.copyWith((message) => updates(message as ListInterfacesRequest))
          as ListInterfacesRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ListInterfacesRequest create() => ListInterfacesRequest._();
  @$core.override
  ListInterfacesRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static ListInterfacesRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListInterfacesRequest>(create);
  static ListInterfacesRequest? _defaultInstance;

  /// Exact interface names; empty lists every interface.
  @$pb.TagNumber(1)
  $pb.PbList<$core.String> get names => $_getList(0);

  /// Only interfaces in the physical-link inventory.
  @$pb.TagNumber(2)
  $core.bool get eligibleOnly => $_getBF(1);
  @$pb.TagNumber(2)
  set eligibleOnly($core.bool value) => $_setBool(1, value);
  @$pb.TagNumber(2)
  $core.bool hasEligibleOnly() => $_has(1);
  @$pb.TagNumber(2)
  void clearEligibleOnly() => $_clearField(2);
}

class ListInterfacesResponse extends $pb.GeneratedMessage {
  factory ListInterfacesResponse({
    $fixnum.Int64? snapshotVersion,
    $core.Iterable<Interface>? interfaces,
  }) {
    final result = create();
    if (snapshotVersion != null) result.snapshotVersion = snapshotVersion;
    if (interfaces != null) result.interfaces.addAll(interfaces);
    return result;
  }

  ListInterfacesResponse._();

  factory ListInterfacesResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ListInterfacesResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListInterfacesResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'linkmonitor.v1'),
      createEmptyInstance: create)
    ..a<$fixnum.Int64>(
        1, _omitFieldNames ? '' : 'snapshotVersion', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..pPM<Interface>(2, _omitFieldNames ? '' : 'interfaces',
        subBuilder: Interface.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListInterfacesResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListInterfacesResponse copyWith(
          void Function(ListInterfacesResponse) updates) =>
      super.copyWith((message) => updates(message as ListInterfacesResponse))
          as ListInterfacesResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ListInterfacesResponse create() => ListInterfacesResponse._();
  @$core.override
  ListInterfacesResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static ListInterfacesResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListInterfacesResponse>(create);
  static ListInterfacesResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $fixnum.Int64 get snapshotVersion => $_getI64(0);
  @$pb.TagNumber(1)
  set snapshotVersion($fixnum.Int64 value) => $_setInt64(0, value);
  @$pb.TagNumber(1)
  $core.bool hasSnapshotVersion() => $_has(0);
  @$pb.TagNumber(1)
  void clearSnapshotVersion() => $_clearField(1);

  @$pb.TagNumber(2)
  $pb.PbList<Interface> get interfaces => $_getList(1);
}

class Collector extends $pb.GeneratedMessage {
  factory Collector({
    $core.String? interface,
    $core.String? name,
    Support? support,
    $core.bool? success,
    $core.bool? fresh,
    $1.Timestamp? lastSuccess,
    $2.Duration? duration,
    $core.String? errorReason,
    $core.Iterable<$core.MapEntry<$core.String, $fixnum.Int64>>? errors,
    $core.Iterable<$core.MapEntry<$core.String, $fixnum.Int64>>? omissions,
    $fixnum.Int64? discontinuities,
  }) {
    final result = create();
    if (interface != null) result.interface = interface;
    if (name != null) result.name = name;
    if (support != null) result.support = support;
    if (success != null) result.success = success;
    if (fresh != null) result.fresh = fresh;
    if (lastSuccess != null) result.lastSuccess = lastSuccess;
    if (duration != null) result.duration = duration;
    if (errorReason != null) result.errorReason = errorReason;
    if (errors != null) result.errors.addEntries(errors);
    if (omissions != null) result.omissions.addEntries(omissions);
    if (discontinuities != null) result.discontinuities = discontinuities;
    return result;
  }

  Collector._();

  factory Collector.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory Collector.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'Collector',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'linkmonitor.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'interface')
    ..aOS(2, _omitFieldNames ? '' : 'name')
    ..aE<Support>(3, _omitFieldNames ? '' : 'support',
        enumValues: Support.values)
    ..aOB(4, _omitFieldNames ? '' : 'success')
    ..aOB(5, _omitFieldNames ? '' : 'fresh')
    ..aOM<$1.Timestamp>(6, _omitFieldNames ? '' : 'lastSuccess',
        subBuilder: $1.Timestamp.create)
    ..aOM<$2.Duration>(7, _omitFieldNames ? '' : 'duration',
        subBuilder: $2.Duration.create)
    ..aOS(8, _omitFieldNames ? '' : 'errorReason')
    ..m<$core.String, $fixnum.Int64>(9, _omitFieldNames ? '' : 'errors',
        entryClassName: 'Collector.ErrorsEntry',
        keyFieldType: $pb.PbFieldType.OS,
        valueFieldType: $pb.PbFieldType.OU6,
        packageName: const $pb.PackageName('linkmonitor.v1'))
    ..m<$core.String, $fixnum.Int64>(10, _omitFieldNames ? '' : 'omissions',
        entryClassName: 'Collector.OmissionsEntry',
        keyFieldType: $pb.PbFieldType.OS,
        valueFieldType: $pb.PbFieldType.OU6,
        packageName: const $pb.PackageName('linkmonitor.v1'))
    ..a<$fixnum.Int64>(
        11, _omitFieldNames ? '' : 'discontinuities', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Collector clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Collector copyWith(void Function(Collector) updates) =>
      super.copyWith((message) => updates(message as Collector)) as Collector;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static Collector create() => Collector._();
  @$core.override
  Collector createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static Collector getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<Collector>(create);
  static Collector? _defaultInstance;

  /// Interface name; empty for the namespace-wide host collector.
  @$pb.TagNumber(1)
  $core.String get interface => $_getSZ(0);
  @$pb.TagNumber(1)
  set interface($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasInterface() => $_has(0);
  @$pb.TagNumber(1)
  void clearInterface() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get name => $_getSZ(1);
  @$pb.TagNumber(2)
  set name($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasName() => $_has(1);
  @$pb.TagNumber(2)
  void clearName() => $_clearField(2);

  @$pb.TagNumber(3)
  Support get support => $_getN(2);
  @$pb.TagNumber(3)
  set support(Support value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasSupport() => $_has(2);
  @$pb.TagNumber(3)
  void clearSupport() => $_clearField(3);

  /// Latest accepted attempt result; unset before any attempt.
  @$pb.TagNumber(4)
  $core.bool get success => $_getBF(3);
  @$pb.TagNumber(4)
  set success($core.bool value) => $_setBool(3, value);
  @$pb.TagNumber(4)
  $core.bool hasSuccess() => $_has(3);
  @$pb.TagNumber(4)
  void clearSuccess() => $_clearField(4);

  @$pb.TagNumber(5)
  $core.bool get fresh => $_getBF(4);
  @$pb.TagNumber(5)
  set fresh($core.bool value) => $_setBool(4, value);
  @$pb.TagNumber(5)
  $core.bool hasFresh() => $_has(4);
  @$pb.TagNumber(5)
  void clearFresh() => $_clearField(5);

  @$pb.TagNumber(6)
  $1.Timestamp get lastSuccess => $_getN(5);
  @$pb.TagNumber(6)
  set lastSuccess($1.Timestamp value) => $_setField(6, value);
  @$pb.TagNumber(6)
  $core.bool hasLastSuccess() => $_has(5);
  @$pb.TagNumber(6)
  void clearLastSuccess() => $_clearField(6);
  @$pb.TagNumber(6)
  $1.Timestamp ensureLastSuccess() => $_ensure(5);

  @$pb.TagNumber(7)
  $2.Duration get duration => $_getN(6);
  @$pb.TagNumber(7)
  set duration($2.Duration value) => $_setField(7, value);
  @$pb.TagNumber(7)
  $core.bool hasDuration() => $_has(6);
  @$pb.TagNumber(7)
  void clearDuration() => $_clearField(7);
  @$pb.TagNumber(7)
  $2.Duration ensureDuration() => $_ensure(6);

  @$pb.TagNumber(8)
  $core.String get errorReason => $_getSZ(7);
  @$pb.TagNumber(8)
  set errorReason($core.String value) => $_setString(7, value);
  @$pb.TagNumber(8)
  $core.bool hasErrorReason() => $_has(7);
  @$pb.TagNumber(8)
  void clearErrorReason() => $_clearField(8);

  @$pb.TagNumber(9)
  $pb.PbMap<$core.String, $fixnum.Int64> get errors => $_getMap(8);

  @$pb.TagNumber(10)
  $pb.PbMap<$core.String, $fixnum.Int64> get omissions => $_getMap(9);

  @$pb.TagNumber(11)
  $fixnum.Int64 get discontinuities => $_getI64(10);
  @$pb.TagNumber(11)
  set discontinuities($fixnum.Int64 value) => $_setInt64(10, value);
  @$pb.TagNumber(11)
  $core.bool hasDiscontinuities() => $_has(10);
  @$pb.TagNumber(11)
  void clearDiscontinuities() => $_clearField(11);
}

class ListCollectorsRequest extends $pb.GeneratedMessage {
  factory ListCollectorsRequest({
    $core.Iterable<$core.String>? names,
  }) {
    final result = create();
    if (names != null) result.names.addAll(names);
    return result;
  }

  ListCollectorsRequest._();

  factory ListCollectorsRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ListCollectorsRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListCollectorsRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'linkmonitor.v1'),
      createEmptyInstance: create)
    ..pPS(1, _omitFieldNames ? '' : 'names')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListCollectorsRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListCollectorsRequest copyWith(
          void Function(ListCollectorsRequest) updates) =>
      super.copyWith((message) => updates(message as ListCollectorsRequest))
          as ListCollectorsRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ListCollectorsRequest create() => ListCollectorsRequest._();
  @$core.override
  ListCollectorsRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static ListCollectorsRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListCollectorsRequest>(create);
  static ListCollectorsRequest? _defaultInstance;

  /// Exact interface names; empty lists every interface and the host collector.
  @$pb.TagNumber(1)
  $pb.PbList<$core.String> get names => $_getList(0);
}

class ListCollectorsResponse extends $pb.GeneratedMessage {
  factory ListCollectorsResponse({
    $fixnum.Int64? snapshotVersion,
    $core.Iterable<Collector>? collectors,
  }) {
    final result = create();
    if (snapshotVersion != null) result.snapshotVersion = snapshotVersion;
    if (collectors != null) result.collectors.addAll(collectors);
    return result;
  }

  ListCollectorsResponse._();

  factory ListCollectorsResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ListCollectorsResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListCollectorsResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'linkmonitor.v1'),
      createEmptyInstance: create)
    ..a<$fixnum.Int64>(
        1, _omitFieldNames ? '' : 'snapshotVersion', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..pPM<Collector>(2, _omitFieldNames ? '' : 'collectors',
        subBuilder: Collector.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListCollectorsResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListCollectorsResponse copyWith(
          void Function(ListCollectorsResponse) updates) =>
      super.copyWith((message) => updates(message as ListCollectorsResponse))
          as ListCollectorsResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ListCollectorsResponse create() => ListCollectorsResponse._();
  @$core.override
  ListCollectorsResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static ListCollectorsResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListCollectorsResponse>(create);
  static ListCollectorsResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $fixnum.Int64 get snapshotVersion => $_getI64(0);
  @$pb.TagNumber(1)
  set snapshotVersion($fixnum.Int64 value) => $_setInt64(0, value);
  @$pb.TagNumber(1)
  $core.bool hasSnapshotVersion() => $_has(0);
  @$pb.TagNumber(1)
  void clearSnapshotVersion() => $_clearField(1);

  @$pb.TagNumber(2)
  $pb.PbList<Collector> get collectors => $_getList(1);
}

class RebaselineRequest extends $pb.GeneratedMessage {
  factory RebaselineRequest({
    $core.String? reason,
    $2.Duration? wait,
  }) {
    final result = create();
    if (reason != null) result.reason = reason;
    if (wait != null) result.wait = wait;
    return result;
  }

  RebaselineRequest._();

  factory RebaselineRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RebaselineRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RebaselineRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'linkmonitor.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'reason')
    ..aOM<$2.Duration>(2, _omitFieldNames ? '' : 'wait',
        subBuilder: $2.Duration.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RebaselineRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RebaselineRequest copyWith(void Function(RebaselineRequest) updates) =>
      super.copyWith((message) => updates(message as RebaselineRequest))
          as RebaselineRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RebaselineRequest create() => RebaselineRequest._();
  @$core.override
  RebaselineRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static RebaselineRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RebaselineRequest>(create);
  static RebaselineRequest? _defaultInstance;

  /// Why the operator is rebaselining; recorded in the audit log.
  @$pb.TagNumber(1)
  $core.String get reason => $_getSZ(0);
  @$pb.TagNumber(1)
  set reason($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasReason() => $_has(0);
  @$pb.TagNumber(1)
  void clearReason() => $_clearField(1);

  /// How long to wait for a durable save. Zero returns after acceptance; a
  /// wait that expires returns DEADLINE_EXCEEDED with the write-error count.
  @$pb.TagNumber(2)
  $2.Duration get wait => $_getN(1);
  @$pb.TagNumber(2)
  set wait($2.Duration value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasWait() => $_has(1);
  @$pb.TagNumber(2)
  void clearWait() => $_clearField(2);
  @$pb.TagNumber(2)
  $2.Duration ensureWait() => $_ensure(1);
}

class RebaselineResponse extends $pb.GeneratedMessage {
  factory RebaselineResponse({
    $fixnum.Int64? generation,
    $core.bool? completed,
    $fixnum.Int64? expected,
    $fixnum.Int64? baselineWriteErrors,
  }) {
    final result = create();
    if (generation != null) result.generation = generation;
    if (completed != null) result.completed = completed;
    if (expected != null) result.expected = expected;
    if (baselineWriteErrors != null)
      result.baselineWriteErrors = baselineWriteErrors;
    return result;
  }

  RebaselineResponse._();

  factory RebaselineResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RebaselineResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RebaselineResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'linkmonitor.v1'),
      createEmptyInstance: create)
    ..a<$fixnum.Int64>(
        1, _omitFieldNames ? '' : 'generation', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..aOB(2, _omitFieldNames ? '' : 'completed')
    ..a<$fixnum.Int64>(
        3, _omitFieldNames ? '' : 'expected', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..a<$fixnum.Int64>(
        4, _omitFieldNames ? '' : 'baselineWriteErrors', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RebaselineResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RebaselineResponse copyWith(void Function(RebaselineResponse) updates) =>
      super.copyWith((message) => updates(message as RebaselineResponse))
          as RebaselineResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RebaselineResponse create() => RebaselineResponse._();
  @$core.override
  RebaselineResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static RebaselineResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RebaselineResponse>(create);
  static RebaselineResponse? _defaultInstance;

  /// Ticket generation of the requested pass.
  @$pb.TagNumber(1)
  $fixnum.Int64 get generation => $_getI64(0);
  @$pb.TagNumber(1)
  set generation($fixnum.Int64 value) => $_setInt64(0, value);
  @$pb.TagNumber(1)
  $core.bool hasGeneration() => $_has(0);
  @$pb.TagNumber(1)
  void clearGeneration() => $_clearField(1);

  /// The pass completed; always false when wait is zero.
  @$pb.TagNumber(2)
  $core.bool get completed => $_getBF(1);
  @$pb.TagNumber(2)
  set completed($core.bool value) => $_setBool(1, value);
  @$pb.TagNumber(2)
  $core.bool hasCompleted() => $_has(1);
  @$pb.TagNumber(2)
  void clearCompleted() => $_clearField(2);

  /// Expected count after the call.
  @$pb.TagNumber(3)
  $fixnum.Int64 get expected => $_getI64(2);
  @$pb.TagNumber(3)
  set expected($fixnum.Int64 value) => $_setInt64(2, value);
  @$pb.TagNumber(3)
  $core.bool hasExpected() => $_has(2);
  @$pb.TagNumber(3)
  void clearExpected() => $_clearField(3);

  @$pb.TagNumber(4)
  $fixnum.Int64 get baselineWriteErrors => $_getI64(3);
  @$pb.TagNumber(4)
  set baselineWriteErrors($fixnum.Int64 value) => $_setInt64(3, value);
  @$pb.TagNumber(4)
  $core.bool hasBaselineWriteErrors() => $_has(3);
  @$pb.TagNumber(4)
  void clearBaselineWriteErrors() => $_clearField(4);
}

class ResyncRequest extends $pb.GeneratedMessage {
  factory ResyncRequest({
    $core.String? reason,
    $2.Duration? wait,
  }) {
    final result = create();
    if (reason != null) result.reason = reason;
    if (wait != null) result.wait = wait;
    return result;
  }

  ResyncRequest._();

  factory ResyncRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ResyncRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ResyncRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'linkmonitor.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'reason')
    ..aOM<$2.Duration>(2, _omitFieldNames ? '' : 'wait',
        subBuilder: $2.Duration.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ResyncRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ResyncRequest copyWith(void Function(ResyncRequest) updates) =>
      super.copyWith((message) => updates(message as ResyncRequest))
          as ResyncRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ResyncRequest create() => ResyncRequest._();
  @$core.override
  ResyncRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static ResyncRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ResyncRequest>(create);
  static ResyncRequest? _defaultInstance;

  /// Why the operator is resyncing; recorded in the audit log.
  @$pb.TagNumber(1)
  $core.String get reason => $_getSZ(0);
  @$pb.TagNumber(1)
  set reason($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasReason() => $_has(0);
  @$pb.TagNumber(1)
  void clearReason() => $_clearField(1);

  /// How long to wait for the reconciliation. Zero returns after acceptance;
  /// a wait that expires returns DEADLINE_EXCEEDED.
  @$pb.TagNumber(2)
  $2.Duration get wait => $_getN(1);
  @$pb.TagNumber(2)
  set wait($2.Duration value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasWait() => $_has(1);
  @$pb.TagNumber(2)
  void clearWait() => $_clearField(2);
  @$pb.TagNumber(2)
  $2.Duration ensureWait() => $_ensure(1);
}

class ResyncResponse extends $pb.GeneratedMessage {
  factory ResyncResponse({
    $fixnum.Int64? generation,
    $core.bool? completed,
    $1.Timestamp? lastSuccessfulResync,
  }) {
    final result = create();
    if (generation != null) result.generation = generation;
    if (completed != null) result.completed = completed;
    if (lastSuccessfulResync != null)
      result.lastSuccessfulResync = lastSuccessfulResync;
    return result;
  }

  ResyncResponse._();

  factory ResyncResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ResyncResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ResyncResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'linkmonitor.v1'),
      createEmptyInstance: create)
    ..a<$fixnum.Int64>(
        1, _omitFieldNames ? '' : 'generation', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..aOB(2, _omitFieldNames ? '' : 'completed')
    ..aOM<$1.Timestamp>(3, _omitFieldNames ? '' : 'lastSuccessfulResync',
        subBuilder: $1.Timestamp.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ResyncResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ResyncResponse copyWith(void Function(ResyncResponse) updates) =>
      super.copyWith((message) => updates(message as ResyncResponse))
          as ResyncResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ResyncResponse create() => ResyncResponse._();
  @$core.override
  ResyncResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static ResyncResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ResyncResponse>(create);
  static ResyncResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $fixnum.Int64 get generation => $_getI64(0);
  @$pb.TagNumber(1)
  set generation($fixnum.Int64 value) => $_setInt64(0, value);
  @$pb.TagNumber(1)
  $core.bool hasGeneration() => $_has(0);
  @$pb.TagNumber(1)
  void clearGeneration() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.bool get completed => $_getBF(1);
  @$pb.TagNumber(2)
  set completed($core.bool value) => $_setBool(1, value);
  @$pb.TagNumber(2)
  $core.bool hasCompleted() => $_has(1);
  @$pb.TagNumber(2)
  void clearCompleted() => $_clearField(2);

  @$pb.TagNumber(3)
  $1.Timestamp get lastSuccessfulResync => $_getN(2);
  @$pb.TagNumber(3)
  set lastSuccessfulResync($1.Timestamp value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasLastSuccessfulResync() => $_has(2);
  @$pb.TagNumber(3)
  void clearLastSuccessfulResync() => $_clearField(3);
  @$pb.TagNumber(3)
  $1.Timestamp ensureLastSuccessfulResync() => $_ensure(2);
}

const $core.bool _omitFieldNames =
    $core.bool.fromEnvironment('protobuf.omit_field_names');
const $core.bool _omitMessageNames =
    $core.bool.fromEnvironment('protobuf.omit_message_names');
