package presence

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// key is the Redis hash holding a document's editors: one field per
// replica, its value the Peer as JSON.
func key(docID string) string {
	return "collab:doc:" + docID + ":presence"
}

// Redis keeps presence in a Redis hash per document, shared by every
// gateway. Each field has its own TTL (HPEXPIRE, Redis 7.4 and later), so one
// editor's entry expires without touching the others', and the hash
// disappears by itself once its last field has expired.
type Redis struct {
	client *redis.Client
}

// NewRedis connects to Redis at addr ("host:port" or a redis:// URL) and
// checks that it answers.
func NewRedis(ctx context.Context, addr string) (*Redis, error) {
	opts := &redis.Options{Addr: addr}
	if strings.Contains(addr, "://") {
		var err error
		if opts, err = redis.ParseURL(addr); err != nil {
			return nil, fmt.Errorf("presence: %w", err)
		}
	}
	client := redis.NewClient(opts)
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("presence: ping %s: %w", addr, err)
	}
	return &Redis{client: client}, nil
}

// Put implements Store: the field is written and given its TTL in one
// transaction, so there is never an entry that would not expire.
func (x *Redis) Put(ctx context.Context, docID string, p Peer, ttl time.Duration) error {
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = x.client.TxPipelined(ctx, func(tx redis.Pipeliner) error {
		tx.HSet(ctx, key(docID), p.Replica, data)
		tx.HPExpire(ctx, key(docID), ttl, p.Replica)
		return nil
	})
	if err != nil {
		return fmt.Errorf("presence: put %s in %q: %w", p.Replica, docID, err)
	}
	return nil
}

// Remove implements Store.
func (x *Redis) Remove(ctx context.Context, docID, replica string) error {
	if err := x.client.HDel(ctx, key(docID), replica).Err(); err != nil {
		return fmt.Errorf("presence: remove %s from %q: %w", replica, docID, err)
	}
	return nil
}

// List implements Store. Redis never returns expired fields.
func (x *Redis) List(ctx context.Context, docID string) ([]Peer, error) {
	fields, err := x.client.HGetAll(ctx, key(docID)).Result()
	if err != nil {
		return nil, fmt.Errorf("presence: list %q: %w", docID, err)
	}
	peers := make([]Peer, 0, len(fields))
	for _, v := range fields {
		var p Peer
		if err := json.Unmarshal([]byte(v), &p); err != nil {
			continue // written by something else; not ours to show
		}
		peers = append(peers, p)
	}
	return peers, nil
}

// Close closes the connection pool.
func (x *Redis) Close() error {
	return x.client.Close()
}
