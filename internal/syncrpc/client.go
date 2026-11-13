package syncrpc

import (
	"context"
	"errors"
	"fmt"
	"io"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	syncv1 "github.com/SrijitGyawali/Collaboration-Engine/gen/collab/sync/v1"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/syncsvc"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/wire"
)

// retryPolicy lets gRPC retry calls that failed with UNAVAILABLE (the sync
// service restarting, a database blip) a few times with backoff, before the
// gateway gives up and asks the browser to reconnect. Retrying is safe:
// ApplyOperations is idempotent, because every op carries its own ID and a
// repeat comes back as a duplicate.
const retryPolicy = `{
  "methodConfig": [{
    "name": [{"service": "collab.sync.v1.SyncService"}],
    "retryPolicy": {
      "maxAttempts": 4,
      "initialBackoff": "0.1s",
      "maxBackoff": "1s",
      "backoffMultiplier": 2,
      "retryableStatusCodes": ["UNAVAILABLE"]
    }
  }]
}`

// Client calls a remote sync service. It has the same methods as
// *syncsvc.Service, so the gateway uses either without knowing which.
type Client struct {
	conn *grpc.ClientConn
	rpc  syncv1.SyncServiceClient
}

// Dial connects to the sync service at target (host:port). gRPC connects
// lazily and reconnects by itself, so this does not fail if the service is
// not up yet.
//
// The connection is not encrypted: gateways and the sync service talk on a
// private network. TLS (or mutual TLS) comes with deployment.
func Dial(target string) (*Client, error) {
	conn, err := grpc.NewClient(target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(retryPolicy),
	)
	if err != nil {
		return nil, fmt.Errorf("syncrpc: dial %s: %w", target, err)
	}
	return NewClient(conn), nil
}

// NewClient uses an existing connection; Close then closes it.
func NewClient(conn *grpc.ClientConn) *Client {
	return &Client{conn: conn, rpc: syncv1.NewSyncServiceClient(conn)}
}

// Close closes the connection.
func (c *Client) Close() error {
	return c.conn.Close()
}

// Apply stores ops and returns one ack per op; see syncsvc.Service.Apply.
func (c *Client) Apply(ctx context.Context, docID string, ops []wire.Op) ([]syncsvc.Ack, error) {
	req := &syncv1.ApplyOperationsRequest{DocId: docID, Operations: make([]*syncv1.Operation, len(ops))}
	for i, op := range ops {
		req.Operations[i] = toProto(docID, 0, op)
		req.ClientId = op.ID.Replica
	}
	resp, err := c.rpc.ApplyOperations(ctx, req)
	if err != nil {
		return nil, err
	}
	if len(resp.GetAcks()) != len(ops) {
		return nil, fmt.Errorf("syncrpc: %d acks for %d ops", len(resp.GetAcks()), len(ops))
	}
	acks := make([]syncsvc.Ack, len(ops))
	for i, a := range resp.GetAcks() {
		acks[i] = syncsvc.Ack{OpID: a.GetOpId(), Seq: a.GetSeq(), Duplicate: a.GetDuplicate(), Err: a.GetError()}
	}
	return acks, nil
}

// OperationsSince returns stored ops after afterSeq, oldest first, at most
// limit of them; see syncsvc.Service.OperationsSince.
func (c *Client) OperationsSince(ctx context.Context, docID string, afterSeq int64, limit int) ([]syncsvc.SeqOp, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel() // ends the stream if we stop reading early
	stream, err := c.rpc.GetOperationsSince(ctx, &syncv1.GetOperationsSinceRequest{
		DocId:    docID,
		AfterSeq: afterSeq,
		Limit:    int32(max(limit, 0)), //nolint:gosec // limit is a page size, far below 2^31
	})
	if err != nil {
		return nil, err
	}

	var ops []syncsvc.SeqOp
	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return ops, nil
		}
		if err != nil {
			return nil, err
		}
		for _, op := range resp.GetOperations() {
			ops = append(ops, syncsvc.SeqOp{Seq: op.GetSeq(), Op: fromProto(op)})
		}
	}
}
