# ── Build stage ──────────────────────────────────────────────────────────────
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Cache module downloads separately from source code.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 — pure Go binary, no libc dependency.
# -s -w strip debug info for a smaller binary.
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o lrclib-cache-proxy .

# ── Runtime stage ─────────────────────────────────────────────────────────────
FROM alpine:3.21

# ca-certificates is required for TLS connections to lrclib.net.
RUN apk add --no-cache ca-certificates && \
    addgroup -S app && adduser -S -G app app

WORKDIR /app
COPY --from=builder /app/lrclib-cache-proxy .

# Ensure the data directory exists and is owned by the non-root user.
RUN mkdir -p /data && chown app:app /data

USER app

EXPOSE 3000

VOLUME ["/data"]

CMD ["./lrclib-cache-proxy"]
