package admin

// integration_test.go — End-to-end integration tests for the admin package.
//
// These tests use a real http.ServeMux with Mount() and httptest.Server to exercise
// the full request/response cycle including authentication, cookies, and RBAC.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
	"golang.org/x/oauth2"

	"github.com/alferio94/lore/internal/store"
)

// ─── Test server helpers ──────────────────────────────────────────────────────

// setupIntegrationStore creates a fresh in-memory store for integration tests.
func setupIntegrationStore(t *testing.T) *store.Store {
	t.Helper()
	cfg, err := store.DefaultConfig()
	if err != nil {
		t.Fatalf("store.DefaultConfig: %v", err)
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

// setupTestServer creates a real mux with admin routes mounted, backed by a
// temp store. Returns the test server and the store for direct inspection.
func setupTestServer(t *testing.T, devAuth bool) (*httptest.Server, *store.Store) {
	t.Helper()
	s := setupIntegrationStore(t)
	mux := http.NewServeMux()
	cfg := AdminConfig{
		Store:     s,
		JWTSecret: []byte("integration-test-secret-32bytes!"),
		DevAuth:   devAuth,
	}
	Mount(mux, cfg)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, s
}

// setupTestServerWithOAuth creates a test server with custom AdminConfig,
// allowing OAuth provider URLs to be injected.
func setupTestServerWithConfig(t *testing.T, cfg AdminConfig, s *store.Store) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	cfg.Store = s
	Mount(mux, cfg)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

// newClientWithJar returns an http.Client with a cookie jar and redirect
// control. stopRedirects=true disables automatic redirect following.
func newClientWithJar(t *testing.T, stopRedirects bool) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	client := &http.Client{Jar: jar}
	if stopRedirects {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}
	return client
}

// makeIntegrationCookie creates a valid JWT session cookie for integration tests.
func makeIntegrationCookie(t *testing.T, secret []byte, email, role string) *http.Cookie {
	t.Helper()
	c := Claims{
		RegisteredClaims: jwtlib.RegisteredClaims{
			Subject:   "999",
			ExpiresAt: jwtlib.NewNumericDate(time.Now().Add(24 * time.Hour)),
		},
		Email: email,
		Name:  "Integration User",
		Role:  role,
	}
	tok := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, c)
	signed, err := tok.SignedString(secret)
	if err != nil {
		t.Fatalf("sign JWT: %v", err)
	}
	return &http.Cookie{Name: sessionCookieName, Value: signed}
}

// ─── 7.1 Dev-auth full flow ───────────────────────────────────────────────────

func TestIntegration_DevAuthFullFlow(t *testing.T) {
	ts, _ := setupTestServer(t, true /* DevAuth=true */)

	// Client with jar that stops redirects so we can inspect each step.
	client := newClientWithJar(t, true)

	// Step 1: GET /admin/auth/dev → should get 302 redirect + set cookie.
	resp, err := client.Get(ts.URL + "/admin/auth/dev")
	if err != nil {
		t.Fatalf("GET /admin/auth/dev: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Errorf("step 1 status: got %d, want 302", resp.StatusCode)
	}

	location := resp.Header.Get("Location")
	if location != "/admin/" {
		t.Errorf("step 1 Location: got %q, want /admin/", location)
	}

	// Confirm cookie was set.
	var sessionCookieVal string
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookieName {
			sessionCookieVal = c.Value
			break
		}
	}
	if sessionCookieVal == "" {
		t.Fatal("step 1: lore_session cookie not set")
	}

	// Step 2: GET /admin/api/skills — the jar should carry the cookie, but since
	// we stopped redirects the jar may not have stored cookies for "". Use a
	// fresh client that follows redirects and manually set the cookie.
	req2, err := http.NewRequest("GET", ts.URL+"/admin/api/skills", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req2.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sessionCookieVal})

	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("GET /admin/api/skills: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp2.Body)
		t.Errorf("step 2 status: got %d, want 200; body: %s", resp2.StatusCode, body)
	}
}

