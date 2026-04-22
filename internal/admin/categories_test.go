package admin

// categories_test.go — TDD tests for admin categories catalog API handlers.
// Phase 3: Skill Catalog Tables — tasks 3.7 (RED) and 3.8 (GREEN)

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/alferio94/lore/internal/store"
)

// ─── Seed helpers ─────────────────────────────────────────────────────────────

// seedCategory creates a category in the store for test setup.
func seedCategory(t *testing.T, s *store.Store, name string) *store.Category {
	t.Helper()
	cat, err := s.CreateCategory(name, name)
	if err != nil {
		t.Fatalf("seedCategory %q: %v", name, err)
	}
	return cat
}

// ─── GET /admin/api/categories ────────────────────────────────────────────────

func TestHandleListCategoriesReturns200WithArray(t *testing.T) {
	s := newTestStoreForAdmin(t)
	seedCategory(t, s, "tutorial")
	seedCategory(t, s, "advanced")

	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	req := newTestRequest(t, "GET", "/admin/api/categories", nil)
	req.AddCookie(makeSkillsCookie(t, cfg, "viewer"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "viewer", h.handleListCategories)
	authed(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", w.Code)
	}

	var categories []store.Category
	if err := json.NewDecoder(w.Body).Decode(&categories); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(categories) != 2 {
		t.Errorf("categories count: got %d, want 2", len(categories))
	}

	names := map[string]bool{}
	for _, cat := range categories {
		names[cat.Name] = true
	}
	if !names["tutorial"] {
		t.Error("expected tutorial in response")
	}
	if !names["advanced"] {
		t.Error("expected advanced in response")
	}
}

func TestHandleListCategoriesReturnsEmptyArrayWhenNoCategories(t *testing.T) {
	s := newTestStoreForAdmin(t)
	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	req := newTestRequest(t, "GET", "/admin/api/categories", nil)
	req.AddCookie(makeSkillsCookie(t, cfg, "viewer"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "viewer", h.handleListCategories)
	authed(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", w.Code)
	}

	var categories []store.Category
	if err := json.NewDecoder(w.Body).Decode(&categories); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(categories) != 0 {
		t.Errorf("expected empty array, got %d categories", len(categories))
	}
}

func TestHandleListCategoriesReturns401WhenUnauthenticated(t *testing.T) {
	s := newTestStoreForAdmin(t)
	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	req := newTestRequest(t, "GET", "/admin/api/categories", nil)
	// No cookie — unauthenticated
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "viewer", h.handleListCategories)
	authed(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want 401", w.Code)
	}
}

// ─── POST /admin/api/categories ──────────────────────────────────────────────

func TestHandleCreateCategoryReturns201OnValidBody(t *testing.T) {
	s := newTestStoreForAdmin(t)
	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	body := strings.NewReader(`{"name":"beginner","display_name":"Beginner"}`)
	req := newTestRequest(t, "POST", "/admin/api/categories", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(makeSkillsCookie(t, cfg, "tech_lead"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "tech_lead", h.handleCreateCategory)
	authed(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status: got %d, want 201 (body: %s)", w.Code, w.Body.String())
	}

	var cat store.Category
	if err := json.NewDecoder(w.Body).Decode(&cat); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if cat.Name != "beginner" {
		t.Errorf("name: got %q, want %q", cat.Name, "beginner")
	}
	if cat.DisplayName != "Beginner" {
		t.Errorf("display_name: got %q, want %q", cat.DisplayName, "Beginner")
	}
	if cat.ID == 0 {
		t.Error("expected non-zero ID in response")
	}
}

func TestHandleCreateCategoryReturns201WithDefaultDisplayName(t *testing.T) {
	s := newTestStoreForAdmin(t)
	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	// No display_name provided — should default to name
	body := strings.NewReader(`{"name":"workshop"}`)
	req := newTestRequest(t, "POST", "/admin/api/categories", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(makeSkillsCookie(t, cfg, "tech_lead"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "tech_lead", h.handleCreateCategory)
	authed(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status: got %d, want 201 (body: %s)", w.Code, w.Body.String())
	}

	var cat store.Category
	if err := json.NewDecoder(w.Body).Decode(&cat); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if cat.DisplayName != "workshop" {
		t.Errorf("display_name: got %q, want %q (should default to name)", cat.DisplayName, "workshop")
	}
}

func TestHandleCreateCategoryReturns409OnDuplicateName(t *testing.T) {
	s := newTestStoreForAdmin(t)
	seedCategory(t, s, "tutorial")

	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	body := strings.NewReader(`{"name":"tutorial","display_name":"Tutorial"}`)
	req := newTestRequest(t, "POST", "/admin/api/categories", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(makeSkillsCookie(t, cfg, "tech_lead"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "tech_lead", h.handleCreateCategory)
	authed(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("status: got %d, want 409", w.Code)
	}
}

func TestHandleCreateCategoryReturns422OnMissingName(t *testing.T) {
	s := newTestStoreForAdmin(t)
	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	body := strings.NewReader(`{"display_name":"No Name Category"}`)
	req := newTestRequest(t, "POST", "/admin/api/categories", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(makeSkillsCookie(t, cfg, "tech_lead"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "tech_lead", h.handleCreateCategory)
	authed(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status: got %d, want 422", w.Code)
	}
}

func TestHandleCreateCategoryReturns403ForViewer(t *testing.T) {
	s := newTestStoreForAdmin(t)
	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	body := strings.NewReader(`{"name":"intermediate","display_name":"Intermediate"}`)
	req := newTestRequest(t, "POST", "/admin/api/categories", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(makeSkillsCookie(t, cfg, "viewer"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "tech_lead", h.handleCreateCategory)
	authed(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want 403", w.Code)
	}
}

// ─── DELETE /admin/api/categories/{id} ───────────────────────────────────────

func TestHandleDeleteCategoryReturns204OnSuccess(t *testing.T) {
	s := newTestStoreForAdmin(t)
	cat := seedCategory(t, s, "to-delete-category")

	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	req := newTestRequest(t, "DELETE", fmt.Sprintf("/admin/api/categories/%d", cat.ID), nil)
	req = withPathValue(req, "id", fmt.Sprintf("%d", cat.ID))
	req.AddCookie(makeSkillsCookie(t, cfg, "admin"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "admin", h.handleDeleteCategory)
	authed(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status: got %d, want 204 (body: %s)", w.Code, w.Body.String())
	}

	// Confirm category is gone from list
	categories, err := s.ListCategories()
	if err != nil {
		t.Fatalf("ListCategories after delete: %v", err)
	}
	for _, c := range categories {
		if c.ID == cat.ID {
			t.Error("expected deleted category to be absent from list, but it was found")
		}
	}
}

func TestHandleDeleteCategoryReturns404OnNotFound(t *testing.T) {
	s := newTestStoreForAdmin(t)
	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	req := newTestRequest(t, "DELETE", "/admin/api/categories/999", nil)
	req = withPathValue(req, "id", "999")
	req.AddCookie(makeSkillsCookie(t, cfg, "admin"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "admin", h.handleDeleteCategory)
	authed(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", w.Code)
	}
}

func TestHandleDeleteCategoryReturns403ForTechLead(t *testing.T) {
	s := newTestStoreForAdmin(t)
	cat := seedCategory(t, s, "protected-category")

	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	req := newTestRequest(t, "DELETE", fmt.Sprintf("/admin/api/categories/%d", cat.ID), nil)
	req = withPathValue(req, "id", fmt.Sprintf("%d", cat.ID))
	req.AddCookie(makeSkillsCookie(t, cfg, "tech_lead"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "admin", h.handleDeleteCategory)
	authed(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want 403", w.Code)
	}
}

func TestHandleDeleteCategoryReturns401WhenUnauthenticated(t *testing.T) {
	s := newTestStoreForAdmin(t)
	cat := seedCategory(t, s, "unauth-category")

	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	req := newTestRequest(t, "DELETE", fmt.Sprintf("/admin/api/categories/%d", cat.ID), nil)
	req = withPathValue(req, "id", fmt.Sprintf("%d", cat.ID))
	// No cookie
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "admin", h.handleDeleteCategory)
	authed(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want 401", w.Code)
	}
}
