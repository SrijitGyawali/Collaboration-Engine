package crdt

import (
	"errors"
	"fmt"
)

// Positions are what an editor works with: "the cursor is at 5", "the user
// selected 3 to 8". The CRDT works with IDs. The functions in this file
// translate between the two. Positions count visible characters (runes) from
// 0; deleted characters have no position.

// ErrOutOfRange is returned for a position outside the text.
var ErrOutOfRange = errors.New("crdt: position out of range")

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
