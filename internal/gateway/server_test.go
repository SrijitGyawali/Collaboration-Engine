package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/SrijitGyawali/Collaboration-Engine/internal/crdt"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/store"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/store/memstore"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/syncsvc"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/wire"
)

func quietLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// startServer runs a gateway over an in-memory sync service and returns its
// ws:// URL.
func startServer(t *testing.T, backend Backend) string {
	t.Helper()
	if backend == nil {
		svc := syncsvc.New(memstore.New(), quietLog(), syncsvc.Options{})
		t.Cleanup(svc.Close)
		backend = svc
	}
	srv := NewServer(Config{Backend: backend, Log: quietLog(), AllowedOrigins: []string{"app.example"}})
	ts := httptest.NewServer(srv)
	t.Cleanup(func() {
		srv.Close() // disconnect clients first; hijacked connections block ts.Close
		ts.Close()
	})
	return "ws" + strings.TrimPrefix(ts.URL, "http")
}

// closeBody closes a handshake response body, if there is one.
func closeBody(resp *http.Response) {
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
}

type testClient struct {
	t    *testing.T
	conn *websocket.Conn
}

func dial(t *testing.T, url string) *testClient {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	conn, resp, err := websocket.Dial(ctx, url, nil)
	closeBody(resp)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	// A page of history can be far larger than the library's 32 KiB
	// default; browsers have no such limit.
	conn.SetReadLimit(8 << 20)
	return &testClient{t: t, conn: conn}
}

func (c *testClient) send(v any) {
	c.t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		c.t.Fatal(err)
	}
	if err := c.conn.Write(c.t.Context(), websocket.MessageText, data); err != nil {
		c.t.Fatalf("write: %v", err)
	}
}

// next reads one server message, failing after 2 s.
func (c *testClient) next() serverMessage {
	c.t.Helper()
	msg, err := c.tryNext(2 * time.Second)
	if err != nil {
		c.t.Fatalf("read: %v", err)
	}
	return msg
}

func (c *testClient) tryNext(wait time.Duration) (serverMessage, error) {
	ctx, cancel := context.WithTimeout(c.t.Context(), wait)
	defer cancel()
	_, data, err := c.conn.Read(ctx)
	if err != nil {
		return serverMessage{}, err
	}
	var msg serverMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		c.t.Fatalf("server sent invalid JSON %s: %v", data, err)
	}
	return msg, nil
}

// join joins doc as replica and returns the history received and the joined
// seq.
func (c *testClient) join(doc, replica string) ([]seqOp, int64) {
	c.t.Helper()
	return c.joinWith(map[string]any{"type": "join", "doc": doc, "replica": replica})
}

// joinWith sends a custom join message and collects the catch-up.
func (c *testClient) joinWith(msg map[string]any) ([]seqOp, int64) {
	c.t.Helper()
	c.send(msg)
	var history []seqOp
	for {
		msg := c.next()
		switch msg.Type {
		case typeRemote:
			history = append(history, msg.Ops...)
		case typeJoined:
			return history, msg.Seq
		default:
			c.t.Fatalf("unexpected %q while joining", msg.Type)
		}
	}
}

// closeStatus reads until the server closes the connection.
func (c *testClient) closeStatus() websocket.StatusCode {
	c.t.Helper()
	for {
		if _, err := c.tryNext(2 * time.Second); err != nil {
			return websocket.CloseStatus(err)
		}
	}
}

func typed(replica, text string) []wire.Op {
	ops, err := crdt.NewDocument(replica).InsertText(0, text)
	if err != nil {
		panic(err)
	}
	out := make([]wire.Op, len(ops))
	for i, op := range ops {
		out[i] = wire.FromCRDT(op)
	}
	return out
}

func TestJoinAnEmptyDocument(t *testing.T) {
	c := dial(t, startServer(t, nil))
	history, seq := c.join("doc", "carol")
	if len(history) != 0 || seq != 0 {
		t.Errorf("history %v, seq %d", history, seq)
	}
}

func TestOpsAreAckedAndBroadcast(t *testing.T) {
	url := startServer(t, nil)
	alice, bob := dial(t, url), dial(t, url)
	alice.join("doc", "alice")
	bob.join("doc", "bob")

	alice.send(map[string]any{"type": "ops", "ops": typed("alice", "hi")})

	ack := alice.next()
	if ack.Type != typeAck || len(ack.Acks) != 2 || ack.Acks[0].Seq != 1 || ack.Acks[1].Seq != 2 {
		t.Fatalf("alice got %+v", ack)
	}
	remote := bob.next()
	if remote.Type != typeRemote || len(remote.Ops) != 2 || remote.Ops[1].Seq != 2 || remote.Ops[1].Op.Value != "i" {
		t.Fatalf("bob got %+v", remote)
	}
}

