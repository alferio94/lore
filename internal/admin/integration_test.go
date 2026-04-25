package admin

import (
	"encoding/json"
	"fmt"
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

func setupTestServer(t *testing.T, devAuth bool) (*httptest.Server, *store.Store, []byte) {
	t.Helper()
	s := setupIntegrationStore(t)
	secret := []byte("integration-test-secret-32bytes!")
	mux := http.NewServeMux()
	Mount(mux, AdminConfig{Store: s, JWTSecret: secret, DevAuth: devAuth})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, s, secret
}

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

func makeIntegrationCookie(t *testing.T, secret []byte, user *store.User) *http.Cookie {
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
	signed, err := tok.SignedString(secret)
	if err != nil {
		t.Fatalf("sign JWT: %v", err)
	}
	return &http.Cookie{Name: sessionCookieName, Value: signed}
}

func TestIntegration_DevAuthFullFlow(t *testing.T) {
	ts, _, _ := setupTestServer(t, true)
	client := newClientWithJar(t, true)

	resp, err := client.Get(ts.URL + "/admin/auth/dev")
	if err != nil {
		t.Fatalf("GET /admin/auth/dev: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status: got %d, want 302", resp.StatusCode)
	}
	if resp.Header.Get("Location") != "/admin/" {
		t.Fatalf("location: got %q, want %q", resp.Header.Get("Location"), "/admin/")
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

	req, err := http.NewRequest("GET", ts.URL+"/admin/api/skills", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sessionCookie})

	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /admin/api/skills: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp2.Body)
		t.Fatalf("status: got %d, want 200; body: %s", resp2.StatusCode, body)
	}
}

