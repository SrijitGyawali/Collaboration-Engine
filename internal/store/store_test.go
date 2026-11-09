package store

import (
	"context"
	"testing"
	"time"
)

func TestOpenAndPing(t *testing.T) {
	s := newTestStore(t)
	if err := s.Ping(t.Context()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestOpenFailsFastOnABadDatabase(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	// Nothing listens on port 1.
	if s, err := Open(ctx, "postgres://nobody:nothing@127.0.0.1:1/none?sslmode=disable"); err == nil {
		s.Close()
		t.Fatal("Open succeeded against a database that does not exist")
	}
}
