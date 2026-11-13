package syncrpc

import (
	"reflect"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/SrijitGyawali/Collaboration-Engine/internal/crdt"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/gateway"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/store"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/syncsvc"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/wire"
)

// The gateway must be able to use the client exactly like the in-process
// service.
var _ gateway.Backend = (*Client)(nil)

func wireTyped(replica, text string) []wire.Op {
	ops, err := crdt.NewDocument(replica).InsertText(0, text)
	if err != nil {
		panic(err)
	}
	out := make([]wire.Op, len(ops))
	for i, op := range ops {
		out[i] = wire.FromCRDT(op)
	}
	return out
}

// The whole remote path: Client -> gRPC -> Server -> syncsvc -> memstore.
func TestClientApply(t *testing.T) {
	c := NewClient(serve(t, nil))
	ctx := t.Context()
	ops := wireTyped("alice", "ab")

	acks, err := c.Apply(ctx, "doc", ops)
	if err != nil {
		t.Fatal(err)
	}
	if acks[0] != (syncsvc.Ack{OpID: "1@alice", Seq: 1}) || acks[1] != (syncsvc.Ack{OpID: "2@alice", Seq: 2}) {
		t.Errorf("acks = %+v", acks)
	}

	acks, err = c.Apply(ctx, "doc", ops[:1])
	if err != nil || !acks[0].Duplicate || acks[0].Seq != 1 {
		t.Errorf("retry = %+v, %v", acks, err)
	}
}

func TestClientOperationsSince(t *testing.T) {
	c := NewClient(serve(t, nil))
	ctx := t.Context()
	ops := wireTyped("alice", strings.Repeat("y", store.MaxPage+3))
	for i := 0; i < len(ops); i += syncsvc.MaxBatch {
		if _, err := c.Apply(ctx, "doc", ops[i:min(i+syncsvc.MaxBatch, len(ops))]); err != nil {
			t.Fatal(err)
		}
	}

	got, err := c.OperationsSince(ctx, "doc", 1, store.MaxPage)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != store.MaxPage || got[0].Seq != 2 || !reflect.DeepEqual(got[0].Op, ops[1]) {
		t.Errorf("page: %d ops, first %+v", len(got), got[0])
	}

	rest, err := c.OperationsSince(ctx, "doc", int64(store.MaxPage), 0)
	if err != nil || len(rest) != 3 || rest[2].Seq != int64(len(ops)) {
		t.Errorf("rest = %d ops, %v", len(rest), err)
	}
}

func TestClientErrorsKeepTheirCode(t *testing.T) {
	c := NewClient(serve(t, nil))
	_, err := c.Apply(t.Context(), "has space", wireTyped("a", "x"))
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("err = %v, want InvalidArgument", err)
	}
}

// A sync service that is not running must not make Dial fail: gRPC connects
// lazily and the gateway can start before the sync service.
func TestDialDoesNotNeedTheServiceUp(t *testing.T) {
	c, err := Dial("127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
}
