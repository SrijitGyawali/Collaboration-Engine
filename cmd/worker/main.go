// Command worker consumes document events from Kafka to build snapshots and
// the document history feed. Every handler is idempotent by op_id, because
// Kafka delivers at least once.
package main

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/SrijitGyawali/Collaboration-Engine/internal/platform"
)

func main() {
	platform.Main("worker", func(ctx context.Context, log *slog.Logger) error {
		mux := http.NewServeMux()
		platform.RegisterHealth(mux)
		return platform.ServeHTTP(ctx, log, platform.Env("WORKER_HTTP_ADDR", ":8083"), mux)
	})
}
