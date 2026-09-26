package itunes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTargetCountryForGenre(t *testing.T) {
	client := NewClient(Config{
		MandopopCountry: "TW",
		CantopopCountry: "HK",
		KpopCountry:     "KR",
		JpopCountry:     "JP",
	})

	tests := []struct {
		genre    string
		expected string
	}{
		{"Mandopop", "TW"},
		{"mandopop", "TW"},
		{"C-Pop", "TW"},
		{"Chinese", "TW"},
		{"Chinese Pop", "TW"},
		{"華語流行樂", "TW"},
		{"Cantopop", "HK"},
		{"粵語流行", "HK"},
		{"K-Pop", "KR"},
		{"kpop", "KR"},
		{"Korean", "KR"},
		{"한국 팝", "KR"},
		{"J-Pop", "JP"},
		{"Anime", "JP"},
		{"Japanese", "JP"},
		{"Pop", ""},
		{"Rock", ""},
		{"R&B/Soul", ""},
		{"Hip-Hop/Rap", ""},
	}

	for _, tt := range tests {
		got := client.targetCountryForGenre(tt.genre)
		if got != tt.expected {
			t.Errorf("targetCountryForGenre(%q) = %q; want %q", tt.genre, got, tt.expected)
		}
	}
}

func TestFindBestMatch(t *testing.T) {
	tracks := []Track{
		{
			TrackID:         1,
			TrackName:       "Deep in the Veins (Instrumental)",
			TrackTimeMillis: 233000,
		},
		{
			TrackID:         2,
			TrackName:       "Deep in the Veins",
			TrackTimeMillis: 233240,
		},
		{
			TrackID:         3,
			TrackName:       "Deep in the Veins (Remix)",
			TrackTimeMillis: 180000,
		},
	}

	match := findBestMatch(tracks, "Deep in the Veins", 233)
	if match == nil {
		t.Fatalf("expected match, got nil")
	}
	if match.TrackID != 2 {
		t.Errorf("expected track ID 2, got %d", match.TrackID)
	}
}

func TestResolveMetadataMandopop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/search" {
			if q.Get("country") == "MY" && q.Get("term") == "Deep in the Veins Hebe Tien" {
				resp := searchResponse{
					ResultCount: 1,
					Results: []Track{
						{
							TrackID:          6812635415,
							ArtistName:       "Hebe Tien",
							TrackName:        "Deep in the Veins",
							CollectionName:   "The Land of Maybe",
							TrackTimeMillis:  233240,
							PrimaryGenreName: "Mandopop",
							Country:          "MYS",
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
				return
			}
		}

		if r.URL.Path == "/lookup" {
			if q.Get("id") == "6812635415" && q.Get("country") == "TW" {
				resp := searchResponse{
					ResultCount: 1,
					Results: []Track{
						{
							TrackID:          6812635415,
							ArtistName:       "田馥甄",
							TrackName:        "靜脈",
							CollectionName:   "要去什麼地方",
							TrackTimeMillis:  233240,
							PrimaryGenreName: "華語流行樂",
							Country:          "TWN",
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
				return
			}
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient(Config{
		BaseURL:         server.URL,
		DefaultCountry:  "MY",
		MandopopCountry: "TW",
		Timeout:         2 * time.Second,
	})

	resolved, err := client.ResolveMetadata(context.Background(), "Hebe Tien", "Deep in the Veins", "The Land of Maybe", 233)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !resolved.Resolved {
		t.Errorf("expected Resolved to be true")
	}
	if resolved.ArtistName != "田馥甄" {
		t.Errorf("expected artist 田馥甄, got %s", resolved.ArtistName)
	}
	if resolved.TrackName != "靜脈" {
		t.Errorf("expected track 靜脈, got %s", resolved.TrackName)
	}
	if resolved.AlbumName != "要去什麼地方" {
		t.Errorf("expected album 要去什麼地方, got %s", resolved.AlbumName)
	}
	if resolved.TargetCountry != "TW" {
		t.Errorf("expected target country TW, got %s", resolved.TargetCountry)
	}
}

func TestResolveMetadataNonAsianGenre(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := searchResponse{
			ResultCount: 1,
			Results: []Track{
				{
					TrackID:          12345,
					ArtistName:       "Queen",
					TrackName:        "Bohemian Rhapsody",
					CollectionName:   "A Night at the Opera",
					TrackTimeMillis:  354000,
					PrimaryGenreName: "Rock",
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(Config{
		BaseURL:        server.URL,
		DefaultCountry: "MY",
		Timeout:        2 * time.Second,
	})

	resolved, err := client.ResolveMetadata(context.Background(), "Queen", "Bohemian Rhapsody", "A Night at the Opera", 354)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resolved.Resolved {
		t.Errorf("expected Resolved to be false for Western Rock genre")
	}
	if resolved.TrackName != "Bohemian Rhapsody" {
		t.Errorf("expected original track name preserved, got %s", resolved.TrackName)
	}
}