// TestIntegration_DevAuthDisabledReturns404 verifies that when DevAuth=false
// the route is not registered and returns 404.
func TestIntegration_DevAuthDisabledReturns404(t *testing.T) {
	ts, _ := setupTestServer(t, false /* DevAuth=false */)

	resp, err := http.Get(ts.URL + "/admin/auth/dev")
	if err != nil {
		t.Fatalf("GET /admin/auth/dev: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", resp.StatusCode)
	}
}

// ─── 7.2 Mock OAuth full flow ─────────────────────────────────────────────────

// TestIntegration_MockOAuthFullFlow exercises the full GitHub OAuth flow using
// a fake token endpoint and an injectable userinfo function. The test is in
// package admin so it can override fetchGitHubUserInfoFn directly.
func TestIntegration_MockOAuthFullFlow(t *testing.T) {
	// Restore the real userinfo function after the test.
	origFn := fetchGitHubUserInfoFn
	t.Cleanup(func() { fetchGitHubUserInfoFn = origFn })

	// Stub out GitHub userinfo so we don't call api.github.com.
	fetchGitHubUserInfoFn = func(_ string) (*oauthUserInfo, error) {
		return &oauthUserInfo{
			Email: "oauth-user@example.com",
			Name:  "OAuth User",
		}, nil
	}

	// Build a fake token endpoint (the oauth2 library hits this during Exchange).
	fakeTokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
			"access_token": "fake-access-token",
			"token_type":   "Bearer",
		})
	}))
	defer fakeTokenServer.Close()

	s := setupIntegrationStore(t)
	jwtSecret := []byte("oauth-integration-secret-32bytes")

	oauthCfg := &oauth2.Config{
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		Scopes:       []string{"user:email"},
		Endpoint: oauth2.Endpoint{
			AuthURL:  fakeTokenServer.URL + "/auth",
			TokenURL: fakeTokenServer.URL + "/token",
		},
	}

	mux := http.NewServeMux()
	cfg := AdminConfig{
		Store:       s,
		JWTSecret:   jwtSecret,
		DevAuth:     false,
		GithubOAuth: oauthCfg,
	}
	Mount(mux, cfg)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	// Patch redirect URL with the real test server URL.
	oauthCfg.RedirectURL = ts.URL + "/admin/auth/callback/github"

	// Step 1: GET /admin/auth/github → 302 + oauth_state cookie.
	client := newClientWithJar(t, true)
	resp1, err := client.Get(ts.URL + "/admin/auth/github")
	if err != nil {
		t.Fatalf("GET /admin/auth/github: %v", err)
	}
	resp1.Body.Close()

	if resp1.StatusCode != http.StatusFound {
		t.Errorf("step 1 status: got %d, want 302", resp1.StatusCode)
	}

	var stateCookieVal string
	for _, c := range resp1.Cookies() {
		if c.Name == stateCookieName {
			stateCookieVal = c.Value
			break
		}
	}
	if stateCookieVal == "" {
		t.Fatal("step 1: oauth_state cookie not set")
	}

	// Step 2: Simulate provider redirecting back with code+state.
	callbackURL := ts.URL + "/admin/auth/callback/github?code=fake-code&state=" + stateCookieVal
	req2, err := http.NewRequest("GET", callbackURL, nil)
	if err != nil {
		t.Fatalf("build callback request: %v", err)
	}
	req2.AddCookie(&http.Cookie{Name: stateCookieName, Value: stateCookieVal})

	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatalf("GET /admin/auth/callback/github: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(resp2.Body)
		t.Fatalf("step 2 status: got %d, want 302; body: %s", resp2.StatusCode, body)
	}

	// Extract the session cookie.
	var sessionCookieVal string
	for _, c := range resp2.Cookies() {
		if c.Name == sessionCookieName {
			sessionCookieVal = c.Value
			break
		}
	}
	if sessionCookieVal == "" {
		t.Fatal("step 2: lore_session cookie not set after callback")
	}

	// Step 3: User should be upserted in the DB.
	user, err := s.GetUserByEmail("oauth-user@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if user.Email != "oauth-user@example.com" {
		t.Errorf("email: got %q, want oauth-user@example.com", user.Email)
	}

	// Step 4: Protected route is accessible with the session cookie.
	req3, err := http.NewRequest("GET", ts.URL+"/admin/api/skills", nil)
	if err != nil {
		t.Fatalf("build skills request: %v", err)
	}
	req3.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sessionCookieVal})

	resp3, err := http.DefaultClient.Do(req3)
	if err != nil {
		t.Fatalf("GET /admin/api/skills: %v", err)
	}
	defer resp3.Body.Close()

	if resp3.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp3.Body)
		t.Errorf("step 4 status: got %d, want 200; body: %s", resp3.StatusCode, body)
	}
}

