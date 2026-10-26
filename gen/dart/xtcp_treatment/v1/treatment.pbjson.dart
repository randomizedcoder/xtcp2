// This is a generated file - do not edit.
//
// Generated from xtcp_treatment/v1/treatment.proto.

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

@$core.Deprecated('Use roleDescriptor instead')
const Role$json = {
  '1': 'Role',
  '2': [
    {'1': 'ROLE_UNSPECIFIED', '2': 0},
    {'1': 'ROLE_AGENT', '2': 1},
    {'1': 'ROLE_OPERATOR', '2': 2},
  ],
};

/// Descriptor for `Role`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List roleDescriptor = $convert.base64Decode(
    'CgRSb2xlEhQKEFJPTEVfVU5TUEVDSUZJRUQQABIOCgpST0xFX0FHRU5UEAESEQoNUk9MRV9PUE'
    'VSQVRPUhAC');

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

@$core.Deprecated('Use outcomeDescriptor instead')
const Outcome$json = {
  '1': 'Outcome',
  '2': [
    {'1': 'OUTCOME_UNSPECIFIED', '2': 0},
    {'1': 'OUTCOME_BASELINE', '2': 1},
    {'1': 'OUTCOME_AWAITING_APPLICATION', '2': 2},
    {'1': 'OUTCOME_AWAITING_EXPOSURE', '2': 3},
    {'1': 'OUTCOME_INSUFFICIENT_DURATION', '2': 4},
    {'1': 'OUTCOME_INSUFFICIENT_DATA', '2': 5},
    {'1': 'OUTCOME_INSUFFICIENT_CONTROLS', '2': 6},
    {'1': 'OUTCOME_STALE', '2': 7},
    {'1': 'OUTCOME_UNAVAILABLE', '2': 8},
    {'1': 'OUTCOME_INCONCLUSIVE', '2': 9},
    {'1': 'OUTCOME_SAFETY_ONLY', '2': 10},
    {'1': 'OUTCOME_FINAL_SAFETY', '2': 11},
    {'1': 'OUTCOME_BENEFICIAL', '2': 12},
    {'1': 'OUTCOME_HARMFUL', '2': 13},
    {'1': 'OUTCOME_EXPIRED', '2': 14},
    {'1': 'OUTCOME_STABLE_OBSERVED', '2': 15},
    {'1': 'OUTCOME_STABLE_EVIDENCE_INCOMPLETE', '2': 16},
    {'1': 'OUTCOME_STABLE_REGRESSION_REVIEW_REQUIRED', '2': 17},
    {'1': 'OUTCOME_OBSERVING', '2': 18},
  ],
};

/// Descriptor for `Outcome`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List outcomeDescriptor = $convert.base64Decode(
    'CgdPdXRjb21lEhcKE09VVENPTUVfVU5TUEVDSUZJRUQQABIUChBPVVRDT01FX0JBU0VMSU5FEA'
    'ESIAocT1VUQ09NRV9BV0FJVElOR19BUFBMSUNBVElPThACEh0KGU9VVENPTUVfQVdBSVRJTkdf'
    'RVhQT1NVUkUQAxIhCh1PVVRDT01FX0lOU1VGRklDSUVOVF9EVVJBVElPThAEEh0KGU9VVENPTU'
    'VfSU5TVUZGSUNJRU5UX0RBVEEQBRIhCh1PVVRDT01FX0lOU1VGRklDSUVOVF9DT05UUk9MUxAG'
    'EhEKDU9VVENPTUVfU1RBTEUQBxIXChNPVVRDT01FX1VOQVZBSUxBQkxFEAgSGAoUT1VUQ09NRV'
    '9JTkNPTkNMVVNJVkUQCRIXChNPVVRDT01FX1NBRkVUWV9PTkxZEAoSGAoUT1VUQ09NRV9GSU5B'
    'TF9TQUZFVFkQCxIWChJPVVRDT01FX0JFTkVGSUNJQUwQDBITCg9PVVRDT01FX0hBUk1GVUwQDR'
    'ITCg9PVVRDT01FX0VYUElSRUQQDhIbChdPVVRDT01FX1NUQUJMRV9PQlNFUlZFRBAPEiYKIk9V'
    'VENPTUVfU1RBQkxFX0VWSURFTkNFX0lOQ09NUExFVEUQEBItCilPVVRDT01FX1NUQUJMRV9SRU'
    'dSRVNTSU9OX1JFVklFV19SRVFVSVJFRBAREhUKEU9VVENPTUVfT0JTRVJWSU5HEBI=');

@$core.Deprecated('Use experimentStatusDescriptor instead')
const ExperimentStatus$json = {
  '1': 'ExperimentStatus',
  '2': [
    {'1': 'EXPERIMENT_STATUS_UNSPECIFIED', '2': 0},
    {'1': 'EXPERIMENT_STATUS_PROPOSED', '2': 1},
    {'1': 'EXPERIMENT_STATUS_RUNNING', '2': 2},
    {'1': 'EXPERIMENT_STATUS_PAUSED', '2': 3},
    {'1': 'EXPERIMENT_STATUS_SUSPENDED', '2': 4},
    {'1': 'EXPERIMENT_STATUS_COMPLETE', '2': 5},
    {'1': 'EXPERIMENT_STATUS_RETIRED', '2': 6},
    {'1': 'EXPERIMENT_STATUS_ROLLED_BACK', '2': 7},
  ],
};

