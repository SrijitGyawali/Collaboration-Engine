// Command sync is the only writer of document state. It applies operations to
// each document's CRDT, stores them in PostgreSQL with an outbox row, and
// publishes committed operations to Redis.
package main

import (
	"context"
	"log/slog"
	"net/http"

	"collab-engine/internal/platform"
)

func main() {
	platform.Main("sync", func(ctx context.Context, log *slog.Logger) error {
		mux := http.NewServeMux()
		platform.RegisterHealth(mux)
		return platform.ServeHTTP(ctx, log, platform.Env("SYNC_HTTP_ADDR", ":8081"), mux)
	})
}
