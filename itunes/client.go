package itunes

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	userAgent        = "lrclib-cache-proxy v1.0.0 (https://github.com/limhenry/lrclib-cache-proxy)"
	maxResponseBytes = 1 << 20 // 1 MB cap
)

// Config configures the iTunes client.
type Config struct {
	BaseURL         string
	DefaultCountry  string
	MandopopCountry string
	CantopopCountry string
	KpopCountry     string
	JpopCountry     string
	Timeout         time.Duration
}

// Client interacts with the iTunes Search and Lookup API.
type Client struct {
	baseURL         string
	defaultCountry  string
	mandopopCountry string
	cantopopCountry string
	kpopCountry     string
	jpopCountry     string
	httpClient      *http.Client
}

// NewClient creates a new iTunes API client.
func NewClient(cfg Config) *Client {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://itunes.apple.com"
	}
	defaultCountry := cfg.DefaultCountry
	if defaultCountry == "" {
		defaultCountry = "MY"
	}
	mandopopCountry := cfg.MandopopCountry
	if mandopopCountry == "" {
		mandopopCountry = "TW"
	}
	cantopopCountry := cfg.CantopopCountry
	if cantopopCountry == "" {
		cantopopCountry = "HK"
	}
	kpopCountry := cfg.KpopCountry
	if kpopCountry == "" {
		kpopCountry = "KR"
	}
	jpopCountry := cfg.JpopCountry
	if jpopCountry == "" {
		jpopCountry = "JP"
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	return &Client{
		baseURL:         baseURL,
		defaultCountry:  defaultCountry,
		mandopopCountry: mandopopCountry,
		cantopopCountry: cantopopCountry,
		kpopCountry:     kpopCountry,
		jpopCountry:     jpopCountry,
		httpClient: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// Track represents an iTunes song track entry.
type Track struct {
	TrackID          int64  `json:"trackId"`
	ArtistName       string `json:"artistName"`
	TrackName        string `json:"trackName"`
	CollectionName   string `json:"collectionName"`
	TrackTimeMillis  int    `json:"trackTimeMillis"`
	PrimaryGenreName string `json:"primaryGenreName"`
	Country          string `json:"country"`
}

type searchResponse struct {
	ResultCount int     `json:"resultCount"`
	Results     []Track `json:"results"`
}

// ResolvedMetadata holds the metadata resolved from iTunes.
type ResolvedMetadata struct {
	ArtistName    string
	TrackName     string
	AlbumName     string
	Duration      int
	Resolved      bool
	OriginalGenre string
	TargetCountry string
}

// Search searches iTunes for songs matching the query term and country.
func (c *Client) Search(ctx context.Context, term, country string, limit int) ([]Track, error) {
	if limit <= 0 {
		limit = 5
	}
	params := url.Values{}
	params.Set("term", term)
	params.Set("entity", "song")
	params.Set("limit", strconv.Itoa(limit))
	if country != "" {
		params.Set("country", country)
	}

	reqURL := fmt.Sprintf("%s/search?%s", c.baseURL, params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build search request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("itunes search request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("itunes search HTTP %d", resp.StatusCode)
	}

	var sr searchResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&sr); err != nil {
		return nil, fmt.Errorf("decode search response: %w", err)
	}

	return sr.Results, nil
}

// Lookup queries iTunes for a track by its Track ID in a specific country storefront.
func (c *Client) Lookup(ctx context.Context, trackID int64, country string) (*Track, error) {
	params := url.Values{}
	params.Set("id", strconv.FormatInt(trackID, 10))
	params.Set("entity", "song")
	if country != "" {
		params.Set("country", country)
	}

	reqURL := fmt.Sprintf("%s/lookup?%s", c.baseURL, params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build lookup request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("itunes lookup request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("itunes lookup HTTP %d", resp.StatusCode)
	}

	var sr searchResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&sr); err != nil {
		return nil, fmt.Errorf("decode lookup response: %w", err)
	}

	if len(sr.Results) == 0 {
		return nil, nil
	}

	return &sr.Results[0], nil
}

// targetCountryForGenre returns the target storefront code if the genre represents
// a regional Asian music genre that is frequently translated to English.
func (c *Client) targetCountryForGenre(genre string) string {
	g := strings.ToLower(strings.TrimSpace(genre))
	switch {
	case strings.Contains(g, "mandopop") || strings.Contains(g, "c-pop") || g == "chinese" || strings.Contains(g, "chinese pop") || strings.Contains(g, "國語流行") || strings.Contains(g, "華語流行") || strings.Contains(g, "国语流行"):
		return c.mandopopCountry
	case strings.Contains(g, "cantopop") || strings.Contains(g, "粵語流行") || strings.Contains(g, "粤语流行"):
		return c.cantopopCountry
	case strings.Contains(g, "k-pop") || strings.Contains(g, "kpop") || strings.Contains(g, "korean") || strings.Contains(g, "한국 팝"):
		return c.kpopCountry
	case strings.Contains(g, "j-pop") || strings.Contains(g, "jpop") || strings.Contains(g, "anime") || strings.Contains(g, "japanese"):
		return c.jpopCountry
	default:
		return ""
	}
}

// ResolveMetadata queries iTunes in the default storefront to check the track's genre,
// and if it's an Asian genre, queries the target regional storefront to retrieve the
// original native language metadata (artist, title, album).
func (c *Client) ResolveMetadata(ctx context.Context, artistName, trackName, albumName string, duration int) (*ResolvedMetadata, error) {
	fallback := &ResolvedMetadata{
		ArtistName: artistName,
		TrackName:  trackName,
		AlbumName:  albumName,
		Duration:   duration,
		Resolved:   false,
	}

	// 1. Search iTunes using track title and artist in the default country storefront (e.g. MY).
	searchTerm := fmt.Sprintf("%s %s", trackName, artistName)
	results, err := c.Search(ctx, searchTerm, c.defaultCountry, 5)
	if err != nil || len(results) == 0 {
		// Fallback try without country if initial search returned empty
		if len(results) == 0 && c.defaultCountry != "US" {
			results, err = c.Search(ctx, searchTerm, "US", 5)
		}
		if err != nil || len(results) == 0 {
			return fallback, nil
		}
	}

	// 2. Find best matching track by duration and title.
	matchedTrack := findBestMatch(results, trackName, duration)
	if matchedTrack == nil {
		return fallback, nil
	}

	// 3. Check genre to determine if a target storefront is needed.
	targetCountry := c.targetCountryForGenre(matchedTrack.PrimaryGenreName)
	if targetCountry == "" {
		// Not a regional genre requiring localization.
		return fallback, nil
	}

	// 4. Retrieve original metadata from target country storefront.
	// We first try Lookup by universal trackId, which is exact and reliable.
	var localizedTrack *Track
	if matchedTrack.TrackID > 0 {
		localizedTrack, _ = c.Lookup(ctx, matchedTrack.TrackID, targetCountry)
	}

	// Fallback to Search with country parameter if Lookup returned nothing.
	if localizedTrack == nil {
		localizedResults, err := c.Search(ctx, searchTerm, targetCountry, 5)
		if err == nil && len(localizedResults) > 0 {
			localizedTrack = findBestMatch(localizedResults, trackName, duration)
		}
	}

	if localizedTrack == nil {
		return fallback, nil
	}

	// Ensure resolved fields are not empty
	resolvedArtist := localizedTrack.ArtistName
	if resolvedArtist == "" {
		resolvedArtist = artistName
	}
	resolvedTrack := localizedTrack.TrackName
	if resolvedTrack == "" {
		resolvedTrack = trackName
	}
	resolvedAlbum := localizedTrack.CollectionName
	if resolvedAlbum == "" {
		resolvedAlbum = albumName
	}

	isDifferent := resolvedArtist != artistName || resolvedTrack != trackName || resolvedAlbum != albumName

	return &ResolvedMetadata{
		ArtistName:    resolvedArtist,
		TrackName:     resolvedTrack,
		AlbumName:     resolvedAlbum,
		Duration:      duration,
		Resolved:      isDifferent,
		OriginalGenre: matchedTrack.PrimaryGenreName,
		TargetCountry: targetCountry,
	}, nil
}

// findBestMatch finds the closest matching track by duration and title.
func findBestMatch(tracks []Track, expectedTitle string, expectedDuration int) *Track {
	if len(tracks) == 0 {
		return nil
	}

	var best *Track
	bestScore := -1

	normalizedExpected := strings.ToLower(strings.TrimSpace(expectedTitle))

	for i := range tracks {
		track := &tracks[i]
		durationSec := track.TrackTimeMillis / 1000
		diff := int(math.Abs(float64(durationSec - expectedDuration)))

		// Duration must be within ±10 seconds
		if diff > 10 {
			continue
		}

		score := 100 - diff*5
		normalizedTitle := strings.ToLower(strings.TrimSpace(track.TrackName))

		if normalizedTitle == normalizedExpected {
			score += 100 // exact match bonus
		} else if strings.Contains(normalizedTitle, normalizedExpected) || strings.Contains(normalizedExpected, normalizedTitle) {
			score += 50
			// Penalize instrumental/remix/karaoke if expected title doesn't request it
			if (strings.Contains(normalizedTitle, "instrumental") || strings.Contains(normalizedTitle, "karaoke") || strings.Contains(normalizedTitle, "remix")) &&
				!strings.Contains(normalizedExpected, "instrumental") && !strings.Contains(normalizedExpected, "karaoke") && !strings.Contains(normalizedExpected, "remix") {
				score -= 40
			}
		} else {
			continue
		}

		if score > bestScore {
			bestScore = score
			best = track
		}
	}

	// Fallback: if no title match within ±10s, pick closest by duration within ±3s
	if best == nil {
		minDiff := math.MaxInt32
		for i := range tracks {
			track := &tracks[i]
			durationSec := track.TrackTimeMillis / 1000
			diff := int(math.Abs(float64(durationSec - expectedDuration)))
			if diff <= 3 && diff < minDiff {
				minDiff = diff
				best = track
			}
		}
	}

	return best
}
