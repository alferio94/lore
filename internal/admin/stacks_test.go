package admin

// stacks_test.go — TDD tests for admin stacks catalog API handlers.
// Phase 3: Skill Catalog Tables — tasks 3.5 (RED) and 3.6 (GREEN)

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/alferio94/lore/internal/store"
)

// ─── Seed helpers ─────────────────────────────────────────────────────────────

// seedStack creates a stack in the store for test setup.
func seedStack(t *testing.T, s *store.Store, name string) *store.Stack {
	t.Helper()
	st, err := s.CreateStack(name, name)
	if err != nil {
		t.Fatalf("seedStack %q: %v", name, err)
	}
	return st
}

// ─── GET /admin/api/stacks ────────────────────────────────────────────────────

func TestHandleListStacksReturns200WithArray(t *testing.T) {
	s := newTestStoreForAdmin(t)
	seedStack(t, s, "angular")
	seedStack(t, s, "nestjs")

	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	req := newTestRequest(t, "GET", "/admin/api/stacks", nil)
	req.AddCookie(makeSkillsCookie(t, cfg, "viewer"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "viewer", h.handleListStacks)
	authed(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", w.Code)
	}

	var stacks []store.Stack
	if err := json.NewDecoder(w.Body).Decode(&stacks); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(stacks) != 2 {
		t.Errorf("stacks count: got %d, want 2", len(stacks))
	}

	names := map[string]bool{}
	for _, st := range stacks {
		names[st.Name] = true
	}
	if !names["angular"] {
		t.Error("expected angular in response")
	}
	if !names["nestjs"] {
		t.Error("expected nestjs in response")
	}
}

func TestHandleListStacksReturnsEmptyArrayWhenNoStacks(t *testing.T) {
	s := newTestStoreForAdmin(t)
	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	req := newTestRequest(t, "GET", "/admin/api/stacks", nil)
	req.AddCookie(makeSkillsCookie(t, cfg, "viewer"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "viewer", h.handleListStacks)
	authed(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", w.Code)
	}

	var stacks []store.Stack
	if err := json.NewDecoder(w.Body).Decode(&stacks); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(stacks) != 0 {
		t.Errorf("expected empty array, got %d stacks", len(stacks))
	}
}

func TestHandleListStacksReturns401WhenUnauthenticated(t *testing.T) {
	s := newTestStoreForAdmin(t)
	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	req := newTestRequest(t, "GET", "/admin/api/stacks", nil)
	// No cookie — unauthenticated
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "viewer", h.handleListStacks)
	authed(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want 401", w.Code)
	}
}

// ─── POST /admin/api/stacks ───────────────────────────────────────────────────

func TestHandleCreateStackReturns201OnValidBody(t *testing.T) {
	s := newTestStoreForAdmin(t)
	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	body := strings.NewReader(`{"name":"svelte","display_name":"Svelte"}`)
	req := newTestRequest(t, "POST", "/admin/api/stacks", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(makeSkillsCookie(t, cfg, "tech_lead"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "tech_lead", h.handleCreateStack)
	authed(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status: got %d, want 201 (body: %s)", w.Code, w.Body.String())
	}

	var st store.Stack
	if err := json.NewDecoder(w.Body).Decode(&st); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if st.Name != "svelte" {
		t.Errorf("name: got %q, want %q", st.Name, "svelte")
	}
	if st.DisplayName != "Svelte" {
		t.Errorf("display_name: got %q, want %q", st.DisplayName, "Svelte")
	}
	if st.ID == 0 {
		t.Error("expected non-zero ID in response")
	}
}

func TestHandleCreateStackReturns201WithDefaultDisplayName(t *testing.T) {
	s := newTestStoreForAdmin(t)
	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	// No display_name provided — should default to name
	body := strings.NewReader(`{"name":"react"}`)
	req := newTestRequest(t, "POST", "/admin/api/stacks", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(makeSkillsCookie(t, cfg, "tech_lead"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "tech_lead", h.handleCreateStack)
	authed(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status: got %d, want 201 (body: %s)", w.Code, w.Body.String())
	}

	var st store.Stack
	if err := json.NewDecoder(w.Body).Decode(&st); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if st.DisplayName != "react" {
		t.Errorf("display_name: got %q, want %q (should default to name)", st.DisplayName, "react")
	}
}

func TestHandleCreateStackReturns409OnDuplicateName(t *testing.T) {
	s := newTestStoreForAdmin(t)
	seedStack(t, s, "angular")

	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	body := strings.NewReader(`{"name":"angular","display_name":"Angular"}`)
	req := newTestRequest(t, "POST", "/admin/api/stacks", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(makeSkillsCookie(t, cfg, "tech_lead"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "tech_lead", h.handleCreateStack)
	authed(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("status: got %d, want 409", w.Code)
	}
}

func TestHandleCreateStackReturns422OnMissingName(t *testing.T) {
	s := newTestStoreForAdmin(t)
	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	body := strings.NewReader(`{"display_name":"No Name Stack"}`)
	req := newTestRequest(t, "POST", "/admin/api/stacks", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(makeSkillsCookie(t, cfg, "tech_lead"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "tech_lead", h.handleCreateStack)
	authed(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status: got %d, want 422", w.Code)
	}
}

func TestHandleCreateStackReturns403ForViewer(t *testing.T) {
	s := newTestStoreForAdmin(t)
	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	body := strings.NewReader(`{"name":"vue","display_name":"Vue"}`)
	req := newTestRequest(t, "POST", "/admin/api/stacks", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(makeSkillsCookie(t, cfg, "viewer"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "tech_lead", h.handleCreateStack)
	authed(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want 403", w.Code)
	}
}

// ─── DELETE /admin/api/stacks/{id} ────────────────────────────────────────────

func TestHandleDeleteStackReturns204OnSuccess(t *testing.T) {
	s := newTestStoreForAdmin(t)
	st := seedStack(t, s, "to-delete-stack")

	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	req := newTestRequest(t, "DELETE", fmt.Sprintf("/admin/api/stacks/%d", st.ID), nil)
	req = withPathValue(req, "id", fmt.Sprintf("%d", st.ID))
	req.AddCookie(makeSkillsCookie(t, cfg, "admin"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "admin", h.handleDeleteStack)
	authed(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status: got %d, want 204 (body: %s)", w.Code, w.Body.String())
	}

	// Confirm stack is gone from list
	stacks, err := s.ListStacks()
	if err != nil {
		t.Fatalf("ListStacks after delete: %v", err)
	}
	for _, stk := range stacks {
		if stk.ID == st.ID {
			t.Error("expected deleted stack to be absent from list, but it was found")
		}
	}
}

func TestHandleDeleteStackReturns404OnNotFound(t *testing.T) {
	s := newTestStoreForAdmin(t)
	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	req := newTestRequest(t, "DELETE", "/admin/api/stacks/999", nil)
	req = withPathValue(req, "id", "999")
	req.AddCookie(makeSkillsCookie(t, cfg, "admin"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "admin", h.handleDeleteStack)
	authed(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", w.Code)
	}
}

func TestHandleDeleteStackReturns403ForTechLead(t *testing.T) {
	s := newTestStoreForAdmin(t)
	st := seedStack(t, s, "protected-stack")

	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	req := newTestRequest(t, "DELETE", fmt.Sprintf("/admin/api/stacks/%d", st.ID), nil)
	req = withPathValue(req, "id", fmt.Sprintf("%d", st.ID))
	req.AddCookie(makeSkillsCookie(t, cfg, "tech_lead"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "admin", h.handleDeleteStack)
	authed(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want 403", w.Code)
	}
}

func TestHandleDeleteStackReturns401WhenUnauthenticated(t *testing.T) {
	s := newTestStoreForAdmin(t)
	st := seedStack(t, s, "unauth-stack")

	cfg := makeSkillsConfig(s)
	h := &adminHandler{cfg: cfg}

	req := newTestRequest(t, "DELETE", fmt.Sprintf("/admin/api/stacks/%d", st.ID), nil)
	req = withPathValue(req, "id", fmt.Sprintf("%d", st.ID))
	// No cookie
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "admin", h.handleDeleteStack)
	authed(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want 401", w.Code)
	}
}
