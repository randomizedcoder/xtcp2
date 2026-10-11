// This is a generated file - do not edit.
//
// Generated from linkmonitor/v1/linkmonitor.proto.

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

@$core.Deprecated('Use eligibilityDescriptor instead')
const Eligibility$json = {
  '1': 'Eligibility',
  '2': [
    {'1': 'ELIGIBILITY_UNSPECIFIED', '2': 0},
    {'1': 'ELIGIBILITY_INCLUDED', '2': 1},
    {'1': 'ELIGIBILITY_EXCLUDED', '2': 2},
  ],
};

/// Descriptor for `Eligibility`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List eligibilityDescriptor = $convert.base64Decode(
    'CgtFbGlnaWJpbGl0eRIbChdFTElHSUJJTElUWV9VTlNQRUNJRklFRBAAEhgKFEVMSUdJQklMSV'
    'RZX0lOQ0xVREVEEAESGAoURUxJR0lCSUxJVFlfRVhDTFVERUQQAg==');

@$core.Deprecated('Use checkStatusDescriptor instead')
const CheckStatus$json = {
  '1': 'CheckStatus',
  '2': [
    {'1': 'CHECK_STATUS_UNSPECIFIED', '2': 0},
    {'1': 'CHECK_STATUS_PASS', '2': 1},
    {'1': 'CHECK_STATUS_FAIL', '2': 2},
    {'1': 'CHECK_STATUS_NOT_APPLICABLE', '2': 3},
  ],
};

/// Descriptor for `CheckStatus`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List checkStatusDescriptor = $convert.base64Decode(
    'CgtDaGVja1N0YXR1cxIcChhDSEVDS19TVEFUVVNfVU5TUEVDSUZJRUQQABIVChFDSEVDS19TVE'
    'FUVVNfUEFTUxABEhUKEUNIRUNLX1NUQVRVU19GQUlMEAISHwobQ0hFQ0tfU1RBVFVTX05PVF9B'
    'UFBMSUNBQkxFEAM=');

@$core.Deprecated('Use supportDescriptor instead')
const Support$json = {
  '1': 'Support',
  '2': [
    {'1': 'SUPPORT_UNSPECIFIED', '2': 0},
    {'1': 'SUPPORT_SUPPORTED', '2': 1},
    {'1': 'SUPPORT_UNSUPPORTED', '2': 2},
    {'1': 'SUPPORT_NOT_APPLICABLE', '2': 3},
  ],
};

/// Descriptor for `Support`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List supportDescriptor = $convert.base64Decode(
    'CgdTdXBwb3J0EhcKE1NVUFBPUlRfVU5TUEVDSUZJRUQQABIVChFTVVBQT1JUX1NVUFBPUlRFRB'
    'ABEhcKE1NVUFBPUlRfVU5TVVBQT1JURUQQAhIaChZTVVBQT1JUX05PVF9BUFBMSUNBQkxFEAM=');

@$core.Deprecated('Use healthDescriptor instead')
const Health$json = {
  '1': 'Health',
  '2': [
    {'1': 'running', '3': 1, '4': 1, '5': 8, '10': 'running'},
    {'1': 'ready', '3': 2, '4': 1, '5': 8, '10': 'ready'},
    {
      '1': 'collection_healthy',
      '3': 3,
      '4': 1,
      '5': 8,
      '10': 'collectionHealthy'
    },
    {'1': 'baseline_ready', '3': 4, '4': 1, '5': 8, '10': 'baselineReady'},
  ],
};

/// Descriptor for `Health`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List healthDescriptor = $convert.base64Decode(
    'CgZIZWFsdGgSGAoHcnVubmluZxgBIAEoCFIHcnVubmluZxIUCgVyZWFkeRgCIAEoCFIFcmVhZH'
    'kSLQoSY29sbGVjdGlvbl9oZWFsdGh5GAMgASgIUhFjb2xsZWN0aW9uSGVhbHRoeRIlCg5iYXNl'
    'bGluZV9yZWFkeRgEIAEoCFINYmFzZWxpbmVSZWFkeQ==');

