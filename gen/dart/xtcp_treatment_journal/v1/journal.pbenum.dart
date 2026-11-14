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

import 'package:protobuf/protobuf.dart' as $pb;

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

const $core.bool _omitEnumNames =
    $core.bool.fromEnvironment('protobuf.omit_enum_names');
