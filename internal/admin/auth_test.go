package admin

import (
	"strconv"
	"testing"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"

	"github.com/alferio94/lore/internal/store"
)

func TestIssueAndParseJWT(t *testing.T) {
	cfg := AdminConfig{JWTSecret: []byte("test-secret-32-bytes-long-enough!")}
	user := store.User{ID: 42, Email: "alice@example.com", Name: "Alice", Role: store.UserRoleAdmin}

	tokenStr, err := issueJWT(cfg, user)
	if err != nil {
		t.Fatalf("issueJWT returned error: %v", err)
	}
	claims, err := parseJWT(cfg.JWTSecret, tokenStr)
	if err != nil {
		t.Fatalf("parseJWT returned error: %v", err)
	}
	if claims.Email != user.Email || claims.Name != user.Name || claims.Role != user.Role {
		t.Fatalf("claims = %#v, want email=%q name=%q role=%q", claims, user.Email, user.Name, user.Role)
	}
	if claims.Subject != "42" {
		t.Fatalf("subject: got %q, want %q", claims.Subject, "42")
	}

	expiredClaims := Claims{
		RegisteredClaims: jwtlib.RegisteredClaims{Subject: "42", ExpiresAt: jwtlib.NewNumericDate(time.Now().Add(-1 * time.Hour))},
		Email:            user.Email,
		Name:             user.Name,
		Role:             user.Role,
	}
	expiredToken := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, expiredClaims)
	expiredStr, err := expiredToken.SignedString(cfg.JWTSecret)
	if err != nil {
		t.Fatalf("sign expired token: %v", err)
	}
	if _, err := parseJWT(cfg.JWTSecret, expiredStr); err == nil {
		t.Fatal("expected expired token to be rejected")
	}
}

func TestOAuthState(t *testing.T) {
	secret := []byte("csrf-test-secret-must-be-long-enough!")
	state, err := generateState(secret)
	if err != nil {
		t.Fatalf("generateState error: %v", err)
	}
	if err := validateState(secret, state); err != nil {
		t.Fatalf("validateState valid: %v", err)
	}
	if err := validateState(secret, state+"x"); err == nil {
		t.Fatal("expected tampered state to fail")
	}
}

func TestHandleDevAuthCreatesStoreBackedAdminSession(t *testing.T) {
	s := newTestStoreForAdmin(t)
	cfg := AdminConfig{
		Store:     s,
		JWTSecret: []byte("dev-auth-secret-32-bytes-long-ok!"),
		DevAuth:   true,
	}
	h := &adminHandler{cfg: cfg}

	req := newTestRequest(t, "GET", "/admin/auth/dev", nil)
	w := newTestResponseRecorder()
	h.handleDevAuth(w, req)

	resp := w.Result()
	if resp.StatusCode != 302 {
		t.Fatalf("status: got %d, want 302", resp.StatusCode)
	}
	if resp.Header.Get("Location") != "/admin/" {
		t.Fatalf("Location: got %q, want %q", resp.Header.Get("Location"), "/admin/")
	}

	var sessionCookie string
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookieName {
			sessionCookie = c.Value
		}
	}
	if sessionCookie == "" {
		t.Fatal("expected lore_session cookie")
	}

	claims, err := parseJWT(cfg.JWTSecret, sessionCookie)
	if err != nil {
		t.Fatalf("parseJWT: %v", err)
	}
	if claims.Role != store.UserRoleAdmin {
		t.Fatalf("role: got %q, want %q", claims.Role, store.UserRoleAdmin)
	}
	user, err := s.GetUserByEmail("dev@localhost")
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if claims.Subject != strconv.FormatInt(user.ID, 10) {
		t.Fatalf("subject: got %q, want %q", claims.Subject, strconv.FormatInt(user.ID, 10))
	}
	if user.Role != store.UserRoleAdmin || user.Status != store.UserStatusActive {
		t.Fatalf("dev user role/status = %q/%q, want %q/%q", user.Role, user.Status, store.UserRoleAdmin, store.UserStatusActive)
	}
}