/// Descriptor for `ExperimentStatus`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List experimentStatusDescriptor = $convert.base64Decode(
    'ChBFeHBlcmltZW50U3RhdHVzEiEKHUVYUEVSSU1FTlRfU1RBVFVTX1VOU1BFQ0lGSUVEEAASHg'
    'oaRVhQRVJJTUVOVF9TVEFUVVNfUFJPUE9TRUQQARIdChlFWFBFUklNRU5UX1NUQVRVU19SVU5O'
    'SU5HEAISHAoYRVhQRVJJTUVOVF9TVEFUVVNfUEFVU0VEEAMSHwobRVhQRVJJTUVOVF9TVEFUVV'
    'NfU1VTUEVOREVEEAQSHgoaRVhQRVJJTUVOVF9TVEFUVVNfQ09NUExFVEUQBRIdChlFWFBFUklN'
    'RU5UX1NUQVRVU19SRVRJUkVEEAYSIQodRVhQRVJJTUVOVF9TVEFUVVNfUk9MTEVEX0JBQ0sQBw'
    '==');

@$core.Deprecated('Use tCPSettingsDescriptor instead')
const TCPSettings$json = {
  '1': 'TCPSettings',
  '2': [
    {
      '1': 'congestion_control',
      '3': 1,
      '4': 1,
      '5': 9,
      '8': {},
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
      '8': {},
      '9': 1,
      '10': 'rtoMinMs',
      '17': true
    },
    {
      '1': 'initial_congestion_window',
      '3': 4,
      '4': 1,
      '5': 13,
      '8': {},
      '9': 2,
      '10': 'initialCongestionWindow',
      '17': true
    },
    {
      '1': 'initial_receive_window',
      '3': 5,
      '4': 1,
      '5': 13,
      '8': {},
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
  '7': {},
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
    'CgtUQ1BTZXR0aW5ncxJZChJjb25nZXN0aW9uX2NvbnRyb2wYASABKAlCJbpIInIgGA8yHF5bYS'
    '16QS1aMC05XVthLXpBLVowLTlfLi1dKiRIAFIRY29uZ2VzdGlvbkNvbnRyb2yIAQESJwoPY29u'
    'Z2VzdGlvbl9sb2NrGAIgASgIUg5jb25nZXN0aW9uTG9jaxIuCgpydG9fbWluX21zGAMgASgNQg'
    'u6SAgqBhjAqQcoAUgBUghydG9NaW5Nc4gBARJLChlpbml0aWFsX2Nvbmdlc3Rpb25fd2luZG93'
    'GAQgASgNQgq6SAcqBRiACCgBSAJSF2luaXRpYWxDb25nZXN0aW9uV2luZG93iAEBEkUKFmluaX'
    'RpYWxfcmVjZWl2ZV93aW5kb3cYBSABKA1CCrpIByoFGIAIKAFIA1IUaW5pdGlhbFJlY2VpdmVX'
    'aW5kb3eIAQESIAoJcXVpY2tfYWNrGAYgASgISARSCHF1aWNrQWNriAEBEhUKA2VjbhgHIAEoCE'
    'gFUgNlY26IAQE6hwO6SIMDGvMBChZ0Y3Bfc2V0dGluZ3Mubm9uX2VtcHR5Ei9hIHRyZWF0bWVu'
    'dCBtdXN0IHNldCBhdCBsZWFzdCBvbmUgVENQIHBhcmFtZXRlchqnAWhhcyh0aGlzLmNvbmdlc3'
    'Rpb25fY29udHJvbCkgfHwgaGFzKHRoaXMucnRvX21pbl9tcykgfHwgaGFzKHRoaXMuaW5pdGlh'
    'bF9jb25nZXN0aW9uX3dpbmRvdykgfHwgaGFzKHRoaXMuaW5pdGlhbF9yZWNlaXZlX3dpbmRvdy'
    'kgfHwgaGFzKHRoaXMucXVpY2tfYWNrKSB8fCBoYXModGhpcy5lY24pGooBCiR0Y3Bfc2V0dGlu'
    'Z3MubG9ja19yZXF1aXJlc19hbGdvcml0aG0SK2Nvbmdlc3Rpb25fbG9jayByZXF1aXJlcyBjb2'
    '5nZXN0aW9uX2NvbnRyb2waNSF0aGlzLmNvbmdlc3Rpb25fbG9jayB8fCBoYXModGhpcy5jb25n'
    'ZXN0aW9uX2NvbnRyb2wpQhUKE19jb25nZXN0aW9uX2NvbnRyb2xCDQoLX3J0b19taW5fbXNCHA'
    'oaX2luaXRpYWxfY29uZ2VzdGlvbl93aW5kb3dCGQoXX2luaXRpYWxfcmVjZWl2ZV93aW5kb3dC'
    'DAoKX3F1aWNrX2Fja0IGCgRfZWNu');

@$core.Deprecated('Use treatmentDescriptor instead')
const Treatment$json = {
  '1': 'Treatment',
  '2': [
    {'1': 'id', '3': 1, '4': 1, '5': 9, '8': {}, '10': 'id'},
    {'1': 'revision', '3': 2, '4': 1, '5': 4, '8': {}, '10': 'revision'},
    {'1': 'destination', '3': 3, '4': 1, '5': 9, '8': {}, '10': 'destination'},
    {
      '1': 'settings',
      '3': 4,
      '4': 1,
      '5': 11,
      '6': '.xtcp_treatment.v1.TCPSettings',
      '8': {},
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
  '7': {},
};

/// Descriptor for `Treatment`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List treatmentDescriptor = $convert.base64Decode(
    'CglUcmVhdG1lbnQSOQoCaWQYASABKAlCKbpIJnIkMiJeW2EtekEtWjAtOV1bYS16QS1aMC05Xy'
    '4tXXswLDEyN30kUgJpZBIjCghyZXZpc2lvbhgCIAEoBEIHukgEMgIgAFIIcmV2aXNpb24SLAoL'
    'ZGVzdGluYXRpb24YAyABKAlCCrpIB3IFGDHoAQFSC2Rlc3RpbmF0aW9uEkIKCHNldHRpbmdzGA'
    'QgASgLMh4ueHRjcF90cmVhdG1lbnQudjEuVENQU2V0dGluZ3NCBrpIA8gBAVIIc2V0dGluZ3MS'
    'LgoTZWNuX2ZhYnJpY192ZXJpZmllZBgFIAEoCFIRZWNuRmFicmljVmVyaWZpZWQ6yAG6SMQBGs'
    'EBChx0cmVhdG1lbnQuZGN0Y3BfcmVxdWlyZXNfZWNuEkFEQ1RDUCByZXF1aXJlcyBpbmRlcGVu'
    'ZGVudGx5IHZlcmlmaWVkIEVDTiBmYWJyaWMgYW5kIGV4cGxpY2l0IEVDThpedGhpcy5zZXR0aW'
    '5ncy5jb25nZXN0aW9uX2NvbnRyb2wgIT0gJ2RjdGNwJyB8fCAodGhpcy5lY25fZmFicmljX3Zl'
    'cmlmaWVkICYmIHRoaXMuc2V0dGluZ3MuZWNuKQ==');

@$core.Deprecated('Use assignmentDescriptor instead')
const Assignment$json = {
  '1': 'Assignment',
  '2': [
    {
      '1': 'experiment_id',
      '3': 2,
      '4': 1,
      '5': 9,
      '8': {},
      '10': 'experimentId'
    },
    {
      '1': 'treatment',
      '3': 3,
      '4': 1,
      '5': 11,
      '6': '.xtcp_treatment.v1.Treatment',
      '8': {},
      '10': 'treatment'
    },
    {
      '1': 'mode',
      '3': 4,
      '4': 1,
      '5': 14,
      '6': '.xtcp_treatment.v1.AssignmentMode',
      '8': {},
      '10': 'mode'
    },
    {
      '1': 'lease_expires_at',
      '3': 5,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'leaseExpiresAt'
    },
    {
      '1': 'predecessor',
      '3': 6,
      '4': 1,
      '5': 11,
      '6': '.xtcp_treatment.v1.Treatment',
      '10': 'predecessor'
    },
    {'1': 'generation', '3': 7, '4': 1, '5': 4, '8': {}, '10': 'generation'},
  ],
  '7': {},
  '9': [
    {'1': 1, '2': 2},
  ],
  '10': ['id'],
};

/// Descriptor for `Assignment`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List assignmentDescriptor = $convert.base64Decode(
    'CgpBc3NpZ25tZW50Ek4KDWV4cGVyaW1lbnRfaWQYAiABKAlCKbpIJnIkMiJeW2EtekEtWjAtOV'
    '1bYS16QS1aMC05Xy4tXXswLDEyN30kUgxleHBlcmltZW50SWQSQgoJdHJlYXRtZW50GAMgASgL'
    'MhwueHRjcF90cmVhdG1lbnQudjEuVHJlYXRtZW50Qga6SAPIAQFSCXRyZWF0bWVudBJBCgRtb2'
    'RlGAQgASgOMiEueHRjcF90cmVhdG1lbnQudjEuQXNzaWdubWVudE1vZGVCCrpIB4IBBBABIABS'
    'BG1vZGUSRAoQbGVhc2VfZXhwaXJlc19hdBgFIAEoCzIaLmdvb2dsZS5wcm90b2J1Zi5UaW1lc3'
    'RhbXBSDmxlYXNlRXhwaXJlc0F0Ej4KC3ByZWRlY2Vzc29yGAYgASgLMhwueHRjcF90cmVhdG1l'
    'bnQudjEuVHJlYXRtZW50UgtwcmVkZWNlc3NvchInCgpnZW5lcmF0aW9uGAcgASgEQge6SAQyAi'
    'AAUgpnZW5lcmF0aW9uOq0CukipAhp9Ch9hc3NpZ25tZW50LnRyaWFsX3JlcXVpcmVzX2xlYXNl'
    'EixhIHRyaWFsIGFzc2lnbm1lbnQgcmVxdWlyZXMgbGVhc2VfZXhwaXJlc19hdBosdGhpcy5tb2'
    'RlICE9IDEgfHwgaGFzKHRoaXMubGVhc2VfZXhwaXJlc19hdCkapwEKImFzc2lnbm1lbnQucHJl'
    'ZGVjZXNzb3JfZGVzdGluYXRpb24SK3ByZWRlY2Vzc29yIG11c3QgY292ZXIgdGhlIHNhbWUgZG'
    'VzdGluYXRpb24aVCFoYXModGhpcy5wcmVkZWNlc3NvcikgfHwgdGhpcy5wcmVkZWNlc3Nvci5k'
    'ZXN0aW5hdGlvbiA9PSB0aGlzLnRyZWF0bWVudC5kZXN0aW5hdGlvbkoECAEQAlICaWQ=');

@$core.Deprecated('Use desiredStateDescriptor instead')
const DesiredState$json = {
  '1': 'DesiredState',
  '2': [
    {'1': 'agent_id', '3': 1, '4': 1, '5': 9, '8': {}, '10': 'agentId'},
    {'1': 'revision', '3': 2, '4': 1, '5': 4, '8': {}, '10': 'revision'},
    {'1': 'suspended', '3': 3, '4': 1, '5': 8, '10': 'suspended'},
    {
      '1': 'assignments',
      '3': 4,
      '4': 3,
      '5': 11,
      '6': '.xtcp_treatment.v1.Assignment',
      '8': {},
      '10': 'assignments'
    },
    {
      '1': 'issued_at',
      '3': 5,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '8': {},
      '10': 'issuedAt'
    },
  ],
};

/// Descriptor for `DesiredState`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List desiredStateDescriptor = $convert.base64Decode(
    'CgxEZXNpcmVkU3RhdGUSRAoIYWdlbnRfaWQYASABKAlCKbpIJnIkMiJeW2EtekEtWjAtOV1bYS'
    '16QS1aMC05Xy4tXXswLDEyN30kUgdhZ2VudElkEiMKCHJldmlzaW9uGAIgASgEQge6SAQyAiAA'
    'UghyZXZpc2lvbhIcCglzdXNwZW5kZWQYAyABKAhSCXN1c3BlbmRlZBJKCgthc3NpZ25tZW50cx'
    'gEIAMoCzIdLnh0Y3BfdHJlYXRtZW50LnYxLkFzc2lnbm1lbnRCCbpIBpIBAxCAIFILYXNzaWdu'
    'bWVudHMSPwoJaXNzdWVkX2F0GAUgASgLMhouZ29vZ2xlLnByb3RvYnVmLlRpbWVzdGFtcEIGuk'
    'gDyAEBUghpc3N1ZWRBdA==');

@$core.Deprecated('Use capabilitiesDescriptor instead')
const Capabilities$json = {
  '1': 'Capabilities',
  '2': [
    {'1': 'boot_id', '3': 1, '4': 1, '5': 9, '8': {}, '10': 'bootId'},
    {
      '1': 'kernel_release',
      '3': 2,
      '4': 1,
      '5': 9,
      '8': {},
      '10': 'kernelRelease'
    },
    {
      '1': 'congestion_algorithms',
      '3': 3,
      '4': 3,
      '5': 9,
      '8': {},
      '10': 'congestionAlgorithms'
    },
    {
      '1': 'simple_host_routing',
      '3': 4,
      '4': 1,
      '5': 8,
      '10': 'simpleHostRouting'
    },
    {'1': 'actuator_enabled', '3': 5, '4': 1, '5': 8, '10': 'actuatorEnabled'},
  ],
};

/// Descriptor for `Capabilities`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List capabilitiesDescriptor = $convert.base64Decode(
    'CgxDYXBhYmlsaXRpZXMSQgoHYm9vdF9pZBgBIAEoCUIpukgmciQyIl5bYS16QS1aMC05XVthLX'
    'pBLVowLTlfLi1dezAsMTI3fSRSBmJvb3RJZBIvCg5rZXJuZWxfcmVsZWFzZRgCIAEoCUIIukgF'
    'cgMYgAJSDWtlcm5lbFJlbGVhc2USQwoVY29uZ2VzdGlvbl9hbGdvcml0aG1zGAMgAygJQg66SA'
    'uSAQgQQCIEcgIYD1IUY29uZ2VzdGlvbkFsZ29yaXRobXMSLgoTc2ltcGxlX2hvc3Rfcm91dGlu'
    'ZxgEIAEoCFIRc2ltcGxlSG9zdFJvdXRpbmcSKQoQYWN0dWF0b3JfZW5hYmxlZBgFIAEoCFIPYW'
    'N0dWF0b3JFbmFibGVk');

