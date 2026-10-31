// Command gateway accepts browser WebSocket connections and forwards edits to
// the sync service over gRPC. Gateways hold no document state, so any number
// of replicas can run behind a load balancer.
package main

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/SrijitGyawali/Collaboration-Engine/internal/platform"
)

func main() {
	platform.Main("gateway", func(ctx context.Context, log *slog.Logger) error {
		mux := http.NewServeMux()
		platform.RegisterHealth(mux)
		return platform.ServeHTTP(ctx, log, platform.Env("GATEWAY_ADDR", ":8080"), mux)
	})
}
