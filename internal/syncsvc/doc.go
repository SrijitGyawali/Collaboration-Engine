// Package syncsvc implements the sync service, the only writer of document
// state.
//
// Each open document is owned by one goroutine (an actor) that holds the
// document's CRDT. Every edit goes through it, one at a time: it is validated
// against the CRDT (malformed, conflicting or unacceptable ops are rejected),
// then stored, and the caller gets one Ack per op with its sequence number.
// Retries are answered with the original sequence number. A document's actor
// starts on first use, rebuilding the CRDT from stored history, and stops
// after a period without requests.
package syncsvc
