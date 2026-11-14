// This is a generated file - do not edit.
//
// Generated from xtcp_treatment/v1/agent.proto.

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

@$core.Deprecated('Use checkInRequestDescriptor instead')
const CheckInRequest$json = {
  '1': 'CheckInRequest',
  '2': [
    {'1': 'agent_id', '3': 1, '4': 1, '5': 9, '8': {}, '10': 'agentId'},
    {'1': 'location', '3': 2, '4': 1, '5': 9, '8': {}, '10': 'location'},
    {
      '1': 'capabilities',
      '3': 3,
      '4': 1,
      '5': 11,
      '6': '.xtcp_treatment.v1.Capabilities',
      '8': {},
      '10': 'capabilities'
    },
    {'1': 'desired_revision', '3': 4, '4': 1, '5': 4, '10': 'desiredRevision'},
    {
      '1': 'records',
      '3': 5,
      '4': 3,
      '5': 11,
      '6': '.xtcp_treatment.v1.ApplicationRecord',
      '8': {},
      '10': 'records'
    },
    {
      '1': 'locally_suspended',
      '3': 6,
      '4': 1,
      '5': 8,
      '10': 'locallySuspended'
    },
  ],
};

/// Descriptor for `CheckInRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List checkInRequestDescriptor = $convert.base64Decode(
    'Cg5DaGVja0luUmVxdWVzdBJECghhZ2VudF9pZBgBIAEoCUIpukgmciQyIl5bYS16QS1aMC05XV'
    'thLXpBLVowLTlfLi1dezAsMTI3fSRSB2FnZW50SWQSRQoIbG9jYXRpb24YAiABKAlCKbpIJnIk'
    'MiJeW2EtekEtWjAtOV1bYS16QS1aMC05Xy4tXXswLDEyN30kUghsb2NhdGlvbhJLCgxjYXBhYm'
    'lsaXRpZXMYAyABKAsyHy54dGNwX3RyZWF0bWVudC52MS5DYXBhYmlsaXRpZXNCBrpIA8gBAVIM'
    'Y2FwYWJpbGl0aWVzEikKEGRlc2lyZWRfcmV2aXNpb24YBCABKARSD2Rlc2lyZWRSZXZpc2lvbh'
    'JJCgdyZWNvcmRzGAUgAygLMiQueHRjcF90cmVhdG1lbnQudjEuQXBwbGljYXRpb25SZWNvcmRC'
    'CbpIBpIBAxCAQFIHcmVjb3JkcxIrChFsb2NhbGx5X3N1c3BlbmRlZBgGIAEoCFIQbG9jYWxseV'
    'N1c3BlbmRlZA==');

@$core.Deprecated('Use checkInResponseDescriptor instead')
const CheckInResponse$json = {
  '1': 'CheckInResponse',
  '2': [
    {
      '1': 'desired',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.xtcp_treatment.v1.DesiredState',
      '10': 'desired'
    },
  ],
};

/// Descriptor for `CheckInResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List checkInResponseDescriptor = $convert.base64Decode(
    'Cg9DaGVja0luUmVzcG9uc2USOQoHZGVzaXJlZBgBIAEoCzIfLnh0Y3BfdHJlYXRtZW50LnYxLk'
    'Rlc2lyZWRTdGF0ZVIHZGVzaXJlZA==');

@$core.Deprecated('Use getDesiredStateRequestDescriptor instead')
const GetDesiredStateRequest$json = {
  '1': 'GetDesiredStateRequest',
  '2': [
    {'1': 'agent_id', '3': 1, '4': 1, '5': 9, '8': {}, '10': 'agentId'},
  ],
};

/// Descriptor for `GetDesiredStateRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List getDesiredStateRequestDescriptor =
    $convert.base64Decode(
        'ChZHZXREZXNpcmVkU3RhdGVSZXF1ZXN0EkQKCGFnZW50X2lkGAEgASgJQim6SCZyJDIiXlthLX'
        'pBLVowLTldW2EtekEtWjAtOV8uLV17MCwxMjd9JFIHYWdlbnRJZA==');

@$core.Deprecated('Use getDesiredStateResponseDescriptor instead')
const GetDesiredStateResponse$json = {
  '1': 'GetDesiredStateResponse',
  '2': [
    {
      '1': 'desired',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.xtcp_treatment.v1.DesiredState',
      '10': 'desired'
    },
  ],
};

/// Descriptor for `GetDesiredStateResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List getDesiredStateResponseDescriptor =
    $convert.base64Decode(
        'ChdHZXREZXNpcmVkU3RhdGVSZXNwb25zZRI5CgdkZXNpcmVkGAEgASgLMh8ueHRjcF90cmVhdG'
        '1lbnQudjEuRGVzaXJlZFN0YXRlUgdkZXNpcmVk');

@$core.Deprecated('Use watchDesiredStateRequestDescriptor instead')
const WatchDesiredStateRequest$json = {
  '1': 'WatchDesiredStateRequest',
  '2': [
    {'1': 'agent_id', '3': 1, '4': 1, '5': 9, '8': {}, '10': 'agentId'},
    {'1': 'after_revision', '3': 2, '4': 1, '5': 4, '10': 'afterRevision'},
  ],
};

/// Descriptor for `WatchDesiredStateRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List watchDesiredStateRequestDescriptor = $convert.base64Decode(
    'ChhXYXRjaERlc2lyZWRTdGF0ZVJlcXVlc3QSRAoIYWdlbnRfaWQYASABKAlCKbpIJnIkMiJeW2'
    'EtekEtWjAtOV1bYS16QS1aMC05Xy4tXXswLDEyN30kUgdhZ2VudElkEiUKDmFmdGVyX3Jldmlz'
    'aW9uGAIgASgEUg1hZnRlclJldmlzaW9u');

@$core.Deprecated('Use watchDesiredStateResponseDescriptor instead')
const WatchDesiredStateResponse$json = {
  '1': 'WatchDesiredStateResponse',
  '2': [
    {'1': 'revision', '3': 1, '4': 1, '5': 4, '10': 'revision'},
  ],
};

/// Descriptor for `WatchDesiredStateResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List watchDesiredStateResponseDescriptor =
    $convert.base64Decode(
        'ChlXYXRjaERlc2lyZWRTdGF0ZVJlc3BvbnNlEhoKCHJldmlzaW9uGAEgASgEUghyZXZpc2lvbg'
        '==');
