package store

import (
	"reflect"
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

func TestSkillReviewStateConstants(t *testing.T) {
	if SkillReviewStateDraft != "draft" {
		t.Fatalf("SkillReviewStateDraft = %q, want %q", SkillReviewStateDraft, "draft")
	}
	if SkillReviewStatePendingReview != "pending_review" {
		t.Fatalf("SkillReviewStatePendingReview = %q, want %q", SkillReviewStatePendingReview, "pending_review")
	}
	if SkillReviewStateApproved != "approved" {
		t.Fatalf("SkillReviewStateApproved = %q, want %q", SkillReviewStateApproved, "approved")
	}
	if SkillReviewStateRejected != "rejected" {
		t.Fatalf("SkillReviewStateRejected = %q, want %q", SkillReviewStateRejected, "rejected")
	}
}

func TestSkillStructHasReviewGovernanceFields(t *testing.T) {
	typ := reflect.TypeOf(Skill{})
	for _, field := range []string{"ReviewState", "CreatedBy", "ReviewedBy", "ReviewedAt", "ReviewNotes"} {
		if _, ok := typ.FieldByName(field); !ok {
			t.Fatalf("Skill struct missing %s field", field)
		}
	}
}

func TestStoreAndPostgresImplementAuditSkillReads(t *testing.T) {
	type auditSkillReader interface {
		ListSkillsForAudit(ListSkillsParams) ([]Skill, error)
		GetSkillForAudit(name string) (*Skill, error)
	}

	var _ auditSkillReader = (*Store)(nil)
	var _ auditSkillReader = (*PostgresStore)(nil)
}
