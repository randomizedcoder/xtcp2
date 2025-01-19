//
//  Generated code. Do not modify.
//  source: xtcp_config/v1/xtcp_config.proto
//
// @dart = 2.12

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names, library_prefixes
// ignore_for_file: non_constant_identifier_names, prefer_final_fields
// ignore_for_file: unnecessary_import, unnecessary_this, unused_import

import 'dart:convert' as $convert;
import 'dart:core' as $core;
import 'dart:typed_data' as $typed_data;

@$core.Deprecated('Use getRequestDescriptor instead')
const GetRequest$json = {
  '1': 'GetRequest',
};

/// Descriptor for `GetRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List getRequestDescriptor = $convert.base64Decode(
    'CgpHZXRSZXF1ZXN0');

@$core.Deprecated('Use getResponseDescriptor instead')
const GetResponse$json = {
  '1': 'GetResponse',
  '2': [
    {'1': 'config', '3': 1, '4': 1, '5': 11, '6': '.xtcp_config.v1.XtcpConfig', '10': 'config'},
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
    {'1': 'config', '3': 1, '4': 1, '5': 11, '6': '.xtcp_config.v1.XtcpConfig', '10': 'config'},
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
    {'1': 'config', '3': 1, '4': 1, '5': 11, '6': '.xtcp_config.v1.XtcpConfig', '10': 'config'},
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
    {'1': 'poll_frequency', '3': 20, '4': 1, '5': 11, '6': '.google.protobuf.Duration', '8': {}, '10': 'pollFrequency'},
    {'1': 'poll_timeout', '3': 30, '4': 1, '5': 11, '6': '.google.protobuf.Duration', '8': {}, '10': 'pollTimeout'},
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
    {'1': 'config', '3': 1, '4': 1, '5': 11, '6': '.xtcp_config.v1.XtcpConfig', '10': 'config'},
  ],
};

/// Descriptor for `SetPollFrequencyResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List setPollFrequencyResponseDescriptor = $convert.base64Decode(
    'ChhTZXRQb2xsRnJlcXVlbmN5UmVzcG9uc2USMgoGY29uZmlnGAEgASgLMhoueHRjcF9jb25maW'
    'cudjEuWHRjcENvbmZpZ1IGY29uZmln');

@$core.Deprecated('Use xtcpConfigDescriptor instead')
const XtcpConfig$json = {
  '1': 'XtcpConfig',
  '2': [
    {'1': 'nl_timeout_milliseconds', '3': 10, '4': 1, '5': 4, '8': {}, '10': 'nlTimeoutMilliseconds'},
    {'1': 'poll_frequency', '3': 20, '4': 1, '5': 11, '6': '.google.protobuf.Duration', '8': {}, '10': 'pollFrequency'},
    {'1': 'poll_timeout', '3': 30, '4': 1, '5': 11, '6': '.google.protobuf.Duration', '8': {}, '10': 'pollTimeout'},
    {'1': 'max_loops', '3': 40, '4': 1, '5': 4, '8': {}, '10': 'maxLoops'},
    {'1': 'netlinkers', '3': 50, '4': 1, '5': 13, '8': {}, '10': 'netlinkers'},
    {'1': 'netlinkers_done_chan_size', '3': 51, '4': 1, '5': 13, '8': {}, '10': 'netlinkersDoneChanSize'},
    {'1': 'nlmsg_seq', '3': 60, '4': 1, '5': 13, '8': {}, '10': 'nlmsgSeq'},
    {'1': 'packet_size', '3': 70, '4': 1, '5': 4, '8': {}, '10': 'packetSize'},
    {'1': 'packet_size_mply', '3': 80, '4': 1, '5': 13, '8': {}, '10': 'packetSizeMply'},
    {'1': 'write_files', '3': 90, '4': 1, '5': 13, '8': {}, '10': 'writeFiles'},
    {'1': 'capture_path', '3': 100, '4': 1, '5': 9, '8': {}, '10': 'capturePath'},
    {'1': 'modulus', '3': 110, '4': 1, '5': 4, '8': {}, '10': 'modulus'},
    {'1': 'marshal_to', '3': 120, '4': 1, '5': 9, '8': {}, '10': 'marshalTo'},
    {'1': 'dest', '3': 130, '4': 1, '5': 9, '8': {}, '10': 'dest'},
    {'1': 'topic', '3': 140, '4': 1, '5': 9, '8': {}, '10': 'topic'},
    {'1': 'kafka_produce_timeout', '3': 150, '4': 1, '5': 11, '6': '.google.protobuf.Duration', '8': {}, '10': 'kafkaProduceTimeout'},
    {'1': 'debug_level', '3': 160, '4': 1, '5': 13, '8': {}, '10': 'debugLevel'},
    {'1': 'label', '3': 170, '4': 1, '5': 9, '8': {}, '10': 'label'},
    {'1': 'tag', '3': 180, '4': 1, '5': 9, '8': {}, '10': 'tag'},
    {'1': 'grpc_port', '3': 190, '4': 1, '5': 13, '8': {}, '10': 'grpcPort'},
    {'1': 'enabled_deserializers', '3': 200, '4': 1, '5': 11, '6': '.xtcp_config.v1.EnabledDeserializers', '8': {}, '10': 'enabledDeserializers'},
  ],
  '7': {},
};

