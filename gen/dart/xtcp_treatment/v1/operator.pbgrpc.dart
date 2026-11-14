// This is a generated file - do not edit.
//
// Generated from xtcp_treatment/v1/operator.proto.

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

import 'operator.pb.dart' as $0;

export 'operator.pb.dart';

@$pb.GrpcServiceName('xtcp_treatment.v1.OperatorService')
class OperatorServiceClient extends $grpc.Client {
  /// The hostname for this service.
  static const $core.String defaultHost = '';

  /// OAuth scopes needed for the client.
  static const $core.List<$core.String> oauthScopes = [
    '',
  ];

  OperatorServiceClient(super.channel, {super.options, super.interceptors});

  $grpc.ResponseFuture<$0.ProposeResponse> propose(
    $0.ProposeRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$propose, request, options: options);
  }

  $grpc.ResponseFuture<$0.AnalyzeResponse> analyze(
    $0.AnalyzeRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$analyze, request, options: options);
  }

  $grpc.ResponseFuture<$0.ReviewResponse> review(
    $0.ReviewRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$review, request, options: options);
  }

  $grpc.ResponseFuture<$0.ControlResponse> control(
    $0.ControlRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$control, request, options: options);
  }

  $grpc.ResponseFuture<$0.InspectResponse> inspect(
    $0.InspectRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$inspect, request, options: options);
  }

  // method descriptors

  static final _$propose =
      $grpc.ClientMethod<$0.ProposeRequest, $0.ProposeResponse>(
          '/xtcp_treatment.v1.OperatorService/Propose',
          ($0.ProposeRequest value) => value.writeToBuffer(),
          $0.ProposeResponse.fromBuffer);
  static final _$analyze =
      $grpc.ClientMethod<$0.AnalyzeRequest, $0.AnalyzeResponse>(
          '/xtcp_treatment.v1.OperatorService/Analyze',
          ($0.AnalyzeRequest value) => value.writeToBuffer(),
          $0.AnalyzeResponse.fromBuffer);
  static final _$review =
      $grpc.ClientMethod<$0.ReviewRequest, $0.ReviewResponse>(
          '/xtcp_treatment.v1.OperatorService/Review',
          ($0.ReviewRequest value) => value.writeToBuffer(),
          $0.ReviewResponse.fromBuffer);
  static final _$control =
      $grpc.ClientMethod<$0.ControlRequest, $0.ControlResponse>(
          '/xtcp_treatment.v1.OperatorService/Control',
          ($0.ControlRequest value) => value.writeToBuffer(),
          $0.ControlResponse.fromBuffer);
  static final _$inspect =
      $grpc.ClientMethod<$0.InspectRequest, $0.InspectResponse>(
          '/xtcp_treatment.v1.OperatorService/Inspect',
          ($0.InspectRequest value) => value.writeToBuffer(),
          $0.InspectResponse.fromBuffer);
}

@$pb.GrpcServiceName('xtcp_treatment.v1.OperatorService')
abstract class OperatorServiceBase extends $grpc.Service {
  $core.String get $name => 'xtcp_treatment.v1.OperatorService';

  OperatorServiceBase() {
    $addMethod($grpc.ServiceMethod<$0.ProposeRequest, $0.ProposeResponse>(
        'Propose',
        propose_Pre,
        false,
        false,
        ($core.List<$core.int> value) => $0.ProposeRequest.fromBuffer(value),
        ($0.ProposeResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.AnalyzeRequest, $0.AnalyzeResponse>(
        'Analyze',
        analyze_Pre,
        false,
        false,
        ($core.List<$core.int> value) => $0.AnalyzeRequest.fromBuffer(value),
        ($0.AnalyzeResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.ReviewRequest, $0.ReviewResponse>(
        'Review',
        review_Pre,
        false,
        false,
        ($core.List<$core.int> value) => $0.ReviewRequest.fromBuffer(value),
        ($0.ReviewResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.ControlRequest, $0.ControlResponse>(
        'Control',
        control_Pre,
        false,
        false,
        ($core.List<$core.int> value) => $0.ControlRequest.fromBuffer(value),
        ($0.ControlResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.InspectRequest, $0.InspectResponse>(
        'Inspect',
        inspect_Pre,
        false,
        false,
        ($core.List<$core.int> value) => $0.InspectRequest.fromBuffer(value),
        ($0.InspectResponse value) => value.writeToBuffer()));
  }

  $async.Future<$0.ProposeResponse> propose_Pre($grpc.ServiceCall $call,
      $async.Future<$0.ProposeRequest> $request) async {
    return propose($call, await $request);
  }

  $async.Future<$0.ProposeResponse> propose(
      $grpc.ServiceCall call, $0.ProposeRequest request);

  $async.Future<$0.AnalyzeResponse> analyze_Pre($grpc.ServiceCall $call,
      $async.Future<$0.AnalyzeRequest> $request) async {
    return analyze($call, await $request);
  }

  $async.Future<$0.AnalyzeResponse> analyze(
      $grpc.ServiceCall call, $0.AnalyzeRequest request);

  $async.Future<$0.ReviewResponse> review_Pre(
      $grpc.ServiceCall $call, $async.Future<$0.ReviewRequest> $request) async {
    return review($call, await $request);
  }

  $async.Future<$0.ReviewResponse> review(
      $grpc.ServiceCall call, $0.ReviewRequest request);

  $async.Future<$0.ControlResponse> control_Pre($grpc.ServiceCall $call,
      $async.Future<$0.ControlRequest> $request) async {
    return control($call, await $request);
  }

  $async.Future<$0.ControlResponse> control(
      $grpc.ServiceCall call, $0.ControlRequest request);

  $async.Future<$0.InspectResponse> inspect_Pre($grpc.ServiceCall $call,
      $async.Future<$0.InspectRequest> $request) async {
    return inspect($call, await $request);
  }

  $async.Future<$0.InspectResponse> inspect(
      $grpc.ServiceCall call, $0.InspectRequest request);
}
