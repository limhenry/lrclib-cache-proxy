package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/limhenry/lrclib-cache-proxy/db"
	"github.com/limhenry/lrclib-cache-proxy/handler"
	"github.com/limhenry/lrclib-cache-proxy/lrclib"
)

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// corsMiddleware allows origins listed in ALLOWED_ORIGINS (comma-separated).
// Supports a trailing "*" wildcard, e.g. "http://localhost:*" matches any port.
func corsMiddleware(allowedOrigins []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" {
				for _, allowed := range allowedOrigins {
					if originMatches(allowed, origin) {
						w.Header().Set("Access-Control-Allow-Origin", origin)
						w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
						w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
						w.Header().Set("Vary", "Origin")
						break
					}
				}
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// originMatches checks if an origin matches a pattern.
// A pattern ending in ":*" matches any port on that scheme+host,
// e.g. "http://localhost:*" matches "http://localhost:3000".
func originMatches(pattern, origin string) bool {
	if strings.HasSuffix(pattern, ":*") {
		prefix := strings.TrimSuffix(pattern, ":*") + ":"
		return strings.HasPrefix(origin, prefix)
	}
	return pattern == origin
}

func main() {
	port := getEnv("PORT", "3000")
	dbPath := getEnv("DB_PATH", "./lyrics.db")
	lrclibBaseURL := getEnv("LRCLIB_BASE_URL", "https://lrclib.net")
	notFoundTTLDays, _ := strconv.Atoi(getEnv("NOT_FOUND_TTL_DAYS", "7"))
	if notFoundTTLDays <= 0 {
		notFoundTTLDays = 7
	}

	allowedOrigins := []string{"http://localhost:*"}
	for _, o := range strings.Split(os.Getenv("ALLOWED_ORIGINS"), ",") {
		if o = strings.TrimSpace(o); o != "" {
			allowedOrigins = append(allowedOrigins, o)
		}
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	database, err := db.New(dbPath)
	if err != nil {
		slog.Error("failed to open database", "err", err, "path", dbPath)
		os.Exit(1)
	}
	defer database.Close()
	slog.Info("database ready", "path", dbPath)

	client := lrclib.NewClient(lrclibBaseURL)
	proxyH := handler.NewProxyHandler(database, client, notFoundTTLDays)
	adminH := handler.NewAdminHandler(database, notFoundTTLDays)

	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware(allowedOrigins))

	r.Get("/api/get", proxyH.ServeHTTP)

	r.Route("/admin", func(r chi.Router) {
		r.Get("/summary", adminH.Summary)
		r.Get("/songs", adminH.Songs)
		r.Get("/not-found", adminH.NotFound)
	})

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		slog.Info("server listening", "port", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down gracefully...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("forced shutdown", "err", err)
	}
	slog.Info("stopped")
}
