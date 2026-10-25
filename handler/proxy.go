package handler

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/limhenry/lrclib-cache-proxy/db"
	"github.com/limhenry/lrclib-cache-proxy/itunes"
	"github.com/limhenry/lrclib-cache-proxy/lrclib"
	"github.com/limhenry/lrclib-cache-proxy/ytmusic"
)

// ProxyHandler handles GET /api/get with local caching for LRCLIB and YouTube Music.
type ProxyHandler struct {
	db           *db.DB
	client       *lrclib.Client
	ytClient     *ytmusic.Client
	itunesClient *itunes.Client
	notFoundTTL  time.Duration
}

// NewProxyHandler creates a ProxyHandler.
func NewProxyHandler(database *db.DB, client *lrclib.Client, ytClient *ytmusic.Client, itunesClient *itunes.Client, notFoundTTLDays int) *ProxyHandler {
	return &ProxyHandler{
		db:           database,
		client:       client,
		ytClient:     ytClient,
		itunesClient: itunesClient,
		notFoundTTL:  time.Duration(notFoundTTLDays) * 24 * time.Hour,
	}
}

type syncedLyricsResponse struct {
	SyncedLyrics *string `json:"syncedLyrics"`
	Source       string  `json:"source,omitempty"`
	ArtistName   string  `json:"artistName,omitempty"`
	TrackName    string  `json:"trackName,omitempty"`
	AlbumName    string  `json:"albumName,omitempty"`
	Duration     int     `json:"duration,omitempty"`
}

