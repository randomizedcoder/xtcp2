// This is a generated file - do not edit.
//
// Generated from xtcp_auth/v1/xtcp_auth.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, prefer_relative_imports

import 'dart:core' as $core;

import 'package:protobuf/protobuf.dart' as $pb;

/// Access is the credential class an RPC requires.
class Access extends $pb.ProtobufEnum {
  /// No declaration. Treated as ACCESS_ADMIN so that a forgotten annotation
  /// fails closed.
  static const Access ACCESS_UNSPECIFIED =
      Access._(0, _omitEnumNames ? '' : 'ACCESS_UNSPECIFIED');

  /// Reads state only. Safe for monitoring and scrape credentials.
  static const Access ACCESS_READ =
      Access._(1, _omitEnumNames ? '' : 'ACCESS_READ');

  /// Changes state, drives work or exposes sensitive detail.
  static const Access ACCESS_ADMIN =
      Access._(2, _omitEnumNames ? '' : 'ACCESS_ADMIN');

  static const $core.List<Access> values = <Access>[
    ACCESS_UNSPECIFIED,
    ACCESS_READ,
    ACCESS_ADMIN,
  ];

  static final $core.List<Access?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static Access? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const Access._(super.value, super.name);
}

const $core.bool _omitEnumNames =
    $core.bool.fromEnvironment('protobuf.omit_enum_names');
