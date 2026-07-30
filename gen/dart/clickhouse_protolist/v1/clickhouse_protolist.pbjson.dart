// This is a generated file - do not edit.
//
// Generated from clickhouse_protolist/v1/clickhouse_protolist.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, prefer_relative_imports
// ignore_for_file: unused_import

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
final $typed_data.Uint8List recordDescriptor = $convert
    .base64Decode('CgZSZWNvcmQSGwoJbXlfdWludDMyGAEgASgNUghteVVpbnQzMg==');

@$core.Deprecated('Use envelopeDescriptor instead')
const Envelope$json = {
  '1': 'Envelope',
  '2': [
    {
      '1': 'rows',
      '3': 1,
      '4': 3,
      '5': 11,
      '6': '.clickhouse_protolist.v1.Envelope.Record',
      '10': 'rows'
    },
  ],
  '3': [Envelope_Record$json],
};

@$core.Deprecated('Use envelopeDescriptor instead')
const Envelope_Record$json = {
  '1': 'Record',
  '2': [
    {'1': 'my_uint32', '3': 1, '4': 1, '5': 13, '10': 'myUint32'},
  ],
};

/// Descriptor for `Envelope`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List envelopeDescriptor = $convert.base64Decode(
    'CghFbnZlbG9wZRI8CgRyb3dzGAEgAygLMiguY2xpY2tob3VzZV9wcm90b2xpc3QudjEuRW52ZW'
    'xvcGUuUmVjb3JkUgRyb3dzGiUKBlJlY29yZBIbCglteV91aW50MzIYASABKA1SCG15VWludDMy');
