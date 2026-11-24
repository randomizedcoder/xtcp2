// This is a generated file - do not edit.
//
// Generated from xtcp_config/v1/xtcp_config.proto.

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

@$core.Deprecated('Use listenerNetworkDescriptor instead')
const ListenerNetwork$json = {
  '1': 'ListenerNetwork',
  '2': [
    {'1': 'LISTENER_NETWORK_UNSPECIFIED', '2': 0},
    {'1': 'LISTENER_NETWORK_TCP', '2': 1},
    {'1': 'LISTENER_NETWORK_UNIX', '2': 2},
  ],
};

/// Descriptor for `ListenerNetwork`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List listenerNetworkDescriptor = $convert.base64Decode(
    'Cg9MaXN0ZW5lck5ldHdvcmsSIAocTElTVEVORVJfTkVUV09SS19VTlNQRUNJRklFRBAAEhgKFE'
    'xJU1RFTkVSX05FVFdPUktfVENQEAESGQoVTElTVEVORVJfTkVUV09SS19VTklYEAI=');

@$core.Deprecated('Use listenerAuthModeDescriptor instead')
const ListenerAuthMode$json = {
  '1': 'ListenerAuthMode',
  '2': [
    {'1': 'LISTENER_AUTH_MODE_UNSPECIFIED', '2': 0},
    {'1': 'LISTENER_AUTH_MODE_DISABLED', '2': 1},
    {'1': 'LISTENER_AUTH_MODE_RAW_TOKEN', '2': 2},
    {'1': 'LISTENER_AUTH_MODE_HMAC_UTC_MINUTE', '2': 3},
  ],
};

/// Descriptor for `ListenerAuthMode`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List listenerAuthModeDescriptor = $convert.base64Decode(
    'ChBMaXN0ZW5lckF1dGhNb2RlEiIKHkxJU1RFTkVSX0FVVEhfTU9ERV9VTlNQRUNJRklFRBAAEh'
    '8KG0xJU1RFTkVSX0FVVEhfTU9ERV9ESVNBQkxFRBABEiAKHExJU1RFTkVSX0FVVEhfTU9ERV9S'
    'QVdfVE9LRU4QAhImCiJMSVNURU5FUl9BVVRIX01PREVfSE1BQ19VVENfTUlOVVRFEAM=');

@$core.Deprecated('Use endpointClassDescriptor instead')
const EndpointClass$json = {
  '1': 'EndpointClass',
  '2': [
    {'1': 'ENDPOINT_CLASS_UNSPECIFIED', '2': 0},
    {'1': 'ENDPOINT_CLASS_HEALTH', '2': 1},
    {'1': 'ENDPOINT_CLASS_METRICS', '2': 2},
    {'1': 'ENDPOINT_CLASS_METRICS_DETAIL', '2': 3},
    {'1': 'ENDPOINT_CLASS_IP', '2': 4},
    {'1': 'ENDPOINT_CLASS_DEBUG', '2': 5},
    {'1': 'ENDPOINT_CLASS_GRPC_READ', '2': 6},
    {'1': 'ENDPOINT_CLASS_GRPC_ADMIN', '2': 7},
  ],
};

/// Descriptor for `EndpointClass`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List endpointClassDescriptor = $convert.base64Decode(
    'Cg1FbmRwb2ludENsYXNzEh4KGkVORFBPSU5UX0NMQVNTX1VOU1BFQ0lGSUVEEAASGQoVRU5EUE'
    '9JTlRfQ0xBU1NfSEVBTFRIEAESGgoWRU5EUE9JTlRfQ0xBU1NfTUVUUklDUxACEiEKHUVORFBP'
    'SU5UX0NMQVNTX01FVFJJQ1NfREVUQUlMEAMSFQoRRU5EUE9JTlRfQ0xBU1NfSVAQBBIYChRFTk'
    'RQT0lOVF9DTEFTU19ERUJVRxAFEhwKGEVORFBPSU5UX0NMQVNTX0dSUENfUkVBRBAGEh0KGUVO'
    'RFBPSU5UX0NMQVNTX0dSUENfQURNSU4QBw==');

@$core.Deprecated('Use getRequestDescriptor instead')
const GetRequest$json = {
  '1': 'GetRequest',
};

/// Descriptor for `GetRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List getRequestDescriptor =
    $convert.base64Decode('CgpHZXRSZXF1ZXN0');

@$core.Deprecated('Use getResponseDescriptor instead')
const GetResponse$json = {
  '1': 'GetResponse',
  '2': [
    {
      '1': 'config',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.xtcp_config.v1.XtcpConfig',
      '10': 'config'
    },
  ],
};

/// Descriptor for `GetResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List getResponseDescriptor = $convert.base64Decode(
    'CgtHZXRSZXNwb25zZRIyCgZjb25maWcYASABKAsyGi54dGNwX2NvbmZpZy52MS5YdGNwQ29uZm'
    'lnUgZjb25maWc=');

@$core.Deprecated('Use setRequestDescriptor instead')
const SetRequest$json = {
  '1': 'SetRequest',
  '2': [
    {
      '1': 'config',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.xtcp_config.v1.XtcpConfig',
      '10': 'config'
    },
  ],
};

/// Descriptor for `SetRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List setRequestDescriptor = $convert.base64Decode(
    'CgpTZXRSZXF1ZXN0EjIKBmNvbmZpZxgBIAEoCzIaLnh0Y3BfY29uZmlnLnYxLlh0Y3BDb25maW'
    'dSBmNvbmZpZw==');

@$core.Deprecated('Use setResponseDescriptor instead')
const SetResponse$json = {
  '1': 'SetResponse',
  '2': [
    {
      '1': 'config',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.xtcp_config.v1.XtcpConfig',
      '10': 'config'
    },
  ],
};

/// Descriptor for `SetResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List setResponseDescriptor = $convert.base64Decode(
    'CgtTZXRSZXNwb25zZRIyCgZjb25maWcYASABKAsyGi54dGNwX2NvbmZpZy52MS5YdGNwQ29uZm'
    'lnUgZjb25maWc=');

@$core.Deprecated('Use setPollFrequencyRequestDescriptor instead')
const SetPollFrequencyRequest$json = {
  '1': 'SetPollFrequencyRequest',
  '2': [
    {
      '1': 'poll_frequency',
      '3': 20,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Duration',
      '8': {},
      '10': 'pollFrequency'
    },
    {
      '1': 'poll_timeout',
      '3': 30,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Duration',
      '8': {},
      '10': 'pollTimeout'
    },
  ],
  '7': {},
};

/// Descriptor for `SetPollFrequencyRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List setPollFrequencyRequestDescriptor = $convert.base64Decode(
    'ChdTZXRQb2xsRnJlcXVlbmN5UmVxdWVzdBJVCg5wb2xsX2ZyZXF1ZW5jeRgUIAEoCzIZLmdvb2'
    'dsZS5wcm90b2J1Zi5EdXJhdGlvbkITukgQyAEBqgEKIgQIgPUkMgIIAVINcG9sbEZyZXF1ZW5j'
    'eRJPCgxwb2xsX3RpbWVvdXQYHiABKAsyGS5nb29nbGUucHJvdG9idWYuRHVyYXRpb25CEbpIDs'
    'gBAaoBCCIECID1JDIAUgtwb2xsVGltZW91dDpzukhwGm4KD1h0Y3BDb25maWcucG9sbBIyUG9s'
    'bCB0aW1lb3V0IG11c3QgYmUgbGVzcyB0aGFuIHBvbGwgcG9sbF9mcmVxdWVuY3kaJ3RoaXMucG'
    '9sbF90aW1lb3V0IDwgdGhpcy5wb2xsX2ZyZXF1ZW5jeQ==');

@$core.Deprecated('Use setPollFrequencyResponseDescriptor instead')
const SetPollFrequencyResponse$json = {
  '1': 'SetPollFrequencyResponse',
  '2': [
    {
      '1': 'config',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.xtcp_config.v1.XtcpConfig',
      '10': 'config'
    },
  ],
};

/// Descriptor for `SetPollFrequencyResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List setPollFrequencyResponseDescriptor =
    $convert.base64Decode(
        'ChhTZXRQb2xsRnJlcXVlbmN5UmVzcG9uc2USMgoGY29uZmlnGAEgASgLMhoueHRjcF9jb25maW'
        'cudjEuWHRjcENvbmZpZ1IGY29uZmln');

@$core.Deprecated('Use triggerPollRequestDescriptor instead')
const TriggerPollRequest$json = {
  '1': 'TriggerPollRequest',
};

/// Descriptor for `TriggerPollRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List triggerPollRequestDescriptor =
    $convert.base64Decode('ChJUcmlnZ2VyUG9sbFJlcXVlc3Q=');

@$core.Deprecated('Use triggerPollResponseDescriptor instead')
const TriggerPollResponse$json = {
  '1': 'TriggerPollResponse',
};

/// Descriptor for `TriggerPollResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List triggerPollResponseDescriptor =
    $convert.base64Decode('ChNUcmlnZ2VyUG9sbFJlc3BvbnNl');

