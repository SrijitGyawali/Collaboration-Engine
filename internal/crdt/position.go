package crdt

import (
	"errors"
	"fmt"
)

// Positions are what an editor works with: "the cursor is at 5", "the user
// selected 3 to 8". The CRDT works with IDs. The functions in this file
// translate between the two. Positions count visible characters (runes) from
// 0; deleted characters have no position.

var (
	// ErrOutOfRange is returned for a position outside the text.
	ErrOutOfRange = errors.New("crdt: position out of range")
	// ErrUnknownID is returned when asked about a character this replica
	// does not have.
	ErrUnknownID = errors.New("crdt: unknown character")
)

// InsertAt inserts value so that it ends up at position pos, 0 <= pos <=
// Len() (Len() appends), and returns the op to send to other replicas.
//
// It finds the visible character just before pos and inserts after it, or
// after Root when pos is 0. That is enough to land exactly at pos: the new
// character gets the highest ID this replica has seen, so it goes straight
// after that character, ahead of any tombstones or older text that also
// follow it.
func (d *Document) InsertAt(pos int, value rune) (Op, error) {
	if pos < 0 || pos > d.visible {
		return Op{}, fmt.Errorf("%w: insert at %d (length %d)", ErrOutOfRange, pos, d.visible)
	}
	after := Root
	if pos > 0 {
		var err error
		if after, err = d.IDAt(pos - 1); err != nil {
			return Op{}, err
		}
	}
	return d.InsertAfter(after, value)
}

// DeleteAt deletes the character at position pos, 0 <= pos < Len(), and
// returns the op to send to other replicas.
func (d *Document) DeleteAt(pos int) (Op, error) {
	id, err := d.IDAt(pos)
	if err != nil {
		return Op{}, err
	}
	return d.Delete(id)
}

// PositionOf returns where character id is in the text: the number of
// visible characters before it. visible reports whether id itself is still
// in the text.
//
// The editor uses it after applying a remote op, to update its own view
// without re-rendering everything:
//
//   - remote insert of X: PositionOf(X) is where to insert it
//   - remote delete of X: X is now a tombstone, and PositionOf(X) is where
//     it was, so that is the character to remove (visible is false)
//
// Like IDAt, it counts from whichever end of the order list is closer.
func (d *Document) PositionOf(id ID) (pos int, visible bool, err error) {
	i := d.indexOf(id)
	if i < 0 {
		return 0, false, fmt.Errorf("%w: %v", ErrUnknownID, id)
	}
	visible = !d.order[i].deleted
	if i < len(d.order)/2 {
		for _, e := range d.order[:i] {
			if !e.deleted {
				pos++
			}
		}
		return pos, visible, nil
	}
	// Count what follows instead: everything visible that is not after id
	// is before it.
	pos = d.visible
	for _, e := range d.order[i:] {
		if !e.deleted {
			pos--
		}
	}
	return pos, visible, nil
}

// IDAt returns the ID of the character at position pos, 0 <= pos < Len().
//
// It walks the order list counting visible characters, from whichever end is
// closer, so a lookup near the end of the text (where most typing happens)
// is quick. Worst case O(n).
func (d *Document) IDAt(pos int) (ID, error) {
	if pos < 0 || pos >= d.visible {
		return ID{}, fmt.Errorf("%w: %d (length %d)", ErrOutOfRange, pos, d.visible)
	}
	if pos < d.visible/2 {
		n := 0
		for _, e := range d.order {
			if e.deleted {
				continue
			}
			if n == pos {
				return e.id, nil
			}
			n++
		}
	} else {
		n := d.visible - 1
		for i := len(d.order) - 1; i >= 0; i-- {
			e := d.order[i]
			if e.deleted {
				continue
			}
			if n == pos {
				return e.id, nil
			}
			n--
		}
	}
	// Unreachable while d.visible matches order (checked by the tests).
	panic(fmt.Sprintf("crdt: visible count %d out of sync with order", d.visible))
}
