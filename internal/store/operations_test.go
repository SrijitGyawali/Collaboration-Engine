package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
)

// newOp returns an operation with a small valid payload.
func newOp(id string) NewOperation {
	return NewOperation{OpID: id, Payload: json.RawMessage(fmt.Sprintf(`{"kind":"insert","opId":%q}`, id))}
}

// ensure creates a document for a test.
func ensure(t *testing.T, s *Store, id string) {
	t.Helper()
	if _, err := s.EnsureDocument(t.Context(), id); err != nil {
		t.Fatal(err)
	}
}

// history returns the seq numbers stored for a document, in order.
func history(t *testing.T, s *Store, docID string) []int64 {
	t.Helper()
	ops, err := s.OperationsSince(t.Context(), docID, 0, MaxPage)
	if err != nil {
		t.Fatal(err)
	}
	seqs := make([]int64, len(ops))
	for i, op := range ops {
		seqs[i] = op.Seq
	}
	return seqs
}

func TestAppendAssignsSequenceNumbers(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	ensure(t, s, "doc")

	acks, err := s.AppendOperations(ctx, "doc", []NewOperation{newOp("1@a"), newOp("2@a")})
	if err != nil {
		t.Fatal(err)
	}
	want := []Ack{{OpID: "1@a", Seq: 1}, {OpID: "2@a", Seq: 2}}
	if !slices.Equal(acks, want) {
		t.Errorf("acks = %+v, want %+v", acks, want)
	}

	acks, err = s.AppendOperations(ctx, "doc", []NewOperation{newOp("3@a")})
	if err != nil {
		t.Fatal(err)
	}
	if acks[0].Seq != 3 {
		t.Errorf("next seq = %d, want 3", acks[0].Seq)
	}
}

func TestSequenceNumbersArePerDocument(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	ensure(t, s, "one")
	ensure(t, s, "two")

	if _, err := s.AppendOperations(ctx, "one", []NewOperation{newOp("1@a"), newOp("2@a")}); err != nil {
		t.Fatal(err)
	}
	// The same op ID in another document is a different operation.
	acks, err := s.AppendOperations(ctx, "two", []NewOperation{newOp("1@a")})
	if err != nil {
		t.Fatal(err)
	}
	if acks[0].Seq != 1 || acks[0].Duplicate {
		t.Errorf("ack = %+v, want seq 1 and not a duplicate", acks[0])
	}
}

// A client that lost its ack retries; it must get the original answer.
func TestRetryReturnsTheOriginalAck(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	ensure(t, s, "doc")

	if _, err := s.AppendOperations(ctx, "doc", []NewOperation{newOp("1@a"), newOp("2@a")}); err != nil {
		t.Fatal(err)
	}
	acks, err := s.AppendOperations(ctx, "doc", []NewOperation{newOp("2@a"), newOp("3@a"), newOp("1@a")})
	if err != nil {
		t.Fatal(err)
	}
	want := []Ack{
		{OpID: "2@a", Seq: 2, Duplicate: true},
		{OpID: "3@a", Seq: 3},
		{OpID: "1@a", Seq: 1, Duplicate: true},
	}
	if !slices.Equal(acks, want) {
		t.Errorf("acks = %+v, want %+v", acks, want)
	}
	if got := history(t, s, "doc"); !slices.Equal(got, []int64{1, 2, 3}) {
		t.Errorf("history = %v", got)
	}
}

func TestSameOpTwiceInOneCall(t *testing.T) {
	s := newTestStore(t)
	ensure(t, s, "doc")

	acks, err := s.AppendOperations(t.Context(), "doc", []NewOperation{newOp("1@a"), newOp("1@a")})
	if err != nil {
		t.Fatal(err)
	}
	want := []Ack{{OpID: "1@a", Seq: 1}, {OpID: "1@a", Seq: 1, Duplicate: true}}
	if !slices.Equal(acks, want) {
		t.Errorf("acks = %+v, want %+v", acks, want)
	}
}

