//
//  Generated code. Do not modify.
//  source: xtcp_flat_record/v1/xtcp_flat_record.proto
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

import 'xtcp_flat_record.pb.dart' as $1;

export 'xtcp_flat_record.pb.dart';

@$pb.GrpcServiceName('xtcp_flat_record.v1.XTCPFlatRecordService')
class XTCPFlatRecordServiceClient extends $grpc.Client {
  static final _$flatRecords = $grpc.ClientMethod<$1.FlatRecordsRequest, $1.FlatRecordsResponse>(
      '/xtcp_flat_record.v1.XTCPFlatRecordService/FlatRecords',
      ($1.FlatRecordsRequest value) => value.writeToBuffer(),
      ($core.List<$core.int> value) => $1.FlatRecordsResponse.fromBuffer(value));
  static final _$pollFlatRecords = $grpc.ClientMethod<$1.PollFlatRecordsRequest, $1.PollFlatRecordsResponse>(
      '/xtcp_flat_record.v1.XTCPFlatRecordService/PollFlatRecords',
      ($1.PollFlatRecordsRequest value) => value.writeToBuffer(),
      ($core.List<$core.int> value) => $1.PollFlatRecordsResponse.fromBuffer(value));

  XTCPFlatRecordServiceClient($grpc.ClientChannel channel,
      {$grpc.CallOptions? options,
      $core.Iterable<$grpc.ClientInterceptor>? interceptors})
      : super(channel, options: options,
        interceptors: interceptors);

  $grpc.ResponseStream<$1.FlatRecordsResponse> flatRecords($1.FlatRecordsRequest request, {$grpc.CallOptions? options}) {
    return $createStreamingCall(_$flatRecords, $async.Stream.fromIterable([request]), options: options);
  }

  $grpc.ResponseStream<$1.PollFlatRecordsResponse> pollFlatRecords($async.Stream<$1.PollFlatRecordsRequest> request, {$grpc.CallOptions? options}) {
    return $createStreamingCall(_$pollFlatRecords, request, options: options);
  }
}

@$pb.GrpcServiceName('xtcp_flat_record.v1.XTCPFlatRecordService')
abstract class XTCPFlatRecordServiceBase extends $grpc.Service {
  $core.String get $name => 'xtcp_flat_record.v1.XTCPFlatRecordService';

  XTCPFlatRecordServiceBase() {
    $addMethod($grpc.ServiceMethod<$1.FlatRecordsRequest, $1.FlatRecordsResponse>(
        'FlatRecords',
        flatRecords_Pre,
        false,
        true,
        ($core.List<$core.int> value) => $1.FlatRecordsRequest.fromBuffer(value),
        ($1.FlatRecordsResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$1.PollFlatRecordsRequest, $1.PollFlatRecordsResponse>(
        'PollFlatRecords',
        pollFlatRecords,
        true,
        true,
        ($core.List<$core.int> value) => $1.PollFlatRecordsRequest.fromBuffer(value),
        ($1.PollFlatRecordsResponse value) => value.writeToBuffer()));
  }

  $async.Stream<$1.FlatRecordsResponse> flatRecords_Pre($grpc.ServiceCall call, $async.Future<$1.FlatRecordsRequest> request) async* {
    yield* flatRecords(call, await request);
  }

  $async.Stream<$1.FlatRecordsResponse> flatRecords($grpc.ServiceCall call, $1.FlatRecordsRequest request);
  $async.Stream<$1.PollFlatRecordsResponse> pollFlatRecords($grpc.ServiceCall call, $async.Stream<$1.PollFlatRecordsRequest> request);
}
