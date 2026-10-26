// This is a generated file - do not edit.
//
// Generated from xtcp_treatment_journal/v1/journal.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, prefer_relative_imports

import 'dart:core' as $core;

import 'package:fixnum/fixnum.dart' as $fixnum;
import 'package:protobuf/protobuf.dart' as $pb;
import 'package:protobuf/well_known_types/google/protobuf/timestamp.pb.dart'
    as $0;

import 'journal.pbenum.dart';

export 'package:protobuf/protobuf.dart' show GeneratedMessageGenericExtensions;

export 'journal.pbenum.dart';

/// TCPSettings mirrors xtcp_treatment.v1.TCPSettings; presence is meaningful.
class TCPSettings extends $pb.GeneratedMessage {
  factory TCPSettings({
    $core.String? congestionControl,
    $core.bool? congestionLock,
    $core.int? rtoMinMs,
    $core.int? initialCongestionWindow,
    $core.int? initialReceiveWindow,
    $core.bool? quickAck,
    $core.bool? ecn,
  }) {
    final result = TCPSettings._();
    if (congestionControl != null) result.congestionControl = congestionControl;
    if (congestionLock != null) result.congestionLock = congestionLock;
    if (rtoMinMs != null) result.rtoMinMs = rtoMinMs;
    if (initialCongestionWindow != null)
      result.initialCongestionWindow = initialCongestionWindow;
    if (initialReceiveWindow != null)
      result.initialReceiveWindow = initialReceiveWindow;
    if (quickAck != null) result.quickAck = quickAck;
    if (ecn != null) result.ecn = ecn;
    return result;
  }

  TCPSettings._();

