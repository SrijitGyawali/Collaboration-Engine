package crdt

import (
	"errors"
	"testing"
)

// typeText inserts s at the end of what doc's replica typed last, one
// character at a time, the way a user typing would. It starts after the
// given ID and returns the ID of the last character typed.
func typeText(t *testing.T, doc *Document, after ID, s string) ID {
	t.Helper()
	for _, r := range s {
		op, err := doc.InsertAfter(after, r)
		if err != nil {
			t.Fatalf("InsertAfter(%v, %q): %v", after, r, err)
		}
		after = op.ID
	}
	return after
}

func TestEmptyDocument(t *testing.T) {
	if got := NewDocument("alice").Text(); got != "" {
		t.Errorf("Text() = %q, want empty", got)
	}
}

func TestTypingInOrder(t *testing.T) {
	doc := NewDocument("alice")
	typeText(t, doc, Root, "hello")

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
	typeText(t, doc, h.ID, "i")

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
	typeText(t, doc, Root, "world")
	typeText(t, doc, Root, "hello ")

	if got := doc.Text(); got != "hello world" {
		t.Errorf("Text() = %q, want %q", got, "hello world")
	}
}

func TestUnicode(t *testing.T) {
	doc := NewDocument("alice")
	typeText(t, doc, Root, "नमस्ते 👋")

	if got := doc.Text(); got != "नमस्ते 👋" {
		t.Errorf("Text() = %q, want %q", got, "नमस्ते 👋")
	}
}

func TestApplyRemoteOps(t *testing.T) {
	alice := NewDocument("alice")
	bob := NewDocument("bob")

	// Alice types; bob receives her ops in order.
	var ops []Op
	after := Root
	for _, r := range "hi" {
		op, err := alice.InsertAfter(after, r)
		if err != nil {
			t.Fatal(err)
		}
		ops = append(ops, op)
		after = op.ID
	}
	for _, op := range ops {
		if err := bob.Apply(op); err != nil {
			t.Fatalf("bob.Apply(%v): %v", op.ID, err)
		}
	}

	if got := bob.Text(); got != "hi" {
		t.Errorf("bob.Text() = %q, want %q", got, "hi")
	}

	// Bob's next character must get a higher counter than anything he saw,
	// so it sorts after Alice's characters.
	op, err := bob.InsertAfter(after, '!')
	if err != nil {
		t.Fatal(err)
	}
	if op.ID.Counter <= ops[len(ops)-1].ID.Counter {
		t.Errorf("bob's ID %v is not newer than alice's %v", op.ID, ops[len(ops)-1].ID)
	}
}

func TestApplyErrors(t *testing.T) {
	doc := NewDocument("alice")
	first, err := doc.InsertAfter(Root, 'a')
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		op   Op
		want error
	}{
		{"unknown parent", Op{Kind: OpInsert, ID: ID{9, "bob"}, After: ID{8, "bob"}, Value: 'x'}, ErrUnknownParent},
		{"duplicate id", first, ErrDuplicateID},
		{"invalid op", Op{Kind: OpInsert, Value: 'x'}, ErrInvalidOp},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := doc.Apply(tt.op); !errors.Is(err, tt.want) {
				t.Errorf("Apply() = %v, want %v", err, tt.want)
			}
		})
	}

	// Failed ops must leave the document unchanged.
	if got := doc.Text(); got != "a" {
		t.Errorf("Text() = %q after failed ops, want %q", got, "a")
	}
}
