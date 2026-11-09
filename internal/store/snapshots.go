package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Snapshot is a document's CRDT state as of one sequence number. Loading the
// newest snapshot and replaying only the operations after it is much faster
// than replaying the whole history.
type Snapshot struct {
	DocID     string
	Seq       int64
	State     []byte
	CreatedAt time.Time
}

// SaveSnapshot stores the document's state as of seq. Saving the same seq
// again changes nothing: the snapshot worker reads from Kafka, which can
// deliver an event twice, and the state at one seq never changes.
func (s *Store) SaveSnapshot(ctx context.Context, docID string, seq int64, state []byte) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO snapshots (doc_id, seq, state) VALUES ($1, $2, $3)
		ON CONFLICT (doc_id, seq) DO NOTHING`, docID, seq, state)
	if err != nil {
		if hasCode(err, pgForeignKeyViolation) {
			return fmt.Errorf("%w: document %q", ErrNotFound, docID)
		}
		return fmt.Errorf("store: save snapshot of %q: %w", docID, err)
	}
	return nil
}

// LatestSnapshot returns the document's newest snapshot, or ErrNotFound if
// it has none.
func (s *Store) LatestSnapshot(ctx context.Context, docID string) (Snapshot, error) {
	return s.querySnapshot(ctx, docID, `
		SELECT seq, state, created_at FROM snapshots
		WHERE doc_id = $1
		ORDER BY seq DESC LIMIT 1`, docID)
}

// SnapshotAtOrBefore returns the newest snapshot taken at or before seq, or
// ErrNotFound. Version history uses it to rebuild an old revision.
func (s *Store) SnapshotAtOrBefore(ctx context.Context, docID string, seq int64) (Snapshot, error) {
	return s.querySnapshot(ctx, docID, `
		SELECT seq, state, created_at FROM snapshots
		WHERE doc_id = $1 AND seq <= $2
		ORDER BY seq DESC LIMIT 1`, docID, seq)
}

func (s *Store) querySnapshot(ctx context.Context, docID, sql string, args ...any) (Snapshot, error) {
	snap := Snapshot{DocID: docID}
	err := s.pool.QueryRow(ctx, sql, args...).Scan(&snap.Seq, &snap.State, &snap.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Snapshot{}, fmt.Errorf("%w: snapshot of %q", ErrNotFound, docID)
	}
	if err != nil {
		return Snapshot{}, fmt.Errorf("store: read snapshot of %q: %w", docID, err)
	}
	return snap, nil
}