@$core.Deprecated('Use applicationRecordDescriptor instead')
const ApplicationRecord$json = {
  '1': 'ApplicationRecord',
  '2': [
    {
      '1': 'assignment',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.xtcp_treatment.v1.Assignment',
      '8': {},
      '10': 'assignment'
    },
    {'1': 'desired_revision', '3': 2, '4': 1, '5': 4, '10': 'desiredRevision'},
    {
      '1': 'status',
      '3': 3,
      '4': 1,
      '5': 14,
      '6': '.xtcp_treatment.v1.ApplyStatus',
      '8': {},
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
    {'1': 'error', '3': 9, '4': 1, '5': 9, '8': {}, '10': 'error'},
    {
      '1': 'installed_route',
      '3': 10,
      '4': 1,
      '5': 12,
      '8': {},
      '10': 'installedRoute'
    },
    {
      '1': 'installed_boot_id',
      '3': 11,
      '4': 1,
      '5': 9,
      '8': {},
      '10': 'installedBootId'
    },
    {'1': 'expired_latched', '3': 12, '4': 1, '5': 8, '10': 'expiredLatched'},
  ],
};

/// Descriptor for `ApplicationRecord`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List applicationRecordDescriptor = $convert.base64Decode(
    'ChFBcHBsaWNhdGlvblJlY29yZBJFCgphc3NpZ25tZW50GAEgASgLMh0ueHRjcF90cmVhdG1lbn'
    'QudjEuQXNzaWdubWVudEIGukgDyAEBUgphc3NpZ25tZW50EikKEGRlc2lyZWRfcmV2aXNpb24Y'
    'AiABKARSD2Rlc2lyZWRSZXZpc2lvbhJACgZzdGF0dXMYAyABKA4yHi54dGNwX3RyZWF0bWVudC'
    '52MS5BcHBseVN0YXR1c0IIukgFggECEAFSBnN0YXR1cxI7CgtyZWNlaXZlZF9hdBgEIAEoCzIa'
    'Lmdvb2dsZS5wcm90b2J1Zi5UaW1lc3RhbXBSCnJlY2VpdmVkQXQSPQoMYXR0ZW1wdGVkX2F0GA'
    'UgASgLMhouZ29vZ2xlLnByb3RvYnVmLlRpbWVzdGFtcFILYXR0ZW1wdGVkQXQSOQoKYXBwbGll'
    'ZF9hdBgGIAEoCzIaLmdvb2dsZS5wcm90b2J1Zi5UaW1lc3RhbXBSCWFwcGxpZWRBdBI7Cgt2ZX'
    'JpZmllZF9hdBgHIAEoCzIaLmdvb2dsZS5wcm90b2J1Zi5UaW1lc3RhbXBSCnZlcmlmaWVkQXQS'
    'OQoKcmVtb3ZlZF9hdBgIIAEoCzIaLmdvb2dsZS5wcm90b2J1Zi5UaW1lc3RhbXBSCXJlbW92ZW'
    'RBdBIeCgVlcnJvchgJIAEoCUIIukgFcgMYgCBSBWVycm9yEjEKD2luc3RhbGxlZF9yb3V0ZRgK'
    'IAEoDEIIukgFegMYgCBSDmluc3RhbGxlZFJvdXRlEjQKEWluc3RhbGxlZF9ib290X2lkGAsgAS'
    'gJQgi6SAVyAxiAAVIPaW5zdGFsbGVkQm9vdElkEicKD2V4cGlyZWRfbGF0Y2hlZBgMIAEoCFIO'
    'ZXhwaXJlZExhdGNoZWQ=');