/// Descriptor for `XtcpConfig`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List xtcpConfigDescriptor = $convert.base64Decode(
    'CgpYdGNwQ29uZmlnEkYKF25sX3RpbWVvdXRfbWlsbGlzZWNvbmRzGAogASgEQg66SAvIAQEyBh'
    'igjQYoAFIVbmxUaW1lb3V0TWlsbGlzZWNvbmRzElMKDnBvbGxfZnJlcXVlbmN5GBQgASgLMhku'
    'Z29vZ2xlLnByb3RvYnVmLkR1cmF0aW9uQhG6SA7IAQGqAQgiBAiA9SQqAFINcG9sbEZyZXF1ZW'
    '5jeRJPCgxwb2xsX3RpbWVvdXQYHiABKAsyGS5nb29nbGUucHJvdG9idWYuRHVyYXRpb25CEbpI'
    'DsgBAaoBCCIECID1JCoAUgtwb2xsVGltZW91dBIrCgltYXhfbG9vcHMYKCABKARCDrpIC8gBAD'
    'IGGKCNBigAUghtYXhMb29wcxIsCgpuZXRsaW5rZXJzGDIgASgNQgy6SAnIAQEqBBhkKAFSCm5l'
    'dGxpbmtlcnMSSAoZbmV0bGlua2Vyc19kb25lX2NoYW5fc2l6ZRgzIAEoDUINukgKyAEBKgUY6A'
    'coAVIWbmV0bGlua2Vyc0RvbmVDaGFuU2l6ZRIqCglubG1zZ19zZXEYPCABKA1CDbpICsgBASoF'
    'GJBOKABSCG5sbXNnU2VxEi8KC3BhY2tldF9zaXplGEYgASgEQg66SAvIAQAyBhjAhD0oAFIKcG'
    'Fja2V0U2l6ZRI2ChBwYWNrZXRfc2l6ZV9tcGx5GFAgASgNQgy6SAnIAQAqBBhkKABSDnBhY2tl'
    'dFNpemVNcGx5Ei4KC3dyaXRlX2ZpbGVzGFogASgNQg26SArIAQAqBRjoBygAUgp3cml0ZUZpbG'
    'VzEi8KDGNhcHR1cmVfcGF0aBhkIAEoCUIMukgJyAEAcgQQARhQUgtjYXB0dXJlUGF0aBIoCgdt'
    'b2R1bHVzGG4gASgEQg66SAvIAQEyBhjAhD0oAVIHbW9kdWx1cxIrCgptYXJzaGFsX3RvGHggAS'
    'gJQgy6SAnIAQFyBBAEGChSCW1hcnNoYWxUbxIhCgRkZXN0GIIBIAEoCUIMukgJyAEBcgQQBBgo'
    'UgRkZXN0EiMKBXRvcGljGIwBIAEoCUIMukgJyAEAcgQQARgoUgV0b3BpYxJgChVrYWZrYV9wcm'
    '9kdWNlX3RpbWVvdXQYlgEgASgLMhkuZ29vZ2xlLnByb3RvYnVmLkR1cmF0aW9uQhC6SA3IAQCq'
    'AQciAwjYBDIAUhNrYWZrYVByb2R1Y2VUaW1lb3V0Ei8KC2RlYnVnX2xldmVsGKABIAEoDUINuk'
    'gKyAEBKgUY6AcoAFIKZGVidWdMZXZlbBIhCgVsYWJlbBiqASABKAlCCrpIB8gBAHICGChSBWxh'
    'YmVsEh0KA3RhZxi0ASABKAlCCrpIB8gBAHICGChSA3RhZxIsCglncnBjX3BvcnQYvgEgASgNQg'
    '66SAvIAQEqBhj//wMoAVIIZ3JwY1BvcnQSYgoVZW5hYmxlZF9kZXNlcmlhbGl6ZXJzGMgBIAEo'
    'CzIkLnh0Y3BfY29uZmlnLnYxLkVuYWJsZWREZXNlcmlhbGl6ZXJzQga6SAPIAQBSFGVuYWJsZW'
    'REZXNlcmlhbGl6ZXJzOnO6SHAabgoPWHRjcENvbmZpZy5wb2xsEjJQb2xsIHRpbWVvdXQgbXVz'
    'dCBiZSBsZXNzIHRoYW4gcG9sbCBwb2xsX2ZyZXF1ZW5jeRondGhpcy5wb2xsX2ZyZXF1ZW5jeS'
    'A+IHRoaXMucG9sbF90aW1lb3V0');

@$core.Deprecated('Use enabledDeserializersDescriptor instead')
const EnabledDeserializers$json = {
  '1': 'EnabledDeserializers',
  '2': [
    {'1': 'enabled', '3': 1, '4': 3, '5': 11, '6': '.xtcp_config.v1.EnabledDeserializers.EnabledEntry', '10': 'enabled'},
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

