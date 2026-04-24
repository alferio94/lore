package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	postgresbackend "github.com/alferio94/lore/internal/store/backend/postgres"
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

	if _, err := s.db.Exec(`INSERT INTO sessions (id, project, directory) VALUES ($1, $2, $3)`, "bootstrap-existing", "Lore", "/tmp/lore"); err != nil {
		t.Fatalf("seed existing runtime row: %v", err)
	}

	if err := postgresbackend.Bootstrap(s.db); err != nil {
		t.Fatalf("Bootstrap() rerun error = %v", err)
	}

	for _, table := range []string{"sessions", "observations", "sync_state", "sync_mutations", "skills", "stacks", "categories", "skill_stacks", "skill_categories", "skill_versions", "users"} {
		assertPostgresTableExists(t, s.db, table)
	}

	for _, index := range []string{"idx_pg_obs_search_vector", "idx_pg_skills_name", "idx_pg_skills_active", "idx_pg_stacks_name", "idx_pg_categories_name", "idx_pg_skill_stacks_stack", "idx_pg_skill_categories_category", "idx_pg_skill_versions_skill", "idx_pg_users_email", "idx_pg_users_role", "idx_pg_skills_search_vector"} {
		assertPostgresIndexExists(t, s.db, index)
	}

	var project string
	if err := s.db.QueryRow(`SELECT project FROM sessions WHERE id = $1`, "bootstrap-existing").Scan(&project); err != nil {
		t.Fatalf("load seeded runtime row: %v", err)
	}
	if project != "Lore" {
		t.Fatalf("seeded runtime row project = %q, want Lore", project)
	}
}

