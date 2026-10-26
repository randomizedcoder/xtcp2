// This is a generated file - do not edit.
//
// Generated from xtcp_treatment_journal/v1/journal.proto.

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

@$core.Deprecated('Use assignmentModeDescriptor instead')
const AssignmentMode$json = {
  '1': 'AssignmentMode',
  '2': [
    {'1': 'ASSIGNMENT_MODE_UNSPECIFIED', '2': 0},
    {'1': 'ASSIGNMENT_MODE_TRIAL', '2': 1},
    {'1': 'ASSIGNMENT_MODE_STABLE', '2': 2},
    {'1': 'ASSIGNMENT_MODE_SUSPENDED', '2': 3},
    {'1': 'ASSIGNMENT_MODE_RETIRED', '2': 4},
  ],
};

/// Descriptor for `AssignmentMode`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List assignmentModeDescriptor = $convert.base64Decode(
    'Cg5Bc3NpZ25tZW50TW9kZRIfChtBU1NJR05NRU5UX01PREVfVU5TUEVDSUZJRUQQABIZChVBU1'
    'NJR05NRU5UX01PREVfVFJJQUwQARIaChZBU1NJR05NRU5UX01PREVfU1RBQkxFEAISHQoZQVNT'
    'SUdOTUVOVF9NT0RFX1NVU1BFTkRFRBADEhsKF0FTU0lHTk1FTlRfTU9ERV9SRVRJUkVEEAQ=');

@$core.Deprecated('Use applyStatusDescriptor instead')
const ApplyStatus$json = {
  '1': 'ApplyStatus',
  '2': [
    {'1': 'APPLY_STATUS_UNSPECIFIED', '2': 0},
    {'1': 'APPLY_STATUS_PENDING', '2': 1},
    {'1': 'APPLY_STATUS_VERIFIED', '2': 2},
    {'1': 'APPLY_STATUS_REMOVED', '2': 3},
    {'1': 'APPLY_STATUS_EXPIRED', '2': 4},
    {'1': 'APPLY_STATUS_FAILED', '2': 5},
  ],
};

/// Descriptor for `ApplyStatus`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List applyStatusDescriptor = $convert.base64Decode(
    'CgtBcHBseVN0YXR1cxIcChhBUFBMWV9TVEFUVVNfVU5TUEVDSUZJRUQQABIYChRBUFBMWV9TVE'
    'FUVVNfUEVORElORxABEhkKFUFQUExZX1NUQVRVU19WRVJJRklFRBACEhgKFEFQUExZX1NUQVRV'
    'U19SRU1PVkVEEAMSGAoUQVBQTFlfU1RBVFVTX0VYUElSRUQQBBIXChNBUFBMWV9TVEFUVVNfRk'
    'FJTEVEEAU=');

@$core.Deprecated('Use tCPSettingsDescriptor instead')
const TCPSettings$json = {
  '1': 'TCPSettings',
  '2': [
    {
      '1': 'congestion_control',
      '3': 1,
      '4': 1,
      '5': 9,
      '9': 0,
      '10': 'congestionControl',
      '17': true
    },
    {'1': 'congestion_lock', '3': 2, '4': 1, '5': 8, '10': 'congestionLock'},
    {
      '1': 'rto_min_ms',
      '3': 3,
      '4': 1,
      '5': 13,
      '9': 1,
      '10': 'rtoMinMs',
      '17': true
    },
    {
      '1': 'initial_congestion_window',
      '3': 4,
      '4': 1,
      '5': 13,
      '9': 2,
      '10': 'initialCongestionWindow',
      '17': true
    },
    {
      '1': 'initial_receive_window',
      '3': 5,
      '4': 1,
      '5': 13,
      '9': 3,
      '10': 'initialReceiveWindow',
      '17': true
    },
    {
      '1': 'quick_ack',
      '3': 6,
      '4': 1,
      '5': 8,
      '9': 4,
      '10': 'quickAck',
      '17': true
    },
    {'1': 'ecn', '3': 7, '4': 1, '5': 8, '9': 5, '10': 'ecn', '17': true},
  ],
  '8': [
    {'1': '_congestion_control'},
    {'1': '_rto_min_ms'},
    {'1': '_initial_congestion_window'},
    {'1': '_initial_receive_window'},
    {'1': '_quick_ack'},
    {'1': '_ecn'},
  ],
};