@$core.Deprecated('Use triggerPollBurstRequestDescriptor instead')
const TriggerPollBurstRequest$json = {
  '1': 'TriggerPollBurstRequest',
  '2': [
    {'1': 'count', '3': 10, '4': 1, '5': 13, '8': {}, '10': 'count'},
    {
      '1': 'interval',
      '3': 20,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Duration',
      '8': {},
      '10': 'interval'
    },
  ],
};

/// Descriptor for `TriggerPollBurstRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List triggerPollBurstRequestDescriptor = $convert.base64Decode(
    'ChdUcmlnZ2VyUG9sbEJ1cnN0UmVxdWVzdBIjCgVjb3VudBgKIAEoDUINukgKyAEBKgUY6AcoAV'
    'IFY291bnQSSQoIaW50ZXJ2YWwYFCABKAsyGS5nb29nbGUucHJvdG9idWYuRHVyYXRpb25CErpI'
    'D8gBAaoBCSIDCJAcMgIIAVIIaW50ZXJ2YWw=');

@$core.Deprecated('Use triggerPollBurstResponseDescriptor instead')
const TriggerPollBurstResponse$json = {
  '1': 'TriggerPollBurstResponse',
  '2': [
    {'1': 'count', '3': 10, '4': 1, '5': 13, '10': 'count'},
    {
      '1': 'interval',
      '3': 20,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Duration',
      '10': 'interval'
    },
  ],
};

/// Descriptor for `TriggerPollBurstResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List triggerPollBurstResponseDescriptor =
    $convert.base64Decode(
        'ChhUcmlnZ2VyUG9sbEJ1cnN0UmVzcG9uc2USFAoFY291bnQYCiABKA1SBWNvdW50EjUKCGludG'
        'VydmFsGBQgASgLMhkuZ29vZ2xlLnByb3RvYnVmLkR1cmF0aW9uUghpbnRlcnZhbA==');

@$core.Deprecated('Use setS3UploadRequestDescriptor instead')
const SetS3UploadRequest$json = {
  '1': 'SetS3UploadRequest',
  '2': [
    {
      '1': 's3_flush_interval',
      '3': 10,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Duration',
      '8': {},
      '10': 's3FlushInterval'
    },
    {
      '1': 's3_parquet_flush_threshold_bytes',
      '3': 20,
      '4': 1,
      '5': 13,
      '8': {},
      '10': 's3ParquetFlushThresholdBytes'
    },
  ],
  '7': {},
};

/// Descriptor for `SetS3UploadRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List setS3UploadRequestDescriptor = $convert.base64Decode(
    'ChJTZXRTM1VwbG9hZFJlcXVlc3QSUgoRczNfZmx1c2hfaW50ZXJ2YWwYCiABKAsyGS5nb29nbG'
    'UucHJvdG9idWYuRHVyYXRpb25CC7pICMgBAKoBAjIAUg9zM0ZsdXNoSW50ZXJ2YWwSTgogczNf'
    'cGFycXVldF9mbHVzaF90aHJlc2hvbGRfYnl0ZXMYFCABKA1CBrpIA8gBAFIcczNQYXJxdWV0Rm'
    'x1c2hUaHJlc2hvbGRCeXRlczqoAbpIpAEaoQEKFlNldFMzVXBsb2FkLmF0TGVhc3RPbmUSPXNl'
    'dCBzM19mbHVzaF9pbnRlcnZhbCBhbmQvb3IgczNfcGFycXVldF9mbHVzaF90aHJlc2hvbGRfYn'
    'l0ZXMaSGhhcyh0aGlzLnMzX2ZsdXNoX2ludGVydmFsKSB8fCB0aGlzLnMzX3BhcnF1ZXRfZmx1'
    'c2hfdGhyZXNob2xkX2J5dGVzID4gMA==');

@$core.Deprecated('Use setS3UploadResponseDescriptor instead')
const SetS3UploadResponse$json = {
  '1': 'SetS3UploadResponse',
  '2': [
    {
      '1': 'config',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.xtcp_config.v1.XtcpConfig',
      '10': 'config'
    },
  ],
};

/// Descriptor for `SetS3UploadResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List setS3UploadResponseDescriptor = $convert.base64Decode(
    'ChNTZXRTM1VwbG9hZFJlc3BvbnNlEjIKBmNvbmZpZxgBIAEoCzIaLnh0Y3BfY29uZmlnLnYxLl'
    'h0Y3BDb25maWdSBmNvbmZpZw==');

@$core.Deprecated('Use setEnvelopeFlushRequestDescriptor instead')
const SetEnvelopeFlushRequest$json = {
  '1': 'SetEnvelopeFlushRequest',
  '2': [
    {
      '1': 'envelope_flush_threshold_bytes',
      '3': 10,
      '4': 1,
      '5': 13,
      '8': {},
      '10': 'envelopeFlushThresholdBytes'
    },
    {
      '1': 'envelope_flush_threshold_rows',
      '3': 20,
      '4': 1,
      '5': 13,
      '8': {},
      '10': 'envelopeFlushThresholdRows'
    },
  ],
  '7': {},
};

/// Descriptor for `SetEnvelopeFlushRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List setEnvelopeFlushRequestDescriptor = $convert.base64Decode(
    'ChdTZXRFbnZlbG9wZUZsdXNoUmVxdWVzdBJLCh5lbnZlbG9wZV9mbHVzaF90aHJlc2hvbGRfYn'
    'l0ZXMYCiABKA1CBrpIA8gBAFIbZW52ZWxvcGVGbHVzaFRocmVzaG9sZEJ5dGVzEkkKHWVudmVs'
    'b3BlX2ZsdXNoX3RocmVzaG9sZF9yb3dzGBQgASgNQga6SAPIAQBSGmVudmVsb3BlRmx1c2hUaH'
    'Jlc2hvbGRSb3dzOsABuki8ARq5AQobU2V0RW52ZWxvcGVGbHVzaC5hdExlYXN0T25lEkdzZXQg'
    'ZW52ZWxvcGVfZmx1c2hfdGhyZXNob2xkX2J5dGVzIGFuZC9vciBlbnZlbG9wZV9mbHVzaF90aH'
    'Jlc2hvbGRfcm93cxpRdGhpcy5lbnZlbG9wZV9mbHVzaF90aHJlc2hvbGRfYnl0ZXMgPiAwIHx8'
    'IHRoaXMuZW52ZWxvcGVfZmx1c2hfdGhyZXNob2xkX3Jvd3MgPiAw');

@$core.Deprecated('Use setEnvelopeFlushResponseDescriptor instead')
const SetEnvelopeFlushResponse$json = {
  '1': 'SetEnvelopeFlushResponse',
  '2': [
    {
      '1': 'config',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.xtcp_config.v1.XtcpConfig',
      '10': 'config'
    },
  ],
};

/// Descriptor for `SetEnvelopeFlushResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List setEnvelopeFlushResponseDescriptor =
    $convert.base64Decode(
        'ChhTZXRFbnZlbG9wZUZsdXNoUmVzcG9uc2USMgoGY29uZmlnGAEgASgLMhoueHRjcF9jb25maW'
        'cudjEuWHRjcENvbmZpZ1IGY29uZmln');

@$core.Deprecated('Use listenerEndpointDescriptor instead')
const ListenerEndpoint$json = {
  '1': 'ListenerEndpoint',
  '2': [
    {
      '1': 'network',
      '3': 1,
      '4': 1,
      '5': 14,
      '6': '.xtcp_config.v1.ListenerNetwork',
      '8': {},
      '10': 'network'
    },
    {'1': 'address', '3': 2, '4': 1, '5': 9, '8': {}, '10': 'address'},
    {
      '1': 'unix_socket_mode',
      '3': 3,
      '4': 1,
      '5': 13,
      '8': {},
      '10': 'unixSocketMode'
    },
    {
      '1': 'unlink_stale_unix_socket',
      '3': 4,
      '4': 1,
      '5': 8,
      '10': 'unlinkStaleUnixSocket'
    },
    {
      '1': 'max_connections',
      '3': 5,
      '4': 1,
      '5': 13,
      '8': {},
      '10': 'maxConnections'
    },
    {
      '1': 'accept_rate_per_second',
      '3': 6,
      '4': 1,
      '5': 13,
      '8': {},
      '10': 'acceptRatePerSecond'
    },
    {
      '1': 'accept_burst',
      '3': 7,
      '4': 1,
      '5': 13,
      '8': {},
      '10': 'acceptBurst'
    },
  ],
};

