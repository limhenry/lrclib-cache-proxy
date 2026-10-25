package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/limhenry/lrclib-cache-proxy/db"
	"github.com/limhenry/lrclib-cache-proxy/itunes"
)

func TestAppleMusicUploadCanBeRetrievedFromCache(t *testing.T) {
	database, err := db.New(filepath.Join(t.TempDir(), "lyrics.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	handler := NewProxyHandler(database, nil, nil, nil, 7)
	lyrics := "[00:01.00] Sample lyric\n"
	youtubeLyrics := "[00:01.00] YouTube version\n"
	if err := database.InsertHit("Test Artist", "Sample Track", "Test Album", 180, &youtubeLyrics, false, "ytmusic"); err != nil {
		t.Fatal(err)
	}
	legacyUpload := httptest.NewRequest(http.MethodPost, "/api/upload?trackId=6825795645", strings.NewReader(lyrics))
	legacyUploadResponse := httptest.NewRecorder()
	handler.Upload(legacyUploadResponse, legacyUpload)
	if legacyUploadResponse.Code != http.StatusBadRequest {
		t.Fatalf("trackId-only upload status = %d, want %d", legacyUploadResponse.Code, http.StatusBadRequest)
	}

	upload := httptest.NewRequest(http.MethodPost, "/api/upload?source=applemusic&artist_name=Test+Artist&track_name=Sample+Track&album_name=Test+Album&duration=180", strings.NewReader(lyrics))
	uploadResponse := httptest.NewRecorder()
	handler.Upload(uploadResponse, upload)
	if uploadResponse.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, want %d: %s", uploadResponse.Code, http.StatusCreated, uploadResponse.Body.String())
	}
	if err := database.InsertHit("Test Artist", "Sample Track", "Test Album", 180, &youtubeLyrics, false, "ytmusic"); err != nil {
		t.Fatal(err)
	}

	metadataGet := httptest.NewRequest(http.MethodGet, "/api/get?artist_name=Test+Artist&track_name=Sample+Track&album_name=Test+Album&duration=180", nil)
	metadataResponse := httptest.NewRecorder()
	handler.ServeHTTP(metadataResponse, metadataGet)
	if metadataResponse.Code != http.StatusOK {
		t.Fatalf("metadata get status = %d, want %d: %s", metadataResponse.Code, http.StatusOK, metadataResponse.Body.String())
	}
	var body syncedLyricsResponse
	if err := json.Unmarshal(metadataResponse.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.SyncedLyrics == nil || *body.SyncedLyrics != lyrics {
		t.Fatalf("metadata get lyrics = %v, want %q", body.SyncedLyrics, lyrics)
	}
	if body.Source != "applemusic" {
		t.Fatalf("metadata source = %q, want applemusic", body.Source)
	}
	if body.ArtistName != "Test Artist" || body.TrackName != "Sample Track" || body.AlbumName != "Test Album" || body.Duration != 180 {
		t.Fatalf("metadata response = %+v", body)
	}

	songs, total, err := database.ListSongs(1, 10, "Test Artist")
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(songs) != 1 || songs[0].Source != "lrclib" || songs[0].LyricsSource != "applemusic" || songs[0].TrackName != "sample track" {
		t.Fatalf("metadata-keyed Apple Music admin entry = total %d, songs %+v", total, songs)
	}
}

func TestMetadataResponseUsesLocalizedMetadata(t *testing.T) {
	database, err := db.New(filepath.Join(t.TempDir(), "lyrics.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	const trackID = int64(6825795645)
	lyrics := "[00:01.00] 本地歌詞\n"
	if err := database.InsertSourceLyrics("applemusic", trackID, lyrics); err != nil {
		t.Fatal(err)
	}

	itunesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/lookup" {
			_, _ = w.Write([]byte(`{"resultCount":1,"results":[{"trackId":6825795645,"artistName":"田馥甄","trackName":"靜脈","collectionName":"日常","trackTimeMillis":180000,"primaryGenreName":"Mandopop","country":"TW"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"resultCount":1,"results":[{"trackId":6825795645,"artistName":"Hebe Tien","trackName":"Deep in the Veins","collectionName":"Day by Day","trackTimeMillis":180000,"primaryGenreName":"Mandopop","country":"US"}]}`))
	}))
	defer itunesServer.Close()

	handler := NewProxyHandler(database, nil, nil, itunes.NewClient(itunes.Config{BaseURL: itunesServer.URL}), 7)
	request := httptest.NewRequest(http.MethodGet, "/api/get?artist_name=Hebe+Tien&track_name=Deep+in+the+Veins&album_name=Day+by+Day&duration=180", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}

	var body syncedLyricsResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.SyncedLyrics == nil || *body.SyncedLyrics != lyrics {
		t.Fatalf("lyrics = %v, want %q", body.SyncedLyrics, lyrics)
	}
	if body.ArtistName != "田馥甄" || body.TrackName != "靜脈" || body.AlbumName != "日常" || body.Duration != 180 {
		t.Fatalf("localized metadata = %+v", body)
	}
	if body.Source != "applemusic" {
		t.Fatalf("localized response source = %q, want applemusic", body.Source)
	}

	cachedResponse := httptest.NewRecorder()
	handler.ServeHTTP(cachedResponse, request)
	if cachedResponse.Code != http.StatusOK {
		t.Fatalf("cached status = %d, want %d: %s", cachedResponse.Code, http.StatusOK, cachedResponse.Body.String())
	}
	if err := json.Unmarshal(cachedResponse.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.ArtistName != "田馥甄" || body.TrackName != "靜脈" || body.AlbumName != "日常" {
		t.Fatalf("cached localized metadata = %+v", body)
	}
	if body.Source != "applemusic" {
		t.Fatalf("cached localized source = %q, want applemusic", body.Source)
	}
}

func TestGetReturnsLyricsSourceForCachedProviders(t *testing.T) {
	database, err := db.New(filepath.Join(t.TempDir(), "lyrics.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	lyrics := "[00:01.00] Cached lyric\n"
	if err := database.InsertHit("Artist", "LRCLIB Track", "Album", 180, &lyrics, false, "lrclib"); err != nil {
		t.Fatal(err)
	}
	if err := database.InsertYTHit("video-id", &lyrics); err != nil {
		t.Fatal(err)
	}
	handler := NewProxyHandler(database, nil, nil, nil, 7)

	metadataResponse := httptest.NewRecorder()
	metadataRequest := httptest.NewRequest(http.MethodGet, "/api/get?artist_name=Artist&track_name=LRCLIB+Track&album_name=Album&duration=180", nil)
	handler.ServeHTTP(metadataResponse, metadataRequest)
	var metadataBody syncedLyricsResponse
	if err := json.Unmarshal(metadataResponse.Body.Bytes(), &metadataBody); err != nil {
		t.Fatal(err)
	}
	if metadataResponse.Code != http.StatusOK || metadataBody.Source != "lrclib" {
		t.Fatalf("metadata cache response status/source = %d/%q, want 200/lrclib", metadataResponse.Code, metadataBody.Source)
	}

	youtubeResponse := httptest.NewRecorder()
	handler.ServeHTTP(youtubeResponse, httptest.NewRequest(http.MethodGet, "/api/get?videoId=video-id", nil))
	var youtubeBody syncedLyricsResponse
	if err := json.Unmarshal(youtubeResponse.Body.Bytes(), &youtubeBody); err != nil {
		t.Fatal(err)
	}
	if youtubeResponse.Code != http.StatusOK || youtubeBody.Source != "ytmusic" {
		t.Fatalf("videoId cache response status/source = %d/%q, want 200/ytmusic", youtubeResponse.Code, youtubeBody.Source)
	}
}

func TestAppleMusicAdminEntryLifecycle(t *testing.T) {
	database, err := db.New(filepath.Join(t.TempDir(), "lyrics.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	const trackID = int64(6825795645)
	if err := database.InsertSourceLyrics("applemusic", trackID, "[00:01.00] Original lyric\n"); err != nil {
		t.Fatal(err)
	}
	admin := NewAdminHandler(database, 7)

	summaryResponse := httptest.NewRecorder()
	admin.Summary(summaryResponse, httptest.NewRequest(http.MethodGet, "/admin/summary", nil))
	var summary db.Summary
	if err := json.Unmarshal(summaryResponse.Body.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.CachedCount != 1 {
		t.Fatalf("cached count = %d, want 1", summary.CachedCount)
	}

	songsResponse := httptest.NewRecorder()
	admin.Songs(songsResponse, httptest.NewRequest(http.MethodGet, "/admin/songs?q=6825795645", nil))
	var songs struct {
		Total int            `json:"total"`
		Data  []db.SongEntry `json:"data"`
	}
	if err := json.Unmarshal(songsResponse.Body.Bytes(), &songs); err != nil {
		t.Fatal(err)
	}
	if songs.Total != 1 || len(songs.Data) != 1 || songs.Data[0].Source != "applemusic" || songs.Data[0].TrackID != trackID || songs.Data[0].ID != trackID {
		t.Fatalf("unexpected Apple Music song listing: %+v", songs)
	}

	allSongsResponse := httptest.NewRecorder()
	admin.Songs(allSongsResponse, httptest.NewRequest(http.MethodGet, "/admin/songs", nil))
	if err := json.Unmarshal(allSongsResponse.Body.Bytes(), &songs); err != nil {
		t.Fatal(err)
	}
	if songs.Total != 1 || len(songs.Data) != 1 || songs.Data[0].Source != "applemusic" {
		t.Fatalf("Apple Music entry missing from unfiltered listing: %+v", songs)
	}

	entryURL := "/admin/entry?source=applemusic&id=6825795645"
	entryResponse := httptest.NewRecorder()
	admin.GetEntry(entryResponse, httptest.NewRequest(http.MethodGet, entryURL, nil))
	var entry db.CacheEntry
	if err := json.Unmarshal(entryResponse.Body.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if entry.TrackID != trackID || entry.SyncedLyrics == nil || *entry.SyncedLyrics != "[00:01.00] Original lyric\n" {
		t.Fatalf("unexpected Apple Music entry: %+v", entry)
	}

	update := httptest.NewRequest(http.MethodPut, entryURL, strings.NewReader(`{"syncedLyrics":"[00:02.00] Edited lyric","instrumental":false}`))
	updateResponse := httptest.NewRecorder()
	admin.UpdateEntry(updateResponse, update)
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("update status = %d, want %d: %s", updateResponse.Code, http.StatusOK, updateResponse.Body.String())
	}

	deleteResponse := httptest.NewRecorder()
	admin.DeleteEntry(deleteResponse, httptest.NewRequest(http.MethodDelete, entryURL, nil))
	if deleteResponse.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want %d: %s", deleteResponse.Code, http.StatusOK, deleteResponse.Body.String())
	}
}
