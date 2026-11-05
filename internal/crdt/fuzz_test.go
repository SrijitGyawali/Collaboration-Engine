package crdt

import (
	"errors"
	"slices"
	"testing"
)

// FuzzPositionEdits lets the fuzzer choose a sequence of position edits
// (InsertAt, InsertText, DeleteAt, DeleteRange) and checks each against a
// plain []rune. Positions come straight from the input, so out-of-range ones
// are common: those must fail with ErrOutOfRange and change nothing.
//
// Run it for longer with: go test -run '^$' -fuzz FuzzPositionEdits ./internal/crdt
func FuzzPositionEdits(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0, 0, 'a', 0, 1, 'b', 2, 0, 0, 3, 0, 2})
	f.Add([]byte{1, 0, 3, 3, 1, 5, 2, 9, 0})

	f.Fuzz(func(t *testing.T, data []byte) {
		doc := NewDocument("fuzz")
		var model []rune
		for len(data) >= 3 {
			kind, pos, arg := data[0]%4, int(data[1]), data[2]
			data = data[3:]

			var err error
			valid := false
			switch kind {
			case 0: // InsertAt
				r := rune('a' + arg%26)
				_, err = doc.InsertAt(pos, r)
				if valid = pos <= len(model); valid {
					model = slices.Insert(model, pos, r)
				}
			case 1: // InsertText
				s := []rune("hello")[:arg%6]
				_, err = doc.InsertText(pos, string(s))
				if valid = pos <= len(model); valid {
					model = slices.Insert(model, pos, s...)
				}
			case 2: // DeleteAt
				_, err = doc.DeleteAt(pos)
				if valid = pos < len(model); valid {
					model = slices.Delete(model, pos, pos+1)
				}
			case 3: // DeleteRange
				n := int(arg % 8)
				_, err = doc.DeleteRange(pos, n)
				if valid = pos+n <= len(model); valid {
					model = slices.Delete(model, pos, pos+n)
				}
			}

			if valid && err != nil {
				t.Fatalf("edit %d at %d: unexpected error %v", kind, pos, err)
			}
			if !valid && !errors.Is(err, ErrOutOfRange) {
				t.Fatalf("edit %d at %d: got %v, want ErrOutOfRange", kind, pos, err)
			}
			if got := doc.Text(); got != string(model) {
				t.Fatalf("Text() = %q, model = %q", got, string(model))
			}
		}
		checkInvariants(t, doc)
	})
}

// decodeOps turns arbitrary bytes into ops, 5 bytes per op. Counters and
// replica names are drawn from small ranges on purpose, so the fuzzer often
// produces ops that collide, conflict, or depend on each other.
func decodeOps(data []byte) []Op {
	replicas := []string{"a", "b", "c"}
	var ops []Op
	for len(data) >= 5 {
		b := data[:5]
		data = data[5:]

		id := ID{Counter: uint64(b[1] % 16), Replica: replicas[int(b[2])%len(replicas)]}
		ref := ID{Counter: uint64(b[3] % 16), Replica: replicas[int(b[3]>>4)%len(replicas)]}
		if b[3]%5 == 0 {
			ref = Root
		}
		op := Op{ID: id, Kind: OpKind(b[0] % 4)} // includes invalid kinds 0 and 3
		if op.Kind == OpDelete {
			op.Target = ref
		} else {
			op.After = ref
			op.Value = rune(b[4]) // 0 is an invalid value
		}
		ops = append(ops, op)
	}
	return ops
}

// FuzzSession lets the fuzzer choose a whole editing history: which replica
// acts, whether it edits or receives, where it edits and which message
// arrives next. Every history must end with all replicas converged. Where the
// property tests explore random histories, the fuzzer steers towards
// histories that reach new code paths.
//
// Run it for longer with: go test -run '^$' -fuzz FuzzSession ./internal/crdt
func FuzzSession(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0, 0, 0, 0, 1, 0, 0, 1, 5, 0, 1, 5, 0})
	f.Add([]byte("concurrent edits at the same spot, delivered late"))

	f.Fuzz(func(t *testing.T, data []byte) {
		n := newByteNetwork(t, data, 3)
		// A step reads two to four bytes, so this uses roughly all of data.
		for range len(data) / 3 {
			n.step()
		}
		n.drain()
		assertConverged(t, n.replicas...)
	})
}

// FuzzApply feeds Apply arbitrary, often malformed or conflicting, ops. A
// replica receives whatever the network hands it, so whatever arrives,
// Apply must not panic, must leave the document consistent, and must report
// a valid status for every op it accepts.
//
// Run it for longer with: go test -run '^$' -fuzz FuzzApply ./internal/crdt
func FuzzApply(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{1, 1, 0, 0, 'a', 1, 2, 0, 1, 'b'})                  // "ab" in order
	f.Add([]byte{1, 2, 0, 1, 'b', 1, 1, 0, 0, 'a'})                  // "ab" backwards
	f.Add([]byte{1, 1, 0, 0, 'a', 2, 2, 0, 1, 0})                    // insert then delete
	f.Add([]byte{1, 1, 0, 0, 'a', 1, 1, 0, 0, 'z', 1, 1, 0, 0, 'a'}) // conflict, then repeat

	f.Fuzz(func(t *testing.T, data []byte) {
		doc := NewDocument("fuzz", WithMaxPending(8))
		for _, op := range decodeOps(data) {
			status, err := doc.Apply(op)
			if err == nil && status != Applied && status != Duplicate && status != Pending {
				t.Fatalf("Apply(%+v) = %v with no error", op, status)
			}
			if err != nil && status != 0 {
				t.Fatalf("Apply(%+v) = %v with error %v", op, status, err)
			}
		}
		checkInvariants(t, doc)
		if got, want := len([]rune(doc.Text())), len(visibleIDs(doc)); got != want {
			t.Fatalf("Text() has %d characters, but %d are visible", got, want)
		}
	})
}
