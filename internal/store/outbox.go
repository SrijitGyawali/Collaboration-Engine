package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// TopicDocEvents is the Kafka topic for committed operations.
const TopicDocEvents = "doc-events"

// OutboxEvent is a message waiting to be published to Kafka.
type OutboxEvent struct {
	ID    int64
	Topic string
	// Key is the Kafka message key: the document ID, so one document's
	// events stay in order on one partition.
	Key       string
	Payload   json.RawMessage
	CreatedAt time.Time
}

// opEvent is the payload of a TopicDocEvents message: one committed operation.
type opEvent struct {
	DocID string          `json:"doc_id"`
	Seq   int64           `json:"seq"`
	OpID  string          `json:"op_id"`
	Op    json.RawMessage `json:"op"`
}

// UnpublishedEvents returns up to limit events not yet published, oldest
// first (never more than MaxPage).
func (s *Store) UnpublishedEvents(ctx context.Context, limit int) ([]OutboxEvent, error) {
	if limit <= 0 || limit > MaxPage {
		limit = MaxPage
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, topic, key, payload, created_at
		FROM outbox
		WHERE published_at IS NULL
		ORDER BY id
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("store: read outbox: %w", err)
	}

	events, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (OutboxEvent, error) {
		var e OutboxEvent
		err := row.Scan(&e.ID, &e.Topic, &e.Key, &e.Payload, &e.CreatedAt)
		return e, err
	})
	if err != nil {
		return nil, fmt.Errorf("store: read outbox: %w", err)
	}
	return events, nil
}

// MarkPublished records that Kafka has accepted the given events, so they are
// not returned again. If the relay crashes before calling it, the events are
// published a second time, which is why Kafka consumers deduplicate by op ID.
func (s *Store) MarkPublished(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE outbox SET published_at = now()
		WHERE id = ANY($1) AND published_at IS NULL`, ids)
	if err != nil {
		return fmt.Errorf("store: mark published: %w", err)
	}
	return nil
}
