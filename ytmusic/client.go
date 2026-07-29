package ytmusic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	apiKey           = "AIzaSyC9XL3ZjWddXya6X74dJoCTL-WEYFDNX30"
	maxResponseBytes = 1 << 20 // 1 MB
)

// Client makes requests to YouTube Music API.
type Client struct {
	httpClient *http.Client
}

// NewClient creates a YouTube Music HTTP client.
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// NotFoundError is returned when YouTube Music has no lyrics for the video.
type NotFoundError struct {
	VideoID string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("lyrics not found for video: %s", e.VideoID)
}

type stringOrInt int64

func (s *stringOrInt) UnmarshalJSON(b []byte) error {
	str := strings.Trim(string(b), `"`)
	if str == "" || str == "null" {
		*s = 0
		return nil
	}
	val, err := strconv.ParseInt(str, 10, 64)
	if err != nil {
		return err
	}
	*s = stringOrInt(val)
	return nil
}

type nextRequest struct {
	Context nextContext `json:"context"`
	VideoID string      `json:"videoId"`
}

type nextContext struct {
	Client nextClient `json:"client"`
}

type nextClient struct {
	ClientName    string `json:"clientName"`
	ClientVersion string `json:"clientVersion"`
}

type nextResponse struct {
	Contents struct {
		SingleColumnMusicWatchNextResultsRenderer struct {
			TabbedRenderer struct {
				WatchNextTabbedResultsRenderer struct {
					Tabs []struct {
						TabRenderer struct {
							Endpoint struct {
								BrowseEndpoint struct {
									BrowseID                              string `json:"browseId"`
									BrowseEndpointContextSupportedConfigs struct {
										BrowseEndpointContextMusicConfig struct {
											PageType string `json:"pageType"`
										} `json:"browseEndpointContextMusicConfig"`
									} `json:"browseEndpointContextSupportedConfigs"`
								} `json:"browseEndpoint"`
							} `json:"endpoint"`
						} `json:"tabRenderer"`
					} `json:"tabs"`
				} `json:"watchNextTabbedResultsRenderer"`
			} `json:"tabbedRenderer"`
		} `json:"singleColumnMusicWatchNextResultsRenderer"`
	} `json:"contents"`
}

type browseRequest struct {
	Context  browseContext `json:"context"`
	BrowseID string        `json:"browseId"`
}

type browseContext struct {
	Client browseClient `json:"client"`
}

type browseClient struct {
	ClientName    string `json:"clientName"`
	ClientVersion string `json:"clientVersion"`
}

type browseResponse struct {
	Contents struct {
		ElementRenderer struct {
			NewElement struct {
				Type struct {
					ComponentType struct {
						Model struct {
							TimedLyricsModel struct {
								LyricsData struct {
									TimedLyricsData []struct {
										CueRange struct {
											StartTimeMilliseconds stringOrInt `json:"startTimeMilliseconds"`
										} `json:"cueRange"`
										LyricLine string `json:"lyricLine"`
									} `json:"timedLyricsData"`
								} `json:"lyricsData"`
							} `json:"timedLyricsModel"`
						} `json:"model"`
					} `json:"componentType"`
				} `json:"type"`
			} `json:"newElement"`
		} `json:"elementRenderer"`
	} `json:"contents"`
}

func getTimestamp(ms int64) string {
	m := ms / 60000
	s := (ms % 60000) / 1000
	xx := (ms % 1000) / 10
	return fmt.Sprintf("%02d:%02d.%02d", m, s, xx)
}

func (c *Client) getBrowseID(ctx context.Context, videoID string) (string, error) {
	reqBody, err := json.Marshal(nextRequest{
		Context: nextContext{
			Client: nextClient{
				ClientName:    "WEB_REMIX",
				ClientVersion: "1.20240101.01.00",
			},
		},
		VideoID: videoID,
	})
	if err != nil {
		return "", fmt.Errorf("marshal next req: %w", err)
	}

	url := fmt.Sprintf("https://music.youtube.com/youtubei/v1/next?alt=json&key=%s", apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("build next req: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("next request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("next API returned HTTP %d", resp.StatusCode)
	}

	var res nextResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&res); err != nil {
		return "", fmt.Errorf("decode next resp: %w", err)
	}

	tabs := res.Contents.SingleColumnMusicWatchNextResultsRenderer.TabbedRenderer.WatchNextTabbedResultsRenderer.Tabs
	for _, tab := range tabs {
		endpoint := tab.TabRenderer.Endpoint.BrowseEndpoint
		pageType := endpoint.BrowseEndpointContextSupportedConfigs.BrowseEndpointContextMusicConfig.PageType
		if pageType == "MUSIC_PAGE_TYPE_TRACK_LYRICS" && endpoint.BrowseID != "" {
			return endpoint.BrowseID, nil
		}
	}

	return "", &NotFoundError{VideoID: videoID}
}

// GetSyncedLyrics fetches synced lyrics for a given YouTube video ID.
func (c *Client) GetSyncedLyrics(ctx context.Context, videoID string) (string, error) {
	browseID, err := c.getBrowseID(ctx, videoID)
	if err != nil {
		return "", err
	}

	reqBody, err := json.Marshal(browseRequest{
		Context: browseContext{
			Client: browseClient{
				ClientName:    "ANDROID_MUSIC",
				ClientVersion: "7.21.50",
			},
		},
		BrowseID: browseID,
	})
	if err != nil {
		return "", fmt.Errorf("marshal browse req: %w", err)
	}

	url := fmt.Sprintf("https://music.youtube.com/youtubei/v1/browse?alt=json&key=%s", apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("build browse req: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("browse request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("browse API returned HTTP %d", resp.StatusCode)
	}

	var browseRes browseResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&browseRes); err != nil {
		return "", fmt.Errorf("decode browse resp: %w", err)
	}

	timedData := browseRes.Contents.ElementRenderer.NewElement.Type.ComponentType.Model.TimedLyricsModel.LyricsData.TimedLyricsData
	if len(timedData) == 0 {
		return "", &NotFoundError{VideoID: videoID}
	}

	lines := make([]string, 0, len(timedData))
	for _, l := range timedData {
		timestamp := getTimestamp(int64(l.CueRange.StartTimeMilliseconds))
		lines = append(lines, fmt.Sprintf("[%s] %s", timestamp, l.LyricLine))
	}

	return strings.Join(lines, "\n"), nil
}
