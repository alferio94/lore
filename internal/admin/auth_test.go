package admin

import (
	"testing"
	"time"

	"github.com/alferio94/lore/internal/store"
	jwtlib "github.com/golang-jwt/jwt/v5"
)

// ─── JWT tests ───────────────────────────────────────────────────────────────

func TestIssueAndParseJWT(t *testing.T) {
	t.Helper()
	cfg := AdminConfig{
		JWTSecret: []byte("test-secret-32-bytes-long-enough!"),
	}
	user := store.User{
		ID:    42,
		Email: "alice@example.com",
		Name:  "Alice",
		Role:  "admin",
	}

	t.Run("valid claims round-trip", func(t *testing.T) {
		tokenStr, err := issueJWT(cfg, user)
		if err != nil {
			t.Fatalf("issueJWT returned error: %v", err)
		}
		if tokenStr == "" {
			t.Fatal("expected non-empty token string")
		}

		claims, err := parseJWT(cfg.JWTSecret, tokenStr)
		if err != nil {
			t.Fatalf("parseJWT returned error: %v", err)
		}
		if claims.Email != user.Email {
			t.Errorf("email: got %q, want %q", claims.Email, user.Email)
		}
		if claims.Name != user.Name {
			t.Errorf("name: got %q, want %q", claims.Name, user.Name)
		}
		if claims.Role != user.Role {
			t.Errorf("role: got %q, want %q", claims.Role, user.Role)
		}
		sub, err := claims.GetSubject()
		if err != nil {
			t.Fatalf("GetSubject error: %v", err)
		}
		if sub != "42" {
			t.Errorf("sub: got %q, want %q", sub, "42")
		}
	})

	t.Run("expired token is rejected", func(t *testing.T) {
		// Build a token manually with exp in the past
		expiredClaims := Claims{
			RegisteredClaims: jwtlib.RegisteredClaims{
				Subject:   "42",
				ExpiresAt: jwtlib.NewNumericDate(time.Now().Add(-1 * time.Hour)),
			},
			Email: user.Email,
			Name:  user.Name,
			Role:  user.Role,
		}
		token := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, expiredClaims)
		tokenStr, err := token.SignedString(cfg.JWTSecret)
		if err != nil {
			t.Fatalf("could not sign expired token: %v", err)
		}

		_, err = parseJWT(cfg.JWTSecret, tokenStr)
		if err == nil {
			t.Fatal("expected error for expired token, got nil")
		}
	})

	t.Run("wrong secret is rejected", func(t *testing.T) {
		tokenStr, err := issueJWT(cfg, user)
		if err != nil {
			t.Fatalf("issueJWT error: %v", err)
		}

		_, err = parseJWT([]byte("wrong-secret-completely-different"), tokenStr)
		if err == nil {
			t.Fatal("expected error for wrong secret, got nil")
		}
	})
}

// ─── OAuth CSRF state tests ───────────────────────────────────────────────────

func TestOAuthState(t *testing.T) {
	secret := []byte("csrf-test-secret-must-be-long-enough!")

	t.Run("valid state passes validation", func(t *testing.T) {
		state, err := generateState(secret)
		if err != nil {
			t.Fatalf("generateState error: %v", err)
		}
		if state == "" {
			t.Fatal("expected non-empty state")
		}

		if err := validateState(secret, state); err != nil {
			t.Errorf("validateState returned error for valid state: %v", err)
		}
	})

	t.Run("tampered state is rejected", func(t *testing.T) {
		state, err := generateState(secret)
		if err != nil {
			t.Fatalf("generateState error: %v", err)
		}

		// Tamper with the state by appending a character
		tampered := state + "x"
		if err := validateState(secret, tampered); err == nil {
			t.Fatal("expected error for tampered state, got nil")
		}
	})

	t.Run("different secret rejects state", func(t *testing.T) {
		state, err := generateState(secret)
		if err != nil {
			t.Fatalf("generateState error: %v", err)
		}

		differentSecret := []byte("different-secret-must-be-long-enough!")
		if err := validateState(differentSecret, state); err == nil {
			t.Fatal("expected error for state validated with wrong secret, got nil")
		}
	})
}

// ─── Dev Auth handler tests ───────────────────────────────────────────────────

