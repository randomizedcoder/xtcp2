// This is a generated file - do not edit.
//
// Generated from xtcp_treatment/v1/operator.proto.

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

@$core.Deprecated('Use controlActionDescriptor instead')
const ControlAction$json = {
  '1': 'ControlAction',
  '2': [
    {'1': 'CONTROL_ACTION_UNSPECIFIED', '2': 0},
    {'1': 'CONTROL_ACTION_PAUSE', '2': 1},
    {'1': 'CONTROL_ACTION_SUSPEND', '2': 2},
    {'1': 'CONTROL_ACTION_RESUME', '2': 3},
    {'1': 'CONTROL_ACTION_ROLLBACK', '2': 4},
    {'1': 'CONTROL_ACTION_RETIRE', '2': 5},
  ],
};

/// Descriptor for `ControlAction`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List controlActionDescriptor = $convert.base64Decode(
    'Cg1Db250cm9sQWN0aW9uEh4KGkNPTlRST0xfQUNUSU9OX1VOU1BFQ0lGSUVEEAASGAoUQ09OVF'
    'JPTF9BQ1RJT05fUEFVU0UQARIaChZDT05UUk9MX0FDVElPTl9TVVNQRU5EEAISGQoVQ09OVFJP'
    'TF9BQ1RJT05fUkVTVU1FEAMSGwoXQ09OVFJPTF9BQ1RJT05fUk9MTEJBQ0sQBBIZChVDT05UUk'
    '9MX0FDVElPTl9SRVRJUkUQBQ==');

@$core.Deprecated('Use proposeRequestDescriptor instead')
const ProposeRequest$json = {
  '1': 'ProposeRequest',
  '2': [
    {
      '1': 'experiment',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.xtcp_treatment.v1.Experiment',
      '8': {},
      '10': 'experiment'
    },
    {'1': 'request_id', '3': 2, '4': 1, '5': 9, '8': {}, '10': 'requestId'},
  ],
  '7': {},
};

/// Descriptor for `ProposeRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List proposeRequestDescriptor = $convert.base64Decode(
    'Cg5Qcm9wb3NlUmVxdWVzdBJFCgpleHBlcmltZW50GAEgASgLMh0ueHRjcF90cmVhdG1lbnQudj'
    'EuRXhwZXJpbWVudEIGukgDyAEBUgpleHBlcmltZW50EkgKCnJlcXVlc3RfaWQYAiABKAlCKbpI'
    'JnIkMiJeW2EtekEtWjAtOV1bYS16QS1aMC05Xy4tXXswLDEyN30kUglyZXF1ZXN0SWQ6lwG6SJ'
    'MBGpABChlwcm9wb3NlLnJlcXVlc3RfaWRfc3VmZml4Ek1yZXF1ZXN0X2lkIG11c3Qgbm90IGVu'
    'ZCB3aXRoIC5yZXN1bHQgKHJlc2VydmVkIGZvciB0aGUgcGFpcmVkIHJlc3BvbnNlIGV2ZW50KR'
    'okIXRoaXMucmVxdWVzdF9pZC5lbmRzV2l0aCgnLnJlc3VsdCcp');

@$core.Deprecated('Use proposeResponseDescriptor instead')
const ProposeResponse$json = {
  '1': 'ProposeResponse',
  '2': [
    {
      '1': 'experiment',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.xtcp_treatment.v1.Experiment',
      '10': 'experiment'
    },
  ],
};

/// Descriptor for `ProposeResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List proposeResponseDescriptor = $convert.base64Decode(
    'Cg9Qcm9wb3NlUmVzcG9uc2USPQoKZXhwZXJpbWVudBgBIAEoCzIdLnh0Y3BfdHJlYXRtZW50Ln'
    'YxLkV4cGVyaW1lbnRSCmV4cGVyaW1lbnQ=');

@$core.Deprecated('Use analyzeRequestDescriptor instead')
const AnalyzeRequest$json = {
  '1': 'AnalyzeRequest',
  '2': [
    {
      '1': 'experiment_id',
      '3': 1,
      '4': 1,
      '5': 9,
      '8': {},
      '10': 'experimentId'
    },
    {
      '1': 'expected_revision',
      '3': 2,
      '4': 1,
      '5': 4,
      '10': 'expectedRevision'
    },
  ],
};

