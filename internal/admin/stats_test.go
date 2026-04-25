package admin

// stats_test.go — TDD tests for handleStats.

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"

	"github.com/alferio94/lore/internal/store"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

// newStatsTestStore returns a real in-memory store for stats handler tests.
func newStatsTestStore(t *testing.T) *store.Store {
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

// makeStatsCfg returns an AdminConfig wired to the given store.
func makeStatsCfg(s *store.Store) AdminConfig {
	return AdminConfig{
		Store:     s,
		JWTSecret: []byte("stats-test-secret-32-bytes-long!!"),
		DevAuth:   false,
	}
}

// makeStatsCookie creates a valid JWT cookie for the given role.
func makeStatsCookie(t *testing.T, cfg AdminConfig, role string) *http.Cookie {
	t.Helper()
	c := Claims{
		RegisteredClaims: jwtlib.RegisteredClaims{
			Subject:   "1",
			ExpiresAt: jwtlib.NewNumericDate(time.Now().Add(24 * time.Hour)),
		},
		Email: "user@example.com",
		Name:  "Test User",
		Role:  role,
	}
	tok := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, c)
	s, err := tok.SignedString(cfg.JWTSecret)
	if err != nil {
		t.Fatalf("makeStatsCookie sign: %v", err)
	}
	return &http.Cookie{Name: sessionCookieName, Value: s}
}

// ─── Tests ────────────────────────────────────────────────────────────────────

func TestHandleStats_200WithValidViewer(t *testing.T) {
	s := newStatsTestStore(t)
	cfg := makeStatsCfg(s)
	h := &adminHandler{cfg: cfg}

	req := newTestRequest(t, "GET", "/admin/api/stats", nil)
	req.AddCookie(makeStatsCookie(t, cfg, "viewer"))
	w := newTestResponseRecorder()
	h.handleStats(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp store.AdminStats
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// Empty DB — all fields must be zero (non-negative integers)
	if resp.ActiveProjects < 0 {
		t.Errorf("active_projects: got %d, want >= 0", resp.ActiveProjects)
	}
	if resp.ActiveSkills < 0 {
		t.Errorf("active_skills: got %d, want >= 0", resp.ActiveSkills)
	}
	if resp.ObservationsThisWeek < 0 {
		t.Errorf("observations_this_week: got %d, want >= 0", resp.ObservationsThisWeek)
	}
	if resp.SessionsThisWeek < 0 {
		t.Errorf("sessions_this_week: got %d, want >= 0", resp.SessionsThisWeek)
	}
}

func TestHandleStats_200WithCorrectShape(t *testing.T) {
	s := newStatsTestStore(t)
	cfg := makeStatsCfg(s)
	h := &adminHandler{cfg: cfg}

	// Seed known data
	if err := s.CreateSession("sess-stats", "proj-a", "/work"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := s.AddObservation(store.AddObservationParams{
		SessionID: "sess-stats",
		Type:      "decision",
		Title:     "a decision",
		Content:   "content",
		Project:   "proj-a",
		Scope:     "project",
	}); err != nil {
		t.Fatalf("AddObservation: %v", err)
	}

	req := newTestRequest(t, "GET", "/admin/api/stats", nil)
	req.AddCookie(makeStatsCookie(t, cfg, "viewer"))
	w := newTestResponseRecorder()
	h.handleStats(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body: %s", w.Code, w.Body.String())
	}

	// Decode to raw map to verify JSON key names
	var raw map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&raw); err != nil {
		t.Fatalf("decode raw response: %v", err)
	}

	for _, key := range []string{"active_projects", "active_skills", "observations_this_week", "sessions_this_week"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("response missing key %q", key)
		}
	}

	if raw["active_projects"].(float64) != 1 {
		t.Errorf("active_projects: got %v, want 1", raw["active_projects"])
	}
}

// TestHandleStats_401Unauthenticated verifies that the requireRole middleware
// rejects requests without a session cookie with 401.
func TestHandleStats_401Unauthenticated(t *testing.T) {
	s := newStatsTestStore(t)
	cfg := makeStatsCfg(s)
	h := &adminHandler{cfg: cfg}

	// Wire via requireRole as done in Mount()
	handler := requireRole(cfg.JWTSecret, store.UserRoleDeveloper, http.HandlerFunc(h.handleStats))

	req := newTestRequest(t, "GET", "/admin/api/stats", nil)
	// No cookie
	w := newTestResponseRecorder()
	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401; body: %s", w.Code, w.Body.String())
	}
}

// TestHandleStats_403NoRole verifies that a valid session with no recognized role
// is rejected with 403.
func TestHandleStats_403NoRole(t *testing.T) {
	s := newStatsTestStore(t)
	cfg := makeStatsCfg(s)
	h := &adminHandler{cfg: cfg}

	handler := requireRole(cfg.JWTSecret, store.UserRoleDeveloper, http.HandlerFunc(h.handleStats))

	req := newTestRequest(t, "GET", "/admin/api/stats", nil)
	// Empty role — not developer/tech_lead/admin
	req.AddCookie(makeStatsCookie(t, cfg, ""))
	w := newTestResponseRecorder()
	handler(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status: got %d, want 403; body: %s", w.Code, w.Body.String())
	}
}