@$core.Deprecated('Use linkCountsDescriptor instead')
const LinkCounts$json = {
  '1': 'LinkCounts',
  '2': [
    {
      '1': 'current',
      '3': 1,
      '4': 1,
      '5': 4,
      '9': 0,
      '10': 'current',
      '17': true
    },
    {
      '1': 'expected',
      '3': 2,
      '4': 1,
      '5': 4,
      '9': 1,
      '10': 'expected',
      '17': true
    },
    {'1': 'delta', '3': 3, '4': 1, '5': 18, '9': 2, '10': 'delta', '17': true},
  ],
  '8': [
    {'1': '_current'},
    {'1': '_expected'},
    {'1': '_delta'},
  ],
};

/// Descriptor for `LinkCounts`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List linkCountsDescriptor = $convert.base64Decode(
    'CgpMaW5rQ291bnRzEh0KB2N1cnJlbnQYASABKARIAFIHY3VycmVudIgBARIfCghleHBlY3RlZB'
    'gCIAEoBEgBUghleHBlY3RlZIgBARIZCgVkZWx0YRgDIAEoEkgCUgVkZWx0YYgBAUIKCghfY3Vy'
    'cmVudEILCglfZXhwZWN0ZWRCCAoGX2RlbHRh');

@$core.Deprecated('Use controlProgressDescriptor instead')
const ControlProgress$json = {
  '1': 'ControlProgress',
  '2': [
    {'1': 'requested', '3': 1, '4': 1, '5': 4, '10': 'requested'},
    {'1': 'completed', '3': 2, '4': 1, '5': 4, '10': 'completed'},
  ],
};

/// Descriptor for `ControlProgress`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List controlProgressDescriptor = $convert.base64Decode(
    'Cg9Db250cm9sUHJvZ3Jlc3MSHAoJcmVxdWVzdGVkGAEgASgEUglyZXF1ZXN0ZWQSHAoJY29tcG'
    'xldGVkGAIgASgEUgljb21wbGV0ZWQ=');

@$core.Deprecated('Use getStatusRequestDescriptor instead')
const GetStatusRequest$json = {
  '1': 'GetStatusRequest',
};

/// Descriptor for `GetStatusRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List getStatusRequestDescriptor =
    $convert.base64Decode('ChBHZXRTdGF0dXNSZXF1ZXN0');

@$core.Deprecated('Use getStatusResponseDescriptor instead')
const GetStatusResponse$json = {
  '1': 'GetStatusResponse',
  '2': [
    {'1': 'snapshot_version', '3': 1, '4': 1, '5': 4, '10': 'snapshotVersion'},
    {
      '1': 'health',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.linkmonitor.v1.Health',
      '10': 'health'
    },
    {
      '1': 'counts',
      '3': 3,
      '4': 1,
      '5': 11,
      '6': '.linkmonitor.v1.LinkCounts',
      '10': 'counts'
    },
    {
      '1': 'baseline_recorded_at',
      '3': 4,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'baselineRecordedAt'
    },
    {
      '1': 'baseline_write_errors',
      '3': 5,
      '4': 1,
      '5': 4,
      '10': 'baselineWriteErrors'
    },
    {
      '1': 'last_successful_resync',
      '3': 6,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'lastSuccessfulResync'
    },
    {
      '1': 'resync',
      '3': 7,
      '4': 1,
      '5': 11,
      '6': '.linkmonitor.v1.ControlProgress',
      '10': 'resync'
    },
    {
      '1': 'rebaseline',
      '3': 8,
      '4': 1,
      '5': 11,
      '6': '.linkmonitor.v1.ControlProgress',
      '10': 'rebaseline'
    },
  ],
};

/// Descriptor for `GetStatusResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List getStatusResponseDescriptor = $convert.base64Decode(
    'ChFHZXRTdGF0dXNSZXNwb25zZRIpChBzbmFwc2hvdF92ZXJzaW9uGAEgASgEUg9zbmFwc2hvdF'
    'ZlcnNpb24SLgoGaGVhbHRoGAIgASgLMhYubGlua21vbml0b3IudjEuSGVhbHRoUgZoZWFsdGgS'
    'MgoGY291bnRzGAMgASgLMhoubGlua21vbml0b3IudjEuTGlua0NvdW50c1IGY291bnRzEkwKFG'
    'Jhc2VsaW5lX3JlY29yZGVkX2F0GAQgASgLMhouZ29vZ2xlLnByb3RvYnVmLlRpbWVzdGFtcFIS'
    'YmFzZWxpbmVSZWNvcmRlZEF0EjIKFWJhc2VsaW5lX3dyaXRlX2Vycm9ycxgFIAEoBFITYmFzZW'
    'xpbmVXcml0ZUVycm9ycxJQChZsYXN0X3N1Y2Nlc3NmdWxfcmVzeW5jGAYgASgLMhouZ29vZ2xl'
    'LnByb3RvYnVmLlRpbWVzdGFtcFIUbGFzdFN1Y2Nlc3NmdWxSZXN5bmMSNwoGcmVzeW5jGAcgAS'
    'gLMh8ubGlua21vbml0b3IudjEuQ29udHJvbFByb2dyZXNzUgZyZXN5bmMSPwoKcmViYXNlbGlu'
    'ZRgIIAEoCzIfLmxpbmttb25pdG9yLnYxLkNvbnRyb2xQcm9ncmVzc1IKcmViYXNlbGluZQ==');

