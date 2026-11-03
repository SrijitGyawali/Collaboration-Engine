package crdt

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

var (
	// ErrUnknownParent is returned when an insert names a parent character
	// this replica has not seen yet.
	ErrUnknownParent = errors.New("crdt: unknown parent")
	// ErrUnknownTarget is returned when a delete names a character this
	// replica has not seen yet.
	ErrUnknownTarget = errors.New("crdt: unknown target")
	// ErrDuplicateID is returned when an insert reuses the ID of a character
	// that already exists.
	ErrDuplicateID = errors.New("crdt: duplicate id")
)

// element is one character in the document.
type element struct {
	id    ID
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
// which reads "h!i".
//
// A Document is not safe for concurrent use.
type Document struct {
	clock *Clock
	// elements holds every character ever inserted, by ID, including
	// deleted ones.
	elements map[ID]*element
	// children lists, for each character (and Root), the characters inserted
	// directly after it, sorted by ID from highest to lowest.
	children map[ID][]ID
}

// NewDocument returns an empty document owned by the named replica. Each
// replica (browser tab, server process) must use a unique name.
func NewDocument(replica string) *Document {
	return &Document{
		clock:    NewClock(replica),
		elements: make(map[ID]*element),
		children: make(map[ID][]ID),
	}
}

// Replica returns the name of the replica that owns this copy.
func (d *Document) Replica() string {
	return d.clock.Replica()
}

// InsertAfter inserts value right after the character after (Root for the
// start of the document) and returns the operation to send to other
// replicas.
func (d *Document) InsertAfter(after ID, value rune) (Op, error) {
	op := Op{Kind: OpInsert, ID: d.clock.Next(), After: after, Value: value}
	if _, err := d.Apply(op); err != nil {
		return Op{}, err
	}
	return op, nil
}

// Delete removes the character target and returns the operation to send to
// other replicas.
func (d *Document) Delete(target ID) (Op, error) {
	op := Op{Kind: OpDelete, ID: d.clock.Next(), Target: target}
	if _, err := d.Apply(op); err != nil {
		return Op{}, err
	}
	return op, nil
}

// Apply applies an operation, local or from another replica, and reports
// what happened to it. On error the document is unchanged.
func (d *Document) Apply(op Op) (Status, error) {
	if err := op.Validate(); err != nil {
		return 0, err
	}
	var err error
	switch op.Kind {
	case OpInsert:
		err = d.applyInsert(op)
	case OpDelete:
		err = d.applyDelete(op)
	}
	if err != nil {
		return 0, err
	}
	// Seeing this op means our next local op must sort after it.
	d.clock.Observe(op.ID)
	return Applied, nil
}

func (d *Document) applyInsert(op Op) error {
	if _, exists := d.elements[op.ID]; exists {
		return fmt.Errorf("%w: %v", ErrDuplicateID, op.ID)
	}
	if !op.After.IsRoot() {
		if _, ok := d.elements[op.After]; !ok {
			return fmt.Errorf("%w: %v (inserting %v)", ErrUnknownParent, op.After, op.ID)
		}
	}
	d.elements[op.ID] = &element{id: op.ID, value: op.Value}
	d.children[op.After] = insertSorted(d.children[op.After], op.ID)
	return nil
}

// applyDelete turns the target into a tombstone instead of removing it.
//
// Removing it would break concurrent edits: if Bob inserts after a character
// at the same moment Alice deletes it, Bob's insert must still find its
// parent when it arrives. Keeping the tombstone also makes deletes
// commutative and safe to repeat: deleting twice, or in any order relative
// to other deletes, gives the same result.
func (d *Document) applyDelete(op Op) error {
	e, ok := d.elements[op.Target]
	if !ok {
		return fmt.Errorf("%w: %v (deleting with %v)", ErrUnknownTarget, op.Target, op.ID)
	}
	e.deleted = true
	return nil
}

// insertSorted adds id to siblings, a list of characters inserted after the
// same parent, keeping it sorted by ID from highest to lowest.
//
// This one rule makes concurrent inserts converge:
//
//   - A local insert always has the highest ID its replica has seen (Lamport
//     clock), so it lands first, right after its parent, where the user
//     typed it.
//   - Two inserts after the same parent made concurrently on different
//     replicas are ordered by ID on every replica, whatever order they
//     arrived in, so all replicas show the same text.
func insertSorted(siblings []ID, id ID) []ID {
	// Find the first sibling with a lower ID than id. Comparing as
	// id.Compare(s) (rather than s.Compare(id)) is what makes the order
	// descending.
	i, _ := slices.BinarySearchFunc(siblings, id, func(s, target ID) int {
		return target.Compare(s)
	})
	return slices.Insert(siblings, i, id)
}

// Text returns the current text of the document, without deleted characters.
//
// It walks the tree depth-first with an explicit stack rather than recursion.
// Ordinary typing builds a chain (each character is the child of the one
// before), so a 100,000-character document would otherwise recurse 100,000
// levels deep.
func (d *Document) Text() string {
	var b strings.Builder
	stack := d.pushChildren(nil, Root)
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		// Tombstones are skipped, but their children are not: text typed
		// after a deleted character is still part of the document.
		if e := d.elements[id]; !e.deleted {
			b.WriteRune(e.value)
		}
		stack = d.pushChildren(stack, id)
	}
	return b.String()
}

// pushChildren pushes the children of id onto stack in reverse, so the
// highest-ID child is popped, and therefore written, first.
func (d *Document) pushChildren(stack []ID, id ID) []ID {
	kids := d.children[id]
	for i := len(kids) - 1; i >= 0; i-- {
		stack = append(stack, kids[i])
	}
	return stack
}
