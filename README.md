# lrclib-cache-proxy

A lightweight synced-lyrics proxy that fetches from YouTube Music and [lrclib.net](https://lrclib.net), with an API for uploading user-provided Apple Music lyrics.

Requests are served from a local SQLite database on cache hits, so repeat lookups are instant and don't consume upstream bandwidth. 404 responses are remembered for 7 days before the upstream is re-checked (in case lyrics have since been added).

Docker image: **~8 MB**. Idle RAM: **~15–20 MB**.

## Features

- Caches `syncedLyrics` locally in SQLite — zero repeated upstream calls for known tracks
- 404 negative-cache with configurable TTL (default 7 days)
- Admin endpoints: storage summary, paginated song list, paginated 404 list
- SSRF-safe: redirects from upstream are rejected
- Response body capped at 1 MB to prevent memory exhaustion
- Runs as a non-root user inside the container
- Single static binary, no CGO, no libc dependency

## Quick start

```bash
git clone https://github.com/limhenry/lrclib-cache-proxy
cd lrclib-cache-proxy
docker compose up -d
```

The proxy is now listening on `http://localhost:3000`.

## API

### Get lyrics

Lyrics are fetched automatically from **YouTube Music** and, when needed, **LRCLIB**. Apple Music lyrics are not fetched automatically; upload them manually with `/api/upload` as described below. iTunes is used only to match tracks and resolve localized metadata, not to fetch lyrics.

#### Option 1: YouTube Music API (via `videoId`)

```
GET /api/get?videoId=B7kKeTRV0Xs
```

Only `videoId` is required. Fetches timed/synced lyrics directly from YouTube Music API (based on [this Gist](https://gist.github.com/limhenry/d9ba0f65234a496a16999714fc87ac78) and inspired by [ytmusicapi](https://github.com/sigma67/ytmusicapi)).

#### Option 2: Track metadata (YouTube Music search first with LRCLIB fallback)

```
GET /api/get?artist_name=Borislav+Slavov&track_name=I+Want+to+Live&album_name=Baldur%27s+Gate+3+(Original+Game+Soundtrack)&duration=233
```

All four query parameters (`artist_name`, `track_name`, `album_name`, `duration`) are required.

For a metadata request, the proxy first checks its local cache, including Apple Music lyrics uploaded under the same artist, track, album, and duration. On a cache miss, it may use iTunes Search to resolve localized metadata for regional Asian genres, such as Mandopop/Cantopop, K-Pop, and J-Pop (for example, `Hebe Tien` / `Deep in the Veins` to `田馥甄` / `靜脈`).

If no uploaded Apple Music lyrics are found, the proxy searches YouTube Music using the resolved metadata. If YouTube Music has no lyrics or the request fails, it falls back to LRCLIB.

**Optional parameter for `videoId` and metadata requests:**

| Parameter    | Description                                                                        |
| ------------ | ---------------------------------------------------------------------------------- |
| `force=true` | Bypass the local lyrics cache and retry the applicable provider flow. For metadata requests, this also skips cached Apple Music lyrics. |

#### Upload Apple Music lyrics manually

The proxy does not download lyrics from Apple Music. Upload LRC lyrics with the track metadata. This stores them in the same cache used by metadata GET requests, replacing any previous lyrics for that metadata key. The body is limited to 1 MB.

```bash
curl -X POST 'http://localhost:3000/api/upload?source=applemusic&artist_name=Yung+Lean&track_name=That%27s+It+%28feat.+Future+%26+Metro+Boomin%29&album_name=Grand+Theft+Auto+VI%3A+The+Album&duration=162' \
  --data-binary '@lyrics.lrc'
```

The upload key is `artist_name`, `track_name`, `album_name`, and `duration`, matching the metadata GET parameters. The optional `source=applemusic` marks the lyrics provider.

```text
GET /api/get?artist_name=Artist&track_name=Track&album_name=Album&duration=180
```

After uploading, retrieve the lyrics with the matching metadata GET request shown above. A cache hit returns immediately without contacting a provider.

**200 — cache hit or upstream found:**

```json
{
  "syncedLyrics": "[00:02.21] 經過沿海的公路 越過漫漫河流\n...",
  "source": "applemusic",
  "artistName": "田馥甄",
  "trackName": "靜脈",
  "albumName": "日常",
  "duration": 180
}
```

Successful GET responses include `source`, identifying where the returned lyrics came from: `applemusic`, `ytmusic`, or `lrclib`. Existing cache entries created before source tracking was added return `unknown`. Metadata-based requests also include `artistName`, `trackName`, `albumName`, and `duration`; when iTunes resolves localized metadata, those fields contain the localized values. `videoId` requests omit the metadata fields. `syncedLyrics` is `null` when a metadata match has no synced lyrics or is instrumental.

**404 — not found:**

```json
{
  "code": 404,
  "name": "TrackNotFound",
  "message": "Failed to find specified track"
}
```

**502** — an upstream request failed (network error or 5xx) during a `videoId` or metadata request. The failure is not cached, so a later request can retry.

**Metadata request flow:**
1. Check the metadata cache, unless `force=true`. Apple Music uploads use this same metadata cache and return with `source: "applemusic"`.
2. On a cache miss, use iTunes Search to resolve localized metadata when possible.
3. Search YouTube Music using the matched/localized metadata. A successful result is cached.
4. If YouTube Music has no lyrics or fails, query LRCLIB `/api/get`; if that returns 404, try LRCLIB `/api/search` for synced lyrics within ±2 seconds of the requested duration.
5. Cache a successful result, or cache a 404 when no lyrics are found.

---

#### Chrome extension

The unpacked Chrome extension watches Apple Music lyric, album, and library-playlist responses, pairs them in memory, converts TTML into line-timed LRC, and uploads lyrics using artist, track, album, and duration. The proxy stores only the metadata-keyed LRC. Word-synced lyrics are grouped by lyric line; per-word timings are not retained. It runs automatically on `music.apple.com` and logs extracted album/playlist metadata, converted LRC, and upload status in that tab's DevTools console. It does not log the full Apple response.

1. Start the proxy with `docker compose up -d`.
2. In Chrome 111 or later, open `chrome://extensions` and enable **Developer mode**.
3. Select **Load unpacked** and choose the repository's `chrome-extension` folder.
4. Open Apple Music and play a track. Check the tab's DevTools console for conversion and upload status.

The extension only matches the Apple Music web app and sends uploads to `http://localhost:3000`. Keep the local proxy running while using it.

To run the extension checks:

```bash
cd chrome-extension
npm install
npm test
npm run check
```

The XML parser dependency is only used by the Node tests; the extension uses Chrome's built-in `DOMParser`.

---

### Admin web interface & endpoints

#### Web Interface: `GET /admin` or `GET /admin/`

Open `http://localhost:3000/admin` in any browser to access the lightweight, built-in management interface:
- **Zero framework / zero external assets**: Single self-contained HTML/CSS/JS page embedded directly in the binary (~25 KB) with practically 0 MB idle RAM overhead.
- **Cache Dashboard**: Real-time stats for cached LRCLIB, YouTube, and Apple Music entries, negative-cache 404s, database file size, and timestamps.
- **Browse & Search**: Paginated table of cached tracks with live search filtering across track names, artists, albums, or YouTube video IDs.
- **Lyrics Inspector & Editor**: View formatted synced lyrics with timecode pills, one-click copy to clipboard, in-place lyrics editor, and instrumental toggle.
- **Cache Eviction & Upstream Refresh**: Delete cached entries or trigger immediate force re-fetch from upstream (`force=true`).
- **404 Management**: View negative cache retry-after timers, retry individual tracks, or flush all 404s in one click.
- **Quick Lookup Tool**: Test queries or fetch songs directly via metadata or YouTube video ID.

#### `GET /admin/summary`

Overall cache stats.

```json
{
  "cachedCount": 1042,
  "notFoundCount": 37,
  "dbSizeMB": 4.2,
  "oldestCachedAt": "2026-05-01T12:00:00Z",
  "newestCachedAt": "2026-05-30T09:41:00Z"
}
```

#### `GET /admin/songs?page=1&limit=50&q=query`

Paginated list of successfully cached metadata-based, YouTube, and Apple Music entries, newest first. Supports optional keyword search (`q`) across track metadata and YouTube video IDs.

```json
{
  "page": 1,
  "limit": 50,
  "total": 1042,
  "data": [
    {
      "id": 1,
      "source": "lrclib",
      "artistName": "borislav slavov",
      "trackName": "i want to live",
      "albumName": "baldur's gate 3 (original game soundtrack)",
      "duration": 233,
      "hasLyrics": true,
      "instrumental": false,
      "cachedAt": "2026-05-30T09:41:00Z"
    },
    {
      "id": 12,
      "source": "yt",
      "videoId": "B7kKeTRV0Xs",
      "hasLyrics": true,
      "instrumental": false,
      "cachedAt": "2026-05-30T09:30:00Z"
    },
    {
      "id": 13,
      "source": "lrclib",
      "lyricsSource": "applemusic",
      "artistName": "yung lean",
      "trackName": "that's it (feat. future & metro boomin)",
      "albumName": "grand theft auto vi: the album",
      "duration": 162,
      "hasLyrics": true,
      "instrumental": false,
      "cachedAt": "2026-05-30T09:20:00Z"
    }
  ]
}
```

#### `GET /admin/not-found?page=1&limit=50&q=query`

Paginated list of tracks that returned 404, newest first. Supports optional keyword search (`q`). Includes `retryAfter` so you can see when the proxy will re-check the upstream provider.

```json
{
  "page": 1,
  "limit": 50,
  "total": 37,
  "data": [
    {
      "id": 4,
      "source": "lrclib",
      "artistName": "some artist",
      "trackName": "unreleased track",
      "albumName": "demo",
      "duration": 180,
      "notFoundAt": "2026-05-30T09:00:00Z",
      "retryAfter": "2026-06-06T09:00:00Z"
    }
  ]
}
```

#### `GET /admin/entry?source=lrclib|yt&id=123`

Fetches a detailed cache entry including full `syncedLyrics` content and its `lyricsSource` provider.

#### `DELETE /admin/entry?source=lrclib|yt&id=123`

Deletes a cached track or 404 record from the database.

#### `PUT /admin/entry?source=lrclib|yt&id=123`

Updates `syncedLyrics` and/or `instrumental` status for an entry.

```json
{
  "syncedLyrics": "[00:01.00] Custom lyrics line\n...",
  "instrumental": false
}
```

#### `POST /admin/not-found/clear`

Flushes all 404 negative-cache records across both LRCLIB and YouTube tables.

## Configuration

The binary reads these options from environment variables. Docker Compose does not pass every variable through automatically; see the Compose note below.

The proxy writes HTTP request summaries, cache decisions, provider fallbacks, uploads, and operational errors to container logs. Follow them with `docker compose logs -f lrclib-cache-proxy`. Lyrics contents are not written to proxy logs.

| Variable                  | Default              | Description                                                                                         |
| ------------------------- | -------------------- | --------------------------------------------------------------------------------------------------- |
| `HOST_PORT`               | `3000`               | Host port Docker binds on — change this to expose on a different port (e.g. `9876`)                 |
| `PORT`                    | `3000`               | Port the binary listens on. For Docker, changing it also requires passing `PORT` into the container and updating the container-side port mapping. |
| `DB_PATH`            | `./lyrics.db`        | Path to the SQLite database file (`/data/lyrics.db` in the provided Docker Compose file)             |
| `LRCLIB_BASE_URL`    | `https://lrclib.net` | Base URL of the upstream lrclib instance                                                            |
| `NOT_FOUND_TTL_DAYS` | `7`                  | Days to serve a cached 404 before re-checking upstream                                              |
| `ALLOWED_ORIGINS`    | _(empty)_            | Comma-separated extra CORS origins beyond `http://localhost:*` (e.g. `https://example.com`)         |
| `ITUNES_RESOLVE_ENABLED`  | `true`               | Enable iTunes metadata resolution for Asian tracks with English names                               |
| `ITUNES_STOREFRONT`       | `MY`                 | Initial iTunes storefront to query for track genre (e.g. `MY`, `US`)                                |
| `ITUNES_MANDOPOP_COUNTRY` | `TW`                 | Target storefront for Mandopop/Chinese tracks (`TW` for Traditional Chinese, `CN` for Simplified)   |
| `ITUNES_CANTOPOP_COUNTRY` | `HK`                 | Target storefront for Cantopop tracks                                                               |
| `ITUNES_KPOP_COUNTRY`     | `KR`                 | Target storefront for K-Pop tracks                                                                 |
| `ITUNES_JPOP_COUNTRY`     | `JP`                 | Target storefront for J-Pop tracks                                                                 |

Copy `.env.example` to `.env` to configure Compose interpolation such as `HOST_PORT` and `ALLOWED_ORIGINS`. Compose does not automatically pass all `.env` values into the container. The provided Compose file sets `DB_PATH`, `LRCLIB_BASE_URL`, and `NOT_FOUND_TTL_DAYS` directly, so change its `environment` section to override them. To customize iTunes settings in Docker, add the corresponding `ITUNES_*` variables to that section.

When running the binary directly, export the variables in your shell before starting it.

## Cache behaviour

| Scenario                      | Behaviour                                                                                               |
| ----------------------------- | ------------------------------------------------------------------------------------------------------- |
| Track cached (200)            | Returned from SQLite immediately — upstream never called                                                |
| Track cached (404), age < TTL | 404 returned immediately — upstream never called                                                        |
| Track cached (404), age ≥ TTL | Re-queried from upstream (iTunes resolution -> YouTube Music search -> LRCLIB); record updated           |
| Metadata request not in cache | Resolve localized metadata with iTunes, search YouTube Music, then fall back to LRCLIB              |
| Apple Music upload             | Stored in the metadata cache and returned on the matching metadata GET                             |
| `force=true`                   | On metadata and `videoId` requests, bypasses their caches and retries the provider flow. Metadata requests skip cached Apple Music lyrics. |
| Upstream 5xx or network error | 502 returned; **nothing cached** — next request retries upstream                                        |

> **Note:** artist name, track name, and album name are normalised (lowercased and trimmed) before storage, so `Taylor Swift` and `taylor swift` resolve to the same cache entry. Video IDs are case-sensitive and only trimmed.

## Building without Docker

Requires Go 1.25+.

```bash
go build -o lrclib-cache-proxy .
DB_PATH=./lyrics.db ./lrclib-cache-proxy
```

## Project structure

```
.
├── main.go             # Entry point — config, router, graceful shutdown
├── db/db.go            # SQLite layer — schema, upserts, paginated queries
├── itunes/
│   ├── client.go       # iTunes Search & Lookup API client, genre detection, localization
│   └── client_test.go  # Unit tests for iTunes client
├── lrclib/client.go    # Upstream LRCLIB HTTP client
├── ytmusic/
│   ├── client.go       # YouTube Music client struct, constructor, shared types
│   ├── search.go       # Search API (/youtubei/v1/search) & video ID scoring
│   ├── next.go         # Next API (/youtubei/v1/next) & browseId lookup
│   └── browse.go       # Browse API (/youtubei/v1/browse) & synced lyrics fetching
├── chrome-extension/
│   ├── manifest.json   # Apple Music host match and local proxy permission
│   ├── main.js         # Lyrics API response observer in the page's main world
│   ├── content.js      # TTML conversion handoff and console output
│   ├── ttml-converter.js # TTML-to-LRC conversion
│   └── service-worker.js # Duplicate check and local upload
├── handler/
│   ├── proxy.go        # GET /api/get — cache logic, iTunes resolution, YouTube Music & LRCLIB
│   └── admin.go        # GET /admin/* — stats and list endpoints
├── Dockerfile          # Multi-stage build: golang:1.25-alpine → alpine:3.21
└── docker-compose.yml
```

## Credits

- YouTube Music synced lyrics logic based on [ytmusicapi_lyrics.js Gist](https://gist.github.com/limhenry/d9ba0f65234a496a16999714fc87ac78) and inspired by [ytmusicapi](https://github.com/sigma67/ytmusicapi).
- Upstream lyrics service by [lrclib.net](https://lrclib.net).

## License

MIT

---

> This project was fully generated with [GitHub Copilot](https://github.com/features/copilot).
