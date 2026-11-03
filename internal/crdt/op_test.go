package crdt

import (
	"errors"
	"testing"
)

func TestOpValidate(t *testing.T) {
	tests := []struct {
		name  string
		op    Op
		valid bool
	}{
		{"insert at start", Op{Kind: OpInsert, ID: ID{1, "a"}, After: Root, Value: 'x'}, true},
		{"insert after another char", Op{Kind: OpInsert, ID: ID{2, "a"}, After: ID{1, "b"}, Value: 'x'}, true},
		{"delete", Op{Kind: OpDelete, ID: ID{3, "a"}, Target: ID{1, "b"}}, true},
		{"missing id", Op{Kind: OpInsert, After: Root, Value: 'x'}, false},
		{"missing replica", Op{Kind: OpInsert, ID: ID{1, ""}, Value: 'x'}, false},
		{"insert without value", Op{Kind: OpInsert, ID: ID{1, "a"}}, false},
		{"insert older than parent", Op{Kind: OpInsert, ID: ID{1, "a"}, After: ID{5, "b"}, Value: 'x'}, false},
		{"delete without target", Op{Kind: OpDelete, ID: ID{1, "a"}}, false},
		{"unknown kind", Op{ID: ID{1, "a"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.op.Validate()
			if tt.valid && err != nil {
				t.Errorf("Validate() = %v, want nil", err)
			}
			if !tt.valid && !errors.Is(err, ErrInvalidOp) {
				t.Errorf("Validate() = %v, want ErrInvalidOp", err)
			}
		})
	}
}

func TestOpDependency(t *testing.T) {
	parent, target := ID{1, "a"}, ID{2, "b"}

	if got := (Op{Kind: OpInsert, ID: ID{3, "a"}, After: parent}).Dependency(); got != parent {
		t.Errorf("insert depends on %v, want its parent %v", got, parent)
	}
	if got := (Op{Kind: OpDelete, ID: ID{3, "a"}, Target: target}).Dependency(); got != target {
		t.Errorf("delete depends on %v, want its target %v", got, target)
	}
	if got := (Op{Kind: OpInsert, ID: ID{1, "a"}, After: Root}).Dependency(); !got.IsRoot() {
		t.Errorf("insert at start depends on %v, want Root", got)
	}
}

func TestOpKindString(t *testing.T) {
	if OpInsert.String() != "insert" || OpDelete.String() != "delete" {
		t.Errorf("got %q and %q", OpInsert, OpDelete)
	}
	if got := OpKind(9).String(); got != "OpKind(9)" {
		t.Errorf("unknown kind String() = %q", got)
	}
}
