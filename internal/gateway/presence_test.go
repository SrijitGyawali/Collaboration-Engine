package gateway

import (
	"slices"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/SrijitGyawali/Collaboration-Engine/internal/presence"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/store/memstore"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/syncsvc"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/wire"
)

// enter dials url as a presence-aware client, joins doc as replica and
// returns it with the peers the server listed.
func enter(t *testing.T, url, doc, replica string) (*testClient, []presence.Peer) {
	t.Helper()
	c := dial(t, url)
	c.presence = true
	c.join(doc, replica)
	msg := c.next()
	if msg.Type != typePeers {
		t.Fatalf("%s got %+v after joined, want peers", replica, msg)
	}
	return c, msg.Peers
}

// nextPresence reads until a presence message, failing on anything else.
func (c *testClient) nextPresence() serverMessage {
	c.t.Helper()
	msg := c.next()
	if !isPresence(msg.Type) {
		c.t.Fatalf("got %+v, want a presence message", msg)
	}
	return msg
}

func caretAfter(counter uint64, replica string) *presence.Cursor {
	id := &wire.ID{Counter: counter, Replica: replica}
	return &presence.Cursor{Anchor: id, Head: id}
}

// startPresenceServer runs one gateway with the given presence store and
// heartbeat, no broker.
func startPresenceServer(t *testing.T, st presence.Store, heartbeat time.Duration) string {
	t.Helper()
	svc := syncsvc.New(memstore.New(), quietLog(), syncsvc.Options{})
	t.Cleanup(svc.Close)
	_, url := startGateway(t, Config{Backend: svc, Presence: st, HeartbeatInterval: heartbeat})
	return url
}

// A newcomer is told who is already there, and everyone there is told
// about the newcomer, with the identity the server gave it.
func TestJoinerSeesWhoIsThere(t *testing.T) {
	url := startPresenceServer(t, nil, time.Hour)
	alice, peers := enter(t, url, "doc", "alice")
	if len(peers) != 0 {
		t.Fatalf("the first editor was told about %+v", peers)
	}

	_, peers = enter(t, url, "doc", "bob")
	if len(peers) != 1 || peers[0].Replica != "alice" || peers[0].Name == "" || peers[0].Color == "" {
		t.Fatalf("bob was told about %+v, want alice with a name and colour", peers)
	}
	msg := alice.nextPresence()
	if msg.Type != typePeer || msg.Peer.Replica != "bob" || msg.Peer.Name != "Guest bob" {
		t.Errorf("alice got %+v, want bob joining", msg)
	}

	// Other documents' editors are not listed.
	if _, peers := enter(t, url, "other", "carol"); len(peers) != 0 {
		t.Errorf("an editor of another document was told about %+v", peers)
	}
}

// A cursor move reaches the others but is not echoed to its sender.
func TestCursorMovesReachOthers(t *testing.T) {
	url := startPresenceServer(t, nil, time.Hour)
	alice, _ := enter(t, url, "doc", "alice")
	bob, _ := enter(t, url, "doc", "bob")
	alice.nextPresence() // bob joined

	alice.send(map[string]any{"type": "cursor", "cursor": caretAfter(4, "bob")})
	msg := bob.nextPresence()
	if msg.Type != typePeer || msg.Peer.Replica != "alice" || msg.Peer.Cursor == nil ||
		msg.Peer.Cursor.Head.Counter != 4 || msg.Peer.Cursor.Head.Replica != "bob" {
		t.Fatalf("bob got %+v, want alice's cursor", msg)
	}

	// Messages arrive in order: had alice been sent her own cursor, it
	// would come before the ack for her next edit.
	alice.send(map[string]any{"type": "ops", "ops": typed("alice", "x")})
	if msg := alice.next(); msg.Type != typeAck {
		t.Errorf("alice got %+v, want her ack (her own cursor came back?)", msg)
	}
}

// A cursor at the very start of the document has no character before it.
func TestCursorAtDocumentStart(t *testing.T) {
	url := startPresenceServer(t, nil, time.Hour)
	alice, _ := enter(t, url, "doc", "alice")
	bob, _ := enter(t, url, "doc", "bob")
	alice.nextPresence()

	alice.send(map[string]any{"type": "cursor", "cursor": map[string]any{"anchor": nil, "head": nil}})
	msg := bob.nextPresence()
	if msg.Peer == nil || msg.Peer.Cursor == nil || msg.Peer.Cursor.Anchor != nil || msg.Peer.Cursor.Head != nil {
		t.Errorf("bob got %+v, want a caret at the start", msg)
	}
}

