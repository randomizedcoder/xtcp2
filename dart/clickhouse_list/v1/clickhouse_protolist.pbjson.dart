//
//  Generated code. Do not modify.
//  source: clickhouse_list/v1/clickhouse_protolist.proto
//
// @dart = 2.12

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names, library_prefixes
// ignore_for_file: non_constant_identifier_names, prefer_final_fields
// ignore_for_file: unnecessary_import, unnecessary_this, unused_import

import 'dart:convert' as $convert;
import 'dart:core' as $core;
import 'dart:typed_data' as $typed_data;

@$core.Deprecated('Use recordDescriptor instead')
const Record$json = {
  '1': 'Record',
  '2': [
    {'1': 'my_uint32', '3': 1, '4': 1, '5': 13, '10': 'myUint32'},
  ],
};

/// Descriptor for `Record`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List recordDescriptor = $convert.base64Decode(
    'CgZSZWNvcmQSGwoJbXlfdWludDMyGAEgASgNUghteVVpbnQzMg==');

@$core.Deprecated('Use envelopeDescriptor instead')
const Envelope$json = {
  '1': 'Envelope',
  '2': [
    {'1': 'row', '3': 1, '4': 3, '5': 11, '6': '.clickhouse_protolist.v1.Record', '10': 'row'},
  ],
};

/// Descriptor for `Envelope`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List envelopeDescriptor = $convert.base64Decode(
    'CghFbnZlbG9wZRIxCgNyb3cYASADKAsyHy5jbGlja2hvdXNlX3Byb3RvbGlzdC52MS5SZWNvcm'
    'RSA3Jvdw==');

