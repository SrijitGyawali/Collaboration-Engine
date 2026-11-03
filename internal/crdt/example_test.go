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
