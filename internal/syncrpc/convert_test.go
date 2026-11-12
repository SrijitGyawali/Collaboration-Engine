package syncrpc

import (
	"reflect"
	"testing"

	"google.golang.org/protobuf/proto"

	syncv1 "github.com/SrijitGyawali/Collaboration-Engine/gen/collab/sync/v1"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/crdt"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/wire"
)

// Every kind of op must survive wire -> protobuf (bytes) -> wire unchanged.
func TestRoundTrip(t *testing.T) {
	doc := crdt.NewDocument("alice")
	ops, err := doc.InsertText(0, "a👋")
	if err != nil {
		t.Fatal(err)
	}
	del, err := doc.DeleteAt(0)
	if err != nil {
		t.Fatal(err)
	}

	for _, op := range append(ops, del) {
		w := wire.FromCRDT(op)
		data, err := proto.Marshal(toProto("doc", 7, w))
		if err != nil {
			t.Fatal(err)
		}
		var decoded syncv1.Operation
		if err := proto.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.GetDocId() != "doc" || decoded.GetSeq() != 7 || decoded.GetOpId() != op.ID.String() {
			t.Errorf("envelope = %v", &decoded)
		}
		if got := fromProto(&decoded); !reflect.DeepEqual(got, w) {
			t.Errorf("round trip of %+v gave %+v", w, got)
		}
	}
}

func TestUnknownTypeBecomesAnEmptyKind(t *testing.T) {
	w := fromProto(&syncv1.Operation{Id: &syncv1.ElementId{Counter: 1, Replica: "a"}, Value: "x"})
	if w.Kind != "" {
		t.Errorf("Kind = %q, want empty", w.Kind)
	}
	if _, err := w.ToCRDT(); err == nil {
		t.Error("an op with no type converted to a valid CRDT op")
	}
}
