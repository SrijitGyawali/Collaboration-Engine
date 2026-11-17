package gateway

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/SrijitGyawali/Collaboration-Engine/internal/pubsub"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/store/memstore"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/syncsvc"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/wire"
)

// cluster is several gateways over one sync service, hearing about
// committed edits through one in-memory broker: the way gateway processes
// share Redis, inside one test.
type cluster struct {
	broker *pubsub.Memory
	svc    *syncsvc.Service
	srvs   []*Server
	urls   []string
}

func startCluster(t *testing.T, gateways int) *cluster {
	t.Helper()
	c := &cluster{broker: pubsub.NewMemory()}
	c.svc = syncsvc.New(memstore.New(), quietLog(), syncsvc.Options{Publisher: c.broker})
	t.Cleanup(c.svc.Close) // registered first, so it runs after the gateways close
	for range gateways {
		srv, url := startGateway(t, Config{Backend: c.svc, Broker: c.broker})
		c.srvs = append(c.srvs, srv)
		c.urls = append(c.urls, url)
	}
	return c
}

// waitUntil polls cond for up to 2 s.
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting until %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// An edit sent through one gateway reaches editors on every gateway, with
// its sequence numbers, and is not echoed back to its author.
func TestEditsReachEditorsOnOtherGateways(t *testing.T) {
	c := startCluster(t, 2)
	alice := dial(t, c.urls[0])
	carol := dial(t, c.urls[0]) // same gateway as the author
	bob := dial(t, c.urls[1])
	alice.join("doc", "alice")
	carol.join("doc", "carol")
	bob.join("doc", "bob")

	alice.send(map[string]any{"type": "ops", "ops": typed("alice", "hi")})

	ack := alice.next()
	if ack.Type != typeAck || len(ack.Acks) != 2 || ack.Acks[1].Seq != 2 {
		t.Fatalf("alice got %+v, want an ack for both ops", ack)
	}
	for name, cl := range map[string]*testClient{"bob": bob, "carol": carol} {
		remote := cl.next()
		if remote.Type != typeRemote || len(remote.Ops) != 2 || remote.Ops[0].Seq != 1 || remote.Ops[1].Op.Value != "i" {
			t.Errorf("%s got %+v", name, remote)
		}
	}

	// Bob answers from the other gateway; alice hears it, and bob gets only
	// his ack. The in-memory broker delivers while the edit is being stored,
	// so an echo of bob's own edit would arrive before his ack.
	bob.send(map[string]any{"type": "ops", "ops": typedAfter("bob", ack.Acks[1].Seq, "!")})
	if msg := bob.next(); msg.Type != typeAck || msg.Acks[0].Seq != 3 {
		t.Fatalf("bob got %+v, want his ack", msg)
	}
	if msg := alice.next(); msg.Type != typeRemote || msg.Ops[0].Seq != 3 || msg.Ops[0].Op.Value != "!" {
		t.Errorf("alice got %+v, want bob's edit", msg)
	}
}

// typedAfter is one insert by replica, at the start of the document, with
// counter n: enough for tests that only look at how ops travel.
func typedAfter(replica string, n int64, value string) []wire.Op {
	return []wire.Op{{Kind: "insert", ID: wire.ID{Counter: uint64(n) + 1, Replica: replica}, Value: value}}
}

// A late joiner on another gateway gets the history, and then live edits.
func TestLateJoinerOnAnotherGateway(t *testing.T) {
	c := startCluster(t, 2)
	alice := dial(t, c.urls[0])
	alice.join("doc", "alice")
	alice.send(map[string]any{"type": "ops", "ops": typed("alice", "hey")})
	alice.next()

	bob := dial(t, c.urls[1])
	history, seq := bob.join("doc", "bob")
	if len(history) != 3 || seq != 3 {
		t.Fatalf("history %+v, seq %d", history, seq)
	}
	alice.send(map[string]any{"type": "ops", "ops": typedAfter("alice", 3, "!")})
	if msg := bob.next(); msg.Type != typeRemote || msg.Ops[0].Seq != 4 {
		t.Errorf("bob got %+v, want the live edit", msg)
	}
}

