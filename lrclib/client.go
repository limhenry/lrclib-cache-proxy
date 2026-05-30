package lrclib

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const userAgent = "lrclib-cache-proxy v1.0.0 (https://github.com/limhenry/lrclib-cache-proxy)"

// Client makes requests to the lrclib.net API.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// maxResponseBytes caps the upstream response body to prevent memory exhaustion
// if lrclib.net were to serve a pathologically large payload.
const maxResponseBytes = 1 << 20 // 1 MB — well above any real lyrics payload

// NewClient creates an lrclib HTTP client pointing at baseURL.
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
			// Disable redirect following to prevent SSRF: a compromised
			// lrclib.net could redirect requests to internal network addresses
			// (e.g. router admin pages, local services).
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// LyricsResponse is the relevant subset of lrclib's response body.
type LyricsResponse struct {
	Instrumental bool   `json:"instrumental"`
	SyncedLyrics string `json:"syncedLyrics"`
}

// searchResult is one element in the /api/search response array.
type searchResult struct {
	Duration     float64 `json:"duration"`
	Instrumental bool    `json:"instrumental"`
	SyncedLyrics string  `json:"syncedLyrics"`
}

// NotFoundError is returned when lrclib responds with 404.
type NotFoundError struct{}

func (e *NotFoundError) Error() string { return "track not found on lrclib" }

// GetLyrics calls /api/get on lrclib and returns lyrics or a typed error.
// Only NotFoundError is typed; all other failures are plain errors.
func (c *Client) GetLyrics(ctx context.Context, artistName, trackName, albumName string, duration int) (*LyricsResponse, error) {
	params := url.Values{}
	params.Set("artist_name", artistName)
	params.Set("track_name", trackName)
	params.Set("album_name", albumName)
	params.Set("duration", strconv.Itoa(duration))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/get?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upstream request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, &NotFoundError{}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream returned HTTP %d", resp.StatusCode)
	}

	var result LyricsResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

// SearchLyrics calls /api/search?track_name=... and returns the first result
// that has synced lyrics and a duration within ±2 s of the requested duration.
// Returns nil, nil when no matching result is found.
func (c *Client) SearchLyrics(ctx context.Context, trackName string, duration int) (*LyricsResponse, error) {
	params := url.Values{}
	params.Set("track_name", trackName)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/search?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("build search request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upstream search request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream search returned HTTP %d", resp.StatusCode)
	}

	var results []searchResult
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&results); err != nil {
		return nil, fmt.Errorf("decode search response: %w", err)
	}

	var fuzzyMatch *LyricsResponse
	for _, r := range results {
		if r.SyncedLyrics == "" {
			continue
		}
		diff := r.Duration - float64(duration)
		if diff < 0 {
			diff = -diff
		}
		if diff == 0 {
			return &LyricsResponse{
				Instrumental: r.Instrumental,
				SyncedLyrics: r.SyncedLyrics,
			}, nil
		}
		if fuzzyMatch == nil && diff <= 2.0 {
			candidate := &LyricsResponse{
				Instrumental: r.Instrumental,
				SyncedLyrics: r.SyncedLyrics,
			}
			fuzzyMatch = candidate
		}
	}
	return fuzzyMatch, nil
}
