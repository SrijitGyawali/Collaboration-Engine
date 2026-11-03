package crdt

import "fmt"

// Status reports what Apply did with an operation.
//
// The sync service needs this to answer clients correctly: a new op gets a
// fresh sequence number, a Duplicate gets the ack it was given the first time,
// and a Pending op is acknowledged only once it is actually applied.
type Status uint8

const (
	// Applied means the op changed the document.
	Applied Status = iota + 1
	// Duplicate means the op had already been applied; nothing changed.
	Duplicate
	// Pending means the op arrived before the character it depends on. It
	// is held back and applied automatically once that character arrives.
	Pending
)

func (s Status) String() string {
	switch s {
	case Applied:
		return "applied"
	case Duplicate:
		return "duplicate"
	case Pending:
		return "pending"
	default:
		return fmt.Sprintf("Status(%d)", uint8(s))
	}
}
