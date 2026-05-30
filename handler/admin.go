package handler

import (
	"net/http"
	"strconv"

	"github.com/limhenry/lrclib-cache-proxy/db"
)

// AdminHandler serves the /admin/* endpoints.
type AdminHandler struct {
	db          *db.DB
	notFoundTTL int
}

// NewAdminHandler creates an AdminHandler.
func NewAdminHandler(database *db.DB, notFoundTTLDays int) *AdminHandler {
	return &AdminHandler{db: database, notFoundTTL: notFoundTTLDays}
}

func parsePagination(r *http.Request) (page, limit int) {
	page, limit = 1, 50
	if p := r.URL.Query().Get("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v > 0 {
			page = v
		}
	}
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 500 {
			limit = v
		}
	}
	return
}

// Summary handles GET /admin/summary.
// Returns total cached count, 404 count, and DB file size.
func (h *AdminHandler) Summary(w http.ResponseWriter, r *http.Request) {
	summary, err := h.db.GetSummary()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Code: 500, Name: "InternalError", Message: "failed to get summary"})
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

// Songs handles GET /admin/songs?page=1&limit=50.
// Returns a paginated list of successfully cached tracks.
func (h *AdminHandler) Songs(w http.ResponseWriter, r *http.Request) {
	page, limit := parsePagination(r)
	songs, total, err := h.db.ListSongs(page, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Code: 500, Name: "InternalError", Message: "failed to list songs"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"page":  page,
		"limit": limit,
		"total": total,
		"data":  songs,
	})
}

// NotFound handles GET /admin/not-found?page=1&limit=50.
// Returns a paginated list of tracks that returned 404, with retry-after timestamps.
func (h *AdminHandler) NotFound(w http.ResponseWriter, r *http.Request) {
	page, limit := parsePagination(r)
	entries, total, err := h.db.ListNotFound(page, limit, h.notFoundTTL)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Code: 500, Name: "InternalError", Message: "failed to list not-found entries"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"page":  page,
		"limit": limit,
		"total": total,
		"data":  entries,
	})
}
