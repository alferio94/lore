package store

// store_admin_stats_test.go — Tests for AdminStats() method.

import (
	"fmt"
	"testing"
	"time"
)

// ─── Helpers ──────────────────────────────────────────────────────────────────

// seedObservationAt inserts an observation with a specific created_at timestamp.
func seedObservationAt(t *testing.T, s *Store, sessionID, project string, createdAt time.Time) {
	t.Helper()
	_, err := s.db.Exec(
		`INSERT INTO observations (session_id, type, title, content, project, scope, created_at, updated_at)
		 VALUES (?, 'decision', 'obs', 'content', ?, 'project', ?, ?)`,
		sessionID, project, createdAt.UTC().Format("2006-01-02 15:04:05"), createdAt.UTC().Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		t.Fatalf("seedObservationAt: %v", err)
	}
}

// seedSessionAt inserts a session with a specific started_at timestamp.
func seedSessionAt(t *testing.T, s *Store, sessionID, project string, startedAt time.Time) {
	t.Helper()
	_, err := s.db.Exec(
		`INSERT INTO sessions (id, project, directory, started_at) VALUES (?, ?, '/work', ?)`,
		sessionID, project, startedAt.UTC().Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		t.Fatalf("seedSessionAt: %v", err)
	}
}

// ─── Tests ────────────────────────────────────────────────────────────────────

func TestAdminStats_EmptyDatabase(t *testing.T) {
	s := newTestStore(t)

	stats, err := s.AdminStats()
	if err != nil {
		t.Fatalf("AdminStats on empty DB: %v", err)
	}

	if stats.ActiveProjects != 0 {
		t.Errorf("ActiveProjects: got %d, want 0", stats.ActiveProjects)
	}
	if stats.ActiveSkills != 0 {
		t.Errorf("ActiveSkills: got %d, want 0", stats.ActiveSkills)
	}
	if stats.ObservationsThisWeek != 0 {
		t.Errorf("ObservationsThisWeek: got %d, want 0", stats.ObservationsThisWeek)
	}
	if stats.SessionsThisWeek != 0 {
		t.Errorf("SessionsThisWeek: got %d, want 0", stats.SessionsThisWeek)
	}
}

func TestAdminStats_ActiveProjects(t *testing.T) {
	s := newTestStore(t)

	// Create sessions for projects
	for _, proj := range []string{"alpha", "beta", "gamma"} {
		if err := s.CreateSession("sess-"+proj, proj, "/work/"+proj); err != nil {
			t.Fatalf("CreateSession %q: %v", proj, err)
		}
		if _, err := s.AddObservation(AddObservationParams{
			SessionID: "sess-" + proj,
			Type:      "decision",
			Title:     "obs",
			Content:   "content",
			Project:   proj,
			Scope:     "project",
		}); err != nil {
			t.Fatalf("AddObservation %q: %v", proj, err)
		}
	}

	stats, err := s.AdminStats()
	if err != nil {
		t.Fatalf("AdminStats: %v", err)
	}

	if stats.ActiveProjects != 3 {
		t.Errorf("ActiveProjects: got %d, want 3", stats.ActiveProjects)
	}
}

func TestAdminStats_ActiveSkills(t *testing.T) {
	s := newTestStore(t)

	// Create 2 active + 1 deleted skill
	for i := 1; i <= 3; i++ {
		sk, err := s.CreateSkill(CreateSkillParams{
			Name:        fmt.Sprintf("skill-%d", i),
			DisplayName: fmt.Sprintf("Skill %d", i),
			Category:    "test",
			Stack:       "go",
			Triggers:    "trigger",
			Content:     "content",
			ChangedBy:   "test",
		})
		if err != nil {
			t.Fatalf("CreateSkill %d: %v", i, err)
		}
		if i == 3 {
			if err := s.DeleteSkill(sk.Name, "test"); err != nil {
				t.Fatalf("DeleteSkill: %v", err)
			}
		}
	}

	stats, err := s.AdminStats()
	if err != nil {
		t.Fatalf("AdminStats: %v", err)
	}

	if stats.ActiveSkills != 2 {
		t.Errorf("ActiveSkills: got %d, want 2", stats.ActiveSkills)
	}
}

func TestAdminStats_ObservationsThisWeek(t *testing.T) {
	s := newTestStore(t)
	now := time.Now().UTC()

	// Need a session first
	if err := s.CreateSession("sess-week", "proj", "/work"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Row at exactly -7 days (must be INCLUDED)
	seedObservationAt(t, s, "sess-week", "proj", now.Add(-7*24*time.Hour))

	// Row at -8 days (must be EXCLUDED)
	seedObservationAt(t, s, "sess-week", "proj", now.Add(-8*24*time.Hour))

	// Row from today (included)
	seedObservationAt(t, s, "sess-week", "proj", now)

	stats, err := s.AdminStats()
	if err != nil {
		t.Fatalf("AdminStats: %v", err)
	}

	// Expect 2: the -7d row and the today row; -8d row is excluded
	if stats.ObservationsThisWeek != 2 {
		t.Errorf("ObservationsThisWeek: got %d, want 2", stats.ObservationsThisWeek)
	}
}

func TestAdminStats_SessionsThisWeek(t *testing.T) {
	s := newTestStore(t)
	now := time.Now().UTC()

	// Session within the last 7 days
	seedSessionAt(t, s, "sess-recent", "proj", now.Add(-3*24*time.Hour))

	// Session older than 7 days
	seedSessionAt(t, s, "sess-old", "proj", now.Add(-8*24*time.Hour))

	stats, err := s.AdminStats()
	if err != nil {
		t.Fatalf("AdminStats: %v", err)
	}

	if stats.SessionsThisWeek != 1 {
		t.Errorf("SessionsThisWeek: got %d, want 1", stats.SessionsThisWeek)
	}
}

func TestAdminStats_DeletedObservationsExcluded(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSession("sess-del", "proj", "/work"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Add and then soft-delete an observation
	id, err := s.AddObservation(AddObservationParams{
		SessionID: "sess-del",
		Type:      "decision",
		Title:     "to-delete",
		Content:   "content",
		Project:   "proj",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}
	if err := s.DeleteObservation(id, false); err != nil {
		t.Fatalf("DeleteObservation: %v", err)
	}

	stats, err := s.AdminStats()
	if err != nil {
		t.Fatalf("AdminStats: %v", err)
	}

	if stats.ObservationsThisWeek != 0 {
		t.Errorf("ObservationsThisWeek: got %d, want 0 (deleted obs should be excluded)", stats.ObservationsThisWeek)
	}
}
