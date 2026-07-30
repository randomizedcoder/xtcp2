// This is a generated file - do not edit.
//
// Generated from xtcp_flat_record/v1/xtcp_flat_record.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, prefer_relative_imports

import 'dart:core' as $core;

import 'package:protobuf/protobuf.dart' as $pb;

class XtcpFlatRecord_CongestionAlgorithm extends $pb.ProtobufEnum {
  static const XtcpFlatRecord_CongestionAlgorithm
      CONGESTION_ALGORITHM_UNSPECIFIED = XtcpFlatRecord_CongestionAlgorithm._(
          0, _omitEnumNames ? '' : 'CONGESTION_ALGORITHM_UNSPECIFIED');
  static const XtcpFlatRecord_CongestionAlgorithm CONGESTION_ALGORITHM_CUBIC =
      XtcpFlatRecord_CongestionAlgorithm._(
          1, _omitEnumNames ? '' : 'CONGESTION_ALGORITHM_CUBIC');
  static const XtcpFlatRecord_CongestionAlgorithm CONGESTION_ALGORITHM_DCTCP =
      XtcpFlatRecord_CongestionAlgorithm._(
          2, _omitEnumNames ? '' : 'CONGESTION_ALGORITHM_DCTCP');
  static const XtcpFlatRecord_CongestionAlgorithm CONGESTION_ALGORITHM_VEGAS =
      XtcpFlatRecord_CongestionAlgorithm._(
          3, _omitEnumNames ? '' : 'CONGESTION_ALGORITHM_VEGAS');
  static const XtcpFlatRecord_CongestionAlgorithm CONGESTION_ALGORITHM_PRAGUE =
      XtcpFlatRecord_CongestionAlgorithm._(
          4, _omitEnumNames ? '' : 'CONGESTION_ALGORITHM_PRAGUE');
  static const XtcpFlatRecord_CongestionAlgorithm CONGESTION_ALGORITHM_BBR1 =
      XtcpFlatRecord_CongestionAlgorithm._(
          5, _omitEnumNames ? '' : 'CONGESTION_ALGORITHM_BBR1');
  static const XtcpFlatRecord_CongestionAlgorithm CONGESTION_ALGORITHM_BBR2 =
      XtcpFlatRecord_CongestionAlgorithm._(
          6, _omitEnumNames ? '' : 'CONGESTION_ALGORITHM_BBR2');
  static const XtcpFlatRecord_CongestionAlgorithm CONGESTION_ALGORITHM_BBR3 =
      XtcpFlatRecord_CongestionAlgorithm._(
          7, _omitEnumNames ? '' : 'CONGESTION_ALGORITHM_BBR3');

  static const $core.List<XtcpFlatRecord_CongestionAlgorithm> values =
      <XtcpFlatRecord_CongestionAlgorithm>[
    CONGESTION_ALGORITHM_UNSPECIFIED,
    CONGESTION_ALGORITHM_CUBIC,
    CONGESTION_ALGORITHM_DCTCP,
    CONGESTION_ALGORITHM_VEGAS,
    CONGESTION_ALGORITHM_PRAGUE,
    CONGESTION_ALGORITHM_BBR1,
    CONGESTION_ALGORITHM_BBR2,
    CONGESTION_ALGORITHM_BBR3,
  ];

  static final $core.List<XtcpFlatRecord_CongestionAlgorithm?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 7);
  static XtcpFlatRecord_CongestionAlgorithm? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const XtcpFlatRecord_CongestionAlgorithm._(super.value, super.name);
}

const $core.bool _omitEnumNames =
    $core.bool.fromEnvironment('protobuf.omit_enum_names');
