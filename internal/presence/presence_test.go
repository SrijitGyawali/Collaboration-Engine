package presence

import (
	"context"
	"fmt"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SrijitGyawali/Collaboration-Engine/internal/pubsub/redistest"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/wire"
)

func TestMain(m *testing.M) {
	code := m.Run()
	redistest.Stop()
	os.Exit(code)
}

// clock is a time source tests move by hand.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// harness runs the same checks on every Store; wait lets time pass, by
// moving a fake clock (memory) or really sleeping (Redis).
type harness struct {
	store Store
	wait  func(time.Duration)
	ttl   time.Duration
}

func memoryHarness(*testing.T) harness {
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	return harness{store: NewMemoryWithClock(c.now), wait: c.advance, ttl: 30 * time.Second}
}

func redisHarness(t *testing.T) harness {
	x, err := NewRedis(t.Context(), redistest.URL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = x.Close() })
	return harness{store: x, wait: time.Sleep, ttl: 400 * time.Millisecond}
}

var docSeq int

// freshDoc returns a document name no other test has used, since the Redis
// container is shared by the whole test binary.
func freshDoc() string {
	docSeq++
	return fmt.Sprintf("presence%d-%d", time.Now().UnixNano(), docSeq)
}

func replicas(t *testing.T, s Store, doc string) []string {
	t.Helper()
	peers, err := s.List(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, p := range peers {
		out = append(out, p.Replica)
	}
	slices.Sort(out)
	return out
}

func runStoreTests(t *testing.T, newHarness func(*testing.T) harness) {
	t.Run("put and list", func(t *testing.T) {
		h := newHarness(t)
		doc := freshDoc()
		cursor := &Cursor{Anchor: &wire.ID{Counter: 3, Replica: "ann"}, Head: nil}
		if err := h.store.Put(t.Context(), doc, Peer{Replica: "ann", Name: "Ann", Color: "#2f6fde", Cursor: cursor}, h.ttl); err != nil {
			t.Fatal(err)
		}
		if err := h.store.Put(t.Context(), doc, Peer{Replica: "bob", Name: "Bob", Color: "#d6336c"}, h.ttl); err != nil {
			t.Fatal(err)
		}
		peers, err := h.store.List(t.Context(), doc)
		if err != nil {
			t.Fatal(err)
		}
		if len(peers) != 2 {
			t.Fatalf("listed %+v, want ann and bob", peers)
		}
		for _, p := range peers {
			if p.Replica == "ann" && (p.Name != "Ann" || p.Cursor == nil || p.Cursor.Anchor.Counter != 3 || p.Cursor.Head != nil) {
				t.Errorf("ann came back as %+v", p)
			}
		}
		if got := replicas(t, h.store, freshDoc()); len(got) != 0 {
			t.Errorf("another document lists %v", got)
		}
	})

	t.Run("put again replaces", func(t *testing.T) {
		h := newHarness(t)
		doc := freshDoc()
		_ = h.store.Put(t.Context(), doc, Peer{Replica: "ann", Name: "Ann"}, h.ttl)
		_ = h.store.Put(t.Context(), doc, Peer{Replica: "ann", Name: "Ann", Cursor: &Cursor{}}, h.ttl)
		peers, _ := h.store.List(t.Context(), doc)
		if len(peers) != 1 || peers[0].Cursor == nil {
			t.Errorf("listed %+v, want one ann with a cursor", peers)
		}
	})

	t.Run("remove", func(t *testing.T) {
		h := newHarness(t)
		doc := freshDoc()
		_ = h.store.Put(t.Context(), doc, Peer{Replica: "ann"}, h.ttl)
		_ = h.store.Put(t.Context(), doc, Peer{Replica: "bob"}, h.ttl)
		if err := h.store.Remove(t.Context(), doc, "ann"); err != nil {
			t.Fatal(err)
		}
		if err := h.store.Remove(t.Context(), doc, "nobody"); err != nil {
			t.Errorf("removing an absent entry: %v", err)
		}
		if got := replicas(t, h.store, doc); !slices.Equal(got, []string{"bob"}) {
			t.Errorf("listed %v, want [bob]", got)
		}
	})

	// The point of the TTL: an editor whose gateway stopped refreshing it
	// disappears, while one that keeps being refreshed stays.
	t.Run("entries expire unless refreshed", func(t *testing.T) {
		h := newHarness(t)
		doc := freshDoc()
		_ = h.store.Put(t.Context(), doc, Peer{Replica: "gone"}, h.ttl)
		_ = h.store.Put(t.Context(), doc, Peer{Replica: "here"}, h.ttl)
		h.wait(h.ttl / 2)
		_ = h.store.Put(t.Context(), doc, Peer{Replica: "here"}, h.ttl) // heartbeat
		h.wait(h.ttl/2 + h.ttl/4)

		if got := replicas(t, h.store, doc); !slices.Equal(got, []string{"here"}) {
			t.Errorf("listed %v, want only the refreshed entry", got)
		}
		h.wait(h.ttl)
		if got := replicas(t, h.store, doc); len(got) != 0 {
			t.Errorf("listed %v after every entry expired", got)
		}
	})
}

func TestMemoryStore(t *testing.T) { runStoreTests(t, memoryHarness) }

func TestRedisStore(t *testing.T) { runStoreTests(t, redisHarness) }

func TestNewRedisFailsFastWithoutRedis(t *testing.T) {
	if _, err := NewRedis(t.Context(), "127.0.0.1:1"); err == nil {
		t.Error("NewRedis succeeded with nothing listening")
	}
}

func TestCursorValid(t *testing.T) {
	good := []Cursor{
		{},
		{Anchor: &wire.ID{Counter: 1, Replica: "a"}, Head: &wire.ID{Counter: 9, Replica: "b-2_x"}},
	}
	for _, c := range good {
		if err := c.Valid(); err != nil {
			t.Errorf("%+v: %v", c, err)
		}
	}
	bad := []Cursor{
		{Anchor: &wire.ID{Counter: 0, Replica: "a"}},
		{Head: &wire.ID{Counter: 1, Replica: ""}},
		{Head: &wire.ID{Counter: 1, Replica: "has space"}},
	}
	for _, c := range bad {
		if err := c.Valid(); err == nil {
			t.Errorf("%+v accepted", c)
		}
	}
}
