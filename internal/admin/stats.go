package admin

// stats.go — Admin dashboard statistics API handler.

import (
	"net/http"
)

// handleStats returns real-time admin dashboard statistics.
// GET /admin/api/stats — min role: viewer
func (h *adminHandler) handleStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.cfg.Store.AdminStats()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "failed to retrieve stats")
		return
	}

	jsonResponse(w, http.StatusOK, stats)
}
