// Package crdt implements a sequence CRDT (a simplified RGA) for plain text.
//
// # Model
//
// Every character is an element with a globally unique ID (a Lamport counter
// plus a replica name, see ID and Clock) and a reference to the character it
// was inserted after. The document is a tree of those references: reading it
// depth-first gives the text. Edits are Ops that name IDs, never positions,
// so an op means the same thing on every replica.
//
// # Guarantees
//
// Any two replicas that have received the same set of ops show the same
// text, no matter in what order, or how many times, each op arrived:
//
//   - Concurrent inserts after the same character are ordered by ID, the
//     same way everywhere.
//   - Deletes leave tombstones, so an insert after a deleted character still
//     finds its parent.
//   - An op that was already applied is reported as Duplicate and changes
//     nothing.
//   - An op whose dependency has not arrived is held back (Pending) and
//     applied automatically once it does, up to a limit (WithMaxPending).
//
// # Use
//
// Local edits go through Document.InsertAfter and Document.Delete, which
// return the Op to send to other replicas. Ops from other replicas go
// through Document.Apply.
//
// A Document is not safe for concurrent use: on the server each document is
// owned by one goroutine, and in the browser by one tab.
//
// The package has no dependencies outside the standard library, so it also
// compiles to WebAssembly for the browser.
package crdt
