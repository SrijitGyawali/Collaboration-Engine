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
// The tree is stored flattened, as a list of characters in reading order
// (tombstones included). New characters are placed with RGA's rule: after
// the parent, skipping every character with a higher ID.
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
// Local edits return the Op (or Ops) to send to other replicas. An editor
// uses the position API: Document.InsertAt, Document.InsertText,
// Document.DeleteAt and Document.DeleteRange, with Document.Len for bounds.
// Document.InsertAfter and Document.Delete do the same by ID.
//
// Ops from other replicas go through Document.Apply. Document.PositionOf
// then says where a remote edit landed, so the editor can update its view
// in place, and Document.IDAt maps a position back to an ID.
//
// # Cost
//
// Typing at the end and applying ops in typing order are O(1) per character
// to locate (the document remembers where the last insert went). Position
// lookups and jumps elsewhere scan the list, O(n); on a 10,000-character
// document that is microseconds per edit.
//
// A Document is not safe for concurrent use: on the server each document is
// owned by one goroutine, and in the browser by one tab.
//
// The package has no dependencies outside the standard library, so it also
// compiles to WebAssembly for the browser.
package crdt
