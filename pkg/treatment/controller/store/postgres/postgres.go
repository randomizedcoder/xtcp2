// Package postgres implements the controller's store ports on PostgreSQL
// through database/sql with the pgx driver. Every SQL string in the
// controller lives in this package.
package postgres

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/store"
)

//go:embed schema.sql
var schema string

// mutationLock is the transaction-level advisory lock every Store.Tx takes.
// It fences controller replicas without electing a leader.
const mutationLock = 867530902

// Store is a connection pool. It satisfies store.Store.
type Store struct{ db *sql.DB }

// Open connects, pings and returns the store. The DSN is anything pgx
// accepts (a URL or key=value pairs).
func Open(ctx context.Context, dsn string) (*Store, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(32)
	db.SetMaxIdleConns(8)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// New wraps an existing pool; tests use it with a per-test search_path.
func New(db *sql.DB) *Store { return &Store{db: db} }

// Migrate creates every table and index that does not exist yet.
func (s *Store) Migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, schema)
	return err
}

func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }
func (s *Store) Close() error                   { return s.db.Close() }

// DB exposes the pool for tests that inspect query plans.
func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) Repos() store.Repos { return repos(&runner{db: s.db}) }

// Tx runs fn in one transaction holding the mutation lock. Agent heartbeats
// go through Repos and never take it.
func (s *Store) Tx(ctx context.Context, fn func(store.Repos) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", mutationLock); err != nil {
		return err
	}
	if err = fn(repos(&runner{tx: tx})); err != nil {
		return err
	}
	return tx.Commit()
}

func repos(r *runner) store.Repos {
	return store.Repos{Agents: &agents{r}, Experiments: &experiments{r}, Evidence: &evidence{r}, Holds: &holds{r}, Audit: &audit{r}, Metrics: &metrics{r}}
}

// querier is what *sql.DB and *sql.Tx have in common.
type querier interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// runner binds a repository to either the pool or an open transaction.
type runner struct {
	db *sql.DB
	tx *sql.Tx
}

func (r *runner) q() querier {
	if r.tx != nil {
		return r.tx
	}
	return r.db
}

// locked reports whether the caller is inside Store.Tx, in which case reads
// that precede writes lock their rows.
func (r *runner) locked() bool { return r.tx != nil }

// atomic runs fn in the open transaction, or in a short private one when the
// repository is bound to the pool, so multi-statement writes stay consistent
// without the mutation lock.
func (r *runner) atomic(ctx context.Context, fn func(querier) error) error {
	if r.tx != nil {
		return fn(r.tx)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func isUniqueViolation(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}

func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return store.ErrNotFound
	}
	return err
}

// placeholders returns "$start,$start+1,...".
func placeholders(start, n int) string {
	parts := make([]string, n)
	for i := range n {
		parts[i] = fmt.Sprintf("$%d", start+i)
	}
	return strings.Join(parts, ",")
}

// insertSQL builds INSERT INTO table(columns) VALUES($1,...). Columns named
// *_destination are cast to cidr.
func insertSQL(table string, columns []string) string {
	vals := make([]string, len(columns))
	for i, c := range columns {
		vals[i] = fmt.Sprintf("$%d", i+1)
		if strings.HasSuffix(c, "destination") {
			vals[i] += "::cidr"
		}
	}
	return "INSERT INTO " + table + "(" + strings.Join(columns, ",") + ") VALUES(" + strings.Join(vals, ",") + ")"
}

// selectList renders columns for SELECT, reading cidr columns as text.
func selectList(columns []string) string {
	out := make([]string, len(columns))
	for i, c := range columns {
		out[i] = c
		if strings.HasSuffix(c, "destination") {
			out[i] = c + "::text"
		}
	}
	return strings.Join(out, ",")
}

// batchInsert inserts rows in chunks that stay under PostgreSQL's parameter
// limit. rows must all have len(columns) values.
func batchInsert(ctx context.Context, q querier, table string, columns []string, rows [][]any) error {
	if len(rows) == 0 {
		return nil
	}
	chunk := max(1, 60000/len(columns))
	for start := 0; start < len(rows); start += chunk {
		end := min(start+chunk, len(rows))
		var sb strings.Builder
		sb.WriteString("INSERT INTO " + table + "(" + strings.Join(columns, ",") + ") VALUES")
		args := make([]any, 0, (end-start)*len(columns))
		for i, row := range rows[start:end] {
			if i > 0 {
				sb.WriteByte(',')
			}
			sb.WriteByte('(')
			for j, c := range columns {
				if j > 0 {
					sb.WriteByte(',')
				}
				fmt.Fprintf(&sb, "$%d", len(args)+1)
				if strings.HasSuffix(c, "destination") {
					sb.WriteString("::cidr")
				}
				args = append(args, row[j])
			}
			sb.WriteByte(')')
		}
		if _, err := q.ExecContext(ctx, sb.String(), args...); err != nil {
			return err
		}
	}
	return nil
}

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

func fromNullTime(t sql.NullTime) time.Time {
	if !t.Valid {
		return time.Time{}
	}
	return t.Time
}

func nullString(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

func nullUint32(p *uint32) any {
	if p == nil {
		return nil
	}
	return int64(*p)
}

func nullBool(p *bool) any {
	if p == nil {
		return nil
	}
	return *p
}
