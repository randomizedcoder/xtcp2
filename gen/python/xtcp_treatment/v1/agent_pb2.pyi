from buf.validate import validate_pb2 as _validate_pb2
from xtcp_treatment.v1 import treatment_pb2 as _treatment_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class CheckInRequest(_message.Message):
    __slots__ = ("agent_id", "location", "capabilities", "desired_revision", "records", "locally_suspended")
    AGENT_ID_FIELD_NUMBER: _ClassVar[int]
    LOCATION_FIELD_NUMBER: _ClassVar[int]
    CAPABILITIES_FIELD_NUMBER: _ClassVar[int]
    DESIRED_REVISION_FIELD_NUMBER: _ClassVar[int]
    RECORDS_FIELD_NUMBER: _ClassVar[int]
    LOCALLY_SUSPENDED_FIELD_NUMBER: _ClassVar[int]
    agent_id: str
    location: str
    capabilities: _treatment_pb2.Capabilities
    desired_revision: int
    records: _containers.RepeatedCompositeFieldContainer[_treatment_pb2.ApplicationRecord]
    locally_suspended: bool
    def __init__(self, agent_id: _Optional[str] = ..., location: _Optional[str] = ..., capabilities: _Optional[_Union[_treatment_pb2.Capabilities, _Mapping]] = ..., desired_revision: _Optional[int] = ..., records: _Optional[_Iterable[_Union[_treatment_pb2.ApplicationRecord, _Mapping]]] = ..., locally_suspended: _Optional[bool] = ...) -> None: ...

class CheckInResponse(_message.Message):
    __slots__ = ("desired",)
    DESIRED_FIELD_NUMBER: _ClassVar[int]
    desired: _treatment_pb2.DesiredState
    def __init__(self, desired: _Optional[_Union[_treatment_pb2.DesiredState, _Mapping]] = ...) -> None: ...

class GetDesiredStateRequest(_message.Message):
    __slots__ = ("agent_id",)
    AGENT_ID_FIELD_NUMBER: _ClassVar[int]
    agent_id: str
    def __init__(self, agent_id: _Optional[str] = ...) -> None: ...

class GetDesiredStateResponse(_message.Message):
    __slots__ = ("desired",)
    DESIRED_FIELD_NUMBER: _ClassVar[int]
    desired: _treatment_pb2.DesiredState
    def __init__(self, desired: _Optional[_Union[_treatment_pb2.DesiredState, _Mapping]] = ...) -> None: ...

class WatchDesiredStateRequest(_message.Message):
    __slots__ = ("agent_id", "after_revision")
    AGENT_ID_FIELD_NUMBER: _ClassVar[int]
    AFTER_REVISION_FIELD_NUMBER: _ClassVar[int]
    agent_id: str
    after_revision: int
    def __init__(self, agent_id: _Optional[str] = ..., after_revision: _Optional[int] = ...) -> None: ...

class WatchDesiredStateResponse(_message.Message):
    __slots__ = ("revision",)
    REVISION_FIELD_NUMBER: _ClassVar[int]
    revision: int
    def __init__(self, revision: _Optional[int] = ...) -> None: ...
