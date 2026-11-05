package crdt

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

var (
	// ErrUnknownParent is returned when a local insert names a character
	// this replica does not have.
	ErrUnknownParent = errors.New("crdt: unknown parent")
	// ErrUnknownTarget is returned when a local delete names a character
	// this replica does not have.
	ErrUnknownTarget = errors.New("crdt: unknown target")
	// ErrConflictingOp is returned when an op reuses the ID of an op already
	// applied but says something different (another character, parent or
	// target). Honest replicas never do this, so it means corrupted input.
	ErrConflictingOp = errors.New("crdt: conflicting operation")
	// ErrTooManyPending is returned when an op would have to wait for its
	// dependency but the pending buffer is full (see WithMaxPending).
	ErrTooManyPending = errors.New("crdt: too many pending operations")
)

// element is one character in the document.
type element struct {
	id ID
	// after is the character this one was inserted after (its parent in
	// the tree). It lets a re-delivered insert be checked against the
	// original, and lets the tree be rebuilt from a snapshot later.
	after ID
	value rune
	// deleted marks a tombstone: the character is hidden from the text but
	// stays in the tree, because other replicas may still insert after it.
	deleted bool
}

// Document is one replica's copy of a text document.
//
// Internally the text is a tree. Every character is a child of the character
// it was inserted after, and Root is the top of the tree. Reading the tree
// depth-first (a character, then everything inserted after it) gives the
// text. For example, typing "hi" and then inserting "!" after "h" gives:
//
//	Root
//	└── h
//	    ├── !   (inserted after h, later)
//	    └── i
//
// which reads "h!i". Siblings (characters inserted after the same one) are
// ordered by ID, highest first; that one rule makes concurrent inserts
// converge (see insertPosition in order.go).
//
// The tree is stored flattened, as a list in reading order.
//
// A Document is not safe for concurrent use.
type Document struct {
	clock *Clock
	// elements holds every character ever inserted, by ID, including
	// deleted ones.
	elements map[ID]*element
	// order holds the same characters in the order they appear in the text,
	// tombstones included.
	order []*element
	// deletes records every applied delete: op ID -> target. Together with
	// elements (which records every applied insert) it answers "have I
	// already applied this op?".
	deletes map[ID]ID
	// pending holds ops that arrived before the character they depend on,
	// keyed by that missing character's ID.
	pending map[ID][]Op
	// waiting indexes the same ops by their own ID, so a repeat of an op
	// that is still waiting is recognised instead of buffered twice.
	waiting map[ID]Op
	// maxPending caps len(waiting).
	maxPending int
}