// A malformed cursor is dropped without closing the connection.
func TestMalformedCursorIsIgnored(t *testing.T) {
	url := startPresenceServer(t, nil, time.Hour)
	alice, _ := enter(t, url, "doc", "alice")
	bob, _ := enter(t, url, "doc", "bob")
	alice.nextPresence()

	alice.send(map[string]any{"type": "cursor", "cursor": caretAfter(0, "alice")}) // counter 0
	alice.send(map[string]any{"type": "cursor"})                                   // no cursor at all
	alice.send(map[string]any{"type": "ops", "ops": typed("alice", "x")})
	if msg := alice.next(); msg.Type != typeAck {
		t.Fatalf("alice got %+v, want her ack: the connection should survive", msg)
	}
	// Bob's next message is the edit, not a cursor.
	bob.presence = false
	if msg := bob.next(); msg.Type != typeRemote {
		t.Errorf("bob got %+v, want alice's edit", msg)
	}
}

// Closing the document is announced, and the store forgets the editor.
func TestLeavingIsAnnounced(t *testing.T) {
	st := presence.NewMemory()
	url := startPresenceServer(t, st, time.Hour)
	alice, _ := enter(t, url, "doc", "alice")
	bob, _ := enter(t, url, "doc", "bob")
	alice.nextPresence()

	_ = bob.conn.Close(websocket.StatusNormalClosure, "")
	msg := alice.nextPresence()
	if msg.Type != typeLeft || msg.Replica != "bob" {
		t.Fatalf("alice got %+v, want bob leaving", msg)
	}
	peers, _ := st.List(t.Context(), "doc")
	if len(peers) != 1 || peers[0].Replica != "alice" {
		t.Errorf("store lists %+v, want only alice", peers)
	}
}

// A reconnected editor's old connection going away is not a departure:
// the editor is still there, on its new connection.
func TestOldConnectionOfAReconnectedEditorIsNotALeave(t *testing.T) {
	url := startPresenceServer(t, nil, time.Hour)
	alice, _ := enter(t, url, "doc", "alice")
	old, _ := enter(t, url, "doc", "bob")
	alice.nextPresence() // bob
	_, _ = enter(t, url, "doc", "bob")
	alice.nextPresence() // bob again, on the new connection

	_ = old.conn.Close(websocket.StatusNormalClosure, "")
	time.Sleep(100 * time.Millisecond) // let the old connection's leave run

	// The next presence alice hears must be bob's cursor, not bob leaving.
	alice.send(map[string]any{"type": "ops", "ops": typed("alice", "x")})
	alice.presence = false
	if msg := alice.next(); msg.Type != typeAck {
		t.Fatalf("alice got %+v, want her ack", msg)
	}
	alice.presence = true
	_, peers := enter(t, url, "doc", "carol")
	if !slices.ContainsFunc(peers, func(p presence.Peer) bool { return p.Replica == "bob" }) {
		t.Errorf("a newcomer was told about %+v; bob should still be there", peers)
	}
}

// Every heartbeat refreshes the stored entry and announces the editor again,
// so an editor who stays keeps being heard about, and its entry never
// expires.
func TestHeartbeatKeepsPresenceAlive(t *testing.T) {
	st := presence.NewMemory()
	// Generous for a loaded CI machine: the TTL is three beats, 300 ms.
	beat := 100 * time.Millisecond
	url := startPresenceServer(t, st, beat)
	_, _ = enter(t, url, "doc", "alice")
	bob, _ := enter(t, url, "doc", "bob")

	for i := range 3 {
		msg := bob.nextPresence()
		if msg.Type != typePeer || msg.Peer.Replica != "alice" {
			t.Fatalf("heartbeat %d: bob got %+v, want alice again", i, msg)
		}
	}
	// Well past one TTL (3 beats) since alice joined: still stored.
	time.Sleep(5 * beat)
	peers, _ := st.List(t.Context(), "doc")
	if len(peers) != 2 {
		t.Errorf("store lists %+v after several TTLs, want both editors", peers)
	}
}

// Presence crosses gateways: editors on different gateways see each other
// arrive, move their cursors and leave.
func TestPresenceAcrossGateways(t *testing.T) {
	c := startCluster(t, 2)
	alice, _ := enter(t, c.urls[0], "doc", "alice")
	bob, peers := enter(t, c.urls[1], "doc", "bob")
	if len(peers) != 1 || peers[0].Replica != "alice" {
		t.Fatalf("bob, on the other gateway, was told about %+v", peers)
	}
	if msg := alice.nextPresence(); msg.Type != typePeer || msg.Peer.Replica != "bob" {
		t.Fatalf("alice got %+v, want bob joining", msg)
	}

	bob.send(map[string]any{"type": "cursor", "cursor": caretAfter(2, "alice")})
	if msg := alice.nextPresence(); msg.Peer == nil || msg.Peer.Cursor == nil || msg.Peer.Cursor.Head.Counter != 2 {
		t.Fatalf("alice got %+v, want bob's cursor", msg)
	}

	_ = bob.conn.Close(websocket.StatusNormalClosure, "")
	if msg := alice.nextPresence(); msg.Type != typeLeft || msg.Replica != "bob" {
		t.Errorf("alice got %+v, want bob leaving", msg)
	}
}
