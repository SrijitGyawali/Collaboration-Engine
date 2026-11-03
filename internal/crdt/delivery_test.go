package crdt

import (
	"math/rand/v2"
	"slices"
	"testing"
)

// session simulates three users editing one document for a few rounds. In
// each round every user, without seeing the others' edits from that round,
// types a short word after a random visible character and deletes one
// random visible character. Between rounds everyone syncs.
//
// It returns every op in creation order and the text all three users agree
// on at the end. The rand seed makes the session the same on every run.
func session(t *testing.T, seed uint64) ([]Op, string) {
	t.Helper()
	rng := rand.New(rand.NewPCG(seed, 0))
	users := []*Document{NewDocument("u1"), NewDocument("u2"), NewDocument("u3")}
	words := []string{"go", "crdt", "sync", "ok", "hey"}

	var all []Op
	for round := range 4 {
		var roundOps []Op
		for _, u := range users {
			ids := visibleIDs(u)
			after := Root
			if len(ids) > 0 {
				after = ids[rng.IntN(len(ids))]
			}
			roundOps = append(roundOps, typeOps(t, u, after, words[rng.IntN(len(words))])...)

			if ids := visibleIDs(u); len(ids) > 0 {
				del, err := u.Delete(ids[rng.IntN(len(ids))])
				if err != nil {
					t.Fatalf("round %d: %v", round, err)
				}
				roundOps = append(roundOps, del)
			}
		}
		// Everyone receives the round's ops. A user's own ops come back as
		// duplicates, which is what a server echo does.
		for _, u := range users {
			for _, op := range roundOps {
				if _, err := u.Apply(op); err != nil {
					t.Fatalf("round %d: %s.Apply(%v): %v", round, u.Replica(), op.ID, err)
				}
			}
		}
		all = append(all, roundOps...)
	}

	assertConverged(t, users...)
	return all, users[0].Text()
}

// deliver applies ops to a fresh replica in the given order and returns it.
func deliver(t *testing.T, ops []Op) *Document {
	t.Helper()
	doc := NewDocument("observer")
	for _, op := range ops {
		if _, err := doc.Apply(op); err != nil {
			t.Fatalf("Apply(%v): %v", op.ID, err)
		}
	}
	return doc
}

// TestSessionDeliveredWithRepeats models an at-least-once network: every op
// arrives one to three times, in a random order.
func TestSessionDeliveredWithRepeats(t *testing.T) {
	for seed := range uint64(20) {
		ops, want := session(t, seed)
		rng := rand.New(rand.NewPCG(seed, 2))
		var noisy []Op
		for _, op := range ops {
			for range 1 + rng.IntN(3) {
				noisy = append(noisy, op)
			}
		}
		rng.Shuffle(len(noisy), func(i, j int) { noisy[i], noisy[j] = noisy[j], noisy[i] })

		doc := deliver(t, noisy)

		if got := doc.Text(); got != want {
			t.Errorf("seed %d: Text() = %q, want %q", seed, got, want)
		}
		if got := doc.PendingCount(); got != 0 {
			t.Errorf("seed %d: PendingCount() = %d, want 0", seed, got)
		}
	}
}

func TestSessionDeliveredInReverse(t *testing.T) {
	ops, want := session(t, 1)
	reversed := slices.Clone(ops)
	slices.Reverse(reversed)

	doc := deliver(t, reversed)

	if got := doc.Text(); got != want {
		t.Errorf("Text() = %q, want %q", got, want)
	}
	if got := doc.PendingCount(); got != 0 {
		t.Errorf("PendingCount() = %d, want 0", got)
	}
}

func TestSessionDeliveredShuffled(t *testing.T) {
	for seed := range uint64(20) {
		ops, want := session(t, seed)
		rng := rand.New(rand.NewPCG(seed, 1))
		shuffled := slices.Clone(ops)
		rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })

		doc := deliver(t, shuffled)

		if got := doc.Text(); got != want {
			t.Errorf("seed %d: Text() = %q, want %q", seed, got, want)
		}
		if got := doc.PendingCount(); got != 0 {
			t.Errorf("seed %d: PendingCount() = %d, want 0", seed, got)
		}
	}
}