// Many writers on one document, with no help from the sync service's actor:
// the row lock alone must keep the history dense and free of duplicates.
func TestConcurrentAppendsKeepHistoryDense(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	ensure(t, s, "doc")

	const writers, each = 8, 25
	var wg sync.WaitGroup
	errs := make([]error, writers)
	for w := range writers {
		wg.Go(func() {
			for i := range each {
				op := newOp(fmt.Sprintf("%d@w%d", i+1, w))
				if _, err := s.AppendOperations(ctx, "doc", []NewOperation{op}); err != nil {
					errs[w] = err
					return
				}
			}
		})
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		t.Fatal(err)
	}

	got := history(t, s, "doc")
	if len(got) != writers*each {
		t.Fatalf("stored %d operations, want %d", len(got), writers*each)
	}
	for i, seq := range got {
		if seq != int64(i+1) {
			t.Fatalf("history[%d] = %d: sequence numbers are not dense", i, seq)
		}
	}
}

func TestAppendToUnknownDocument(t *testing.T) {
	s := newTestStore(t)
	_, err := s.AppendOperations(t.Context(), "missing", []NewOperation{newOp("1@a")})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("AppendOperations() = %v, want ErrNotFound", err)
	}
}

func TestAppendRejectsInvalidOperations(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	ensure(t, s, "doc")

	for name, op := range map[string]NewOperation{
		"empty id":      {OpID: "", Payload: json.RawMessage(`{}`)},
		"invalid JSON":  {OpID: "1@a", Payload: json.RawMessage(`{`)},
		"empty payload": {OpID: "1@a"},
	} {
		// A valid op first: nothing from a rejected batch may be stored.
		_, err := s.AppendOperations(ctx, "doc", []NewOperation{newOp("9@a"), op})
		if !errors.Is(err, ErrInvalidOperation) {
			t.Errorf("%s: AppendOperations() = %v, want ErrInvalidOperation", name, err)
		}
	}
	if got := history(t, s, "doc"); len(got) != 0 {
		t.Errorf("history = %v, want empty", got)
	}
}

func TestAppendNothing(t *testing.T) {
	s := newTestStore(t)
	acks, err := s.AppendOperations(t.Context(), "does-not-matter", nil)
	if err != nil || acks != nil {
		t.Errorf("AppendOperations(nil) = %v, %v", acks, err)
	}
}

func TestOperationsSince(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	ensure(t, s, "doc")
	var ops []NewOperation
	for i := range 5 {
		ops = append(ops, newOp(fmt.Sprintf("%d@a", i+1)))
	}
	if _, err := s.AppendOperations(ctx, "doc", ops); err != nil {
		t.Fatal(err)
	}

	got, err := s.OperationsSince(ctx, "doc", 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Seq != 3 || got[1].Seq != 4 {
		t.Fatalf("OperationsSince(after 2, limit 2) = %+v", got)
	}
	if got[0].DocID != "doc" || got[0].OpID != "3@a" || got[0].CreatedAt.IsZero() {
		t.Errorf("operation = %+v", got[0])
	}

	var payload struct {
		Kind string `json:"kind"`
		OpID string `json:"opId"`
	}
	if err := json.Unmarshal(got[0].Payload, &payload); err != nil {
		t.Fatalf("payload %s: %v", got[0].Payload, err)
	}
	if payload.Kind != "insert" || payload.OpID != "3@a" {
		t.Errorf("payload = %+v", payload)
	}

	rest, err := s.OperationsSince(ctx, "doc", 5, 10)
	if err != nil || len(rest) != 0 {
		t.Errorf("OperationsSince(after the end) = %v, %v", rest, err)
	}
}

func TestOperationsSinceCapsThePage(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	ensure(t, s, "doc")
	ops := make([]NewOperation, MaxPage+5)
	for i := range ops {
		ops[i] = newOp(fmt.Sprintf("%d@a", i+1))
	}
	if _, err := s.AppendOperations(ctx, "doc", ops); err != nil {
		t.Fatal(err)
	}

	for _, limit := range []int{0, -1, MaxPage + 100} {
		got, err := s.OperationsSince(ctx, "doc", 0, limit)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != MaxPage {
			t.Errorf("limit %d returned %d operations, want %d", limit, len(got), MaxPage)
		}
	}
}

func TestLatestSeq(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	ensure(t, s, "doc")

	if got, err := s.LatestSeq(ctx, "doc"); err != nil || got != 0 {
		t.Errorf("LatestSeq(empty) = %d, %v", got, err)
	}
	if _, err := s.AppendOperations(ctx, "doc", []NewOperation{newOp("1@a"), newOp("2@a")}); err != nil {
		t.Fatal(err)
	}
	if got, err := s.LatestSeq(ctx, "doc"); err != nil || got != 2 {
		t.Errorf("LatestSeq = %d, %v; want 2", got, err)
	}
}