/// Descriptor for `ListenerEndpoint`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listenerEndpointDescriptor = $convert.base64Decode(
    'ChBMaXN0ZW5lckVuZHBvaW50EkMKB25ldHdvcmsYASABKA4yHy54dGNwX2NvbmZpZy52MS5MaX'
    'N0ZW5lck5ldHdvcmtCCLpIBYIBAhABUgduZXR3b3JrEiIKB2FkZHJlc3MYAiABKAlCCLpIBXID'
    'GIAEUgdhZGRyZXNzEjIKEHVuaXhfc29ja2V0X21vZGUYAyABKA1CCLpIBSoDGP8DUg51bml4U2'
    '9ja2V0TW9kZRI3Chh1bmxpbmtfc3RhbGVfdW5peF9zb2NrZXQYBCABKAhSFXVubGlua1N0YWxl'
    'VW5peFNvY2tldBIyCg9tYXhfY29ubmVjdGlvbnMYBSABKA1CCbpIBioEGKCNBlIObWF4Q29ubm'
    'VjdGlvbnMSPgoWYWNjZXB0X3JhdGVfcGVyX3NlY29uZBgGIAEoDUIJukgGKgQYoI0GUhNhY2Nl'
    'cHRSYXRlUGVyU2Vjb25kEiwKDGFjY2VwdF9idXJzdBgHIAEoDUIJukgGKgQYoI0GUgthY2NlcH'
    'RCdXJzdA==');

@$core.Deprecated('Use listenerAuthDescriptor instead')
const ListenerAuth$json = {
  '1': 'ListenerAuth',
  '2': [
    {
      '1': 'mode',
      '3': 1,
      '4': 1,
      '5': 14,
      '6': '.xtcp_config.v1.ListenerAuthMode',
      '8': {},
      '10': 'mode'
    },
    {'1': 'raw_token', '3': 2, '4': 1, '5': 9, '8': {}, '10': 'rawToken'},
    {
      '1': 'hmac_shared_key',
      '3': 3,
      '4': 1,
      '5': 9,
      '8': {},
      '10': 'hmacSharedKey'
    },
    {
      '1': 'signed_token_skew_minutes',
      '3': 4,
      '4': 1,
      '5': 13,
      '8': {},
      '9': 0,
      '10': 'signedTokenSkewMinutes',
      '17': true
    },
    {
      '1': 'failure_jitter_min',
      '3': 5,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Duration',
      '8': {},
      '10': 'failureJitterMin'
    },
    {
      '1': 'failure_jitter_max',
      '3': 6,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Duration',
      '8': {},
      '10': 'failureJitterMax'
    },
    {
      '1': 'raw_token_file',
      '3': 7,
      '4': 1,
      '5': 9,
      '8': {},
      '10': 'rawTokenFile'
    },
    {
      '1': 'hmac_shared_key_file',
      '3': 8,
      '4': 1,
      '5': 9,
      '8': {},
      '10': 'hmacSharedKeyFile'
    },
  ],
  '7': {},
  '8': [
    {'1': '_signed_token_skew_minutes'},
  ],
};

/// Descriptor for `ListenerAuth`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listenerAuthDescriptor = $convert.base64Decode(
    'CgxMaXN0ZW5lckF1dGgSPgoEbW9kZRgBIAEoDjIgLnh0Y3BfY29uZmlnLnYxLkxpc3RlbmVyQX'
    'V0aE1vZGVCCLpIBYIBAhABUgRtb2RlEiUKCXJhd190b2tlbhgCIAEoCUIIukgFcgMYgCBSCHJh'
    'd1Rva2VuEjAKD2htYWNfc2hhcmVkX2tleRgDIAEoCUIIukgFcgMYgCBSDWhtYWNTaGFyZWRLZX'
    'kSRwoZc2lnbmVkX3Rva2VuX3NrZXdfbWludXRlcxgEIAEoDUIHukgEKgIYBUgAUhZzaWduZWRU'
    'b2tlblNrZXdNaW51dGVziAEBElEKEmZhaWx1cmVfaml0dGVyX21pbhgFIAEoCzIZLmdvb2dsZS'
    '5wcm90b2J1Zi5EdXJhdGlvbkIIukgFqgECMgBSEGZhaWx1cmVKaXR0ZXJNaW4SUQoSZmFpbHVy'
    'ZV9qaXR0ZXJfbWF4GAYgASgLMhkuZ29vZ2xlLnByb3RvYnVmLkR1cmF0aW9uQgi6SAWqAQIyAF'
    'IQZmFpbHVyZUppdHRlck1heBIuCg5yYXdfdG9rZW5fZmlsZRgHIAEoCUIIukgFcgMYgCBSDHJh'
    'd1Rva2VuRmlsZRI5ChRobWFjX3NoYXJlZF9rZXlfZmlsZRgIIAEoCUIIukgFcgMYgCBSEWhtYW'
    'NTaGFyZWRLZXlGaWxlOuEBukjdARraAQoaTGlzdGVuZXJBdXRoLmZhaWx1cmVKaXR0ZXISRmZh'
    'aWx1cmVfaml0dGVyX21heCBtdXN0IGJlIGdyZWF0ZXIgdGhhbiBvciBlcXVhbCB0byBmYWlsdX'
    'JlX2ppdHRlcl9taW4adCFoYXModGhpcy5mYWlsdXJlX2ppdHRlcl9taW4pIHx8ICFoYXModGhp'
    'cy5mYWlsdXJlX2ppdHRlcl9tYXgpIHx8IHRoaXMuZmFpbHVyZV9qaXR0ZXJfbWF4ID49IHRoaX'
    'MuZmFpbHVyZV9qaXR0ZXJfbWluQhwKGl9zaWduZWRfdG9rZW5fc2tld19taW51dGVz');

@$core.Deprecated('Use listenerDescriptor instead')
const Listener$json = {
  '1': 'Listener',
  '2': [
    {'1': 'name', '3': 1, '4': 1, '5': 9, '8': {}, '10': 'name'},
    {
      '1': 'endpoint',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.xtcp_config.v1.ListenerEndpoint',
      '8': {},
      '10': 'endpoint'
    },
    {
      '1': 'auth',
      '3': 3,
      '4': 1,
      '5': 11,
      '6': '.xtcp_config.v1.ListenerAuth',
      '10': 'auth'
    },
    {
      '1': 'admin_auth',
      '3': 4,
      '4': 1,
      '5': 11,
      '6': '.xtcp_config.v1.ListenerAuth',
      '10': 'adminAuth'
    },
    {
      '1': 'serves',
      '3': 5,
      '4': 3,
      '5': 14,
      '6': '.xtcp_config.v1.EndpointClass',
      '8': {},
      '10': 'serves'
    },
  ],
};

/// Descriptor for `Listener`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listenerDescriptor = $convert.base64Decode(
    'CghMaXN0ZW5lchIwCgRuYW1lGAEgASgJQhy6SBlyFxABGCAyEV5bYS16XVthLXowLTktXSokUg'
    'RuYW1lEkQKCGVuZHBvaW50GAIgASgLMiAueHRjcF9jb25maWcudjEuTGlzdGVuZXJFbmRwb2lu'
    'dEIGukgDyAEBUghlbmRwb2ludBIwCgRhdXRoGAMgASgLMhwueHRjcF9jb25maWcudjEuTGlzdG'
    'VuZXJBdXRoUgRhdXRoEjsKCmFkbWluX2F1dGgYBCABKAsyHC54dGNwX2NvbmZpZy52MS5MaXN0'
    'ZW5lckF1dGhSCWFkbWluQXV0aBJMCgZzZXJ2ZXMYBSADKA4yHS54dGNwX2NvbmZpZy52MS5Fbm'
    'Rwb2ludENsYXNzQhW6SBKSAQ8IARAHGAEiB4IBBBABIABSBnNlcnZlcw==');

