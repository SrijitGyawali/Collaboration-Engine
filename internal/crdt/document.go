package crdt

import (
	"errors"
	"fmt"
	"strings"
)

var (
	// ErrUnknownParent is returned when an insert names a parent character
	// this replica has not seen yet.
	ErrUnknownParent = errors.New("crdt: unknown parent")
	// ErrDuplicateID is returned when an insert reuses the ID of a character
	// that already exists.
	ErrDuplicateID = errors.New("crdt: duplicate id")
)

// element is one character in the document.
type element struct {
	id    ID
	value rune
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
	// elements holds every character ever inserted, by ID.
	elements map[ID]*element
	// children lists, for each character (and Root), the characters inserted
	// directly after it.
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
	if err := d.Apply(op); err != nil {
		return Op{}, err
	}
	return op, nil
}

// Apply applies an operation, local or from another replica.
func (d *Document) Apply(op Op) error {
	if err := op.Validate(); err != nil {
		return err
	}
	switch op.Kind {
	case OpInsert:
		if err := d.applyInsert(op); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%w: %v is not supported yet", ErrInvalidOp, op.Kind)
	}
	// Seeing this op means our next local op must sort after it.
	d.clock.Observe(op.ID)
	return nil
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
	// The new character goes right after its parent, so it comes before any
	// older siblings: put it first. This is right for a single replica, but
	// two replicas that receive concurrent inserts in different orders can
	// still disagree; ordering siblings by ID fixes that next.
	d.children[op.After] = append([]ID{op.ID}, d.children[op.After]...)
	return nil
}

// Text returns the current text of the document.
func (d *Document) Text() string {
	var b strings.Builder
	d.writeSubtree(&b, Root)
	return b.String()
}

// writeSubtree writes every character inserted after id, depth-first.
func (d *Document) writeSubtree(b *strings.Builder, id ID) {
	for _, child := range d.children[id] {
		b.WriteRune(d.elements[child].value)
		d.writeSubtree(b, child)
	}
}
