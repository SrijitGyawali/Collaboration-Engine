package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// MaxPage is the most operations OperationsSince returns in one call. A client
// catching up on a long history pages through it.
const MaxPage = 1000

// NewOperation is an operation to store.
type NewOperation struct {
	// OpID identifies the operation ("counter@replica"). Storing the same
	// OpID again is a retry and changes nothing.
	OpID string
	// Payload is the operation in the wire format (see internal/wire).
	Payload json.RawMessage
}

// Ack says what happened to one operation passed to AppendOperations.
type Ack struct {
	OpID string
	// Seq is the operation's position in the document's history.
	Seq int64
	// Duplicate is true if the operation was already stored. Seq is then the
	// number it was given the first time, so a client that retries because
	// it lost the ack receives the same answer.
	Duplicate bool
}

// Operation is a stored operation.
type Operation struct {
	DocID string
	Seq   int64
	OpID  string
	// Payload is equivalent JSON to what was stored, but not byte for byte:
	// PostgreSQL's jsonb does not keep key order or whitespace.
	Payload   json.RawMessage
	CreatedAt time.Time
}

// AppendOperations stores ops at the end of the document's history, in one
// transaction, and returns one Ack per op, in order. The document must exist
// (see EnsureDocument), or it returns ErrNotFound. It is safe to retry: ops
// already stored are reported as duplicates and not written again.
func (s *Store) AppendOperations(ctx context.Context, docID string, ops []NewOperation) ([]Ack, error) {
	if len(ops) == 0 {
		return nil, nil
	}
	for _, op := range ops {
		if op.OpID == "" {
			return nil, fmt.Errorf("%w: empty op id", ErrInvalidOperation)
		}
		if !json.Valid(op.Payload) {
			return nil, fmt.Errorf("%w: payload of %s is not valid JSON", ErrInvalidOperation, op.OpID)
		}
	}

	var acks []Ack
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		acks, err = appendOperations(ctx, tx, docID, ops)
		return err
	})
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, err
		}
		return nil, fmt.Errorf("store: append to %q: %w", docID, err)
	}
	return acks, nil
}

func appendOperations(ctx context.Context, tx pgx.Tx, docID string, ops []NewOperation) ([]Ack, error) {
	// Lock the document's row until commit. Writers to one document then go
	// strictly one at a time, so MAX(seq) below cannot be read by two of
	// them at once, while writers to other documents are not affected.
	var locked string
	err := tx.QueryRow(ctx, `SELECT id FROM documents WHERE id = $1 FOR UPDATE`, docID).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w: document %q", ErrNotFound, docID)
	}
	if err != nil {
		return nil, err
	}

	known, err := storedSeqs(ctx, tx, docID, ops)
	if err != nil {
		return nil, err
	}

	var seq int64
	err = tx.QueryRow(ctx, `SELECT COALESCE(MAX(seq), 0) FROM operations WHERE doc_id = $1`, docID).Scan(&seq)
	if err != nil {
		return nil, err
	}

	// Numbers are taken from MAX(seq) inside the lock rather than from a
	// database sequence, which would leave gaps after a rollback. A client
	// treats a gap as a missed operation, so history must have none.
	acks := make([]Ack, len(ops))
	batch := &pgx.Batch{}
	for i, op := range ops {
		if existing, ok := known[op.OpID]; ok {
			acks[i] = Ack{OpID: op.OpID, Seq: existing, Duplicate: true}
			continue
		}
		seq++
		known[op.OpID] = seq // the same op may appear twice in one call
		acks[i] = Ack{OpID: op.OpID, Seq: seq}
		batch.Queue(`INSERT INTO operations (doc_id, seq, op_id, payload) VALUES ($1, $2, $3, $4)`,
			docID, seq, op.OpID, op.Payload)
	}
	if batch.Len() > 0 {
		if err := tx.SendBatch(ctx, batch).Close(); err != nil {
			return nil, err
		}
	}
	return acks, nil
}

// storedSeqs returns the sequence numbers of those ops already stored.
func storedSeqs(ctx context.Context, tx pgx.Tx, docID string, ops []NewOperation) (map[string]int64, error) {
	ids := make([]string, len(ops))
	for i, op := range ops {
		ids[i] = op.OpID
	}
	rows, err := tx.Query(ctx, `SELECT op_id, seq FROM operations WHERE doc_id = $1 AND op_id = ANY($2)`, docID, ids)
	if err != nil {
		return nil, err
	}

	known := make(map[string]int64, len(ops))
	var id string
	var seq int64
	_, err = pgx.ForEachRow(rows, []any{&id, &seq}, func() error {
		known[id] = seq
		return nil
	})
	return known, err
}

// OperationsSince returns the document's operations after sequence number
// afterSeq, oldest first, at most limit of them (and never more than MaxPage).
// A client reconnecting with the last seq it saw gets exactly what it missed.
func (s *Store) OperationsSince(ctx context.Context, docID string, afterSeq int64, limit int) ([]Operation, error) {
	if limit <= 0 || limit > MaxPage {
		limit = MaxPage
	}
	rows, err := s.pool.Query(ctx, `
		SELECT seq, op_id, payload, created_at
		FROM operations
		WHERE doc_id = $1 AND seq > $2
		ORDER BY seq
		LIMIT $3`, docID, afterSeq, limit)
	if err != nil {
		return nil, fmt.Errorf("store: read %q: %w", docID, err)
	}

	ops, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Operation, error) {
		op := Operation{DocID: docID}
		err := row.Scan(&op.Seq, &op.OpID, &op.Payload, &op.CreatedAt)
		return op, err
	})
	if err != nil {
		return nil, fmt.Errorf("store: read %q: %w", docID, err)
	}
	return ops, nil
}

// LatestSeq returns the document's highest sequence number, or 0 if it has
// no operations.
func (s *Store) LatestSeq(ctx context.Context, docID string) (int64, error) {
	var seq int64
	err := s.pool.QueryRow(ctx, `SELECT COALESCE(MAX(seq), 0) FROM operations WHERE doc_id = $1`, docID).Scan(&seq)
	if err != nil {
		return 0, fmt.Errorf("store: latest seq of %q: %w", docID, err)
	}
	return seq, nil
}
