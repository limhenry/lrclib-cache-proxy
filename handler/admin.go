package handler

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/limhenry/lrclib-cache-proxy/db"
)

//go:embed web/index.html
var indexHTML []byte

// AdminHandler serves the /admin/* endpoints and web interface.
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

// UI serves the embedded web interface.
func (h *AdminHandler) UI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(indexHTML)
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

// Songs handles GET /admin/songs?page=1&limit=50&q=keyword.
// Returns a paginated list of successfully cached tracks.
func (h *AdminHandler) Songs(w http.ResponseWriter, r *http.Request) {
	page, limit := parsePagination(r)
	q := r.URL.Query().Get("q")
	songs, total, err := h.db.ListSongs(page, limit, q)
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

// NotFound handles GET /admin/not-found?page=1&limit=50&q=keyword.
// Returns a paginated list of tracks that returned 404, with retry-after timestamps.
func (h *AdminHandler) NotFound(w http.ResponseWriter, r *http.Request) {
	page, limit := parsePagination(r)
	q := r.URL.Query().Get("q")
	entries, total, err := h.db.ListNotFound(page, limit, h.notFoundTTL, q)
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

// GetEntry handles GET /admin/entry?source=lrclib|yt&id=123.
func (h *AdminHandler) GetEntry(w http.ResponseWriter, r *http.Request) {
	source := r.URL.Query().Get("source")
	idStr := r.URL.Query().Get("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || (source != "lrclib" && source != "yt") {
		writeJSON(w, http.StatusBadRequest, errorResponse{Code: 400, Name: "BadRequest", Message: "valid source and id are required"})
		return
	}

	entry, err := h.db.GetEntry(source, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Code: 500, Name: "InternalError", Message: "failed to get entry"})
		return
	}
	if entry == nil {
		writeJSON(w, http.StatusNotFound, errorResponse{Code: 404, Name: "NotFound", Message: "entry not found"})
		return
	}

	writeJSON(w, http.StatusOK, entry)
}

// DeleteEntry handles DELETE /admin/entry?source=lrclib|yt&id=123.
func (h *AdminHandler) DeleteEntry(w http.ResponseWriter, r *http.Request) {
	source := r.URL.Query().Get("source")
	idStr := r.URL.Query().Get("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || (source != "lrclib" && source != "yt") {
		writeJSON(w, http.StatusBadRequest, errorResponse{Code: 400, Name: "BadRequest", Message: "valid source and id are required"})
		return
	}

	if err := h.db.DeleteEntry(source, id); err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Code: 500, Name: "InternalError", Message: "failed to delete entry"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type updateEntryRequest struct {
	SyncedLyrics *string `json:"syncedLyrics"`
	Instrumental bool    `json:"instrumental"`
}

// UpdateEntry handles PUT /admin/entry?source=lrclib|yt&id=123.
func (h *AdminHandler) UpdateEntry(w http.ResponseWriter, r *http.Request) {
	source := r.URL.Query().Get("source")
	idStr := r.URL.Query().Get("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || (source != "lrclib" && source != "yt") {
		writeJSON(w, http.StatusBadRequest, errorResponse{Code: 400, Name: "BadRequest", Message: "valid source and id are required"})
		return
	}

	var req updateEntryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Code: 400, Name: "BadRequest", Message: "invalid JSON body"})
		return
	}

	if err := h.db.UpdateLyrics(source, id, req.SyncedLyrics, req.Instrumental); err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Code: 500, Name: "InternalError", Message: "failed to update entry"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ClearNotFound handles POST /admin/not-found/clear.
func (h *AdminHandler) ClearNotFound(w http.ResponseWriter, r *http.Request) {
	if err := h.db.ClearAllNotFound(); err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Code: 500, Name: "InternalError", Message: "failed to clear 404 records"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
