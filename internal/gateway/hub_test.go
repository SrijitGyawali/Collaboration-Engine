package gateway

import (
	"sync/atomic"
	"testing"
)

func TestBroadcastSkipsTheSender(t *testing.T) {
	h := newHub()
	a := newClient(4, func(string) {})
	b := newClient(4, func(string) {})
	h.join("doc", a)
	h.join("doc", b)

	h.broadcast("doc", a, []byte("hi"))

	if len(a.send) != 0 || len(b.send) != 1 {
		t.Errorf("queued: sender %d, other %d", len(a.send), len(b.send))
	}
}

// A client that stops reading must be disconnected, not allowed to block
// broadcasts to everyone else.
func TestSlowClientIsKicked(t *testing.T) {
	h := newHub()
	var kicked atomic.Int32
	slow := newClient(2, func(string) { kicked.Add(1) })
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
	c := newClient(1, func(string) {})
	h.join("doc", c)
	h.leave("doc", c)

	if h.size("doc") != 0 || len(h.rooms) != 0 {
		t.Errorf("rooms left: %v", h.rooms)
	}
}
