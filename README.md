# lrclib-cache-proxy

A lightweight caching proxy for [lrclib.net](https://lrclib.net) — a free, open-source synced lyrics API.

Requests are served from a local SQLite database on cache hits, so repeat lookups are instant and don't consume lrclib's bandwidth. 404 responses are remembered for 7 days before the upstream is re-checked (in case lrclib has since added the track).

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

```
GET /api/get?artist_name=Borislav+Slavov&track_name=I+Want+to+Live&album_name=Baldur%27s+Gate+3+(Original+Game+Soundtrack)&duration=233
```

Drop-in replacement for the lrclib `/api/get` endpoint. All four query parameters are required.

**200 — cache hit or upstream found:**

```json
{ "syncedLyrics": "[00:17.12] I feel your breath upon my neck\n..." }
```

`syncedLyrics` is `null` when the track is instrumental or has no synced lyrics.

**404 — not found:**

```json
{
  "code": 404,
  "name": "TrackNotFound",
  "message": "Failed to find specified track"
}
```

**502** — upstream request failed (network error or lrclib 5xx). Not cached; the next request will retry.

---

### Admin endpoints

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

#### `GET /admin/songs?page=1&limit=50`

Paginated list of successfully cached tracks, newest first.

```json
{
  "page": 1,
  "limit": 50,
  "total": 1042,
  "data": [
    {
      "artistName": "borislav slavov",
      "trackName": "i want to live",
      "albumName": "baldur's gate 3 (original game soundtrack)",
      "duration": 233,
      "cachedAt": "2026-05-30T09:41:00Z"
    }
  ]
}
```

#### `GET /admin/not-found?page=1&limit=50`

Paginated list of tracks that returned 404, newest first. Includes `retryAfter` so you can see when the proxy will re-check lrclib.

```json
{
  "page": 1,
  "limit": 50,
  "total": 37,
  "data": [
    {
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

`limit` is capped at 500 per page.

## Configuration

All options are set via environment variables.

| Variable             | Default              | Description                                                                                         |
| -------------------- | -------------------- | --------------------------------------------------------------------------------------------------- |
| `HOST_PORT`          | `3000`               | Host port Docker binds on — change this to expose on a different port (e.g. `9876`)                 |
| `PORT`               | `3000`               | Port the binary listens on **inside** the container — only needed if you change the `ports` mapping |
| `DB_PATH`            | `./lyrics.db`        | Path to the SQLite database file                                                                    |
| `LRCLIB_BASE_URL`    | `https://lrclib.net` | Base URL of the upstream lrclib instance                                                            |
| `NOT_FOUND_TTL_DAYS` | `7`                  | Days to serve a cached 404 before re-checking upstream                                              |

Copy `.env.example` to `.env` and edit as needed, then pass it to Compose:

```yaml
# docker-compose.yml
env_file: .env
```

## Cache behaviour

| Scenario                      | Behaviour                                                        |
| ----------------------------- | ---------------------------------------------------------------- |
| Track cached (200)            | Returned from SQLite immediately — upstream never called         |
| Track cached (404), age < TTL | 404 returned immediately — upstream never called                 |
| Track cached (404), age ≥ TTL | Re-queried from upstream; record updated                         |
| Track not in cache            | Queried from upstream; result cached regardless of 200 or 404    |
| Upstream 5xx or network error | 502 returned; **nothing cached** — next request retries upstream |

> **Note:** artist name, track name, and album name are normalised (lowercased and trimmed) before storage, so `Taylor Swift` and `taylor swift` resolve to the same cache entry.

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
├── lrclib/client.go    # Upstream HTTP client
├── handler/
│   ├── proxy.go        # GET /api/get — cache logic
│   └── admin.go        # GET /admin/* — stats and list endpoints
├── Dockerfile          # Multi-stage build: golang:1.25-alpine → alpine:3.21
└── docker-compose.yml
```

## License

MIT

---

> This project was fully generated with [GitHub Copilot](https://github.com/features/copilot).
