package crdt

import (
	"strings"
	"testing"
)

// typeN types n characters in order on a fresh document, like one user
// typing a long text, and returns the document.
func typeN(tb testing.TB, n int) *Document {
	tb.Helper()
	doc := NewDocument("alice")
	after := Root
	for i := range n {
		op, err := doc.InsertAfter(after, rune('a'+i%26))
		if err != nil {
			tb.Fatal(err)
		}
		after = op.ID
	}
	return doc
}

// Typing in order builds one long chain in the tree; Text must handle it
// without deep recursion.
func TestLongDocument(t *testing.T) {
	const n = 200_000
	doc := typeN(t, n)

	got := doc.Text()
	if len(got) != n {
		t.Fatalf("len(Text()) = %d, want %d", len(got), n)
	}
	if !strings.HasPrefix(got, "abcdefghijklmnopqrstuvwxyz") {
		t.Errorf("Text() starts with %q", got[:26])
	}
}

// typedOps returns the ops for n characters typed in order.
func typedOps(tb testing.TB, n int) []Op {
	tb.Helper()
	doc := NewDocument("alice")
	ops := make([]Op, 0, n)
	after := Root
	for i := range n {
		op, err := doc.InsertAfter(after, rune('a'+i%26))
		if err != nil {
			tb.Fatal(err)
		}
		ops = append(ops, op)
		after = op.ID
	}
	return ops
}

// The worst case for the pending buffer: a long document delivered completely
// backwards. Every op but the last waits, and the last releases all of them
// in a single cascade.
func TestLongDocumentDeliveredBackwards(t *testing.T) {
	const n = 50_000
	ops := typedOps(t, n)
	doc := NewDocument("bob")

	for i := n - 1; i >= 0; i-- {
		if _, err := doc.Apply(ops[i]); err != nil {
			t.Fatal(err)
		}
	}

	if got := len(doc.Text()); got != n {
		t.Errorf("len(Text()) = %d, want %d", got, n)
	}
	if got := doc.PendingCount(); got != 0 {
		t.Errorf("PendingCount() = %d, want 0", got)
	}
}

func BenchmarkApplyInOrder10k(b *testing.B) {
	ops := typedOps(b, 10_000)
	for b.Loop() {
		doc := NewDocument("bob")
		for _, op := range ops {
			_, _ = doc.Apply(op)
		}
	}
}

func BenchmarkApplyBackwards10k(b *testing.B) {
	ops := typedOps(b, 10_000)
	for b.Loop() {
		doc := NewDocument("bob")
		for i := len(ops) - 1; i >= 0; i-- {
			_, _ = doc.Apply(ops[i])
		}
	}
}

// Typing at the end of a 10,000-character document through the position API,
// the most common editor action.
func BenchmarkInsertAtEnd10k(b *testing.B) {
	doc := typeN(b, 10_000)
	for i := 0; b.Loop(); i++ {
		if _, err := doc.InsertAt(doc.Len(), rune('a'+i%26)); err != nil {
			b.Fatal(err)
		}
	}
}

// Typing in the middle of a 10,000-character document: the worst case for
// position lookups, about n/2 characters scanned per keystroke. The document
// is rebuilt every 1,000 keystrokes so it stays close to 10,000 characters.
func BenchmarkInsertAtMiddle10k(b *testing.B) {
	doc := typeN(b, 10_000)
	for i := 0; b.Loop(); i++ {
		if i%1000 == 999 {
			b.StopTimer()
			doc = typeN(b, 10_000)
			b.StartTimer()
		}
		if _, err := doc.InsertAt(doc.Len()/2, rune('a'+i%26)); err != nil {
			b.Fatal(err)
		}
	}
}

// Pressing Backspace at the end of a long document, refilled as it empties.
// Each delete leaves a tombstone at the end that later lookups from the end
// must skip, so this measures the cost of accumulated tombstones too.
func BenchmarkDeleteAtEnd10k(b *testing.B) {
	doc := typeN(b, 10_000)
	for b.Loop() {
		if doc.Len() == 0 {
			b.StopTimer()
			doc = typeN(b, 10_000)
			b.StartTimer()
		}
		if _, err := doc.DeleteAt(doc.Len() - 1); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkIDAtMiddle10k(b *testing.B) {
	doc := typeN(b, 10_000)
	for b.Loop() {
		if _, err := doc.IDAt(5_000); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkInsertAfter(b *testing.B) {
	doc := NewDocument("alice")
	after := Root
	for i := 0; b.Loop(); i++ {
		op, err := doc.InsertAfter(after, rune('a'+i%26))
		if err != nil {
			b.Fatal(err)
		}
		after = op.ID
	}
}

func BenchmarkText10k(b *testing.B) {
	doc := typeN(b, 10_000)
	for b.Loop() {
		_ = doc.Text()
	}
}
