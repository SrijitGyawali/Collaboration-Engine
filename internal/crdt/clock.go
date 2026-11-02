package crdt

// Clock is a Lamport clock for one replica. It hands out IDs for new local
// operations.
//
// The rule is simple: every new operation gets a counter one higher than
// anything this replica has seen so far, its own operations and everyone
// else's. So if Alice saw Bob's character before typing, her character gets
// a higher counter than his. That "seen before" relationship is what the
// CRDT relies on to keep sequential typing in order.
//
// A Clock is not safe for concurrent use; each document owns one and is
// driven by a single goroutine.
type Clock struct {
	replica string
	counter uint64
}

// NewClock returns a clock for the named replica, starting at zero.
func NewClock(replica string) *Clock {
	return &Clock{replica: replica}
}

// Replica returns the name of the replica that owns the clock.
func (c *Clock) Replica() string {
	return c.replica
}

// Next advances the clock and returns a fresh ID for a local operation.
func (c *Clock) Next() ID {
	c.counter++
	return ID{Counter: c.counter, Replica: c.replica}
}

// Observe moves the clock forward past id, an ID received from another
// replica, so the next local ID sorts after it. Observing an older ID does
// nothing.
func (c *Clock) Observe(id ID) {
	c.counter = max(c.counter, id.Counter)
}
