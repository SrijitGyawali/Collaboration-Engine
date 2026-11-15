// Command sync is the only writer of document state. It serves the sync
// service over gRPC (SYNC_GRPC_ADDR, default :9090) for the gateways: each
// document's edits are applied to its CRDT and stored, with an outbox row for
// Kafka, in PostgreSQL when DATABASE_URL is set and in memory otherwise.
//
// With REDIS_ADDR set, every newly stored batch is also published to Redis,
// where each gateway subscribes to the documents its clients have open. That
// is what lets several gateways serve one document; with a single gateway it
// can be left unset.
//
// It also serves GET /healthz over HTTP (SYNC_HTTP_ADDR, default :8081) and
// the standard gRPC health service, for load balancers and orchestrators.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"github.com/SrijitGyawali/Collaboration-Engine/internal/platform"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/pubsub"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/store"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/store/memstore"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/syncrpc"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/syncsvc"
)

const grpcStopTimeout = 10 * time.Second

func main() {
	platform.Main("sync", func(ctx context.Context, log *slog.Logger) error {
		st, closeStore, err := openStore(ctx, log)
		if err != nil {
			return err
		}
		defer closeStore()
		var opts syncsvc.Options
		if addr := platform.Env("REDIS_ADDR", ""); addr != "" {
			r, err := pubsub.NewRedis(ctx, addr, log)
			if err != nil {
				return err
			}
			// Deferred before svc.Close, so it runs after: documents may
			// still publish while they shut down.
			defer func() { _ = r.Close() }()
			opts.Publisher = r
			log.Info("publishing committed edits to Redis", "addr", addr)
		} else {
			log.Warn("REDIS_ADDR is not set: edits are not published, so only a single gateway can be run")
		}
		svc := syncsvc.New(st, log, opts)
		defer svc.Close()

		g := grpc.NewServer()
		syncrpc.Register(g, svc)
		healthpb.RegisterHealthServer(g, health.NewServer())

		mux := http.NewServeMux()
		platform.RegisterHealth(mux)

		// Either server failing stops the whole process: a sync service
		// that answers health checks but not gRPC would look healthy while
		// every edit fails.
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		errc := make(chan error, 2)
		go func() { errc <- serveGRPC(ctx, log, g, platform.Env("SYNC_GRPC_ADDR", ":9090")) }()
		go func() { errc <- platform.ServeHTTP(ctx, log, platform.Env("SYNC_HTTP_ADDR", ":8081"), mux) }()

		first := <-errc
		cancel()
		return errors.Join(first, <-errc)
	})
}

// serveGRPC serves g on addr until ctx ends, then stops it gracefully: calls
// in flight may finish (an edit being stored is not cut in half), up to a
// timeout.
func serveGRPC(ctx context.Context, log *slog.Logger, g *grpc.Server, addr string) error {
	lis, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	errc := make(chan error, 1)
	go func() {
		log.Info("grpc server listening", "addr", addr)
		errc <- g.Serve(lis)
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down grpc server")
	stopped := make(chan struct{})
	go func() {
		g.GracefulStop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(grpcStopTimeout):
		g.Stop()
	}
	return nil
}

// openStore connects to PostgreSQL at DATABASE_URL, or falls back to memory
// when it is not set, saying loudly that nothing will be saved.
func openStore(ctx context.Context, log *slog.Logger) (syncsvc.Store, func(), error) {
	url := platform.Env("DATABASE_URL", "")
	if url == "" {
		log.Warn("DATABASE_URL is not set: documents are kept in memory and lost when the service stops")
		return memstore.New(), func() {}, nil
	}
	st, err := store.Open(ctx, url)
	if err != nil {
		return nil, nil, err
	}
	log.Info("storing documents in PostgreSQL")
	return st, st.Close, nil
}
