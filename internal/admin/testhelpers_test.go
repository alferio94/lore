package admin

// Test helpers for the admin package.
// These are only compiled during tests.

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

// httpCookie is a type alias so tests can construct http.Cookie without
// importing net/http directly in every file.
type httpCookie = http.Cookie

// cookieResult holds a subset of cookie attributes needed by tests.
type cookieResult struct {
	Value    string
	HttpOnly bool
}

// newTestRequest builds a *http.Request for testing. body may be nil.
func newTestRequest(t *testing.T, method, target string, body *strings.Reader) *http.Request {
	t.Helper()
	var r *http.Request
	var err error
	if body != nil {
		r, err = http.NewRequest(method, target, body)
	} else {
		r, err = http.NewRequest(method, target, nil)
	}
	if err != nil {
		t.Fatalf("newTestRequest: %v", err)
	}
	return r
}

// newTestResponseRecorder returns an httptest.ResponseRecorder.
func newTestResponseRecorder() *httptest.ResponseRecorder {
	return httptest.NewRecorder()
}

// withPathValue clones r and sets the named path value (Go 1.22+ PathValue).
func withPathValue(r *http.Request, name, value string) *http.Request {
	// Go 1.22 mux PathValue requires a pattern to have been matched; for unit
	// tests we use SetPathValue directly.
	r2 := r.Clone(r.Context())
	r2.SetPathValue(name, value)
	return r2
}

// newTestOAuthConfig builds an AdminConfig with stub Google and GitHub
// oauth2.Config pointing at a fake endpoint (tests redirect only, no exchange).
func newTestOAuthConfig(t *testing.T) AdminConfig {
	t.Helper()
	return AdminConfig{
		JWTSecret: []byte("test-secret-32-bytes-long-enough!"),
		GoogleOAuth: &oauth2.Config{
			ClientID:     "google-test-id",
			ClientSecret: "google-test-secret",
			RedirectURL:  "http://localhost/admin/auth/callback/google",
			Scopes:       []string{"openid", "email", "profile"},
			Endpoint: oauth2.Endpoint{
				AuthURL:  "https://accounts.google.com/o/oauth2/auth",
				TokenURL: "https://oauth2.googleapis.com/token",
			},
		},
		GithubOAuth: &oauth2.Config{
			ClientID:     "github-test-id",
			ClientSecret: "github-test-secret",
			RedirectURL:  "http://localhost/admin/auth/callback/github",
			Scopes:       []string{"user:email"},
			Endpoint: oauth2.Endpoint{
				AuthURL:  "https://github.com/login/oauth/authorize",
				TokenURL: "https://github.com/login/oauth/access_token",
			},
		},
	}
}

// containsParam checks whether rawURL contains a query parameter named key.
func containsParam(rawURL, key string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	_, ok := u.Query()[key]
	return ok
}