@$core.Deprecated('Use linkMonitorConfigDescriptor instead')
const LinkMonitorConfig$json = {
  '1': 'LinkMonitorConfig',
  '2': [
    {'1': 'enable', '3': 1, '4': 1, '5': 8, '10': 'enable'},
    {
      '1': 'baseline_file',
      '3': 2,
      '4': 1,
      '5': 9,
      '8': {},
      '10': 'baselineFile'
    },
    {
      '1': 'settle',
      '3': 3,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Duration',
      '8': {},
      '10': 'settle'
    },
    {
      '1': 'resync',
      '3': 4,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Duration',
      '8': {},
      '10': 'resync'
    },
    {
      '1': 'stats_interval',
      '3': 5,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Duration',
      '8': {},
      '10': 'statsInterval'
    },
    {
      '1': 'stats_include',
      '3': 6,
      '4': 1,
      '5': 9,
      '8': {},
      '10': 'statsInclude'
    },
    {
      '1': 'stats_exclude',
      '3': 7,
      '4': 1,
      '5': 9,
      '8': {},
      '10': 'statsExclude'
    },
    {
      '1': 'netstat_fields',
      '3': 8,
      '4': 1,
      '5': 9,
      '8': {},
      '10': 'netstatFields'
    },
    {
      '1': 'max_speed_exceptions',
      '3': 9,
      '4': 3,
      '5': 9,
      '8': {},
      '10': 'maxSpeedExceptions'
    },
    {
      '1': 'metrics_core_include',
      '3': 10,
      '4': 1,
      '5': 9,
      '8': {},
      '10': 'metricsCoreInclude'
    },
    {
      '1': 'metrics_core_exclude',
      '3': 11,
      '4': 1,
      '5': 9,
      '8': {},
      '10': 'metricsCoreExclude'
    },
    {'1': 'metrics_all', '3': 12, '4': 1, '5': 8, '10': 'metricsAll'},
    {
      '1': 'collection_mode',
      '3': 13,
      '4': 1,
      '5': 9,
      '8': {},
      '10': 'collectionMode'
    },
    {
      '1': 'watch_interval',
      '3': 14,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Duration',
      '8': {},
      '10': 'watchInterval'
    },
    {
      '1': 'slow_interval',
      '3': 15,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Duration',
      '8': {},
      '10': 'slowInterval'
    },
    {
      '1': 'on_demand_ttl',
      '3': 16,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Duration',
      '8': {},
      '10': 'onDemandTtl'
    },
    {
      '1': 'on_demand_wait',
      '3': 17,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Duration',
      '8': {},
      '10': 'onDemandWait'
    },
    {
      '1': 'error_window',
      '3': 18,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Duration',
      '8': {},
      '10': 'errorWindow'
    },
    {
      '1': 'flap_window',
      '3': 19,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Duration',
      '8': {},
      '10': 'flapWindow'
    },
    {
      '1': 'error_classes',
      '3': 20,
      '4': 1,
      '5': 9,
      '8': {},
      '9': 0,
      '10': 'errorClasses',
      '17': true
    },
    {
      '1': 'error_exclude',
      '3': 21,
      '4': 1,
      '5': 9,
      '8': {},
      '10': 'errorExclude'
    },
    {
      '1': 'metrics_categories',
      '3': 22,
      '4': 1,
      '5': 9,
      '8': {},
      '9': 1,
      '10': 'metricsCategories',
      '17': true
    },
    {
      '1': 'metrics_interface_paths',
      '3': 23,
      '4': 1,
      '5': 8,
      '9': 2,
      '10': 'metricsInterfacePaths',
      '17': true
    },
    {
      '1': 'metrics_all_path',
      '3': 24,
      '4': 1,
      '5': 9,
      '8': {},
      '9': 3,
      '10': 'metricsAllPath',
      '17': true
    },
    {'1': 'ip_paths', '3': 25, '4': 1, '5': 8, '10': 'ipPaths'},
    {
      '1': 'ip_max_bytes',
      '3': 26,
      '4': 1,
      '5': 13,
      '8': {},
      '10': 'ipMaxBytes'
    },
    {'1': 'ip_listen', '3': 27, '4': 1, '5': 9, '8': {}, '10': 'ipListen'},
    {
      '1': 'learn_deadline',
      '3': 28,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Duration',
      '8': {},
      '10': 'learnDeadline'
    },
  ],
  '8': [
    {'1': '_error_classes'},
    {'1': '_metrics_categories'},
    {'1': '_metrics_interface_paths'},
    {'1': '_metrics_all_path'},
  ],
};

/// Descriptor for `LinkMonitorConfig`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List linkMonitorConfigDescriptor = $convert.base64Decode(
    'ChFMaW5rTW9uaXRvckNvbmZpZxIWCgZlbmFibGUYASABKAhSBmVuYWJsZRItCg1iYXNlbGluZV'
    '9maWxlGAIgASgJQgi6SAVyAxiAIFIMYmFzZWxpbmVGaWxlEkEKBnNldHRsZRgDIAEoCzIZLmdv'
    'b2dsZS5wcm90b2J1Zi5EdXJhdGlvbkIOukgLqgEIIgQIgKMFMgBSBnNldHRsZRJBCgZyZXN5bm'
    'MYBCABKAsyGS5nb29nbGUucHJvdG9idWYuRHVyYXRpb25CDrpIC6oBCCIECID1JCoAUgZyZXN5'
    'bmMSUAoOc3RhdHNfaW50ZXJ2YWwYBSABKAsyGS5nb29nbGUucHJvdG9idWYuRHVyYXRpb25CDr'
    'pIC6oBCCIECICjBSoAUg1zdGF0c0ludGVydmFsEi0KDXN0YXRzX2luY2x1ZGUYBiABKAlCCLpI'
    'BXIDGIAgUgxzdGF0c0luY2x1ZGUSLQoNc3RhdHNfZXhjbHVkZRgHIAEoCUIIukgFcgMYgCBSDH'
    'N0YXRzRXhjbHVkZRIvCg5uZXRzdGF0X2ZpZWxkcxgIIAEoCUIIukgFcgMYgCBSDW5ldHN0YXRG'
    'aWVsZHMSRAoUbWF4X3NwZWVkX2V4Y2VwdGlvbnMYCSADKAlCErpID5IBDBCAAiIHcgUQARiAAV'
    'ISbWF4U3BlZWRFeGNlcHRpb25zEjoKFG1ldHJpY3NfY29yZV9pbmNsdWRlGAogASgJQgi6SAVy'
    'AxiAIFISbWV0cmljc0NvcmVJbmNsdWRlEjoKFG1ldHJpY3NfY29yZV9leGNsdWRlGAsgASgJQg'
    'i6SAVyAxiAIFISbWV0cmljc0NvcmVFeGNsdWRlEh8KC21ldHJpY3NfYWxsGAwgASgIUgptZXRy'
    'aWNzQWxsEkEKD2NvbGxlY3Rpb25fbW9kZRgNIAEoCUIYukgVchNSAFIEcG9sbFIJb24tZGVtYW'
    '5kUg5jb2xsZWN0aW9uTW9kZRJQCg53YXRjaF9pbnRlcnZhbBgOIAEoCzIZLmdvb2dsZS5wcm90'
    'b2J1Zi5EdXJhdGlvbkIOukgLqgEIIgQIgKMFKgBSDXdhdGNoSW50ZXJ2YWwSTgoNc2xvd19pbn'
    'RlcnZhbBgPIAEoCzIZLmdvb2dsZS5wcm90b2J1Zi5EdXJhdGlvbkIOukgLqgEIIgQIgKMFKgBS'
    'DHNsb3dJbnRlcnZhbBJNCg1vbl9kZW1hbmRfdHRsGBAgASgLMhkuZ29vZ2xlLnByb3RvYnVmLk'
    'R1cmF0aW9uQg66SAuqAQgiBAiAowUyAFILb25EZW1hbmRUdGwSTQoOb25fZGVtYW5kX3dhaXQY'
    'ESABKAsyGS5nb29nbGUucHJvdG9idWYuRHVyYXRpb25CDLpICaoBBhoCCAoqAFIMb25EZW1hbm'
    'RXYWl0EkwKDGVycm9yX3dpbmRvdxgSIAEoCzIZLmdvb2dsZS5wcm90b2J1Zi5EdXJhdGlvbkIO'
    'ukgLqgEIIgQIgPUkMgBSC2Vycm9yV2luZG93EkoKC2ZsYXBfd2luZG93GBMgASgLMhkuZ29vZ2'
    'xlLnByb3RvYnVmLkR1cmF0aW9uQg66SAuqAQgiBAiA9SQyAFIKZmxhcFdpbmRvdxJcCg1lcnJv'
    'cl9jbGFzc2VzGBQgASgJQjK6SC9yLRhAMileKChsaW5rfHJkbWF8ZHJvcCkoLChsaW5rfHJkbW'
    'F8ZHJvcCkpKik/JEgAUgxlcnJvckNsYXNzZXOIAQESLQoNZXJyb3JfZXhjbHVkZRgVIAEoCUII'
    'ukgFcgMYgCBSDGVycm9yRXhjbHVkZRI8ChJtZXRyaWNzX2NhdGVnb3JpZXMYFiABKAlCCLpIBX'
    'IDGIACSAFSEW1ldHJpY3NDYXRlZ29yaWVziAEBEjsKF21ldHJpY3NfaW50ZXJmYWNlX3BhdGhz'
    'GBcgASgISAJSFW1ldHJpY3NJbnRlcmZhY2VQYXRoc4gBARI3ChBtZXRyaWNzX2FsbF9wYXRoGB'
    'ggASgJQgi6SAVyAxiAAkgDUg5tZXRyaWNzQWxsUGF0aIgBARIZCghpcF9wYXRocxgZIAEoCFIH'
    'aXBQYXRocxItCgxpcF9tYXhfYnl0ZXMYGiABKA1CC7pICCoGGICAgIAEUgppcE1heEJ5dGVzEi'
    'QKCWlwX2xpc3RlbhgbIAEoCUIHukgEcgIYQFIIaXBMaXN0ZW4SUAoObGVhcm5fZGVhZGxpbmUY'
    'HCABKAsyGS5nb29nbGUucHJvdG9idWYuRHVyYXRpb25CDrpIC6oBCCIECICjBTIAUg1sZWFybk'
    'RlYWRsaW5lQhAKDl9lcnJvcl9jbGFzc2VzQhUKE19tZXRyaWNzX2NhdGVnb3JpZXNCGgoYX21l'
    'dHJpY3NfaW50ZXJmYWNlX3BhdGhzQhMKEV9tZXRyaWNzX2FsbF9wYXRo');

