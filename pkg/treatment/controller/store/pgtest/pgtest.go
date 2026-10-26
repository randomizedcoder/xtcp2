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
	s, err := postgres.Open(ctx, scoped)
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
