package admin

import (
	"net/http"
	"testing"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
)

// ─── requireAuth tests ────────────────────────────────────────────────────────

func TestRequireAuth(t *testing.T) {
	secret := []byte("middleware-test-secret-32-bytes!!")

	// makeToken creates a signed JWT for tests; past=true makes it expired.
	makeToken := func(t *testing.T, past bool) string {
		t.Helper()
		exp := time.Now().Add(24 * time.Hour)
		if past {
			exp = time.Now().Add(-1 * time.Hour)
		}
		c := Claims{
			RegisteredClaims: jwtlib.RegisteredClaims{
				Subject:   "1",
				ExpiresAt: jwtlib.NewNumericDate(exp),
			},
			Email: "user@example.com",
			Name:  "Test User",
			Role:  "viewer",
		}
		tok := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, c)
		s, err := tok.SignedString(secret)
		if err != nil {
			t.Fatalf("makeToken: %v", err)
		}
		return s
	}

	t.Run("valid cookie passes claims to context", func(t *testing.T) {
		tokenStr := makeToken(t, false)

		handlerCalled := false
		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handlerCalled = true
			claims, ok := claimsFromCtx(r.Context())
			if !ok {
				t.Error("expected claims in context, got none")
				return
			}
			if claims.Email != "user@example.com" {
				t.Errorf("email: got %q, want %q", claims.Email, "user@example.com")
			}
			w.WriteHeader(http.StatusOK)
		})

		handler := requireAuth(secret, inner)
		req := newTestRequest(t, "GET", "/admin/api/skills", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tokenStr})
		w := newTestResponseRecorder()
		handler(w, req)

		if !handlerCalled {
			t.Error("expected inner handler to be called")
		}
		if w.Code != http.StatusOK {
			t.Errorf("status: got %d, want 200", w.Code)
		}
	})

	t.Run("missing cookie returns 401", func(t *testing.T) {
		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("inner handler must not be called when cookie is missing")
		})

		handler := requireAuth(secret, inner)
		req := newTestRequest(t, "GET", "/admin/api/skills", nil)
		// No cookie set
		w := newTestResponseRecorder()
		handler(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("status: got %d, want 401", w.Code)
		}
	})

	t.Run("expired JWT returns 401", func(t *testing.T) {
		expiredToken := makeToken(t, true)

		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("inner handler must not be called with expired token")
		})

		handler := requireAuth(secret, inner)
		req := newTestRequest(t, "GET", "/admin/api/skills", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: expiredToken})
		w := newTestResponseRecorder()
		handler(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("status: got %d, want 401", w.Code)
		}
	})
}

// ─── requireRole tests ────────────────────────────────────────────────────────

func TestRequireRole(t *testing.T) {
	secret := []byte("middleware-test-secret-32-bytes!!")

	makeRoleCookie := func(t *testing.T, role string) *http.Cookie {
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
		s, err := tok.SignedString(secret)
		if err != nil {
			t.Fatalf("makeRoleCookie: %v", err)
		}
		return &http.Cookie{Name: sessionCookieName, Value: s}
	}

	t.Run("admin accesses viewer route (200)", func(t *testing.T) {
		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		handler := requireRole(secret, "viewer", inner)
		req := newTestRequest(t, "GET", "/admin/api/skills", nil)
		req.AddCookie(makeRoleCookie(t, "admin"))
		w := newTestResponseRecorder()
		handler(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("status: got %d, want 200", w.Code)
		}
	})

	t.Run("viewer accesses admin route (403)", func(t *testing.T) {
		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("inner handler must not be called when role is insufficient")
		})

		handler := requireRole(secret, "admin", inner)
		req := newTestRequest(t, "GET", "/admin/api/users", nil)
		req.AddCookie(makeRoleCookie(t, "viewer"))
		w := newTestResponseRecorder()
		handler(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("status: got %d, want 403", w.Code)
		}
	})

	t.Run("tech_lead accesses tech_lead route (200)", func(t *testing.T) {
		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		handler := requireRole(secret, "tech_lead", inner)
		req := newTestRequest(t, "GET", "/admin/api/skills", nil)
		req.AddCookie(makeRoleCookie(t, "tech_lead"))
		w := newTestResponseRecorder()
		handler(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("status: got %d, want 200", w.Code)
		}
	})

	t.Run("tech_lead accesses admin route (403)", func(t *testing.T) {
		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("inner handler must not be called when role is insufficient")
		})

		handler := requireRole(secret, "admin", inner)
		req := newTestRequest(t, "GET", "/admin/api/users", nil)
		req.AddCookie(makeRoleCookie(t, "tech_lead"))
		w := newTestResponseRecorder()
		handler(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("status: got %d, want 403", w.Code)
		}
	})

	t.Run("unauthenticated request returns 401", func(t *testing.T) {
		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("inner handler must not be called when unauthenticated")
		})

		handler := requireRole(secret, "viewer", inner)
		req := newTestRequest(t, "GET", "/admin/api/skills", nil)
		// No cookie
		w := newTestResponseRecorder()
		handler(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("status: got %d, want 401", w.Code)
		}
	})
}
