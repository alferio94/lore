// Package admin provides the web admin backend for Lore.
//
// It mounts OAuth2 authentication, JWT session management, RBAC middleware,
// and REST API endpoints under the /admin/ prefix on an existing http.ServeMux.
// Call admin.Mount(mux, cfg) from cmdServe() after building AdminConfig.
package admin

import (
	"context"
	"encoding/json"
	"net/http"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
	"golang.org/x/oauth2/google"

	"github.com/alferio94/lore/internal/store"
	jwtlib "github.com/golang-jwt/jwt/v5"
)

// ─── Config ──────────────────────────────────────────────────────────────────

// AdminConfig holds all configuration needed by the admin package.
type AdminConfig struct {
	Store              *store.Store
	JWTSecret          []byte         // from LORE_JWT_SECRET or auto-generated
	GoogleClientID     string         // from LORE_GOOGLE_CLIENT_ID
	GoogleClientSecret string         // from LORE_GOOGLE_CLIENT_SECRET
	GitHubClientID     string         // from LORE_GITHUB_CLIENT_ID
	GitHubClientSecret string         // from LORE_GITHUB_CLIENT_SECRET
	DevAuth            bool           // --dev-auth flag
	CookieSecure       bool           // session/oauth cookie Secure attribute
	BaseURL            string         // e.g. "http://localhost:7437"
	GoogleOAuth        *oauth2.Config // nil if creds not configured
	GithubOAuth        *oauth2.Config // nil if creds not configured
}

// ─── Claims ──────────────────────────────────────────────────────────────────

// Claims holds the JWT payload for admin sessions.
type Claims struct {
	jwtlib.RegisteredClaims
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  string `json:"role"` // "admin" | "tech_lead" | "viewer"
}

// ─── Context helpers ─────────────────────────────────────────────────────────

type ctxKey string

const claimsCtxKey ctxKey = "claims"

// claimsFromCtx retrieves Claims injected by requireAuth middleware.
func claimsFromCtx(ctx context.Context) (*Claims, bool) {
	c, ok := ctx.Value(claimsCtxKey).(*Claims)
	return c, ok
}

// withClaims returns a new context with claims attached.
func withClaims(ctx context.Context, c *Claims) context.Context {
	return context.WithValue(ctx, claimsCtxKey, c)
}

// ─── Mount ───────────────────────────────────────────────────────────────────

// Mount registers all admin routes on the given mux.
// Routes are registered under /admin/* prefix.
// Call this from cmdServe() after constructing AdminConfig.
func Mount(mux *http.ServeMux, cfg AdminConfig) {
	h := &adminHandler{cfg: cfg}

	// Auth routes (no JWT required)
	mux.HandleFunc("GET /admin/auth/{provider}", h.handleAuthStart)
	mux.HandleFunc("GET /admin/auth/callback/{provider}", h.handleAuthCallback)

	// Dev-auth: only register when flag is set
	if cfg.DevAuth {
		mux.HandleFunc("GET /admin/auth/dev", h.handleDevAuth)
	}

	// Skills API routes
	mux.HandleFunc("GET /admin/api/skills", requireRole(cfg.JWTSecret, "viewer", h.handleListSkills))
	mux.HandleFunc("GET /admin/api/skills/{name}", requireRole(cfg.JWTSecret, "viewer", h.handleGetSkill))
	mux.HandleFunc("POST /admin/api/skills", requireRole(cfg.JWTSecret, "tech_lead", h.handleCreateSkill))
	mux.HandleFunc("PUT /admin/api/skills/{name}", requireRole(cfg.JWTSecret, "tech_lead", h.handleUpdateSkill))
	mux.HandleFunc("DELETE /admin/api/skills/{name}", requireRole(cfg.JWTSecret, "admin", h.handleDeleteSkill))

	// Stacks catalog API routes
	mux.HandleFunc("GET /admin/api/stacks", requireRole(cfg.JWTSecret, "viewer", h.handleListStacks))
	mux.HandleFunc("POST /admin/api/stacks", requireRole(cfg.JWTSecret, "tech_lead", h.handleCreateStack))
	mux.HandleFunc("DELETE /admin/api/stacks/{id}", requireRole(cfg.JWTSecret, "admin", h.handleDeleteStack))

	// Categories catalog API routes
	mux.HandleFunc("GET /admin/api/categories", requireRole(cfg.JWTSecret, "viewer", h.handleListCategories))
	mux.HandleFunc("POST /admin/api/categories", requireRole(cfg.JWTSecret, "tech_lead", h.handleCreateCategory))
	mux.HandleFunc("DELETE /admin/api/categories/{id}", requireRole(cfg.JWTSecret, "admin", h.handleDeleteCategory))

	// ── Stats API ──
	mux.HandleFunc("GET /admin/api/stats", requireRole(cfg.JWTSecret, "viewer", h.handleStats))

	// ── Projects API (read-only) ──
	mux.HandleFunc("GET /admin/api/projects", requireRole(cfg.JWTSecret, "viewer", h.handleListProjects))
	mux.HandleFunc("GET /admin/api/projects/{name}", requireRole(cfg.JWTSecret, "viewer", h.handleGetProject))

	// ── Users API ──
	mux.HandleFunc("GET /admin/api/users", requireRole(cfg.JWTSecret, "admin", h.handleListUsers))
	mux.HandleFunc("PUT /admin/api/users/{id}/role", requireRole(cfg.JWTSecret, "admin", h.handleUpdateUserRole))
	mux.HandleFunc("GET /admin/api/me", requireAuth(cfg.JWTSecret, h.handleGetMe))

	// SPA catch-all — MUST be last so API and auth routes take precedence.
	// Serves embedded admin_dist/* files; falls back to index.html for
	// any path that doesn't match a real static file (Angular routing).
	mux.HandleFunc("GET /admin/{path...}", spaHandler())
}

// ─── OAuth config constructors ───────────────────────────────────────────────

// NewGoogleOAuthConfig builds an oauth2.Config for Google authentication.
// redirectURL must be the full callback URL, e.g. "http://host/admin/auth/callback/google".
func NewGoogleOAuthConfig(clientID, clientSecret, redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Scopes: []string{
			"openid",
			"https://www.googleapis.com/auth/userinfo.email",
			"https://www.googleapis.com/auth/userinfo.profile",
		},
		Endpoint: google.Endpoint,
	}
}

// NewGitHubOAuthConfig builds an oauth2.Config for GitHub authentication.
// redirectURL must be the full callback URL, e.g. "http://host/admin/auth/callback/github".
func NewGitHubOAuthConfig(clientID, clientSecret, redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Scopes:       []string{"user:email"},
		Endpoint:     github.Endpoint,
	}
}

// ─── Handler struct ──────────────────────────────────────────────────────────

// adminHandler carries config and implements all admin HTTP handlers.
type adminHandler struct {
	cfg AdminConfig
}

// ─── JSON helpers ─────────────────────────────────────────────────────────────

func jsonResponse(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

func jsonError(w http.ResponseWriter, status int, msg string) {
	jsonResponse(w, status, map[string]string{"error": msg})
}
