// Command outbox-relay reads unpublished rows from the outbox table and
// publishes them to Kafka, marking each row once the broker has accepted it.
package main

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/SrijitGyawali/Collaboration-Engine/internal/platform"
)

func main() {
	platform.Main("outbox-relay", func(ctx context.Context, log *slog.Logger) error {
		mux := http.NewServeMux()
		platform.RegisterHealth(mux)
		return platform.ServeHTTP(ctx, log, platform.Env("RELAY_HTTP_ADDR", ":8082"), mux)
	})
}