// A gateway subscribes to a document once, however many of its clients have
// it open, and unsubscribes when the last one leaves.
func TestOneSubscriptionPerOpenDocument(t *testing.T) {
	c := startCluster(t, 1)
	srv := c.srvs[0]
	a, b, other := dial(t, c.urls[0]), dial(t, c.urls[0]), dial(t, c.urls[0])
	a.join("doc", "a")
	b.join("doc", "b")
	other.join("other", "o")

	if srv.subscribed() != 2 || c.broker.Subscribed() != 2 {
		t.Fatalf("gateway subscribed to %d documents, broker has %d; want 2", srv.subscribed(), c.broker.Subscribed())
	}

	_ = a.conn.Close(websocket.StatusNormalClosure, "")
	_ = other.conn.Close(websocket.StatusNormalClosure, "")
	waitUntil(t, "the closed-only document is unsubscribed", func() bool {
		return srv.subscribed() == 1 && c.broker.Subscribed() == 1
	})
	_ = b.conn.Close(websocket.StatusNormalClosure, "")
	waitUntil(t, "nothing is subscribed", func() bool {
		return srv.subscribed() == 0 && c.broker.Subscribed() == 0
	})
}

// gatedBroker holds every Subscribe call until gate is closed.
type gatedBroker struct {
	inner *pubsub.Memory
	gate  chan struct{}
	calls atomic.Int32
}

func (g *gatedBroker) Publish(ctx context.Context, m pubsub.Message) error {
	return g.inner.Publish(ctx, m)
}

func (g *gatedBroker) Subscribe(doc string, fn func(pubsub.Message)) (func(), error) {
	g.calls.Add(1)
	<-g.gate
	return g.inner.Subscribe(doc, fn)
}

// countingBackend counts history reads.
type countingBackend struct {
	Backend
	reads atomic.Int32
}

func (b *countingBackend) OperationsSince(ctx context.Context, doc string, after int64, limit int) ([]syncsvc.SeqOp, error) {
	b.reads.Add(1)
	return b.Backend.OperationsSince(ctx, doc, after, limit)
}

// While the first join's subscription is still being set up, a second join
// of the same document shares it, and neither reads history until it is
// live: an edit committed in between would otherwise reach neither of them.
func TestJoinsWaitForTheSubscription(t *testing.T) {
	broker := pubsub.NewMemory()
	svc := syncsvc.New(memstore.New(), quietLog(), syncsvc.Options{Publisher: broker})
	t.Cleanup(svc.Close)
	sub := &gatedBroker{inner: broker, gate: make(chan struct{})}
	backend := &countingBackend{Backend: svc}
	srv, url := startGateway(t, Config{Backend: backend, Broker: sub})

	first, second := dial(t, url), dial(t, url)
	first.send(map[string]any{"type": "join", "doc": "doc", "replica": "first"})
	second.send(map[string]any{"type": "join", "doc": "doc", "replica": "second"})
	waitUntil(t, "both joins wait on one subscription", func() bool {
		srv.subsMu.Lock()
		defer srv.subsMu.Unlock()
		s := srv.subs["doc"]
		return s != nil && s.users == 2
	})
	if n := sub.calls.Load(); n != 1 {
		t.Errorf("Subscribe called %d times, want 1", n)
	}
	if n := backend.reads.Load(); n != 0 {
		t.Errorf("history read %d times before the subscription was live", n)
	}

	close(sub.gate)
	for _, cl := range []*testClient{first, second} {
		if msg := cl.next(); msg.Type != typeJoined {
			t.Errorf("got %+v, want joined", msg)
		}
	}
}

type failingBroker struct{}

func (failingBroker) Publish(context.Context, pubsub.Message) error {
	return errors.New("redis is down")
}

func (failingBroker) Subscribe(string, func(pubsub.Message)) (func(), error) {
	return nil, errors.New("redis is down")
}

// If the gateway cannot follow a document, the client is told to try again
// later rather than left editing alone, and nothing stays registered.
func TestSubscribeFailureAsksTheClientToRetry(t *testing.T) {
	svc := syncsvc.New(memstore.New(), quietLog(), syncsvc.Options{})
	t.Cleanup(svc.Close)
	srv, url := startGateway(t, Config{Backend: svc, Broker: failingBroker{}})
	c := dial(t, url)
	c.send(map[string]any{"type": "join", "doc": "doc", "replica": "alice"})
	if code := c.closeStatus(); code != websocket.StatusTryAgainLater {
		t.Errorf("closed with %v, want try again later", code)
	}
	if srv.subscribed() != 0 {
		t.Errorf("%d subscriptions left after a failed join", srv.subscribed())
	}
}
