package gateway

import (
	"cmp"
	"context"
	"slices"
	"sync"
	"time"

	"github.com/SrijitGyawali/Collaboration-Engine/internal/presence"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/pubsub"
)

// Presence, the gateway's side. Each connection is a member of its
// document: on joining it is stored and announced, then the new client is
// sent everyone already there. Every heartbeat it is stored again (keeping
// its entry from expiring) and announced again (keeping it alive in other
// browsers, which forget a peer they have not heard from in three
// heartbeats). Cursor moves are announced as they come. On leaving it is
// removed and its departure announced.
//
// Presence is best effort throughout: a failed store or publish is logged,
// never a reason to drop the connection. The next heartbeat repairs it.

const (
	defaultHeartbeat = 10 * time.Second
	// presenceTimeout bounds each presence store or publish call, so a slow
	// Redis cannot hold up the connection's reads.
	presenceTimeout = 2 * time.Second
)

// presenceTTL is how long an entry lives without a heartbeat: three missed
// beats, so one slow beat does not make anyone vanish.
func (s *Server) presenceTTL() time.Duration {
	return 3 * s.cfg.HeartbeatInterval
}

// member is one connection's presence. The read loop moves its cursor, the
// heartbeat goroutine re-announces it; mu keeps the two apart.
type member struct {
	doc string
	c   *client

	mu   sync.Mutex
	peer presence.Peer

	stopBeat func()
	beatDone chan struct{}
}

func (m *member) snapshot() presence.Peer {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.peer
	if p.Cursor != nil {
		cur := *p.Cursor
		p.Cursor = &cur
	}
	return p
}

func (m *member) setCursor(cur *presence.Cursor) {
	m.mu.Lock()
	m.peer.Cursor = cur
	m.mu.Unlock()
}

// enter makes c a member of doc: it is stored and announced, the client is
// sent everyone already there, and the heartbeat starts. The caller must
// call leave when the connection ends.
func (s *Server) enter(ctx context.Context, doc string, c *client, you identity) (*member, error) {
	m := &member{
		doc:      doc,
		c:        c,
		peer:     presence.Peer{Replica: you.Replica, Name: you.Name, Color: you.Color},
		beatDone: make(chan struct{}),
	}
	s.announce(ctx, m)

	// Announced first, then listed: anyone joining in between hears about
	// us live, and we hear about them live too, since the connection is
	// already in the room (and subscribed).
	lctx, cancel := context.WithTimeout(ctx, presenceTimeout)
	peers, err := s.cfg.Presence.List(lctx, doc)
	cancel()
	if err != nil {
		s.cfg.Log.Warn("gateway: could not list who is in the document", "doc", doc, "err", err)
	}
	peers = slices.DeleteFunc(peers, func(p presence.Peer) bool { return p.Replica == you.Replica })
	slices.SortFunc(peers, func(a, b presence.Peer) int { return cmp.Compare(a.Replica, b.Replica) })

	beatCtx, stop := context.WithCancel(ctx)
	m.stopBeat = stop
	go s.heartbeat(beatCtx, m)

	return m, c.push(ctx, encode(serverMessage{Type: typePeers, Peers: peers}))
}

// moveCursor records a member's new selection and announces it.
func (s *Server) moveCursor(ctx context.Context, m *member, cur *presence.Cursor) {
	m.setCursor(cur)
	s.announce(ctx, m)
}

// announce stores the member's presence, refreshing its TTL, and tells the
// document's other editors on every gateway.
func (s *Server) announce(ctx context.Context, m *member) {
	ctx, cancel := context.WithTimeout(ctx, presenceTimeout)
	defer cancel()
	p := m.snapshot()
	if err := s.cfg.Presence.Put(ctx, m.doc, p, s.presenceTTL()); err != nil {
		s.cfg.Log.Warn("gateway: could not store presence", "doc", m.doc, "err", err)
	}
	s.sendPresence(ctx, m, encode(serverMessage{Type: typePeer, Peer: &p}))
}

// leave stops the heartbeat, removes the member and announces that it left.
// It runs after the connection has ended, so it has its own deadline.
func (s *Server) leave(m *member) {
	m.stopBeat()
	<-m.beatDone // no heartbeat may re-announce the member after this

	p := m.snapshot()
	// A reconnected editor's new connection may already be in the room;
	// then it has not left, only its old connection has.
	if s.hub.hasOther(m.doc, p.Replica, m.c) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), presenceTimeout)
	defer cancel()
	if err := s.cfg.Presence.Remove(ctx, m.doc, p.Replica); err != nil {
		s.cfg.Log.Warn("gateway: could not remove presence", "doc", m.doc, "err", err)
	}
	s.sendPresence(ctx, m, encode(serverMessage{Type: typeLeft, Replica: p.Replica}))
}

// heartbeat re-announces m every interval until ctx ends.
func (s *Server) heartbeat(ctx context.Context, m *member) {
	defer close(m.beatDone)
	ticker := time.NewTicker(s.cfg.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.announce(ctx, m)
		case <-ctx.Done():
			return
		}
	}
}

// sendPresence delivers a presence message to the document's other editors:
// through the broker to every gateway (this one included, which forwards it
// like any other), or, with no broker, straight to this gateway's room.
func (s *Server) sendPresence(ctx context.Context, m *member, msg []byte) {
	if s.cfg.Broker == nil {
		s.hub.broadcastExceptAuthor(m.doc, m.c.replica, msg)
		return
	}
	err := s.cfg.Broker.Publish(ctx, pubsub.Message{DocID: m.doc, Author: m.c.replica, Presence: msg})
	if err != nil {
		s.cfg.Log.Warn("gateway: could not publish presence", "doc", m.doc, "err", err)
	}
}
