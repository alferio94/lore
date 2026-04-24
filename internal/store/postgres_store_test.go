package store

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

func TestPostgresStoreUnsupportedMethodsReturnExplicitError(t *testing.T) {
	s := &PostgresStore{cfg: Config{Backend: BackendPostgreSQL}}

	checks := []struct {
		name string
		err  error
	}{
		{name: "prompts", err: func() error { _, err := s.AddPrompt(AddPromptParams{}); return err }()},
		{name: "skills catalog", err: func() error { _, err := s.ListSkills(ListSkillsParams{}); return err }()},
	}

	for _, tc := range checks {
		if tc.err == nil {
			t.Fatalf("%s: expected unsupported error", tc.name)
		}
		unsupported, ok := tc.err.(ErrUnsupportedBackendFeature)
		if !ok {
			t.Fatalf("%s: error type = %T, want ErrUnsupportedBackendFeature", tc.name, tc.err)
		}
		if unsupported.Backend != BackendPostgreSQL {
			t.Fatalf("%s: backend = %q, want %q", tc.name, unsupported.Backend, BackendPostgreSQL)
		}
		if !strings.Contains(tc.err.Error(), tc.name) {
			t.Fatalf("%s: error text %q missing feature name", tc.name, tc.err.Error())
		}
	}
}

