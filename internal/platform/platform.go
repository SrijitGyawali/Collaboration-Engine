// Package platform holds the process plumbing shared by every service:
// configuration from the environment, structured logging, signal handling and
// HTTP serving with graceful shutdown.
package platform

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const shutdownTimeout = 10 * time.Second

// Env returns the value of the environment variable key, or def when it is
// unset or empty.
func Env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// NewLogger returns a JSON logger tagged with the service name. Set
// LOG_LEVEL=debug for verbose output.
func NewLogger(service string) *slog.Logger {
	level := slog.LevelInfo
	if Env("LOG_LEVEL", "info") == "debug" {
		level = slog.LevelDebug
	}
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	return slog.New(h).With("service", service)
}

// Main runs fn with a service logger and a context that is cancelled on
// SIGINT or SIGTERM, and exits non-zero if fn returns an error.
func Main(service string, fn func(ctx context.Context, log *slog.Logger) error) {
	log := NewLogger(service)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := fn(ctx, log)
	stop()
	if err != nil {
		log.Error("exited with error", "err", err)
		os.Exit(1)
	}
	log.Info("stopped")
}

// RegisterHealth adds a liveness endpoint, GET /healthz, to mux.
func RegisterHealth(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}

// ServeHTTP serves h on addr until ctx is cancelled, then shuts the server
// down gracefully.
func ServeHTTP(ctx context.Context, log *slog.Logger, addr string, h http.Handler) error {
	srv := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 5 * time.Second}

	errc := make(chan error, 1)
	go func() {
		log.Info("http server listening", "addr", addr)
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}

	log.Info("shutting down http server")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
