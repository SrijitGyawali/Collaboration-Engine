package syncrpc

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	syncv1 "github.com/SrijitGyawali/Collaboration-Engine/gen/collab/sync/v1"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/crdt"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/store"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/store/memstore"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/syncsvc"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/wire"
)

// serve runs a gRPC server for svc on an in-memory connection (bufconn: real
// gRPC and HTTP/2, no network port) and returns a connection to it.
func serve(t *testing.T, svc Service) *grpc.ClientConn {
	t.Helper()
	if svc == nil {
		s := syncsvc.New(memstore.New(), slog.New(slog.NewTextHandler(io.Discard, nil)), syncsvc.Options{})
		t.Cleanup(s.Close)
		svc = s
	}
	lis := bufconn.Listen(1 << 20)
	g := grpc.NewServer()
	Register(g, svc)
	go func() { _ = g.Serve(lis) }()
	t.Cleanup(g.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// typed returns the protobuf ops for text typed by replica.
func typed(replica, text string) []*syncv1.Operation {
	ops, err := crdt.NewDocument(replica).InsertText(0, text)
	if err != nil {
		panic(err)
	}
	out := make([]*syncv1.Operation, len(ops))
	for i, op := range ops {
		out[i] = toProto("doc", 0, wire.FromCRDT(op))
	}
	return out
}

func TestApplyOperations(t *testing.T) {
	client := syncv1.NewSyncServiceClient(serve(t, nil))
	ctx := t.Context()
	ops := typed("alice", "abc")

	resp, err := client.ApplyOperations(ctx, &syncv1.ApplyOperationsRequest{DocId: "doc", ClientId: "alice", Operations: ops[:2]})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.GetAcks()) != 2 || resp.GetAcks()[1].GetSeq() != 2 {
		t.Fatalf("acks = %v", resp.GetAcks())
	}

	bad := &syncv1.Operation{Id: &syncv1.ElementId{Counter: 9, Replica: "alice"}, Value: "x"} // no type
	resp, err = client.ApplyOperations(ctx, &syncv1.ApplyOperationsRequest{DocId: "doc", Operations: []*syncv1.Operation{ops[0], bad, ops[2]}})
	if err != nil {
		t.Fatal(err)
	}
	acks := resp.GetAcks()
	if !acks[0].GetDuplicate() || acks[0].GetSeq() != 1 {
		t.Errorf("retried op: %v", acks[0])
	}
	if acks[1].GetError() == "" || acks[1].GetSeq() != 0 {
		t.Errorf("op without a type: %v", acks[1])
	}
	if acks[2].GetSeq() != 3 || acks[2].GetDuplicate() {
		t.Errorf("new op: %v", acks[2])
	}
}

func TestGetOperationsSinceStreamsPages(t *testing.T) {
	client := syncv1.NewSyncServiceClient(serve(t, nil))
	ctx := t.Context()
	ops := typed("alice", strings.Repeat("x", store.MaxPage+10))
	for i := 0; i < len(ops); i += syncsvc.MaxBatch {
		if _, err := client.ApplyOperations(ctx, &syncv1.ApplyOperationsRequest{DocId: "doc", Operations: ops[i:min(i+syncsvc.MaxBatch, len(ops))]}); err != nil {
			t.Fatal(err)
		}
	}

	read := func(after int64, limit int32) (pages int, seqs []int64) {
		t.Helper()
		stream, err := client.GetOperationsSince(ctx, &syncv1.GetOperationsSinceRequest{DocId: "doc", AfterSeq: after, Limit: limit})
		if err != nil {
			t.Fatal(err)
		}
		for {
			resp, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				return pages, seqs
			}
			if err != nil {
				t.Fatal(err)
			}
			pages++
			for _, op := range resp.GetOperations() {
				seqs = append(seqs, op.GetSeq())
			}
		}
	}

	pages, seqs := read(0, 0)
	if pages != 2 || len(seqs) != len(ops) || seqs[0] != 1 || seqs[len(seqs)-1] != int64(len(ops)) {
		t.Errorf("everything: %d pages, %d ops", pages, len(seqs))
	}
	if _, seqs := read(5, 3); len(seqs) != 3 || seqs[0] != 6 || seqs[2] != 8 {
		t.Errorf("after 5, limit 3: %v", seqs)
	}
	if pages, seqs := read(int64(len(ops)), 0); pages != 0 || len(seqs) != 0 {
		t.Errorf("past the end: %d pages", pages)
	}
}

func TestErrorsMapToCodes(t *testing.T) {
	client := syncv1.NewSyncServiceClient(serve(t, nil))
	ctx := t.Context()

	_, err := client.ApplyOperations(ctx, &syncv1.ApplyOperationsRequest{DocId: "has space", Operations: typed("a", "x")})
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("invalid document: %v", err)
	}

	stream, err := client.GetOperationsSince(ctx, &syncv1.GetOperationsSinceRequest{DocId: "doc", AfterSeq: -1})
	if err == nil {
		_, err = stream.Recv()
	}
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("negative after_seq: %v", err)
	}

	_, err = client.GetSnapshot(ctx, &syncv1.GetSnapshotRequest{DocId: "doc"})
	if status.Code(err) != codes.Unimplemented {
		t.Errorf("GetSnapshot: %v", err)
	}
}

// failingService stands in for a sync service whose database is down.
type failingService struct{}

func (failingService) Apply(context.Context, string, []wire.Op) ([]syncsvc.Ack, error) {
	return nil, errors.New("database unreachable")
}

func (failingService) OperationsSince(context.Context, string, int64, int) ([]syncsvc.SeqOp, error) {
	return nil, errors.New("database unreachable")
}

// A temporary failure must reach the client as UNAVAILABLE, so it retries.
func TestTemporaryFailuresAreUnavailable(t *testing.T) {
	client := syncv1.NewSyncServiceClient(serve(t, failingService{}))
	_, err := client.ApplyOperations(t.Context(), &syncv1.ApplyOperationsRequest{DocId: "doc", Operations: typed("a", "x")})
	if status.Code(err) != codes.Unavailable {
		t.Errorf("code = %v, want Unavailable", status.Code(err))
	}
}