@$core.Deprecated('Use localStateDescriptor instead')
const LocalState$json = {
  '1': 'LocalState',
  '2': [
    {
      '1': 'desired',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.xtcp_treatment.v1.DesiredState',
      '10': 'desired'
    },
    {
      '1': 'locally_suspended',
      '3': 2,
      '4': 1,
      '5': 8,
      '10': 'locallySuspended'
    },
    {
      '1': 'suspension_reason',
      '3': 3,
      '4': 1,
      '5': 9,
      '8': {},
      '10': 'suspensionReason'
    },
    {
      '1': 'records',
      '3': 4,
      '4': 3,
      '5': 11,
      '6': '.xtcp_treatment.v1.ApplicationRecord',
      '8': {},
      '10': 'records'
    },
    {
      '1': 'updated_at',
      '3': 5,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'updatedAt'
    },
  ],
};

/// Descriptor for `LocalState`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List localStateDescriptor = $convert.base64Decode(
    'CgpMb2NhbFN0YXRlEjkKB2Rlc2lyZWQYASABKAsyHy54dGNwX3RyZWF0bWVudC52MS5EZXNpcm'
    'VkU3RhdGVSB2Rlc2lyZWQSKwoRbG9jYWxseV9zdXNwZW5kZWQYAiABKAhSEGxvY2FsbHlTdXNw'
    'ZW5kZWQSNQoRc3VzcGVuc2lvbl9yZWFzb24YAyABKAlCCLpIBXIDGIAIUhBzdXNwZW5zaW9uUm'
    'Vhc29uEkkKB3JlY29yZHMYBCADKAsyJC54dGNwX3RyZWF0bWVudC52MS5BcHBsaWNhdGlvblJl'
    'Y29yZEIJukgGkgEDEIBAUgdyZWNvcmRzEjkKCnVwZGF0ZWRfYXQYBSABKAsyGi5nb29nbGUucH'
    'JvdG9idWYuVGltZXN0YW1wUgl1cGRhdGVkQXQ=');

