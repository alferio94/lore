package admin

// users_test.go — TDD tests for handleListUsers, handleUpdateUserRole, and handleGetMe.
// Phase 5: Users API — tasks 5.3, 5.5, 5.7 (RED phases)

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"

	"github.com/alferio94/lore/internal/store"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

// newUsersTestStore returns a real store with a temp dir for user handler tests.
func newUsersTestStore(t *testing.T) *store.Store {
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

// makeUsersCfg returns an AdminConfig wired to the given store.
func makeUsersCfg(s *store.Store) AdminConfig {
	return AdminConfig{
		Store:     s,
		JWTSecret: []byte("users-test-secret-32-bytes-long!!"),
		DevAuth:   false,
	}
}

// makeUsersCookie creates a valid JWT cookie for the given role.
func makeUsersCookie(t *testing.T, cfg AdminConfig, role string) *http.Cookie {
	t.Helper()
	c := Claims{
		RegisteredClaims: jwtlib.RegisteredClaims{
			Subject:   "1",
			ExpiresAt: jwtlib.NewNumericDate(time.Now().Add(24 * time.Hour)),
		},
		Email: "admin@example.com",
		Name:  "Admin User",
		Role:  role,
	}
	tok := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, c)
	s, err := tok.SignedString(cfg.JWTSecret)
	if err != nil {
		t.Fatalf("makeUsersCookie sign: %v", err)
	}
	return &http.Cookie{Name: sessionCookieName, Value: s}
}

// seedUser inserts a user into the store for test setup.
func seedUser(t *testing.T, s *store.Store, email, name string) *store.User {
	t.Helper()
	u, err := s.UpsertUser(email, name, "", "test")
	if err != nil {
		t.Fatalf("seedUser %q: %v", email, err)
	}
	return u
}

// ─── Task 5.3 RED: handleListUsers ───────────────────────────────────────────

