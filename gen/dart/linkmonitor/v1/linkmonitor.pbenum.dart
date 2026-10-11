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

import 'package:protobuf/protobuf.dart' as $pb;

/// Eligibility mirrors linkmonitor.Eligibility. UNSPECIFIED means unknown.
class Eligibility extends $pb.ProtobufEnum {
  static const Eligibility ELIGIBILITY_UNSPECIFIED =
      Eligibility._(0, _omitEnumNames ? '' : 'ELIGIBILITY_UNSPECIFIED');
  static const Eligibility ELIGIBILITY_INCLUDED =
      Eligibility._(1, _omitEnumNames ? '' : 'ELIGIBILITY_INCLUDED');
  static const Eligibility ELIGIBILITY_EXCLUDED =
      Eligibility._(2, _omitEnumNames ? '' : 'ELIGIBILITY_EXCLUDED');

  static const $core.List<Eligibility> values = <Eligibility>[
    ELIGIBILITY_UNSPECIFIED,
    ELIGIBILITY_INCLUDED,
    ELIGIBILITY_EXCLUDED,
  ];

  static final $core.List<Eligibility?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static Eligibility? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const Eligibility._(super.value, super.name);
}

/// CheckStatus mirrors linkmonitor.CheckStatus. UNSPECIFIED means unknown.
class CheckStatus extends $pb.ProtobufEnum {
  static const CheckStatus CHECK_STATUS_UNSPECIFIED =
      CheckStatus._(0, _omitEnumNames ? '' : 'CHECK_STATUS_UNSPECIFIED');
  static const CheckStatus CHECK_STATUS_PASS =
      CheckStatus._(1, _omitEnumNames ? '' : 'CHECK_STATUS_PASS');
  static const CheckStatus CHECK_STATUS_FAIL =
      CheckStatus._(2, _omitEnumNames ? '' : 'CHECK_STATUS_FAIL');
  static const CheckStatus CHECK_STATUS_NOT_APPLICABLE =
      CheckStatus._(3, _omitEnumNames ? '' : 'CHECK_STATUS_NOT_APPLICABLE');

  static const $core.List<CheckStatus> values = <CheckStatus>[
    CHECK_STATUS_UNSPECIFIED,
    CHECK_STATUS_PASS,
    CHECK_STATUS_FAIL,
    CHECK_STATUS_NOT_APPLICABLE,
  ];

  static final $core.List<CheckStatus?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static CheckStatus? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const CheckStatus._(super.value, super.name);
}

/// Support mirrors CollectorView.Support. UNSPECIFIED means unknown.
class Support extends $pb.ProtobufEnum {
  static const Support SUPPORT_UNSPECIFIED =
      Support._(0, _omitEnumNames ? '' : 'SUPPORT_UNSPECIFIED');
  static const Support SUPPORT_SUPPORTED =
      Support._(1, _omitEnumNames ? '' : 'SUPPORT_SUPPORTED');
  static const Support SUPPORT_UNSUPPORTED =
      Support._(2, _omitEnumNames ? '' : 'SUPPORT_UNSUPPORTED');
  static const Support SUPPORT_NOT_APPLICABLE =
      Support._(3, _omitEnumNames ? '' : 'SUPPORT_NOT_APPLICABLE');

  static const $core.List<Support> values = <Support>[
    SUPPORT_UNSPECIFIED,
    SUPPORT_SUPPORTED,
    SUPPORT_UNSUPPORTED,
    SUPPORT_NOT_APPLICABLE,
  ];

  static final $core.List<Support?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static Support? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const Support._(super.value, super.name);
}

const $core.bool _omitEnumNames =
    $core.bool.fromEnvironment('protobuf.omit_enum_names');
