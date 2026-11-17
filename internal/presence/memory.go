package presence

import (
	"context"
	"sync"
	"time"
)

// Memory keeps presence in this process. It is what a single gateway uses
// without Redis, and what tests use; expiry follows the clock it is given.
type Memory struct {
	now func() time.Time

	mu   sync.Mutex
	docs map[string]map[string]entry
}

type entry struct {
	peer    Peer
	expires time.Time
}

// NewMemory returns an empty store using the real clock.
func NewMemory() *Memory {
	return NewMemoryWithClock(time.Now)
}

// NewMemoryWithClock returns an empty store that reads the time from now,
// so tests can move it forward.
func NewMemoryWithClock(now func() time.Time) *Memory {
	return &Memory{now: now, docs: make(map[string]map[string]entry)}
}

// Put implements Store.
func (m *Memory) Put(_ context.Context, docID string, p Peer, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	doc, ok := m.docs[docID]
	if !ok {
		doc = make(map[string]entry)
		m.docs[docID] = doc
	}
	doc[p.Replica] = entry{peer: p, expires: m.now().Add(ttl)}
	return nil
}

// Remove implements Store.
func (m *Memory) Remove(_ context.Context, docID, replica string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.docs[docID], replica)
	if len(m.docs[docID]) == 0 {
		delete(m.docs, docID)
	}
	return nil
}

// List implements Store. Expired entries are dropped as they are found, the
// way Redis drops expired hash fields.
func (m *Memory) List(_ context.Context, docID string) ([]Peer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	var peers []Peer
	for replica, e := range m.docs[docID] {
		if !now.Before(e.expires) {
			delete(m.docs[docID], replica)
			continue
		}
		peers = append(peers, e.peer)
	}
	if len(m.docs[docID]) == 0 {
		delete(m.docs, docID)
	}
	return peers, nil
}
