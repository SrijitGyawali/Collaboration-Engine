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
