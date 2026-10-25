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

import 'package:protobuf/protobuf.dart' as $pb;

/// Role is the certificate class an RPC requires. The server resolves the
/// caller's SPIFFE identity once per call and compares it with this option;
/// a method without a declaration is refused for everyone (fail closed).
class Role extends $pb.ProtobufEnum {
  static const Role ROLE_UNSPECIFIED =
      Role._(0, _omitEnumNames ? '' : 'ROLE_UNSPECIFIED');

  /// spiffe://xtcp/agent/<location>/<id>; may only speak for its own agent_id.
  static const Role ROLE_AGENT = Role._(1, _omitEnumNames ? '' : 'ROLE_AGENT');

  /// spiffe://xtcp/operator/<id>; recorded as the audit actor.
  static const Role ROLE_OPERATOR =
      Role._(2, _omitEnumNames ? '' : 'ROLE_OPERATOR');

  static const $core.List<Role> values = <Role>[
    ROLE_UNSPECIFIED,
    ROLE_AGENT,
    ROLE_OPERATOR,
  ];

  static final $core.List<Role?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static Role? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const Role._(super.value, super.name);
}

class AssignmentMode extends $pb.ProtobufEnum {
  static const AssignmentMode ASSIGNMENT_MODE_UNSPECIFIED =
      AssignmentMode._(0, _omitEnumNames ? '' : 'ASSIGNMENT_MODE_UNSPECIFIED');
  static const AssignmentMode ASSIGNMENT_MODE_TRIAL =
      AssignmentMode._(1, _omitEnumNames ? '' : 'ASSIGNMENT_MODE_TRIAL');
  static const AssignmentMode ASSIGNMENT_MODE_STABLE =
      AssignmentMode._(2, _omitEnumNames ? '' : 'ASSIGNMENT_MODE_STABLE');
  static const AssignmentMode ASSIGNMENT_MODE_SUSPENDED =
      AssignmentMode._(3, _omitEnumNames ? '' : 'ASSIGNMENT_MODE_SUSPENDED');
  static const AssignmentMode ASSIGNMENT_MODE_RETIRED =
      AssignmentMode._(4, _omitEnumNames ? '' : 'ASSIGNMENT_MODE_RETIRED');

  static const $core.List<AssignmentMode> values = <AssignmentMode>[
    ASSIGNMENT_MODE_UNSPECIFIED,
    ASSIGNMENT_MODE_TRIAL,
    ASSIGNMENT_MODE_STABLE,
    ASSIGNMENT_MODE_SUSPENDED,
    ASSIGNMENT_MODE_RETIRED,
  ];

  static final $core.List<AssignmentMode?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 4);
  static AssignmentMode? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const AssignmentMode._(super.value, super.name);
}

class ApplyStatus extends $pb.ProtobufEnum {
  static const ApplyStatus APPLY_STATUS_UNSPECIFIED =
      ApplyStatus._(0, _omitEnumNames ? '' : 'APPLY_STATUS_UNSPECIFIED');
  static const ApplyStatus APPLY_STATUS_PENDING =
      ApplyStatus._(1, _omitEnumNames ? '' : 'APPLY_STATUS_PENDING');
  static const ApplyStatus APPLY_STATUS_VERIFIED =
      ApplyStatus._(2, _omitEnumNames ? '' : 'APPLY_STATUS_VERIFIED');
  static const ApplyStatus APPLY_STATUS_REMOVED =
      ApplyStatus._(3, _omitEnumNames ? '' : 'APPLY_STATUS_REMOVED');
  static const ApplyStatus APPLY_STATUS_EXPIRED =
      ApplyStatus._(4, _omitEnumNames ? '' : 'APPLY_STATUS_EXPIRED');
  static const ApplyStatus APPLY_STATUS_FAILED =
      ApplyStatus._(5, _omitEnumNames ? '' : 'APPLY_STATUS_FAILED');

  static const $core.List<ApplyStatus> values = <ApplyStatus>[
    APPLY_STATUS_UNSPECIFIED,
    APPLY_STATUS_PENDING,
    APPLY_STATUS_VERIFIED,
    APPLY_STATUS_REMOVED,
    APPLY_STATUS_EXPIRED,
    APPLY_STATUS_FAILED,
  ];

  static final $core.List<ApplyStatus?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 5);
  static ApplyStatus? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ApplyStatus._(super.value, super.name);
}

class ExperimentStatus extends $pb.ProtobufEnum {
  static const ExperimentStatus EXPERIMENT_STATUS_UNSPECIFIED =
      ExperimentStatus._(
          0, _omitEnumNames ? '' : 'EXPERIMENT_STATUS_UNSPECIFIED');
  static const ExperimentStatus EXPERIMENT_STATUS_PROPOSED =
      ExperimentStatus._(1, _omitEnumNames ? '' : 'EXPERIMENT_STATUS_PROPOSED');
  static const ExperimentStatus EXPERIMENT_STATUS_RUNNING =
      ExperimentStatus._(2, _omitEnumNames ? '' : 'EXPERIMENT_STATUS_RUNNING');
  static const ExperimentStatus EXPERIMENT_STATUS_PAUSED =
      ExperimentStatus._(3, _omitEnumNames ? '' : 'EXPERIMENT_STATUS_PAUSED');
  static const ExperimentStatus EXPERIMENT_STATUS_SUSPENDED =
      ExperimentStatus._(
          4, _omitEnumNames ? '' : 'EXPERIMENT_STATUS_SUSPENDED');
  static const ExperimentStatus EXPERIMENT_STATUS_COMPLETE =
      ExperimentStatus._(5, _omitEnumNames ? '' : 'EXPERIMENT_STATUS_COMPLETE');
  static const ExperimentStatus EXPERIMENT_STATUS_RETIRED =
      ExperimentStatus._(6, _omitEnumNames ? '' : 'EXPERIMENT_STATUS_RETIRED');
  static const ExperimentStatus EXPERIMENT_STATUS_ROLLED_BACK =
      ExperimentStatus._(
          7, _omitEnumNames ? '' : 'EXPERIMENT_STATUS_ROLLED_BACK');

  static const $core.List<ExperimentStatus> values = <ExperimentStatus>[
    EXPERIMENT_STATUS_UNSPECIFIED,
    EXPERIMENT_STATUS_PROPOSED,
    EXPERIMENT_STATUS_RUNNING,
    EXPERIMENT_STATUS_PAUSED,
    EXPERIMENT_STATUS_SUSPENDED,
    EXPERIMENT_STATUS_COMPLETE,
    EXPERIMENT_STATUS_RETIRED,
    EXPERIMENT_STATUS_ROLLED_BACK,
  ];

  static final $core.List<ExperimentStatus?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 7);
  static ExperimentStatus? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ExperimentStatus._(super.value, super.name);
}

const $core.bool _omitEnumNames =
    $core.bool.fromEnvironment('protobuf.omit_enum_names');
