package admin

// stacks.go — REST CRUD handlers for admin stacks catalog API.
//
// Routes (registered in Mount):
//   GET    /admin/api/stacks        → handleListStacks    (viewer+)
//   POST   /admin/api/stacks        → handleCreateStack   (tech_lead+)
//   DELETE /admin/api/stacks/{id}   → handleDeleteStack   (admin only)

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/alferio94/lore/internal/store"
)

// ─── handleListStacks ─────────────────────────────────────────────────────────

// handleListStacks returns all stack catalog entries as a JSON array.
// Requires: viewer or higher role (enforced by requireRole in Mount).
func (h *adminHandler) handleListStacks(w http.ResponseWriter, r *http.Request) {
	stacks, err := h.cfg.Store.ListStacks()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	// Return empty JSON array rather than null when there are no stacks.
	if stacks == nil {
		stacks = []store.Stack{}
	}

	jsonResponse(w, http.StatusOK, stacks)
}

// ─── createStackRequest ───────────────────────────────────────────────────────

// createStackRequest is the JSON payload for POST /admin/api/stacks.
type createStackRequest struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
}

// ─── handleCreateStack ────────────────────────────────────────────────────────

// handleCreateStack creates a new stack catalog entry.
// Validates: name is required.
// Returns 201 on success, 422 on validation failure, 409 on duplicate name.
// Requires: tech_lead or higher role (enforced by requireRole in Mount).
func (h *adminHandler) handleCreateStack(w http.ResponseWriter, r *http.Request) {
	var req createStackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusUnprocessableEntity, "invalid_json")
		return
	}

	// Validate required fields.
	if strings.TrimSpace(req.Name) == "" {
		jsonResponse(w, http.StatusUnprocessableEntity, map[string]any{
			"error":  "validation",
			"fields": map[string]string{"name": "required"},
		})
		return
	}

	// Default display_name to name if not provided.
	if strings.TrimSpace(req.DisplayName) == "" {
		req.DisplayName = req.Name
	}

	st, err := h.cfg.Store.CreateStack(req.Name, req.DisplayName)
	if err != nil {
		if isDuplicateError(err) {
			jsonError(w, http.StatusConflict, "conflict")
			return
		}
		jsonError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	jsonResponse(w, http.StatusCreated, st)
}

// ─── handleDeleteStack ────────────────────────────────────────────────────────

// handleDeleteStack removes a stack by ID.
// Returns 204 on success, 404 when the stack does not exist.
// Requires: admin role only (enforced by requireRole in Mount).
func (h *adminHandler) handleDeleteStack(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "invalid_id")
		return
	}

	if err := h.cfg.Store.DeleteStack(id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			jsonError(w, http.StatusNotFound, "not_found")
			return
		}
		jsonError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
