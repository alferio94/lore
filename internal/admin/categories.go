package admin

// categories.go — REST CRUD handlers for admin categories catalog API.
//
// Routes (registered in Mount):
//   GET    /admin/api/categories        → handleListCategories    (viewer+)
//   POST   /admin/api/categories        → handleCreateCategory    (tech_lead+)
//   DELETE /admin/api/categories/{id}   → handleDeleteCategory    (admin only)

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/alferio94/lore/internal/store"
)

// ─── handleListCategories ─────────────────────────────────────────────────────

// handleListCategories returns all category catalog entries as a JSON array.
// Requires: viewer or higher role (enforced by requireRole in Mount).
func (h *adminHandler) handleListCategories(w http.ResponseWriter, r *http.Request) {
	categories, err := h.cfg.Store.ListCategories()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	// Return empty JSON array rather than null when there are no categories.
	if categories == nil {
		categories = []store.Category{}
	}

	jsonResponse(w, http.StatusOK, categories)
}

// ─── createCategoryRequest ────────────────────────────────────────────────────

// createCategoryRequest is the JSON payload for POST /admin/api/categories.
type createCategoryRequest struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
}

// ─── handleCreateCategory ─────────────────────────────────────────────────────

// handleCreateCategory creates a new category catalog entry.
// Validates: name is required.
// Returns 201 on success, 422 on validation failure, 409 on duplicate name.
// Requires: tech_lead or higher role (enforced by requireRole in Mount).
func (h *adminHandler) handleCreateCategory(w http.ResponseWriter, r *http.Request) {
	var req createCategoryRequest
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

	cat, err := h.cfg.Store.CreateCategory(req.Name, req.DisplayName)
	if err != nil {
		if isDuplicateError(err) {
			jsonError(w, http.StatusConflict, "conflict")
			return
		}
		jsonError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	jsonResponse(w, http.StatusCreated, cat)
}

// ─── handleDeleteCategory ─────────────────────────────────────────────────────

// handleDeleteCategory removes a category by ID.
// Returns 204 on success, 404 when the category does not exist.
// Requires: admin role only (enforced by requireRole in Mount).
func (h *adminHandler) handleDeleteCategory(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "invalid_id")
		return
	}

	if err := h.cfg.Store.DeleteCategory(id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			jsonError(w, http.StatusNotFound, "not_found")
			return
		}
		jsonError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