func TestIntegration_RegisterLoginApprovalFlow(t *testing.T) {
	ts, s, secret := setupTestServer(t, false)
	client := newClientWithJar(t, true)

	registerResp, err := client.Post(ts.URL+"/admin/auth/register", "application/json", strings.NewReader(`{"email":"pending@example.com","name":"Pending User","password":"super-secret-password"}`))
	if err != nil {
		t.Fatalf("POST /admin/auth/register: %v", err)
	}
	defer registerResp.Body.Close()
	if registerResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(registerResp.Body)
		t.Fatalf("register status: got %d, want 201; body: %s", registerResp.StatusCode, body)
	}

	registered, err := s.GetUserByEmail("pending@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if registered.Role != store.UserRoleNA || registered.Status != store.UserStatusPending {
		t.Fatalf("registered user = role %q status %q, want %q/%q", registered.Role, registered.Status, store.UserRoleNA, store.UserStatusPending)
	}

	pendingLoginResp, err := client.Post(ts.URL+"/admin/auth/login", "application/json", strings.NewReader(`{"email":"pending@example.com","password":"super-secret-password"}`))
	if err != nil {
		t.Fatalf("POST /admin/auth/login pending: %v", err)
	}
	defer pendingLoginResp.Body.Close()
	if pendingLoginResp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(pendingLoginResp.Body)
		t.Fatalf("pending login status: got %d, want 403; body: %s", pendingLoginResp.StatusCode, body)
	}

	invalidLoginResp, err := client.Post(ts.URL+"/admin/auth/login", "application/json", strings.NewReader(`{"email":"pending@example.com","password":"wrong-password"}`))
	if err != nil {
		t.Fatalf("POST /admin/auth/login invalid: %v", err)
	}
	defer invalidLoginResp.Body.Close()
	if invalidLoginResp.StatusCode != http.StatusUnauthorized {
		body, _ := io.ReadAll(invalidLoginResp.Body)
		t.Fatalf("invalid login status: got %d, want 401; body: %s", invalidLoginResp.StatusCode, body)
	}

	adminUser, err := s.UpsertUser("admin@example.com", "Admin", "", "bootstrap")
	if err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	adminUser, err = s.UpdateUserStatusRole(adminUser.ID, store.UserStatusActive, store.UserRoleAdmin)
	if err != nil {
		t.Fatalf("promote admin: %v", err)
	}

	approveReq, err := http.NewRequest("PATCH", fmt.Sprintf("%s/admin/api/users/%d", ts.URL, registered.ID), strings.NewReader(`{"role":"developer","status":"active"}`))
	if err != nil {
		t.Fatalf("build approve request: %v", err)
	}
	approveReq.Header.Set("Content-Type", "application/json")
	approveReq.AddCookie(makeIntegrationCookie(t, secret, adminUser))
	approveResp, err := http.DefaultClient.Do(approveReq)
	if err != nil {
		t.Fatalf("PATCH /admin/api/users/{id}: %v", err)
	}
	defer approveResp.Body.Close()
	if approveResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(approveResp.Body)
		t.Fatalf("approve status: got %d, want 200; body: %s", approveResp.StatusCode, body)
	}

	loginResp, err := client.Post(ts.URL+"/admin/auth/login", "application/json", strings.NewReader(`{"email":"pending@example.com","password":"super-secret-password"}`))
	if err != nil {
		t.Fatalf("POST /admin/auth/login active: %v", err)
	}
	defer loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(loginResp.Body)
		t.Fatalf("active login status: got %d, want 200; body: %s", loginResp.StatusCode, body)
	}
	var loginPayload map[string]any
	if err := json.NewDecoder(loginResp.Body).Decode(&loginPayload); err != nil {
		t.Fatalf("decode login payload: %v", err)
	}
	if _, ok := loginPayload["token"].(string); !ok {
		t.Fatal("expected token in login payload")
	}

	meReq, err := http.NewRequest("GET", ts.URL+"/admin/api/me", nil)
	if err != nil {
		t.Fatalf("build me request: %v", err)
	}
	meResp, err := client.Do(meReq)
	if err != nil {
		t.Fatalf("GET /admin/api/me: %v", err)
	}
	defer meResp.Body.Close()
	if meResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(meResp.Body)
		t.Fatalf("me status: got %d, want 200; body: %s", meResp.StatusCode, body)
	}
	var me store.User
	if err := json.NewDecoder(meResp.Body).Decode(&me); err != nil {
		t.Fatalf("decode me payload: %v", err)
	}
	if me.Role != store.UserRoleDeveloper || me.Status != store.UserStatusActive {
		t.Fatalf("me payload role/status = %q/%q, want %q/%q", me.Role, me.Status, store.UserRoleDeveloper, store.UserStatusActive)
	}

	if _, err := s.UpdateUserStatusRole(registered.ID, store.UserStatusDisabled, store.UserRoleDeveloper); err != nil {
		t.Fatalf("disable user: %v", err)
	}
	meAfterDisableReq, err := http.NewRequest("GET", ts.URL+"/admin/api/me", nil)
	if err != nil {
		t.Fatalf("build me-after-disable request: %v", err)
	}
	meAfterDisableResp, err := client.Do(meAfterDisableReq)
	if err != nil {
		t.Fatalf("GET /admin/api/me after disable: %v", err)
	}
	defer meAfterDisableResp.Body.Close()
	if meAfterDisableResp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(meAfterDisableResp.Body)
		t.Fatalf("me after disable status: got %d, want 403; body: %s", meAfterDisableResp.StatusCode, body)
	}

	disabledLoginResp, err := client.Post(ts.URL+"/admin/auth/login", "application/json", strings.NewReader(`{"email":"pending@example.com","password":"super-secret-password"}`))
	if err != nil {
		t.Fatalf("POST /admin/auth/login disabled: %v", err)
	}
	defer disabledLoginResp.Body.Close()
	if disabledLoginResp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(disabledLoginResp.Body)
		t.Fatalf("disabled login status: got %d, want 403; body: %s", disabledLoginResp.StatusCode, body)
	}

	logoutReq, err := http.NewRequest("POST", ts.URL+"/admin/auth/logout", nil)
	if err != nil {
		t.Fatalf("build logout request: %v", err)
	}
	logoutResp, err := client.Do(logoutReq)
	if err != nil {
		t.Fatalf("POST /admin/auth/logout: %v", err)
	}
	defer logoutResp.Body.Close()
	if logoutResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(logoutResp.Body)
		t.Fatalf("logout status: got %d, want 200; body: %s", logoutResp.StatusCode, body)
	}

	meAfterLogoutReq, err := http.NewRequest("GET", ts.URL+"/admin/api/me", nil)
	if err != nil {
		t.Fatalf("build me-after-logout request: %v", err)
	}
	meAfterLogoutResp, err := client.Do(meAfterLogoutReq)
	if err != nil {
		t.Fatalf("GET /admin/api/me after logout: %v", err)
	}
	defer meAfterLogoutResp.Body.Close()
	if meAfterLogoutResp.StatusCode != http.StatusUnauthorized {
		body, _ := io.ReadAll(meAfterLogoutResp.Body)
		t.Fatalf("me after logout status: got %d, want 401; body: %s", meAfterLogoutResp.StatusCode, body)
	}
}

