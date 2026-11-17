// Package presence records who has each document open, and where their
// cursor is.
//
// Entries expire on their own unless refreshed: a gateway re-puts each of its
// editors every few seconds (a heartbeat), so an editor whose gateway crashed
// disappears after one TTL instead of staying "here" forever. Live changes
// (someone joins, moves their cursor, leaves) travel separately, through the
// document's pub/sub channel; the store is for a newcomer's first look at
// who is already there.
package presence

import (
	"context"
	"errors"
	"regexp"
	"time"

	"github.com/SrijitGyawali/Collaboration-Engine/internal/wire"
)

// Cursor is a selection, held by the characters it sits after rather than
// by index, so it stays in place while others edit. A nil ID means the
// start of the document. Anchor is the end that stays put while selecting,
// Head the end that moves; they are equal for a plain caret.
type Cursor struct {
	Anchor *wire.ID `json:"anchor"`
	Head   *wire.ID `json:"head"`
}

// Peer is one editor with the document open.
type Peer struct {
	Replica string  `json:"replica"`
	Name    string  `json:"name"`
	Color   string  `json:"color"`
	Cursor  *Cursor `json:"cursor,omitempty"`
}

// Store keeps the editors of each document, each entry expiring after the
// TTL it was last put with. Implementations are safe for concurrent use.
type Store interface {
	// Put adds or refreshes p, which expires ttl from now unless put again.
	Put(ctx context.Context, docID string, p Peer, ttl time.Duration) error
	// Remove deletes the entry of replica, if any.
	Remove(ctx context.Context, docID, replica string) error
	// List returns the document's unexpired entries, in no particular order.
	List(ctx context.Context, docID string) ([]Peer, error)
}

// ErrBadCursor is returned by Valid for a cursor that cannot name characters.
var ErrBadCursor = errors.New("presence: malformed cursor")

// replicaPattern matches the replica names gateways accept.
var replicaPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,32}$`)

// Valid checks that a cursor sent by a client names characters the way op
// IDs do. Whether those characters exist is not checked: they may not have
// reached every replica yet, and a client that does not know one just does
// not show that cursor.
func (c *Cursor) Valid() error {
	for _, id := range []*wire.ID{c.Anchor, c.Head} {
		if id != nil && (id.Counter == 0 || !replicaPattern.MatchString(id.Replica)) {
			return ErrBadCursor
		}
	}
	return nil
}
