// Package integration tests the whole server side together, as it runs in
// production: WebSocket clients -> gateways -> gRPC (real TCP) -> sync
// service -> storage, with committed edits fanned back out to every gateway
// through pub/sub. Nothing is mocked between the client and the database.
package integration

import (
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"

	"github.com/SrijitGyawali/Collaboration-Engine/internal/gateway"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/pubsub"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/pubsub/redistest"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/store"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/store/memstore"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/store/storetest"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/syncrpc"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/syncsvc"
)

func TestMain(m *testing.M) {
	code := m.Run()
	storetest.Stop()
	redistest.Stop()
	os.Exit(code)
}

func quietLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// broker is a pub/sub connection: the sync service publishes through one,
// each gateway subscribes (and publishes presence) through its own.
type broker interface {
	syncsvc.Publisher
	gateway.Broker
}

// brokers returns a new pub/sub connection for one process.
type brokers func(t *testing.T) broker

// memoryBrokers connects every process to one in-memory broker.
func memoryBrokers() brokers {
	m := pubsub.NewMemory()
	return func(*testing.T) broker { return m }
}

// redisBrokers gives every process its own connection to a real Redis, as
// separate processes would have. It skips the test without Docker.
func redisBrokers(t *testing.T) brokers {
	url := redistest.URL(t)
	return func(t *testing.T) broker {
		t.Helper()
		r, err := pubsub.NewRedis(t.Context(), url, quietLog())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = r.Close() })
		return r
	}
}

// system is a running sync service and one or more gateways, connected over
// real gRPC.
type system struct {
	t       *testing.T
	store   syncsvc.Store
	brokers brokers // nil: no pub/sub, a single gateway broadcasting itself
	addr    string  // the sync service's gRPC address
	url     string  // the first gateway's WebSocket URL
	urls    []string
	gws     []*gatewayProc
	svc     *syncsvc.Service
	grpcSrv *grpc.Server
}

// gatewayProc is one gateway process: its WebSocket server, the HTTP server
// in front of it, and its own gRPC connection to the sync service.
type gatewayProc struct {
	srv     *gateway.Server
	ts      *httptest.Server
	backend *syncrpc.Client
	stopped bool
}

// startSystem runs one gateway with no pub/sub: the single-gateway setup.
func startSystem(t *testing.T, st syncsvc.Store) *system {
	t.Helper()
	return startCluster(t, st, 1, nil)
}

// startCluster runs n gateways, which hear about each other's edits through
// connections made by b.
func startCluster(t *testing.T, st syncsvc.Store, n int, b brokers) *system {
	t.Helper()
	s := &system{t: t, store: st, brokers: b, addr: "127.0.0.1:0"}
	s.startSync()
	for range n {
		s.startGateway()
	}
	s.url = s.urls[0]
	t.Cleanup(func() {
		for i := range s.gws {
			s.stopGateway(i)
		}
		s.stopSync()
	})
	return s
}

// startGateway starts one more gateway process.
func (s *system) startGateway() {
	s.t.Helper()
	backend, err := syncrpc.Dial(s.addr)
	if err != nil {
		s.t.Fatal(err)
	}
	cfg := gateway.Config{Backend: backend, Log: quietLog()}
	if s.brokers != nil {
		cfg.Broker = s.brokers(s.t)
	}
	gw := gateway.NewServer(cfg)
	ts := httptest.NewServer(gw)
	s.gws = append(s.gws, &gatewayProc{srv: gw, ts: ts, backend: backend})
	s.urls = append(s.urls, "ws"+strings.TrimPrefix(ts.URL, "http")+"/ws")
}

// stopGateway shuts gateway i down, as a deploy or a crash of that process
// would; its clients are told it is going away.
func (s *system) stopGateway(i int) {
	g := s.gws[i]
	if g.stopped {
		return
	}
	g.stopped = true
	g.srv.Close()
	g.ts.Close()
	_ = g.backend.Close()
}

