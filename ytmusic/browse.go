package ytmusic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type browseRequest struct {
	Context  innerTubeContext `json:"context"`
	BrowseID string           `json:"browseId"`
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

// GetSyncedLyrics fetches synced lyrics for a given YouTube video ID.
func (c *Client) GetSyncedLyrics(ctx context.Context, videoID string) (string, error) {
	browseID, err := c.getBrowseID(ctx, videoID)
	if err != nil {
		return "", err
	}

	reqBody, err := json.Marshal(browseRequest{
		Context: innerTubeContext{
			Client: innerTubeClient{
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
