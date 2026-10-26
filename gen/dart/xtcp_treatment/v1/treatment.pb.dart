// This is a generated file - do not edit.
//
// Generated from xtcp_treatment/v1/treatment.proto.

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
import 'package:protobuf/well_known_types/google/protobuf/timestamp.pb.dart'
    as $0;

import 'treatment.pbenum.dart';

export 'package:protobuf/protobuf.dart' show GeneratedMessageGenericExtensions;

export 'treatment.pbenum.dart';

/// TCPSettings are the per-route TCP parameters a treatment installs. Every
/// field is optional; at least one must be present.
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
      package:
          const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_treatment.v1'),
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

  /// Kernel algorithm name (TCP_CA_NAME_MAX is 16 including NUL).
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
      package:
          const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_treatment.v1'),
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

  /// Canonical unicast host prefix (/32 or /128). Canonical form, unicast and
  /// not-loopback are checked by treatment.ValidateTreatment.
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
      package:
          const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_treatment.v1'),
      createEmptyInstance: Assignment.$_createMessage)
    ..aOS(2, _omitFieldNames ? '' : 'experimentId')
    ..aOM<Treatment>(3, _omitFieldNames ? '' : 'treatment',
        subBuilder: Treatment.$_createMessage)
    ..aE<AssignmentMode>(4, _omitFieldNames ? '' : 'mode',
        enumValues: AssignmentMode.values)
    ..aOM<$0.Timestamp>(5, _omitFieldNames ? '' : 'leaseExpiresAt',
        subBuilder: $0.Timestamp.$_createMessage)
    ..aOM<Treatment>(6, _omitFieldNames ? '' : 'predecessor',
        subBuilder: Treatment.$_createMessage)
    ..a<$fixnum.Int64>(
        7, _omitFieldNames ? '' : 'generation', $pb.PbFieldType.OU6,
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

  @$pb.TagNumber(2)
  $core.String get experimentId => $_getSZ(0);
  @$pb.TagNumber(2)
  set experimentId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(2)
  $core.bool hasExperimentId() => $_has(0);
  @$pb.TagNumber(2)
  void clearExperimentId() => $_clearField(2);

  @$pb.TagNumber(3)
  Treatment get treatment => $_getN(1);
  @$pb.TagNumber(3)
  set treatment(Treatment value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasTreatment() => $_has(1);
  @$pb.TagNumber(3)
  void clearTreatment() => $_clearField(3);
  @$pb.TagNumber(3)
  Treatment ensureTreatment() => $_ensure(1);

  @$pb.TagNumber(4)
  AssignmentMode get mode => $_getN(2);
  @$pb.TagNumber(4)
  set mode(AssignmentMode value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasMode() => $_has(2);
  @$pb.TagNumber(4)
  void clearMode() => $_clearField(4);

  @$pb.TagNumber(5)
  $0.Timestamp get leaseExpiresAt => $_getN(3);
  @$pb.TagNumber(5)
  set leaseExpiresAt($0.Timestamp value) => $_setField(5, value);
  @$pb.TagNumber(5)
  $core.bool hasLeaseExpiresAt() => $_has(3);
  @$pb.TagNumber(5)
  void clearLeaseExpiresAt() => $_clearField(5);
  @$pb.TagNumber(5)
  $0.Timestamp ensureLeaseExpiresAt() => $_ensure(3);

  /// Previous approved stable treatment, restored on trial expiry/rollback.
  @$pb.TagNumber(6)
  Treatment get predecessor => $_getN(4);
  @$pb.TagNumber(6)
  set predecessor(Treatment value) => $_setField(6, value);
  @$pb.TagNumber(6)
  $core.bool hasPredecessor() => $_has(4);
  @$pb.TagNumber(6)
  void clearPredecessor() => $_clearField(6);
  @$pb.TagNumber(6)
  Treatment ensurePredecessor() => $_ensure(4);

  /// Increment only on an explicitly approved reactivation, not lease renewal.
  @$pb.TagNumber(7)
  $fixnum.Int64 get generation => $_getI64(5);
  @$pb.TagNumber(7)
  set generation($fixnum.Int64 value) => $_setInt64(5, value);
  @$pb.TagNumber(7)
  $core.bool hasGeneration() => $_has(5);
  @$pb.TagNumber(7)
  void clearGeneration() => $_clearField(7);
}

/// DesiredState is the complete revisioned snapshot an agent reconciles to.
/// Duplicate assignment IDs and overlapping destinations are rejected by
/// treatment.ValidateDesired.
class DesiredState extends $pb.GeneratedMessage {
  factory DesiredState({
    $core.String? agentId,
    $fixnum.Int64? revision,
    $core.bool? suspended,
    $core.Iterable<Assignment>? assignments,
    $0.Timestamp? issuedAt,
  }) {
    final result = DesiredState._();
    if (agentId != null) result.agentId = agentId;
    if (revision != null) result.revision = revision;
    if (suspended != null) result.suspended = suspended;
    if (assignments != null) result.assignments.addAll(assignments);
    if (issuedAt != null) result.issuedAt = issuedAt;
    return result;
  }

  DesiredState._();

  factory DesiredState.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      DesiredState()..mergeFromBuffer(data, registry);
  factory DesiredState.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      DesiredState()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'DesiredState',
      package:
          const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_treatment.v1'),
      createEmptyInstance: DesiredState.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'agentId')
    ..a<$fixnum.Int64>(
        2, _omitFieldNames ? '' : 'revision', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..aOB(3, _omitFieldNames ? '' : 'suspended')
    ..pPM<Assignment>(4, _omitFieldNames ? '' : 'assignments',
        subBuilder: Assignment.$_createMessage)
    ..aOM<$0.Timestamp>(5, _omitFieldNames ? '' : 'issuedAt',
        subBuilder: $0.Timestamp.$_createMessage)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DesiredState clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DesiredState copyWith(void Function(DesiredState) updates) =>
      super.copyWith((message) => updates(message as DesiredState))
          as DesiredState;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated('Use DesiredState() / DesiredState.new instead')
  static DesiredState create() => DesiredState._();
  static $pb.GeneratedMessage $_createMessage() => DesiredState._();
  @$core.override
  DesiredState createEmptyInstance() => DesiredState._();
  @$core.pragma('dart2js:noInline')
  static DesiredState getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<DesiredState>(
          DesiredState.$_createMessage);
  static DesiredState? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get agentId => $_getSZ(0);
  @$pb.TagNumber(1)
  set agentId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasAgentId() => $_has(0);
  @$pb.TagNumber(1)
  void clearAgentId() => $_clearField(1);

  @$pb.TagNumber(2)
  $fixnum.Int64 get revision => $_getI64(1);
  @$pb.TagNumber(2)
  set revision($fixnum.Int64 value) => $_setInt64(1, value);
  @$pb.TagNumber(2)
  $core.bool hasRevision() => $_has(1);
  @$pb.TagNumber(2)
  void clearRevision() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.bool get suspended => $_getBF(2);
  @$pb.TagNumber(3)
  set suspended($core.bool value) => $_setBool(2, value);
  @$pb.TagNumber(3)
  $core.bool hasSuspended() => $_has(2);
  @$pb.TagNumber(3)
  void clearSuspended() => $_clearField(3);

  @$pb.TagNumber(4)
  $pb.PbList<Assignment> get assignments => $_getList(3);

  @$pb.TagNumber(5)
  $0.Timestamp get issuedAt => $_getN(4);
  @$pb.TagNumber(5)
  set issuedAt($0.Timestamp value) => $_setField(5, value);
  @$pb.TagNumber(5)
  $core.bool hasIssuedAt() => $_has(4);
  @$pb.TagNumber(5)
  void clearIssuedAt() => $_clearField(5);
  @$pb.TagNumber(5)
  $0.Timestamp ensureIssuedAt() => $_ensure(4);
}

class Capabilities extends $pb.GeneratedMessage {
  factory Capabilities({
    $core.String? bootId,
    $core.String? kernelRelease,
    $core.Iterable<$core.String>? congestionAlgorithms,
    $core.bool? simpleHostRouting,
    $core.bool? actuatorEnabled,
    $core.String? collectionId,
  }) {
    final result = Capabilities._();
    if (bootId != null) result.bootId = bootId;
    if (kernelRelease != null) result.kernelRelease = kernelRelease;
    if (congestionAlgorithms != null)
      result.congestionAlgorithms.addAll(congestionAlgorithms);
    if (simpleHostRouting != null) result.simpleHostRouting = simpleHostRouting;
    if (actuatorEnabled != null) result.actuatorEnabled = actuatorEnabled;
    if (collectionId != null) result.collectionId = collectionId;
    return result;
  }

  Capabilities._();

  factory Capabilities.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      Capabilities()..mergeFromBuffer(data, registry);
  factory Capabilities.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      Capabilities()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'Capabilities',
      package:
          const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_treatment.v1'),
      createEmptyInstance: Capabilities.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'bootId')
    ..aOS(2, _omitFieldNames ? '' : 'kernelRelease')
    ..pPS(3, _omitFieldNames ? '' : 'congestionAlgorithms')
    ..aOB(4, _omitFieldNames ? '' : 'simpleHostRouting')
    ..aOB(5, _omitFieldNames ? '' : 'actuatorEnabled')
    ..aOS(6, _omitFieldNames ? '' : 'collectionId')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Capabilities clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Capabilities copyWith(void Function(Capabilities) updates) =>
      super.copyWith((message) => updates(message as Capabilities))
          as Capabilities;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated('Use Capabilities() / Capabilities.new instead')
  static Capabilities create() => Capabilities._();
  static $pb.GeneratedMessage $_createMessage() => Capabilities._();
  @$core.override
  Capabilities createEmptyInstance() => Capabilities._();
  @$core.pragma('dart2js:noInline')
  static Capabilities getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<Capabilities>(
          Capabilities.$_createMessage);
  static Capabilities? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get bootId => $_getSZ(0);
  @$pb.TagNumber(1)
  set bootId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasBootId() => $_has(0);
  @$pb.TagNumber(1)
  void clearBootId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get kernelRelease => $_getSZ(1);
  @$pb.TagNumber(2)
  set kernelRelease($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasKernelRelease() => $_has(1);
  @$pb.TagNumber(2)
  void clearKernelRelease() => $_clearField(2);

  @$pb.TagNumber(3)
  $pb.PbList<$core.String> get congestionAlgorithms => $_getList(2);

  @$pb.TagNumber(4)
  $core.bool get simpleHostRouting => $_getBF(3);
  @$pb.TagNumber(4)
  set simpleHostRouting($core.bool value) => $_setBool(3, value);
  @$pb.TagNumber(4)
  $core.bool hasSimpleHostRouting() => $_has(3);
  @$pb.TagNumber(4)
  void clearSimpleHostRouting() => $_clearField(4);

  @$pb.TagNumber(5)
  $core.bool get actuatorEnabled => $_getBF(4);
  @$pb.TagNumber(5)
  set actuatorEnabled($core.bool value) => $_setBool(4, value);
  @$pb.TagNumber(5)
  $core.bool hasActuatorEnabled() => $_has(4);
  @$pb.TagNumber(5)
  void clearActuatorEnabled() => $_clearField(5);

  /// Per-process nonce the agent writes into its journal header and the
  /// daemon stamps into every record as collection_id. Evidence counts only
  /// records whose (agent_id, host_boot_id, collection_id) an agent reported.
  @$pb.TagNumber(6)
  $core.String get collectionId => $_getSZ(5);
  @$pb.TagNumber(6)
  set collectionId($core.String value) => $_setString(5, value);
  @$pb.TagNumber(6)
  $core.bool hasCollectionId() => $_has(5);
  @$pb.TagNumber(6)
  void clearCollectionId() => $_clearField(6);
}

/// ApplicationRecord is the agent's account of one assignment. Agents report
/// these on check-in with installed_route and installed_boot_id cleared.
class ApplicationRecord extends $pb.GeneratedMessage {
  factory ApplicationRecord({
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
    final result = ApplicationRecord._();
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

  ApplicationRecord._();

  factory ApplicationRecord.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ApplicationRecord()..mergeFromBuffer(data, registry);
  factory ApplicationRecord.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ApplicationRecord()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ApplicationRecord',
      package:
          const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_treatment.v1'),
      createEmptyInstance: ApplicationRecord.$_createMessage)
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
  ApplicationRecord clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ApplicationRecord copyWith(void Function(ApplicationRecord) updates) =>
      super.copyWith((message) => updates(message as ApplicationRecord))
          as ApplicationRecord;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated('Use ApplicationRecord() / ApplicationRecord.new instead')
  static ApplicationRecord create() => ApplicationRecord._();
  static $pb.GeneratedMessage $_createMessage() => ApplicationRecord._();
  @$core.override
  ApplicationRecord createEmptyInstance() => ApplicationRecord._();
  @$core.pragma('dart2js:noInline')
  static ApplicationRecord getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<ApplicationRecord>(
          ApplicationRecord.$_createMessage);
  static ApplicationRecord? _defaultInstance;

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

  /// Locally generated canonical netlink route body, never accepted from RPC.
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

/// LocalState is the agent's journal snapshot. It is a file format for the
/// host-side journal and the local tooling, not an RPC payload.
class LocalState extends $pb.GeneratedMessage {
  factory LocalState({
    DesiredState? desired,
    $core.bool? locallySuspended,
    $core.String? suspensionReason,
    $core.Iterable<ApplicationRecord>? records,
    $0.Timestamp? updatedAt,
  }) {
    final result = LocalState._();
    if (desired != null) result.desired = desired;
    if (locallySuspended != null) result.locallySuspended = locallySuspended;
    if (suspensionReason != null) result.suspensionReason = suspensionReason;
    if (records != null) result.records.addAll(records);
    if (updatedAt != null) result.updatedAt = updatedAt;
    return result;
  }

  LocalState._();

  factory LocalState.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      LocalState()..mergeFromBuffer(data, registry);
  factory LocalState.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      LocalState()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'LocalState',
      package:
          const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_treatment.v1'),
      createEmptyInstance: LocalState.$_createMessage)
    ..aOM<DesiredState>(1, _omitFieldNames ? '' : 'desired',
        subBuilder: DesiredState.$_createMessage)
    ..aOB(2, _omitFieldNames ? '' : 'locallySuspended')
    ..aOS(3, _omitFieldNames ? '' : 'suspensionReason')
    ..pPM<ApplicationRecord>(4, _omitFieldNames ? '' : 'records',
        subBuilder: ApplicationRecord.$_createMessage)
    ..aOM<$0.Timestamp>(5, _omitFieldNames ? '' : 'updatedAt',
        subBuilder: $0.Timestamp.$_createMessage)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  LocalState clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  LocalState copyWith(void Function(LocalState) updates) =>
      super.copyWith((message) => updates(message as LocalState)) as LocalState;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated('Use LocalState() / LocalState.new instead')
  static LocalState create() => LocalState._();
  static $pb.GeneratedMessage $_createMessage() => LocalState._();
  @$core.override
  LocalState createEmptyInstance() => LocalState._();
  @$core.pragma('dart2js:noInline')
  static LocalState getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<LocalState>(LocalState.$_createMessage);
  static LocalState? _defaultInstance;

  @$pb.TagNumber(1)
  DesiredState get desired => $_getN(0);
  @$pb.TagNumber(1)
  set desired(DesiredState value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasDesired() => $_has(0);
  @$pb.TagNumber(1)
  void clearDesired() => $_clearField(1);
  @$pb.TagNumber(1)
  DesiredState ensureDesired() => $_ensure(0);

  @$pb.TagNumber(2)
  $core.bool get locallySuspended => $_getBF(1);
  @$pb.TagNumber(2)
  set locallySuspended($core.bool value) => $_setBool(1, value);
  @$pb.TagNumber(2)
  $core.bool hasLocallySuspended() => $_has(1);
  @$pb.TagNumber(2)
  void clearLocallySuspended() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get suspensionReason => $_getSZ(2);
  @$pb.TagNumber(3)
  set suspensionReason($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasSuspensionReason() => $_has(2);
  @$pb.TagNumber(3)
  void clearSuspensionReason() => $_clearField(3);

  @$pb.TagNumber(4)
  $pb.PbList<ApplicationRecord> get records => $_getList(3);

  @$pb.TagNumber(5)
  $0.Timestamp get updatedAt => $_getN(4);
  @$pb.TagNumber(5)
  set updatedAt($0.Timestamp value) => $_setField(5, value);
  @$pb.TagNumber(5)
  $core.bool hasUpdatedAt() => $_has(4);
  @$pb.TagNumber(5)
  void clearUpdatedAt() => $_clearField(5);
  @$pb.TagNumber(5)
  $0.Timestamp ensureUpdatedAt() => $_ensure(4);
}

class HostEvidence extends $pb.GeneratedMessage {
  factory HostEvidence({
    $core.String? agentId,
    $fixnum.Int64? sockets,
    $fixnum.Int64? bytesAcked,
    $core.double? goodputBps,
    $core.double? retransmissionRatio,
    $core.double? rttP95Ms,
    $fixnum.Int64? exposedSockets,
    $core.bool? collectionHealthy,
    $fixnum.Int64? unboundSockets,
  }) {
    final result = HostEvidence._();
    if (agentId != null) result.agentId = agentId;
    if (sockets != null) result.sockets = sockets;
    if (bytesAcked != null) result.bytesAcked = bytesAcked;
    if (goodputBps != null) result.goodputBps = goodputBps;
    if (retransmissionRatio != null)
      result.retransmissionRatio = retransmissionRatio;
    if (rttP95Ms != null) result.rttP95Ms = rttP95Ms;
    if (exposedSockets != null) result.exposedSockets = exposedSockets;
    if (collectionHealthy != null) result.collectionHealthy = collectionHealthy;
    if (unboundSockets != null) result.unboundSockets = unboundSockets;
    return result;
  }

  HostEvidence._();

  factory HostEvidence.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      HostEvidence()..mergeFromBuffer(data, registry);
  factory HostEvidence.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      HostEvidence()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'HostEvidence',
      package:
          const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_treatment.v1'),
      createEmptyInstance: HostEvidence.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'agentId')
    ..a<$fixnum.Int64>(2, _omitFieldNames ? '' : 'sockets', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..a<$fixnum.Int64>(
        3, _omitFieldNames ? '' : 'bytesAcked', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..aD(4, _omitFieldNames ? '' : 'goodputBps')
    ..aD(5, _omitFieldNames ? '' : 'retransmissionRatio')
    ..aD(6, _omitFieldNames ? '' : 'rttP95Ms')
    ..a<$fixnum.Int64>(
        7, _omitFieldNames ? '' : 'exposedSockets', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..aOB(8, _omitFieldNames ? '' : 'collectionHealthy')
    ..a<$fixnum.Int64>(
        9, _omitFieldNames ? '' : 'unboundSockets', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  HostEvidence clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  HostEvidence copyWith(void Function(HostEvidence) updates) =>
      super.copyWith((message) => updates(message as HostEvidence))
          as HostEvidence;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated('Use HostEvidence() / HostEvidence.new instead')
  static HostEvidence create() => HostEvidence._();
  static $pb.GeneratedMessage $_createMessage() => HostEvidence._();
  @$core.override
  HostEvidence createEmptyInstance() => HostEvidence._();
  @$core.pragma('dart2js:noInline')
  static HostEvidence getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<HostEvidence>(
          HostEvidence.$_createMessage);
  static HostEvidence? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get agentId => $_getSZ(0);
  @$pb.TagNumber(1)
  set agentId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasAgentId() => $_has(0);
  @$pb.TagNumber(1)
  void clearAgentId() => $_clearField(1);

  @$pb.TagNumber(2)
  $fixnum.Int64 get sockets => $_getI64(1);
  @$pb.TagNumber(2)
  set sockets($fixnum.Int64 value) => $_setInt64(1, value);
  @$pb.TagNumber(2)
  $core.bool hasSockets() => $_has(1);
  @$pb.TagNumber(2)
  void clearSockets() => $_clearField(2);

  @$pb.TagNumber(3)
  $fixnum.Int64 get bytesAcked => $_getI64(2);
  @$pb.TagNumber(3)
  set bytesAcked($fixnum.Int64 value) => $_setInt64(2, value);
  @$pb.TagNumber(3)
  $core.bool hasBytesAcked() => $_has(2);
  @$pb.TagNumber(3)
  void clearBytesAcked() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.double get goodputBps => $_getN(3);
  @$pb.TagNumber(4)
  set goodputBps($core.double value) => $_setDouble(3, value);
  @$pb.TagNumber(4)
  $core.bool hasGoodputBps() => $_has(3);
  @$pb.TagNumber(4)
  void clearGoodputBps() => $_clearField(4);

  @$pb.TagNumber(5)
  $core.double get retransmissionRatio => $_getN(4);
  @$pb.TagNumber(5)
  set retransmissionRatio($core.double value) => $_setDouble(4, value);
  @$pb.TagNumber(5)
  $core.bool hasRetransmissionRatio() => $_has(4);
  @$pb.TagNumber(5)
  void clearRetransmissionRatio() => $_clearField(5);

  @$pb.TagNumber(6)
  $core.double get rttP95Ms => $_getN(5);
  @$pb.TagNumber(6)
  set rttP95Ms($core.double value) => $_setDouble(5, value);
  @$pb.TagNumber(6)
  $core.bool hasRttP95Ms() => $_has(5);
  @$pb.TagNumber(6)
  void clearRttP95Ms() => $_clearField(6);

  @$pb.TagNumber(7)
  $fixnum.Int64 get exposedSockets => $_getI64(6);
  @$pb.TagNumber(7)
  set exposedSockets($fixnum.Int64 value) => $_setInt64(6, value);
  @$pb.TagNumber(7)
  $core.bool hasExposedSockets() => $_has(6);
  @$pb.TagNumber(7)
  void clearExposedSockets() => $_clearField(7);

  @$pb.TagNumber(8)
  $core.bool get collectionHealthy => $_getBF(7);
  @$pb.TagNumber(8)
  set collectionHealthy($core.bool value) => $_setBool(7, value);
  @$pb.TagNumber(8)
  $core.bool hasCollectionHealthy() => $_has(7);
  @$pb.TagNumber(8)
  void clearCollectionHealthy() => $_clearField(8);

  /// Sockets stamped with this agent_id whose boot and collection nonce
  /// match no session the agent reported. They are excluded from every other
  /// field and counted here so a reviewer can see them.
  @$pb.TagNumber(9)
  $fixnum.Int64 get unboundSockets => $_getI64(8);
  @$pb.TagNumber(9)
  set unboundSockets($fixnum.Int64 value) => $_setInt64(8, value);
  @$pb.TagNumber(9)
  $core.bool hasUnboundSockets() => $_has(8);
  @$pb.TagNumber(9)
  void clearUnboundSockets() => $_clearField(9);
}

class Evidence extends $pb.GeneratedMessage {
  factory Evidence({
    $core.String? id,
    $0.Timestamp? analyzedAt,
    $0.Timestamp? windowStart,
    $0.Timestamp? windowEnd,
    $core.String? queryVersion,
    $core.String? enrichmentVersion,
    $core.Iterable<HostEvidence>? hosts,
    $core.String? artifactSha256,
  }) {
    final result = Evidence._();
    if (id != null) result.id = id;
    if (analyzedAt != null) result.analyzedAt = analyzedAt;
    if (windowStart != null) result.windowStart = windowStart;
    if (windowEnd != null) result.windowEnd = windowEnd;
    if (queryVersion != null) result.queryVersion = queryVersion;
    if (enrichmentVersion != null) result.enrichmentVersion = enrichmentVersion;
    if (hosts != null) result.hosts.addAll(hosts);
    if (artifactSha256 != null) result.artifactSha256 = artifactSha256;
    return result;
  }

  Evidence._();

  factory Evidence.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      Evidence()..mergeFromBuffer(data, registry);
  factory Evidence.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      Evidence()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'Evidence',
      package:
          const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_treatment.v1'),
      createEmptyInstance: Evidence.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'id')
    ..aOM<$0.Timestamp>(2, _omitFieldNames ? '' : 'analyzedAt',
        subBuilder: $0.Timestamp.$_createMessage)
    ..aOM<$0.Timestamp>(3, _omitFieldNames ? '' : 'windowStart',
        subBuilder: $0.Timestamp.$_createMessage)
    ..aOM<$0.Timestamp>(4, _omitFieldNames ? '' : 'windowEnd',
        subBuilder: $0.Timestamp.$_createMessage)
    ..aOS(5, _omitFieldNames ? '' : 'queryVersion')
    ..aOS(6, _omitFieldNames ? '' : 'enrichmentVersion')
    ..pPM<HostEvidence>(7, _omitFieldNames ? '' : 'hosts',
        subBuilder: HostEvidence.$_createMessage)
    ..aOS(8, _omitFieldNames ? '' : 'artifactSha256')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Evidence clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Evidence copyWith(void Function(Evidence) updates) =>
      super.copyWith((message) => updates(message as Evidence)) as Evidence;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated('Use Evidence() / Evidence.new instead')
  static Evidence create() => Evidence._();
  static $pb.GeneratedMessage $_createMessage() => Evidence._();
  @$core.override
  Evidence createEmptyInstance() => Evidence._();
  @$core.pragma('dart2js:noInline')
  static Evidence getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<Evidence>(Evidence.$_createMessage);
  static Evidence? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get id => $_getSZ(0);
  @$pb.TagNumber(1)
  set id($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasId() => $_has(0);
  @$pb.TagNumber(1)
  void clearId() => $_clearField(1);

  @$pb.TagNumber(2)
  $0.Timestamp get analyzedAt => $_getN(1);
  @$pb.TagNumber(2)
  set analyzedAt($0.Timestamp value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasAnalyzedAt() => $_has(1);
  @$pb.TagNumber(2)
  void clearAnalyzedAt() => $_clearField(2);
  @$pb.TagNumber(2)
  $0.Timestamp ensureAnalyzedAt() => $_ensure(1);

  @$pb.TagNumber(3)
  $0.Timestamp get windowStart => $_getN(2);
  @$pb.TagNumber(3)
  set windowStart($0.Timestamp value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasWindowStart() => $_has(2);
  @$pb.TagNumber(3)
  void clearWindowStart() => $_clearField(3);
  @$pb.TagNumber(3)
  $0.Timestamp ensureWindowStart() => $_ensure(2);

  @$pb.TagNumber(4)
  $0.Timestamp get windowEnd => $_getN(3);
  @$pb.TagNumber(4)
  set windowEnd($0.Timestamp value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasWindowEnd() => $_has(3);
  @$pb.TagNumber(4)
  void clearWindowEnd() => $_clearField(4);
  @$pb.TagNumber(4)
  $0.Timestamp ensureWindowEnd() => $_ensure(3);

  @$pb.TagNumber(5)
  $core.String get queryVersion => $_getSZ(4);
  @$pb.TagNumber(5)
  set queryVersion($core.String value) => $_setString(4, value);
  @$pb.TagNumber(5)
  $core.bool hasQueryVersion() => $_has(4);
  @$pb.TagNumber(5)
  void clearQueryVersion() => $_clearField(5);

  @$pb.TagNumber(6)
  $core.String get enrichmentVersion => $_getSZ(5);
  @$pb.TagNumber(6)
  set enrichmentVersion($core.String value) => $_setString(5, value);
  @$pb.TagNumber(6)
  $core.bool hasEnrichmentVersion() => $_has(5);
  @$pb.TagNumber(6)
  void clearEnrichmentVersion() => $_clearField(6);

  @$pb.TagNumber(7)
  $pb.PbList<HostEvidence> get hosts => $_getList(6);

  /// SHA-256 of the raw warehouse response bytes, as received, that this
  /// summary was decoded from. The response itself is not archived: the
  /// digest lets two controllers (or a later audit) confirm they decoded the
  /// same bytes, and reproducing it means re-running query_version over
  /// [window_start, window_end) against the same table. Archiving responses
  /// to object storage is a possible follow-up, not a current guarantee.
  @$pb.TagNumber(8)
  $core.String get artifactSha256 => $_getSZ(7);
  @$pb.TagNumber(8)
  set artifactSha256($core.String value) => $_setString(7, value);
  @$pb.TagNumber(8)
  $core.bool hasArtifactSha256() => $_has(7);
  @$pb.TagNumber(8)
  void clearArtifactSha256() => $_clearField(8);
}

class ExperimentPolicy extends $pb.GeneratedMessage {
  factory ExperimentPolicy({
    $1.Duration? minimumObservation,
    $1.Duration? maximumPhase,
    $fixnum.Int64? minimumSocketsPerHost,
    $fixnum.Int64? minimumBytesPerHost,
    $core.int? minimumTreatedHosts,
    $core.double? minimumImprovement,
    $core.double? maximumRetransmissionIncrease,
    $core.double? maximumRttIncrease,
    $1.Duration? maximumEvidenceAge,
  }) {
    final result = ExperimentPolicy._();
    if (minimumObservation != null)
      result.minimumObservation = minimumObservation;
    if (maximumPhase != null) result.maximumPhase = maximumPhase;
    if (minimumSocketsPerHost != null)
      result.minimumSocketsPerHost = minimumSocketsPerHost;
    if (minimumBytesPerHost != null)
      result.minimumBytesPerHost = minimumBytesPerHost;
    if (minimumTreatedHosts != null)
      result.minimumTreatedHosts = minimumTreatedHosts;
    if (minimumImprovement != null)
      result.minimumImprovement = minimumImprovement;
    if (maximumRetransmissionIncrease != null)
      result.maximumRetransmissionIncrease = maximumRetransmissionIncrease;
    if (maximumRttIncrease != null)
      result.maximumRttIncrease = maximumRttIncrease;
    if (maximumEvidenceAge != null)
      result.maximumEvidenceAge = maximumEvidenceAge;
    return result;
  }

  ExperimentPolicy._();

  factory ExperimentPolicy.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ExperimentPolicy()..mergeFromBuffer(data, registry);
  factory ExperimentPolicy.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ExperimentPolicy()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ExperimentPolicy',
      package:
          const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_treatment.v1'),
      createEmptyInstance: ExperimentPolicy.$_createMessage)
    ..aOM<$1.Duration>(1, _omitFieldNames ? '' : 'minimumObservation',
        subBuilder: $1.Duration.$_createMessage)
    ..aOM<$1.Duration>(2, _omitFieldNames ? '' : 'maximumPhase',
        subBuilder: $1.Duration.$_createMessage)
    ..a<$fixnum.Int64>(
        3, _omitFieldNames ? '' : 'minimumSocketsPerHost', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..a<$fixnum.Int64>(
        4, _omitFieldNames ? '' : 'minimumBytesPerHost', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..aI(5, _omitFieldNames ? '' : 'minimumTreatedHosts',
        fieldType: $pb.PbFieldType.OU3)
    ..aD(6, _omitFieldNames ? '' : 'minimumImprovement')
    ..aD(7, _omitFieldNames ? '' : 'maximumRetransmissionIncrease')
    ..aD(8, _omitFieldNames ? '' : 'maximumRttIncrease')
    ..aOM<$1.Duration>(9, _omitFieldNames ? '' : 'maximumEvidenceAge',
        subBuilder: $1.Duration.$_createMessage)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ExperimentPolicy clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ExperimentPolicy copyWith(void Function(ExperimentPolicy) updates) =>
      super.copyWith((message) => updates(message as ExperimentPolicy))
          as ExperimentPolicy;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated('Use ExperimentPolicy() / ExperimentPolicy.new instead')
  static ExperimentPolicy create() => ExperimentPolicy._();
  static $pb.GeneratedMessage $_createMessage() => ExperimentPolicy._();
  @$core.override
  ExperimentPolicy createEmptyInstance() => ExperimentPolicy._();
  @$core.pragma('dart2js:noInline')
  static ExperimentPolicy getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<ExperimentPolicy>(
          ExperimentPolicy.$_createMessage);
  static ExperimentPolicy? _defaultInstance;

  @$pb.TagNumber(1)
  $1.Duration get minimumObservation => $_getN(0);
  @$pb.TagNumber(1)
  set minimumObservation($1.Duration value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasMinimumObservation() => $_has(0);
  @$pb.TagNumber(1)
  void clearMinimumObservation() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.Duration ensureMinimumObservation() => $_ensure(0);

  @$pb.TagNumber(2)
  $1.Duration get maximumPhase => $_getN(1);
  @$pb.TagNumber(2)
  set maximumPhase($1.Duration value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasMaximumPhase() => $_has(1);
  @$pb.TagNumber(2)
  void clearMaximumPhase() => $_clearField(2);
  @$pb.TagNumber(2)
  $1.Duration ensureMaximumPhase() => $_ensure(1);

  @$pb.TagNumber(3)
  $fixnum.Int64 get minimumSocketsPerHost => $_getI64(2);
  @$pb.TagNumber(3)
  set minimumSocketsPerHost($fixnum.Int64 value) => $_setInt64(2, value);
  @$pb.TagNumber(3)
  $core.bool hasMinimumSocketsPerHost() => $_has(2);
  @$pb.TagNumber(3)
  void clearMinimumSocketsPerHost() => $_clearField(3);

  @$pb.TagNumber(4)
  $fixnum.Int64 get minimumBytesPerHost => $_getI64(3);
  @$pb.TagNumber(4)
  set minimumBytesPerHost($fixnum.Int64 value) => $_setInt64(3, value);
  @$pb.TagNumber(4)
  $core.bool hasMinimumBytesPerHost() => $_has(3);
  @$pb.TagNumber(4)
  void clearMinimumBytesPerHost() => $_clearField(4);

  @$pb.TagNumber(5)
  $core.int get minimumTreatedHosts => $_getIZ(4);
  @$pb.TagNumber(5)
  set minimumTreatedHosts($core.int value) => $_setUnsignedInt32(4, value);
  @$pb.TagNumber(5)
  $core.bool hasMinimumTreatedHosts() => $_has(4);
  @$pb.TagNumber(5)
  void clearMinimumTreatedHosts() => $_clearField(5);

  @$pb.TagNumber(6)
  $core.double get minimumImprovement => $_getN(5);
  @$pb.TagNumber(6)
  set minimumImprovement($core.double value) => $_setDouble(5, value);
  @$pb.TagNumber(6)
  $core.bool hasMinimumImprovement() => $_has(5);
  @$pb.TagNumber(6)
  void clearMinimumImprovement() => $_clearField(6);

  @$pb.TagNumber(7)
  $core.double get maximumRetransmissionIncrease => $_getN(6);
  @$pb.TagNumber(7)
  set maximumRetransmissionIncrease($core.double value) =>
      $_setDouble(6, value);
  @$pb.TagNumber(7)
  $core.bool hasMaximumRetransmissionIncrease() => $_has(6);
  @$pb.TagNumber(7)
  void clearMaximumRetransmissionIncrease() => $_clearField(7);

  @$pb.TagNumber(8)
  $core.double get maximumRttIncrease => $_getN(7);
  @$pb.TagNumber(8)
  set maximumRttIncrease($core.double value) => $_setDouble(7, value);
  @$pb.TagNumber(8)
  $core.bool hasMaximumRttIncrease() => $_has(7);
  @$pb.TagNumber(8)
  void clearMaximumRttIncrease() => $_clearField(8);

  @$pb.TagNumber(9)
  $1.Duration get maximumEvidenceAge => $_getN(8);
  @$pb.TagNumber(9)
  set maximumEvidenceAge($1.Duration value) => $_setField(9, value);
  @$pb.TagNumber(9)
  $core.bool hasMaximumEvidenceAge() => $_has(8);
  @$pb.TagNumber(9)
  void clearMaximumEvidenceAge() => $_clearField(9);
  @$pb.TagNumber(9)
  $1.Duration ensureMaximumEvidenceAge() => $_ensure(8);
}

class Phase extends $pb.GeneratedMessage {
  factory Phase({
    $core.int? targetHosts,
    $0.Timestamp? issuedAt,
    $0.Timestamp? observationStartedAt,
    $0.Timestamp? evaluatedAt,
    Evidence? evidence,
    Outcome? outcome,
    $core.String? reviewer,
    $core.String? reason,
  }) {
    final result = Phase._();
    if (targetHosts != null) result.targetHosts = targetHosts;
    if (issuedAt != null) result.issuedAt = issuedAt;
    if (observationStartedAt != null)
      result.observationStartedAt = observationStartedAt;
    if (evaluatedAt != null) result.evaluatedAt = evaluatedAt;
    if (evidence != null) result.evidence = evidence;
    if (outcome != null) result.outcome = outcome;
    if (reviewer != null) result.reviewer = reviewer;
    if (reason != null) result.reason = reason;
    return result;
  }

  Phase._();

  factory Phase.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      Phase()..mergeFromBuffer(data, registry);
  factory Phase.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      Phase()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'Phase',
      package:
          const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_treatment.v1'),
      createEmptyInstance: Phase.$_createMessage)
    ..aI(1, _omitFieldNames ? '' : 'targetHosts',
        fieldType: $pb.PbFieldType.OU3)
    ..aOM<$0.Timestamp>(2, _omitFieldNames ? '' : 'issuedAt',
        subBuilder: $0.Timestamp.$_createMessage)
    ..aOM<$0.Timestamp>(3, _omitFieldNames ? '' : 'observationStartedAt',
        subBuilder: $0.Timestamp.$_createMessage)
    ..aOM<$0.Timestamp>(4, _omitFieldNames ? '' : 'evaluatedAt',
        subBuilder: $0.Timestamp.$_createMessage)
    ..aOM<Evidence>(5, _omitFieldNames ? '' : 'evidence',
        subBuilder: Evidence.$_createMessage)
    ..aE<Outcome>(6, _omitFieldNames ? '' : 'outcome',
        enumValues: Outcome.values)
    ..aOS(7, _omitFieldNames ? '' : 'reviewer')
    ..aOS(8, _omitFieldNames ? '' : 'reason')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Phase clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Phase copyWith(void Function(Phase) updates) =>
      super.copyWith((message) => updates(message as Phase)) as Phase;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated('Use Phase() / Phase.new instead')
  static Phase create() => Phase._();
  static $pb.GeneratedMessage $_createMessage() => Phase._();
  @$core.override
  Phase createEmptyInstance() => Phase._();
  @$core.pragma('dart2js:noInline')
  static Phase getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<Phase>(Phase.$_createMessage);
  static Phase? _defaultInstance;

  @$pb.TagNumber(1)
  $core.int get targetHosts => $_getIZ(0);
  @$pb.TagNumber(1)
  set targetHosts($core.int value) => $_setUnsignedInt32(0, value);
  @$pb.TagNumber(1)
  $core.bool hasTargetHosts() => $_has(0);
  @$pb.TagNumber(1)
  void clearTargetHosts() => $_clearField(1);

  @$pb.TagNumber(2)
  $0.Timestamp get issuedAt => $_getN(1);
  @$pb.TagNumber(2)
  set issuedAt($0.Timestamp value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasIssuedAt() => $_has(1);
  @$pb.TagNumber(2)
  void clearIssuedAt() => $_clearField(2);
  @$pb.TagNumber(2)
  $0.Timestamp ensureIssuedAt() => $_ensure(1);

  @$pb.TagNumber(3)
  $0.Timestamp get observationStartedAt => $_getN(2);
  @$pb.TagNumber(3)
  set observationStartedAt($0.Timestamp value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasObservationStartedAt() => $_has(2);
  @$pb.TagNumber(3)
  void clearObservationStartedAt() => $_clearField(3);
  @$pb.TagNumber(3)
  $0.Timestamp ensureObservationStartedAt() => $_ensure(2);

  @$pb.TagNumber(4)
  $0.Timestamp get evaluatedAt => $_getN(3);
  @$pb.TagNumber(4)
  set evaluatedAt($0.Timestamp value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasEvaluatedAt() => $_has(3);
  @$pb.TagNumber(4)
  void clearEvaluatedAt() => $_clearField(4);
  @$pb.TagNumber(4)
  $0.Timestamp ensureEvaluatedAt() => $_ensure(3);

  @$pb.TagNumber(5)
  Evidence get evidence => $_getN(4);
  @$pb.TagNumber(5)
  set evidence(Evidence value) => $_setField(5, value);
  @$pb.TagNumber(5)
  $core.bool hasEvidence() => $_has(4);
  @$pb.TagNumber(5)
  void clearEvidence() => $_clearField(5);
  @$pb.TagNumber(5)
  Evidence ensureEvidence() => $_ensure(4);

  @$pb.TagNumber(6)
  Outcome get outcome => $_getN(5);
  @$pb.TagNumber(6)
  set outcome(Outcome value) => $_setField(6, value);
  @$pb.TagNumber(6)
  $core.bool hasOutcome() => $_has(5);
  @$pb.TagNumber(6)
  void clearOutcome() => $_clearField(6);

  @$pb.TagNumber(7)
  $core.String get reviewer => $_getSZ(6);
  @$pb.TagNumber(7)
  set reviewer($core.String value) => $_setString(6, value);
  @$pb.TagNumber(7)
  $core.bool hasReviewer() => $_has(6);
  @$pb.TagNumber(7)
  void clearReviewer() => $_clearField(7);

  @$pb.TagNumber(8)
  $core.String get reason => $_getSZ(7);
  @$pb.TagNumber(8)
  set reason($core.String value) => $_setString(7, value);
  @$pb.TagNumber(8)
  $core.bool hasReason() => $_has(7);
  @$pb.TagNumber(8)
  void clearReason() => $_clearField(8);
}

class Experiment extends $pb.GeneratedMessage {
  factory Experiment({
    $core.String? id,
    $core.String? location,
    $core.String? hypothesis,
    Treatment? treatment,
    ExperimentPolicy? policy,
    Evidence? baseline,
    $core.Iterable<$core.String>? cohortOrder,
    $core.Iterable<Phase>? phases,
    ExperimentStatus? status,
    $fixnum.Int64? revision,
    $0.Timestamp? proposedAt,
    Evidence? latestEvidence,
    Outcome? latestOutcome,
    $0.Timestamp? updatedAt,
  }) {
    final result = Experiment._();
    if (id != null) result.id = id;
    if (location != null) result.location = location;
    if (hypothesis != null) result.hypothesis = hypothesis;
    if (treatment != null) result.treatment = treatment;
    if (policy != null) result.policy = policy;
    if (baseline != null) result.baseline = baseline;
    if (cohortOrder != null) result.cohortOrder.addAll(cohortOrder);
    if (phases != null) result.phases.addAll(phases);
    if (status != null) result.status = status;
    if (revision != null) result.revision = revision;
    if (proposedAt != null) result.proposedAt = proposedAt;
    if (latestEvidence != null) result.latestEvidence = latestEvidence;
    if (latestOutcome != null) result.latestOutcome = latestOutcome;
    if (updatedAt != null) result.updatedAt = updatedAt;
    return result;
  }

  Experiment._();

  factory Experiment.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      Experiment()..mergeFromBuffer(data, registry);
  factory Experiment.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      Experiment()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'Experiment',
      package:
          const $pb.PackageName(_omitMessageNames ? '' : 'xtcp_treatment.v1'),
      createEmptyInstance: Experiment.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'id')
    ..aOS(2, _omitFieldNames ? '' : 'location')
    ..aOS(3, _omitFieldNames ? '' : 'hypothesis')
    ..aOM<Treatment>(4, _omitFieldNames ? '' : 'treatment',
        subBuilder: Treatment.$_createMessage)
    ..aOM<ExperimentPolicy>(5, _omitFieldNames ? '' : 'policy',
        subBuilder: ExperimentPolicy.$_createMessage)
    ..aOM<Evidence>(6, _omitFieldNames ? '' : 'baseline',
        subBuilder: Evidence.$_createMessage)
    ..pPS(7, _omitFieldNames ? '' : 'cohortOrder')
    ..pPM<Phase>(8, _omitFieldNames ? '' : 'phases',
        subBuilder: Phase.$_createMessage)
    ..aE<ExperimentStatus>(9, _omitFieldNames ? '' : 'status',
        enumValues: ExperimentStatus.values)
    ..a<$fixnum.Int64>(
        10, _omitFieldNames ? '' : 'revision', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..aOM<$0.Timestamp>(11, _omitFieldNames ? '' : 'proposedAt',
        subBuilder: $0.Timestamp.$_createMessage)
    ..aOM<Evidence>(12, _omitFieldNames ? '' : 'latestEvidence',
        subBuilder: Evidence.$_createMessage)
    ..aE<Outcome>(13, _omitFieldNames ? '' : 'latestOutcome',
        enumValues: Outcome.values)
    ..aOM<$0.Timestamp>(14, _omitFieldNames ? '' : 'updatedAt',
        subBuilder: $0.Timestamp.$_createMessage)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Experiment clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Experiment copyWith(void Function(Experiment) updates) =>
      super.copyWith((message) => updates(message as Experiment)) as Experiment;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated('Use Experiment() / Experiment.new instead')
  static Experiment create() => Experiment._();
  static $pb.GeneratedMessage $_createMessage() => Experiment._();
  @$core.override
  Experiment createEmptyInstance() => Experiment._();
  @$core.pragma('dart2js:noInline')
  static Experiment getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<Experiment>(Experiment.$_createMessage);
  static Experiment? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get id => $_getSZ(0);
  @$pb.TagNumber(1)
  set id($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasId() => $_has(0);
  @$pb.TagNumber(1)
  void clearId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get location => $_getSZ(1);
  @$pb.TagNumber(2)
  set location($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasLocation() => $_has(1);
  @$pb.TagNumber(2)
  void clearLocation() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get hypothesis => $_getSZ(2);
  @$pb.TagNumber(3)
  set hypothesis($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasHypothesis() => $_has(2);
  @$pb.TagNumber(3)
  void clearHypothesis() => $_clearField(3);

  @$pb.TagNumber(4)
  Treatment get treatment => $_getN(3);
  @$pb.TagNumber(4)
  set treatment(Treatment value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasTreatment() => $_has(3);
  @$pb.TagNumber(4)
  void clearTreatment() => $_clearField(4);
  @$pb.TagNumber(4)
  Treatment ensureTreatment() => $_ensure(3);

  @$pb.TagNumber(5)
  ExperimentPolicy get policy => $_getN(4);
  @$pb.TagNumber(5)
  set policy(ExperimentPolicy value) => $_setField(5, value);
  @$pb.TagNumber(5)
  $core.bool hasPolicy() => $_has(4);
  @$pb.TagNumber(5)
  void clearPolicy() => $_clearField(5);
  @$pb.TagNumber(5)
  ExperimentPolicy ensurePolicy() => $_ensure(4);

  @$pb.TagNumber(6)
  Evidence get baseline => $_getN(5);
  @$pb.TagNumber(6)
  set baseline(Evidence value) => $_setField(6, value);
  @$pb.TagNumber(6)
  $core.bool hasBaseline() => $_has(5);
  @$pb.TagNumber(6)
  void clearBaseline() => $_clearField(6);
  @$pb.TagNumber(6)
  Evidence ensureBaseline() => $_ensure(5);

  /// Randomised host order; phases treat a growing prefix of it.
  @$pb.TagNumber(7)
  $pb.PbList<$core.String> get cohortOrder => $_getList(6);

  @$pb.TagNumber(8)
  $pb.PbList<Phase> get phases => $_getList(7);

  @$pb.TagNumber(9)
  ExperimentStatus get status => $_getN(8);
  @$pb.TagNumber(9)
  set status(ExperimentStatus value) => $_setField(9, value);
  @$pb.TagNumber(9)
  $core.bool hasStatus() => $_has(8);
  @$pb.TagNumber(9)
  void clearStatus() => $_clearField(9);

  @$pb.TagNumber(10)
  $fixnum.Int64 get revision => $_getI64(9);
  @$pb.TagNumber(10)
  set revision($fixnum.Int64 value) => $_setInt64(9, value);
  @$pb.TagNumber(10)
  $core.bool hasRevision() => $_has(9);
  @$pb.TagNumber(10)
  void clearRevision() => $_clearField(10);

  @$pb.TagNumber(11)
  $0.Timestamp get proposedAt => $_getN(10);
  @$pb.TagNumber(11)
  set proposedAt($0.Timestamp value) => $_setField(11, value);
  @$pb.TagNumber(11)
  $core.bool hasProposedAt() => $_has(10);
  @$pb.TagNumber(11)
  void clearProposedAt() => $_clearField(11);
  @$pb.TagNumber(11)
  $0.Timestamp ensureProposedAt() => $_ensure(10);

  @$pb.TagNumber(12)
  Evidence get latestEvidence => $_getN(11);
  @$pb.TagNumber(12)
  set latestEvidence(Evidence value) => $_setField(12, value);
  @$pb.TagNumber(12)
  $core.bool hasLatestEvidence() => $_has(11);
  @$pb.TagNumber(12)
  void clearLatestEvidence() => $_clearField(12);
  @$pb.TagNumber(12)
  Evidence ensureLatestEvidence() => $_ensure(11);

  @$pb.TagNumber(13)
  Outcome get latestOutcome => $_getN(12);
  @$pb.TagNumber(13)
  set latestOutcome(Outcome value) => $_setField(13, value);
  @$pb.TagNumber(13)
  $core.bool hasLatestOutcome() => $_has(12);
  @$pb.TagNumber(13)
  void clearLatestOutcome() => $_clearField(13);

  /// When the controller last changed this experiment. Terminal experiments
  /// suppress rediscovery of their destination for a day after this instant.
  @$pb.TagNumber(14)
  $0.Timestamp get updatedAt => $_getN(13);
  @$pb.TagNumber(14)
  set updatedAt($0.Timestamp value) => $_setField(14, value);
  @$pb.TagNumber(14)
  $core.bool hasUpdatedAt() => $_has(13);
  @$pb.TagNumber(14)
  void clearUpdatedAt() => $_clearField(14);
  @$pb.TagNumber(14)
  $0.Timestamp ensureUpdatedAt() => $_ensure(13);
}

class TreatmentExt {
  static final role = $pb.Extension<Role>(
      _omitMessageNames ? '' : 'google.protobuf.MethodOptions',
      _omitFieldNames ? '' : 'role',
      51002,
      $pb.PbFieldType.OE,
      defaultOrMaker: Role.ROLE_UNSPECIFIED,
      valueOf: Role.valueOf,
      enumValues: Role.values);
  static void registerAllExtensions($pb.ExtensionRegistry registry) {
    registry.add(role);
  }
}

const $core.bool _omitFieldNames =
    $core.bool.fromEnvironment('protobuf.omit_field_names');
const $core.bool _omitMessageNames =
    $core.bool.fromEnvironment('protobuf.omit_message_names');