@$core.Deprecated('Use hostEvidenceDescriptor instead')
const HostEvidence$json = {
  '1': 'HostEvidence',
  '2': [
    {'1': 'agent_id', '3': 1, '4': 1, '5': 9, '8': {}, '10': 'agentId'},
    {'1': 'sockets', '3': 2, '4': 1, '5': 4, '10': 'sockets'},
    {'1': 'bytes_acked', '3': 3, '4': 1, '5': 4, '10': 'bytesAcked'},
    {'1': 'goodput_bps', '3': 4, '4': 1, '5': 1, '8': {}, '10': 'goodputBps'},
    {
      '1': 'retransmission_ratio',
      '3': 5,
      '4': 1,
      '5': 1,
      '8': {},
      '10': 'retransmissionRatio'
    },
    {'1': 'rtt_p95_ms', '3': 6, '4': 1, '5': 1, '8': {}, '10': 'rttP95Ms'},
    {'1': 'exposed_sockets', '3': 7, '4': 1, '5': 4, '10': 'exposedSockets'},
    {
      '1': 'collection_healthy',
      '3': 8,
      '4': 1,
      '5': 8,
      '10': 'collectionHealthy'
    },
  ],
  '7': {},
};

/// Descriptor for `HostEvidence`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List hostEvidenceDescriptor = $convert.base64Decode(
    'CgxIb3N0RXZpZGVuY2USRAoIYWdlbnRfaWQYASABKAlCKbpIJnIkMiJeW2EtekEtWjAtOV1bYS'
    '16QS1aMC05Xy4tXXswLDEyN30kUgdhZ2VudElkEhgKB3NvY2tldHMYAiABKARSB3NvY2tldHMS'
    'HwoLYnl0ZXNfYWNrZWQYAyABKARSCmJ5dGVzQWNrZWQSMQoLZ29vZHB1dF9icHMYBCABKAFCEL'
    'pIDRILQAEpAAAAAAAAAABSCmdvb2RwdXRCcHMSQwoUcmV0cmFuc21pc3Npb25fcmF0aW8YBSAB'
    'KAFCELpIDRILQAEpAAAAAAAAAABSE3JldHJhbnNtaXNzaW9uUmF0aW8SLgoKcnR0X3A5NV9tcx'
    'gGIAEoAUIQukgNEgtAASkAAAAAAAAAAFIIcnR0UDk1TXMSJwoPZXhwb3NlZF9zb2NrZXRzGAcg'
    'ASgEUg5leHBvc2VkU29ja2V0cxItChJjb2xsZWN0aW9uX2hlYWx0aHkYCCABKAhSEWNvbGxlY3'
    'Rpb25IZWFsdGh5Oni6SHUacwokaG9zdF9ldmlkZW5jZS5leHBvc2VkX3dpdGhpbl9zb2NrZXRz'
    'EiVleHBvc2VkX3NvY2tldHMgY2Fubm90IGV4Y2VlZCBzb2NrZXRzGiR0aGlzLmV4cG9zZWRfc2'
    '9ja2V0cyA8PSB0aGlzLnNvY2tldHM=');

