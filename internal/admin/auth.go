package admin

// auth.go — JWT issuance/parsing, OAuth CSRF state, OAuth handlers, dev-auth.

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
	"golang.org/x/oauth2/google"

	"github.com/alferio94/lore/internal/store"
)

const (
	sessionCookieName = "lore_session"
	stateCookieName   = "oauth_state"
	cookieMaxAge      = 86400 // 24 hours
	jwtTTL            = 24 * time.Hour
	stateCookieMaxAge = 300 // 5 minutes
)

// ─── JWT ─────────────────────────────────────────────────────────────────────

// issueJWT creates and signs a JWT for the given user.
// Claims: sub (user ID), email, name, role, exp (24h TTL).
func issueJWT(cfg AdminConfig, user store.User) (string, error) {
	now := time.Now()
	c := Claims{
		RegisteredClaims: jwtlib.RegisteredClaims{
			Subject:   strconv.FormatInt(user.ID, 10),
			IssuedAt:  jwtlib.NewNumericDate(now),
			ExpiresAt: jwtlib.NewNumericDate(now.Add(jwtTTL)),
		},
		Email: user.Email,
		Name:  user.Name,
		Role:  user.Role,
	}
	token := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, c)
	return token.SignedString(cfg.JWTSecret)
}

// parseJWT validates and parses a JWT string. Returns parsed Claims or an error.
// Rejects: expired tokens, wrong signature, wrong algorithm.
func parseJWT(secret []byte, tokenStr string) (*Claims, error) {
	token, err := jwtlib.ParseWithClaims(
		tokenStr,
		&Claims{},
		func(t *jwtlib.Token) (any, error) {
			if _, ok := t.Method.(*jwtlib.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			return secret, nil
		},
		jwtlib.WithValidMethods([]string{"HS256"}),
	)
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token claims")
	}
	return claims, nil
}

// ─── OAuth CSRF state ─────────────────────────────────────────────────────────

// generateState creates a CSRF state token: base64(nonce + "." + HMAC(nonce, secret)).
func generateState(secret []byte) (string, error) {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generateState: read random: %w", err)
	}
	nonceHex := hex.EncodeToString(nonce)
	mac := computeHMAC(secret, nonceHex)
	return nonceHex + "." + mac, nil
}

// validateState verifies a CSRF state token produced by generateState.
// Returns nil on success, error on tampered/invalid state.
func validateState(secret []byte, state string) error {
	// Find the separator
	for i := 0; i < len(state); i++ {
		if state[i] == '.' {
			nonceHex := state[:i]
			gotMAC := state[i+1:]
			expectedMAC := computeHMAC(secret, nonceHex)
			if !hmac.Equal([]byte(gotMAC), []byte(expectedMAC)) {
				return errors.New("invalid state: HMAC mismatch")
			}
			return nil
		}
	}
	return errors.New("invalid state: missing separator")
}

// computeHMAC returns a hex-encoded HMAC-SHA256 of data using secret.
func computeHMAC(secret []byte, data string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(data))
	return hex.EncodeToString(mac.Sum(nil))
}

// ─── Cookie helpers ───────────────────────────────────────────────────────────

func setSessionCookie(w http.ResponseWriter, tokenStr string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    tokenStr,
		Path:     "/admin/",
		MaxAge:   cookieMaxAge,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// ─── Dev Auth ─────────────────────────────────────────────────────────────────

// handleDevAuth issues an admin JWT for dev@localhost without OAuth exchange.
// Only registered in Mount when cfg.DevAuth=true.
func (h *adminHandler) handleDevAuth(w http.ResponseWriter, r *http.Request) {
	devUser := store.User{
		ID:       0,
		Email:    "dev@localhost",
		Name:     "Dev User",
		Role:     "admin",
		Provider: "dev",
	}

	tokenStr, err := issueJWT(h.cfg, devUser)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "failed to issue token")
		return
	}

	setSessionCookie(w, tokenStr, h.cfg.CookieSecure)
	http.Redirect(w, r, "/admin/", http.StatusFound)
}

// ─── OAuth providers ──────────────────────────────────────────────────────────

// oauthConfigForProvider returns the oauth2.Config for the named provider,
// or nil if not configured.
func (h *adminHandler) oauthConfigForProvider(provider string) *oauth2.Config {
	switch provider {
	case "google":
		return h.cfg.GoogleOAuth
	case "github":
		return h.cfg.GithubOAuth
	default:
		return nil
	}
}

// buildOAuthConfig constructs an oauth2.Config from env-style fields in AdminConfig
// when GoogleOAuth/GithubOAuth are not pre-built (e.g. in production wiring).
// Returns nil when the provider is unknown or credentials are empty.
func buildOAuthConfig(cfg AdminConfig, provider, baseURL string) *oauth2.Config {
	switch provider {
	case "google":
		if cfg.GoogleClientID == "" || cfg.GoogleClientSecret == "" {
			return nil
		}
		return &oauth2.Config{
			ClientID:     cfg.GoogleClientID,
			ClientSecret: cfg.GoogleClientSecret,
			RedirectURL:  baseURL + "/admin/auth/callback/google",
			Scopes:       []string{"openid", "https://www.googleapis.com/auth/userinfo.email", "https://www.googleapis.com/auth/userinfo.profile"},
			Endpoint:     google.Endpoint,
		}
	case "github":
		if cfg.GitHubClientID == "" || cfg.GitHubClientSecret == "" {
			return nil
		}
		return &oauth2.Config{
			ClientID:     cfg.GitHubClientID,
			ClientSecret: cfg.GitHubClientSecret,
			RedirectURL:  baseURL + "/admin/auth/callback/github",
			Scopes:       []string{"user:email"},
			Endpoint:     github.Endpoint,
		}
	}
	return nil
}

