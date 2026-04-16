package admin

// projects_test.go — TDD tests for handleListProjects and handleGetProject.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"

	"github.com/alferio94/lore/internal/store"
)

// ─── helpers (local to projects_test) ────────────────────────────────────────

// newProjectsTestStore returns a real in-memory store pre-seeded with projects.
func newProjectsTestStore(t *testing.T) *store.Store {
	t.Helper()
	cfg, err := store.DefaultConfig()
	if err != nil {
		t.Fatalf("DefaultConfig: %v", err)
	}
	cfg.DataDir = t.TempDir()
	cfg.DedupeWindow = time.Hour

	s, err := store.New(cfg)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// seedProjectSession creates a session + observations for a named project so
// ListProjectsWithStats returns useful results.
func seedProjectSession(t *testing.T, s *store.Store, project, sessionID string, obsCount int) {
	t.Helper()
	if err := s.CreateSession(sessionID, project, "/work/"+project); err != nil {
		t.Fatalf("CreateSession %q: %v", project, err)
	}
	for i := 0; i < obsCount; i++ {
		_, err := s.AddObservation(store.AddObservationParams{
			SessionID: sessionID,
			Type:      "decision",
			Title:     fmt.Sprintf("obs-%d", i),
			Content:   fmt.Sprintf("content for %s obs %d", project, i),
			Project:   project,
			Scope:     "project",
		})
		if err != nil {
			t.Fatalf("AddObservation %q[%d]: %v", project, i, err)
		}
	}
}

// makeViewerCookieForProjects builds a valid viewer JWT cookie for project tests.
func makeViewerCookieForProjects(t *testing.T, secret []byte) *http.Cookie {
	t.Helper()
	c := Claims{
		RegisteredClaims: jwtlib.RegisteredClaims{
			Subject:   "1",
			ExpiresAt: jwtlib.NewNumericDate(time.Now().Add(24 * time.Hour)),
		},
		Email: "viewer@test.com",
		Name:  "Viewer",
		Role:  "viewer",
	}
	tok := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, c)
	s, err := tok.SignedString(secret)
	if err != nil {
		t.Fatalf("makeViewerCookieForProjects: %v", err)
	}
	return &http.Cookie{Name: sessionCookieName, Value: s}
}

// ─── Task 5.1 RED — handleListProjects ───────────────────────────────────────

func TestHandleListProjects_200WithObservationCount(t *testing.T) {
	secret := []byte("projects-test-secret-32-bytes!!!")
	s := newProjectsTestStore(t)
	seedProjectSession(t, s, "proj-alpha", "s-alpha", 3)
	seedProjectSession(t, s, "proj-beta", "s-beta", 1)

	h := &adminHandler{cfg: AdminConfig{
		Store:     s,
		JWTSecret: secret,
	}}

	req := newTestRequest(t, "GET", "/admin/api/projects", nil)
	req.AddCookie(makeViewerCookieForProjects(t, secret))
	w := newTestResponseRecorder()
	h.handleListProjects(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body: %s", w.Code, w.Body.String())
	}

	var projects []store.ProjectStats
	if err := json.NewDecoder(w.Body).Decode(&projects); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(projects) < 2 {
		t.Fatalf("expected at least 2 projects, got %d", len(projects))
	}

	// Build a map for easy lookup
	pm := make(map[string]store.ProjectStats)
	for _, p := range projects {
		pm[p.Name] = p
	}

	if p, ok := pm["proj-alpha"]; !ok {
		t.Error("proj-alpha not in response")
	} else if p.ObservationCount != 3 {
		t.Errorf("proj-alpha observation_count: got %d, want 3", p.ObservationCount)
	}

	if p, ok := pm["proj-beta"]; !ok {
		t.Error("proj-beta not in response")
	} else if p.ObservationCount != 1 {
		t.Errorf("proj-beta observation_count: got %d, want 1", p.ObservationCount)
	}
}

// ─── Task 5.1 RED — handleGetProject ──────────────────────────────────────────

func TestHandleGetProject_200ReturnsProject(t *testing.T) {
	secret := []byte("projects-test-secret-32-bytes!!!")
	s := newProjectsTestStore(t)
	seedProjectSession(t, s, "my-project", "sess-1", 5)

	h := &adminHandler{cfg: AdminConfig{
		Store:     s,
		JWTSecret: secret,
	}}

	req := newTestRequest(t, "GET", "/admin/api/projects/my-project", nil)
	req = withPathValue(req, "name", "my-project")
	req.AddCookie(makeViewerCookieForProjects(t, secret))
	w := newTestResponseRecorder()
	h.handleGetProject(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body: %s", w.Code, w.Body.String())
	}

	var project store.ProjectStats
	if err := json.NewDecoder(w.Body).Decode(&project); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if project.Name != "my-project" {
		t.Errorf("name: got %q, want %q", project.Name, "my-project")
	}
	if project.ObservationCount != 5 {
		t.Errorf("observation_count: got %d, want 5", project.ObservationCount)
	}
}

func TestHandleGetProject_404UnknownProject(t *testing.T) {
	secret := []byte("projects-test-secret-32-bytes!!!")
	s := newProjectsTestStore(t)
	// No projects seeded — "ghost" does not exist

	h := &adminHandler{cfg: AdminConfig{
		Store:     s,
		JWTSecret: secret,
	}}

	req := newTestRequest(t, "GET", "/admin/api/projects/ghost", nil)
	req = withPathValue(req, "name", "ghost")
	req.AddCookie(makeViewerCookieForProjects(t, secret))
	w := newTestResponseRecorder()
	h.handleGetProject(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want 404; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if resp["error"] != "not_found" {
		t.Errorf("error field: got %q, want %q", resp["error"], "not_found")
	}
}
