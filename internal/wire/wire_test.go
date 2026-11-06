package wire

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/SrijitGyawali/Collaboration-Engine/internal/crdt"
)

func TestRoundTrip(t *testing.T) {
	doc := crdt.NewDocument("alice")
	ops, err := doc.InsertText(0, "hé👋")
	if err != nil {
		t.Fatal(err)
	}
	del, err := doc.DeleteAt(1)
	if err != nil {
		t.Fatal(err)
	}
	ops = append(ops, del)

	for _, op := range ops {
		// Through the wire struct and through JSON, as on a WebSocket.
		data, err := json.Marshal(FromCRDT(op))
		if err != nil {
			t.Fatal(err)
		}
		var w Op
		if err := json.Unmarshal(data, &w); err != nil {
			t.Fatal(err)
		}
		got, err := w.ToCRDT()
		if err != nil {
			t.Fatalf("ToCRDT(%s): %v", data, err)
		}
		if got != op {
			t.Errorf("round trip of %s: got %+v, want %+v", data, got, op)
		}
	}
}

func TestJSONShape(t *testing.T) {
	insert := crdt.Op{Kind: crdt.OpInsert, ID: crdt.ID{Counter: 42, Replica: "alice"}, After: crdt.ID{Counter: 17, Replica: "bob"}, Value: 'X'}
	atStart := crdt.Op{Kind: crdt.OpInsert, ID: crdt.ID{Counter: 1, Replica: "alice"}, Value: 'a'}
	del := crdt.Op{Kind: crdt.OpDelete, ID: crdt.ID{Counter: 43, Replica: "alice"}, Target: crdt.ID{Counter: 17, Replica: "bob"}}

	for _, tt := range []struct {
		op   crdt.Op
		want string
	}{
		{insert, `{"kind":"insert","id":{"counter":42,"replica":"alice"},"after":{"counter":17,"replica":"bob"},"value":"X"}`},
		{atStart, `{"kind":"insert","id":{"counter":1,"replica":"alice"},"value":"a"}`},
		{del, `{"kind":"delete","id":{"counter":43,"replica":"alice"},"target":{"counter":17,"replica":"bob"}}`},
	} {
		data, err := json.Marshal(FromCRDT(tt.op))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != tt.want {
			t.Errorf("JSON = %s\n want %s", data, tt.want)
		}
	}
}

func TestToCRDTRejectsMalformed(t *testing.T) {
	id := ID{Counter: 5, Replica: "alice"}
	parent := &ID{Counter: 1, Replica: "bob"}

	for _, tt := range []struct {
		name string
		op   Op
	}{
		{"unknown kind", Op{Kind: "move", ID: id}},
		{"insert without value", Op{Kind: "insert", ID: id}},
		{"insert of two characters", Op{Kind: "insert", ID: id, Value: "ab"}},
		{"insert of invalid UTF-8", Op{Kind: "insert", ID: id, Value: "\xff"}},
		{"insert with target", Op{Kind: "insert", ID: id, Value: "a", Target: parent}},
		{"delete without target", Op{Kind: "delete", ID: id}},
		{"delete with value", Op{Kind: "delete", ID: id, Target: parent, Value: "a"}},
		{"missing id", Op{Kind: "insert", Value: "a"}},
		{"insert older than parent", Op{Kind: "insert", ID: ID{Counter: 1, Replica: "a"}, After: &ID{Counter: 9, Replica: "b"}, Value: "a"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.op.ToCRDT(); !errors.Is(err, ErrMalformed) {
				t.Errorf("ToCRDT() = %v, want ErrMalformed", err)
			}
		})
	}
}