// startSync starts the sync service process: a fresh service (nothing in
// memory) on the shared store, serving gRPC on s.addr.
func (s *system) startSync() {
	s.t.Helper()
	lis, err := (&net.ListenConfig{}).Listen(s.t.Context(), "tcp", s.addr)
	if err != nil {
		s.t.Fatal(err)
	}
	s.addr = lis.Addr().String() // keep the port for restarts
	var opts syncsvc.Options
	if s.brokers != nil {
		opts.Publisher = s.brokers(s.t)
	}
	s.svc = syncsvc.New(s.store, quietLog(), opts)
	s.grpcSrv = grpc.NewServer()
	syncrpc.Register(s.grpcSrv, s.svc)
	go func() { _ = s.grpcSrv.Serve(lis) }()
}

// stopSync kills the sync service process.
func (s *system) stopSync() {
	if s.grpcSrv == nil {
		return
	}
	s.grpcSrv.Stop()
	s.svc.Close()
	s.grpcSrv = nil
}

// eventually retries check until it passes or 10 s have gone by.
func eventually(t *testing.T, what string, check func() error) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		err := check()
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: %v", what, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// converged checks that every client has its edits acknowledged and shows the
// same text as the sync service's own replica.
func converged(t *testing.T, s *system, docID string, clients ...*client) {
	t.Helper()
	eventually(t, "convergence", func() error {
		want, err := s.svc.Text(t.Context(), docID)
		if err != nil {
			return err
		}
		for _, c := range clients {
			if n := c.pending(); n > 0 {
				return fmt.Errorf("%s still has %d unacknowledged ops", c.replica, n)
			}
			if got := c.text(); got != want {
				return fmt.Errorf("%s shows %q, server has %q", c.replica, got, want)
			}
		}
		return nil
	})
}

// randomEdits makes n random edits on c, as a person typing and deleting.
func randomEdits(c *client, seed uint64, n int) {
	rng := rand.New(rand.NewPCG(seed, 0))
	for range n {
		c.change(func(length int) (int, int, string) {
			if length > 0 && rng.IntN(3) == 0 {
				return rng.IntN(length), 1, ""
			}
			return rng.IntN(length + 1), 0, string(rune('a' + rng.IntN(26)))
		})
		time.Sleep(time.Duration(rng.IntN(3)) * time.Millisecond)
	}
}

// concurrentEditing is the main scenario: several people type and delete at
// the same time in one document; afterwards everyone, including the server,
// has the same text. The clients are spread over the system's gateways.
func concurrentEditing(t *testing.T, s *system) {
	docID := fmt.Sprintf("it%d", time.Now().UnixNano())
	var clients []*client
	for i, name := range []string{"alice", "bob", "carol", "dave"} {
		clients = append(clients, newClient(t, s.urls[i%len(s.urls)], docID, name))
	}

	var wg sync.WaitGroup
	for i, c := range clients {
		wg.Go(func() { randomEdits(c, uint64(i), 60) })
	}
	wg.Wait()

	converged(t, s, docID, clients...)
	if clients[0].length() == 0 {
		t.Error("the document ended up empty; the test exercised nothing")
	}
}

func TestConcurrentEditorsConverge(t *testing.T) {
	concurrentEditing(t, startSystem(t, memstore.New()))
}

