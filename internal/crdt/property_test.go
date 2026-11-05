package crdt

import (
	"fmt"
	"math/rand/v2"
	"testing"
)

// network simulates replicas connected by an unreliable network. Messages
// are delayed for a random time, delivered in random order and sometimes
// delivered again, but never lost for good: that is the delivery guarantee
// the real system (WebSocket retries, Redis, Kafka, catch-up from Postgres)
// gives the CRDT.
type network struct {
	t        *testing.T
	rng      *rand.Rand
	replicas []*Document
	// inbox holds, per replica, ops sent to it but not yet delivered.
	inbox [][]Op
	// sent is every op ever made, in creation order.
	sent []Op
}

func newNetwork(t *testing.T, seed uint64, replicas int) *network {
	n := &network{
		t:     t,
		rng:   rand.New(rand.NewPCG(seed, 0)),
		inbox: make([][]Op, replicas),
	}
	for i := range replicas {
		n.replicas = append(n.replicas, NewDocument(fmt.Sprintf("r%d", i)))
	}
	return n
}

// step does one random thing: a local edit, a delivery or a re-delivery.
func (n *network) step() {
	r := n.rng.IntN(len(n.replicas))
	switch k := n.rng.IntN(10); {
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

	var op Op
	var err error
	if len(ids) > 0 && n.rng.IntN(3) == 0 {
		op, err = doc.Delete(ids[n.rng.IntN(len(ids))])
	} else {
		pos := n.rng.IntN(len(ids) + 1)
		after := Root
		if pos > 0 {
			after = ids[pos-1]
		}
		op, err = doc.InsertAfter(after, rune('a'+n.rng.IntN(26)))
	}
	if err != nil {
		n.t.Fatalf("%s: local edit: %v", doc.Replica(), err)
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
	i := n.rng.IntN(len(box))
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
	n.inbox[r] = append(n.inbox[r], n.sent[n.rng.IntN(len(n.sent))])
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
		n.deliver(busy[n.rng.IntN(len(busy))])
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
