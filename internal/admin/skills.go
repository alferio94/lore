package admin

// skills.go — REST CRUD handlers for admin skills API.
//
// Routes (registered in Mount):
//   GET    /admin/api/skills           → handleListSkills    (viewer+)
//   GET    /admin/api/skills/{name}    → handleGetSkill      (viewer+)
//   POST   /admin/api/skills           → handleCreateSkill   (tech_lead+)
//   PUT    /admin/api/skills/{name}    → handleUpdateSkill   (tech_lead+)
//   DELETE /admin/api/skills/{name}    → handleDeleteSkill   (admin only)

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/alferio94/lore/internal/store"
)

// ─── handleListSkills ─────────────────────────────────────────────────────────

// handleListSkills returns all active skills as a JSON array.
// Requires: viewer or higher role (enforced by requireRole in Mount).
func (h *adminHandler) handleListSkills(w http.ResponseWriter, r *http.Request) {
	skills, err := h.cfg.Store.ListSkills(store.ListSkillsParams{})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	// Return empty JSON array rather than null when there are no skills.
	if skills == nil {
		skills = []store.Skill{}
	}

	jsonResponse(w, http.StatusOK, skills)
}

// ─── handleGetSkill ───────────────────────────────────────────────────────────

// handleGetSkill returns a single skill by name.
// Returns 404 when no active or inactive skill matches — GetSkill returns
// sql.ErrNoRows for truly absent rows.
// Requires: viewer or higher role (enforced by requireRole in Mount).
func (h *adminHandler) handleGetSkill(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	skill, err := h.cfg.Store.GetSkill(name)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, http.StatusNotFound, "not_found")
			return
		}
		jsonError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	jsonResponse(w, http.StatusOK, skill)
}

// ─── createSkillRequest ───────────────────────────────────────────────────────

// createSkillRequest is the JSON payload for POST /admin/api/skills.
type createSkillRequest struct {
	Name        string  `json:"name"`
	DisplayName string  `json:"display_name"`
	StackIDs    []int64 `json:"stack_ids"`
	CategoryIDs []int64 `json:"category_ids"`
	Triggers    string  `json:"triggers"`
	Content     string  `json:"content"`
}

// ─── handleCreateSkill ────────────────────────────────────────────────────────

// handleCreateSkill creates a new skill.
// Validates: name and content are required.
// Returns 201 on success, 422 on validation failure, 409 on duplicate name.
// Requires: tech_lead or higher role (enforced by requireRole in Mount).
func (h *adminHandler) handleCreateSkill(w http.ResponseWriter, r *http.Request) {
	var req createSkillRequest
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
	if strings.TrimSpace(req.Content) == "" {
		jsonResponse(w, http.StatusUnprocessableEntity, map[string]any{
			"error":  "validation",
			"fields": map[string]string{"content": "required"},
		})
		return
	}

	// Determine changedBy from JWT claims (injected by requireRole middleware).
	changedBy := "admin"
	if claims, ok := claimsFromCtx(r.Context()); ok {
		changedBy = claims.Email
	}

	skill, err := h.cfg.Store.CreateSkill(store.CreateSkillParams{
		Name:        req.Name,
		DisplayName: req.DisplayName,
		StackIDs:    req.StackIDs,
		CategoryIDs: req.CategoryIDs,
		Triggers:    req.Triggers,
		Content:     req.Content,
		ChangedBy:   changedBy,
	})
	if err != nil {
		// SQLite UNIQUE constraint violation → duplicate name
		if isDuplicateError(err) {
			jsonError(w, http.StatusConflict, "conflict")
			return
		}
		jsonError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	jsonResponse(w, http.StatusCreated, skill)
}

// ─── updateSkillRequest ───────────────────────────────────────────────────────

// updateSkillRequest is the JSON payload for PUT /admin/api/skills/{name}.
// All fields are optional pointers — only provided fields are updated.
type updateSkillRequest struct {
	DisplayName *string  `json:"display_name"`
	StackIDs    *[]int64 `json:"stack_ids"`
	CategoryIDs *[]int64 `json:"category_ids"`
	Triggers    *string  `json:"triggers"`
	Content     *string  `json:"content"`
}

// hasAnyField returns true if at least one field in the request is non-nil.
func (u *updateSkillRequest) hasAnyField() bool {
	return u.DisplayName != nil || u.StackIDs != nil ||
		u.CategoryIDs != nil || u.Triggers != nil || u.Content != nil
}

// ─── handleUpdateSkill ────────────────────────────────────────────────────────

// handleUpdateSkill updates an existing skill by name.
// Returns 200 on success, 404 on unknown name, 422 when no fields provided.
// Requires: tech_lead or higher role (enforced by requireRole in Mount).
func (h *adminHandler) handleUpdateSkill(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	var req updateSkillRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusUnprocessableEntity, "invalid_json")
		return
	}

	// At least one field must be present.
	if !req.hasAnyField() {
		jsonResponse(w, http.StatusUnprocessableEntity, map[string]any{
			"error":  "validation",
			"fields": map[string]string{"body": "at least one field required"},
		})
		return
	}

	// Determine changedBy from JWT claims.
	changedBy := "admin"
	if claims, ok := claimsFromCtx(r.Context()); ok {
		changedBy = claims.Email
	}

	skill, err := h.cfg.Store.UpdateSkill(name, store.UpdateSkillParams{
		DisplayName: req.DisplayName,
		StackIDs:    req.StackIDs,
		CategoryIDs: req.CategoryIDs,
		Triggers:    req.Triggers,
		Content:     req.Content,
		ChangedBy:   changedBy,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, http.StatusNotFound, "not_found")
			return
		}
		jsonError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	jsonResponse(w, http.StatusOK, skill)
}

// ─── handleDeleteSkill ────────────────────────────────────────────────────────

// handleDeleteSkill soft-deletes a skill by name.
// Returns 204 on success, 404 when skill does not exist.
// Requires: admin role only (enforced by requireRole in Mount).
func (h *adminHandler) handleDeleteSkill(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	// Determine changedBy from JWT claims.
	changedBy := "admin"
	if claims, ok := claimsFromCtx(r.Context()); ok {
		changedBy = claims.Email
	}

	if err := h.cfg.Store.DeleteSkill(name, changedBy); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			jsonError(w, http.StatusNotFound, "not_found")
			return
		}
		jsonError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// isDuplicateError returns true when err signals a SQLite UNIQUE constraint
// violation. The driver surfaces this as an error message containing "UNIQUE".
func isDuplicateError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "UNIQUE")
}
