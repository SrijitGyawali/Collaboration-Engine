package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Document is a row of the documents table.
type Document struct {
	ID        string
	Title     string
	CreatedAt time.Time
}

// EnsureDocument returns the document named id, creating it if it does not
// exist. Opening /editor?doc=name is what brings a document into being, so
// two people opening a new name at the same moment must both succeed and get
// the same document.
func (s *Store) EnsureDocument(ctx context.Context, id string) (Document, error) {
	_, err := s.pool.Exec(ctx, `INSERT INTO documents (id) VALUES ($1) ON CONFLICT (id) DO NOTHING`, id)
	if err != nil {
		if hasCode(err, pgCheckViolation) {
			return Document{}, fmt.Errorf("%w: %q", ErrInvalidDocumentID, id)
		}
		return Document{}, fmt.Errorf("store: ensure document %q: %w", id, err)
	}
	return s.Document(ctx, id)
}

// Document returns the document named id, or ErrNotFound.
func (s *Store) Document(ctx context.Context, id string) (Document, error) {
	d := Document{ID: id}
	err := s.pool.QueryRow(ctx, `SELECT title, created_at FROM documents WHERE id = $1`, id).Scan(&d.Title, &d.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, fmt.Errorf("%w: document %q", ErrNotFound, id)
	}
	if err != nil {
		return Document{}, fmt.Errorf("store: get document %q: %w", id, err)
	}
	return d, nil
}
