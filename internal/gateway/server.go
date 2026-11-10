package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/SrijitGyawali/Collaboration-Engine/internal/store"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/syncsvc"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/wire"
)

const (
	maxMessageBytes = 1 << 20 // one paste of a long document fits
	sendQueue       = 256     // messages waiting for one client
	joinTimeout     = 10 * time.Second
	writeTimeout    = 10 * time.Second
)

// Backend is what the gateway needs from the sync service: store edits, and
// read history. In-process it is a *syncsvc.Service; after the split it is a
// gRPC client.
type Backend interface {
	Apply(ctx context.Context, docID string, ops []wire.Op) ([]syncsvc.Ack, error)
	OperationsSince(ctx context.Context, docID string, afterSeq int64, limit int) ([]syncsvc.SeqOp, error)
}

// Config configures a Server.
type Config struct {
	Backend Backend
	Log     *slog.Logger
	// AllowedOrigins are the hosts (such as "localhost:3000") whose pages
	// may connect. Browsers send an Origin header and the server refuses
	// any other host, so other websites cannot open connections with a
	// visitor's browser. Clients that send no Origin (not browsers) are
	// allowed.
	AllowedOrigins []string
}

// Server accepts WebSocket connections and keeps each document's clients in
// a room. It implements http.Handler.
type Server struct {
	cfg  Config
	hub  *hub
	base context.Context
	stop context.CancelFunc
	wg   sync.WaitGroup

	mu      sync.Mutex
	conns   map[*websocket.Conn]struct{}
	closing bool
}

// NewServer returns a Server.
func NewServer(cfg Config) *Server {
	base, stop := context.WithCancel(context.Background())
	return &Server{cfg: cfg, hub: newHub(), base: base, stop: stop, conns: make(map[*websocket.Conn]struct{})}
}

// Close disconnects every client ("going away") and waits for their
// connections to finish. http.Server.Shutdown does not do this, because
// WebSocket connections are hijacked from the HTTP server.
//
// Each connection is closed explicitly with "going away" so clients know to
// reconnect. Cancelling the connections' context instead would make the
// WebSocket library drop them without saying why.
func (s *Server) Close() {
	s.mu.Lock()
	s.closing = true
	conns := make([]*websocket.Conn, 0, len(s.conns))
	for conn := range s.conns {
		conns = append(conns, conn)
	}
	s.mu.Unlock()

	for _, conn := range conns {
		go closeWith(conn, websocket.StatusGoingAway, "server shutting down")
	}
	s.wg.Wait()
	s.stop()
}

// track registers a live connection, or reports false if the server is
// closing and the connection must be turned away.
func (s *Server) track(conn *websocket.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return false
	}
	s.conns[conn] = struct{}{}
	s.wg.Add(1)
	return true
}

func (s *Server) untrack(conn *websocket.Conn) {
	s.mu.Lock()
	delete(s.conns, conn)
	s.mu.Unlock()
	s.wg.Done()
}

// ServeHTTP upgrades the request to a WebSocket and serves it until either
// side closes it.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: s.cfg.AllowedOrigins})
	if err != nil {
		s.cfg.Log.Debug("websocket handshake refused", "err", err, "origin", r.Header.Get("Origin"))
		return // Accept has already written the HTTP error
	}
	conn.SetReadLimit(maxMessageBytes)

	if !s.track(conn) {
		closeWith(conn, websocket.StatusGoingAway, "server shutting down")
		return
	}
	defer s.untrack(conn)
	ctx, cancel := context.WithCancel(s.base)
	defer cancel()
	s.serve(ctx, conn)
}

// serve runs one connection: join, catch-up, then edits until it closes.
func (s *Server) serve(ctx context.Context, conn *websocket.Conn) {
	defer func() { _ = conn.CloseNow() }()

	joinCtx, cancel := context.WithTimeout(ctx, joinTimeout)
	msg, err := read(joinCtx, conn)
	cancel()
	if err != nil {
		closeForReadError(conn, err, "expected a join message")
		return
	}
	if msg.Type != typeJoin || !store.ValidDocumentID(msg.Doc) || msg.LastSeq < 0 {
		closeWith(conn, websocket.StatusPolicyViolation, "first message must be join with a valid document name")
		return
	}
	doc := msg.Doc

	ctx, cancelConn := context.WithCancel(ctx)
	defer cancelConn()
	var once sync.Once
	c := newClient(sendQueue, func(reason string) {
		once.Do(func() {
			go closeWith(conn, websocket.StatusTryAgainLater, reason)
			cancelConn()
		})
	})
	go s.write(ctx, conn, c, cancelConn)

	// Join the room before reading history, so nothing committed in between
	// is missed. An op may then arrive twice (live and in history); clients
	// ignore what they already have.
	s.hub.join(doc, c)
	defer s.hub.leave(doc, c)

	// A reconnecting client already has everything up to LastSeq.
	last, err := s.catchUp(ctx, c, doc, msg.LastSeq)
	if err != nil {
		s.fail(conn, "could not load the document", err)
		return
	}
	if c.push(ctx, encode(serverMessage{Type: typeJoined, Doc: doc, Seq: last})) != nil {
		return
	}

	for {
		msg, err := read(ctx, conn)
		if err != nil {
			closeForReadError(conn, err, "")
			return
		}
		if msg.Type != typeOps {
			closeWith(conn, websocket.StatusPolicyViolation, "unexpected message type "+msg.Type)
			return
		}
		if err := s.handleOps(ctx, c, doc, msg.Ops); err != nil {
			s.fail(conn, "could not save edits; reconnect and send them again", err)
			return
		}
	}
}