@$core.Deprecated('Use interfaceDescriptor instead')
const Interface$json = {
  '1': 'Interface',
  '2': [
    {'1': 'name', '3': 1, '4': 1, '5': 9, '10': 'name'},
    {'1': 'identity', '3': 2, '4': 1, '5': 9, '10': 'identity'},
    {'1': 'generation', '3': 3, '4': 1, '5': 4, '10': 'generation'},
    {'1': 'up', '3': 4, '4': 1, '5': 8, '9': 0, '10': 'up', '17': true},
    {
      '1': 'eligibility',
      '3': 5,
      '4': 1,
      '5': 14,
      '6': '.linkmonitor.v1.Eligibility',
      '10': 'eligibility'
    },
    {
      '1': 'maximum_speed',
      '3': 6,
      '4': 1,
      '5': 14,
      '6': '.linkmonitor.v1.CheckStatus',
      '10': 'maximumSpeed'
    },
    {
      '1': 'maximum_width',
      '3': 7,
      '4': 1,
      '5': 14,
      '6': '.linkmonitor.v1.CheckStatus',
      '10': 'maximumWidth'
    },
    {
      '1': 'full_duplex',
      '3': 8,
      '4': 1,
      '5': 14,
      '6': '.linkmonitor.v1.CheckStatus',
      '10': 'fullDuplex'
    },
    {
      '1': 'rdma_readiness',
      '3': 9,
      '4': 1,
      '5': 14,
      '6': '.linkmonitor.v1.CheckStatus',
      '10': 'rdmaReadiness'
    },
    {
      '1': 'maximum_speed_exception',
      '3': 10,
      '4': 1,
      '5': 8,
      '10': 'maximumSpeedException'
    },
    {'1': 'up_transitions', '3': 11, '4': 1, '5': 4, '10': 'upTransitions'},
    {'1': 'down_transitions', '3': 12, '4': 1, '5': 4, '10': 'downTransitions'},
  ],
  '8': [
    {'1': '_up'},
  ],
};

/// Descriptor for `Interface`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List interfaceDescriptor = $convert.base64Decode(
    'CglJbnRlcmZhY2USEgoEbmFtZRgBIAEoCVIEbmFtZRIaCghpZGVudGl0eRgCIAEoCVIIaWRlbn'
    'RpdHkSHgoKZ2VuZXJhdGlvbhgDIAEoBFIKZ2VuZXJhdGlvbhITCgJ1cBgEIAEoCEgAUgJ1cIgB'
    'ARI9CgtlbGlnaWJpbGl0eRgFIAEoDjIbLmxpbmttb25pdG9yLnYxLkVsaWdpYmlsaXR5UgtlbG'
    'lnaWJpbGl0eRJACg1tYXhpbXVtX3NwZWVkGAYgASgOMhsubGlua21vbml0b3IudjEuQ2hlY2tT'
    'dGF0dXNSDG1heGltdW1TcGVlZBJACg1tYXhpbXVtX3dpZHRoGAcgASgOMhsubGlua21vbml0b3'
    'IudjEuQ2hlY2tTdGF0dXNSDG1heGltdW1XaWR0aBI8CgtmdWxsX2R1cGxleBgIIAEoDjIbLmxp'
    'bmttb25pdG9yLnYxLkNoZWNrU3RhdHVzUgpmdWxsRHVwbGV4EkIKDnJkbWFfcmVhZGluZXNzGA'
    'kgASgOMhsubGlua21vbml0b3IudjEuQ2hlY2tTdGF0dXNSDXJkbWFSZWFkaW5lc3MSNgoXbWF4'
    'aW11bV9zcGVlZF9leGNlcHRpb24YCiABKAhSFW1heGltdW1TcGVlZEV4Y2VwdGlvbhIlCg51cF'
    '90cmFuc2l0aW9ucxgLIAEoBFINdXBUcmFuc2l0aW9ucxIpChBkb3duX3RyYW5zaXRpb25zGAwg'
    'ASgEUg9kb3duVHJhbnNpdGlvbnNCBQoDX3Vw');