/// Descriptor for `TCPSettings`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List tCPSettingsDescriptor = $convert.base64Decode(
    'CgtUQ1BTZXR0aW5ncxIyChJjb25nZXN0aW9uX2NvbnRyb2wYASABKAlIAFIRY29uZ2VzdGlvbk'
    'NvbnRyb2yIAQESJwoPY29uZ2VzdGlvbl9sb2NrGAIgASgIUg5jb25nZXN0aW9uTG9jaxIhCgpy'
    'dG9fbWluX21zGAMgASgNSAFSCHJ0b01pbk1ziAEBEj8KGWluaXRpYWxfY29uZ2VzdGlvbl93aW'
    '5kb3cYBCABKA1IAlIXaW5pdGlhbENvbmdlc3Rpb25XaW5kb3eIAQESOQoWaW5pdGlhbF9yZWNl'
    'aXZlX3dpbmRvdxgFIAEoDUgDUhRpbml0aWFsUmVjZWl2ZVdpbmRvd4gBARIgCglxdWlja19hY2'
    'sYBiABKAhIBFIIcXVpY2tBY2uIAQESFQoDZWNuGAcgASgISAVSA2VjbogBAUIVChNfY29uZ2Vz'
    'dGlvbl9jb250cm9sQg0KC19ydG9fbWluX21zQhwKGl9pbml0aWFsX2Nvbmdlc3Rpb25fd2luZG'
    '93QhkKF19pbml0aWFsX3JlY2VpdmVfd2luZG93QgwKCl9xdWlja19hY2tCBgoEX2Vjbg==');

@$core.Deprecated('Use treatmentDescriptor instead')
const Treatment$json = {
  '1': 'Treatment',
  '2': [
    {'1': 'id', '3': 1, '4': 1, '5': 9, '10': 'id'},
    {'1': 'revision', '3': 2, '4': 1, '5': 4, '10': 'revision'},
    {'1': 'destination', '3': 3, '4': 1, '5': 9, '10': 'destination'},
    {
      '1': 'settings',
      '3': 4,
      '4': 1,
      '5': 11,
      '6': '.xtcp_treatment_journal.v1.TCPSettings',
      '10': 'settings'
    },
    {
      '1': 'ecn_fabric_verified',
      '3': 5,
      '4': 1,
      '5': 8,
      '10': 'ecnFabricVerified'
    },
  ],
};

/// Descriptor for `Treatment`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List treatmentDescriptor = $convert.base64Decode(
    'CglUcmVhdG1lbnQSDgoCaWQYASABKAlSAmlkEhoKCHJldmlzaW9uGAIgASgEUghyZXZpc2lvbh'
    'IgCgtkZXN0aW5hdGlvbhgDIAEoCVILZGVzdGluYXRpb24SQgoIc2V0dGluZ3MYBCABKAsyJi54'
    'dGNwX3RyZWF0bWVudF9qb3VybmFsLnYxLlRDUFNldHRpbmdzUghzZXR0aW5ncxIuChNlY25fZm'
    'FicmljX3ZlcmlmaWVkGAUgASgIUhFlY25GYWJyaWNWZXJpZmllZA==');

@$core.Deprecated('Use assignmentDescriptor instead')
const Assignment$json = {
  '1': 'Assignment',
  '2': [
    {'1': 'experiment_id', '3': 1, '4': 1, '5': 9, '10': 'experimentId'},
    {
      '1': 'treatment',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.xtcp_treatment_journal.v1.Treatment',
      '10': 'treatment'
    },
    {
      '1': 'mode',
      '3': 3,
      '4': 1,
      '5': 14,
      '6': '.xtcp_treatment_journal.v1.AssignmentMode',
      '10': 'mode'
    },
    {
      '1': 'lease_expires_at',
      '3': 4,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'leaseExpiresAt'
    },
    {
      '1': 'predecessor',
      '3': 5,
      '4': 1,
      '5': 11,
      '6': '.xtcp_treatment_journal.v1.Treatment',
      '10': 'predecessor'
    },
    {'1': 'generation', '3': 6, '4': 1, '5': 4, '10': 'generation'},
  ],
};

