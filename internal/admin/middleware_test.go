package admin

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"

	"github.com/alferio94/lore/internal/store"
)

func makeMiddlewareToken(t *testing.T, secret []byte, subject, email, role string, past bool) string {
	t.Helper()
	exp := time.Now().Add(24 * time.Hour)
	if past {
		exp = time.Now().Add(-1 * time.Hour)
	}
	claims := Claims{
		RegisteredClaims: jwtlib.RegisteredClaims{
			Subject:   subject,
			ExpiresAt: jwtlib.NewNumericDate(exp),
		},
		Email: email,
		Name:  "Test User",
		Role:  role,
	}
	tok := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, claims)
	signed, err := tok.SignedString(secret)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

func TestRequireAuth(t *testing.T) {
	secret := []byte("middleware-test-secret-32-bytes!!")

	t.Run("valid cookie passes claims to context without store resolver", func(t *testing.T) {
		tokenStr := makeMiddlewareToken(t, secret, "1", "user@example.com", store.UserRoleDeveloper, false)
		called := false
		handler := requireAuth(secret, func(w http.ResponseWriter, r *http.Request) {
			called = true
			claims, ok := claimsFromCtx(r.Context())
			if !ok || claims.Email != "user@example.com" {
				t.Fatalf("claims missing or wrong: %#v", claims)
			}
			w.WriteHeader(http.StatusOK)
		})

		req := newTestRequest(t, http.MethodGet, "/admin/api/skills", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tokenStr})
		w := newTestResponseRecorder()
		handler(w, req)

		if !called {
			t.Fatal("expected inner handler to be called")
		}
		if w.Code != http.StatusOK {
			t.Fatalf("status: got %d, want 200", w.Code)
		}
	})

	t.Run("store resolver loads active actor and overrides stale claims", func(t *testing.T) {
		s := newTestStoreForAdmin(t)
		actor, err := s.UpsertUser("actor@example.com", "Actor", "", "github")
		if err != nil {
			t.Fatalf("seed actor: %v", err)
		}
		actor, err = s.UpdateUserStatusRole(actor.ID, store.UserStatusActive, store.UserRoleDeveloper)
		if err != nil {
			t.Fatalf("update actor: %v", err)
		}

		tokenStr := makeMiddlewareToken(t, secret, "1", actor.Email, store.UserRoleAdmin, false)
		called := false
		handler := withAdminStore(s, requireAuth(secret, func(w http.ResponseWriter, r *http.Request) {
			called = true
			user, ok := actorFromCtx(r.Context())
			if !ok {
				t.Fatal("expected actor in context")
			}
			if user.ID != actor.ID || user.Role != store.UserRoleDeveloper {
				t.Fatalf("actor = %#v, want id=%d role=%q", user, actor.ID, store.UserRoleDeveloper)
			}
			w.WriteHeader(http.StatusOK)
		}))

		req := newTestRequest(t, http.MethodGet, "/admin/api/skills", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tokenStr})
		w := newTestResponseRecorder()
		handler(w, req)

		if !called {
			t.Fatal("expected inner handler to be called")
		}
		if w.Code != http.StatusOK {
			t.Fatalf("status: got %d, want 200", w.Code)
		}
	})

	t.Run("pending actor is rejected from current store state", func(t *testing.T) {
		s := newTestStoreForAdmin(t)
		actor, err := s.UpsertUser("pending@example.com", "Pending", "", "github")
		if err != nil {
			t.Fatalf("seed actor: %v", err)
		}
		actor, err = s.UpdateUserStatusRole(actor.ID, store.UserStatusPending, store.UserRoleNA)
		if err != nil {
			t.Fatalf("update actor: %v", err)
		}

		tokenStr := makeMiddlewareToken(t, secret, "1", actor.Email, store.UserRoleAdmin, false)
		handler := withAdminStore(s, requireAuth(secret, func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("inner handler must not be called for pending actor")
		}))

		req := newTestRequest(t, http.MethodGet, "/admin/api/skills", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tokenStr})
		w := newTestResponseRecorder()
		handler(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("status: got %d, want 403", w.Code)
		}
	})

	t.Run("legacy viewer actor is rejected from current store state", func(t *testing.T) {
		s := newTestStoreForAdmin(t)
		actor, err := s.UpsertUser("legacy-viewer@example.com", "Legacy Viewer", "", "github")
		if err != nil {
			t.Fatalf("seed actor: %v", err)
		}
		actor, err = s.UpdateUserStatusRole(actor.ID, store.UserStatusActive, store.LegacyUserRoleViewer)
		if err != nil {
			t.Fatalf("update actor: %v", err)
		}

		tokenStr := makeMiddlewareToken(t, secret, strconv.FormatInt(actor.ID, 10), actor.Email, store.UserRoleDeveloper, false)
		handler := withAdminStore(s, requireAuth(secret, func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("inner handler must not be called for legacy viewer actor")
		}))

		req := newTestRequest(t, http.MethodGet, "/admin/api/skills", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tokenStr})
		w := newTestResponseRecorder()
		handler(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("status: got %d, want 403", w.Code)
		}
	})
}

func TestRequireRole(t *testing.T) {
	secret := []byte("middleware-test-secret-32-bytes!!")

	t.Run("developer accesses developer route", func(t *testing.T) {
		handler := requireRole(secret, store.UserRoleDeveloper, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		req := newTestRequest(t, http.MethodGet, "/admin/api/skills", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: makeMiddlewareToken(t, secret, "1", "dev@example.com", store.UserRoleDeveloper, false)})
		w := newTestResponseRecorder()
		handler(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status: got %d, want 200", w.Code)
		}
	})

	t.Run("stale admin claim cannot bypass store developer role", func(t *testing.T) {
		s := newTestStoreForAdmin(t)
		actor, err := s.UpsertUser("actor@example.com", "Actor", "", "github")
		if err != nil {
			t.Fatalf("seed actor: %v", err)
		}
		actor, err = s.UpdateUserStatusRole(actor.ID, store.UserStatusActive, store.UserRoleDeveloper)
		if err != nil {
			t.Fatalf("update actor: %v", err)
		}

		handler := withAdminStore(s, requireRole(secret, store.UserRoleAdmin, func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("inner handler must not be called when store role is insufficient")
		}))
		req := newTestRequest(t, http.MethodGet, "/admin/api/users", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: makeMiddlewareToken(t, secret, "1", actor.Email, store.UserRoleAdmin, false)})
		w := newTestResponseRecorder()
		handler(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("status: got %d, want 403", w.Code)
		}
	})

	t.Run("unauthenticated request returns 401", func(t *testing.T) {
		handler := requireRole(secret, store.UserRoleDeveloper, func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("inner handler must not be called when unauthenticated")
		})
		req := newTestRequest(t, http.MethodGet, "/admin/api/skills", nil)
		w := newTestResponseRecorder()
		handler(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status: got %d, want 401", w.Code)
		}
	})
}
