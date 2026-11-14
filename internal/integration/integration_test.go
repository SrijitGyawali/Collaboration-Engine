// Package integration tests the whole server side together, as it runs in
// production: WebSocket clients -> gateway -> gRPC (real TCP) -> sync service
// -> storage. Nothing is mocked between the client and the database.
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
	"github.com/SrijitGyawali/Collaboration-Engine/internal/store"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/store/memstore"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/store/storetest"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/syncrpc"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/syncsvc"
)

func TestMain(m *testing.M) {
	code := m.Run()
	storetest.Stop()
	os.Exit(code)
}

func quietLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// system is a running sync service and gateway, connected over real gRPC.
type system struct {
	t       *testing.T
	store   syncsvc.Store
	addr    string // the sync service's gRPC address
	url     string // the gateway's WebSocket URL
	svc     *syncsvc.Service
	grpcSrv *grpc.Server
}

func startSystem(t *testing.T, st syncsvc.Store) *system {
	t.Helper()
	s := &system{t: t, store: st, addr: "127.0.0.1:0"}
	s.startSync()

	backend, err := syncrpc.Dial(s.addr)
	if err != nil {
		t.Fatal(err)
	}
	gw := gateway.NewServer(gateway.Config{Backend: backend, Log: quietLog()})
	ts := httptest.NewServer(gw)
	s.url = "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"

	t.Cleanup(func() {
		gw.Close()
		ts.Close()
		_ = backend.Close()
		s.stopSync()
	})
	return s
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
	s.svc = syncsvc.New(s.store, quietLog(), syncsvc.Options{})
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
// has the same text.
func concurrentEditing(t *testing.T, st syncsvc.Store) {
	s := startSystem(t, st)
	docID := fmt.Sprintf("it%d", time.Now().UnixNano())
	clients := []*client{
		newClient(t, s.url, docID, "alice"),
		newClient(t, s.url, docID, "bob"),
		newClient(t, s.url, docID, "carol"),
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
	concurrentEditing(t, memstore.New())
}

func TestConcurrentEditorsConvergeOnPostgres(t *testing.T) {
	url := storetest.URL(t)
	storetest.Reset(t, url)
	st, err := store.Open(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	concurrentEditing(t, st)
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
