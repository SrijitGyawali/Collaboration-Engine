// Package wire defines how CRDT operations look outside Go: as JSON on the
// WebSocket between browser and gateway, and as plain objects handed to
// JavaScript by the WebAssembly bridge. Both use the same field names, so an
// op looks the same everywhere it travels.
//
// An insert of "X" by alice, after a character typed by bob, looks like:
//
//	{"kind": "insert", "id": {"counter": 42, "replica": "alice"},
//	 "after": {"counter": 17, "replica": "bob"}, "value": "X"}
//
// and a delete like:
//
//	{"kind": "delete", "id": {"counter": 43, "replica": "alice"},
//	 "target": {"counter": 17, "replica": "bob"}}
//
// The package depends only on crdt and the standard library, so it compiles
// to WebAssembly too.
package wire

import (
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/SrijitGyawali/Collaboration-Engine/internal/crdt"
)

// ID is the wire form of crdt.ID.
type ID struct {
	Counter uint64 `json:"counter"`
	Replica string `json:"replica"`
}

// Op is the wire form of crdt.Op.
type Op struct {
	// Kind is "insert" or "delete".
	Kind string `json:"kind"`
	ID   ID     `json:"id"`
	// After is set for inserts that do not go at the document start.
	After *ID `json:"after,omitempty"`
	// Target is set for deletes.
	Target *ID `json:"target,omitempty"`
	// Value is, for inserts, exactly one character.
	Value string `json:"value,omitempty"`
}

// ErrMalformed is returned for a wire op that cannot be a valid crdt.Op.
var ErrMalformed = errors.New("wire: malformed operation")

// FromCRDT converts a crdt.Op to its wire form.
func FromCRDT(op crdt.Op) Op {
	w := Op{Kind: op.Kind.String(), ID: fromID(op.ID)}
	switch op.Kind {
	case crdt.OpInsert:
		if !op.After.IsRoot() {
			after := fromID(op.After)
			w.After = &after
		}
		w.Value = string(op.Value)
	case crdt.OpDelete:
		target := fromID(op.Target)
		w.Target = &target
	}
	return w
}

// ToCRDT converts a wire op back to a crdt.Op. It rejects anything a valid
// op could not have produced: an unknown kind, an insert whose value is not
// exactly one character, a delete without a target, or fields that fail
// crdt.Op.Validate. Input comes from the network, so nothing is trusted.
func (w Op) ToCRDT() (crdt.Op, error) {
	op := crdt.Op{ID: w.ID.toCRDT()}
	switch w.Kind {
	case "insert":
		op.Kind = crdt.OpInsert
		r, size := utf8.DecodeRuneInString(w.Value)
		if size == 0 || size != len(w.Value) || r == utf8.RuneError {
			return crdt.Op{}, fmt.Errorf("%w: insert value %q is not exactly one character", ErrMalformed, w.Value)
		}
		op.Value = r
		if w.After != nil {
			op.After = w.After.toCRDT()
		}
		if w.Target != nil {
			return crdt.Op{}, fmt.Errorf("%w: insert with a target", ErrMalformed)
		}
	case "delete":
		op.Kind = crdt.OpDelete
		if w.Target == nil {
			return crdt.Op{}, fmt.Errorf("%w: delete without a target", ErrMalformed)
		}
		op.Target = w.Target.toCRDT()
		if w.After != nil || w.Value != "" {
			return crdt.Op{}, fmt.Errorf("%w: delete with insert fields", ErrMalformed)
		}
	default:
		return crdt.Op{}, fmt.Errorf("%w: unknown kind %q", ErrMalformed, w.Kind)
	}
	if err := op.Validate(); err != nil {
		return crdt.Op{}, fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	return op, nil
}

func fromID(id crdt.ID) ID {
	return ID{Counter: id.Counter, Replica: id.Replica}
}

func (id ID) toCRDT() crdt.ID {
	return crdt.ID{Counter: id.Counter, Replica: id.Replica}
}
