package gateway

import (
	"encoding/json"

	"github.com/SrijitGyawali/Collaboration-Engine/internal/wire"
)

// The WebSocket protocol between browser and gateway. Every message is one
// JSON object with a "type".
//
// Client to server:
//
//	{"type":"join","doc":"demo","replica":"k3j9x2","name":"Ann","lastSeq":41}
//	    first message, exactly once. replica is the name the client's CRDT
//	    uses in op IDs; name (optional) is shown to others; lastSeq
//	    (optional) is the last seq the client already has
//	{"type":"ops","ops":[<wire op>, ...]}   local edits
//
// Server to client:
//
//	{"type":"remote","ops":[{"seq":7,"op":<wire op>}, ...]}  history and others' edits
//	{"type":"joined","doc":"demo","seq":7,"you":{"replica":"k3j9x2","name":"Ann","color":"#2f6fde"}}
//	    catch-up done; seq is the last one sent (or lastSeq if nothing was
//	    missed; 0 is omitted); you is the identity the server gave the client
//	{"type":"ack","acks":[{"opId":"3@alice","seq":8}, ...]}  one per op sent, in order
//
// When the server closes a connection, the WebSocket close reason says why.
//
// Ops use the wire format (internal/wire). Sequence numbers say where each op
// is in the document's history; a client keeps the last one it has seen.

const (
	typeJoin   = "join"
	typeOps    = "ops"
	typeRemote = "remote"
	typeJoined = "joined"
	typeAck    = "ack"
)

// clientMessage is any message a client sends; fields unused by its type are
// empty.
type clientMessage struct {
	Type    string `json:"type"`
	Doc     string `json:"doc,omitempty"`
	Replica string `json:"replica,omitempty"`
	Name    string `json:"name,omitempty"`
	// LastSeq is, on join, the last sequence number the client already has;
	// catch-up sends only what comes after it. 0 means "nothing yet".
	LastSeq int64     `json:"lastSeq,omitempty"`
	Ops     []wire.Op `json:"ops,omitempty"`
}

// serverMessage is any message the server sends.
type serverMessage struct {
	Type string    `json:"type"`
	Doc  string    `json:"doc,omitempty"`
	Seq  int64     `json:"seq,omitempty"`
	Ops  []seqOp   `json:"ops,omitempty"`
	Acks []ackJSON `json:"acks,omitempty"`
	You  *identity `json:"you,omitempty"`
}

type seqOp struct {
	Seq int64   `json:"seq"`
	Op  wire.Op `json:"op"`
}

type ackJSON struct {
	OpID      string `json:"opId"`
	Seq       int64  `json:"seq,omitempty"`
	Duplicate bool   `json:"duplicate,omitempty"`
	// Error is set when the op was rejected; the client should drop it.
	Error string `json:"error,omitempty"`
}

// encode marshals a server message. It cannot fail: every field is a plain
// value or a wire op.
func encode(m serverMessage) []byte {
	data, err := json.Marshal(m)
	if err != nil {
		panic("gateway: encode: " + err.Error())
	}
	return data
}
