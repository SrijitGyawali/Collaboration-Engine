package pubsub

import (
	"sync"

	"github.com/SrijitGyawali/Collaboration-Engine/internal/wire"
)

// Message is one batch of committed operations for a document, in sequence
// order.
type Message struct {
	DocID string `json:"doc"`
	// Author is the replica that made the ops. Gateways do not send a
	// message back to its author's connections, which already have the ops
	// (and their seqs, from the ack).
	Author string  `json:"author"`
	Ops    []SeqOp `json:"ops"`
}

// SeqOp is an operation with its sequence number.
type SeqOp struct {
	Seq int64   `json:"seq"`
	Op  wire.Op `json:"op"`
}

// router keeps the handlers subscribed to each document and dispatches
// messages to them. Both implementations use it; the Redis one additionally
// subscribes and unsubscribes on the Redis connection when a document gets
// its first handler or loses its last.
type router struct {
	mu       sync.Mutex
	next     int
	handlers map[string]map[int]func(Message)
}

// add registers fn for docID and reports whether it is the document's first
// handler.
func (r *router) add(docID string, fn func(Message)) (id int, first bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.handlers == nil {
		r.handlers = make(map[string]map[int]func(Message))
	}
	hs, ok := r.handlers[docID]
	if !ok {
		hs = make(map[int]func(Message))
		r.handlers[docID] = hs
	}
	r.next++
	hs[r.next] = fn
	return r.next, !ok
}

// remove unregisters a handler and reports whether it was the document's
// last one.
func (r *router) remove(docID string, id int) (last bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	hs, ok := r.handlers[docID]
	if !ok {
		return false
	}
	delete(hs, id)
	if len(hs) == 0 {
		delete(r.handlers, docID)
		return true
	}
	return false
}

// dispatch calls every handler of msg's document. Handlers are copied first,
// so one may unsubscribe while being called.
func (r *router) dispatch(msg Message) {
	r.mu.Lock()
	fns := make([]func(Message), 0, len(r.handlers[msg.DocID]))
	for _, fn := range r.handlers[msg.DocID] {
		fns = append(fns, fn)
	}
	r.mu.Unlock()
	for _, fn := range fns {
		fn(msg)
	}
}

// subscribed reports how many documents have at least one handler.
func (r *router) subscribed() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.handlers)
}
