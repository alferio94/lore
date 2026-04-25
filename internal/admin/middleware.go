package admin

// middleware.go — JWT auth middleware and RBAC role checking.

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/alferio94/lore/internal/store"
)

// roleLevel maps role names to numeric levels for comparison.
// Higher number = more privileged.
var roleLevel = map[string]int{
	store.UserRoleAdmin:     3,
	store.UserRoleTechLead:  2,
	store.UserRoleDeveloper: 1,
	store.UserRoleNA:        0,
}

func isCanonicalRole(role string) bool {
	_, ok := roleLevel[role]
	return ok
}

func requireResolvedActor(r *http.Request, claims *Claims) (*store.User, int, string) {
	adminStore, ok := adminStoreFromCtx(r.Context())
	if !ok || adminStore == nil {
		return nil, 0, ""
	}

	userID, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil {
		return nil, http.StatusUnauthorized, "authentication required"
	}

	actor, err := adminStore.GetUserByID(userID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, http.StatusUnauthorized, "authentication required"
		}
		return nil, http.StatusInternalServerError, "failed to load actor"
	}

	if actor.Status != store.UserStatusActive {
		return nil, http.StatusForbidden, "account_inactive"
	}
	if !isCanonicalRole(actor.Role) {
		return nil, http.StatusForbidden, "forbidden"
	}
	return actor, 0, ""
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

		ctx := withClaims(r.Context(), claims)
		if actor, status, msg := requireResolvedActor(r, claims); status != 0 {
			jsonError(w, status, msg)
			return
		} else if actor != nil {
			ctx = withActor(ctx, actor)
		}

		r = r.WithContext(ctx)
		next(w, r)
	}
}

// requireRole checks the role claim against minRole.
// It wraps requireAuth — an unauthenticated request gets 401 before role check.
// A request with insufficient role gets 403.
func requireRole(secret []byte, minRole string, next http.HandlerFunc) http.HandlerFunc {
	authed := requireAuth(secret, func(w http.ResponseWriter, r *http.Request) {
		if actor, ok := actorFromCtx(r.Context()); ok {
			if roleLevel[actor.Role] < roleLevel[minRole] {
				jsonError(w, http.StatusForbidden, "forbidden")
				return
			}
			next(w, r)
			return
		}

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
