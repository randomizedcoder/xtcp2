package controller

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"testing"
)

// Only the cheap pre-scan budget lookup is stubbed in template-validation unit
// tests. PostgreSQL integration tests exercise the real queries and indexes.
func emptyDiscoveryStore(t *testing.T) *Store {
	t.Helper()
	db := sql.OpenDB(discoveryConnector{})
	t.Cleanup(func() { db.Close() })
	return &Store{DB: db}
}

type discoveryConnector struct{}

func (discoveryConnector) Connect(context.Context) (driver.Conn, error) { return discoveryConn{}, nil }
func (discoveryConnector) Driver() driver.Driver                        { return discoveryDriver{} }

type discoveryDriver struct{}

func (discoveryDriver) Open(string) (driver.Conn, error) { return discoveryConn{}, nil }

type discoveryConn struct{}

func (discoveryConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepared query")
}
func (discoveryConn) Close() error              { return nil }
func (discoveryConn) Begin() (driver.Tx, error) { return nil, errors.New("unexpected transaction") }
func (discoveryConn) QueryContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	if q != activeExperimentSQL {
		return nil, errors.New("unexpected discovery query")
	}
	return &discoveryRows{}, nil
}

type discoveryRows struct{ done bool }

func (*discoveryRows) Columns() []string { return []string{"exists"} }
func (*discoveryRows) Close() error      { return nil }
func (r *discoveryRows) Next(values []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	values[0] = false
	return nil
}
