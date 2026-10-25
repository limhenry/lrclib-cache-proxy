package db

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestNewMigratesExistingLyricsSourceAsUnknown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lyrics.db")
	legacyDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = legacyDB.Exec(`CREATE TABLE lyrics (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		artist_name TEXT NOT NULL,
		track_name TEXT NOT NULL,
		album_name TEXT NOT NULL,
		duration INTEGER NOT NULL,
		synced_lyrics TEXT,
		instrumental INTEGER NOT NULL DEFAULT 0,
		status INTEGER NOT NULL,
		cached_at DATETIME NOT NULL,
		not_found_at DATETIME,
		UNIQUE(artist_name, track_name, album_name, duration)
	)`)
	if err != nil {
		legacyDB.Close()
		t.Fatal(err)
	}
	_, err = legacyDB.Exec(`INSERT INTO lyrics
		(artist_name, track_name, album_name, duration, synced_lyrics, status, cached_at)
		VALUES ('artist', 'track', 'album', 180, '[00:01.00] lyric', 200, '2026-10-25T00:00:00Z')`)
	if err != nil {
		legacyDB.Close()
		t.Fatal(err)
	}
	if err := legacyDB.Close(); err != nil {
		t.Fatal(err)
	}

	database, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	entry, err := database.Lookup("artist", "track", "album", 180)
	if err != nil {
		t.Fatal(err)
	}
	if entry == nil || entry.LyricsSource != "unknown" {
		t.Fatalf("legacy lyrics source = %v, want unknown", entry)
	}
}
