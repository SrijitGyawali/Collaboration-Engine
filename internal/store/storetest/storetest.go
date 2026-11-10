// Package storetest gives tests a real, migrated PostgreSQL.
//
// The data layer's behaviour depends on how SQL, locks, constraints and
// transactions actually behave, which a mock would only guess. So tests in
// any package that touches the database share this helper: one container per
// test binary, started on first use, with the repository's real migration
// files applied.
//
// Without Docker, tests are skipped locally, so `go test ./...` works on any
// machine; in CI (the CI variable is set) they fail instead, because a skip
// there would silently hide the whole data layer. -short skips them too.
package storetest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

var shared struct {
	once      sync.Once
	url       string
	container testcontainers.Container
	err       error
}

// URL returns the connection URL of a migrated PostgreSQL, starting it on
// first use, or skips the test (see the package comment).
func URL(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping PostgreSQL test in -short mode")
	}
	if os.Getenv("CI") == "" {
		testcontainers.SkipIfProviderIsNotHealthy(t)
	}
	shared.once.Do(start)
	if shared.err != nil {
		t.Fatalf("start PostgreSQL: %v", shared.err)
	}
	return shared.url
}

// Reset empties every table, so a test starts from a clean database.
func Reset(t *testing.T, url string) {
	t.Helper()
	conn, err := pgx.Connect(t.Context(), url)
	if err != nil {
		t.Fatalf("reset database: %v", err)
	}
	defer func() { _ = conn.Close(context.Background()) }()
	if _, err := conn.Exec(t.Context(), `TRUNCATE documents, outbox RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("reset database: %v", err)
	}
}

// Stop removes the container. Call it from TestMain after m.Run.
func Stop() {
	if shared.container != nil {
		_ = shared.container.Terminate(context.Background())
	}
}

func start() {
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

	if shared.url, shared.err = container.ConnectionString(ctx, "sslmode=disable"); shared.err != nil {
		return
	}
	shared.err = ApplyMigrations(ctx, shared.url, ".up.sql")
}

// ApplyMigrations runs the repository's migration files whose names end in
// suffix: in order for ".up.sql", in reverse for ".down.sql".
func ApplyMigrations(ctx context.Context, url, suffix string) error {
	files, err := filepath.Glob(filepath.Join(migrationsDir(), "*"+suffix))
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no %s files in %s", suffix, migrationsDir())
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

// migrationsDir finds the repository's migrations folder from this source
// file, so it works whichever package's tests are running.
func migrationsDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "migrations")
}
