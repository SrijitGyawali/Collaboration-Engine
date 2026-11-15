package pubsub

import (
	"testing"

	"github.com/SrijitGyawali/Collaboration-Engine/internal/wire"
)

func msg(doc string, seqs ...int64) Message {
	m := Message{DocID: doc, Author: "alice"}
	for _, s := range seqs {
		m.Ops = append(m.Ops, SeqOp{Seq: s, Op: wire.Op{Kind: "insert", ID: wire.ID{Counter: uint64(s), Replica: "alice"}, Value: "x"}})
	}
	return m
}

func TestMemoryDeliversToSubscribersOfTheDocument(t *testing.T) {
	m := NewMemory()
	var one, two, other []Message
	_, _ = m.Subscribe("doc", func(msg Message) { one = append(one, msg) })
	_, _ = m.Subscribe("doc", func(msg Message) { two = append(two, msg) })
	_, _ = m.Subscribe("other", func(msg Message) { other = append(other, msg) })

	if err := m.Publish(t.Context(), msg("doc", 1, 2)); err != nil {
		t.Fatal(err)
	}

	if len(one) != 1 || len(two) != 1 || len(one[0].Ops) != 2 || one[0].Ops[1].Seq != 2 {
		t.Errorf("subscribers of doc got %v and %v", one, two)
	}
	if len(other) != 0 {
		t.Errorf("a subscriber of another document got %v", other)
	}
}

func TestMemoryUnsubscribe(t *testing.T) {
	m := NewMemory()
	got := 0
	stop, _ := m.Subscribe("doc", func(Message) { got++ })
	_ = m.Publish(t.Context(), msg("doc", 1))
	stop()
	_ = m.Publish(t.Context(), msg("doc", 2))

	if got != 1 {
		t.Errorf("got %d messages, want 1", got)
	}
	if m.Subscribed() != 0 {
		t.Errorf("%d documents still subscribed", m.Subscribed())
	}
}

// A handler may unsubscribe itself while it is being called.
func TestMemoryHandlerCanUnsubscribeItself(t *testing.T) {
	m := NewMemory()
	var stop func()
	calls := 0
	stop, _ = m.Subscribe("doc", func(Message) {
		calls++
		stop()
	})
	_ = m.Publish(t.Context(), msg("doc", 1))
	_ = m.Publish(t.Context(), msg("doc", 2))

	if calls != 1 {
		t.Errorf("handler called %d times, want 1", calls)
	}
}

func TestRouterReportsFirstAndLast(t *testing.T) {
	var r router
	a, first := r.add("doc", func(Message) {})
	if !first {
		t.Error("first handler not reported as first")
	}
	b, first := r.add("doc", func(Message) {})
	if first {
		t.Error("second handler reported as first")
	}
	if r.remove("doc", a) {
		t.Error("removing one of two reported as last")
	}
	if !r.remove("doc", b) {
		t.Error("removing the last one not reported as last")
	}
	if r.remove("doc", b) {
		t.Error("removing twice reported as last")
	}
}