/// Descriptor for `Assignment`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List assignmentDescriptor = $convert.base64Decode(
    'CgpBc3NpZ25tZW50EiMKDWV4cGVyaW1lbnRfaWQYASABKAlSDGV4cGVyaW1lbnRJZBJCCgl0cm'
    'VhdG1lbnQYAiABKAsyJC54dGNwX3RyZWF0bWVudF9qb3VybmFsLnYxLlRyZWF0bWVudFIJdHJl'
    'YXRtZW50Ej0KBG1vZGUYAyABKA4yKS54dGNwX3RyZWF0bWVudF9qb3VybmFsLnYxLkFzc2lnbm'
    '1lbnRNb2RlUgRtb2RlEkQKEGxlYXNlX2V4cGlyZXNfYXQYBCABKAsyGi5nb29nbGUucHJvdG9i'
    'dWYuVGltZXN0YW1wUg5sZWFzZUV4cGlyZXNBdBJGCgtwcmVkZWNlc3NvchgFIAEoCzIkLnh0Y3'
    'BfdHJlYXRtZW50X2pvdXJuYWwudjEuVHJlYXRtZW50UgtwcmVkZWNlc3NvchIeCgpnZW5lcmF0'
    'aW9uGAYgASgEUgpnZW5lcmF0aW9u');

@$core.Deprecated('Use desiredDescriptor instead')
const Desired$json = {
  '1': 'Desired',
  '2': [
    {'1': 'revision', '3': 1, '4': 1, '5': 4, '10': 'revision'},
    {'1': 'suspended', '3': 2, '4': 1, '5': 8, '10': 'suspended'},
    {
      '1': 'issued_at',
      '3': 3,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'issuedAt'
    },
    {
      '1': 'assignments',
      '3': 4,
      '4': 3,
      '5': 11,
      '6': '.xtcp_treatment_journal.v1.Assignment',
      '10': 'assignments'
    },
  ],
};

/// Descriptor for `Desired`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List desiredDescriptor = $convert.base64Decode(
    'CgdEZXNpcmVkEhoKCHJldmlzaW9uGAEgASgEUghyZXZpc2lvbhIcCglzdXNwZW5kZWQYAiABKA'
    'hSCXN1c3BlbmRlZBI3Cglpc3N1ZWRfYXQYAyABKAsyGi5nb29nbGUucHJvdG9idWYuVGltZXN0'
    'YW1wUghpc3N1ZWRBdBJHCgthc3NpZ25tZW50cxgEIAMoCzIlLnh0Y3BfdHJlYXRtZW50X2pvdX'
    'JuYWwudjEuQXNzaWdubWVudFILYXNzaWdubWVudHM=');

@$core.Deprecated('Use recordDescriptor instead')
const Record$json = {
  '1': 'Record',
  '2': [
    {
      '1': 'assignment',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.xtcp_treatment_journal.v1.Assignment',
      '10': 'assignment'
    },
    {'1': 'desired_revision', '3': 2, '4': 1, '5': 4, '10': 'desiredRevision'},
    {
      '1': 'status',
      '3': 3,
      '4': 1,
      '5': 14,
      '6': '.xtcp_treatment_journal.v1.ApplyStatus',
      '10': 'status'
    },
    {
      '1': 'received_at',
      '3': 4,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'receivedAt'
    },
    {
      '1': 'attempted_at',
      '3': 5,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'attemptedAt'
    },
    {
      '1': 'applied_at',
      '3': 6,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'appliedAt'
    },
    {
      '1': 'verified_at',
      '3': 7,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'verifiedAt'
    },
    {
      '1': 'removed_at',
      '3': 8,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'removedAt'
    },
    {'1': 'error', '3': 9, '4': 1, '5': 9, '10': 'error'},
    {'1': 'installed_route', '3': 10, '4': 1, '5': 12, '10': 'installedRoute'},
    {
      '1': 'installed_boot_id',
      '3': 11,
      '4': 1,
      '5': 9,
      '10': 'installedBootId'
    },
    {'1': 'expired_latched', '3': 12, '4': 1, '5': 8, '10': 'expiredLatched'},
  ],
};

