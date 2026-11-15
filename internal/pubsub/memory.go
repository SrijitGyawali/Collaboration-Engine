package pubsub

import (
	"context"
	"slices"
)

// Memory is an in-process broker: every subscriber in the same process
// receives every message published for its documents. Tests use it to run
// several gateways in one process, and it lets a single process run without
// Redis.
//
// Unlike Redis, delivery is synchronous and never lost, so tests are exact.
type Memory struct {
	r router
}

// NewMemory returns an empty broker.
func NewMemory() *Memory {
	return &Memory{}
}

// Publish delivers msg to every current subscriber of its document.
func (m *Memory) Publish(_ context.Context, msg Message) error {
	msg.Ops = slices.Clone(msg.Ops) // subscribers must not share the publisher's slice
	m.r.dispatch(msg)
	return nil
}

// Subscribe calls fn for every message published for docID until the
// returned function is called.
func (m *Memory) Subscribe(docID string, fn func(Message)) (unsubscribe func(), err error) {
	id, _ := m.r.add(docID, fn)
	return func() { m.r.remove(docID, id) }, nil
}

// Subscribed reports how many documents have at least one subscriber.
func (m *Memory) Subscribed() int {
	return m.r.subscribed()
}
