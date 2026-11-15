// Command gateway accepts browser WebSocket connections on /ws and keeps the
// clients of each document in a room. Every edit goes to the sync service
// over gRPC (SYNC_GRPC_TARGET, default localhost:9090), which validates and
// stores it.
//
// A gateway holds no document state and no database connection of its own,
// so any number of them can run side by side. For that, set REDIS_ADDR here
// and on the sync service: the gateway then subscribes to each open
// document's channel and hears about every committed edit, whichever gateway
// it was sent through. Without it, the gateway broadcasts only the edits it
// carried itself, which is right for exactly one gateway.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/SrijitGyawali/Collaboration-Engine/internal/gateway"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/platform"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/pubsub"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/syncrpc"
)

func main() {
	platform.Main("gateway", func(ctx context.Context, log *slog.Logger) error {
		target := platform.Env("SYNC_GRPC_TARGET", "localhost:9090")
		backend, err := syncrpc.Dial(target)
		if err != nil {
			return err
		}
		defer func() { _ = backend.Close() }()
		log.Info("using the sync service", "target", target)

		cfg := gateway.Config{
			Backend:        backend,
			Log:            log,
			AllowedOrigins: splitList(platform.Env("GATEWAY_ALLOWED_ORIGINS", "localhost:3000")),
		}
		if addr := platform.Env("REDIS_ADDR", ""); addr != "" {
			r, err := pubsub.NewRedis(ctx, addr, log)
			if err != nil {
				return err
			}
			// Closed after the WebSocket server (deferred before it), which
			// unsubscribes as its connections end.
			defer func() { _ = r.Close() }()
			cfg.Subscriber = r
			log.Info("following documents through Redis", "addr", addr)
		} else {
			log.Warn("REDIS_ADDR is not set: only edits sent through this gateway are broadcast, so run no more than one")
		}

		ws := gateway.NewServer(cfg)
		// After the HTTP server stops: WebSocket connections are hijacked
		// from it, so they are closed separately, before the gRPC client.
		defer ws.Close()

		mux := http.NewServeMux()
		platform.RegisterHealth(mux)
		mux.Handle("GET /ws", ws)
		return platform.ServeHTTP(ctx, log, platform.Env("GATEWAY_ADDR", ":8080"), mux)
	})
}

// splitList splits a comma-separated setting, ignoring blanks.
func splitList(s string) []string {
	var out []string
	for part := range strings.SplitSeq(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
