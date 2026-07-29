package db

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// DB wraps a SQLite connection with typed query helpers.
type DB struct {
	sql  *sql.DB
	path string
}

// CacheEntry represents a row in the lyrics or yt_lyrics table.
type CacheEntry struct {
	ArtistName   string
	TrackName    string
	AlbumName    string
	Duration     int
	VideoID      string
	SyncedLyrics *string
	Instrumental bool
	Status       int
	CachedAt     time.Time
	NotFoundAt   *time.Time
}

// New opens (or creates) a SQLite database at path and runs schema migrations.
func New(path string) (*DB, error) {
	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// SQLite is single-writer; one connection avoids SQLITE_BUSY contention.
	sqlDB.SetMaxOpenConns(1)

	d := &DB{sql: sqlDB, path: path}
	if err := d.migrate(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return d, nil
}

func (d *DB) migrate() error {
	stmts := []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA synchronous=NORMAL`,
		`PRAGMA cache_size=-2000`, // 2 MB page cache
		`PRAGMA temp_store=MEMORY`,
		`CREATE TABLE IF NOT EXISTS lyrics (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			artist_name   TEXT NOT NULL,
			track_name    TEXT NOT NULL,
			album_name    TEXT NOT NULL,
			duration      INTEGER NOT NULL,
			synced_lyrics TEXT,
			instrumental  INTEGER NOT NULL DEFAULT 0,
			status        INTEGER NOT NULL,
			cached_at     DATETIME NOT NULL,
			not_found_at  DATETIME,
			UNIQUE(artist_name, track_name, album_name, duration)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_status ON lyrics(status)`,
		`CREATE TABLE IF NOT EXISTS yt_lyrics (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			video_id      TEXT NOT NULL UNIQUE,
			synced_lyrics TEXT,
			status        INTEGER NOT NULL,
			cached_at     DATETIME NOT NULL,
			not_found_at  DATETIME
		)`,
		`CREATE INDEX IF NOT EXISTS idx_yt_status ON yt_lyrics(status)`,
	}
	for _, stmt := range stmts {
		if _, err := d.sql.Exec(stmt); err != nil {
			return fmt.Errorf("exec %q: %w", stmt[:min(len(stmt), 40)], err)
		}
	}
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// Lookup finds a cache entry by track signature.
// Returns (nil, nil) when no matching entry exists.
func (d *DB) Lookup(artistName, trackName, albumName string, duration int) (*CacheEntry, error) {
	row := d.sql.QueryRow(`
		SELECT artist_name, track_name, album_name, duration,
		       synced_lyrics, instrumental, status, cached_at, not_found_at
		FROM lyrics
		WHERE artist_name=? AND track_name=? AND album_name=? AND duration=?`,
		normalize(artistName), normalize(trackName), normalize(albumName), duration,
	)

	var e CacheEntry
	var instrumental int
	var cachedAt string
	var notFoundAt *string

	err := row.Scan(
		&e.ArtistName, &e.TrackName, &e.AlbumName, &e.Duration,
		&e.SyncedLyrics, &instrumental, &e.Status, &cachedAt, &notFoundAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	e.Instrumental = instrumental == 1
	e.CachedAt, _ = time.Parse(time.RFC3339, cachedAt)
	if notFoundAt != nil {
		t, _ := time.Parse(time.RFC3339, *notFoundAt)
		e.NotFoundAt = &t
	}
	return &e, nil
}

// LookupYT finds a cache entry by YouTube video ID.
// Returns (nil, nil) when no matching entry exists.
func (d *DB) LookupYT(videoID string) (*CacheEntry, error) {
	row := d.sql.QueryRow(`
		SELECT video_id, synced_lyrics, status, cached_at, not_found_at
		FROM yt_lyrics
		WHERE video_id=?`,
		normalize(videoID),
	)

	var e CacheEntry
	var cachedAt string
	var notFoundAt *string

	err := row.Scan(&e.VideoID, &e.SyncedLyrics, &e.Status, &cachedAt, &notFoundAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	e.CachedAt, _ = time.Parse(time.RFC3339, cachedAt)
	if notFoundAt != nil {
		t, _ := time.Parse(time.RFC3339, *notFoundAt)
		e.NotFoundAt = &t
	}
	return &e, nil
}

// InsertHit stores (or updates) a successful lrclib lyrics result.
func (d *DB) InsertHit(artistName, trackName, albumName string, duration int, syncedLyrics *string, instrumental bool) error {
	instr := 0
	if instrumental {
		instr = 1
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.sql.Exec(`
		INSERT INTO lyrics
		    (artist_name, track_name, album_name, duration, synced_lyrics, instrumental, status, cached_at)
		VALUES (?, ?, ?, ?, ?, ?, 200, ?)
		ON CONFLICT(artist_name, track_name, album_name, duration)
		DO UPDATE SET
		    synced_lyrics = excluded.synced_lyrics,
		    instrumental  = excluded.instrumental,
		    status        = 200,
		    cached_at     = excluded.cached_at,
		    not_found_at  = NULL`,
		normalize(artistName), normalize(trackName), normalize(albumName),
		duration, syncedLyrics, instr, now,
	)
	return err
}

// InsertNotFound records (or refreshes) a 404 lrclib result.
func (d *DB) InsertNotFound(artistName, trackName, albumName string, duration int) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.sql.Exec(`
		INSERT INTO lyrics
		    (artist_name, track_name, album_name, duration, status, cached_at, not_found_at)
		VALUES (?, ?, ?, ?, 404, ?, ?)
		ON CONFLICT(artist_name, track_name, album_name, duration)
		DO UPDATE SET
		    status       = 404,
		    not_found_at = excluded.not_found_at,
		    cached_at    = excluded.cached_at`,
		normalize(artistName), normalize(trackName), normalize(albumName), duration, now, now,
	)
	return err
}

// InsertYTHit stores (or updates) a successful YouTube Music lyrics result.
func (d *DB) InsertYTHit(videoID string, syncedLyrics *string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.sql.Exec(`
		INSERT INTO yt_lyrics
		    (video_id, synced_lyrics, status, cached_at)
		VALUES (?, ?, 200, ?)
		ON CONFLICT(video_id)
		DO UPDATE SET
		    synced_lyrics = excluded.synced_lyrics,
		    status        = 200,
		    cached_at     = excluded.cached_at,
		    not_found_at  = NULL`,
		normalize(videoID), syncedLyrics, now,
	)
	return err
}

// InsertYTNotFound records (or refreshes) a 404 YouTube Music result.
func (d *DB) InsertYTNotFound(videoID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.sql.Exec(`
		INSERT INTO yt_lyrics
		    (video_id, status, cached_at, not_found_at)
		VALUES (?, 404, ?, ?)
		ON CONFLICT(video_id)
		DO UPDATE SET
		    status       = 404,
		    not_found_at = excluded.not_found_at,
		    cached_at    = excluded.cached_at`,
		normalize(videoID), now, now,
	)
	return err
}

// Summary holds aggregate stats about the cache.
type Summary struct {
	CachedCount    int     `json:"cachedCount"`
	NotFoundCount  int     `json:"notFoundCount"`
	DBSizeMB       float64 `json:"dbSizeMB"`
	OldestCachedAt *string `json:"oldestCachedAt"`
	NewestCachedAt *string `json:"newestCachedAt"`
}

// GetSummary returns aggregate stats about the cache.
func (d *DB) GetSummary() (*Summary, error) {
	var s Summary

	if err := d.sql.QueryRow(`SELECT (SELECT COUNT(*) FROM lyrics WHERE status=200) + (SELECT COUNT(*) FROM yt_lyrics WHERE status=200)`).Scan(&s.CachedCount); err != nil {
		return nil, err
	}
	if err := d.sql.QueryRow(`SELECT (SELECT COUNT(*) FROM lyrics WHERE status=404) + (SELECT COUNT(*) FROM yt_lyrics WHERE status=404)`).Scan(&s.NotFoundCount); err != nil {
		return nil, err
	}

	if info, err := os.Stat(d.path); err == nil {
		s.DBSizeMB = float64(info.Size()) / (1024 * 1024)
	}

	var oldest, newest *string
	_ = d.sql.QueryRow(`SELECT MIN(cached_at), MAX(cached_at) FROM (SELECT cached_at FROM lyrics UNION ALL SELECT cached_at FROM yt_lyrics)`).Scan(&oldest, &newest)
	s.OldestCachedAt = oldest
	s.NewestCachedAt = newest

	return &s, nil
}

// SongEntry is one row in the cached-hits list.
type SongEntry struct {
	ArtistName string `json:"artistName,omitempty"`
	TrackName  string `json:"trackName,omitempty"`
	AlbumName  string `json:"albumName,omitempty"`
	Duration   int    `json:"duration,omitempty"`
	VideoID    string `json:"videoId,omitempty"`
	CachedAt   string `json:"cachedAt"`
}

// ListSongs returns a paginated list of cached hits, newest first.
func (d *DB) ListSongs(page, limit int) ([]SongEntry, int, error) {
	var total int
	if err := d.sql.QueryRow(`SELECT (SELECT COUNT(*) FROM lyrics WHERE status=200) + (SELECT COUNT(*) FROM yt_lyrics WHERE status=200)`).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := d.sql.Query(`
		SELECT artist_name, track_name, album_name, duration, video_id, cached_at
		FROM (
			SELECT artist_name, track_name, album_name, duration, '' AS video_id, cached_at FROM lyrics WHERE status=200
			UNION ALL
			SELECT '' AS artist_name, '' AS track_name, '' AS album_name, 0 AS duration, video_id, cached_at FROM yt_lyrics WHERE status=200
		)
		ORDER BY cached_at DESC
		LIMIT ? OFFSET ?`, limit, (page-1)*limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	songs := make([]SongEntry, 0, limit)
	for rows.Next() {
		var s SongEntry
		if err := rows.Scan(&s.ArtistName, &s.TrackName, &s.AlbumName, &s.Duration, &s.VideoID, &s.CachedAt); err != nil {
			return nil, 0, err
		}
		songs = append(songs, s)
	}
	return songs, total, rows.Err()
}

// NotFoundEntry is one row in the 404 list.
type NotFoundEntry struct {
	ArtistName string `json:"artistName,omitempty"`
	TrackName  string `json:"trackName,omitempty"`
	AlbumName  string `json:"albumName,omitempty"`
	Duration   int    `json:"duration,omitempty"`
	VideoID    string `json:"videoId,omitempty"`
	NotFoundAt string `json:"notFoundAt"`
	RetryAfter string `json:"retryAfter"`
}

// ListNotFound returns a paginated list of 404 entries, newest first.
func (d *DB) ListNotFound(page, limit, ttlDays int) ([]NotFoundEntry, int, error) {
	var total int
	if err := d.sql.QueryRow(`SELECT (SELECT COUNT(*) FROM lyrics WHERE status=404) + (SELECT COUNT(*) FROM yt_lyrics WHERE status=404)`).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := d.sql.Query(`
		SELECT artist_name, track_name, album_name, duration, video_id, not_found_at
		FROM (
			SELECT artist_name, track_name, album_name, duration, '' AS video_id, not_found_at FROM lyrics WHERE status=404
			UNION ALL
			SELECT '' AS artist_name, '' AS track_name, '' AS album_name, 0 AS duration, video_id, not_found_at FROM yt_lyrics WHERE status=404
		)
		ORDER BY not_found_at DESC
		LIMIT ? OFFSET ?`, limit, (page-1)*limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	entries := make([]NotFoundEntry, 0, limit)
	for rows.Next() {
		var e NotFoundEntry
		var notFoundAt string
		if err := rows.Scan(&e.ArtistName, &e.TrackName, &e.AlbumName, &e.Duration, &e.VideoID, &notFoundAt); err != nil {
			return nil, 0, err
		}
		e.NotFoundAt = notFoundAt
		if t, err := time.Parse(time.RFC3339, notFoundAt); err == nil {
			e.RetryAfter = t.AddDate(0, 0, ttlDays).Format(time.RFC3339)
		}
		entries = append(entries, e)
	}
	return entries, total, rows.Err()
}

// Close closes the underlying database connection.
func (d *DB) Close() error {
	return d.sql.Close()
}
