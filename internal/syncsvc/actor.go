package syncsvc

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/SrijitGyawali/Collaboration-Engine/internal/crdt"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/store"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/wire"
)

// actor is the one goroutine that owns a document's CRDT. Every write to the
// document goes through its inbox, one at a time, so the CRDT needs no locks
// and operations are validated and stored in a single order.
type actor struct {
	svc   *Service
	docID string
	inbox chan request
	// users counts callers between acquire and release; guarded by svc.mu.
	users int
	// doc is the CRDT, loaded from storage on first use; nil until then, or
	// after a storage failure (it is then rebuilt from storage).
	doc *crdt.Document
}

type request struct {
	fn    func(*actor) error
	reply chan error
}

func (a *actor) run() {
	defer a.svc.wg.Done()
	idle := time.NewTimer(a.svc.idle)
	defer idle.Stop()

	for {
		select {
		case req := <-a.inbox:
			req.reply <- req.fn(a)
			idle.Reset(a.svc.idle)
		case <-idle.C:
			if a.svc.retire(a) {
				return
			}
			idle.Reset(a.svc.idle)
		case <-a.svc.done:
			return
		}
	}
}

// loaded returns the CRDT, building it from storage if needed: the document
// is created if new, and its whole history is replayed.
func (a *actor) loaded(ctx context.Context) (*crdt.Document, error) {
	if a.doc != nil {
		return a.doc, nil
	}
	st := a.svc.store
	if _, err := st.EnsureDocument(ctx, a.docID); err != nil {
		return nil, err
	}

	// The server's replica only receives ops; it never makes local edits,
	// so its name is never used in an ID.
	doc := crdt.NewDocument("server")
	var after int64
	for {
		page, err := st.OperationsSince(ctx, a.docID, after, store.MaxPage)
		if err != nil {
			return nil, err
		}
		for _, stored := range page {
			if err := replay(doc, stored); err != nil {
				// A stored op was valid when stored; if it no longer
				// is, keep going rather than make the document
				// unusable, and say so loudly.
				a.svc.log.Error("skipping unreadable stored op", "doc", a.docID, "seq", stored.Seq, "err", err)
			}
			after = stored.Seq
		}
		if len(page) < store.MaxPage {
			break
		}
	}
	a.svc.log.Debug("document loaded", "doc", a.docID, "seq", after)
	a.doc = doc
	return doc, nil
}

func replay(doc *crdt.Document, stored store.Operation) error {
	var w wire.Op
	if err := json.Unmarshal(stored.Payload, &w); err != nil {
		return err
	}
	op, err := w.ToCRDT()
	if err != nil {
		return err
	}
	_, err = doc.Apply(op)
	return err
}

// apply validates and applies ops to the CRDT, stores the valid ones in one
// call, and returns their acks.
func (a *actor) apply(ctx context.Context, ops []wire.Op) ([]Ack, error) {
	doc, err := a.loaded(ctx)
	if err != nil {
		return nil, err
	}

	acks := make([]Ack, len(ops))
	toStore := make([]store.NewOperation, 0, len(ops))
	positions := make([]int, 0, len(ops)) // acks[positions[j]] belongs to toStore[j]
	for i, w := range ops {
		acks[i].OpID = fmt.Sprintf("%d@%s", w.ID.Counter, w.ID.Replica)
		op, err := w.ToCRDT()
		if err != nil {
			acks[i].Err = err.Error()
			continue
		}
		// Applied, Duplicate and Pending are all fine: a pending op is
		// valid, its dependency is just not here yet, and it must be
		// stored so other replicas receive it too.
		if _, err := doc.Apply(op); err != nil {
			acks[i].Err = err.Error()
			continue
		}
		payload, err := json.Marshal(w)
		if err != nil {
			acks[i].Err = err.Error()
			continue
		}
		toStore = append(toStore, store.NewOperation{OpID: acks[i].OpID, Payload: payload})
		positions = append(positions, i)
	}

	stored, err := a.svc.store.AppendOperations(ctx, a.docID, toStore)
	if err != nil {
		// The CRDT now holds ops that were not saved. Drop it; the next
		// request rebuilds it from what storage really has.
		a.doc = nil
		return nil, err
	}
	for j, st := range stored {
		acks[positions[j]].Seq = st.Seq
		acks[positions[j]].Duplicate = st.Duplicate
	}
	return acks, nil
}