/// Descriptor for `AnalyzeRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List analyzeRequestDescriptor = $convert.base64Decode(
    'Cg5BbmFseXplUmVxdWVzdBJOCg1leHBlcmltZW50X2lkGAEgASgJQim6SCZyJDIiXlthLXpBLV'
    'owLTldW2EtekEtWjAtOV8uLV17MCwxMjd9JFIMZXhwZXJpbWVudElkEisKEWV4cGVjdGVkX3Jl'
    'dmlzaW9uGAIgASgEUhBleHBlY3RlZFJldmlzaW9u');

@$core.Deprecated('Use analyzeResponseDescriptor instead')
const AnalyzeResponse$json = {
  '1': 'AnalyzeResponse',
  '2': [
    {
      '1': 'evidence',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.xtcp_treatment.v1.Evidence',
      '10': 'evidence'
    },
    {
      '1': 'outcome',
      '3': 2,
      '4': 1,
      '5': 14,
      '6': '.xtcp_treatment.v1.Outcome',
      '10': 'outcome'
    },
  ],
};

/// Descriptor for `AnalyzeResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List analyzeResponseDescriptor = $convert.base64Decode(
    'Cg9BbmFseXplUmVzcG9uc2USNwoIZXZpZGVuY2UYASABKAsyGy54dGNwX3RyZWF0bWVudC52MS'
    '5FdmlkZW5jZVIIZXZpZGVuY2USNAoHb3V0Y29tZRgCIAEoDjIaLnh0Y3BfdHJlYXRtZW50LnYx'
    'Lk91dGNvbWVSB291dGNvbWU=');

@$core.Deprecated('Use reviewRequestDescriptor instead')
const ReviewRequest$json = {
  '1': 'ReviewRequest',
  '2': [
    {
      '1': 'experiment_id',
      '3': 1,
      '4': 1,
      '5': 9,
      '8': {},
      '10': 'experimentId'
    },
    {
      '1': 'expected_revision',
      '3': 2,
      '4': 1,
      '5': 4,
      '10': 'expectedRevision'
    },
    {'1': 'approve', '3': 3, '4': 1, '5': 8, '10': 'approve'},
    {'1': 'reason', '3': 4, '4': 1, '5': 9, '8': {}, '10': 'reason'},
    {'1': 'request_id', '3': 5, '4': 1, '5': 9, '8': {}, '10': 'requestId'},
    {
      '1': 'evidence',
      '3': 6,
      '4': 1,
      '5': 11,
      '6': '.xtcp_treatment.v1.Evidence',
      '10': 'evidence'
    },
  ],
  '7': {},
};

/// Descriptor for `ReviewRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List reviewRequestDescriptor = $convert.base64Decode(
    'Cg1SZXZpZXdSZXF1ZXN0Ek4KDWV4cGVyaW1lbnRfaWQYASABKAlCKbpIJnIkMiJeW2EtekEtWj'
    'AtOV1bYS16QS1aMC05Xy4tXXswLDEyN30kUgxleHBlcmltZW50SWQSKwoRZXhwZWN0ZWRfcmV2'
    'aXNpb24YAiABKARSEGV4cGVjdGVkUmV2aXNpb24SGAoHYXBwcm92ZRgDIAEoCFIHYXBwcm92ZR'
    'IiCgZyZWFzb24YBCABKAlCCrpIB3IFEAEYgCBSBnJlYXNvbhJICgpyZXF1ZXN0X2lkGAUgASgJ'
    'Qim6SCZyJDIiXlthLXpBLVowLTldW2EtekEtWjAtOV8uLV17MCwxMjd9JFIJcmVxdWVzdElkEj'
    'cKCGV2aWRlbmNlGAYgASgLMhsueHRjcF90cmVhdG1lbnQudjEuRXZpZGVuY2VSCGV2aWRlbmNl'
    'OpYBukiSARqPAQoYcmV2aWV3LnJlcXVlc3RfaWRfc3VmZml4Ek1yZXF1ZXN0X2lkIG11c3Qgbm'
    '90IGVuZCB3aXRoIC5yZXN1bHQgKHJlc2VydmVkIGZvciB0aGUgcGFpcmVkIHJlc3BvbnNlIGV2'
    'ZW50KRokIXRoaXMucmVxdWVzdF9pZC5lbmRzV2l0aCgnLnJlc3VsdCcp');

