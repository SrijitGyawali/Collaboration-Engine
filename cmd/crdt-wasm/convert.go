//go:build js && wasm

package main

import (
	"fmt"
	"syscall/js"

	"github.com/SrijitGyawali/Collaboration-Engine/internal/wire"
)

// readOp reads a JavaScript op object in the wire format. Ops come from other
// replicas, through the network and the page's own code, so every field's type
// is checked before it is read; the result is then validated by
// wire.Op.ToCRDT.
func readOp(v js.Value) (wire.Op, error) {
	if v.Type() != js.TypeObject {
		return wire.Op{}, fmt.Errorf("%w: op must be an object", wire.ErrMalformed)
	}
	kind := v.Get("kind")
	if kind.Type() != js.TypeString {
		return wire.Op{}, fmt.Errorf("%w: kind must be a string", wire.ErrMalformed)
	}
	id, err := readID(v.Get("id"), "id")
	if err != nil {
		return wire.Op{}, err
	}
	op := wire.Op{Kind: kind.String(), ID: id}

	if after := v.Get("after"); !isMissing(after) {
		a, err := readID(after, "after")
		if err != nil {
			return wire.Op{}, err
		}
		op.After = &a
	}
	if target := v.Get("target"); !isMissing(target) {
		t, err := readID(target, "target")
		if err != nil {
			return wire.Op{}, err
		}
		op.Target = &t
	}
	if value := v.Get("value"); !isMissing(value) {
		if value.Type() != js.TypeString {
			return wire.Op{}, fmt.Errorf("%w: value must be a string", wire.ErrMalformed)
		}
		op.Value = value.String()
	}
	return op, nil
}

// readID reads a {counter, replica} object. name says which field it was, for
// the error message.
func readID(v js.Value, name string) (wire.ID, error) {
	if v.Type() != js.TypeObject {
		return wire.ID{}, fmt.Errorf("%w: %s must be an object", wire.ErrMalformed, name)
	}
	counter, replica := v.Get("counter"), v.Get("replica")
	if counter.Type() != js.TypeNumber || replica.Type() != js.TypeString {
		return wire.ID{}, fmt.Errorf("%w: %s must be {counter: number, replica: string}", wire.ErrMalformed, name)
	}
	c, err := wire.CounterFromFloat(counter.Float())
	if err != nil {
		return wire.ID{}, err
	}
	return wire.ID{Counter: c, Replica: replica.String()}, nil
}

// isMissing reports whether an optional field is absent. JSON.parse never
// produces undefined, so null is accepted as "absent" too.
func isMissing(v js.Value) bool {
	return v.Type() == js.TypeUndefined || v.Type() == js.TypeNull
}
