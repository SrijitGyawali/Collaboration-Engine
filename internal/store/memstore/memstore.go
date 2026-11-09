// Package memstore keeps documents and operations in memory, with the same
// behaviour as store.Store: the same errors, gap-free sequence numbers,
// retries answered with the original sequence number, and the same page cap.
//
// It is for tests, and for running the services locally without PostgreSQL.
// Everything is lost when the process exits.
package memstore

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/SrijitGyawali/Collaboration-Engine/internal/store"
)

// Store is safe for concurrent use.
type Store struct {
	mu   sync.Mutex
	docs map[string]*document
}

type document struct {
	createdAt time.Time
	ops       []store.Operation // ops[i].Seq == i+1
	seqByOpID map[string]int64
}

// New returns an empty store.
func New() *Store {
	return &Store{docs: make(map[string]*document)}
}

// EnsureDocument returns the document named id, creating it if needed.
func (s *Store) EnsureDocument(_ context.Context, id string) (store.Document, error) {
	if !store.ValidDocumentID(id) {
		return store.Document{}, fmt.Errorf("%w: %q", store.ErrInvalidDocumentID, id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.docs[id]
	if !ok {
		d = &document{createdAt: time.Now(), seqByOpID: make(map[string]int64)}
		s.docs[id] = d
	}
	return store.Document{ID: id, Title: "Untitled", CreatedAt: d.createdAt}, nil
}

// AppendOperations stores ops at the end of the document's history; see
// store.Store.AppendOperations.
func (s *Store) AppendOperations(_ context.Context, docID string, ops []store.NewOperation) ([]store.Ack, error) {
	if len(ops) == 0 {
		return nil, nil
	}
	for _, op := range ops {
		if op.OpID == "" {
			return nil, fmt.Errorf("%w: empty op id", store.ErrInvalidOperation)
		}
		if !json.Valid(op.Payload) {
			return nil, fmt.Errorf("%w: payload of %s is not valid JSON", store.ErrInvalidOperation, op.OpID)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.docs[docID]
	if !ok {
		return nil, fmt.Errorf("%w: document %q", store.ErrNotFound, docID)
	}

	acks := make([]store.Ack, len(ops))
	for i, op := range ops {
		if seq, ok := d.seqByOpID[op.OpID]; ok {
			acks[i] = store.Ack{OpID: op.OpID, Seq: seq, Duplicate: true}
			continue
		}
		seq := int64(len(d.ops) + 1)
		d.ops = append(d.ops, store.Operation{
			DocID:     docID,
			Seq:       seq,
			OpID:      op.OpID,
			Payload:   slices.Clone(op.Payload), // the caller may reuse its buffer
			CreatedAt: time.Now(),
		})
		d.seqByOpID[op.OpID] = seq
		acks[i] = store.Ack{OpID: op.OpID, Seq: seq}
	}
	return acks, nil
}

// OperationsSince returns operations after afterSeq, oldest first, at most
// limit of them (and never more than store.MaxPage).
func (s *Store) OperationsSince(_ context.Context, docID string, afterSeq int64, limit int) ([]store.Operation, error) {
	if limit <= 0 || limit > store.MaxPage {
		limit = store.MaxPage
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.docs[docID]
	if !ok || afterSeq >= int64(len(d.ops)) {
		return []store.Operation{}, nil
	}
	start := max(afterSeq, 0)
	end := min(start+int64(limit), int64(len(d.ops)))
	return slices.Clone(d.ops[start:end]), nil
}

// LatestSeq returns the document's highest sequence number, or 0.
func (s *Store) LatestSeq(_ context.Context, docID string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.docs[docID]; ok {
		return int64(len(d.ops)), nil
	}
	return 0, nil
}