func TestHandleListUsersReturns200ForAdmin(t *testing.T) {
	s := newUsersTestStore(t)
	seedUser(t, s, "alice@example.com", "Alice")
	seedUser(t, s, "bob@example.com", "Bob")

	cfg := makeUsersCfg(s)
	h := &adminHandler{cfg: cfg}

	req := newTestRequest(t, "GET", "/admin/api/users", nil)
	req.AddCookie(makeUsersCookie(t, cfg, "admin"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "admin", h.handleListUsers)
	authed(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200 (body: %s)", w.Code, w.Body.String())
	}

	var users []store.User
	if err := json.NewDecoder(w.Body).Decode(&users); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(users) != 2 {
		t.Errorf("users count: got %d, want 2", len(users))
	}
}

func TestHandleListUsersReturns403ForTechLead(t *testing.T) {
	s := newUsersTestStore(t)
	cfg := makeUsersCfg(s)
	h := &adminHandler{cfg: cfg}

	req := newTestRequest(t, "GET", "/admin/api/users", nil)
	req.AddCookie(makeUsersCookie(t, cfg, "tech_lead"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "admin", h.handleListUsers)
	authed(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want 403", w.Code)
	}
}

func TestHandleListUsersReturns403ForViewer(t *testing.T) {
	s := newUsersTestStore(t)
	cfg := makeUsersCfg(s)
	h := &adminHandler{cfg: cfg}

	req := newTestRequest(t, "GET", "/admin/api/users", nil)
	req.AddCookie(makeUsersCookie(t, cfg, "viewer"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "admin", h.handleListUsers)
	authed(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want 403", w.Code)
	}
}

// ─── Task 5.5 RED: handleUpdateUserRole ──────────────────────────────────────

func TestHandleUpdateUserRoleReturns200OnValidPromotion(t *testing.T) {
	s := newUsersTestStore(t)
	// First user gets admin (bootstrap); seed a second one as viewer
	seedUser(t, s, "first@example.com", "First")
	target := seedUser(t, s, "target@example.com", "Target")

	cfg := makeUsersCfg(s)
	h := &adminHandler{cfg: cfg}

	body := strings.NewReader(`{"role":"tech_lead"}`)
	req := newTestRequest(t, "PUT", fmt.Sprintf("/admin/api/users/%d/role", target.ID), body)
	req = withPathValue(req, "id", fmt.Sprintf("%d", target.ID))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(makeUsersCookie(t, cfg, "admin"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "admin", h.handleUpdateUserRole)
	authed(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200 (body: %s)", w.Code, w.Body.String())
	}

	var user store.User
	if err := json.NewDecoder(w.Body).Decode(&user); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if user.Role != "tech_lead" {
		t.Errorf("role: got %q, want %q", user.Role, "tech_lead")
	}
}

func TestHandleUpdateUserRoleReturns422OnInvalidRole(t *testing.T) {
	s := newUsersTestStore(t)
	target := seedUser(t, s, "target2@example.com", "Target2")

	cfg := makeUsersCfg(s)
	h := &adminHandler{cfg: cfg}

	body := strings.NewReader(`{"role":"superuser"}`)
	req := newTestRequest(t, "PUT", fmt.Sprintf("/admin/api/users/%d/role", target.ID), body)
	req = withPathValue(req, "id", fmt.Sprintf("%d", target.ID))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(makeUsersCookie(t, cfg, "admin"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "admin", h.handleUpdateUserRole)
	authed(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status: got %d, want 422", w.Code)
	}
}

func TestHandleUpdateUserRoleReturns404OnUnknownUser(t *testing.T) {
	s := newUsersTestStore(t)
	cfg := makeUsersCfg(s)
	h := &adminHandler{cfg: cfg}

	body := strings.NewReader(`{"role":"viewer"}`)
	req := newTestRequest(t, "PUT", "/admin/api/users/99999/role", body)
	req = withPathValue(req, "id", "99999")
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(makeUsersCookie(t, cfg, "admin"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "admin", h.handleUpdateUserRole)
	authed(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", w.Code)
	}
}

func TestHandleUpdateUserRoleReturns403ForNonAdmin(t *testing.T) {
	s := newUsersTestStore(t)
	target := seedUser(t, s, "target3@example.com", "Target3")

	cfg := makeUsersCfg(s)
	h := &adminHandler{cfg: cfg}

	body := strings.NewReader(`{"role":"tech_lead"}`)
	req := newTestRequest(t, "PUT", fmt.Sprintf("/admin/api/users/%d/role", target.ID), body)
	req = withPathValue(req, "id", fmt.Sprintf("%d", target.ID))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(makeUsersCookie(t, cfg, "tech_lead"))
	w := newTestResponseRecorder()

	authed := requireRole(cfg.JWTSecret, "admin", h.handleUpdateUserRole)
	authed(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want 403", w.Code)
	}
}

// ─── Task 5.7 RED: handleGetMe ───────────────────────────────────────────────

func TestHandleGetMeReturnsCurrentUserFromClaims(t *testing.T) {
	s := newUsersTestStore(t)
	cfg := makeUsersCfg(s)
	h := &adminHandler{cfg: cfg}

	// Build a cookie with specific email/role to verify claims are returned
	c := Claims{
		RegisteredClaims: jwtlib.RegisteredClaims{
			Subject:   "42",
			ExpiresAt: jwtlib.NewNumericDate(time.Now().Add(24 * time.Hour)),
		},
		Email: "me@example.com",
		Name:  "Me User",
		Role:  "tech_lead",
	}
	tok := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, c)
	tokenStr, err := tok.SignedString(cfg.JWTSecret)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	req := newTestRequest(t, "GET", "/admin/api/me", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tokenStr})
	w := newTestResponseRecorder()

	authed := requireAuth(cfg.JWTSecret, h.handleGetMe)
	authed(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200 (body: %s)", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["email"] != "me@example.com" {
		t.Errorf("email: got %q, want %q", resp["email"], "me@example.com")
	}
	if resp["role"] != "tech_lead" {
		t.Errorf("role: got %q, want %q", resp["role"], "tech_lead")
	}
	if resp["name"] != "Me User" {
		t.Errorf("name: got %q, want %q", resp["name"], "Me User")
	}
}

func TestHandleGetMeReturns401WithoutToken(t *testing.T) {
	s := newUsersTestStore(t)
	cfg := makeUsersCfg(s)
	h := &adminHandler{cfg: cfg}

	req := newTestRequest(t, "GET", "/admin/api/me", nil)
	// No cookie set
	w := newTestResponseRecorder()

	authed := requireAuth(cfg.JWTSecret, h.handleGetMe)
	authed(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want 401", w.Code)
	}
}