func TestPostgresStoreSearchAndProjectListingErrorPaths(t *testing.T) {
	t.Run("Search returns wrapped direct query errors", func(t *testing.T) {
		s := newStubPostgresStore(t, Config{Backend: BackendPostgreSQL, MaxSearchResults: 5}, func(query string, _ []driver.NamedValue) (driver.Rows, error) {
			if strings.Contains(query, "WHERE topic_key = $1") {
				return nil, errors.New("forced direct query error")
			}
			return &stubPostgresRows{columns: []string{"project"}}, nil
		})

		_, err := s.Search("topic/key", SearchOptions{Project: "Lore", Scope: "project", Limit: 5})
		if err == nil || !strings.Contains(err.Error(), "search: forced direct query error") {
			t.Fatalf("Search() error = %v, want wrapped direct query error", err)
		}
	})

	t.Run("searchExact direct topic_key iterator failures surface", func(t *testing.T) {
		t.Run("scan error", func(t *testing.T) {
			s := newStubPostgresStore(t, Config{Backend: BackendPostgreSQL, MaxSearchResults: 5}, func(query string, _ []driver.NamedValue) (driver.Rows, error) {
				if strings.Contains(query, "WHERE topic_key = $1") {
					return &stubPostgresRows{
						columns: []string{"id"},
						values:  [][]driver.Value{{int64(1)}},
					}, nil
				}
				return &stubPostgresRows{columns: []string{"project"}}, nil
			})

			_, err := s.searchExact("topic/key", SearchOptions{Project: "Lore", Scope: "project", Limit: 5})
			if err == nil || !strings.Contains(err.Error(), "expected 1 destination arguments in Scan, not 16") {
				t.Fatalf("searchExact() error = %v, want direct scan error", err)
			}
		})

		t.Run("rows error", func(t *testing.T) {
			s := newStubPostgresStore(t, Config{Backend: BackendPostgreSQL, MaxSearchResults: 5}, func(query string, _ []driver.NamedValue) (driver.Rows, error) {
				if strings.Contains(query, "WHERE topic_key = $1") {
					return &stubPostgresRows{columns: []string{"id"}, nextErr: errors.New("forced direct rows err")}, nil
				}
				return &stubPostgresRows{columns: []string{"project"}}, nil
			})

			_, err := s.searchExact("topic/key", SearchOptions{Project: "Lore", Scope: "project", Limit: 5})
			if err == nil || !strings.Contains(err.Error(), "forced direct rows err") {
				t.Fatalf("searchExact() error = %v, want direct rows err", err)
			}
		})
	})

	t.Run("searchExact full-text iterator failures surface", func(t *testing.T) {
		t.Run("query error", func(t *testing.T) {
			s := newStubPostgresStore(t, Config{Backend: BackendPostgreSQL, MaxSearchResults: 5}, func(query string, _ []driver.NamedValue) (driver.Rows, error) {
				if strings.Contains(query, "ts_rank_cd") {
					return nil, errors.New("forced fts query error")
				}
				return &stubPostgresRows{columns: []string{"project"}}, nil
			})

			_, err := s.searchExact("auth", SearchOptions{Project: "Lore", Scope: "project", Limit: 5})
			if err == nil || !strings.Contains(err.Error(), "search: forced fts query error") {
				t.Fatalf("searchExact() error = %v, want wrapped fts query error", err)
			}
		})

		t.Run("scan error", func(t *testing.T) {
			s := newStubPostgresStore(t, Config{Backend: BackendPostgreSQL, MaxSearchResults: 5}, func(query string, _ []driver.NamedValue) (driver.Rows, error) {
				if strings.Contains(query, "ts_rank_cd") {
					return &stubPostgresRows{
						columns: []string{"id"},
						values:  [][]driver.Value{{int64(1)}},
					}, nil
				}
				return &stubPostgresRows{columns: []string{"project"}}, nil
			})

			_, err := s.searchExact("auth", SearchOptions{Project: "Lore", Scope: "project", Limit: 5})
			if err == nil || !strings.Contains(err.Error(), "expected 1 destination arguments in Scan, not 17") {
				t.Fatalf("searchExact() error = %v, want fts scan error", err)
			}
		})

		t.Run("rows error", func(t *testing.T) {
			s := newStubPostgresStore(t, Config{Backend: BackendPostgreSQL, MaxSearchResults: 5}, func(query string, _ []driver.NamedValue) (driver.Rows, error) {
				if strings.Contains(query, "ts_rank_cd") {
					return &stubPostgresRows{columns: []string{"id"}, nextErr: errors.New("forced fts rows err")}, nil
				}
				return &stubPostgresRows{columns: []string{"project"}}, nil
			})

			_, err := s.searchExact("auth", SearchOptions{Project: "Lore", Scope: "project", Limit: 5})
			if err == nil || !strings.Contains(err.Error(), "forced fts rows err") {
				t.Fatalf("searchExact() error = %v, want fts rows err", err)
			}
		})
	})

	t.Run("ListProjectNames surfaces query and iterator failures", func(t *testing.T) {
		t.Run("query error", func(t *testing.T) {
			s := newStubPostgresStore(t, Config{Backend: BackendPostgreSQL}, func(query string, _ []driver.NamedValue) (driver.Rows, error) {
				if strings.Contains(query, "SELECT DISTINCT project") {
					return nil, errors.New("forced list query error")
				}
				return &stubPostgresRows{columns: []string{"project"}}, nil
			})

			_, err := s.ListProjectNames()
			if err == nil || !strings.Contains(err.Error(), "forced list query error") {
				t.Fatalf("ListProjectNames() error = %v, want query error", err)
			}
		})

		t.Run("scan error", func(t *testing.T) {
			s := newStubPostgresStore(t, Config{Backend: BackendPostgreSQL}, func(query string, _ []driver.NamedValue) (driver.Rows, error) {
				if strings.Contains(query, "SELECT DISTINCT project") {
					return &stubPostgresRows{
						columns: []string{"project"},
						values:  [][]driver.Value{{nil}},
					}, nil
				}
				return &stubPostgresRows{columns: []string{"project"}}, nil
			})

			_, err := s.ListProjectNames()
			if err == nil || !strings.Contains(err.Error(), "converting NULL to string is unsupported") {
				t.Fatalf("ListProjectNames() error = %v, want scan error", err)
			}
		})

		t.Run("rows error", func(t *testing.T) {
			s := newStubPostgresStore(t, Config{Backend: BackendPostgreSQL}, func(query string, _ []driver.NamedValue) (driver.Rows, error) {
				if strings.Contains(query, "SELECT DISTINCT project") {
					return &stubPostgresRows{columns: []string{"project"}, nextErr: errors.New("forced list rows err")}, nil
				}
				return &stubPostgresRows{columns: []string{"project"}}, nil
			})

			_, err := s.ListProjectNames()
			if err == nil || !strings.Contains(err.Error(), "forced list rows err") {
				t.Fatalf("ListProjectNames() error = %v, want rows error", err)
			}
		})
	})
}

