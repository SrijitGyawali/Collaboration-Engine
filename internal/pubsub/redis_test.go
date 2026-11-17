package pubsub

import (
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/SrijitGyawali/Collaboration-Engine/internal/pubsub/redistest"
)

func TestMain(m *testing.M) {
	code := m.Run()
	redistest.Stop()
	os.Exit(code)
}

func newRedis(t *testing.T) *Redis {
	t.Helper()
	x, err := NewRedis(t.Context(), redistest.URL(t), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = x.Close() })
	return x
}

// inbox collects messages delivered to a handler.
type inbox struct {
	mu   sync.Mutex
	msgs []Message
}

func (b *inbox) add(m Message) {
	b.mu.Lock()
	b.msgs = append(b.msgs, m)
	b.mu.Unlock()
}

func (b *inbox) wait(t *testing.T, n int) []Message {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		b.mu.Lock()
		got := append([]Message(nil), b.msgs...)
		b.mu.Unlock()
		if len(got) >= n || time.Now().After(deadline) {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Two processes (two Redis instances here): what one publishes, the other
// receives, in order, for the documents it subscribed to only.
func TestRedisFansOutBetweenProcesses(t *testing.T) {
	syncService, gateway := newRedis(t), newRedis(t)
	var got, other inbox
	if _, err := gateway.Subscribe("doc", got.add); err != nil {
		t.Fatal(err)
	}
	if _, err := gateway.Subscribe("other", other.add); err != nil {
		t.Fatal(err)
	}

	// No waiting needed: Subscribe returns only once Redis has confirmed.
	for i := range int64(3) {
		if err := syncService.Publish(t.Context(), msg("doc", i*2+1, i*2+2)); err != nil {
			t.Fatal(err)
		}
	}

	msgs := got.wait(t, 3)
	if len(msgs) != 3 {
		t.Fatalf("got %d messages, want 3", len(msgs))
	}
	for i, m := range msgs {
		if m.DocID != "doc" || m.Author != "alice" || m.Ops[0].Seq != int64(i*2+1) || m.Ops[0].Op.Value != "x" {
			t.Errorf("message %d = %+v", i, m)
		}
	}
	if n := len(other.wait(t, 0)); n != 0 {
		t.Errorf("a subscriber of another document got %d messages", n)
	}
}

func TestRedisUnsubscribe(t *testing.T) {
	pub, sub := newRedis(t), newRedis(t)
	var first, second inbox
	stopFirst, err := sub.Subscribe("doc", first.add)
	if err != nil {
		t.Fatal(err)
	}
	stopSecond, err := sub.Subscribe("doc", second.add)
	if err != nil {
		t.Fatal(err)
	}

	stopFirst() // the document still has a handler: stay subscribed
	if err := pub.Publish(t.Context(), msg("doc", 1)); err != nil {
		t.Fatal(err)
	}
	if n := len(second.wait(t, 1)); n != 1 {
		t.Fatalf("remaining handler got %d messages", n)
	}

	stopSecond() // last handler: unsubscribe from Redis
	if err := pub.Publish(t.Context(), msg("doc", 2)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if n := len(first.wait(t, 0)) + len(second.wait(t, 0)); n != 1 {
		t.Errorf("handlers got %d messages in total, want 1", n)
	}
	if sub.r.subscribed() != 0 {
		t.Errorf("%d documents still subscribed", sub.r.subscribed())
	}
}

func TestNewRedisFailsFastWithoutRedis(t *testing.T) {
	if _, err := NewRedis(t.Context(), "127.0.0.1:1", slog.New(slog.NewTextHandler(io.Discard, nil))); err == nil {
		t.Error("NewRedis succeeded with nothing listening")
	}
}

// A presence update travels as the bytes the gateway encoded, untouched.
func TestRedisCarriesPresenceUpdates(t *testing.T) {
	pub, sub := newRedis(t), newRedis(t)
	var got inbox
	if _, err := sub.Subscribe("doc", got.add); err != nil {
		t.Fatal(err)
	}
	update := []byte(`{"type":"peer","peer":{"replica":"ann","name":"Ann","color":"#2f6fde"}}`)
	if err := pub.Publish(t.Context(), Message{DocID: "doc", Author: "ann", Presence: update}); err != nil {
		t.Fatal(err)
	}
	msgs := got.wait(t, 1)
	if len(msgs) != 1 || string(msgs[0].Presence) != string(update) || len(msgs[0].Ops) != 0 || msgs[0].Author != "ann" {
		t.Fatalf("got %+v", msgs)
	}
}