@$core.Deprecated('Use evidenceDescriptor instead')
const Evidence$json = {
  '1': 'Evidence',
  '2': [
    {'1': 'id', '3': 1, '4': 1, '5': 9, '8': {}, '10': 'id'},
    {
      '1': 'analyzed_at',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '8': {},
      '10': 'analyzedAt'
    },
    {
      '1': 'window_start',
      '3': 3,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '8': {},
      '10': 'windowStart'
    },
    {
      '1': 'window_end',
      '3': 4,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '8': {},
      '10': 'windowEnd'
    },
    {
      '1': 'query_version',
      '3': 5,
      '4': 1,
      '5': 9,
      '8': {},
      '10': 'queryVersion'
    },
    {
      '1': 'enrichment_version',
      '3': 6,
      '4': 1,
      '5': 9,
      '8': {},
      '10': 'enrichmentVersion'
    },
    {
      '1': 'hosts',
      '3': 7,
      '4': 3,
      '5': 11,
      '6': '.xtcp_treatment.v1.HostEvidence',
      '8': {},
      '10': 'hosts'
    },
    {
      '1': 'artifact_sha256',
      '3': 8,
      '4': 1,
      '5': 9,
      '8': {},
      '10': 'artifactSha256'
    },
  ],
  '7': {},
};

/// Descriptor for `Evidence`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List evidenceDescriptor = $convert.base64Decode(
    'CghFdmlkZW5jZRI5CgJpZBgBIAEoCUIpukgmciQyIl5bYS16QS1aMC05XVthLXpBLVowLTlfLi'
    '1dezAsMTI3fSRSAmlkEkMKC2FuYWx5emVkX2F0GAIgASgLMhouZ29vZ2xlLnByb3RvYnVmLlRp'
    'bWVzdGFtcEIGukgDyAEBUgphbmFseXplZEF0EkUKDHdpbmRvd19zdGFydBgDIAEoCzIaLmdvb2'
    'dsZS5wcm90b2J1Zi5UaW1lc3RhbXBCBrpIA8gBAVILd2luZG93U3RhcnQSQQoKd2luZG93X2Vu'
    'ZBgEIAEoCzIaLmdvb2dsZS5wcm90b2J1Zi5UaW1lc3RhbXBCBrpIA8gBAVIJd2luZG93RW5kEi'
    '8KDXF1ZXJ5X3ZlcnNpb24YBSABKAlCCrpIB3IFEAEYgAJSDHF1ZXJ5VmVyc2lvbhI3ChJlbnJp'
    'Y2htZW50X3ZlcnNpb24YBiABKAlCCLpIBXIDGIACUhFlbnJpY2htZW50VmVyc2lvbhJCCgVob3'
    'N0cxgHIAMoCzIfLnh0Y3BfdHJlYXRtZW50LnYxLkhvc3RFdmlkZW5jZUILukgIkgEFCAEQkE5S'
    'BWhvc3RzEj4KD2FydGlmYWN0X3NoYTI1NhgIIAEoCUIVukgSchAyDl5bMC05YS1mXXs2NH0kUg'
    '5hcnRpZmFjdFNoYTI1Njq3AbpIswEasAEKEWV2aWRlbmNlLmludGVydmFsEk93aW5kb3dfZW5k'
    'IG11c3QgZm9sbG93IHdpbmRvd19zdGFydCBhbmQgYW5hbHl6ZWRfYXQgbXVzdCBub3QgcHJlY2'
    'VkZSB3aW5kb3dfZW5kGkp0aGlzLndpbmRvd19lbmQgPiB0aGlzLndpbmRvd19zdGFydCAmJiB0'
    'aGlzLmFuYWx5emVkX2F0ID49IHRoaXMud2luZG93X2VuZA==');

@$core.Deprecated('Use experimentPolicyDescriptor instead')
const ExperimentPolicy$json = {
  '1': 'ExperimentPolicy',
  '2': [
    {
      '1': 'minimum_observation',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Duration',
      '8': {},
      '10': 'minimumObservation'
    },
    {
      '1': 'maximum_phase',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Duration',
      '8': {},
      '10': 'maximumPhase'
    },
    {
      '1': 'minimum_sockets_per_host',
      '3': 3,
      '4': 1,
      '5': 4,
      '8': {},
      '10': 'minimumSocketsPerHost'
    },
    {
      '1': 'minimum_bytes_per_host',
      '3': 4,
      '4': 1,
      '5': 4,
      '8': {},
      '10': 'minimumBytesPerHost'
    },
    {
      '1': 'minimum_treated_hosts',
      '3': 5,
      '4': 1,
      '5': 13,
      '8': {},
      '10': 'minimumTreatedHosts'
    },
    {
      '1': 'minimum_improvement',
      '3': 6,
      '4': 1,
      '5': 1,
      '8': {},
      '10': 'minimumImprovement'
    },
    {
      '1': 'maximum_retransmission_increase',
      '3': 7,
      '4': 1,
      '5': 1,
      '8': {},
      '10': 'maximumRetransmissionIncrease'
    },
    {
      '1': 'maximum_rtt_increase',
      '3': 8,
      '4': 1,
      '5': 1,
      '8': {},
      '10': 'maximumRttIncrease'
    },
    {
      '1': 'maximum_evidence_age',
      '3': 9,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Duration',
      '8': {},
      '10': 'maximumEvidenceAge'
    },
  ],
  '7': {},
};

