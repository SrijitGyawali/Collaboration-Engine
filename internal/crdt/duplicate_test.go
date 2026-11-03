package crdt

import (
	"errors"
	"testing"
)

// mustApply applies op and fails the test unless Apply reports want.
func mustApply(t *testing.T, doc *Document, op Op, want Status) {
	t.Helper()
	got, err := doc.Apply(op)
	if err != nil {
		t.Fatalf("Apply(%v): %v", op.ID, err)
	}
	if got != want {
		t.Fatalf("Apply(%v) = %v, want %v", op.ID, got, want)
	}
}

func TestReapplyingInsertIsNoOp(t *testing.T) {
	alice := NewDocument("alice")
	bob := NewDocument("bob")
	ops := typeOps(t, alice, Root, "abc")

	// Bob receives every op twice, as after a client retry.
	for _, op := range ops {
		mustApply(t, bob, op, Applied)
		mustApply(t, bob, op, Duplicate)
	}

	if got := bob.Text(); got != "abc" {
		t.Errorf("Text() = %q, want %q", got, "abc")
	}
}

func TestReapplyingDeleteIsNoOp(t *testing.T) {
	alice := NewDocument("alice")
	bob := NewDocument("bob")
	ops := typeOps(t, alice, Root, "abc")
	applyAll(t, bob, ops)

	del, err := alice.Delete(ops[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	mustApply(t, bob, del, Applied)
	mustApply(t, bob, del, Duplicate)

	if got := bob.Text(); got != "ac" {
		t.Errorf("Text() = %q, want %q", got, "ac")
	}
}

// A replica applying its own op again (for example when the server echoes it
// back) must also see a duplicate.
func TestOwnOpEchoedBackIsDuplicate(t *testing.T) {
	doc := NewDocument("alice")
	op, err := doc.InsertAfter(Root, 'a')
	if err != nil {
		t.Fatal(err)
	}

	mustApply(t, doc, op, Duplicate)
}

func TestConflictingReuseOfIDIsRejected(t *testing.T) {
	doc := NewDocument("alice")
	ops := typeOps(t, doc, Root, "ab")
	del, err := doc.Delete(ops[0].ID)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		op   Op
	}{
		{"insert with another character", Op{Kind: OpInsert, ID: ops[1].ID, After: ops[0].ID, Value: 'z'}},
		{"insert with another parent", Op{Kind: OpInsert, ID: ops[1].ID, After: Root, Value: 'b'}},
		{"delete reusing an insert's id", Op{Kind: OpDelete, ID: ops[1].ID, Target: ops[0].ID}},
		{"delete with another target", Op{Kind: OpDelete, ID: del.ID, Target: ops[1].ID}},
		{"insert reusing a delete's id", Op{Kind: OpInsert, ID: del.ID, After: ops[1].ID, Value: 'x'}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, err := doc.Apply(tt.op)
			if !errors.Is(err, ErrConflictingOp) {
				t.Errorf("Apply() error = %v, want ErrConflictingOp", err)
			}
			if status != 0 {
				t.Errorf("Apply() status = %v, want none on error", status)
			}
		})
	}

	if got := doc.Text(); got != "b" {
		t.Errorf("Text() = %q, want %q", got, "b")
	}
}
