package admin

// skills_test.go — TDD tests for admin skills API handlers.
// Phase 4: Skills API — tasks 4.1, 4.3, 4.5, 4.7 (RED phases)

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"

	"github.com/alferio94/lore/internal/store"
)

// ─── Test helpers ─────────────────────────────────────────────────────────────

// newTestStoreForAdmin creates a real store with a temp dir for handler tests.
func newTestStoreForAdmin(t *testing.T) *store.Store {
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

// makeSkillsConfig returns an AdminConfig wired to the given store.
func makeSkillsConfig(s *store.Store) AdminConfig {
	return AdminConfig{
		Store:     s,
		JWTSecret: []byte("skills-test-secret-32-bytes-long!"),
		DevAuth:   false,
	}
}

// makeSkillsCookie creates a valid JWT cookie for the given role.
func makeSkillsCookie(t *testing.T, cfg AdminConfig, role string) *http.Cookie {
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
		t.Fatalf("makeSkillsCookie sign: %v", err)
	}
	return &http.Cookie{Name: sessionCookieName, Value: s}
}

// seedSkill creates a skill in the store for test setup.
func seedSkill(t *testing.T, s *store.Store, name string) *store.Skill {
	t.Helper()
	sk, err := s.CreateSkill(store.CreateSkillParams{
		Name:        name,
		DisplayName: name,
		Category:    "test",
		Stack:       "go",
		Triggers:    "trigger",
		Content:     "content for " + name,
		ChangedBy:   "test",
	})
	if err != nil {
		t.Fatalf("seedSkill %q: %v", name, err)
	}
	return sk
}

// ─── Task 4.1 RED: handleListSkills + handleGetSkill ─────────────────────────