/// Descriptor for `ExperimentPolicy`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List experimentPolicyDescriptor = $convert.base64Decode(
    'ChBFeHBlcmltZW50UG9saWN5El0KE21pbmltdW1fb2JzZXJ2YXRpb24YASABKAsyGS5nb29nbG'
    'UucHJvdG9idWYuRHVyYXRpb25CEbpIDsgBAaoBCCIECID1JCoAUhJtaW5pbXVtT2JzZXJ2YXRp'
    'b24SUQoNbWF4aW11bV9waGFzZRgCIAEoCzIZLmdvb2dsZS5wcm90b2J1Zi5EdXJhdGlvbkIRuk'
    'gOyAEBqgEIIgQIgPUkKgBSDG1heGltdW1QaGFzZRJAChhtaW5pbXVtX3NvY2tldHNfcGVyX2hv'
    'c3QYAyABKARCB7pIBDICIABSFW1pbmltdW1Tb2NrZXRzUGVySG9zdBI8ChZtaW5pbXVtX2J5dG'
    'VzX3Blcl9ob3N0GAQgASgEQge6SAQyAiAAUhNtaW5pbXVtQnl0ZXNQZXJIb3N0Ej4KFW1pbmlt'
    'dW1fdHJlYXRlZF9ob3N0cxgFIAEoDUIKukgHKgUYkE4oAlITbWluaW11bVRyZWF0ZWRIb3N0cx'
    'JKChNtaW5pbXVtX2ltcHJvdmVtZW50GAYgASgBQhm6SBYSFEABGQAAAAAAAPA/KQAAAAAAAAAA'
    'UhJtaW5pbXVtSW1wcm92ZW1lbnQSYQofbWF4aW11bV9yZXRyYW5zbWlzc2lvbl9pbmNyZWFzZR'
    'gHIAEoAUIZukgWEhRAARkAAAAAAADwPykAAAAAAAAAAFIdbWF4aW11bVJldHJhbnNtaXNzaW9u'
    'SW5jcmVhc2USSwoUbWF4aW11bV9ydHRfaW5jcmVhc2UYCCABKAFCGbpIFhIUQAEZAAAAAAAA8D'
    '8pAAAAAAAAAABSEm1heGltdW1SdHRJbmNyZWFzZRJeChRtYXhpbXVtX2V2aWRlbmNlX2FnZRgJ'
    'IAEoCzIZLmdvb2dsZS5wcm90b2J1Zi5EdXJhdGlvbkIRukgOyAEBqgEIIgQIgPUkKgBSEm1heG'
    'ltdW1FdmlkZW5jZUFnZTqJAbpIhQEaggEKH3BvbGljeS5vYnNlcnZhdGlvbl93aXRoaW5fcGhh'
    'c2USL21pbmltdW1fb2JzZXJ2YXRpb24gY2Fubm90IGV4Y2VlZCBtYXhpbXVtX3BoYXNlGi50aG'
    'lzLm1pbmltdW1fb2JzZXJ2YXRpb24gPD0gdGhpcy5tYXhpbXVtX3BoYXNl');

@$core.Deprecated('Use phaseDescriptor instead')
const Phase$json = {
  '1': 'Phase',
  '2': [
    {
      '1': 'target_hosts',
      '3': 1,
      '4': 1,
      '5': 13,
      '8': {},
      '10': 'targetHosts'
    },
    {
      '1': 'issued_at',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'issuedAt'
    },
    {
      '1': 'observation_started_at',
      '3': 3,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'observationStartedAt'
    },
    {
      '1': 'evaluated_at',
      '3': 4,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'evaluatedAt'
    },
    {
      '1': 'evidence',
      '3': 5,
      '4': 1,
      '5': 11,
      '6': '.xtcp_treatment.v1.Evidence',
      '10': 'evidence'
    },
    {
      '1': 'outcome',
      '3': 6,
      '4': 1,
      '5': 14,
      '6': '.xtcp_treatment.v1.Outcome',
      '8': {},
      '10': 'outcome'
    },
    {'1': 'reviewer', '3': 7, '4': 1, '5': 9, '8': {}, '10': 'reviewer'},
    {'1': 'reason', '3': 8, '4': 1, '5': 9, '8': {}, '10': 'reason'},
  ],
};

/// Descriptor for `Phase`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List phaseDescriptor = $convert.base64Decode(
    'CgVQaGFzZRItCgx0YXJnZXRfaG9zdHMYASABKA1CCrpIByoFGJBOKAFSC3RhcmdldEhvc3RzEj'
    'cKCWlzc3VlZF9hdBgCIAEoCzIaLmdvb2dsZS5wcm90b2J1Zi5UaW1lc3RhbXBSCGlzc3VlZEF0'
    'ElAKFm9ic2VydmF0aW9uX3N0YXJ0ZWRfYXQYAyABKAsyGi5nb29nbGUucHJvdG9idWYuVGltZX'
    'N0YW1wUhRvYnNlcnZhdGlvblN0YXJ0ZWRBdBI9CgxldmFsdWF0ZWRfYXQYBCABKAsyGi5nb29n'
    'bGUucHJvdG9idWYuVGltZXN0YW1wUgtldmFsdWF0ZWRBdBI3CghldmlkZW5jZRgFIAEoCzIbLn'
    'h0Y3BfdHJlYXRtZW50LnYxLkV2aWRlbmNlUghldmlkZW5jZRI+CgdvdXRjb21lGAYgASgOMhou'
    'eHRjcF90cmVhdG1lbnQudjEuT3V0Y29tZUIIukgFggECEAFSB291dGNvbWUSJAoIcmV2aWV3ZX'
    'IYByABKAlCCLpIBXIDGIABUghyZXZpZXdlchIgCgZyZWFzb24YCCABKAlCCLpIBXIDGIAgUgZy'
    'ZWFzb24=');

