package controller

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"time"

	_ "github.com/lib/pq"
	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_controller"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

//go:embed schema.sql
var schema string

type Store struct{ DB *sql.DB }

type rowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

const activeExperimentSQL = `SELECT EXISTS(SELECT 1 FROM xtcp_control_experiments
 WHERE location=$1 AND state->>'status' IN
 ('EXPERIMENT_STATUS_PROPOSED','EXPERIMENT_STATUS_RUNNING','EXPERIMENT_STATUS_PAUSED','EXPERIMENT_STATUS_SUSPENDED'))`

const discoveryDestinationSQL = `SELECT EXISTS(SELECT 1 FROM xtcp_control_experiments
 WHERE location=$1 AND state->'treatment'->>'destination'=$2
 AND (state->>'status' NOT IN ('EXPERIMENT_STATUS_RETIRED','EXPERIMENT_STATUS_ROLLED_BACK') OR updated_at > $3))`

var errExperimentBudget = errors.New("data-center experiment budget exhausted (one active experiment)")
var errDiscoverySuppressed = errors.New("destination already treated, held, proposed, or in cooldown")

func hasActiveExperiment(ctx context.Context, q rowQuerier, location string) (bool, error) {
	var exists bool
	err := q.QueryRowContext(ctx, activeExperimentSQL, location).Scan(&exists)
	return exists, err
}

func discoveryDestinationBlocked(ctx context.Context, q rowQuerier, location, destination string, now time.Time) (bool, error) {
	var exists bool
	err := q.QueryRowContext(ctx, discoveryDestinationSQL, location, destination, now.Add(-24*time.Hour)).Scan(&exists)
	return exists, err
}

func Open(ctx context.Context, dsn string) (*Store, error) {
	db, e := sql.Open("postgres", dsn)
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(32)
	db.SetMaxIdleConns(8)
	db.SetConnMaxLifetime(30 * time.Minute)
	if e = db.PingContext(ctx); e != nil {
		db.Close()
		return nil, e
	}
	return &Store{DB: db}, nil
}
func (s *Store) Migrate(ctx context.Context) error { _, e := s.DB.ExecContext(ctx, schema); return e }
func jsonMessage(m proto.Message) (string, error)  { b, e := protojson.Marshal(m); return string(b), e }

// Control mutations are rare and use one transaction-level advisory lock.
// This fences controller replicas without holding leadership during analysis.
// Agent heartbeats do not take this lock or rewrite experiments/fleet state.
func (s *Store) mutate(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(867530902)"); e != nil {
		return e
	}
	if e = fn(tx); e != nil {
		return e
	}
	return tx.Commit()
}

func loadExperiment(ctx context.Context, tx *sql.Tx, id string) (*pb.Experiment, error) {
	var raw []byte
	if e := tx.QueryRowContext(ctx, "SELECT state FROM xtcp_control_experiments WHERE id=$1 FOR UPDATE", id).Scan(&raw); e != nil {
		return nil, e
	}
	v := new(pb.Experiment)
	if e := protojson.Unmarshal(raw, v); e != nil {
		return nil, e
	}
	return v, nil
}
func saveExperiment(ctx context.Context, tx *sql.Tx, e *pb.Experiment) error {
	b, err := jsonMessage(e)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO xtcp_control_experiments(id,location,state) VALUES($1,$2,$3) ON CONFLICT(id) DO UPDATE SET state=EXCLUDED.state,updated_at=now()", e.Id, e.Location, b)
	return err
}
func desired(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id string) (*pb.DesiredState, error) {
	var raw []byte
	if e := q.QueryRowContext(ctx, "SELECT desired FROM xtcp_control_agents WHERE id=$1", id).Scan(&raw); e != nil {
		return nil, e
	}
	d := new(pb.DesiredState)
	if e := protojson.Unmarshal(raw, d); e != nil {
		return nil, e
	}
	return d, nil
}
func saveDesired(ctx context.Context, tx *sql.Tx, d *pb.DesiredState) error {
	b, e := jsonMessage(d)
	if e != nil {
		return e
	}
	res, e := tx.ExecContext(ctx, "UPDATE xtcp_control_agents SET desired=$2 WHERE id=$1", d.AgentId, b)
	if e != nil {
		return e
	}
	n, e := res.RowsAffected()
	if e == nil && n != 1 {
		return errors.New("agent not enrolled")
	}
	return e
}
func audit(ctx context.Context, tx *sql.Tx, actor, id, kind string, m proto.Message) error {
	b, e := jsonMessage(m)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO xtcp_control_events(actor,request_id,kind,payload) VALUES($1,NULLIF($2,''),$3,$4)", actor, id, kind, b)
	return e
}

// Repeated request IDs must have the same operation and request body. The
// original response is returned rather than applying a second mutation.
func replay(ctx context.Context, tx *sql.Tx, id, kind string, request, response proto.Message) (bool, error) {
	var k string
	var raw []byte
	e := tx.QueryRowContext(ctx, "SELECT kind,payload FROM xtcp_control_events WHERE request_id=$1", id).Scan(&k, &raw)
	if errors.Is(e, sql.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	if k != kind {
		return false, errors.New("request_id reused for another operation")
	}
	// Request audit is paired with a response event under request_id + '.result'.
	want, e := jsonMessage(request)
	if e != nil {
		return false, e
	}
	copyReq := request.ProtoReflect().New().Interface()
	if e = protojson.Unmarshal(raw, copyReq); e != nil {
		return false, e
	}
	got, e := jsonMessage(copyReq)
	if e != nil {
		return false, e
	}
	if got != want {
		return false, errors.New("request_id reused with different body")
	}
	if e = tx.QueryRowContext(ctx, "SELECT payload FROM xtcp_control_events WHERE request_id=$1", id+".result").Scan(&raw); e != nil {
		return false, fmt.Errorf("missing idempotent response: %w", e)
	}
	return true, protojson.Unmarshal(raw, response)
}
