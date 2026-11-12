package syncrpc

import (
	"context"
	"errors"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	syncv1 "github.com/SrijitGyawali/Collaboration-Engine/gen/collab/sync/v1"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/store"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/syncsvc"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/wire"
)

// Service is what the server exposes: the sync service's two operations.
// *syncsvc.Service implements it.
type Service interface {
	Apply(ctx context.Context, docID string, ops []wire.Op) ([]syncsvc.Ack, error)
	OperationsSince(ctx context.Context, docID string, afterSeq int64, limit int) ([]syncsvc.SeqOp, error)
}

// Server implements the generated SyncServiceServer on top of a Service.
type Server struct {
	syncv1.UnimplementedSyncServiceServer // GetSnapshot answers UNIMPLEMENTED for now
	svc                                   Service
}

// Register adds a Server for svc to a gRPC server.
func Register(g *grpc.Server, svc Service) {
	syncv1.RegisterSyncServiceServer(g, &Server{svc: svc})
}

// ApplyOperations stores a batch of ops and returns one ack per op, in order.
func (s *Server) ApplyOperations(ctx context.Context, req *syncv1.ApplyOperationsRequest) (*syncv1.ApplyOperationsResponse, error) {
	ops := make([]wire.Op, len(req.GetOperations()))
	for i, op := range req.GetOperations() {
		ops[i] = fromProto(op)
	}
	acks, err := s.svc.Apply(ctx, req.GetDocId(), ops)
	if err != nil {
		return nil, toStatus(err)
	}

	resp := &syncv1.ApplyOperationsResponse{Acks: make([]*syncv1.Ack, len(acks))}
	for i, a := range acks {
		resp.Acks[i] = &syncv1.Ack{OpId: a.OpID, Seq: a.Seq, Duplicate: a.Duplicate, Error: a.Err}
	}
	return resp, nil
}

// GetOperationsSince streams a document's history after a seq, one page per
// message, up to the request's limit (0 for everything).
func (s *Server) GetOperationsSince(req *syncv1.GetOperationsSinceRequest, stream grpc.ServerStreamingServer[syncv1.GetOperationsSinceResponse]) error {
	if req.GetAfterSeq() < 0 || req.GetLimit() < 0 {
		return status.Error(codes.InvalidArgument, "after_seq and limit must not be negative")
	}
	ctx := stream.Context()
	after, left := req.GetAfterSeq(), int(req.GetLimit())

	for left > 0 || req.GetLimit() == 0 {
		size := store.MaxPage
		if req.GetLimit() > 0 {
			size = min(size, left)
		}
		page, err := s.svc.OperationsSince(ctx, req.GetDocId(), after, size)
		if err != nil {
			return toStatus(err)
		}
		if len(page) == 0 {
			return nil
		}
		resp := &syncv1.GetOperationsSinceResponse{Operations: make([]*syncv1.Operation, len(page))}
		for i, op := range page {
			resp.Operations[i] = toProto(req.GetDocId(), op.Seq, op.Op)
		}
		if err := stream.Send(resp); err != nil {
			return err
		}
		after = page[len(page)-1].Seq
		left -= len(page)
		if len(page) < size {
			return nil
		}
	}
	return nil
}

// toStatus maps service errors to gRPC codes, so clients can tell a bad
// request (do not retry) from a temporary failure (retry later).
func toStatus(err error) error {
	switch {
	case errors.Is(err, store.ErrInvalidDocumentID), errors.Is(err, syncsvc.ErrBatchTooLarge):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return status.FromContextError(err).Err()
	default:
		// A database that is down, a service shutting down: all temporary.
		return status.Error(codes.Unavailable, err.Error())
	}
}