@$core.Deprecated('Use experimentDescriptor instead')
const Experiment$json = {
  '1': 'Experiment',
  '2': [
    {'1': 'id', '3': 1, '4': 1, '5': 9, '8': {}, '10': 'id'},
    {'1': 'location', '3': 2, '4': 1, '5': 9, '8': {}, '10': 'location'},
    {'1': 'hypothesis', '3': 3, '4': 1, '5': 9, '8': {}, '10': 'hypothesis'},
    {
      '1': 'treatment',
      '3': 4,
      '4': 1,
      '5': 11,
      '6': '.xtcp_treatment.v1.Treatment',
      '8': {},
      '10': 'treatment'
    },
    {
      '1': 'policy',
      '3': 5,
      '4': 1,
      '5': 11,
      '6': '.xtcp_treatment.v1.ExperimentPolicy',
      '8': {},
      '10': 'policy'
    },
    {
      '1': 'baseline',
      '3': 6,
      '4': 1,
      '5': 11,
      '6': '.xtcp_treatment.v1.Evidence',
      '8': {},
      '10': 'baseline'
    },
    {'1': 'cohort_order', '3': 7, '4': 3, '5': 9, '8': {}, '10': 'cohortOrder'},
    {
      '1': 'phases',
      '3': 8,
      '4': 3,
      '5': 11,
      '6': '.xtcp_treatment.v1.Phase',
      '8': {},
      '10': 'phases'
    },
    {
      '1': 'status',
      '3': 9,
      '4': 1,
      '5': 14,
      '6': '.xtcp_treatment.v1.ExperimentStatus',
      '8': {},
      '10': 'status'
    },
    {'1': 'revision', '3': 10, '4': 1, '5': 4, '10': 'revision'},
    {
      '1': 'proposed_at',
      '3': 11,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'proposedAt'
    },
    {
      '1': 'latest_evidence',
      '3': 12,
      '4': 1,
      '5': 11,
      '6': '.xtcp_treatment.v1.Evidence',
      '10': 'latestEvidence'
    },
    {
      '1': 'latest_outcome',
      '3': 13,
      '4': 1,
      '5': 14,
      '6': '.xtcp_treatment.v1.Outcome',
      '8': {},
      '10': 'latestOutcome'
    },
    {
      '1': 'updated_at',
      '3': 14,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'updatedAt'
    },
  ],
};

/// Descriptor for `Experiment`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List experimentDescriptor = $convert.base64Decode(
    'CgpFeHBlcmltZW50EjkKAmlkGAEgASgJQim6SCZyJDIiXlthLXpBLVowLTldW2EtekEtWjAtOV'
    '8uLV17MCwxMjd9JFICaWQSRQoIbG9jYXRpb24YAiABKAlCKbpIJnIkMiJeW2EtekEtWjAtOV1b'
    'YS16QS1aMC05Xy4tXXswLDEyN30kUghsb2NhdGlvbhIqCgpoeXBvdGhlc2lzGAMgASgJQgq6SA'
    'dyBRABGIBAUgpoeXBvdGhlc2lzEkIKCXRyZWF0bWVudBgEIAEoCzIcLnh0Y3BfdHJlYXRtZW50'
    'LnYxLlRyZWF0bWVudEIGukgDyAEBUgl0cmVhdG1lbnQSQwoGcG9saWN5GAUgASgLMiMueHRjcF'
    '90cmVhdG1lbnQudjEuRXhwZXJpbWVudFBvbGljeUIGukgDyAEBUgZwb2xpY3kSPwoIYmFzZWxp'
    'bmUYBiABKAsyGy54dGNwX3RyZWF0bWVudC52MS5FdmlkZW5jZUIGukgDyAEBUghiYXNlbGluZR'
    'JYCgxjb2hvcnRfb3JkZXIYByADKAlCNbpIMpIBLwgDEJBOGAEiJnIkMiJeW2EtekEtWjAtOV1b'
    'YS16QS1aMC05Xy4tXXswLDEyN30kUgtjb2hvcnRPcmRlchI6CgZwaGFzZXMYCCADKAsyGC54dG'
    'NwX3RyZWF0bWVudC52MS5QaGFzZUIIukgFkgECEEBSBnBoYXNlcxJFCgZzdGF0dXMYCSABKA4y'
    'Iy54dGNwX3RyZWF0bWVudC52MS5FeHBlcmltZW50U3RhdHVzQgi6SAWCAQIQAVIGc3RhdHVzEh'
    'oKCHJldmlzaW9uGAogASgEUghyZXZpc2lvbhI7Cgtwcm9wb3NlZF9hdBgLIAEoCzIaLmdvb2ds'
    'ZS5wcm90b2J1Zi5UaW1lc3RhbXBSCnByb3Bvc2VkQXQSRAoPbGF0ZXN0X2V2aWRlbmNlGAwgAS'
    'gLMhsueHRjcF90cmVhdG1lbnQudjEuRXZpZGVuY2VSDmxhdGVzdEV2aWRlbmNlEksKDmxhdGVz'
    'dF9vdXRjb21lGA0gASgOMhoueHRjcF90cmVhdG1lbnQudjEuT3V0Y29tZUIIukgFggECEAFSDW'
    'xhdGVzdE91dGNvbWUSOQoKdXBkYXRlZF9hdBgOIAEoCzIaLmdvb2dsZS5wcm90b2J1Zi5UaW1l'
    'c3RhbXBSCXVwZGF0ZWRBdA==');
