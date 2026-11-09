package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

func TestAppendWritesOneEventPerNewOperation(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	ensure(t, s, "doc")

	if _, err := s.AppendOperations(ctx, "doc", []NewOperation{newOp("1@a"), newOp("2@a")}); err != nil {
		t.Fatal(err)
	}

	events, err := s.UnpublishedEvents(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	e := events[1]
	if e.Topic != TopicDocEvents || e.Key != "doc" || e.CreatedAt.IsZero() {
		t.Errorf("event = %+v", e)
	}

	var payload struct {
		DocID string          `json:"doc_id"`
		Seq   int64           `json:"seq"`
		OpID  string          `json:"op_id"`
		Op    json.RawMessage `json:"op"`
	}
	if err := json.Unmarshal(e.Payload, &payload); err != nil {
		t.Fatalf("payload %s: %v", e.Payload, err)
	}
	if payload.DocID != "doc" || payload.Seq != 2 || payload.OpID != "2@a" || len(payload.Op) == 0 {
		t.Errorf("payload = %+v", payload)
	}
}

// A retried operation must not be published twice.
func TestRetryWritesNoNewEvents(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	ensure(t, s, "doc")

	for range 3 {
		if _, err := s.AppendOperations(ctx, "doc", []NewOperation{newOp("1@a")}); err != nil {
			t.Fatal(err)
		}
	}

	events, err := s.UnpublishedEvents(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Errorf("got %d events, want 1", len(events))
	}
}

// The reason the outbox exists: an operation and its event are saved together
// or not at all. A trigger makes the outbox insert fail, after the operation
// was inserted in the same transaction.
func TestOperationAndEventCommitTogether(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	ensure(t, s, "doc")

	exec := func(sql string) {
		t.Helper()
		if _, err := s.pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE FUNCTION fail_outbox() RETURNS trigger AS $$
	      BEGIN RAISE EXCEPTION 'outbox is broken'; END; $$ LANGUAGE plpgsql`)
	exec(`CREATE TRIGGER fail_outbox BEFORE INSERT ON outbox FOR EACH ROW EXECUTE FUNCTION fail_outbox()`)
	t.Cleanup(func() {
		// t.Context is already cancelled when cleanups run.
		ctx := context.Background()
		_, _ = s.pool.Exec(ctx, `DROP TRIGGER IF EXISTS fail_outbox ON outbox`)
		_, _ = s.pool.Exec(ctx, `DROP FUNCTION IF EXISTS fail_outbox`)
	})

	if _, err := s.AppendOperations(ctx, "doc", []NewOperation{newOp("1@a")}); err == nil {
		t.Fatal("AppendOperations succeeded with a broken outbox")
	}
	if got := history(t, s, "doc"); len(got) != 0 {
		t.Errorf("operation was stored without its event: %v", got)
	}

	// Once the outbox works again, the retry succeeds and history has no gap.
	exec(`DROP TRIGGER fail_outbox ON outbox`)
	acks, err := s.AppendOperations(ctx, "doc", []NewOperation{newOp("1@a")})
	if err != nil {
		t.Fatal(err)
	}
	if acks[0].Seq != 1 || acks[0].Duplicate {
		t.Errorf("ack after recovery = %+v, want seq 1 and not a duplicate", acks[0])
	}
}

func TestUnpublishedEventsOrderAndLimit(t *testing.T) {
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

	events, err := s.UnpublishedEvents(ctx, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3", len(events))
	}
	for i := 1; i < len(events); i++ {
		if events[i].ID <= events[i-1].ID {
			t.Errorf("events out of order: %d after %d", events[i].ID, events[i-1].ID)
		}
	}
}

func TestMarkPublished(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	ensure(t, s, "doc")
	if _, err := s.AppendOperations(ctx, "doc", []NewOperation{newOp("1@a"), newOp("2@a")}); err != nil {
		t.Fatal(err)
	}
	events, err := s.UnpublishedEvents(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}

	// Marking twice is harmless, as after a relay retry.
	for range 2 {
		if err := s.MarkPublished(ctx, []int64{events[0].ID}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.MarkPublished(ctx, nil); err != nil {
		t.Fatal(err)
	}

	left, err := s.UnpublishedEvents(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 1 || left[0].ID != events[1].ID {
		t.Errorf("unpublished = %+v, want only event %d", left, events[1].ID)
	}
}

func TestFailedAppendWritesNoEvents(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()

	_, err := s.AppendOperations(ctx, "missing", []NewOperation{newOp("1@a")})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("AppendOperations() = %v, want ErrNotFound", err)
	}
	events, err := s.UnpublishedEvents(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Errorf("got %d events for a failed append", len(events))
	}
}