@$core.Deprecated('Use xtcpConfigDescriptor instead')
const XtcpConfig$json = {
  '1': 'XtcpConfig',
  '2': [
    {
      '1': 'nl_timeout_milliseconds',
      '3': 10,
      '4': 1,
      '5': 4,
      '8': {},
      '10': 'nlTimeoutMilliseconds'
    },
    {
      '1': 'poll_frequency',
      '3': 11,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Duration',
      '8': {},
      '10': 'pollFrequency'
    },
    {
      '1': 'poll_timeout',
      '3': 12,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Duration',
      '8': {},
      '10': 'pollTimeout'
    },
    {
      '1': 'poll_jitter_pct',
      '3': 13,
      '4': 1,
      '5': 13,
      '8': {},
      '10': 'pollJitterPct'
    },
    {'1': 'max_loops', '3': 14, '4': 1, '5': 4, '8': {}, '10': 'maxLoops'},
    {'1': 'netlinkers', '3': 15, '4': 1, '5': 13, '8': {}, '10': 'netlinkers'},
    {
      '1': 'netlinkers_done_chan_size',
      '3': 16,
      '4': 1,
      '5': 13,
      '8': {},
      '10': 'netlinkersDoneChanSize'
    },
    {'1': 'nlmsg_seq', '3': 17, '4': 1, '5': 13, '8': {}, '10': 'nlmsgSeq'},
    {'1': 'packet_size', '3': 18, '4': 1, '5': 4, '8': {}, '10': 'packetSize'},
    {
      '1': 'packet_size_mply',
      '3': 19,
      '4': 1,
      '5': 13,
      '8': {},
      '10': 'packetSizeMply'
    },
    {'1': 'modulus', '3': 20, '4': 1, '5': 4, '8': {}, '10': 'modulus'},
    {
      '1': 'enabled_deserializers',
      '3': 21,
      '4': 1,
      '5': 11,
      '6': '.xtcp_config.v1.EnabledDeserializers',
      '8': {},
      '10': 'enabledDeserializers'
    },
    {'1': 'io_uring', '3': 22, '4': 1, '5': 8, '8': {}, '10': 'ioUring'},
    {
      '1': 'io_uring_recv_batch_size',
      '3': 23,
      '4': 1,
      '5': 13,
      '8': {},
      '10': 'ioUringRecvBatchSize'
    },
    {
      '1': 'io_uring_cqe_batch_size',
      '3': 24,
      '4': 1,
      '5': 13,
      '8': {},
      '10': 'ioUringCqeBatchSize'
    },
    {
      '1': 'max_dump_bytes',
      '3': 25,
      '4': 1,
      '5': 4,
      '8': {},
      '10': 'maxDumpBytes'
    },
    {
      '1': 'max_records_per_netns',
      '3': 26,
      '4': 1,
      '5': 4,
      '8': {},
      '10': 'maxRecordsPerNetns'
    },
    {
      '1': 'max_records_per_poll',
      '3': 27,
      '4': 1,
      '5': 4,
      '8': {},
      '10': 'maxRecordsPerPoll'
    },
    {
      '1': 'io_uring_sockstat_batch_size',
      '3': 28,
      '4': 1,
      '5': 13,
      '8': {},
      '10': 'ioUringSockstatBatchSize'
    },
    {
      '1': 'io_uring_sockstat_workers',
      '3': 29,
      '4': 1,
      '5': 13,
      '8': {},
      '10': 'ioUringSockstatWorkers'
    },
    {
      '1': 'reconcile_frequency',
      '3': 40,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Duration',
      '8': {},
      '10': 'reconcileFrequency'
    },
    {
      '1': 'reconcile_before_poll',
      '3': 41,
      '4': 1,
      '5': 8,
      '10': 'reconcileBeforePoll'
    },
    {'1': 'max_netns', '3': 42, '4': 1, '5': 13, '8': {}, '10': 'maxNetns'},
    {
      '1': 'reconcile_timeout',
      '3': 43,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Duration',
      '8': {},
      '10': 'reconcileTimeout'
    },
    {
      '1': 'max_netns_per_owner',
      '3': 44,
      '4': 1,
      '5': 13,
      '8': {},
      '10': 'maxNetnsPerOwner'
    },
    {
      '1': 'max_netns_adds_per_reconcile',
      '3': 45,
      '4': 1,
      '5': 13,
      '8': {},
      '10': 'maxNetnsAddsPerReconcile'
    },
    {'1': 'write_files', '3': 50, '4': 1, '5': 13, '8': {}, '10': 'writeFiles'},
    {
      '1': 'capture_path',
      '3': 51,
      '4': 1,
      '5': 9,
      '8': {},
      '10': 'capturePath'
    },
    {
      '1': 'dest_write_files',
      '3': 52,
      '4': 1,
      '5': 13,
      '8': {},
      '10': 'destWriteFiles'
    },
    {'1': 'debug_level', '3': 53, '4': 1, '5': 13, '8': {}, '10': 'debugLevel'},
    {
      '1': 'debug_prometheus',
      '3': 54,
      '4': 1,
      '5': 8,
      '8': {},
      '10': 'debugPrometheus'
    },
    {'1': 'dest', '3': 60, '4': 1, '5': 9, '8': {}, '10': 'dest'},
    {'1': 'marshal_to', '3': 61, '4': 1, '5': 9, '8': {}, '10': 'marshalTo'},
    {'1': 'csv_columns', '3': 62, '4': 1, '5': 9, '8': {}, '10': 'csvColumns'},
    {
      '1': 'xtcp_proto_file',
      '3': 63,
      '4': 1,
      '5': 9,
      '8': {},
      '10': 'xtcpProtoFile'
    },
    {
      '1': 'envelope_flush_threshold_bytes',
      '3': 64,
      '4': 1,
      '5': 13,
      '8': {},
      '10': 'envelopeFlushThresholdBytes'
    },
    {
      '1': 'envelope_flush_threshold_rows',
      '3': 65,
      '4': 1,
      '5': 13,
      '8': {},
      '10': 'envelopeFlushThresholdRows'
    },
    {'1': 'topic', '3': 80, '4': 1, '5': 9, '8': {}, '10': 'topic'},
    {
      '1': 'kafka_schema_url',
      '3': 81,
      '4': 1,
      '5': 9,
      '8': {},
      '10': 'kafkaSchemaUrl'
    },
    {
      '1': 'kafka_produce_timeout',
      '3': 82,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Duration',
      '8': {},
      '10': 'kafkaProduceTimeout'
    },
    {
      '1': 'kafka_compression',
      '3': 83,
      '4': 1,
      '5': 9,
      '8': {},
      '10': 'kafkaCompression'
    },
    {'1': 's3_endpoint', '3': 100, '4': 1, '5': 9, '8': {}, '10': 's3Endpoint'},
    {'1': 's3_region', '3': 101, '4': 1, '5': 9, '8': {}, '10': 's3Region'},
    {'1': 's3_bucket', '3': 102, '4': 1, '5': 9, '8': {}, '10': 's3Bucket'},
    {'1': 's3_prefix', '3': 103, '4': 1, '5': 9, '8': {}, '10': 's3Prefix'},
    {
      '1': 's3_access_key',
      '3': 104,
      '4': 1,
      '5': 9,
      '8': {},
      '10': 's3AccessKey'
    },
    {
      '1': 's3_secret_key',
      '3': 105,
      '4': 1,
      '5': 9,
      '8': {},
      '10': 's3SecretKey'
    },
    {
      '1': 's3_skip_bucket_probe',
      '3': 106,
      '4': 1,
      '5': 8,
      '8': {},
      '10': 's3SkipBucketProbe'
    },
    {
      '1': 's3_allow_insecure',
      '3': 107,
      '4': 1,
      '5': 8,
      '8': {},
      '10': 's3AllowInsecure'
    },
    {'1': 's3_ca_file', '3': 108, '4': 1, '5': 9, '8': {}, '10': 's3CaFile'},
    {
      '1': 's3_parquet_flush_threshold_bytes',
      '3': 110,
      '4': 1,
      '5': 13,
      '8': {},
      '10': 's3ParquetFlushThresholdBytes'
    },
    {
      '1': 's3_flush_interval',
      '3': 111,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Duration',
      '8': {},
      '10': 's3FlushInterval'
    },
    {
      '1': 's3_flush_jitter_pct',
      '3': 112,
      '4': 1,
      '5': 13,
      '8': {},
      '10': 's3FlushJitterPct'
    },
    {
      '1': 's3_flush_threshold_jitter_pct',
      '3': 113,
      '4': 1,
      '5': 13,
      '8': {},
      '10': 's3FlushThresholdJitterPct'
    },
    {
      '1': 's3_upload_max_attempts',
      '3': 114,
      '4': 1,
      '5': 13,
      '8': {},
      '10': 's3UploadMaxAttempts'
    },
    {
      '1': 's3_upload_backoff_cap',
      '3': 115,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Duration',
      '8': {},
      '10': 's3UploadBackoffCap'
    },
    {
      '1': 's3_queue_depth',
      '3': 116,
      '4': 1,
      '5': 13,
      '8': {},
      '10': 's3QueueDepth'
    },
    {
      '1': 's3_max_buffered_bytes',
      '3': 117,
      '4': 1,
      '5': 4,
      '8': {},
      '10': 's3MaxBufferedBytes'
    },
    {
      '1': 's3_spill_dir',
      '3': 118,
      '4': 1,
      '5': 9,
      '8': {},
      '10': 's3SpillDir'
    },
    {
      '1': 's3_spill_max_bytes',
      '3': 119,
      '4': 1,
      '5': 4,
      '8': {},
      '10': 's3SpillMaxBytes'
    },
    {'1': 'hostname', '3': 130, '4': 1, '5': 9, '8': {}, '10': 'hostname'},
    {'1': 'location', '3': 131, '4': 1, '5': 9, '8': {}, '10': 'location'},
    {'1': 'label', '3': 132, '4': 1, '5': 9, '8': {}, '10': 'label'},
    {'1': 'tag', '3': 133, '4': 1, '5': 9, '8': {}, '10': 'tag'},
    {
      '1': 'daemon_version',
      '3': 134,
      '4': 1,
      '5': 9,
      '8': {},
      '10': 'daemonVersion'
    },
    {'1': 'ipv4_ttl', '3': 150, '4': 1, '5': 13, '8': {}, '10': 'ipv4Ttl'},
    {
      '1': 'ipv6_hop_limit',
      '3': 151,
      '4': 1,
      '5': 13,
      '8': {},
      '10': 'ipv6HopLimit'
    },
    {
      '1': 'listener_auth',
      '3': 152,
      '4': 1,
      '5': 11,
      '6': '.xtcp_config.v1.ListenerAuth',
      '8': {},
      '10': 'listenerAuth'
    },
    {
      '1': 'prometheus_listener',
      '3': 153,
      '4': 1,
      '5': 11,
      '6': '.xtcp_config.v1.ListenerEndpoint',
      '8': {},
      '10': 'prometheusListener'
    },
    {
      '1': 'admin_listener_auth',
      '3': 154,
      '4': 1,
      '5': 11,
      '6': '.xtcp_config.v1.ListenerAuth',
      '8': {},
      '10': 'adminListenerAuth'
    },
    {'1': 'grpc_port', '3': 160, '4': 1, '5': 13, '8': {}, '10': 'grpcPort'},
    {
      '1': 'grpc_listener',
      '3': 161,
      '4': 1,
      '5': 11,
      '6': '.xtcp_config.v1.ListenerEndpoint',
      '8': {},
      '10': 'grpcListener'
    },
    {
      '1': 'listeners',
      '3': 162,
      '4': 3,
      '5': 11,
      '6': '.xtcp_config.v1.Listener',
      '8': {},
      '10': 'listeners'
    },
    {
      '1': 'pyroscope_url',
      '3': 170,
      '4': 1,
      '5': 9,
      '8': {},
      '10': 'pyroscopeUrl'
    },
    {
      '1': 'pyroscope_app_name',
      '3': 171,
      '4': 1,
      '5': 9,
      '8': {},
      '10': 'pyroscopeAppName'
    },
    {
      '1': 'pyroscope_sample_hz',
      '3': 172,
      '4': 1,
      '5': 13,
      '8': {},
      '10': 'pyroscopeSampleHz'
    },
    {
      '1': 'pyroscope_upload_interval_sec',
      '3': 173,
      '4': 1,
      '5': 13,
      '8': {},
      '10': 'pyroscopeUploadIntervalSec'
    },
    {
      '1': 'resolve_container_id',
      '3': 200,
      '4': 1,
      '5': 8,
      '8': {},
      '10': 'resolveContainerId'
    },
    {
      '1': 'enrich_container_enable',
      '3': 201,
      '4': 1,
      '5': 8,
      '10': 'enrichContainerEnable'
    },
    {
      '1': 'docker_socket_path',
      '3': 202,
      '4': 1,
      '5': 9,
      '8': {},
      '10': 'dockerSocketPath'
    },
    {
      '1': 'enrich_lldp_enable',
      '3': 210,
      '4': 1,
      '5': 8,
      '10': 'enrichLldpEnable'
    },
    {
      '1': 'lldpd_socket_path',
      '3': 211,
      '4': 1,
      '5': 9,
      '8': {},
      '10': 'lldpdSocketPath'
    },
    {
      '1': 'lldpd_version_hint',
      '3': 212,
      '4': 1,
      '5': 9,
      '8': {},
      '10': 'lldpdVersionHint'
    },
    {
      '1': 'enrich_nic_enable',
      '3': 220,
      '4': 1,
      '5': 8,
      '10': 'enrichNicEnable'
    },
    {
      '1': 'uplink_count',
      '3': 221,
      '4': 1,
      '5': 13,
      '8': {},
      '10': 'uplinkCount'
    },
    {
      '1': 'uplink_interfaces',
      '3': 222,
      '4': 3,
      '5': 9,
      '8': {},
      '10': 'uplinkInterfaces'
    },
    {'1': 'populate_nsid', '3': 230, '4': 1, '5': 8, '10': 'populateNsid'},
    {
      '1': 'enrich_asn_enable',
      '3': 240,
      '4': 1,
      '5': 8,
      '10': 'enrichAsnEnable'
    },
    {'1': 'asn_db_path', '3': 241, '4': 1, '5': 9, '8': {}, '10': 'asnDbPath'},
    {
      '1': 'asn_refresh_interval',
      '3': 242,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Duration',
      '10': 'asnRefreshInterval'
    },
    {
      '1': 'ipmeta_bootstrap_path',
      '3': 243,
      '4': 1,
      '5': 9,
      '8': {},
      '10': 'ipmetaBootstrapPath'
    },
    {
      '1': 'ipmeta_cache_path',
      '3': 244,
      '4': 1,
      '5': 9,
      '8': {},
      '10': 'ipmetaCachePath'
    },
    {
      '1': 'enrich_locality_enable',
      '3': 245,
      '4': 1,
      '5': 8,
      '10': 'enrichLocalityEnable'
    },
    {
      '1': 'locality_refresh_interval',
      '3': 246,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Duration',
      '10': 'localityRefreshInterval'
    },
    {
      '1': 'locality_events_enable',
      '3': 247,
      '4': 1,
      '5': 8,
      '10': 'localityEventsEnable'
    },
    {
      '1': 'link_monitor',
      '3': 250,
      '4': 1,
      '5': 11,
      '6': '.xtcp_config.v1.LinkMonitorConfig',
      '10': 'linkMonitor'
    },
  ],
  '7': {},
};

