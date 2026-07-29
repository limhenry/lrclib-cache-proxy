package ytmusic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type nextRequest struct {
	Context innerTubeContext `json:"context"`
	VideoID string           `json:"videoId"`
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

func (c *Client) getBrowseID(ctx context.Context, videoID string) (string, error) {
	reqBody, err := json.Marshal(nextRequest{
		Context: innerTubeContext{
			Client: innerTubeClient{
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