func TestHandleListSkillsReturns200WithArray(t *testing.T) {
	s := newTestStoreForAdmin(t)
	seedSkill(t, s, "skill-alpha")
	seedSkill(t, s, "skill-beta")

	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	req := newTestRequest(t, "GET", "/admin/api/skills", nil)
	req.AddCookie(makeSkillsCookie(t, cfg, "viewer"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "viewer", h.handleListSkills)
	authed(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", w.Code)
	}

	var skills []store.Skill
	if err := json.NewDecoder(w.Body).Decode(&skills); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(skills) != 2 {
		t.Errorf("skills count: got %d, want 2", len(skills))
	}

	// Verify we got the right skills
	names := map[string]bool{}
	for _, sk := range skills {
		names[sk.Name] = true
	}
	if !names["skill-alpha"] {
		t.Error("expected skill-alpha in response")
	}
	if !names["skill-beta"] {
		t.Error("expected skill-beta in response")
	}
}

func TestHandleListSkillsReturnsEmptyArrayWhenNoSkills(t *testing.T) {
	s := newTestStoreForAdmin(t)
	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	req := newTestRequest(t, "GET", "/admin/api/skills", nil)
	req.AddCookie(makeSkillsCookie(t, cfg, "viewer"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "viewer", h.handleListSkills)
	authed(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", w.Code)
	}

	var skills []store.Skill
	if err := json.NewDecoder(w.Body).Decode(&skills); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(skills) != 0 {
		t.Errorf("expected empty array, got %d skills", len(skills))
	}
}

func TestHandleGetSkillReturns200WithSkillObject(t *testing.T) {
	s := newTestStoreForAdmin(t)
	seedSkill(t, s, "my-skill")

	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	req := newTestRequest(t, "GET", "/admin/api/skills/my-skill", nil)
	req = withPathValue(req, "name", "my-skill")
	req.AddCookie(makeSkillsCookie(t, cfg, "viewer"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "viewer", h.handleGetSkill)
	authed(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", w.Code)
	}

	var sk store.Skill
	if err := json.NewDecoder(w.Body).Decode(&sk); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if sk.Name != "my-skill" {
		t.Errorf("name: got %q, want %q", sk.Name, "my-skill")
	}
}

func TestHandleGetSkillReturns404OnUnknownName(t *testing.T) {
	s := newTestStoreForAdmin(t)
	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	req := newTestRequest(t, "GET", "/admin/api/skills/ghost", nil)
	req = withPathValue(req, "name", "ghost")
	req.AddCookie(makeSkillsCookie(t, cfg, "viewer"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "viewer", h.handleGetSkill)
	authed(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", w.Code)
	}
}

// ─── Task 4.3 RED: handleCreateSkill ─────────────────────────────────────────

func TestHandleCreateSkillReturns201OnValidBody(t *testing.T) {
	s := newTestStoreForAdmin(t)
	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	body := strings.NewReader(`{"name":"new-skill","content":"some content"}`)
	req := newTestRequest(t, "POST", "/admin/api/skills", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(makeSkillsCookie(t, cfg, "tech_lead"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "tech_lead", h.handleCreateSkill)
	authed(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status: got %d, want 201 (body: %s)", w.Code, w.Body.String())
	}

	var sk store.Skill
	if err := json.NewDecoder(w.Body).Decode(&sk); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if sk.Name != "new-skill" {
		t.Errorf("name: got %q, want %q", sk.Name, "new-skill")
	}
	if sk.Content != "some content" {
		t.Errorf("content: got %q, want %q", sk.Content, "some content")
	}
}

func TestHandleCreateSkillReturns422OnMissingContent(t *testing.T) {
	s := newTestStoreForAdmin(t)
	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	body := strings.NewReader(`{"name":"no-content-skill"}`)
	req := newTestRequest(t, "POST", "/admin/api/skills", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(makeSkillsCookie(t, cfg, "tech_lead"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "tech_lead", h.handleCreateSkill)
	authed(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status: got %d, want 422", w.Code)
	}
}

func TestHandleCreateSkillReturns409OnDuplicateName(t *testing.T) {
	s := newTestStoreForAdmin(t)
	seedSkill(t, s, "existing-skill")

	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	body := strings.NewReader(`{"name":"existing-skill","content":"some content"}`)
	req := newTestRequest(t, "POST", "/admin/api/skills", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(makeSkillsCookie(t, cfg, "tech_lead"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "tech_lead", h.handleCreateSkill)
	authed(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("status: got %d, want 409", w.Code)
	}
}

func TestHandleCreateSkillReturns403ForViewer(t *testing.T) {
	s := newTestStoreForAdmin(t)
	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	body := strings.NewReader(`{"name":"viewer-skill","content":"content"}`)
	req := newTestRequest(t, "POST", "/admin/api/skills", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(makeSkillsCookie(t, cfg, "viewer"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "tech_lead", h.handleCreateSkill)
	authed(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want 403", w.Code)
	}
}

// ─── Task 4.5 RED: handleUpdateSkill ─────────────────────────────────────────

func TestHandleUpdateSkillReturns200OnValidUpdate(t *testing.T) {
	s := newTestStoreForAdmin(t)
	seedSkill(t, s, "to-update")

	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	body := strings.NewReader(`{"content":"updated content"}`)
	req := newTestRequest(t, "PUT", "/admin/api/skills/to-update", body)
	req = withPathValue(req, "name", "to-update")
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(makeSkillsCookie(t, cfg, "tech_lead"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "tech_lead", h.handleUpdateSkill)
	authed(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200 (body: %s)", w.Code, w.Body.String())
	}

	var sk store.Skill
	if err := json.NewDecoder(w.Body).Decode(&sk); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if sk.Content != "updated content" {
		t.Errorf("content: got %q, want %q", sk.Content, "updated content")
	}
}

func TestHandleUpdateSkillReturns404OnUnknownName(t *testing.T) {
	s := newTestStoreForAdmin(t)
	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	body := strings.NewReader(`{"content":"something"}`)
	req := newTestRequest(t, "PUT", "/admin/api/skills/nonexistent", body)
	req = withPathValue(req, "name", "nonexistent")
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(makeSkillsCookie(t, cfg, "tech_lead"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "tech_lead", h.handleUpdateSkill)
	authed(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", w.Code)
	}
}

func TestHandleUpdateSkillReturns422OnEmptyBody(t *testing.T) {
	s := newTestStoreForAdmin(t)
	seedSkill(t, s, "update-422-skill")

	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	// Empty JSON object = no fields to update — should be 422
	body := strings.NewReader(`{}`)
	req := newTestRequest(t, "PUT", "/admin/api/skills/update-422-skill", body)
	req = withPathValue(req, "name", "update-422-skill")
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(makeSkillsCookie(t, cfg, "tech_lead"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "tech_lead", h.handleUpdateSkill)
	authed(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status: got %d, want 422", w.Code)
	}
}

func TestHandleUpdateSkillReturns403ForViewer(t *testing.T) {
	s := newTestStoreForAdmin(t)
	seedSkill(t, s, "viewer-update-skill")

	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	body := strings.NewReader(`{"content":"content"}`)
	req := newTestRequest(t, "PUT", "/admin/api/skills/viewer-update-skill", body)
	req = withPathValue(req, "name", "viewer-update-skill")
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(makeSkillsCookie(t, cfg, "viewer"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "tech_lead", h.handleUpdateSkill)
	authed(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want 403", w.Code)
	}
}

// ─── Task 4.7 RED: handleDeleteSkill ─────────────────────────────────────────

func TestHandleDeleteSkillReturns204OnSuccess(t *testing.T) {
	s := newTestStoreForAdmin(t)
	seedSkill(t, s, "to-delete")

	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	req := newTestRequest(t, "DELETE", "/admin/api/skills/to-delete", nil)
	req = withPathValue(req, "name", "to-delete")
	req.AddCookie(makeSkillsCookie(t, cfg, "admin"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "admin", h.handleDeleteSkill)
	authed(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status: got %d, want 204", w.Code)
	}

	// Confirm skill is soft-deleted (not in list)
	skills, err := s.ListSkills(store.ListSkillsParams{})
	if err != nil {
		t.Fatalf("ListSkills after delete: %v", err)
	}
	for _, sk := range skills {
		if sk.Name == "to-delete" {
			t.Error("expected deleted skill to be absent from list, but it was found")
		}
	}
}

func TestHandleDeleteSkillReturns404OnUnknown(t *testing.T) {
	s := newTestStoreForAdmin(t)
	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	req := newTestRequest(t, "DELETE", "/admin/api/skills/ghost", nil)
	req = withPathValue(req, "name", "ghost")
	req.AddCookie(makeSkillsCookie(t, cfg, "admin"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "admin", h.handleDeleteSkill)
	authed(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", w.Code)
	}
}

func TestHandleDeleteSkillReturns403ForTechLead(t *testing.T) {
	s := newTestStoreForAdmin(t)
	seedSkill(t, s, "tech-lead-delete-skill")

	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	req := newTestRequest(t, "DELETE", "/admin/api/skills/tech-lead-delete-skill", nil)
	req = withPathValue(req, "name", "tech-lead-delete-skill")
	req.AddCookie(makeSkillsCookie(t, cfg, "tech_lead"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "admin", h.handleDeleteSkill)
	authed(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want 403", w.Code)
	}
}

// Sentinel to use sql.ErrNoRows in the handler — keep compiler happy
var _ = sql.ErrNoRows
