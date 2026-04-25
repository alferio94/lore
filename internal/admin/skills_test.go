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
		Triggers:    "trigger",
		Content:     "content for " + name,
		ChangedBy:   "test",
	})
	if err != nil {
		t.Fatalf("seedSkill %q: %v", name, err)
	}
	return sk
}

func seedInactiveSkill(t *testing.T, s *store.Store, name string) *store.Skill {
	t.Helper()
	seedSkill(t, s, name)
	if err := s.DeleteSkill(name, "reviewer@example.com"); err != nil {
		t.Fatalf("DeleteSkill %q: %v", name, err)
	}
	auditSkill, err := s.GetSkillForAudit(name)
	if err != nil {
		t.Fatalf("GetSkillForAudit %q: %v", name, err)
	}
	return auditSkill
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

func TestHandleListSkillsReturnsAuditVisibleInactiveSkills(t *testing.T) {
	s := newTestStoreForAdmin(t)
	seedSkill(t, s, "approved-skill")
	inactive := seedInactiveSkill(t, s, "inactive-skill")

	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	req := newTestRequest(t, "GET", "/admin/api/skills", nil)
	req.AddCookie(makeSkillsCookie(t, cfg, "viewer"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "viewer", h.handleListSkills)
	authed(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}

	var skills []store.Skill
	if err := json.NewDecoder(w.Body).Decode(&skills); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(skills) != 2 {
		t.Fatalf("skills count: got %d, want 2", len(skills))
	}

	var foundInactive *store.Skill
	for i := range skills {
		if skills[i].Name == inactive.Name {
			foundInactive = &skills[i]
			break
		}
	}
	if foundInactive == nil {
		t.Fatalf("expected inactive skill %q in audit list", inactive.Name)
	}
	if foundInactive.IsActive {
		t.Fatalf("expected inactive skill %q to remain inactive in audit list", inactive.Name)
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

func TestHandleGetSkillReturnsInactiveSkillViaAuditRead(t *testing.T) {
	s := newTestStoreForAdmin(t)
	inactive := seedInactiveSkill(t, s, "inactive-detail")

	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	req := newTestRequest(t, "GET", "/admin/api/skills/inactive-detail", nil)
	req = withPathValue(req, "name", "inactive-detail")
	req.AddCookie(makeSkillsCookie(t, cfg, "viewer"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "viewer", h.handleGetSkill)
	authed(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (body: %s)", w.Code, w.Body.String())
	}

	var got store.Skill
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Name != inactive.Name {
		t.Fatalf("name: got %q, want %q", got.Name, inactive.Name)
	}
	if got.IsActive {
		t.Fatalf("expected inactive audit read for %q", inactive.Name)
	}
	if got.ReviewState != store.SkillReviewStateApproved {
		t.Fatalf("review_state: got %q, want %q", got.ReviewState, store.SkillReviewStateApproved)
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

// ─── Task 3.9 RED: createSkill/updateSkill with stack_ids/category_ids ───────

// TestHandleCreateSkillWithStackIDsReturns201 verifies that POST /admin/api/skills
// correctly accepts stack_ids and category_ids arrays and persists relationships.
func TestHandleCreateSkillWithStackIDsReturns201(t *testing.T) {
	s := newTestStoreForAdmin(t)

	// Create catalog entries first
	angular, err := s.CreateStack("angular", "Angular")
	if err != nil {
		t.Fatalf("CreateStack: %v", err)
	}
	tutorial, err := s.CreateCategory("tutorial", "Tutorial")
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}

	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	body := strings.NewReader(`{
		"name": "angular-intro",
		"content": "intro content",
		"stack_ids": [` + formatInt(angular.ID) + `],
		"category_ids": [` + formatInt(tutorial.ID) + `]
	}`)
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
	if sk.Name != "angular-intro" {
		t.Errorf("name: got %q, want %q", sk.Name, "angular-intro")
	}
	if len(sk.Stacks) != 1 {
		t.Errorf("stacks: got %d, want 1", len(sk.Stacks))
	} else if sk.Stacks[0].Name != "angular" {
		t.Errorf("stacks[0].name: got %q, want %q", sk.Stacks[0].Name, "angular")
	}
	if len(sk.Categories) != 1 {
		t.Errorf("categories: got %d, want 1", len(sk.Categories))
	} else if sk.Categories[0].Name != "tutorial" {
		t.Errorf("categories[0].name: got %q, want %q", sk.Categories[0].Name, "tutorial")
	}
}

// TestHandleUpdateSkillWithStackIDsReturns200 verifies that PUT /admin/api/skills/{name}
// correctly accepts stack_ids arrays and replaces existing relationships.
func TestHandleUpdateSkillWithStackIDsReturns200(t *testing.T) {
	s := newTestStoreForAdmin(t)

	// Create catalog entries
	angular, err := s.CreateStack("angular-upd", "Angular")
	if err != nil {
		t.Fatalf("CreateStack: %v", err)
	}
	nestjs, err := s.CreateStack("nestjs-upd", "NestJS")
	if err != nil {
		t.Fatalf("CreateStack nestjs: %v", err)
	}

	// Create skill with initial stack
	sk, err := s.CreateSkill(store.CreateSkillParams{
		Name:      "skill-to-update-stacks",
		Content:   "content",
		StackIDs:  []int64{angular.ID},
		ChangedBy: "test",
	})
	if err != nil {
		t.Fatalf("CreateSkill: %v", err)
	}

	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	// Update: replace stacks with [nestjs]
	body := strings.NewReader(`{"stack_ids": [` + formatInt(nestjs.ID) + `]}`)
	req := newTestRequest(t, "PUT", "/admin/api/skills/skill-to-update-stacks", body)
	req = withPathValue(req, "name", sk.Name)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(makeSkillsCookie(t, cfg, "tech_lead"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "tech_lead", h.handleUpdateSkill)
	authed(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200 (body: %s)", w.Code, w.Body.String())
	}

	var updated store.Skill
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(updated.Stacks) != 1 {
		t.Errorf("stacks: got %d, want 1", len(updated.Stacks))
	} else if updated.Stacks[0].Name != "nestjs-upd" {
		t.Errorf("stacks[0].name: got %q, want %q", updated.Stacks[0].Name, "nestjs-upd")
	}
}

// TestHandleCreateSkillResponseHasEmptyArraysForStacks verifies that a skill
// created without stack_ids/category_ids returns empty arrays (not null).
func TestHandleCreateSkillResponseHasEmptyArraysForStacks(t *testing.T) {
	s := newTestStoreForAdmin(t)
	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	body := strings.NewReader(`{"name":"plain-skill","content":"plain content"}`)
	req := newTestRequest(t, "POST", "/admin/api/skills", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(makeSkillsCookie(t, cfg, "tech_lead"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "tech_lead", h.handleCreateSkill)
	authed(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status: got %d, want 201 (body: %s)", w.Code, w.Body.String())
	}

	// Decode as raw JSON to check for null vs empty array
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(w.Body).Decode(&raw); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	stacksRaw, ok := raw["stacks"]
	if !ok {
		t.Fatal("response missing 'stacks' field")
	}
	if string(stacksRaw) == "null" {
		t.Error("stacks should be [] not null")
	}

	catsRaw, ok := raw["categories"]
	if !ok {
		t.Fatal("response missing 'categories' field")
	}
	if string(catsRaw) == "null" {
		t.Error("categories should be [] not null")
	}
}

// formatInt converts int64 to string for use in JSON literals in tests.
func formatInt(i int64) string {
	return sql.NullString{String: "", Valid: false}.String + strings.TrimSpace(strings.Repeat(" ", 0)) + jsonInt64(i)
}

// jsonInt64 converts int64 to its string representation for JSON embedding.
func jsonInt64(i int64) string {
	var buf [20]byte
	pos := len(buf)
	for i >= 10 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	pos--
	buf[pos] = byte('0' + i)
	return string(buf[pos:])
}

// ─── compact-rules Phase 3: Admin API ────────────────────────────────────────

// Task 3.1 RED: POST /admin/api/skills with compact_rules persists it; without
// compact_rules the request still succeeds with empty string.

func TestHandleCreateSkillPersistsCompactRules(t *testing.T) {
	s := newTestStoreForAdmin(t)
	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	body := strings.NewReader(`{"name":"cr-skill","content":"some content","compact_rules":"use table-driven tests"}`)
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
	if sk.CompactRules != "use table-driven tests" {
		t.Errorf("compact_rules: got %q, want %q", sk.CompactRules, "use table-driven tests")
	}
}

func TestHandleCreateSkillWithoutCompactRulesSucceeds(t *testing.T) {
	s := newTestStoreForAdmin(t)
	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	body := strings.NewReader(`{"name":"no-cr-skill","content":"some content"}`)
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
	if sk.CompactRules != "" {
		t.Errorf("compact_rules: got %q, want empty string", sk.CompactRules)
	}
}

// Task 3.3 RED: PUT /admin/api/skills/{name} with only compact_rules returns 200;
// hasAnyField() returns true when only compact_rules is set.

func TestHandleUpdateSkillWithOnlyCompactRulesReturns200(t *testing.T) {
	s := newTestStoreForAdmin(t)
	seedSkill(t, s, "update-cr-skill")

	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	body := strings.NewReader(`{"compact_rules":"always use RFC 2119"}`)
	req := newTestRequest(t, "PUT", "/admin/api/skills/update-cr-skill", body)
	req = withPathValue(req, "name", "update-cr-skill")
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
	if sk.CompactRules != "always use RFC 2119" {
		t.Errorf("compact_rules: got %q, want %q", sk.CompactRules, "always use RFC 2119")
	}
}

func TestHasAnyFieldReturnsTrueWhenOnlyCompactRulesSet(t *testing.T) {
	cr := "some rules"
	u := &updateSkillRequest{CompactRules: &cr}
	if !u.hasAnyField() {
		t.Error("hasAnyField() should return true when only CompactRules is set")
	}
}

func TestHandleUpdateSkillNullCompactRulesPreservesExisting(t *testing.T) {
	s := newTestStoreForAdmin(t)
	// Create skill with compact_rules set
	_, err := s.CreateSkill(store.CreateSkillParams{
		Name:         "cr-preserve",
		Content:      "content",
		CompactRules: "existing rules",
		ChangedBy:    "test",
	})
	if err != nil {
		t.Fatalf("CreateSkill: %v", err)
	}

	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	// Send null compact_rules — should preserve existing value
	body := strings.NewReader(`{"compact_rules":null}`)
	req := newTestRequest(t, "PUT", "/admin/api/skills/cr-preserve", body)
	req = withPathValue(req, "name", "cr-preserve")
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(makeSkillsCookie(t, cfg, "tech_lead"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "tech_lead", h.handleUpdateSkill)
	authed(w, req)

	// null compact_rules means no change — but we need another field too or 422
	// Per spec: `{"compact_rules": null}` → UpdateSkillParams.CompactRules = nil (no change)
	// but hasAnyField() with null means the field is absent/nil → still 422
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status: got %d, want 422 when all fields are null", w.Code)
	}
}

// Sentinel to use sql.ErrNoRows in the handler — keep compiler happy
var _ = sql.ErrNoRows
