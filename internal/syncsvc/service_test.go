package syncsvc

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SrijitGyawali/Collaboration-Engine/internal/crdt"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/store"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/store/memstore"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/wire"
)

func quietLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newService(t *testing.T, st Store, opts Options) *Service {
	t.Helper()
	s := New(st, quietLog(), opts)
	t.Cleanup(s.Close)
	return s
}

// typed returns the wire ops for text typed by replica, as a client would
// send them.
func typed(replica, text string) []wire.Op {
	doc := crdt.NewDocument(replica)
	ops, err := doc.InsertText(0, text)
	if err != nil {
		panic(err)
	}
	out := make([]wire.Op, len(ops))
	for i, op := range ops {
		out[i] = wire.FromCRDT(op)
	}
	return out
}

func TestApplyStoresAndAcks(t *testing.T) {
	s := newService(t, memstore.New(), Options{})
	ctx := t.Context()

	acks, err := s.Apply(ctx, "doc", typed("alice", "hi"))
	if err != nil {
		t.Fatal(err)
	}
	if len(acks) != 2 || acks[0] != (Ack{OpID: "1@alice", Seq: 1}) || acks[1] != (Ack{OpID: "2@alice", Seq: 2}) {
		t.Errorf("acks = %+v", acks)
	}
	if text, _ := s.Text(ctx, "doc"); text != "hi" {
		t.Errorf("Text = %q, want %q", text, "hi")
	}

	ops, err := s.OperationsSince(ctx, "doc", 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 1 || ops[0].Seq != 2 || ops[0].Op.Value != "i" {
		t.Errorf("OperationsSince(1) = %+v", ops)
	}
}

func TestRetryGetsTheOriginalSeq(t *testing.T) {
	s := newService(t, memstore.New(), Options{})
	ops := typed("alice", "abc")

	if _, err := s.Apply(t.Context(), "doc", ops[:2]); err != nil {
		t.Fatal(err)
	}
	acks, err := s.Apply(t.Context(), "doc", ops)
	if err != nil {
		t.Fatal(err)
	}
	want := []Ack{{OpID: "1@alice", Seq: 1, Duplicate: true}, {OpID: "2@alice", Seq: 2, Duplicate: true}, {OpID: "3@alice", Seq: 3}}
	for i := range want {
		if acks[i] != want[i] {
			t.Errorf("acks[%d] = %+v, want %+v", i, acks[i], want[i])
		}
	}
}

// Invalid ops are rejected one by one; the valid ones in the batch go through.
func TestInvalidOpsAreRejectedIndividually(t *testing.T) {
	s := newService(t, memstore.New(), Options{})
	good := typed("alice", "a")[0]
	malformed := wire.Op{Kind: "insert", ID: wire.ID{Counter: 5, Replica: "bob"}, Value: "too long"}
	conflict := good
	conflict.Value = "z" // reuses 1@alice for a different character

	acks, err := s.Apply(t.Context(), "doc", []wire.Op{good, malformed})
	if err != nil {
		t.Fatal(err)
	}
	if acks[0].Seq != 1 || acks[0].Err != "" {
		t.Errorf("good op: %+v", acks[0])
	}
	if acks[1].Seq != 0 || !strings.Contains(acks[1].Err, "malformed") {
		t.Errorf("malformed op: %+v", acks[1])
	}

	acks, err = s.Apply(t.Context(), "doc", []wire.Op{conflict})
	if err != nil {
		t.Fatal(err)
	}
	if acks[0].Seq != 0 || !strings.Contains(acks[0].Err, "conflicting") {
		t.Errorf("conflicting op: %+v", acks[0])
	}
	if text, _ := s.Text(t.Context(), "doc"); text != "a" {
		t.Errorf("Text = %q, want %q", text, "a")
	}
}

// An op that arrives before its parent is valid: it is stored and acked.
func TestEarlyOpsAreStored(t *testing.T) {
	s := newService(t, memstore.New(), Options{})
	ops := typed("alice", "ab")

	acks, err := s.Apply(t.Context(), "doc", []wire.Op{ops[1]})
	if err != nil {
		t.Fatal(err)
	}
	if acks[0].Seq != 1 {
		t.Errorf("early op: %+v", acks[0])
	}
	if _, err := s.Apply(t.Context(), "doc", []wire.Op{ops[0]}); err != nil {
		t.Fatal(err)
	}
	if text, _ := s.Text(t.Context(), "doc"); text != "ab" {
		t.Errorf("Text = %q, want %q", text, "ab")
	}
}

func TestConcurrentWritersOnOneDocument(t *testing.T) {
	s := newService(t, memstore.New(), Options{})
	var wg sync.WaitGroup
	for w := range 10 {
		wg.Go(func() {
			if _, err := s.Apply(t.Context(), "doc", typed(fmt.Sprintf("w%d", w), "abcde")); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()

	text, err := s.Text(t.Context(), "doc")
	if err != nil {
		t.Fatal(err)
	}
	if len(text) != 50 {
		t.Errorf("Text has %d characters, want 50", len(text))
	}
	ops, _ := s.OperationsSince(t.Context(), "doc", 0, 100)
	for i, op := range ops {
		if op.Seq != int64(i+1) {
			t.Fatalf("history not dense at %d: seq %d", i, op.Seq)
		}
	}
}

func TestDocumentsAreIndependent(t *testing.T) {
	s := newService(t, memstore.New(), Options{})
	if _, err := s.Apply(t.Context(), "one", typed("a", "x")); err != nil {
		t.Fatal(err)
	}
	acks, err := s.Apply(t.Context(), "two", typed("a", "y"))
	if err != nil {
		t.Fatal(err)
	}
	if acks[0].Seq != 1 || acks[0].Duplicate {
		t.Errorf("same op id in another document: %+v", acks[0])
	}
}

func TestRejectsBadRequests(t *testing.T) {
	s := newService(t, memstore.New(), Options{})

	if _, err := s.Apply(t.Context(), "has space", typed("a", "x")); !errors.Is(err, store.ErrInvalidDocumentID) {
		t.Errorf("invalid document: %v", err)
	}
	if _, err := s.Apply(t.Context(), "doc", make([]wire.Op, MaxBatch+1)); !errors.Is(err, ErrBatchTooLarge) {
		t.Errorf("huge batch: %v", err)
	}
	if acks, err := s.Apply(t.Context(), "doc", nil); acks != nil || err != nil {
		t.Errorf("empty batch: %v, %v", acks, err)
	}
}

func TestIdleDocumentsAreUnloadedAndReloaded(t *testing.T) {
	st := memstore.New()
	s := newService(t, st, Options{IdleTimeout: 20 * time.Millisecond})
	if _, err := s.Apply(t.Context(), "doc", typed("alice", "hello")); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for s.loadedDocuments() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("idle document was never unloaded")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Reloaded from storage on the next request, history intact.
	if text, _ := s.Text(t.Context(), "doc"); text != "hello" {
		t.Errorf("Text after reload = %q", text)
	}
	acks, err := s.Apply(t.Context(), "doc", typed("alice", "hello")[:1])
	if err != nil || !acks[0].Duplicate || acks[0].Seq != 1 {
		t.Errorf("retry after reload = %+v, %v", acks, err)
	}
}

func TestClose(t *testing.T) {
	s := New(memstore.New(), quietLog(), Options{})
	if _, err := s.Apply(t.Context(), "doc", typed("a", "x")); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s.Close() // safe to repeat

	if _, err := s.Apply(t.Context(), "doc", typed("b", "y")); !errors.Is(err, ErrClosed) {
		t.Errorf("Apply after Close = %v, want ErrClosed", err)
	}
}
