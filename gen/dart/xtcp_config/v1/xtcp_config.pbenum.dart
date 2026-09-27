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

import 'package:protobuf/protobuf.dart' as $pb;

class ListenerNetwork extends $pb.ProtobufEnum {
  static const ListenerNetwork LISTENER_NETWORK_UNSPECIFIED = ListenerNetwork._(
      0, _omitEnumNames ? '' : 'LISTENER_NETWORK_UNSPECIFIED');
  static const ListenerNetwork LISTENER_NETWORK_TCP =
      ListenerNetwork._(1, _omitEnumNames ? '' : 'LISTENER_NETWORK_TCP');
  static const ListenerNetwork LISTENER_NETWORK_UNIX =
      ListenerNetwork._(2, _omitEnumNames ? '' : 'LISTENER_NETWORK_UNIX');

  static const $core.List<ListenerNetwork> values = <ListenerNetwork>[
    LISTENER_NETWORK_UNSPECIFIED,
    LISTENER_NETWORK_TCP,
    LISTENER_NETWORK_UNIX,
  ];

  static final $core.List<ListenerNetwork?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static ListenerNetwork? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ListenerNetwork._(super.value, super.name);
}

class ListenerAuthMode extends $pb.ProtobufEnum {
  static const ListenerAuthMode LISTENER_AUTH_MODE_UNSPECIFIED =
      ListenerAuthMode._(
          0, _omitEnumNames ? '' : 'LISTENER_AUTH_MODE_UNSPECIFIED');
  static const ListenerAuthMode LISTENER_AUTH_MODE_DISABLED =
      ListenerAuthMode._(
          1, _omitEnumNames ? '' : 'LISTENER_AUTH_MODE_DISABLED');
  static const ListenerAuthMode LISTENER_AUTH_MODE_RAW_TOKEN =
      ListenerAuthMode._(
          2, _omitEnumNames ? '' : 'LISTENER_AUTH_MODE_RAW_TOKEN');
  static const ListenerAuthMode LISTENER_AUTH_MODE_HMAC_UTC_MINUTE =
      ListenerAuthMode._(
          3, _omitEnumNames ? '' : 'LISTENER_AUTH_MODE_HMAC_UTC_MINUTE');

  static const $core.List<ListenerAuthMode> values = <ListenerAuthMode>[
    LISTENER_AUTH_MODE_UNSPECIFIED,
    LISTENER_AUTH_MODE_DISABLED,
    LISTENER_AUTH_MODE_RAW_TOKEN,
    LISTENER_AUTH_MODE_HMAC_UTC_MINUTE,
  ];

  static final $core.List<ListenerAuthMode?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static ListenerAuthMode? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ListenerAuthMode._(super.value, super.name);
}

const $core.bool _omitEnumNames =
    $core.bool.fromEnvironment('protobuf.omit_enum_names');