func metadataLyricsResponse(lyrics *string, source, artistName, trackName, albumName string, duration int) syncedLyricsResponse {
	return syncedLyricsResponse{
		SyncedLyrics: lyrics,
		Source:       source,
		ArtistName:   artistName,
		TrackName:    trackName,
		AlbumName:    albumName,
		Duration:     duration,
	}
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
	source := strings.TrimSpace(q.Get("source"))
	trackIDStr := strings.TrimSpace(q.Get("trackId"))
	if source != "" || trackIDStr != "" {
		if source != "" && source != "applemusic" {
			writeJSON(w, http.StatusBadRequest, errorResponse{Code: 400, Name: "BadRequest", Message: "trackId identifies an Apple Music track"})
			return
		}
		h.handleSourceTrack(w, trackIDStr)
		return
	}

	videoID := strings.TrimSpace(q.Get("videoId"))
	force := q.Get("force") == "true"

	if videoID != "" {
		h.handleYouTube(w, r, videoID, force)
		return
	}

	artistName := q.Get("artist_name")
	trackName := q.Get("track_name")
	albumName := q.Get("album_name")
	durationStr := q.Get("duration")

	if artistName == "" || trackName == "" || albumName == "" || durationStr == "" {
		writeJSON(w, http.StatusBadRequest, errorResponse{
			Code:    400,
			Name:    "BadRequest",
			Message: "artist_name, track_name, album_name and duration are required, or videoId is required",
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

	h.handleLRCLIB(w, r, artistName, trackName, albumName, duration, force)
}

func (h *ProxyHandler) handleSourceTrack(w http.ResponseWriter, trackIDStr string) {
	trackID, err := strconv.ParseInt(trackIDStr, 10, 64)
	if err != nil || trackID <= 0 {
		writeJSON(w, http.StatusBadRequest, errorResponse{
			Code:    400,
			Name:    "BadRequest",
			Message: "a positive Apple Music trackId is required",
		})
		return
	}

	lyrics, err := h.db.LookupSourceLyrics("applemusic", trackID)
	if err != nil {
		slog.Error("source lyrics lookup failed", "err", err, "source", "applemusic", "trackId", trackID)
		writeJSON(w, http.StatusInternalServerError, errorResponse{Code: 500, Name: "InternalError", Message: "internal error"})
		return
	}
	if lyrics == nil {
		slog.Info("Apple Music lyrics cache miss", "trackId", trackID)
		writeJSON(w, http.StatusNotFound, errorResponse{Code: 404, Name: "TrackNotFound", Message: "lyrics not found in cache"})
		return
	}
	slog.Info("Apple Music lyrics cache hit", "trackId", trackID)
	writeJSON(w, http.StatusOK, syncedLyricsResponse{SyncedLyrics: lyrics, Source: "applemusic"})
}

// Upload stores raw synced lyrics using the metadata cache key.
func (h *ProxyHandler) Upload(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	source := strings.TrimSpace(q.Get("source"))
	artistName := strings.TrimSpace(q.Get("artist_name"))
	trackName := strings.TrimSpace(q.Get("track_name"))
	albumName := strings.TrimSpace(q.Get("album_name"))
	duration, err := strconv.Atoi(strings.TrimSpace(q.Get("duration")))
	if (source != "" && source != "applemusic") || artistName == "" || trackName == "" || albumName == "" || err != nil || duration < 0 {
		writeJSON(w, http.StatusBadRequest, errorResponse{
			Code:    400,
			Name:    "BadRequest",
			Message: "artist_name, track_name, album_name, and a non-negative duration are required",
		})
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20+1))
	if err != nil {
		slog.Warn("Apple Music upload body read failed", "track", trackName, "artist", artistName, "err", err)
		writeJSON(w, http.StatusBadRequest, errorResponse{Code: 400, Name: "BadRequest", Message: "failed to read lyrics body"})
		return
	}
	if len(body) > 1<<20 {
		slog.Warn("Apple Music lyrics upload rejected: body too large", "track", trackName, "artist", artistName, "bytesRead", len(body))
		writeJSON(w, http.StatusRequestEntityTooLarge, errorResponse{Code: 413, Name: "PayloadTooLarge", Message: "lyrics body exceeds 1 MB"})
		return
	}
	if strings.TrimSpace(string(body)) == "" {
		slog.Warn("Apple Music lyrics upload rejected: empty body", "track", trackName, "artist", artistName)
		writeJSON(w, http.StatusBadRequest, errorResponse{Code: 400, Name: "BadRequest", Message: "lyrics body must not be empty"})
		return
	}

	lyrics := string(body)
	if err := h.db.InsertHit(artistName, trackName, albumName, duration, &lyrics, false, "applemusic"); err != nil {
		slog.Error("Apple Music metadata lyrics upload failed", "err", err, "track", trackName, "artist", artistName, "album", albumName)
		writeJSON(w, http.StatusInternalServerError, errorResponse{Code: 500, Name: "InternalError", Message: "failed to store lyrics"})
		return
	}
	slog.Info("Apple Music lyrics uploaded", "track", trackName, "artist", artistName, "album", albumName, "duration", duration, "bytes", len(body))
	writeJSON(w, http.StatusCreated, map[string]bool{"ok": true})
}

func (h *ProxyHandler) handleYouTube(w http.ResponseWriter, r *http.Request, videoID string, force bool) {
	var entry *db.CacheEntry
	var err error
	if force {
		slog.Info("YouTube Music cache bypassed", "videoId", videoID)
	}
	if !force {
		entry, err = h.db.LookupYT(videoID)
		if err != nil {
			slog.Error("db yt lookup failed", "err", err, "videoId", videoID)
			writeJSON(w, http.StatusInternalServerError, errorResponse{Code: 500, Name: "InternalError", Message: "internal error"})
			return
		}
	}

	if entry != nil {
		if entry.Status == 200 {
			slog.Info("YouTube Music cache hit", "videoId", videoID)
			writeJSON(w, http.StatusOK, syncedLyricsResponse{SyncedLyrics: entry.SyncedLyrics, Source: "ytmusic"})
			return
		}
		// Status == 404
		if entry.NotFoundAt != nil && time.Since(*entry.NotFoundAt) < h.notFoundTTL {
			slog.Info("YouTube Music negative cache hit", "videoId", videoID)
			writeJSON(w, http.StatusNotFound, errorResponse{
				Code:    404,
				Name:    "TrackNotFound",
				Message: "Failed to find specified track",
			})
			return
		}
		slog.Info("yt 404 TTL expired, re-querying upstream", "videoId", videoID)
	} else if !force {
		slog.Info("YouTube Music cache miss", "videoId", videoID)
	}

	syncedLyrics, err := h.ytClient.GetSyncedLyrics(r.Context(), videoID)
	if err != nil {
		var nfe *ytmusic.NotFoundError
		if errors.As(err, &nfe) {
			if dbErr := h.db.InsertYTNotFound(videoID); dbErr != nil {
				slog.Error("db insert yt not-found failed", "err", dbErr, "videoId", videoID)
			}
			writeJSON(w, http.StatusNotFound, errorResponse{
				Code:    404,
				Name:    "TrackNotFound",
				Message: "Failed to find specified track",
			})
			return
		}
		slog.Error("yt upstream request failed", "err", err, "videoId", videoID)
		writeJSON(w, http.StatusBadGateway, errorResponse{
			Code:    502,
			Name:    "UpstreamError",
			Message: "upstream request failed",
		})
		return
	}

	if dbErr := h.db.InsertYTHit(videoID, &syncedLyrics); dbErr != nil {
		slog.Error("db insert yt hit failed", "err", dbErr, "videoId", videoID)
	}

	slog.Info("cached new yt track", "videoId", videoID)
	writeJSON(w, http.StatusOK, syncedLyricsResponse{SyncedLyrics: &syncedLyrics, Source: "ytmusic"})
}

func (h *ProxyHandler) handleLRCLIB(w http.ResponseWriter, r *http.Request, artistName, trackName, albumName string, duration int, force bool) {
	var entry *db.CacheEntry
	var err error
	if force {
		slog.Info("metadata cache bypassed", "track", trackName, "artist", artistName, "duration", duration)
	}
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
			slog.Info("metadata cache hit", "track", trackName, "artist", artistName, "lyricsSource", entry.LyricsSource)
			responseArtist, responseTrack, responseAlbum := artistName, trackName, albumName
			metadata, metadataErr := h.db.LookupMetadataResolution(artistName, trackName, albumName, duration)
			if metadataErr != nil {
				slog.Error("metadata resolution lookup failed", "err", metadataErr)
				writeJSON(w, http.StatusInternalServerError, errorResponse{Code: 500, Name: "InternalError", Message: "internal error"})
				return
			}
			if metadata != nil {
				responseArtist, responseTrack, responseAlbum = metadata.ArtistName, metadata.TrackName, metadata.AlbumName
			} else if h.itunesClient != nil {
				resolved, resolveErr := h.itunesClient.ResolveMetadata(r.Context(), artistName, trackName, albumName, duration)
				if resolveErr == nil && resolved != nil {
					responseArtist, responseTrack, responseAlbum = resolved.ArtistName, resolved.TrackName, resolved.AlbumName
					if dbErr := h.db.InsertMetadataResolution(artistName, trackName, albumName, duration, responseArtist, responseTrack, responseAlbum); dbErr != nil {
						slog.Warn("metadata resolution cache insert failed", "err", dbErr)
					}
				} else if resolveErr != nil {
					slog.Warn("itunes metadata resolution failed for cache hit", "err", resolveErr, "track", trackName)
				}
			}
			writeJSON(w, http.StatusOK, metadataLyricsResponse(entry.SyncedLyrics, entry.LyricsSource, responseArtist, responseTrack, responseAlbum, duration))
			return
		}
		// Status == 404
		if entry.NotFoundAt != nil && time.Since(*entry.NotFoundAt) < h.notFoundTTL {
			slog.Info("metadata negative cache hit", "track", trackName, "artist", artistName)
			writeJSON(w, http.StatusNotFound, errorResponse{
				Code:    404,
				Name:    "TrackNotFound",
				Message: "Failed to find specified track",
			})
			return
		}
		slog.Info("404 TTL expired, re-querying upstream", "track", trackName, "artist", artistName)
	} else if !force {
		slog.Info("metadata cache miss", "track", trackName, "artist", artistName, "duration", duration)
	}

	// Cache miss or expired 404 — resolve localized metadata if applicable.
	searchArtist := artistName
	searchTrack := trackName
	searchAlbum := albumName
	var isResolved bool
	var appleTrackID int64

	if h.itunesClient != nil {
		resolved, err := h.itunesClient.ResolveMetadata(r.Context(), artistName, trackName, albumName, duration)
		if err != nil {
			slog.Warn("itunes metadata resolution failed, using original metadata", "err", err, "track", trackName)
		} else if resolved != nil {
			appleTrackID = resolved.TrackID
			if appleTrackID > 0 {
				slog.Info("iTunes track matched", "trackId", appleTrackID, "genre", resolved.OriginalGenre, "targetCountry", resolved.TargetCountry)
			} else {
				slog.Info("iTunes track not matched", "track", trackName, "artist", artistName)
			}
			if dbErr := h.db.InsertMetadataResolution(artistName, trackName, albumName, duration, resolved.ArtistName, resolved.TrackName, resolved.AlbumName); dbErr != nil {
				slog.Warn("metadata resolution cache insert failed", "err", dbErr)
			}
			if resolved.Resolved {
				slog.Info("resolved localized metadata via itunes",
					"originalTrack", trackName, "resolvedTrack", resolved.TrackName,
					"originalArtist", artistName, "resolvedArtist", resolved.ArtistName,
					"genre", resolved.OriginalGenre, "country", resolved.TargetCountry)
				searchArtist = resolved.ArtistName
				searchTrack = resolved.TrackName
				searchAlbum = resolved.AlbumName
				isResolved = true
			}
		}
	}

	cacheHit := func(lyrics *string, instrumental bool, source string) {
		if dbErr := h.db.InsertHit(artistName, trackName, albumName, duration, lyrics, instrumental, source); dbErr != nil {
			slog.Error("db insert hit failed", "err", dbErr)
		}
		if isResolved {
			if dbErr := h.db.InsertHit(searchArtist, searchTrack, searchAlbum, duration, lyrics, instrumental, source); dbErr != nil {
				slog.Error("db insert hit (resolved) failed", "err", dbErr)
			}
		}
	}

	cacheNotFound := func() {
		if dbErr := h.db.InsertNotFound(artistName, trackName, albumName, duration); dbErr != nil {
			slog.Error("db insert not-found failed", "err", dbErr)
		}
		if isResolved {
			if dbErr := h.db.InsertNotFound(searchArtist, searchTrack, searchAlbum, duration); dbErr != nil {
				slog.Error("db insert not-found (resolved) failed", "err", dbErr)
			}
		}
	}

	if appleTrackID > 0 && !force {
		lyrics, dbErr := h.db.LookupSourceLyrics("applemusic", appleTrackID)
		if dbErr != nil {
			slog.Error("apple music lyrics lookup failed", "err", dbErr, "trackId", appleTrackID)
			writeJSON(w, http.StatusInternalServerError, errorResponse{Code: 500, Name: "InternalError", Message: "internal error"})
			return
		}
		if lyrics != nil {
			cacheHit(lyrics, false, "applemusic")
			slog.Info("served uploaded apple music lyrics", "track", trackName, "artist", artistName, "trackId", appleTrackID)
			writeJSON(w, http.StatusOK, metadataLyricsResponse(lyrics, "applemusic", searchArtist, searchTrack, searchAlbum, duration))
			return
		}
		slog.Info("uploaded Apple Music lyrics not found", "trackId", appleTrackID)
	} else if appleTrackID > 0 && force {
		slog.Info("uploaded Apple Music lyrics lookup skipped because force=true", "trackId", appleTrackID)
	}

	// Try YouTube Music first with resolved metadata.
	videoID, ytErr := h.ytClient.GetVideoID(r.Context(), searchTrack, searchArtist, searchAlbum, duration)
	if ytErr != nil && isResolved {
		// Fallback to original metadata
		videoID, ytErr = h.ytClient.GetVideoID(r.Context(), trackName, artistName, albumName, duration)
	}

	if ytErr != nil {
		slog.Warn("ytmusic get video id failed, falling back to lrclib", "err", ytErr, "track", searchTrack)
	} else if videoID != "" {
		slog.Info("YouTube Music match found", "videoId", videoID, "track", searchTrack)
		syncedLyrics, lyricsErr := h.ytClient.GetSyncedLyrics(r.Context(), videoID)
		if lyricsErr == nil && syncedLyrics != "" {
			cacheHit(&syncedLyrics, false, "ytmusic")
			if dbErr := h.db.InsertYTHit(videoID, &syncedLyrics); dbErr != nil {
				slog.Error("db insert yt hit failed", "err", dbErr)
			}
			slog.Info("cached track via ytmusic", "track", searchTrack, "artist", searchArtist, "videoId", videoID)
			writeJSON(w, http.StatusOK, metadataLyricsResponse(&syncedLyrics, "ytmusic", searchArtist, searchTrack, searchAlbum, duration))
			return
		}
		slog.Info("ytmusic get lyrics failed or empty, falling back to lrclib", "err", lyricsErr, "track", searchTrack, "videoId", videoID)
	} else {
		slog.Info("YouTube Music match not found, falling back to LRCLIB", "track", searchTrack, "artist", searchArtist)
	}

	// Fallback: call LRCLIB with resolved metadata.
	result, err := h.client.GetLyrics(r.Context(), searchArtist, searchTrack, searchAlbum, duration)
	if err != nil {
		var nfe *lrclib.NotFoundError
		if errors.As(err, &nfe) {
			// Fallback: search by track name, pick the first result that has
			// synced lyrics and a duration within ±2 s of the requested duration.
			searchResult, searchErr := h.client.SearchLyrics(r.Context(), searchTrack, duration)
			if (searchErr != nil || searchResult == nil) && isResolved {
				// Retry search with original track name if resolved search yielded nothing
				searchResult, searchErr = h.client.SearchLyrics(r.Context(), trackName, duration)
			}

			if searchErr != nil {
				slog.Warn("search fallback failed", "err", searchErr, "track", searchTrack)
			} else if searchResult != nil {
				var syncedLyrics *string
				if searchResult.SyncedLyrics != "" {
					syncedLyrics = &searchResult.SyncedLyrics
				}
				cacheHit(syncedLyrics, searchResult.Instrumental, "lrclib")
				slog.Info("cached track via search fallback", "track", searchTrack, "artist", searchArtist)
				writeJSON(w, http.StatusOK, metadataLyricsResponse(syncedLyrics, "lrclib", searchArtist, searchTrack, searchAlbum, duration))
				return
			}

			// If still not found and was resolved, try original metadata GetLyrics as last resort
			if isResolved {
				origResult, origErr := h.client.GetLyrics(r.Context(), artistName, trackName, albumName, duration)
				if origErr == nil && origResult != nil {
					var syncedLyrics *string
					if origResult.SyncedLyrics != "" {
						syncedLyrics = &origResult.SyncedLyrics
					}
					cacheHit(syncedLyrics, origResult.Instrumental, "lrclib")
					slog.Info("cached track via original metadata lrclib", "track", trackName, "artist", artistName)
					writeJSON(w, http.StatusOK, metadataLyricsResponse(syncedLyrics, "lrclib", artistName, trackName, albumName, duration))
					return
				}
			}

			cacheNotFound()
			slog.Info("lyrics not found from configured providers", "track", trackName, "artist", artistName)
			writeJSON(w, http.StatusNotFound, errorResponse{
				Code:    404,
				Name:    "TrackNotFound",
				Message: "Failed to find specified track",
			})
			return
		}
		// Network error, 5xx, etc. — do NOT cache.
		slog.Error("LRCLIB request failed", "err", err, "track", searchTrack, "artist", searchArtist)
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

	// /api/get returned 200 but no synced lyrics — try the search fallback.
	if syncedLyrics == nil && !result.Instrumental {
		searchResult, searchErr := h.client.SearchLyrics(r.Context(), searchTrack, duration)
		if searchErr != nil {
			slog.Warn("search fallback failed", "err", searchErr, "track", searchTrack)
		} else if searchResult != nil && searchResult.SyncedLyrics != "" {
			slog.Info("found synced lyrics via search fallback", "track", searchTrack, "artist", searchArtist)
			syncedLyrics = &searchResult.SyncedLyrics
		}
	}

	cacheHit(syncedLyrics, result.Instrumental, "lrclib")
	slog.Info("cached track via LRCLIB", "track", searchTrack, "artist", searchArtist, "instrumental", result.Instrumental)
	writeJSON(w, http.StatusOK, metadataLyricsResponse(syncedLyrics, "lrclib", searchArtist, searchTrack, searchAlbum, duration))
}
