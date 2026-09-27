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
    'IQZmFpbHVyZUppdHRlck1heDrhAbpI3QEa2gEKGkxpc3RlbmVyQXV0aC5mYWlsdXJlSml0dGVy'
    'EkZmYWlsdXJlX2ppdHRlcl9tYXggbXVzdCBiZSBncmVhdGVyIHRoYW4gb3IgZXF1YWwgdG8gZm'
    'FpbHVyZV9qaXR0ZXJfbWluGnQhaGFzKHRoaXMuZmFpbHVyZV9qaXR0ZXJfbWluKSB8fCAhaGFz'
    'KHRoaXMuZmFpbHVyZV9qaXR0ZXJfbWF4KSB8fCB0aGlzLmZhaWx1cmVfaml0dGVyX21heCA+PS'
    'B0aGlzLmZhaWx1cmVfaml0dGVyX21pbkIcChpfc2lnbmVkX3Rva2VuX3NrZXdfbWludXRlcw==');

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
    'emUYGCABKA1CDbpICsgBACoFGIAgKAFSE2lvVXJpbmdDcWVCYXRjaFNpemUSVwoTcmVjb25jaW'
    'xlX2ZyZXF1ZW5jeRgoIAEoCzIZLmdvb2dsZS5wcm90b2J1Zi5EdXJhdGlvbkILukgIyAEAqgEC'
    'MgBSEnJlY29uY2lsZUZyZXF1ZW5jeRIyChVyZWNvbmNpbGVfYmVmb3JlX3BvbGwYKSABKAhSE3'
    'JlY29uY2lsZUJlZm9yZVBvbGwSLgoLd3JpdGVfZmlsZXMYMiABKA1CDbpICsgBACoFGOgHKABS'
    'CndyaXRlRmlsZXMSLwoMY2FwdHVyZV9wYXRoGDMgASgJQgy6SAnIAQByBBABGFBSC2NhcHR1cm'
    'VQYXRoEjcKEGRlc3Rfd3JpdGVfZmlsZXMYNCABKA1CDbpICsgBACoFGOgHKABSDmRlc3RXcml0'
    'ZUZpbGVzEi4KC2RlYnVnX2xldmVsGDUgASgNQg26SArIAQEqBRjoBygAUgpkZWJ1Z0xldmVsEi'
    'EKBGRlc3QYPCABKAlCDbpICsgBAXIFEAQYgARSBGRlc3QSKwoKbWFyc2hhbF90bxg9IAEoCUIM'
    'ukgJyAEBcgQQAxgoUgltYXJzaGFsVG8SJwoLY3N2X2NvbHVtbnMYPiABKAlCBrpIA8gBAFIKY3'
    'N2Q29sdW1ucxI0Cg94dGNwX3Byb3RvX2ZpbGUYPyABKAlCDLpICcgBAHIEEAEYUFINeHRjcFBy'
    'b3RvRmlsZRJLCh5lbnZlbG9wZV9mbHVzaF90aHJlc2hvbGRfYnl0ZXMYQCABKA1CBrpIA8gBAF'
    'IbZW52ZWxvcGVGbHVzaFRocmVzaG9sZEJ5dGVzEkkKHWVudmVsb3BlX2ZsdXNoX3RocmVzaG9s'
    'ZF9yb3dzGEEgASgNQga6SAPIAQBSGmVudmVsb3BlRmx1c2hUaHJlc2hvbGRSb3dzEiIKBXRvcG'
    'ljGFAgASgJQgy6SAnIAQByBBABGChSBXRvcGljEjYKEGthZmthX3NjaGVtYV91cmwYUSABKAlC'
    'DLpICcgBAHIEEAEYPFIOa2Fma2FTY2hlbWFVcmwSXwoVa2Fma2FfcHJvZHVjZV90aW1lb3V0GF'
    'IgASgLMhkuZ29vZ2xlLnByb3RvYnVmLkR1cmF0aW9uQhC6SA3IAQCqAQciAwjYBDIAUhNrYWZr'
    'YVByb2R1Y2VUaW1lb3V0EjMKEWthZmthX2NvbXByZXNzaW9uGFMgASgJQga6SAPIAQBSEGthZm'
    'thQ29tcHJlc3Npb24SJwoLczNfZW5kcG9pbnQYZCABKAlCBrpIA8gBAFIKczNFbmRwb2ludBIj'
    'CglzM19yZWdpb24YZSABKAlCBrpIA8gBAFIIczNSZWdpb24SIwoJczNfYnVja2V0GGYgASgJQg'
    'a6SAPIAQBSCHMzQnVja2V0EiMKCXMzX3ByZWZpeBhnIAEoCUIGukgDyAEAUghzM1ByZWZpeBIq'
    'Cg1zM19hY2Nlc3Nfa2V5GGggASgJQga6SAPIAQBSC3MzQWNjZXNzS2V5EioKDXMzX3NlY3JldF'
    '9rZXkYaSABKAlCBrpIA8gBAFILczNTZWNyZXRLZXkSNwoUczNfc2tpcF9idWNrZXRfcHJvYmUY'
    'aiABKAhCBrpIA8gBAFIRczNTa2lwQnVja2V0UHJvYmUSTgogczNfcGFycXVldF9mbHVzaF90aH'
    'Jlc2hvbGRfYnl0ZXMYbiABKA1CBrpIA8gBAFIcczNQYXJxdWV0Rmx1c2hUaHJlc2hvbGRCeXRl'
    'cxJSChFzM19mbHVzaF9pbnRlcnZhbBhvIAEoCzIZLmdvb2dsZS5wcm90b2J1Zi5EdXJhdGlvbk'
    'ILukgIyAEAqgECMgBSD3MzRmx1c2hJbnRlcnZhbBI5ChNzM19mbHVzaF9qaXR0ZXJfcGN0GHAg'
    'ASgNQgq6SAfIAQAqAhhkUhBzM0ZsdXNoSml0dGVyUGN0EkwKHXMzX2ZsdXNoX3RocmVzaG9sZF'
    '9qaXR0ZXJfcGN0GHEgASgNQgq6SAfIAQAqAhhkUhlzM0ZsdXNoVGhyZXNob2xkSml0dGVyUGN0'
    'EkEKFnMzX3VwbG9hZF9tYXhfYXR0ZW1wdHMYciABKA1CDLpICcgBACoEGGQoAVITczNVcGxvYW'
    'RNYXhBdHRlbXB0cxJZChVzM191cGxvYWRfYmFja29mZl9jYXAYcyABKAsyGS5nb29nbGUucHJv'
    'dG9idWYuRHVyYXRpb25CC7pICMgBAKoBAjIAUhJzM1VwbG9hZEJhY2tvZmZDYXASKAoIaG9zdG'
    '5hbWUYggEgASgJQgu6SAjIAQByAxj9AVIIaG9zdG5hbWUSKAoIbG9jYXRpb24YgwEgASgJQgu6'
    'SAjIAQByAxj9AVIIbG9jYXRpb24SIQoFbGFiZWwYhAEgASgJQgq6SAfIAQByAhgoUgVsYWJlbB'
    'IdCgN0YWcYhQEgASgJQgq6SAfIAQByAhgoUgN0YWcSMwoOZGFlbW9uX3ZlcnNpb24YhgEgASgJ'
    'Qgu6SAjIAQByAxj9AVINZGFlbW9uVmVyc2lvbhInCghpcHY0X3R0bBiWASABKA1CC7pICMgBAC'
    'oDGP8BUgdpcHY0VHRsEjIKDmlwdjZfaG9wX2xpbWl0GJcBIAEoDUILukgIyAEAKgMY/wFSDGlw'
    'djZIb3BMaW1pdBJKCg1saXN0ZW5lcl9hdXRoGJgBIAEoCzIcLnh0Y3BfY29uZmlnLnYxLkxpc3'
    'RlbmVyQXV0aEIGukgDyAEAUgxsaXN0ZW5lckF1dGgSWgoTcHJvbWV0aGV1c19saXN0ZW5lchiZ'
    'ASABKAsyIC54dGNwX2NvbmZpZy52MS5MaXN0ZW5lckVuZHBvaW50Qga6SAPIAQBSEnByb21ldG'
    'hldXNMaXN0ZW5lchIsCglncnBjX3BvcnQYoAEgASgNQg66SAvIAQEqBhj//wMoAVIIZ3JwY1Bv'
    'cnQSTgoNZ3JwY19saXN0ZW5lchihASABKAsyIC54dGNwX2NvbmZpZy52MS5MaXN0ZW5lckVuZH'
    'BvaW50Qga6SAPIAQBSDGdycGNMaXN0ZW5lchIsCg1weXJvc2NvcGVfdXJsGKoBIAEoCUIGukgD'
    'yAEAUgxweXJvc2NvcGVVcmwSNQoScHlyb3Njb3BlX2FwcF9uYW1lGKsBIAEoCUIGukgDyAEAUh'
    'BweXJvc2NvcGVBcHBOYW1lEjcKE3B5cm9zY29wZV9zYW1wbGVfaHoYrAEgASgNQga6SAPIAQBS'
    'EXB5cm9zY29wZVNhbXBsZUh6EkoKHXB5cm9zY29wZV91cGxvYWRfaW50ZXJ2YWxfc2VjGK0BIA'
    'EoDUIGukgDyAEAUhpweXJvc2NvcGVVcGxvYWRJbnRlcnZhbFNlYxI5ChRyZXNvbHZlX2NvbnRh'
    'aW5lcl9pZBjIASABKAhCBrpIA8gBAFIScmVzb2x2ZUNvbnRhaW5lcklkEjcKF2VucmljaF9jb2'
    '50YWluZXJfZW5hYmxlGMkBIAEoCFIVZW5yaWNoQ29udGFpbmVyRW5hYmxlEjcKEmRvY2tlcl9z'
    'b2NrZXRfcGF0aBjKASABKAlCCLpIBXIDGP8BUhBkb2NrZXJTb2NrZXRQYXRoEi0KEmVucmljaF'
    '9sbGRwX2VuYWJsZRjSASABKAhSEGVucmljaExsZHBFbmFibGUSNQoRbGxkcGRfc29ja2V0X3Bh'
    'dGgY0wEgASgJQgi6SAVyAxj/AVIPbGxkcGRTb2NrZXRQYXRoEjYKEmxsZHBkX3ZlcnNpb25faG'
    'ludBjUASABKAlCB7pIBHICGBBSEGxsZHBkVmVyc2lvbkhpbnQSKwoRZW5yaWNoX25pY19lbmFi'
    'bGUY3AEgASgIUg9lbnJpY2hOaWNFbmFibGUSKwoMdXBsaW5rX2NvdW50GN0BIAEoDUIHukgEKg'
    'IYAlILdXBsaW5rQ291bnQSNgoRdXBsaW5rX2ludGVyZmFjZXMY3gEgAygJQgi6SAWSAQIQAlIQ'
    'dXBsaW5rSW50ZXJmYWNlcxIkCg1wb3B1bGF0ZV9uc2lkGOYBIAEoCFIMcG9wdWxhdGVOc2lkEi'
    'sKEWVucmljaF9hc25fZW5hYmxlGPABIAEoCFIPZW5yaWNoQXNuRW5hYmxlEikKC2Fzbl9kYl9w'
    'YXRoGPEBIAEoCUIIukgFcgMY/wFSCWFzbkRiUGF0aBJMChRhc25fcmVmcmVzaF9pbnRlcnZhbB'
    'jyASABKAsyGS5nb29nbGUucHJvdG9idWYuRHVyYXRpb25SEmFzblJlZnJlc2hJbnRlcnZhbBI1'
    'ChZlbnJpY2hfbG9jYWxpdHlfZW5hYmxlGPUBIAEoCFIUZW5yaWNoTG9jYWxpdHlFbmFibGUSVg'
    'oZbG9jYWxpdHlfcmVmcmVzaF9pbnRlcnZhbBj2ASABKAsyGS5nb29nbGUucHJvdG9idWYuRHVy'
    'YXRpb25SF2xvY2FsaXR5UmVmcmVzaEludGVydmFsOnO6SHAabgoPWHRjcENvbmZpZy5wb2xsEj'
    'JQb2xsIHRpbWVvdXQgbXVzdCBiZSBsZXNzIHRoYW4gcG9sbCBwb2xsX2ZyZXF1ZW5jeRondGhp'
    'cy5wb2xsX2ZyZXF1ZW5jeSA+IHRoaXMucG9sbF90aW1lb3V0');

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