// ─── 7.3 First-user-gets-admin ────────────────────────────────────────────────

func TestIntegration_FirstUserGetsAdmin(t *testing.T) {
	t.Run("first OAuth user gets admin role", func(t *testing.T) {
		origFn := fetchGitHubUserInfoFn
		t.Cleanup(func() { fetchGitHubUserInfoFn = origFn })
		fetchGitHubUserInfoFn = func(_ string) (*oauthUserInfo, error) {
			return &oauthUserInfo{Email: "first@example.com", Name: "First User"}, nil
		}

		fakeToken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"access_token": "tok1", "token_type": "Bearer"}) //nolint:errcheck
		}))
		defer fakeToken.Close()

		s := setupIntegrationStore(t)
		jwtSecret := []byte("first-user-admin-secret-32bytes!")
		oauthCfg := &oauth2.Config{
			ClientID:     "cid",
			ClientSecret: "csec",
			Scopes:       []string{"user:email"},
			Endpoint: oauth2.Endpoint{
				AuthURL:  fakeToken.URL + "/auth",
				TokenURL: fakeToken.URL + "/token",
			},
		}
		mux := http.NewServeMux()
		Mount(mux, AdminConfig{
			Store:       s,
			JWTSecret:   jwtSecret,
			GithubOAuth: oauthCfg,
		})
		ts := httptest.NewServer(mux)
		defer ts.Close()
		oauthCfg.RedirectURL = ts.URL + "/admin/auth/callback/github"

		// Trigger the OAuth flow: get state.
		client := newClientWithJar(t, true)
		resp1, err := client.Get(ts.URL + "/admin/auth/github")
		if err != nil {
			t.Fatalf("GET /admin/auth/github: %v", err)
		}
		resp1.Body.Close()

		var stateVal string
		for _, c := range resp1.Cookies() {
			if c.Name == stateCookieName {
				stateVal = c.Value
			}
		}

		// Complete the callback.
		req2, _ := http.NewRequest("GET", ts.URL+"/admin/auth/callback/github?code=c&state="+stateVal, nil)
		req2.AddCookie(&http.Cookie{Name: stateCookieName, Value: stateVal})
		resp2, err := client.Do(req2)
		if err != nil {
			t.Fatalf("callback: %v", err)
		}
		resp2.Body.Close()

		// Check user in DB has role=admin.
		user, err := s.GetUserByEmail("first@example.com")
		if err != nil {
			t.Fatalf("GetUserByEmail: %v", err)
		}
		if user.Role != "admin" {
			t.Errorf("first user role: got %q, want admin", user.Role)
		}
	})

	t.Run("second user gets viewer role", func(t *testing.T) {
		s := setupIntegrationStore(t)

		// Pre-seed a first user so the DB is non-empty.
		_, err := s.UpsertUser("first@example.com", "First", "", "google")
		if err != nil {
			t.Fatalf("seed first user: %v", err)
		}

		// Upsert the second user directly.
		second, err := s.UpsertUser("second@example.com", "Second", "", "google")
		if err != nil {
			t.Fatalf("upsert second user: %v", err)
		}

		if second.Role != "viewer" {
			t.Errorf("second user role: got %q, want viewer", second.Role)
		}
	})
}

// ─── 7.4 RBAC enforcement ─────────────────────────────────────────────────────

