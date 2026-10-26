import datetime

from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class AssignmentMode(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    ASSIGNMENT_MODE_UNSPECIFIED: _ClassVar[AssignmentMode]
    ASSIGNMENT_MODE_TRIAL: _ClassVar[AssignmentMode]
    ASSIGNMENT_MODE_STABLE: _ClassVar[AssignmentMode]
    ASSIGNMENT_MODE_SUSPENDED: _ClassVar[AssignmentMode]
    ASSIGNMENT_MODE_RETIRED: _ClassVar[AssignmentMode]

class ApplyStatus(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    APPLY_STATUS_UNSPECIFIED: _ClassVar[ApplyStatus]
    APPLY_STATUS_PENDING: _ClassVar[ApplyStatus]
    APPLY_STATUS_VERIFIED: _ClassVar[ApplyStatus]
    APPLY_STATUS_REMOVED: _ClassVar[ApplyStatus]
    APPLY_STATUS_EXPIRED: _ClassVar[ApplyStatus]
    APPLY_STATUS_FAILED: _ClassVar[ApplyStatus]
ASSIGNMENT_MODE_UNSPECIFIED: AssignmentMode
ASSIGNMENT_MODE_TRIAL: AssignmentMode
ASSIGNMENT_MODE_STABLE: AssignmentMode
ASSIGNMENT_MODE_SUSPENDED: AssignmentMode
ASSIGNMENT_MODE_RETIRED: AssignmentMode
APPLY_STATUS_UNSPECIFIED: ApplyStatus
APPLY_STATUS_PENDING: ApplyStatus
APPLY_STATUS_VERIFIED: ApplyStatus
APPLY_STATUS_REMOVED: ApplyStatus
APPLY_STATUS_EXPIRED: ApplyStatus
APPLY_STATUS_FAILED: ApplyStatus

class TCPSettings(_message.Message):
    __slots__ = ("congestion_control", "congestion_lock", "rto_min_ms", "initial_congestion_window", "initial_receive_window", "quick_ack", "ecn")
    CONGESTION_CONTROL_FIELD_NUMBER: _ClassVar[int]
    CONGESTION_LOCK_FIELD_NUMBER: _ClassVar[int]
    RTO_MIN_MS_FIELD_NUMBER: _ClassVar[int]
    INITIAL_CONGESTION_WINDOW_FIELD_NUMBER: _ClassVar[int]
    INITIAL_RECEIVE_WINDOW_FIELD_NUMBER: _ClassVar[int]
    QUICK_ACK_FIELD_NUMBER: _ClassVar[int]
    ECN_FIELD_NUMBER: _ClassVar[int]
    congestion_control: str
    congestion_lock: bool
    rto_min_ms: int
    initial_congestion_window: int
    initial_receive_window: int
    quick_ack: bool
    ecn: bool
    def __init__(self, congestion_control: _Optional[str] = ..., congestion_lock: _Optional[bool] = ..., rto_min_ms: _Optional[int] = ..., initial_congestion_window: _Optional[int] = ..., initial_receive_window: _Optional[int] = ..., quick_ack: _Optional[bool] = ..., ecn: _Optional[bool] = ...) -> None: ...

class Treatment(_message.Message):
    __slots__ = ("id", "revision", "destination", "settings", "ecn_fabric_verified")
    ID_FIELD_NUMBER: _ClassVar[int]
    REVISION_FIELD_NUMBER: _ClassVar[int]
    DESTINATION_FIELD_NUMBER: _ClassVar[int]
    SETTINGS_FIELD_NUMBER: _ClassVar[int]
    ECN_FABRIC_VERIFIED_FIELD_NUMBER: _ClassVar[int]
    id: str
    revision: int
    destination: str
    settings: TCPSettings
    ecn_fabric_verified: bool
    def __init__(self, id: _Optional[str] = ..., revision: _Optional[int] = ..., destination: _Optional[str] = ..., settings: _Optional[_Union[TCPSettings, _Mapping]] = ..., ecn_fabric_verified: _Optional[bool] = ...) -> None: ...

class Assignment(_message.Message):
    __slots__ = ("experiment_id", "treatment", "mode", "lease_expires_at", "predecessor", "generation")
    EXPERIMENT_ID_FIELD_NUMBER: _ClassVar[int]
    TREATMENT_FIELD_NUMBER: _ClassVar[int]
    MODE_FIELD_NUMBER: _ClassVar[int]
    LEASE_EXPIRES_AT_FIELD_NUMBER: _ClassVar[int]
    PREDECESSOR_FIELD_NUMBER: _ClassVar[int]
    GENERATION_FIELD_NUMBER: _ClassVar[int]
    experiment_id: str
    treatment: Treatment
    mode: AssignmentMode
    lease_expires_at: _timestamp_pb2.Timestamp
    predecessor: Treatment
    generation: int
    def __init__(self, experiment_id: _Optional[str] = ..., treatment: _Optional[_Union[Treatment, _Mapping]] = ..., mode: _Optional[_Union[AssignmentMode, str]] = ..., lease_expires_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., predecessor: _Optional[_Union[Treatment, _Mapping]] = ..., generation: _Optional[int] = ...) -> None: ...

class Desired(_message.Message):
    __slots__ = ("revision", "suspended", "issued_at", "assignments")
    REVISION_FIELD_NUMBER: _ClassVar[int]
    SUSPENDED_FIELD_NUMBER: _ClassVar[int]
    ISSUED_AT_FIELD_NUMBER: _ClassVar[int]
    ASSIGNMENTS_FIELD_NUMBER: _ClassVar[int]
    revision: int
    suspended: bool
    issued_at: _timestamp_pb2.Timestamp
    assignments: _containers.RepeatedCompositeFieldContainer[Assignment]
    def __init__(self, revision: _Optional[int] = ..., suspended: _Optional[bool] = ..., issued_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., assignments: _Optional[_Iterable[_Union[Assignment, _Mapping]]] = ...) -> None: ...

class Record(_message.Message):
    __slots__ = ("assignment", "desired_revision", "status", "received_at", "attempted_at", "applied_at", "verified_at", "removed_at", "error", "installed_route", "installed_boot_id", "expired_latched")
    ASSIGNMENT_FIELD_NUMBER: _ClassVar[int]
    DESIRED_REVISION_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    RECEIVED_AT_FIELD_NUMBER: _ClassVar[int]
    ATTEMPTED_AT_FIELD_NUMBER: _ClassVar[int]
    APPLIED_AT_FIELD_NUMBER: _ClassVar[int]
    VERIFIED_AT_FIELD_NUMBER: _ClassVar[int]
    REMOVED_AT_FIELD_NUMBER: _ClassVar[int]
    ERROR_FIELD_NUMBER: _ClassVar[int]
    INSTALLED_ROUTE_FIELD_NUMBER: _ClassVar[int]
    INSTALLED_BOOT_ID_FIELD_NUMBER: _ClassVar[int]
    EXPIRED_LATCHED_FIELD_NUMBER: _ClassVar[int]
    assignment: Assignment
    desired_revision: int
    status: ApplyStatus
    received_at: _timestamp_pb2.Timestamp
    attempted_at: _timestamp_pb2.Timestamp
    applied_at: _timestamp_pb2.Timestamp
    verified_at: _timestamp_pb2.Timestamp
    removed_at: _timestamp_pb2.Timestamp
    error: str
    installed_route: bytes
    installed_boot_id: str
    expired_latched: bool
    def __init__(self, assignment: _Optional[_Union[Assignment, _Mapping]] = ..., desired_revision: _Optional[int] = ..., status: _Optional[_Union[ApplyStatus, str]] = ..., received_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., attempted_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., applied_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., verified_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., removed_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., error: _Optional[str] = ..., installed_route: _Optional[bytes] = ..., installed_boot_id: _Optional[str] = ..., expired_latched: _Optional[bool] = ...) -> None: ...

class Journal(_message.Message):
    __slots__ = ("agent_id", "location", "boot_id", "desired", "locally_suspended", "suspension_reason", "records", "updated_at")
    AGENT_ID_FIELD_NUMBER: _ClassVar[int]
    LOCATION_FIELD_NUMBER: _ClassVar[int]
    BOOT_ID_FIELD_NUMBER: _ClassVar[int]
    DESIRED_FIELD_NUMBER: _ClassVar[int]
    LOCALLY_SUSPENDED_FIELD_NUMBER: _ClassVar[int]
    SUSPENSION_REASON_FIELD_NUMBER: _ClassVar[int]
    RECORDS_FIELD_NUMBER: _ClassVar[int]
    UPDATED_AT_FIELD_NUMBER: _ClassVar[int]
    agent_id: str
    location: str
    boot_id: str
    desired: Desired
    locally_suspended: bool
    suspension_reason: str
    records: _containers.RepeatedCompositeFieldContainer[Record]
    updated_at: _timestamp_pb2.Timestamp
    def __init__(self, agent_id: _Optional[str] = ..., location: _Optional[str] = ..., boot_id: _Optional[str] = ..., desired: _Optional[_Union[Desired, _Mapping]] = ..., locally_suspended: _Optional[bool] = ..., suspension_reason: _Optional[str] = ..., records: _Optional[_Iterable[_Union[Record, _Mapping]]] = ..., updated_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...