/// Descriptor for `XtcpConfig`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List xtcpConfigDescriptor = $convert.base64Decode(
    'CgpYdGNwQ29uZmlnEkYKF25sX3RpbWVvdXRfbWlsbGlzZWNvbmRzGAogASgEQg66SAvIAQEyBh'
    'igjQYoAFIVbmxUaW1lb3V0TWlsbGlzZWNvbmRzElUKDnBvbGxfZnJlcXVlbmN5GAsgASgLMhku'
    'Z29vZ2xlLnByb3RvYnVmLkR1cmF0aW9uQhO6SBDIAQGqAQoiBAiA9SQyAggBUg1wb2xsRnJlcX'
    'VlbmN5Ek8KDHBvbGxfdGltZW91dBgMIAEoCzIZLmdvb2dsZS5wcm90b2J1Zi5EdXJhdGlvbkIR'
    'ukgOyAEBqgEIIgQIgPUkKgBSC3BvbGxUaW1lb3V0EjIKD3BvbGxfaml0dGVyX3BjdBgNIAEoDU'
    'IKukgHyAEAKgIYZFINcG9sbEppdHRlclBjdBIrCgltYXhfbG9vcHMYDiABKARCDrpIC8gBADIG'
    'GKCNBigAUghtYXhMb29wcxIsCgpuZXRsaW5rZXJzGA8gASgNQgy6SAnIAQEqBBhkKAFSCm5ldG'
    'xpbmtlcnMSSAoZbmV0bGlua2Vyc19kb25lX2NoYW5fc2l6ZRgQIAEoDUINukgKyAEBKgUY6Aco'
    'AVIWbmV0bGlua2Vyc0RvbmVDaGFuU2l6ZRIqCglubG1zZ19zZXEYESABKA1CDbpICsgBASoFGJ'
    'BOKABSCG5sbXNnU2VxEi8KC3BhY2tldF9zaXplGBIgASgEQg66SAvIAQAyBhjAhD0oAFIKcGFj'
    'a2V0U2l6ZRI2ChBwYWNrZXRfc2l6ZV9tcGx5GBMgASgNQgy6SAnIAQAqBBhkKABSDnBhY2tldF'
    'NpemVNcGx5EigKB21vZHVsdXMYFCABKARCDrpIC8gBATIGGMCEPSgBUgdtb2R1bHVzEmEKFWVu'
    'YWJsZWRfZGVzZXJpYWxpemVycxgVIAEoCzIkLnh0Y3BfY29uZmlnLnYxLkVuYWJsZWREZXNlcm'
    'lhbGl6ZXJzQga6SAPIAQBSFGVuYWJsZWREZXNlcmlhbGl6ZXJzEiEKCGlvX3VyaW5nGBYgASgI'
    'Qga6SAPIAQBSB2lvVXJpbmcSRQoYaW9fdXJpbmdfcmVjdl9iYXRjaF9zaXplGBcgASgNQg26SA'
    'rIAQAqBRiAICgBUhRpb1VyaW5nUmVjdkJhdGNoU2l6ZRJDChdpb191cmluZ19jcWVfYmF0Y2hf'
    'c2l6ZRgYIAEoDUINukgKyAEAKgUYgCAoAVITaW9VcmluZ0NxZUJhdGNoU2l6ZRIsCg5tYXhfZH'
    'VtcF9ieXRlcxgZIAEoBEIGukgDyAEAUgxtYXhEdW1wQnl0ZXMSOQoVbWF4X3JlY29yZHNfcGVy'
    'X25ldG5zGBogASgEQga6SAPIAQBSEm1heFJlY29yZHNQZXJOZXRucxI3ChRtYXhfcmVjb3Jkc1'
    '9wZXJfcG9sbBgbIAEoBEIGukgDyAEAUhFtYXhSZWNvcmRzUGVyUG9sbBJLChxpb191cmluZ19z'
    'b2Nrc3RhdF9iYXRjaF9zaXplGBwgASgNQgu6SAjIAQAqAxiACFIYaW9VcmluZ1NvY2tzdGF0Qm'
    'F0Y2hTaXplEkUKGWlvX3VyaW5nX3NvY2tzdGF0X3dvcmtlcnMYHSABKA1CCrpIB8gBACoCGEBS'
    'FmlvVXJpbmdTb2Nrc3RhdFdvcmtlcnMSVwoTcmVjb25jaWxlX2ZyZXF1ZW5jeRgoIAEoCzIZLm'
    'dvb2dsZS5wcm90b2J1Zi5EdXJhdGlvbkILukgIyAEAqgECMgBSEnJlY29uY2lsZUZyZXF1ZW5j'
    'eRIyChVyZWNvbmNpbGVfYmVmb3JlX3BvbGwYKSABKAhSE3JlY29uY2lsZUJlZm9yZVBvbGwSIw'
    'oJbWF4X25ldG5zGCogASgNQga6SAPIAQBSCG1heE5ldG5zElMKEXJlY29uY2lsZV90aW1lb3V0'
    'GCsgASgLMhkuZ29vZ2xlLnByb3RvYnVmLkR1cmF0aW9uQgu6SAjIAQCqAQIyAFIQcmVjb25jaW'
    'xlVGltZW91dBI1ChNtYXhfbmV0bnNfcGVyX293bmVyGCwgASgNQga6SAPIAQBSEG1heE5ldG5z'
    'UGVyT3duZXISRgocbWF4X25ldG5zX2FkZHNfcGVyX3JlY29uY2lsZRgtIAEoDUIGukgDyAEAUh'
    'htYXhOZXRuc0FkZHNQZXJSZWNvbmNpbGUSLgoLd3JpdGVfZmlsZXMYMiABKA1CDbpICsgBACoF'
    'GOgHKABSCndyaXRlRmlsZXMSLwoMY2FwdHVyZV9wYXRoGDMgASgJQgy6SAnIAQByBBABGFBSC2'
    'NhcHR1cmVQYXRoEjcKEGRlc3Rfd3JpdGVfZmlsZXMYNCABKA1CDbpICsgBACoFGOgHKABSDmRl'
    'c3RXcml0ZUZpbGVzEi4KC2RlYnVnX2xldmVsGDUgASgNQg26SArIAQEqBRjoBygAUgpkZWJ1Z0'
    'xldmVsEjEKEGRlYnVnX3Byb21ldGhldXMYNiABKAhCBrpIA8gBAFIPZGVidWdQcm9tZXRoZXVz'
    'EiEKBGRlc3QYPCABKAlCDbpICsgBAXIFEAQYgARSBGRlc3QSKwoKbWFyc2hhbF90bxg9IAEoCU'
    'IMukgJyAEBcgQQAxgoUgltYXJzaGFsVG8SJwoLY3N2X2NvbHVtbnMYPiABKAlCBrpIA8gBAFIK'
    'Y3N2Q29sdW1ucxI0Cg94dGNwX3Byb3RvX2ZpbGUYPyABKAlCDLpICcgBAHIEEAEYUFINeHRjcF'
    'Byb3RvRmlsZRJLCh5lbnZlbG9wZV9mbHVzaF90aHJlc2hvbGRfYnl0ZXMYQCABKA1CBrpIA8gB'
    'AFIbZW52ZWxvcGVGbHVzaFRocmVzaG9sZEJ5dGVzEkkKHWVudmVsb3BlX2ZsdXNoX3RocmVzaG'
    '9sZF9yb3dzGEEgASgNQga6SAPIAQBSGmVudmVsb3BlRmx1c2hUaHJlc2hvbGRSb3dzEiIKBXRv'
    'cGljGFAgASgJQgy6SAnIAQByBBABGChSBXRvcGljEjYKEGthZmthX3NjaGVtYV91cmwYUSABKA'
    'lCDLpICcgBAHIEEAEYPFIOa2Fma2FTY2hlbWFVcmwSXwoVa2Fma2FfcHJvZHVjZV90aW1lb3V0'
    'GFIgASgLMhkuZ29vZ2xlLnByb3RvYnVmLkR1cmF0aW9uQhC6SA3IAQCqAQciAwjYBDIAUhNrYW'
    'ZrYVByb2R1Y2VUaW1lb3V0EjMKEWthZmthX2NvbXByZXNzaW9uGFMgASgJQga6SAPIAQBSEGth'
    'ZmthQ29tcHJlc3Npb24SJwoLczNfZW5kcG9pbnQYZCABKAlCBrpIA8gBAFIKczNFbmRwb2ludB'
    'IjCglzM19yZWdpb24YZSABKAlCBrpIA8gBAFIIczNSZWdpb24SIwoJczNfYnVja2V0GGYgASgJ'
    'Qga6SAPIAQBSCHMzQnVja2V0EiMKCXMzX3ByZWZpeBhnIAEoCUIGukgDyAEAUghzM1ByZWZpeB'
    'IqCg1zM19hY2Nlc3Nfa2V5GGggASgJQga6SAPIAQBSC3MzQWNjZXNzS2V5EioKDXMzX3NlY3Jl'
    'dF9rZXkYaSABKAlCBrpIA8gBAFILczNTZWNyZXRLZXkSNwoUczNfc2tpcF9idWNrZXRfcHJvYm'
    'UYaiABKAhCBrpIA8gBAFIRczNTa2lwQnVja2V0UHJvYmUSMgoRczNfYWxsb3dfaW5zZWN1cmUY'
    'ayABKAhCBrpIA8gBAFIPczNBbGxvd0luc2VjdXJlEikKCnMzX2NhX2ZpbGUYbCABKAlCC7pICM'
    'gBAHIDGIAgUghzM0NhRmlsZRJOCiBzM19wYXJxdWV0X2ZsdXNoX3RocmVzaG9sZF9ieXRlcxhu'
    'IAEoDUIGukgDyAEAUhxzM1BhcnF1ZXRGbHVzaFRocmVzaG9sZEJ5dGVzElIKEXMzX2ZsdXNoX2'
    'ludGVydmFsGG8gASgLMhkuZ29vZ2xlLnByb3RvYnVmLkR1cmF0aW9uQgu6SAjIAQCqAQIyAFIP'
    'czNGbHVzaEludGVydmFsEjkKE3MzX2ZsdXNoX2ppdHRlcl9wY3QYcCABKA1CCrpIB8gBACoCGG'
    'RSEHMzRmx1c2hKaXR0ZXJQY3QSTAodczNfZmx1c2hfdGhyZXNob2xkX2ppdHRlcl9wY3QYcSAB'
    'KA1CCrpIB8gBACoCGGRSGXMzRmx1c2hUaHJlc2hvbGRKaXR0ZXJQY3QSQQoWczNfdXBsb2FkX2'
    '1heF9hdHRlbXB0cxhyIAEoDUIMukgJyAEAKgQYZCgBUhNzM1VwbG9hZE1heEF0dGVtcHRzElkK'
    'FXMzX3VwbG9hZF9iYWNrb2ZmX2NhcBhzIAEoCzIZLmdvb2dsZS5wcm90b2J1Zi5EdXJhdGlvbk'
    'ILukgIyAEAqgECMgBSEnMzVXBsb2FkQmFja29mZkNhcBIxCg5zM19xdWV1ZV9kZXB0aBh0IAEo'
    'DUILukgIyAEAKgMYgCBSDHMzUXVldWVEZXB0aBI5ChVzM19tYXhfYnVmZmVyZWRfYnl0ZXMYdS'
    'ABKARCBrpIA8gBAFISczNNYXhCdWZmZXJlZEJ5dGVzEkcKDHMzX3NwaWxsX2Rpchh2IAEoCUIl'
    'ukgiyAEAch0YgCAyGF5bXlx4MDAtXHgxZlx4N2YtXHg5Zl0qJFIKczNTcGlsbERpchIzChJzM1'
    '9zcGlsbF9tYXhfYnl0ZXMYdyABKARCBrpIA8gBAFIPczNTcGlsbE1heEJ5dGVzEkIKCGhvc3Ru'
    'YW1lGIIBIAEoCUIlukgiyAEAch0Y/QEyGF5bXlx4MDAtXHgxZlx4N2YtXHg5Zl0qJFIIaG9zdG'
    '5hbWUSQgoIbG9jYXRpb24YgwEgASgJQiW6SCLIAQByHRj9ATIYXlteXHgwMC1ceDFmXHg3Zi1c'
    'eDlmXSokUghsb2NhdGlvbhI7CgVsYWJlbBiEASABKAlCJLpIIcgBAHIcGCgyGF5bXlx4MDAtXH'
    'gxZlx4N2YtXHg5Zl0qJFIFbGFiZWwSNwoDdGFnGIUBIAEoCUIkukghyAEAchwYKDIYXlteXHgw'
    'MC1ceDFmXHg3Zi1ceDlmXSokUgN0YWcSMwoOZGFlbW9uX3ZlcnNpb24YhgEgASgJQgu6SAjIAQ'
    'ByAxj9AVINZGFlbW9uVmVyc2lvbhInCghpcHY0X3R0bBiWASABKA1CC7pICMgBACoDGP8BUgdp'
    'cHY0VHRsEjIKDmlwdjZfaG9wX2xpbWl0GJcBIAEoDUILukgIyAEAKgMY/wFSDGlwdjZIb3BMaW'
    '1pdBJKCg1saXN0ZW5lcl9hdXRoGJgBIAEoCzIcLnh0Y3BfY29uZmlnLnYxLkxpc3RlbmVyQXV0'
    'aEIGukgDyAEAUgxsaXN0ZW5lckF1dGgSWgoTcHJvbWV0aGV1c19saXN0ZW5lchiZASABKAsyIC'
    '54dGNwX2NvbmZpZy52MS5MaXN0ZW5lckVuZHBvaW50Qga6SAPIAQBSEnByb21ldGhldXNMaXN0'
    'ZW5lchJVChNhZG1pbl9saXN0ZW5lcl9hdXRoGJoBIAEoCzIcLnh0Y3BfY29uZmlnLnYxLkxpc3'
    'RlbmVyQXV0aEIGukgDyAEAUhFhZG1pbkxpc3RlbmVyQXV0aBInCglncnBjX3BvcnQYoAEgASgN'
    'Qgm6SAYqBBj//wNSCGdycGNQb3J0Ek4KDWdycGNfbGlzdGVuZXIYoQEgASgLMiAueHRjcF9jb2'
    '5maWcudjEuTGlzdGVuZXJFbmRwb2ludEIGukgDyAEAUgxncnBjTGlzdGVuZXISQQoJbGlzdGVu'
    'ZXJzGKIBIAMoCzIYLnh0Y3BfY29uZmlnLnYxLkxpc3RlbmVyQgi6SAWSAQIQEFIJbGlzdGVuZX'
    'JzEiwKDXB5cm9zY29wZV91cmwYqgEgASgJQga6SAPIAQBSDHB5cm9zY29wZVVybBI1ChJweXJv'
    'c2NvcGVfYXBwX25hbWUYqwEgASgJQga6SAPIAQBSEHB5cm9zY29wZUFwcE5hbWUSNwoTcHlyb3'
    'Njb3BlX3NhbXBsZV9oehisASABKA1CBrpIA8gBAFIRcHlyb3Njb3BlU2FtcGxlSHoSSgodcHly'
    'b3Njb3BlX3VwbG9hZF9pbnRlcnZhbF9zZWMYrQEgASgNQga6SAPIAQBSGnB5cm9zY29wZVVwbG'
    '9hZEludGVydmFsU2VjEjkKFHJlc29sdmVfY29udGFpbmVyX2lkGMgBIAEoCEIGukgDyAEAUhJy'
    'ZXNvbHZlQ29udGFpbmVySWQSNwoXZW5yaWNoX2NvbnRhaW5lcl9lbmFibGUYyQEgASgIUhVlbn'
    'JpY2hDb250YWluZXJFbmFibGUSNwoSZG9ja2VyX3NvY2tldF9wYXRoGMoBIAEoCUIIukgFcgMY'
    '/wFSEGRvY2tlclNvY2tldFBhdGgSLQoSZW5yaWNoX2xsZHBfZW5hYmxlGNIBIAEoCFIQZW5yaW'
    'NoTGxkcEVuYWJsZRI1ChFsbGRwZF9zb2NrZXRfcGF0aBjTASABKAlCCLpIBXIDGP8BUg9sbGRw'
    'ZFNvY2tldFBhdGgSNgoSbGxkcGRfdmVyc2lvbl9oaW50GNQBIAEoCUIHukgEcgIYEFIQbGxkcG'
    'RWZXJzaW9uSGludBIrChFlbnJpY2hfbmljX2VuYWJsZRjcASABKAhSD2VucmljaE5pY0VuYWJs'
    'ZRIrCgx1cGxpbmtfY291bnQY3QEgASgNQge6SAQqAhgCUgt1cGxpbmtDb3VudBI2ChF1cGxpbm'
    'tfaW50ZXJmYWNlcxjeASADKAlCCLpIBZIBAhACUhB1cGxpbmtJbnRlcmZhY2VzEiQKDXBvcHVs'
    'YXRlX25zaWQY5gEgASgIUgxwb3B1bGF0ZU5zaWQSKwoRZW5yaWNoX2Fzbl9lbmFibGUY8AEgAS'
    'gIUg9lbnJpY2hBc25FbmFibGUSKQoLYXNuX2RiX3BhdGgY8QEgASgJQgi6SAVyAxj/AVIJYXNu'
    'RGJQYXRoEkwKFGFzbl9yZWZyZXNoX2ludGVydmFsGPIBIAEoCzIZLmdvb2dsZS5wcm90b2J1Zi'
    '5EdXJhdGlvblISYXNuUmVmcmVzaEludGVydmFsEj0KFWlwbWV0YV9ib290c3RyYXBfcGF0aBjz'
    'ASABKAlCCLpIBXIDGP8BUhNpcG1ldGFCb290c3RyYXBQYXRoEjUKEWlwbWV0YV9jYWNoZV9wYX'
    'RoGPQBIAEoCUIIukgFcgMY/wFSD2lwbWV0YUNhY2hlUGF0aBI1ChZlbnJpY2hfbG9jYWxpdHlf'
    'ZW5hYmxlGPUBIAEoCFIUZW5yaWNoTG9jYWxpdHlFbmFibGUSVgoZbG9jYWxpdHlfcmVmcmVzaF'
    '9pbnRlcnZhbBj2ASABKAsyGS5nb29nbGUucHJvdG9idWYuRHVyYXRpb25SF2xvY2FsaXR5UmVm'
    'cmVzaEludGVydmFsEjUKFmxvY2FsaXR5X2V2ZW50c19lbmFibGUY9wEgASgIUhRsb2NhbGl0eU'
    'V2ZW50c0VuYWJsZRJFCgxsaW5rX21vbml0b3IY+gEgASgLMiEueHRjcF9jb25maWcudjEuTGlu'
    'a01vbml0b3JDb25maWdSC2xpbmtNb25pdG9yOnO6SHAabgoPWHRjcENvbmZpZy5wb2xsEjJQb2'
    'xsIHRpbWVvdXQgbXVzdCBiZSBsZXNzIHRoYW4gcG9sbCBwb2xsX2ZyZXF1ZW5jeRondGhpcy5w'
    'b2xsX2ZyZXF1ZW5jeSA+IHRoaXMucG9sbF90aW1lb3V0');