func TestIntegration_RBACEnforcement(t *testing.T) {
	jwtSecret := []byte("rbac-integration-secret-32bytes!")

	// Helper to build a server and run a request with the given role.
	runRequest := func(t *testing.T, method, path, role string, body string) int {
		t.Helper()
		s := setupIntegrationStore(t)
		// Seed a skill so GET routes return real data.
		_, _ = s.CreateSkill(store.CreateSkillParams{
			Name:      "test-skill",
			Content:   "test content",
			ChangedBy: "seed",
		})
		// Seed a user so user routes work.
		_, _ = s.UpsertUser("seed@example.com", "Seed", "", "google")

		mux := http.NewServeMux()
		Mount(mux, AdminConfig{
			Store:     s,
			JWTSecret: jwtSecret,
		})
		ts2 := httptest.NewServer(mux)
		t.Cleanup(ts2.Close)

		var reqBody io.Reader
		if body != "" {
			reqBody = strings.NewReader(body)
		}
		req, err := http.NewRequest(method, ts2.URL+path, reqBody)
		if err != nil {
			t.Fatalf("build request: %v", err)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		req.AddCookie(makeIntegrationCookie(t, jwtSecret, "user@example.com", role))

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("do request: %v", err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	t.Run("viewer can GET /admin/api/skills", func(t *testing.T) {
		status := runRequest(t, "GET", "/admin/api/skills", "viewer", "")
		if status != http.StatusOK {
			t.Errorf("viewer GET skills: got %d, want 200", status)
		}
	})

	t.Run("viewer cannot POST /admin/api/skills (needs tech_lead)", func(t *testing.T) {
		body := `{"name":"new-skill","content":"body"}`
		status := runRequest(t, "POST", "/admin/api/skills", "viewer", body)
		if status != http.StatusForbidden {
			t.Errorf("viewer POST skills: got %d, want 403", status)
		}
	})

	t.Run("tech_lead can POST /admin/api/skills", func(t *testing.T) {
		body := `{"name":"tech-skill","content":"body"}`
		status := runRequest(t, "POST", "/admin/api/skills", "tech_lead", body)
		if status != http.StatusCreated {
			t.Errorf("tech_lead POST skills: got %d, want 201", status)
		}
	})

	t.Run("tech_lead cannot DELETE /admin/api/skills/{name} (needs admin)", func(t *testing.T) {
		status := runRequest(t, "DELETE", "/admin/api/skills/test-skill", "tech_lead", "")
		if status != http.StatusForbidden {
			t.Errorf("tech_lead DELETE skill: got %d, want 403", status)
		}
	})

	t.Run("admin can DELETE /admin/api/skills/{name}", func(t *testing.T) {
		status := runRequest(t, "DELETE", "/admin/api/skills/test-skill", "admin", "")
		if status != http.StatusNoContent {
			t.Errorf("admin DELETE skill: got %d, want 204", status)
		}
	})

	t.Run("viewer cannot GET /admin/api/users (needs admin)", func(t *testing.T) {
		status := runRequest(t, "GET", "/admin/api/users", "viewer", "")
		if status != http.StatusForbidden {
			t.Errorf("viewer GET users: got %d, want 403", status)
		}
	})

	t.Run("admin can GET /admin/api/users", func(t *testing.T) {
		status := runRequest(t, "GET", "/admin/api/users", "admin", "")
		if status != http.StatusOK {
			t.Errorf("admin GET users: got %d, want 200", status)
		}
	})

	t.Run("unauthenticated request returns 401", func(t *testing.T) {
		ts, _ := setupTestServer(t, false)
		resp, err := http.Get(ts.URL + "/admin/api/skills")
		if err != nil {
			t.Fatalf("GET /admin/api/skills: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("unauthenticated: got %d, want 401", resp.StatusCode)
		}
	})

	t.Run("viewer can GET /admin/api/me", func(t *testing.T) {
		status := runRequest(t, "GET", "/admin/api/me", "viewer", "")
		if status != http.StatusOK {
			t.Errorf("viewer GET me: got %d, want 200", status)
		}
	})
}