@$core.Deprecated('Use reviewResponseDescriptor instead')
const ReviewResponse$json = {
  '1': 'ReviewResponse',
  '2': [
    {
      '1': 'experiment',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.xtcp_treatment.v1.Experiment',
      '10': 'experiment'
    },
  ],
};

/// Descriptor for `ReviewResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List reviewResponseDescriptor = $convert.base64Decode(
    'Cg5SZXZpZXdSZXNwb25zZRI9CgpleHBlcmltZW50GAEgASgLMh0ueHRjcF90cmVhdG1lbnQudj'
    'EuRXhwZXJpbWVudFIKZXhwZXJpbWVudA==');

@$core.Deprecated('Use controlRequestDescriptor instead')
const ControlRequest$json = {
  '1': 'ControlRequest',
  '2': [
    {
      '1': 'experiment_id',
      '3': 1,
      '4': 1,
      '5': 9,
      '8': {},
      '10': 'experimentId'
    },
    {'1': 'agent_id', '3': 2, '4': 1, '5': 9, '8': {}, '10': 'agentId'},
    {'1': 'location', '3': 3, '4': 1, '5': 9, '8': {}, '10': 'location'},
    {'1': 'fleet', '3': 4, '4': 1, '5': 8, '10': 'fleet'},
    {
      '1': 'action',
      '3': 5,
      '4': 1,
      '5': 14,
      '6': '.xtcp_treatment.v1.ControlAction',
      '8': {},
      '10': 'action'
    },
    {'1': 'reason', '3': 6, '4': 1, '5': 9, '8': {}, '10': 'reason'},
    {'1': 'request_id', '3': 7, '4': 1, '5': 9, '8': {}, '10': 'requestId'},
  ],
  '7': {},
};

/// Descriptor for `ControlRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List controlRequestDescriptor = $convert.base64Decode(
    'Cg5Db250cm9sUmVxdWVzdBJRCg1leHBlcmltZW50X2lkGAEgASgJQiy6SCnYAQFyJDIiXlthLX'
    'pBLVowLTldW2EtekEtWjAtOV8uLV17MCwxMjd9JFIMZXhwZXJpbWVudElkEkcKCGFnZW50X2lk'
    'GAIgASgJQiy6SCnYAQFyJDIiXlthLXpBLVowLTldW2EtekEtWjAtOV8uLV17MCwxMjd9JFIHYW'
    'dlbnRJZBJICghsb2NhdGlvbhgDIAEoCUIsukgp2AEBciQyIl5bYS16QS1aMC05XVthLXpBLVow'
    'LTlfLi1dezAsMTI3fSRSCGxvY2F0aW9uEhQKBWZsZWV0GAQgASgIUgVmbGVldBJECgZhY3Rpb2'
    '4YBSABKA4yIC54dGNwX3RyZWF0bWVudC52MS5Db250cm9sQWN0aW9uQgq6SAeCAQQQASAAUgZh'
    'Y3Rpb24SIgoGcmVhc29uGAYgASgJQgq6SAdyBRABGIAgUgZyZWFzb24SSAoKcmVxdWVzdF9pZB'
    'gHIAEoCUIpukgmciQyIl5bYS16QS1aMC05XVthLXpBLVowLTlfLi1dezAsMTI3fSRSCXJlcXVl'
    'c3RJZDr3ArpI8wIakAEKGWNvbnRyb2wucmVxdWVzdF9pZF9zdWZmaXgSTXJlcXVlc3RfaWQgbX'
    'VzdCBub3QgZW5kIHdpdGggLnJlc3VsdCAocmVzZXJ2ZWQgZm9yIHRoZSBwYWlyZWQgcmVzcG9u'
    'c2UgZXZlbnQpGiQhdGhpcy5yZXF1ZXN0X2lkLmVuZHNXaXRoKCcucmVzdWx0Jyka3QEKFGNvbn'
    'Ryb2wub25lX3NlbGVjdG9yEkVleGFjdGx5IG9uZSBvZiBleHBlcmltZW50X2lkLCBhZ2VudF9p'
    'ZCwgbG9jYXRpb24gb3IgZmxlZXQgaXMgcmVxdWlyZWQafih0aGlzLmZsZWV0ID8gMSA6IDApIC'
    'sgKHRoaXMuZXhwZXJpbWVudF9pZCAhPSAnJyA/IDEgOiAwKSArICh0aGlzLmFnZW50X2lkICE9'
    'ICcnID8gMSA6IDApICsgKHRoaXMubG9jYXRpb24gIT0gJycgPyAxIDogMCkgPT0gMQ==');

