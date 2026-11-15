package pubsub

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// subscribeTimeout bounds how long Subscribe waits for Redis to confirm.
const subscribeTimeout = 5 * time.Second

// channel is the Redis channel for a document's operations.
func channel(docID string) string {
	return "collab:doc:" + docID + ":ops"
}

// Redis publishes and subscribes through Redis Pub/Sub. One instance holds
// one connection for publishing (pooled) and one subscriber connection shared
// by every document this process subscribes to.
type Redis struct {
	client *redis.Client
	ps     *redis.PubSub
	log    *slog.Logger
	r      router

	// mu orders subscribe and unsubscribe commands with the router's view
	// of which documents have handlers.
	mu sync.Mutex
	// confirms holds, per channel, the Subscribe calls waiting for Redis to
	// confirm the subscription.
	confirmMu sync.Mutex
	confirms  map[string][]chan struct{}
	done      chan struct{}
}

// NewRedis connects to Redis at addr ("host:port" or a redis:// URL) and
// checks that it answers.
func NewRedis(ctx context.Context, addr string, log *slog.Logger) (*Redis, error) {
	opts := &redis.Options{Addr: addr}
	if strings.Contains(addr, "://") {
		var err error
		if opts, err = redis.ParseURL(addr); err != nil {
			return nil, fmt.Errorf("pubsub: %w", err)
		}
	}
	client := redis.NewClient(opts)
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("pubsub: ping %s: %w", addr, err)
	}

	x := &Redis{
		client:   client,
		ps:       client.Subscribe(ctx),
		log:      log,
		confirms: make(map[string][]chan struct{}),
		done:     make(chan struct{}),
	}
	go x.receive()
	return x, nil
}

// Publish sends msg to every process subscribed to its document.
func (x *Redis) Publish(ctx context.Context, msg Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return x.client.Publish(ctx, channel(msg.DocID), data).Err()
}

// Subscribe calls fn for every message published for docID until the
// returned function is called. When it returns, Redis has confirmed the
// subscription, so anything published from then on is delivered.
func (x *Redis) Subscribe(docID string, fn func(Message)) (unsubscribe func(), err error) {
	x.mu.Lock()
	defer x.mu.Unlock()

	id, first := x.r.add(docID, fn)
	if first {
		if err := x.subscribe(channel(docID)); err != nil {
			x.r.remove(docID, id)
			return nil, err
		}
	}
	return func() {
		x.mu.Lock()
		defer x.mu.Unlock()
		if x.r.remove(docID, id) {
			ctx, cancel := context.WithTimeout(context.Background(), subscribeTimeout)
			defer cancel()
			if err := x.ps.Unsubscribe(ctx, channel(docID)); err != nil {
				x.log.Warn("pubsub: unsubscribe failed", "doc", docID, "err", err)
			}
		}
	}, nil
}

// subscribe sends SUBSCRIBE and waits for Redis to confirm it. Without the
// wait, a message published right after Subscribe returned could reach Redis
// before the subscription did, and be lost.
func (x *Redis) subscribe(ch string) error {
	confirmed := make(chan struct{})
	x.confirmMu.Lock()
	x.confirms[ch] = append(x.confirms[ch], confirmed)
	x.confirmMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), subscribeTimeout)
	defer cancel()
	if err := x.ps.Subscribe(ctx, ch); err != nil {
		return fmt.Errorf("pubsub: subscribe %s: %w", ch, err)
	}
	select {
	case <-confirmed:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("pubsub: subscribe %s: not confirmed: %w", ch, ctx.Err())
	}
}

// receive reads the subscriber connection until it closes: subscription
// confirmations wake waiting Subscribe calls, and messages go to their
// document's handlers. go-redis reconnects and resubscribes by itself if the
// connection drops; messages published meanwhile are lost, which clients
// recover from by sequence number.
func (x *Redis) receive() {
	defer close(x.done)
	for v := range x.ps.ChannelWithSubscriptions(redis.WithChannelSize(1024)) {
		switch m := v.(type) {
		case *redis.Subscription:
			if m.Kind == "subscribe" {
				x.confirmMu.Lock()
				for _, c := range x.confirms[m.Channel] {
					close(c)
				}
				delete(x.confirms, m.Channel)
				x.confirmMu.Unlock()
			}
		case *redis.Message:
			var msg Message
			if err := json.Unmarshal([]byte(m.Payload), &msg); err != nil {
				x.log.Warn("pubsub: ignoring a malformed message", "channel", m.Channel, "err", err)
				continue
			}
			x.r.dispatch(msg)
		}
	}
}

// Close stops receiving and closes both connections.
func (x *Redis) Close() error {
	err := x.ps.Close()
	<-x.done
	if cerr := x.client.Close(); err == nil {
		err = cerr
	}
	return err
}