func TestPostgresObservationSearchVectorOmitsAliasPrefixWhenEmpty(t *testing.T) {
	got := postgresObservationSearchVector("")
	if strings.Contains(got, "..") || strings.Contains(got, ".title") {
		t.Fatalf("postgresObservationSearchVector(empty) = %q, want unqualified columns", got)
	}
	for _, column := range []string{"title", "topic_key", "type", "tool_name", "project", "content"} {
		if !strings.Contains(got, column) {
			t.Fatalf("postgresObservationSearchVector(empty) missing %q in %q", column, got)
		}
	}
}

var (
	stubPostgresDriverMu   sync.Mutex
	stubPostgresDriverSeq  int
	stubPostgresResponders = map[string]func(query string, args []driver.NamedValue) (driver.Rows, error){}
)

func newStubPostgresStore(t *testing.T, cfg Config, responder func(query string, args []driver.NamedValue) (driver.Rows, error)) *PostgresStore {
	t.Helper()

	name := registerStubPostgresDriver(t, responder)
	db, err := sql.Open(name, "stub")
	if err != nil {
		t.Fatalf("sql.Open(%q): %v", name, err)
	}
	t.Cleanup(func() { _ = db.Close() })

	return &PostgresStore{db: db, cfg: cfg}
}

func registerStubPostgresDriver(t *testing.T, responder func(query string, args []driver.NamedValue) (driver.Rows, error)) string {
	t.Helper()

	stubPostgresDriverMu.Lock()
	defer stubPostgresDriverMu.Unlock()

	stubPostgresDriverSeq++
	name := fmt.Sprintf("stub-postgres-store-%d", stubPostgresDriverSeq)
	stubPostgresResponders[name] = responder
	sql.Register(name, stubPostgresDriver{name: name})
	return name
}

type stubPostgresDriver struct{ name string }

func (d stubPostgresDriver) Open(string) (driver.Conn, error) {
	return &stubPostgresConn{name: d.name}, nil
}

type stubPostgresConn struct{ name string }

func (c *stubPostgresConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("not implemented") }
func (c *stubPostgresConn) Close() error                        { return nil }
func (c *stubPostgresConn) Begin() (driver.Tx, error)           { return stubPostgresTx{}, nil }

func (c *stubPostgresConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	stubPostgresDriverMu.Lock()
	responder := stubPostgresResponders[c.name]
	stubPostgresDriverMu.Unlock()
	if responder == nil {
		return nil, errors.New("missing stub responder")
	}
	return responder(query, args)
}

type stubPostgresTx struct{}

func (stubPostgresTx) Commit() error   { return nil }
func (stubPostgresTx) Rollback() error { return nil }

type stubPostgresRows struct {
	columns []string
	values  [][]driver.Value
	nextErr error
	index   int
	errSent bool
}

func (r *stubPostgresRows) Columns() []string { return r.columns }
func (r *stubPostgresRows) Close() error      { return nil }

func (r *stubPostgresRows) Next(dest []driver.Value) error {
	if r.index < len(r.values) {
		copy(dest, r.values[r.index])
		r.index++
		return nil
	}
	if r.nextErr != nil && !r.errSent {
		r.errSent = true
		return r.nextErr
	}
	return io.EOF
}
