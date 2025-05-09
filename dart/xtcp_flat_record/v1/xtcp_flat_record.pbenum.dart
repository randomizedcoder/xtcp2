//
//  Generated code. Do not modify.
//  source: xtcp_flat_record/v1/xtcp_flat_record.proto
//
// @dart = 2.12

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names, library_prefixes
// ignore_for_file: non_constant_identifier_names, prefer_final_fields
// ignore_for_file: unnecessary_import, unnecessary_this, unused_import

import 'dart:core' as $core;

import 'package:protobuf/protobuf.dart' as $pb;

class Envelope_XtcpFlatRecord_CongestionAlgorithm extends $pb.ProtobufEnum {
  static const Envelope_XtcpFlatRecord_CongestionAlgorithm CONGESTION_ALGORITHM_UNSPECIFIED = Envelope_XtcpFlatRecord_CongestionAlgorithm._(0, _omitEnumNames ? '' : 'CONGESTION_ALGORITHM_UNSPECIFIED');
  static const Envelope_XtcpFlatRecord_CongestionAlgorithm CONGESTION_ALGORITHM_CUBIC = Envelope_XtcpFlatRecord_CongestionAlgorithm._(1, _omitEnumNames ? '' : 'CONGESTION_ALGORITHM_CUBIC');
  static const Envelope_XtcpFlatRecord_CongestionAlgorithm CONGESTION_ALGORITHM_DCTCP = Envelope_XtcpFlatRecord_CongestionAlgorithm._(2, _omitEnumNames ? '' : 'CONGESTION_ALGORITHM_DCTCP');
  static const Envelope_XtcpFlatRecord_CongestionAlgorithm CONGESTION_ALGORITHM_VEGAS = Envelope_XtcpFlatRecord_CongestionAlgorithm._(3, _omitEnumNames ? '' : 'CONGESTION_ALGORITHM_VEGAS');
  static const Envelope_XtcpFlatRecord_CongestionAlgorithm CONGESTION_ALGORITHM_PRAGUE = Envelope_XtcpFlatRecord_CongestionAlgorithm._(4, _omitEnumNames ? '' : 'CONGESTION_ALGORITHM_PRAGUE');
  static const Envelope_XtcpFlatRecord_CongestionAlgorithm CONGESTION_ALGORITHM_BBR1 = Envelope_XtcpFlatRecord_CongestionAlgorithm._(5, _omitEnumNames ? '' : 'CONGESTION_ALGORITHM_BBR1');
  static const Envelope_XtcpFlatRecord_CongestionAlgorithm CONGESTION_ALGORITHM_BBR2 = Envelope_XtcpFlatRecord_CongestionAlgorithm._(6, _omitEnumNames ? '' : 'CONGESTION_ALGORITHM_BBR2');
  static const Envelope_XtcpFlatRecord_CongestionAlgorithm CONGESTION_ALGORITHM_BBR3 = Envelope_XtcpFlatRecord_CongestionAlgorithm._(7, _omitEnumNames ? '' : 'CONGESTION_ALGORITHM_BBR3');

  static const $core.List<Envelope_XtcpFlatRecord_CongestionAlgorithm> values = <Envelope_XtcpFlatRecord_CongestionAlgorithm> [
    CONGESTION_ALGORITHM_UNSPECIFIED,
    CONGESTION_ALGORITHM_CUBIC,
    CONGESTION_ALGORITHM_DCTCP,
    CONGESTION_ALGORITHM_VEGAS,
    CONGESTION_ALGORITHM_PRAGUE,
    CONGESTION_ALGORITHM_BBR1,
    CONGESTION_ALGORITHM_BBR2,
    CONGESTION_ALGORITHM_BBR3,
  ];

  static final $core.Map<$core.int, Envelope_XtcpFlatRecord_CongestionAlgorithm> _byValue = $pb.ProtobufEnum.initByValue(values);
  static Envelope_XtcpFlatRecord_CongestionAlgorithm? valueOf($core.int value) => _byValue[value];

  const Envelope_XtcpFlatRecord_CongestionAlgorithm._($core.int v, $core.String n) : super(v, n);
}

class XtcpFlatRecord_CongestionAlgorithm extends $pb.ProtobufEnum {
  static const XtcpFlatRecord_CongestionAlgorithm CONGESTION_ALGORITHM_UNSPECIFIED = XtcpFlatRecord_CongestionAlgorithm._(0, _omitEnumNames ? '' : 'CONGESTION_ALGORITHM_UNSPECIFIED');
  static const XtcpFlatRecord_CongestionAlgorithm CONGESTION_ALGORITHM_CUBIC = XtcpFlatRecord_CongestionAlgorithm._(1, _omitEnumNames ? '' : 'CONGESTION_ALGORITHM_CUBIC');
  static const XtcpFlatRecord_CongestionAlgorithm CONGESTION_ALGORITHM_DCTCP = XtcpFlatRecord_CongestionAlgorithm._(2, _omitEnumNames ? '' : 'CONGESTION_ALGORITHM_DCTCP');
  static const XtcpFlatRecord_CongestionAlgorithm CONGESTION_ALGORITHM_VEGAS = XtcpFlatRecord_CongestionAlgorithm._(3, _omitEnumNames ? '' : 'CONGESTION_ALGORITHM_VEGAS');
  static const XtcpFlatRecord_CongestionAlgorithm CONGESTION_ALGORITHM_PRAGUE = XtcpFlatRecord_CongestionAlgorithm._(4, _omitEnumNames ? '' : 'CONGESTION_ALGORITHM_PRAGUE');
  static const XtcpFlatRecord_CongestionAlgorithm CONGESTION_ALGORITHM_BBR1 = XtcpFlatRecord_CongestionAlgorithm._(5, _omitEnumNames ? '' : 'CONGESTION_ALGORITHM_BBR1');
  static const XtcpFlatRecord_CongestionAlgorithm CONGESTION_ALGORITHM_BBR2 = XtcpFlatRecord_CongestionAlgorithm._(6, _omitEnumNames ? '' : 'CONGESTION_ALGORITHM_BBR2');
  static const XtcpFlatRecord_CongestionAlgorithm CONGESTION_ALGORITHM_BBR3 = XtcpFlatRecord_CongestionAlgorithm._(7, _omitEnumNames ? '' : 'CONGESTION_ALGORITHM_BBR3');

  static const $core.List<XtcpFlatRecord_CongestionAlgorithm> values = <XtcpFlatRecord_CongestionAlgorithm> [
    CONGESTION_ALGORITHM_UNSPECIFIED,
    CONGESTION_ALGORITHM_CUBIC,
    CONGESTION_ALGORITHM_DCTCP,
    CONGESTION_ALGORITHM_VEGAS,
    CONGESTION_ALGORITHM_PRAGUE,
    CONGESTION_ALGORITHM_BBR1,
    CONGESTION_ALGORITHM_BBR2,
    CONGESTION_ALGORITHM_BBR3,
  ];

  static final $core.Map<$core.int, XtcpFlatRecord_CongestionAlgorithm> _byValue = $pb.ProtobufEnum.initByValue(values);
  static XtcpFlatRecord_CongestionAlgorithm? valueOf($core.int value) => _byValue[value];

  const XtcpFlatRecord_CongestionAlgorithm._($core.int v, $core.String n) : super(v, n);
}


const _omitEnumNames = $core.bool.fromEnvironment('protobuf.omit_enum_names');
