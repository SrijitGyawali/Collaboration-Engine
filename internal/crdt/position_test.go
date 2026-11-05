package crdt

import (
	"errors"
	"testing"
)

func TestIDAt(t *testing.T) {
	doc := NewDocument("alice")
	ops := typeOps(t, doc, Root, "cart")
	if _, err := doc.Delete(ops[2].ID); err != nil { // "cat", with a tombstone
		t.Fatal(err)
	}

	// Every position, from both scan directions, skipping the tombstone.
	want := []ID{ops[0].ID, ops[1].ID, ops[3].ID}
	for pos, id := range want {
		got, err := doc.IDAt(pos)
		if err != nil {
			t.Fatalf("IDAt(%d): %v", pos, err)
		}
		if got != id {
			t.Errorf("IDAt(%d) = %v, want %v", pos, got, id)
		}
	}

	for _, pos := range []int{-1, 3, 100} {
		if _, err := doc.IDAt(pos); !errors.Is(err, ErrOutOfRange) {
			t.Errorf("IDAt(%d) = %v, want ErrOutOfRange", pos, err)
		}
	}
}

func TestIDAtEmptyDocument(t *testing.T) {
	if _, err := NewDocument("alice").IDAt(0); !errors.Is(err, ErrOutOfRange) {
		t.Errorf("IDAt(0) = %v, want ErrOutOfRange", err)
	}
}