func TestLateJoinerGetsHistory(t *testing.T) {
	url := startServer(t, nil)
	alice := dial(t, url)
	alice.join("doc", "alice")
	alice.send(map[string]any{"type": "ops", "ops": typed("alice", "hello")})
	alice.next() // ack

	history, seq := dial(t, url).join("doc", "dave")
	if len(history) != 5 || seq != 5 || history[4].Op.Value != "o" {
		t.Errorf("history %+v, seq %d", history, seq)
	}
}

// A resent op is acked as a duplicate and not broadcast again; a rejected op
// is acked with its error and never broadcast.
func TestDuplicatesAndRejectionsAreNotBroadcast(t *testing.T) {
	url := startServer(t, nil)
	alice, bob := dial(t, url), dial(t, url)
	alice.join("doc", "alice")
	bob.join("doc", "bob")
	ops := typed("alice", "ab")

	alice.send(map[string]any{"type": "ops", "ops": ops[:1]})
	alice.next()
	bob.next()

	malformed := wire.Op{Kind: "insert", ID: wire.ID{Counter: 9, Replica: "alice"}, Value: "too long"}
	alice.send(map[string]any{"type": "ops", "ops": []wire.Op{ops[0], malformed, ops[1]}})
	ack := alice.next()
	if !ack.Acks[0].Duplicate || ack.Acks[0].Seq != 1 {
		t.Errorf("resent op: %+v", ack.Acks[0])
	}
	if ack.Acks[1].Error == "" || ack.Acks[1].Seq != 0 {
		t.Errorf("malformed op: %+v", ack.Acks[1])
	}
	if ack.Acks[2].Seq != 2 {
		t.Errorf("new op: %+v", ack.Acks[2])
	}

	remote := bob.next()
	if len(remote.Ops) != 1 || remote.Ops[0].Seq != 2 {
		t.Errorf("bob should only get the new op, got %+v", remote.Ops)
	}
}

func TestRoomsAreSeparate(t *testing.T) {
	url := startServer(t, nil)
	alice, other := dial(t, url), dial(t, url)
	alice.join("one", "alice")
	other.join("two", "other")

	alice.send(map[string]any{"type": "ops", "ops": typed("alice", "x")})
	alice.next()

	if msg, err := other.tryNext(200 * time.Millisecond); err == nil {
		t.Errorf("a client in another room received %+v", msg)
	}
}

// A reconnecting client sends the last seq it has and gets only the rest.
func TestCatchUpFromLastSeq(t *testing.T) {
	url := startServer(t, nil)
	alice := dial(t, url)
	alice.join("doc", "alice")
	alice.send(map[string]any{"type": "ops", "ops": typed("alice", "hello")})
	alice.next()

	history, seq := dial(t, url).joinWith(map[string]any{"type": "join", "doc": "doc", "replica": "erin", "lastSeq": 3})
	if len(history) != 2 || history[0].Seq != 4 || history[1].Seq != 5 || seq != 5 {
		t.Errorf("after 3: history %+v, seq %d", history, seq)
	}

	history, seq = dial(t, url).joinWith(map[string]any{"type": "join", "doc": "doc", "replica": "erin", "lastSeq": 5})
	if len(history) != 0 || seq != 5 {
		t.Errorf("up to date: history %+v, seq %d", history, seq)
	}
}

// History longer than one page arrives as several messages, in order.
func TestCatchUpPagesThroughLongHistory(t *testing.T) {
	svc := syncsvc.New(memstore.New(), quietLog(), syncsvc.Options{})
	t.Cleanup(svc.Close)
	ops := typed("alice", strings.Repeat("x", store.MaxPage+5))
	for start := 0; start < len(ops); start += syncsvc.MaxBatch {
		if _, err := svc.Apply(t.Context(), "doc", ops[start:min(start+syncsvc.MaxBatch, len(ops))]); err != nil {
			t.Fatal(err)
		}
	}

	c := dial(t, startServer(t, svc))
	c.send(map[string]any{"type": "join", "doc": "doc", "replica": "carol"})
	pages, next := 0, int64(1)
	for {
		msg := c.next()
		if msg.Type == typeJoined {
			if msg.Seq != int64(len(ops)) {
				t.Errorf("joined seq %d, want %d", msg.Seq, len(ops))
			}
			break
		}
		pages++
		for _, op := range msg.Ops {
			if op.Seq != next {
				t.Fatalf("got seq %d, want %d", op.Seq, next)
			}
			next++
		}
	}
	if pages != 2 {
		t.Errorf("history came in %d messages, want 2", pages)
	}
}