@$core.Deprecated('Use listInterfacesRequestDescriptor instead')
const ListInterfacesRequest$json = {
  '1': 'ListInterfacesRequest',
  '2': [
    {'1': 'names', '3': 1, '4': 3, '5': 9, '8': {}, '10': 'names'},
    {'1': 'eligible_only', '3': 2, '4': 1, '5': 8, '10': 'eligibleOnly'},
  ],
};

/// Descriptor for `ListInterfacesRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listInterfacesRequestDescriptor = $convert.base64Decode(
    'ChVMaXN0SW50ZXJmYWNlc1JlcXVlc3QSJwoFbmFtZXMYASADKAlCEbpIDpIBCxCAAiIGcgQQAR'
    'hAUgVuYW1lcxIjCg1lbGlnaWJsZV9vbmx5GAIgASgIUgxlbGlnaWJsZU9ubHk=');

@$core.Deprecated('Use listInterfacesResponseDescriptor instead')
const ListInterfacesResponse$json = {
  '1': 'ListInterfacesResponse',
  '2': [
    {'1': 'snapshot_version', '3': 1, '4': 1, '5': 4, '10': 'snapshotVersion'},
    {
      '1': 'interfaces',
      '3': 2,
      '4': 3,
      '5': 11,
      '6': '.linkmonitor.v1.Interface',
      '10': 'interfaces'
    },
  ],
};

/// Descriptor for `ListInterfacesResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listInterfacesResponseDescriptor = $convert.base64Decode(
    'ChZMaXN0SW50ZXJmYWNlc1Jlc3BvbnNlEikKEHNuYXBzaG90X3ZlcnNpb24YASABKARSD3NuYX'
    'BzaG90VmVyc2lvbhI5CgppbnRlcmZhY2VzGAIgAygLMhkubGlua21vbml0b3IudjEuSW50ZXJm'
    'YWNlUgppbnRlcmZhY2Vz');

@$core.Deprecated('Use collectorDescriptor instead')
const Collector$json = {
  '1': 'Collector',
  '2': [
    {'1': 'interface', '3': 1, '4': 1, '5': 9, '10': 'interface'},
    {'1': 'name', '3': 2, '4': 1, '5': 9, '10': 'name'},
    {
      '1': 'support',
      '3': 3,
      '4': 1,
      '5': 14,
      '6': '.linkmonitor.v1.Support',
      '10': 'support'
    },
    {
      '1': 'success',
      '3': 4,
      '4': 1,
      '5': 8,
      '9': 0,
      '10': 'success',
      '17': true
    },
    {'1': 'fresh', '3': 5, '4': 1, '5': 8, '10': 'fresh'},
    {
      '1': 'last_success',
      '3': 6,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'lastSuccess'
    },
    {
      '1': 'duration',
      '3': 7,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Duration',
      '10': 'duration'
    },
    {'1': 'error_reason', '3': 8, '4': 1, '5': 9, '10': 'errorReason'},
    {
      '1': 'errors',
      '3': 9,
      '4': 3,
      '5': 11,
      '6': '.linkmonitor.v1.Collector.ErrorsEntry',
      '10': 'errors'
    },
    {
      '1': 'omissions',
      '3': 10,
      '4': 3,
      '5': 11,
      '6': '.linkmonitor.v1.Collector.OmissionsEntry',
      '10': 'omissions'
    },
    {'1': 'discontinuities', '3': 11, '4': 1, '5': 4, '10': 'discontinuities'},
  ],
  '3': [Collector_ErrorsEntry$json, Collector_OmissionsEntry$json],
  '8': [
    {'1': '_success'},
  ],
};

@$core.Deprecated('Use collectorDescriptor instead')
const Collector_ErrorsEntry$json = {
  '1': 'ErrorsEntry',
  '2': [
    {'1': 'key', '3': 1, '4': 1, '5': 9, '10': 'key'},
    {'1': 'value', '3': 2, '4': 1, '5': 4, '10': 'value'},
  ],
  '7': {'7': true},
};

