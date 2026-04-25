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

type AdminStore interface {
	ListSkills(params store.ListSkillsParams) ([]store.Skill, error)
	GetSkill(name string) (*store.Skill, error)
	CreateSkill(params store.CreateSkillParams) (*store.Skill, error)
	UpdateSkill(name string, params store.UpdateSkillParams) (*store.Skill, error)
	DeleteSkill(name, changedBy string) error
	ListStacks() ([]store.Stack, error)
	CreateStack(name, displayName string) (*store.Stack, error)
	DeleteStack(id int64) error
	ListCategories() ([]store.Category, error)
	CreateCategory(name, displayName string) (*store.Category, error)
	DeleteCategory(id int64) error
	AdminStats() (store.AdminStats, error)
	ListProjectsWithStats() ([]store.ProjectStats, error)
	UpsertUser(email, name, avatarURL, provider string) (*store.User, error)
	CreatePendingUser(email, name, passwordHash string) (*store.User, error)
	GetUserByEmail(email string) (*store.User, error)
	GetUserAuthByEmail(email string) (*store.UserAuth, error)
	GetUserByID(id int64) (*store.User, error)
	ListUsers() ([]store.User, error)
	UpdateUserRole(id int64, role string) (*store.User, error)
	UpdateUserStatusRole(id int64, status, role string) (*store.User, error)
}

// ─── Config ──────────────────────────────────────────────────────────────────

// AdminConfig holds all configuration needed by the admin package.
type AdminConfig struct {
	Store              AdminStore
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
	Role  string `json:"role"`
}

// ─── Context helpers ─────────────────────────────────────────────────────────

type ctxKey string

const claimsCtxKey ctxKey = "claims"
const actorCtxKey ctxKey = "actor"
const adminStoreCtxKey ctxKey = "admin-store"

// claimsFromCtx retrieves Claims injected by requireAuth middleware.
func claimsFromCtx(ctx context.Context) (*Claims, bool) {
	c, ok := ctx.Value(claimsCtxKey).(*Claims)
	return c, ok
}

// withClaims returns a new context with claims attached.
func withClaims(ctx context.Context, c *Claims) context.Context {
	return context.WithValue(ctx, claimsCtxKey, c)
}

func actorFromCtx(ctx context.Context) (*store.User, bool) {
	actor, ok := ctx.Value(actorCtxKey).(*store.User)
	return actor, ok
}

func withActor(ctx context.Context, actor *store.User) context.Context {
	return context.WithValue(ctx, actorCtxKey, actor)
}

func withAdminStore(store AdminStore, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		next(w, r.WithContext(context.WithValue(r.Context(), adminStoreCtxKey, store)))
	}
}

func adminStoreFromCtx(ctx context.Context) (AdminStore, bool) {
	store, ok := ctx.Value(adminStoreCtxKey).(AdminStore)
	return store, ok
}

// ─── Mount ───────────────────────────────────────────────────────────────────

// Mount registers all admin routes on the given mux.
// Routes are registered under /admin/* prefix.
// Call this from cmdServe() after constructing AdminConfig.
func Mount(mux *http.ServeMux, cfg AdminConfig) {
	h := &adminHandler{cfg: cfg}
	protectAuth := func(next http.HandlerFunc) http.HandlerFunc {
		return withAdminStore(cfg.Store, requireAuth(cfg.JWTSecret, next))
	}
	protectRole := func(minRole string, next http.HandlerFunc) http.HandlerFunc {
		return withAdminStore(cfg.Store, requireRole(cfg.JWTSecret, minRole, next))
	}

	// Auth routes (no JWT required)
	mux.HandleFunc("POST /admin/auth/register", h.handleRegister)
	mux.HandleFunc("POST /admin/auth/login", h.handleLogin)
	mux.HandleFunc("POST /admin/auth/logout", h.handleLogout)
	mux.HandleFunc("GET /admin/auth/{provider}", h.handleAuthStart)
	mux.HandleFunc("GET /admin/auth/callback/{provider}", h.handleAuthCallback)

	// Dev-auth: only register when flag is set
	if cfg.DevAuth {
		mux.HandleFunc("GET /admin/auth/dev", h.handleDevAuth)
	}

	// Skills API routes
	mux.HandleFunc("GET /admin/api/skills", protectRole(store.UserRoleDeveloper, h.handleListSkills))
	mux.HandleFunc("GET /admin/api/skills/{name}", protectRole(store.UserRoleDeveloper, h.handleGetSkill))
	mux.HandleFunc("POST /admin/api/skills", protectRole(store.UserRoleTechLead, h.handleCreateSkill))
	mux.HandleFunc("PUT /admin/api/skills/{name}", protectRole(store.UserRoleTechLead, h.handleUpdateSkill))
	mux.HandleFunc("DELETE /admin/api/skills/{name}", protectRole(store.UserRoleAdmin, h.handleDeleteSkill))

	// Stacks catalog API routes
	mux.HandleFunc("GET /admin/api/stacks", protectRole(store.UserRoleDeveloper, h.handleListStacks))
	mux.HandleFunc("POST /admin/api/stacks", protectRole(store.UserRoleTechLead, h.handleCreateStack))
	mux.HandleFunc("DELETE /admin/api/stacks/{id}", protectRole(store.UserRoleAdmin, h.handleDeleteStack))

	// Categories catalog API routes
	mux.HandleFunc("GET /admin/api/categories", protectRole(store.UserRoleDeveloper, h.handleListCategories))
	mux.HandleFunc("POST /admin/api/categories", protectRole(store.UserRoleTechLead, h.handleCreateCategory))
	mux.HandleFunc("DELETE /admin/api/categories/{id}", protectRole(store.UserRoleAdmin, h.handleDeleteCategory))

	// ── Stats API ──
	mux.HandleFunc("GET /admin/api/stats", protectRole(store.UserRoleDeveloper, h.handleStats))

	// ── Projects API (read-only) ──
	mux.HandleFunc("GET /admin/api/projects", protectRole(store.UserRoleDeveloper, h.handleListProjects))
	mux.HandleFunc("GET /admin/api/projects/{name}", protectRole(store.UserRoleDeveloper, h.handleGetProject))

	// ── Users API ──
	mux.HandleFunc("GET /admin/api/users", protectRole(store.UserRoleAdmin, h.handleListUsers))
	mux.HandleFunc("PATCH /admin/api/users/{id}", protectRole(store.UserRoleAdmin, h.handleUpdateUserRole))
	mux.HandleFunc("PUT /admin/api/users/{id}/role", protectRole(store.UserRoleAdmin, h.handleUpdateUserRole))
	mux.HandleFunc("GET /admin/api/me", protectAuth(h.handleGetMe))

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