func TestJoinedSaysWhoYouAre(t *testing.T) {
	c := dial(t, startServer(t, nil))
	c.send(map[string]any{"type": "join", "doc": "doc", "replica": "k3j9x2", "name": " Ann "})
	msg := c.next()
	if msg.Type != typeJoined || msg.You == nil {
		t.Fatalf("got %+v", msg)
	}
	if msg.You.Replica != "k3j9x2" || msg.You.Name != "Ann" || msg.You.Color == "" {
		t.Errorf("you = %+v", *msg.You)
	}
}

// A client cannot send edits in another replica's name.
func TestOpsFromAnotherReplicaAreRefused(t *testing.T) {
	url := startServer(t, nil)
	mallory, bob := dial(t, url), dial(t, url)
	mallory.join("doc", "mallory")
	bob.join("doc", "bob")

	mallory.send(map[string]any{"type": "ops", "ops": typed("alice", "forged")})
	if got := mallory.closeStatus(); got != websocket.StatusPolicyViolation {
		t.Errorf("close status %v, want policy violation", got)
	}
	if msg, err := bob.tryNext(200 * time.Millisecond); err == nil {
		t.Errorf("bob received forged edits: %+v", msg)
	}
}

func TestFirstMessageMustBeJoin(t *testing.T) {
	url := startServer(t, nil)
	for name, msg := range map[string]any{
		"ops before join":  map[string]any{"type": "ops"},
		"invalid document": map[string]any{"type": "join", "doc": "has space", "replica": "carol"},
		"negative lastSeq": map[string]any{"type": "join", "doc": "doc", "replica": "carol", "lastSeq": -1},
		"missing replica":  map[string]any{"type": "join", "doc": "doc"},
		"invalid replica":  map[string]any{"type": "join", "doc": "doc", "replica": "has space"},
	} {
		c := dial(t, url)
		c.send(msg)
		if got := c.closeStatus(); got != websocket.StatusPolicyViolation {
			t.Errorf("%s: close status %v, want policy violation", name, got)
		}
	}
}

func TestMalformedMessagesCloseTheConnection(t *testing.T) {
	c := dial(t, startServer(t, nil))
	c.join("doc", "carol")
	if err := c.conn.Write(t.Context(), websocket.MessageText, []byte("{not json")); err != nil {
		t.Fatal(err)
	}
	if got := c.closeStatus(); got != websocket.StatusUnsupportedData {
		t.Errorf("close status %v, want unsupported data", got)
	}
}

func TestOtherWebsitesCannotConnect(t *testing.T) {
	url := startServer(t, nil)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	header := http.Header{"Origin": {"https://evil.example"}}
	conn, resp, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: header})
	closeBody(resp)
	if err == nil {
		_ = conn.CloseNow()
		t.Fatal("connection from another origin was accepted")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Errorf("response = %v, want 403", resp)
	}

	header.Set("Origin", "https://app.example")
	conn, resp, err = websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: header})
	closeBody(resp)
	if err != nil {
		t.Fatalf("allowed origin refused: %v", err)
	}
	_ = conn.CloseNow()
}

// failingBackend stands in for a sync service whose database is down.
type failingBackend struct{}

func (failingBackend) Apply(context.Context, string, []wire.Op) ([]syncsvc.Ack, error) {
	return nil, errors.New("database unreachable")
}

func (failingBackend) OperationsSince(context.Context, string, int64, int) ([]syncsvc.SeqOp, error) {
	return nil, nil
}

// When edits cannot be saved, the client is told to come back later; it then
// resends them.
func TestBackendFailureAsksTheClientToRetry(t *testing.T) {
	c := dial(t, startServer(t, failingBackend{}))
	c.join("doc", "carol")
	c.send(map[string]any{"type": "ops", "ops": typed("carol", "x")})
	if got := c.closeStatus(); got != websocket.StatusTryAgainLater {
		t.Errorf("close status %v, want try again later", got)
	}
}

func TestCloseDisconnectsClients(t *testing.T) {
	svc := syncsvc.New(memstore.New(), quietLog(), syncsvc.Options{})
	defer svc.Close()
	srv := NewServer(Config{Backend: svc, Log: quietLog()})
	ts := httptest.NewServer(srv)
	defer ts.Close()

	c := dial(t, "ws"+strings.TrimPrefix(ts.URL, "http"))
	c.join("doc", "carol")
	go srv.Close()
	if got := c.closeStatus(); got != websocket.StatusGoingAway {
		t.Errorf("close status %v, want going away", got)
	}
}
