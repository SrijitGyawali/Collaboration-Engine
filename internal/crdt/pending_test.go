package crdt

import (
	"errors"
	"slices"
	"testing"
)

func TestInsertBeforeItsParentWaits(t *testing.T) {
	alice := NewDocument("alice")
	bob := NewDocument("bob")
	ops := typeOps(t, alice, Root, "ab")

	// 'b' arrives before 'a', the character it goes after.
	mustApply(t, bob, ops[1], Pending)
	if got := bob.Text(); got != "" {
		t.Errorf("Text() = %q while waiting, want empty", got)
	}
	if got := bob.PendingCount(); got != 1 {
		t.Errorf("PendingCount() = %d, want 1", got)
	}

	// Once 'a' arrives, 'b' is applied with it.
	mustApply(t, bob, ops[0], Applied)
	if got := bob.Text(); got != "ab" {
		t.Errorf("Text() = %q, want %q", got, "ab")
	}
	if got := bob.PendingCount(); got != 0 {
		t.Errorf("PendingCount() = %d after release, want 0", got)
	}
}

func TestDeleteBeforeItsTargetWaits(t *testing.T) {
	alice := NewDocument("alice")
	bob := NewDocument("bob")
	ops := typeOps(t, alice, Root, "ab")
	del, err := alice.Delete(ops[0].ID)
	if err != nil {
		t.Fatal(err)
	}

	// The delete of 'a' overtakes the insert of 'a'.
	mustApply(t, bob, del, Pending)
	applyAll(t, bob, ops)

	if got := bob.Text(); got != "b" {
		t.Errorf("Text() = %q, want %q", got, "b")
	}
}

// A whole word delivered backwards: each character waits for the one before
// it, and the first one releases all of them in a cascade.
func TestWordDeliveredBackwards(t *testing.T) {
	alice := NewDocument("alice")
	bob := NewDocument("bob")
	ops := typeOps(t, alice, Root, "hello")

	reversed := slices.Clone(ops)
	slices.Reverse(reversed)
	for _, op := range reversed[:len(reversed)-1] {
		mustApply(t, bob, op, Pending)
	}
	mustApply(t, bob, reversed[len(reversed)-1], Applied)

	if got := bob.Text(); got != "hello" {
		t.Errorf("Text() = %q, want %q", got, "hello")
	}
	if got := bob.PendingCount(); got != 0 {
		t.Errorf("PendingCount() = %d, want 0", got)
	}
}

// Several ops can wait on the same missing character.
func TestSeveralOpsWaitForOneCharacter(t *testing.T) {
	alice := NewDocument("alice")
	bob := NewDocument("bob")
	base := typeOps(t, alice, Root, "a")
	x := typeOps(t, alice, base[0].ID, "x")
	del, _ := alice.Delete(base[0].ID)

	mustApply(t, bob, x[0], Pending)
	mustApply(t, bob, del, Pending)
	mustApply(t, bob, base[0], Applied)

	assertConverged(t, alice, bob)
	if got := bob.Text(); got != "x" {
		t.Errorf("Text() = %q, want %q", got, "x")
	}
}

// The same op arriving twice while it waits must be buffered once and applied
// once.
func TestRepeatWhileWaitingIsBufferedOnce(t *testing.T) {
	alice := NewDocument("alice")
	bob := NewDocument("bob")
	ops := typeOps(t, alice, Root, "ab")

	mustApply(t, bob, ops[1], Pending)
	mustApply(t, bob, ops[1], Pending)
	if got := bob.PendingCount(); got != 1 {
		t.Errorf("PendingCount() = %d, want 1", got)
	}

	mustApply(t, bob, ops[0], Applied)
	mustApply(t, bob, ops[1], Duplicate) // a third copy, after release
	if got := bob.Text(); got != "ab" {
		t.Errorf("Text() = %q, want %q", got, "ab")
	}
}

func TestConflictWhileWaitingIsRejected(t *testing.T) {
	alice := NewDocument("alice")
	bob := NewDocument("bob")
	ops := typeOps(t, alice, Root, "ab")

	mustApply(t, bob, ops[1], Pending)
	forged := ops[1]
	forged.Value = 'z'
	if _, err := bob.Apply(forged); !errors.Is(err, ErrConflictingOp) {
		t.Errorf("Apply(forged) = %v, want ErrConflictingOp", err)
	}

	mustApply(t, bob, ops[0], Applied)
	if got := bob.Text(); got != "ab" {
		t.Errorf("Text() = %q, want %q", got, "ab")
	}
}

// Local edits must name characters the replica has; they never wait.
func TestLocalEditsOfUnknownCharactersFail(t *testing.T) {
	doc := NewDocument("alice")

	if _, err := doc.InsertAfter(ID{5, "bob"}, 'x'); !errors.Is(err, ErrUnknownParent) {
		t.Errorf("InsertAfter() = %v, want ErrUnknownParent", err)
	}
	if _, err := doc.Delete(ID{5, "bob"}); !errors.Is(err, ErrUnknownTarget) {
		t.Errorf("Delete() = %v, want ErrUnknownTarget", err)
	}
	if _, err := doc.Delete(Root); !errors.Is(err, ErrUnknownTarget) {
		t.Errorf("Delete(Root) = %v, want ErrUnknownTarget", err)
	}
	if got := doc.PendingCount(); got != 0 {
		t.Errorf("PendingCount() = %d, want 0", got)
	}
}
