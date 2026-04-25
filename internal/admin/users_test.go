package admin

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

func makeUsersCfg(s *store.Store) AdminConfig {
	return AdminConfig{Store: s, JWTSecret: []byte("users-test-secret-32-bytes-long!!")}
}

func makeUsersCookie(t *testing.T, cfg AdminConfig, user *store.User) *http.Cookie {
	t.Helper()
	claims := Claims{
		RegisteredClaims: jwtlib.RegisteredClaims{
			Subject:   fmt.Sprintf("%d", user.ID),
			ExpiresAt: jwtlib.NewNumericDate(time.Now().Add(24 * time.Hour)),
		},
		Email: user.Email,
		Name:  user.Name,
		Role:  user.Role,
	}
	tok := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, claims)
	signed, err := tok.SignedString(cfg.JWTSecret)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return &http.Cookie{Name: sessionCookieName, Value: signed}
}

func seedUser(t *testing.T, s *store.Store, email, name, role, status string) *store.User {
	t.Helper()
	u, err := s.UpsertUser(email, name, "", "test")
	if err != nil {
		t.Fatalf("seedUser %q: %v", email, err)
	}
	u, err = s.UpdateUserStatusRole(u.ID, status, role)
	if err != nil {
		t.Fatalf("seedUser update %q: %v", email, err)
	}
	return u
}

func TestHandleUpdateUserRoleSupportsRoleAndStatus(t *testing.T) {
	s := newUsersTestStore(t)
	cfg := makeUsersCfg(s)
	h := &adminHandler{cfg: cfg}
	target := seedUser(t, s, "target@example.com", "Target", store.UserRoleNA, store.UserStatusPending)

	body := strings.NewReader(`{"role":"developer","status":"active"}`)
	req := newTestRequest(t, http.MethodPatch, fmt.Sprintf("/admin/api/users/%d", target.ID), body)
	req = withPathValue(req, "id", fmt.Sprintf("%d", target.ID))
	req.Header.Set("Content-Type", "application/json")
	w := newTestResponseRecorder()

	h.handleUpdateUserRole(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (body: %s)", w.Code, w.Body.String())
	}

	var updated store.User
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if updated.Role != store.UserRoleDeveloper || updated.Status != store.UserStatusActive {
		t.Fatalf("updated role/status = %q/%q, want %q/%q", updated.Role, updated.Status, store.UserRoleDeveloper, store.UserStatusActive)
	}
}

func TestHandleUpdateUserRoleRejectsInvalidStatus(t *testing.T) {
	s := newUsersTestStore(t)
	cfg := makeUsersCfg(s)
	h := &adminHandler{cfg: cfg}
	target := seedUser(t, s, "target@example.com", "Target", store.UserRoleNA, store.UserStatusPending)

	body := strings.NewReader(`{"role":"developer","status":"suspended"}`)
	req := newTestRequest(t, http.MethodPatch, fmt.Sprintf("/admin/api/users/%d", target.ID), body)
	req = withPathValue(req, "id", fmt.Sprintf("%d", target.ID))
	req.Header.Set("Content-Type", "application/json")
	w := newTestResponseRecorder()

	h.handleUpdateUserRole(w, req)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status: got %d, want 422", w.Code)
	}
}

func TestHandleGetMeReturnsStoreResolvedActor(t *testing.T) {
	s := newUsersTestStore(t)
	cfg := makeUsersCfg(s)
	h := &adminHandler{cfg: cfg}
	actor := seedUser(t, s, "me@example.com", "Me", store.UserRoleDeveloper, store.UserStatusActive)

	req := newTestRequest(t, http.MethodGet, "/admin/api/me", nil)
	req = req.WithContext(withActor(req.Context(), actor))
	req.AddCookie(makeUsersCookie(t, cfg, actor))
	w := newTestResponseRecorder()

	h.handleGetMe(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (body: %s)", w.Code, w.Body.String())
	}

	var me store.User
	if err := json.NewDecoder(w.Body).Decode(&me); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if me.ID != actor.ID || me.Role != actor.Role || me.Status != actor.Status {
		t.Fatalf("me = %#v, want %#v", me, *actor)
	}
}
