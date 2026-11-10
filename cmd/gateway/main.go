// Command gateway accepts browser WebSocket connections on /ws and keeps the
// clients of each document in a room.
//
// For now the sync service runs inside the gateway process. Documents are
// stored in PostgreSQL when DATABASE_URL is set, and in memory otherwise.
// Gateways hold no document state of their own, so once the sync service
// moves behind gRPC any number of them can run side by side.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/SrijitGyawali/Collaboration-Engine/internal/gateway"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/platform"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/store"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/store/memstore"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/syncsvc"
)

func main() {
	platform.Main("gateway", func(ctx context.Context, log *slog.Logger) error {
		st, closeStore, err := openStore(ctx, log)
		if err != nil {
			return err
		}
		defer closeStore()
		svc := syncsvc.New(st, log, syncsvc.Options{})
		defer svc.Close()

		ws := gateway.NewServer(gateway.Config{
			Backend:        svc,
			Log:            log,
			AllowedOrigins: splitList(platform.Env("GATEWAY_ALLOWED_ORIGINS", "localhost:3000")),
		})
		// After the HTTP server stops: WebSocket connections are hijacked
		// from it, so they are closed separately, before the service.
		defer ws.Close()

		mux := http.NewServeMux()
		platform.RegisterHealth(mux)
		mux.Handle("GET /ws", ws)
		return platform.ServeHTTP(ctx, log, platform.Env("GATEWAY_ADDR", ":8080"), mux)
	})
}

// openStore connects to PostgreSQL at DATABASE_URL, or falls back to memory
// when it is not set, saying loudly that nothing will be saved.
func openStore(ctx context.Context, log *slog.Logger) (syncsvc.Store, func(), error) {
	url := platform.Env("DATABASE_URL", "")
	if url == "" {
		log.Warn("DATABASE_URL is not set: documents are kept in memory and lost when the gateway stops")
		return memstore.New(), func() {}, nil
	}
	st, err := store.Open(ctx, url)
	if err != nil {
		return nil, nil, err
	}
	log.Info("storing documents in PostgreSQL")
	return st, st.Close, nil
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
