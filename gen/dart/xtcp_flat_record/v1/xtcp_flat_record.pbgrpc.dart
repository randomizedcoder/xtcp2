// This is a generated file - do not edit.
//
// Generated from xtcp_flat_record/v1/xtcp_flat_record.proto.

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

import 'xtcp_flat_record.pb.dart' as $0;

export 'xtcp_flat_record.pb.dart';

@$pb.GrpcServiceName('xtcp_flat_record.v1.XTCPFlatRecordService')
class XTCPFlatRecordServiceClient extends $grpc.Client {
  /// The hostname for this service.
  static const $core.String defaultHost = '';

  /// OAuth scopes needed for the client.
  static const $core.List<$core.String> oauthScopes = [
    '',
  ];

  XTCPFlatRecordServiceClient(super.channel,
      {super.options, super.interceptors});

  /// If xtcp is polling, this will return the stream
  $grpc.ResponseStream<$0.FlatRecordsResponse> flatRecords(
    $0.FlatRecordsRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createStreamingCall(
        _$flatRecords, $async.Stream.fromIterable([request]),
        options: options);
  }

  /// If xtcp is not polling, this allows the client to send a poll request
  $grpc.ResponseStream<$0.PollFlatRecordsResponse> pollFlatRecords(
    $async.Stream<$0.PollFlatRecordsRequest> request, {
    $grpc.CallOptions? options,
  }) {
    return $createStreamingCall(_$pollFlatRecords, request, options: options);
  }

  // method descriptors

  static final _$flatRecords =
      $grpc.ClientMethod<$0.FlatRecordsRequest, $0.FlatRecordsResponse>(
          '/xtcp_flat_record.v1.XTCPFlatRecordService/FlatRecords',
          ($0.FlatRecordsRequest value) => value.writeToBuffer(),
          $0.FlatRecordsResponse.fromBuffer);
  static final _$pollFlatRecords =
      $grpc.ClientMethod<$0.PollFlatRecordsRequest, $0.PollFlatRecordsResponse>(
          '/xtcp_flat_record.v1.XTCPFlatRecordService/PollFlatRecords',
          ($0.PollFlatRecordsRequest value) => value.writeToBuffer(),
          $0.PollFlatRecordsResponse.fromBuffer);
}

@$pb.GrpcServiceName('xtcp_flat_record.v1.XTCPFlatRecordService')
abstract class XTCPFlatRecordServiceBase extends $grpc.Service {
  $core.String get $name => 'xtcp_flat_record.v1.XTCPFlatRecordService';

  XTCPFlatRecordServiceBase() {
    $addMethod(
        $grpc.ServiceMethod<$0.FlatRecordsRequest, $0.FlatRecordsResponse>(
            'FlatRecords',
            flatRecords_Pre,
            false,
            true,
            ($core.List<$core.int> value) =>
                $0.FlatRecordsRequest.fromBuffer(value),
            ($0.FlatRecordsResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.PollFlatRecordsRequest,
            $0.PollFlatRecordsResponse>(
        'PollFlatRecords',
        pollFlatRecords,
        true,
        true,
        ($core.List<$core.int> value) =>
            $0.PollFlatRecordsRequest.fromBuffer(value),
        ($0.PollFlatRecordsResponse value) => value.writeToBuffer()));
  }

  $async.Stream<$0.FlatRecordsResponse> flatRecords_Pre($grpc.ServiceCall $call,
      $async.Future<$0.FlatRecordsRequest> $request) async* {
    yield* flatRecords($call, await $request);
  }

  $async.Stream<$0.FlatRecordsResponse> flatRecords(
      $grpc.ServiceCall call, $0.FlatRecordsRequest request);

  $async.Stream<$0.PollFlatRecordsResponse> pollFlatRecords(
      $grpc.ServiceCall call, $async.Stream<$0.PollFlatRecordsRequest> request);
}
