package crdt

import "testing"

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
