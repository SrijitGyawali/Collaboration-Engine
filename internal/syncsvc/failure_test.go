package syncsvc

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/SrijitGyawali/Collaboration-Engine/internal/store"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/store/memstore"
)

// flakyStore is a memstore whose appends fail while broken is set, like a
// database that is briefly unreachable.
type flakyStore struct {
	*memstore.Store
	broken atomic.Bool
}

func (f *flakyStore) AppendOperations(ctx context.Context, docID string, ops []store.NewOperation) ([]store.Ack, error) {
	if f.broken.Load() {
		return nil, errors.New("database unreachable")
	}
	return f.Store.AppendOperations(ctx, docID, ops)
}

// If storing fails, the CRDT already holds the ops. The service must not keep
// believing in them: they were never saved, so other replicas will never get
// them, and a retry must be treated as new rather than as a duplicate.
func TestStorageFailureDoesNotLeaveUnsavedEdits(t *testing.T) {
	st := &flakyStore{Store: memstore.New()}
	s := newService(t, st, Options{})
	ops := typed("alice", "ab")

	if _, err := s.Apply(t.Context(), "doc", ops[:1]); err != nil {
		t.Fatal(err)
	}

	st.broken.Store(true)
	if _, err := s.Apply(t.Context(), "doc", ops[1:]); err == nil {
		t.Fatal("Apply succeeded while storage was down")
	}

	st.broken.Store(false)
	if text, _ := s.Text(t.Context(), "doc"); text != "a" {
		t.Errorf("Text after the failure = %q, want only the saved %q", text, "a")
	}
	acks, err := s.Apply(t.Context(), "doc", ops[1:])
	if err != nil {
		t.Fatal(err)
	}
	if acks[0].Seq != 2 || acks[0].Duplicate {
		t.Errorf("retry = %+v, want seq 2 as a new op", acks[0])
	}
}