func TestHandleDevAuth(t *testing.T) {
	t.Run("DevAuth=true sets lore_session cookie and redirects to /admin/", func(t *testing.T) {
		cfg := AdminConfig{
			JWTSecret: []byte("dev-auth-secret-32-bytes-long-ok!"),
			DevAuth:   true,
		}
		h := &adminHandler{cfg: cfg}

		req := newTestRequest(t, "GET", "/admin/auth/dev", nil)
		w := newTestResponseRecorder()
		h.handleDevAuth(w, req)

		resp := w.Result()
		if resp.StatusCode != 302 {
			t.Errorf("status: got %d, want 302", resp.StatusCode)
		}

		location := resp.Header.Get("Location")
		if location != "/admin/" {
			t.Errorf("Location: got %q, want %q", location, "/admin/")
		}

		// Check lore_session cookie is present
		var sessionCookie *cookieResult
		for _, c := range resp.Cookies() {
			if c.Name == "lore_session" {
				sessionCookie = &cookieResult{Value: c.Value, HttpOnly: c.HttpOnly}
				break
			}
		}
		if sessionCookie == nil {
			t.Fatal("expected lore_session cookie to be set")
		}
		if !sessionCookie.HttpOnly {
			t.Error("expected lore_session cookie to be HttpOnly")
		}

		// Verify the cookie value is a valid JWT with role=admin
		claims, err := parseJWT(cfg.JWTSecret, sessionCookie.Value)
		if err != nil {
			t.Fatalf("parseJWT on dev session cookie: %v", err)
		}
		if claims.Role != "admin" {
			t.Errorf("role: got %q, want %q", claims.Role, "admin")
		}
		if claims.Email != "dev@localhost" {
			t.Errorf("email: got %q, want %q", claims.Email, "dev@localhost")
		}
	})
}

// ─── OAuth handler tests ──────────────────────────────────────────────────────

func TestHandleAuthStart(t *testing.T) {
	t.Run("unknown provider returns 404", func(t *testing.T) {
		cfg := AdminConfig{
			JWTSecret: []byte("test-secret-32-bytes-long-enough!"),
		}
		h := &adminHandler{cfg: cfg}

		req := newTestRequest(t, "GET", "/admin/auth/unknown", nil)
		req = withPathValue(req, "provider", "unknown")
		w := newTestResponseRecorder()
		h.handleAuthStart(w, req)

		resp := w.Result()
		if resp.StatusCode != 404 {
			t.Errorf("status: got %d, want 404", resp.StatusCode)
		}
	})

	t.Run("google provider redirects to OAuth URL with state param", func(t *testing.T) {
		cfg := newTestOAuthConfig(t)
		h := &adminHandler{cfg: cfg}

		req := newTestRequest(t, "GET", "/admin/auth/google", nil)
		req = withPathValue(req, "provider", "google")
		w := newTestResponseRecorder()
		h.handleAuthStart(w, req)

		resp := w.Result()
		if resp.StatusCode != 302 {
			t.Errorf("status: got %d, want 302", resp.StatusCode)
		}

		location := resp.Header.Get("Location")
		if location == "" {
			t.Fatal("expected redirect Location header")
		}
		// Location should contain state param
		if !containsParam(location, "state") {
			t.Errorf("redirect URL %q does not contain state param", location)
		}
	})

	t.Run("github provider redirects to OAuth URL with state param", func(t *testing.T) {
		cfg := newTestOAuthConfig(t)
		h := &adminHandler{cfg: cfg}

		req := newTestRequest(t, "GET", "/admin/auth/github", nil)
		req = withPathValue(req, "provider", "github")
		w := newTestResponseRecorder()
		h.handleAuthStart(w, req)

		resp := w.Result()
		if resp.StatusCode != 302 {
			t.Errorf("status: got %d, want 302", resp.StatusCode)
		}

		location := resp.Header.Get("Location")
		if location == "" {
			t.Fatal("expected redirect Location header")
		}
		if !containsParam(location, "state") {
			t.Errorf("redirect URL %q does not contain state param", location)
		}
	})
}

func TestHandleAuthCallback(t *testing.T) {
	t.Run("invalid state returns 400", func(t *testing.T) {
		cfg := AdminConfig{
			JWTSecret: []byte("test-secret-32-bytes-long-enough!"),
		}
		h := &adminHandler{cfg: cfg}

		req := newTestRequest(t, "GET", "/admin/auth/callback/google?code=abc&state=tampered", nil)
		req = withPathValue(req, "provider", "google")
		// Set a state cookie with a different valid state (mismatch = bad)
		validState, _ := generateState(cfg.JWTSecret)
		req.AddCookie(&httpCookie{Name: "oauth_state", Value: validState})
		w := newTestResponseRecorder()
		h.handleAuthCallback(w, req)

		resp := w.Result()
		if resp.StatusCode != 400 {
			t.Errorf("status: got %d, want 400", resp.StatusCode)
		}
	})

	// Note: valid callback test requires a mock OAuth server — covered separately
	// by the integration test in Phase 7.
}
