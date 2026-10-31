// Package syncsvc implements the sync service: one goroutine per active
// document owns its CRDT state, assigns sequence numbers, deduplicates by
// op_id and commits each operation with its outbox row in one transaction.
package syncsvc
