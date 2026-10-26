// Package pgtest opens an isolated PostgreSQL store for tests. Each call
// creates a fresh schema on the database named by XTCP_CONTROLLER_TEST_DSN,
// points the connection's search_path at it and drops it on cleanup, so
// tests share one server without sharing rows.
package pgtest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/store/postgres"
)

// EnvDSN names the environment variable that enables PostgreSQL tests.
const EnvDSN = "XTCP_CONTROLLER_TEST_DSN"

var sequence atomic.Int64

// Available reports whether a PostgreSQL DSN is configured.
func Available() bool { return os.Getenv(EnvDSN) != "" }

// Open returns a migrated store on a private schema, or skips the test.
func Open(t testing.TB) *postgres.Store {
	t.Helper()
	return open(t, postgres.Open)
}

// Counter counts the statements a store sends to PostgreSQL.
type Counter struct{ n atomic.Int64 }

// Statements returns the number of statements sent so far.
func (c *Counter) Statements() int64 { return c.n.Load() }

// Reset sets the count back to zero.
func (c *Counter) Reset() { c.n.Store(0) }

func (c *Counter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	c.n.Add(1)
	return ctx
}

func (*Counter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// OpenCounting is Open with every statement counted, so tests can pin how
// many round trips a repository method makes.
func OpenCounting(t testing.TB) (*postgres.Store, *Counter) {
	t.Helper()
	c := new(Counter)
	s := open(t, func(ctx context.Context, dsn string) (*postgres.Store, error) {
		cfg, err := pgx.ParseConfig(dsn)
		if err != nil {
			return nil, err
		}
		cfg.Tracer = c
		db := stdlib.OpenDB(*cfg)
		if err = db.PingContext(ctx); err != nil {
			return nil, errors.Join(err, db.Close())
		}
		return postgres.New(db), nil
	})
	return s, c
}

func open(t testing.TB, connect func(context.Context, string) (*postgres.Store, error)) *postgres.Store {
	t.Helper()
	dsn := os.Getenv(EnvDSN)
	if dsn == "" {
		t.Skipf("set %s for PostgreSQL integration tests", EnvDSN)
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	schema := fmt.Sprintf("xtcp_test_%d_%d", time.Now().UnixNano(), sequence.Add(1))
	if _, err = admin.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(errors.Join(err, admin.Close()))
	}
	scoped, err := withSearchPath(dsn, schema)
	if err != nil {
		t.Fatal(errors.Join(err, admin.Close()))
	}
	s, err := connect(ctx, scoped)
	if err != nil {
		t.Fatal(errors.Join(err, admin.Close()))
	}
	t.Cleanup(func() {
		// The test's context is already canceled when cleanups run.
		cctx, ccancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer ccancel()
		if cerr := s.Close(); cerr != nil {
			t.Error(cerr)
		}
		if _, derr := admin.ExecContext(cctx, "DROP SCHEMA "+schema+" CASCADE"); derr != nil {
			t.Errorf("drop schema %s: %v", schema, derr)
		}
		if cerr := admin.Close(); cerr != nil {
			t.Error(cerr)
		}
	})
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return s
}

func withSearchPath(dsn, schema string) (string, error) {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			return "", err
		}
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		return u.String(), nil
	}
	return dsn + " search_path=" + schema, nil
}