func TestConcurrentEditorsConvergeOnPostgres(t *testing.T) {
	url := storetest.URL(t)
	storetest.Reset(t, url)
	st, err := store.Open(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	concurrentEditing(t, startSystem(t, st))
}

// Editors on two gateways see each other's edits live, through pub/sub, not
// only after reconnecting.
func TestEditorsOnTwoGatewaysConverge(t *testing.T) {
	concurrentEditing(t, startCluster(t, memstore.New(), 2, memoryBrokers()))
}

// The same through a real Redis, with every process on its own connection,
// as in Compose.
func TestEditorsOnTwoGatewaysConvergeThroughRedis(t *testing.T) {
	concurrentEditing(t, startCluster(t, memstore.New(), 2, redisBrokers(t)))
}

// Every edit reaches the other gateway's editor while both stay connected:
// each client opens exactly one connection, so catch-up after a reconnect
// cannot be what delivered the edits.
func TestCrossGatewayEditsArriveLive(t *testing.T) {
	s := startCluster(t, memstore.New(), 2, redisBrokers(t))
	alice := newClient(t, s.urls[0], "doc", "alice")
	bob := newClient(t, s.urls[1], "doc", "bob")

	alice.edit(0, 0, "hello")
	converged(t, s, "doc", alice, bob)
	bob.edit(5, 0, " world")
	converged(t, s, "doc", alice, bob)
	if alice.connections() != 1 || bob.connections() != 1 {
		t.Errorf("connections: alice %d, bob %d; the edits came by reconnecting", alice.connections(), bob.connections())
	}
	if got := alice.text(); got != "hello world" {
		t.Errorf("text = %q", got)
	}
}

// A gateway goes away (a deploy, a crash) while its editor keeps typing. The
// editor reconnects to the other gateway, as the proxy would send it, and
// nothing typed on either side is lost.
func TestGatewayFailover(t *testing.T) {
	s := startCluster(t, memstore.New(), 2, memoryBrokers())
	alice := newClient(t, s.urls[0], "doc", "alice")
	bob := newClient(t, s.urls[1], "doc", "bob")
	alice.edit(0, 0, "one")
	converged(t, s, "doc", alice, bob)

	s.stopGateway(1)
	if !bob.closedByServer(5 * time.Second) {
		t.Fatal("the gateway went away without closing its connections")
	}
	bob.edit(3, 0, " two")    // offline: waits in bob's queue
	alice.edit(0, 0, "zero ") // bob misses this live

	bob.moveTo(s.urls[0])
	converged(t, s, "doc", alice, bob)
	if got := bob.text(); got != "zero one two" {
		t.Errorf("bob sees %q", got)
	}
}

// A client drops off, everyone keeps editing (the offline client too), and on
// reconnect each side gets what it missed.
func TestReconnectCatchesUpBothWays(t *testing.T) {
	s := startSystem(t, memstore.New())
	alice := newClient(t, s.url, "doc", "alice")
	bob := newClient(t, s.url, "doc", "bob")

	alice.edit(0, 0, "hello")
	converged(t, s, "doc", alice, bob)

	bob.disconnect()
	alice.edit(5, 0, " world") // bob misses this
	bob.edit(5, 0, "!")        // alice cannot see this yet

	bob.connect()
	converged(t, s, "doc", alice, bob)
	if got := alice.text(); got != "hello world!" && got != "hello! world" {
		t.Errorf("text = %q", got)
	}
}

// The sync service dies while someone is typing. Their edit cannot be saved,
// so the gateway asks the client to come back later; the client keeps the
// edit, and once the sync service is back (with nothing in memory, rebuilding
// from storage) the edit is saved and reaches everyone.
func TestSyncServiceRestart(t *testing.T) {
	s := startSystem(t, memstore.New())
	alice := newClient(t, s.url, "doc", "alice")
	bob := newClient(t, s.url, "doc", "bob")
	alice.edit(0, 0, "before ")
	converged(t, s, "doc", alice, bob)

	s.stopSync()
	alice.edit(7, 0, "during")
	if !alice.closedByServer(10 * time.Second) {
		t.Fatal("the gateway kept a client whose edits could not be saved")
	}
	if alice.pending() == 0 {
		t.Fatal("the unsaved edit was dropped")
	}

	s.startSync()
	alice.reconnect()
	converged(t, s, "doc", alice, bob)
	if got := bob.text(); got != "before during" {
		t.Errorf("bob sees %q", got)
	}
}