/// Descriptor for `Record`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List recordDescriptor = $convert.base64Decode(
    'CgZSZWNvcmQSRQoKYXNzaWdubWVudBgBIAEoCzIlLnh0Y3BfdHJlYXRtZW50X2pvdXJuYWwudj'
    'EuQXNzaWdubWVudFIKYXNzaWdubWVudBIpChBkZXNpcmVkX3JldmlzaW9uGAIgASgEUg9kZXNp'
    'cmVkUmV2aXNpb24SPgoGc3RhdHVzGAMgASgOMiYueHRjcF90cmVhdG1lbnRfam91cm5hbC52MS'
    '5BcHBseVN0YXR1c1IGc3RhdHVzEjsKC3JlY2VpdmVkX2F0GAQgASgLMhouZ29vZ2xlLnByb3Rv'
    'YnVmLlRpbWVzdGFtcFIKcmVjZWl2ZWRBdBI9CgxhdHRlbXB0ZWRfYXQYBSABKAsyGi5nb29nbG'
    'UucHJvdG9idWYuVGltZXN0YW1wUgthdHRlbXB0ZWRBdBI5CgphcHBsaWVkX2F0GAYgASgLMhou'
    'Z29vZ2xlLnByb3RvYnVmLlRpbWVzdGFtcFIJYXBwbGllZEF0EjsKC3ZlcmlmaWVkX2F0GAcgAS'
    'gLMhouZ29vZ2xlLnByb3RvYnVmLlRpbWVzdGFtcFIKdmVyaWZpZWRBdBI5CgpyZW1vdmVkX2F0'
    'GAggASgLMhouZ29vZ2xlLnByb3RvYnVmLlRpbWVzdGFtcFIJcmVtb3ZlZEF0EhQKBWVycm9yGA'
    'kgASgJUgVlcnJvchInCg9pbnN0YWxsZWRfcm91dGUYCiABKAxSDmluc3RhbGxlZFJvdXRlEioK'
    'EWluc3RhbGxlZF9ib290X2lkGAsgASgJUg9pbnN0YWxsZWRCb290SWQSJwoPZXhwaXJlZF9sYX'
    'RjaGVkGAwgASgIUg5leHBpcmVkTGF0Y2hlZA==');

@$core.Deprecated('Use journalDescriptor instead')
const Journal$json = {
  '1': 'Journal',
  '2': [
    {'1': 'agent_id', '3': 1, '4': 1, '5': 9, '10': 'agentId'},
    {'1': 'location', '3': 2, '4': 1, '5': 9, '10': 'location'},
    {'1': 'boot_id', '3': 3, '4': 1, '5': 9, '10': 'bootId'},
    {
      '1': 'desired',
      '3': 4,
      '4': 1,
      '5': 11,
      '6': '.xtcp_treatment_journal.v1.Desired',
      '10': 'desired'
    },
    {
      '1': 'locally_suspended',
      '3': 5,
      '4': 1,
      '5': 8,
      '10': 'locallySuspended'
    },
    {
      '1': 'suspension_reason',
      '3': 6,
      '4': 1,
      '5': 9,
      '10': 'suspensionReason'
    },
    {
      '1': 'records',
      '3': 7,
      '4': 3,
      '5': 11,
      '6': '.xtcp_treatment_journal.v1.Record',
      '10': 'records'
    },
    {
      '1': 'updated_at',
      '3': 8,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'updatedAt'
    },
  ],
};

/// Descriptor for `Journal`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List journalDescriptor = $convert.base64Decode(
    'CgdKb3VybmFsEhkKCGFnZW50X2lkGAEgASgJUgdhZ2VudElkEhoKCGxvY2F0aW9uGAIgASgJUg'
    'hsb2NhdGlvbhIXCgdib290X2lkGAMgASgJUgZib290SWQSPAoHZGVzaXJlZBgEIAEoCzIiLnh0'
    'Y3BfdHJlYXRtZW50X2pvdXJuYWwudjEuRGVzaXJlZFIHZGVzaXJlZBIrChFsb2NhbGx5X3N1c3'
    'BlbmRlZBgFIAEoCFIQbG9jYWxseVN1c3BlbmRlZBIrChFzdXNwZW5zaW9uX3JlYXNvbhgGIAEo'
    'CVIQc3VzcGVuc2lvblJlYXNvbhI7CgdyZWNvcmRzGAcgAygLMiEueHRjcF90cmVhdG1lbnRfam'
    '91cm5hbC52MS5SZWNvcmRSB3JlY29yZHMSOQoKdXBkYXRlZF9hdBgIIAEoCzIaLmdvb2dsZS5w'
    'cm90b2J1Zi5UaW1lc3RhbXBSCXVwZGF0ZWRBdA==');
