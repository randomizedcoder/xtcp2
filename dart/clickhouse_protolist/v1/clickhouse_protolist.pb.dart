// This is a generated file - do not edit.
//
// Generated from clickhouse_protolist/v1/clickhouse_protolist.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, prefer_relative_imports

import 'dart:core' as $core;

import 'package:protobuf/protobuf.dart' as $pb;

export 'package:protobuf/protobuf.dart' show GeneratedMessageGenericExtensions;

class Record extends $pb.GeneratedMessage {
  factory Record({
    $core.int? myUint32,
  }) {
    final result = create();
    if (myUint32 != null) result.myUint32 = myUint32;
    return result;
  }

  Record._();

  factory Record.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory Record.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'Record',
      package: const $pb.PackageName(
          _omitMessageNames ? '' : 'clickhouse_protolist.v1'),
      createEmptyInstance: create)
    ..aI(1, _omitFieldNames ? '' : 'myUint32', fieldType: $pb.PbFieldType.OU3)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Record clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Record copyWith(void Function(Record) updates) =>
      super.copyWith((message) => updates(message as Record)) as Record;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static Record create() => Record._();
  @$core.override
  Record createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static Record getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<Record>(create);
  static Record? _defaultInstance;

  @$pb.TagNumber(1)
  $core.int get myUint32 => $_getIZ(0);
  @$pb.TagNumber(1)
  set myUint32($core.int value) => $_setUnsignedInt32(0, value);
  @$pb.TagNumber(1)
  $core.bool hasMyUint32() => $_has(0);
  @$pb.TagNumber(1)
  void clearMyUint32() => $_clearField(1);
}

class Envelope_Record extends $pb.GeneratedMessage {
  factory Envelope_Record({
    $core.int? myUint32,
  }) {
    final result = create();
    if (myUint32 != null) result.myUint32 = myUint32;
    return result;
  }

  Envelope_Record._();

  factory Envelope_Record.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory Envelope_Record.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'Envelope.Record',
      package: const $pb.PackageName(
          _omitMessageNames ? '' : 'clickhouse_protolist.v1'),
      createEmptyInstance: create)
    ..aI(1, _omitFieldNames ? '' : 'myUint32', fieldType: $pb.PbFieldType.OU3)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Envelope_Record clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Envelope_Record copyWith(void Function(Envelope_Record) updates) =>
      super.copyWith((message) => updates(message as Envelope_Record))
          as Envelope_Record;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static Envelope_Record create() => Envelope_Record._();
  @$core.override
  Envelope_Record createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static Envelope_Record getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<Envelope_Record>(create);
  static Envelope_Record? _defaultInstance;

  @$pb.TagNumber(1)
  $core.int get myUint32 => $_getIZ(0);
  @$pb.TagNumber(1)
  set myUint32($core.int value) => $_setUnsignedInt32(0, value);
  @$pb.TagNumber(1)
  $core.bool hasMyUint32() => $_has(0);
  @$pb.TagNumber(1)
  void clearMyUint32() => $_clearField(1);
}

/// https://clickhouse.com/docs/en/interfaces/formats#protobuflist
class Envelope extends $pb.GeneratedMessage {
  factory Envelope({
    $core.Iterable<Envelope_Record>? rows,
  }) {
    final result = create();
    if (rows != null) result.rows.addAll(rows);
    return result;
  }

  Envelope._();

  factory Envelope.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory Envelope.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'Envelope',
      package: const $pb.PackageName(
          _omitMessageNames ? '' : 'clickhouse_protolist.v1'),
      createEmptyInstance: create)
    ..pPM<Envelope_Record>(1, _omitFieldNames ? '' : 'rows',
        subBuilder: Envelope_Record.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Envelope clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Envelope copyWith(void Function(Envelope) updates) =>
      super.copyWith((message) => updates(message as Envelope)) as Envelope;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static Envelope create() => Envelope._();
  @$core.override
  Envelope createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static Envelope getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<Envelope>(create);
  static Envelope? _defaultInstance;

  @$pb.TagNumber(1)
  $pb.PbList<Envelope_Record> get rows => $_getList(0);
}

const $core.bool _omitFieldNames =
    $core.bool.fromEnvironment('protobuf.omit_field_names');
const $core.bool _omitMessageNames =
    $core.bool.fromEnvironment('protobuf.omit_message_names');
