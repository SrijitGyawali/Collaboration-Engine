package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgreSQL error codes this package reacts to.
const (
	pgForeignKeyViolation = "23503"
	pgCheckViolation      = "23514"
)

var (
	// ErrNotFound is returned when a document or snapshot does not exist.
	ErrNotFound = errors.New("store: not found")
	// ErrInvalidDocumentID is returned for a name the schema does not allow:
	// 1 to 40 letters, digits, "_" or "-".
	ErrInvalidDocumentID = errors.New("store: invalid document id")
	// ErrInvalidOperation is returned for an operation that cannot be stored,
	// such as one with no op ID or a payload that is not valid JSON.
	ErrInvalidOperation = errors.New("store: invalid operation")
)

// Store is the data layer. It is safe for concurrent use.
type Store struct {
	pool *pgxpool.Pool
}

// Open connects to PostgreSQL at url (see DATABASE_URL in .env.example) and
// checks that the database answers, so a bad URL fails at start-up rather than
// on the first edit.
func Open(ctx context.Context, url string) (*Store, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("store: connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("store: ping: %w", err)
	}
	return &Store{pool: pool}, nil
}

// Close releases every connection. The Store cannot be used afterwards.
func (s *Store) Close() {
	s.pool.Close()
}

// Ping checks that the database answers; used by readiness checks.
func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// hasCode reports whether err is a PostgreSQL error with the given code.
func hasCode(err error, code string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == code
}
