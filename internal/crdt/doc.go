// Package crdt implements a sequence CRDT (a simplified RGA) for plain text.
//
// Every character is an element with a globally unique ID (a Lamport counter
// plus a replica ID) and a reference to the element it was inserted after.
// Concurrent inserts after the same element are ordered by ID, deletes leave
// tombstones, and operations are idempotent by op ID, so every replica that
// has seen the same set of operations holds the same text, whatever order
// they arrived in.
//
// The package has no dependencies outside the standard library so it can be
// compiled to WebAssembly for the browser.
package crdt
