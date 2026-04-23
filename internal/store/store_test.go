package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	projectpkg "github.com/alferio94/lore/internal/project"
	_ "modernc.org/sqlite"
)

func TestStorePing(t *testing.T) {
	t.Run("healthy db", func(t *testing.T) {
		s := newTestStore(t)

		if err := s.Ping(context.Background()); err != nil {
			t.Fatalf("Ping() error = %v, want nil", err)
		}
	})

	t.Run("unavailable db", func(t *testing.T) {
		s := newTestStore(t)
		if err := s.Close(); err != nil {
			t.Fatalf("Close(): %v", err)
		}

		if err := s.Ping(context.Background()); err == nil {
			t.Fatalf("Ping() error = nil, want error")
		}
	})
}

func mustDefaultConfig(t *testing.T) Config {
	t.Helper()
	cfg, err := DefaultConfig()
	if err != nil {
		t.Fatalf("DefaultConfig: %v", err)
	}
	return cfg
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	cfg := mustDefaultConfig(t)
	cfg.DataDir = t.TempDir()
	cfg.DedupeWindow = time.Hour

	s, err := New(cfg)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() {
		_ = s.Close()
	})
	return s
}

type fakeRows struct {
	next    []bool
	scanErr error
	err     error
}

func (f *fakeRows) Next() bool {
	if len(f.next) == 0 {
		return false
	}
	v := f.next[0]
	f.next = f.next[1:]
	return v
}

func (f *fakeRows) Scan(dest ...any) error {
	return f.scanErr
}

func (f *fakeRows) Err() error {
	return f.err
}

func (f *fakeRows) Close() error {
	return nil
}

