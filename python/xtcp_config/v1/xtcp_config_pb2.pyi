from google.protobuf import duration_pb2 as _duration_pb2
from google.api import annotations_pb2 as _annotations_pb2
from buf.validate import validate_pb2 as _validate_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class GetRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class GetResponse(_message.Message):
    __slots__ = ("config",)
    CONFIG_FIELD_NUMBER: _ClassVar[int]
    config: XtcpConfig
    def __init__(self, config: _Optional[_Union[XtcpConfig, _Mapping]] = ...) -> None: ...

class SetRequest(_message.Message):
    __slots__ = ("config",)
    CONFIG_FIELD_NUMBER: _ClassVar[int]
    config: XtcpConfig
    def __init__(self, config: _Optional[_Union[XtcpConfig, _Mapping]] = ...) -> None: ...

class SetResponse(_message.Message):
    __slots__ = ("config",)
    CONFIG_FIELD_NUMBER: _ClassVar[int]
    config: XtcpConfig
    def __init__(self, config: _Optional[_Union[XtcpConfig, _Mapping]] = ...) -> None: ...

class SetPollFrequencyRequest(_message.Message):
    __slots__ = ("poll_frequency", "poll_timeout")
    POLL_FREQUENCY_FIELD_NUMBER: _ClassVar[int]
    POLL_TIMEOUT_FIELD_NUMBER: _ClassVar[int]
    poll_frequency: _duration_pb2.Duration
    poll_timeout: _duration_pb2.Duration
    def __init__(self, poll_frequency: _Optional[_Union[_duration_pb2.Duration, _Mapping]] = ..., poll_timeout: _Optional[_Union[_duration_pb2.Duration, _Mapping]] = ...) -> None: ...

class SetPollFrequencyResponse(_message.Message):
    __slots__ = ("config",)
    CONFIG_FIELD_NUMBER: _ClassVar[int]
    config: XtcpConfig
    def __init__(self, config: _Optional[_Union[XtcpConfig, _Mapping]] = ...) -> None: ...