// NewDocument returns an empty document owned by the named replica. Each
// replica (browser tab, server process) must use a unique name.
func NewDocument(replica string, opts ...Option) *Document {
	d := &Document{
		clock:      NewClock(replica),
		elements:   make(map[ID]*element),
		deletes:    make(map[ID]ID),
		pending:    make(map[ID][]Op),
		waiting:    make(map[ID]Op),
		maxPending: DefaultMaxPending,
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// Replica returns the name of the replica that owns this copy.
func (d *Document) Replica() string {
	return d.clock.Replica()
}

// InsertAfter inserts value right after the character after (Root for the
// start of the document) and returns the operation to send to other
// replicas.
//
// Unlike remote ops, a local edit must name a character this replica already
// has (the user can only type next to what they see), so an unknown after
// is an error rather than something to wait for.
func (d *Document) InsertAfter(after ID, value rune) (Op, error) {
	if !d.has(after) {
		return Op{}, fmt.Errorf("%w: %v", ErrUnknownParent, after)
	}
	op := Op{Kind: OpInsert, ID: d.clock.Next(), After: after, Value: value}
	if _, err := d.Apply(op); err != nil {
		return Op{}, err
	}
	return op, nil
}

// Delete removes the character target and returns the operation to send to
// other replicas. Like InsertAfter, it fails if target is unknown.
func (d *Document) Delete(target ID) (Op, error) {
	if target.IsRoot() || !d.has(target) {
		return Op{}, fmt.Errorf("%w: %v", ErrUnknownTarget, target)
	}
	op := Op{Kind: OpDelete, ID: d.clock.Next(), Target: target}
	if _, err := d.Apply(op); err != nil {
		return Op{}, err
	}
	return op, nil
}

// Apply applies an operation, local or from another replica, and reports
// what happened to it. On error the document is unchanged.
//
// Ops may arrive in any order. One that depends on a character this replica
// has not seen yet is held back and reported as Pending; it is applied
// automatically, together with anything waiting on it, as soon as that
// character arrives.
func (d *Document) Apply(op Op) (Status, error) {
	if err := op.Validate(); err != nil {
		return 0, err
	}
	seen, err := d.alreadyApplied(op)
	if err != nil {
		return 0, err
	}
	if seen {
		return Duplicate, nil
	}
	if w, ok := d.waiting[op.ID]; ok {
		// Already buffered: a repeat changes nothing, a different op under
		// the same ID is corrupt.
		if w != op {
			return 0, fmt.Errorf("%w: %v is already waiting as a different op", ErrConflictingOp, op.ID)
		}
		return Pending, nil
	}

	dep := op.Dependency()
	mustWait := !d.has(dep)
	if mustWait && len(d.waiting) >= d.maxPending {
		return 0, fmt.Errorf("%w: %d waiting, rejecting %v", ErrTooManyPending, len(d.waiting), op.ID)
	}

	// Seeing this op means our next local op must sort after it.
	d.clock.Observe(op.ID)

	if mustWait {
		d.pending[dep] = append(d.pending[dep], op)
		d.waiting[op.ID] = op
		return Pending, nil
	}
	d.applyReady(op)
	d.releasePending(op)
	return Applied, nil
}

// PendingCount returns how many received ops are waiting for a character
// that has not arrived yet.
func (d *Document) PendingCount() int {
	return len(d.waiting)
}

// has reports whether the character id exists. Root always exists.
func (d *Document) has(id ID) bool {
	if id.IsRoot() {
		return true
	}
	_, ok := d.elements[id]
	return ok
}

// applyReady applies an op whose dependency is known to exist.
func (d *Document) applyReady(op Op) {
	switch op.Kind {
	case OpInsert:
		d.applyInsert(op)
	case OpDelete:
		d.applyDelete(op)
	}
}

// releasePending applies every buffered op that was waiting for the character
// op just inserted. Releasing one insert can unblock others (a whole word
// that arrived backwards), so it works through them with a stack until
// nothing more is ready. Deletes unblock nothing: no op depends on a delete.
func (d *Document) releasePending(op Op) {
	if op.Kind != OpInsert {
		return
	}
	ready := []ID{op.ID}
	for len(ready) > 0 {
		id := ready[len(ready)-1]
		ready = ready[:len(ready)-1]

		released := d.pending[id]
		delete(d.pending, id)
		// Each op is buffered at most once (see waiting), so every released
		// op is new and is applied exactly once.
		for _, w := range released {
			delete(d.waiting, w.ID)
			d.applyReady(w)
			if w.Kind == OpInsert {
				ready = append(ready, w.ID)
			}
		}
	}
}

// alreadyApplied reports whether op was applied before. Networks retry, so
// the same op routinely arrives twice (a client resends because it never saw
// the ack, Redis and Kafka both deliver at least once). A repeat must be a
// harmless no-op, not an error.
//
// It returns ErrConflictingOp if the ID is known but the op differs from the
// one applied under that ID.
func (d *Document) alreadyApplied(op Op) (bool, error) {
	if e, ok := d.elements[op.ID]; ok {
		if op.Kind == OpInsert && op.After == e.after && op.Value == e.value {
			return true, nil
		}
		return false, fmt.Errorf("%w: %v was an insert of %q after %v", ErrConflictingOp, op.ID, e.value, e.after)
	}
	if target, ok := d.deletes[op.ID]; ok {
		if op.Kind == OpDelete && op.Target == target {
			return true, nil
		}
		return false, fmt.Errorf("%w: %v was a delete of %v", ErrConflictingOp, op.ID, target)
	}
	return false, nil
}

func (d *Document) applyInsert(op Op) {
	e := &element{id: op.ID, after: op.After, value: op.Value}
	d.elements[op.ID] = e
	d.order = slices.Insert(d.order, d.insertPosition(op), e)
}

// applyDelete turns the target into a tombstone instead of removing it.
//
// Removing it would break concurrent edits: if Bob inserts after a character
// at the same moment Alice deletes it, Bob's insert must still find its
// parent when it arrives. Keeping the tombstone also makes deletes
// commutative and safe to repeat: deleting twice, or in any order relative
// to other deletes, gives the same result.
func (d *Document) applyDelete(op Op) {
	d.elements[op.Target].deleted = true
	d.deletes[op.ID] = op.Target
}

// Text returns the current text of the document, without deleted characters.
func (d *Document) Text() string {
	var b strings.Builder
	for _, e := range d.order {
		if !e.deleted {
			b.WriteRune(e.value)
		}
	}
	return b.String()
}
