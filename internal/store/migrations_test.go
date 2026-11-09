package store

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// TestMigrationsUpDownUp runs every migration forward, back and forward again
// on a separate database. A down migration that nobody runs tends not to work
// on the day it is needed.
func TestMigrationsUpDownUp(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()

	if _, err := s.pool.Exec(ctx, `DROP DATABASE IF EXISTS migrations_check`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `CREATE DATABASE migrations_check`); err != nil {
		t.Fatal(err)
	}
	url := strings.Replace(shared.url, "/collab_test?", "/migrations_check?", 1)

	for _, suffix := range []string{".up.sql", ".down.sql", ".up.sql"} {
		if err := applyMigrations(ctx, url, suffix); err != nil {
			t.Fatalf("applying %s migrations: %v", suffix, err)
		}
	}

	// The final schema accepts a named document, which 000001 alone did not.
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	if _, err := conn.Exec(ctx, `INSERT INTO documents (id) VALUES ('demo')`); err != nil {
		t.Errorf("insert named document: %v", err)
	}
}
