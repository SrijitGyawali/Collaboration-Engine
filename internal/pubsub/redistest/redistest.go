// Package redistest gives tests a real Redis: one container per test binary,
// started on first use. Like storetest, it skips without Docker locally, and
// fails in CI (the CI variable is set) or with -short skips.
package redistest

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
)

var shared struct {
	once      sync.Once
	url       string
	container testcontainers.Container
	err       error
}

// URL returns a redis:// URL of a running Redis, or skips the test.
func URL(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping Redis test in -short mode")
	}
	if os.Getenv("CI") == "" {
		testcontainers.SkipIfProviderIsNotHealthy(t)
	}
	shared.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		c, err := tcredis.Run(ctx, "redis:8-alpine")
		if err != nil {
			shared.err = err
			return
		}
		shared.container = c
		shared.url, shared.err = c.ConnectionString(ctx)
	})
	if shared.err != nil {
		t.Fatalf("start Redis: %v", shared.err)
	}
	return shared.url
}

// Stop removes the container. Call it from TestMain after m.Run.
func Stop() {
	if shared.container != nil {
		_ = shared.container.Terminate(context.Background())
	}
}