func TestIntegration_OAuthPendingApproval(t *testing.T) {
	origFn := fetchGitHubUserInfoFn
	t.Cleanup(func() { fetchGitHubUserInfoFn = origFn })

	makeTokenServer := func(t *testing.T) *httptest.Server {
		t.Helper()
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
				"access_token": "fake-access-token",
				"token_type":   "Bearer",
			})
		}))
	}

	t.Run("new oauth user is marked pending and denied session", func(t *testing.T) {
		fetchGitHubUserInfoFn = func(_ string) (*oauthUserInfo, error) {
			return &oauthUserInfo{Email: "oauth-user@example.com", Name: "OAuth User"}, nil
		}

		fakeTokenServer := makeTokenServer(t)
		defer fakeTokenServer.Close()

		s := setupIntegrationStore(t)
		cfg := AdminConfig{
			Store:     s,
			JWTSecret: []byte("oauth-integration-secret-32bytes"),
			GithubOAuth: &oauth2.Config{
				ClientID:     "test-client-id",
				ClientSecret: "test-client-secret",
				Scopes:       []string{"user:email"},
				Endpoint: oauth2.Endpoint{
					AuthURL:  fakeTokenServer.URL + "/auth",
					TokenURL: fakeTokenServer.URL + "/token",
				},
			},
		}
		mux := http.NewServeMux()
		Mount(mux, cfg)
		ts := httptest.NewServer(mux)
		defer ts.Close()
		cfg.GithubOAuth.RedirectURL = ts.URL + "/admin/auth/callback/github"

		client := newClientWithJar(t, true)
		resp1, err := client.Get(ts.URL + "/admin/auth/github")
		if err != nil {
			t.Fatalf("GET /admin/auth/github: %v", err)
		}
		resp1.Body.Close()
		var state string
		for _, c := range resp1.Cookies() {
			if c.Name == stateCookieName {
				state = c.Value
			}
		}
		if state == "" {
			t.Fatal("expected oauth_state cookie")
		}

		req, err := http.NewRequest("GET", ts.URL+"/admin/auth/callback/github?code=fake-code&state="+state, nil)
		if err != nil {
			t.Fatalf("build callback request: %v", err)
		}
		req.AddCookie(&http.Cookie{Name: stateCookieName, Value: state})
		resp2, err := client.Do(req)
		if err != nil {
			t.Fatalf("GET callback: %v", err)
		}
		defer resp2.Body.Close()
		if resp2.StatusCode != http.StatusForbidden {
			body, _ := io.ReadAll(resp2.Body)
			t.Fatalf("callback status: got %d, want 403; body: %s", resp2.StatusCode, body)
		}
		for _, c := range resp2.Cookies() {
			if c.Name == sessionCookieName && c.Value != "" {
				t.Fatal("expected no session cookie for pending oauth user")
			}
		}

		user, err := s.GetUserByEmail("oauth-user@example.com")
		if err != nil {
			t.Fatalf("GetUserByEmail: %v", err)
		}
		if user.Role != store.UserRoleNA || user.Status != store.UserStatusPending {
			t.Fatalf("user role/status = %q/%q, want %q/%q", user.Role, user.Status, store.UserRoleNA, store.UserStatusPending)
		}
	})

	t.Run("existing active oauth user receives session", func(t *testing.T) {
		fetchGitHubUserInfoFn = func(_ string) (*oauthUserInfo, error) {
			return &oauthUserInfo{Email: "oauth-active@example.com", Name: "OAuth Active"}, nil
		}

		fakeTokenServer := makeTokenServer(t)
		defer fakeTokenServer.Close()

		s := setupIntegrationStore(t)
		user, err := s.UpsertUser("oauth-active@example.com", "OAuth Active", "", "github")
		if err != nil {
			t.Fatalf("seed user: %v", err)
		}
		if _, err := s.UpdateUserStatusRole(user.ID, store.UserStatusActive, store.UserRoleDeveloper); err != nil {
			t.Fatalf("approve user: %v", err)
		}

		cfg := AdminConfig{
			Store:     s,
			JWTSecret: []byte("oauth-integration-secret-32bytes"),
			GithubOAuth: &oauth2.Config{
				ClientID:     "test-client-id",
				ClientSecret: "test-client-secret",
				Scopes:       []string{"user:email"},
				Endpoint: oauth2.Endpoint{
					AuthURL:  fakeTokenServer.URL + "/auth",
					TokenURL: fakeTokenServer.URL + "/token",
				},
			},
		}
		mux := http.NewServeMux()
		Mount(mux, cfg)
		ts := httptest.NewServer(mux)
		defer ts.Close()
		cfg.GithubOAuth.RedirectURL = ts.URL + "/admin/auth/callback/github"

		client := newClientWithJar(t, true)
		resp1, err := client.Get(ts.URL + "/admin/auth/github")
		if err != nil {
			t.Fatalf("GET /admin/auth/github: %v", err)
		}
		resp1.Body.Close()
		var state string
		for _, c := range resp1.Cookies() {
			if c.Name == stateCookieName {
				state = c.Value
			}
		}
		if state == "" {
			t.Fatal("expected oauth_state cookie")
		}

		req, err := http.NewRequest("GET", ts.URL+"/admin/auth/callback/github?code=fake-code&state="+state, nil)
		if err != nil {
			t.Fatalf("build callback request: %v", err)
		}
		req.AddCookie(&http.Cookie{Name: stateCookieName, Value: state})
		resp2, err := client.Do(req)
		if err != nil {
			t.Fatalf("GET callback: %v", err)
		}
		defer resp2.Body.Close()
		if resp2.StatusCode != http.StatusFound {
			body, _ := io.ReadAll(resp2.Body)
			t.Fatalf("callback status: got %d, want 302; body: %s", resp2.StatusCode, body)
		}

		var sessionCookie string
		for _, c := range resp2.Cookies() {
			if c.Name == sessionCookieName {
				sessionCookie = c.Value
			}
		}
		if sessionCookie == "" {
			t.Fatal("expected session cookie for active oauth user")
		}
	})
}