@$core.Deprecated('Use collectorDescriptor instead')
const Collector_OmissionsEntry$json = {
  '1': 'OmissionsEntry',
  '2': [
    {'1': 'key', '3': 1, '4': 1, '5': 9, '10': 'key'},
    {'1': 'value', '3': 2, '4': 1, '5': 4, '10': 'value'},
  ],
  '7': {'7': true},
};

/// Descriptor for `Collector`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List collectorDescriptor = $convert.base64Decode(
    'CglDb2xsZWN0b3ISHAoJaW50ZXJmYWNlGAEgASgJUglpbnRlcmZhY2USEgoEbmFtZRgCIAEoCV'
    'IEbmFtZRIxCgdzdXBwb3J0GAMgASgOMhcubGlua21vbml0b3IudjEuU3VwcG9ydFIHc3VwcG9y'
    'dBIdCgdzdWNjZXNzGAQgASgISABSB3N1Y2Nlc3OIAQESFAoFZnJlc2gYBSABKAhSBWZyZXNoEj'
    '0KDGxhc3Rfc3VjY2VzcxgGIAEoCzIaLmdvb2dsZS5wcm90b2J1Zi5UaW1lc3RhbXBSC2xhc3RT'
    'dWNjZXNzEjUKCGR1cmF0aW9uGAcgASgLMhkuZ29vZ2xlLnByb3RvYnVmLkR1cmF0aW9uUghkdX'
    'JhdGlvbhIhCgxlcnJvcl9yZWFzb24YCCABKAlSC2Vycm9yUmVhc29uEj0KBmVycm9ycxgJIAMo'
    'CzIlLmxpbmttb25pdG9yLnYxLkNvbGxlY3Rvci5FcnJvcnNFbnRyeVIGZXJyb3JzEkYKCW9taX'
    'NzaW9ucxgKIAMoCzIoLmxpbmttb25pdG9yLnYxLkNvbGxlY3Rvci5PbWlzc2lvbnNFbnRyeVIJ'
    'b21pc3Npb25zEigKD2Rpc2NvbnRpbnVpdGllcxgLIAEoBFIPZGlzY29udGludWl0aWVzGjkKC0'
    'Vycm9yc0VudHJ5EhAKA2tleRgBIAEoCVIDa2V5EhQKBXZhbHVlGAIgASgEUgV2YWx1ZToCOAEa'
    'PAoOT21pc3Npb25zRW50cnkSEAoDa2V5GAEgASgJUgNrZXkSFAoFdmFsdWUYAiABKARSBXZhbH'
    'VlOgI4AUIKCghfc3VjY2Vzcw==');

@$core.Deprecated('Use listCollectorsRequestDescriptor instead')
const ListCollectorsRequest$json = {
  '1': 'ListCollectorsRequest',
  '2': [
    {'1': 'names', '3': 1, '4': 3, '5': 9, '8': {}, '10': 'names'},
  ],
};

/// Descriptor for `ListCollectorsRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listCollectorsRequestDescriptor = $convert.base64Decode(
    'ChVMaXN0Q29sbGVjdG9yc1JlcXVlc3QSJwoFbmFtZXMYASADKAlCEbpIDpIBCxCAAiIGcgQQAR'
    'hAUgVuYW1lcw==');

@$core.Deprecated('Use listCollectorsResponseDescriptor instead')
const ListCollectorsResponse$json = {
  '1': 'ListCollectorsResponse',
  '2': [
    {'1': 'snapshot_version', '3': 1, '4': 1, '5': 4, '10': 'snapshotVersion'},
    {
      '1': 'collectors',
      '3': 2,
      '4': 3,
      '5': 11,
      '6': '.linkmonitor.v1.Collector',
      '10': 'collectors'
    },
  ],
};

/// Descriptor for `ListCollectorsResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listCollectorsResponseDescriptor = $convert.base64Decode(
    'ChZMaXN0Q29sbGVjdG9yc1Jlc3BvbnNlEikKEHNuYXBzaG90X3ZlcnNpb24YASABKARSD3NuYX'
    'BzaG90VmVyc2lvbhI5Cgpjb2xsZWN0b3JzGAIgAygLMhkubGlua21vbml0b3IudjEuQ29sbGVj'
    'dG9yUgpjb2xsZWN0b3Jz');

