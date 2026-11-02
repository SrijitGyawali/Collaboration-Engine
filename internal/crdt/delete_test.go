package crdt

import (
	"errors"
	"testing"
)

func TestDeleteHidesCharacter(t *testing.T) {
	doc := NewDocument("alice")
	ops := typeOps(t, doc, Root, "cart")

	if _, err := doc.Delete(ops[2].ID); err != nil { // the 'r'
		t.Fatal(err)
	}

	if got := doc.Text(); got != "cat" {
		t.Errorf("Text() = %q, want %q", got, "cat")
	}
}

func TestDeleteEverything(t *testing.T) {
	doc := NewDocument("alice")
	for _, op := range typeOps(t, doc, Root, "abc") {
		if _, err := doc.Delete(op.ID); err != nil {
			t.Fatal(err)
		}
	}

	if got := doc.Text(); got != "" {
		t.Errorf("Text() = %q, want empty", got)
	}
}

// Text typed after a character must survive when that character is deleted.
func TestInsertAfterDeletedCharacter(t *testing.T) {
	doc := NewDocument("alice")
	ops := typeOps(t, doc, Root, "ab")

	if _, err := doc.Delete(ops[0].ID); err != nil { // delete 'a'
		t.Fatal(err)
	}
	// Inserting after the tombstone still works and keeps its place.
	if _, err := doc.InsertAfter(ops[0].ID, 'x'); err != nil {
		t.Fatal(err)
	}

	if got := doc.Text(); got != "xb" {
		t.Errorf("Text() = %q, want %q", got, "xb")
	}
}

func TestDeleteTwiceIsHarmless(t *testing.T) {
	doc := NewDocument("alice")
	ops := typeOps(t, doc, Root, "ab")

	for range 2 {
		if _, err := doc.Delete(ops[0].ID); err != nil {
			t.Fatalf("Delete: %v", err)
		}
	}

	if got := doc.Text(); got != "b" {
		t.Errorf("Text() = %q, want %q", got, "b")
	}
}

func TestDeleteUnknownTarget(t *testing.T) {
	doc := NewDocument("alice")
	typeOps(t, doc, Root, "ab")

	_, err := doc.Delete(ID{99, "bob"})
	if !errors.Is(err, ErrUnknownTarget) {
		t.Errorf("Delete() = %v, want ErrUnknownTarget", err)
	}
	if got := doc.Text(); got != "ab" {
		t.Errorf("Text() = %q, want %q", got, "ab")
	}
}
