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

	"github.com/lib/pq"
)

func TestPostgresStoreUnsupportedPromptMethodsReturnExplicitError(t *testing.T) {
	s := &PostgresStore{cfg: Config{Backend: BackendPostgreSQL}}

	checks := []struct {
		name string
		err  error
	}{
		{name: "prompts", err: func() error { _, err := s.AddPrompt(AddPromptParams{}); return err }()},
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

func TestPostgresSkillSearchQuerySanitizesSpecialCharacters(t *testing.T) {
	if got := postgresSkillSearchQuery(`"special" OR AND`); got != `"special" "OR" "AND"` {
		t.Fatalf("postgresSkillSearchQuery() = %q, want %q", got, `"special" "OR" "AND"`)
	}

	if got := postgresSkillSearchQuery(`!!! (( ))`); got != "" {
		t.Fatalf("postgresSkillSearchQuery(punctuation-only) = %q, want empty string", got)
	}
}

func TestPostgresStoreLoadSkillRelationshipsNormalizesEmptySlices(t *testing.T) {
	s := newStubPostgresStore(t, Config{Backend: BackendPostgreSQL}, func(query string, _ []driver.NamedValue) (driver.Rows, error) {
		switch {
		case strings.Contains(query, "FROM skill_stacks"):
			return &stubPostgresRows{
				columns: []string{"id", "name", "display_name"},
				values:  [][]driver.Value{{int64(3), "go", "Go"}},
			}, nil
		case strings.Contains(query, "FROM skill_categories"):
			return &stubPostgresRows{columns: []string{"id", "name", "display_name"}}, nil
		default:
			return &stubPostgresRows{columns: []string{"id"}}, nil
		}
	})

	stacks, categories, err := s.loadSkillRelationships(s.db, 42)
	if err != nil {
		t.Fatalf("loadSkillRelationships(): %v", err)
	}
	if len(stacks) != 1 || stacks[0].Name != "go" {
		t.Fatalf("stacks = %+v, want one Go stack", stacks)
	}
	if categories == nil {
		t.Fatal("expected empty categories slice, got nil")
	}
	if len(categories) != 0 {
		t.Fatalf("categories len = %d, want 0", len(categories))
	}
}

func TestPostgresStoreListSkillsSanitizesSpecialCharacterSearchQuery(t *testing.T) {
	var capturedQuery string
	s := newStubPostgresStore(t, Config{Backend: BackendPostgreSQL}, func(query string, args []driver.NamedValue) (driver.Rows, error) {
		switch {
		case strings.Contains(query, "ts_rank_cd"):
			capturedQuery, _ = args[0].Value.(string)
			return &stubPostgresRows{columns: []string{"id", "name", "display_name", "triggers", "content", "compact_rules", "version", "is_active", "changed_by", "created_at", "updated_at"}}, nil
		case strings.Contains(query, "FROM skill_stacks") || strings.Contains(query, "FROM skill_categories"):
			return &stubPostgresRows{columns: []string{"id", "name", "display_name"}}, nil
		default:
			return &stubPostgresRows{columns: []string{"id"}}, nil
		}
	})

	skills, err := s.ListSkills(ListSkillsParams{Query: `"special" OR AND`})
	if err != nil {
		t.Fatalf("ListSkills(): %v", err)
	}
	if skills == nil {
		t.Fatal("expected empty skills slice, got nil")
	}
	if capturedQuery != `"special" "OR" "AND"` {
		t.Fatalf("sanitized query = %q, want %q", capturedQuery, `"special" "OR" "AND"`)
	}
}

func TestPostgresStoreCreateStackNormalizesDuplicateKey(t *testing.T) {
	s := newStubPostgresStoreWithExec(t, Config{Backend: BackendPostgreSQL}, nil, func(query string, _ []driver.NamedValue) (driver.Result, error) {
		if strings.Contains(query, "INSERT INTO stacks") {
			return nil, &pq.Error{Code: "23505", Constraint: "stacks_name_key"}
		}
		return stubPostgresResult{rowsAffected: 1}, nil
	})

	_, err := s.CreateStack("go", "Go")
	if err == nil || !strings.Contains(err.Error(), "UNIQUE") {
		t.Fatalf("CreateStack() error = %v, want normalized UNIQUE conflict", err)
	}
}

func TestPostgresStoreCreateCategoryNormalizesDuplicateKey(t *testing.T) {
	s := newStubPostgresStoreWithExec(t, Config{Backend: BackendPostgreSQL}, nil, func(query string, _ []driver.NamedValue) (driver.Result, error) {
		if strings.Contains(query, "INSERT INTO categories") {
			return nil, &pq.Error{Code: "23505", Constraint: "categories_name_key"}
		}
		return stubPostgresResult{rowsAffected: 1}, nil
	})

	_, err := s.CreateCategory("testing", "Testing")
	if err == nil || !strings.Contains(err.Error(), "UNIQUE") {
		t.Fatalf("CreateCategory() error = %v, want normalized UNIQUE conflict", err)
	}
}

func TestNormalizePostgresCatalogErrorRecognizesDuplicateKey(t *testing.T) {
	err := normalizePostgresCatalogError(&pq.Error{Code: "23505", Constraint: "skills_name_key"})
	if err == nil || !strings.Contains(err.Error(), "UNIQUE") {
		t.Fatalf("normalizePostgresCatalogError() = %v, want UNIQUE conflict text", err)
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
	stubPostgresDriverMu       sync.Mutex
	stubPostgresDriverSeq      int
	stubPostgresResponders     = map[string]func(query string, args []driver.NamedValue) (driver.Rows, error){}
	stubPostgresExecResponders = map[string]func(query string, args []driver.NamedValue) (driver.Result, error){}
)

func newStubPostgresStore(t *testing.T, cfg Config, responder func(query string, args []driver.NamedValue) (driver.Rows, error)) *PostgresStore {
	t.Helper()

	name := registerStubPostgresDriver(t, responder, nil)
	db, err := sql.Open(name, "stub")
	if err != nil {
		t.Fatalf("sql.Open(%q): %v", name, err)
	}
	t.Cleanup(func() { _ = db.Close() })

	return &PostgresStore{db: db, cfg: cfg}
}

func newStubPostgresStoreWithExec(t *testing.T, cfg Config, queryResponder func(query string, args []driver.NamedValue) (driver.Rows, error), execResponder func(query string, args []driver.NamedValue) (driver.Result, error)) *PostgresStore {
	t.Helper()

	name := registerStubPostgresDriver(t, queryResponder, execResponder)
	db, err := sql.Open(name, "stub")
	if err != nil {
		t.Fatalf("sql.Open(%q): %v", name, err)
	}
	t.Cleanup(func() { _ = db.Close() })

	return &PostgresStore{db: db, cfg: cfg}
}

func registerStubPostgresDriver(t *testing.T, responder func(query string, args []driver.NamedValue) (driver.Rows, error), execResponder func(query string, args []driver.NamedValue) (driver.Result, error)) string {
	t.Helper()

	stubPostgresDriverMu.Lock()
	defer stubPostgresDriverMu.Unlock()

	stubPostgresDriverSeq++
	name := fmt.Sprintf("stub-postgres-store-%d", stubPostgresDriverSeq)
	stubPostgresResponders[name] = responder
	stubPostgresExecResponders[name] = execResponder
	sql.Register(name, stubPostgresDriver{name: name})
	return name
}

type stubPostgresDriver struct{ name string }

func (d stubPostgresDriver) Open(string) (driver.Conn, error) {
	return &stubPostgresConn{name: d.name}, nil
}

type stubPostgresConn struct{ name string }

func (c *stubPostgresConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("not implemented")
}
func (c *stubPostgresConn) Close() error              { return nil }
func (c *stubPostgresConn) Begin() (driver.Tx, error) { return stubPostgresTx{}, nil }

func (c *stubPostgresConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	stubPostgresDriverMu.Lock()
	responder := stubPostgresResponders[c.name]
	stubPostgresDriverMu.Unlock()
	if responder == nil {
		return nil, errors.New("missing stub responder")
	}
	return responder(query, args)
}

func (c *stubPostgresConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	stubPostgresDriverMu.Lock()
	responder := stubPostgresExecResponders[c.name]
	stubPostgresDriverMu.Unlock()
	if responder == nil {
		return nil, errors.New("missing stub exec responder")
	}
	return responder(query, args)
}

type stubPostgresTx struct{}

func (stubPostgresTx) Commit() error   { return nil }
func (stubPostgresTx) Rollback() error { return nil }

type stubPostgresResult struct {
	lastInsertID int64
	rowsAffected int64
}

func (r stubPostgresResult) LastInsertId() (int64, error) { return r.lastInsertID, nil }
func (r stubPostgresResult) RowsAffected() (int64, error) { return r.rowsAffected, nil }

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