class XtcpConfig(_message.Message):
    __slots__ = ("nl_timeout_milliseconds", "poll_frequency", "poll_timeout", "max_loops", "netlinkers", "netlinkers_done_chan_size", "nlmsg_seq", "packet_size", "packet_size_mply", "write_files", "capture_path", "modulus", "marshal_to", "envelope_flush_threshold_bytes", "envelope_flush_threshold_rows", "kafka_compression", "s3_endpoint", "s3_bucket", "s3_prefix", "s3_access_key", "s3_secret_key", "s3_parquet_flush_threshold_bytes", "s3_region", "s3_skip_bucket_probe", "pyroscope_url", "pyroscope_app_name", "pyroscope_sample_hz", "pyroscope_upload_interval_sec", "dest", "dest_write_files", "topic", "xtcp_proto_file", "kafka_schema_url", "kafka_produce_timeout", "debug_level", "label", "tag", "location", "hostname", "resolve_container_id", "ipv4_ttl", "ipv6_hop_limit", "grpc_port", "enabled_deserializers", "io_uring", "io_uring_recv_batch_size", "io_uring_cqe_batch_size", "csv_columns")
    NL_TIMEOUT_MILLISECONDS_FIELD_NUMBER: _ClassVar[int]
    POLL_FREQUENCY_FIELD_NUMBER: _ClassVar[int]
    POLL_TIMEOUT_FIELD_NUMBER: _ClassVar[int]
    MAX_LOOPS_FIELD_NUMBER: _ClassVar[int]
    NETLINKERS_FIELD_NUMBER: _ClassVar[int]
    NETLINKERS_DONE_CHAN_SIZE_FIELD_NUMBER: _ClassVar[int]
    NLMSG_SEQ_FIELD_NUMBER: _ClassVar[int]
    PACKET_SIZE_FIELD_NUMBER: _ClassVar[int]
    PACKET_SIZE_MPLY_FIELD_NUMBER: _ClassVar[int]
    WRITE_FILES_FIELD_NUMBER: _ClassVar[int]
    CAPTURE_PATH_FIELD_NUMBER: _ClassVar[int]
    MODULUS_FIELD_NUMBER: _ClassVar[int]
    MARSHAL_TO_FIELD_NUMBER: _ClassVar[int]
    ENVELOPE_FLUSH_THRESHOLD_BYTES_FIELD_NUMBER: _ClassVar[int]
    ENVELOPE_FLUSH_THRESHOLD_ROWS_FIELD_NUMBER: _ClassVar[int]
    KAFKA_COMPRESSION_FIELD_NUMBER: _ClassVar[int]
    S3_ENDPOINT_FIELD_NUMBER: _ClassVar[int]
    S3_BUCKET_FIELD_NUMBER: _ClassVar[int]
    S3_PREFIX_FIELD_NUMBER: _ClassVar[int]
    S3_ACCESS_KEY_FIELD_NUMBER: _ClassVar[int]
    S3_SECRET_KEY_FIELD_NUMBER: _ClassVar[int]
    S3_PARQUET_FLUSH_THRESHOLD_BYTES_FIELD_NUMBER: _ClassVar[int]
    S3_REGION_FIELD_NUMBER: _ClassVar[int]
    S3_SKIP_BUCKET_PROBE_FIELD_NUMBER: _ClassVar[int]
    PYROSCOPE_URL_FIELD_NUMBER: _ClassVar[int]
    PYROSCOPE_APP_NAME_FIELD_NUMBER: _ClassVar[int]
    PYROSCOPE_SAMPLE_HZ_FIELD_NUMBER: _ClassVar[int]
    PYROSCOPE_UPLOAD_INTERVAL_SEC_FIELD_NUMBER: _ClassVar[int]
    DEST_FIELD_NUMBER: _ClassVar[int]
    DEST_WRITE_FILES_FIELD_NUMBER: _ClassVar[int]
    TOPIC_FIELD_NUMBER: _ClassVar[int]
    XTCP_PROTO_FILE_FIELD_NUMBER: _ClassVar[int]
    KAFKA_SCHEMA_URL_FIELD_NUMBER: _ClassVar[int]
    KAFKA_PRODUCE_TIMEOUT_FIELD_NUMBER: _ClassVar[int]
    DEBUG_LEVEL_FIELD_NUMBER: _ClassVar[int]
    LABEL_FIELD_NUMBER: _ClassVar[int]
    TAG_FIELD_NUMBER: _ClassVar[int]
    LOCATION_FIELD_NUMBER: _ClassVar[int]
    HOSTNAME_FIELD_NUMBER: _ClassVar[int]
    RESOLVE_CONTAINER_ID_FIELD_NUMBER: _ClassVar[int]
    IPV4_TTL_FIELD_NUMBER: _ClassVar[int]
    IPV6_HOP_LIMIT_FIELD_NUMBER: _ClassVar[int]
    GRPC_PORT_FIELD_NUMBER: _ClassVar[int]
    ENABLED_DESERIALIZERS_FIELD_NUMBER: _ClassVar[int]
    IO_URING_FIELD_NUMBER: _ClassVar[int]
    IO_URING_RECV_BATCH_SIZE_FIELD_NUMBER: _ClassVar[int]
    IO_URING_CQE_BATCH_SIZE_FIELD_NUMBER: _ClassVar[int]
    CSV_COLUMNS_FIELD_NUMBER: _ClassVar[int]
    nl_timeout_milliseconds: int
    poll_frequency: _duration_pb2.Duration
    poll_timeout: _duration_pb2.Duration
    max_loops: int
    netlinkers: int
    netlinkers_done_chan_size: int
    nlmsg_seq: int
    packet_size: int
    packet_size_mply: int
    write_files: int
    capture_path: str
    modulus: int
    marshal_to: str
    envelope_flush_threshold_bytes: int
    envelope_flush_threshold_rows: int
    kafka_compression: str
    s3_endpoint: str
    s3_bucket: str
    s3_prefix: str
    s3_access_key: str
    s3_secret_key: str
    s3_parquet_flush_threshold_bytes: int
    s3_region: str
    s3_skip_bucket_probe: bool
    pyroscope_url: str
    pyroscope_app_name: str
    pyroscope_sample_hz: int
    pyroscope_upload_interval_sec: int
    dest: str
    dest_write_files: int
    topic: str
    xtcp_proto_file: str
    kafka_schema_url: str
    kafka_produce_timeout: _duration_pb2.Duration
    debug_level: int
    label: str
    tag: str
    location: str
    hostname: str
    resolve_container_id: bool
    ipv4_ttl: int
    ipv6_hop_limit: int
    grpc_port: int
    enabled_deserializers: EnabledDeserializers
    io_uring: bool
    io_uring_recv_batch_size: int
    io_uring_cqe_batch_size: int
    csv_columns: str
    def __init__(self, nl_timeout_milliseconds: _Optional[int] = ..., poll_frequency: _Optional[_Union[_duration_pb2.Duration, _Mapping]] = ..., poll_timeout: _Optional[_Union[_duration_pb2.Duration, _Mapping]] = ..., max_loops: _Optional[int] = ..., netlinkers: _Optional[int] = ..., netlinkers_done_chan_size: _Optional[int] = ..., nlmsg_seq: _Optional[int] = ..., packet_size: _Optional[int] = ..., packet_size_mply: _Optional[int] = ..., write_files: _Optional[int] = ..., capture_path: _Optional[str] = ..., modulus: _Optional[int] = ..., marshal_to: _Optional[str] = ..., envelope_flush_threshold_bytes: _Optional[int] = ..., envelope_flush_threshold_rows: _Optional[int] = ..., kafka_compression: _Optional[str] = ..., s3_endpoint: _Optional[str] = ..., s3_bucket: _Optional[str] = ..., s3_prefix: _Optional[str] = ..., s3_access_key: _Optional[str] = ..., s3_secret_key: _Optional[str] = ..., s3_parquet_flush_threshold_bytes: _Optional[int] = ..., s3_region: _Optional[str] = ..., s3_skip_bucket_probe: bool = ..., pyroscope_url: _Optional[str] = ..., pyroscope_app_name: _Optional[str] = ..., pyroscope_sample_hz: _Optional[int] = ..., pyroscope_upload_interval_sec: _Optional[int] = ..., dest: _Optional[str] = ..., dest_write_files: _Optional[int] = ..., topic: _Optional[str] = ..., xtcp_proto_file: _Optional[str] = ..., kafka_schema_url: _Optional[str] = ..., kafka_produce_timeout: _Optional[_Union[_duration_pb2.Duration, _Mapping]] = ..., debug_level: _Optional[int] = ..., label: _Optional[str] = ..., tag: _Optional[str] = ..., location: _Optional[str] = ..., hostname: _Optional[str] = ..., resolve_container_id: bool = ..., ipv4_ttl: _Optional[int] = ..., ipv6_hop_limit: _Optional[int] = ..., grpc_port: _Optional[int] = ..., enabled_deserializers: _Optional[_Union[EnabledDeserializers, _Mapping]] = ..., io_uring: bool = ..., io_uring_recv_batch_size: _Optional[int] = ..., io_uring_cqe_batch_size: _Optional[int] = ..., csv_columns: _Optional[str] = ...) -> None: ...

class EnabledDeserializers(_message.Message):
    __slots__ = ("enabled",)
    class EnabledEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: bool
        def __init__(self, key: _Optional[str] = ..., value: bool = ...) -> None: ...
    ENABLED_FIELD_NUMBER: _ClassVar[int]
    enabled: _containers.ScalarMap[str, bool]
    def __init__(self, enabled: _Optional[_Mapping[str, bool]] = ...) -> None: ...
