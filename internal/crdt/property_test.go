package crdt

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"
)

// network simulates replicas connected by an unreliable network. Messages
// are delayed for a random time, delivered in random order and sometimes
// delivered again, but never lost for good: that is the delivery guarantee
// the real system (WebSocket retries, Redis, Kafka, catch-up from Postgres)
// gives the CRDT.
type network struct {
	t *testing.T
	// pick returns a number in [0, k). Every choice the network makes goes
	// through it, so the same source of choices replays the same history.
	pick func(k int) int
	// rng backs pick for seeded networks; nil for fuzz-driven ones.
	rng      *rand.Rand
	replicas []*Document
	// inbox holds, per replica, ops sent to it but not yet delivered.
	inbox [][]Op
	// sent is every op ever made, in creation order.
	sent []Op
}

// newNetwork returns a network whose choices come from a seeded generator.
func newNetwork(t *testing.T, seed uint64, replicas int) *network {
	rng := rand.New(rand.NewPCG(seed, 0))
	n := newNetworkWith(t, rng.IntN, replicas)
	n.rng = rng
	return n
}

// newByteNetwork returns a network whose choices are read from data, one
// byte per choice, so a fuzzer can steer it. Once data runs out every
// choice is 0.
func newByteNetwork(t *testing.T, data []byte, replicas int) *network {
	return newNetworkWith(t, func(k int) int {
		if len(data) == 0 {
			return 0
		}
		b := data[0]
		data = data[1:]
		return int(b) % k
	}, replicas)
}

func newNetworkWith(t *testing.T, pick func(int) int, replicas int) *network {
	n := &network{t: t, pick: pick, inbox: make([][]Op, replicas)}
	for i := range replicas {
		n.replicas = append(n.replicas, NewDocument(fmt.Sprintf("r%d", i)))
	}
	return n
}

// step does one random thing: a local edit, a delivery or a re-delivery.
func (n *network) step() {
	r := n.pick(len(n.replicas))
	switch k := n.pick(10); {
	case k < 4:
		n.edit(r)
	case k < 9:
		n.deliver(r)
	default:
		n.redeliver(r)
	}
}

// edit makes a random local edit on replica r, as a user would: insert a
// letter at a random position, or delete a random character. The op is sent
// to every other replica.
func (n *network) edit(r int) {
	doc := n.replicas[r]
	ids := visibleIDs(doc)
	before := []rune(doc.Text())

	// want is what the user expects to see right after the edit: exactly
	// their change, applied to exactly the text they were looking at.
	var op Op
	var err error
	var want []rune
	if len(ids) > 0 && n.pick(3) == 0 {
		pos := n.pick(len(ids))
		op, err = doc.Delete(ids[pos])
		want = slices.Delete(slices.Clone(before), pos, pos+1)
	} else {
		pos := n.pick(len(ids) + 1)
		after := Root
		if pos > 0 {
			after = ids[pos-1]
		}
		value := rune('a' + n.pick(26))
		op, err = doc.InsertAfter(after, value)
		want = slices.Insert(slices.Clone(before), pos, value)
	}
	if err != nil {
		n.t.Fatalf("%s: local edit: %v", doc.Replica(), err)
	}
	if got := doc.Text(); got != string(want) {
		n.t.Fatalf("%s: local edit %v: text %q, want %q", doc.Replica(), op.ID, got, string(want))
	}

	n.sent = append(n.sent, op)
	for i := range n.replicas {
		if i != r {
			n.inbox[i] = append(n.inbox[i], op)
		}
	}
}

// deliver hands replica r one message from its inbox, picked at random, so
// messages overtake each other.
func (n *network) deliver(r int) {
	box := n.inbox[r]
	if len(box) == 0 {
		return
	}
	i := n.pick(len(box))
	op := box[i]
	box[i] = box[len(box)-1]
	n.inbox[r] = box[:len(box)-1]

	if _, err := n.replicas[r].Apply(op); err != nil {
		n.t.Fatalf("%s: Apply(%v): %v", n.replicas[r].Replica(), op.ID, err)
	}
}

// redeliver queues a random op that already exists for replica r again,
// including r's own ops (as a server echo would).
func (n *network) redeliver(r int) {
	if len(n.sent) == 0 {
		return
	}
	n.inbox[r] = append(n.inbox[r], n.sent[n.pick(len(n.sent))])
}

// drain delivers every outstanding message, in random order.
func (n *network) drain() {
	for {
		var busy []int
		for r, box := range n.inbox {
			if len(box) > 0 {
				busy = append(busy, r)
			}
		}
		if len(busy) == 0 {
			return
		}
		n.deliver(busy[n.pick(len(busy))])
	}
}

// seeds returns how many random sessions a property test runs: fewer with
// -short, so a quick local run stays quick.
func seeds() uint64 {
	if testing.Short() {
		return 20
	}
	return 200
}

// TestPropertyConvergence is the main CRDT guarantee: whatever edits are made
// and however the network delays, reorders and repeats them, once every op
// has been delivered every replica shows the same text.
func TestPropertyConvergence(t *testing.T) {
	for seed := range seeds() {
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			n := newNetwork(t, seed, 3+int(seed%3)) // 3 to 5 replicas
			for range 300 {
				n.step()
			}
			n.drain()

			assertConverged(t, n.replicas...)
			for _, doc := range n.replicas {
				if got := doc.PendingCount(); got != 0 {
					t.Errorf("%s: PendingCount() = %d, want 0", doc.Replica(), got)
				}
			}
		})
	}
}

// TestPropertyStateDependsOnlyOnOps checks the property that convergence
// follows from: a replica's text is a function of the set of ops it has
// received, not of their order or of how often each one arrived. The ops of
// a random session are replayed into fresh replicas, shuffled and with
// repeats, and every replay must match the session's own replicas.
func TestPropertyStateDependsOnlyOnOps(t *testing.T) {
	for seed := range seeds() {
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			n := newNetwork(t, seed, 3)
			for range 200 {
				n.step()
			}
			n.drain()
			want := n.replicas[0].Text()

			for replay := range 5 {
				ops := slices.Clone(n.sent)
				for range n.pick(len(ops) + 1) {
					ops = append(ops, ops[n.pick(len(ops))])
				}
				n.rng.Shuffle(len(ops), func(i, j int) { ops[i], ops[j] = ops[j], ops[i] })

				doc := deliver(t, ops)
				if got := doc.Text(); got != want {
					t.Fatalf("replay %d: Text() = %q, want %q", replay, got, want)
				}
			}
		})
	}
}
