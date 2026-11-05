package crdt

import (
	"errors"
	"testing"
)

func TestEmptyDocument(t *testing.T) {
	if got := NewDocument("alice").Text(); got != "" {
		t.Errorf("Text() = %q, want empty", got)
	}
}

func TestLen(t *testing.T) {
	doc := NewDocument("alice")
	if doc.Len() != 0 {
		t.Errorf("empty Len() = %d, want 0", doc.Len())
	}

	ops := typeOps(t, doc, Root, "héllo👋") // Len counts characters, not bytes
	if doc.Len() != 6 {
		t.Errorf("Len() = %d, want 6", doc.Len())
	}

	if _, err := doc.Delete(ops[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := doc.Delete(ops[0].ID); err != nil { // deleting again changes nothing
		t.Fatal(err)
	}
	if doc.Len() != 5 {
		t.Errorf("Len() after delete = %d, want 5", doc.Len())
	}
}

func TestTypingInOrder(t *testing.T) {
	doc := NewDocument("alice")
	typeOps(t, doc, Root, "hello")

	if got := doc.Text(); got != "hello" {
		t.Errorf("Text() = %q, want %q", got, "hello")
	}
}

func TestInsertInTheMiddle(t *testing.T) {
	doc := NewDocument("alice")
	h, err := doc.InsertAfter(Root, 'h')
	if err != nil {
		t.Fatal(err)
	}
	typeOps(t, doc, h.ID, "i")

	// Insert '!' between 'h' and 'i'.
	if _, err := doc.InsertAfter(h.ID, '!'); err != nil {
		t.Fatal(err)
	}

	if got := doc.Text(); got != "h!i" {
		t.Errorf("Text() = %q, want %q", got, "h!i")
	}
}

func TestInsertAtStart(t *testing.T) {
	doc := NewDocument("alice")
	typeOps(t, doc, Root, "world")
	typeOps(t, doc, Root, "hello ")

	if got := doc.Text(); got != "hello world" {
		t.Errorf("Text() = %q, want %q", got, "hello world")
	}
}

func TestUnicode(t *testing.T) {
	doc := NewDocument("alice")
	typeOps(t, doc, Root, "नमस्ते 👋")

	if got := doc.Text(); got != "नमस्ते 👋" {
		t.Errorf("Text() = %q, want %q", got, "नमस्ते 👋")
	}
}

func TestApplyRemoteOps(t *testing.T) {
	alice := NewDocument("alice")
	bob := NewDocument("bob")

	// Alice types; bob receives her ops in order.
	ops := typeOps(t, alice, Root, "hi")
	for _, op := range ops {
		status, err := bob.Apply(op)
		if err != nil || status != Applied {
			t.Fatalf("bob.Apply(%v) = %v, %v; want applied", op.ID, status, err)
		}
	}

	if got := bob.Text(); got != "hi" {
		t.Errorf("bob.Text() = %q, want %q", got, "hi")
	}

	// Bob's next character must get a higher counter than anything he saw,
	// so it sorts after Alice's characters.
	op, err := bob.InsertAfter(ops[len(ops)-1].ID, '!')
	if err != nil {
		t.Fatal(err)
	}
	if op.ID.Counter <= ops[len(ops)-1].ID.Counter {
		t.Errorf("bob's ID %v is not newer than alice's %v", op.ID, ops[len(ops)-1].ID)
	}
}

func TestApplyErrors(t *testing.T) {
	doc := NewDocument("alice")
	if _, err := doc.InsertAfter(Root, 'a'); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		op   Op
		want error
	}{
		{"invalid op", Op{Kind: OpInsert, Value: 'x'}, ErrInvalidOp},
		{"invalid delete", Op{Kind: OpDelete, ID: ID{9, "bob"}}, ErrInvalidOp},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := doc.Apply(tt.op); !errors.Is(err, tt.want) {
				t.Errorf("Apply() = %v, want %v", err, tt.want)
			}
		})
	}

	// Failed ops must leave the document unchanged.
	if got := doc.Text(); got != "a" {
		t.Errorf("Text() = %q after failed ops, want %q", got, "a")
	}
}
