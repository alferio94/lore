package admin

// embed_test.go — TDD tests for the SPA fallback handler.
// Phase 6: Frontend Embed — task 6.1 (RED), 6.2 (GREEN)

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSPAFallback exercises the embedded static file handler registered by
// Mount under GET /admin/{path...}. The handler must:
//   - Return 200 + index.html for /admin/ (root)
//   - Return 200 + index.html for any path that doesn't map to a real file (SPA routing)
//   - Return 200 + the real file content for actual embedded files (e.g. main.js)
func TestSPAFallback(t *testing.T) {
	mux := http.NewServeMux()
	cfg := AdminConfig{
		JWTSecret: []byte("embed-test-secret-32-bytes-long!!"),
		DevAuth:   false,
	}
	Mount(mux, cfg)

	t.Run("GET /admin/ returns index.html 200", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "<html") {
			t.Errorf("expected HTML body, got: %s", body)
		}
		ct := rec.Header().Get("Content-Type")
		if !strings.Contains(ct, "text/html") {
			t.Errorf("expected text/html Content-Type, got %q", ct)
		}
	})

	t.Run("GET /admin/dashboard returns index.html SPA fallback", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "<html") {
			t.Errorf("expected HTML SPA fallback, got: %s", body)
		}
	})

	t.Run("GET /admin/skills/list returns index.html SPA fallback", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/skills/list", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "<html") {
			t.Errorf("expected HTML SPA fallback, got: %s", body)
		}
	})
}
