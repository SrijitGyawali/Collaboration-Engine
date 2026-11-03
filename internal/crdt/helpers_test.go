package crdt

import "testing"

// typeOps types s on doc one character at a time, starting after the given
// ID, the way a user typing would. It returns the ops in typing order, ready
// to send to other replicas.
func typeOps(t *testing.T, doc *Document, after ID, s string) []Op {
	t.Helper()
	var ops []Op
	for _, r := range s {
		op, err := doc.InsertAfter(after, r)
		if err != nil {
			t.Fatalf("InsertAfter(%v, %q): %v", after, r, err)
		}
		ops = append(ops, op)
		after = op.ID
	}
	return ops
}

// applyAll applies ops to doc in order, as if they arrived from another
// replica, failing the test on any error.
func applyAll(t *testing.T, doc *Document, ops []Op) {
	t.Helper()
	for _, op := range ops {
		if _, err := doc.Apply(op); err != nil {
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