@$core.Deprecated('Use enabledDeserializersDescriptor instead')
const EnabledDeserializers$json = {
  '1': 'EnabledDeserializers',
  '2': [
    {
      '1': 'enabled',
      '3': 1,
      '4': 3,
      '5': 11,
      '6': '.xtcp_config.v1.EnabledDeserializers.EnabledEntry',
      '10': 'enabled'
    },
  ],
  '3': [EnabledDeserializers_EnabledEntry$json],
};

@$core.Deprecated('Use enabledDeserializersDescriptor instead')
const EnabledDeserializers_EnabledEntry$json = {
  '1': 'EnabledEntry',
  '2': [
    {'1': 'key', '3': 1, '4': 1, '5': 9, '10': 'key'},
    {'1': 'value', '3': 2, '4': 1, '5': 8, '10': 'value'},
  ],
  '7': {'7': true},
};

/// Descriptor for `EnabledDeserializers`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List enabledDeserializersDescriptor = $convert.base64Decode(
    'ChRFbmFibGVkRGVzZXJpYWxpemVycxJLCgdlbmFibGVkGAEgAygLMjEueHRjcF9jb25maWcudj'
    'EuRW5hYmxlZERlc2VyaWFsaXplcnMuRW5hYmxlZEVudHJ5UgdlbmFibGVkGjoKDEVuYWJsZWRF'
    'bnRyeRIQCgNrZXkYASABKAlSA2tleRIUCgV2YWx1ZRgCIAEoCFIFdmFsdWU6AjgB');
