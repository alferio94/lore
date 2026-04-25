package admin

// users.go — User management API handlers.

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/alferio94/lore/internal/store"
)

// validRoles is the set of allowed role values for UpdateUserRole.
var validRoles = map[string]bool{
	store.UserRoleAdmin:     true,
	store.UserRoleTechLead:  true,
	store.UserRoleDeveloper: true,
	store.UserRoleNA:        true,
}

var validStatuses = map[string]bool{
	store.UserStatusPending:  true,
	store.UserStatusActive:   true,
	store.UserStatusDisabled: true,
}

// ─── handleListUsers ──────────────────────────────────────────────────────────

// handleListUsers returns all user records.
// GET /admin/api/users — admin only (enforced by requireRole in Mount).
func (h *adminHandler) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.cfg.Store.ListUsers()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "failed to list users")
		return
	}

	jsonResponse(w, http.StatusOK, users)
}

// ─── handleUpdateUserRole ─────────────────────────────────────────────────────

// updateUserRoleRequest is the JSON payload for PUT /admin/api/users/{id}/role.
type updateUserRoleRequest struct {
	Role   string `json:"role"`
	Status string `json:"status"`
}

// handleUpdateUserRole sets the role of the specified user.
// PUT /admin/api/users/{id}/role — admin only (enforced by requireRole in Mount).
func (h *adminHandler) handleUpdateUserRole(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "invalid user id")
		return
	}

	var req updateUserRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	current, err := h.cfg.Store.GetUserByID(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			jsonError(w, http.StatusNotFound, "not_found")
			return
		}
		jsonError(w, http.StatusInternalServerError, "failed to load user")
		return
	}

	if req.Role == "" {
		req.Role = current.Role
	}
	if req.Status == "" {
		req.Status = current.Status
	}

	if !validRoles[req.Role] {
		jsonResponse(w, http.StatusUnprocessableEntity, map[string]any{
			"error":  "validation",
			"fields": map[string]string{"role": "invalid_value"},
		})
		return
	}
	if !validStatuses[req.Status] {
		jsonResponse(w, http.StatusUnprocessableEntity, map[string]any{
			"error":  "validation",
			"fields": map[string]string{"status": "invalid_value"},
		})
		return
	}

	user, err := h.cfg.Store.UpdateUserStatusRole(id, req.Status, req.Role)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			jsonError(w, http.StatusNotFound, "not_found")
			return
		}
		jsonError(w, http.StatusInternalServerError, "failed to update user")
		return
	}

	jsonResponse(w, http.StatusOK, user)
}

func (h *adminHandler) handleGetMe(w http.ResponseWriter, r *http.Request) {
	if actor, ok := actorFromCtx(r.Context()); ok {
		jsonResponse(w, http.StatusOK, actor)
		return
	}

	claims, ok := claimsFromCtx(r.Context())
	if !ok {
		jsonError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	jsonResponse(w, http.StatusOK, map[string]any{
		"sub":   claims.Subject,
		"email": claims.Email,
		"name":  claims.Name,
		"role":  claims.Role,
	})
}