func TestAddObservationDeduplicatesWithinWindow(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSession("s1", "engram", "/tmp/engram"); err != nil {
		t.Fatalf("create session: %v", err)
	}

	firstID, err := s.AddObservation(AddObservationParams{
		SessionID: "s1",
		Type:      "bugfix",
		Title:     "Fixed tokenizer",
		Content:   "Normalized tokenizer panic on edge case",
		Project:   "engram",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("add first observation: %v", err)
	}

	secondID, err := s.AddObservation(AddObservationParams{
		SessionID: "s1",
		Type:      "bugfix",
		Title:     "Fixed tokenizer",
		Content:   "normalized   tokenizer panic on EDGE case",
		Project:   "engram",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("add duplicate observation: %v", err)
	}

	if firstID != secondID {
		t.Fatalf("expected duplicate to reuse same id, got %d and %d", firstID, secondID)
	}

	obs, err := s.GetObservation(firstID)
	if err != nil {
		t.Fatalf("get deduped observation: %v", err)
	}
	if obs.DuplicateCount != 2 {
		t.Fatalf("expected duplicate_count=2, got %d", obs.DuplicateCount)
	}
}

func TestScopeFiltersSearchAndContext(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSession("s1", "engram", "/tmp/engram"); err != nil {
		t.Fatalf("create session: %v", err)
	}

	_, err := s.AddObservation(AddObservationParams{
		SessionID: "s1",
		Type:      "decision",
		Title:     "Project auth",
		Content:   "Keep auth middleware in project memory",
		Project:   "engram",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("add project observation: %v", err)
	}

	_, err = s.AddObservation(AddObservationParams{
		SessionID: "s1",
		Type:      "decision",
		Title:     "Personal note",
		Content:   "Use this regex trick later",
		Project:   "engram",
		Scope:     "personal",
	})
	if err != nil {
		t.Fatalf("add personal observation: %v", err)
	}

	projectResults, err := s.Search("regex", SearchOptions{Project: "engram", Scope: "project", Limit: 10})
	if err != nil {
		t.Fatalf("search project scope: %v", err)
	}
	if len(projectResults) != 0 {
		t.Fatalf("expected no project-scope regex results, got %d", len(projectResults))
	}

	personalResults, err := s.Search("regex", SearchOptions{Project: "engram", Scope: "personal", Limit: 10})
	if err != nil {
		t.Fatalf("search personal scope: %v", err)
	}
	if len(personalResults) != 1 {
		t.Fatalf("expected 1 personal-scope result, got %d", len(personalResults))
	}

	ctx, err := s.FormatContext("engram", "personal")
	if err != nil {
		t.Fatalf("format context personal: %v", err)
	}
	if !strings.Contains(ctx, "Personal note") {
		t.Fatalf("expected personal context to include personal observation")
	}
	if strings.Contains(ctx, "Project auth") {
		t.Fatalf("expected personal context to exclude project observation")
	}
}

func TestUpdateAndSoftDeleteExcludedFromSearchAndTimeline(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSession("s1", "engram", "/tmp/engram"); err != nil {
		t.Fatalf("create session: %v", err)
	}

	firstID, err := s.AddObservation(AddObservationParams{
		SessionID: "s1",
		Type:      "bugfix",
		Title:     "first",
		Content:   "first event",
		Project:   "engram",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("add first: %v", err)
	}

	middleID, err := s.AddObservation(AddObservationParams{
		SessionID: "s1",
		Type:      "bugfix",
		Title:     "middle",
		Content:   "to be deleted",
		Project:   "engram",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("add middle: %v", err)
	}

	lastID, err := s.AddObservation(AddObservationParams{
		SessionID: "s1",
		Type:      "bugfix",
		Title:     "last",
		Content:   "last event",
		Project:   "engram",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("add last: %v", err)
	}

	newTitle := "last-updated"
	newContent := "updated content"
	newScope := "personal"
	updated, err := s.UpdateObservation(lastID, UpdateObservationParams{
		Title:   &newTitle,
		Content: &newContent,
		Scope:   &newScope,
	})
	if err != nil {
		t.Fatalf("update observation: %v", err)
	}
	if updated.Title != newTitle || updated.Scope != "personal" {
		t.Fatalf("update did not apply; got title=%q scope=%q", updated.Title, updated.Scope)
	}

	if err := s.DeleteObservation(middleID, false); err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	if _, err := s.GetObservation(middleID); err == nil {
		t.Fatalf("expected deleted observation to be hidden from GetObservation")
	}

	searchResults, err := s.Search("deleted", SearchOptions{Project: "engram", Limit: 10})
	if err != nil {
		t.Fatalf("search after delete: %v", err)
	}
	if len(searchResults) != 0 {
		t.Fatalf("expected deleted observation excluded from search")
	}

	timeline, err := s.Timeline(firstID, 5, 5)
	if err != nil {
		t.Fatalf("timeline: %v", err)
	}
	if len(timeline.After) != 1 || timeline.After[0].ID != lastID {
		t.Fatalf("expected timeline to skip deleted observation")
	}

	if err := s.DeleteObservation(lastID, true); err != nil {
		t.Fatalf("hard delete: %v", err)
	}
	if _, err := s.GetObservation(lastID); err == nil {
		t.Fatalf("expected hard-deleted observation to be missing")
	}
}

func TestTopicKeyUpsertUpdatesSameTopicWithoutCreatingNewRow(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSession("s1", "engram", "/tmp/engram"); err != nil {
		t.Fatalf("create session: %v", err)
	}

	firstID, err := s.AddObservation(AddObservationParams{
		SessionID: "s1",
		Type:      "architecture",
		Title:     "Auth architecture",
		Content:   "Use middleware for JWT validation.",
		Project:   "engram",
		Scope:     "project",
		TopicKey:  "architecture auth model",
	})
	if err != nil {
		t.Fatalf("add first architecture: %v", err)
	}

	secondID, err := s.AddObservation(AddObservationParams{
		SessionID: "s1",
		Type:      "architecture",
		Title:     "Auth architecture",
		Content:   "Move auth to gateway + middleware chain.",
		Project:   "engram",
		Scope:     "project",
		TopicKey:  "ARCHITECTURE   AUTH  MODEL",
	})
	if err != nil {
		t.Fatalf("upsert architecture: %v", err)
	}

	if firstID != secondID {
		t.Fatalf("expected topic upsert to reuse id, got %d and %d", firstID, secondID)
	}

	obs, err := s.GetObservation(firstID)
	if err != nil {
		t.Fatalf("get upserted observation: %v", err)
	}
	if obs.RevisionCount != 2 {
		t.Fatalf("expected revision_count=2, got %d", obs.RevisionCount)
	}
	if obs.TopicKey == nil || *obs.TopicKey != "architecture-auth-model" {
		t.Fatalf("expected normalized topic key, got %v", obs.TopicKey)
	}
	if !strings.Contains(obs.Content, "gateway") {
		t.Fatalf("expected latest content after upsert, got %q", obs.Content)
	}
}

func TestDifferentTopicsDoNotReplaceEachOther(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSession("s1", "engram", "/tmp/engram"); err != nil {
		t.Fatalf("create session: %v", err)
	}

	archID, err := s.AddObservation(AddObservationParams{
		SessionID: "s1",
		Type:      "architecture",
		Title:     "Auth architecture",
		Content:   "Architecture decision",
		Project:   "engram",
		Scope:     "project",
		TopicKey:  "architecture/auth",
	})
	if err != nil {
		t.Fatalf("add architecture observation: %v", err)
	}

	bugID, err := s.AddObservation(AddObservationParams{
		SessionID: "s1",
		Type:      "bugfix",
		Title:     "Fix auth nil panic",
		Content:   "Bugfix details",
		Project:   "engram",
		Scope:     "project",
		TopicKey:  "bug/auth-nil-panic",
	})
	if err != nil {
		t.Fatalf("add bug observation: %v", err)
	}

	if archID == bugID {
		t.Fatalf("expected different topic keys to create different observations")
	}

	observations, err := s.AllObservations("engram", "project", 10)
	if err != nil {
		t.Fatalf("all observations: %v", err)
	}
	if len(observations) != 2 {
		t.Fatalf("expected 2 observations, got %d", len(observations))
	}
}

func TestNewMigratesLegacyObservationIDSchema(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := filepath.Join(dataDir, "lore.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}

	_, err = db.Exec(`
		CREATE TABLE sessions (
			id TEXT PRIMARY KEY,
			project TEXT NOT NULL,
			directory TEXT NOT NULL,
			started_at TEXT NOT NULL DEFAULT (datetime('now')),
			ended_at TEXT,
			summary TEXT
		);
		CREATE TABLE observations (
			id INT,
			session_id TEXT,
			type TEXT,
			title TEXT,
			content TEXT,
			tool_name TEXT,
			project TEXT,
			created_at TEXT
		);
		INSERT INTO sessions (id, project, directory) VALUES ('s1', 'engram', '/tmp/engram');
		INSERT INTO observations (id, session_id, type, title, content, project, created_at)
		VALUES
			(NULL, 's1', 'bugfix', 'legacy null', 'legacy null content', 'engram', datetime('now')),
			(7, 's1', 'bugfix', 'legacy fixed', 'legacy fixed content', 'engram', datetime('now')),
			(7, 's1', 'bugfix', 'legacy duplicate', 'legacy duplicate content', 'engram', datetime('now'));
	`)
	if err != nil {
		_ = db.Close()
		t.Fatalf("seed legacy db: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	cfg := mustDefaultConfig(t)
	cfg.DataDir = dataDir

	s, err := New(cfg)
	if err != nil {
		t.Fatalf("new store after legacy schema: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	obs, err := s.AllObservations("engram", "", 20)
	if err != nil {
		t.Fatalf("all observations after migration: %v", err)
	}
	if len(obs) != 3 {
		t.Fatalf("expected 3 migrated observations, got %d", len(obs))
	}

	seen := make(map[int64]bool)
	for _, o := range obs {
		if o.ID <= 0 {
			t.Fatalf("expected migrated observation id > 0, got %d", o.ID)
		}
		if seen[o.ID] {
			t.Fatalf("expected unique migrated ids, duplicate %d", o.ID)
		}
		seen[o.ID] = true
	}

	results, err := s.Search("legacy", SearchOptions{Project: "engram", Limit: 10})
	if err != nil {
		t.Fatalf("search after migration: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected search results after migration")
	}

	newID, err := s.AddObservation(AddObservationParams{
		SessionID: "s1",
		Type:      "bugfix",
		Title:     "post migration",
		Content:   "new row should get id",
		Project:   "engram",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("add observation after migration: %v", err)
	}
	if newID <= 0 {
		t.Fatalf("expected autoincrement id after migration, got %d", newID)
	}
}

func TestNewMigratesLegacyUserPromptsSyncIDSchema(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := filepath.Join(dataDir, "lore.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}

	_, err = db.Exec(`
		CREATE TABLE sessions (
			id TEXT PRIMARY KEY,
			project TEXT NOT NULL,
			directory TEXT NOT NULL,
			started_at TEXT NOT NULL DEFAULT (datetime('now')),
			ended_at TEXT,
			summary TEXT
		);
		CREATE TABLE user_prompts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			content TEXT NOT NULL,
			project TEXT,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			FOREIGN KEY (session_id) REFERENCES sessions(id)
		);
		INSERT INTO sessions (id, project, directory) VALUES ('s1', 'engram', '/tmp/engram');
		INSERT INTO user_prompts (session_id, content, project) VALUES ('s1', 'legacy prompt', 'engram');
	`)
	if err != nil {
		_ = db.Close()
		t.Fatalf("seed legacy db: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	cfg := mustDefaultConfig(t)
	cfg.DataDir = dataDir

	s, err := New(cfg)
	if err != nil {
		t.Fatalf("new store after legacy prompt schema: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	var syncID string
	if err := s.db.QueryRow("SELECT sync_id FROM user_prompts WHERE content = ?", "legacy prompt").Scan(&syncID); err != nil {
		t.Fatalf("query migrated prompt sync_id: %v", err)
	}
	if syncID == "" {
		t.Fatalf("expected migrated prompt sync_id to be backfilled")
	}

	var hasSyncIDColumn bool
	rows, err := s.db.Query("PRAGMA table_info(user_prompts)")
	if err != nil {
		t.Fatalf("query prompt columns: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, pk int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			t.Fatalf("scan prompt column: %v", err)
		}
		if name == "sync_id" {
			hasSyncIDColumn = true
			break
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate prompt columns: %v", err)
	}
	if !hasSyncIDColumn {
		t.Fatalf("expected user_prompts.sync_id column after migration")
	}

	var indexName string
	if err := s.db.QueryRow("SELECT name FROM sqlite_master WHERE type = 'index' AND name = 'idx_prompts_sync_id'").Scan(&indexName); err != nil {
		t.Fatalf("query prompt sync index: %v", err)
	}
	if indexName != "idx_prompts_sync_id" {
		t.Fatalf("expected idx_prompts_sync_id to exist, got %q", indexName)
	}
}

func TestSuggestTopicKeyNormalizesDeterministically(t *testing.T) {
	got := SuggestTopicKey("Architecture", "  Auth Model  ", "ignored")
	if got != "architecture/auth-model" {
		t.Fatalf("expected architecture/auth-model, got %q", got)
	}

	fallback := SuggestTopicKey("bugfix", "", "Fix nil panic in auth middleware on empty token")
	if fallback != "bug/fix-nil-panic-in-auth-middleware-on-empty" {
		t.Fatalf("unexpected fallback topic key: %q", fallback)
	}
}

func TestSuggestTopicKeyInfersFamilyFromTextWhenTypeIsGeneric(t *testing.T) {
	bug := SuggestTopicKey("manual", "", "Fix regression in auth login flow")
	if bug != "bug/fix-regression-in-auth-login-flow" {
		t.Fatalf("expected bug family inference, got %q", bug)
	}

	arch := SuggestTopicKey("", "ADR: Split API gateway boundary", "")
	if arch != "architecture/adr-split-api-gateway-boundary" {
		t.Fatalf("expected architecture family inference, got %q", arch)
	}
}

func TestTopicKeyUpsertIsScopedByProjectAndScope(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSession("s1", "engram", "/tmp/engram"); err != nil {
		t.Fatalf("create session: %v", err)
	}

	baseID, err := s.AddObservation(AddObservationParams{
		SessionID: "s1",
		Type:      "architecture",
		Title:     "Auth model",
		Content:   "Initial architecture",
		Project:   "engram",
		Scope:     "project",
		TopicKey:  "architecture/auth-model",
	})
	if err != nil {
		t.Fatalf("add base observation: %v", err)
	}

	personalID, err := s.AddObservation(AddObservationParams{
		SessionID: "s1",
		Type:      "architecture",
		Title:     "Auth model",
		Content:   "Personal take",
		Project:   "engram",
		Scope:     "personal",
		TopicKey:  "architecture/auth-model",
	})
	if err != nil {
		t.Fatalf("add personal scoped observation: %v", err)
	}

	otherProjectID, err := s.AddObservation(AddObservationParams{
		SessionID: "s1",
		Type:      "architecture",
		Title:     "Auth model",
		Content:   "Other project",
		Project:   "another-project",
		Scope:     "project",
		TopicKey:  "architecture/auth-model",
	})
	if err != nil {
		t.Fatalf("add other project observation: %v", err)
	}

	if baseID == personalID || baseID == otherProjectID || personalID == otherProjectID {
		t.Fatalf("expected topic upsert boundaries by project+scope, got ids base=%d personal=%d other=%d", baseID, personalID, otherProjectID)
	}
}

func TestPromptProjectNullScan(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSession("s1", "engram", "/tmp/engram"); err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Manually insert a prompt with NULL project to simulate legacy data or external changes
	_, err := s.db.Exec(
		"INSERT INTO user_prompts (session_id, content, project) VALUES (?, ?, NULL)",
		"s1", "prompt with null project",
	)
	if err != nil {
		t.Fatalf("manual insert: %v", err)
	}

	// 1. Test RecentPrompts
	prompts, err := s.RecentPrompts("", 10)
	if err != nil {
		t.Fatalf("RecentPrompts failed with null project: %v", err)
	}
	if len(prompts) != 1 || prompts[0].Project != "" {
		t.Errorf("expected empty string for null project, got %q", prompts[0].Project)
	}

	// 2. Test SearchPrompts
	searchResult, err := s.SearchPrompts("null", "", 10)
	if err != nil {
		t.Fatalf("SearchPrompts failed with null project: %v", err)
	}
	if len(searchResult) != 1 || searchResult[0].Project != "" {
		t.Errorf("expected empty string for null project in search, got %q", searchResult[0].Project)
	}

	// 3. Test Export
	data, err := s.Export()
	if err != nil {
		t.Fatalf("Export failed with null project: %v", err)
	}
	found := false
	for _, p := range data.Prompts {
		if p.Content == "prompt with null project" {
			found = true
			if p.Project != "" {
				t.Errorf("expected empty string for null project in export, got %q", p.Project)
			}
		}
	}
	if !found {
		t.Error("exported prompts missing the test prompt")
	}
}

// ─── Passive Capture Tests ───────────────────────────────────────────────────

func TestExtractLearningsNumberedList(t *testing.T) {
	text := `Some preamble text here.

## Key Learnings:

1. bcrypt cost=12 is the right balance for our server performance
2. JWT refresh tokens need atomic rotation to prevent race conditions
3. Always validate the audience claim in JWT tokens before trusting them

## Next Steps
- something else
`
	learnings := ExtractLearnings(text)
	if len(learnings) != 3 {
		t.Fatalf("expected 3 learnings, got %d: %v", len(learnings), learnings)
	}
	if !strings.Contains(learnings[0], "bcrypt") {
		t.Fatalf("expected first learning about bcrypt, got %q", learnings[0])
	}
}

func TestExtractLearningsSpanishHeader(t *testing.T) {
	text := `## Aprendizajes Clave:

1. El costo de bcrypt=12 es el balance correcto para nuestro servidor
2. Los refresh tokens de JWT necesitan rotacion atomica
`
	learnings := ExtractLearnings(text)
	if len(learnings) != 2 {
		t.Fatalf("expected 2 learnings, got %d: %v", len(learnings), learnings)
	}
}

func TestExtractLearningsBulletList(t *testing.T) {
	text := `### Learnings:

- bcrypt cost=12 is the right balance for our server performance
- JWT refresh tokens need atomic rotation to prevent race conditions
`
	learnings := ExtractLearnings(text)
	if len(learnings) != 2 {
		t.Fatalf("expected 2 learnings, got %d: %v", len(learnings), learnings)
	}
}

func TestExtractLearningsIgnoresShortItems(t *testing.T) {
	text := `## Key Learnings:

1. too short
2. bcrypt cost=12 is the right balance for our server performance
3. also short
`
	learnings := ExtractLearnings(text)
	if len(learnings) != 1 {
		t.Fatalf("expected 1 learning (short ones filtered), got %d: %v", len(learnings), learnings)
	}
}

func TestExtractLearningsNoSection(t *testing.T) {
	text := `This is just regular text without any learning section headers.
It has multiple lines but no ## Key Learnings or similar.
`
	learnings := ExtractLearnings(text)
	if len(learnings) != 0 {
		t.Fatalf("expected 0 learnings, got %d: %v", len(learnings), learnings)
	}
}

func TestExtractLearningsSectionPresentButNoValidItems(t *testing.T) {
	text := `## Key Learnings:

1. short
2. tiny
`
	learnings := ExtractLearnings(text)
	if len(learnings) != 0 {
		t.Fatalf("expected 0 learnings when section has no valid items, got %d: %v", len(learnings), learnings)
	}
}

func TestExtractLearningsUsesLastSection(t *testing.T) {
	text := `## Key Learnings:

1. This is from the first section and should be ignored

Some other text here.

## Key Learnings:

1. This is from the last section and should be captured as the real one
`
	learnings := ExtractLearnings(text)
	if len(learnings) != 1 {
		t.Fatalf("expected 1 learning from last section, got %d: %v", len(learnings), learnings)
	}
	if !strings.Contains(learnings[0], "last section") {
		t.Fatalf("expected learning from last section, got %q", learnings[0])
	}
}

func TestExtractLearningsFallsBackWhenLastSectionHasNoValidItems(t *testing.T) {
	text := `## Key Learnings:

1. This is long enough and should be captured from the previous section

## Key Learnings:

1. short
2. tiny
`
	learnings := ExtractLearnings(text)
	if len(learnings) != 1 {
		t.Fatalf("expected fallback to previous valid section, got %d: %v", len(learnings), learnings)
	}
	if !strings.Contains(learnings[0], "previous section") {
		t.Fatalf("expected learning from previous section, got %q", learnings[0])
	}
}

func TestExtractLearningsCleansMarkdown(t *testing.T) {
	text := "## Key Learnings:\n\n1. **Use** `context.Context` in *all* handlers to support cancellation correctly\n"
	learnings := ExtractLearnings(text)
	if len(learnings) != 1 {
		t.Fatalf("expected 1 learning, got %d: %v", len(learnings), learnings)
	}
	if strings.Contains(learnings[0], "**") || strings.Contains(learnings[0], "`") || strings.Contains(learnings[0], "*") {
		t.Fatalf("expected markdown to be stripped, got %q", learnings[0])
	}
}

func TestPassiveCaptureStoresLearnings(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSession("s1", "engram", "/tmp/engram"); err != nil {
		t.Fatalf("create session: %v", err)
	}

	text := `## Key Learnings:

1. bcrypt cost=12 is the right balance for our server performance
2. JWT refresh tokens need atomic rotation to prevent race conditions
`
	result, err := s.PassiveCapture(PassiveCaptureParams{
		SessionID: "s1",
		Content:   text,
		Project:   "engram",
		Source:    "test",
	})
	if err != nil {
		t.Fatalf("passive capture: %v", err)
	}
	if result.Extracted != 2 {
		t.Fatalf("expected 2 extracted, got %d", result.Extracted)
	}
	if result.Saved != 2 {
		t.Fatalf("expected 2 saved, got %d", result.Saved)
	}

	obs, err := s.AllObservations("engram", "", 10)
	if err != nil {
		t.Fatalf("all observations: %v", err)
	}
	if len(obs) != 2 {
		t.Fatalf("expected 2 observations, got %d", len(obs))
	}
	for _, o := range obs {
		if o.Type != "passive" {
			t.Fatalf("expected type=passive, got %q", o.Type)
		}
	}
	if obs[0].ToolName == nil || *obs[0].ToolName != "test" {
		t.Fatalf("expected tool_name source to be stored as 'test', got %+v", obs[0].ToolName)
	}
}

func TestPassiveCaptureEmptyContent(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSession("s1", "engram", "/tmp/engram"); err != nil {
		t.Fatalf("create session: %v", err)
	}

	result, err := s.PassiveCapture(PassiveCaptureParams{
		SessionID: "s1",
		Content:   "",
		Project:   "engram",
		Source:    "test",
	})
	if err != nil {
		t.Fatalf("passive capture: %v", err)
	}
	if result.Extracted != 0 || result.Saved != 0 {
		t.Fatalf("expected 0 extracted and 0 saved, got %d/%d", result.Extracted, result.Saved)
	}
}

func TestPassiveCaptureDedupesAgainstExistingObservations(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSession("s1", "engram", "/tmp/engram"); err != nil {
		t.Fatalf("create session: %v", err)
	}

	// First: agent saves actively via mem_save
	_, err := s.AddObservation(AddObservationParams{
		SessionID: "s1",
		Type:      "decision",
		Title:     "bcrypt cost",
		Content:   "bcrypt cost=12 is the right balance for our server performance",
		Project:   "engram",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("add active observation: %v", err)
	}

	// Then: passive capture fires with overlapping content
	text := `## Key Learnings:

1. bcrypt cost=12 is the right balance for our server performance
2. JWT refresh tokens need atomic rotation to prevent race conditions
`
	result, err := s.PassiveCapture(PassiveCaptureParams{
		SessionID: "s1",
		Content:   text,
		Project:   "engram",
		Source:    "test",
	})
	if err != nil {
		t.Fatalf("passive capture: %v", err)
	}
	if result.Extracted != 2 {
		t.Fatalf("expected 2 extracted, got %d", result.Extracted)
	}
	if result.Saved != 1 {
		t.Fatalf("expected 1 saved (1 deduped), got %d", result.Saved)
	}
	if result.Duplicates != 1 {
		t.Fatalf("expected 1 duplicate, got %d", result.Duplicates)
	}
}

func TestPassiveCaptureReturnsErrorWhenSessionDoesNotExist(t *testing.T) {
	s := newTestStore(t)

	text := `## Key Learnings:

1. This learning is long enough to attempt insert and fail without session
`
	_, err := s.PassiveCapture(PassiveCaptureParams{
		SessionID: "missing-session",
		Content:   text,
		Project:   "engram",
		Source:    "test",
	})
	if err == nil {
		t.Fatalf("expected error when session does not exist")
	}
}

func TestStatsProjectsOrderedByMostRecentObservation(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSession("s1", "engram", "/tmp/engram"); err != nil {
		t.Fatalf("create session s1: %v", err)
	}
	if err := s.CreateSession("s2", "engram", "/tmp/engram"); err != nil {
		t.Fatalf("create session s2: %v", err)
	}

	_, err := s.db.Exec(
		`INSERT INTO observations (session_id, type, title, content, project, scope, normalized_hash, revision_count, duplicate_count, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, 1, 1, ?, ?),
		        (?, ?, ?, ?, ?, ?, ?, 1, 1, ?, ?)`,
		"s1", "note", "older", "older alpha", "alpha", "project", hashNormalized("older alpha"), "2026-02-01 10:00:00", "2026-02-01 10:00:00",
		"s2", "note", "newer", "newer beta", "beta", "project", hashNormalized("newer beta"), "2026-02-02 10:00:00", "2026-02-02 10:00:00",
	)
	if err != nil {
		t.Fatalf("insert observations: %v", err)
	}

	stats, err := s.Stats()
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if len(stats.Projects) < 2 {
		t.Fatalf("expected at least 2 projects, got %d", len(stats.Projects))
	}

	if stats.Projects[0] != "beta" || stats.Projects[1] != "alpha" {
		t.Fatalf("expected recency order [beta alpha], got %v", stats.Projects[:2])
	}
}

func TestSessionsOrderedByMostRecentActivity(t *testing.T) {
	s := newTestStore(t)

	_, err := s.db.Exec(
		`INSERT INTO sessions (id, project, directory, started_at) VALUES
		 (?, ?, ?, ?),
		 (?, ?, ?, ?)`,
		"s-older", "engram", "/tmp/engram", "2026-02-01 09:00:00",
		"s-newer", "engram", "/tmp/engram", "2026-02-02 09:00:00",
	)
	if err != nil {
		t.Fatalf("insert sessions: %v", err)
	}

	_, err = s.db.Exec(
		`INSERT INTO observations (session_id, type, title, content, project, scope, normalized_hash, revision_count, duplicate_count, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, 1, 1, ?, ?)`,
		"s-older", "note", "latest", "session old got new activity", "engram", "project", hashNormalized("session old got new activity"), "2026-02-03 09:00:00", "2026-02-03 09:00:00",
	)
	if err != nil {
		t.Fatalf("insert latest observation: %v", err)
	}

	all, err := s.AllSessions("", 10)
	if err != nil {
		t.Fatalf("all sessions: %v", err)
	}
	if len(all) < 2 {
		t.Fatalf("expected at least 2 sessions, got %d", len(all))
	}
	if all[0].ID != "s-older" {
		t.Fatalf("expected s-older first in all sessions, got %s", all[0].ID)
	}

	recent, err := s.RecentSessions("", 10)
	if err != nil {
		t.Fatalf("recent sessions: %v", err)
	}
	if len(recent) < 2 {
		t.Fatalf("expected at least 2 recent sessions, got %d", len(recent))
	}
	if recent[0].ID != "s-older" {
		t.Fatalf("expected s-older first in recent sessions, got %s", recent[0].ID)
	}
}

func TestSessionObservationsAddPromptImportAndSyncChunks(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSession("s1", "engram", "/tmp/engram"); err != nil {
		t.Fatalf("create session: %v", err)
	}

	_, err := s.AddObservation(AddObservationParams{
		SessionID: "s1",
		Type:      "decision",
		Title:     "Auth",
		Content:   "Use middleware chain",
		Project:   "engram",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("add observation: %v", err)
	}

	longPrompt := strings.Repeat("x", s.cfg.MaxObservationLength+25)
	promptID, err := s.AddPrompt(AddPromptParams{SessionID: "s1", Content: longPrompt, Project: "engram"})
	if err != nil {
		t.Fatalf("add prompt: %v", err)
	}
	if promptID <= 0 {
		t.Fatalf("expected valid prompt id, got %d", promptID)
	}

	sessionObs, err := s.SessionObservations("s1", 0)
	if err != nil {
		t.Fatalf("session observations: %v", err)
	}
	if len(sessionObs) != 1 {
		t.Fatalf("expected 1 session observation, got %d", len(sessionObs))
	}

	exported, err := s.Export()
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	cfg := mustDefaultConfig(t)
	cfg.DataDir = t.TempDir()
	dst, err := New(cfg)
	if err != nil {
		t.Fatalf("new destination store: %v", err)
	}
	t.Cleanup(func() { _ = dst.Close() })

	imported, err := dst.Import(exported)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if imported.SessionsImported < 1 || imported.ObservationsImported < 1 || imported.PromptsImported < 1 {
		t.Fatalf("expected non-zero import counts, got %+v", imported)
	}

	if err := dst.RecordSyncedChunk("chunk-1"); err != nil {
		t.Fatalf("record synced chunk: %v", err)
	}
	chunks, err := dst.GetSyncedChunks()
	if err != nil {
		t.Fatalf("get synced chunks: %v", err)
	}
	if !chunks["chunk-1"] {
		t.Fatalf("expected chunk-1 to be marked as synced")
	}
}

func TestStoreLocalSyncFoundationEnqueuesCoreMutations(t *testing.T) {
	s := newTestStore(t)

	// Enroll "engram" so mutations are visible via ListPendingSyncMutations.
	if err := s.EnrollProject("engram"); err != nil {
		t.Fatalf("enroll: %v", err)
	}

	if err := s.CreateSession("sync-session", "engram", "/tmp/engram"); err != nil {
		t.Fatalf("create session: %v", err)
	}

	obsID, err := s.AddObservation(AddObservationParams{
		SessionID: "sync-session",
		Type:      "decision",
		Title:     "Initial title",
		Content:   "Initial content",
		Project:   "engram",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("add observation: %v", err)
	}

	updatedTitle := "Updated title"
	updatedContent := "Updated content"
	if _, err := s.UpdateObservation(obsID, UpdateObservationParams{
		Title:   &updatedTitle,
		Content: &updatedContent,
	}); err != nil {
		t.Fatalf("update observation: %v", err)
	}

	if err := s.DeleteObservation(obsID, false); err != nil {
		t.Fatalf("soft delete observation: %v", err)
	}

	promptID, err := s.AddPrompt(AddPromptParams{
		SessionID: "sync-session",
		Content:   "How do we keep this local-first?",
		Project:   "engram",
	})
	if err != nil {
		t.Fatalf("add prompt: %v", err)
	}

	if err := s.EndSession("sync-session", "done"); err != nil {
		t.Fatalf("end session: %v", err)
	}

	state, err := s.GetSyncState(DefaultSyncTargetKey)
	if err != nil {
		t.Fatalf("get sync state: %v", err)
	}
	if state.TargetKey != DefaultSyncTargetKey {
		t.Fatalf("expected target %q, got %q", DefaultSyncTargetKey, state.TargetKey)
	}
	if state.Lifecycle != SyncLifecyclePending {
		t.Fatalf("expected pending lifecycle after local writes, got %q", state.Lifecycle)
	}
	if state.LastEnqueuedSeq != 6 {
		t.Fatalf("expected 6 enqueued mutations, got %d", state.LastEnqueuedSeq)
	}

	mutations, err := s.ListPendingSyncMutations(DefaultSyncTargetKey, 10)
	if err != nil {
		t.Fatalf("list pending sync mutations: %v", err)
	}
	if len(mutations) != 6 {
		t.Fatalf("expected 6 pending mutations, got %d", len(mutations))
	}

	var observationSyncID string
	if err := s.db.QueryRow("SELECT sync_id FROM observations WHERE id = ?", obsID).Scan(&observationSyncID); err != nil {
		t.Fatalf("lookup observation sync id: %v", err)
	}
	if observationSyncID == "" {
		t.Fatalf("expected observation sync id to be persisted")
	}

	var promptSyncID string
	if err := s.db.QueryRow("SELECT sync_id FROM user_prompts WHERE id = ?", promptID).Scan(&promptSyncID); err != nil {
		t.Fatalf("lookup prompt sync id: %v", err)
	}
	if promptSyncID == "" {
		t.Fatalf("expected prompt sync id to be persisted")
	}

	if mutations[0].Entity != SyncEntitySession || mutations[0].EntityKey != "sync-session" || mutations[0].Op != SyncOpUpsert {
		t.Fatalf("unexpected session mutation: %+v", mutations[0])
	}
	if mutations[1].Entity != SyncEntityObservation || mutations[1].EntityKey != observationSyncID || mutations[1].Op != SyncOpUpsert {
		t.Fatalf("unexpected observation insert mutation: %+v", mutations[1])
	}
	if mutations[2].Entity != SyncEntityObservation || mutations[2].EntityKey != observationSyncID || mutations[2].Op != SyncOpUpsert {
		t.Fatalf("unexpected observation update mutation: %+v", mutations[2])
	}
	if mutations[3].Entity != SyncEntityObservation || mutations[3].EntityKey != observationSyncID || mutations[3].Op != SyncOpDelete {
		t.Fatalf("unexpected observation delete mutation: %+v", mutations[3])
	}
	if mutations[4].Entity != SyncEntityPrompt || mutations[4].EntityKey != promptSyncID || mutations[4].Op != SyncOpUpsert {
		t.Fatalf("unexpected prompt mutation: %+v", mutations[4])
	}
	if mutations[5].Entity != SyncEntitySession || mutations[5].EntityKey != "sync-session" || mutations[5].Op != SyncOpUpsert {
		t.Fatalf("unexpected end session mutation: %+v", mutations[5])
	}

	var deletedPayload map[string]any
	if err := json.Unmarshal([]byte(mutations[3].Payload), &deletedPayload); err != nil {
		t.Fatalf("decode delete payload: %v", err)
	}
	if deletedPayload["sync_id"] != observationSyncID {
		t.Fatalf("expected delete payload sync id %q, got %#v", observationSyncID, deletedPayload["sync_id"])
	}
	if deletedPayload["deleted"] != true {
		t.Fatalf("expected delete payload to mark deleted=true, got %#v", deletedPayload["deleted"])
	}

	if err := s.AckSyncMutations(DefaultSyncTargetKey, mutations[3].Seq); err != nil {
		t.Fatalf("ack sync mutations: %v", err)
	}
	remaining, err := s.ListPendingSyncMutations(DefaultSyncTargetKey, 10)
	if err != nil {
		t.Fatalf("list remaining sync mutations: %v", err)
	}
	if len(remaining) != 2 || remaining[0].Entity != SyncEntityPrompt || remaining[1].Entity != SyncEntitySession {
		t.Fatalf("expected prompt and end-session mutations to remain pending, got %+v", remaining)
	}
}

func TestStoreLocalSyncFoundationStateHelpers(t *testing.T) {
	s := newTestStore(t)

	state, err := s.GetSyncState(DefaultSyncTargetKey)
	if err != nil {
		t.Fatalf("get initial sync state: %v", err)
	}
	if state.Lifecycle != SyncLifecycleIdle {
		t.Fatalf("expected idle lifecycle, got %q", state.Lifecycle)
	}

	acquired, err := s.AcquireSyncLease(DefaultSyncTargetKey, "worker-a", 2*time.Minute, time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("acquire first lease: %v", err)
	}
	if !acquired {
		t.Fatalf("expected first lease acquisition to succeed")
	}

	acquired, err = s.AcquireSyncLease(DefaultSyncTargetKey, "worker-b", 2*time.Minute, time.Date(2026, 3, 7, 12, 1, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("acquire conflicting lease: %v", err)
	}
	if acquired {
		t.Fatalf("expected conflicting lease acquisition to fail")
	}

	if err := s.ReleaseSyncLease(DefaultSyncTargetKey, "worker-a"); err != nil {
		t.Fatalf("release lease: %v", err)
	}

	acquired, err = s.AcquireSyncLease(DefaultSyncTargetKey, "worker-b", 2*time.Minute, time.Date(2026, 3, 7, 12, 2, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("acquire released lease: %v", err)
	}
	if !acquired {
		t.Fatalf("expected lease acquisition after release to succeed")
	}

	if err := s.MarkSyncFailure(DefaultSyncTargetKey, "timeout talking to cloud", time.Date(2026, 3, 7, 12, 10, 0, 0, time.UTC)); err != nil {
		t.Fatalf("mark sync failure: %v", err)
	}

	state, err = s.GetSyncState(DefaultSyncTargetKey)
	if err != nil {
		t.Fatalf("get degraded sync state: %v", err)
	}
	if state.Lifecycle != SyncLifecycleDegraded {
		t.Fatalf("expected degraded lifecycle, got %q", state.Lifecycle)
	}
	if state.ConsecutiveFailures != 1 {
		t.Fatalf("expected failure count 1, got %d", state.ConsecutiveFailures)
	}
	if state.LastError == nil || *state.LastError != "timeout talking to cloud" {
		t.Fatalf("expected last error to be stored, got %+v", state.LastError)
	}
	if state.BackoffUntil == nil || *state.BackoffUntil != "2026-03-07T12:10:00Z" {
		t.Fatalf("expected backoff timestamp to be stored, got %+v", state.BackoffUntil)
	}

	if err := s.MarkSyncHealthy(DefaultSyncTargetKey); err != nil {
		t.Fatalf("mark sync healthy: %v", err)
	}

	state, err = s.GetSyncState(DefaultSyncTargetKey)
	if err != nil {
		t.Fatalf("get healthy sync state: %v", err)
	}
	if state.Lifecycle != SyncLifecycleHealthy {
		t.Fatalf("expected healthy lifecycle, got %q", state.Lifecycle)
	}
	if state.ConsecutiveFailures != 0 || state.LastError != nil || state.BackoffUntil != nil {
		t.Fatalf("expected healthy state to clear failure metadata, got %+v", state)
	}
}

func TestApplyRemoteMutationIdempotent(t *testing.T) {
	s := newTestStore(t)

	create := SyncMutation{
		Seq:       41,
		TargetKey: DefaultSyncTargetKey,
		Entity:    SyncEntitySession,
		EntityKey: "remote-session",
		Op:        SyncOpUpsert,
		Payload:   `{"id":"remote-session","project":"engram","directory":"/remote"}`,
	}
	if err := s.ApplyPulledMutation(DefaultSyncTargetKey, create); err != nil {
		t.Fatalf("apply session mutation: %v", err)
	}
	if err := s.ApplyPulledMutation(DefaultSyncTargetKey, create); err != nil {
		t.Fatalf("reapply session mutation: %v", err)
	}

	obsMutation := SyncMutation{
		Seq:       42,
		TargetKey: DefaultSyncTargetKey,
		Entity:    SyncEntityObservation,
		EntityKey: "obs-remote-1",
		Op:        SyncOpUpsert,
		Payload:   `{"sync_id":"obs-remote-1","session_id":"remote-session","type":"decision","title":"Remote","content":"Pulled from cloud","project":"engram","scope":"project"}`,
	}
	if err := s.ApplyPulledMutation(DefaultSyncTargetKey, obsMutation); err != nil {
		t.Fatalf("apply observation mutation: %v", err)
	}
	if err := s.ApplyPulledMutation(DefaultSyncTargetKey, obsMutation); err != nil {
		t.Fatalf("reapply observation mutation: %v", err)
	}

	var rowCount int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM observations WHERE sync_id = ?", "obs-remote-1").Scan(&rowCount); err != nil {
		t.Fatalf("count remote observation rows: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("expected one remote observation row after idempotent upsert, got %d", rowCount)
	}

	deleteMutation := SyncMutation{
		Seq:       43,
		TargetKey: DefaultSyncTargetKey,
		Entity:    SyncEntityObservation,
		EntityKey: "obs-remote-1",
		Op:        SyncOpDelete,
		Payload:   `{"sync_id":"obs-remote-1","deleted":true}`,
	}
	if err := s.ApplyPulledMutation(DefaultSyncTargetKey, deleteMutation); err != nil {
		t.Fatalf("apply delete mutation: %v", err)
	}
	if err := s.ApplyPulledMutation(DefaultSyncTargetKey, deleteMutation); err != nil {
		t.Fatalf("reapply delete mutation: %v", err)
	}

	if _, err := s.GetObservationBySyncID("obs-remote-1"); err == nil {
		t.Fatalf("expected pulled delete to hide observation")
	}

	pending, err := s.ListPendingSyncMutations(DefaultSyncTargetKey, 10)
	if err != nil {
		t.Fatalf("list pending after pulled apply: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("expected pulled apply helpers to avoid local re-enqueue, got %+v", pending)
	}

	state, err := s.GetSyncState(DefaultSyncTargetKey)
	if err != nil {
		t.Fatalf("get sync state after pulled apply: %v", err)
	}
	if state.LastPulledSeq != 43 {
		t.Fatalf("expected last pulled seq 43, got %d", state.LastPulledSeq)
	}
}

func TestApplyPulledMutationAcceptsStringifiedSessionPayload(t *testing.T) {
	s := newTestStore(t)

	mutation := SyncMutation{
		Seq:       1,
		TargetKey: DefaultSyncTargetKey,
		Entity:    SyncEntitySession,
		EntityKey: "remote-session",
		Op:        SyncOpUpsert,
		Payload:   `"{\"id\":\"remote-session\",\"project\":\"engram\",\"directory\":\"/remote\"}"`,
	}
	if err := s.ApplyPulledMutation(DefaultSyncTargetKey, mutation); err != nil {
		t.Fatalf("apply stringified session mutation: %v", err)
	}

	session, err := s.GetSession("remote-session")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if session.Project != "engram" || session.Directory != "/remote" {
		t.Fatalf("unexpected session after pulled apply: %+v", session)
	}
}

func TestUtilityHelpersCoverage(t *testing.T) {
	if got := derefString(nil); got != "" {
		t.Fatalf("expected empty string for nil pointer, got %q", got)
	}
	v := "value"
	if got := derefString(&v); got != "value" {
		t.Fatalf("expected dereferenced value, got %q", got)
	}

	if got := maxInt(10, 5); got != 10 {
		t.Fatalf("expected maxInt(10,5)=10, got %d", got)
	}
	if got := maxInt(3, 7); got != 7 {
		t.Fatalf("expected maxInt(3,7)=7, got %d", got)
	}

	if got := dedupeWindowExpression(0); got != "-15 minutes" {
		t.Fatalf("expected default dedupe window, got %q", got)
	}
	if got := dedupeWindowExpression(20 * time.Second); got != "-1 minutes" {
		t.Fatalf("expected minimum 1 minute window, got %q", got)
	}

	cases := map[string]string{
		"write":   "file_change",
		"patch":   "file_change",
		"bash":    "command",
		"read":    "file_read",
		"glob":    "search",
		"unknown": "tool_use",
	}
	for in, want := range cases {
		if got := ClassifyTool(in); got != want {
			t.Fatalf("ClassifyTool(%q): expected %q, got %q", in, want, got)
		}
	}
}

func TestEndSessionAndTimelineDefaults(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSession("s-end", "engram", "/tmp/engram"); err != nil {
		t.Fatalf("create session: %v", err)
	}

	firstID, err := s.AddObservation(AddObservationParams{
		SessionID: "s-end",
		Type:      "note",
		Title:     "first",
		Content:   "first note",
		Project:   "engram",
	})
	if err != nil {
		t.Fatalf("add first observation: %v", err)
	}
	_, err = s.AddObservation(AddObservationParams{
		SessionID: "s-end",
		Type:      "note",
		Title:     "second",
		Content:   "second note",
		Project:   "engram",
	})
	if err != nil {
		t.Fatalf("add second observation: %v", err)
	}

	if err := s.EndSession("s-end", "finished session"); err != nil {
		t.Fatalf("end session: %v", err)
	}

	sess, err := s.GetSession("s-end")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if sess.EndedAt == nil {
		t.Fatalf("expected ended_at to be set")
	}
	if sess.Summary == nil || *sess.Summary != "finished session" {
		t.Fatalf("expected summary to be stored, got %+v", sess.Summary)
	}

	timeline, err := s.Timeline(firstID, 0, -1)
	if err != nil {
		t.Fatalf("timeline with default before/after: %v", err)
	}
	if timeline.SessionInfo == nil {
		t.Fatalf("expected session info in timeline")
	}
	if timeline.TotalInRange != 2 {
		t.Fatalf("expected total_in_range=2, got %d", timeline.TotalInRange)
	}
}

func TestInferTopicFamilyCoverage(t *testing.T) {
	cases := []struct {
		name    string
		typ     string
		title   string
		content string
		want    string
	}{
		{name: "type architecture", typ: "architecture", want: "architecture"},
		{name: "type bugfix", typ: "bugfix", want: "bug"},
		{name: "type decision", typ: "decision", want: "decision"},
		{name: "type pattern", typ: "pattern", want: "pattern"},
		{name: "type config", typ: "config", want: "config"},
		{name: "type discovery", typ: "discovery", want: "discovery"},
		{name: "type learning", typ: "learning", want: "learning"},
		{name: "type session summary", typ: "session_summary", want: "session"},
		{name: "text bug", title: "", content: "this caused a crash regression", want: "bug"},
		{name: "text architecture", title: "", content: "new boundary design", want: "architecture"},
		{name: "text decision", title: "", content: "we chose this tradeoff", want: "decision"},
		{name: "text pattern", title: "", content: "naming convention for handlers", want: "pattern"},
		{name: "text config", title: "", content: "docker env setup", want: "config"},
		{name: "text discovery", title: "", content: "root cause found", want: "discovery"},
		{name: "text learning", title: "", content: "key learning from this issue", want: "learning"},
		{name: "fallback type", typ: "Custom Type", want: "custom-type"},
		{name: "default topic", typ: "manual", title: "", content: "", want: "topic"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := inferTopicFamily(tc.typ, tc.title, tc.content)
			if got != tc.want {
				t.Fatalf("inferTopicFamily(%q,%q,%q): expected %q, got %q", tc.typ, tc.title, tc.content, tc.want, got)
			}
		})
	}
}

func TestStoreAdditionalQueryAndMutationBranches(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSession("s-q", "engram", "/tmp/engram"); err != nil {
		t.Fatalf("create session: %v", err)
	}

	longContent := strings.Repeat("x", s.cfg.MaxObservationLength+100)
	obsID, err := s.AddObservation(AddObservationParams{
		SessionID: "s-q",
		Type:      "note",
		Title:     "Private <private>secret</private> title",
		Content:   longContent + " <private>token</private>",
		Project:   "engram",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("add observation: %v", err)
	}
	obs, err := s.GetObservation(obsID)
	if err != nil {
		t.Fatalf("get observation: %v", err)
	}
	if !strings.Contains(obs.Title, "[REDACTED]") {
		t.Fatalf("expected private tags redacted in title, got %q", obs.Title)
	}
	if !strings.Contains(obs.Content, "... [truncated]") {
		t.Fatalf("expected truncated content marker, got %q", obs.Content)
	}

	newProject := ""
	newTopic := ""
	updated, err := s.UpdateObservation(obsID, UpdateObservationParams{Project: &newProject, TopicKey: &newTopic})
	if err != nil {
		t.Fatalf("update observation: %v", err)
	}
	if updated.Project != nil {
		t.Fatalf("expected nil project after empty update")
	}
	if updated.TopicKey != nil {
		t.Fatalf("expected nil topic key after empty update")
	}

	if _, err := s.AddPrompt(AddPromptParams{SessionID: "s-q", Content: "alpha prompt", Project: "alpha"}); err != nil {
		t.Fatalf("add alpha prompt: %v", err)
	}
	if _, err := s.AddPrompt(AddPromptParams{SessionID: "s-q", Content: "beta prompt", Project: "beta"}); err != nil {
		t.Fatalf("add beta prompt: %v", err)
	}

	recentPrompts, err := s.RecentPrompts("beta", 1)
	if err != nil {
		t.Fatalf("recent prompts with project filter: %v", err)
	}
	if len(recentPrompts) != 1 || recentPrompts[0].Project != "beta" {
		t.Fatalf("expected one beta prompt, got %+v", recentPrompts)
	}

	searchPrompts, err := s.SearchPrompts("prompt", "alpha", 0)
	if err != nil {
		t.Fatalf("search prompts with project filter/default limit: %v", err)
	}
	if len(searchPrompts) != 1 || searchPrompts[0].Project != "alpha" {
		t.Fatalf("expected one alpha prompt search result, got %+v", searchPrompts)
	}

	searchResults, err := s.Search("title", SearchOptions{Scope: "project", Limit: 9999})
	if err != nil {
		t.Fatalf("search with clamped limit: %v", err)
	}
	if len(searchResults) == 0 {
		t.Fatalf("expected search results")
	}

	ctx, err := s.FormatContext("", "project")
	if err != nil {
		t.Fatalf("format context: %v", err)
	}
	if !strings.Contains(ctx, "Recent User Prompts") {
		t.Fatalf("expected prompts section in context output")
	}
}

func TestStoreErrorBranchesWithClosedDatabase(t *testing.T) {
	s := newTestStore(t)

	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	if _, err := s.GetSession("missing"); err == nil {
		t.Fatalf("expected GetSession error when db is closed")
	}
	if _, err := s.AllSessions("", 1); err == nil {
		t.Fatalf("expected AllSessions error when db is closed")
	}
	if _, err := s.RecentSessions("", 1); err == nil {
		t.Fatalf("expected RecentSessions error when db is closed")
	}
	if _, err := s.SearchPrompts("x", "", 1); err == nil {
		t.Fatalf("expected SearchPrompts error when db is closed")
	}
	if _, err := s.Search("x", SearchOptions{}); err == nil {
		t.Fatalf("expected Search error when db is closed")
	}
	if _, err := s.Export(); err == nil {
		t.Fatalf("expected Export error when db is closed")
	}
	if _, err := s.Timeline(1, 1, 1); err == nil {
		t.Fatalf("expected Timeline error when db is closed")
	}
}

func TestEndSessionEdgeCases(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSession("s-edge", "engram", "/tmp/engram"); err != nil {
		t.Fatalf("create session: %v", err)
	}

	if err := s.EndSession("missing", "ignored"); err != nil {
		t.Fatalf("end missing session should be no-op: %v", err)
	}

	if err := s.EndSession("s-edge", ""); err != nil {
		t.Fatalf("end session with empty summary: %v", err)
	}

	sess, err := s.GetSession("s-edge")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if sess.EndedAt == nil {
		t.Fatalf("expected ended_at to be set")
	}
	if sess.Summary != nil {
		t.Fatalf("expected empty summary to persist as NULL, got %q", *sess.Summary)
	}
}

func TestTimelineHandlesMissingSessionRecord(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.db.Exec("PRAGMA foreign_keys = OFF"); err != nil {
		t.Fatalf("disable fk: %v", err)
	}
	defer func() {
		_, _ = s.db.Exec("PRAGMA foreign_keys = ON")
	}()

	res, err := s.db.Exec(
		`INSERT INTO observations (session_id, type, title, content, project, scope, normalized_hash, revision_count, duplicate_count, last_seen_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, 1, 1, datetime('now'), datetime('now'))`,
		"manual-save", "manual", "orphan", "orphan content", "engram", "project", hashNormalized("orphan content"),
	)
	if err != nil {
		t.Fatalf("insert orphan observation: %v", err)
	}
	obsID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}

	timeline, err := s.Timeline(obsID, 1, 1)
	if err != nil {
		t.Fatalf("timeline: %v", err)
	}
	if timeline.SessionInfo != nil {
		t.Fatalf("expected nil session info for missing session, got %+v", timeline.SessionInfo)
	}
	if timeline.TotalInRange != 1 {
		t.Fatalf("expected total in range=1, got %d", timeline.TotalInRange)
	}
}

func TestQueryObservationsScanError(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.queryObservations("SELECT 1"); err == nil {
		t.Fatalf("expected scan error for mismatched projection")
	}
}

func TestMigrationAndHelperEdgeBranches(t *testing.T) {
	t.Run("migrate is idempotent with existing triggers", func(t *testing.T) {
		s := newTestStore(t)
		if err := s.migrate(); err != nil {
			t.Fatalf("second migrate should succeed: %v", err)
		}
	})

	t.Run("legacy migrate skips table without id column", func(t *testing.T) {
		s := newTestStore(t)

		if _, err := s.db.Exec(`
			DROP TRIGGER IF EXISTS obs_fts_insert;
			DROP TRIGGER IF EXISTS obs_fts_update;
			DROP TRIGGER IF EXISTS obs_fts_delete;
			DROP TABLE IF EXISTS observations_fts;
			DROP TABLE observations;
			CREATE TABLE observations (
				session_id TEXT,
				type TEXT,
				title TEXT,
				content TEXT
			);
		`); err != nil {
			t.Fatalf("recreate observations without id: %v", err)
		}

		if err := s.migrateLegacyObservationsTable(); err != nil {
			t.Fatalf("legacy migrate should skip tables without id: %v", err)
		}
	})

	t.Run("topic helpers normalize edge cases", func(t *testing.T) {
		if got := SuggestTopicKey("decision", "decision", ""); got != "decision/general" {
			t.Fatalf("expected decision/general, got %q", got)
		}
		if got := SuggestTopicKey("bugfix", "bug-auth-panic", ""); got != "bug/auth-panic" {
			t.Fatalf("expected bug/auth-panic, got %q", got)
		}
		if got := SuggestTopicKey("manual", "!!!", "..."); got != "topic/general" {
			t.Fatalf("expected topic/general fallback, got %q", got)
		}

		longSegment := normalizeTopicSegment(strings.Repeat("abc", 50))
		if len(longSegment) != 100 {
			t.Fatalf("expected topic segment truncation to 100, got %d", len(longSegment))
		}

		longKey := normalizeTopicKey(strings.Repeat("k", 200))
		if len(longKey) != 120 {
			t.Fatalf("expected topic key truncation to 120, got %d", len(longKey))
		}
	})

	t.Run("format context empty returns empty string", func(t *testing.T) {
		s := newTestStore(t)
		ctx, err := s.FormatContext("", "")
		if err != nil {
			t.Fatalf("format context: %v", err)
		}
		if ctx != "" {
			t.Fatalf("expected empty context when no data, got %q", ctx)
		}
	})
}

func TestExportImportEdgeBranches(t *testing.T) {
	t.Run("export fails when observations query fails", func(t *testing.T) {
		s := newTestStore(t)

		if _, err := s.db.Exec(`
			DROP TRIGGER IF EXISTS obs_fts_insert;
			DROP TRIGGER IF EXISTS obs_fts_update;
			DROP TRIGGER IF EXISTS obs_fts_delete;
			DROP TABLE IF EXISTS observations_fts;
			DROP TABLE observations;
		`); err != nil {
			t.Fatalf("drop observations: %v", err)
		}

		_, err := s.Export()
		if err == nil || !strings.Contains(err.Error(), "export observations") {
			t.Fatalf("expected observations export error, got %v", err)
		}
	})

	t.Run("export fails when prompts query fails", func(t *testing.T) {
		s := newTestStore(t)

		if _, err := s.db.Exec(`
			DROP TRIGGER IF EXISTS prompt_fts_insert;
			DROP TRIGGER IF EXISTS prompt_fts_update;
			DROP TRIGGER IF EXISTS prompt_fts_delete;
			DROP TABLE IF EXISTS prompts_fts;
			DROP TABLE user_prompts;
		`); err != nil {
			t.Fatalf("drop prompts: %v", err)
		}

		_, err := s.Export()
		if err == nil || !strings.Contains(err.Error(), "export prompts") {
			t.Fatalf("expected prompts export error, got %v", err)
		}
	})

	t.Run("import begin tx fails on closed db", func(t *testing.T) {
		s := newTestStore(t)
		if err := s.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}

		_, err := s.Import(&ExportData{})
		if err == nil || !strings.Contains(err.Error(), "begin tx") {
			t.Fatalf("expected begin tx import error, got %v", err)
		}
	})

	t.Run("import fails on observation fk error", func(t *testing.T) {
		s := newTestStore(t)
		_, err := s.Import(&ExportData{
			Observations: []Observation{{
				ID:        1,
				SessionID: "missing-session",
				Type:      "bugfix",
				Title:     "x",
				Content:   "y",
				Scope:     "project",
				CreatedAt: Now(),
				UpdatedAt: Now(),
			}},
		})
		if err == nil || !strings.Contains(err.Error(), "import observation") {
			t.Fatalf("expected observation import error, got %v", err)
		}
	})

	t.Run("import fails on prompt fk error", func(t *testing.T) {
		s := newTestStore(t)
		_, err := s.Import(&ExportData{
			Prompts: []Prompt{{
				ID:        1,
				SessionID: "missing-session",
				Content:   "prompt",
				Project:   "engram",
				CreatedAt: Now(),
			}},
		})
		if err == nil || !strings.Contains(err.Error(), "import prompt") {
			t.Fatalf("expected prompt import error, got %v", err)
		}
	})
}

func TestNewErrorBranches(t *testing.T) {
	t.Run("fails when data dir is a file", func(t *testing.T) {
		base := t.TempDir()
		badPath := filepath.Join(base, "not-a-dir")
		if err := os.WriteFile(badPath, []byte("x"), 0600); err != nil {
			t.Fatalf("write file: %v", err)
		}

		cfg := mustDefaultConfig(t)
		cfg.DataDir = badPath

		_, err := New(cfg)
		if err == nil || !strings.Contains(err.Error(), "create data dir") {
			t.Fatalf("expected create data dir error, got %v", err)
		}
	})

	t.Run("fails when db path is a directory", func(t *testing.T) {
		dataDir := t.TempDir()
		dbAsDir := filepath.Join(dataDir, "lore.db")
		if err := os.Mkdir(dbAsDir, 0755); err != nil {
			t.Fatalf("mkdir db path: %v", err)
		}

		cfg := mustDefaultConfig(t)
		cfg.DataDir = dataDir

		_, err := New(cfg)
		if err == nil {
			t.Fatalf("expected New to fail when db path is a directory")
		}
	})

	t.Run("fails when migration encounters conflicting object", func(t *testing.T) {
		dataDir := t.TempDir()
		dbPath := filepath.Join(dataDir, "lore.db")

		db, err := sql.Open("sqlite", dbPath)
		if err != nil {
			t.Fatalf("open db: %v", err)
		}
		_, err = db.Exec(`
			CREATE TABLE sessions (
				id TEXT PRIMARY KEY,
				project TEXT NOT NULL,
				directory TEXT NOT NULL,
				started_at TEXT NOT NULL,
				ended_at TEXT,
				summary TEXT
			);
			CREATE TABLE user_prompts (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				session_id TEXT NOT NULL,
				content TEXT NOT NULL,
				created_at TEXT NOT NULL
			);
		`)
		if err != nil {
			_ = db.Close()
			t.Fatalf("create conflicting view: %v", err)
		}
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}

		cfg := mustDefaultConfig(t)
		cfg.DataDir = dataDir

		_, err = New(cfg)
		if err == nil || !strings.Contains(err.Error(), "migration") {
			t.Fatalf("expected migration error, got %v", err)
		}
	})
}

func TestMigrationInternalErrorAndNoopBranches(t *testing.T) {
	t.Run("addColumnIfNotExists adds then noops", func(t *testing.T) {
		s := newTestStore(t)
		if _, err := s.db.Exec(`CREATE TABLE extra_table (id INTEGER)`); err != nil {
			t.Fatalf("create extra table: %v", err)
		}

		if err := s.addColumnIfNotExists("extra_table", "name", "TEXT"); err != nil {
			t.Fatalf("add column: %v", err)
		}
		if err := s.addColumnIfNotExists("extra_table", "name", "TEXT"); err != nil {
			t.Fatalf("add existing column should noop: %v", err)
		}

		if err := s.addColumnIfNotExists("missing_table", "x", "TEXT"); err == nil {
			t.Fatalf("expected missing table error")
		}
	})

	t.Run("legacy migrate noops when id is primary key", func(t *testing.T) {
		s := newTestStore(t)
		if err := s.migrateLegacyObservationsTable(); err != nil {
			t.Fatalf("expected noop for modern schema: %v", err)
		}
	})

	t.Run("legacy migrate fails if temp table already exists", func(t *testing.T) {
		s := newTestStore(t)
		if _, err := s.db.Exec(`
			DROP TRIGGER IF EXISTS obs_fts_insert;
			DROP TRIGGER IF EXISTS obs_fts_update;
			DROP TRIGGER IF EXISTS obs_fts_delete;
			DROP TABLE IF EXISTS observations_fts;
			DROP TABLE observations;
			CREATE TABLE observations (
				id INT,
				session_id TEXT,
				type TEXT,
				title TEXT,
				content TEXT,
				created_at TEXT
			);
			CREATE TABLE observations_migrated (id INTEGER PRIMARY KEY);
		`); err != nil {
			t.Fatalf("prepare legacy schema: %v", err)
		}

		err := s.migrateLegacyObservationsTable()
		if err == nil || !strings.Contains(err.Error(), "create table") {
			t.Fatalf("expected create table error, got %v", err)
		}
	})

	t.Run("migrate returns deterministic exec hook errors", func(t *testing.T) {
		s := newTestStore(t)

		origExec := s.hooks.exec
		s.hooks.exec = func(db execer, query string, args ...any) (sql.Result, error) {
			if strings.Contains(query, "UPDATE observations SET scope = 'project'") {
				return nil, errors.New("forced migrate update failure")
			}
			return origExec(db, query, args...)
		}

		err := s.migrate()
		if err == nil || !strings.Contains(err.Error(), "forced migrate update failure") {
			t.Fatalf("expected forced migrate failure, got %v", err)
		}
	})

	t.Run("migrate fails when creating missing triggers", func(t *testing.T) {
		s := newTestStore(t)

		if _, err := s.db.Exec(`
			DROP TRIGGER IF EXISTS obs_fts_insert;
			DROP TRIGGER IF EXISTS obs_fts_update;
			DROP TRIGGER IF EXISTS obs_fts_delete;
		`); err != nil {
			t.Fatalf("drop obs triggers: %v", err)
		}

		origExec := s.hooks.exec
		s.hooks.exec = func(db execer, query string, args ...any) (sql.Result, error) {
			if strings.Contains(query, "CREATE TRIGGER obs_fts_insert") {
				return nil, errors.New("forced obs trigger failure")
			}
			return origExec(db, query, args...)
		}

		err := s.migrate()
		if err == nil || !strings.Contains(err.Error(), "forced obs trigger failure") {
			t.Fatalf("expected forced trigger failure, got %v", err)
		}
	})

	t.Run("legacy migrate surfaces begin and commit hook failures", func(t *testing.T) {
		prepareLegacyStore := func(t *testing.T) *Store {
			t.Helper()
			s := newTestStore(t)
			if _, err := s.db.Exec(`
				DROP TRIGGER IF EXISTS obs_fts_insert;
				DROP TRIGGER IF EXISTS obs_fts_update;
				DROP TRIGGER IF EXISTS obs_fts_delete;
				DROP TABLE IF EXISTS observations_fts;
				DROP TABLE observations;
				INSERT OR IGNORE INTO sessions (id, project, directory) VALUES ('s1', 'engram', '/tmp/engram');
				CREATE TABLE observations (
					id INT,
					session_id TEXT,
					type TEXT,
					title TEXT,
					content TEXT,
					tool_name TEXT,
					project TEXT,
					scope TEXT,
					topic_key TEXT,
					normalized_hash TEXT,
					revision_count INTEGER,
					duplicate_count INTEGER,
					last_seen_at TEXT,
					created_at TEXT,
					updated_at TEXT,
					deleted_at TEXT
				);
				INSERT INTO observations (id, session_id, type, title, content, project, created_at, updated_at)
				VALUES (1, 's1', 'bugfix', 'legacy', 'legacy row', 'engram', datetime('now'), datetime('now'));
			`); err != nil {
				t.Fatalf("prepare legacy table: %v", err)
			}
			return s
		}

		t.Run("begin tx", func(t *testing.T) {
			s := prepareLegacyStore(t)
			s.hooks.beginTx = func(_ *sql.DB) (*sql.Tx, error) {
				return nil, errors.New("forced begin failure")
			}

			err := s.migrateLegacyObservationsTable()
			if err == nil || !strings.Contains(err.Error(), "forced begin failure") {
				t.Fatalf("expected begin failure, got %v", err)
			}
		})

		t.Run("commit", func(t *testing.T) {
			s := prepareLegacyStore(t)
			s.hooks.commit = func(_ *sql.Tx) error {
				return errors.New("forced legacy commit failure")
			}

			err := s.migrateLegacyObservationsTable()
			if err == nil || !strings.Contains(err.Error(), "forced legacy commit failure") {
				t.Fatalf("expected commit failure, got %v", err)
			}
		})
	})
}

func TestImportExportSeamErrors(t *testing.T) {
	t.Run("export query hooks", func(t *testing.T) {
		s := newTestStore(t)

		origQueryIt := s.hooks.queryIt
		s.hooks.queryIt = func(db queryer, query string, args ...any) (rowScanner, error) {
			if strings.Contains(query, "FROM sessions") {
				return nil, errors.New("forced sessions export query error")
			}
			return origQueryIt(db, query, args...)
		}
		if _, err := s.Export(); err == nil || !strings.Contains(err.Error(), "export sessions") {
			t.Fatalf("expected sessions export error, got %v", err)
		}

		s.hooks.queryIt = func(db queryer, query string, args ...any) (rowScanner, error) {
			if strings.Contains(query, "FROM observations") {
				return nil, errors.New("forced observations export query error")
			}
			return origQueryIt(db, query, args...)
		}
		if _, err := s.Export(); err == nil || !strings.Contains(err.Error(), "export observations") {
			t.Fatalf("expected observations export error, got %v", err)
		}

		s.hooks.queryIt = func(db queryer, query string, args ...any) (rowScanner, error) {
			if strings.Contains(query, "FROM user_prompts") {
				return nil, errors.New("forced prompts export query error")
			}
			return origQueryIt(db, query, args...)
		}
		if _, err := s.Export(); err == nil || !strings.Contains(err.Error(), "export prompts") {
			t.Fatalf("expected prompts export error, got %v", err)
		}
	})

	t.Run("import tx and exec hooks", func(t *testing.T) {
		s := newTestStore(t)

		s.hooks.beginTx = func(_ *sql.DB) (*sql.Tx, error) {
			return nil, errors.New("forced import begin failure")
		}
		if _, err := s.Import(&ExportData{}); err == nil || !strings.Contains(err.Error(), "begin tx") {
			t.Fatalf("expected begin tx error, got %v", err)
		}

		s.hooks = defaultStoreHooks()
		origExec := s.hooks.exec
		s.hooks.exec = func(db execer, query string, args ...any) (sql.Result, error) {
			if strings.Contains(query, "INSERT OR IGNORE INTO sessions") {
				return nil, errors.New("forced import session insert failure")
			}
			return origExec(db, query, args...)
		}
		if _, err := s.Import(&ExportData{Sessions: []Session{{ID: "s-x", Project: "p", Directory: "/tmp", StartedAt: Now()}}}); err == nil || !strings.Contains(err.Error(), "import session") {
			t.Fatalf("expected session import error, got %v", err)
		}

		s.hooks = defaultStoreHooks()
		s.hooks.commit = func(_ *sql.Tx) error {
			return errors.New("forced import commit failure")
		}
		if _, err := s.Import(&ExportData{}); err == nil || !strings.Contains(err.Error(), "import: commit") {
			t.Fatalf("expected commit error, got %v", err)
		}
	})
}

func TestHookFallbacksAndAdditionalBranches(t *testing.T) {
	t.Run("hook fallbacks call default DB methods", func(t *testing.T) {
		s := newTestStore(t)
		s.hooks = storeHooks{}

		if _, err := s.execHook(s.db, "SELECT 1"); err != nil {
			t.Fatalf("exec hook fallback: %v", err)
		}
		rows, err := s.queryHook(s.db, "SELECT 1")
		if err != nil {
			t.Fatalf("query hook fallback: %v", err)
		}
		_ = rows.Close()

		iter, err := s.queryItHook(s.db, "SELECT 1")
		if err != nil {
			t.Fatalf("query iterator fallback: %v", err)
		}
		_ = iter.Close()

		tx, err := s.beginTxHook()
		if err != nil {
			t.Fatalf("begin tx hook fallback: %v", err)
		}
		if err := s.commitHook(tx); err != nil {
			t.Fatalf("commit hook fallback: %v", err)
		}

		s2 := newTestStore(t)
		rows2, err := s2.queryHook(s2.db, "SELECT 1")
		if err != nil {
			t.Fatalf("query hook default closure: %v", err)
		}
		_ = rows2.Close()

		s.hooks.query = func(db queryer, query string, args ...any) (*sql.Rows, error) {
			return nil, errors.New("forced query hook error")
		}
		s.hooks.queryIt = nil
		if _, err := s.queryItHook(s.db, "SELECT 1"); err == nil {
			t.Fatalf("expected queryItHook error through queryHook fallback")
		}
	})

	t.Run("sessions and observations filters with default limits", func(t *testing.T) {
		s := newTestStore(t)
		if err := s.CreateSession("s-p", "proj-a", "/tmp/proj-a"); err != nil {
			t.Fatalf("create session proj-a: %v", err)
		}
		if err := s.CreateSession("s-q", "proj-b", "/tmp/proj-b"); err != nil {
			t.Fatalf("create session proj-b: %v", err)
		}
		if _, err := s.AddObservation(AddObservationParams{SessionID: "s-p", Type: "note", Title: "a", Content: "a", Project: "proj-a", Scope: "project"}); err != nil {
			t.Fatalf("add observation proj-a: %v", err)
		}
		if _, err := s.AddObservation(AddObservationParams{SessionID: "s-q", Type: "note", Title: "b", Content: "b", Project: "proj-b", Scope: "project"}); err != nil {
			t.Fatalf("add observation proj-b: %v", err)
		}

		recent, err := s.RecentSessions("proj-a", 0)
		if err != nil {
			t.Fatalf("recent sessions filtered: %v", err)
		}
		if len(recent) != 1 || recent[0].Project != "proj-a" {
			t.Fatalf("expected one proj-a recent session, got %+v", recent)
		}

		all, err := s.AllSessions("proj-b", -1)
		if err != nil {
			t.Fatalf("all sessions filtered: %v", err)
		}
		if len(all) != 1 || all[0].Project != "proj-b" {
			t.Fatalf("expected one proj-b session, got %+v", all)
		}

		obs, err := s.AllObservations("proj-a", "project", 0)
		if err != nil {
			t.Fatalf("all observations defaults: %v", err)
		}
		if len(obs) != 1 || obs[0].SessionID != "s-p" {
			t.Fatalf("expected one proj-a observation, got %+v", obs)
		}

		sessionObs, err := s.SessionObservations("s-p", 0)
		if err != nil {
			t.Fatalf("session observations default limit: %v", err)
		}
		if len(sessionObs) != 1 {
			t.Fatalf("expected one session observation, got %d", len(sessionObs))
		}

		recentObs, err := s.RecentObservations("proj-a", "project", 0)
		if err != nil {
			t.Fatalf("recent observations default limit: %v", err)
		}
		if len(recentObs) != 1 {
			t.Fatalf("expected one recent observation, got %d", len(recentObs))
		}

		recentPrompts, err := s.RecentPrompts("", 0)
		if err != nil {
			t.Fatalf("recent prompts default limit: %v", err)
		}
		if len(recentPrompts) != 0 {
			t.Fatalf("expected zero prompts, got %d", len(recentPrompts))
		}
	})

	t.Run("timeline includes before and after in chronological order", func(t *testing.T) {
		s := newTestStore(t)
		if err := s.CreateSession("s-tl", "engram", "/tmp/engram"); err != nil {
			t.Fatalf("create session: %v", err)
		}

		firstID, err := s.AddObservation(AddObservationParams{SessionID: "s-tl", Type: "note", Title: "1", Content: "one", Project: "engram"})
		if err != nil {
			t.Fatalf("add first observation: %v", err)
		}
		middleID, err := s.AddObservation(AddObservationParams{SessionID: "s-tl", Type: "note", Title: "2", Content: "two", Project: "engram"})
		if err != nil {
			t.Fatalf("add middle observation: %v", err)
		}
		lastID, err := s.AddObservation(AddObservationParams{SessionID: "s-tl", Type: "note", Title: "3", Content: "three", Project: "engram"})
		if err != nil {
			t.Fatalf("add last observation: %v", err)
		}

		tl, err := s.Timeline(middleID, 5, 5)
		if err != nil {
			t.Fatalf("timeline middle: %v", err)
		}
		if len(tl.Before) != 1 || tl.Before[0].ID != firstID {
			t.Fatalf("expected first in before list, got %+v", tl.Before)
		}
		if len(tl.After) != 1 || tl.After[0].ID != lastID {
			t.Fatalf("expected last in after list, got %+v", tl.After)
		}
	})

	t.Run("format context returns specific query stage errors", func(t *testing.T) {
		t.Run("recent sessions error", func(t *testing.T) {
			s := newTestStore(t)
			_ = s.Close()
			if _, err := s.FormatContext("", ""); err == nil {
				t.Fatalf("expected format context to fail from recent sessions")
			}
		})

		t.Run("recent observations error", func(t *testing.T) {
			s := newTestStore(t)
			if err := s.CreateSession("s-ctx", "engram", "/tmp/engram"); err != nil {
				t.Fatalf("create session: %v", err)
			}
			if _, err := s.db.Exec("DROP TABLE observations"); err != nil {
				t.Fatalf("drop observations: %v", err)
			}
			if _, err := s.FormatContext("", ""); err == nil {
				t.Fatalf("expected format context to fail from recent observations")
			}
		})

		t.Run("recent prompts error", func(t *testing.T) {
			s := newTestStore(t)
			if err := s.CreateSession("s-ctx2", "engram", "/tmp/engram"); err != nil {
				t.Fatalf("create session: %v", err)
			}
			if _, err := s.db.Exec("DROP TABLE user_prompts"); err != nil {
				t.Fatalf("drop prompts: %v", err)
			}
			if _, err := s.FormatContext("", ""); err == nil {
				t.Fatalf("expected format context to fail from recent prompts")
			}
		})
	})
}

func TestStoreUncoveredBranchesPushToHundred(t *testing.T) {
	t.Run("new open database hook error", func(t *testing.T) {
		orig := openDB
		t.Cleanup(func() { openDB = orig })
		openDB = func(driverName, dataSourceName string) (*sql.DB, error) {
			return nil, errors.New("forced open error")
		}

		cfg := mustDefaultConfig(t)
		cfg.DataDir = t.TempDir()
		if _, err := New(cfg); err == nil || !strings.Contains(err.Error(), "open database") {
			t.Fatalf("expected open database error, got %v", err)
		}
	})

	t.Run("migrate forced failures for remaining exec branches", func(t *testing.T) {
		failCases := []string{
			"CREATE INDEX IF NOT EXISTS idx_obs_scope",
			"UPDATE observations SET topic_key = NULL",
			"UPDATE observations SET revision_count = 1",
			"UPDATE observations SET duplicate_count = 1",
			"UPDATE observations SET updated_at = created_at",
			"UPDATE user_prompts SET project = ''",
			"CREATE TRIGGER prompt_fts_insert",
		}
		for _, needle := range failCases {
			t.Run(needle, func(t *testing.T) {
				s := newTestStore(t)
				if strings.Contains(needle, "CREATE TRIGGER prompt_fts_insert") {
					if _, err := s.db.Exec(`
						DROP TRIGGER IF EXISTS prompt_fts_insert;
						DROP TRIGGER IF EXISTS prompt_fts_update;
						DROP TRIGGER IF EXISTS prompt_fts_delete;
					`); err != nil {
						t.Fatalf("drop prompt triggers: %v", err)
					}
				}
				origExec := s.hooks.exec
				s.hooks.exec = func(db execer, query string, args ...any) (sql.Result, error) {
					if strings.Contains(query, needle) {
						return nil, errors.New("forced migrate failure")
					}
					return origExec(db, query, args...)
				}
				if err := s.migrate(); err == nil {
					t.Fatalf("expected migrate error for %q", needle)
				}
			})
		}
	})

	t.Run("migrate addColumn and legacy-call propagation", func(t *testing.T) {
		t.Run("propagates addColumn error", func(t *testing.T) {
			s := newTestStore(t)
			origQueryIt := s.hooks.queryIt
			called := 0
			s.hooks.queryIt = func(db queryer, query string, args ...any) (rowScanner, error) {
				if strings.Contains(query, "PRAGMA table_info(observations)") {
					called++
					if called == 1 {
						return nil, errors.New("forced addColumn failure")
					}
				}
				return origQueryIt(db, query, args...)
			}
			if err := s.migrate(); err == nil {
				t.Fatalf("expected migrate to propagate addColumn failure")
			}
		})

		t.Run("propagates legacy migrate error", func(t *testing.T) {
			s := newTestStore(t)
			origQueryIt := s.hooks.queryIt
			called := 0
			s.hooks.queryIt = func(db queryer, query string, args ...any) (rowScanner, error) {
				if strings.Contains(query, "PRAGMA table_info(observations)") {
					called++
					if called == 9 {
						return nil, errors.New("forced legacy call failure")
					}
				}
				return origQueryIt(db, query, args...)
			}
			if err := s.migrate(); err == nil {
				t.Fatalf("expected migrate to propagate legacy migrate failure")
			}
		})
	})

	t.Run("add observation, prompt, update forced errors", func(t *testing.T) {
		s := newTestStore(t)
		if err := s.CreateSession("s-e", "engram", "/tmp/engram"); err != nil {
			t.Fatalf("create session: %v", err)
		}

		if _, err := s.AddObservation(AddObservationParams{SessionID: "s-e", Type: "note", Title: "top", Content: "x", Project: "engram", TopicKey: "x"}); err != nil {
			t.Fatalf("seed topic observation: %v", err)
		}
		origExec := s.hooks.exec
		s.hooks.exec = func(db execer, query string, args ...any) (sql.Result, error) {
			if strings.Contains(query, "SET type = ?") {
				return nil, errors.New("forced topic update error")
			}
			return origExec(db, query, args...)
		}
		if _, err := s.AddObservation(AddObservationParams{SessionID: "s-e", Type: "note", Title: "top", Content: "x", Project: "engram", TopicKey: "x"}); err == nil {
			t.Fatalf("expected topic upsert exec error")
		}

		s.hooks = defaultStoreHooks()
		if _, err := s.AddObservation(AddObservationParams{SessionID: "s-e", Type: "note", Title: "dup", Content: "dup content", Project: "engram"}); err != nil {
			t.Fatalf("seed dedupe observation: %v", err)
		}
		origExec = s.hooks.exec
		s.hooks.exec = func(db execer, query string, args ...any) (sql.Result, error) {
			if strings.Contains(query, "SET duplicate_count = duplicate_count + 1") {
				return nil, errors.New("forced dedupe update error")
			}
			return origExec(db, query, args...)
		}
		if _, err := s.AddObservation(AddObservationParams{SessionID: "s-e", Type: "note", Title: "dup", Content: "dup content", Project: "engram"}); err == nil {
			t.Fatalf("expected dedupe exec error")
		}

		if err := s.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
		if _, err := s.AddObservation(AddObservationParams{SessionID: "s-e", Type: "note", Title: "x", Content: "y", Project: "engram", TopicKey: "t"}); err == nil {
			t.Fatalf("expected topic query error on closed db")
		}
		if _, err := s.AddObservation(AddObservationParams{SessionID: "s-e", Type: "note", Title: "x", Content: "y", Project: "engram"}); err == nil {
			t.Fatalf("expected dedupe query error on closed db")
		}
		if _, err := s.AddPrompt(AddPromptParams{SessionID: "s-e", Content: "x"}); err == nil {
			t.Fatalf("expected add prompt error on closed db")
		}
	})

	t.Run("update observation remaining branches", func(t *testing.T) {
		s := newTestStore(t)
		if err := s.CreateSession("s-u", "engram", "/tmp/engram"); err != nil {
			t.Fatalf("create session: %v", err)
		}
		id, err := s.AddObservation(AddObservationParams{SessionID: "s-u", Type: "old", Title: "t", Content: "c", Project: "engram", TopicKey: "topic/key"})
		if err != nil {
			t.Fatalf("seed observation: %v", err)
		}

		if _, err := s.UpdateObservation(999999, UpdateObservationParams{}); err == nil {
			t.Fatalf("expected update missing observation error")
		}

		newType := "new-type"
		longContent := strings.Repeat("z", s.cfg.MaxObservationLength+50)
		if _, err := s.UpdateObservation(id, UpdateObservationParams{Type: &newType, Content: &longContent}); err != nil {
			t.Fatalf("update with type+truncation: %v", err)
		}

		origExec := s.hooks.exec
		s.hooks.exec = func(db execer, query string, args ...any) (sql.Result, error) {
			if strings.Contains(query, "UPDATE observations") {
				return nil, errors.New("forced update exec error")
			}
			return origExec(db, query, args...)
		}
		if _, err := s.UpdateObservation(id, UpdateObservationParams{}); err == nil {
			t.Fatalf("expected update exec error")
		}
	})

	t.Run("query iterator scan and rows.Err branches", func(t *testing.T) {
		s := newTestStore(t)
		origQueryIt := s.hooks.queryIt

		setScanErr := func(match string) {
			s.hooks.queryIt = func(db queryer, query string, args ...any) (rowScanner, error) {
				if strings.Contains(query, match) {
					return &fakeRows{next: []bool{true, false}, scanErr: errors.New("forced scan error")}, nil
				}
				return origQueryIt(db, query, args...)
			}
		}

		setRowsErr := func(match string) {
			s.hooks.queryIt = func(db queryer, query string, args ...any) (rowScanner, error) {
				if strings.Contains(query, match) {
					return &fakeRows{next: []bool{false}, err: errors.New("forced rows err")}, nil
				}
				return origQueryIt(db, query, args...)
			}
		}

		if err := s.CreateSession("s-iter", "engram", "/tmp/engram"); err != nil {
			t.Fatalf("create session: %v", err)
		}
		if _, err := s.AddObservation(AddObservationParams{SessionID: "s-iter", Type: "note", Title: "one", Content: "one", Project: "engram"}); err != nil {
			t.Fatalf("add observation: %v", err)
		}
		if _, err := s.AddPrompt(AddPromptParams{SessionID: "s-iter", Content: "prompt", Project: "engram"}); err != nil {
			t.Fatalf("add prompt: %v", err)
		}

		setScanErr("FROM sessions s")
		if _, err := s.RecentSessions("", 10); err == nil {
			t.Fatalf("expected recent sessions scan error")
		}

		setScanErr("FROM sessions s")
		if _, err := s.AllSessions("", 10); err == nil {
			t.Fatalf("expected all sessions scan error")
		}

		setScanErr("FROM user_prompts")
		if _, err := s.RecentPrompts("", 10); err == nil {
			t.Fatalf("expected recent prompts scan error")
		}

		setScanErr("FROM prompts_fts")
		if _, err := s.SearchPrompts("prompt", "", 10); err == nil {
			t.Fatalf("expected search prompts scan error")
		}

		setScanErr("FROM observations_fts")
		if _, err := s.Search("one", SearchOptions{}); err == nil {
			t.Fatalf("expected search scan error")
		}

		setRowsErr("FROM observations_fts")
		if _, err := s.Search("one", SearchOptions{}); err == nil {
			t.Fatalf("expected search rows err")
		}

		setScanErr("SELECT id, project, directory")
		if _, err := s.Export(); err == nil {
			t.Fatalf("expected export sessions scan error")
		}

		setRowsErr("SELECT id, project, directory")
		if _, err := s.Export(); err == nil {
			t.Fatalf("expected export sessions rows err")
		}

		setScanErr("FROM observations ORDER BY id")
		if _, err := s.Export(); err == nil {
			t.Fatalf("expected export observations scan error")
		}

		setRowsErr("FROM observations ORDER BY id")
		if _, err := s.Export(); err == nil {
			t.Fatalf("expected export observations rows err")
		}

		setScanErr("FROM user_prompts ORDER BY id")
		if _, err := s.Export(); err == nil {
			t.Fatalf("expected export prompts scan error")
		}

		setRowsErr("FROM user_prompts ORDER BY id")
		if _, err := s.Export(); err == nil {
			t.Fatalf("expected export prompts rows err")
		}

		setScanErr("FROM sync_chunks")
		if _, err := s.GetSyncedChunks(); err == nil {
			t.Fatalf("expected synced chunks scan error")
		}

		setRowsErr("PRAGMA table_info(extra_table)")
		if _, err := s.db.Exec(`CREATE TABLE extra_table (id INTEGER)`); err != nil {
			t.Fatalf("create extra table: %v", err)
		}
		if err := s.addColumnIfNotExists("extra_table", "n", "TEXT"); err == nil {
			t.Fatalf("expected add column rows err")
		}

		setScanErr("PRAGMA table_info(extra_table)")
		if err := s.addColumnIfNotExists("extra_table", "n2", "TEXT"); err == nil {
			t.Fatalf("expected add column scan error")
		}

		setRowsErr("PRAGMA table_info(observations)")
		if err := s.migrateLegacyObservationsTable(); err == nil {
			t.Fatalf("expected legacy migrate pragma rows err")
		}

		setScanErr("PRAGMA table_info(observations)")
		if err := s.migrateLegacyObservationsTable(); err == nil {
			t.Fatalf("expected legacy migrate pragma scan error")
		}

		s.hooks.queryIt = origQueryIt
	})

	t.Run("timeline and search type filter branches", func(t *testing.T) {
		s := newTestStore(t)
		if err := s.CreateSession("s-t2", "engram", "/tmp/engram"); err != nil {
			t.Fatalf("create session: %v", err)
		}
		first, _ := s.AddObservation(AddObservationParams{SessionID: "s-t2", Type: "decision", Title: "a", Content: "a", Project: "engram"})
		_, _ = s.AddObservation(AddObservationParams{SessionID: "s-t2", Type: "decision", Title: "aa", Content: "aa", Project: "engram"})
		focus, _ := s.AddObservation(AddObservationParams{SessionID: "s-t2", Type: "decision", Title: "b", Content: "b", Project: "engram"})
		_, _ = s.AddObservation(AddObservationParams{SessionID: "s-t2", Type: "decision", Title: "c", Content: "c", Project: "engram"})

		if _, err := s.Search("b", SearchOptions{Type: "decision", Project: "engram", Scope: "project", Limit: 5}); err != nil {
			t.Fatalf("search with type filter: %v", err)
		}

		origQueryIt := s.hooks.queryIt
		s.hooks.queryIt = func(db queryer, query string, args ...any) (rowScanner, error) {
			if strings.Contains(query, "id < ?") {
				return nil, errors.New("forced before query error")
			}
			return origQueryIt(db, query, args...)
		}
		if _, err := s.Timeline(focus, 2, 2); err == nil {
			t.Fatalf("expected timeline before query error")
		}

		s.hooks.queryIt = func(db queryer, query string, args ...any) (rowScanner, error) {
			if strings.Contains(query, "id < ?") {
				return &fakeRows{next: []bool{true, false}, scanErr: errors.New("forced before scan error")}, nil
			}
			return origQueryIt(db, query, args...)
		}
		if _, err := s.Timeline(focus, 2, 2); err == nil {
			t.Fatalf("expected timeline before scan error")
		}

		s.hooks.queryIt = func(db queryer, query string, args ...any) (rowScanner, error) {
			if strings.Contains(query, "id < ?") {
				return &fakeRows{next: []bool{false}, err: errors.New("forced before rows err")}, nil
			}
			return origQueryIt(db, query, args...)
		}
		if _, err := s.Timeline(focus, 2, 2); err == nil {
			t.Fatalf("expected timeline before rows err")
		}

		s.hooks.queryIt = func(db queryer, query string, args ...any) (rowScanner, error) {
			if strings.Contains(query, "id > ?") {
				return nil, errors.New("forced after query error")
			}
			return origQueryIt(db, query, args...)
		}
		if _, err := s.Timeline(focus, 2, 2); err == nil {
			t.Fatalf("expected timeline after query error")
		}

		s.hooks.queryIt = func(db queryer, query string, args ...any) (rowScanner, error) {
			if strings.Contains(query, "id > ?") {
				return &fakeRows{next: []bool{true, false}, scanErr: errors.New("forced after scan error")}, nil
			}
			return origQueryIt(db, query, args...)
		}
		if _, err := s.Timeline(focus, 2, 2); err == nil {
			t.Fatalf("expected timeline after scan error")
		}

		s.hooks.queryIt = func(db queryer, query string, args ...any) (rowScanner, error) {
			if strings.Contains(query, "id > ?") {
				return &fakeRows{next: []bool{false}, err: errors.New("forced after rows err")}, nil
			}
			return origQueryIt(db, query, args...)
		}
		if _, err := s.Timeline(focus, 2, 2); err == nil {
			t.Fatalf("expected timeline after rows err")
		}

		s.hooks.queryIt = origQueryIt
		tl, err := s.Timeline(first, 5, 5)
		if err != nil {
			t.Fatalf("timeline reverse branch run: %v", err)
		}
		if len(tl.After) == 0 {
			t.Fatalf("expected timeline after entries")
		}
	})

	t.Run("format context and stats remaining branches", func(t *testing.T) {
		s := newTestStore(t)
		if err := s.CreateSession("s-c", "engram", "/tmp/engram"); err != nil {
			t.Fatalf("create session: %v", err)
		}
		if _, err := s.AddObservation(AddObservationParams{SessionID: "s-c", Type: "note", Title: "n", Content: "n", Project: "engram"}); err != nil {
			t.Fatalf("add obs: %v", err)
		}

		origQueryIt := s.hooks.queryIt
		s.hooks.queryIt = func(db queryer, query string, args ...any) (rowScanner, error) {
			if strings.Contains(query, "FROM observations o") && strings.Contains(query, "WHERE o.deleted_at IS NULL") {
				return nil, errors.New("forced recent observations error")
			}
			return origQueryIt(db, query, args...)
		}
		if _, err := s.FormatContext("engram", "project"); err == nil {
			t.Fatalf("expected format context observations error")
		}

		s.hooks.queryIt = func(db queryer, query string, args ...any) (rowScanner, error) {
			if strings.Contains(query, "GROUP BY project") {
				return nil, errors.New("forced stats query error")
			}
			return origQueryIt(db, query, args...)
		}
		if _, err := s.Stats(); err != nil {
			t.Fatalf("stats should swallow project query errors: %v", err)
		}

		if err := s.EndSession("s-c", "has summary"); err != nil {
			t.Fatalf("end session: %v", err)
		}
		s.hooks.queryIt = origQueryIt
		ctx, err := s.FormatContext("engram", "project")
		if err != nil {
			t.Fatalf("format context with summary: %v", err)
		}
		if !strings.Contains(ctx, "has summary") {
			t.Fatalf("expected session summary included in context")
		}
	})

	t.Run("helper query errors and legacy migration late-stage failures", func(t *testing.T) {
		s := newTestStore(t)
		if err := s.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
		if _, err := s.GetSyncedChunks(); err == nil {
			t.Fatalf("expected synced chunks query error")
		}
		if _, err := s.queryObservations("SELECT id FROM observations"); err == nil {
			t.Fatalf("expected queryObservations query error")
		}
		if err := s.addColumnIfNotExists("observations", "x", "TEXT"); err == nil {
			t.Fatalf("expected addColumn query error")
		}
		if err := s.migrateLegacyObservationsTable(); err == nil {
			t.Fatalf("expected legacy migrate query error")
		}

		s2 := newTestStore(t)
		if _, err := s2.db.Exec(`
			DROP TRIGGER IF EXISTS obs_fts_insert;
			DROP TRIGGER IF EXISTS obs_fts_update;
			DROP TRIGGER IF EXISTS obs_fts_delete;
			DROP TABLE IF EXISTS observations_fts;
			DROP TABLE observations;
			INSERT OR IGNORE INTO sessions (id, project, directory) VALUES ('s1', 'engram', '/tmp/engram');
			CREATE TABLE observations (
				id INT,
				session_id TEXT,
				type TEXT,
				title TEXT,
				content TEXT,
				tool_name TEXT,
				project TEXT,
				scope TEXT,
				topic_key TEXT,
				normalized_hash TEXT,
				revision_count INTEGER,
				duplicate_count INTEGER,
				last_seen_at TEXT,
				created_at TEXT,
				updated_at TEXT,
				deleted_at TEXT
			);
			INSERT INTO observations (id, session_id, type, title, content, project, created_at, updated_at)
			VALUES (1, 's1', 'bugfix', 'legacy', 'legacy row', 'engram', datetime('now'), datetime('now'));
		`); err != nil {
			t.Fatalf("prepare legacy table: %v", err)
		}

		lateFail := []string{"INSERT INTO observations_migrated", "DROP TABLE observations", "RENAME TO observations", "CREATE VIRTUAL TABLE observations_fts"}
		for _, needle := range lateFail {
			t.Run(needle, func(t *testing.T) {
				s3 := newTestStore(t)
				if _, err := s3.db.Exec(`
					DROP TRIGGER IF EXISTS obs_fts_insert;
					DROP TRIGGER IF EXISTS obs_fts_update;
					DROP TRIGGER IF EXISTS obs_fts_delete;
					DROP TABLE IF EXISTS observations_fts;
					DROP TABLE observations;
					INSERT OR IGNORE INTO sessions (id, project, directory) VALUES ('s1', 'engram', '/tmp/engram');
					CREATE TABLE observations (
						id INT,
						session_id TEXT,
						type TEXT,
						title TEXT,
						content TEXT,
						tool_name TEXT,
						project TEXT,
						scope TEXT,
						topic_key TEXT,
						normalized_hash TEXT,
						revision_count INTEGER,
						duplicate_count INTEGER,
						last_seen_at TEXT,
						created_at TEXT,
						updated_at TEXT,
						deleted_at TEXT
					);
					INSERT INTO observations (id, session_id, type, title, content, project, created_at, updated_at)
					VALUES (1, 's1', 'bugfix', 'legacy', 'legacy row', 'engram', datetime('now'), datetime('now'));
				`); err != nil {
					t.Fatalf("prepare legacy schema: %v", err)
				}

				origExec := s3.hooks.exec
				s3.hooks.exec = func(db execer, query string, args ...any) (sql.Result, error) {
					if strings.Contains(query, needle) {
						return nil, errors.New("forced legacy late failure")
					}
					return origExec(db, query, args...)
				}
				if err := s3.migrateLegacyObservationsTable(); err == nil {
					t.Fatalf("expected legacy migrate error for %q", needle)
				}
			})
		}
	})
}

// ─── Issue #25: Session collision regression tests ──────────────────────────

func TestCreateSessionUpsertsEmptyProjectAndDirectory(t *testing.T) {
	s := newTestStore(t)

	// Create session with empty project/directory (simulates first MCP call without context)
	if err := s.CreateSession("sess-upsert", "", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Second call with real project/directory should fill in the blanks.
	// Project names are normalized to lowercase, so "projectA" becomes "projecta".
	if err := s.CreateSession("sess-upsert", "projectA", "/tmp/a"); err != nil {
		t.Fatalf("upsert session: %v", err)
	}

	sess, err := s.GetSession("sess-upsert")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if sess.Project != "projecta" {
		t.Fatalf("expected project=projecta after upsert (normalized), got %q", sess.Project)
	}
	if sess.Directory != "/tmp/a" {
		t.Fatalf("expected directory=/tmp/a after upsert, got %q", sess.Directory)
	}
}

func TestCreateSessionDoesNotOverwriteExistingProject(t *testing.T) {
	s := newTestStore(t)

	// Create session with project A (normalized to "projecta")
	if err := s.CreateSession("sess-preserve", "projectA", "/tmp/a"); err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Second call with project B should NOT overwrite
	if err := s.CreateSession("sess-preserve", "projectB", "/tmp/b"); err != nil {
		t.Fatalf("upsert session: %v", err)
	}

	sess, err := s.GetSession("sess-preserve")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	// Project names are normalized to lowercase, so "projectA" is stored as "projecta"
	if sess.Project != "projecta" {
		t.Fatalf("expected project=projecta (preserved, normalized), got %q", sess.Project)
	}
	if sess.Directory != "/tmp/a" {
		t.Fatalf("expected directory=/tmp/a (preserved), got %q", sess.Directory)
	}
}

func TestCreateSessionPartialUpsert(t *testing.T) {
	s := newTestStore(t)

	t.Run("fills directory when project already set", func(t *testing.T) {
		if err := s.CreateSession("sess-partial-1", "myproject", ""); err != nil {
			t.Fatalf("create: %v", err)
		}
		// Second call fills directory but project stays
		if err := s.CreateSession("sess-partial-1", "other", "/new/dir"); err != nil {
			t.Fatalf("upsert: %v", err)
		}
		sess, err := s.GetSession("sess-partial-1")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if sess.Project != "myproject" {
			t.Fatalf("project should be preserved, got %q", sess.Project)
		}
		if sess.Directory != "/new/dir" {
			t.Fatalf("directory should be filled, got %q", sess.Directory)
		}
	})

	t.Run("fills project when directory already set", func(t *testing.T) {
		if err := s.CreateSession("sess-partial-2", "", "/existing/dir"); err != nil {
			t.Fatalf("create: %v", err)
		}
		if err := s.CreateSession("sess-partial-2", "newproject", ""); err != nil {
			t.Fatalf("upsert: %v", err)
		}
		sess, err := s.GetSession("sess-partial-2")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if sess.Project != "newproject" {
			t.Fatalf("project should be filled, got %q", sess.Project)
		}
		if sess.Directory != "/existing/dir" {
			t.Fatalf("directory should be preserved, got %q", sess.Directory)
		}
	})

	t.Run("both empty stays empty", func(t *testing.T) {
		if err := s.CreateSession("sess-partial-3", "", ""); err != nil {
			t.Fatalf("create: %v", err)
		}
		if err := s.CreateSession("sess-partial-3", "", ""); err != nil {
			t.Fatalf("upsert: %v", err)
		}
		sess, err := s.GetSession("sess-partial-3")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if sess.Project != "" {
			t.Fatalf("project should stay empty, got %q", sess.Project)
		}
		if sess.Directory != "" {
			t.Fatalf("directory should stay empty, got %q", sess.Directory)
		}
	})
}

func TestTruncateUTF8(t *testing.T) {
	tests := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{name: "short ascii", in: "abc", max: 10, want: "abc"},
		{name: "exact length", in: "hello", max: 5, want: "hello"},
		{name: "long ascii", in: "abcdef", max: 3, want: "abc..."},
		{name: "spanish accents", in: "Decisión de arquitectura", max: 8, want: "Decisión..."},
		{name: "emoji", in: "🐛🔧🚀✨🎉💡", max: 3, want: "🐛🔧🚀..."},
		{name: "mixed ascii and multibyte", in: "café☕latte", max: 5, want: "café☕..."},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := truncate(tc.in, tc.max)
			if got != tc.want {
				t.Fatalf("truncate(%q, %d) = %q, want %q", tc.in, tc.max, got, tc.want)
			}
		})
	}
}

// ─── Project Enrollment CRUD Tests ───────────────────────────────────────────

func TestEnrollProjectBasic(t *testing.T) {
	s := newTestStore(t)

	// Enroll a project.
	if err := s.EnrollProject("engram"); err != nil {
		t.Fatalf("enroll project: %v", err)
	}

	// Verify it shows up in the list.
	projects, err := s.ListEnrolledProjects()
	if err != nil {
		t.Fatalf("list enrolled projects: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("expected 1 enrolled project, got %d", len(projects))
	}
	if projects[0].Project != "engram" {
		t.Fatalf("expected project 'engram', got %q", projects[0].Project)
	}
	if projects[0].EnrolledAt == "" {
		t.Fatal("expected enrolled_at to be set")
	}

	// Verify IsProjectEnrolled returns true.
	enrolled, err := s.IsProjectEnrolled("engram")
	if err != nil {
		t.Fatalf("is project enrolled: %v", err)
	}
	if !enrolled {
		t.Fatal("expected project to be enrolled")
	}
}

func TestEnrollProjectIdempotent(t *testing.T) {
	s := newTestStore(t)

	// Enroll twice — should not error.
	if err := s.EnrollProject("engram"); err != nil {
		t.Fatalf("first enroll: %v", err)
	}
	if err := s.EnrollProject("engram"); err != nil {
		t.Fatalf("second enroll (idempotent): %v", err)
	}

	// Should still be exactly one row.
	projects, err := s.ListEnrolledProjects()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("expected 1 enrolled project after double-enroll, got %d", len(projects))
	}
}

func TestEnrollProjectBackfillsHistoricalMutations(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.db.Exec(
		`INSERT INTO sessions (id, project, directory, ended_at, summary) VALUES (?, ?, ?, datetime('now'), ?)`,
		"legacy-session", "legacy-proj", "/tmp/legacy", "done",
	); err != nil {
		t.Fatalf("insert session: %v", err)
	}

	if _, err := s.db.Exec(
		`INSERT INTO observations (sync_id, session_id, type, title, content, project, scope, normalized_hash, revision_count, duplicate_count, last_seen_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, 1, datetime('now'), datetime('now'))`,
		"obs-legacy", "legacy-session", "decision", "Legacy obs", "Historical content", "legacy-proj", "project", hashNormalized("Historical content"),
	); err != nil {
		t.Fatalf("insert observation: %v", err)
	}

	if _, err := s.db.Exec(
		`INSERT INTO user_prompts (sync_id, session_id, content, project) VALUES (?, ?, ?, ?)`,
		"prompt-legacy", "legacy-session", "What happened before enterprise?", "legacy-proj",
	); err != nil {
		t.Fatalf("insert prompt: %v", err)
	}

	var before int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sync_mutations`).Scan(&before); err != nil {
		t.Fatalf("count mutations before enroll: %v", err)
	}
	if before != 0 {
		t.Fatalf("expected 0 sync mutations before enroll, got %d", before)
	}

	if err := s.EnrollProject("legacy-proj"); err != nil {
		t.Fatalf("enroll project: %v", err)
	}

	mutations, err := s.ListPendingSyncMutations(DefaultSyncTargetKey, 10)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(mutations) != 3 {
		t.Fatalf("expected 3 backfilled mutations, got %d", len(mutations))
	}

	expected := map[string]string{
		SyncEntitySession:     "legacy-session",
		SyncEntityObservation: "obs-legacy",
		SyncEntityPrompt:      "prompt-legacy",
	}
	for _, mutation := range mutations {
		entityKey, ok := expected[mutation.Entity]
		if !ok {
			t.Fatalf("unexpected mutation entity %q", mutation.Entity)
		}
		if mutation.EntityKey != entityKey {
			t.Fatalf("expected entity_key %q for %s, got %q", entityKey, mutation.Entity, mutation.EntityKey)
		}
		if mutation.Project != "legacy-proj" {
			t.Fatalf("expected project legacy-proj, got %q", mutation.Project)
		}
	}
	state, err := s.GetSyncState(DefaultSyncTargetKey)
	if err != nil {
		t.Fatalf("get sync state: %v", err)
	}
	if state.LastEnqueuedSeq != 3 {
		t.Fatalf("expected last_enqueued_seq 3 after backfill, got %d", state.LastEnqueuedSeq)
	}
}

func TestEnrollProjectBackfillIsIdempotentAndSkipsExistingMutations(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.db.Exec(
		`INSERT INTO sessions (id, project, directory) VALUES (?, ?, ?)`,
		"legacy-session", "legacy-proj", "/tmp/legacy",
	); err != nil {
		t.Fatalf("insert session: %v", err)
	}

	if _, err := s.db.Exec(
		`INSERT INTO observations (sync_id, session_id, type, title, content, project, scope, normalized_hash, revision_count, duplicate_count, last_seen_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, 1, datetime('now'), datetime('now'))`,
		"obs-legacy", "legacy-session", "decision", "Legacy obs", "Historical content", "legacy-proj", "project", hashNormalized("Historical content"),
	); err != nil {
		t.Fatalf("insert observation: %v", err)
	}

	if _, err := s.db.Exec(
		`INSERT INTO user_prompts (sync_id, session_id, content, project) VALUES (?, ?, ?, ?)`,
		"prompt-legacy", "legacy-session", "Historical prompt", "legacy-proj",
	); err != nil {
		t.Fatalf("insert prompt: %v", err)
	}

	if _, err := s.db.Exec(
		`INSERT INTO sync_mutations (target_key, entity, entity_key, op, payload, source, project)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		DefaultSyncTargetKey, SyncEntityObservation, "obs-legacy", SyncOpUpsert, `{"sync_id":"obs-legacy","session_id":"legacy-session","project":"legacy-proj"}`, SyncSourceLocal, "legacy-proj",
	); err != nil {
		t.Fatalf("insert existing mutation: %v", err)
	}

	if err := s.EnrollProject("legacy-proj"); err != nil {
		t.Fatalf("first enroll: %v", err)
	}

	var afterFirst int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sync_mutations`).Scan(&afterFirst); err != nil {
		t.Fatalf("count after first enroll: %v", err)
	}
	if afterFirst != 3 {
		t.Fatalf("expected 3 total mutations after first enroll, got %d", afterFirst)
	}

	var observationMutations int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sync_mutations WHERE entity = ? AND entity_key = ?`, SyncEntityObservation, "obs-legacy").Scan(&observationMutations); err != nil {
		t.Fatalf("count observation mutations: %v", err)
	}
	if observationMutations != 1 {
		t.Fatalf("expected existing observation mutation to remain single, got %d rows", observationMutations)
	}

	if err := s.EnrollProject("legacy-proj"); err != nil {
		t.Fatalf("second enroll: %v", err)
	}

	var afterSecond int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sync_mutations`).Scan(&afterSecond); err != nil {
		t.Fatalf("count after second enroll: %v", err)
	}
	if afterSecond != afterFirst {
		t.Fatalf("expected no duplicate backfill on re-enroll, got %d mutations after second enroll vs %d after first", afterSecond, afterFirst)
	}
}

func TestNewRepairsAlreadyEnrolledProjectsMissingHistoricalSyncMutations(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := filepath.Join(dataDir, "lore.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}

	obsHash := hashNormalized("Historical content")
	_, err = db.Exec(`
		CREATE TABLE sessions (
			id TEXT PRIMARY KEY,
			project TEXT NOT NULL,
			directory TEXT NOT NULL,
			started_at TEXT NOT NULL DEFAULT (datetime('now')),
			ended_at TEXT,
			summary TEXT
		);
		CREATE TABLE observations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			sync_id TEXT,
			session_id TEXT NOT NULL,
			type TEXT NOT NULL,
			title TEXT NOT NULL,
			content TEXT NOT NULL,
			tool_name TEXT,
			project TEXT,
			scope TEXT NOT NULL DEFAULT 'project',
			topic_key TEXT,
			normalized_hash TEXT,
			revision_count INTEGER NOT NULL DEFAULT 1,
			duplicate_count INTEGER NOT NULL DEFAULT 1,
			last_seen_at TEXT,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now')),
			deleted_at TEXT,
			FOREIGN KEY (session_id) REFERENCES sessions(id)
		);
		CREATE TABLE user_prompts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			sync_id TEXT,
			session_id TEXT NOT NULL,
			content TEXT NOT NULL,
			project TEXT,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			FOREIGN KEY (session_id) REFERENCES sessions(id)
		);
		CREATE TABLE sync_state (
			target_key TEXT PRIMARY KEY,
			lifecycle TEXT NOT NULL DEFAULT 'idle',
			last_enqueued_seq INTEGER NOT NULL DEFAULT 0,
			last_acked_seq INTEGER NOT NULL DEFAULT 0,
			last_pulled_seq INTEGER NOT NULL DEFAULT 0,
			consecutive_failures INTEGER NOT NULL DEFAULT 0,
			backoff_until TEXT,
			lease_owner TEXT,
			lease_until TEXT,
			last_error TEXT,
			updated_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		CREATE TABLE sync_mutations (
			seq INTEGER PRIMARY KEY AUTOINCREMENT,
			target_key TEXT NOT NULL,
			entity TEXT NOT NULL,
			entity_key TEXT NOT NULL,
			op TEXT NOT NULL,
			payload TEXT NOT NULL,
			source TEXT NOT NULL DEFAULT 'local',
			occurred_at TEXT NOT NULL DEFAULT (datetime('now')),
			acked_at TEXT,
			project TEXT NOT NULL DEFAULT '',
			FOREIGN KEY (target_key) REFERENCES sync_state(target_key)
		);
		CREATE TABLE sync_enrolled_projects (
			project TEXT PRIMARY KEY,
			enrolled_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		INSERT INTO sessions (id, project, directory, summary) VALUES ('legacy-session', 'legacy-proj', '/tmp/legacy', 'done');
		INSERT INTO observations (sync_id, session_id, type, title, content, project, scope, normalized_hash, revision_count, duplicate_count, last_seen_at, updated_at)
		VALUES ('obs-legacy', 'legacy-session', 'decision', 'Legacy obs', 'Historical content', 'legacy-proj', 'project', ?, 1, 1, datetime('now'), datetime('now'));
		INSERT INTO user_prompts (sync_id, session_id, content, project) VALUES ('prompt-legacy', 'legacy-session', 'Historical prompt', 'legacy-proj');
		INSERT INTO sync_state (target_key, lifecycle, updated_at) VALUES (?, 'idle', datetime('now'));
		INSERT INTO sync_enrolled_projects (project) VALUES ('legacy-proj');
	`, obsHash, DefaultSyncTargetKey)
	if err != nil {
		_ = db.Close()
		t.Fatalf("seed legacy db: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	cfg := mustDefaultConfig(t)
	cfg.DataDir = dataDir

	s, err := New(cfg)
	if err != nil {
		t.Fatalf("new store after enrolled legacy state: %v", err)
	}

	mutations, err := s.ListPendingSyncMutations(DefaultSyncTargetKey, 10)
	if err != nil {
		_ = s.Close()
		t.Fatalf("list pending after repair: %v", err)
	}
	if len(mutations) != 3 {
		_ = s.Close()
		t.Fatalf("expected 3 repaired mutations, got %d", len(mutations))
	}

	state, err := s.GetSyncState(DefaultSyncTargetKey)
	if err != nil {
		_ = s.Close()
		t.Fatalf("get sync state after repair: %v", err)
	}
	if state.LastEnqueuedSeq != 3 {
		_ = s.Close()
		t.Fatalf("expected last_enqueued_seq 3 after automatic repair, got %d", state.LastEnqueuedSeq)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("close repaired store: %v", err)
	}

	s, err = New(cfg)
	if err != nil {
		t.Fatalf("reopen repaired store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sync_mutations`).Scan(&count); err != nil {
		t.Fatalf("count repaired mutations after reopen: %v", err)
	}
	if count != 3 {
		t.Fatalf("expected repair to stay idempotent across reopen, got %d sync mutations", count)
	}
}

func TestEnrollProjectEmptyNameReturnsError(t *testing.T) {
	s := newTestStore(t)

	if err := s.EnrollProject(""); err == nil {
		t.Fatal("expected error when enrolling empty project name")
	}
}

func TestUnenrollProjectBasic(t *testing.T) {
	s := newTestStore(t)

	if err := s.EnrollProject("engram"); err != nil {
		t.Fatalf("enroll: %v", err)
	}

	// Unenroll.
	if err := s.UnenrollProject("engram"); err != nil {
		t.Fatalf("unenroll: %v", err)
	}

	// Should be gone.
	enrolled, err := s.IsProjectEnrolled("engram")
	if err != nil {
		t.Fatalf("is enrolled after unenroll: %v", err)
	}
	if enrolled {
		t.Fatal("expected project to be unenrolled")
	}

	projects, err := s.ListEnrolledProjects()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(projects) != 0 {
		t.Fatalf("expected 0 enrolled projects after unenroll, got %d", len(projects))
	}
}

func TestUnenrollProjectIdempotent(t *testing.T) {
	s := newTestStore(t)

	// Unenroll a project that was never enrolled — should not error.
	if err := s.UnenrollProject("nonexistent"); err != nil {
		t.Fatalf("unenroll non-enrolled project should be idempotent: %v", err)
	}
}

func TestUnenrollProjectEmptyNameReturnsError(t *testing.T) {
	s := newTestStore(t)

	if err := s.UnenrollProject(""); err == nil {
		t.Fatal("expected error when unenrolling empty project name")
	}
}

func TestIsProjectEnrolledReturnsFalseForUnknown(t *testing.T) {
	s := newTestStore(t)

	enrolled, err := s.IsProjectEnrolled("unknown-project")
	if err != nil {
		t.Fatalf("is enrolled: %v", err)
	}
	if enrolled {
		t.Fatal("expected false for unknown project")
	}
}

func TestListEnrolledProjectsEmpty(t *testing.T) {
	s := newTestStore(t)

	projects, err := s.ListEnrolledProjects()
	if err != nil {
		t.Fatalf("list enrolled projects: %v", err)
	}
	if projects != nil {
		t.Fatalf("expected nil for empty list, got %v", projects)
	}
}

func TestListEnrolledProjectsAlphabeticalOrder(t *testing.T) {
	s := newTestStore(t)

	// Enroll in non-alphabetical order.
	for _, p := range []string{"zebra", "alpha", "mango"} {
		if err := s.EnrollProject(p); err != nil {
			t.Fatalf("enroll %q: %v", p, err)
		}
	}

	projects, err := s.ListEnrolledProjects()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(projects) != 3 {
		t.Fatalf("expected 3 projects, got %d", len(projects))
	}
	expected := []string{"alpha", "mango", "zebra"}
	for i, ep := range projects {
		if ep.Project != expected[i] {
			t.Fatalf("position %d: expected %q, got %q", i, expected[i], ep.Project)
		}
	}
}

func TestSyncMutationProjectColumnExists(t *testing.T) {
	s := newTestStore(t)

	// Verify the project column exists on sync_mutations by inserting a row.
	_, err := s.db.Exec(
		`INSERT INTO sync_mutations (target_key, entity, entity_key, op, payload, source, project)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		DefaultSyncTargetKey, "session", "test-key", SyncOpUpsert, `{"project":"myproj"}`, SyncSourceLocal, "myproj",
	)
	if err != nil {
		t.Fatalf("insert sync_mutation with project: %v", err)
	}

	// Read it back and verify project is populated.
	var project string
	if err := s.db.QueryRow(`SELECT project FROM sync_mutations WHERE entity_key = ?`, "test-key").Scan(&project); err != nil {
		t.Fatalf("scan project: %v", err)
	}
	if project != "myproj" {
		t.Fatalf("expected project 'myproj', got %q", project)
	}
}

func TestSyncMutationProjectBackfill(t *testing.T) {
	s := newTestStore(t)

	// Insert a mutation that simulates a pre-migration row (project is empty, but payload has it).
	// The backfill runs during schema init, so we test it by inserting directly then re-running.
	// Since the store already ran migrations, let's verify backfill logic by inserting a new row
	// with empty project and manually running the backfill.
	_, err := s.db.Exec(
		`INSERT INTO sync_mutations (target_key, entity, entity_key, op, payload, source, project)
		 VALUES (?, ?, ?, ?, ?, ?, '')`,
		DefaultSyncTargetKey, "observation", "backfill-key", SyncOpUpsert, `{"project":"backfilled"}`, SyncSourceLocal,
	)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Run the backfill manually.
	_, err = s.db.Exec(`
		UPDATE sync_mutations
		SET project = COALESCE(json_extract(payload, '$.project'), '')
		WHERE project = '' AND payload != ''
	`)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}

	var project string
	if err := s.db.QueryRow(`SELECT project FROM sync_mutations WHERE entity_key = ?`, "backfill-key").Scan(&project); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if project != "backfilled" {
		t.Fatalf("expected backfilled project 'backfilled', got %q", project)
	}
}

func TestListPendingSyncMutationsIncludesProject(t *testing.T) {
	s := newTestStore(t)

	// Enroll the project so mutations are visible in ListPendingSyncMutations.
	if err := s.EnrollProject("my-project"); err != nil {
		t.Fatalf("enroll: %v", err)
	}

	if err := s.CreateSession("proj-session", "my-project", "/tmp"); err != nil {
		t.Fatalf("create session: %v", err)
	}

	_, err := s.AddObservation(AddObservationParams{
		SessionID: "proj-session",
		Type:      "decision",
		Title:     "Test obs",
		Content:   "Content",
		Project:   "my-project",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("add observation: %v", err)
	}

	mutations, err := s.ListPendingSyncMutations(DefaultSyncTargetKey, 10)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}

	// There should be mutations (session create + observation create at minimum).
	if len(mutations) == 0 {
		t.Fatal("expected at least one pending mutation")
	}

	// Phase 3: Verify the Project field is populated at enqueue time.
	foundProject := false
	for _, m := range mutations {
		if m.Project == "my-project" {
			foundProject = true
			break
		}
	}
	if !foundProject {
		t.Fatal("expected at least one mutation with project='my-project'")
	}
}

// ─── Phase 3: extractProjectFromPayload ──────────────────────────────────────

func TestExtractProjectFromSessionPayload(t *testing.T) {
	p := syncSessionPayload{ID: "s1", Project: "acme"}
	got := extractProjectFromPayload(p)
	if got != "acme" {
		t.Fatalf("expected 'acme', got %q", got)
	}
}

func TestExtractProjectFromObservationPayload(t *testing.T) {
	proj := "obs-project"
	p := syncObservationPayload{SyncID: "obs-1", Project: &proj}
	got := extractProjectFromPayload(p)
	if got != "obs-project" {
		t.Fatalf("expected 'obs-project', got %q", got)
	}
}

func TestExtractProjectFromObservationPayloadNil(t *testing.T) {
	p := syncObservationPayload{SyncID: "obs-1", Project: nil}
	got := extractProjectFromPayload(p)
	if got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

func TestExtractProjectFromPromptPayload(t *testing.T) {
	proj := "prompt-project"
	p := syncPromptPayload{SyncID: "p1", Project: &proj}
	got := extractProjectFromPayload(p)
	if got != "prompt-project" {
		t.Fatalf("expected 'prompt-project', got %q", got)
	}
}

func TestExtractProjectFromPromptPayloadNil(t *testing.T) {
	p := syncPromptPayload{SyncID: "p1", Project: nil}
	got := extractProjectFromPayload(p)
	if got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

func TestExtractProjectFromUnknownPayloadFallback(t *testing.T) {
	// Unknown struct with a project field — uses JSON fallback.
	p := struct {
		Project string `json:"project"`
		Other   string `json:"other"`
	}{Project: "fallback-proj", Other: "x"}
	got := extractProjectFromPayload(p)
	if got != "fallback-proj" {
		t.Fatalf("expected 'fallback-proj', got %q", got)
	}
}

func TestExtractProjectFromPayloadWithoutProjectField(t *testing.T) {
	// Unknown struct without a project field — returns empty.
	p := struct {
		Name string `json:"name"`
	}{Name: "test"}
	got := extractProjectFromPayload(p)
	if got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

// ─── Phase 3: enqueueSyncMutationTx populates project column ────────────────

func TestEnqueueSyncMutationPopulatesProjectFromSessionPayload(t *testing.T) {
	s := newTestStore(t)
	if err := s.CreateSession("enq-session", "enqueued-project", "/tmp"); err != nil {
		t.Fatalf("create session: %v", err)
	}

	// CreateSession enqueues a sync mutation internally. Check the project column.
	var project string
	err := s.db.QueryRow(
		`SELECT project FROM sync_mutations WHERE entity = ? AND entity_key = ?`,
		SyncEntitySession, "enq-session",
	).Scan(&project)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if project != "enqueued-project" {
		t.Fatalf("expected project='enqueued-project', got %q", project)
	}
}

func TestEnqueueSyncMutationPopulatesProjectFromObservationPayload(t *testing.T) {
	s := newTestStore(t)
	if err := s.CreateSession("obs-enq", "obs-proj", "/tmp"); err != nil {
		t.Fatalf("create session: %v", err)
	}

	_, err := s.AddObservation(AddObservationParams{
		SessionID: "obs-enq",
		Type:      "decision",
		Title:     "Test",
		Content:   "Content",
		Project:   "obs-proj",
	})
	if err != nil {
		t.Fatalf("add observation: %v", err)
	}

	// Check the observation mutation's project column.
	var project string
	err = s.db.QueryRow(
		`SELECT project FROM sync_mutations WHERE entity = ? ORDER BY seq DESC LIMIT 1`,
		SyncEntityObservation,
	).Scan(&project)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if project != "obs-proj" {
		t.Fatalf("expected project='obs-proj', got %q", project)
	}
}

func TestEnqueueSyncMutationPopulatesProjectFromPromptPayload(t *testing.T) {
	s := newTestStore(t)
	if err := s.CreateSession("prompt-enq", "prompt-proj", "/tmp"); err != nil {
		t.Fatalf("create session: %v", err)
	}

	_, err := s.AddPrompt(AddPromptParams{
		SessionID: "prompt-enq",
		Content:   "What did we do?",
		Project:   "prompt-proj",
	})
	if err != nil {
		t.Fatalf("add prompt: %v", err)
	}

	var project string
	err = s.db.QueryRow(
		`SELECT project FROM sync_mutations WHERE entity = ? ORDER BY seq DESC LIMIT 1`,
		SyncEntityPrompt,
	).Scan(&project)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if project != "prompt-proj" {
		t.Fatalf("expected project='prompt-proj', got %q", project)
	}
}

// ─── Phase 4: ListPendingSyncMutations enrollment filtering ──────────────────

func TestListPendingFiltersNonEnrolledProjects(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSession("s-enrolled", "enrolled-proj", "/tmp"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := s.CreateSession("s-not-enrolled", "other-proj", "/tmp"); err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Enroll only "enrolled-proj".
	if err := s.EnrollProject("enrolled-proj"); err != nil {
		t.Fatalf("enroll: %v", err)
	}

	mutations, err := s.ListPendingSyncMutations(DefaultSyncTargetKey, 100)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}

	// Only enrolled-proj mutations should appear.
	for _, m := range mutations {
		if m.Project == "other-proj" {
			t.Fatalf("non-enrolled project 'other-proj' should not appear in pending mutations")
		}
	}

	foundEnrolled := false
	for _, m := range mutations {
		if m.Project == "enrolled-proj" {
			foundEnrolled = true
			break
		}
	}
	if !foundEnrolled {
		t.Fatal("expected enrolled-proj mutations to appear")
	}
}

func TestListPendingReturnsNoMutationsWhenNoneEnrolled(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSession("s-no-enroll", "some-proj", "/tmp"); err != nil {
		t.Fatalf("create session: %v", err)
	}

	mutations, err := s.ListPendingSyncMutations(DefaultSyncTargetKey, 100)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}

	// No projects enrolled → no mutations (all have project != '').
	if len(mutations) != 0 {
		t.Fatalf("expected 0 mutations when no projects enrolled, got %d", len(mutations))
	}
}

// ─── Phase 4: SkipAckNonEnrolledMutations ────────────────────────────────────

func TestSkipAckNonEnrolledMutationsBasic(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSession("skip-session", "skip-proj", "/tmp"); err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Do NOT enroll "skip-proj" → mutations should be skip-acked.
	skipped, err := s.SkipAckNonEnrolledMutations(DefaultSyncTargetKey)
	if err != nil {
		t.Fatalf("skip-ack: %v", err)
	}
	if skipped == 0 {
		t.Fatal("expected at least one mutation to be skip-acked")
	}

	// After skip-ack, there should be no pending mutations left.
	mutations, err := s.ListPendingSyncMutations(DefaultSyncTargetKey, 100)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(mutations) != 0 {
		t.Fatalf("expected 0 pending mutations after skip-ack, got %d", len(mutations))
	}
}

func TestSkipAckPreservesEnrolledProjectMutations(t *testing.T) {
	s := newTestStore(t)

	if err := s.EnrollProject("enrolled"); err != nil {
		t.Fatalf("enroll: %v", err)
	}

	if err := s.CreateSession("s-enrolled", "enrolled", "/tmp"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := s.CreateSession("s-not-enrolled", "not-enrolled", "/tmp"); err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Count total pending before skip-ack.
	var totalBefore int
	s.db.QueryRow(`SELECT COUNT(*) FROM sync_mutations WHERE acked_at IS NULL`).Scan(&totalBefore)

	skipped, err := s.SkipAckNonEnrolledMutations(DefaultSyncTargetKey)
	if err != nil {
		t.Fatalf("skip-ack: %v", err)
	}
	if skipped == 0 {
		t.Fatal("expected at least one mutation to be skip-acked for 'not-enrolled'")
	}

	// Remaining pending should be only "enrolled" mutations.
	mutations, err := s.ListPendingSyncMutations(DefaultSyncTargetKey, 100)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	for _, m := range mutations {
		if m.Project == "not-enrolled" {
			t.Fatal("skip-acked mutation still appears as pending")
		}
	}
	if len(mutations) == 0 {
		t.Fatal("expected enrolled-project mutations to remain")
	}
}

// ─── Phase 5: Empty/global project always syncs ──────────────────────────────

func TestEmptyProjectMutationsAlwaysSync(t *testing.T) {
	s := newTestStore(t)

	// Create a session with empty project (global).
	if err := s.CreateSession("global-session", "", "/tmp"); err != nil {
		t.Fatalf("create session: %v", err)
	}

	// No projects enrolled, but empty-project mutations should still appear.
	mutations, err := s.ListPendingSyncMutations(DefaultSyncTargetKey, 100)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}

	if len(mutations) == 0 {
		t.Fatal("expected empty-project mutations to always sync regardless of enrollment")
	}

	// Verify they have project = ''.
	for _, m := range mutations {
		if m.Project != "" {
			t.Fatalf("expected empty project, got %q", m.Project)
		}
	}
}

func TestSkipAckDoesNotAffectEmptyProjectMutations(t *testing.T) {
	s := newTestStore(t)

	// Create a session with empty project (global).
	if err := s.CreateSession("global-session-2", "", "/tmp"); err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Count pending before skip-ack.
	beforeMutations, err := s.ListPendingSyncMutations(DefaultSyncTargetKey, 100)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	beforeCount := len(beforeMutations)

	// Skip-ack should not affect empty-project mutations.
	skipped, err := s.SkipAckNonEnrolledMutations(DefaultSyncTargetKey)
	if err != nil {
		t.Fatalf("skip-ack: %v", err)
	}
	if skipped != 0 {
		t.Fatalf("expected 0 mutations to be skip-acked (all empty project), got %d", skipped)
	}

	// Verify count unchanged.
	afterMutations, err := s.ListPendingSyncMutations(DefaultSyncTargetKey, 100)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(afterMutations) != beforeCount {
		t.Fatalf("expected %d mutations after skip-ack, got %d", beforeCount, len(afterMutations))
	}
}

func TestMixedEnrolledAndEmptyProjectMutations(t *testing.T) {
	s := newTestStore(t)

	if err := s.EnrollProject("enrolled-mix"); err != nil {
		t.Fatalf("enroll: %v", err)
	}

	// Create sessions with different project states.
	if err := s.CreateSession("mix-enrolled", "enrolled-mix", "/tmp"); err != nil {
		t.Fatalf("create enrolled session: %v", err)
	}
	if err := s.CreateSession("mix-global", "", "/tmp"); err != nil {
		t.Fatalf("create global session: %v", err)
	}
	if err := s.CreateSession("mix-unenrolled", "unenrolled-mix", "/tmp"); err != nil {
		t.Fatalf("create unenrolled session: %v", err)
	}

	mutations, err := s.ListPendingSyncMutations(DefaultSyncTargetKey, 100)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}

	// Should have enrolled-mix and empty-project mutations, but NOT unenrolled-mix.
	var hasEnrolled, hasGlobal bool
	for _, m := range mutations {
		if m.Project == "unenrolled-mix" {
			t.Fatal("unenrolled project mutations should not appear")
		}
		if m.Project == "enrolled-mix" {
			hasEnrolled = true
		}
		if m.Project == "" {
			hasGlobal = true
		}
	}
	if !hasEnrolled {
		t.Fatal("expected enrolled-mix mutations to appear")
	}
	if !hasGlobal {
		t.Fatal("expected empty-project (global) mutations to appear")
	}
}

// ─── MigrateProject ─────────────────────────────────────────────────────────

func TestMigrateProject(t *testing.T) {
	s := newTestStore(t)
	old, new_ := "old-name", "new-name"

	// Seed data under old project name
	s.CreateSession("s1", old, "/tmp/old")
	s.AddObservation(AddObservationParams{
		SessionID: "s1", Type: "decision", Title: "test obs",
		Content: "some content", Project: old, Scope: "project",
	})
	s.AddPrompt(AddPromptParams{SessionID: "s1", Content: "test prompt", Project: old})

	// Run migration
	result, err := s.MigrateProject(old, new_)
	if err != nil {
		t.Fatalf("MigrateProject: %v", err)
	}
	if !result.Migrated {
		t.Fatal("expected migration to happen")
	}
	if result.ObservationsUpdated != 1 {
		t.Fatalf("expected 1 observation migrated, got %d", result.ObservationsUpdated)
	}
	if result.SessionsUpdated != 1 {
		t.Fatalf("expected 1 session migrated, got %d", result.SessionsUpdated)
	}
	if result.PromptsUpdated != 1 {
		t.Fatalf("expected 1 prompt migrated, got %d", result.PromptsUpdated)
	}

	// Verify old project has no records
	obs, _ := s.RecentObservations(old, "", 10)
	if len(obs) != 0 {
		t.Fatalf("expected 0 observations under old name, got %d", len(obs))
	}

	// Verify new project has the records
	obs, _ = s.RecentObservations(new_, "", 10)
	if len(obs) != 1 {
		t.Fatalf("expected 1 observation under new name, got %d", len(obs))
	}

	// Verify FTS search finds it under new project
	results, _ := s.Search("test obs", SearchOptions{Project: new_, Limit: 10})
	if len(results) != 1 {
		t.Fatalf("expected FTS to find 1 result under new project, got %d", len(results))
	}
}

func TestMigrateProjectNoOp(t *testing.T) {
	s := newTestStore(t)

	// No records under "nonexistent" — should be a no-op
	result, err := s.MigrateProject("nonexistent", "anything")
	if err != nil {
		t.Fatalf("MigrateProject: %v", err)
	}
	if result.Migrated {
		t.Fatal("expected no migration for nonexistent project")
	}
}

func TestMigrateProjectIdempotent(t *testing.T) {
	s := newTestStore(t)
	old, new_ := "old-proj", "new-proj"

	s.CreateSession("s1", old, "/tmp")
	s.AddObservation(AddObservationParams{
		SessionID: "s1", Type: "decision", Title: "test",
		Content: "content", Project: old, Scope: "project",
	})

	// First migration
	r1, err := s.MigrateProject(old, new_)
	if err != nil {
		t.Fatalf("first MigrateProject: %v", err)
	}
	if !r1.Migrated {
		t.Fatal("first migration should migrate")
	}

	// Second migration — no records under old name anymore
	r2, err := s.MigrateProject(old, new_)
	if err != nil {
		t.Fatalf("second MigrateProject: %v", err)
	}
	if r2.Migrated {
		t.Fatal("second migration should be a no-op")
	}
}

// ─── Phase 2: project-name-drift — NormalizeProject, ListProjectNames,
//              ListProjectsWithStats, MergeProjects tests ─────────────────────

func TestNormalizeProjectFunction(t *testing.T) {
	tests := []struct {
		input       string
		wantName    string
		wantWarning bool
	}{
		{"engram", "engram", false},
		{"Engram", "engram", true},
		{"ENGRAM", "engram", true},
		{"  engram  ", "engram", true},
		{"Engram-Memory", "engram-memory", true},
		{"engram--memory", "engram-memory", true},
		{"engram__memory", "engram_memory", true},
		{"", "", false},
		{"already-lower", "already-lower", false},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, warning := NormalizeProject(tc.input)
			if got != tc.wantName {
				t.Errorf("NormalizeProject(%q) name = %q, want %q", tc.input, got, tc.wantName)
			}
			if tc.wantWarning && warning == "" {
				t.Errorf("NormalizeProject(%q) expected a warning, got empty string", tc.input)
			}
			if !tc.wantWarning && warning != "" {
				t.Errorf("NormalizeProject(%q) expected no warning, got %q", tc.input, warning)
			}
		})
	}
}

func TestAddObservationNormalizesProject(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSession("s1", "engram", "/tmp"); err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Save with mixed-case project name
	id, err := s.AddObservation(AddObservationParams{
		SessionID: "s1",
		Type:      "decision",
		Title:     "Normalize test",
		Content:   "This should be stored under lowercase project",
		Project:   "Engram",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}

	obs, err := s.GetObservation(id)
	if err != nil {
		t.Fatalf("GetObservation: %v", err)
	}

	// Stored project should be normalized to lowercase
	if obs.Project == nil || *obs.Project != "engram" {
		got := "<nil>"
		if obs.Project != nil {
			got = *obs.Project
		}
		t.Errorf("stored project = %q, want \"engram\"", got)
	}
}

func TestSearchNormalizesProjectFilter(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSession("s1", "engram", "/tmp"); err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Store observation under already-lowercase project
	_, err := s.AddObservation(AddObservationParams{
		SessionID: "s1",
		Type:      "decision",
		Title:     "Search normalize test",
		Content:   "content for project filter normalization",
		Project:   "engram",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}

	// Search with UPPERCASE project filter — should still find the record
	results, err := s.Search("normalize test", SearchOptions{
		Project: "Engram", // intentionally mixed-case
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(results) == 0 {
		t.Fatalf("expected ≥1 result when searching with normalized project filter, got 0")
	}
}

func TestSearchWithMetadataExactProjectHitSkipsFallback(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSession("s-exact", "lore", "/tmp"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := s.CreateSession("s-other", "lorex", "/tmp"); err != nil {
		t.Fatalf("create session other: %v", err)
	}

	_, err := s.AddObservation(AddObservationParams{
		SessionID: "s-exact",
		Type:      "decision",
		Title:     "Exact hit",
		Content:   "project fallback exact test keyword",
		Project:   "lore",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("add exact observation: %v", err)
	}

	_, err = s.AddObservation(AddObservationParams{
		SessionID: "s-other",
		Type:      "decision",
		Title:     "Other project hit",
		Content:   "project fallback exact test keyword",
		Project:   "lorex",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("add other observation: %v", err)
	}

	outcome, err := s.SearchWithMetadata("fallback exact", SearchOptions{Project: "lore", Limit: 10})
	if err != nil {
		t.Fatalf("SearchWithMetadata: %v", err)
	}

	if outcome.Metadata.FallbackUsed {
		t.Fatalf("expected fallback_used=false when exact hits exist")
	}
	if len(outcome.Metadata.FallbackProjects) != 0 {
		t.Fatalf("expected empty fallback projects on exact hit path, got %v", outcome.Metadata.FallbackProjects)
	}
	if len(outcome.Results) != 1 {
		t.Fatalf("expected 1 exact result, got %d", len(outcome.Results))
	}
	if outcome.Results[0].Project == nil || *outcome.Results[0].Project != "lore" {
		t.Fatalf("expected exact project lore result, got %+v", outcome.Results[0].Project)
	}
}

func TestSearchWithMetadataFallsBackWhenExactMisses(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSession("s-fallback", "lore-core", "/tmp"); err != nil {
		t.Fatalf("create session: %v", err)
	}

	_, err := s.AddObservation(AddObservationParams{
		SessionID: "s-fallback",
		Type:      "decision",
		Title:     "Fallback hit",
		Content:   "bounded fallback search hit",
		Project:   "lore-core",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("add fallback observation: %v", err)
	}

	outcome, err := s.SearchWithMetadata("bounded fallback", SearchOptions{Project: "lore-c0re", Limit: 10})
	if err != nil {
		t.Fatalf("SearchWithMetadata: %v", err)
	}

	if !outcome.Metadata.FallbackUsed {
		t.Fatalf("expected fallback_used=true when exact project misses and fallback candidates exist")
	}
	if len(outcome.Metadata.FallbackProjects) == 0 || outcome.Metadata.FallbackProjects[0] != "lore-core" {
		t.Fatalf("expected fallback projects to include lore-core, got %v", outcome.Metadata.FallbackProjects)
	}
	if len(outcome.Results) != 1 {
		t.Fatalf("expected 1 fallback result, got %d", len(outcome.Results))
	}
	if outcome.Results[0].Project == nil || *outcome.Results[0].Project != "lore-core" {
		t.Fatalf("expected fallback result from lore-core, got %+v", outcome.Results[0].Project)
	}
}

func TestSearchWithMetadataReturnsCandidateSelectionError(t *testing.T) {
	s := newTestStore(t)

	originalQueryIt := s.hooks.queryIt
	s.hooks.queryIt = func(db queryer, query string, args ...any) (rowScanner, error) {
		if strings.Contains(query, "SELECT DISTINCT project FROM observations") {
			return nil, errors.New("forced fallback candidate failure")
		}
		return originalQueryIt(db, query, args...)
	}

	_, err := s.SearchWithMetadata("missing fallback candidate", SearchOptions{Project: "lore-c0re", Limit: 10})
	if err == nil || !strings.Contains(err.Error(), "search fallback candidates: forced fallback candidate failure") {
		t.Fatalf("expected fallback candidate selection error, got %v", err)
	}
}

func TestSearchWithMetadataReturnsFallbackSearchError(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSession("s-fallback-error", "lore-core", "/tmp"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := s.AddObservation(AddObservationParams{
		SessionID: "s-fallback-error",
		Type:      "decision",
		Title:     "Fallback hit",
		Content:   "fallback error path hit",
		Project:   "lore-core",
		Scope:     "project",
	}); err != nil {
		t.Fatalf("seed fallback observation: %v", err)
	}

	originalQueryIt := s.hooks.queryIt
	ftsCalls := 0
	s.hooks.queryIt = func(db queryer, query string, args ...any) (rowScanner, error) {
		if strings.Contains(query, "FROM observations_fts") {
			ftsCalls++
			if ftsCalls == 2 {
				return nil, errors.New("forced fallback search failure")
			}
		}
		return originalQueryIt(db, query, args...)
	}

	_, err := s.SearchWithMetadata("fallback error path", SearchOptions{Project: "lore-c0re", Limit: 10})
	if err == nil || !strings.Contains(err.Error(), "forced fallback search failure") {
		t.Fatalf("expected fallback search error, got %v", err)
	}
}

func TestSearchWithMetadataFallbackHonorsLimit(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSession("s-fallback-limit", "lore-core", "/tmp"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := s.AddObservation(AddObservationParams{
			SessionID: "s-fallback-limit",
			Type:      "decision",
			Title:     fmt.Sprintf("Fallback hit %d", i),
			Content:   "fallback limit path hit",
			Project:   "lore-core",
			Scope:     "project",
		}); err != nil {
			t.Fatalf("seed fallback observation %d: %v", i, err)
		}
	}

	outcome, err := s.SearchWithMetadata("fallback limit path", SearchOptions{Project: "lore-c0re", Limit: 1})
	if err != nil {
		t.Fatalf("SearchWithMetadata: %v", err)
	}

	if !outcome.Metadata.FallbackUsed {
		t.Fatalf("expected fallback path to be used")
	}
	if len(outcome.Results) != 1 {
		t.Fatalf("expected fallback results to honor limit=1, got %d", len(outcome.Results))
	}
}

func TestSearchWithMetadataNoCandidatesKeepsEmpty(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSession("s-none", "alpha", "/tmp"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := s.AddObservation(AddObservationParams{
		SessionID: "s-none",
		Type:      "decision",
		Title:     "Unrelated",
		Content:   "query unrelated content",
		Project:   "alpha",
		Scope:     "project",
	}); err != nil {
		t.Fatalf("seed observation: %v", err)
	}

	outcome, err := s.SearchWithMetadata("nonexistent-query-term", SearchOptions{Project: "zzzzzzzz", Limit: 10})
	if err != nil {
		t.Fatalf("SearchWithMetadata: %v", err)
	}

	if outcome.Metadata.FallbackUsed {
		t.Fatalf("expected fallback_used=false when no fallback candidates are available")
	}
	if len(outcome.Metadata.FallbackProjects) != 0 {
		t.Fatalf("expected no fallback projects, got %v", outcome.Metadata.FallbackProjects)
	}
	if len(outcome.Results) != 0 {
		t.Fatalf("expected zero results, got %d", len(outcome.Results))
	}
}

func TestSelectFallbackProjectsAppliesCapAndFloor(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSession("s-cap", "abcde", "/tmp"); err != nil {
		t.Fatalf("create session abcde: %v", err)
	}
	if err := s.CreateSession("s-cap-2", "abcdf", "/tmp"); err != nil {
		t.Fatalf("create session abcdf: %v", err)
	}
	if err := s.CreateSession("s-cap-3", "abxde", "/tmp"); err != nil {
		t.Fatalf("create session abxde: %v", err)
	}
	if err := s.CreateSession("s-cap-4", "xbcde", "/tmp"); err != nil {
		t.Fatalf("create session xbcde: %v", err)
	}
	if err := s.CreateSession("s-floor", "abcde-super-long-project-name", "/tmp"); err != nil {
		t.Fatalf("create session floor candidate: %v", err)
	}

	for i, project := range []string{"abcde", "abcdf", "abxde", "xbcde", "abcde-super-long-project-name"} {
		if _, err := s.AddObservation(AddObservationParams{
			SessionID: "s-cap",
			Type:      "decision",
			Title:     fmt.Sprintf("seed %d", i),
			Content:   fmt.Sprintf("seed content %d", i),
			Project:   project,
			Scope:     "project",
		}); err != nil {
			t.Fatalf("seed observation for %s: %v", project, err)
		}
	}

	candidates, err := s.selectFallbackProjects("abcde")
	if err != nil {
		t.Fatalf("selectFallbackProjects: %v", err)
	}

	if len(candidates) > 3 {
		t.Fatalf("expected fallback candidates capped at 3, got %d (%v)", len(candidates), candidates)
	}
	for _, candidate := range candidates {
		if candidate == "abcde-super-long-project-name" {
			t.Fatalf("expected low-similarity floor candidate to be excluded, got %v", candidates)
		}
	}
}

func TestSelectFallbackProjectsHandlesEmptyProjectAndListErrors(t *testing.T) {
	s := newTestStore(t)

	candidates, err := s.selectFallbackProjects("")
	if err != nil {
		t.Fatalf("selectFallbackProjects empty project: %v", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("expected no candidates for empty project, got %v", candidates)
	}

	originalQueryIt := s.hooks.queryIt
	s.hooks.queryIt = func(db queryer, query string, args ...any) (rowScanner, error) {
		return nil, errors.New("forced project listing failure")
	}

	_, err = s.selectFallbackProjects("lore")
	if err == nil || !strings.Contains(err.Error(), "forced project listing failure") {
		t.Fatalf("expected project listing error, got %v", err)
	}

	s.hooks.queryIt = originalQueryIt
}

func TestSimilarityScoreFallbackBranches(t *testing.T) {
	if got := similarityScore("", projectpkg.ProjectMatch{Name: "", MatchType: "substring"}); got != 0 {
		t.Fatalf("expected zero score for empty inputs, got %v", got)
	}

	if got := similarityScore("Lore", projectpkg.ProjectMatch{Name: "lore", MatchType: "case-insensitive"}); got != 1 {
		t.Fatalf("expected case-insensitive match to score 1, got %v", got)
	}

	if got := similarityScore("abcd", projectpkg.ProjectMatch{Name: "ab", MatchType: "substring"}); got != 0.5 {
		t.Fatalf("expected shorter substring score 0.5, got %v", got)
	}

	if got := similarityScore("abcd", projectpkg.ProjectMatch{Name: "abce", MatchType: "unexpected"}); got != 0 {
		t.Fatalf("expected unknown match type to score 0, got %v", got)
	}
}

func TestRecentObservationsNormalizesProjectFilter(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSession("s1", "engram", "/tmp"); err != nil {
		t.Fatalf("create session: %v", err)
	}

	_, err := s.AddObservation(AddObservationParams{
		SessionID: "s1",
		Type:      "decision",
		Title:     "Recent obs test",
		Content:   "some content",
		Project:   "engram",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}

	// Query with uppercase project name
	obs, err := s.RecentObservations("ENGRAM", "", 10)
	if err != nil {
		t.Fatalf("RecentObservations: %v", err)
	}
	if len(obs) == 0 {
		t.Fatalf("expected ≥1 result with normalized project filter, got 0")
	}
}

func TestCreateSessionNormalizesProject(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSession("s-norm", "MyProject", "/tmp"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	sess, err := s.GetSession("s-norm")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.Project != "myproject" {
		t.Errorf("expected project=myproject (normalized), got %q", sess.Project)
	}
}

func TestListProjectNames(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSession("s1", "alpha", "/tmp"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := s.CreateSession("s2", "beta", "/tmp"); err != nil {
		t.Fatalf("create session: %v", err)
	}

	for _, proj := range []string{"alpha", "alpha", "beta", "gamma"} {
		_, err := s.AddObservation(AddObservationParams{
			SessionID: "s1",
			Type:      "decision",
			Title:     "test " + proj,
			Content:   "content for " + proj,
			Project:   proj,
			Scope:     "project",
		})
		if err != nil {
			t.Fatalf("AddObservation: %v", err)
		}
	}

	names, err := s.ListProjectNames()
	if err != nil {
		t.Fatalf("ListProjectNames: %v", err)
	}

	// Should return distinct names: alpha, beta, gamma
	want := map[string]bool{"alpha": true, "beta": true, "gamma": true}
	for _, n := range names {
		if !want[n] {
			t.Errorf("unexpected project name %q in results", n)
		}
		delete(want, n)
	}
	if len(want) > 0 {
		t.Errorf("missing project names: %v", want)
	}
}

func TestListProjectsWithStats(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSession("s1", "proj-a", "/work/a"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := s.CreateSession("s2", "proj-b", "/work/b"); err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Add 3 observations to proj-a
	for i := 0; i < 3; i++ {
		_, err := s.AddObservation(AddObservationParams{
			SessionID: "s1",
			Type:      "decision",
			Title:     "obs a",
			Content:   strings.Repeat("x", i+1), // unique content per obs
			Project:   "proj-a",
			Scope:     "project",
		})
		if err != nil {
			t.Fatalf("AddObservation proj-a: %v", err)
		}
	}

	// Add 1 observation to proj-b
	_, err := s.AddObservation(AddObservationParams{
		SessionID: "s2",
		Type:      "decision",
		Title:     "obs b",
		Content:   "content for proj-b",
		Project:   "proj-b",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("AddObservation proj-b: %v", err)
	}

	stats, err := s.ListProjectsWithStats()
	if err != nil {
		t.Fatalf("ListProjectsWithStats: %v", err)
	}

	if len(stats) < 2 {
		t.Fatalf("expected ≥2 project stats, got %d", len(stats))
	}

	// Find proj-a and proj-b in results
	statsMap := make(map[string]ProjectStats)
	for _, ps := range stats {
		statsMap[ps.Name] = ps
	}

	if a, ok := statsMap["proj-a"]; !ok {
		t.Error("proj-a not in ListProjectsWithStats results")
	} else {
		if a.ObservationCount != 3 {
			t.Errorf("proj-a: expected 3 observations, got %d", a.ObservationCount)
		}
		if a.SessionCount != 1 {
			t.Errorf("proj-a: expected 1 session, got %d", a.SessionCount)
		}
	}

	if b, ok := statsMap["proj-b"]; !ok {
		t.Error("proj-b not in ListProjectsWithStats results")
	} else {
		if b.ObservationCount != 1 {
			t.Errorf("proj-b: expected 1 observation, got %d", b.ObservationCount)
		}
	}

	// Results should be sorted by observation count descending
	if stats[0].Name != "proj-a" {
		t.Errorf("expected proj-a first (most observations), got %q", stats[0].Name)
	}
}

func TestMergeProjects(t *testing.T) {
	s := newTestStore(t)

	// Set up three source projects
	sources := []string{"engram", "Engram", "engram-memory"}
	canonical := "engram"

	if err := s.CreateSession("s1", "engram", "/work"); err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Add observations to each source
	for _, src := range []string{"engram", "engram-memory"} {
		for i := 0; i < 2; i++ {
			_, err := s.AddObservation(AddObservationParams{
				SessionID: "s1",
				Type:      "decision",
				Title:     "obs from " + src,
				Content:   strings.Repeat(src, i+1),
				Project:   src,
				Scope:     "project",
			})
			if err != nil {
				t.Fatalf("AddObservation %s: %v", src, err)
			}
		}
	}

	result, err := s.MergeProjects(sources, canonical)
	if err != nil {
		t.Fatalf("MergeProjects: %v", err)
	}

	if result.Canonical != "engram" {
		t.Errorf("canonical = %q, want \"engram\"", result.Canonical)
	}

	// "Engram" normalizes to "engram" (same as canonical) → skipped
	// "engram-memory" is different → merged
	// Only "engram-memory" should appear in SourcesMerged (and possibly "engram" if it had records,
	// but it equals canonical after normalization → skipped)
	for _, merged := range result.SourcesMerged {
		if merged == "engram" {
			t.Error("canonical 'engram' should not appear in SourcesMerged")
		}
	}

	// All records from engram-memory should now be under "engram"
	obs, err := s.RecentObservations("engram", "", 20)
	if err != nil {
		t.Fatalf("RecentObservations: %v", err)
	}
	if len(obs) < 4 {
		t.Errorf("expected ≥4 observations under 'engram' after merge, got %d", len(obs))
	}

	// engram-memory should have 0 observations
	obsMerged, err := s.RecentObservations("engram-memory", "", 10)
	if err != nil {
		t.Fatalf("RecentObservations engram-memory: %v", err)
	}
	if len(obsMerged) != 0 {
		t.Errorf("expected 0 observations under 'engram-memory' after merge, got %d", len(obsMerged))
	}
}

func TestMergeProjectsIdempotent(t *testing.T) {
	s := newTestStore(t)

	// Merge a nonexistent source — should not error
	result, err := s.MergeProjects([]string{"ghost-project"}, "engram")
	if err != nil {
		t.Fatalf("MergeProjects with nonexistent source: %v", err)
	}
	if result.ObservationsUpdated != 0 {
		t.Errorf("expected 0 observations updated for nonexistent source, got %d", result.ObservationsUpdated)
	}
}

func TestMergeProjectsCanonicalInSources(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSession("s1", "engram", "/work"); err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Put some obs under "engram"
	_, err := s.AddObservation(AddObservationParams{
		SessionID: "s1",
		Type:      "decision",
		Title:     "existing",
		Content:   "existing observation",
		Project:   "engram",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}

	// Sources include the canonical itself — should be silently skipped
	result, err := s.MergeProjects([]string{"engram", "Engram"}, "engram")
	if err != nil {
		t.Fatalf("MergeProjects: %v", err)
	}

	// Nothing should have been changed (engram and Engram both normalize to "engram" = canonical)
	if result.ObservationsUpdated != 0 {
		t.Errorf("expected 0 observations updated when sources equal canonical, got %d", result.ObservationsUpdated)
	}
	if len(result.SourcesMerged) != 0 {
		t.Errorf("expected empty SourcesMerged when all sources equal canonical, got %v", result.SourcesMerged)
	}
}

func TestCountObservationsForProject(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSession("s1", "alpha", "/work/alpha"); err != nil {
		t.Fatalf("create session: %v", err)
	}

	// No observations yet — count should be 0
	count, err := s.CountObservationsForProject("alpha")
	if err != nil {
		t.Fatalf("CountObservationsForProject: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0, got %d", count)
	}

	// Add two observations
	for i := 0; i < 2; i++ {
		if _, err := s.AddObservation(AddObservationParams{
			SessionID: "s1",
			Type:      "decision",
			Title:     "obs " + string(rune('A'+i)),
			Content:   "unique content that is definitely unique " + string(rune('A'+i)),
			Project:   "alpha",
			Scope:     "project",
		}); err != nil {
			t.Fatalf("AddObservation: %v", err)
		}
	}

	count, err = s.CountObservationsForProject("alpha")
	if err != nil {
		t.Fatalf("CountObservationsForProject: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2, got %d", count)
	}

	// Different project should return 0
	count, err = s.CountObservationsForProject("beta")
	if err != nil {
		t.Fatalf("CountObservationsForProject for beta: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 for beta, got %d", count)
	}
}

// ─── Skills Tests ─────────────────────────────────────────────────────────────

// Task 1.4 — assert skills, skill_versions, skills_fts, and 3 triggers exist after New()
func TestSkillsMigrationCreatesTablesAndTriggers(t *testing.T) {
	s := newTestStore(t)

	tables := []string{"skills", "skill_versions", "skills_fts"}
	for _, tbl := range tables {
		var name string
		err := s.db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type IN ('table','shadow') AND name = ?", tbl,
		).Scan(&name)
		if err != nil {
			t.Errorf("expected table %q to exist, got: %v", tbl, err)
		}
	}

	triggers := []string{"skills_fts_insert", "skills_fts_delete", "skills_fts_update"}
	for _, tr := range triggers {
		var name string
		err := s.db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='trigger' AND name = ?", tr,
		).Scan(&name)
		if err != nil {
			t.Errorf("expected trigger %q to exist, got: %v", tr, err)
		}
	}
}

// Task 1.5 — call New() on existing DB, assert no error and existing rows survive
func TestSkillsMigrationIsIdempotent(t *testing.T) {
	cfg := mustDefaultConfig(t)
	cfg.DataDir = t.TempDir()

	// First open
	s1, err := New(cfg)
	if err != nil {
		t.Fatalf("first New(): %v", err)
	}
	// Seed a skill directly via SQL so we can verify data survives
	if _, err := s1.db.Exec(
		`INSERT INTO skills (name, display_name, content) VALUES ('seed-skill', 'Seed Skill', 'content')`,
	); err != nil {
		t.Fatalf("seed skill: %v", err)
	}
	_ = s1.Close()

	// Second open — must not error and must not lose the row
	s2, err := New(cfg)
	if err != nil {
		t.Fatalf("second New() (idempotent migration): %v", err)
	}
	defer s2.Close()

	var count int
	if err := s2.db.QueryRow("SELECT COUNT(*) FROM skills WHERE name='seed-skill'").Scan(&count); err != nil {
		t.Fatalf("count seed skill: %v", err)
	}
	if count != 1 {
		t.Errorf("expected seed-skill to survive idempotent migration, got count=%d", count)
	}
}

// Task 2.1 — TestCreateSkillHappyPath + TestCreateSkillDuplicateNameReturnsError
func TestCreateSkillHappyPath(t *testing.T) {
	s := newTestStore(t)

	skill, err := s.CreateSkill(CreateSkillParams{
		Name:        "test-skill",
		DisplayName: "Test Skill",
		Triggers:    "when writing tests",
		Content:     "Use table-driven tests",
		ChangedBy:   "system",
	})
	if err != nil {
		t.Fatalf("CreateSkill: %v", err)
	}
	if skill == nil {
		t.Fatal("expected non-nil skill")
	}
	if skill.Name != "test-skill" {
		t.Errorf("expected name 'test-skill', got %q", skill.Name)
	}
	if skill.Version != 1 {
		t.Errorf("expected version 1, got %d", skill.Version)
	}
	if skill.ID == 0 {
		t.Error("expected non-zero ID")
	}

	// Verify skill_versions row was inserted
	var versionCount int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM skill_versions WHERE skill_id = ?", skill.ID).Scan(&versionCount); err != nil {
		t.Fatalf("count skill_versions: %v", err)
	}
	if versionCount != 1 {
		t.Errorf("expected 1 skill_versions row, got %d", versionCount)
	}
}

func TestCreateSkillDuplicateNameReturnsError(t *testing.T) {
	s := newTestStore(t)

	params := CreateSkillParams{
		Name:        "dup-skill",
		DisplayName: "Dup Skill",
		Content:     "content",
		ChangedBy:   "system",
	}
	if _, err := s.CreateSkill(params); err != nil {
		t.Fatalf("first CreateSkill: %v", err)
	}
	_, err := s.CreateSkill(params)
	if err == nil {
		t.Fatal("expected error for duplicate name, got nil")
	}
}

// Task 2.3 — TestUpdateSkillIncrementsVersionAndInsertsVersionRow + TestUpdateSkillNotFoundReturnsErrNoRows
func TestUpdateSkillIncrementsVersionAndInsertsVersionRow(t *testing.T) {
	s := newTestStore(t)

	created, err := s.CreateSkill(CreateSkillParams{
		Name:        "update-skill",
		DisplayName: "Update Skill",
		Content:     "original content",
		ChangedBy:   "system",
	})
	if err != nil {
		t.Fatalf("CreateSkill: %v", err)
	}

	newContent := "updated content"
	updated, err := s.UpdateSkill("update-skill", UpdateSkillParams{
		Content:   &newContent,
		ChangedBy: "mcp",
	})
	if err != nil {
		t.Fatalf("UpdateSkill: %v", err)
	}
	if updated.Version != created.Version+1 {
		t.Errorf("expected version %d, got %d", created.Version+1, updated.Version)
	}
	if updated.Content != "updated content" {
		t.Errorf("expected updated content, got %q", updated.Content)
	}

	// Verify a new skill_versions row was inserted
	var versionCount int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM skill_versions WHERE skill_id = ?", created.ID).Scan(&versionCount); err != nil {
		t.Fatalf("count skill_versions: %v", err)
	}
	if versionCount != 2 {
		t.Errorf("expected 2 skill_versions rows (v1 + v2), got %d", versionCount)
	}
}

func TestUpdateSkillNotFoundReturnsErrNoRows(t *testing.T) {
	s := newTestStore(t)

	newContent := "something"
	_, err := s.UpdateSkill("nonexistent-skill", UpdateSkillParams{
		Content:   &newContent,
		ChangedBy: "system",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent skill, got nil")
	}
}

// Task 2.5 — TestGetSkillFound + TestGetSkillNotFoundReturnsErrNoRows
func TestGetSkillFound(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.CreateSkill(CreateSkillParams{
		Name:        "get-skill",
		DisplayName: "Get Skill",
		Content:     "skill content here",
		ChangedBy:   "system",
	}); err != nil {
		t.Fatalf("CreateSkill: %v", err)
	}

	skill, err := s.GetSkill("get-skill")
	if err != nil {
		t.Fatalf("GetSkill: %v", err)
	}
	if skill == nil {
		t.Fatal("expected non-nil skill")
	}
	if skill.Name != "get-skill" {
		t.Errorf("expected 'get-skill', got %q", skill.Name)
	}
	if skill.Content != "skill content here" {
		t.Errorf("expected full content, got %q", skill.Content)
	}
}

func TestGetSkillNotFoundReturnsErrNoRows(t *testing.T) {
	s := newTestStore(t)

	_, err := s.GetSkill("missing-skill")
	if err == nil {
		t.Fatal("expected error for missing skill, got nil")
	}
}

// Task 2.7 — TestListSkillsNoFilter + TestListSkillsExcludesContentField
// Note: StackID/CategoryID filter tests are in TestMigrateSkillsCatalogTables (Phase 1)
// and will be expanded in Phase 2 once catalog CRUD is implemented.
func seedSkills(t *testing.T, s *Store) {
	t.Helper()
	skills := []CreateSkillParams{
		{Name: "go-testing", DisplayName: "Go Testing", Content: "test content go", ChangedBy: "system"},
		{Name: "nestjs-api", DisplayName: "NestJS API", Content: "nestjs content ts", ChangedBy: "system"},
		{Name: "react-hooks", DisplayName: "React Hooks", Content: "react hooks content", ChangedBy: "system"},
	}
	for _, p := range skills {
		if _, err := s.CreateSkill(p); err != nil {
			t.Fatalf("seedSkills CreateSkill(%s): %v", p.Name, err)
		}
	}
}

func TestListSkillsNoFilter(t *testing.T) {
	s := newTestStore(t)
	seedSkills(t, s)

	skills, err := s.ListSkills(ListSkillsParams{})
	if err != nil {
		t.Fatalf("ListSkills: %v", err)
	}
	if len(skills) != 3 {
		t.Errorf("expected 3 skills, got %d", len(skills))
	}
}

func TestListSkillsStackIDFilter(t *testing.T) {
	s := newTestStore(t)

	// Create a stack first
	res, err := s.db.Exec(`INSERT INTO stacks (name, display_name) VALUES ('typescript', 'TypeScript')`)
	if err != nil {
		t.Fatalf("insert stack: %v", err)
	}
	stackID, _ := res.LastInsertId()

	// Create skills — two with the TypeScript stack, one without
	sk1, err := s.CreateSkill(CreateSkillParams{Name: "nestjs", DisplayName: "NestJS", Content: "nestjs", StackIDs: []int64{stackID}, ChangedBy: "test"})
	if err != nil {
		t.Fatalf("CreateSkill nestjs: %v", err)
	}
	sk2, err := s.CreateSkill(CreateSkillParams{Name: "react", DisplayName: "React", Content: "react", StackIDs: []int64{stackID}, ChangedBy: "test"})
	if err != nil {
		t.Fatalf("CreateSkill react: %v", err)
	}
	if _, err := s.CreateSkill(CreateSkillParams{Name: "go-skill", DisplayName: "Go", Content: "go", ChangedBy: "test"}); err != nil {
		t.Fatalf("CreateSkill go: %v", err)
	}
	_ = sk1
	_ = sk2

	sid := stackID
	skills, err := s.ListSkills(ListSkillsParams{StackID: &sid})
	if err != nil {
		t.Fatalf("ListSkills StackID filter: %v", err)
	}
	if len(skills) != 2 {
		t.Errorf("expected 2 TypeScript skills, got %d", len(skills))
	}
}

func TestListSkillsCategoryIDFilter(t *testing.T) {
	s := newTestStore(t)

	// Create a category
	res, err := s.db.Exec(`INSERT INTO categories (name, display_name) VALUES ('testing', 'Testing')`)
	if err != nil {
		t.Fatalf("insert category: %v", err)
	}
	catID, _ := res.LastInsertId()

	// Create skills — one with the testing category
	if _, err := s.CreateSkill(CreateSkillParams{Name: "go-testing", DisplayName: "Go Testing", Content: "go test", CategoryIDs: []int64{catID}, ChangedBy: "test"}); err != nil {
		t.Fatalf("CreateSkill go-testing: %v", err)
	}
	if _, err := s.CreateSkill(CreateSkillParams{Name: "nestjs-api", DisplayName: "NestJS API", Content: "nestjs", ChangedBy: "test"}); err != nil {
		t.Fatalf("CreateSkill nestjs: %v", err)
	}

	cid := catID
	skills, err := s.ListSkills(ListSkillsParams{CategoryID: &cid})
	if err != nil {
		t.Fatalf("ListSkills CategoryID filter: %v", err)
	}
	if len(skills) != 1 {
		t.Errorf("expected 1 testing skill, got %d", len(skills))
	}
}

func TestListSkillsExcludesContentField(t *testing.T) {
	s := newTestStore(t)
	seedSkills(t, s)

	skills, err := s.ListSkills(ListSkillsParams{})
	if err != nil {
		t.Fatalf("ListSkills: %v", err)
	}
	for _, sk := range skills {
		if sk.Content != "" {
			t.Errorf("expected Content to be empty in ListSkills, got %q for skill %q", sk.Content, sk.Name)
		}
	}
}

// Task 2.9 — TestGetSkillVersionsReturnsHistory + TestGetSkillVersionsUnknownSkillReturnsEmptySlice
func TestGetSkillVersionsReturnsHistory(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.CreateSkill(CreateSkillParams{
		Name:        "versioned-skill",
		DisplayName: "Versioned Skill",
		Content:     "v1 content",
		ChangedBy:   "system",
	}); err != nil {
		t.Fatalf("CreateSkill: %v", err)
	}

	c2 := "v2 content"
	if _, err := s.UpdateSkill("versioned-skill", UpdateSkillParams{Content: &c2, ChangedBy: "mcp"}); err != nil {
		t.Fatalf("UpdateSkill v2: %v", err)
	}
	c3 := "v3 content"
	if _, err := s.UpdateSkill("versioned-skill", UpdateSkillParams{Content: &c3, ChangedBy: "mcp"}); err != nil {
		t.Fatalf("UpdateSkill v3: %v", err)
	}

	versions, err := s.GetSkillVersions("versioned-skill")
	if err != nil {
		t.Fatalf("GetSkillVersions: %v", err)
	}
	if len(versions) != 3 {
		t.Errorf("expected 3 versions, got %d", len(versions))
	}
	// First should be highest version (DESC order)
	if versions[0].Version != 3 {
		t.Errorf("expected first version to be 3, got %d", versions[0].Version)
	}
}

func TestGetSkillVersionsUnknownSkillReturnsEmptySlice(t *testing.T) {
	s := newTestStore(t)

	versions, err := s.GetSkillVersions("ghost-skill")
	if err != nil {
		t.Fatalf("GetSkillVersions for unknown skill: %v", err)
	}
	if len(versions) != 0 {
		t.Errorf("expected empty slice, got %d versions", len(versions))
	}
}

// ─── Phase 2: Catalog CRUD tests ─────────────────────────────────────────────

// Task 2.1 [RED] TestStackCRUD — ListStacks, CreateStack, DeleteStack
func TestStackCRUD(t *testing.T) {
	t.Run("list empty returns empty slice", func(t *testing.T) {
		s := newTestStore(t)
		stacks, err := s.ListStacks()
		if err != nil {
			t.Fatalf("ListStacks: %v", err)
		}
		if stacks == nil {
			t.Fatal("expected non-nil slice, got nil")
		}
		if len(stacks) != 0 {
			t.Errorf("expected 0 stacks, got %d", len(stacks))
		}
	})

	t.Run("create stack returns assigned ID and fields", func(t *testing.T) {
		s := newTestStore(t)
		stack, err := s.CreateStack("angular", "Angular")
		if err != nil {
			t.Fatalf("CreateStack: %v", err)
		}
		if stack == nil {
			t.Fatal("expected non-nil stack")
		}
		if stack.ID == 0 {
			t.Error("expected non-zero ID")
		}
		if stack.Name != "angular" {
			t.Errorf("expected name 'angular', got %q", stack.Name)
		}
		if stack.DisplayName != "Angular" {
			t.Errorf("expected display_name 'Angular', got %q", stack.DisplayName)
		}
	})

	t.Run("list returns created stacks ordered by name", func(t *testing.T) {
		s := newTestStore(t)
		if _, err := s.CreateStack("nestjs", "NestJS"); err != nil {
			t.Fatalf("CreateStack nestjs: %v", err)
		}
		if _, err := s.CreateStack("angular", "Angular"); err != nil {
			t.Fatalf("CreateStack angular: %v", err)
		}

		stacks, err := s.ListStacks()
		if err != nil {
			t.Fatalf("ListStacks: %v", err)
		}
		if len(stacks) != 2 {
			t.Fatalf("expected 2 stacks, got %d", len(stacks))
		}
		// Should be ordered by name ASC
		if stacks[0].Name != "angular" {
			t.Errorf("expected first stack 'angular', got %q", stacks[0].Name)
		}
		if stacks[1].Name != "nestjs" {
			t.Errorf("expected second stack 'nestjs', got %q", stacks[1].Name)
		}
	})

	t.Run("duplicate name returns error", func(t *testing.T) {
		s := newTestStore(t)
		if _, err := s.CreateStack("angular", "Angular"); err != nil {
			t.Fatalf("first CreateStack: %v", err)
		}
		_, err := s.CreateStack("angular", "Angular v2")
		if err == nil {
			t.Fatal("expected duplicate name error, got nil")
		}
	})

	t.Run("delete removes stack and cascades to skill_stacks", func(t *testing.T) {
		s := newTestStore(t)
		stack, err := s.CreateStack("go", "Go")
		if err != nil {
			t.Fatalf("CreateStack: %v", err)
		}

		// Create a skill that references this stack
		skill, err := s.CreateSkill(CreateSkillParams{
			Name:        "go-skill",
			DisplayName: "Go Skill",
			Content:     "content",
			StackIDs:    []int64{stack.ID},
			ChangedBy:   "test",
		})
		if err != nil {
			t.Fatalf("CreateSkill: %v", err)
		}

		// Verify join row exists
		var joinCount int
		if err := s.db.QueryRow(
			"SELECT COUNT(*) FROM skill_stacks WHERE skill_id = ? AND stack_id = ?",
			skill.ID, stack.ID,
		).Scan(&joinCount); err != nil {
			t.Fatalf("count join rows: %v", err)
		}
		if joinCount != 1 {
			t.Errorf("expected 1 skill_stacks row before delete, got %d", joinCount)
		}

		// Delete the stack — join rows should cascade delete
		if err := s.DeleteStack(stack.ID); err != nil {
			t.Fatalf("DeleteStack: %v", err)
		}

		// Verify join rows are gone
		if err := s.db.QueryRow(
			"SELECT COUNT(*) FROM skill_stacks WHERE stack_id = ?", stack.ID,
		).Scan(&joinCount); err != nil {
			t.Fatalf("count join rows after delete: %v", err)
		}
		if joinCount != 0 {
			t.Errorf("expected 0 skill_stacks rows after cascade delete, got %d", joinCount)
		}

		// Verify stack is gone from list
		stacks, err := s.ListStacks()
		if err != nil {
			t.Fatalf("ListStacks after delete: %v", err)
		}
		if len(stacks) != 0 {
			t.Errorf("expected 0 stacks after delete, got %d", len(stacks))
		}
	})
}

// Task 2.3 [RED] TestCategoryCRUD — ListCategories, CreateCategory, DeleteCategory
func TestCategoryCRUD(t *testing.T) {
	t.Run("list empty returns empty slice", func(t *testing.T) {
		s := newTestStore(t)
		categories, err := s.ListCategories()
		if err != nil {
			t.Fatalf("ListCategories: %v", err)
		}
		if categories == nil {
			t.Fatal("expected non-nil slice, got nil")
		}
		if len(categories) != 0 {
			t.Errorf("expected 0 categories, got %d", len(categories))
		}
	})

	t.Run("create category returns assigned ID and fields", func(t *testing.T) {
		s := newTestStore(t)
		cat, err := s.CreateCategory("conventions", "Conventions")
		if err != nil {
			t.Fatalf("CreateCategory: %v", err)
		}
		if cat == nil {
			t.Fatal("expected non-nil category")
		}
		if cat.ID == 0 {
			t.Error("expected non-zero ID")
		}
		if cat.Name != "conventions" {
			t.Errorf("expected name 'conventions', got %q", cat.Name)
		}
		if cat.DisplayName != "Conventions" {
			t.Errorf("expected display_name 'Conventions', got %q", cat.DisplayName)
		}
	})

	t.Run("list returns created categories ordered by name", func(t *testing.T) {
		s := newTestStore(t)
		if _, err := s.CreateCategory("patterns", "Patterns"); err != nil {
			t.Fatalf("CreateCategory patterns: %v", err)
		}
		if _, err := s.CreateCategory("conventions", "Conventions"); err != nil {
			t.Fatalf("CreateCategory conventions: %v", err)
		}

		categories, err := s.ListCategories()
		if err != nil {
			t.Fatalf("ListCategories: %v", err)
		}
		if len(categories) != 2 {
			t.Fatalf("expected 2 categories, got %d", len(categories))
		}
		// Should be ordered by name ASC
		if categories[0].Name != "conventions" {
			t.Errorf("expected first category 'conventions', got %q", categories[0].Name)
		}
		if categories[1].Name != "patterns" {
			t.Errorf("expected second category 'patterns', got %q", categories[1].Name)
		}
	})

	t.Run("duplicate name returns error", func(t *testing.T) {
		s := newTestStore(t)
		if _, err := s.CreateCategory("patterns", "Patterns"); err != nil {
			t.Fatalf("first CreateCategory: %v", err)
		}
		_, err := s.CreateCategory("patterns", "Patterns v2")
		if err == nil {
			t.Fatal("expected duplicate name error, got nil")
		}
	})

	t.Run("delete removes category and cascades to skill_categories", func(t *testing.T) {
		s := newTestStore(t)
		cat, err := s.CreateCategory("tutorial", "Tutorial")
		if err != nil {
			t.Fatalf("CreateCategory: %v", err)
		}

		// Create a skill referencing this category
		skill, err := s.CreateSkill(CreateSkillParams{
			Name:        "tutorial-skill",
			DisplayName: "Tutorial Skill",
			Content:     "content",
			CategoryIDs: []int64{cat.ID},
			ChangedBy:   "test",
		})
		if err != nil {
			t.Fatalf("CreateSkill: %v", err)
		}

		// Verify join row exists
		var joinCount int
		if err := s.db.QueryRow(
			"SELECT COUNT(*) FROM skill_categories WHERE skill_id = ? AND category_id = ?",
			skill.ID, cat.ID,
		).Scan(&joinCount); err != nil {
			t.Fatalf("count join rows: %v", err)
		}
		if joinCount != 1 {
			t.Errorf("expected 1 skill_categories row before delete, got %d", joinCount)
		}

		// Delete the category
		if err := s.DeleteCategory(cat.ID); err != nil {
			t.Fatalf("DeleteCategory: %v", err)
		}

		// Verify join rows are gone
		if err := s.db.QueryRow(
			"SELECT COUNT(*) FROM skill_categories WHERE category_id = ?", cat.ID,
		).Scan(&joinCount); err != nil {
			t.Fatalf("count join rows after delete: %v", err)
		}
		if joinCount != 0 {
			t.Errorf("expected 0 skill_categories rows after cascade delete, got %d", joinCount)
		}

		// Verify category is gone from list
		categories, err := s.ListCategories()
		if err != nil {
			t.Fatalf("ListCategories after delete: %v", err)
		}
		if len(categories) != 0 {
			t.Errorf("expected 0 categories after delete, got %d", len(categories))
		}
	})
}

// Task 2.5 [RED] Update TestCreateSkill with join row assertions + FK error test
func TestCreateSkillWithStacksAndCategories(t *testing.T) {
	t.Run("create skill with multiple stacks inserts join rows", func(t *testing.T) {
		s := newTestStore(t)

		st1, err := s.CreateStack("angular", "Angular")
		if err != nil {
			t.Fatalf("CreateStack angular: %v", err)
		}
		st2, err := s.CreateStack("nestjs", "NestJS")
		if err != nil {
			t.Fatalf("CreateStack nestjs: %v", err)
		}
		cat1, err := s.CreateCategory("tutorial", "Tutorial")
		if err != nil {
			t.Fatalf("CreateCategory tutorial: %v", err)
		}

		skill, err := s.CreateSkill(CreateSkillParams{
			Name:        "full-skill",
			DisplayName: "Full Skill",
			Content:     "content",
			StackIDs:    []int64{st1.ID, st2.ID},
			CategoryIDs: []int64{cat1.ID},
			ChangedBy:   "test",
		})
		if err != nil {
			t.Fatalf("CreateSkill: %v", err)
		}

		// Assert: two skill_stacks rows
		var stackJoins int
		if err := s.db.QueryRow(
			"SELECT COUNT(*) FROM skill_stacks WHERE skill_id = ?", skill.ID,
		).Scan(&stackJoins); err != nil {
			t.Fatalf("count skill_stacks: %v", err)
		}
		if stackJoins != 2 {
			t.Errorf("expected 2 skill_stacks rows, got %d", stackJoins)
		}

		// Assert: one skill_categories row
		var catJoins int
		if err := s.db.QueryRow(
			"SELECT COUNT(*) FROM skill_categories WHERE skill_id = ?", skill.ID,
		).Scan(&catJoins); err != nil {
			t.Fatalf("count skill_categories: %v", err)
		}
		if catJoins != 1 {
			t.Errorf("expected 1 skill_categories row, got %d", catJoins)
		}

		// Assert: GetSkill returns populated slices
		got, err := s.GetSkill("full-skill")
		if err != nil {
			t.Fatalf("GetSkill: %v", err)
		}
		if len(got.Stacks) != 2 {
			t.Errorf("expected 2 Stacks, got %d", len(got.Stacks))
		}
		if len(got.Categories) != 1 {
			t.Errorf("expected 1 Categories, got %d", len(got.Categories))
		}
	})

	t.Run("create skill with unknown stack ID returns FK error", func(t *testing.T) {
		s := newTestStore(t)

		_, err := s.CreateSkill(CreateSkillParams{
			Name:        "fk-error-skill",
			DisplayName: "FK Error Skill",
			Content:     "content",
			StackIDs:    []int64{99999}, // non-existent stack ID
			ChangedBy:   "test",
		})
		if err == nil {
			t.Fatal("expected FK constraint error for unknown stack ID, got nil")
		}

		// Assert: no skill row was persisted (TX should have rolled back)
		var count int
		if err2 := s.db.QueryRow("SELECT COUNT(*) FROM skills WHERE name = 'fk-error-skill'").Scan(&count); err2 != nil {
			t.Fatalf("count skills: %v", err2)
		}
		if count != 0 {
			t.Errorf("expected 0 skill rows after FK error (rollback), got %d", count)
		}
	})

	t.Run("get skill with no relationships returns empty slices", func(t *testing.T) {
		s := newTestStore(t)

		if _, err := s.CreateSkill(CreateSkillParams{
			Name:        "bare-skill",
			DisplayName: "Bare Skill",
			Content:     "content",
			ChangedBy:   "test",
		}); err != nil {
			t.Fatalf("CreateSkill: %v", err)
		}

		got, err := s.GetSkill("bare-skill")
		if err != nil {
			t.Fatalf("GetSkill: %v", err)
		}
		if got.Stacks == nil {
			t.Error("expected Stacks to be empty slice (not nil)")
		}
		if len(got.Stacks) != 0 {
			t.Errorf("expected 0 stacks, got %d", len(got.Stacks))
		}
		if got.Categories == nil {
			t.Error("expected Categories to be empty slice (not nil)")
		}
		if len(got.Categories) != 0 {
			t.Errorf("expected 0 categories, got %d", len(got.Categories))
		}
	})
}

// Task 3.1 — FTS tests: keyword, content, special-char sanitization, FTS empty after delete
func TestSearchSkillsByTriggerKeyword(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateSkill(CreateSkillParams{
		Name:        "fts-skill-trigger",
		DisplayName: "FTS Skill",
		Triggers:    "when writing htmx",
		Content:     "htmx guide content",
		ChangedBy:   "system",
	}); err != nil {
		t.Fatalf("CreateSkill: %v", err)
	}

	results, err := s.ListSkills(ListSkillsParams{Query: "htmx"})
	if err != nil {
		t.Fatalf("ListSkills(Query=htmx): %v", err)
	}
	if len(results) == 0 {
		t.Error("expected FTS to find skill by trigger keyword 'htmx', got 0 results")
	}
}

func TestSearchSkillsByContent(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateSkill(CreateSkillParams{
		Name:        "fts-skill-content",
		DisplayName: "FTS Content Skill",
		Content:     "use bubble tea for terminal UI",
		ChangedBy:   "system",
	}); err != nil {
		t.Fatalf("CreateSkill: %v", err)
	}

	results, err := s.ListSkills(ListSkillsParams{Query: "bubble"})
	if err != nil {
		t.Fatalf("ListSkills(Query=bubble): %v", err)
	}
	if len(results) == 0 {
		t.Error("expected FTS to find skill by content keyword 'bubble', got 0 results")
	}
}

func TestSearchSkillsSanitizesSpecialChars(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateSkill(CreateSkillParams{
		Name:        "fts-skill-special",
		DisplayName: "FTS Special Skill",
		Content:     "handles special characters gracefully",
		ChangedBy:   "system",
	}); err != nil {
		t.Fatalf("CreateSkill: %v", err)
	}

	// Query with special FTS chars that would cause syntax errors if not sanitized
	_, err := s.ListSkills(ListSkillsParams{Query: `"special" OR AND`})
	if err != nil {
		t.Errorf("expected sanitized special-char query to not error, got: %v", err)
	}
}

func TestSearchSkillsFTSEmptyAfterDelete(t *testing.T) {
	s := newTestStore(t)
	skill, err := s.CreateSkill(CreateSkillParams{
		Name:        "fts-delete-skill",
		DisplayName: "FTS Delete Skill",
		Content:     "uniquewordxyz content",
		ChangedBy:   "system",
	})
	if err != nil {
		t.Fatalf("CreateSkill: %v", err)
	}

	// Verify it's findable before delete
	before, err := s.ListSkills(ListSkillsParams{Query: "uniquewordxyz"})
	if err != nil {
		t.Fatalf("ListSkills before delete: %v", err)
	}
	if len(before) == 0 {
		t.Fatal("expected to find skill before delete")
	}

	// Delete via direct SQL (no DeleteSkill method yet) — must delete versions first (FK constraint)
	if _, err := s.db.Exec("DELETE FROM skill_versions WHERE skill_id = ?", skill.ID); err != nil {
		t.Fatalf("DELETE skill_versions: %v", err)
	}
	if _, err := s.db.Exec("DELETE FROM skills WHERE id = ?", skill.ID); err != nil {
		t.Fatalf("DELETE skill: %v", err)
	}

	// Verify FTS no longer returns it
	after, err := s.ListSkills(ListSkillsParams{Query: "uniquewordxyz"})
	if err != nil {
		t.Fatalf("ListSkills after delete: %v", err)
	}
	if len(after) != 0 {
		t.Errorf("expected 0 results after delete, got %d", len(after))
	}
}

// ─── Phase 1 — Schema Migration Tests ────────────────────────────────────────

// TestMigrateSkillsCatalogTables verifies the full catalog migration:
// - pre-migration DB with stack/category columns gets migrated correctly
// - catalog tables (stacks, categories, skill_stacks, skill_categories) are created
// - old stack/category columns are removed from skills
// - comma-separated values are parsed into join rows
// - duplicate catalog names are deduplicated
// - migration is idempotent (2nd run = no-op, no error)
func TestMigrateSkillsCatalogTables(t *testing.T) {
	t.Run("migrates pre-migration DB correctly", func(t *testing.T) {
		dataDir := t.TempDir()
		dbPath := filepath.Join(dataDir, "lore.db")

		// Seed a pre-migration DB with skills that have stack/category columns
		db, err := sql.Open("sqlite", dbPath)
		if err != nil {
			t.Fatalf("open legacy db: %v", err)
		}
		if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
			t.Fatalf("enable fk: %v", err)
		}
		_, err = db.Exec(`
			CREATE TABLE sessions (
				id TEXT PRIMARY KEY,
				project TEXT NOT NULL,
				directory TEXT NOT NULL,
				started_at TEXT NOT NULL DEFAULT (datetime('now')),
				ended_at TEXT,
				summary TEXT
			);
			CREATE TABLE skills (
				id           INTEGER PRIMARY KEY AUTOINCREMENT,
				name         TEXT    NOT NULL UNIQUE,
				display_name TEXT    NOT NULL,
				category     TEXT    NOT NULL DEFAULT '',
				stack        TEXT    NOT NULL DEFAULT '',
				triggers     TEXT    NOT NULL DEFAULT '',
				content      TEXT    NOT NULL,
				version      INTEGER NOT NULL DEFAULT 1,
				is_active    INTEGER NOT NULL DEFAULT 1,
				changed_by   TEXT    NOT NULL DEFAULT 'system',
				created_at   TEXT    NOT NULL DEFAULT (datetime('now')),
				updated_at   TEXT    NOT NULL DEFAULT (datetime('now'))
			);
			CREATE TABLE skill_versions (
				id         INTEGER PRIMARY KEY AUTOINCREMENT,
				skill_id   INTEGER NOT NULL,
				version    INTEGER NOT NULL,
				content    TEXT    NOT NULL,
				changed_by TEXT    NOT NULL DEFAULT 'system',
				created_at TEXT    NOT NULL DEFAULT (datetime('now')),
				FOREIGN KEY (skill_id) REFERENCES skills(id)
			);
			INSERT INTO skills (name, display_name, stack, category, content)
			VALUES
				('angular-guard', 'Angular Guard', 'angular', 'conventions', 'angular content'),
				('multi-stack', 'Multi Stack', 'angular,nestjs', 'conventions,patterns', 'multi content'),
				('no-meta', 'No Meta', '', '', 'bare content');
		`)
		if err != nil {
			_ = db.Close()
			t.Fatalf("seed legacy db: %v", err)
		}
		if err := db.Close(); err != nil {
			t.Fatalf("close legacy db: %v", err)
		}

		// Open store — this triggers migration
		cfg := mustDefaultConfig(t)
		cfg.DataDir = dataDir
		s, err := New(cfg)
		if err != nil {
			t.Fatalf("New() after legacy schema: %v", err)
		}
		t.Cleanup(func() { _ = s.Close() })

		// Assert: catalog tables exist
		for _, tbl := range []string{"stacks", "categories", "skill_stacks", "skill_categories"} {
			var name string
			if err := s.db.QueryRow(
				"SELECT name FROM sqlite_master WHERE type='table' AND name = ?", tbl,
			).Scan(&name); err != nil {
				t.Errorf("expected table %q to exist, got: %v", tbl, err)
			}
		}

		// Assert: old stack/category columns removed from skills
		rows, err := s.db.Query("PRAGMA table_info(skills)")
		if err != nil {
			t.Fatalf("PRAGMA table_info(skills): %v", err)
		}
		defer rows.Close()
		var hasStack, hasCategory bool
		for rows.Next() {
			var cid int
			var colName, colType string
			var notNull, pk int
			var defaultVal any
			if err := rows.Scan(&cid, &colName, &colType, &notNull, &defaultVal, &pk); err != nil {
				t.Fatalf("scan column: %v", err)
			}
			if colName == "stack" {
				hasStack = true
			}
			if colName == "category" {
				hasCategory = true
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("iterate columns: %v", err)
		}
		if hasStack {
			t.Error("expected skills.stack column to be removed after migration")
		}
		if hasCategory {
			t.Error("expected skills.category column to be removed after migration")
		}

		// Assert: single stack migrated correctly (angular-guard → stacks row + skill_stacks row)
		var angularStackID int64
		if err := s.db.QueryRow(
			"SELECT id FROM stacks WHERE name = 'angular'",
		).Scan(&angularStackID); err != nil {
			t.Fatalf("expected stacks row for 'angular': %v", err)
		}

		var guardSkillID int64
		if err := s.db.QueryRow(
			"SELECT id FROM skills WHERE name = 'angular-guard'",
		).Scan(&guardSkillID); err != nil {
			t.Fatalf("expected angular-guard skill to exist: %v", err)
		}

		var joinCount int
		if err := s.db.QueryRow(
			"SELECT COUNT(*) FROM skill_stacks WHERE skill_id = ? AND stack_id = ?",
			guardSkillID, angularStackID,
		).Scan(&joinCount); err != nil {
			t.Fatalf("count skill_stacks for angular-guard: %v", err)
		}
		if joinCount != 1 {
			t.Errorf("expected 1 skill_stacks row for angular-guard/angular, got %d", joinCount)
		}

		// Assert: comma-separated values produce multiple join rows (multi-stack)
		var multiSkillID int64
		if err := s.db.QueryRow(
			"SELECT id FROM skills WHERE name = 'multi-stack'",
		).Scan(&multiSkillID); err != nil {
			t.Fatalf("expected multi-stack skill to exist: %v", err)
		}

		var multiStackJoins int
		if err := s.db.QueryRow(
			"SELECT COUNT(*) FROM skill_stacks WHERE skill_id = ?", multiSkillID,
		).Scan(&multiStackJoins); err != nil {
			t.Fatalf("count multi-stack stacks: %v", err)
		}
		if multiStackJoins != 2 {
			t.Errorf("expected 2 skill_stacks rows for multi-stack (angular,nestjs), got %d", multiStackJoins)
		}

		var multiCatJoins int
		if err := s.db.QueryRow(
			"SELECT COUNT(*) FROM skill_categories WHERE skill_id = ?", multiSkillID,
		).Scan(&multiCatJoins); err != nil {
			t.Fatalf("count multi-stack categories: %v", err)
		}
		if multiCatJoins != 2 {
			t.Errorf("expected 2 skill_categories rows for multi-stack (conventions,patterns), got %d", multiCatJoins)
		}

		// Assert: duplicate catalog entries are deduplicated
		// Both angular-guard and multi-stack share "angular" → only 1 row in stacks
		var angularCount int
		if err := s.db.QueryRow(
			"SELECT COUNT(*) FROM stacks WHERE name = 'angular'",
		).Scan(&angularCount); err != nil {
			t.Fatalf("count stacks for angular: %v", err)
		}
		if angularCount != 1 {
			t.Errorf("expected exactly 1 stacks row for 'angular', got %d", angularCount)
		}

		// Assert: total stacks count (angular, nestjs = 2 unique stacks)
		var totalStacks int
		if err := s.db.QueryRow("SELECT COUNT(*) FROM stacks").Scan(&totalStacks); err != nil {
			t.Fatalf("count stacks: %v", err)
		}
		if totalStacks != 2 {
			t.Errorf("expected 2 unique stacks (angular, nestjs), got %d", totalStacks)
		}

		// Assert: skill with no stack/category produces no join rows
		var noMetaID int64
		if err := s.db.QueryRow(
			"SELECT id FROM skills WHERE name = 'no-meta'",
		).Scan(&noMetaID); err != nil {
			t.Fatalf("expected no-meta skill to exist: %v", err)
		}
		var noMetaJoins int
		if err := s.db.QueryRow(
			"SELECT COUNT(*) FROM skill_stacks WHERE skill_id = ?", noMetaID,
		).Scan(&noMetaJoins); err != nil {
			t.Fatalf("count no-meta stacks: %v", err)
		}
		if noMetaJoins != 0 {
			t.Errorf("expected 0 skill_stacks rows for no-meta, got %d", noMetaJoins)
		}
	})

	t.Run("idempotent - second call is a no-op", func(t *testing.T) {
		s := newTestStore(t)

		// First call from New() already ran. Call again — must not error.
		if err := s.migrateSkillsCatalogTables(); err != nil {
			t.Fatalf("second migrateSkillsCatalogTables should be no-op: %v", err)
		}

		// And again (truly idempotent)
		if err := s.migrateSkillsCatalogTables(); err != nil {
			t.Fatalf("third migrateSkillsCatalogTables should be no-op: %v", err)
		}
	})

	t.Run("cascade delete removes join rows", func(t *testing.T) {
		s := newTestStore(t)

		// Create a stack and skill with a join row
		res, err := s.db.Exec(`INSERT INTO stacks (name, display_name) VALUES ('go', 'Go')`)
		if err != nil {
			t.Fatalf("insert stack: %v", err)
		}
		stackID, _ := res.LastInsertId()

		skill, err := s.CreateSkill(CreateSkillParams{
			Name:        "cascade-skill",
			DisplayName: "Cascade Skill",
			StackIDs:    []int64{stackID},
			Content:     "content",
			ChangedBy:   "test",
		})
		if err != nil {
			t.Fatalf("CreateSkill: %v", err)
		}

		// Verify join row exists
		var joinCount int
		if err := s.db.QueryRow(
			"SELECT COUNT(*) FROM skill_stacks WHERE skill_id = ?", skill.ID,
		).Scan(&joinCount); err != nil {
			t.Fatalf("count join rows: %v", err)
		}
		if joinCount != 1 {
			t.Errorf("expected 1 skill_stacks row before delete, got %d", joinCount)
		}

		// Delete the skill (requires deleting skill_versions first due to FK)
		if _, err := s.db.Exec("DELETE FROM skill_versions WHERE skill_id = ?", skill.ID); err != nil {
			t.Fatalf("delete skill_versions: %v", err)
		}
		if _, err := s.db.Exec("DELETE FROM skills WHERE id = ?", skill.ID); err != nil {
			t.Fatalf("delete skill: %v", err)
		}

		// Verify cascade delete removed join rows
		if err := s.db.QueryRow(
			"SELECT COUNT(*) FROM skill_stacks WHERE skill_id = ?", skill.ID,
		).Scan(&joinCount); err != nil {
			t.Fatalf("count join rows after delete: %v", err)
		}
		if joinCount != 0 {
			t.Errorf("expected 0 skill_stacks rows after cascade delete, got %d", joinCount)
		}
	})
}

// ─── compact-rules: Phase 1 — Schema & Migration ─────────────────────────────

// Task 1.1 [RED] DDL includes compact_rules in skills and skill_versions tables
func TestSkillsDDLHasCompactRulesColumn(t *testing.T) {
	s := newTestStore(t)

	tables := []string{"skills", "skill_versions"}
	for _, tbl := range tables {
		var colName string
		err := s.db.QueryRow(
			`SELECT name FROM pragma_table_info(?) WHERE name = 'compact_rules'`, tbl,
		).Scan(&colName)
		if err != nil {
			t.Errorf("table %q: expected compact_rules column to exist, got: %v", tbl, err)
			continue
		}
		if colName != "compact_rules" {
			t.Errorf("table %q: expected column name 'compact_rules', got %q", tbl, colName)
		}
	}
}

// Task 1.3 [RED] Migration adds compact_rules to existing DB idempotently
func TestCompactRulesMigrationOnExistingDB(t *testing.T) {
	cfg := mustDefaultConfig(t)
	cfg.DataDir = t.TempDir()

	// First open — creates the DB
	s1, err := New(cfg)
	if err != nil {
		t.Fatalf("first New(): %v", err)
	}
	// Seed a skill to verify it survives the second open
	if _, err := s1.db.Exec(
		`INSERT INTO skills (name, display_name, content) VALUES ('legacy-skill', 'Legacy Skill', 'old content')`,
	); err != nil {
		t.Fatalf("seed legacy skill: %v", err)
	}
	_ = s1.Close()

	// Second open — migration must be idempotent (compact_rules already exists after first open)
	s2, err := New(cfg)
	if err != nil {
		t.Fatalf("second New() (migration): %v", err)
	}
	defer s2.Close()

	// compact_rules must exist in both tables
	for _, tbl := range []string{"skills", "skill_versions"} {
		var colName string
		if err := s2.db.QueryRow(
			`SELECT name FROM pragma_table_info(?) WHERE name = 'compact_rules'`, tbl,
		).Scan(&colName); err != nil {
			t.Errorf("after migration, table %q missing compact_rules column: %v", tbl, err)
		}
	}

	// Existing row must survive with compact_rules = ''
	var compactRules string
	if err := s2.db.QueryRow(
		`SELECT compact_rules FROM skills WHERE name = 'legacy-skill'`,
	).Scan(&compactRules); err != nil {
		t.Fatalf("select compact_rules from legacy-skill: %v", err)
	}
	if compactRules != "" {
		t.Errorf("expected compact_rules = '' for legacy row, got %q", compactRules)
	}
}

// ─── compact-rules: Phase 2 — Store Structs & CRUD ───────────────────────────

// Task 2.1 [RED] Skill struct has CompactRules field with correct JSON tag
func TestSkillStructHasCompactRulesField(t *testing.T) {
	s := newTestStore(t)

	skill, err := s.CreateSkill(CreateSkillParams{
		Name:         "compact-test",
		DisplayName:  "Compact Test",
		Content:      "some content",
		CompactRules: "Use table-driven tests.",
		ChangedBy:    "test",
	})
	if err != nil {
		t.Fatalf("CreateSkill: %v", err)
	}

	// Marshal to JSON and verify compact_rules key exists with correct value
	b, err := json.Marshal(skill)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	jsonStr := string(b)
	if !strings.Contains(jsonStr, `"compact_rules"`) {
		t.Errorf("expected JSON to contain 'compact_rules' key, got: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"Use table-driven tests."`) {
		t.Errorf("expected JSON to contain compact_rules value, got: %s", jsonStr)
	}
}

// Task 2.3 [RED] CreateSkill persists compact_rules; GetSkill returns it
func TestCreateSkillPersistsCompactRules(t *testing.T) {
	s := newTestStore(t)

	skill, err := s.CreateSkill(CreateSkillParams{
		Name:         "cr-skill",
		DisplayName:  "CR Skill",
		Content:      "content",
		CompactRules: "Use Given/When/Then.",
		ChangedBy:    "test",
	})
	if err != nil {
		t.Fatalf("CreateSkill: %v", err)
	}
	if skill.CompactRules != "Use Given/When/Then." {
		t.Errorf("CreateSkill: expected CompactRules 'Use Given/When/Then.', got %q", skill.CompactRules)
	}

	// GetSkill must return compact_rules
	fetched, err := s.GetSkill("cr-skill")
	if err != nil {
		t.Fatalf("GetSkill: %v", err)
	}
	if fetched.CompactRules != "Use Given/When/Then." {
		t.Errorf("GetSkill: expected CompactRules 'Use Given/When/Then.', got %q", fetched.CompactRules)
	}
}

// Triangulate: CreateSkill with empty compact_rules stores empty string and version row captures it
func TestCreateSkillEmptyCompactRules(t *testing.T) {
	s := newTestStore(t)

	skill, err := s.CreateSkill(CreateSkillParams{
		Name:         "empty-cr-skill",
		DisplayName:  "Empty CR Skill",
		Content:      "content",
		CompactRules: "",
		ChangedBy:    "test",
	})
	if err != nil {
		t.Fatalf("CreateSkill: %v", err)
	}
	if skill.CompactRules != "" {
		t.Errorf("expected empty CompactRules, got %q", skill.CompactRules)
	}

	// skill_versions row must also have compact_rules = ''
	var versionCompactRules string
	if err := s.db.QueryRow(
		`SELECT compact_rules FROM skill_versions WHERE skill_id = ?`, skill.ID,
	).Scan(&versionCompactRules); err != nil {
		t.Fatalf("select compact_rules from skill_versions: %v", err)
	}
	if versionCompactRules != "" {
		t.Errorf("expected skill_versions.compact_rules = '', got %q", versionCompactRules)
	}
}

// Task 2.5 [RED] UpdateSkill updates compact_rules when non-nil
func TestUpdateSkillCompactRulesNonNil(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.CreateSkill(CreateSkillParams{
		Name:         "upd-cr-skill",
		DisplayName:  "Upd CR Skill",
		Content:      "original",
		CompactRules: "",
		ChangedBy:    "system",
	}); err != nil {
		t.Fatalf("CreateSkill: %v", err)
	}

	newRules := "Always use RFC 2119."
	updated, err := s.UpdateSkill("upd-cr-skill", UpdateSkillParams{
		CompactRules: &newRules,
		ChangedBy:    "mcp",
	})
	if err != nil {
		t.Fatalf("UpdateSkill: %v", err)
	}
	if updated.CompactRules != "Always use RFC 2119." {
		t.Errorf("expected CompactRules 'Always use RFC 2119.', got %q", updated.CompactRules)
	}

	// Verify the new skill_versions row captured compact_rules
	var versionCR string
	if err := s.db.QueryRow(
		`SELECT compact_rules FROM skill_versions WHERE skill_id = ? AND version = 2`, updated.ID,
	).Scan(&versionCR); err != nil {
		t.Fatalf("select compact_rules from skill_versions v2: %v", err)
	}
	if versionCR != "Always use RFC 2119." {
		t.Errorf("expected skill_versions v2.compact_rules = 'Always use RFC 2119.', got %q", versionCR)
	}
}

// Triangulate: nil CompactRules preserves existing value
func TestUpdateSkillCompactRulesNilPreservesExisting(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.CreateSkill(CreateSkillParams{
		Name:         "preserve-cr-skill",
		DisplayName:  "Preserve CR Skill",
		Content:      "original",
		CompactRules: "existing",
		ChangedBy:    "system",
	}); err != nil {
		t.Fatalf("CreateSkill: %v", err)
	}

	newTriggers := "new trigger"
	updated, err := s.UpdateSkill("preserve-cr-skill", UpdateSkillParams{
		Triggers:     &newTriggers,
		CompactRules: nil, // no change
		ChangedBy:    "mcp",
	})
	if err != nil {
		t.Fatalf("UpdateSkill: %v", err)
	}
	if updated.CompactRules != "existing" {
		t.Errorf("expected CompactRules to remain 'existing', got %q", updated.CompactRules)
	}
}

// Task 2.7 [GREEN] ListSkills omits compact_rules (leaves it as empty string)
func TestListSkillsOmitsCompactRules(t *testing.T) {
	s := newTestStore(t)

	// Create skills with non-empty compact_rules
	for _, name := range []string{"skill-a", "skill-b"} {
		if _, err := s.CreateSkill(CreateSkillParams{
			Name:         name,
			DisplayName:  name,
			Content:      "content",
			CompactRules: "some rules",
			ChangedBy:    "test",
		}); err != nil {
			t.Fatalf("CreateSkill %s: %v", name, err)
		}
	}

	skills, err := s.ListSkills(ListSkillsParams{})
	if err != nil {
		t.Fatalf("ListSkills: %v", err)
	}
	if len(skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(skills))
	}
	for _, sk := range skills {
		if sk.CompactRules != "" {
			t.Errorf("ListSkills: expected CompactRules to be empty for %q, got %q", sk.Name, sk.CompactRules)
		}
	}
}

// Task 2.7 [GREEN] GetSkillVersions includes compact_rules
func TestGetSkillVersionsIncludesCompactRules(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.CreateSkill(CreateSkillParams{
		Name:         "versioned-cr-skill",
		DisplayName:  "Versioned CR Skill",
		Content:      "v1 content",
		CompactRules: "v1 rules",
		ChangedBy:    "system",
	}); err != nil {
		t.Fatalf("CreateSkill: %v", err)
	}

	newRules := "v2 rules"
	if _, err := s.UpdateSkill("versioned-cr-skill", UpdateSkillParams{
		CompactRules: &newRules,
		ChangedBy:    "mcp",
	}); err != nil {
		t.Fatalf("UpdateSkill: %v", err)
	}

	versions, err := s.GetSkillVersions("versioned-cr-skill")
	if err != nil {
		t.Fatalf("GetSkillVersions: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("expected 2 versions, got %d", len(versions))
	}
	// versions are DESC: v2 first
	if versions[0].CompactRules != "v2 rules" {
		t.Errorf("v2: expected CompactRules 'v2 rules', got %q", versions[0].CompactRules)
	}
	if versions[1].CompactRules != "v1 rules" {
		t.Errorf("v1: expected CompactRules 'v1 rules', got %q", versions[1].CompactRules)
	}
}