// ─── handleAuthStart ─────────────────────────────────────────────────────────

// handleAuthStart initiates the OAuth2 authorization code flow.
// GET /admin/auth/{provider}
func (h *adminHandler) handleAuthStart(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")

	// Prefer pre-built config; fall back to building from fields.
	oauthCfg := h.oauthConfigForProvider(provider)
	if oauthCfg == nil {
		oauthCfg = buildOAuthConfig(h.cfg, provider, h.cfg.BaseURL)
	}
	if oauthCfg == nil {
		jsonError(w, http.StatusNotFound, "unknown provider: "+provider)
		return
	}

	state, err := generateState(h.cfg.JWTSecret)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "failed to generate state")
		return
	}

	// Store state in a short-lived cookie for CSRF validation in callback.
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    state,
		Path:     "/admin/",
		MaxAge:   stateCookieMaxAge,
		HttpOnly: true,
		Secure:   h.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})

	http.Redirect(w, r, oauthCfg.AuthCodeURL(state), http.StatusFound)
}

// ─── handleAuthCallback ───────────────────────────────────────────────────────

// handleAuthCallback completes the OAuth2 flow, upserts the user, issues JWT.
// GET /admin/auth/callback/{provider}
func (h *adminHandler) handleAuthCallback(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")

	// Validate CSRF state
	stateParam := r.URL.Query().Get("state")
	stateCookie, err := r.Cookie(stateCookieName)
	if err != nil || stateCookie.Value == "" {
		jsonError(w, http.StatusBadRequest, "missing state cookie")
		return
	}

	// The state cookie holds our generated state; verify the query param matches it
	if stateParam != stateCookie.Value {
		jsonError(w, http.StatusBadRequest, "state mismatch")
		return
	}
	if err := validateState(h.cfg.JWTSecret, stateCookie.Value); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid state")
		return
	}

	// Clear the state cookie
	http.SetCookie(w, &http.Cookie{
		Name:   stateCookieName,
		Value:  "",
		Path:   "/admin/",
		MaxAge: -1,
	})

	oauthCfg := h.oauthConfigForProvider(provider)
	if oauthCfg == nil {
		oauthCfg = buildOAuthConfig(h.cfg, provider, h.cfg.BaseURL)
	}
	if oauthCfg == nil {
		jsonError(w, http.StatusNotFound, "unknown provider: "+provider)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		jsonError(w, http.StatusBadRequest, "missing code parameter")
		return
	}

	// Exchange code for token
	oauthToken, err := oauthCfg.Exchange(context.Background(), code)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "invalid_code")
		return
	}

	// Fetch user info from provider
	userInfo, err := fetchUserInfo(provider, oauthCfg, oauthToken.AccessToken)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "failed to fetch user info")
		return
	}

	// Upsert user in store
	if h.cfg.Store == nil {
		jsonError(w, http.StatusInternalServerError, "store not configured")
		return
	}
	user, err := h.cfg.Store.UpsertUser(userInfo.Email, userInfo.Name, userInfo.AvatarURL, provider)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "failed to upsert user")
		return
	}

	// Issue JWT and set cookie
	tokenStr, err := issueJWT(h.cfg, *user)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "failed to issue token")
		return
	}

	setSessionCookie(w, tokenStr, h.cfg.CookieSecure)
	http.Redirect(w, r, "/admin/", http.StatusFound)
}

// ─── User info fetching ───────────────────────────────────────────────────────

type oauthUserInfo struct {
	Email     string
	Name      string
	AvatarURL string
}

func fetchUserInfo(provider string, cfg *oauth2.Config, accessToken string) (*oauthUserInfo, error) {
	switch provider {
	case "google":
		return fetchGoogleUserInfo(cfg, accessToken)
	case "github":
		return fetchGitHubUserInfo(accessToken)
	}
	return nil, fmt.Errorf("unknown provider: %s", provider)
}

func fetchGoogleUserInfo(cfg *oauth2.Config, accessToken string) (*oauthUserInfo, error) {
	client := cfg.Client(context.Background(), &oauth2.Token{AccessToken: accessToken})
	resp, err := client.Get("https://www.googleapis.com/oauth2/v3/userinfo")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var info struct {
		Email   string `json:"email"`
		Name    string `json:"name"`
		Picture string `json:"picture"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	return &oauthUserInfo{Email: info.Email, Name: info.Name, AvatarURL: info.Picture}, nil
}

// fetchGitHubUserInfoFn allows injection in tests.
var fetchGitHubUserInfoFn = defaultFetchGitHubUserInfo

func fetchGitHubUserInfo(accessToken string) (*oauthUserInfo, error) {
	return fetchGitHubUserInfoFn(accessToken)
}

func defaultFetchGitHubUserInfo(accessToken string) (*oauthUserInfo, error) {
	req, err := http.NewRequest("GET", "https://api.github.com/user", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var info struct {
		Email     string `json:"email"`
		Name      string `json:"name"`
		Login     string `json:"login"`
		AvatarURL string `json:"avatar_url"`
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, err
	}
	if info.Email == "" {
		info.Email = info.Login + "@users.noreply.github.com"
	}
	if info.Name == "" {
		info.Name = info.Login
	}
	return &oauthUserInfo{Email: info.Email, Name: info.Name, AvatarURL: info.AvatarURL}, nil
}
