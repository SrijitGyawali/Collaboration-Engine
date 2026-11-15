package gateway

import (
	"sync/atomic"
	"testing"
)

func TestBroadcastSkipsTheSender(t *testing.T) {
	h := newHub()
	a := newClient(4, "a", func(string) {})
	b := newClient(4, "b", func(string) {})
	h.join("doc", a)
	h.join("doc", b)

	h.broadcast("doc", a, []byte("hi"))

	if len(a.send) != 0 || len(b.send) != 1 {
		t.Errorf("queued: sender %d, other %d", len(a.send), len(b.send))
	}
}

// Edits from pub/sub skip every connection of their author, whichever
// gateway they were made on, and reach everyone else in the room only.
func TestBroadcastExceptAuthorSkipsTheAuthorsConnections(t *testing.T) {
	h := newHub()
	author := newClient(4, "alice", func(string) {})
	reconnecting := newClient(4, "alice", func(string) {}) // same replica, old connection
	other := newClient(4, "bob", func(string) {})
	elsewhere := newClient(4, "carol", func(string) {})
	h.join("doc", author)
	h.join("doc", reconnecting)
	h.join("doc", other)
	h.join("other-doc", elsewhere)

	h.broadcastExceptAuthor("doc", "alice", []byte("hi"))

	if len(author.send) != 0 || len(reconnecting.send) != 0 {
		t.Errorf("the author's connections got %d and %d messages", len(author.send), len(reconnecting.send))
	}
	if len(other.send) != 1 {
		t.Errorf("another editor got %d messages, want 1", len(other.send))
	}
	if len(elsewhere.send) != 0 {
		t.Errorf("a client of another document got %d messages", len(elsewhere.send))
	}
}

// A client that stops reading must be disconnected, not allowed to block
// broadcasts to everyone else.
func TestSlowClientIsKicked(t *testing.T) {
	h := newHub()
	var kicked atomic.Int32
	slow := newClient(2, "slow", func(string) { kicked.Add(1) })
	h.join("doc", slow)

	for range 5 {
		h.broadcast("doc", nil, []byte("x"))
	}

	if kicked.Load() == 0 {
		t.Error("a client with a full queue was not kicked")
	}
}

func TestLeaveRemovesEmptyRooms(t *testing.T) {
	h := newHub()
	c := newClient(1, "c", func(string) {})
	h.join("doc", c)
	h.leave("doc", c)

	if h.size("doc") != 0 || len(h.rooms) != 0 {
		t.Errorf("rooms left: %v", h.rooms)
	}
}
