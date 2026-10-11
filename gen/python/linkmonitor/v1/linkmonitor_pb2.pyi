import datetime

from google.protobuf import duration_pb2 as _duration_pb2
from google.protobuf import timestamp_pb2 as _timestamp_pb2
from buf.validate import validate_pb2 as _validate_pb2
from xtcp_auth.v1 import xtcp_auth_pb2 as _xtcp_auth_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class Eligibility(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    ELIGIBILITY_UNSPECIFIED: _ClassVar[Eligibility]
    ELIGIBILITY_INCLUDED: _ClassVar[Eligibility]
    ELIGIBILITY_EXCLUDED: _ClassVar[Eligibility]

class CheckStatus(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    CHECK_STATUS_UNSPECIFIED: _ClassVar[CheckStatus]
    CHECK_STATUS_PASS: _ClassVar[CheckStatus]
    CHECK_STATUS_FAIL: _ClassVar[CheckStatus]
    CHECK_STATUS_NOT_APPLICABLE: _ClassVar[CheckStatus]

class Support(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    SUPPORT_UNSPECIFIED: _ClassVar[Support]
    SUPPORT_SUPPORTED: _ClassVar[Support]
    SUPPORT_UNSUPPORTED: _ClassVar[Support]
    SUPPORT_NOT_APPLICABLE: _ClassVar[Support]
ELIGIBILITY_UNSPECIFIED: Eligibility
ELIGIBILITY_INCLUDED: Eligibility
ELIGIBILITY_EXCLUDED: Eligibility
CHECK_STATUS_UNSPECIFIED: CheckStatus
CHECK_STATUS_PASS: CheckStatus
CHECK_STATUS_FAIL: CheckStatus
CHECK_STATUS_NOT_APPLICABLE: CheckStatus
SUPPORT_UNSPECIFIED: Support
SUPPORT_SUPPORTED: Support
SUPPORT_UNSUPPORTED: Support
SUPPORT_NOT_APPLICABLE: Support

class Health(_message.Message):
    __slots__ = ("running", "ready", "collection_healthy", "baseline_ready")
    RUNNING_FIELD_NUMBER: _ClassVar[int]
    READY_FIELD_NUMBER: _ClassVar[int]
    COLLECTION_HEALTHY_FIELD_NUMBER: _ClassVar[int]
    BASELINE_READY_FIELD_NUMBER: _ClassVar[int]
    running: bool
    ready: bool
    collection_healthy: bool
    baseline_ready: bool
    def __init__(self, running: _Optional[bool] = ..., ready: _Optional[bool] = ..., collection_healthy: _Optional[bool] = ..., baseline_ready: _Optional[bool] = ...) -> None: ...

class LinkCounts(_message.Message):
    __slots__ = ("current", "expected", "delta")
    CURRENT_FIELD_NUMBER: _ClassVar[int]
    EXPECTED_FIELD_NUMBER: _ClassVar[int]
    DELTA_FIELD_NUMBER: _ClassVar[int]
    current: int
    expected: int
    delta: int
    def __init__(self, current: _Optional[int] = ..., expected: _Optional[int] = ..., delta: _Optional[int] = ...) -> None: ...

class ControlProgress(_message.Message):
    __slots__ = ("requested", "completed")
    REQUESTED_FIELD_NUMBER: _ClassVar[int]
    COMPLETED_FIELD_NUMBER: _ClassVar[int]
    requested: int
    completed: int
    def __init__(self, requested: _Optional[int] = ..., completed: _Optional[int] = ...) -> None: ...

class GetStatusRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class GetStatusResponse(_message.Message):
    __slots__ = ("snapshot_version", "health", "counts", "baseline_recorded_at", "baseline_write_errors", "last_successful_resync", "resync", "rebaseline")
    SNAPSHOT_VERSION_FIELD_NUMBER: _ClassVar[int]
    HEALTH_FIELD_NUMBER: _ClassVar[int]
    COUNTS_FIELD_NUMBER: _ClassVar[int]
    BASELINE_RECORDED_AT_FIELD_NUMBER: _ClassVar[int]
    BASELINE_WRITE_ERRORS_FIELD_NUMBER: _ClassVar[int]
    LAST_SUCCESSFUL_RESYNC_FIELD_NUMBER: _ClassVar[int]
    RESYNC_FIELD_NUMBER: _ClassVar[int]
    REBASELINE_FIELD_NUMBER: _ClassVar[int]
    snapshot_version: int
    health: Health
    counts: LinkCounts
    baseline_recorded_at: _timestamp_pb2.Timestamp
    baseline_write_errors: int
    last_successful_resync: _timestamp_pb2.Timestamp
    resync: ControlProgress
    rebaseline: ControlProgress
    def __init__(self, snapshot_version: _Optional[int] = ..., health: _Optional[_Union[Health, _Mapping]] = ..., counts: _Optional[_Union[LinkCounts, _Mapping]] = ..., baseline_recorded_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., baseline_write_errors: _Optional[int] = ..., last_successful_resync: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., resync: _Optional[_Union[ControlProgress, _Mapping]] = ..., rebaseline: _Optional[_Union[ControlProgress, _Mapping]] = ...) -> None: ...

class Interface(_message.Message):
    __slots__ = ("name", "identity", "generation", "up", "eligibility", "maximum_speed", "maximum_width", "full_duplex", "rdma_readiness", "maximum_speed_exception", "up_transitions", "down_transitions")
    NAME_FIELD_NUMBER: _ClassVar[int]
    IDENTITY_FIELD_NUMBER: _ClassVar[int]
    GENERATION_FIELD_NUMBER: _ClassVar[int]
    UP_FIELD_NUMBER: _ClassVar[int]
    ELIGIBILITY_FIELD_NUMBER: _ClassVar[int]
    MAXIMUM_SPEED_FIELD_NUMBER: _ClassVar[int]
    MAXIMUM_WIDTH_FIELD_NUMBER: _ClassVar[int]
    FULL_DUPLEX_FIELD_NUMBER: _ClassVar[int]
    RDMA_READINESS_FIELD_NUMBER: _ClassVar[int]
    MAXIMUM_SPEED_EXCEPTION_FIELD_NUMBER: _ClassVar[int]
    UP_TRANSITIONS_FIELD_NUMBER: _ClassVar[int]
    DOWN_TRANSITIONS_FIELD_NUMBER: _ClassVar[int]
    name: str
    identity: str
    generation: int
    up: bool
    eligibility: Eligibility
    maximum_speed: CheckStatus
    maximum_width: CheckStatus
    full_duplex: CheckStatus
    rdma_readiness: CheckStatus
    maximum_speed_exception: bool
    up_transitions: int
    down_transitions: int
    def __init__(self, name: _Optional[str] = ..., identity: _Optional[str] = ..., generation: _Optional[int] = ..., up: _Optional[bool] = ..., eligibility: _Optional[_Union[Eligibility, str]] = ..., maximum_speed: _Optional[_Union[CheckStatus, str]] = ..., maximum_width: _Optional[_Union[CheckStatus, str]] = ..., full_duplex: _Optional[_Union[CheckStatus, str]] = ..., rdma_readiness: _Optional[_Union[CheckStatus, str]] = ..., maximum_speed_exception: _Optional[bool] = ..., up_transitions: _Optional[int] = ..., down_transitions: _Optional[int] = ...) -> None: ...

class ListInterfacesRequest(_message.Message):
    __slots__ = ("names", "eligible_only")
    NAMES_FIELD_NUMBER: _ClassVar[int]
    ELIGIBLE_ONLY_FIELD_NUMBER: _ClassVar[int]
    names: _containers.RepeatedScalarFieldContainer[str]
    eligible_only: bool
    def __init__(self, names: _Optional[_Iterable[str]] = ..., eligible_only: _Optional[bool] = ...) -> None: ...

class ListInterfacesResponse(_message.Message):
    __slots__ = ("snapshot_version", "interfaces")
    SNAPSHOT_VERSION_FIELD_NUMBER: _ClassVar[int]
    INTERFACES_FIELD_NUMBER: _ClassVar[int]
    snapshot_version: int
    interfaces: _containers.RepeatedCompositeFieldContainer[Interface]
    def __init__(self, snapshot_version: _Optional[int] = ..., interfaces: _Optional[_Iterable[_Union[Interface, _Mapping]]] = ...) -> None: ...

class Collector(_message.Message):
    __slots__ = ("interface", "name", "support", "success", "fresh", "last_success", "duration", "error_reason", "errors", "omissions", "discontinuities")
    class ErrorsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: int
        def __init__(self, key: _Optional[str] = ..., value: _Optional[int] = ...) -> None: ...
    class OmissionsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: int
        def __init__(self, key: _Optional[str] = ..., value: _Optional[int] = ...) -> None: ...
    INTERFACE_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    SUPPORT_FIELD_NUMBER: _ClassVar[int]
    SUCCESS_FIELD_NUMBER: _ClassVar[int]
    FRESH_FIELD_NUMBER: _ClassVar[int]
    LAST_SUCCESS_FIELD_NUMBER: _ClassVar[int]
    DURATION_FIELD_NUMBER: _ClassVar[int]
    ERROR_REASON_FIELD_NUMBER: _ClassVar[int]
    ERRORS_FIELD_NUMBER: _ClassVar[int]
    OMISSIONS_FIELD_NUMBER: _ClassVar[int]
    DISCONTINUITIES_FIELD_NUMBER: _ClassVar[int]
    interface: str
    name: str
    support: Support
    success: bool
    fresh: bool
    last_success: _timestamp_pb2.Timestamp
    duration: _duration_pb2.Duration
    error_reason: str
    errors: _containers.ScalarMap[str, int]
    omissions: _containers.ScalarMap[str, int]
    discontinuities: int
    def __init__(self, interface: _Optional[str] = ..., name: _Optional[str] = ..., support: _Optional[_Union[Support, str]] = ..., success: _Optional[bool] = ..., fresh: _Optional[bool] = ..., last_success: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., duration: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ..., error_reason: _Optional[str] = ..., errors: _Optional[_Mapping[str, int]] = ..., omissions: _Optional[_Mapping[str, int]] = ..., discontinuities: _Optional[int] = ...) -> None: ...

class ListCollectorsRequest(_message.Message):
    __slots__ = ("names",)
    NAMES_FIELD_NUMBER: _ClassVar[int]
    names: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, names: _Optional[_Iterable[str]] = ...) -> None: ...

class ListCollectorsResponse(_message.Message):
    __slots__ = ("snapshot_version", "collectors")
    SNAPSHOT_VERSION_FIELD_NUMBER: _ClassVar[int]
    COLLECTORS_FIELD_NUMBER: _ClassVar[int]
    snapshot_version: int
    collectors: _containers.RepeatedCompositeFieldContainer[Collector]
    def __init__(self, snapshot_version: _Optional[int] = ..., collectors: _Optional[_Iterable[_Union[Collector, _Mapping]]] = ...) -> None: ...

class RebaselineRequest(_message.Message):
    __slots__ = ("reason", "wait")
    REASON_FIELD_NUMBER: _ClassVar[int]
    WAIT_FIELD_NUMBER: _ClassVar[int]
    reason: str
    wait: _duration_pb2.Duration
    def __init__(self, reason: _Optional[str] = ..., wait: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ...) -> None: ...

class RebaselineResponse(_message.Message):
    __slots__ = ("generation", "completed", "expected", "baseline_write_errors")
    GENERATION_FIELD_NUMBER: _ClassVar[int]
    COMPLETED_FIELD_NUMBER: _ClassVar[int]
    EXPECTED_FIELD_NUMBER: _ClassVar[int]
    BASELINE_WRITE_ERRORS_FIELD_NUMBER: _ClassVar[int]
    generation: int
    completed: bool
    expected: int
    baseline_write_errors: int
    def __init__(self, generation: _Optional[int] = ..., completed: _Optional[bool] = ..., expected: _Optional[int] = ..., baseline_write_errors: _Optional[int] = ...) -> None: ...

class ResyncRequest(_message.Message):
    __slots__ = ("reason", "wait")
    REASON_FIELD_NUMBER: _ClassVar[int]
    WAIT_FIELD_NUMBER: _ClassVar[int]
    reason: str
    wait: _duration_pb2.Duration
    def __init__(self, reason: _Optional[str] = ..., wait: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ...) -> None: ...

class ResyncResponse(_message.Message):
    __slots__ = ("generation", "completed", "last_successful_resync")
    GENERATION_FIELD_NUMBER: _ClassVar[int]
    COMPLETED_FIELD_NUMBER: _ClassVar[int]
    LAST_SUCCESSFUL_RESYNC_FIELD_NUMBER: _ClassVar[int]
    generation: int
    completed: bool
    last_successful_resync: _timestamp_pb2.Timestamp
    def __init__(self, generation: _Optional[int] = ..., completed: _Optional[bool] = ..., last_successful_resync: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...