func assertPostgresTableExists(t *testing.T, db *sql.DB, table string) {
	t.Helper()

	var exists bool
	if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = $1)`, table).Scan(&exists); err != nil {
		t.Fatalf("check table %s: %v", table, err)
	}
	if !exists {
		t.Fatalf("expected table %s to exist", table)
	}
}

func assertPostgresIndexExists(t *testing.T, db *sql.DB, index string) {
	t.Helper()

	var exists bool
	if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE schemaname = 'public' AND indexname = $1)`, index).Scan(&exists); err != nil {
		t.Fatalf("check index %s: %v", index, err)
	}
	if !exists {
		t.Fatalf("expected index %s to exist", index)
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

func TestPostgresStoreSearchParityIntegration(t *testing.T) {
	s := newPostgresTestStore(t)

	if err := s.CreateSession("pg-search-a", "Lore", "/tmp/lore"); err != nil {
		t.Fatalf("CreateSession lore: %v", err)
	}
	if err := s.CreateSession("pg-search-b", "Lore-Core", "/tmp/lore-core"); err != nil {
		t.Fatalf("CreateSession lore-core: %v", err)
	}

	seed := []AddObservationParams{
		{
			SessionID: "pg-search-a",
			Type:      "decision",
			Title:     "Auth search strongest",
			Content:   "auth ranking keyword strongest body match",
			ToolName:  "search-tool",
			Project:   "Lore",
			Scope:     "project",
			TopicKey:  "search/auth-strongest",
		},
		{
			SessionID: "pg-search-a",
			Type:      "decision",
			Title:     "Tool name hit",
			Content:   "secondary content",
			ToolName:  "auth-runner",
			Project:   "Lore",
			Scope:     "project",
		},
		{
			SessionID: "pg-search-b",
			Type:      "decision",
			Title:     "Fallback candidate",
			Content:   "auth fallback metadata keyword",
			Project:   "Lore-Core",
			Scope:     "project",
		},
		{
			SessionID: "pg-search-a",
			Type:      "architecture",
			Title:     "Scope-only personal",
			Content:   "auth personal keyword",
			Project:   "Lore",
			Scope:     "personal",
		},
	}

	var strongestID int64
	for _, params := range seed {
		id, err := s.AddObservation(params)
		if err != nil {
			t.Fatalf("AddObservation(%s): %v", params.Title, err)
		}
		if params.TopicKey == "search/auth-strongest" {
			strongestID = id
		}
	}

	results, err := s.Search("auth", SearchOptions{Project: "Lore", Scope: "project", Limit: 10})
	if err != nil {
		t.Fatalf("Search project auth: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("expected at least 2 project search results, got %d", len(results))
	}
	if results[0].ID != strongestID {
		t.Fatalf("expected strongest title/content hit first, got %+v", results)
	}

	toolResults, err := s.Search("auth-runner", SearchOptions{Project: "Lore", Scope: "project", Limit: 10})
	if err != nil {
		t.Fatalf("Search tool_name auth-runner: %v", err)
	}
	if len(toolResults) != 1 || toolResults[0].Title != "Tool name hit" {
		t.Fatalf("expected tool_name match, got %+v", toolResults)
	}

	topicResults, err := s.Search("search/auth-strongest", SearchOptions{Project: "Lore", Scope: "project", Limit: 10})
	if err != nil {
		t.Fatalf("Search topic key: %v", err)
	}
	if len(topicResults) == 0 || topicResults[0].ID != strongestID || topicResults[0].Rank != -1000 {
		t.Fatalf("expected direct topic_key hit to lead results, got %+v", topicResults)
	}
	if len(topicResults) > 1 && topicResults[1].ID == strongestID {
		t.Fatalf("expected topic_key result to be de-duplicated from FTS hits, got %+v", topicResults)
	}

	personalResults, err := s.Search("personal", SearchOptions{Project: "Lore", Scope: "project", Limit: 10})
	if err != nil {
		t.Fatalf("Search personal in project scope: %v", err)
	}
	if len(personalResults) != 0 {
		t.Fatalf("expected personal-scope observation excluded from project search, got %+v", personalResults)
	}

	if err := s.DeleteObservation(strongestID, false); err != nil {
		t.Fatalf("DeleteObservation strongest: %v", err)
	}
	postDeleteResults, err := s.Search("strongest", SearchOptions{Project: "Lore", Scope: "project", Limit: 10})
	if err != nil {
		t.Fatalf("Search strongest after delete: %v", err)
	}
	for _, result := range postDeleteResults {
		if result.ID == strongestID {
			t.Fatalf("expected deleted observation excluded from search results")
		}
	}

	fallback, err := s.SearchWithMetadata("metadata", SearchOptions{Project: "lore-c0re", Scope: "project", Limit: 10})
	if err != nil {
		t.Fatalf("SearchWithMetadata fallback: %v", err)
	}
	if !fallback.Metadata.FallbackUsed {
		t.Fatalf("expected fallback metadata to be used")
	}
	if got := strings.Join(fallback.Metadata.FallbackProjects, ","); got != "lore-core" {
		t.Fatalf("fallback projects = %q, want lore-core", got)
	}
	if len(fallback.Results) != 1 || fallback.Results[0].Project == nil || *fallback.Results[0].Project != "lore-core" {
		t.Fatalf("expected fallback result from lore-core, got %+v", fallback.Results)
	}

	names, err := s.ListProjectNames()
	if err != nil {
		t.Fatalf("ListProjectNames: %v", err)
	}
	if got := strings.Join(names, ","); got != "lore,lore-core" {
		t.Fatalf("project names = %q, want lore,lore-core", got)
	}
}

func TestSearchParityAllowsBackendRankVariance(t *testing.T) {
	sqlite := newTestStore(t)
	postgres := newPostgresTestStore(t)

	seedBackend := func(t *testing.T, name string, s Contract) {
		t.Helper()
		if err := s.CreateSession(name+"-a", "Lore", "/tmp/lore"); err != nil {
			t.Fatalf("%s CreateSession lore: %v", name, err)
		}
		if err := s.CreateSession(name+"-b", "Lore", "/tmp/lore-alt"); err != nil {
			t.Fatalf("%s CreateSession lore alt: %v", name, err)
		}

		for _, params := range []AddObservationParams{
			{
				SessionID: name + "-a",
				Type:      "decision",
				Title:     "Auth auth strongest",
				Content:   "auth auth auth ranking keyword strongest body match",
				ToolName:  "search-tool",
				Project:   "Lore",
				Scope:     "project",
			},
			{
				SessionID: name + "-a",
				Type:      "decision",
				Title:     "Tool name hit",
				Content:   "secondary content",
				ToolName:  "auth-runner",
				Project:   "Lore",
				Scope:     "project",
			},
			{
				SessionID: name + "-b",
				Type:      "decision",
				Title:     "Body-only auth helper",
				Content:   "auth helper match in body only",
				Project:   "Lore",
				Scope:     "project",
			},
		} {
			if _, err := s.AddObservation(params); err != nil {
				t.Fatalf("%s AddObservation(%s): %v", name, params.Title, err)
			}
		}
	}

	seedBackend(t, "sqlite", sqlite)
	seedBackend(t, "postgres", postgres)

	opts := SearchOptions{Project: "Lore", Scope: "project", Limit: 10}
	sqliteResults, err := sqlite.Search("auth", opts)
	if err != nil {
		t.Fatalf("sqlite Search auth: %v", err)
	}
	postgresResults, err := postgres.Search("auth", opts)
	if err != nil {
		t.Fatalf("postgres Search auth: %v", err)
	}

	if len(sqliteResults) != 3 || len(postgresResults) != 3 {
		t.Fatalf("expected 3 search results per backend, got sqlite=%d postgres=%d", len(sqliteResults), len(postgresResults))
	}
	if sqliteResults[0].Title != "Auth auth strongest" || postgresResults[0].Title != "Auth auth strongest" {
		t.Fatalf("expected strongest match first on both backends, got sqlite=%q postgres=%q", sqliteResults[0].Title, postgresResults[0].Title)
	}

	if sqliteResults[0].Rank == postgresResults[0].Rank {
		t.Fatalf("expected backend rank values to be allowed to differ at runtime, got equal strongest-match rank %v", sqliteResults[0].Rank)
	}

	remaining := map[string]bool{
		"Tool name hit":         true,
		"Body-only auth helper": true,
	}
	for _, results := range [][]SearchResult{sqliteResults[1:], postgresResults[1:]} {
		seen := map[string]bool{}
		for _, result := range results {
			seen[result.Title] = true
		}
		for title := range remaining {
			if !seen[title] {
				t.Fatalf("expected remaining results to include %q, got %+v", title, results)
			}
		}
	}
}

func TestPostgresStoreUserLifecycleIntegration(t *testing.T) {
	s := newPostgresTestStore(t)

	first, err := s.UpsertUser("first@example.com", "First", "https://example.com/first.png", "google")
	if err != nil {
		t.Fatalf("UpsertUser(first): %v", err)
	}
	if first.Role != "admin" {
		t.Fatalf("first role = %q, want admin", first.Role)
	}

	second, err := s.UpsertUser("second@example.com", "Second", "", "github")
	if err != nil {
		t.Fatalf("UpsertUser(second): %v", err)
	}
	if second.Role != "viewer" {
		t.Fatalf("second role = %q, want viewer", second.Role)
	}

	promoted, err := s.UpdateUserRole(second.ID, "tech_lead")
	if err != nil {
		t.Fatalf("UpdateUserRole(second): %v", err)
	}
	if promoted.Role != "tech_lead" {
		t.Fatalf("promoted role = %q, want tech_lead", promoted.Role)
	}

	relogin, err := s.UpsertUser("second@example.com", "Second Renamed", "https://example.com/second.png", "github")
	if err != nil {
		t.Fatalf("UpsertUser(relogin): %v", err)
	}
	if relogin.ID != second.ID {
		t.Fatalf("relogin id = %d, want %d", relogin.ID, second.ID)
	}
	if relogin.Role != "tech_lead" {
		t.Fatalf("relogin role = %q, want tech_lead", relogin.Role)
	}
	if relogin.Name != "Second Renamed" {
		t.Fatalf("relogin name = %q, want Second Renamed", relogin.Name)
	}
	if relogin.AvatarURL != "https://example.com/second.png" {
		t.Fatalf("relogin avatar = %q, want updated avatar", relogin.AvatarURL)
	}

	users, err := s.ListUsers()
	if err != nil {
		t.Fatalf("ListUsers(): %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("ListUsers len = %d, want 2", len(users))
	}
	if users[0].Email != "first@example.com" || users[1].Email != "second@example.com" {
		t.Fatalf("ListUsers order = %+v, want first then second", users)
	}

	if _, err := s.UpdateUserRole(99999, "admin"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateUserRole(missing) error = %v, want ErrNotFound", err)
	}
}

func TestPostgresStoreUpsertUserAssignsSingleAdminConcurrently(t *testing.T) {
	s := newPostgresTestStore(t)

	const totalUsers = 12
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, totalUsers)

	for i := 0; i < totalUsers; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := s.UpsertUser(fmt.Sprintf("concurrent-%02d@example.com", i), fmt.Sprintf("User %02d", i), "", "test")
			errs <- err
		}()
	}

	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("UpsertUser concurrent error: %v", err)
		}
	}

	users, err := s.ListUsers()
	if err != nil {
		t.Fatalf("ListUsers(): %v", err)
	}
	if len(users) != totalUsers {
		t.Fatalf("ListUsers len = %d, want %d", len(users), totalUsers)
	}

	adminCount := 0
	for _, user := range users {
		if user.Role == "admin" {
			adminCount++
		}
	}
	if adminCount != 1 {
		t.Fatalf("admin user count = %d, want 1; users=%+v", adminCount, users)
	}
}