func TestIntegration_RBACEnforcement(t *testing.T) {
	buildRequest := func(t *testing.T, role, path string, body string) int {
		t.Helper()
		s := setupIntegrationStore(t)
		_, _ = s.CreateSkill(store.CreateSkillParams{Name: "skill", DisplayName: "skill", Content: "content", ChangedBy: "seed"})
		actor, err := s.UpsertUser("actor@example.com", "Actor", "", "github")
		if err != nil {
			t.Fatalf("seed actor: %v", err)
		}
		actor, err = s.UpdateUserStatusRole(actor.ID, store.UserStatusActive, role)
		if err != nil {
			t.Fatalf("set actor role: %v", err)
		}

		secret := []byte("rbac-integration-secret-32bytes!")
		mux := http.NewServeMux()
		Mount(mux, AdminConfig{Store: s, JWTSecret: secret})
		ts := httptest.NewServer(mux)
		defer ts.Close()

		var reqBody io.Reader
		if body != "" {
			reqBody = strings.NewReader(body)
		}
		req, err := http.NewRequest(http.MethodGet, ts.URL+path, reqBody)
		if err != nil {
			t.Fatalf("build request: %v", err)
		}
		if body != "" {
			req.Method = http.MethodPost
			req.Header.Set("Content-Type", "application/json")
		}
		req.AddCookie(makeIntegrationCookie(t, secret, actor))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("do request: %v", err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	if status := buildRequest(t, store.UserRoleDeveloper, "/admin/api/skills", ""); status != http.StatusOK {
		t.Fatalf("developer GET skills: got %d, want 200", status)
	}
	if status := buildRequest(t, store.UserRoleDeveloper, "/admin/api/skills", `{"name":"new-skill","content":"body"}`); status != http.StatusForbidden {
		t.Fatalf("developer POST skills: got %d, want 403", status)
	}
	if status := buildRequest(t, store.UserRoleAdmin, "/admin/api/users", ""); status != http.StatusOK {
		t.Fatalf("admin GET users: got %d, want 200", status)
	}

	t.Run("developer cannot patch another user", func(t *testing.T) {
		s := setupIntegrationStore(t)
		actor, err := s.UpsertUser("actor@example.com", "Actor", "", "github")
		if err != nil {
			t.Fatalf("seed actor: %v", err)
		}
		actor, err = s.UpdateUserStatusRole(actor.ID, store.UserStatusActive, store.UserRoleDeveloper)
		if err != nil {
			t.Fatalf("set actor role: %v", err)
		}
		target, err := s.CreatePendingUser("pending2@example.com", "Pending Two", "hashed")
		if err != nil {
			t.Fatalf("seed target: %v", err)
		}

		secret := []byte("rbac-integration-secret-32bytes!")
		mux := http.NewServeMux()
		Mount(mux, AdminConfig{Store: s, JWTSecret: secret})
		ts := httptest.NewServer(mux)
		defer ts.Close()

		req, err := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/admin/api/users/%d", ts.URL, target.ID), strings.NewReader(`{"role":"developer","status":"active"}`))
		if err != nil {
			t.Fatalf("build patch request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(makeIntegrationCookie(t, secret, actor))

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("PATCH /admin/api/users/{id}: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("non-admin patch status: got %d, want 403; body: %s", resp.StatusCode, body)
		}
	})
}
