package integration

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/SrijitGyawali/Collaboration-Engine/internal/crdt"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/wire"
)

// client behaves like the browser editor, in Go: it keeps its own CRDT
// replica, edits it by position, sends the ops to the gateway, applies
// everyone else's, and keeps every op until the server acknowledges it. After
// a reconnect it joins with the last sequence number it has without gaps and
// resends whatever was not acknowledged, exactly as the TypeScript transport
// does.
type client struct {
	t       *testing.T
	url     string
	docID   string
	replica string

	mu      sync.Mutex
	doc     *crdt.Document
	unacked map[string]wire.Op
	order   []string // op IDs in the order they were made, for resending
	seqs    map[int64]bool
	last    int64 // highest seq with none missing below it
	conn    *websocket.Conn
	joined  chan struct{}
	done    chan struct{}
}

func newClient(t *testing.T, url, docID, replica string) *client {
	c := &client{
		t: t, url: url, docID: docID, replica: replica,
		doc:     crdt.NewDocument(replica),
		unacked: make(map[string]wire.Op),
		seqs:    make(map[int64]bool),
	}
	c.connect()
	t.Cleanup(c.disconnect)
	return c
}

// connect opens a connection, joins from the last seq, and waits until the
// server has caught it up; then any unacknowledged ops are sent again.
func (c *client) connect() {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, resp, err := websocket.Dial(ctx, c.url, nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		c.t.Fatalf("%s: dial: %v", c.replica, err)
	}
	conn.SetReadLimit(8 << 20)

	c.mu.Lock()
	c.conn = conn
	c.joined = make(chan struct{})
	c.done = make(chan struct{})
	join := map[string]any{"type": "join", "doc": c.docID, "replica": c.replica, "lastSeq": c.last}
	c.mu.Unlock()

	c.write(conn, join)
	go c.read(conn, c.joined, c.done)
	select {
	case <-c.joined:
	case <-time.After(5 * time.Second):
		c.t.Fatalf("%s: never joined", c.replica)
	}

	c.mu.Lock()
	var pending []wire.Op
	for _, id := range c.order {
		if op, ok := c.unacked[id]; ok {
			pending = append(pending, op)
		}
	}
	c.mu.Unlock()
	if len(pending) > 0 {
		c.write(conn, map[string]any{"type": "ops", "ops": pending})
	}
}

// disconnect drops the connection, as a network failure would; unacked ops
// are kept for the next connect.
func (c *client) disconnect() {
	c.mu.Lock()
	conn, done := c.conn, c.done
	c.conn = nil
	c.mu.Unlock()
	if conn == nil {
		return
	}
	_ = conn.CloseNow()
	<-done
}

// edit makes a local edit by position and sends it if connected; offline, the
// ops wait in the unacked queue.
func (c *client) edit(pos, deleteCount int, insert string) {
	c.change(func(int) (int, int, string) { return pos, deleteCount, insert })
}

// change makes the edit that choose returns, given the current length, as one
// step: the length cannot change between choosing and editing, just as a
// browser reads and edits the textarea on one thread.
func (c *client) change(choose func(length int) (pos, deleteCount int, insert string)) {
	c.mu.Lock()
	pos, deleteCount, insert := choose(c.doc.Len())
	var ops []crdt.Op
	if deleteCount > 0 {
		deleted, err := c.doc.DeleteRange(pos, deleteCount)
		if err != nil {
			c.mu.Unlock()
			c.t.Errorf("%s: delete: %v", c.replica, err) // may run off the test goroutine
			return
		}
		ops = append(ops, deleted...)
	}
	if insert != "" {
		inserted, err := c.doc.InsertText(pos, insert)
		if err != nil {
			c.mu.Unlock()
			c.t.Errorf("%s: insert: %v", c.replica, err)
			return
		}
		ops = append(ops, inserted...)
	}
	out := make([]wire.Op, len(ops))
	for i, op := range ops {
		out[i] = wire.FromCRDT(op)
		id := op.ID.String()
		c.unacked[id] = out[i]
		c.order = append(c.order, id)
	}
	conn := c.conn
	c.mu.Unlock()

	if conn != nil && len(out) > 0 {
		c.write(conn, map[string]any{"type": "ops", "ops": out})
	}
}

// read applies everything the server sends until the connection closes.
func (c *client) read(conn *websocket.Conn, joined, done chan struct{}) {
	defer close(done)
	for {
		_, data, err := conn.Read(context.Background())
		if err != nil {
			return
		}
		var msg struct {
			Type string `json:"type"`
			Ops  []struct {
				Seq int64   `json:"seq"`
				Op  wire.Op `json:"op"`
			} `json:"ops"`
			Acks []struct {
				OpID  string `json:"opId"`
				Seq   int64  `json:"seq"`
				Error string `json:"error"`
			} `json:"acks"`
		}
		if err := json.Unmarshal(data, &msg); err != nil {
			c.t.Errorf("%s: bad message %s", c.replica, data)
			return
		}

		c.mu.Lock()
		switch msg.Type {
		case "remote":
			for _, o := range msg.Ops {
				op, err := o.Op.ToCRDT()
				if err == nil {
					_, err = c.doc.Apply(op)
				}
				if err != nil {
					c.t.Errorf("%s: apply remote %d: %v", c.replica, o.Seq, err)
				}
				c.saw(o.Seq)
			}
		case "ack":
			for _, a := range msg.Acks {
				if a.Error != "" {
					c.t.Errorf("%s: server rejected %s: %s", c.replica, a.OpID, a.Error)
				}
				delete(c.unacked, a.OpID)
				c.saw(a.Seq)
			}
		case "joined":
			close(joined)
		}
		c.mu.Unlock()
	}
}

// saw records a seq; last only advances over seqs with none missing below.
func (c *client) saw(seq int64) {
	if seq <= c.last {
		return
	}
	c.seqs[seq] = true
	for c.seqs[c.last+1] {
		delete(c.seqs, c.last+1)
		c.last++
	}
}

func (c *client) write(conn *websocket.Conn, msg any) {
	data, err := json.Marshal(msg)
	if err != nil {
		c.t.Error(err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// A write on a connection that is closing is lost, like a real one;
	// the op stays unacked and is resent after the next connect.
	_ = conn.Write(ctx, websocket.MessageText, data)
}

func (c *client) text() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.doc.Text()
}

func (c *client) length() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.doc.Len()
}

func (c *client) pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.unacked)
}

// reconnect replaces a connection the server closed (or that was dropped).
func (c *client) reconnect() {
	c.t.Helper()
	c.disconnect()
	c.connect()
}

// closedByServer waits until the server closes the connection.
func (c *client) closedByServer(wait time.Duration) bool {
	c.mu.Lock()
	done := c.done
	c.mu.Unlock()
	select {
	case <-done:
		return true
	case <-time.After(wait):
		return false
	}
}