  factory TCPSettings.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      TCPSettings()..mergeFromBuffer(data, registry);
  factory TCPSettings.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      TCPSettings()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'TCPSettings',
      package: const $pb.PackageName(
          _omitMessageNames ? '' : 'xtcp_treatment_journal.v1'),
      createEmptyInstance: TCPSettings.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'congestionControl')
    ..aOB(2, _omitFieldNames ? '' : 'congestionLock')
    ..aI(3, _omitFieldNames ? '' : 'rtoMinMs', fieldType: $pb.PbFieldType.OU3)
    ..aI(4, _omitFieldNames ? '' : 'initialCongestionWindow',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(5, _omitFieldNames ? '' : 'initialReceiveWindow',
        fieldType: $pb.PbFieldType.OU3)
    ..aOB(6, _omitFieldNames ? '' : 'quickAck')
    ..aOB(7, _omitFieldNames ? '' : 'ecn')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  TCPSettings clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  TCPSettings copyWith(void Function(TCPSettings) updates) =>
      super.copyWith((message) => updates(message as TCPSettings))
          as TCPSettings;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated('Use TCPSettings() / TCPSettings.new instead')
  static TCPSettings create() => TCPSettings._();
  static $pb.GeneratedMessage $_createMessage() => TCPSettings._();
  @$core.override
  TCPSettings createEmptyInstance() => TCPSettings._();
  @$core.pragma('dart2js:noInline')
  static TCPSettings getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<TCPSettings>(
          TCPSettings.$_createMessage);
  static TCPSettings? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get congestionControl => $_getSZ(0);
  @$pb.TagNumber(1)
  set congestionControl($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasCongestionControl() => $_has(0);
  @$pb.TagNumber(1)
  void clearCongestionControl() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.bool get congestionLock => $_getBF(1);
  @$pb.TagNumber(2)
  set congestionLock($core.bool value) => $_setBool(1, value);
  @$pb.TagNumber(2)
  $core.bool hasCongestionLock() => $_has(1);
  @$pb.TagNumber(2)
  void clearCongestionLock() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.int get rtoMinMs => $_getIZ(2);
  @$pb.TagNumber(3)
  set rtoMinMs($core.int value) => $_setUnsignedInt32(2, value);
  @$pb.TagNumber(3)
  $core.bool hasRtoMinMs() => $_has(2);
  @$pb.TagNumber(3)
  void clearRtoMinMs() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.int get initialCongestionWindow => $_getIZ(3);
  @$pb.TagNumber(4)
  set initialCongestionWindow($core.int value) => $_setUnsignedInt32(3, value);
  @$pb.TagNumber(4)
  $core.bool hasInitialCongestionWindow() => $_has(3);
  @$pb.TagNumber(4)
  void clearInitialCongestionWindow() => $_clearField(4);

  @$pb.TagNumber(5)
  $core.int get initialReceiveWindow => $_getIZ(4);
  @$pb.TagNumber(5)
  set initialReceiveWindow($core.int value) => $_setUnsignedInt32(4, value);
  @$pb.TagNumber(5)
  $core.bool hasInitialReceiveWindow() => $_has(4);
  @$pb.TagNumber(5)
  void clearInitialReceiveWindow() => $_clearField(5);

  @$pb.TagNumber(6)
  $core.bool get quickAck => $_getBF(5);
  @$pb.TagNumber(6)
  set quickAck($core.bool value) => $_setBool(5, value);
  @$pb.TagNumber(6)
  $core.bool hasQuickAck() => $_has(5);
  @$pb.TagNumber(6)
  void clearQuickAck() => $_clearField(6);

  @$pb.TagNumber(7)
  $core.bool get ecn => $_getBF(6);
  @$pb.TagNumber(7)
  set ecn($core.bool value) => $_setBool(6, value);
  @$pb.TagNumber(7)
  $core.bool hasEcn() => $_has(6);
  @$pb.TagNumber(7)
  void clearEcn() => $_clearField(7);
}

class Treatment extends $pb.GeneratedMessage {
  factory Treatment({
    $core.String? id,
    $fixnum.Int64? revision,
    $core.String? destination,
    TCPSettings? settings,
    $core.bool? ecnFabricVerified,
  }) {
    final result = Treatment._();
    if (id != null) result.id = id;
    if (revision != null) result.revision = revision;
    if (destination != null) result.destination = destination;
    if (settings != null) result.settings = settings;
    if (ecnFabricVerified != null) result.ecnFabricVerified = ecnFabricVerified;
    return result;
  }

  Treatment._();

  factory Treatment.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      Treatment()..mergeFromBuffer(data, registry);
  factory Treatment.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      Treatment()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'Treatment',
      package: const $pb.PackageName(
          _omitMessageNames ? '' : 'xtcp_treatment_journal.v1'),
      createEmptyInstance: Treatment.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'id')
    ..a<$fixnum.Int64>(
        2, _omitFieldNames ? '' : 'revision', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..aOS(3, _omitFieldNames ? '' : 'destination')
    ..aOM<TCPSettings>(4, _omitFieldNames ? '' : 'settings',
        subBuilder: TCPSettings.$_createMessage)
    ..aOB(5, _omitFieldNames ? '' : 'ecnFabricVerified')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Treatment clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Treatment copyWith(void Function(Treatment) updates) =>
      super.copyWith((message) => updates(message as Treatment)) as Treatment;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated('Use Treatment() / Treatment.new instead')
  static Treatment create() => Treatment._();
  static $pb.GeneratedMessage $_createMessage() => Treatment._();
  @$core.override
  Treatment createEmptyInstance() => Treatment._();
  @$core.pragma('dart2js:noInline')
  static Treatment getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<Treatment>(Treatment.$_createMessage);
  static Treatment? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get id => $_getSZ(0);
  @$pb.TagNumber(1)
  set id($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasId() => $_has(0);
  @$pb.TagNumber(1)
  void clearId() => $_clearField(1);

  @$pb.TagNumber(2)
  $fixnum.Int64 get revision => $_getI64(1);
  @$pb.TagNumber(2)
  set revision($fixnum.Int64 value) => $_setInt64(1, value);
  @$pb.TagNumber(2)
  $core.bool hasRevision() => $_has(1);
  @$pb.TagNumber(2)
  void clearRevision() => $_clearField(2);

  /// Canonical /32 or /128 host prefix.
  @$pb.TagNumber(3)
  $core.String get destination => $_getSZ(2);
  @$pb.TagNumber(3)
  set destination($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasDestination() => $_has(2);
  @$pb.TagNumber(3)
  void clearDestination() => $_clearField(3);

  @$pb.TagNumber(4)
  TCPSettings get settings => $_getN(3);
  @$pb.TagNumber(4)
  set settings(TCPSettings value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasSettings() => $_has(3);
  @$pb.TagNumber(4)
  void clearSettings() => $_clearField(4);
  @$pb.TagNumber(4)
  TCPSettings ensureSettings() => $_ensure(3);

  @$pb.TagNumber(5)
  $core.bool get ecnFabricVerified => $_getBF(4);
  @$pb.TagNumber(5)
  set ecnFabricVerified($core.bool value) => $_setBool(4, value);
  @$pb.TagNumber(5)
  $core.bool hasEcnFabricVerified() => $_has(4);
  @$pb.TagNumber(5)
  void clearEcnFabricVerified() => $_clearField(5);
}

class Assignment extends $pb.GeneratedMessage {
  factory Assignment({
    $core.String? experimentId,
    Treatment? treatment,
    AssignmentMode? mode,
    $0.Timestamp? leaseExpiresAt,
    Treatment? predecessor,
    $fixnum.Int64? generation,
  }) {
    final result = Assignment._();
    if (experimentId != null) result.experimentId = experimentId;
    if (treatment != null) result.treatment = treatment;
    if (mode != null) result.mode = mode;
    if (leaseExpiresAt != null) result.leaseExpiresAt = leaseExpiresAt;
    if (predecessor != null) result.predecessor = predecessor;
    if (generation != null) result.generation = generation;
    return result;
  }

  Assignment._();

  factory Assignment.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      Assignment()..mergeFromBuffer(data, registry);
  factory Assignment.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      Assignment()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'Assignment',
      package: const $pb.PackageName(
          _omitMessageNames ? '' : 'xtcp_treatment_journal.v1'),
      createEmptyInstance: Assignment.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'experimentId')
    ..aOM<Treatment>(2, _omitFieldNames ? '' : 'treatment',
        subBuilder: Treatment.$_createMessage)
    ..aE<AssignmentMode>(3, _omitFieldNames ? '' : 'mode',
        enumValues: AssignmentMode.values)
    ..aOM<$0.Timestamp>(4, _omitFieldNames ? '' : 'leaseExpiresAt',
        subBuilder: $0.Timestamp.$_createMessage)
    ..aOM<Treatment>(5, _omitFieldNames ? '' : 'predecessor',
        subBuilder: Treatment.$_createMessage)
    ..a<$fixnum.Int64>(
        6, _omitFieldNames ? '' : 'generation', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Assignment clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Assignment copyWith(void Function(Assignment) updates) =>
      super.copyWith((message) => updates(message as Assignment)) as Assignment;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated('Use Assignment() / Assignment.new instead')
  static Assignment create() => Assignment._();
  static $pb.GeneratedMessage $_createMessage() => Assignment._();
  @$core.override
  Assignment createEmptyInstance() => Assignment._();
  @$core.pragma('dart2js:noInline')
  static Assignment getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<Assignment>(Assignment.$_createMessage);
  static Assignment? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get experimentId => $_getSZ(0);
  @$pb.TagNumber(1)
  set experimentId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasExperimentId() => $_has(0);
  @$pb.TagNumber(1)
  void clearExperimentId() => $_clearField(1);

  @$pb.TagNumber(2)
  Treatment get treatment => $_getN(1);
  @$pb.TagNumber(2)
  set treatment(Treatment value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasTreatment() => $_has(1);
  @$pb.TagNumber(2)
  void clearTreatment() => $_clearField(2);
  @$pb.TagNumber(2)
  Treatment ensureTreatment() => $_ensure(1);

  @$pb.TagNumber(3)
  AssignmentMode get mode => $_getN(2);
  @$pb.TagNumber(3)
  set mode(AssignmentMode value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasMode() => $_has(2);
  @$pb.TagNumber(3)
  void clearMode() => $_clearField(3);

  @$pb.TagNumber(4)
  $0.Timestamp get leaseExpiresAt => $_getN(3);
  @$pb.TagNumber(4)
  set leaseExpiresAt($0.Timestamp value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasLeaseExpiresAt() => $_has(3);
  @$pb.TagNumber(4)
  void clearLeaseExpiresAt() => $_clearField(4);
  @$pb.TagNumber(4)
  $0.Timestamp ensureLeaseExpiresAt() => $_ensure(3);

  @$pb.TagNumber(5)
  Treatment get predecessor => $_getN(4);
  @$pb.TagNumber(5)
  set predecessor(Treatment value) => $_setField(5, value);
  @$pb.TagNumber(5)
  $core.bool hasPredecessor() => $_has(4);
  @$pb.TagNumber(5)
  void clearPredecessor() => $_clearField(5);
  @$pb.TagNumber(5)
  Treatment ensurePredecessor() => $_ensure(4);

  @$pb.TagNumber(6)
  $fixnum.Int64 get generation => $_getI64(5);
  @$pb.TagNumber(6)
  set generation($fixnum.Int64 value) => $_setInt64(5, value);
  @$pb.TagNumber(6)
  $core.bool hasGeneration() => $_has(5);
  @$pb.TagNumber(6)
  void clearGeneration() => $_clearField(6);
}

/// Desired is the controller's last accepted snapshot for this agent. The
/// agent identity lives on the Journal header.
class Desired extends $pb.GeneratedMessage {
  factory Desired({
    $fixnum.Int64? revision,
    $core.bool? suspended,
    $0.Timestamp? issuedAt,
    $core.Iterable<Assignment>? assignments,
  }) {
    final result = Desired._();
    if (revision != null) result.revision = revision;
    if (suspended != null) result.suspended = suspended;
    if (issuedAt != null) result.issuedAt = issuedAt;
    if (assignments != null) result.assignments.addAll(assignments);
    return result;
  }

  Desired._();

  factory Desired.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      Desired()..mergeFromBuffer(data, registry);
  factory Desired.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      Desired()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'Desired',
      package: const $pb.PackageName(
          _omitMessageNames ? '' : 'xtcp_treatment_journal.v1'),
      createEmptyInstance: Desired.$_createMessage)
    ..a<$fixnum.Int64>(
        1, _omitFieldNames ? '' : 'revision', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..aOB(2, _omitFieldNames ? '' : 'suspended')
    ..aOM<$0.Timestamp>(3, _omitFieldNames ? '' : 'issuedAt',
        subBuilder: $0.Timestamp.$_createMessage)
    ..pPM<Assignment>(4, _omitFieldNames ? '' : 'assignments',
        subBuilder: Assignment.$_createMessage)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Desired clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Desired copyWith(void Function(Desired) updates) =>
      super.copyWith((message) => updates(message as Desired)) as Desired;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated('Use Desired() / Desired.new instead')
  static Desired create() => Desired._();
  static $pb.GeneratedMessage $_createMessage() => Desired._();
  @$core.override
  Desired createEmptyInstance() => Desired._();
  @$core.pragma('dart2js:noInline')
  static Desired getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<Desired>(Desired.$_createMessage);
  static Desired? _defaultInstance;

  @$pb.TagNumber(1)
  $fixnum.Int64 get revision => $_getI64(0);
  @$pb.TagNumber(1)
  set revision($fixnum.Int64 value) => $_setInt64(0, value);
  @$pb.TagNumber(1)
  $core.bool hasRevision() => $_has(0);
  @$pb.TagNumber(1)
  void clearRevision() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.bool get suspended => $_getBF(1);
  @$pb.TagNumber(2)
  set suspended($core.bool value) => $_setBool(1, value);
  @$pb.TagNumber(2)
  $core.bool hasSuspended() => $_has(1);
  @$pb.TagNumber(2)
  void clearSuspended() => $_clearField(2);

  @$pb.TagNumber(3)
  $0.Timestamp get issuedAt => $_getN(2);
  @$pb.TagNumber(3)
  set issuedAt($0.Timestamp value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasIssuedAt() => $_has(2);
  @$pb.TagNumber(3)
  void clearIssuedAt() => $_clearField(3);
  @$pb.TagNumber(3)
  $0.Timestamp ensureIssuedAt() => $_ensure(2);

  @$pb.TagNumber(4)
  $pb.PbList<Assignment> get assignments => $_getList(3);
}

/// Record is the agent's account of one assignment plus the kernel-local
/// facts (installed route bytes, installing boot) that never leave the host.
class Record extends $pb.GeneratedMessage {
  factory Record({
    Assignment? assignment,
    $fixnum.Int64? desiredRevision,
    ApplyStatus? status,
    $0.Timestamp? receivedAt,
    $0.Timestamp? attemptedAt,
    $0.Timestamp? appliedAt,
    $0.Timestamp? verifiedAt,
    $0.Timestamp? removedAt,
    $core.String? error,
    $core.List<$core.int>? installedRoute,
    $core.String? installedBootId,
    $core.bool? expiredLatched,
  }) {
    final result = Record._();
    if (assignment != null) result.assignment = assignment;
    if (desiredRevision != null) result.desiredRevision = desiredRevision;
    if (status != null) result.status = status;
    if (receivedAt != null) result.receivedAt = receivedAt;
    if (attemptedAt != null) result.attemptedAt = attemptedAt;
    if (appliedAt != null) result.appliedAt = appliedAt;
    if (verifiedAt != null) result.verifiedAt = verifiedAt;
    if (removedAt != null) result.removedAt = removedAt;
    if (error != null) result.error = error;
    if (installedRoute != null) result.installedRoute = installedRoute;
    if (installedBootId != null) result.installedBootId = installedBootId;
    if (expiredLatched != null) result.expiredLatched = expiredLatched;
    return result;
  }

  Record._();

  factory Record.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      Record()..mergeFromBuffer(data, registry);
  factory Record.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      Record()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'Record',
      package: const $pb.PackageName(
          _omitMessageNames ? '' : 'xtcp_treatment_journal.v1'),
      createEmptyInstance: Record.$_createMessage)
    ..aOM<Assignment>(1, _omitFieldNames ? '' : 'assignment',
        subBuilder: Assignment.$_createMessage)
    ..a<$fixnum.Int64>(
        2, _omitFieldNames ? '' : 'desiredRevision', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..aE<ApplyStatus>(3, _omitFieldNames ? '' : 'status',
        enumValues: ApplyStatus.values)
    ..aOM<$0.Timestamp>(4, _omitFieldNames ? '' : 'receivedAt',
        subBuilder: $0.Timestamp.$_createMessage)
    ..aOM<$0.Timestamp>(5, _omitFieldNames ? '' : 'attemptedAt',
        subBuilder: $0.Timestamp.$_createMessage)
    ..aOM<$0.Timestamp>(6, _omitFieldNames ? '' : 'appliedAt',
        subBuilder: $0.Timestamp.$_createMessage)
    ..aOM<$0.Timestamp>(7, _omitFieldNames ? '' : 'verifiedAt',
        subBuilder: $0.Timestamp.$_createMessage)
    ..aOM<$0.Timestamp>(8, _omitFieldNames ? '' : 'removedAt',
        subBuilder: $0.Timestamp.$_createMessage)
    ..aOS(9, _omitFieldNames ? '' : 'error')
    ..a<$core.List<$core.int>>(
        10, _omitFieldNames ? '' : 'installedRoute', $pb.PbFieldType.OY)
    ..aOS(11, _omitFieldNames ? '' : 'installedBootId')
    ..aOB(12, _omitFieldNames ? '' : 'expiredLatched')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Record clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Record copyWith(void Function(Record) updates) =>
      super.copyWith((message) => updates(message as Record)) as Record;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated('Use Record() / Record.new instead')
  static Record create() => Record._();
  static $pb.GeneratedMessage $_createMessage() => Record._();
  @$core.override
  Record createEmptyInstance() => Record._();
  @$core.pragma('dart2js:noInline')
  static Record getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<Record>(Record.$_createMessage);
  static Record? _defaultInstance;

  @$pb.TagNumber(1)
  Assignment get assignment => $_getN(0);
  @$pb.TagNumber(1)
  set assignment(Assignment value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasAssignment() => $_has(0);
  @$pb.TagNumber(1)
  void clearAssignment() => $_clearField(1);
  @$pb.TagNumber(1)
  Assignment ensureAssignment() => $_ensure(0);

  @$pb.TagNumber(2)
  $fixnum.Int64 get desiredRevision => $_getI64(1);
  @$pb.TagNumber(2)
  set desiredRevision($fixnum.Int64 value) => $_setInt64(1, value);
  @$pb.TagNumber(2)
  $core.bool hasDesiredRevision() => $_has(1);
  @$pb.TagNumber(2)
  void clearDesiredRevision() => $_clearField(2);

  @$pb.TagNumber(3)
  ApplyStatus get status => $_getN(2);
  @$pb.TagNumber(3)
  set status(ApplyStatus value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasStatus() => $_has(2);
  @$pb.TagNumber(3)
  void clearStatus() => $_clearField(3);

  @$pb.TagNumber(4)
  $0.Timestamp get receivedAt => $_getN(3);
  @$pb.TagNumber(4)
  set receivedAt($0.Timestamp value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasReceivedAt() => $_has(3);
  @$pb.TagNumber(4)
  void clearReceivedAt() => $_clearField(4);
  @$pb.TagNumber(4)
  $0.Timestamp ensureReceivedAt() => $_ensure(3);

  @$pb.TagNumber(5)
  $0.Timestamp get attemptedAt => $_getN(4);
  @$pb.TagNumber(5)
  set attemptedAt($0.Timestamp value) => $_setField(5, value);
  @$pb.TagNumber(5)
  $core.bool hasAttemptedAt() => $_has(4);
  @$pb.TagNumber(5)
  void clearAttemptedAt() => $_clearField(5);
  @$pb.TagNumber(5)
  $0.Timestamp ensureAttemptedAt() => $_ensure(4);

  @$pb.TagNumber(6)
  $0.Timestamp get appliedAt => $_getN(5);
  @$pb.TagNumber(6)
  set appliedAt($0.Timestamp value) => $_setField(6, value);
  @$pb.TagNumber(6)
  $core.bool hasAppliedAt() => $_has(5);
  @$pb.TagNumber(6)
  void clearAppliedAt() => $_clearField(6);
  @$pb.TagNumber(6)
  $0.Timestamp ensureAppliedAt() => $_ensure(5);

  @$pb.TagNumber(7)
  $0.Timestamp get verifiedAt => $_getN(6);
  @$pb.TagNumber(7)
  set verifiedAt($0.Timestamp value) => $_setField(7, value);
  @$pb.TagNumber(7)
  $core.bool hasVerifiedAt() => $_has(6);
  @$pb.TagNumber(7)
  void clearVerifiedAt() => $_clearField(7);
  @$pb.TagNumber(7)
  $0.Timestamp ensureVerifiedAt() => $_ensure(6);

  @$pb.TagNumber(8)
  $0.Timestamp get removedAt => $_getN(7);
  @$pb.TagNumber(8)
  set removedAt($0.Timestamp value) => $_setField(8, value);
  @$pb.TagNumber(8)
  $core.bool hasRemovedAt() => $_has(7);
  @$pb.TagNumber(8)
  void clearRemovedAt() => $_clearField(8);
  @$pb.TagNumber(8)
  $0.Timestamp ensureRemovedAt() => $_ensure(7);

  @$pb.TagNumber(9)
  $core.String get error => $_getSZ(8);
  @$pb.TagNumber(9)
  set error($core.String value) => $_setString(8, value);
  @$pb.TagNumber(9)
  $core.bool hasError() => $_has(8);
  @$pb.TagNumber(9)
  void clearError() => $_clearField(9);

  /// Locally planned canonical netlink route body, the only thing Remove will
  /// ever be asked to withdraw.
  @$pb.TagNumber(10)
  $core.List<$core.int> get installedRoute => $_getN(9);
  @$pb.TagNumber(10)
  set installedRoute($core.List<$core.int> value) => $_setBytes(9, value);
  @$pb.TagNumber(10)
  $core.bool hasInstalledRoute() => $_has(9);
  @$pb.TagNumber(10)
  void clearInstalledRoute() => $_clearField(10);

  @$pb.TagNumber(11)
  $core.String get installedBootId => $_getSZ(10);
  @$pb.TagNumber(11)
  set installedBootId($core.String value) => $_setString(10, value);
  @$pb.TagNumber(11)
  $core.bool hasInstalledBootId() => $_has(10);
  @$pb.TagNumber(11)
  void clearInstalledBootId() => $_clearField(11);

  @$pb.TagNumber(12)
  $core.bool get expiredLatched => $_getBF(11);
  @$pb.TagNumber(12)
  set expiredLatched($core.bool value) => $_setBool(11, value);
  @$pb.TagNumber(12)
  $core.bool hasExpiredLatched() => $_has(11);
  @$pb.TagNumber(12)
  void clearExpiredLatched() => $_clearField(12);
}

/// Journal is the atomic snapshot in <directory>/state.textproto. The header
/// identifies the writer so a journal can never be reconciled for another
/// agent and so a telemetry-only daemon can attribute records from it.
class Journal extends $pb.GeneratedMessage {
  factory Journal({
    $core.String? agentId,
    $core.String? location,
    $core.String? bootId,
    Desired? desired,
    $core.bool? locallySuspended,
    $core.String? suspensionReason,
    $core.Iterable<Record>? records,
    $0.Timestamp? updatedAt,
  }) {
    final result = Journal._();
    if (agentId != null) result.agentId = agentId;
    if (location != null) result.location = location;
    if (bootId != null) result.bootId = bootId;
    if (desired != null) result.desired = desired;
    if (locallySuspended != null) result.locallySuspended = locallySuspended;
    if (suspensionReason != null) result.suspensionReason = suspensionReason;
    if (records != null) result.records.addAll(records);
    if (updatedAt != null) result.updatedAt = updatedAt;
    return result;
  }

  Journal._();

  factory Journal.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      Journal()..mergeFromBuffer(data, registry);
  factory Journal.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      Journal()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'Journal',
      package: const $pb.PackageName(
          _omitMessageNames ? '' : 'xtcp_treatment_journal.v1'),
      createEmptyInstance: Journal.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'agentId')
    ..aOS(2, _omitFieldNames ? '' : 'location')
    ..aOS(3, _omitFieldNames ? '' : 'bootId')
    ..aOM<Desired>(4, _omitFieldNames ? '' : 'desired',
        subBuilder: Desired.$_createMessage)
    ..aOB(5, _omitFieldNames ? '' : 'locallySuspended')
    ..aOS(6, _omitFieldNames ? '' : 'suspensionReason')
    ..pPM<Record>(7, _omitFieldNames ? '' : 'records',
        subBuilder: Record.$_createMessage)
    ..aOM<$0.Timestamp>(8, _omitFieldNames ? '' : 'updatedAt',
        subBuilder: $0.Timestamp.$_createMessage)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Journal clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Journal copyWith(void Function(Journal) updates) =>
      super.copyWith((message) => updates(message as Journal)) as Journal;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated('Use Journal() / Journal.new instead')
  static Journal create() => Journal._();
  static $pb.GeneratedMessage $_createMessage() => Journal._();
  @$core.override
  Journal createEmptyInstance() => Journal._();
  @$core.pragma('dart2js:noInline')
  static Journal getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<Journal>(Journal.$_createMessage);
  static Journal? _defaultInstance;

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

  /// Boot that last wrote the journal; per-record installed_boot_id says
  /// which boot installed each route.
  @$pb.TagNumber(3)
  $core.String get bootId => $_getSZ(2);
  @$pb.TagNumber(3)
  set bootId($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasBootId() => $_has(2);
  @$pb.TagNumber(3)
  void clearBootId() => $_clearField(3);

  @$pb.TagNumber(4)
  Desired get desired => $_getN(3);
  @$pb.TagNumber(4)
  set desired(Desired value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasDesired() => $_has(3);
  @$pb.TagNumber(4)
  void clearDesired() => $_clearField(4);
  @$pb.TagNumber(4)
  Desired ensureDesired() => $_ensure(3);

  @$pb.TagNumber(5)
  $core.bool get locallySuspended => $_getBF(4);
  @$pb.TagNumber(5)
  set locallySuspended($core.bool value) => $_setBool(4, value);
  @$pb.TagNumber(5)
  $core.bool hasLocallySuspended() => $_has(4);
  @$pb.TagNumber(5)
  void clearLocallySuspended() => $_clearField(5);

  @$pb.TagNumber(6)
  $core.String get suspensionReason => $_getSZ(5);
  @$pb.TagNumber(6)
  set suspensionReason($core.String value) => $_setString(5, value);
  @$pb.TagNumber(6)
  $core.bool hasSuspensionReason() => $_has(5);
  @$pb.TagNumber(6)
  void clearSuspensionReason() => $_clearField(6);

  @$pb.TagNumber(7)
  $pb.PbList<Record> get records => $_getList(6);

  @$pb.TagNumber(8)
  $0.Timestamp get updatedAt => $_getN(7);
  @$pb.TagNumber(8)
  set updatedAt($0.Timestamp value) => $_setField(8, value);
  @$pb.TagNumber(8)
  $core.bool hasUpdatedAt() => $_has(7);
  @$pb.TagNumber(8)
  void clearUpdatedAt() => $_clearField(8);
  @$pb.TagNumber(8)
  $0.Timestamp ensureUpdatedAt() => $_ensure(7);
}

const $core.bool _omitFieldNames =
    $core.bool.fromEnvironment('protobuf.omit_field_names');
const $core.bool _omitMessageNames =
    $core.bool.fromEnvironment('protobuf.omit_message_names');
