package gateway

import (
	"context"
	"sync"
)

// client is one WebSocket connection, as the hub sees it: a queue of encoded
// messages waiting to be written, drained by the connection's writer
// goroutine.
type client struct {
	send chan []byte
	// replica is the replica this connection edits as. Edits arriving
	// through pub/sub carry their author's replica, which is how the gateway
	// knows not to echo them back to the connection that made them.
	replica string
	// kick closes the connection; it must be safe to call more than once and
	// from any goroutine.
	kick func(reason string)
}

func newClient(queue int, replica string, kick func(string)) *client {
	return &client{send: make(chan []byte, queue), replica: replica, kick: kick}
}

// offer queues msg without waiting. A client whose queue is full is not
// keeping up; it is disconnected rather than allowed to hold up everyone
// else's broadcasts, and catches up from history when it reconnects.
func (c *client) offer(msg []byte) {
	select {
	case c.send <- msg:
	default:
		c.kick("too slow: messages were piling up")
	}
}

// push queues msg, waiting for room. Used for catch-up and acks, which belong
// to this client alone and must not be dropped.
func (c *client) push(ctx context.Context, msg []byte) error {
	select {
	case c.send <- msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// hub groups clients into rooms, one per document.
type hub struct {
	mu    sync.Mutex
	rooms map[string]map[*client]struct{}
}

func newHub() *hub {
	return &hub{rooms: make(map[string]map[*client]struct{})}
}

func (h *hub) join(doc string, c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	room, ok := h.rooms[doc]
	if !ok {
		room = make(map[*client]struct{})
		h.rooms[doc] = room
	}
	room[c] = struct{}{}
}

func (h *hub) leave(doc string, c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.rooms[doc], c)
	if len(h.rooms[doc]) == 0 {
		delete(h.rooms, doc)
	}
}

// broadcast offers msg to every client in the room except from.
func (h *hub) broadcast(doc string, from *client, msg []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.rooms[doc] {
		if c != from {
			c.offer(msg)
		}
	}
}

// broadcastExceptAuthor offers msg to every client in the room except those
// editing as author. It is used for edits arriving through pub/sub, which
// may come from any gateway: the author, if connected here, already had an
// ack for them.
func (h *hub) broadcastExceptAuthor(doc, author string, msg []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.rooms[doc] {
		if c.replica != author {
			c.offer(msg)
		}
	}
}

// hasOther reports whether the room has a connection of replica other than
// c: a reconnected editor whose old connection is only now going away.
func (h *hub) hasOther(doc, replica string, c *client) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for other := range h.rooms[doc] {
		if other != c && other.replica == replica {
			return true
		}
	}
	return false
}

// size reports how many clients are in a room.
func (h *hub) size(doc string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.rooms[doc])
}
