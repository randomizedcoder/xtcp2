-- Control-plane state is transactional. ClickHouse is never a coordination lock.
CREATE TABLE IF NOT EXISTS xtcp_control_agents (
  id text PRIMARY KEY,
  location text NOT NULL,
  desired jsonb NOT NULL,
  observed jsonb,
  last_seen timestamptz,
  suspended boolean NOT NULL DEFAULT false
);
CREATE INDEX IF NOT EXISTS xtcp_control_agents_location ON xtcp_control_agents(location);
CREATE TABLE IF NOT EXISTS xtcp_control_experiments (
  id text PRIMARY KEY,
  location text NOT NULL,
  state jsonb NOT NULL
);
ALTER TABLE xtcp_control_experiments ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT now();
-- Match discovery's equality lookups and its terminal-experiment cooldown.
-- Do not index the large evidence-bearing state document as a whole.
CREATE INDEX IF NOT EXISTS xtcp_control_experiments_destination
  ON xtcp_control_experiments(location, (state->'treatment'->>'destination'), updated_at DESC);
-- Status first also supports maintenance across all data centers; location
-- narrows the one-active-experiment budget check.
CREATE INDEX IF NOT EXISTS xtcp_control_experiments_status_location
  ON xtcp_control_experiments((state->>'status'), location);
CREATE TABLE IF NOT EXISTS xtcp_control_holds (
  scope text PRIMARY KEY,
  reason text NOT NULL
);
CREATE TABLE IF NOT EXISTS xtcp_control_events (
  sequence bigserial PRIMARY KEY,
  occurred_at timestamptz NOT NULL DEFAULT now(),
  actor text NOT NULL,
  request_id text UNIQUE,
  kind text NOT NULL,
  payload jsonb NOT NULL
);
-- Durable event rows are the outbox and audit log. Watch subscribers only need
-- revision hints; reconnect/check-in always fetches authoritative agent state.
CREATE INDEX IF NOT EXISTS xtcp_control_events_time ON xtcp_control_events(occurred_at);
CREATE TABLE IF NOT EXISTS xtcp_control_evidence (
  id text PRIMARY KEY,
  experiment_id text NOT NULL,
  received_at timestamptz NOT NULL DEFAULT now(),
  state jsonb NOT NULL
);
