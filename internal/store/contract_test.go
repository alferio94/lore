package store

import (
	"strings"
	"testing"
)

func TestConfigSelectedBackendDefaultsToSQLite(t *testing.T) {
	cfg := Config{}
	if got := cfg.SelectedBackend(); got != BackendSQLite {
		t.Fatalf("SelectedBackend() = %q, want %q", got, BackendSQLite)
	}
}

func TestOpenUsesSQLiteBackendByDefault(t *testing.T) {
	cfg := mustDefaultConfig(t)
	cfg.DataDir = t.TempDir()

	opened, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = opened.Close() })

	concrete, ok := opened.(*Store)
	if !ok {
		t.Fatalf("Open() returned %T, want *Store", opened)
	}
	if concrete.cfg.SelectedBackend() != BackendSQLite {
		t.Fatalf("selected backend = %q, want %q", concrete.cfg.SelectedBackend(), BackendSQLite)
	}
}

func TestOpenRejectsUnsupportedBackend(t *testing.T) {
	cfg := mustDefaultConfig(t)
	cfg.DataDir = t.TempDir()
	cfg.Backend = Backend("postgres")

	_, err := Open(cfg)
	if err == nil {
		t.Fatal("expected unsupported backend error")
	}
	if !strings.Contains(err.Error(), "unsupported store backend postgres") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOpenPostgreSQLBackendReturnsOpenError(t *testing.T) {
	cfg := mustDefaultConfig(t)
	cfg.Backend = BackendPostgreSQL
	cfg.DatabaseURL = "postgres://"

	opened, err := Open(cfg)
	if err == nil {
		t.Fatalf("expected Open() error for invalid PostgreSQL config")
	}
	if opened != nil {
		t.Fatalf("Open() returned non-nil store on error: %T", opened)
	}
	if !strings.Contains(err.Error(), "invalid postgres DATABASE_URL") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUnsupportedBackendFeatureErrorMessage(t *testing.T) {
	err := ErrUnsupportedBackendFeature{Backend: BackendPostgreSQL, Feature: "search"}
	if !strings.Contains(err.Error(), "backend postgresql does not support search") {
		t.Fatalf("unexpected error text: %q", err.Error())
	}
}
