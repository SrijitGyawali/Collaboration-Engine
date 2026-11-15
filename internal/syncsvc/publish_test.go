package syncsvc

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/SrijitGyawali/Collaboration-Engine/internal/pubsub"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/store/memstore"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/wire"
)

// recorder is a Publisher that keeps what it was given, and can fail.
type recorder struct {
	mu   sync.Mutex
	msgs []pubsub.Message
	fail bool
}

func (r *recorder) Publish(_ context.Context, msg pubsub.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail {
		return errors.New("redis unreachable")
	}
	r.msgs = append(r.msgs, msg)
	return nil
}

func TestStoredOpsArePublished(t *testing.T) {
	rec := &recorder{}
	s := newService(t, memstore.New(), Options{Publisher: rec})
	ops := typed("alice", "abc")

	if _, err := s.Apply(t.Context(), "doc", ops[:2]); err != nil {
		t.Fatal(err)
	}
	// A retry of the first two plus one new op: only the new one is news.
	if _, err := s.Apply(t.Context(), "doc", ops); err != nil {
		t.Fatal(err)
	}

	if len(rec.msgs) != 2 {
		t.Fatalf("published %d messages, want 2", len(rec.msgs))
	}
	first, second := rec.msgs[0], rec.msgs[1]
	if first.DocID != "doc" || first.Author != "alice" || len(first.Ops) != 2 || first.Ops[0].Seq != 1 || first.Ops[1].Seq != 2 {
		t.Errorf("first message = %+v", first)
	}
	if len(second.Ops) != 1 || second.Ops[0].Seq != 3 || second.Ops[0].Op.Value != "c" {
		t.Errorf("second message = %+v", second)
	}
}

// Ops from different authors in one batch go out as separate messages, so
// each gateway can skip the right author.
func TestOneMessagePerAuthor(t *testing.T) {
	rec := &recorder{}
	s := newService(t, memstore.New(), Options{Publisher: rec})
	batch := append(typed("alice", "a"), typed("bob", "b")...)

	if _, err := s.Apply(t.Context(), "doc", batch); err != nil {
		t.Fatal(err)
	}
	if len(rec.msgs) != 2 || rec.msgs[0].Author != "alice" || rec.msgs[1].Author != "bob" {
		t.Errorf("messages = %+v", rec.msgs)
	}
}

func TestRejectedOpsAreNotPublished(t *testing.T) {
	rec := &recorder{}
	s := newService(t, memstore.New(), Options{Publisher: rec})
	bad := wire.Op{Kind: "insert", ID: wire.ID{Counter: 1, Replica: "alice"}, Value: "too long"}

	if _, err := s.Apply(t.Context(), "doc", []wire.Op{bad}); err != nil {
		t.Fatal(err)
	}
	if len(rec.msgs) != 0 {
		t.Errorf("published %+v for a rejected op", rec.msgs)
	}
}

// Fan-out is best effort: the edit is stored, so a publish failure must not
// turn into an error for the client.
func TestPublishFailureDoesNotFailTheEdit(t *testing.T) {
	rec := &recorder{fail: true}
	s := newService(t, memstore.New(), Options{Publisher: rec})

	acks, err := s.Apply(t.Context(), "doc", typed("alice", "a"))
	if err != nil || acks[0].Seq != 1 {
		t.Errorf("Apply = %+v, %v", acks, err)
	}
}
