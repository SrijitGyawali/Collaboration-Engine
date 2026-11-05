package crdt

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"
)

// TestPropertyPositionEditsMatchAString applies random position edits to a
// Document and to a plain []rune at the same time. A single user's edits
// must behave exactly like editing an ordinary string, whatever tombstones
// pile up underneath. The ops are then shuffled into a second replica, which
// must reach the same text.
func TestPropertyPositionEditsMatchAString(t *testing.T) {
	for seed := range seeds() {
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewPCG(seed, 3))
			doc := NewDocument("alice")
			var model []rune
			var ops []Op

			for range 200 {
				var batch []Op
				var err error
				switch k := rng.IntN(4); {
				case k == 0 || len(model) == 0:
					pos := rng.IntN(len(model) + 1)
					r := rune('a' + rng.IntN(26))
					var op Op
					op, err = doc.InsertAt(pos, r)
					batch = []Op{op}
					model = slices.Insert(model, pos, r)
				case k == 1:
					pos := rng.IntN(len(model) + 1)
					s := []rune("xyz")[:1+rng.IntN(3)]
					batch, err = doc.InsertText(pos, string(s))
					model = slices.Insert(model, pos, s...)
				case k == 2:
					pos := rng.IntN(len(model))
					var op Op
					op, err = doc.DeleteAt(pos)
					batch = []Op{op}
					model = slices.Delete(model, pos, pos+1)
				default:
					pos := rng.IntN(len(model))
					n := rng.IntN(min(4, len(model)-pos) + 1)
					batch, err = doc.DeleteRange(pos, n)
					model = slices.Delete(model, pos, pos+n)
				}
				if err != nil {
					t.Fatal(err)
				}
				if got := doc.Text(); got != string(model) {
					t.Fatalf("Text() = %q, model = %q", got, string(model))
				}
				if doc.Len() != len(model) {
					t.Fatalf("Len() = %d, model has %d", doc.Len(), len(model))
				}
				ops = append(ops, batch...)
			}
			checkInvariants(t, doc)

			rng.Shuffle(len(ops), func(i, j int) { ops[i], ops[j] = ops[j], ops[i] })
			if got := deliver(t, ops).Text(); got != string(model) {
				t.Errorf("replica Text() = %q, want %q", got, string(model))
			}
		})
	}
}

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

func TestInsertAt(t *testing.T) {
	doc := NewDocument("alice")
	steps := []struct {
		pos  int
		char rune
		want string
	}{
		{0, 'b', "b"},     // into an empty document
		{0, 'a', "ab"},    // at the start
		{2, 'd', "abd"},   // at the end (pos == Len)
		{2, 'c', "abcd"},  // in the middle
		{4, '!', "abcd!"}, // at the end again
	}
	for _, s := range steps {
		if _, err := doc.InsertAt(s.pos, s.char); err != nil {
			t.Fatalf("InsertAt(%d, %q): %v", s.pos, s.char, err)
		}
		if got := doc.Text(); got != s.want {
			t.Fatalf("after InsertAt(%d, %q): Text() = %q, want %q", s.pos, s.char, got, s.want)
		}
	}

	for _, pos := range []int{-1, 6} {
		if _, err := doc.InsertAt(pos, 'x'); !errors.Is(err, ErrOutOfRange) {
			t.Errorf("InsertAt(%d) = %v, want ErrOutOfRange", pos, err)
		}
	}
}

