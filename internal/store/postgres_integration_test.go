package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
)

func newPostgresTestStore(t *testing.T) *PostgresStore {
	t.Helper()

	pool, err := dockertest.NewPool("")
	if err != nil {
		t.Skipf("docker unavailable: %v", err)
	}

	resource, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "postgres",
		Tag:        "16-alpine",
		Env: []string{
			"POSTGRES_USER=lore",
			"POSTGRES_PASSWORD=lore",
			"POSTGRES_DB=lore",
		},
	}, func(config *docker.HostConfig) {
		config.AutoRemove = true
		config.RestartPolicy = docker.RestartPolicy{Name: "no"}
	})
	if err != nil {
		t.Skipf("postgres container unavailable: %v", err)
	}

	t.Cleanup(func() {
		_ = pool.Purge(resource)
	})

	databaseURL := fmt.Sprintf("postgres://lore:lore@127.0.0.1:%s/lore?sslmode=disable", resource.GetPort("5432/tcp"))

	var opened Contract
	err = pool.Retry(func() error {
		cfg := Config{
			Backend:              BackendPostgreSQL,
			DatabaseURL:          databaseURL,
			MaxObservationLength: 50000,
			MaxContextResults:    20,
			MaxSearchResults:     20,
			DedupeWindow:         15 * time.Minute,
		}
		if opened != nil {
			_ = opened.Close()
		}
		opened, err = Open(cfg)
		return err
	})
	if err != nil {
		t.Skipf("postgres did not become ready: %v", err)
	}

	pg, ok := opened.(*PostgresStore)
	if !ok {
		_ = opened.Close()
		t.Fatalf("Open() returned %T, want *PostgresStore", opened)
	}
	t.Cleanup(func() { _ = pg.Close() })
	return pg
}

