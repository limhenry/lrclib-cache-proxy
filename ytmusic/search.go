package ytmusic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

var durationRegexp = regexp.MustCompile(`^\d+:\d+(:\d+)?$`)

type searchRequest struct {
	Context innerTubeContext `json:"context"`
	Query   string           `json:"query"`
	Params  string           `json:"params"`
}

type searchRunItem struct {
	Text               string `json:"text"`
	NavigationEndpoint *struct {
		WatchEndpoint *struct {
			VideoID string `json:"videoId"`
		} `json:"watchEndpoint"`
		BrowseEndpoint *struct {
			BrowseID                              string `json:"browseId"`
			BrowseEndpointContextSupportedConfigs *struct {
				BrowseEndpointContextMusicConfig *struct {
					PageType string `json:"pageType"`
				} `json:"browseEndpointContextMusicConfig"`
			} `json:"browseEndpointContextSupportedConfigs"`
		} `json:"browseEndpoint"`
	} `json:"navigationEndpoint"`
}

type musicResponsiveListItemRenderer struct {
	PlaylistItemData *struct {
		VideoID string `json:"videoId"`
	} `json:"playlistItemData"`
	FlexColumns []struct {
		MusicResponsiveListItemFlexColumnRenderer struct {
			Text struct {
				Runs []searchRunItem `json:"runs"`
			} `json:"text"`
		} `json:"musicResponsiveListItemFlexColumnRenderer"`
	} `json:"flexColumns"`
	Overlay *struct {
		MusicItemThumbnailOverlayRenderer *struct {
			Content *struct {
				MusicPlayButtonRenderer *struct {
					PlayNavigationEndpoint *struct {
						WatchEndpoint *struct {
							VideoID string `json:"videoId"`
						} `json:"watchEndpoint"`
					} `json:"playNavigationEndpoint"`
				} `json:"musicPlayButtonRenderer"`
			} `json:"content"`
		} `json:"musicItemThumbnailOverlayRenderer"`
	} `json:"overlay"`
}

type searchContentItem struct {
	MusicShelfRenderer *struct {
		Contents []struct {
			MusicResponsiveListItemRenderer *musicResponsiveListItemRenderer `json:"musicResponsiveListItemRenderer"`
		} `json:"contents"`
	} `json:"musicShelfRenderer"`
}

type searchResponse struct {
	Contents struct {
		TabbedSearchResultsRenderer struct {
			Tabs []struct {
				TabRenderer struct {
					Content struct {
						SectionListRenderer struct {
							Contents []searchContentItem `json:"contents"`
						} `json:"sectionListRenderer"`
					} `json:"content"`
				} `json:"tabRenderer"`
			} `json:"tabs"`
		} `json:"tabbedSearchResultsRenderer"`
	} `json:"contents"`
}

type parsedSearchCandidate struct {
	videoID         string
	title           string
	artists         []string
	album           string
	durationStr     string
	durationSeconds int
}

