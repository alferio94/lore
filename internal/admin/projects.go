package admin

// projects.go — Read-only project metrics API handlers.

import (
	"net/http"
)

// handleListProjects returns all projects with aggregated stats.
// GET /admin/api/projects — min role: viewer
func (h *adminHandler) handleListProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := h.cfg.Store.ListProjectsWithStats()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "failed to list projects")
		return
	}

	jsonResponse(w, http.StatusOK, projects)
}

// handleGetProject returns a single project by name.
// GET /admin/api/projects/{name} — min role: viewer
func (h *adminHandler) handleGetProject(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	projects, err := h.cfg.Store.ListProjectsWithStats()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "failed to list projects")
		return
	}

	for _, p := range projects {
		if p.Name == name {
			jsonResponse(w, http.StatusOK, p)
			return
		}
	}

	jsonError(w, http.StatusNotFound, "not_found")
}