@$core.Deprecated('Use rebaselineRequestDescriptor instead')
const RebaselineRequest$json = {
  '1': 'RebaselineRequest',
  '2': [
    {'1': 'reason', '3': 1, '4': 1, '5': 9, '8': {}, '10': 'reason'},
    {
      '1': 'wait',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Duration',
      '8': {},
      '10': 'wait'
    },
  ],
};

/// Descriptor for `RebaselineRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List rebaselineRequestDescriptor = $convert.base64Decode(
    'ChFSZWJhc2VsaW5lUmVxdWVzdBIiCgZyZWFzb24YASABKAlCCrpIB3IFEAEogARSBnJlYXNvbh'
    'I7CgR3YWl0GAIgASgLMhkuZ29vZ2xlLnByb3RvYnVmLkR1cmF0aW9uQgy6SAmqAQYiAgg8MgBS'
    'BHdhaXQ=');

@$core.Deprecated('Use rebaselineResponseDescriptor instead')
const RebaselineResponse$json = {
  '1': 'RebaselineResponse',
  '2': [
    {'1': 'generation', '3': 1, '4': 1, '5': 4, '10': 'generation'},
    {'1': 'completed', '3': 2, '4': 1, '5': 8, '10': 'completed'},
    {
      '1': 'expected',
      '3': 3,
      '4': 1,
      '5': 4,
      '9': 0,
      '10': 'expected',
      '17': true
    },
    {
      '1': 'baseline_write_errors',
      '3': 4,
      '4': 1,
      '5': 4,
      '10': 'baselineWriteErrors'
    },
  ],
  '8': [
    {'1': '_expected'},
  ],
};

/// Descriptor for `RebaselineResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List rebaselineResponseDescriptor = $convert.base64Decode(
    'ChJSZWJhc2VsaW5lUmVzcG9uc2USHgoKZ2VuZXJhdGlvbhgBIAEoBFIKZ2VuZXJhdGlvbhIcCg'
    'ljb21wbGV0ZWQYAiABKAhSCWNvbXBsZXRlZBIfCghleHBlY3RlZBgDIAEoBEgAUghleHBlY3Rl'
    'ZIgBARIyChViYXNlbGluZV93cml0ZV9lcnJvcnMYBCABKARSE2Jhc2VsaW5lV3JpdGVFcnJvcn'
    'NCCwoJX2V4cGVjdGVk');

@$core.Deprecated('Use resyncRequestDescriptor instead')
const ResyncRequest$json = {
  '1': 'ResyncRequest',
  '2': [
    {'1': 'reason', '3': 1, '4': 1, '5': 9, '8': {}, '10': 'reason'},
    {
      '1': 'wait',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Duration',
      '8': {},
      '10': 'wait'
    },
  ],
};

/// Descriptor for `ResyncRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List resyncRequestDescriptor = $convert.base64Decode(
    'Cg1SZXN5bmNSZXF1ZXN0EiIKBnJlYXNvbhgBIAEoCUIKukgHcgUQASiABFIGcmVhc29uEjsKBH'
    'dhaXQYAiABKAsyGS5nb29nbGUucHJvdG9idWYuRHVyYXRpb25CDLpICaoBBiICCDwyAFIEd2Fp'
    'dA==');

@$core.Deprecated('Use resyncResponseDescriptor instead')
const ResyncResponse$json = {
  '1': 'ResyncResponse',
  '2': [
    {'1': 'generation', '3': 1, '4': 1, '5': 4, '10': 'generation'},
    {'1': 'completed', '3': 2, '4': 1, '5': 8, '10': 'completed'},
    {
      '1': 'last_successful_resync',
      '3': 3,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'lastSuccessfulResync'
    },
  ],
};

/// Descriptor for `ResyncResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List resyncResponseDescriptor = $convert.base64Decode(
    'Cg5SZXN5bmNSZXNwb25zZRIeCgpnZW5lcmF0aW9uGAEgASgEUgpnZW5lcmF0aW9uEhwKCWNvbX'
    'BsZXRlZBgCIAEoCFIJY29tcGxldGVkElAKFmxhc3Rfc3VjY2Vzc2Z1bF9yZXN5bmMYAyABKAsy'
    'Gi5nb29nbGUucHJvdG9idWYuVGltZXN0YW1wUhRsYXN0U3VjY2Vzc2Z1bFJlc3luYw==');
