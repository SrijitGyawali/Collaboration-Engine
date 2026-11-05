package crdt_test

import (
	"fmt"

	"github.com/SrijitGyawali/Collaboration-Engine/internal/crdt"
)

// Two users edit the same spot at the same time and still end up with the
// same text.
func Example_concurrentEdits() {
	alice := crdt.NewDocument("alice")
	bob := crdt.NewDocument("bob")

	// Alice types "Hi" and Bob receives it.
	h, _ := alice.InsertAfter(crdt.Root, 'H')
	i, _ := alice.InsertAfter(h.ID, 'i')
	_, _ = bob.Apply(h)
	_, _ = bob.Apply(i)

	// At the same moment, both insert a character right after "H".
	x, _ := alice.InsertAfter(h.ID, 'X')
	y, _ := bob.InsertAfter(h.ID, 'Y')

	// They exchange edits, in opposite orders.
	_, _ = alice.Apply(y)
	_, _ = bob.Apply(x)

	fmt.Println(alice.Text(), bob.Text())
	// Output: HYXi HYXi
}

// An edit that arrives before the character it depends on waits, and is
// applied as soon as that character arrives.
func ExampleDocument_Apply_outOfOrder() {
	alice := crdt.NewDocument("alice")
	a, _ := alice.InsertAfter(crdt.Root, 'a')
	b, _ := alice.InsertAfter(a.ID, 'b')

	bob := crdt.NewDocument("bob")
	status, _ := bob.Apply(b) // 'b' overtakes 'a'
	fmt.Println(status, bob.PendingCount(), fmt.Sprintf("%q", bob.Text()))

	status, _ = bob.Apply(a)
	fmt.Println(status, bob.PendingCount(), fmt.Sprintf("%q", bob.Text()))
	// Output:
	// pending 1 ""
	// applied 0 "ab"
}

// Receiving the same edit twice is harmless.
func ExampleDocument_Apply_duplicate() {
	alice := crdt.NewDocument("alice")
	op, _ := alice.InsertAfter(crdt.Root, 'a')

	bob := crdt.NewDocument("bob")
	first, _ := bob.Apply(op)
	second, _ := bob.Apply(op)

	fmt.Println(first, second, bob.Text())
	// Output: applied duplicate a
}

// An editor works with positions; the CRDT turns them into position-free ops
// for the network, and back into positions for remote edits.
func ExampleDocument_InsertAt() {
	alice := crdt.NewDocument("alice")
	bob := crdt.NewDocument("bob")

	// Alice pastes a sentence, then fixes a typo by position.
	ops, _ := alice.InsertText(0, "helo world")
	fix, _ := alice.InsertAt(3, 'l')
	ops = append(ops, fix)

	// Bob receives the ops and asks where the fix landed, to update his view.
	for _, op := range ops {
		_, _ = bob.Apply(op)
	}
	pos, _, _ := bob.PositionOf(fix.ID)

	fmt.Println(bob.Text(), pos, bob.Len())
	// Output: hello world 3 11
}

// Deleting a selection by position.
func ExampleDocument_DeleteRange() {
	doc := crdt.NewDocument("alice")
	_, _ = doc.InsertText(0, "hello cruel world")

	ops, _ := doc.DeleteRange(5, 6) // " cruel"

	fmt.Println(doc.Text(), len(ops))
	// Output: hello world 6
}

// Deleted characters are hidden, but text typed after them stays in place.
func ExampleDocument_Delete() {
	doc := crdt.NewDocument("alice")
	c, _ := doc.InsertAfter(crdt.Root, 'c')
	a, _ := doc.InsertAfter(c.ID, 'a')
	r, _ := doc.InsertAfter(a.ID, 'r')
	_, _ = doc.InsertAfter(r.ID, 't')

	_, _ = doc.Delete(r.ID)

	fmt.Println(doc.Text())
	// Output: cat
}
