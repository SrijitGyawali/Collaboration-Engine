package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// These tests run against a real PostgreSQL, not a mock: what matters here is
// how SQL, locks, constraints and transactions actually behave. One container
// is started for the whole package, on first use, and the repository's real
// migration files are applied to it.

// shared is the container and store used by every test in the package.
var shared struct {
	once      sync.Once
	store     *Store
	url       string
	container testcontainers.Container
	err       error
}

func TestMain(m *testing.M) {
	code := m.Run()
	if shared.container != nil {
		_ = shared.container.Terminate(context.Background())
	}
	if shared.store != nil {
		shared.store.Close()
	}
	os.Exit(code)
}

// newTestStore returns a store on an empty, fully migrated database.
//
// Without Docker the test is skipped locally, so `go test ./...` still works
// on a machine without it, but it fails in CI, where a skip would silently
// hide the whole data layer.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping PostgreSQL test in -short mode")
	}
	if os.Getenv("CI") == "" {
		testcontainers.SkipIfProviderIsNotHealthy(t)
	}

	shared.once.Do(startPostgres)
	if shared.err != nil {
		t.Fatalf("start PostgreSQL: %v", shared.err)
	}

	ctx := t.Context()
	if _, err := shared.store.pool.Exec(ctx, `TRUNCATE documents, outbox RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("reset database: %v", err)
	}
	return shared.store
}

func startPostgres() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	container, err := postgres.Run(ctx, "postgres:17-alpine",
		postgres.WithDatabase("collab_test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		shared.err = err
		return
	}
	shared.container = container

	url, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		shared.err = err
		return
	}
	shared.url = url

	if shared.err = applyMigrations(ctx, url, ".up.sql"); shared.err != nil {
		return
	}
	shared.store, shared.err = Open(ctx, url)
}

// applyMigrations runs the repository's migration files whose names end in
// suffix, in order for ".up.sql" and in reverse for ".down.sql".
func applyMigrations(ctx context.Context, url, suffix string) error {
	files, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*"+suffix))
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no %s files found", suffix)
	}
	slices.Sort(files)
	if suffix == ".down.sql" {
		slices.Reverse(files)
	}

	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(ctx) }()

	for _, file := range files {
		sql, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		if _, err := conn.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(file), err)
		}
	}
	return nil
}
