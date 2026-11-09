package memstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/SrijitGyawali/Collaboration-Engine/internal/store"
)

func op(id string) store.NewOperation {
	return store.NewOperation{OpID: id, Payload: json.RawMessage(`{"kind":"insert"}`)}
}

func TestAppendAndRetry(t *testing.T) {
	s := New()
	ctx := t.Context()
	if _, err := s.EnsureDocument(ctx, "doc"); err != nil {
		t.Fatal(err)
	}

	if _, err := s.AppendOperations(ctx, "doc", []store.NewOperation{op("1@a"), op("2@a")}); err != nil {
		t.Fatal(err)
	}
	acks, err := s.AppendOperations(ctx, "doc", []store.NewOperation{op("2@a"), op("3@a"), op("3@a")})
	if err != nil {
		t.Fatal(err)
	}
	want := []store.Ack{
		{OpID: "2@a", Seq: 2, Duplicate: true},
		{OpID: "3@a", Seq: 3},
		{OpID: "3@a", Seq: 3, Duplicate: true},
	}
	if !slices.Equal(acks, want) {
		t.Errorf("acks = %+v, want %+v", acks, want)
	}
	if seq, _ := s.LatestSeq(ctx, "doc"); seq != 3 {
		t.Errorf("LatestSeq = %d, want 3", seq)
	}
}

func TestErrorsMatchTheRealStore(t *testing.T) {
	s := New()
	ctx := t.Context()

	if _, err := s.EnsureDocument(ctx, "has space"); !errors.Is(err, store.ErrInvalidDocumentID) {
		t.Errorf("EnsureDocument(invalid) = %v", err)
	}
	if _, err := s.AppendOperations(ctx, "missing", []store.NewOperation{op("1@a")}); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("AppendOperations(missing) = %v", err)
	}
	if _, err := s.EnsureDocument(ctx, "doc"); err != nil {
		t.Fatal(err)
	}
	bad := store.NewOperation{OpID: "1@a", Payload: json.RawMessage(`{`)}
	if _, err := s.AppendOperations(ctx, "doc", []store.NewOperation{op("9@a"), bad}); !errors.Is(err, store.ErrInvalidOperation) {
		t.Errorf("AppendOperations(invalid) = %v", err)
	}
	if seq, _ := s.LatestSeq(ctx, "doc"); seq != 0 {
		t.Errorf("a rejected batch stored %d ops", seq)
	}
}

func TestOperationsSincePages(t *testing.T) {
	s := New()
	ctx := t.Context()
	if _, err := s.EnsureDocument(ctx, "doc"); err != nil {
		t.Fatal(err)
	}
	var ops []store.NewOperation
	for i := range store.MaxPage + 3 {
		ops = append(ops, op(fmt.Sprintf("%d@a", i+1)))
	}
	if _, err := s.AppendOperations(ctx, "doc", ops); err != nil {
		t.Fatal(err)
	}

	got, err := s.OperationsSince(ctx, "doc", 2, 2)
	if err != nil || len(got) != 2 || got[0].Seq != 3 || got[1].Seq != 4 {
		t.Fatalf("OperationsSince(2, 2) = %+v, %v", got, err)
	}
	if got, _ := s.OperationsSince(ctx, "doc", 0, 0); len(got) != store.MaxPage {
		t.Errorf("page = %d ops, want the cap %d", len(got), store.MaxPage)
	}
	if got, _ := s.OperationsSince(ctx, "doc", int64(store.MaxPage+3), 10); len(got) != 0 {
		t.Errorf("past the end = %d ops", len(got))
	}
	if got, _ := s.OperationsSince(ctx, "missing", 0, 10); got == nil || len(got) != 0 {
		t.Errorf("unknown document = %v, want an empty slice", got)
	}
}

func TestConcurrentAppends(t *testing.T) {
	s := New()
	ctx := t.Context()
	if _, err := s.EnsureDocument(ctx, "doc"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for w := range 8 {
		wg.Go(func() {
			for i := range 50 {
				if _, err := s.AppendOperations(ctx, "doc", []store.NewOperation{op(fmt.Sprintf("%d@w%d", i, w))}); err != nil {
					t.Error(err)
				}
			}
		})
	}
	wg.Wait()

	all, _ := s.OperationsSince(ctx, "doc", 0, store.MaxPage)
	for i, o := range all {
		if o.Seq != int64(i+1) {
			t.Fatalf("ops[%d].Seq = %d: not dense", i, o.Seq)
		}
	}
	if len(all) != 400 {
		t.Errorf("stored %d ops, want 400", len(all))
	}
}
