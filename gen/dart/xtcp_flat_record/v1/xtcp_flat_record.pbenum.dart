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

/// ---- enrichment: destination-side (310-349) ------------------------------
/// Destination endpoint locality, classified from the socket's own network
/// namespace's local addresses + routing table (discovered via rtnetlink,
/// see pkg/localnet). Computed BEFORE the ASN lookup: SELF and LOCAL_SUBNET
/// destinations never reach the ASN feed, so enrich_socket_dest_asn (320) /
/// enrich_socket_dest_network_owner (322) stay empty for them. UNSPECIFIED
/// when locality enrichment is disabled or the namespace has no snapshot yet.
class XtcpFlatRecord_Locality extends $pb.ProtobufEnum {
  static const XtcpFlatRecord_Locality LOCALITY_UNSPECIFIED =
      XtcpFlatRecord_Locality._(
          0, _omitEnumNames ? '' : 'LOCALITY_UNSPECIFIED');
  static const XtcpFlatRecord_Locality LOCALITY_SELF =
      XtcpFlatRecord_Locality._(1, _omitEnumNames ? '' : 'LOCALITY_SELF');
  static const XtcpFlatRecord_Locality LOCALITY_LOCAL_SUBNET =
      XtcpFlatRecord_Locality._(
          2, _omitEnumNames ? '' : 'LOCALITY_LOCAL_SUBNET');
  static const XtcpFlatRecord_Locality LOCALITY_REMOTE =
      XtcpFlatRecord_Locality._(3, _omitEnumNames ? '' : 'LOCALITY_REMOTE');

  static const $core.List<XtcpFlatRecord_Locality> values =
      <XtcpFlatRecord_Locality>[
    LOCALITY_UNSPECIFIED,
    LOCALITY_SELF,
    LOCALITY_LOCAL_SUBNET,
    LOCALITY_REMOTE,
  ];

  static final $core.List<XtcpFlatRecord_Locality?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static XtcpFlatRecord_Locality? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const XtcpFlatRecord_Locality._(super.value, super.name);
}

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
