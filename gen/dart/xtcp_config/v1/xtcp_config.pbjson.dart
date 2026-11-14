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
    'ChdTZXRQb2xsRnJlcXVlbmN5UmVxdWVzdBJTCg5wb2xsX2ZyZXF1ZW5jeRgUIAEoCzIZLmdvb2'
    'dsZS5wcm90b2J1Zi5EdXJhdGlvbkIRukgOyAEBqgEIIgQIgPUkMgBSDXBvbGxGcmVxdWVuY3kS'
    'TwoMcG9sbF90aW1lb3V0GB4gASgLMhkuZ29vZ2xlLnByb3RvYnVmLkR1cmF0aW9uQhG6SA7IAQ'
    'GqAQgiBAiA9SQyAFILcG9sbFRpbWVvdXQ6c7pIcBpuCg9YdGNwQ29uZmlnLnBvbGwSMlBvbGwg'
    'dGltZW91dCBtdXN0IGJlIGxlc3MgdGhhbiBwb2xsIHBvbGxfZnJlcXVlbmN5Gid0aGlzLnBvbG'
    'xfdGltZW91dCA8IHRoaXMucG9sbF9mcmVxdWVuY3k=');

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
    'igjQYoAFIVbmxUaW1lb3V0TWlsbGlzZWNvbmRzElMKDnBvbGxfZnJlcXVlbmN5GAsgASgLMhku'
    'Z29vZ2xlLnByb3RvYnVmLkR1cmF0aW9uQhG6SA7IAQGqAQgiBAiA9SQqAFINcG9sbEZyZXF1ZW'
    '5jeRJPCgxwb2xsX3RpbWVvdXQYDCABKAsyGS5nb29nbGUucHJvdG9idWYuRHVyYXRpb25CEbpI'
    'DsgBAaoBCCIECID1JCoAUgtwb2xsVGltZW91dBIyCg9wb2xsX2ppdHRlcl9wY3QYDSABKA1CCr'
    'pIB8gBACoCGGRSDXBvbGxKaXR0ZXJQY3QSKwoJbWF4X2xvb3BzGA4gASgEQg66SAvIAQAyBhig'
    'jQYoAFIIbWF4TG9vcHMSLAoKbmV0bGlua2VycxgPIAEoDUIMukgJyAEBKgQYZCgBUgpuZXRsaW'
    '5rZXJzEkgKGW5ldGxpbmtlcnNfZG9uZV9jaGFuX3NpemUYECABKA1CDbpICsgBASoFGOgHKAFS'
    'Fm5ldGxpbmtlcnNEb25lQ2hhblNpemUSKgoJbmxtc2dfc2VxGBEgASgNQg26SArIAQEqBRiQTi'
    'gAUghubG1zZ1NlcRIvCgtwYWNrZXRfc2l6ZRgSIAEoBEIOukgLyAEAMgYYwIQ9KABSCnBhY2tl'
    'dFNpemUSNgoQcGFja2V0X3NpemVfbXBseRgTIAEoDUIMukgJyAEAKgQYZCgAUg5wYWNrZXRTaX'
    'plTXBseRIoCgdtb2R1bHVzGBQgASgEQg66SAvIAQEyBhjAhD0oAVIHbW9kdWx1cxJhChVlbmFi'
    'bGVkX2Rlc2VyaWFsaXplcnMYFSABKAsyJC54dGNwX2NvbmZpZy52MS5FbmFibGVkRGVzZXJpYW'
    'xpemVyc0IGukgDyAEAUhRlbmFibGVkRGVzZXJpYWxpemVycxIhCghpb191cmluZxgWIAEoCEIG'
    'ukgDyAEAUgdpb1VyaW5nEkUKGGlvX3VyaW5nX3JlY3ZfYmF0Y2hfc2l6ZRgXIAEoDUINukgKyA'
    'EAKgUYgCAoAVIUaW9VcmluZ1JlY3ZCYXRjaFNpemUSQwoXaW9fdXJpbmdfY3FlX2JhdGNoX3Np'
    'emUYGCABKA1CDbpICsgBACoFGIAgKAFSE2lvVXJpbmdDcWVCYXRjaFNpemUSLAoObWF4X2R1bX'
    'BfYnl0ZXMYGSABKARCBrpIA8gBAFIMbWF4RHVtcEJ5dGVzEjkKFW1heF9yZWNvcmRzX3Blcl9u'
    'ZXRucxgaIAEoBEIGukgDyAEAUhJtYXhSZWNvcmRzUGVyTmV0bnMSNwoUbWF4X3JlY29yZHNfcG'
    'VyX3BvbGwYGyABKARCBrpIA8gBAFIRbWF4UmVjb3Jkc1BlclBvbGwSSwocaW9fdXJpbmdfc29j'
    'a3N0YXRfYmF0Y2hfc2l6ZRgcIAEoDUILukgIyAEAKgMYgAhSGGlvVXJpbmdTb2Nrc3RhdEJhdG'
    'NoU2l6ZRJFChlpb191cmluZ19zb2Nrc3RhdF93b3JrZXJzGB0gASgNQgq6SAfIAQAqAhhAUhZp'
    'b1VyaW5nU29ja3N0YXRXb3JrZXJzElcKE3JlY29uY2lsZV9mcmVxdWVuY3kYKCABKAsyGS5nb2'
    '9nbGUucHJvdG9idWYuRHVyYXRpb25CC7pICMgBAKoBAjIAUhJyZWNvbmNpbGVGcmVxdWVuY3kS'
    'MgoVcmVjb25jaWxlX2JlZm9yZV9wb2xsGCkgASgIUhNyZWNvbmNpbGVCZWZvcmVQb2xsEiMKCW'
    '1heF9uZXRucxgqIAEoDUIGukgDyAEAUghtYXhOZXRucxJTChFyZWNvbmNpbGVfdGltZW91dBgr'
    'IAEoCzIZLmdvb2dsZS5wcm90b2J1Zi5EdXJhdGlvbkILukgIyAEAqgECMgBSEHJlY29uY2lsZV'
    'RpbWVvdXQSNQoTbWF4X25ldG5zX3Blcl9vd25lchgsIAEoDUIGukgDyAEAUhBtYXhOZXRuc1Bl'
    'ck93bmVyEkYKHG1heF9uZXRuc19hZGRzX3Blcl9yZWNvbmNpbGUYLSABKA1CBrpIA8gBAFIYbW'
    'F4TmV0bnNBZGRzUGVyUmVjb25jaWxlEi4KC3dyaXRlX2ZpbGVzGDIgASgNQg26SArIAQAqBRjo'
    'BygAUgp3cml0ZUZpbGVzEi8KDGNhcHR1cmVfcGF0aBgzIAEoCUIMukgJyAEAcgQQARhQUgtjYX'
    'B0dXJlUGF0aBI3ChBkZXN0X3dyaXRlX2ZpbGVzGDQgASgNQg26SArIAQAqBRjoBygAUg5kZXN0'
    'V3JpdGVGaWxlcxIuCgtkZWJ1Z19sZXZlbBg1IAEoDUINukgKyAEBKgUY6AcoAFIKZGVidWdMZX'
    'ZlbBIxChBkZWJ1Z19wcm9tZXRoZXVzGDYgASgIQga6SAPIAQBSD2RlYnVnUHJvbWV0aGV1cxIh'
    'CgRkZXN0GDwgASgJQg26SArIAQFyBRAEGIAEUgRkZXN0EisKCm1hcnNoYWxfdG8YPSABKAlCDL'
    'pICcgBAXIEEAMYKFIJbWFyc2hhbFRvEicKC2Nzdl9jb2x1bW5zGD4gASgJQga6SAPIAQBSCmNz'
    'dkNvbHVtbnMSNAoPeHRjcF9wcm90b19maWxlGD8gASgJQgy6SAnIAQByBBABGFBSDXh0Y3BQcm'
    '90b0ZpbGUSSwoeZW52ZWxvcGVfZmx1c2hfdGhyZXNob2xkX2J5dGVzGEAgASgNQga6SAPIAQBS'
    'G2VudmVsb3BlRmx1c2hUaHJlc2hvbGRCeXRlcxJJCh1lbnZlbG9wZV9mbHVzaF90aHJlc2hvbG'
    'Rfcm93cxhBIAEoDUIGukgDyAEAUhplbnZlbG9wZUZsdXNoVGhyZXNob2xkUm93cxIiCgV0b3Bp'
    'YxhQIAEoCUIMukgJyAEAcgQQARgoUgV0b3BpYxI2ChBrYWZrYV9zY2hlbWFfdXJsGFEgASgJQg'
    'y6SAnIAQByBBABGDxSDmthZmthU2NoZW1hVXJsEl8KFWthZmthX3Byb2R1Y2VfdGltZW91dBhS'
    'IAEoCzIZLmdvb2dsZS5wcm90b2J1Zi5EdXJhdGlvbkIQukgNyAEAqgEHIgMI2AQyAFITa2Fma2'
    'FQcm9kdWNlVGltZW91dBIzChFrYWZrYV9jb21wcmVzc2lvbhhTIAEoCUIGukgDyAEAUhBrYWZr'
    'YUNvbXByZXNzaW9uEicKC3MzX2VuZHBvaW50GGQgASgJQga6SAPIAQBSCnMzRW5kcG9pbnQSIw'
    'oJczNfcmVnaW9uGGUgASgJQga6SAPIAQBSCHMzUmVnaW9uEiMKCXMzX2J1Y2tldBhmIAEoCUIG'
    'ukgDyAEAUghzM0J1Y2tldBIjCglzM19wcmVmaXgYZyABKAlCBrpIA8gBAFIIczNQcmVmaXgSKg'
    'oNczNfYWNjZXNzX2tleRhoIAEoCUIGukgDyAEAUgtzM0FjY2Vzc0tleRIqCg1zM19zZWNyZXRf'
    'a2V5GGkgASgJQga6SAPIAQBSC3MzU2VjcmV0S2V5EjcKFHMzX3NraXBfYnVja2V0X3Byb2JlGG'
    'ogASgIQga6SAPIAQBSEXMzU2tpcEJ1Y2tldFByb2JlEjIKEXMzX2FsbG93X2luc2VjdXJlGGsg'
    'ASgIQga6SAPIAQBSD3MzQWxsb3dJbnNlY3VyZRIpCgpzM19jYV9maWxlGGwgASgJQgu6SAjIAQ'
    'ByAxiAIFIIczNDYUZpbGUSTgogczNfcGFycXVldF9mbHVzaF90aHJlc2hvbGRfYnl0ZXMYbiAB'
    'KA1CBrpIA8gBAFIcczNQYXJxdWV0Rmx1c2hUaHJlc2hvbGRCeXRlcxJSChFzM19mbHVzaF9pbn'
    'RlcnZhbBhvIAEoCzIZLmdvb2dsZS5wcm90b2J1Zi5EdXJhdGlvbkILukgIyAEAqgECMgBSD3Mz'
    'Rmx1c2hJbnRlcnZhbBI5ChNzM19mbHVzaF9qaXR0ZXJfcGN0GHAgASgNQgq6SAfIAQAqAhhkUh'
    'BzM0ZsdXNoSml0dGVyUGN0EkwKHXMzX2ZsdXNoX3RocmVzaG9sZF9qaXR0ZXJfcGN0GHEgASgN'
    'Qgq6SAfIAQAqAhhkUhlzM0ZsdXNoVGhyZXNob2xkSml0dGVyUGN0EkEKFnMzX3VwbG9hZF9tYX'
    'hfYXR0ZW1wdHMYciABKA1CDLpICcgBACoEGGQoAVITczNVcGxvYWRNYXhBdHRlbXB0cxJZChVz'
    'M191cGxvYWRfYmFja29mZl9jYXAYcyABKAsyGS5nb29nbGUucHJvdG9idWYuRHVyYXRpb25CC7'
    'pICMgBAKoBAjIAUhJzM1VwbG9hZEJhY2tvZmZDYXASMQoOczNfcXVldWVfZGVwdGgYdCABKA1C'
    'C7pICMgBACoDGIAgUgxzM1F1ZXVlRGVwdGgSOQoVczNfbWF4X2J1ZmZlcmVkX2J5dGVzGHUgAS'
    'gEQga6SAPIAQBSEnMzTWF4QnVmZmVyZWRCeXRlcxIoCghob3N0bmFtZRiCASABKAlCC7pICMgB'
    'AHIDGP0BUghob3N0bmFtZRIoCghsb2NhdGlvbhiDASABKAlCC7pICMgBAHIDGP0BUghsb2NhdG'
    'lvbhIhCgVsYWJlbBiEASABKAlCCrpIB8gBAHICGChSBWxhYmVsEh0KA3RhZxiFASABKAlCCrpI'
    'B8gBAHICGChSA3RhZxIzCg5kYWVtb25fdmVyc2lvbhiGASABKAlCC7pICMgBAHIDGP0BUg1kYW'
    'Vtb25WZXJzaW9uEicKCGlwdjRfdHRsGJYBIAEoDUILukgIyAEAKgMY/wFSB2lwdjRUdGwSMgoO'
    'aXB2Nl9ob3BfbGltaXQYlwEgASgNQgu6SAjIAQAqAxj/AVIMaXB2NkhvcExpbWl0EkoKDWxpc3'
    'RlbmVyX2F1dGgYmAEgASgLMhwueHRjcF9jb25maWcudjEuTGlzdGVuZXJBdXRoQga6SAPIAQBS'
    'DGxpc3RlbmVyQXV0aBJaChNwcm9tZXRoZXVzX2xpc3RlbmVyGJkBIAEoCzIgLnh0Y3BfY29uZm'
    'lnLnYxLkxpc3RlbmVyRW5kcG9pbnRCBrpIA8gBAFIScHJvbWV0aGV1c0xpc3RlbmVyElUKE2Fk'
    'bWluX2xpc3RlbmVyX2F1dGgYmgEgASgLMhwueHRjcF9jb25maWcudjEuTGlzdGVuZXJBdXRoQg'
    'a6SAPIAQBSEWFkbWluTGlzdGVuZXJBdXRoEicKCWdycGNfcG9ydBigASABKA1CCbpIBioEGP//'
    'A1IIZ3JwY1BvcnQSTgoNZ3JwY19saXN0ZW5lchihASABKAsyIC54dGNwX2NvbmZpZy52MS5MaX'
    'N0ZW5lckVuZHBvaW50Qga6SAPIAQBSDGdycGNMaXN0ZW5lchJBCglsaXN0ZW5lcnMYogEgAygL'
    'MhgueHRjcF9jb25maWcudjEuTGlzdGVuZXJCCLpIBZIBAhAQUglsaXN0ZW5lcnMSLAoNcHlyb3'
    'Njb3BlX3VybBiqASABKAlCBrpIA8gBAFIMcHlyb3Njb3BlVXJsEjUKEnB5cm9zY29wZV9hcHBf'
    'bmFtZRirASABKAlCBrpIA8gBAFIQcHlyb3Njb3BlQXBwTmFtZRI3ChNweXJvc2NvcGVfc2FtcG'
    'xlX2h6GKwBIAEoDUIGukgDyAEAUhFweXJvc2NvcGVTYW1wbGVIehJKCh1weXJvc2NvcGVfdXBs'
    'b2FkX2ludGVydmFsX3NlYxitASABKA1CBrpIA8gBAFIacHlyb3Njb3BlVXBsb2FkSW50ZXJ2YW'
    'xTZWMSOQoUcmVzb2x2ZV9jb250YWluZXJfaWQYyAEgASgIQga6SAPIAQBSEnJlc29sdmVDb250'
    'YWluZXJJZBI3ChdlbnJpY2hfY29udGFpbmVyX2VuYWJsZRjJASABKAhSFWVucmljaENvbnRhaW'
    '5lckVuYWJsZRI3ChJkb2NrZXJfc29ja2V0X3BhdGgYygEgASgJQgi6SAVyAxj/AVIQZG9ja2Vy'
    'U29ja2V0UGF0aBItChJlbnJpY2hfbGxkcF9lbmFibGUY0gEgASgIUhBlbnJpY2hMbGRwRW5hYm'
    'xlEjUKEWxsZHBkX3NvY2tldF9wYXRoGNMBIAEoCUIIukgFcgMY/wFSD2xsZHBkU29ja2V0UGF0'
    'aBI2ChJsbGRwZF92ZXJzaW9uX2hpbnQY1AEgASgJQge6SARyAhgQUhBsbGRwZFZlcnNpb25IaW'
    '50EisKEWVucmljaF9uaWNfZW5hYmxlGNwBIAEoCFIPZW5yaWNoTmljRW5hYmxlEisKDHVwbGlu'
    'a19jb3VudBjdASABKA1CB7pIBCoCGAJSC3VwbGlua0NvdW50EjYKEXVwbGlua19pbnRlcmZhY2'
    'VzGN4BIAMoCUIIukgFkgECEAJSEHVwbGlua0ludGVyZmFjZXMSJAoNcG9wdWxhdGVfbnNpZBjm'
    'ASABKAhSDHBvcHVsYXRlTnNpZBIrChFlbnJpY2hfYXNuX2VuYWJsZRjwASABKAhSD2VucmljaE'
    'FzbkVuYWJsZRIpCgthc25fZGJfcGF0aBjxASABKAlCCLpIBXIDGP8BUglhc25EYlBhdGgSTAoU'
    'YXNuX3JlZnJlc2hfaW50ZXJ2YWwY8gEgASgLMhkuZ29vZ2xlLnByb3RvYnVmLkR1cmF0aW9uUh'
    'Jhc25SZWZyZXNoSW50ZXJ2YWwSPQoVaXBtZXRhX2Jvb3RzdHJhcF9wYXRoGPMBIAEoCUIIukgF'
    'cgMY/wFSE2lwbWV0YUJvb3RzdHJhcFBhdGgSNQoRaXBtZXRhX2NhY2hlX3BhdGgY9AEgASgJQg'
    'i6SAVyAxj/AVIPaXBtZXRhQ2FjaGVQYXRoEjUKFmVucmljaF9sb2NhbGl0eV9lbmFibGUY9QEg'
    'ASgIUhRlbnJpY2hMb2NhbGl0eUVuYWJsZRJWChlsb2NhbGl0eV9yZWZyZXNoX2ludGVydmFsGP'
    'YBIAEoCzIZLmdvb2dsZS5wcm90b2J1Zi5EdXJhdGlvblIXbG9jYWxpdHlSZWZyZXNoSW50ZXJ2'
    'YWwSNQoWbG9jYWxpdHlfZXZlbnRzX2VuYWJsZRj3ASABKAhSFGxvY2FsaXR5RXZlbnRzRW5hYm'
    'xlEkUKDGxpbmtfbW9uaXRvchj6ASABKAsyIS54dGNwX2NvbmZpZy52MS5MaW5rTW9uaXRvckNv'
    'bmZpZ1ILbGlua01vbml0b3I6c7pIcBpuCg9YdGNwQ29uZmlnLnBvbGwSMlBvbGwgdGltZW91dC'
    'BtdXN0IGJlIGxlc3MgdGhhbiBwb2xsIHBvbGxfZnJlcXVlbmN5Gid0aGlzLnBvbGxfZnJlcXVl'
    'bmN5ID4gdGhpcy5wb2xsX3RpbWVvdXQ=');

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