func parseDurationSeconds(str string) int {
	if str == "" {
		return 0
	}
	parts := strings.Split(str, ":")
	nums := make([]int, len(parts))
	for i, p := range parts {
		val, err := strconv.Atoi(p)
		if err != nil {
			return 0
		}
		nums[i] = val
	}
	if len(nums) == 2 {
		return nums[0]*60 + nums[1]
	}
	if len(nums) == 3 {
		return nums[0]*3600 + nums[1]*60 + nums[2]
	}
	return 0
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// GetVideoID searches YouTube Music for track metadata and returns the matching video ID.
func (c *Client) GetVideoID(ctx context.Context, title, artist, album string, duration int) (string, error) {
	query := title
	if artist != "" {
		query = title + " " + artist
	}

	reqBody, err := json.Marshal(searchRequest{
		Context: innerTubeContext{
			Client: innerTubeClient{
				ClientName:    "WEB_REMIX",
				ClientVersion: "1.20240101.01.00",
			},
		},
		Query:  query,
		Params: "EgWKAQIIAWoMEA4QChADEAQQCRAF", // song filter
	})
	if err != nil {
		return "", fmt.Errorf("marshal search req: %w", err)
	}

	url := fmt.Sprintf("https://music.youtube.com/youtubei/v1/search?alt=json&key=%s", apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("build search req: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("search API returned HTTP %d", resp.StatusCode)
	}

	var searchRes searchResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&searchRes); err != nil {
		return "", fmt.Errorf("decode search resp: %w", err)
	}

	tabs := searchRes.Contents.TabbedSearchResultsRenderer.Tabs
	if len(tabs) == 0 {
		return "", nil
	}
	sectionContents := tabs[0].TabRenderer.Content.SectionListRenderer.Contents

	var rawItems []struct {
		MusicResponsiveListItemRenderer *musicResponsiveListItemRenderer `json:"musicResponsiveListItemRenderer"`
	}
	for _, sec := range sectionContents {
		if sec.MusicShelfRenderer != nil {
			rawItems = sec.MusicShelfRenderer.Contents
			break
		}
	}

	if len(rawItems) > 5 {
		rawItems = rawItems[:5]
	}

	parsedItems := make([]parsedSearchCandidate, 0, len(rawItems))
	for _, raw := range rawItems {
		item := raw.MusicResponsiveListItemRenderer
		if item == nil {
			continue
		}

		vID := ""
		if item.PlaylistItemData != nil && item.PlaylistItemData.VideoID != "" {
			vID = item.PlaylistItemData.VideoID
		} else if len(item.FlexColumns) > 0 {
			runs := item.FlexColumns[0].MusicResponsiveListItemFlexColumnRenderer.Text.Runs
			if len(runs) > 0 && runs[0].NavigationEndpoint != nil && runs[0].NavigationEndpoint.WatchEndpoint != nil {
				vID = runs[0].NavigationEndpoint.WatchEndpoint.VideoID
			}
		}
		if vID == "" && item.Overlay != nil &&
			item.Overlay.MusicItemThumbnailOverlayRenderer != nil &&
			item.Overlay.MusicItemThumbnailOverlayRenderer.Content != nil &&
			item.Overlay.MusicItemThumbnailOverlayRenderer.Content.MusicPlayButtonRenderer != nil &&
			item.Overlay.MusicItemThumbnailOverlayRenderer.Content.MusicPlayButtonRenderer.PlayNavigationEndpoint != nil &&
			item.Overlay.MusicItemThumbnailOverlayRenderer.Content.MusicPlayButtonRenderer.PlayNavigationEndpoint.WatchEndpoint != nil {
			vID = item.Overlay.MusicItemThumbnailOverlayRenderer.Content.MusicPlayButtonRenderer.PlayNavigationEndpoint.WatchEndpoint.VideoID
		}

		itemTitle := ""
		if len(item.FlexColumns) > 0 {
			runs := item.FlexColumns[0].MusicResponsiveListItemFlexColumnRenderer.Text.Runs
			if len(runs) > 0 {
				itemTitle = runs[0].Text
			}
		}

		var runs []searchRunItem
		if len(item.FlexColumns) > 1 {
			runs = item.FlexColumns[1].MusicResponsiveListItemFlexColumnRenderer.Text.Runs
		}

		var artists []string
		albumName := ""
		durationStr := ""

		for _, run := range runs {
			pageType := ""
			browseID := ""
			if run.NavigationEndpoint != nil && run.NavigationEndpoint.BrowseEndpoint != nil {
				browseID = run.NavigationEndpoint.BrowseEndpoint.BrowseID
				if run.NavigationEndpoint.BrowseEndpoint.BrowseEndpointContextSupportedConfigs != nil &&
					run.NavigationEndpoint.BrowseEndpoint.BrowseEndpointContextSupportedConfigs.BrowseEndpointContextMusicConfig != nil {
					pageType = run.NavigationEndpoint.BrowseEndpoint.BrowseEndpointContextSupportedConfigs.BrowseEndpointContextMusicConfig.PageType
				}
			}

			if pageType == "MUSIC_PAGE_TYPE_ARTIST" || strings.HasPrefix(browseID, "UC") || strings.HasPrefix(browseID, "FVC") {
				artists = append(artists, run.Text)
			} else if pageType == "MUSIC_PAGE_TYPE_ALBUM" || strings.HasPrefix(browseID, "MPRE") {
				albumName = run.Text
			} else if durationRegexp.MatchString(run.Text) {
				durationStr = run.Text
			}
		}

		parsedItems = append(parsedItems, parsedSearchCandidate{
			videoID:         vID,
			title:           itemTitle,
			artists:         artists,
			album:           albumName,
			durationStr:     durationStr,
			durationSeconds: parseDurationSeconds(durationStr),
		})
	}

	if len(parsedItems) == 0 {
		return "", nil
	}

	targetDurationSeconds := duration
	if duration > 10000 {
		targetDurationSeconds = (duration + 500) / 1000
	}

	clean := func(s string) string {
		return strings.ToLower(strings.TrimSpace(s))
	}

	var bestMatch *parsedSearchCandidate
	maxScore := -1

	for i := range parsedItems {
		item := &parsedItems[i]
		score := 0

		cTitle := clean(item.title)
		tTitle := clean(title)
		if tTitle != "" {
			if cTitle == tTitle {
				score += 4
			} else if strings.Contains(cTitle, tTitle) || strings.Contains(tTitle, cTitle) {
				score += 2
			}
		}

		if artist != "" {
			tArtist := clean(artist)
			hasArtist := false
			for _, a := range item.artists {
				ca := clean(a)
				if ca == tArtist || strings.Contains(ca, tArtist) || strings.Contains(tArtist, ca) {
					hasArtist = true
					break
				}
			}
			if hasArtist {
				score += 3
			}
		}

		if album != "" && item.album != "" {
			tAlbum := clean(album)
			cAlbum := clean(item.album)
			if cAlbum == tAlbum {
				score += 2
			} else if strings.Contains(cAlbum, tAlbum) || strings.Contains(tAlbum, cAlbum) {
				score += 1
			}
		}

		if targetDurationSeconds > 0 && item.durationSeconds > 0 {
			diff := abs(item.durationSeconds - targetDurationSeconds)
			if diff <= 3 {
				score += 3
			} else if diff <= 10 {
				score += 2
			} else if diff <= 20 {
				score += 1
			}
		}

		if score > maxScore {
			maxScore = score
			bestMatch = item
		}
	}

	if bestMatch != nil && bestMatch.videoID != "" {
		return bestMatch.videoID, nil
	}

	for _, item := range parsedItems {
		if item.videoID != "" {
			return item.videoID, nil
		}
	}

	return "", nil
}