// Inserting next to deleted characters must still land at the requested
// position, ahead of the tombstones.
func TestInsertAtNextToTombstones(t *testing.T) {
	doc := NewDocument("alice")
	ops := typeOps(t, doc, Root, "abcd")
	for _, op := range ops[:2] { // delete "ab": two tombstones at the start
		if _, err := doc.Delete(op.ID); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := doc.InsertAt(0, 'x'); err != nil {
		t.Fatal(err)
	}
	if _, err := doc.InsertAt(1, 'y'); err != nil {
		t.Fatal(err)
	}
	if got := doc.Text(); got != "xycd" {
		t.Errorf("Text() = %q, want %q", got, "xycd")
	}
	checkInvariants(t, doc)
}

func TestDeleteAt(t *testing.T) {
	doc := NewDocument("alice")
	typeOps(t, doc, Root, "abcde")

	for _, s := range []struct {
		pos  int
		want string
	}{
		{0, "bcde"}, // first
		{3, "bcd"},  // last
		{1, "bd"},   // middle
		{1, "b"},    // the same position again: now the next character
	} {
		if _, err := doc.DeleteAt(s.pos); err != nil {
			t.Fatalf("DeleteAt(%d): %v", s.pos, err)
		}
		if got := doc.Text(); got != s.want {
			t.Fatalf("after DeleteAt(%d): Text() = %q, want %q", s.pos, got, s.want)
		}
	}

	for _, pos := range []int{-1, 1} {
		if _, err := doc.DeleteAt(pos); !errors.Is(err, ErrOutOfRange) {
			t.Errorf("DeleteAt(%d) = %v, want ErrOutOfRange", pos, err)
		}
	}
	checkInvariants(t, doc)
}

func TestInsertText(t *testing.T) {
	doc := NewDocument("alice")
	for _, s := range []struct {
		pos  int
		text string
		want string
	}{
		{0, "world", "world"},
		{0, "hello ", "hello world"},
		{11, "!", "hello world!"},
		{5, ", dear", "hello, dear world!"},
		{3, "", "hello, dear world!"}, // empty paste changes nothing
	} {
		ops, err := doc.InsertText(s.pos, s.text)
		if err != nil {
			t.Fatalf("InsertText(%d, %q): %v", s.pos, s.text, err)
		}
		if len(ops) != len([]rune(s.text)) {
			t.Errorf("InsertText(%d, %q) returned %d ops", s.pos, s.text, len(ops))
		}
		if got := doc.Text(); got != s.want {
			t.Fatalf("Text() = %q, want %q", got, s.want)
		}
	}

	if _, err := doc.InsertText(99, "x"); !errors.Is(err, ErrOutOfRange) {
		t.Errorf("InsertText(99) = %v, want ErrOutOfRange", err)
	}
}

// Two users pasting at the same spot concurrently must each keep their text
// in one piece.
func TestConcurrentPastesStayWhole(t *testing.T) {
	alice := NewDocument("alice")
	bob := NewDocument("bob")
	base, _ := alice.InsertText(0, "[]")
	applyAll(t, bob, base)

	a, _ := alice.InsertText(1, "alpha")
	b, _ := bob.InsertText(1, "beta")
	applyAll(t, alice, b)
	applyAll(t, bob, a)

	assertConverged(t, alice, bob)
	if got := alice.Text(); got != "[alphabeta]" && got != "[betaalpha]" {
		t.Errorf("Text() = %q, want both pastes whole", got)
	}
}

func TestDeleteRange(t *testing.T) {
	doc := NewDocument("alice")
	if _, err := doc.InsertText(0, "hello, dear world!"); err != nil {
		t.Fatal(err)
	}

	for _, s := range []struct {
		pos, n int
		want   string
	}{
		{5, 6, "hello world!"}, // ", dear" from the middle
		{11, 1, "hello world"}, // the last character
		{0, 6, "world"},        // from the start
		{2, 0, "world"},        // an empty range
		{0, 5, ""},             // everything
	} {
		ops, err := doc.DeleteRange(s.pos, s.n)
		if err != nil {
			t.Fatalf("DeleteRange(%d, %d): %v", s.pos, s.n, err)
		}
		if len(ops) != s.n {
			t.Errorf("DeleteRange(%d, %d) returned %d ops", s.pos, s.n, len(ops))
		}
		if got := doc.Text(); got != s.want {
			t.Fatalf("after DeleteRange(%d, %d): Text() = %q, want %q", s.pos, s.n, got, s.want)
		}
	}
	checkInvariants(t, doc)
}

func TestDeleteRangeOutOfRange(t *testing.T) {
	doc := NewDocument("alice")
	if _, err := doc.InsertText(0, "abc"); err != nil {
		t.Fatal(err)
	}
	for _, r := range [][2]int{{-1, 1}, {0, -1}, {2, 2}, {4, 0}} {
		if _, err := doc.DeleteRange(r[0], r[1]); !errors.Is(err, ErrOutOfRange) {
			t.Errorf("DeleteRange(%d, %d) = %v, want ErrOutOfRange", r[0], r[1], err)
		}
	}
	if got := doc.Text(); got != "abc" {
		t.Errorf("Text() = %q after rejected ranges, want %q", got, "abc")
	}
}

func TestIDAtEmptyDocument(t *testing.T) {
	if _, err := NewDocument("alice").IDAt(0); !errors.Is(err, ErrOutOfRange) {
		t.Errorf("IDAt(0) = %v, want ErrOutOfRange", err)
	}
}
