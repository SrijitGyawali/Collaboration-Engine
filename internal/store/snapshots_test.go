package store

import (
	"errors"
	"testing"
)

func TestSnapshots(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	ensure(t, s, "doc")

	if _, err := s.LatestSnapshot(ctx, "doc"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("LatestSnapshot(none) = %v, want ErrNotFound", err)
	}

	for seq, state := range map[int64]string{10: "ten", 20: "twenty", 30: "thirty"} {
		if err := s.SaveSnapshot(ctx, "doc", seq, []byte(state)); err != nil {
			t.Fatal(err)
		}
	}

	latest, err := s.LatestSnapshot(ctx, "doc")
	if err != nil {
		t.Fatal(err)
	}
	if latest.DocID != "doc" || latest.Seq != 30 || string(latest.State) != "thirty" || latest.CreatedAt.IsZero() {
		t.Errorf("latest = %+v", latest)
	}

	for _, tt := range []struct {
		at, wantSeq int64
	}{{10, 10}, {25, 20}, {99, 30}} {
		got, err := s.SnapshotAtOrBefore(ctx, "doc", tt.at)
		if err != nil {
			t.Fatalf("SnapshotAtOrBefore(%d): %v", tt.at, err)
		}
		if got.Seq != tt.wantSeq {
			t.Errorf("SnapshotAtOrBefore(%d).Seq = %d, want %d", tt.at, got.Seq, tt.wantSeq)
		}
	}
	if _, err := s.SnapshotAtOrBefore(ctx, "doc", 5); !errors.Is(err, ErrNotFound) {
		t.Errorf("SnapshotAtOrBefore(5) = %v, want ErrNotFound", err)
	}
}

// The worker can receive the same event twice; the first snapshot stands.
func TestSaveSnapshotIsIdempotent(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	ensure(t, s, "doc")

	for _, state := range []string{"first", "second"} {
		if err := s.SaveSnapshot(ctx, "doc", 1, []byte(state)); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.LatestSnapshot(ctx, "doc")
	if err != nil {
		t.Fatal(err)
	}
	if string(got.State) != "first" {
		t.Errorf("State = %q, want the first one saved", got.State)
	}
}

func TestSaveSnapshotOfUnknownDocument(t *testing.T) {
	s := newTestStore(t)
	err := s.SaveSnapshot(t.Context(), "missing", 1, []byte("x"))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("SaveSnapshot() = %v, want ErrNotFound", err)
	}
}

func TestDeletingADocumentRemovesItsData(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	ensure(t, s, "doc")
	if _, err := s.AppendOperations(ctx, "doc", []NewOperation{newOp("1@a")}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSnapshot(ctx, "doc", 1, []byte("x")); err != nil {
		t.Fatal(err)
	}

	if _, err := s.pool.Exec(ctx, `DELETE FROM documents WHERE id = 'doc'`); err != nil {
		t.Fatal(err)
	}

	if got := history(t, s, "doc"); len(got) != 0 {
		t.Errorf("operations left after delete: %v", got)
	}
	if _, err := s.LatestSnapshot(ctx, "doc"); !errors.Is(err, ErrNotFound) {
		t.Errorf("LatestSnapshot after delete = %v, want ErrNotFound", err)
	}
}
