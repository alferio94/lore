package admin

// middleware.go — JWT auth middleware and RBAC role checking.

import (
	"net/http"
)

// roleLevel maps role names to numeric levels for comparison.
// Higher number = more privileged.
var roleLevel = map[string]int{
	"admin":     3,
	"tech_lead": 2,
	"viewer":    1,
}

// requireAuth validates the JWT from the lore_session cookie.
// On success, injects Claims into the request context and calls next.
// On failure, returns 401 JSON error without calling next.
func requireAuth(secret []byte, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil || cookie.Value == "" {
			jsonError(w, http.StatusUnauthorized, "authentication required")
			return
		}

		claims, err := parseJWT(secret, cookie.Value)
		if err != nil {
			jsonError(w, http.StatusUnauthorized, "token_expired")
			return
		}

		r = r.WithContext(withClaims(r.Context(), claims))
		next(w, r)
	}
}

// requireRole checks the role claim against minRole.
// It wraps requireAuth — an unauthenticated request gets 401 before role check.
// A request with insufficient role gets 403.
func requireRole(secret []byte, minRole string, next http.HandlerFunc) http.HandlerFunc {
	authed := requireAuth(secret, func(w http.ResponseWriter, r *http.Request) {
		claims, ok := claimsFromCtx(r.Context())
		if !ok {
			jsonError(w, http.StatusUnauthorized, "authentication required")
			return
		}

		userLevel := roleLevel[claims.Role]
		requiredLevel := roleLevel[minRole]

		if userLevel < requiredLevel {
			jsonError(w, http.StatusForbidden, "forbidden")
			return
		}

		next(w, r)
	})
	return authed
}
