import datetime

from buf.validate import validate_pb2 as _validate_pb2
from google.protobuf import descriptor_pb2 as _descriptor_pb2
from google.protobuf import duration_pb2 as _duration_pb2
from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class Role(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    ROLE_UNSPECIFIED: _ClassVar[Role]
    ROLE_AGENT: _ClassVar[Role]
    ROLE_OPERATOR: _ClassVar[Role]

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

class Outcome(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    OUTCOME_UNSPECIFIED: _ClassVar[Outcome]
    OUTCOME_BASELINE: _ClassVar[Outcome]
    OUTCOME_AWAITING_APPLICATION: _ClassVar[Outcome]
    OUTCOME_AWAITING_EXPOSURE: _ClassVar[Outcome]
    OUTCOME_INSUFFICIENT_DURATION: _ClassVar[Outcome]
    OUTCOME_INSUFFICIENT_DATA: _ClassVar[Outcome]
    OUTCOME_INSUFFICIENT_CONTROLS: _ClassVar[Outcome]
    OUTCOME_STALE: _ClassVar[Outcome]
    OUTCOME_UNAVAILABLE: _ClassVar[Outcome]
    OUTCOME_INCONCLUSIVE: _ClassVar[Outcome]
    OUTCOME_SAFETY_ONLY: _ClassVar[Outcome]
    OUTCOME_FINAL_SAFETY: _ClassVar[Outcome]
    OUTCOME_BENEFICIAL: _ClassVar[Outcome]
    OUTCOME_HARMFUL: _ClassVar[Outcome]
    OUTCOME_EXPIRED: _ClassVar[Outcome]
    OUTCOME_STABLE_OBSERVED: _ClassVar[Outcome]
    OUTCOME_STABLE_EVIDENCE_INCOMPLETE: _ClassVar[Outcome]
    OUTCOME_STABLE_REGRESSION_REVIEW_REQUIRED: _ClassVar[Outcome]
    OUTCOME_OBSERVING: _ClassVar[Outcome]

class ExperimentStatus(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    EXPERIMENT_STATUS_UNSPECIFIED: _ClassVar[ExperimentStatus]
    EXPERIMENT_STATUS_PROPOSED: _ClassVar[ExperimentStatus]
    EXPERIMENT_STATUS_RUNNING: _ClassVar[ExperimentStatus]
    EXPERIMENT_STATUS_PAUSED: _ClassVar[ExperimentStatus]
    EXPERIMENT_STATUS_SUSPENDED: _ClassVar[ExperimentStatus]
    EXPERIMENT_STATUS_COMPLETE: _ClassVar[ExperimentStatus]
    EXPERIMENT_STATUS_RETIRED: _ClassVar[ExperimentStatus]
    EXPERIMENT_STATUS_ROLLED_BACK: _ClassVar[ExperimentStatus]
ROLE_UNSPECIFIED: Role
ROLE_AGENT: Role
ROLE_OPERATOR: Role
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
OUTCOME_UNSPECIFIED: Outcome
OUTCOME_BASELINE: Outcome
OUTCOME_AWAITING_APPLICATION: Outcome
OUTCOME_AWAITING_EXPOSURE: Outcome
OUTCOME_INSUFFICIENT_DURATION: Outcome
OUTCOME_INSUFFICIENT_DATA: Outcome
OUTCOME_INSUFFICIENT_CONTROLS: Outcome
OUTCOME_STALE: Outcome
OUTCOME_UNAVAILABLE: Outcome
OUTCOME_INCONCLUSIVE: Outcome
OUTCOME_SAFETY_ONLY: Outcome
OUTCOME_FINAL_SAFETY: Outcome
OUTCOME_BENEFICIAL: Outcome
OUTCOME_HARMFUL: Outcome
OUTCOME_EXPIRED: Outcome
OUTCOME_STABLE_OBSERVED: Outcome
OUTCOME_STABLE_EVIDENCE_INCOMPLETE: Outcome
OUTCOME_STABLE_REGRESSION_REVIEW_REQUIRED: Outcome
OUTCOME_OBSERVING: Outcome
EXPERIMENT_STATUS_UNSPECIFIED: ExperimentStatus
EXPERIMENT_STATUS_PROPOSED: ExperimentStatus
EXPERIMENT_STATUS_RUNNING: ExperimentStatus
EXPERIMENT_STATUS_PAUSED: ExperimentStatus
EXPERIMENT_STATUS_SUSPENDED: ExperimentStatus
EXPERIMENT_STATUS_COMPLETE: ExperimentStatus
EXPERIMENT_STATUS_RETIRED: ExperimentStatus
EXPERIMENT_STATUS_ROLLED_BACK: ExperimentStatus
ROLE_FIELD_NUMBER: _ClassVar[int]
role: _descriptor.FieldDescriptor

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

class DesiredState(_message.Message):
    __slots__ = ("agent_id", "revision", "suspended", "assignments", "issued_at")
    AGENT_ID_FIELD_NUMBER: _ClassVar[int]
    REVISION_FIELD_NUMBER: _ClassVar[int]
    SUSPENDED_FIELD_NUMBER: _ClassVar[int]
    ASSIGNMENTS_FIELD_NUMBER: _ClassVar[int]
    ISSUED_AT_FIELD_NUMBER: _ClassVar[int]
    agent_id: str
    revision: int
    suspended: bool
    assignments: _containers.RepeatedCompositeFieldContainer[Assignment]
    issued_at: _timestamp_pb2.Timestamp
    def __init__(self, agent_id: _Optional[str] = ..., revision: _Optional[int] = ..., suspended: _Optional[bool] = ..., assignments: _Optional[_Iterable[_Union[Assignment, _Mapping]]] = ..., issued_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class Capabilities(_message.Message):
    __slots__ = ("boot_id", "kernel_release", "congestion_algorithms", "simple_host_routing", "actuator_enabled")
    BOOT_ID_FIELD_NUMBER: _ClassVar[int]
    KERNEL_RELEASE_FIELD_NUMBER: _ClassVar[int]
    CONGESTION_ALGORITHMS_FIELD_NUMBER: _ClassVar[int]
    SIMPLE_HOST_ROUTING_FIELD_NUMBER: _ClassVar[int]
    ACTUATOR_ENABLED_FIELD_NUMBER: _ClassVar[int]
    boot_id: str
    kernel_release: str
    congestion_algorithms: _containers.RepeatedScalarFieldContainer[str]
    simple_host_routing: bool
    actuator_enabled: bool
    def __init__(self, boot_id: _Optional[str] = ..., kernel_release: _Optional[str] = ..., congestion_algorithms: _Optional[_Iterable[str]] = ..., simple_host_routing: _Optional[bool] = ..., actuator_enabled: _Optional[bool] = ...) -> None: ...

class ApplicationRecord(_message.Message):
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

class LocalState(_message.Message):
    __slots__ = ("desired", "locally_suspended", "suspension_reason", "records", "updated_at")
    DESIRED_FIELD_NUMBER: _ClassVar[int]
    LOCALLY_SUSPENDED_FIELD_NUMBER: _ClassVar[int]
    SUSPENSION_REASON_FIELD_NUMBER: _ClassVar[int]
    RECORDS_FIELD_NUMBER: _ClassVar[int]
    UPDATED_AT_FIELD_NUMBER: _ClassVar[int]
    desired: DesiredState
    locally_suspended: bool
    suspension_reason: str
    records: _containers.RepeatedCompositeFieldContainer[ApplicationRecord]
    updated_at: _timestamp_pb2.Timestamp
    def __init__(self, desired: _Optional[_Union[DesiredState, _Mapping]] = ..., locally_suspended: _Optional[bool] = ..., suspension_reason: _Optional[str] = ..., records: _Optional[_Iterable[_Union[ApplicationRecord, _Mapping]]] = ..., updated_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class HostEvidence(_message.Message):
    __slots__ = ("agent_id", "sockets", "bytes_acked", "goodput_bps", "retransmission_ratio", "rtt_p95_ms", "exposed_sockets", "collection_healthy")
    AGENT_ID_FIELD_NUMBER: _ClassVar[int]
    SOCKETS_FIELD_NUMBER: _ClassVar[int]
    BYTES_ACKED_FIELD_NUMBER: _ClassVar[int]
    GOODPUT_BPS_FIELD_NUMBER: _ClassVar[int]
    RETRANSMISSION_RATIO_FIELD_NUMBER: _ClassVar[int]
    RTT_P95_MS_FIELD_NUMBER: _ClassVar[int]
    EXPOSED_SOCKETS_FIELD_NUMBER: _ClassVar[int]
    COLLECTION_HEALTHY_FIELD_NUMBER: _ClassVar[int]
    agent_id: str
    sockets: int
    bytes_acked: int
    goodput_bps: float
    retransmission_ratio: float
    rtt_p95_ms: float
    exposed_sockets: int
    collection_healthy: bool
    def __init__(self, agent_id: _Optional[str] = ..., sockets: _Optional[int] = ..., bytes_acked: _Optional[int] = ..., goodput_bps: _Optional[float] = ..., retransmission_ratio: _Optional[float] = ..., rtt_p95_ms: _Optional[float] = ..., exposed_sockets: _Optional[int] = ..., collection_healthy: _Optional[bool] = ...) -> None: ...

class Evidence(_message.Message):
    __slots__ = ("id", "analyzed_at", "window_start", "window_end", "query_version", "enrichment_version", "hosts", "artifact_sha256")
    ID_FIELD_NUMBER: _ClassVar[int]
    ANALYZED_AT_FIELD_NUMBER: _ClassVar[int]
    WINDOW_START_FIELD_NUMBER: _ClassVar[int]
    WINDOW_END_FIELD_NUMBER: _ClassVar[int]
    QUERY_VERSION_FIELD_NUMBER: _ClassVar[int]
    ENRICHMENT_VERSION_FIELD_NUMBER: _ClassVar[int]
    HOSTS_FIELD_NUMBER: _ClassVar[int]
    ARTIFACT_SHA256_FIELD_NUMBER: _ClassVar[int]
    id: str
    analyzed_at: _timestamp_pb2.Timestamp
    window_start: _timestamp_pb2.Timestamp
    window_end: _timestamp_pb2.Timestamp
    query_version: str
    enrichment_version: str
    hosts: _containers.RepeatedCompositeFieldContainer[HostEvidence]
    artifact_sha256: str
    def __init__(self, id: _Optional[str] = ..., analyzed_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., window_start: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., window_end: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., query_version: _Optional[str] = ..., enrichment_version: _Optional[str] = ..., hosts: _Optional[_Iterable[_Union[HostEvidence, _Mapping]]] = ..., artifact_sha256: _Optional[str] = ...) -> None: ...

class ExperimentPolicy(_message.Message):
    __slots__ = ("minimum_observation", "maximum_phase", "minimum_sockets_per_host", "minimum_bytes_per_host", "minimum_treated_hosts", "minimum_improvement", "maximum_retransmission_increase", "maximum_rtt_increase", "maximum_evidence_age")
    MINIMUM_OBSERVATION_FIELD_NUMBER: _ClassVar[int]
    MAXIMUM_PHASE_FIELD_NUMBER: _ClassVar[int]
    MINIMUM_SOCKETS_PER_HOST_FIELD_NUMBER: _ClassVar[int]
    MINIMUM_BYTES_PER_HOST_FIELD_NUMBER: _ClassVar[int]
    MINIMUM_TREATED_HOSTS_FIELD_NUMBER: _ClassVar[int]
    MINIMUM_IMPROVEMENT_FIELD_NUMBER: _ClassVar[int]
    MAXIMUM_RETRANSMISSION_INCREASE_FIELD_NUMBER: _ClassVar[int]
    MAXIMUM_RTT_INCREASE_FIELD_NUMBER: _ClassVar[int]
    MAXIMUM_EVIDENCE_AGE_FIELD_NUMBER: _ClassVar[int]
    minimum_observation: _duration_pb2.Duration
    maximum_phase: _duration_pb2.Duration
    minimum_sockets_per_host: int
    minimum_bytes_per_host: int
    minimum_treated_hosts: int
    minimum_improvement: float
    maximum_retransmission_increase: float
    maximum_rtt_increase: float
    maximum_evidence_age: _duration_pb2.Duration
    def __init__(self, minimum_observation: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ..., maximum_phase: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ..., minimum_sockets_per_host: _Optional[int] = ..., minimum_bytes_per_host: _Optional[int] = ..., minimum_treated_hosts: _Optional[int] = ..., minimum_improvement: _Optional[float] = ..., maximum_retransmission_increase: _Optional[float] = ..., maximum_rtt_increase: _Optional[float] = ..., maximum_evidence_age: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ...) -> None: ...

class Phase(_message.Message):
    __slots__ = ("target_hosts", "issued_at", "observation_started_at", "evaluated_at", "evidence", "outcome", "reviewer", "reason")
    TARGET_HOSTS_FIELD_NUMBER: _ClassVar[int]
    ISSUED_AT_FIELD_NUMBER: _ClassVar[int]
    OBSERVATION_STARTED_AT_FIELD_NUMBER: _ClassVar[int]
    EVALUATED_AT_FIELD_NUMBER: _ClassVar[int]
    EVIDENCE_FIELD_NUMBER: _ClassVar[int]
    OUTCOME_FIELD_NUMBER: _ClassVar[int]
    REVIEWER_FIELD_NUMBER: _ClassVar[int]
    REASON_FIELD_NUMBER: _ClassVar[int]
    target_hosts: int
    issued_at: _timestamp_pb2.Timestamp
    observation_started_at: _timestamp_pb2.Timestamp
    evaluated_at: _timestamp_pb2.Timestamp
    evidence: Evidence
    outcome: Outcome
    reviewer: str
    reason: str
    def __init__(self, target_hosts: _Optional[int] = ..., issued_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., observation_started_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., evaluated_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., evidence: _Optional[_Union[Evidence, _Mapping]] = ..., outcome: _Optional[_Union[Outcome, str]] = ..., reviewer: _Optional[str] = ..., reason: _Optional[str] = ...) -> None: ...

class Experiment(_message.Message):
    __slots__ = ("id", "location", "hypothesis", "treatment", "policy", "baseline", "cohort_order", "phases", "status", "revision", "proposed_at", "latest_evidence", "latest_outcome", "updated_at")
    ID_FIELD_NUMBER: _ClassVar[int]
    LOCATION_FIELD_NUMBER: _ClassVar[int]
    HYPOTHESIS_FIELD_NUMBER: _ClassVar[int]
    TREATMENT_FIELD_NUMBER: _ClassVar[int]
    POLICY_FIELD_NUMBER: _ClassVar[int]
    BASELINE_FIELD_NUMBER: _ClassVar[int]
    COHORT_ORDER_FIELD_NUMBER: _ClassVar[int]
    PHASES_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    REVISION_FIELD_NUMBER: _ClassVar[int]
    PROPOSED_AT_FIELD_NUMBER: _ClassVar[int]
    LATEST_EVIDENCE_FIELD_NUMBER: _ClassVar[int]
    LATEST_OUTCOME_FIELD_NUMBER: _ClassVar[int]
    UPDATED_AT_FIELD_NUMBER: _ClassVar[int]
    id: str
    location: str
    hypothesis: str
    treatment: Treatment
    policy: ExperimentPolicy
    baseline: Evidence
    cohort_order: _containers.RepeatedScalarFieldContainer[str]
    phases: _containers.RepeatedCompositeFieldContainer[Phase]
    status: ExperimentStatus
    revision: int
    proposed_at: _timestamp_pb2.Timestamp
    latest_evidence: Evidence
    latest_outcome: Outcome
    updated_at: _timestamp_pb2.Timestamp
    def __init__(self, id: _Optional[str] = ..., location: _Optional[str] = ..., hypothesis: _Optional[str] = ..., treatment: _Optional[_Union[Treatment, _Mapping]] = ..., policy: _Optional[_Union[ExperimentPolicy, _Mapping]] = ..., baseline: _Optional[_Union[Evidence, _Mapping]] = ..., cohort_order: _Optional[_Iterable[str]] = ..., phases: _Optional[_Iterable[_Union[Phase, _Mapping]]] = ..., status: _Optional[_Union[ExperimentStatus, str]] = ..., revision: _Optional[int] = ..., proposed_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., latest_evidence: _Optional[_Union[Evidence, _Mapping]] = ..., latest_outcome: _Optional[_Union[Outcome, str]] = ..., updated_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...
