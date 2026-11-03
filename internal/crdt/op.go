package crdt

import (
	"errors"
	"fmt"
)

// OpKind says what an operation does.
type OpKind uint8

const (
	// OpInsert adds one character after an existing one (or after Root).
	OpInsert OpKind = iota + 1
	// OpDelete hides one existing character.
	OpDelete
)

func (k OpKind) String() string {
	switch k {
	case OpInsert:
		return "insert"
	case OpDelete:
		return "delete"
	default:
		return fmt.Sprintf("OpKind(%d)", uint8(k))
	}
}

// Op is one edit, in the form replicas send to each other. It never refers to
// positions, only to IDs, so it means the same thing on every replica no
// matter what else has changed in the document.
type Op struct {
	Kind OpKind
	// ID identifies the operation itself. For an insert it is also the ID of
	// the new character. Its String form is the op_id used for idempotency.
	ID ID
	// After is, for inserts, the character the new one goes right after.
	// Root means the start of the document.
	After ID
	// Target is, for deletes, the character to remove.
	Target ID
	// Value is, for inserts, the character itself.
	Value rune
}

// Dependency returns the character that must exist before op can be applied:
// the parent of an insert, or the target of a delete. It returns Root for an
// insert at the start of the document, which depends on nothing.
//
// Every op has at most one dependency. That keeps out-of-order delivery
// simple: an op that arrives early waits for exactly one character.
func (op Op) Dependency() ID {
	if op.Kind == OpDelete {
		return op.Target
	}
	return op.After
}

// ErrInvalidOp is returned for operations that are malformed on their own,
// before looking at any document.
var ErrInvalidOp = errors.New("crdt: invalid operation")

// Validate checks the fields an operation of its kind must have.
func (op Op) Validate() error {
	if op.ID.Counter == 0 || op.ID.Replica == "" {
		return fmt.Errorf("%w: missing id", ErrInvalidOp)
	}
	switch op.Kind {
	case OpInsert:
		if op.Value == 0 {
			return fmt.Errorf("%w: insert %v has no value", ErrInvalidOp, op.ID)
		}
		// A character can only follow something created before it.
		if !op.After.IsRoot() && op.After.Compare(op.ID) >= 0 {
			return fmt.Errorf("%w: insert %v is not newer than its parent %v", ErrInvalidOp, op.ID, op.After)
		}
	case OpDelete:
		if op.Target.IsRoot() {
			return fmt.Errorf("%w: delete %v has no target", ErrInvalidOp, op.ID)
		}
	default:
		return fmt.Errorf("%w: unknown kind %v", ErrInvalidOp, op.Kind)
	}
	return nil
}
