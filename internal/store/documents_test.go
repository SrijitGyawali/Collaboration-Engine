package store

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestEnsureDocumentCreatesItOnce(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()

	first, err := s.EnsureDocument(ctx, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != "demo" || first.Title != "Untitled" || first.CreatedAt.IsZero() {
		t.Errorf("new document = %+v", first)
	}

	second, err := s.EnsureDocument(ctx, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if !second.CreatedAt.Equal(first.CreatedAt) {
		t.Errorf("second call made a new document: %v then %v", first.CreatedAt, second.CreatedAt)
	}
}

// Two people opening a new document name at the same moment.
func TestEnsureDocumentConcurrently(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()

	var wg sync.WaitGroup
	errs := make([]error, 20)
	for i := range errs {
		wg.Go(func() {
			_, errs[i] = s.EnsureDocument(ctx, "race")
		})
	}
	wg.Wait()

	for _, err := range errs {
		if err != nil {
			t.Errorf("EnsureDocument: %v", err)
		}
	}
}

// TestDocumentNotFound checks the error callers branch on.
func TestDocumentNotFound(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Document(t.Context(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Document() = %v, want ErrNotFound", err)
	}
}

func TestEnsureDocumentRejectsInvalidNames(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()

	for _, id := range []string{"", "has space", "a/b", strings.Repeat("x", 41)} {
		if _, err := s.EnsureDocument(ctx, id); !errors.Is(err, ErrInvalidDocumentID) {
			t.Errorf("EnsureDocument(%q) = %v, want ErrInvalidDocumentID", id, err)
		}
	}
	for _, id := range []string{"a", "my_doc-1", strings.Repeat("x", 40), "550e8400-e29b-41d4-a716-446655440000"} {
		if _, err := s.EnsureDocument(ctx, id); err != nil {
			t.Errorf("EnsureDocument(%q): %v", id, err)
		}
	}
}
