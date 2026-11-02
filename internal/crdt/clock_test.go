package crdt

import "testing"

func TestClockNext(t *testing.T) {
	c := NewClock("alice")

	first, second := c.Next(), c.Next()

	if first != (ID{1, "alice"}) {
		t.Errorf("first ID = %v, want 1@alice", first)
	}
	if second != (ID{2, "alice"}) {
		t.Errorf("second ID = %v, want 2@alice", second)
	}
}

func TestClockObserve(t *testing.T) {
	c := NewClock("alice")
	c.Next() // alice is at 1

	// Seeing bob's operation at 10 must push alice past it.
	c.Observe(ID{10, "bob"})
	if got := c.Next(); got != (ID{11, "alice"}) {
		t.Errorf("after observing 10@bob, Next() = %v, want 11@alice", got)
	}

	// An older ID must not move the clock backwards.
	c.Observe(ID{3, "bob"})
	if got := c.Next(); got != (ID{12, "alice"}) {
		t.Errorf("after observing 3@bob, Next() = %v, want 12@alice", got)
	}
}