// catchUp sends every stored op after afterSeq, one page per message, and
// returns the last sequence number sent (afterSeq if none).
func (s *Server) catchUp(ctx context.Context, c *client, doc string, afterSeq int64) (int64, error) {
	for {
		page, err := s.cfg.Backend.OperationsSince(ctx, doc, afterSeq, store.MaxPage)
		if err != nil {
			return 0, err
		}
		if len(page) == 0 {
			return afterSeq, nil
		}
		ops := make([]seqOp, len(page))
		for i, op := range page {
			ops[i] = seqOp{Seq: op.Seq, Op: op.Op}
		}
		if err := c.push(ctx, encode(serverMessage{Type: typeRemote, Ops: ops})); err != nil {
			return 0, err
		}
		afterSeq = page[len(page)-1].Seq
		if len(page) < store.MaxPage {
			return afterSeq, nil
		}
	}
}

// handleOps stores a client's edits, acks them to the client, and broadcasts
// the newly stored ones to the rest of the room.
func (s *Server) handleOps(ctx context.Context, c *client, doc string, ops []wire.Op) error {
	if len(ops) == 0 {
		return nil
	}
	acks, err := s.cfg.Backend.Apply(ctx, doc, ops)
	if err != nil {
		return err
	}

	reply := make([]ackJSON, len(acks))
	var fresh []seqOp
	for i, a := range acks {
		reply[i] = ackJSON{OpID: a.OpID, Seq: a.Seq, Duplicate: a.Duplicate, Error: a.Err}
		// Duplicates were broadcast the first time; rejected ops never are.
		if a.Err == "" && !a.Duplicate {
			fresh = append(fresh, seqOp{Seq: a.Seq, Op: ops[i]})
		}
	}
	if err := c.push(ctx, encode(serverMessage{Type: typeAck, Acks: reply})); err != nil {
		return nil // the connection is closing anyway
	}
	if len(fresh) > 0 {
		s.hub.broadcast(doc, c, encode(serverMessage{Type: typeRemote, Ops: fresh}))
	}
	return nil
}

// write sends queued messages until ctx ends or a write fails.
func (s *Server) write(ctx context.Context, conn *websocket.Conn, c *client, done context.CancelFunc) {
	defer done()
	for {
		select {
		case msg := <-c.send:
			wctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := conn.Write(wctx, websocket.MessageText, msg)
			cancel()
			if err != nil {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

// fail reports a backend problem to the client and closes the connection
// with "try again later": the client reconnects, catches up, and resends
// whatever was not acknowledged.
func (s *Server) fail(conn *websocket.Conn, reason string, err error) {
	if errors.Is(err, context.Canceled) {
		return
	}
	s.cfg.Log.Error("gateway: "+reason, "err", err)
	closeWith(conn, websocket.StatusTryAgainLater, reason)
}

// errMalformed marks a message that is not a JSON text message.
var errMalformed = errors.New("gateway: malformed message")

// read reads one client message.
func read(ctx context.Context, conn *websocket.Conn) (clientMessage, error) {
	typ, data, err := conn.Read(ctx)
	if err != nil {
		return clientMessage{}, err
	}
	var msg clientMessage
	if typ != websocket.MessageText {
		return msg, fmt.Errorf("%w: binary", errMalformed)
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return msg, fmt.Errorf("%w: %w", errMalformed, err)
	}
	return msg, nil
}

// closeForReadError closes the connection after a failed read: with
// "unsupported data" for a malformed message, with reason (if any) for
// anything else, such as a timeout. A connection the client closed needs
// nothing more.
func closeForReadError(conn *websocket.Conn, err error, reason string) {
	switch {
	case errors.Is(err, errMalformed):
		closeWith(conn, websocket.StatusUnsupportedData, "messages must be JSON text")
	case reason != "":
		closeWith(conn, websocket.StatusPolicyViolation, reason)
	}
}

func closeWith(conn *websocket.Conn, code websocket.StatusCode, reason string) {
	_ = conn.Close(code, reason)
}
