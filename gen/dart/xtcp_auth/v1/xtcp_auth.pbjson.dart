// This is a generated file - do not edit.
//
// Generated from xtcp_auth/v1/xtcp_auth.proto.

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

@$core.Deprecated('Use accessDescriptor instead')
const Access$json = {
  '1': 'Access',
  '2': [
    {'1': 'ACCESS_UNSPECIFIED', '2': 0},
    {'1': 'ACCESS_READ', '2': 1},
    {'1': 'ACCESS_ADMIN', '2': 2},
  ],
};

/// Descriptor for `Access`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List accessDescriptor = $convert.base64Decode(
    'CgZBY2Nlc3MSFgoSQUNDRVNTX1VOU1BFQ0lGSUVEEAASDwoLQUNDRVNTX1JFQUQQARIQCgxBQ0'
    'NFU1NfQURNSU4QAg==');
