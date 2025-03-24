//
//  Generated code. Do not modify.
//  source: xtcp_config/v1/xtcp_config.proto
//
// @dart = 2.12

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names, library_prefixes
// ignore_for_file: non_constant_identifier_names, prefer_final_fields
// ignore_for_file: unnecessary_import, unnecessary_this, unused_import

import 'dart:async' as $async;
import 'dart:core' as $core;

import 'package:grpc/service_api.dart' as $grpc;
import 'package:protobuf/protobuf.dart' as $pb;

import 'xtcp_config.pb.dart' as $0;

export 'xtcp_config.pb.dart';

@$pb.GrpcServiceName('xtcp_config.v1.ConfigService')
class ConfigServiceClient extends $grpc.Client {
  static final _$get = $grpc.ClientMethod<$0.GetRequest, $0.GetResponse>(
      '/xtcp_config.v1.ConfigService/Get',
      ($0.GetRequest value) => value.writeToBuffer(),
      ($core.List<$core.int> value) => $0.GetResponse.fromBuffer(value));
  static final _$set = $grpc.ClientMethod<$0.SetRequest, $0.SetResponse>(
      '/xtcp_config.v1.ConfigService/Set',
      ($0.SetRequest value) => value.writeToBuffer(),
      ($core.List<$core.int> value) => $0.SetResponse.fromBuffer(value));
  static final _$setPollFrequency = $grpc.ClientMethod<$0.SetPollFrequencyRequest, $0.SetPollFrequencyResponse>(
      '/xtcp_config.v1.ConfigService/SetPollFrequency',
      ($0.SetPollFrequencyRequest value) => value.writeToBuffer(),
      ($core.List<$core.int> value) => $0.SetPollFrequencyResponse.fromBuffer(value));

  ConfigServiceClient($grpc.ClientChannel channel,
      {$grpc.CallOptions? options,
      $core.Iterable<$grpc.ClientInterceptor>? interceptors})
      : super(channel, options: options,
        interceptors: interceptors);

  $grpc.ResponseFuture<$0.GetResponse> get($0.GetRequest request, {$grpc.CallOptions? options}) {
    return $createUnaryCall(_$get, request, options: options);
  }

  $grpc.ResponseFuture<$0.SetResponse> set($0.SetRequest request, {$grpc.CallOptions? options}) {
    return $createUnaryCall(_$set, request, options: options);
  }

  $grpc.ResponseFuture<$0.SetPollFrequencyResponse> setPollFrequency($0.SetPollFrequencyRequest request, {$grpc.CallOptions? options}) {
    return $createUnaryCall(_$setPollFrequency, request, options: options);
  }
}

@$pb.GrpcServiceName('xtcp_config.v1.ConfigService')
abstract class ConfigServiceBase extends $grpc.Service {
  $core.String get $name => 'xtcp_config.v1.ConfigService';

  ConfigServiceBase() {
    $addMethod($grpc.ServiceMethod<$0.GetRequest, $0.GetResponse>(
        'Get',
        get_Pre,
        false,
        false,
        ($core.List<$core.int> value) => $0.GetRequest.fromBuffer(value),
        ($0.GetResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.SetRequest, $0.SetResponse>(
        'Set',
        set_Pre,
        false,
        false,
        ($core.List<$core.int> value) => $0.SetRequest.fromBuffer(value),
        ($0.SetResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.SetPollFrequencyRequest, $0.SetPollFrequencyResponse>(
        'SetPollFrequency',
        setPollFrequency_Pre,
        false,
        false,
        ($core.List<$core.int> value) => $0.SetPollFrequencyRequest.fromBuffer(value),
        ($0.SetPollFrequencyResponse value) => value.writeToBuffer()));
  }

  $async.Future<$0.GetResponse> get_Pre($grpc.ServiceCall call, $async.Future<$0.GetRequest> request) async {
    return get(call, await request);
  }

  $async.Future<$0.SetResponse> set_Pre($grpc.ServiceCall call, $async.Future<$0.SetRequest> request) async {
    return set(call, await request);
  }

  $async.Future<$0.SetPollFrequencyResponse> setPollFrequency_Pre($grpc.ServiceCall call, $async.Future<$0.SetPollFrequencyRequest> request) async {
    return setPollFrequency(call, await request);
  }

  $async.Future<$0.GetResponse> get($grpc.ServiceCall call, $0.GetRequest request);
  $async.Future<$0.SetResponse> set($grpc.ServiceCall call, $0.SetRequest request);
  $async.Future<$0.SetPollFrequencyResponse> setPollFrequency($grpc.ServiceCall call, $0.SetPollFrequencyRequest request);
}
