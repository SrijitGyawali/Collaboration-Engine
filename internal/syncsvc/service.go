package syncsvc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/SrijitGyawali/Collaboration-Engine/internal/store"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/store/memstore"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/wire"
)

// MaxBatch is the most operations one Apply call accepts.
const MaxBatch = store.MaxPage

var (
	// ErrClosed is returned after Close.
	ErrClosed = errors.New("syncsvc: service closed")
	// ErrBatchTooLarge is returned for more than MaxBatch operations.
	ErrBatchTooLarge = errors.New("syncsvc: too many operations in one batch")
)

// Store is what the service needs from storage. *store.Store implements it,
// and so does memstore.Store.
type Store interface {
	EnsureDocument(ctx context.Context, id string) (store.Document, error)
	AppendOperations(ctx context.Context, docID string, ops []store.NewOperation) ([]store.Ack, error)
	OperationsSince(ctx context.Context, docID string, afterSeq int64, limit int) ([]store.Operation, error)
}

// Both stores must keep satisfying Store; this fails to compile otherwise.
var (
	_ Store = (*store.Store)(nil)
	_ Store = (*memstore.Store)(nil)
)

// Ack is the outcome of one operation passed to Apply.
type Ack struct {
	OpID string
	// Seq is the operation's place in the document's history; 0 if it was
	// rejected.
	Seq int64
	// Duplicate means the operation was already stored; Seq is then the
	// number it was given the first time.
	Duplicate bool
	// Err says why the operation was rejected, or is empty. Rejected
	// operations are not stored, and the client should drop them.
	Err string
}

// SeqOp is a stored operation with its sequence number.
type SeqOp struct {
	Seq int64
	Op  wire.Op
}

// Options tunes a Service. The zero value means the defaults.
type Options struct {
	// IdleTimeout is how long a document stays loaded without requests.
	IdleTimeout time.Duration
}

// Service owns the live state of every open document. It is safe for
// concurrent use.
type Service struct {
	store Store
	log   *slog.Logger
	idle  time.Duration

	mu     sync.Mutex
	actors map[string]*actor
	closed bool
	done   chan struct{}
	wg     sync.WaitGroup
}

// New returns a service storing operations in st.
func New(st Store, log *slog.Logger, opts Options) *Service {
	if opts.IdleTimeout <= 0 {
		opts.IdleTimeout = 5 * time.Minute
	}
	return &Service{
		store:  st,
		log:    log,
		idle:   opts.IdleTimeout,
		actors: make(map[string]*actor),
		done:   make(chan struct{}),
	}
}

// Apply validates ops, applies them to the document's CRDT, stores them, and
// returns one Ack per op, in order. Retrying the same ops is safe: they come
// back as duplicates with their original sequence numbers.
//
// An error means nothing about the batch is known (for example the database
// was unreachable); the caller should retry the whole batch later.
func (s *Service) Apply(ctx context.Context, docID string, ops []wire.Op) ([]Ack, error) {
	if len(ops) == 0 {
		return nil, nil
	}
	if len(ops) > MaxBatch {
		return nil, fmt.Errorf("%w: %d (max %d)", ErrBatchTooLarge, len(ops), MaxBatch)
	}
	var acks []Ack
	err := s.do(ctx, docID, func(a *actor) error {
		var err error
		acks, err = a.apply(ctx, ops)
		return err
	})
	return acks, err
}

// Text returns the document's current text, as the service's CRDT has it.
func (s *Service) Text(ctx context.Context, docID string) (string, error) {
	var text string
	err := s.do(ctx, docID, func(a *actor) error {
		doc, err := a.loaded(ctx)
		if err == nil {
			text = doc.Text()
		}
		return err
	})
	return text, err
}

// OperationsSince returns the document's stored operations after afterSeq,
// oldest first, at most limit of them (and never more than store.MaxPage). It
// reads storage directly: history is immutable, so it needs no actor.
func (s *Service) OperationsSince(ctx context.Context, docID string, afterSeq int64, limit int) ([]SeqOp, error) {
	stored, err := s.store.OperationsSince(ctx, docID, afterSeq, limit)
	if err != nil {
		return nil, err
	}
	ops := make([]SeqOp, len(stored))
	for i, st := range stored {
		if err := json.Unmarshal(st.Payload, &ops[i].Op); err != nil {
			return nil, fmt.Errorf("syncsvc: stored op %s of %q is corrupt: %w", st.OpID, docID, err)
		}
		ops[i].Seq = st.Seq
	}
	return ops, nil
}

// Close stops every document's goroutine and waits for them. Calls already
// queued finish first; later calls return ErrClosed.
func (s *Service) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	close(s.done)
	s.mu.Unlock()
	s.wg.Wait()
}

// do runs fn on the document's actor goroutine and waits for it.
func (s *Service) do(ctx context.Context, docID string, fn func(*actor) error) error {
	a, err := s.acquire(docID)
	if err != nil {
		return err
	}
	defer s.release(a)

	req := request{fn: fn, reply: make(chan error, 1)}
	select {
	case a.inbox <- req:
	case <-ctx.Done():
		return ctx.Err()
	case <-s.done:
		return ErrClosed
	}
	// Once queued, the request runs even if ctx ends meanwhile, so an edit
	// is either fully stored or not at all; the caller learns which on
	// retry, from the duplicate flag.
	select {
	case err := <-req.reply:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// acquire returns the document's actor, starting it if needed, and marks it
// in use so it cannot retire while the caller talks to it.
func (s *Service) acquire(docID string) (*actor, error) {
	if !store.ValidDocumentID(docID) {
		return nil, fmt.Errorf("%w: %q", store.ErrInvalidDocumentID, docID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrClosed
	}
	a, ok := s.actors[docID]
	if !ok {
		a = &actor{svc: s, docID: docID, inbox: make(chan request)}
		s.actors[docID] = a
		s.wg.Add(1)
		go a.run()
	}
	a.users++
	return a, nil
}

func (s *Service) release(a *actor) {
	s.mu.Lock()
	a.users--
	s.mu.Unlock()
}

// retire removes an idle actor, unless someone is about to use it.
func (s *Service) retire(a *actor) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a.users > 0 {
		return false
	}
	delete(s.actors, a.docID)
	return true
}

// loadedDocuments reports how many documents have a running actor.
func (s *Service) loadedDocuments() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.actors)
}