func TestPostgresStoreStatsAndSearchParityIntegration(t *testing.T) {
	s := newPostgresTestStore(t)
	now := time.Now().UTC()

	if err := s.CreateSession("stats-recent", "Lore", "/tmp/lore"); err != nil {
		t.Fatalf("CreateSession(stats-recent): %v", err)
	}
	if err := s.CreateSession("stats-old", "Lore", "/tmp/lore-old"); err != nil {
		t.Fatalf("CreateSession(stats-old): %v", err)
	}
	if _, err := s.db.Exec(`UPDATE sessions SET started_at = $1 WHERE id = $2`, now.Add(-8*24*time.Hour).Format("2006-01-02 15:04:05"), "stats-old"); err != nil {
		t.Fatalf("age old session: %v", err)
	}

	stack, err := s.CreateStack("go", "Go")
	if err != nil {
		t.Fatalf("CreateStack(): %v", err)
	}
	category, err := s.CreateCategory("backend", "Backend")
	if err != nil {
		t.Fatalf("CreateCategory(): %v", err)
	}

	created, err := s.CreateSkill(CreateSkillParams{
		Name:         "postgres-search",
		DisplayName:  "Postgres Search",
		StackIDs:     []int64{stack.ID},
		CategoryIDs:  []int64{category.ID},
		Triggers:     "postgres search parity",
		Content:      "Search inclusion over postgres content",
		CompactRules: "Prefer deterministic inclusion checks",
		ChangedBy:    "slice-c",
	})
	if err != nil {
		t.Fatalf("CreateSkill(): %v", err)
	}
	if _, err := s.CreateSkill(CreateSkillParams{
		Name:         "inactive-skill",
		DisplayName:  "Inactive Skill",
		Triggers:     "archived",
		Content:      "should disappear from active lists",
		CompactRules: "",
		ChangedBy:    "slice-c",
	}); err != nil {
		t.Fatalf("CreateSkill(inactive): %v", err)
	}
	if err := s.DeleteSkill("inactive-skill", "slice-c"); err != nil {
		t.Fatalf("DeleteSkill(inactive): %v", err)
	}

	updatedContent := "Search inclusion over postgres content and admin stats"
	updatedRules := "Prefer inclusion/filter checks over rank lockstep"
	updated, err := s.UpdateSkill("postgres-search", UpdateSkillParams{
		Content:      &updatedContent,
		CompactRules: &updatedRules,
		ChangedBy:    "slice-c",
	})
	if err != nil {
		t.Fatalf("UpdateSkill(): %v", err)
	}
	if updated.Version != 2 {
		t.Fatalf("updated version = %d, want 2", updated.Version)
	}

	var versions int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM skill_versions WHERE skill_id = $1`, created.ID).Scan(&versions); err != nil {
		t.Fatalf("count skill_versions: %v", err)
	}
	if versions != 2 {
		t.Fatalf("skill_versions count = %d, want 2", versions)
	}

	searchResults, err := s.ListSkills(ListSkillsParams{Query: "postgres admin stats"})
	if err != nil {
		t.Fatalf("ListSkills(search): %v", err)
	}
	if len(searchResults) != 1 || searchResults[0].Name != "postgres-search" {
		t.Fatalf("ListSkills(search) = %+v, want only postgres-search", searchResults)
	}

	if err := s.DeleteStack(stack.ID); err != nil {
		t.Fatalf("DeleteStack(): %v", err)
	}
	if err := s.DeleteCategory(category.ID); err != nil {
		t.Fatalf("DeleteCategory(): %v", err)
	}

	reloaded, err := s.GetSkill("postgres-search")
	if err != nil {
		t.Fatalf("GetSkill(after cascades): %v", err)
	}
	if len(reloaded.Stacks) != 0 || len(reloaded.Categories) != 0 {
		t.Fatalf("GetSkill relationships after cascades = stacks:%+v categories:%+v, want both empty", reloaded.Stacks, reloaded.Categories)
	}

	obsID, err := s.AddObservation(AddObservationParams{
		SessionID: "stats-recent",
		Type:      "decision",
		Title:     "fresh",
		Content:   "fresh content",
		Project:   "Lore",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("AddObservation(fresh): %v", err)
	}
	if _, err := s.AddObservation(AddObservationParams{
		SessionID: "stats-recent",
		Type:      "decision",
		Title:     "active project",
		Content:   "counts toward stats",
		Project:   "Lore-Admin",
		Scope:     "project",
	}); err != nil {
		t.Fatalf("AddObservation(second project): %v", err)
	}
	if _, err := s.AddObservation(AddObservationParams{
		SessionID: "stats-old",
		Type:      "decision",
		Title:     "old observation",
		Content:   "should fall out of weekly count",
		Project:   "Lore",
		Scope:     "project",
	}); err != nil {
		t.Fatalf("AddObservation(old): %v", err)
	}
	if _, err := s.db.Exec(`UPDATE observations SET created_at = $1, updated_at = $1 WHERE title = $2`, now.Add(-8*24*time.Hour).Format("2006-01-02 15:04:05"), "old observation"); err != nil {
		t.Fatalf("age old observation: %v", err)
	}
	if err := s.DeleteObservation(obsID, false); err != nil {
		t.Fatalf("DeleteObservation(fresh): %v", err)
	}

	stats, err := s.AdminStats()
	if err != nil {
		t.Fatalf("AdminStats(): %v", err)
	}
	if stats.ActiveProjects != 2 {
		t.Fatalf("ActiveProjects = %d, want 2", stats.ActiveProjects)
	}
	if stats.ActiveSkills != 1 {
		t.Fatalf("ActiveSkills = %d, want 1", stats.ActiveSkills)
	}
	if stats.ObservationsThisWeek != 1 {
		t.Fatalf("ObservationsThisWeek = %d, want 1", stats.ObservationsThisWeek)
	}
	if stats.SessionsThisWeek != 1 {
		t.Fatalf("SessionsThisWeek = %d, want 1", stats.SessionsThisWeek)
	}

	if err := s.DeleteSkill("postgres-search", "slice-c"); err != nil {
		t.Fatalf("DeleteSkill(postgres-search): %v", err)
	}
	searchResults, err = s.ListSkills(ListSkillsParams{Query: "postgres admin stats"})
	if err != nil {
		t.Fatalf("ListSkills(search after delete): %v", err)
	}
	if len(searchResults) != 0 {
		t.Fatalf("ListSkills(search after delete) = %+v, want empty", searchResults)
	}

	var isActive bool
	if err := s.db.QueryRow(`SELECT is_active FROM skills WHERE name = $1`, "postgres-search").Scan(&isActive); err != nil {
		t.Fatalf("load soft-deleted skill row: %v", err)
	}
	if isActive {
		t.Fatal("expected soft-deleted skill row to remain with is_active=false")
	}
}

func TestPostgresStoreUpdateSkillSerializesConcurrentVersionHistory(t *testing.T) {
	s := newPostgresTestStore(t)

	created, err := s.CreateSkill(CreateSkillParams{
		Name:         "concurrent-version-skill",
		DisplayName:  "Concurrent Version Skill",
		Triggers:     "concurrent update",
		Content:      "initial content",
		CompactRules: "initial rules",
		ChangedBy:    "test",
	})
	if err != nil {
		t.Fatalf("CreateSkill(): %v", err)
	}

	const updates = 10
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, updates)

	for i := 0; i < updates; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			content := fmt.Sprintf("updated content %02d", i)
			_, err := s.UpdateSkill("concurrent-version-skill", UpdateSkillParams{
				Content:   &content,
				ChangedBy: "test",
			})
			errs <- err
		}()
	}

	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("UpdateSkill concurrent error: %v", err)
		}
	}

	reloaded, err := s.GetSkill("concurrent-version-skill")
	if err != nil {
		t.Fatalf("GetSkill(): %v", err)
	}
	if reloaded.Version != updates+1 {
		t.Fatalf("skill version = %d, want %d", reloaded.Version, updates+1)
	}

	var versionRows int
	var distinctVersions int
	if err := s.db.QueryRow(`SELECT COUNT(*), COUNT(DISTINCT version) FROM skill_versions WHERE skill_id = $1`, created.ID).Scan(&versionRows, &distinctVersions); err != nil {
		t.Fatalf("count skill_versions: %v", err)
	}
	if versionRows != updates+1 || distinctVersions != updates+1 {
		t.Fatalf("skill_versions rows=%d distinct_versions=%d, want %d", versionRows, distinctVersions, updates+1)
	}
}

var _ = sql.ErrNoRows
