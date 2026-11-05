package crdt

import "testing"

// checkInvariants fails the test if doc's internal structure is inconsistent.
// Text could look right while the structure underneath is broken (a character
// listed twice, a buffered op that should have been released), and such bugs
// only show up many edits later. Checking the structure directly catches
// them at the step that caused them.
func checkInvariants(t *testing.T, doc *Document) {
	t.Helper()

	// Every character is listed exactly once, under its own parent, and
	// siblings are sorted by ID from highest to lowest.
	listed := make(map[ID]bool, len(doc.elements))
	for parent, kids := range doc.children {
		if !doc.has(parent) {
			t.Errorf("children of unknown character %v", parent)
		}
		for i, id := range kids {
			e, ok := doc.elements[id]
			switch {
			case !ok:
				t.Errorf("child %v of %v is not an element", id, parent)
				continue
			case e.after != parent:
				t.Errorf("%v is listed under %v but its parent is %v", id, parent, e.after)
			case listed[id]:
				t.Errorf("%v is listed twice", id)
			}
			listed[id] = true
			if i > 0 && kids[i-1].Compare(id) <= 0 {
				t.Errorf("children of %v out of order: %v before %v", parent, kids[i-1], id)
			}
		}
	}
	for id, e := range doc.elements {
		if !listed[id] {
			t.Errorf("%v is not listed under any parent", id)
		}
		// A character is always newer than the one it follows.
		if !e.after.IsRoot() && e.after.Compare(id) >= 0 {
			t.Errorf("%v is not newer than its parent %v", id, e.after)
		}
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
