package ytmusic

import (
	"fmt"
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

type innerTubeContext struct {
	Client innerTubeClient `json:"client"`
}

type innerTubeClient struct {
	ClientName    string `json:"clientName"`
	ClientVersion string `json:"clientVersion"`
}

