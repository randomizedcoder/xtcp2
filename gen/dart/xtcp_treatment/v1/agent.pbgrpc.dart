// This is a generated file - do not edit.
//
// Generated from xtcp_treatment/v1/agent.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, prefer_relative_imports

import 'dart:async' as $async;
import 'dart:core' as $core;

import 'package:grpc/service_api.dart' as $grpc;
import 'package:protobuf/protobuf.dart' as $pb;

import 'agent.pb.dart' as $0;

export 'agent.pb.dart';

@$pb.GrpcServiceName('xtcp_treatment.v1.AgentControlService')
class AgentControlServiceClient extends $grpc.Client {
  /// The hostname for this service.
  static const $core.String defaultHost = '';

  /// OAuth scopes needed for the client.
  static const $core.List<$core.String> oauthScopes = [
    '',
  ];

  AgentControlServiceClient(super.channel, {super.options, super.interceptors});

  $grpc.ResponseFuture<$0.CheckInResponse> checkIn(
    $0.CheckInRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$checkIn, request, options: options);
  }

  $grpc.ResponseFuture<$0.GetDesiredStateResponse> getDesiredState(
    $0.GetDesiredStateRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$getDesiredState, request, options: options);
  }

  $grpc.ResponseStream<$0.WatchDesiredStateResponse> watchDesiredState(
    $0.WatchDesiredStateRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createStreamingCall(
        _$watchDesiredState, $async.Stream.fromIterable([request]),
        options: options);
  }

  // method descriptors

  static final _$checkIn =
      $grpc.ClientMethod<$0.CheckInRequest, $0.CheckInResponse>(
          '/xtcp_treatment.v1.AgentControlService/CheckIn',
          ($0.CheckInRequest value) => value.writeToBuffer(),
          $0.CheckInResponse.fromBuffer);
  static final _$getDesiredState =
      $grpc.ClientMethod<$0.GetDesiredStateRequest, $0.GetDesiredStateResponse>(
          '/xtcp_treatment.v1.AgentControlService/GetDesiredState',
          ($0.GetDesiredStateRequest value) => value.writeToBuffer(),
          $0.GetDesiredStateResponse.fromBuffer);
  static final _$watchDesiredState = $grpc.ClientMethod<
          $0.WatchDesiredStateRequest, $0.WatchDesiredStateResponse>(
      '/xtcp_treatment.v1.AgentControlService/WatchDesiredState',
      ($0.WatchDesiredStateRequest value) => value.writeToBuffer(),
      $0.WatchDesiredStateResponse.fromBuffer);
}

@$pb.GrpcServiceName('xtcp_treatment.v1.AgentControlService')
abstract class AgentControlServiceBase extends $grpc.Service {
  $core.String get $name => 'xtcp_treatment.v1.AgentControlService';

  AgentControlServiceBase() {
    $addMethod($grpc.ServiceMethod<$0.CheckInRequest, $0.CheckInResponse>(
        'CheckIn',
        checkIn_Pre,
        false,
        false,
        ($core.List<$core.int> value) => $0.CheckInRequest.fromBuffer(value),
        ($0.CheckInResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.GetDesiredStateRequest,
            $0.GetDesiredStateResponse>(
        'GetDesiredState',
        getDesiredState_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.GetDesiredStateRequest.fromBuffer(value),
        ($0.GetDesiredStateResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.WatchDesiredStateRequest,
            $0.WatchDesiredStateResponse>(
        'WatchDesiredState',
        watchDesiredState_Pre,
        false,
        true,
        ($core.List<$core.int> value) =>
            $0.WatchDesiredStateRequest.fromBuffer(value),
        ($0.WatchDesiredStateResponse value) => value.writeToBuffer()));
  }

  $async.Future<$0.CheckInResponse> checkIn_Pre($grpc.ServiceCall $call,
      $async.Future<$0.CheckInRequest> $request) async {
    return checkIn($call, await $request);
  }

  $async.Future<$0.CheckInResponse> checkIn(
      $grpc.ServiceCall call, $0.CheckInRequest request);

  $async.Future<$0.GetDesiredStateResponse> getDesiredState_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.GetDesiredStateRequest> $request) async {
    return getDesiredState($call, await $request);
  }

  $async.Future<$0.GetDesiredStateResponse> getDesiredState(
      $grpc.ServiceCall call, $0.GetDesiredStateRequest request);

  $async.Stream<$0.WatchDesiredStateResponse> watchDesiredState_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.WatchDesiredStateRequest> $request) async* {
    yield* watchDesiredState($call, await $request);
  }

  $async.Stream<$0.WatchDesiredStateResponse> watchDesiredState(
      $grpc.ServiceCall call, $0.WatchDesiredStateRequest request);
}
