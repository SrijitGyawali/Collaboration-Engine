// Package store is the PostgreSQL data layer: the append-only operations log,
// document snapshots and the transactional outbox. PostgreSQL is the source
// of truth; Redis and Kafka only carry copies of what is committed here.
package store
