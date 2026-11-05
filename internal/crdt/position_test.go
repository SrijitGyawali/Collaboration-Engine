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

func TestPositionOf(t *testing.T) {
	doc := NewDocument("alice")
	ops := typeOps(t, doc, Root, "carts")
	if _, err := doc.Delete(ops[2].ID); err != nil { // "cats"
		t.Fatal(err)
	}

	tests := []struct {
		char    string
		id      ID
		pos     int
		visible bool
	}{
		{"c", ops[0].ID, 0, true},
		{"a", ops[1].ID, 1, true},
		{"deleted r", ops[2].ID, 2, false}, // where it was: between "a" and "t"
		{"t", ops[3].ID, 2, true},
		{"s", ops[4].ID, 3, true},
	}
	for _, tt := range tests {
		pos, visible, err := doc.PositionOf(tt.id)
		if err != nil {
			t.Fatalf("PositionOf(%s): %v", tt.char, err)
		}
		if pos != tt.pos || visible != tt.visible {
			t.Errorf("PositionOf(%s) = %d, %v; want %d, %v", tt.char, pos, visible, tt.pos, tt.visible)
		}
	}

	if _, _, err := doc.PositionOf(ID{99, "bob"}); !errors.Is(err, ErrUnknownID) {
		t.Errorf("PositionOf(unknown) = %v, want ErrUnknownID", err)
	}
	if _, _, err := doc.PositionOf(Root); !errors.Is(err, ErrUnknownID) {
		t.Errorf("PositionOf(Root) = %v, want ErrUnknownID", err)
	}
}

// IDAt and PositionOf must be inverses for every visible character.
func TestIDAtAndPositionOfAgree(t *testing.T) {
	n := newNetwork(t, 7, 3)
	for range 300 {
		n.step()
	}
	n.drain()

	doc := n.replicas[0]
	for pos := range doc.Len() {
		id, err := doc.IDAt(pos)
		if err != nil {
			t.Fatal(err)
		}
		got, visible, err := doc.PositionOf(id)
		if err != nil || got != pos || !visible {
			t.Fatalf("PositionOf(IDAt(%d)) = %d, %v, %v", pos, got, visible, err)
		}
	}
}

func TestIDAtEmptyDocument(t *testing.T) {
	if _, err := NewDocument("alice").IDAt(0); !errors.Is(err, ErrOutOfRange) {
		t.Errorf("IDAt(0) = %v, want ErrOutOfRange", err)
	}
}
