from buf.validate import validate_pb2 as _validate_pb2
from xtcp_treatment.v1 import agent_pb2 as _agent_pb2
from xtcp_treatment.v1 import treatment_pb2 as _treatment_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class ControlAction(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    CONTROL_ACTION_UNSPECIFIED: _ClassVar[ControlAction]
    CONTROL_ACTION_PAUSE: _ClassVar[ControlAction]
    CONTROL_ACTION_SUSPEND: _ClassVar[ControlAction]
    CONTROL_ACTION_RESUME: _ClassVar[ControlAction]
    CONTROL_ACTION_ROLLBACK: _ClassVar[ControlAction]
    CONTROL_ACTION_RETIRE: _ClassVar[ControlAction]
CONTROL_ACTION_UNSPECIFIED: ControlAction
CONTROL_ACTION_PAUSE: ControlAction
CONTROL_ACTION_SUSPEND: ControlAction
CONTROL_ACTION_RESUME: ControlAction
CONTROL_ACTION_ROLLBACK: ControlAction
CONTROL_ACTION_RETIRE: ControlAction

class ProposeRequest(_message.Message):
    __slots__ = ("experiment", "request_id")
    EXPERIMENT_FIELD_NUMBER: _ClassVar[int]
    REQUEST_ID_FIELD_NUMBER: _ClassVar[int]
    experiment: _treatment_pb2.Experiment
    request_id: str
    def __init__(self, experiment: _Optional[_Union[_treatment_pb2.Experiment, _Mapping]] = ..., request_id: _Optional[str] = ...) -> None: ...

class ProposeResponse(_message.Message):
    __slots__ = ("experiment",)
    EXPERIMENT_FIELD_NUMBER: _ClassVar[int]
    experiment: _treatment_pb2.Experiment
    def __init__(self, experiment: _Optional[_Union[_treatment_pb2.Experiment, _Mapping]] = ...) -> None: ...

class AnalyzeRequest(_message.Message):
    __slots__ = ("experiment_id", "expected_revision")
    EXPERIMENT_ID_FIELD_NUMBER: _ClassVar[int]
    EXPECTED_REVISION_FIELD_NUMBER: _ClassVar[int]
    experiment_id: str
    expected_revision: int
    def __init__(self, experiment_id: _Optional[str] = ..., expected_revision: _Optional[int] = ...) -> None: ...

class AnalyzeResponse(_message.Message):
    __slots__ = ("evidence", "outcome")
    EVIDENCE_FIELD_NUMBER: _ClassVar[int]
    OUTCOME_FIELD_NUMBER: _ClassVar[int]
    evidence: _treatment_pb2.Evidence
    outcome: _treatment_pb2.Outcome
    def __init__(self, evidence: _Optional[_Union[_treatment_pb2.Evidence, _Mapping]] = ..., outcome: _Optional[_Union[_treatment_pb2.Outcome, str]] = ...) -> None: ...

class ReviewRequest(_message.Message):
    __slots__ = ("experiment_id", "expected_revision", "approve", "reason", "request_id", "evidence")
    EXPERIMENT_ID_FIELD_NUMBER: _ClassVar[int]
    EXPECTED_REVISION_FIELD_NUMBER: _ClassVar[int]
    APPROVE_FIELD_NUMBER: _ClassVar[int]
    REASON_FIELD_NUMBER: _ClassVar[int]
    REQUEST_ID_FIELD_NUMBER: _ClassVar[int]
    EVIDENCE_FIELD_NUMBER: _ClassVar[int]
    experiment_id: str
    expected_revision: int
    approve: bool
    reason: str
    request_id: str
    evidence: _treatment_pb2.Evidence
    def __init__(self, experiment_id: _Optional[str] = ..., expected_revision: _Optional[int] = ..., approve: _Optional[bool] = ..., reason: _Optional[str] = ..., request_id: _Optional[str] = ..., evidence: _Optional[_Union[_treatment_pb2.Evidence, _Mapping]] = ...) -> None: ...

class ReviewResponse(_message.Message):
    __slots__ = ("experiment",)
    EXPERIMENT_FIELD_NUMBER: _ClassVar[int]
    experiment: _treatment_pb2.Experiment
    def __init__(self, experiment: _Optional[_Union[_treatment_pb2.Experiment, _Mapping]] = ...) -> None: ...

class ControlRequest(_message.Message):
    __slots__ = ("experiment_id", "agent_id", "location", "fleet", "action", "reason", "request_id")
    EXPERIMENT_ID_FIELD_NUMBER: _ClassVar[int]
    AGENT_ID_FIELD_NUMBER: _ClassVar[int]
    LOCATION_FIELD_NUMBER: _ClassVar[int]
    FLEET_FIELD_NUMBER: _ClassVar[int]
    ACTION_FIELD_NUMBER: _ClassVar[int]
    REASON_FIELD_NUMBER: _ClassVar[int]
    REQUEST_ID_FIELD_NUMBER: _ClassVar[int]
    experiment_id: str
    agent_id: str
    location: str
    fleet: bool
    action: ControlAction
    reason: str
    request_id: str
    def __init__(self, experiment_id: _Optional[str] = ..., agent_id: _Optional[str] = ..., location: _Optional[str] = ..., fleet: _Optional[bool] = ..., action: _Optional[_Union[ControlAction, str]] = ..., reason: _Optional[str] = ..., request_id: _Optional[str] = ...) -> None: ...

class ControlResponse(_message.Message):
    __slots__ = ("affected_agents",)
    AFFECTED_AGENTS_FIELD_NUMBER: _ClassVar[int]
    affected_agents: int
    def __init__(self, affected_agents: _Optional[int] = ...) -> None: ...

class InspectRequest(_message.Message):
    __slots__ = ("experiment_id", "agent_id")
    EXPERIMENT_ID_FIELD_NUMBER: _ClassVar[int]
    AGENT_ID_FIELD_NUMBER: _ClassVar[int]
    experiment_id: str
    agent_id: str
    def __init__(self, experiment_id: _Optional[str] = ..., agent_id: _Optional[str] = ...) -> None: ...

class InspectResponse(_message.Message):
    __slots__ = ("experiments", "desired", "observed")
    EXPERIMENTS_FIELD_NUMBER: _ClassVar[int]
    DESIRED_FIELD_NUMBER: _ClassVar[int]
    OBSERVED_FIELD_NUMBER: _ClassVar[int]
    experiments: _containers.RepeatedCompositeFieldContainer[_treatment_pb2.Experiment]
    desired: _containers.RepeatedCompositeFieldContainer[_treatment_pb2.DesiredState]
    observed: _containers.RepeatedCompositeFieldContainer[_agent_pb2.CheckInRequest]
    def __init__(self, experiments: _Optional[_Iterable[_Union[_treatment_pb2.Experiment, _Mapping]]] = ..., desired: _Optional[_Iterable[_Union[_treatment_pb2.DesiredState, _Mapping]]] = ..., observed: _Optional[_Iterable[_Union[_agent_pb2.CheckInRequest, _Mapping]]] = ...) -> None: ...
