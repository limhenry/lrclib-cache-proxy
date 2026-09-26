# lrclib-cache-proxy

A lightweight caching proxy for [lrclib.net](https://lrclib.net) and YouTube Music API for synced lyrics.

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

The endpoint supports fetching lyrics via either **YouTube Music API** or **LRCLIB**:

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

When metadata parameters are passed, the proxy first checks the local SQLite cache. On a cache miss (or with `force=true`), the proxy resolves whether the song is an Asian track (such as Chinese Mandopop/Cantopop, Korean K-Pop, Japanese J-Pop) that has English metadata (e.g. from an Apple Music US or Malaysia storefront) via the iTunes Search API. If so, it looks up the original localized metadata from the regional storefront (e.g., `田馥甄` / `靜脈` instead of `Hebe Tien` / `Deep in the Veins`, or `방탄소년단` / `봄날` instead of `BTS` / `Spring Day`).

The proxy then searches YouTube Music using the resolved original metadata and attempts to fetch synced lyrics. If YouTube Music returns no lyrics or fails, it automatically falls back to LRCLIB.

**Optional parameter (supported on both):**

| Parameter    | Description                                                                        |
| ------------ | ---------------------------------------------------------------------------------- |
| `force=true` | Bypass the cache and always query the upstream API, then update the cached entry   |

**200 — cache hit or upstream found:**

```json
{ "syncedLyrics": "[00:02.21] 經過沿海的公路 越過漫漫河流\n..." }
```

`syncedLyrics` is `null` when the track has no synced lyrics or is instrumental.

**404 — not found:**

```json
{
  "code": 404,
  "name": "TrackNotFound",
  "message": "Failed to find specified track"
}
```

**502** — upstream request failed (network error or 5xx). Not cached; the next request will retry.

**Fallback sequence:**
1. **Metadata Resolution (iTunes API)**: Checks iTunes Search API with the track name and artist. If the genre indicates a localized Asian genre (Mandopop, Cantopop, K-Pop, J-Pop), fetches the original native title and artist from the target storefront (default: `TW` for Mandopop, `HK` for Cantopop, `KR` for K-Pop, `JP` for J-Pop).
2. **YouTube Music Search**: Searches YouTube Music using the resolved track metadata and ranks candidate video IDs by title, artist, album, and duration similarity. If a matching video ID with synced lyrics is found, it is returned and cached in SQLite under both the requested metadata and the resolved metadata.
3. **LRCLIB Lookup**: If YouTube Music returns no lyrics or fails, the proxy queries LRCLIB `/api/get` with resolved metadata.
4. **LRCLIB Search Fallback**: If LRCLIB `/api/get` returns 404, it retries via LRCLIB `/api/search` for a track with synced lyrics within ±2 seconds duration.
5. If a match is found from either provider, it is cached in SQLite as a hit (200); otherwise the 404 is cached normally.

---

### Admin web interface & endpoints

#### Web Interface: `GET /admin` or `GET /admin/`

Open `http://localhost:3000/admin` in any browser to access the lightweight, built-in management interface:
- **Zero framework / zero external assets**: Single self-contained HTML/CSS/JS page embedded directly in the binary (~25 KB) with practically 0 MB idle RAM overhead.
- **Cache Dashboard**: Real-time stats for cached tracks, negative-cache 404s, database file size, and timestamps.
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

Paginated list of successfully cached tracks, newest first. Supports optional keyword search (`q`).

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

Fetches detailed cache entry including full `syncedLyrics` content and `instrumental` flag.

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

All options are set via environment variables.

| Variable                  | Default              | Description                                                                                         |
| ------------------------- | -------------------- | --------------------------------------------------------------------------------------------------- |
| `HOST_PORT`               | `3000`               | Host port Docker binds on — change this to expose on a different port (e.g. `9876`)                 |
| `PORT`                    | `3000`               | Port the binary listens on **inside** the container — only needed if you change the `ports` mapping |
| `DB_PATH`            | `./lyrics.db`        | Path to the SQLite database file                                                                    |
| `LRCLIB_BASE_URL`    | `https://lrclib.net` | Base URL of the upstream lrclib instance                                                            |
| `NOT_FOUND_TTL_DAYS` | `7`                  | Days to serve a cached 404 before re-checking upstream                                              |
| `ALLOWED_ORIGINS`    | _(empty)_            | Comma-separated extra CORS origins beyond `http://localhost:*` (e.g. `https://example.com`)         |
| `ITUNES_RESOLVE_ENABLED`  | `true`               | Enable iTunes metadata resolution for Asian tracks with English names                               |
| `ITUNES_STOREFRONT`       | `MY`                 | Initial iTunes storefront to query for track genre (e.g. `MY`, `US`)                                |
| `ITUNES_MANDOPOP_COUNTRY` | `TW`                 | Target storefront for Mandopop/Chinese tracks (`TW` for Traditional Chinese, `CN` for Simplified)   |
| `ITUNES_CANTOPOP_COUNTRY` | `HK`                 | Target storefront for Cantopop tracks                                                               |
| `ITUNES_KPOP_COUNTRY`     | `KR`                 | Target storefront for K-Pop tracks                                                                 |
| `ITUNES_JPOP_COUNTRY`     | `JP`                 | Target storefront for J-Pop tracks                                                                 |

Copy `.env.example` to `.env` and edit as needed, then pass it to Compose:

```yaml
# docker-compose.yml
env_file: .env
```

## Cache behaviour

| Scenario                      | Behaviour                                                                                               |
| ----------------------------- | ------------------------------------------------------------------------------------------------------- |
| Track cached (200)            | Returned from SQLite immediately — upstream never called                                                |
| Track cached (404), age < TTL | 404 returned immediately — upstream never called                                                        |
| Track cached (404), age ≥ TTL | Re-queried from upstream (iTunes resolution -> YouTube Music search -> LRCLIB); record updated           |
| Track not in cache            | Resolved via iTunes, searched on YouTube Music, fallback to LRCLIB; result cached (200 or 404) in DB    |
| `force=true`                  | Cache bypassed; upstream always queried and cached entry updated                                        |
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