@$core.Deprecated('Use controlResponseDescriptor instead')
const ControlResponse$json = {
  '1': 'ControlResponse',
  '2': [
    {'1': 'affected_agents', '3': 1, '4': 1, '5': 13, '10': 'affectedAgents'},
  ],
};

/// Descriptor for `ControlResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List controlResponseDescriptor = $convert.base64Decode(
    'Cg9Db250cm9sUmVzcG9uc2USJwoPYWZmZWN0ZWRfYWdlbnRzGAEgASgNUg5hZmZlY3RlZEFnZW'
    '50cw==');

@$core.Deprecated('Use inspectRequestDescriptor instead')
const InspectRequest$json = {
  '1': 'InspectRequest',
  '2': [
    {
      '1': 'experiment_id',
      '3': 1,
      '4': 1,
      '5': 9,
      '8': {},
      '10': 'experimentId'
    },
    {'1': 'agent_id', '3': 2, '4': 1, '5': 9, '8': {}, '10': 'agentId'},
  ],
};

/// Descriptor for `InspectRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List inspectRequestDescriptor = $convert.base64Decode(
    'Cg5JbnNwZWN0UmVxdWVzdBJRCg1leHBlcmltZW50X2lkGAEgASgJQiy6SCnYAQFyJDIiXlthLX'
    'pBLVowLTldW2EtekEtWjAtOV8uLV17MCwxMjd9JFIMZXhwZXJpbWVudElkEkcKCGFnZW50X2lk'
    'GAIgASgJQiy6SCnYAQFyJDIiXlthLXpBLVowLTldW2EtekEtWjAtOV8uLV17MCwxMjd9JFIHYW'
    'dlbnRJZA==');

@$core.Deprecated('Use inspectResponseDescriptor instead')
const InspectResponse$json = {
  '1': 'InspectResponse',
  '2': [
    {
      '1': 'experiments',
      '3': 1,
      '4': 3,
      '5': 11,
      '6': '.xtcp_treatment.v1.Experiment',
      '10': 'experiments'
    },
    {
      '1': 'desired',
      '3': 2,
      '4': 3,
      '5': 11,
      '6': '.xtcp_treatment.v1.DesiredState',
      '10': 'desired'
    },
    {
      '1': 'observed',
      '3': 3,
      '4': 3,
      '5': 11,
      '6': '.xtcp_treatment.v1.CheckInRequest',
      '10': 'observed'
    },
  ],
};

/// Descriptor for `InspectResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List inspectResponseDescriptor = $convert.base64Decode(
    'Cg9JbnNwZWN0UmVzcG9uc2USPwoLZXhwZXJpbWVudHMYASADKAsyHS54dGNwX3RyZWF0bWVudC'
    '52MS5FeHBlcmltZW50UgtleHBlcmltZW50cxI5CgdkZXNpcmVkGAIgAygLMh8ueHRjcF90cmVh'
    'dG1lbnQudjEuRGVzaXJlZFN0YXRlUgdkZXNpcmVkEj0KCG9ic2VydmVkGAMgAygLMiEueHRjcF'
    '90cmVhdG1lbnQudjEuQ2hlY2tJblJlcXVlc3RSCG9ic2VydmVk');
