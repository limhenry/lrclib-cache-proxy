package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/limhenry/lrclib-cache-proxy/db"
	"github.com/limhenry/lrclib-cache-proxy/lrclib"
)

// ProxyHandler handles GET /api/get with local caching.
type ProxyHandler struct {
	db          *db.DB
	client      *lrclib.Client
	notFoundTTL time.Duration
}

// NewProxyHandler creates a ProxyHandler.
func NewProxyHandler(database *db.DB, client *lrclib.Client, notFoundTTLDays int) *ProxyHandler {
	return &ProxyHandler{
		db:          database,
		client:      client,
		notFoundTTL: time.Duration(notFoundTTLDays) * 24 * time.Hour,
	}
}

type syncedLyricsResponse struct {
	SyncedLyrics *string `json:"syncedLyrics"`
}

type errorResponse struct {
	Code    int    `json:"code"`
	Name    string `json:"name"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (h *ProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	artistName := q.Get("artist_name")
	trackName := q.Get("track_name")
	albumName := q.Get("album_name")
	durationStr := q.Get("duration")

	if artistName == "" || trackName == "" || albumName == "" || durationStr == "" {
		writeJSON(w, http.StatusBadRequest, errorResponse{
			Code:    400,
			Name:    "BadRequest",
			Message: "artist_name, track_name, album_name and duration are all required",
		})
		return
	}

	duration, err := strconv.Atoi(durationStr)
	if err != nil || duration < 0 {
		writeJSON(w, http.StatusBadRequest, errorResponse{
			Code:    400,
			Name:    "BadRequest",
			Message: "duration must be a non-negative integer",
		})
		return
	}

	force := q.Get("force") == "true"

	var entry *db.CacheEntry
	if !force {
		entry, err = h.db.Lookup(artistName, trackName, albumName, duration)
		if err != nil {
			slog.Error("db lookup failed", "err", err)
			writeJSON(w, http.StatusInternalServerError, errorResponse{Code: 500, Name: "InternalError", Message: "internal error"})
			return
		}
	}

	if entry != nil {
		if entry.Status == 200 {
			slog.Debug("cache hit (200)", "track", trackName, "artist", artistName)
			writeJSON(w, http.StatusOK, syncedLyricsResponse{SyncedLyrics: entry.SyncedLyrics})
			return
		}
		// Status == 404
		if entry.NotFoundAt != nil && time.Since(*entry.NotFoundAt) < h.notFoundTTL {
			slog.Debug("cache hit (404, still fresh)", "track", trackName, "artist", artistName)
			writeJSON(w, http.StatusNotFound, errorResponse{
				Code:    404,
				Name:    "TrackNotFound",
				Message: "Failed to find specified track",
			})
			return
		}
		slog.Info("404 TTL expired, re-querying upstream", "track", trackName, "artist", artistName)
	}

	// Cache miss or expired 404 — call lrclib.
	result, err := h.client.GetLyrics(r.Context(), artistName, trackName, albumName, duration)
	if err != nil {
		var nfe *lrclib.NotFoundError
		if errors.As(err, &nfe) {
			// Fallback: search by track name, pick the first result that has
			// synced lyrics and a duration within ±2 s of the requested duration.
			searchResult, searchErr := h.client.SearchLyrics(r.Context(), trackName, duration)
			if searchErr != nil {
				slog.Warn("search fallback failed", "err", searchErr, "track", trackName)
			} else if searchResult != nil {
				var syncedLyrics *string
				if searchResult.SyncedLyrics != "" {
					syncedLyrics = &searchResult.SyncedLyrics
				}
				if dbErr := h.db.InsertHit(artistName, trackName, albumName, duration, syncedLyrics, searchResult.Instrumental); dbErr != nil {
					slog.Error("db insert hit (search fallback) failed", "err", dbErr)
				}
				slog.Info("cached track via search fallback", "track", trackName, "artist", artistName)
				writeJSON(w, http.StatusOK, syncedLyricsResponse{SyncedLyrics: syncedLyrics})
				return
			}
			if dbErr := h.db.InsertNotFound(artistName, trackName, albumName, duration); dbErr != nil {
				slog.Error("db insert not-found failed", "err", dbErr)
			}
			writeJSON(w, http.StatusNotFound, errorResponse{
				Code:    404,
				Name:    "TrackNotFound",
				Message: "Failed to find specified track",
			})
			return
		}
		// Network error, 5xx, etc. — do NOT cache.
		slog.Error("upstream request failed", "err", err)
		writeJSON(w, http.StatusBadGateway, errorResponse{
			Code:    502,
			Name:    "UpstreamError",
			Message: "upstream request failed",
		})
		return
	}

	var syncedLyrics *string
	if result.SyncedLyrics != "" {
		syncedLyrics = &result.SyncedLyrics
	}

	if dbErr := h.db.InsertHit(artistName, trackName, albumName, duration, syncedLyrics, result.Instrumental); dbErr != nil {
		slog.Error("db insert hit failed", "err", dbErr)
	}

	slog.Info("cached new track", "track", trackName, "artist", artistName)
	writeJSON(w, http.StatusOK, syncedLyricsResponse{SyncedLyrics: syncedLyrics})
}
