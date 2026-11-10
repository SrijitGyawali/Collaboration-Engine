package syncsvc

import (
	"os"
	"testing"

	"github.com/SrijitGyawali/Collaboration-Engine/internal/store"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/store/storetest"
)

func TestMain(m *testing.M) {
	code := m.Run()
	storetest.Stop()
	os.Exit(code)
}

func openPostgres(t *testing.T) *store.Store {
	t.Helper()
	url := storetest.URL(t)
	storetest.Reset(t, url)
	st, err := store.Open(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	return st
}

// The real thing end to end: edits survive a restart of the service, and the
// reloaded CRDT continues exactly where the old one stopped.
func TestEditsSurviveARestart(t *testing.T) {
	st := openPostgres(t)
	ops := typed("alice", "hello")

	first := New(st, quietLog(), Options{})
	if _, err := first.Apply(t.Context(), "doc", ops[:3]); err != nil {
		t.Fatal(err)
	}
	first.Close()

	// A new service process: nothing in memory, everything from PostgreSQL.
	second := newService(t, st, Options{})
	if text, err := second.Text(t.Context(), "doc"); err != nil || text != "hel" {
		t.Fatalf("Text after restart = %q, %v", text, err)
	}

	acks, err := second.Apply(t.Context(), "doc", ops)
	if err != nil {
		t.Fatal(err)
	}
	for i, a := range acks {
		wantDup := i < 3
		if a.Seq != int64(i+1) || a.Duplicate != wantDup || a.Err != "" {
			t.Errorf("acks[%d] = %+v", i, a)
		}
	}
	if text, _ := second.Text(t.Context(), "doc"); text != "hello" {
		t.Errorf("Text = %q, want %q", text, "hello")
	}

	// Every stored op is in the outbox too, for Kafka.
	events, err := st.UnpublishedEvents(t.Context(), 100)
	if err != nil || len(events) != 5 {
		t.Errorf("outbox has %d events, %v; want 5", len(events), err)
	}
}

func TestOperationsSinceReadsPostgres(t *testing.T) {
	st := openPostgres(t)
	s := newService(t, st, Options{})
	if _, err := s.Apply(t.Context(), "doc", typed("alice", "abc")); err != nil {
		t.Fatal(err)
	}

	ops, err := s.OperationsSince(t.Context(), "doc", 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 2 || ops[0].Seq != 2 || ops[0].Op.Value != "b" || ops[1].Op.Value != "c" {
		t.Errorf("OperationsSince(1) = %+v", ops)
	}
}
