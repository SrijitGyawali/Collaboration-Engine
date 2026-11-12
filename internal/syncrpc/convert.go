// Package syncrpc puts the sync service on the network with gRPC: Server
// exposes a syncsvc.Service, and (for the gateway) a client implements the
// same interface remotely. Both sides convert between wire ops and the
// protobuf types generated from proto/collab/sync/v1/sync.proto.
package syncrpc

import (
	"fmt"

	syncv1 "github.com/SrijitGyawali/Collaboration-Engine/gen/collab/sync/v1"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/wire"
)

// toProto converts a wire op, with the document it belongs to and its seq (0
// if not stored yet), to the protobuf Operation.
func toProto(docID string, seq int64, w wire.Op) *syncv1.Operation {
	op := &syncv1.Operation{
		DocId: docID,
		OpId:  fmt.Sprintf("%d@%s", w.ID.Counter, w.ID.Replica),
		Id:    toProtoID(w.ID),
		Value: w.Value,
		Seq:   seq,
	}
	switch w.Kind {
	case "insert":
		op.Type = syncv1.OpType_OP_TYPE_INSERT
	case "delete":
		op.Type = syncv1.OpType_OP_TYPE_DELETE
	}
	if w.After != nil {
		op.After = toProtoID(*w.After)
	}
	if w.Target != nil {
		op.Target = toProtoID(*w.Target)
	}
	return op
}

// fromProto converts a protobuf Operation back to a wire op. It does not
// validate: an unknown type becomes an empty kind, which the sync service
// rejects for that op alone, with the reason in its ack.
func fromProto(op *syncv1.Operation) wire.Op {
	w := wire.Op{ID: fromProtoID(op.GetId()), Value: op.GetValue()}
	switch op.GetType() {
	case syncv1.OpType_OP_TYPE_INSERT:
		w.Kind = "insert"
	case syncv1.OpType_OP_TYPE_DELETE:
		w.Kind = "delete"
	}
	if op.GetAfter() != nil {
		after := fromProtoID(op.GetAfter())
		w.After = &after
	}
	if op.GetTarget() != nil {
		target := fromProtoID(op.GetTarget())
		w.Target = &target
	}
	return w
}

func toProtoID(id wire.ID) *syncv1.ElementId {
	return &syncv1.ElementId{Counter: id.Counter, Replica: id.Replica}
}

func fromProtoID(id *syncv1.ElementId) wire.ID {
	return wire.ID{Counter: id.GetCounter(), Replica: id.GetReplica()}
}
