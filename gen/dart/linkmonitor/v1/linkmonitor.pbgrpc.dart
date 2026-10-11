// This is a generated file - do not edit.
//
// Generated from linkmonitor/v1/linkmonitor.proto.

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

import 'linkmonitor.pb.dart' as $0;

export 'linkmonitor.pb.dart';

@$pb.GrpcServiceName('linkmonitor.v1.LinkMonitorService')
class LinkMonitorServiceClient extends $grpc.Client {
  /// The hostname for this service.
  static const $core.String defaultHost = '';

  /// OAuth scopes needed for the client.
  static const $core.List<$core.String> oauthScopes = [
    '',
  ];

  LinkMonitorServiceClient(super.channel, {super.options, super.interceptors});

  /// Health, link counts, baseline and control progress from one snapshot.
  $grpc.ResponseFuture<$0.GetStatusResponse> getStatus(
    $0.GetStatusRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$getStatus, request, options: options);
  }

  /// Per-interface identity, operational state and policy checks.
  $grpc.ResponseFuture<$0.ListInterfacesResponse> listInterfaces(
    $0.ListInterfacesRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$listInterfaces, request, options: options);
  }

  /// Per-interface and host collector diagnostics.
  $grpc.ResponseFuture<$0.ListCollectorsResponse> listCollectors(
    $0.ListCollectorsRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$listCollectors, request, options: options);
  }

  /// Reconcile and durably replace the expected link count.
  $grpc.ResponseFuture<$0.RebaselineResponse> rebaseline(
    $0.RebaselineRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$rebaseline, request, options: options);
  }

  /// Reconcile inventory against the kernel.
  $grpc.ResponseFuture<$0.ResyncResponse> resync(
    $0.ResyncRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$resync, request, options: options);
  }

  // method descriptors

  static final _$getStatus =
      $grpc.ClientMethod<$0.GetStatusRequest, $0.GetStatusResponse>(
          '/linkmonitor.v1.LinkMonitorService/GetStatus',
          ($0.GetStatusRequest value) => value.writeToBuffer(),
          $0.GetStatusResponse.fromBuffer);
  static final _$listInterfaces =
      $grpc.ClientMethod<$0.ListInterfacesRequest, $0.ListInterfacesResponse>(
          '/linkmonitor.v1.LinkMonitorService/ListInterfaces',
          ($0.ListInterfacesRequest value) => value.writeToBuffer(),
          $0.ListInterfacesResponse.fromBuffer);
  static final _$listCollectors =
      $grpc.ClientMethod<$0.ListCollectorsRequest, $0.ListCollectorsResponse>(
          '/linkmonitor.v1.LinkMonitorService/ListCollectors',
          ($0.ListCollectorsRequest value) => value.writeToBuffer(),
          $0.ListCollectorsResponse.fromBuffer);
  static final _$rebaseline =
      $grpc.ClientMethod<$0.RebaselineRequest, $0.RebaselineResponse>(
          '/linkmonitor.v1.LinkMonitorService/Rebaseline',
          ($0.RebaselineRequest value) => value.writeToBuffer(),
          $0.RebaselineResponse.fromBuffer);
  static final _$resync =
      $grpc.ClientMethod<$0.ResyncRequest, $0.ResyncResponse>(
          '/linkmonitor.v1.LinkMonitorService/Resync',
          ($0.ResyncRequest value) => value.writeToBuffer(),
          $0.ResyncResponse.fromBuffer);
}

@$pb.GrpcServiceName('linkmonitor.v1.LinkMonitorService')
abstract class LinkMonitorServiceBase extends $grpc.Service {
  $core.String get $name => 'linkmonitor.v1.LinkMonitorService';

  LinkMonitorServiceBase() {
    $addMethod($grpc.ServiceMethod<$0.GetStatusRequest, $0.GetStatusResponse>(
        'GetStatus',
        getStatus_Pre,
        false,
        false,
        ($core.List<$core.int> value) => $0.GetStatusRequest.fromBuffer(value),
        ($0.GetStatusResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.ListInterfacesRequest,
            $0.ListInterfacesResponse>(
        'ListInterfaces',
        listInterfaces_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.ListInterfacesRequest.fromBuffer(value),
        ($0.ListInterfacesResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.ListCollectorsRequest,
            $0.ListCollectorsResponse>(
        'ListCollectors',
        listCollectors_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.ListCollectorsRequest.fromBuffer(value),
        ($0.ListCollectorsResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.RebaselineRequest, $0.RebaselineResponse>(
        'Rebaseline',
        rebaseline_Pre,
        false,
        false,
        ($core.List<$core.int> value) => $0.RebaselineRequest.fromBuffer(value),
        ($0.RebaselineResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.ResyncRequest, $0.ResyncResponse>(
        'Resync',
        resync_Pre,
        false,
        false,
        ($core.List<$core.int> value) => $0.ResyncRequest.fromBuffer(value),
        ($0.ResyncResponse value) => value.writeToBuffer()));
  }

  $async.Future<$0.GetStatusResponse> getStatus_Pre($grpc.ServiceCall $call,
      $async.Future<$0.GetStatusRequest> $request) async {
    return getStatus($call, await $request);
  }

  $async.Future<$0.GetStatusResponse> getStatus(
      $grpc.ServiceCall call, $0.GetStatusRequest request);

  $async.Future<$0.ListInterfacesResponse> listInterfaces_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.ListInterfacesRequest> $request) async {
    return listInterfaces($call, await $request);
  }

  $async.Future<$0.ListInterfacesResponse> listInterfaces(
      $grpc.ServiceCall call, $0.ListInterfacesRequest request);

  $async.Future<$0.ListCollectorsResponse> listCollectors_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.ListCollectorsRequest> $request) async {
    return listCollectors($call, await $request);
  }

  $async.Future<$0.ListCollectorsResponse> listCollectors(
      $grpc.ServiceCall call, $0.ListCollectorsRequest request);

  $async.Future<$0.RebaselineResponse> rebaseline_Pre($grpc.ServiceCall $call,
      $async.Future<$0.RebaselineRequest> $request) async {
    return rebaseline($call, await $request);
  }

  $async.Future<$0.RebaselineResponse> rebaseline(
      $grpc.ServiceCall call, $0.RebaselineRequest request);

  $async.Future<$0.ResyncResponse> resync_Pre(
      $grpc.ServiceCall $call, $async.Future<$0.ResyncRequest> $request) async {
    return resync($call, await $request);
  }

  $async.Future<$0.ResyncResponse> resync(
      $grpc.ServiceCall call, $0.ResyncRequest request);
}
