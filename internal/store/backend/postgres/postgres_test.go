package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
)

func TestNormalizeDSNRejectsMissingDatabaseName(t *testing.T) {
	_, err := NormalizeDSN("postgres://lore:secret@127.0.0.1:5432")
	if err == nil || !strings.Contains(err.Error(), "missing database name") {
		t.Fatalf("expected missing database name error, got %v", err)
	}
}

func TestNormalizeDSNAddsDisableSSLForLocalhostOnlyWhenMissing(t *testing.T) {
	normalized, err := NormalizeDSN("postgres://lore:secret@127.0.0.1:5432/lore")
	if err != nil {
		t.Fatalf("NormalizeDSN() error = %v", err)
	}
	if !strings.Contains(normalized, "sslmode=disable") {
		t.Fatalf("expected sslmode=disable for localhost DSN, got %q", normalized)
	}

	remote, err := NormalizeDSN("postgres://lore:secret@db.internal:5432/lore")
	if err != nil {
		t.Fatalf("NormalizeDSN() remote error = %v", err)
	}
	if strings.Contains(remote, "sslmode=disable") {
		t.Fatalf("did not expect sslmode override for remote DSN, got %q", remote)
	}

	withQuery, err := NormalizeDSN("postgres://lore:secret@localhost:5432/lore?connect_timeout=5")
	if err != nil {
		t.Fatalf("NormalizeDSN() localhost query error = %v", err)
	}
	if !strings.Contains(withQuery, "connect_timeout=5") || !strings.Contains(withQuery, "sslmode=disable") {
		t.Fatalf("expected existing query params plus sslmode=disable, got %q", withQuery)
	}

	withSSLMode, err := NormalizeDSN("postgres://lore:secret@localhost:5432/lore?connect_timeout=5&sslmode=require")
	if err != nil {
		t.Fatalf("NormalizeDSN() localhost sslmode error = %v", err)
	}
	if !strings.Contains(withSSLMode, "sslmode=require") || strings.Contains(withSSLMode, "sslmode=disable") {
		t.Fatalf("expected existing sslmode to be preserved, got %q", withSSLMode)
	}
}

func TestOpenDatabaseBootstrapsIdempotently(t *testing.T) {
	name := registerStubDriver(t)

	oldPing := pingDatabase
	pingDatabase = func(context.Context, *sql.DB) error { return nil }
	t.Cleanup(func() { pingDatabase = oldPing })

	openFn := func(driverName, dataSourceName string) (*sql.DB, error) {
		return sql.Open(name, dataSourceName)
	}

	db, err := OpenDatabase("postgres://lore:secret@127.0.0.1:5432/lore", openFn)
	if err != nil {
		t.Fatalf("first OpenDatabase() error = %v", err)
	}
	_ = db.Close()

	db, err = OpenDatabase("postgres://lore:secret@127.0.0.1:5432/lore", openFn)
	if err != nil {
		t.Fatalf("second OpenDatabase() error = %v", err)
	}
	_ = db.Close()

	if execs := stubExecCount(name); execs == 0 {
		t.Fatalf("expected bootstrap statements to execute")
	}

	statements := strings.Join(stubExecutedStatements(name), "\n")
	for _, needle := range []string{
		"idx_pg_obs_search_vector",
		"CREATE TABLE IF NOT EXISTS skills",
		"CREATE TABLE IF NOT EXISTS stacks",
		"CREATE TABLE IF NOT EXISTS categories",
		"CREATE TABLE IF NOT EXISTS skill_stacks",
		"CREATE TABLE IF NOT EXISTS skill_categories",
		"CREATE TABLE IF NOT EXISTS skill_versions",
		"CREATE TABLE IF NOT EXISTS users",
		"password_hash TEXT NOT NULL DEFAULT ''",
		"status TEXT NOT NULL DEFAULT 'active'",
		"role TEXT NOT NULL DEFAULT 'developer'",
		"idx_pg_skills_search_vector",
		"idx_pg_skill_versions_skill_version_unique",
		"idx_pg_skill_versions_skill",
		"idx_pg_users_role",
		"idx_pg_users_status",
		"ADD COLUMN IF NOT EXISTS password_hash",
		"ADD COLUMN IF NOT EXISTS status",
		"UPDATE users SET role = 'developer' WHERE role = 'viewer'",
		"to_tsvector('simple'",
		"setweight(",
		"USING GIN",
	} {
		if !strings.Contains(statements, needle) {
			t.Fatalf("expected bootstrap statements to include %q, got %s", needle, statements)
		}
	}
}

func TestOpenDatabaseReturnsPingFailure(t *testing.T) {
	name := registerStubDriver(t)
	pingErr := errors.New("dial timeout")

	oldPing := pingDatabase
	pingDatabase = func(context.Context, *sql.DB) error { return pingErr }
	t.Cleanup(func() { pingDatabase = oldPing })

	_, err := OpenDatabase("postgres://lore:secret@127.0.0.1:5432/lore", func(driverName, dataSourceName string) (*sql.DB, error) {
		return sql.Open(name, dataSourceName)
	})
	if err == nil || !strings.Contains(err.Error(), "ping postgres database") {
		t.Fatalf("expected ping failure, got %v", err)
	}
}

var (
	stubMu         sync.Mutex
	stubCounters   = map[string]int{}
	stubStatements = map[string][]string{}
	registerSeq    int
)

func registerStubDriver(t *testing.T) string {
	t.Helper()
	stubMu.Lock()
	defer stubMu.Unlock()
	registerSeq++
	name := fmt.Sprintf("stub-postgres-%d", registerSeq)
	stubCounters[name] = 0
	stubStatements[name] = nil
	sql.Register(name, stubDriver{name: name})
	return name
}

func stubExecCount(name string) int {
	stubMu.Lock()
	defer stubMu.Unlock()
	return stubCounters[name]
}

func stubExecutedStatements(name string) []string {
	stubMu.Lock()
	defer stubMu.Unlock()
	return append([]string(nil), stubStatements[name]...)
}

type stubDriver struct{ name string }

func (d stubDriver) Open(string) (driver.Conn, error) { return &stubConn{name: d.name}, nil }

type stubConn struct{ name string }

func (c *stubConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("not implemented") }
func (c *stubConn) Close() error                        { return nil }
func (c *stubConn) Begin() (driver.Tx, error)           { return stubTx{}, nil }

func (c *stubConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	stubMu.Lock()
	stubCounters[c.name]++
	stubStatements[c.name] = append(stubStatements[c.name], query)
	stubMu.Unlock()
	return driver.RowsAffected(1), nil
}

func (c *stubConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(query, "RETURNING seq") {
		return &stubRows{columns: []string{"seq"}, values: [][]driver.Value{{int64(1)}}}, nil
	}
	return &stubRows{columns: []string{"ignored"}}, nil
}

type stubTx struct{}

func (stubTx) Commit() error   { return nil }
func (stubTx) Rollback() error { return nil }

type stubRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

func (r *stubRows) Columns() []string { return r.columns }
func (r *stubRows) Close() error      { return nil }

func (r *stubRows) Next(dest []driver.Value) error {
	if r.index >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.index])
	r.index++
	return nil
}
