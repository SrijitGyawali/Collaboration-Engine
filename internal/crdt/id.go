package crdt

import (
	"cmp"
	"fmt"
)

// ID identifies one operation, and the character it inserts, for its whole
// life. It never changes, even when other characters are inserted or deleted
// around it, which is what lets replicas refer to "the character after X"
// without using positions that shift.
//
// IDs are unique because each replica uses its own Replica string and never
// reuses a Counter value. They are totally ordered (see Compare), so every
// replica can break ties the same way.
type ID struct {
	// Counter is the Lamport timestamp of the operation (see Clock).
	Counter uint64
	// Replica is the unique name of the replica (one browser tab or one
	// server process) that created the operation.
	Replica string
}

// Root is the zero ID. It stands for the start of the document: inserting
// "after Root" puts a character at the very beginning. No real operation has
// a zero Counter, so Root never collides with a real ID.
var Root = ID{}

// IsRoot reports whether id is the document start.
func (id ID) IsRoot() bool {
	return id == Root
}

// Compare orders IDs by Counter, then by Replica. It returns -1 if id sorts
// before other, 0 if they are equal, and +1 if id sorts after other.
//
// The order means "created later wins ties": a higher Counter usually means
// the operation was created after its author saw the other one. When two
// replicas pick the same Counter concurrently, Replica breaks the tie, so
// all replicas agree without talking to each other.
func (id ID) Compare(other ID) int {
	if c := cmp.Compare(id.Counter, other.Counter); c != 0 {
		return c
	}
	return cmp.Compare(id.Replica, other.Replica)
}

// String formats id as "counter@replica", for logs and as an op_id.
func (id ID) String() string {
	if id.IsRoot() {
		return "root"
	}
	return fmt.Sprintf("%d@%s", id.Counter, id.Replica)
}
