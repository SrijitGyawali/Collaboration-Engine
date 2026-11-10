package store

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/SrijitGyawali/Collaboration-Engine/internal/store/storetest"
)

// These tests run against a real PostgreSQL (see storetest). One Store is
// opened for the whole package and every test starts from empty tables.

var shared struct {
	once  sync.Once
	store *Store
	url   string
	err   error
}

func TestMain(m *testing.M) {
	code := m.Run()
	if shared.store != nil {
		shared.store.Close()
	}
	storetest.Stop()
	os.Exit(code)
}

// newTestStore returns a store on an empty, fully migrated database.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	url := storetest.URL(t)
	shared.once.Do(func() {
		shared.url = url
		shared.store, shared.err = Open(context.Background(), url)
	})
	if shared.err != nil {
		t.Fatalf("open store: %v", shared.err)
	}
	storetest.Reset(t, url)
	return shared.store
}

// applyMigrations runs the repository's migrations against url.
func applyMigrations(ctx context.Context, url, suffix string) error {
	return storetest.ApplyMigrations(ctx, url, suffix)
}
