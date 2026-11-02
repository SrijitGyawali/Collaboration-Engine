package crdt

import "testing"

// applyAll applies ops to doc in order, failing the test on any error.
func applyAll(t *testing.T, doc *Document, ops []Op) {
	t.Helper()
	for _, op := range ops {
		if err := doc.Apply(op); err != nil {
			t.Fatalf("%s: Apply(%v): %v", doc.Replica(), op.ID, err)
		}
	}
}

// assertConverged fails the test unless every doc has the same text.
func assertConverged(t *testing.T, docs ...*Document) {
	t.Helper()
	want := docs[0].Text()
	for _, d := range docs[1:] {
		if got := d.Text(); got != want {
			t.Errorf("%s has %q, %s has %q", d.Replica(), got, docs[0].Replica(), want)
		}
	}
}

// TestConcurrentInsertsAtSamePosition is the classic conflict: both users see
// "Hi" and insert a character right after "H" at the same moment.
func TestConcurrentInsertsAtSamePosition(t *testing.T) {
	alice := NewDocument("alice")
	bob := NewDocument("bob")

	// Both start from the same "Hi", typed by alice.
	h, _ := alice.InsertAfter(Root, 'H')
	i, _ := alice.InsertAfter(h.ID, 'i')
	applyAll(t, bob, []Op{h, i})

	// Concurrently: alice inserts X after H, bob inserts Y after H. Both get
	// counter 3, so the replica name breaks the tie.
	x, err := alice.InsertAfter(h.ID, 'X')
	if err != nil {
		t.Fatal(err)
	}
	y, err := bob.InsertAfter(h.ID, 'Y')
	if err != nil {
		t.Fatal(err)
	}

	// Each receives the other's op, in opposite orders.
	applyAll(t, alice, []Op{y})
	applyAll(t, bob, []Op{x})

	assertConverged(t, alice, bob)
	// 3@bob sorts above 3@alice, and higher IDs come first after a parent.
	if got := alice.Text(); got != "HYXi" {
		t.Errorf("Text() = %q, want %q", got, "HYXi")
	}
}

// TestConcurrentWordsDoNotInterleave checks that two users typing whole words
// at the same spot get one word after the other, not mixed letters.
func TestConcurrentWordsDoNotInterleave(t *testing.T) {
	alice := NewDocument("alice")
	bob := NewDocument("bob")

	var aliceOps, bobOps []Op
	after := Root
	for _, r := range "cat" {
		op, _ := alice.InsertAfter(after, r)
		aliceOps = append(aliceOps, op)
		after = op.ID
	}
	after = Root
	for _, r := range "dog" {
		op, _ := bob.InsertAfter(after, r)
		bobOps = append(bobOps, op)
		after = op.ID
	}

	applyAll(t, alice, bobOps)
	applyAll(t, bob, aliceOps)

	assertConverged(t, alice, bob)
	if got := alice.Text(); got != "dogcat" && got != "catdog" {
		t.Errorf("Text() = %q, want the two words whole", got)
	}
}

// TestDeliveryOrderDoesNotMatter has three replicas edit concurrently, then
// delivers their edits to fresh replicas in every possible order. All of them
// must end with the same text.
func TestDeliveryOrderDoesNotMatter(t *testing.T) {
	base := NewDocument("base")
	baseOps := []Op{}
	after := Root
	for _, r := range "ab" {
		op, _ := base.InsertAfter(after, r)
		baseOps = append(baseOps, op)
		after = op.ID
	}
	a := baseOps[0].ID // the shared character all three edit next to

	// Each replica starts from "ab" and types two characters after 'a'.
	batches := make([][]Op, 3)
	for n, name := range []string{"r1", "r2", "r3"} {
		doc := NewDocument(name)
		applyAll(t, doc, baseOps)
		prev := a
		for _, r := range name[1:] + "!" {
			op, err := doc.InsertAfter(prev, r)
			if err != nil {
				t.Fatal(err)
			}
			batches[n] = append(batches[n], op)
			prev = op.ID
		}
	}

	var docs []*Document
	for _, order := range [][]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}} {
		doc := NewDocument("observer")
		applyAll(t, doc, baseOps)
		for _, n := range order {
			applyAll(t, doc, batches[n])
		}
		docs = append(docs, doc)
	}

	assertConverged(t, docs...)
}

// TestDeleteWhileOtherUserInsertsAfterIt is why deletes leave tombstones:
// alice deletes 'b' while bob, concurrently, types after it.
func TestDeleteWhileOtherUserInsertsAfterIt(t *testing.T) {
	alice := NewDocument("alice")
	bob := NewDocument("bob")
	base := typeOps(t, alice, Root, "abc")
	applyAll(t, bob, base)

	del, err := alice.Delete(base[1].ID) // alice deletes 'b'
	if err != nil {
		t.Fatal(err)
	}
	ins := typeOps(t, bob, base[1].ID, "XY") // bob types "XY" after 'b'

	applyAll(t, alice, ins)
	applyAll(t, bob, []Op{del})

	assertConverged(t, alice, bob)
	if got := alice.Text(); got != "aXYc" {
		t.Errorf("Text() = %q, want %q", got, "aXYc")
	}
}

// TestConcurrentDeletesOfSameCharacter: both users delete the same character
// and receive each other's delete.
func TestConcurrentDeletesOfSameCharacter(t *testing.T) {
	alice := NewDocument("alice")
	bob := NewDocument("bob")
	base := typeOps(t, alice, Root, "abc")
	applyAll(t, bob, base)

	d1, _ := alice.Delete(base[1].ID)
	d2, _ := bob.Delete(base[1].ID)
	applyAll(t, alice, []Op{d2})
	applyAll(t, bob, []Op{d1})

	assertConverged(t, alice, bob)
	if got := alice.Text(); got != "ac" {
		t.Errorf("Text() = %q, want %q", got, "ac")
	}
}
