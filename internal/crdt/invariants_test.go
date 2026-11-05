package crdt

import (
	"slices"
	"strings"
	"testing"
)

// referenceOrder computes the correct order of all characters (tombstones
// included) from first principles, using only each element's parent and ID:
// a depth-first walk of the "inserted after" tree, siblings sorted from
// highest ID to lowest. It shares no code with Document's own ordering, so it
// can check it, including after Document's storage is rewritten.
func referenceOrder(doc *Document) []ID {
	kids := make(map[ID][]ID)
	for id, e := range doc.elements {
		kids[e.after] = append(kids[e.after], id)
	}
	for _, k := range kids {
		slices.SortFunc(k, func(a, b ID) int { return b.Compare(a) })
	}
	var order []ID
	var walk func(ID)
	walk = func(id ID) {
		for _, c := range kids[id] {
			order = append(order, c)
			walk(c)
		}
	}
	walk(Root)
	return order
}

// referenceText is the text referenceOrder implies.
func referenceText(doc *Document) string {
	var b strings.Builder
	for _, id := range referenceOrder(doc) {
		if e := doc.elements[id]; !e.deleted {
			b.WriteRune(e.value)
		}
	}
	return b.String()
}

// checkInvariants fails the test if doc's internal structure is inconsistent.
// Text could look right while the structure underneath is broken (a character
// listed twice, a buffered op that should have been released), and such bugs
// only show up many edits later. Checking the structure directly catches
// them at the step that caused them.
func checkInvariants(t *testing.T, doc *Document) {
	t.Helper()

	// The order list holds every character exactly once, and the same
	// element the ID map points to.
	if len(doc.order) != len(doc.elements) {
		t.Errorf("order has %d characters, elements has %d", len(doc.order), len(doc.elements))
	}
	listed := make(map[ID]bool, len(doc.order))
	for _, e := range doc.order {
		if listed[e.id] {
			t.Errorf("%v is in order twice", e.id)
		}
		listed[e.id] = true
		if doc.elements[e.id] != e {
			t.Errorf("%v in order is not the element stored under its ID", e.id)
		}
	}
	for id, e := range doc.elements {
		// A character is always newer than the one it follows, and its
		// parent exists.
		if !e.after.IsRoot() && e.after.Compare(id) >= 0 {
			t.Errorf("%v is not newer than its parent %v", id, e.after)
		}
		if !doc.has(e.after) {
			t.Errorf("parent %v of %v does not exist", e.after, id)
		}
	}

	// The order, and therefore the text, matches the order computed
	// independently from the parent links.
	if want := referenceOrder(doc); len(want) == len(doc.order) {
		for i, e := range doc.order {
			if e.id != want[i] {
				t.Errorf("order[%d] = %v, reference model says %v", i, e.id, want[i])
				break
			}
		}
	}
	if got, want := doc.Text(), referenceText(doc); got != want {
		t.Errorf("Text() = %q, reference model says %q", got, want)
	}

	// Every applied delete hid a character that exists.
	for opID, target := range doc.deletes {
		e, ok := doc.elements[target]
		if !ok || !e.deleted {
			t.Errorf("delete %v of %v did not leave a tombstone", opID, target)
		}
	}

	// The pending buffer and its index agree, and nothing is waiting for a
	// character that is already here (it would never be released).
	buffered := 0
	for dep, ops := range doc.pending {
		if doc.has(dep) {
			t.Errorf("%d ops still wait for %v, which exists", len(ops), dep)
		}
		for _, op := range ops {
			if op.Dependency() != dep {
				t.Errorf("%v waits under %v but depends on %v", op.ID, dep, op.Dependency())
			}
			if w, ok := doc.waiting[op.ID]; !ok || w != op {
				t.Errorf("%v is buffered but not indexed as waiting", op.ID)
			}
		}
		buffered += len(ops)
	}
	if buffered != len(doc.waiting) {
		t.Errorf("%d ops buffered but %d indexed as waiting", buffered, len(doc.waiting))
	}
}

func TestInvariantsOnEmptyDocument(t *testing.T) {
	checkInvariants(t, NewDocument("alice"))
}