func TestPostgresStoreBootstrapAndPingIntegration(t *testing.T) {
	s := newPostgresTestStore(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Ping(ctx); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}

	for _, table := range []string{"sessions", "observations", "sync_state", "sync_mutations"} {
		var exists bool
		if err := s.db.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = $1)`, table).Scan(&exists); err != nil {
			t.Fatalf("check table %s: %v", table, err)
		}
		if !exists {
			t.Fatalf("expected table %s to exist", table)
		}
	}
}

func TestPostgresStoreSessionObservationSliceIntegration(t *testing.T) {
	s := newPostgresTestStore(t)

	if err := s.CreateSession("pg-session", "Lore", "/tmp/lore"); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	firstID, err := s.AddObservation(AddObservationParams{
		SessionID: "pg-session",
		Type:      "architecture",
		Title:     "Auth boundary",
		Content:   "Keep postgres additive to sqlite",
		Project:   "Lore",
		Scope:     "project",
		TopicKey:  "architecture/auth-boundary",
	})
	if err != nil {
		t.Fatalf("AddObservation() first error = %v", err)
	}

	secondID, err := s.AddObservation(AddObservationParams{
		SessionID: "pg-session",
		Type:      "architecture",
		Title:     "Auth boundary",
		Content:   "Keep postgres additive to sqlite and sync-aware",
		Project:   "Lore",
		Scope:     "project",
		TopicKey:  "architecture/auth-boundary",
	})
	if err != nil {
		t.Fatalf("AddObservation() topic upsert error = %v", err)
	}
	if firstID != secondID {
		t.Fatalf("expected topic upsert to reuse observation id, got %d and %d", firstID, secondID)
	}

	dupID, err := s.AddObservation(AddObservationParams{
		SessionID: "pg-session",
		Type:      "decision",
		Title:     "Same title",
		Content:   "Repeated normalized payload for dedupe window",
		Project:   "Lore",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("AddObservation() dedupe first error = %v", err)
	}
	dupID2, err := s.AddObservation(AddObservationParams{
		SessionID: "pg-session",
		Type:      "decision",
		Title:     "Same title",
		Content:   "Repeated normalized payload for dedupe window",
		Project:   "Lore",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("AddObservation() dedupe second error = %v", err)
	}
	if dupID != dupID2 {
		t.Fatalf("expected duplicate content to reuse observation id, got %d and %d", dupID, dupID2)
	}
	if _, err := s.AddObservation(AddObservationParams{
		SessionID: "pg-session",
		Type:      "discovery",
		Title:     "Timeline count",
		Content:   "Postgres timeline total should count the full session",
		Project:   "Lore",
		Scope:     "project",
	}); err != nil {
		t.Fatalf("AddObservation() timeline count error = %v", err)
	}

	obs, err := s.GetObservation(firstID)
	if err != nil {
		t.Fatalf("GetObservation() error = %v", err)
	}
	if obs.RevisionCount != 2 {
		t.Fatalf("RevisionCount = %d, want 2", obs.RevisionCount)
	}
	if !strings.Contains(obs.Content, "sync-aware") {
		t.Fatalf("expected latest content, got %q", obs.Content)
	}

	recent, err := s.RecentObservations("lore", "project", 10)
	if err != nil {
		t.Fatalf("RecentObservations() error = %v", err)
	}
	if len(recent) != 3 {
		t.Fatalf("expected 3 visible observations, got %d", len(recent))
	}

	timeline, err := s.Timeline(firstID, 1, 1)
	if err != nil {
		t.Fatalf("Timeline() error = %v", err)
	}
	if timeline.Focus.ID != firstID {
		t.Fatalf("timeline focus id = %d, want %d", timeline.Focus.ID, firstID)
	}
	if timeline.SessionInfo == nil || timeline.SessionInfo.ID != "pg-session" {
		t.Fatalf("expected timeline session info for pg-session, got %+v", timeline.SessionInfo)
	}
	if timeline.TotalInRange != 3 {
		t.Fatalf("timeline TotalInRange = %d, want 3", timeline.TotalInRange)
	}

	updatedTitle := "Auth boundary updated"
	updatedContent := "Keep postgres additive, sync-aware, and backend-scoped"
	updated, err := s.UpdateObservation(firstID, UpdateObservationParams{Title: &updatedTitle, Content: &updatedContent})
	if err != nil {
		t.Fatalf("UpdateObservation() error = %v", err)
	}
	if updated.RevisionCount != 3 {
		t.Fatalf("updated RevisionCount = %d, want 3", updated.RevisionCount)
	}

	if err := s.DeleteObservation(firstID, false); err != nil {
		t.Fatalf("DeleteObservation() error = %v", err)
	}
	if _, err := s.GetObservation(firstID); err == nil {
		t.Fatalf("expected soft-deleted observation to be hidden")
	}

	recent, err = s.RecentObservations("lore", "project", 10)
	if err != nil {
		t.Fatalf("RecentObservations() after delete error = %v", err)
	}
	if len(recent) != 2 {
		t.Fatalf("expected 2 visible observations after soft delete, got %d", len(recent))
	}

	if err := s.EndSession("pg-session", "done"); err != nil {
		t.Fatalf("EndSession() error = %v", err)
	}

	session, err := s.GetSession("pg-session")
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if session.Summary == nil || *session.Summary != "done" {
		t.Fatalf("expected session summary to persist, got %+v", session.Summary)
	}

	rows, err := s.db.Query(`SELECT entity, entity_key, op, payload, project FROM sync_mutations ORDER BY seq ASC`)
	if err != nil {
		t.Fatalf("query sync_mutations: %v", err)
	}
	defer rows.Close()

	type mutation struct {
		Entity    string
		EntityKey string
		Op        string
		Payload   string
		Project   string
	}
	mutations := []mutation{}
	for rows.Next() {
		var m mutation
		if err := rows.Scan(&m.Entity, &m.EntityKey, &m.Op, &m.Payload, &m.Project); err != nil {
			t.Fatalf("scan sync mutation: %v", err)
		}
		mutations = append(mutations, m)
	}
	if len(mutations) < 6 {
		t.Fatalf("expected at least 6 sync mutations, got %d", len(mutations))
	}

	var sawDelete bool
	for _, m := range mutations {
		if m.Entity == SyncEntityObservation && m.Op == SyncOpDelete {
			sawDelete = true
			var payload map[string]any
			if err := json.Unmarshal([]byte(m.Payload), &payload); err != nil {
				t.Fatalf("decode delete payload: %v", err)
			}
			if payload["deleted"] != true {
				t.Fatalf("expected delete payload deleted=true, got %#v", payload["deleted"])
			}
		}
		if m.Project != "lore" {
			t.Fatalf("expected normalized project lore, got %q", m.Project)
		}
	}
	if !sawDelete {
		t.Fatalf("expected observation delete mutation to be enqueued")
	}

	var stateTarget string
	var lifecycle string
	var lastEnqueued int64
	if err := s.db.QueryRow(`SELECT target_key, lifecycle, last_enqueued_seq FROM sync_state WHERE target_key = $1`, DefaultSyncTargetKey).Scan(&stateTarget, &lifecycle, &lastEnqueued); err != nil {
		t.Fatalf("query sync_state: %v", err)
	}
	if stateTarget != DefaultSyncTargetKey || lifecycle != SyncLifecyclePending || lastEnqueued == 0 {
		t.Fatalf("unexpected sync_state target=%q lifecycle=%q last_enqueued_seq=%d", stateTarget, lifecycle, lastEnqueued)
	}
}

func TestPostgresStoreUnsupportedPromptSliceIntegration(t *testing.T) {
	s := newPostgresTestStore(t)
	_, err := s.AddPrompt(AddPromptParams{SessionID: "s", Content: "prompt", Project: "lore"})
	if err == nil {
		t.Fatalf("expected AddPrompt to be unsupported")
	}
	if _, ok := err.(ErrUnsupportedBackendFeature); !ok {
		t.Fatalf("expected ErrUnsupportedBackendFeature, got %T", err)
	}
}

var _ = sql.ErrNoRows
