-- Relational schema for the treatment controller. Every table is a storage
-- format for the domain types in pkg/treatment; nothing here is a protobuf
-- document. Enumerations are stored as their snake_case domain names so the
-- rows read in psql and so a renamed wire enum cannot break a query.

CREATE TABLE IF NOT EXISTS xtcp_treatment_agents (
  id text PRIMARY KEY,
  location text NOT NULL,
  enrolled_at timestamptz NOT NULL DEFAULT now(),
  last_seen timestamptz,
  boot_id text NOT NULL DEFAULT '',
  kernel_release text NOT NULL DEFAULT '',
  simple_host_routing boolean NOT NULL DEFAULT false,
  actuator_enabled boolean NOT NULL DEFAULT false,
  locally_suspended boolean NOT NULL DEFAULT false,
  reported_revision bigint NOT NULL DEFAULT 0,
  -- Digest of the algorithm and record rows last written for this agent;
  -- a heartbeat with an equal digest updates this row only.
  report_digest text NOT NULL DEFAULT '',
  desired_revision bigint NOT NULL,
  desired_suspended boolean NOT NULL DEFAULT false,
  desired_issued_at timestamptz NOT NULL,
  desired_updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS xtcp_treatment_agents_location ON xtcp_treatment_agents(location);
CREATE INDEX IF NOT EXISTS xtcp_treatment_agents_desired_updated ON xtcp_treatment_agents(desired_updated_at);

CREATE TABLE IF NOT EXISTS xtcp_treatment_agent_algorithms (
  agent_id text NOT NULL REFERENCES xtcp_treatment_agents(id) ON DELETE CASCADE,
  position integer NOT NULL,
  name text NOT NULL,
  PRIMARY KEY (agent_id, position)
);

-- Assignments are the controller's desired state per agent; records are the
-- agent's report of what it did with them. Both carry the treatment inline
-- because a record may describe an assignment that no longer exists.
CREATE TABLE IF NOT EXISTS xtcp_treatment_assignments (
  agent_id text NOT NULL REFERENCES xtcp_treatment_agents(id) ON DELETE CASCADE,
  experiment_id text NOT NULL,
  position integer NOT NULL,
  mode text NOT NULL,
  generation bigint NOT NULL,
  lease_expires_at timestamptz,
  treatment_id text NOT NULL,
  treatment_revision bigint NOT NULL,
  treatment_destination cidr NOT NULL,
  treatment_congestion_control text,
  treatment_congestion_lock boolean NOT NULL,
  treatment_rto_min_ms integer,
  treatment_initial_congestion_window integer,
  treatment_initial_receive_window integer,
  treatment_quick_ack boolean,
  treatment_ecn boolean,
  treatment_ecn_fabric_verified boolean NOT NULL,
  predecessor_id text,
  predecessor_revision bigint,
  predecessor_destination cidr,
  predecessor_congestion_control text,
  predecessor_congestion_lock boolean,
  predecessor_rto_min_ms integer,
  predecessor_initial_congestion_window integer,
  predecessor_initial_receive_window integer,
  predecessor_quick_ack boolean,
  predecessor_ecn boolean,
  predecessor_ecn_fabric_verified boolean,
  PRIMARY KEY (agent_id, experiment_id)
);
CREATE INDEX IF NOT EXISTS xtcp_treatment_assignments_experiment ON xtcp_treatment_assignments(experiment_id);

CREATE TABLE IF NOT EXISTS xtcp_treatment_records (
  agent_id text NOT NULL REFERENCES xtcp_treatment_agents(id) ON DELETE CASCADE,
  experiment_id text NOT NULL,
  position integer NOT NULL,
  mode text NOT NULL,
  generation bigint NOT NULL,
  lease_expires_at timestamptz,
  treatment_id text NOT NULL,
  treatment_revision bigint NOT NULL,
  treatment_destination cidr NOT NULL,
  treatment_congestion_control text,
  treatment_congestion_lock boolean NOT NULL,
  treatment_rto_min_ms integer,
  treatment_initial_congestion_window integer,
  treatment_initial_receive_window integer,
  treatment_quick_ack boolean,
  treatment_ecn boolean,
  treatment_ecn_fabric_verified boolean NOT NULL,
  predecessor_id text,
  predecessor_revision bigint,
  predecessor_destination cidr,
  predecessor_congestion_control text,
  predecessor_congestion_lock boolean,
  predecessor_rto_min_ms integer,
  predecessor_initial_congestion_window integer,
  predecessor_initial_receive_window integer,
  predecessor_quick_ack boolean,
  predecessor_ecn boolean,
  predecessor_ecn_fabric_verified boolean,
  desired_revision bigint NOT NULL,
  status text NOT NULL,
  received_at timestamptz,
  attempted_at timestamptz,
  applied_at timestamptz,
  verified_at timestamptz,
  removed_at timestamptz,
  error text NOT NULL DEFAULT '',
  expired_latched boolean NOT NULL DEFAULT false,
  PRIMARY KEY (agent_id, experiment_id)
);

CREATE TABLE IF NOT EXISTS xtcp_treatment_evidence (
  id text PRIMARY KEY,
  experiment_id text NOT NULL,
  analyzed_at timestamptz NOT NULL,
  window_start timestamptz NOT NULL,
  window_end timestamptz NOT NULL,
  query_version text NOT NULL,
  enrichment_version text NOT NULL DEFAULT '',
  artifact_sha256 text NOT NULL,
  digest text NOT NULL,
  received_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS xtcp_treatment_evidence_experiment ON xtcp_treatment_evidence(experiment_id);

CREATE TABLE IF NOT EXISTS xtcp_treatment_evidence_hosts (
  evidence_id text NOT NULL REFERENCES xtcp_treatment_evidence(id) ON DELETE CASCADE,
  position integer NOT NULL,
  agent_id text NOT NULL,
  sockets bigint NOT NULL,
  bytes_acked bigint NOT NULL,
  goodput_bps double precision NOT NULL,
  retransmission_ratio double precision NOT NULL,
  rtt_p95_ms double precision NOT NULL,
  exposed_sockets bigint NOT NULL,
  collection_healthy boolean NOT NULL,
  PRIMARY KEY (evidence_id, position)
);

CREATE TABLE IF NOT EXISTS xtcp_treatment_experiments (
  id text PRIMARY KEY,
  location text NOT NULL,
  hypothesis text NOT NULL,
  treatment_id text NOT NULL,
  treatment_revision bigint NOT NULL,
  treatment_destination cidr NOT NULL,
  treatment_congestion_control text,
  treatment_congestion_lock boolean NOT NULL,
  treatment_rto_min_ms integer,
  treatment_initial_congestion_window integer,
  treatment_initial_receive_window integer,
  treatment_quick_ack boolean,
  treatment_ecn boolean,
  treatment_ecn_fabric_verified boolean NOT NULL,
  minimum_observation_ns bigint NOT NULL,
  maximum_phase_ns bigint NOT NULL,
  maximum_evidence_age_ns bigint NOT NULL,
  minimum_sockets_per_host bigint NOT NULL,
  minimum_bytes_per_host bigint NOT NULL,
  minimum_treated_hosts integer NOT NULL,
  minimum_improvement double precision NOT NULL,
  maximum_retransmission_increase double precision NOT NULL,
  maximum_rtt_increase double precision NOT NULL,
  baseline_evidence_id text REFERENCES xtcp_treatment_evidence(id),
  status text NOT NULL,
  revision bigint NOT NULL,
  proposed_at timestamptz,
  latest_evidence_id text REFERENCES xtcp_treatment_evidence(id),
  latest_outcome text NOT NULL,
  updated_at timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS xtcp_treatment_experiments_location_status ON xtcp_treatment_experiments(location, status);
CREATE INDEX IF NOT EXISTS xtcp_treatment_experiments_destination ON xtcp_treatment_experiments(location, treatment_destination, updated_at);

CREATE TABLE IF NOT EXISTS xtcp_treatment_cohort (
  experiment_id text NOT NULL REFERENCES xtcp_treatment_experiments(id) ON DELETE CASCADE,
  position integer NOT NULL,
  agent_id text NOT NULL,
  PRIMARY KEY (experiment_id, position)
);

CREATE TABLE IF NOT EXISTS xtcp_treatment_phases (
  experiment_id text NOT NULL REFERENCES xtcp_treatment_experiments(id) ON DELETE CASCADE,
  position integer NOT NULL,
  target_hosts integer NOT NULL,
  issued_at timestamptz,
  observation_started_at timestamptz,
  evaluated_at timestamptz,
  evidence_id text REFERENCES xtcp_treatment_evidence(id),
  outcome text NOT NULL,
  reviewer text NOT NULL DEFAULT '',
  reason text NOT NULL DEFAULT '',
  PRIMARY KEY (experiment_id, position)
);

CREATE TABLE IF NOT EXISTS xtcp_treatment_holds (
  scope text PRIMARY KEY,
  reason text NOT NULL
);

-- The audit log is append-only and never queried by payload field, so the
-- payload stays a JSON document (protojson of the wire request or response).
CREATE TABLE IF NOT EXISTS xtcp_treatment_events (
  sequence bigserial PRIMARY KEY,
  occurred_at timestamptz NOT NULL DEFAULT now(),
  actor text NOT NULL,
  request_id text UNIQUE,
  kind text NOT NULL,
  payload jsonb NOT NULL
);
