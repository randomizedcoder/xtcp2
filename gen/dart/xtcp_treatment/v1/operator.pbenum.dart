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

import 'package:protobuf/protobuf.dart' as $pb;

class ControlAction extends $pb.ProtobufEnum {
  static const ControlAction CONTROL_ACTION_UNSPECIFIED =
      ControlAction._(0, _omitEnumNames ? '' : 'CONTROL_ACTION_UNSPECIFIED');
  static const ControlAction CONTROL_ACTION_PAUSE =
      ControlAction._(1, _omitEnumNames ? '' : 'CONTROL_ACTION_PAUSE');
  static const ControlAction CONTROL_ACTION_SUSPEND =
      ControlAction._(2, _omitEnumNames ? '' : 'CONTROL_ACTION_SUSPEND');
  static const ControlAction CONTROL_ACTION_RESUME =
      ControlAction._(3, _omitEnumNames ? '' : 'CONTROL_ACTION_RESUME');
  static const ControlAction CONTROL_ACTION_ROLLBACK =
      ControlAction._(4, _omitEnumNames ? '' : 'CONTROL_ACTION_ROLLBACK');
  static const ControlAction CONTROL_ACTION_RETIRE =
      ControlAction._(5, _omitEnumNames ? '' : 'CONTROL_ACTION_RETIRE');

  static const $core.List<ControlAction> values = <ControlAction>[
    CONTROL_ACTION_UNSPECIFIED,
    CONTROL_ACTION_PAUSE,
    CONTROL_ACTION_SUSPEND,
    CONTROL_ACTION_RESUME,
    CONTROL_ACTION_ROLLBACK,
    CONTROL_ACTION_RETIRE,
  ];

  static final $core.List<ControlAction?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 5);
  static ControlAction? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ControlAction._(super.value, super.name);
}

const $core.bool _omitEnumNames =
    $core.bool.fromEnvironment('protobuf.omit_enum_names');
